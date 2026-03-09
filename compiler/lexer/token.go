// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg1/candor

package lexer

import "fmt"

// TokenType identifies the syntactic category of a token.
type TokenType int

const (
	// --- Literals ---
	TokInt    TokenType = iota // 42  0xFF  0b1010  0o77
	TokFloat                   // 3.14  1.0e-3
	TokString                  // "hello"

	// --- Identifier ---
	TokIdent // any non-keyword name

	// --- Keywords ---
	TokFn
	TokLet
	TokReturn
	TokIf
	TokElse
	TokMatch
	TokLoop
	TokBreak
	TokFor
	TokStruct
	TokExtern
	TokPure
	TokCap
	TokMust
	TokMove
	TokSome
	TokNone
	TokOk
	TokErr
	TokTrue
	TokFalse
	TokAnd
	TokOr
	TokNot
	TokForall
	TokExists
	TokOld
	TokIn
	TokEffects
	TokRequires
	TokEnsures
	TokInvariant
	TokAssert
	TokModule
	TokUse
	TokMut

	// --- Punctuation & Operators ---
	TokLParen    // (
	TokRParen    // )
	TokLBrace    // {
	TokRBrace    // }
	TokLBracket  // [
	TokRBracket  // ]
	TokColon     // :
	TokComma     // ,
	TokDot       // .
	TokDotDot    // ..
	TokSemicolon // ;
	TokAmp       // &
	TokAt        // @

	// Arithmetic
	TokPlus    // +
	TokMinus   // -
	TokStar    // *
	TokSlash   // /
	TokPercent // %

	// Assignment & comparison
	TokEq      // =
	TokEqEq    // ==
	TokBangEq  // !=
	TokLt      // <
	TokGt      // >
	TokLtEq    // <=
	TokGtEq    // >=
	TokBang    // !
	TokUScore  // _

	// Arrows
	TokArrow    // ->
	TokFatArrow // =>

	// --- Directives (#word) ---
	TokDirective // the word after #: "use", "intent", "import_c_header", etc.

	// --- End of file ---
	TokEOF
)

// keywords maps source text to its keyword TokenType.
var keywords = map[string]TokenType{
	"fn":        TokFn,
	"let":       TokLet,
	"return":    TokReturn,
	"if":        TokIf,
	"else":      TokElse,
	"match":     TokMatch,
	"loop":      TokLoop,
	"break":     TokBreak,
	"for":       TokFor,
	"struct":    TokStruct,
	"extern":    TokExtern,
	"pure":      TokPure,
	"cap":       TokCap,
	"must":      TokMust,
	"move":      TokMove,
	"some":      TokSome,
	"none":      TokNone,
	"ok":        TokOk,
	"err":       TokErr,
	"true":      TokTrue,
	"false":     TokFalse,
	"and":       TokAnd,
	"or":        TokOr,
	"not":       TokNot,
	"forall":    TokForall,
	"exists":    TokExists,
	"old":       TokOld,
	"in":        TokIn,
	"effects":   TokEffects,
	"requires":  TokRequires,
	"ensures":   TokEnsures,
	"invariant": TokInvariant,
	"assert":    TokAssert,
	"module":    TokModule,
	"use":       TokUse,
	"mut":       TokMut,
}

// Token is a single lexical unit from a .cnd source file.
type Token struct {
	Type   TokenType
	Lexeme string // raw source text
	File   string // source file name
	Line   int    // 1-based line number
	Col    int    // 1-based column number
}

func (t Token) String() string {
	return fmt.Sprintf("%s:%d:%d %s(%q)", t.File, t.Line, t.Col, t.Type, t.Lexeme)
}

// names for pretty-printing token types in errors and tests.
var tokenNames = map[TokenType]string{
	TokInt:       "Int",
	TokFloat:     "Float",
	TokString:    "String",
	TokIdent:     "Ident",
	TokFn:        "fn",
	TokLet:       "let",
	TokReturn:    "return",
	TokIf:        "if",
	TokElse:      "else",
	TokMatch:     "match",
	TokLoop:      "loop",
	TokBreak:     "break",
	TokFor:       "for",
	TokStruct:    "struct",
	TokExtern:    "extern",
	TokPure:      "pure",
	TokCap:       "cap",
	TokMust:      "must",
	TokMove:      "move",
	TokSome:      "some",
	TokNone:      "none",
	TokOk:        "ok",
	TokErr:       "err",
	TokTrue:      "true",
	TokFalse:     "false",
	TokAnd:       "and",
	TokOr:        "or",
	TokNot:       "not",
	TokForall:    "forall",
	TokExists:    "exists",
	TokOld:       "old",
	TokIn:        "in",
	TokEffects:   "effects",
	TokRequires:  "requires",
	TokEnsures:   "ensures",
	TokInvariant: "invariant",
	TokAssert:    "assert",
	TokModule:    "module",
	TokUse:       "use",
	TokMut:       "mut",
	TokLParen:    "(",
	TokRParen:    ")",
	TokLBrace:    "{",
	TokRBrace:    "}",
	TokLBracket:  "[",
	TokRBracket:  "]",
	TokColon:     ":",
	TokComma:     ",",
	TokDot:       ".",
	TokDotDot:    "..",
	TokSemicolon: ";",
	TokAmp:       "&",
	TokAt:        "@",
	TokPlus:      "+",
	TokMinus:     "-",
	TokStar:      "*",
	TokSlash:     "/",
	TokPercent:   "%",
	TokEq:        "=",
	TokEqEq:      "==",
	TokBangEq:    "!=",
	TokLt:        "<",
	TokGt:        ">",
	TokLtEq:      "<=",
	TokGtEq:      ">=",
	TokBang:      "!",
	TokUScore:    "_",
	TokArrow:     "->",
	TokFatArrow:  "=>",
	TokDirective: "Directive",
	TokEOF:       "EOF",
}

func (t TokenType) String() string {
	if s, ok := tokenNames[t]; ok {
		return s
	}
	return fmt.Sprintf("TokenType(%d)", int(t))
}
