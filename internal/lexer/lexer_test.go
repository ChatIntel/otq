package lexer

import "testing"

func tokenize(input string) []Token {
	l := New(input)
	var toks []Token
	for {
		tok := l.NextToken()
		toks = append(toks, tok)
		if tok.Type == EOF {
			return toks
		}
	}
}

func TestBasicTokens(t *testing.T) {
	toks := tokenize(`spans | select(.name == "gen_ai.chat")`)
	want := []struct {
		typ TokenType
		lit string
		pos int
	}{
		{IDENT, "spans", 1},
		{PIPE, "|", 7},
		{IDENT, "select", 9},
		{LPAREN, "(", 15},
		{FIELD, "name", 16},
		{EQ, "==", 22},
		{STRING, "gen_ai.chat", 25},
		{RPAREN, ")", 38},
		{EOF, "", 39},
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %+v", len(toks), len(want), toks)
	}
	for i, w := range want {
		if toks[i].Type != w.typ || toks[i].Literal != w.lit || toks[i].Pos != w.pos {
			t.Errorf("token %d: got {%v %q %d}, want {%v %q %d}", i, toks[i].Type, toks[i].Literal, toks[i].Pos, w.typ, w.lit, w.pos)
		}
	}
}

func TestDottedFieldSingleToken(t *testing.T) {
	toks := tokenize(`.gen_ai.request.model`)
	if len(toks) != 2 {
		t.Fatalf("got %d tokens, want 2 (FIELD, EOF): %+v", len(toks), toks)
	}
	if toks[0].Type != FIELD || toks[0].Literal != "gen_ai.request.model" || toks[0].Pos != 1 {
		t.Errorf("got %+v", toks[0])
	}
}

func TestOperators(t *testing.T) {
	toks := tokenize(`== != > < >= <=`)
	want := []TokenType{EQ, NE, GT, LT, GE, LE, EOF}
	for i, w := range want {
		if toks[i].Type != w {
			t.Errorf("token %d: got %v want %v", i, toks[i].Type, w)
		}
	}
}

func TestSortByDescExample(t *testing.T) {
	// This is the spec's own error-example query; lex it and check position
	// of the bare 'desc' identifier, since the parser error test depends on
	// this exact position (14) for the caret diagram regression test.
	toks := tokenize(`sort_by(.duration_ms desc)`)
	// sort_by(  -> pos 1
	// .duration_ms -> pos 9
	// desc -> pos 22
	for _, tok := range toks {
		if tok.Literal == "desc" {
			if tok.Pos != 22 {
				t.Errorf("desc token pos = %d, want 22", tok.Pos)
			}
			return
		}
	}
	t.Fatal("desc token not found")
}

func TestNumberAndString(t *testing.T) {
	toks := tokenize(`limit(10) "a\"b\\c\n"`)
	if toks[2].Type != NUMBER || toks[2].Literal != "10" {
		t.Errorf("got %+v", toks[2])
	}
	strTok := toks[4]
	if strTok.Type != STRING || strTok.Literal != "a\"b\\c\n" {
		t.Errorf("got %+v", strTok)
	}
}

func TestWhitespaceAndPositionContinuity(t *testing.T) {
	toks := tokenize("a   b")
	if toks[0].Pos != 1 || toks[1].Pos != 5 {
		t.Errorf("got positions %d, %d, want 1, 5", toks[0].Pos, toks[1].Pos)
	}
}

func TestIllegalToken(t *testing.T) {
	toks := tokenize(`&`)
	if toks[0].Type != ILLEGAL {
		t.Errorf("got %v, want ILLEGAL", toks[0].Type)
	}
}
