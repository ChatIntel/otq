// Package otlp defines hand-rolled Go structs for unmarshaling OTLP JSON
// (resourceSpans -> scopeSpans -> spans), rather than depending on
// go.opentelemetry.io/proto/otlp's protobuf-generated oneof types. This
// keeps otq's dependency tree at stdlib-only and gives a single, direct
// conversion point (AnyValue.ToValue) from OTLP's attribute oneof straight
// into otq's own eval.Value, skipping an intermediate protobuf-struct hop.
package otlp

import (
	"encoding/json"
	"strconv"

	"otq/internal/eval"
)

type ExportTraceServiceRequest struct {
	ResourceSpans []ResourceSpans `json:"resourceSpans"`
}

type ResourceSpans struct {
	Resource   Resource     `json:"resource"`
	ScopeSpans []ScopeSpans `json:"scopeSpans"`
}

type Resource struct {
	Attributes []KeyValue `json:"attributes"`
}

type ScopeSpans struct {
	Scope Scope  `json:"scope"`
	Spans []Span `json:"spans"`
}

type Scope struct {
	Name string `json:"name"`
}

type Span struct {
	TraceID           string          `json:"traceId"`
	SpanID            string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanId"`
	Name              string          `json:"name"`
	Kind              json.RawMessage `json:"kind"`
	StartTimeUnixNano string          `json:"startTimeUnixNano"`
	EndTimeUnixNano   string          `json:"endTimeUnixNano"`
	Attributes        []KeyValue      `json:"attributes"`
	Status            *Status         `json:"status"`
}

// spanKindNames maps the numeric OTLP SpanKind enum (used by a minority of
// older exporters; most emit the string form directly) to its string name.
var spanKindNames = map[int]string{
	0: "SPAN_KIND_UNSPECIFIED",
	1: "SPAN_KIND_INTERNAL",
	2: "SPAN_KIND_SERVER",
	3: "SPAN_KIND_CLIENT",
	4: "SPAN_KIND_PRODUCER",
	5: "SPAN_KIND_CONSUMER",
}

// KindString normalizes Span.Kind, which OTLP JSON may encode as either the
// enum's string name or (defensively handled) a bare integer.
func (s Span) KindString() string {
	if len(s.Kind) == 0 {
		return ""
	}
	var str string
	if err := json.Unmarshal(s.Kind, &str); err == nil {
		return str
	}
	var n int
	if err := json.Unmarshal(s.Kind, &n); err == nil {
		if name, ok := spanKindNames[n]; ok {
			return name
		}
	}
	return string(s.Kind)
}

type Status struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type KeyValue struct {
	Key   string   `json:"key"`
	Value AnyValue `json:"value"`
}

// AnyValue mirrors OTLP's AnyValue oneof as it appears in proto3 JSON:
// exactly one of these fields is populated depending on the value's type.
// IntValue is string-encoded per proto3 JSON's int64 convention (same as
// the span timestamps).
type AnyValue struct {
	StringValue *string       `json:"stringValue,omitempty"`
	BoolValue   *bool         `json:"boolValue,omitempty"`
	IntValue    *string       `json:"intValue,omitempty"`
	DoubleValue *float64      `json:"doubleValue,omitempty"`
	ArrayValue  *ArrayValue   `json:"arrayValue,omitempty"`
	KvlistValue *KeyValueList `json:"kvlistValue,omitempty"`
}

type ArrayValue struct {
	Values []AnyValue `json:"values"`
}

type KeyValueList struct {
	Values []KeyValue `json:"values"`
}

// ToValue converts an OTLP AnyValue oneof into otq's dynamic eval.Value.
// An AnyValue with no field populated (should not occur in valid OTLP, but
// handled defensively) converts to null.
func (av AnyValue) ToValue() eval.Value {
	switch {
	case av.StringValue != nil:
		return eval.String(*av.StringValue)
	case av.BoolValue != nil:
		return eval.Bool(*av.BoolValue)
	case av.IntValue != nil:
		n, err := strconv.ParseInt(*av.IntValue, 10, 64)
		if err != nil {
			return eval.String(*av.IntValue)
		}
		return eval.Number(float64(n))
	case av.DoubleValue != nil:
		return eval.Number(*av.DoubleValue)
	case av.ArrayValue != nil:
		vs := make([]eval.Value, len(av.ArrayValue.Values))
		for i, e := range av.ArrayValue.Values {
			vs[i] = e.ToValue()
		}
		return eval.Array(vs)
	case av.KvlistValue != nil:
		obj := eval.NewObject()
		for _, kv := range av.KvlistValue.Values {
			obj.Set(kv.Key, kv.Value.ToValue())
		}
		return obj
	}
	return eval.Null()
}

// AttrMap flattens a KeyValue slice (span or resource attributes) into a
// plain map keyed by attribute name, as consumed by internal/semconv and
// internal/flatten. Later duplicate keys win, matching how OTLP producers
// are expected to avoid emitting duplicate attribute keys in the first
// place — if they do, last-one-wins is a reasonable, unsurprising default.
func AttrMap(kvs []KeyValue) map[string]eval.Value {
	m := make(map[string]eval.Value, len(kvs))
	for _, kv := range kvs {
		m[kv.Key] = kv.Value.ToValue()
	}
	return m
}
