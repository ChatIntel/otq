package parser

import (
	"fmt"
	"strconv"
	"strings"

	"otq/internal/lexer"
	"otq/internal/otlperr"
)

// Parser is a recursive-descent parser for the otq query grammar. It always
// retains the original query string so any error it raises can render the
// caret-diagram ParseError format.
type Parser struct {
	lex   *lexer.Lexer
	query string
	cur   lexer.Token

	// sawGroupBy tracks whether a group_by stage has already been parsed
	// earlier in this pipeline. project()'s bare-field shorthand is only
	// valid pre-group_by (see parseObjectField) — once group_by has been
	// seen, every object field must be explicit (field: value), since the
	// post-group_by bucket context no longer has a same-named field to
	// shorthand-reference.
	sawGroupBy bool
}

func New(query string) *Parser {
	p := &Parser{lex: lexer.New(query), query: query}
	p.cur = p.lex.NextToken()
	return p
}

// Parse parses a full `query := source ('|' stage)*` and returns the AST, or
// a *otlperr.ParseError.
func Parse(query string) (*Query, error) {
	p := New(query)
	return p.Parse()
}

func (p *Parser) Parse() (*Query, error) {
	src, err := p.parseSource()
	if err != nil {
		return nil, err
	}
	q := &Query{Source: src}
	for p.cur.Type == lexer.PIPE {
		p.advance()
		stage, err := p.parseStage()
		if err != nil {
			return nil, err
		}
		q.Stages = append(q.Stages, stage)
	}
	if p.cur.Type != lexer.EOF {
		return nil, p.errorf(p.cur.Pos, "unexpected token %s (expected end of query)", p.describe(p.cur))
	}
	return q, nil
}

func (p *Parser) advance() { p.cur = p.lex.NextToken() }

func (p *Parser) errorf(pos int, format string, args ...any) *otlperr.ParseError {
	return &otlperr.ParseError{Pos: pos, Msg: fmt.Sprintf(format, args...), Query: p.query}
}

func (p *Parser) describe(t lexer.Token) string {
	switch t.Type {
	case lexer.EOF:
		return "end of query"
	case lexer.ILLEGAL:
		return t.Literal
	case lexer.IDENT, lexer.NUMBER:
		return fmt.Sprintf("'%s'", t.Literal)
	case lexer.FIELD:
		return fmt.Sprintf("'.%s'", t.Literal)
	case lexer.STRING:
		return fmt.Sprintf("%q", t.Literal)
	default:
		return fmt.Sprintf("'%s'", t.Type.String())
	}
}

func (p *Parser) expect(tt lexer.TokenType) error {
	if p.cur.Type != tt {
		return p.errorf(p.cur.Pos, "unexpected token %s (expected '%s')", p.describe(p.cur), tt.String())
	}
	p.advance()
	return nil
}

func (p *Parser) parseSource() (SourceKind, error) {
	if p.cur.Type != lexer.IDENT || (p.cur.Literal != "spans" && p.cur.Literal != "traces") {
		return 0, p.errorf(p.cur.Pos, "unexpected token %s (expected 'spans' or 'traces')", p.describe(p.cur))
	}
	kind := SourceSpans
	if p.cur.Literal == "traces" {
		kind = SourceTraces
	}
	p.advance()
	return kind, nil
}

func (p *Parser) parseStage() (Stage, error) {
	if p.cur.Type != lexer.IDENT {
		return nil, p.errorf(p.cur.Pos, "unexpected token %s (expected a stage)", p.describe(p.cur))
	}
	switch p.cur.Literal {
	case "select":
		return p.parseSelectStage()
	case "sort_by":
		return p.parseSortByStage()
	case "limit":
		return p.parseLimitStage()
	case "project":
		return p.parseProjectStage()
	case "group_by":
		return p.parseGroupByStage()
	case "children":
		p.advance()
		return NavStage{Kind: NavChildren}, nil
	case "descendants":
		p.advance()
		return NavStage{Kind: NavDescendants}, nil
	case "parent":
		p.advance()
		return NavStage{Kind: NavParent}, nil
	case "ancestors":
		p.advance()
		return NavStage{Kind: NavAncestors}, nil
	case "root":
		p.advance()
		return NavStage{Kind: NavRoot}, nil
	default:
		return nil, p.errorf(p.cur.Pos, "unknown stage %q", p.cur.Literal)
	}
}

func (p *Parser) parseSelectStage() (Stage, error) {
	p.advance() // 'select'
	if err := p.expect(lexer.LPAREN); err != nil {
		return nil, err
	}
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(lexer.RPAREN); err != nil {
		return nil, err
	}
	return SelectStage{Expr: expr}, nil
}

func (p *Parser) parseSortByStage() (Stage, error) {
	p.advance() // 'sort_by'
	if err := p.expect(lexer.LPAREN); err != nil {
		return nil, err
	}
	path, err := p.parsePath()
	if err != nil {
		return nil, err
	}
	order := Asc
	if p.cur.Type == lexer.COMMA {
		p.advance()
		if p.cur.Type != lexer.IDENT || (p.cur.Literal != "asc" && p.cur.Literal != "desc") {
			return nil, p.errorf(p.cur.Pos, "unexpected token %s (expected 'asc' or 'desc')", p.describe(p.cur))
		}
		if p.cur.Literal == "desc" {
			order = Desc
		}
		p.advance()
	}
	if err := p.expect(lexer.RPAREN); err != nil {
		return nil, err
	}
	return SortByStage{Path: path, Order: order}, nil
}

