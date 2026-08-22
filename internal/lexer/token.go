// Package lexer tokenizes otq query strings.
package lexer

type TokenType int

const (
	EOF TokenType = iota
	ILLEGAL

	IDENT  // bare word: spans, traces, select, sort_by, and, not, startswith, asc, desc, null, true, false, ...
	FIELD  // .foo or .foo.bar.baz — whole dotted path lexed as one token, literal excludes the leading dot
	STRING // "..."
	NUMBER // 123 or 1.5

	PIPE      // |
	LPAREN    // (
	RPAREN    // )
	LBRACE    // {
	RBRACE    // }
	COLON     // :
	COMMA     // ,
	SEMICOLON // ; (used by any/all quantifiers)

	EQ // ==
	NE // !=
	GT // >
	LT // <
	GE // >=
	LE // <=
)

var tokenNames = map[TokenType]string{
	EOF: "EOF", ILLEGAL: "ILLEGAL",
	IDENT: "IDENT", FIELD: "FIELD", STRING: "STRING", NUMBER: "NUMBER",
	PIPE: "|", LPAREN: "(", RPAREN: ")", LBRACE: "{", RBRACE: "}",
	COLON: ":", COMMA: ",", SEMICOLON: ";",
	EQ: "==", NE: "!=", GT: ">", LT: "<", GE: ">=", LE: "<=",
}

func (t TokenType) String() string {
	if n, ok := tokenNames[t]; ok {
		return n
	}
	return "UNKNOWN"
}

// Token is one lexical unit. Pos is the 1-indexed byte offset of the
// token's first character in the original query string, used to render
// caret-diagram parse errors.
type Token struct {
	Type    TokenType
	Literal string
	Pos     int
}
