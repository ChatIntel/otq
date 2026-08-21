package flatten

import (
	"testing"

	"otq/internal/otlp"
)

func mkRaw(statusCode int, hasStatus bool, statusMsg string) otlp.RawSpan {
	var status *otlp.Status
	if hasStatus {
		status = &otlp.Status{Code: statusCode, Message: statusMsg}
	}
	return otlp.RawSpan{
		Span: otlp.Span{
			TraceID:           "t1",
			SpanID:            "s1",
			ParentSpanID:      "",
			Name:              "gen_ai.chat",
			Kind:              []byte(`"SPAN_KIND_CLIENT"`),
			StartTimeUnixNano: "1000000000",
			EndTimeUnixNano:   "2500000000",
			Status:            status,
		},
	}
}

func TestFlatten_StatusMapping(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{0, "UNSET"},
		{1, "OK"},
		{2, "ERROR"},
	}
	for _, c := range cases {
		sp, err := Flatten(mkRaw(c.code, true, ""))
		if err != nil {
			t.Fatalf("code %d: Flatten error: %v", c.code, err)
		}
		if sp.StatusCode != c.want {
			t.Errorf("code %d: StatusCode = %q, want %q", c.code, sp.StatusCode, c.want)
		}
	}
}

func TestFlatten_UnsetNotCollapsedToOK(t *testing.T) {
	sp, err := Flatten(mkRaw(0, true, ""))
	if err != nil {
		t.Fatal(err)
	}
	if sp.StatusCode == "OK" {
		t.Fatal("UNSET (code 0) must not be collapsed into OK")
	}
}

func TestFlatten_NoStatusObjectIsUnset(t *testing.T) {
	sp, err := Flatten(mkRaw(0, false, ""))
	if err != nil {
		t.Fatal(err)
	}
	if sp.StatusCode != "UNSET" {
		t.Errorf("StatusCode = %q, want UNSET when no Status object present", sp.StatusCode)
	}
}

func TestFlatten_UnrecognizedStatusCodeErrors(t *testing.T) {
	_, err := Flatten(mkRaw(99, true, ""))
	if err == nil {
		t.Fatal("expected error for unrecognized status code")
	}
}

func TestFlatten_DurationMs(t *testing.T) {
	sp, err := Flatten(mkRaw(1, true, ""))
	if err != nil {
		t.Fatal(err)
	}
	// 2500000000 - 1000000000 = 1500000000 ns = 1500.0 ms
	if sp.DurationMs != 1500.0 {
		t.Errorf("DurationMs = %v, want 1500.0", sp.DurationMs)
	}
}

func TestFlatten_TimeFormat(t *testing.T) {
	sp, err := Flatten(mkRaw(1, true, ""))
	if err != nil {
		t.Fatal(err)
	}
	if sp.StartTime != "1970-01-01T00:00:01Z" {
		t.Errorf("StartTime = %q", sp.StartTime)
	}
}

func TestFlatten_ParentSpanIDEmptyIsNil(t *testing.T) {
	sp, err := Flatten(mkRaw(1, true, ""))
	if err != nil {
		t.Fatal(err)
	}
	if sp.ParentSpanID != nil {
		t.Errorf("ParentSpanID = %v, want nil for empty string parentSpanId", *sp.ParentSpanID)
	}
}

func TestFlatten_MalformedTimestamp(t *testing.T) {
	raw := mkRaw(1, true, "")
	raw.Span.StartTimeUnixNano = "not-a-number"
	_, err := Flatten(raw)
	if err == nil {
		t.Fatal("expected error for malformed startTimeUnixNano")
	}
}

func TestFlatten_AttributesCatchAll(t *testing.T) {
	raw := mkRaw(1, true, "")
	raw.Span.Attributes = []otlp.KeyValue{
		{Key: "gen_ai.system", Value: otlp.AnyValue{StringValue: strp("openai")}},
		{Key: "custom.tag", Value: otlp.AnyValue{StringValue: strp("keep-me")}},
	}
	sp, err := Flatten(raw)
	if err != nil {
		t.Fatal(err)
	}
	if sp.GenAI.System == nil || *sp.GenAI.System != "openai" {
		t.Errorf("GenAI.System = %v", sp.GenAI.System)
	}
	if _, ok := sp.Attributes["gen_ai.system"]; ok {
		t.Errorf("gen_ai.system should not duplicate into catch-all attributes")
	}
	if v, ok := sp.Attributes["custom.tag"]; !ok || v.S != "keep-me" {
		t.Errorf("custom.tag missing from catch-all attributes: %#v", sp.Attributes)
	}
}

func strp(s string) *string { return &s }
