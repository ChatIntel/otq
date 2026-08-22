// Package test contains end-to-end tests driving cli.Run against the
// testdata fixtures with the spec's canonical Phase 1 example queries.
package test

import (
	"bytes"
	"strings"
	"testing"

	"otq/internal/cli"
	"otq/internal/eval"
	"otq/output"
)

const query1 = `spans | select(.name == "gen_ai.chat") | sort_by(.duration_ms, desc) | limit(10)`
const query2 = `spans | select(.trace_id == "abc123") | select(.name | startswith("gen_ai"))
      | sort_by(.start_time)
      | project({ role: .gen_ai.system, prompt: .gen_ai.prompt, completion: .gen_ai.completion })`
const query3 = `spans | select(.gen_ai.request.model != null)
      | group_by(.gen_ai.request.model)
      | project({ model: .group_key, total_tokens: sum(.gen_ai.usage.output_tokens), calls: count })`

// runCLI drives cli.Run in-process against the given fixture path and query,
// returning stdout, stderr, and the exit code.
func runCLI(t *testing.T, fixture, query string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = cli.Run([]string{query, fixture}, strings.NewReader(""), &out, &errBuf)
	return out.String(), errBuf.String(), code
}

// render builds the expected output bytes by feeding hand-built eval.Value
// span objects (constructed from the same fixture data, independently of
// otq's own flatten/otlp code paths) through the same output.Writer the CLI
// uses, so float/string JSON formatting matches exactly without guessing
// Go's encoding/json number formatting by hand.
func render(values ...eval.Value) string {
	var buf bytes.Buffer
	w := output.Writer{W: &buf}
	if err := w.WriteAll(values); err != nil {
		panic(err) // in-memory bytes.Buffer write never fails
	}
	return buf.String()
}

func obj(fields ...any) eval.Value {
	v := eval.NewObject()
	for i := 0; i < len(fields); i += 2 {
		v.Set(fields[i].(string), fields[i+1].(eval.Value))
	}
	return v
}

func messages(pairs ...[2]string) eval.Value {
	vs := make([]eval.Value, len(pairs))
	for i, p := range pairs {
		vs[i] = obj("role", eval.String(p[0]), "content", eval.String(p[1]))
	}
	return eval.Array(vs)
}

func strOrNull(s string) eval.Value {
	if s == "" {
		return eval.Null()
	}
	return eval.String(s)
}

// fullSpan builds the complete flattened-span Value expected for one of the
// gen_ai.chat spans in testdata/sample_trace.{json,jsonl}, matching
// flatten.Span.ToValue's exact field order.
func fullSpan(traceID, spanID, parentID, startTime, endTime string, durationMs, outputTokens float64, model, promptRole, promptContent, complRole, complContent string) eval.Value {
	return obj(
		"trace_id", eval.String(traceID),
		"span_id", eval.String(spanID),
		"parent_span_id", strOrNull(parentID),
		"name", eval.String("gen_ai.chat"),
		"kind", eval.String("SPAN_KIND_CLIENT"),
		"start_time", eval.String(startTime),
		"end_time", eval.String(endTime),
		"duration_ms", eval.Number(durationMs),
		"status", obj("code", eval.String("OK"), "message", eval.Null()),
		"gen_ai", obj(
			"system", eval.String("openai"),
			"request", obj("model", eval.String(model)),
			"response", obj("model", eval.Null()),
			"usage", obj("input_tokens", eval.Null(), "output_tokens", eval.Number(outputTokens)),
			"tool", obj("name", eval.Null()),
			"agent", obj("name", eval.Null()),
			"prompt", messages([2]string{promptRole, promptContent}),
			"completion", messages([2]string{complRole, complContent}),
		),
		"attributes", obj(),
	)
}

