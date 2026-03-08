// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg1/candor

package lexer

import (
	"testing"
)

// tok is a compact (type, lexeme) pair for test assertions.
type tok struct {
	typ    TokenType
	lexeme string
}

func tokenize(src string) ([]tok, error) {
	tokens, err := Tokenize("<test>", src)
	if err != nil {
		return nil, err
	}
	out := make([]tok, len(tokens))
	for i, t := range tokens {
		out[i] = tok{t.Type, t.Lexeme}
	}
	return out, nil
}

func assertTokens(t *testing.T, src string, want []tok) {
	t.Helper()
	got, err := tokenize(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// always ends with EOF — append it to want for comparison
	want = append(want, tok{TokEOF, ""})
	if len(got) != len(want) {
		t.Fatalf("token count mismatch\ngot  %d: %v\nwant %d: %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token[%d]: got {%v %q}, want {%v %q}", i, got[i].typ, got[i].lexeme, want[i].typ, want[i].lexeme)
		}
	}
}

// --- Comments ---

func TestLineComment(t *testing.T) {
	assertTokens(t, "## this is a comment", nil)
}

func TestLineCommentMidLine(t *testing.T) {
	assertTokens(t, "fn ## comment\nlet", []tok{
		{TokFn, "fn"},
		{TokLet, "let"},
	})
}

func TestCommentDoesNotConsumeNewlineTokens(t *testing.T) {
	// The comment ends at \n; the token on the next line must still appear.
	assertTokens(t, "## first line\nfn", []tok{{TokFn, "fn"}})
}

// --- Keywords ---

func TestAllKeywords(t *testing.T) {
	cases := []struct {
		src string
		typ TokenType
	}{
		{"fn", TokFn},
		{"let", TokLet},
		{"return", TokReturn},
		{"if", TokIf},
		{"else", TokElse},
		{"match", TokMatch},
		{"loop", TokLoop},
		{"break", TokBreak},
		{"struct", TokStruct},
		{"extern", TokExtern},
		{"pure", TokPure},
		{"cap", TokCap},
		{"must", TokMust},
		{"move", TokMove},
		{"some", TokSome},
		{"none", TokNone},
		{"ok", TokOk},
		{"err", TokErr},
		{"true", TokTrue},
		{"false", TokFalse},
		{"and", TokAnd},
		{"or", TokOr},
		{"not", TokNot},
		{"forall", TokForall},
		{"exists", TokExists},
		{"old", TokOld},
		{"in", TokIn},
		{"effects", TokEffects},
		{"requires", TokRequires},
		{"ensures", TokEnsures},
		{"invariant", TokInvariant},
		{"assert", TokAssert},
		{"module", TokModule},
		{"use", TokUse},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			assertTokens(t, c.src, []tok{{c.typ, c.src}})
		})
	}
}

// --- Identifiers ---

func TestIdent(t *testing.T) {
	assertTokens(t, "foo", []tok{{TokIdent, "foo"}})
}

func TestIdentWithUnderscoreAndDigits(t *testing.T) {
	assertTokens(t, "my_var2", []tok{{TokIdent, "my_var2"}})
}

func TestKeywordPrefixIsIdent(t *testing.T) {
	// "fnfoo" is not keyword "fn" followed by "foo" — it's one identifier
	assertTokens(t, "fnfoo", []tok{{TokIdent, "fnfoo"}})
}

// --- Integer literals ---

func TestIntDecimal(t *testing.T) {
	assertTokens(t, "42", []tok{{TokInt, "42"}})
}

func TestIntHex(t *testing.T) {
	assertTokens(t, "0xFF", []tok{{TokInt, "0xFF"}})
}

func TestIntBinary(t *testing.T) {
	assertTokens(t, "0b1010", []tok{{TokInt, "0b1010"}})
}

func TestIntOctal(t *testing.T) {
	assertTokens(t, "0o77", []tok{{TokInt, "0o77"}})
}

// --- Float literals ---

func TestFloatBasic(t *testing.T) {
	assertTokens(t, "3.14", []tok{{TokFloat, "3.14"}})
}

func TestFloatExponent(t *testing.T) {
	assertTokens(t, "1.0e-3", []tok{{TokFloat, "1.0e-3"}})
}

func TestFloatExponentPositive(t *testing.T) {
	assertTokens(t, "2.5E+10", []tok{{TokFloat, "2.5E+10"}})
}

