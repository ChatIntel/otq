package otlp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"otq/internal/eval"
)

const sampleReq = `{
  "resourceSpans": [{
    "resource": {"attributes": []},
    "scopeSpans": [{
      "scope": {"name": "test"},
      "spans": [{
        "traceId": "abc123",
        "spanId": "s1",
        "parentSpanId": "",
        "name": "gen_ai.chat",
        "kind": "SPAN_KIND_CLIENT",
        "startTimeUnixNano": "1000000000",
        "endTimeUnixNano": "1500000000",
        "attributes": [
          {"key": "gen_ai.system", "value": {"stringValue": "openai"}},
          {"key": "gen_ai.usage.output_tokens", "value": {"intValue": "42"}}
        ],
        "status": {"code": 1, "message": ""}
      }]
    }]
  }]
}`

func TestAnyValueToValue(t *testing.T) {
	cases := []struct {
		name string
		av   AnyValue
		want eval.Value
	}{
		{"string", AnyValue{StringValue: strPtr("x")}, eval.String("x")},
		{"bool", AnyValue{BoolValue: boolPtr(true)}, eval.Bool(true)},
		{"int", AnyValue{IntValue: strPtr("42")}, eval.Number(42)},
		{"double", AnyValue{DoubleValue: floatPtr(1.5)}, eval.Number(1.5)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.av.ToValue()
			if !eval.Equal(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func strPtr(s string) *string     { return &s }
func boolPtr(b bool) *bool        { return &b }
func floatPtr(f float64) *float64 { return &f }

func TestLoadJSONFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.json")
	if err := os.WriteFile(path, []byte(sampleReq), 0o644); err != nil {
		t.Fatal(err)
	}
	spans, err := loadJSONFile(path)
	if err != nil {
		t.Fatalf("loadJSONFile: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if spans[0].Span.TraceID != "abc123" {
		t.Errorf("trace id = %q", spans[0].Span.TraceID)
	}
}

func TestLoadJSONLFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.jsonl")
	content := compactJSON(t, sampleReq) + "\n" + compactJSON(t, sampleReq) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	spans, err := loadJSONLFile(path)
	if err != nil {
		t.Fatalf("loadJSONLFile: %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2 (one per batch line)", len(spans))
	}
}

// compactJSON re-encodes a JSON document onto a single line, for building
// JSONL test fixtures out of the same multi-line sampleReq string.
func compactJSON(t *testing.T, s string) string {
	t.Helper()
	var req ExportTraceServiceRequest
	if err := json.Unmarshal([]byte(s), &req); err != nil {
		t.Fatalf("compactJSON: %v", err)
	}
	out, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("compactJSON: %v", err)
	}
	return string(out)
}

func TestLoadInputsMergesDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(sampleReq), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.jsonl"), []byte(compactJSON(t, sampleReq)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spans, err := LoadInputs([]string{dir})
	if err != nil {
		t.Fatalf("LoadInputs: %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2 merged across both files", len(spans))
	}
}

func TestLoadInputsMissingFile(t *testing.T) {
	_, err := LoadInputs([]string{"/nonexistent/path/trace.json"})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadInputsMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadInputs([]string{path})
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}
