package eval

import (
	"encoding/json"
	"testing"

	"otq/internal/parser"
)

func mustParseExpr(t *testing.T, exprSrc string) parser.Expr {
	t.Helper()
	q, err := parser.Parse(`spans | select(` + exprSrc + `)`)
	if err != nil {
		t.Fatalf("parse %q: %v", exprSrc, err)
	}
	return q.Stages[0].(parser.SelectStage).Expr
}

func spanValue(fields map[string]Value) Value {
	v := NewObject()
	for k, val := range fields {
		v.Set(k, val)
	}
	return v
}

func TestValue_Get_MissingIsNull(t *testing.T) {
	v := spanValue(map[string]Value{"name": String("x")})
	got := v.Get([]string{"missing"})
	if got.Kind != KNull {
		t.Errorf("Get(missing) = %+v, want KNull", got)
	}
	got2 := v.Get([]string{"gen_ai", "request", "model"}) // gen_ai itself absent
	if got2.Kind != KNull {
		t.Errorf("Get(nested missing) = %+v, want KNull", got2)
	}
}

func TestValue_EqualAcrossKindsIsFalse(t *testing.T) {
	if Equal(Number(1), String("1")) {
		t.Error("Equal(number, string) should be false, not an error/panic")
	}
	if !Equal(Null(), Null()) {
		t.Error("Equal(null, null) should be true")
	}
}