func TestDotDotNotFloat(t *testing.T) {
	// "1..5" should be Int("1") DotDot Int("5"), not a float
	assertTokens(t, "1..5", []tok{
		{TokInt, "1"},
		{TokDotDot, ".."},
		{TokInt, "5"},
	})
}

// --- String literals ---

func TestString(t *testing.T) {
	assertTokens(t, `"hello"`, []tok{{TokString, `"hello"`}})
}

func TestStringEscape(t *testing.T) {
	assertTokens(t, `"say \"hi\""`, []tok{{TokString, `"say \"hi\""`}})
}

func TestUnterminatedString(t *testing.T) {
	_, err := tokenize(`"oops`)
	if err == nil {
		t.Fatal("expected error for unterminated string")
	}
}

// --- Operators and punctuation ---

func TestArrow(t *testing.T) {
	assertTokens(t, "->", []tok{{TokArrow, "->"}})
}

func TestFatArrow(t *testing.T) {
	assertTokens(t, "=>", []tok{{TokFatArrow, "=>"}})
}

func TestEqEq(t *testing.T) {
	assertTokens(t, "==", []tok{{TokEqEq, "=="}})
}

func TestBangEq(t *testing.T) {
	assertTokens(t, "!=", []tok{{TokBangEq, "!="}})
}

func TestLtEq(t *testing.T) {
	assertTokens(t, "<=", []tok{{TokLtEq, "<="}})
}

func TestGtEq(t *testing.T) {
	assertTokens(t, ">=", []tok{{TokGtEq, ">="}})
}

func TestDotDot(t *testing.T) {
	assertTokens(t, "..", []tok{{TokDotDot, ".."}})
}

func TestUnderscore(t *testing.T) {
	assertTokens(t, "_", []tok{{TokUScore, "_"}})
}

// --- Directives ---

func TestDirectiveUse(t *testing.T) {
	assertTokens(t, "#use", []tok{{TokDirective, "use"}})
}

func TestDirectiveIntent(t *testing.T) {
	assertTokens(t, "#intent", []tok{{TokDirective, "intent"}})
}

func TestDirectiveImportCHeader(t *testing.T) {
	assertTokens(t, "#import_c_header", []tok{{TokDirective, "import_c_header"}})
}

// --- Acceptance criterion program ---
// candorc v0.0.1 must lex this without error.

func TestAcceptanceCriterionProgram(t *testing.T) {
	src := `
fn add(a: u32, b: u32) -> u32 { return a + b }

fn main() -> unit {
    let x = add(1, 2)
    return unit
}
`
	want := []tok{
		{TokFn, "fn"}, {TokIdent, "add"}, {TokLParen, "("},
		{TokIdent, "a"}, {TokColon, ":"}, {TokIdent, "u32"}, {TokComma, ","},
		{TokIdent, "b"}, {TokColon, ":"}, {TokIdent, "u32"}, {TokRParen, ")"},
		{TokArrow, "->"}, {TokIdent, "u32"},
		{TokLBrace, "{"}, {TokReturn, "return"}, {TokIdent, "a"}, {TokPlus, "+"}, {TokIdent, "b"}, {TokRBrace, "}"},

		{TokFn, "fn"}, {TokIdent, "main"}, {TokLParen, "("}, {TokRParen, ")"},
		{TokArrow, "->"}, {TokIdent, "unit"},
		{TokLBrace, "{"},
		{TokLet, "let"}, {TokIdent, "x"}, {TokEq, "="}, {TokIdent, "add"},
		{TokLParen, "("}, {TokInt, "1"}, {TokComma, ","}, {TokInt, "2"}, {TokRParen, ")"},
		{TokReturn, "return"}, {TokIdent, "unit"},
		{TokRBrace, "}"},
	}
	assertTokens(t, src, want)
}

// --- Source position tracking ---

func TestPositionTracking(t *testing.T) {
	src := "fn\nlet"
	tokens, err := Tokenize("<test>", src)
	if err != nil {
		t.Fatal(err)
	}
	if tokens[0].Line != 1 || tokens[0].Col != 1 {
		t.Errorf("fn: want line 1 col 1, got line %d col %d", tokens[0].Line, tokens[0].Col)
	}
	if tokens[1].Line != 2 || tokens[1].Col != 1 {
		t.Errorf("let: want line 2 col 1, got line %d col %d", tokens[1].Line, tokens[1].Col)
	}
}

// --- Error cases ---

func TestUnexpectedCharacter(t *testing.T) {
	_, err := tokenize("fn ` bad")
	if err == nil {
		t.Fatal("expected error for unexpected character")
	}
}