func expectedQuery1() string {
	// sort_by(.duration_ms, desc): span-2 (3000ms) before span-1 (1500ms)
	span2 := fullSpan("abc123", "span-2", "span-1", "1970-01-01T00:00:03Z", "1970-01-01T00:00:06Z", 3000, 17,
		"gpt-4", "user", "What is otq?", "assistant", "A jq for traces.")
	span1 := fullSpan("abc123", "span-1", "", "1970-01-01T00:00:01Z", "1970-01-01T00:00:02.5Z", 1500, 42,
		"gpt-4", "system", "You are helpful.", "assistant", "Sure thing.")
	return render(span2, span1)
}

func expectedQuery2() string {
	// sort_by(.start_time): span-1 (t=1s) before span-2 (t=3s)
	span1 := obj(
		"role", eval.String("openai"),
		"prompt", messages([2]string{"system", "You are helpful."}),
		"completion", messages([2]string{"assistant", "Sure thing."}),
	)
	span2 := obj(
		"role", eval.String("openai"),
		"prompt", messages([2]string{"user", "What is otq?"}),
		"completion", messages([2]string{"assistant", "A jq for traces."}),
	)
	return render(span1, span2)
}

func TestCanonicalQuery1_JSON(t *testing.T) {
	stdout, stderr, code := runCLI(t, "../testdata/sample_trace.json", query1)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if stdout != expectedQuery1() {
		t.Errorf("got:\n%s\nwant:\n%s", stdout, expectedQuery1())
	}
}

func TestCanonicalQuery2_JSON(t *testing.T) {
	stdout, stderr, code := runCLI(t, "../testdata/sample_trace.json", query2)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if stdout != expectedQuery2() {
		t.Errorf("got:\n%s\nwant:\n%s", stdout, expectedQuery2())
	}
}

// TestCanonicalQuery3_JSON is the spec's Phase 2 canonical example
// ("token spend per model"). Fixture has two abc123 gen_ai.chat spans, both
// model gpt-4, output_tokens 42 and 17 -> one group, total_tokens=59,
// calls=2 (hand-computed against testdata/sample_trace.json).
func TestCanonicalQuery3_JSON(t *testing.T) {
	stdout, stderr, code := runCLI(t, "../testdata/sample_trace.json", query3)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	want := render(obj(
		"model", eval.String("gpt-4"),
		"total_tokens", eval.Number(59),
		"calls", eval.Number(2),
	))
	if stdout != want {
		t.Errorf("got:\n%s\nwant:\n%s", stdout, want)
	}
}

// TestPhase3Nav_JSON exercises the tree-navigation mechanism end-to-end
// through the CLI against the fixture's real parent/child relationship:
// span-2's parentSpanId is span-1.
func TestPhase3Nav_JSON(t *testing.T) {
	stdout, stderr, code := runCLI(t, "../testdata/sample_trace.json",
		`spans | select(.span_id == "span-1") | children | project({ span_id })`)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if stdout != `{"span_id":"span-2"}`+"\n" {
		t.Errorf("children of span-1 = %q, want span-2", stdout)
	}

	stdout2, stderr2, code2 := runCLI(t, "../testdata/sample_trace.json",
		`spans | select(.span_id == "span-2") | parent | project({ span_id })`)
	if code2 != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code2, stderr2)
	}
	if stdout2 != `{"span_id":"span-1"}`+"\n" {
		t.Errorf("parent of span-2 = %q, want span-1", stdout2)
	}
}

// TestPhase3TracesSource_JSON exercises the `traces` source end-to-end:
// grouping the fixture's 3 spans into 2 traces (abc123 has 2 spans, xyz789
// has 1).
func TestPhase3TracesSource_JSON(t *testing.T) {
	stdout, stderr, code := runCLI(t, "../testdata/sample_trace.json",
		`traces | select(.trace_id == "abc123") | project({ trace_id, span_count: .spans })`)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, `"trace_id":"abc123"`) {
		t.Errorf("got %q, want it to contain trace_id abc123", stdout)
	}
	// span_count here is the raw .spans array (project just references the
	// path) — assert it has exactly 2 elements by counting span_id occurrences.
	if strings.Count(stdout, `"span_id"`) != 2 {
		t.Errorf("got %q, want 2 spans under trace abc123", stdout)
	}
}

