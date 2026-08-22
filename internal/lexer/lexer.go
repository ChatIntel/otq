package lexer

import (
	"strings"
)

// Lexer tokenizes an otq query string. It never fails outright — unrecognized
// input produces an ILLEGAL token whose Literal describes the problem, and the
// parser turns that into a otlperr.ParseError with position information.
type Lexer struct {
	input string
	pos   int // 0-indexed byte offset of the next unread byte
}

func New(input string) *Lexer {
	return &Lexer{input: input}
}

func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentPart(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func (l *Lexer) cur() byte {
	if l.pos >= len(l.input) {
		return 0
	}
	return l.input[l.pos]
}

func (l *Lexer) peekAt(off int) byte {
	if l.pos+off >= len(l.input) {
		return 0
	}
	return l.input[l.pos+off]
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.input) {
		switch l.input[l.pos] {
		case ' ', '\t', '\n', '\r':
			l.pos++
		default:
			return
		}
	}
}

// NextToken returns the next token, advancing the lexer. Position tracking
// continues across whitespace so error carets land on real content.
func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	if l.pos >= len(l.input) {
		return Token{Type: EOF, Pos: l.pos + 1}
	}

	startPos := l.pos + 1 // 1-indexed
	b := l.cur()

	switch {
	case b == '.':
		return l.lexField(startPos)
	case b == '"':
		return l.lexString(startPos)
	case isDigit(b):
		return l.lexNumber(startPos)
	case isIdentStart(b):
		return l.lexIdent(startPos)
	}

	switch b {
	case '|':
		l.pos++
		return Token{Type: PIPE, Literal: "|", Pos: startPos}
	case '(':
		l.pos++
		return Token{Type: LPAREN, Literal: "(", Pos: startPos}
	case ')':
		l.pos++
		return Token{Type: RPAREN, Literal: ")", Pos: startPos}
	case '{':
		l.pos++
		return Token{Type: LBRACE, Literal: "{", Pos: startPos}
	case '}':
		l.pos++
		return Token{Type: RBRACE, Literal: "}", Pos: startPos}
	case ':':
		l.pos++
		return Token{Type: COLON, Literal: ":", Pos: startPos}
	case ',':
		l.pos++
		return Token{Type: COMMA, Literal: ",", Pos: startPos}
	case ';':
		l.pos++
		return Token{Type: SEMICOLON, Literal: ";", Pos: startPos}
	case '=':
		if l.peekAt(1) == '=' {
			l.pos += 2
			return Token{Type: EQ, Literal: "==", Pos: startPos}
		}
		l.pos++
		return Token{Type: ILLEGAL, Literal: "unexpected '='", Pos: startPos}
	case '!':
		if l.peekAt(1) == '=' {
			l.pos += 2
			return Token{Type: NE, Literal: "!=", Pos: startPos}
		}
		l.pos++
		return Token{Type: ILLEGAL, Literal: "unexpected '!'", Pos: startPos}
	case '>':
		if l.peekAt(1) == '=' {
			l.pos += 2
			return Token{Type: GE, Literal: ">=", Pos: startPos}
		}
		l.pos++
		return Token{Type: GT, Literal: ">", Pos: startPos}
	case '<':
		if l.peekAt(1) == '=' {
			l.pos += 2
			return Token{Type: LE, Literal: "<=", Pos: startPos}
		}
		l.pos++
		return Token{Type: LT, Literal: "<", Pos: startPos}
	}

	l.pos++
	return Token{Type: ILLEGAL, Literal: "unexpected character '" + string(b) + "'", Pos: startPos}
}

// lexField consumes '.' ident ('.' ident)* as a single FIELD token, literal
// is the dotted path without the leading dot (e.g. "gen_ai.request.model").
func (l *Lexer) lexField(startPos int) Token {
	start := l.pos
	l.pos++ // consume leading '.'
	if !isIdentStart(l.cur()) {
		return Token{Type: ILLEGAL, Literal: "expected field name after '.'", Pos: startPos}
	}
	for isIdentPart(l.cur()) {
		l.pos++
	}
	for l.cur() == '.' && isIdentStart(l.peekAt(1)) {
		l.pos++ // consume '.'
		for isIdentPart(l.cur()) {
			l.pos++
		}
	}
	return Token{Type: FIELD, Literal: l.input[start+1 : l.pos], Pos: startPos}
}

func (l *Lexer) lexIdent(startPos int) Token {
	start := l.pos
	for isIdentPart(l.cur()) {
		l.pos++
	}
	return Token{Type: IDENT, Literal: l.input[start:l.pos], Pos: startPos}
}

func (l *Lexer) lexNumber(startPos int) Token {
	start := l.pos
	for isDigit(l.cur()) {
		l.pos++
	}
	if l.cur() == '.' && isDigit(l.peekAt(1)) {
		l.pos++
		for isDigit(l.cur()) {
			l.pos++
		}
	}
	return Token{Type: NUMBER, Literal: l.input[start:l.pos], Pos: startPos}
}

func (l *Lexer) lexString(startPos int) Token {
	l.pos++ // consume opening quote
	var sb strings.Builder
	for {
		if l.pos >= len(l.input) {
			return Token{Type: ILLEGAL, Literal: "unterminated string literal", Pos: startPos}
		}
		c := l.input[l.pos]
		if c == '"' {
			l.pos++
			return Token{Type: STRING, Literal: sb.String(), Pos: startPos}
		}
		if c == '\\' {
			l.pos++
			if l.pos >= len(l.input) {
				return Token{Type: ILLEGAL, Literal: "unterminated string literal", Pos: startPos}
			}
			switch l.input[l.pos] {
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			default:
				sb.WriteByte(l.input[l.pos])
			}
			l.pos++
			continue
		}
		sb.WriteByte(c)
		l.pos++
	}
}