func (p *Parser) parseLimitStage() (Stage, error) {
	p.advance() // 'limit'
	if err := p.expect(lexer.LPAREN); err != nil {
		return nil, err
	}
	if p.cur.Type != lexer.NUMBER {
		return nil, p.errorf(p.cur.Pos, "unexpected token %s (expected a number)", p.describe(p.cur))
	}
	n, err := strconv.Atoi(p.cur.Literal)
	if err != nil {
		return nil, p.errorf(p.cur.Pos, "invalid integer %q", p.cur.Literal)
	}
	p.advance()
	if err := p.expect(lexer.RPAREN); err != nil {
		return nil, err
	}
	return LimitStage{N: n}, nil
}

func (p *Parser) parseGroupByStage() (Stage, error) {
	p.advance() // 'group_by'
	if err := p.expect(lexer.LPAREN); err != nil {
		return nil, err
	}
	path, err := p.parsePath()
	if err != nil {
		return nil, err
	}
	if err := p.expect(lexer.RPAREN); err != nil {
		return nil, err
	}
	p.sawGroupBy = true
	return GroupByStage{Path: path}, nil
}

func (p *Parser) parseProjectStage() (Stage, error) {
	p.advance() // 'project'
	if err := p.expect(lexer.LPAREN); err != nil {
		return nil, err
	}
	fields, err := p.parseObject()
	if err != nil {
		return nil, err
	}
	if err := p.expect(lexer.RPAREN); err != nil {
		return nil, err
	}
	return ProjectStage{Fields: fields}, nil
}

func (p *Parser) parseObject() ([]ObjectField, error) {
	if err := p.expect(lexer.LBRACE); err != nil {
		return nil, err
	}
	var fields []ObjectField
	if p.cur.Type != lexer.RBRACE {
		for {
			f, err := p.parseObjectField()
			if err != nil {
				return nil, err
			}
			fields = append(fields, f)
			if p.cur.Type == lexer.COMMA {
				p.advance()
				continue
			}
			break
		}
	}
	if err := p.expect(lexer.RBRACE); err != nil {
		return nil, err
	}
	return fields, nil
}

func isAggName(lit string) bool {
	switch lit {
	case "sum", "avg", "max", "p95", "count":
		return true
	}
	return false
}

func (p *Parser) parseObjectField() (ObjectField, error) {
	if p.cur.Type != lexer.IDENT {
		return ObjectField{}, p.errorf(p.cur.Pos, "unexpected token %s (expected a field name)", p.describe(p.cur))
	}
	name := p.cur.Literal
	namePos := p.cur.Pos
	p.advance()
	if p.cur.Type != lexer.COLON {
		if p.sawGroupBy {
			return ObjectField{}, p.errorf(namePos, "project() after group_by requires explicit field: value pairs")
		}
		// shorthand: `field` means `field: .field` on the current context
		return ObjectField{Name: name, Value: Path{Segments: []string{name}}}, nil
	}
	p.advance() // ':'
	if p.cur.Type == lexer.FIELD {
		path, err := p.parsePath()
		if err != nil {
			return ObjectField{}, err
		}
		return ObjectField{Name: name, Value: path}, nil
	}
	if p.cur.Type == lexer.IDENT && isAggName(p.cur.Literal) {
		agg, err := p.parseAggCall()
		if err != nil {
			return ObjectField{}, err
		}
		return ObjectField{Name: name, Value: agg}, nil
	}
	return ObjectField{}, p.errorf(p.cur.Pos, "unexpected token %s (expected a field path or aggregation call)", p.describe(p.cur))
}

func (p *Parser) parseAggCall() (AggCallExpr, error) {
	var kind AggKind
	switch p.cur.Literal {
	case "sum":
		kind = AggSum
	case "avg":
		kind = AggAvg
	case "max":
		kind = AggMax
	case "p95":
		kind = AggP95
	case "count":
		kind = AggCount
	}
	p.advance()
	if kind == AggCount {
		return AggCallExpr{Kind: kind}, nil
	}
	if err := p.expect(lexer.LPAREN); err != nil {
		return AggCallExpr{}, err
	}
	path, err := p.parsePath()
	if err != nil {
		return AggCallExpr{}, err
	}
	if err := p.expect(lexer.RPAREN); err != nil {
		return AggCallExpr{}, err
	}
	return AggCallExpr{Kind: kind, Path: path}, nil
}

// parseExpr / parseOr / parseAnd / parseUnary implement the spec's fixed
// precedence: `not` > `and` > `or`, left-associative. Loops (not recursion)
// at the `or`/`and` levels keep left-associativity explicit.
func (p *Parser) parseExpr() (Expr, error) { return p.parseOr() }