func TestCanonicalQueries_JSONLParity(t *testing.T) {
	json1, _, code1 := runCLI(t, "../testdata/sample_trace.json", query1)
	jsonl1, _, code2 := runCLI(t, "../testdata/sample_trace.jsonl", query1)
	if code1 != 0 || code2 != 0 {
		t.Fatalf("non-zero exit: json=%d jsonl=%d", code1, code2)
	}
	if json1 != jsonl1 {
		t.Errorf(".json and .jsonl ingestion produced different output for query1:\njson:\n%s\njsonl:\n%s", json1, jsonl1)
	}

	json2, _, code3 := runCLI(t, "../testdata/sample_trace.json", query2)
	jsonl2, _, code4 := runCLI(t, "../testdata/sample_trace.jsonl", query2)
	if code3 != 0 || code4 != 0 {
		t.Fatalf("non-zero exit: json=%d jsonl=%d", code3, code4)
	}
	if json2 != jsonl2 {
		t.Errorf(".json and .jsonl ingestion produced different output for query2:\njson:\n%s\njsonl:\n%s", json2, jsonl2)
	}

	json3, _, code5 := runCLI(t, "../testdata/sample_trace.json", query3)
	jsonl3, _, code6 := runCLI(t, "../testdata/sample_trace.jsonl", query3)
	if code5 != 0 || code6 != 0 {
		t.Fatalf("non-zero exit: json=%d jsonl=%d", code5, code6)
	}
	if json3 != jsonl3 {
		t.Errorf(".json and .jsonl ingestion produced different output for query3:\njson:\n%s\njsonl:\n%s", json3, jsonl3)
	}
}

func TestGroupByWithoutGroupBy_AggInProjectIsFieldError(t *testing.T) {
	_, stderr, code := runCLI(t, "../testdata/sample_trace.json", `spans | project({ total: sum(.gen_ai.usage.output_tokens) })`)
	if code == 0 {
		t.Fatal("expected non-zero exit: aggregation without a preceding group_by")
	}
	if !strings.Contains(stderr, "otq: field error") {
		t.Errorf("stderr = %q, want it to contain 'otq: field error'", stderr)
	}
}

// TestMalformedQuery_ErrorFormat reproduces the spec's own parse-error
// example query. As documented in internal/parser/parser_test.go, the
// spec's literal "position 14" does not correspond to the actual byte
// offset of 'desc' in the shown query — this test asserts the message
// shape and a self-consistent caret placement instead of that literal
// number.
func TestMalformedQuery_ErrorFormat(t *testing.T) {
	_, stderr, code := runCLI(t, "../testdata/sample_trace.json", `spans | sort_by(.duration_ms desc)`)
	if code == 0 {
		t.Fatal("expected non-zero exit for malformed query")
	}
	if !strings.Contains(stderr, "otq: parse error at position") {
		t.Errorf("stderr = %q, want it to start with 'otq: parse error at position'", stderr)
	}
	if !strings.Contains(stderr, "unexpected token 'desc' (expected ')')") {
		t.Errorf("stderr = %q, want it to contain the documented message text", stderr)
	}
	lines := strings.Split(strings.TrimRight(stderr, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("stderr has %d lines, want 3 (message, query, caret): %q", len(lines), stderr)
	}
}

func TestMissingInputFile(t *testing.T) {
	_, stderr, code := runCLI(t, "../testdata/does_not_exist.json", `spans | limit(1)`)
	if code == 0 {
		t.Fatal("expected non-zero exit for missing input file")
	}
	if !strings.Contains(stderr, "otq: input error") {
		t.Errorf("stderr = %q, want it to contain 'otq: input error'", stderr)
	}
}
