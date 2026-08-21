package eval

import (
	"encoding/json"
	"math"
	"sort"

	"otq/internal/otlperr"
	"otq/internal/parser"
)

// pipelineCtx carries state that spans stages but isn't itself part of the
// value stream: whether group_by has run yet (gates aggregation in
// project()), and the trace-tree index (built once, from the full input
// span set, regardless of source or later filtering — nav stages resolve
// parent/child relationships against the whole export, not just whatever
// the stream has been filtered down to so far).
type pipelineCtx struct {
	grouped bool
	index   *spanIndex
}

// Evaluate runs a parsed query against already-flattened span values (see
// flatten.Span.ToValue — the caller converts spans to Value before calling
// this, since internal/flatten depends on internal/eval and this package
// cannot depend back on internal/flatten without an import cycle).
func Evaluate(q *parser.Query, spanValues []Value) ([]Value, error) {
	ctx := &pipelineCtx{index: buildSpanIndex(spanValues)}

	var values []Value
	switch q.Source {
	case parser.SourceSpans:
		values = spanValues
	case parser.SourceTraces:
		values = buildTraces(spanValues)
	}

	for _, stage := range q.Stages {
		var err error
		values, err = runStage(stage, values, ctx)
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func runStage(stage parser.Stage, values []Value, ctx *pipelineCtx) ([]Value, error) {
	switch st := stage.(type) {
	case parser.SelectStage:
		return stageSelect(st, values)
	case parser.SortByStage:
		return stageSortBy(st, values), nil
	case parser.LimitStage:
		return stageLimit(st, values), nil
	case parser.ProjectStage:
		return stageProject(st, values, ctx.grouped)
	case parser.GroupByStage:
		out := stageGroupBy(st, values)
		ctx.grouped = true
		return out, nil
	case parser.NavStage:
		return stageNav(st, values, ctx.index), nil
	}
	return nil, &otlperr.FieldError{Field: "", Msg: "internal: unhandled stage type"}
}

// stageGroupBy buckets values by the structural equality of the grouped
// path's value, in first-occurrence order (not sorted — sort_by(.group_key)
// after group_by works for that, since the bucket generically exposes
// .group_key through the same Get() every other stage uses). Each output
// bucket is {group_key: <value>, spans: [<original values in this
// group>]} — the shape every subsequent stage (select/sort_by/project) sees
// as its "current context", per the spec's post-group_by semantics.
func stageGroupBy(st parser.GroupByStage, values []Value) []Value {
	type bucket struct {
		key   Value
		spans []Value
	}
	buckets := map[string]*bucket{}
	var order []string

	for _, v := range values {
		key := v.Get(st.Path.Segments)
		keyStr := canonicalKeyString(key)
		b, ok := buckets[keyStr]
		if !ok {
			b = &bucket{key: key}
			buckets[keyStr] = b
			order = append(order, keyStr)
		}
		b.spans = append(b.spans, v)
	}

	out := make([]Value, len(order))
	for i, k := range order {
		b := buckets[k]
		bv := NewObject()
		bv.Set("group_key", b.key)
		bv.Set("spans", Array(b.spans))
		out[i] = bv
	}
	return out
}

func canonicalKeyString(v Value) string {
	data, _ := json.Marshal(v.ToJSON())
	return string(data)
}

func stageSelect(st parser.SelectStage, values []Value) ([]Value, error) {
	var out []Value
	for _, v := range values {
		ok, err := EvalExpr(st.Expr, v)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, v)
		}
	}
	return out, nil
}

// stageSortBy: a value whose sort path is missing/null sorts first
// regardless of asc/desc — computed before the order-flip, so null
// placement stays predictable rather than flip-flopping with direction.
func stageSortBy(st parser.SortByStage, values []Value) []Value {
	out := append([]Value(nil), values...)
	less := func(a, b Value) bool {
		av := a.Get(st.Path.Segments)
		bv := b.Get(st.Path.Segments)
		aNull, bNull := av.Kind == KNull, bv.Kind == KNull
		if aNull && bNull {
			return false
		}
		if aNull {
			return true
		}
		if bNull {
			return false
		}
		if st.Order == parser.Desc {
			return Less(bv, av)
		}
		return Less(av, bv)
	}
	SortStableByLess(out, less)
	return out
}

func stageLimit(st parser.LimitStage, values []Value) []Value {
	n := st.N
	if n < 0 {
		n = 0
	}
	if n > len(values) {
		n = len(values)
	}
	return values[:n]
}

func stageProject(st parser.ProjectStage, values []Value, grouped bool) ([]Value, error) {
	out := make([]Value, len(values))
	for i, v := range values {
		obj := NewObject()
		for _, f := range st.Fields {
			switch fv := f.Value.(type) {
			case parser.Path:
				obj.Set(f.Name, v.Get(fv.Segments))
			case parser.AggCallExpr:
				if !grouped {
					return nil, &otlperr.FieldError{Field: f.Name, Msg: "aggregation requires a preceding group_by stage"}
				}
				obj.Set(f.Name, evalAggCall(fv, v))
			default:
				return nil, &otlperr.FieldError{Field: f.Name, Msg: "internal: unhandled object field value type"}
			}
		}
		out[i] = obj
	}
	return out, nil
}

// evalAggCall evaluates an aggregation over bucket.spans (the current
// group_by bucket). count is always len(spans), well-defined even for an
// empty bucket (0). sum treats non-numeric/missing field values as a
// zero contribution (well-defined for an empty numeric set too: 0). avg,
// max, and p95 operate only over the numeric values actually found across
// the bucket's spans — an empty bucket, or a bucket with no numeric values
// at that path, yields null for all three (never a crash), per the spec's
// explicit empty-bucket rule.
func evalAggCall(agg parser.AggCallExpr, bucket Value) Value {
	spansVal := bucket.Get([]string{"spans"})
	var spans []Value
	if spansVal.Kind == KArray {
		spans = spansVal.A
	}

	if agg.Kind == parser.AggCount {
		return Number(float64(len(spans)))
	}

	var nums []float64
	for _, s := range spans {
		fv := s.Get(agg.Path.Segments)
		if fv.Kind == KNumber {
			nums = append(nums, fv.N)
		}
	}

	switch agg.Kind {
	case parser.AggSum:
		var sum float64
		for _, n := range nums {
			sum += n
		}
		return Number(sum)
	case parser.AggAvg:
		if len(nums) == 0 {
			return Null()
		}
		var sum float64
		for _, n := range nums {
			sum += n
		}
		return Number(sum / float64(len(nums)))
	case parser.AggMax:
		if len(nums) == 0 {
			return Null()
		}
		max := nums[0]
		for _, n := range nums[1:] {
			if n > max {
				max = n
			}
		}
		return Number(max)
	case parser.AggP95:
		if len(nums) == 0 {
			return Null()
		}
		sorted := append([]float64(nil), nums...)
		sort.Float64s(sorted)
		idx := int(math.Ceil(0.95*float64(len(sorted)))) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sorted) {
			idx = len(sorted) - 1
		}
		return Number(sorted[idx])
	}
	return Null()
}
