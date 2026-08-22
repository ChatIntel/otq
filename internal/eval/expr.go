package eval

import (
	"strings"

	"otq/internal/otlperr"
	"otq/internal/parser"
)

// literalToValue converts a parsed AST literal into a Value for comparison.
func literalToValue(lit parser.Literal) Value {
	switch lit.Kind {
	case parser.LitString:
		return String(lit.Str)
	case parser.LitNumber:
		return Number(lit.Num)
	case parser.LitBool:
		return Bool(lit.Bool)
	default: // parser.LitNull
		return Null()
	}
}

// EvalExpr evaluates a boolean expression against a root Value — a span
// under `spans`, or a {trace_id, spans} object under `traces` (that's what
// makes `any(.spans; ...)` resolve).
func EvalExpr(e parser.Expr, root Value) (bool, error) {
	switch expr := e.(type) {
	case parser.BinaryExpr:
		left, err := EvalExpr(expr.Left, root)
		if err != nil {
			return false, err
		}
		if expr.Op == parser.OpAnd && !left {
			return false, nil
		}
		if expr.Op == parser.OpOr && left {
			return true, nil
		}
		return EvalExpr(expr.Right, root)

	case parser.NotExpr:
		inner, err := EvalExpr(expr.Expr, root)
		if err != nil {
			return false, err
		}
		return !inner, nil

	case parser.ComparisonExpr:
		return evalComparison(expr, root), nil

	case parser.StrcallExpr:
		return evalStrcall(expr, root), nil

	case parser.QuantifierExpr:
		return evalQuantifier(expr, root)
	}
	return false, &otlperr.FieldError{Field: "", Msg: "internal: unhandled expression type"}
}

func evalComparison(e parser.ComparisonExpr, root Value) bool {
	lhs := root.Get(e.Path.Segments)
	rhs := literalToValue(e.Literal)
	switch e.Op {
	case parser.CmpEq:
		return Equal(lhs, rhs)
	case parser.CmpNe:
		return !Equal(lhs, rhs)
	case parser.CmpGt:
		return Less(rhs, lhs)
	case parser.CmpLt:
		return Less(lhs, rhs)
	case parser.CmpGe:
		return Equal(lhs, rhs) || Less(rhs, lhs)
	case parser.CmpLe:
		return Equal(lhs, rhs) || Less(lhs, rhs)
	}
	return false
}

// evalQuantifier requires the quantified path to resolve to an array (e.g.
// .spans within the `traces` source) — a type mismatch here is a field
// error, not a parse error: the query is syntactically valid, the
// referenced field just has the wrong shape at evaluation time. Body is
// evaluated once per array element, with that element as the new root —
// this is the one place a nested evaluation scope exists in the grammar.
func evalQuantifier(e parser.QuantifierExpr, root Value) (bool, error) {
	arr := root.Get(e.Path.Segments)
	if arr.Kind != KArray {
		return false, &otlperr.FieldError{
			Field: "." + strings.Join(e.Path.Segments, "."),
			Msg:   "is not an array (required by any/all)",
		}
	}
	switch e.Kind {
	case parser.QuantAny:
		for _, elem := range arr.A {
			ok, err := EvalExpr(e.Body, elem)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case parser.QuantAll:
		for _, elem := range arr.A {
			ok, err := EvalExpr(e.Body, elem)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil
	}
	return false, nil
}

func evalStrcall(e parser.StrcallExpr, root Value) bool {
	lhs := root.Get(e.Path.Segments)
	if lhs.Kind != KString {
		return false
	}
	switch e.Func {
	case parser.StrStartsWith:
		return strings.HasPrefix(lhs.S, e.Arg)
	case parser.StrEndsWith:
		return strings.HasSuffix(lhs.S, e.Arg)
	case parser.StrContains:
		return strings.Contains(lhs.S, e.Arg)
	}
	return false
}
