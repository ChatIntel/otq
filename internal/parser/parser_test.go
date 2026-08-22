package parser

import (
	"reflect"
	"strings"
	"testing"
)

func mustParse(t *testing.T, q string) *Query {
	t.Helper()
	ast, err := Parse(q)
	if err != nil {
		t.Fatalf("Parse(%q) returned error: %v", q, err)
	}
	return ast
}

func TestPrecedence_AndOrNot(t *testing.T) {
	// spec: `a and b or not c` parses as `(a and b) or (not c)`
	q := mustParse(t, `spans | select(.a == "x" and .b == "y" or not .c == "z")`)
	sel, ok := q.Stages[0].(SelectStage)
	if !ok {
		t.Fatalf("stage is %T, want SelectStage", q.Stages[0])
	}
	top, ok := sel.Expr.(BinaryExpr)
	if !ok || top.Op != OpOr {
		t.Fatalf("top expr = %#v, want BinaryExpr{Op: OpOr}", sel.Expr)
	}
	left, ok := top.Left.(BinaryExpr)
	if !ok || left.Op != OpAnd {
		t.Fatalf("left = %#v, want BinaryExpr{Op: OpAnd}", top.Left)
	}
	if _, ok := top.Right.(NotExpr); !ok {
		t.Fatalf("right = %#v, want NotExpr", top.Right)
	}
}

func TestCanonicalQuery1(t *testing.T) {
	q := mustParse(t, `spans | select(.name == "gen_ai.chat") | sort_by(.duration_ms, desc) | limit(10)`)
	if q.Source != SourceSpans {
		t.Fatalf("source = %v, want SourceSpans", q.Source)
	}
	if len(q.Stages) != 3 {
		t.Fatalf("got %d stages, want 3", len(q.Stages))
	}
	sel, ok := q.Stages[0].(SelectStage)
	if !ok {
		t.Fatalf("stage 0 = %T, want SelectStage", q.Stages[0])
	}
	cmp, ok := sel.Expr.(ComparisonExpr)
	if !ok || cmp.Op != CmpEq || !reflect.DeepEqual(cmp.Path, Path{Segments: []string{"name"}}) || cmp.Literal.Kind != LitString || cmp.Literal.Str != "gen_ai.chat" {
		t.Fatalf("select expr = %#v", sel.Expr)
	}
	sortBy, ok := q.Stages[1].(SortByStage)
	if !ok || sortBy.Order != Desc || !reflect.DeepEqual(sortBy.Path, Path{Segments: []string{"duration_ms"}}) {
		t.Fatalf("stage 1 = %#v", q.Stages[1])
	}
	limit, ok := q.Stages[2].(LimitStage)
	if !ok || limit.N != 10 {
		t.Fatalf("stage 2 = %#v", q.Stages[2])
	}
}

func TestCanonicalQuery2(t *testing.T) {
	q := mustParse(t, `spans | select(.trace_id == "abc123") | select(.name | startswith("gen_ai"))
      | sort_by(.start_time)
      | project({ role: .gen_ai.system, prompt: .gen_ai.prompt, completion: .gen_ai.completion })`)
	if len(q.Stages) != 4 {
		t.Fatalf("got %d stages, want 4", len(q.Stages))
	}
	sel2, ok := q.Stages[1].(SelectStage)
	if !ok {
		t.Fatalf("stage 1 = %T", q.Stages[1])
	}
	sc, ok := sel2.Expr.(StrcallExpr)
	if !ok || sc.Func != StrStartsWith || sc.Arg != "gen_ai" {
		t.Fatalf("strcall expr = %#v", sel2.Expr)
	}
	sortBy, ok := q.Stages[2].(SortByStage)
	if !ok || sortBy.Order != Asc {
		t.Fatalf("stage 2 = %#v, want default Asc order", q.Stages[2])
	}
	proj, ok := q.Stages[3].(ProjectStage)
	if !ok || len(proj.Fields) != 3 {
		t.Fatalf("stage 3 = %#v", q.Stages[3])
	}
	wantNames := []string{"role", "prompt", "completion"}
	wantPaths := [][]string{{"gen_ai", "system"}, {"gen_ai", "prompt"}, {"gen_ai", "completion"}}
	for i, f := range proj.Fields {
		if f.Name != wantNames[i] {
			t.Errorf("field %d name = %q, want %q", i, f.Name, wantNames[i])
		}
		p, ok := f.Value.(Path)
		if !ok || !reflect.DeepEqual(p.Segments, wantPaths[i]) {
			t.Errorf("field %d value = %#v, want Path%v", i, f.Value, wantPaths[i])
		}
	}
}

func TestProjectShorthand(t *testing.T) {
	q := mustParse(t, `spans | project({ name })`)
	proj := q.Stages[0].(ProjectStage)
	if len(proj.Fields) != 1 || proj.Fields[0].Name != "name" {
		t.Fatalf("got %#v", proj.Fields)
	}
	p, ok := proj.Fields[0].Value.(Path)
	if !ok || !reflect.DeepEqual(p.Segments, []string{"name"}) {
		t.Fatalf("shorthand value = %#v, want Path{[name]}", proj.Fields[0].Value)
	}
}