func TestValue_ToJSON_PreservesKeyOrder(t *testing.T) {
	v := NewObject()
	v.Set("b", Number(2))
	v.Set("a", Number(1))
	out, err := json.Marshal(v.ToJSON())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"b":2,"a":1}`
	if string(out) != want {
		t.Errorf("got %s, want %s (insertion order, not alphabetical)", out, want)
	}
}

func TestValue_ToJSON_NullArray(t *testing.T) {
	v := Array(nil)
	out, _ := json.Marshal(v.ToJSON())
	if string(out) != "null" {
		t.Errorf("got %s, want null for nil array", out)
	}
}

func TestEvalExpr_NullPropagation_PresenceCheck(t *testing.T) {
	// select(.gen_ai.request.model != null) must be true when gen_ai is
	// entirely absent — this is the canonical presence-check pattern from
	// the spec and must not error.
	expr := mustParseExpr(t, `.gen_ai.request.model != null`)
	span := spanValue(map[string]Value{"name": String("retrieval")})
	ok, err := EvalExpr(expr, span)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected false: field is absent, so != null should not match")
	}
}

func TestEvalExpr_Comparisons(t *testing.T) {
	span := spanValue(map[string]Value{"duration_ms": Number(500), "name": String("gen_ai.chat")})
	cases := []struct {
		expr string
		want bool
	}{
		{`.duration_ms == 500`, true},
		{`.duration_ms != 500`, false},
		{`.duration_ms > 100`, true},
		{`.duration_ms < 100`, false},
		{`.duration_ms >= 500`, true},
		{`.duration_ms <= 500`, true},
		{`.name == "gen_ai.chat"`, true},
	}
	for _, c := range cases {
		e := mustParseExpr(t, c.expr)
		got, err := EvalExpr(e, span)
		if err != nil {
			t.Fatalf("%s: %v", c.expr, err)
		}
		if got != c.want {
			t.Errorf("%s = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestEvalExpr_AndOrNot(t *testing.T) {
	span := spanValue(map[string]Value{"a": Bool(true), "b": Bool(false)})
	_ = span
	// use string comparisons instead since our grammar only compares paths to literals
	span2 := spanValue(map[string]Value{"a": String("x"), "b": String("y")})
	e := mustParseExpr(t, `.a == "x" and .b == "y"`)
	ok, err := EvalExpr(e, span2)
	if err != nil || !ok {
		t.Fatalf("got %v, %v, want true, nil", ok, err)
	}
	e2 := mustParseExpr(t, `.a == "no" or .b == "y"`)
	ok2, _ := EvalExpr(e2, span2)
	if !ok2 {
		t.Error("or should be true when right side matches")
	}
	e3 := mustParseExpr(t, `not .a == "x"`)
	ok3, _ := EvalExpr(e3, span2)
	if ok3 {
		t.Error("not should negate true to false")
	}
}

func TestEvalExpr_Strcall(t *testing.T) {
	span := spanValue(map[string]Value{"name": String("gen_ai.chat")})
	cases := []struct {
		expr string
		want bool
	}{
		{`.name | startswith("gen_ai")`, true},
		{`.name | startswith("nope")`, false},
		{`.name | endswith("chat")`, true},
		{`.name | contains("ai.ch")`, true},
	}
	for _, c := range cases {
		e := mustParseExpr(t, c.expr)
		got, err := EvalExpr(e, span)
		if err != nil {
			t.Fatalf("%s: %v", c.expr, err)
		}
		if got != c.want {
			t.Errorf("%s = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestEvalExpr_QuantifierIsFieldError(t *testing.T) {
	q, err := parser.Parse(`traces | select(any(.spans; .name != null))`)
	if err != nil {
		t.Fatal(err)
	}
	expr := q.Stages[0].(parser.SelectStage).Expr
	_, err = EvalExpr(expr, NewObject())
	if err == nil {
		t.Fatal("expected field error for unsupported quantifier")
	}
}

func TestPipeline_Select(t *testing.T) {
	q, err := parser.Parse(`spans | select(.name == "gen_ai.chat")`)
	if err != nil {
		t.Fatal(err)
	}
	values := []Value{
		spanValue(map[string]Value{"name": String("gen_ai.chat")}),
		spanValue(map[string]Value{"name": String("retrieval")}),
	}
	out, err := Evaluate(q, values)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d results, want 1", len(out))
	}
}

func TestPipeline_SortByDescAndLimit(t *testing.T) {
	q, err := parser.Parse(`spans | sort_by(.duration_ms, desc) | limit(2)`)
	if err != nil {
		t.Fatal(err)
	}
	values := []Value{
		spanValue(map[string]Value{"duration_ms": Number(10)}),
		spanValue(map[string]Value{"duration_ms": Number(30)}),
		spanValue(map[string]Value{"duration_ms": Number(20)}),
	}
	out, err := Evaluate(q, values)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d results, want 2 (limited)", len(out))
	}
	if out[0].Get([]string{"duration_ms"}).N != 30 || out[1].Get([]string{"duration_ms"}).N != 20 {
		t.Errorf("not sorted desc: %v, %v", out[0].Get([]string{"duration_ms"}).N, out[1].Get([]string{"duration_ms"}).N)
	}
}

func TestPipeline_SortByStableTies(t *testing.T) {
	q, err := parser.Parse(`spans | sort_by(.duration_ms)`)
	if err != nil {
		t.Fatal(err)
	}
	values := []Value{
		spanValue(map[string]Value{"duration_ms": Number(10), "id": String("first")}),
		spanValue(map[string]Value{"duration_ms": Number(10), "id": String("second")}),
	}
	out, err := Evaluate(q, values)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Get([]string{"id"}).S != "first" || out[1].Get([]string{"id"}).S != "second" {
		t.Error("stable sort should preserve input order for ties")
	}
}

func TestPipeline_Project(t *testing.T) {
	q, err := parser.Parse(`spans | project({ role: .gen_ai.system, prompt: .gen_ai.prompt })`)
	if err != nil {
		t.Fatal(err)
	}
	span := NewObject()
	genai := NewObject()
	genai.Set("system", String("openai"))
	genai.Set("prompt", Null())
	span.Set("gen_ai", genai)
	out, err := Evaluate(q, []Value{span})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Get([]string{"role"}).S != "openai" {
		t.Errorf("role = %+v", out[0].Get([]string{"role"}))
	}
	if out[0].Get([]string{"prompt"}).Kind != KNull {
		t.Errorf("prompt = %+v, want null", out[0].Get([]string{"prompt"}))
	}
}

func spanNode(traceID, spanID, parentID string) Value {
	v := NewObject()
	v.Set("trace_id", String(traceID))
	v.Set("span_id", String(spanID))
	if parentID == "" {
		v.Set("parent_span_id", Null())
	} else {
		v.Set("parent_span_id", String(parentID))
	}
	return v
}

func TestPipeline_TracesSource_Grouping(t *testing.T) {
	q, err := parser.Parse(`traces | sort_by(.trace_id)`)
	if err != nil {
		t.Fatal(err)
	}
	values := []Value{
		spanNode("t2", "a", ""),
		spanNode("t1", "b", ""),
		spanNode("t2", "c", "a"),
	}
	out, err := Evaluate(q, values)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d traces, want 2", len(out))
	}
	if out[0].Get([]string{"trace_id"}).S != "t1" || out[1].Get([]string{"trace_id"}).S != "t2" {
		t.Fatalf("trace_ids = %q, %q", out[0].Get([]string{"trace_id"}).S, out[1].Get([]string{"trace_id"}).S)
	}
	t2Spans := out[1].Get([]string{"spans"})
	if t2Spans.Kind != KArray || len(t2Spans.A) != 2 {
		t.Fatalf("t2 spans = %+v, want 2 spans", t2Spans)
	}
}

func TestPipeline_CanonicalQuery_AnyQuantifier(t *testing.T) {
	// spec's Phase 3 example: traces containing an errored tool call.
	q, err := parser.Parse(`traces | select(any(.spans; .gen_ai.tool.name != null and .status.code == "ERROR"))`)
	if err != nil {
		t.Fatal(err)
	}
	errored := spanNode("t1", "a", "")
	genai := NewObject()
	tool := NewObject()
	tool.Set("name", String("search"))
	genai.Set("tool", tool)
	errored.Set("gen_ai", genai)
	status := NewObject()
	status.Set("code", String("ERROR"))
	errored.Set("status", status)

	clean := spanNode("t2", "b", "")
	clean.Set("gen_ai", NewObject())
	cleanStatus := NewObject()
	cleanStatus.Set("code", String("OK"))
	clean.Set("status", cleanStatus)

	out, err := Evaluate(q, []Value{errored, clean})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d traces, want 1 (only t1 has an errored tool call)", len(out))
	}
	if out[0].Get([]string{"trace_id"}).S != "t1" {
		t.Errorf("matched trace = %q, want t1", out[0].Get([]string{"trace_id"}).S)
	}
}

func TestEvalExpr_Quantifier_VacuousTruth(t *testing.T) {
	root := NewObject()
	root.Set("spans", Value{Kind: KArray, A: []Value{}}) // present but empty array

	anyExpr := mustParseExpr(t, `any(.spans; .x == "never")`)
	ok, err := EvalExpr(anyExpr, root)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("any() over an empty array must be false")
	}

	allExpr := mustParseExpr(t, `all(.spans; .x == "never")`)
	ok2, err := EvalExpr(allExpr, root)
	if err != nil {
		t.Fatal(err)
	}
	if !ok2 {
		t.Error("all() over an empty array must be true (vacuous truth)")
	}
}

func TestPipeline_Nav_ChildrenDescendantsParentAncestorsRoot(t *testing.T) {
	root := spanNode("t1", "root", "")
	child := spanNode("t1", "child", "root")
	grand := spanNode("t1", "grand", "child")
	all := []Value{root, child, grand}

	run := func(q string) []Value {
		t.Helper()
		parsed, err := parser.Parse(q)
		if err != nil {
			t.Fatal(err)
		}
		out, err := Evaluate(parsed, all)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	ids := func(vs []Value) []string {
		out := make([]string, len(vs))
		for i, v := range vs {
			out[i] = v.Get([]string{"span_id"}).S
		}
		return out
	}

	if got := ids(run(`spans | select(.span_id == "root") | children`)); len(got) != 1 || got[0] != "child" {
		t.Errorf("children of root = %v, want [child]", got)
	}
	if got := ids(run(`spans | select(.span_id == "root") | descendants`)); len(got) != 2 || got[0] != "child" || got[1] != "grand" {
		t.Errorf("descendants of root = %v, want [child grand]", got)
	}
	if got := ids(run(`spans | select(.span_id == "grand") | parent`)); len(got) != 1 || got[0] != "child" {
		t.Errorf("parent of grand = %v, want [child]", got)
	}
	if got := ids(run(`spans | select(.span_id == "grand") | ancestors`)); len(got) != 2 || got[0] != "child" || got[1] != "root" {
		t.Errorf("ancestors of grand = %v, want [child root]", got)
	}
	if got := ids(run(`spans | select(.span_id == "grand") | root`)); len(got) != 1 || got[0] != "root" {
		t.Errorf("root of grand = %v, want [root]", got)
	}
	if got := ids(run(`spans | select(.span_id == "root") | parent`)); len(got) != 0 {
		t.Errorf("parent of root (no parent) = %v, want empty", got)
	}
}

func TestPipeline_Nav_DedupesAcrossMultipleInputs(t *testing.T) {
	root := spanNode("t1", "root", "")
	childA := spanNode("t1", "a", "root")
	childB := spanNode("t1", "b", "root")
	q, err := parser.Parse(`spans | select(.span_id == "a" or .span_id == "b") | parent`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Evaluate(q, []Value{root, childA, childB})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Get([]string{"span_id"}).S != "root" {
		t.Errorf("parent of [a,b] = %v, want deduped [root]", out)
	}
}

func TestPipeline_Nav_DanglingParentReference(t *testing.T) {
	// orphan references a parent span_id absent from the export
	orphan := spanNode("t1", "orphan", "missing-parent")
	q, err := parser.Parse(`spans | select(.span_id == "orphan") | root`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Evaluate(q, []Value{orphan})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Get([]string{"span_id"}).S != "orphan" {
		t.Errorf("root() with dangling parent ref = %v, want [orphan] (itself, can't go higher)", out)
	}
}

func genAISpan(model string, outputTokens float64) Value {
	genai := NewObject()
	req := NewObject()
	req.Set("model", String(model))
	genai.Set("request", req)
	usage := NewObject()
	usage.Set("output_tokens", Number(outputTokens))
	genai.Set("usage", usage)
	v := NewObject()
	v.Set("gen_ai", genai)
	return v
}

func TestPipeline_CanonicalQuery_GroupBySumCount(t *testing.T) {
	q, err := parser.Parse(`spans | select(.gen_ai.request.model != null)
      | group_by(.gen_ai.request.model)
      | project({ model: .group_key, total_tokens: sum(.gen_ai.usage.output_tokens), calls: count })`)
	if err != nil {
		t.Fatal(err)
	}
	values := []Value{
		genAISpan("gpt-4", 10),
		genAISpan("gpt-3.5", 5),
		genAISpan("gpt-4", 20),
	}
	out, err := Evaluate(q, values)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d groups, want 2", len(out))
	}
	// first-occurrence order: gpt-4 seen first, then gpt-3.5
	g1 := out[0]
	if g1.Get([]string{"model"}).S != "gpt-4" {
		t.Fatalf("group 0 model = %q, want gpt-4", g1.Get([]string{"model"}).S)
	}
	if g1.Get([]string{"total_tokens"}).N != 30 {
		t.Errorf("gpt-4 total_tokens = %v, want 30", g1.Get([]string{"total_tokens"}).N)
	}
	if g1.Get([]string{"calls"}).N != 2 {
		t.Errorf("gpt-4 calls = %v, want 2", g1.Get([]string{"calls"}).N)
	}
	g2 := out[1]
	if g2.Get([]string{"model"}).S != "gpt-3.5" {
		t.Fatalf("group 1 model = %q, want gpt-3.5", g2.Get([]string{"model"}).S)
	}
	if g2.Get([]string{"total_tokens"}).N != 5 || g2.Get([]string{"calls"}).N != 1 {
		t.Errorf("gpt-3.5 group = total_tokens %v calls %v, want 5, 1", g2.Get([]string{"total_tokens"}).N, g2.Get([]string{"calls"}).N)
	}
}

func TestPipeline_GroupBy_AvgMaxP95(t *testing.T) {
	q, err := parser.Parse(`spans | group_by(.gen_ai.request.model)
      | project({ model: .group_key, avg_tokens: avg(.gen_ai.usage.output_tokens), max_tokens: max(.gen_ai.usage.output_tokens), p95_tokens: p95(.gen_ai.usage.output_tokens) })`)
	if err != nil {
		t.Fatal(err)
	}
	values := []Value{
		genAISpan("gpt-4", 10),
		genAISpan("gpt-4", 20),
		genAISpan("gpt-4", 30),
		genAISpan("gpt-4", 40),
	}
	out, err := Evaluate(q, values)
	if err != nil {
		t.Fatal(err)
	}
	g := out[0]
	if g.Get([]string{"avg_tokens"}).N != 25 {
		t.Errorf("avg_tokens = %v, want 25", g.Get([]string{"avg_tokens"}).N)
	}
	if g.Get([]string{"max_tokens"}).N != 40 {
		t.Errorf("max_tokens = %v, want 40", g.Get([]string{"max_tokens"}).N)
	}
	// nearest-rank: sorted [10,20,30,40], n=4, idx = ceil(0.95*4)-1 = ceil(3.8)-1 = 4-1 = 3 -> 40
	if g.Get([]string{"p95_tokens"}).N != 40 {
		t.Errorf("p95_tokens = %v, want 40", g.Get([]string{"p95_tokens"}).N)
	}
}

func TestPipeline_GroupBy_EmptyNumericSetIsNull(t *testing.T) {
	// bucket has spans, but none carry a numeric value at the aggregated path
	q, err := parser.Parse(`spans | group_by(.gen_ai.request.model)
      | project({ model: .group_key, avg_tokens: avg(.nonexistent), max_tokens: max(.nonexistent), p95_tokens: p95(.nonexistent), total: sum(.nonexistent), n: count })`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Evaluate(q, []Value{genAISpan("gpt-4", 10)})
	if err != nil {
		t.Fatal(err)
	}
	g := out[0]
	for _, f := range []string{"avg_tokens", "max_tokens", "p95_tokens"} {
		if g.Get([]string{f}).Kind != KNull {
			t.Errorf("%s = %+v, want null when no numeric values present", f, g.Get([]string{f}))
		}
	}
	if g.Get([]string{"total"}).N != 0 {
		t.Errorf("sum over no numeric values = %v, want 0", g.Get([]string{"total"}).N)
	}
	if g.Get([]string{"n"}).N != 1 {
		t.Errorf("count = %v, want 1 (bucket has 1 span, regardless of field presence)", g.Get([]string{"n"}).N)
	}
}

func TestPipeline_GroupBy_SelectAndSortByOnGroupKey(t *testing.T) {
	// select/sort_by after group_by operate generically on the bucket's
	// exposed .group_key/.spans fields — no special-casing required.
	q, err := parser.Parse(`spans | group_by(.gen_ai.request.model) | sort_by(.group_key)`)
	if err != nil {
		t.Fatal(err)
	}
	values := []Value{genAISpan("gpt-4", 1), genAISpan("gpt-3.5", 1)}
	out, err := Evaluate(q, values)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Get([]string{"group_key"}).S != "gpt-3.5" || out[1].Get([]string{"group_key"}).S != "gpt-4" {
		t.Errorf("sort_by(.group_key) did not sort buckets: %v, %v",
			out[0].Get([]string{"group_key"}).S, out[1].Get([]string{"group_key"}).S)
	}
}

func TestPipeline_AggCallWithoutGroupByIsFieldError(t *testing.T) {
	q, err := parser.Parse(`spans | project({ total: sum(.gen_ai.usage.output_tokens) })`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Evaluate(q, []Value{genAISpan("gpt-4", 10)})
	if err == nil {
		t.Fatal("expected field error: aggregation without a preceding group_by")
	}
}
