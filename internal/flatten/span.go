// Package flatten projects an OTLP span (internal/otlp) into otq's
// query-facing, flattened row shape (internal/eval.Value) with GenAI
// semantic-convention fields lifted as named fields.
package flatten

import (
	"strconv"
	"time"

	"otq/internal/eval"
	"otq/internal/otlp"
	"otq/internal/otlperr"
	"otq/internal/semconv"
)

type Span struct {
	TraceID       string
	SpanID        string
	ParentSpanID  *string
	Name          string
	Kind          string
	StartTime     string
	EndTime       string
	DurationMs    float64
	StatusCode    string
	StatusMessage *string
	GenAI         semconv.GenAI
	Attributes    map[string]eval.Value
}

// Flatten converts one otlp.RawSpan into otq's flattened Span. Returns an
// *otlperr.InputError for malformed source data (unparseable timestamps or
// an out-of-range status code) — these are ingestion-time problems with the
// trace file, not query errors.
func Flatten(raw otlp.RawSpan) (Span, error) {
	sp := raw.Span

	startNanos, err := strconv.ParseUint(sp.StartTimeUnixNano, 10, 64)
	if err != nil {
		return Span{}, &otlperr.InputError{Source: "span " + sp.SpanID, Msg: "malformed startTimeUnixNano: " + err.Error()}
	}
	endNanos, err := strconv.ParseUint(sp.EndTimeUnixNano, 10, 64)
	if err != nil {
		return Span{}, &otlperr.InputError{Source: "span " + sp.SpanID, Msg: "malformed endTimeUnixNano: " + err.Error()}
	}

	statusCode, statusMsg, err := mapStatus(sp.Status)
	if err != nil {
		return Span{}, &otlperr.InputError{Source: "span " + sp.SpanID, Msg: err.Error()}
	}

	var parentSpanID *string
	if sp.ParentSpanID != "" {
		p := sp.ParentSpanID
		parentSpanID = &p
	}

	attrs := otlp.AttrMap(sp.Attributes)
	genAI, remaining := semconv.LiftGenAI(attrs)

	return Span{
		TraceID:       sp.TraceID,
		SpanID:        sp.SpanID,
		ParentSpanID:  parentSpanID,
		Name:          sp.Name,
		Kind:          sp.KindString(),
		StartTime:     nanosToRFC3339(startNanos),
		EndTime:       nanosToRFC3339(endNanos),
		DurationMs:    float64(endNanos-startNanos) / 1e6,
		StatusCode:    statusCode,
		StatusMessage: statusMsg,
		GenAI:         genAI,
		Attributes:    remaining,
	}, nil
}

func nanosToRFC3339(nanos uint64) string {
	return time.Unix(0, int64(nanos)).UTC().Format(time.RFC3339Nano)
}

// mapStatus is an explicit 3-way switch (never a bool collapse) per the
// spec's requirement that UNSET must never be treated as OK.
//
// StatusMessage: OTLP has no signal distinguishing "no Status object" from
// "Status object present with an empty message" — both collapse to a nil
// (JSON null) StatusMessage here, since that distinction isn't meaningful
// for querying. This is a deliberate judgment call the spec left open.
func mapStatus(status *otlp.Status) (code string, message *string, err error) {
	if status == nil {
		return "UNSET", nil, nil
	}
	switch status.Code {
	case 0:
		code = "UNSET"
	case 1:
		code = "OK"
	case 2:
		code = "ERROR"
	default:
		return "", nil, &statusCodeError{status.Code}
	}
	if status.Message != "" {
		m := status.Message
		message = &m
	}
	return code, message, nil
}

type statusCodeError struct{ code int }

func (e *statusCodeError) Error() string {
	return "unrecognized OTLP status code: " + strconv.Itoa(e.code)
}

// ToValue converts a flattened Span into otq's dynamic eval.Value tree,
// with object keys in the data model's documented order.
func (s Span) ToValue() eval.Value {
	v := eval.NewObject()
	v.Set("trace_id", eval.String(s.TraceID))
	v.Set("span_id", eval.String(s.SpanID))
	if s.ParentSpanID != nil {
		v.Set("parent_span_id", eval.String(*s.ParentSpanID))
	} else {
		v.Set("parent_span_id", eval.Null())
	}
	v.Set("name", eval.String(s.Name))
	v.Set("kind", eval.String(s.Kind))
	v.Set("start_time", eval.String(s.StartTime))
	v.Set("end_time", eval.String(s.EndTime))
	v.Set("duration_ms", eval.Number(s.DurationMs))

	status := eval.NewObject()
	status.Set("code", eval.String(s.StatusCode))
	if s.StatusMessage != nil {
		status.Set("message", eval.String(*s.StatusMessage))
	} else {
		status.Set("message", eval.Null())
	}
	v.Set("status", status)

	v.Set("gen_ai", genAIToValue(s.GenAI))

	attrs := eval.NewObject()
	for k, val := range s.Attributes {
		attrs.Set(k, val)
	}
	v.Set("attributes", attrs)

	return v
}

func genAIToValue(g semconv.GenAI) eval.Value {
	v := eval.NewObject()
	v.Set("system", optStringToValue(g.System))
	v.Set("request", func() eval.Value {
		o := eval.NewObject()
		o.Set("model", optStringToValue(g.RequestModel))
		return o
	}())
	v.Set("response", func() eval.Value {
		o := eval.NewObject()
		o.Set("model", optStringToValue(g.ResponseModel))
		return o
	}())
	v.Set("usage", func() eval.Value {
		o := eval.NewObject()
		o.Set("input_tokens", optFloatToValue(g.InputTokens))
		o.Set("output_tokens", optFloatToValue(g.OutputTokens))
		return o
	}())
	v.Set("tool", func() eval.Value {
		o := eval.NewObject()
		o.Set("name", optStringToValue(g.ToolName))
		return o
	}())
	v.Set("agent", func() eval.Value {
		o := eval.NewObject()
		o.Set("name", optStringToValue(g.AgentName))
		return o
	}())
	v.Set("prompt", messagesToValue(g.Prompt))
	v.Set("completion", messagesToValue(g.Completion))
	return v
}

func optStringToValue(s *string) eval.Value {
	if s == nil {
		return eval.Null()
	}
	return eval.String(*s)
}

func optFloatToValue(f *float64) eval.Value {
	if f == nil {
		return eval.Null()
	}
	return eval.Number(*f)
}

func messagesToValue(msgs []semconv.Message) eval.Value {
	if msgs == nil {
		return eval.Null()
	}
	vs := make([]eval.Value, len(msgs))
	for i, m := range msgs {
		o := eval.NewObject()
		o.Set("role", eval.String(m.Role))
		o.Set("content", eval.String(m.Content))
		vs[i] = o
	}
	return eval.Array(vs)
}
