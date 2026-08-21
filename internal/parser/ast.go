// Package parser turns a token stream from internal/lexer into an AST.
package parser

// SourceKind is the data source a query pipeline reads from.
type SourceKind int

const (
	SourceSpans SourceKind = iota
	SourceTraces
)

// Query is the root AST node: `source ('|' stage)*`.
type Query struct {
	Source SourceKind
	Stages []Stage
}

// Stage is a pipeline stage. Sealed via unexported marker method so the
// evaluator's type switch is exhaustive at compile time.
type Stage interface{ stageNode() }

type SelectStage struct{ Expr Expr }
type SortByStage struct {
	Path  Path
	Order Order
}
type LimitStage struct{ N int }
type ProjectStage struct{ Fields []ObjectField }

// GroupByStage, NavStage are Phase 2/3 grammar — they parse successfully now
// (parse-only stubs) but the evaluator rejects them with a clear
// "not supported" field error until their phases land.
type GroupByStage struct{ Path Path }
type NavStage struct{ Kind NavKind }

func (SelectStage) stageNode()  {}
func (SortByStage) stageNode()  {}
func (LimitStage) stageNode()   {}
func (ProjectStage) stageNode() {}
func (GroupByStage) stageNode() {}
func (NavStage) stageNode()     {}

type Order int

const (
	Asc Order = iota
	Desc
)

type NavKind int

const (
	NavChildren NavKind = iota
	NavDescendants
	NavParent
	NavAncestors
	NavRoot
)

// Expr is a boolean expression usable inside select(...) and any/all(...).
type Expr interface{ exprNode() }

type BoolOp int

const (
	OpAnd BoolOp = iota
	OpOr
)

type BinaryExpr struct {
	Op          BoolOp
	Left, Right Expr
}
type NotExpr struct{ Expr Expr }

type CmpOp int

const (
	CmpEq CmpOp = iota
	CmpNe
	CmpGt
	CmpLt
	CmpGe
	CmpLe
)

type ComparisonExpr struct {
	Path    Path
	Op      CmpOp
	Literal Literal
}

type StrFunc int

const (
	StrStartsWith StrFunc = iota
	StrEndsWith
	StrContains
)

type StrcallExpr struct {
	Path Path
	Func StrFunc
	Arg  string
}

// QuantifierExpr is Phase 3 grammar (any/all) — parses now, evaluator rejects.
type QuantKind int

const (
	QuantAny QuantKind = iota
	QuantAll
)

type QuantifierExpr struct {
	Kind QuantKind
	Path Path
	Body Expr
}

func (BinaryExpr) exprNode()     {}
func (NotExpr) exprNode()        {}
func (ComparisonExpr) exprNode() {}
func (StrcallExpr) exprNode()    {}
func (QuantifierExpr) exprNode() {}

// Path is a dotted field reference, e.g. .gen_ai.request.model -> ["gen_ai","request","model"].
type Path struct{ Segments []string }

type LiteralKind int

const (
	LitString LiteralKind = iota
	LitNumber
	LitNull
	LitBool
)

type Literal struct {
	Kind LiteralKind
	Str  string
	Num  float64
	Bool bool
}

// AggKind/AggCallExpr are Phase 2 grammar (sum/avg/max/p95/count) — usable
// only inside project() object field values. Parses now, evaluator rejects
// unless a later phase implements aggregation.
type AggKind int

const (
	AggSum AggKind = iota
	AggAvg
	AggMax
	AggP95
	AggCount
)

type AggCallExpr struct {
	Kind AggKind
	Path Path // unused (zero value) for AggCount
}

// ObjectField is one `field` or `field: value` entry inside project({...}).
// Value is either a Path (Phase 1) or an AggCallExpr (Phase 2 stub).
type ObjectField struct {
	Name  string
	Value ObjectFieldValue
}

type ObjectFieldValue interface{ objectFieldValueNode() }

func (Path) objectFieldValueNode()        {}
func (AggCallExpr) objectFieldValueNode() {}