func TestProjectShorthand_DisallowedAfterGroupBy(t *testing.T) {
	_, err := Parse(`spans | group_by(.gen_ai.request.model) | project({ model })`)
	if err == nil {
		t.Fatal("expected parse error: bare shorthand field is disallowed in project() after group_by")
	}
	if !strings.Contains(err.Error(), "requires explicit field: value pairs") {
		t.Errorf("error = %v, want it to mention explicit field: value pairs", err)
	}
}

func TestProjectShorthand_AllowedBeforeGroupBy(t *testing.T) {
	// shorthand is only restricted *after* group_by has been seen in the
	// pipeline — a project() stage earlier in the same query is unaffected.
	_, err := Parse(`spans | project({ name }) | group_by(.gen_ai.request.model) | project({ model: .group_key })`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPhase2Phase3Stage_ParsesButNotError(t *testing.T) {
	queries := []string{
		`spans | group_by(.gen_ai.request.model)`,
		`traces | children`,
		`traces | descendants`,
		`traces | parent`,
		`traces | ancestors`,
		`traces | root`,
	}
	for _, q := range queries {
		if _, err := Parse(q); err != nil {
			t.Errorf("Parse(%q) returned error, want parse-only stub success: %v", q, err)
		}
	}
}

func TestQuantifierParses(t *testing.T) {
	_, err := Parse(`traces | select(any(.spans; .gen_ai.tool.name != null and .status.code == "ERROR"))`)
	if err != nil {
		t.Fatalf("quantifier query failed to parse: %v", err)
	}
}

func TestAggCallInProjectParses(t *testing.T) {
	q := mustParse(t, `spans | group_by(.gen_ai.request.model)
      | project({ model: .group_key, total_tokens: sum(.gen_ai.usage.output_tokens), calls: count })`)
	proj := q.Stages[1].(ProjectStage)
	if len(proj.Fields) != 3 {
		t.Fatalf("got %d fields, want 3", len(proj.Fields))
	}
	agg, ok := proj.Fields[1].Value.(AggCallExpr)
	if !ok || agg.Kind != AggSum {
		t.Fatalf("field 1 value = %#v, want AggCallExpr{Kind: AggSum}", proj.Fields[1].Value)
	}
	count, ok := proj.Fields[2].Value.(AggCallExpr)
	if !ok || count.Kind != AggCount {
		t.Fatalf("field 2 value = %#v, want AggCallExpr{Kind: AggCount}", proj.Fields[2].Value)
	}
}

// Regression test reproducing the spec's own parse-error example. The spec's
// PRD text shows "position 14" pointing at 'desc', but that number does not
// correspond to the actual byte offset of 'desc' in the shown query
// (`sort_by(.duration_ms desc)` — 'desc' is at byte offset 22, since
// "sort_by(.duration_ms " is 21 characters). The spec's example is treated
// as illustrative of the *message shape*, not a literal position to
// reproduce; this test asserts our computed, self-consistent position and
// the message text/caret-diagram format instead.
func TestErrorFormat_SortByBareDesc(t *testing.T) {
	q := `sort_by(.duration_ms desc)`
	fullQuery := "spans | " + q
	_, err := Parse(fullQuery)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unexpected token 'desc' (expected ')')") {
		t.Errorf("error message = %q, want it to contain \"unexpected token 'desc' (expected ')')\"", msg)
	}
	if !strings.HasPrefix(msg, "otq: parse error at position ") {
		t.Errorf("error message = %q, want prefix 'otq: parse error at position '", msg)
	}
	lines := strings.Split(msg, "\n")
	if len(lines) != 3 {
		t.Fatalf("error message has %d lines, want 3 (message, query, caret): %q", len(lines), msg)
	}
	if lines[1] != "  "+fullQuery {
		t.Errorf("query line = %q, want %q", lines[1], "  "+fullQuery)
	}
	caretIdx := strings.Index(lines[2], "^")
	if caretIdx == -1 {
		t.Fatalf("no caret found in %q", lines[2])
	}
	if lines[1][caretIdx] != 'd' {
		t.Errorf("caret points at %q, want it to point at 'd' (start of 'desc')", string(lines[1][caretIdx]))
	}
}

func TestSourceRequired(t *testing.T) {
	_, err := Parse(`select(.name == "x")`)
	if err == nil {
		t.Fatal("expected parse error for missing source")
	}
}

func TestUnknownStage(t *testing.T) {
	_, err := Parse(`spans | bogus(.x)`)
	if err == nil {
		t.Fatal("expected parse error for unknown stage")
	}
}

func TestTrailingGarbage(t *testing.T) {
	_, err := Parse(`spans | limit(1) garbage`)
	if err == nil {
		t.Fatal("expected parse error for trailing tokens")
	}
}

func TestParenthesizedExpr(t *testing.T) {
	q := mustParse(t, `spans | select((.a == "x" or .b == "y") and .c == "z")`)
	sel := q.Stages[0].(SelectStage)
	top, ok := sel.Expr.(BinaryExpr)
	if !ok || top.Op != OpAnd {
		t.Fatalf("top = %#v, want BinaryExpr{Op: OpAnd}", sel.Expr)
	}
	if _, ok := top.Left.(BinaryExpr); !ok {
		t.Fatalf("left = %#v, want BinaryExpr (parenthesized or)", top.Left)
	}
}