func (p *Parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.cur.Type == lexer.IDENT && p.cur.Literal == "or" {
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = BinaryExpr{Op: OpOr, Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseAnd() (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.cur.Type == lexer.IDENT && p.cur.Literal == "and" {
		p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = BinaryExpr{Op: OpAnd, Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseUnary() (Expr, error) {
	if p.cur.Type == lexer.IDENT && p.cur.Literal == "not" {
		p.advance()
		e, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return NotExpr{Expr: e}, nil
	}
	return p.parsePrimary()
}

func (p *Parser) parsePrimary() (Expr, error) {
	if p.cur.Type == lexer.LPAREN {
		p.advance()
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(lexer.RPAREN); err != nil {
			return nil, err
		}
		return e, nil
	}
	if p.cur.Type == lexer.IDENT && (p.cur.Literal == "any" || p.cur.Literal == "all") {
		return p.parseQuantifier()
	}
	return p.parseComparisonOrStrcall()
}

func (p *Parser) parseQuantifier() (Expr, error) {
	kind := QuantAny
	if p.cur.Literal == "all" {
		kind = QuantAll
	}
	p.advance()
	if err := p.expect(lexer.LPAREN); err != nil {
		return nil, err
	}
	path, err := p.parsePath()
	if err != nil {
		return nil, err
	}
	if err := p.expect(lexer.SEMICOLON); err != nil {
		return nil, err
	}
	body, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(lexer.RPAREN); err != nil {
		return nil, err
	}
	return QuantifierExpr{Kind: kind, Path: path, Body: body}, nil
}

func isStrFuncName(lit string) bool {
	switch lit {
	case "startswith", "endswith", "contains":
		return true
	}
	return false
}

func (p *Parser) parseComparisonOrStrcall() (Expr, error) {
	path, err := p.parsePath()
	if err != nil {
		return nil, err
	}
	if p.cur.Type == lexer.PIPE {
		p.advance()
		if p.cur.Type != lexer.IDENT || !isStrFuncName(p.cur.Literal) {
			return nil, p.errorf(p.cur.Pos, "unexpected token %s (expected startswith, endswith, or contains)", p.describe(p.cur))
		}
		var fn StrFunc
		switch p.cur.Literal {
		case "startswith":
			fn = StrStartsWith
		case "endswith":
			fn = StrEndsWith
		case "contains":
			fn = StrContains
		}
		p.advance()
		if err := p.expect(lexer.LPAREN); err != nil {
			return nil, err
		}
		if p.cur.Type != lexer.STRING {
			return nil, p.errorf(p.cur.Pos, "unexpected token %s (expected a string)", p.describe(p.cur))
		}
		arg := p.cur.Literal
		p.advance()
		if err := p.expect(lexer.RPAREN); err != nil {
			return nil, err
		}
		return StrcallExpr{Path: path, Func: fn, Arg: arg}, nil
	}

	op, err := p.parseCmpOp()
	if err != nil {
		return nil, err
	}
	lit, err := p.parseLiteral()
	if err != nil {
		return nil, err
	}
	return ComparisonExpr{Path: path, Op: op, Literal: lit}, nil
}

func (p *Parser) parseCmpOp() (CmpOp, error) {
	var op CmpOp
	switch p.cur.Type {
	case lexer.EQ:
		op = CmpEq
	case lexer.NE:
		op = CmpNe
	case lexer.GT:
		op = CmpGt
	case lexer.LT:
		op = CmpLt
	case lexer.GE:
		op = CmpGe
	case lexer.LE:
		op = CmpLe
	default:
		return 0, p.errorf(p.cur.Pos, "unexpected token %s (expected a comparison operator)", p.describe(p.cur))
	}
	p.advance()
	return op, nil
}

func (p *Parser) parseLiteral() (Literal, error) {
	switch p.cur.Type {
	case lexer.STRING:
		lit := Literal{Kind: LitString, Str: p.cur.Literal}
		p.advance()
		return lit, nil
	case lexer.NUMBER:
		n, err := strconv.ParseFloat(p.cur.Literal, 64)
		if err != nil {
			return Literal{}, p.errorf(p.cur.Pos, "invalid number %q", p.cur.Literal)
		}
		lit := Literal{Kind: LitNumber, Num: n}
		p.advance()
		return lit, nil
	case lexer.IDENT:
		switch p.cur.Literal {
		case "null":
			p.advance()
			return Literal{Kind: LitNull}, nil
		case "true":
			p.advance()
			return Literal{Kind: LitBool, Bool: true}, nil
		case "false":
			p.advance()
			return Literal{Kind: LitBool, Bool: false}, nil
		}
	}
	return Literal{}, p.errorf(p.cur.Pos, "unexpected token %s (expected a literal value)", p.describe(p.cur))
}

func (p *Parser) parsePath() (Path, error) {
	if p.cur.Type != lexer.FIELD {
		return Path{}, p.errorf(p.cur.Pos, "unexpected token %s (expected a field path)", p.describe(p.cur))
	}
	segments := strings.Split(p.cur.Literal, ".")
	p.advance()
	return Path{Segments: segments}, nil
}
