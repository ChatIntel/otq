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

// AggKind/AggCallExpr (sum/avg/max/p95/count) are usable only inside
// project() object field values, and only meaningful after a group_by
// stage — see eval.stageProject.
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
// Value is either a Path or an AggCallExpr.
type ObjectField struct {
	Name  string
	Value ObjectFieldValue
}

type ObjectFieldValue interface{ objectFieldValueNode() }

func (Path) objectFieldValueNode()        {}
func (AggCallExpr) objectFieldValueNode() {}
