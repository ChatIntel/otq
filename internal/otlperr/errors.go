// Package otlperr defines otq's three error classes (parse, field, input)
// and their exact user-facing message formats. Stack traces never reach the
// user; internal panics are recovered elsewhere and reported as a fourth,
// unstructured "internal error" class.
package otlperr

import (
	"fmt"
	"strings"
)

// ParseError is a syntax error in the query string. It always carries the
// original, unmodified query so it can render a two-line caret diagram
// pointing at the offending position.
type ParseError struct {
	Pos   int // 1-indexed byte offset into Query
	Msg   string
	Query string
}

func (e *ParseError) Error() string {
	caretOffset := 2 + e.Pos - 1
	if caretOffset < 0 {
		caretOffset = 0
	}
	return fmt.Sprintf("otq: parse error at position %d: %s\n  %s\n%s^",
		e.Pos, e.Msg, e.Query, strings.Repeat(" ", caretOffset))
}

// FieldError is an evaluation-time type mismatch (e.g. any/all applied to a
// non-array path) or a reference to a not-yet-implemented grammar construct
// (Phase 2/3 stubs). A merely absent field is NOT a FieldError — that
// evaluates to null per the query language's null-propagation rule.
type FieldError struct {
	Field string
	Msg   string
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("otq: field error: '%s' %s", e.Field, e.Msg)
}

// InputError is a problem with the source trace data itself (malformed
// JSON/JSONL, unparseable timestamps, missing input files).
type InputError struct {
	Source string // filename, or "filename:line" for JSONL
	Msg    string
}

func (e *InputError) Error() string {
	return fmt.Sprintf("otq: input error: %s: %s", e.Source, e.Msg)
}
