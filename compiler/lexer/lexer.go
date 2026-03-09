// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg1/candor

// Package lexer tokenizes .cnd Candor source files into a flat token stream.
//
// Comment syntax: ## begins a line comment running to end of line.
// There are no block comments — every commented line is independently visible.
package lexer

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Error is a lexer diagnostic with source position.
type Error struct {
	File string
	Line int
	Col  int
	Msg  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Col, e.Msg)
}

// Tokenize scans src (the full contents of file) and returns all tokens
// including a final TokEOF. It returns the first lexical error encountered,
// if any, along with any tokens successfully scanned before the error.
func Tokenize(file, src string) ([]Token, error) {
	l := &lexer{
		file: file,
		src:  src,
		line: 1,
		col:  1,
	}
	return l.run()
}

// lexer holds scanning state.
type lexer struct {
	file   string
	src    string
	pos    int // byte offset of next character to read
	line   int // current line (1-based)
	col    int // current column (1-based)
	tokens []Token
}

// --- Low-level rune operations ---

func (l *lexer) done() bool {
	return l.pos >= len(l.src)
}

func (l *lexer) peek() rune {
	if l.done() {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.src[l.pos:])
	return r
}

func (l *lexer) peekAt(offset int) rune {
	p := l.pos
	for i := 0; i < offset; i++ {
		if p >= len(l.src) {
			return 0
		}
		_, sz := utf8.DecodeRuneInString(l.src[p:])
		p += sz
	}
	if p >= len(l.src) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.src[p:])
	return r
}

func (l *lexer) advance() rune {
	r, sz := utf8.DecodeRuneInString(l.src[l.pos:])
	l.pos += sz
	if r == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return r
}

func (l *lexer) emit(typ TokenType, lexeme string, line, col int) {
	l.tokens = append(l.tokens, Token{
		Type:   typ,
		Lexeme: lexeme,
		File:   l.file,
		Line:   line,
		Col:    col,
	})
}

func (l *lexer) errorf(line, col int, format string, args ...any) error {
	return &Error{File: l.file, Line: line, Col: col, Msg: fmt.Sprintf(format, args...)}
}

// --- Main scan loop ---

func (l *lexer) run() ([]Token, error) {
	for !l.done() {
		if err := l.scanOne(); err != nil {
			return l.tokens, err
		}
	}
	l.emit(TokEOF, "", l.line, l.col)
	return l.tokens, nil
}

func (l *lexer) scanOne() error {
	r := l.peek()

	// Whitespace
	if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
		l.advance()
		return nil
	}

	// ## line comment — consume to end of line, emit nothing
	if r == '#' && l.peekAt(1) == '#' {
		for !l.done() && l.peek() != '\n' {
			l.advance()
		}
		return nil
	}

	// # directive — #use, #intent, #import_c_header, etc.
	if r == '#' {
		return l.scanDirective()
	}

	line, col := l.line, l.col

	switch r {
	case '(':
		l.advance()
		l.emit(TokLParen, "(", line, col)
	case ')':
		l.advance()
		l.emit(TokRParen, ")", line, col)
	case '{':
		l.advance()
		l.emit(TokLBrace, "{", line, col)
	case '}':
		l.advance()
		l.emit(TokRBrace, "}", line, col)
	case '[':
		l.advance()
		l.emit(TokLBracket, "[", line, col)
	case ']':
		l.advance()
		l.emit(TokRBracket, "]", line, col)
	case ',':
		l.advance()
		l.emit(TokComma, ",", line, col)
	case ';':
		l.advance()
		l.emit(TokSemicolon, ";", line, col)
	case '&':
		l.advance()
		l.emit(TokAmp, "&", line, col)
	case '@':
		l.advance()
		l.emit(TokAt, "@", line, col)
	case '+':
		l.advance()
		l.emit(TokPlus, "+", line, col)
	case '*':
		l.advance()
		l.emit(TokStar, "*", line, col)
	case '/':
		l.advance()
		l.emit(TokSlash, "/", line, col)
	case '%':
		l.advance()
		l.emit(TokPercent, "%", line, col)
	case ':':
		l.advance()
		if l.peek() == ':' {
			l.advance()
			l.emit(TokColonColon, "::", line, col)
		} else {
			l.emit(TokColon, ":", line, col)
		}
	case '_':
		// Bare underscore is the wildcard pattern; starts an identifier if
		// followed by an alphanumeric character.
		if next := l.peekAt(1); unicode.IsLetter(next) || unicode.IsDigit(next) {
			return l.scanIdent()
		}
		l.advance()
		l.emit(TokUScore, "_", line, col)
	case '.':
		l.advance()
		if l.peek() == '.' {
			l.advance()
			l.emit(TokDotDot, "..", line, col)
		} else {
			l.emit(TokDot, ".", line, col)
		}
	case '-':
		l.advance()
		if l.peek() == '>' {
			l.advance()
			l.emit(TokArrow, "->", line, col)
		} else {
			l.emit(TokMinus, "-", line, col)
		}
	case '=':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			l.emit(TokEqEq, "==", line, col)
		} else if l.peek() == '>' {
			l.advance()
			l.emit(TokFatArrow, "=>", line, col)
		} else {
			l.emit(TokEq, "=", line, col)
		}
	case '!':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			l.emit(TokBangEq, "!=", line, col)
		} else {
			l.emit(TokBang, "!", line, col)
		}
	case '<':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			l.emit(TokLtEq, "<=", line, col)
		} else {
			l.emit(TokLt, "<", line, col)
		}
	case '>':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			l.emit(TokGtEq, ">=", line, col)
		} else {
			l.emit(TokGt, ">", line, col)
		}
	case '"':
		return l.scanString()
	default:
		if unicode.IsDigit(r) {
			return l.scanNumber()
		}
		if unicode.IsLetter(r) {
			return l.scanIdent()
		}
		l.advance()
		return l.errorf(line, col, "unexpected character %q", r)
	}
	return nil
}

// --- Identifiers and keywords ---

func (l *lexer) scanIdent() error {
	line, col := l.line, l.col
	var b strings.Builder
	for !l.done() {
		r := l.peek()
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(l.advance())
		} else {
			break
		}
	}
	word := b.String()
	if typ, ok := keywords[word]; ok {
		l.emit(typ, word, line, col)
	} else {
		l.emit(TokIdent, word, line, col)
	}
	return nil
}

// --- Directives: #word ---

func (l *lexer) scanDirective() error {
	line, col := l.line, l.col
	l.advance() // consume '#'

	if l.done() || (!unicode.IsLetter(l.peek()) && l.peek() != '_') {
		return l.errorf(line, col, "expected directive name after '#'")
	}

	var b strings.Builder
	for !l.done() {
		r := l.peek()
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(l.advance())
		} else {
			break
		}
	}
	l.emit(TokDirective, b.String(), line, col)
	return nil
}

// --- Number literals ---

func (l *lexer) scanNumber() error {
	line, col := l.line, l.col
	var b strings.Builder
	isFloat := false

	first := l.peek()
	b.WriteRune(l.advance())

	// Detect base prefixes: 0x, 0b, 0o
	if first == '0' && !l.done() {
		switch l.peek() {
		case 'x', 'X':
			b.WriteRune(l.advance())
			for !l.done() && isHexDigit(l.peek()) {
				b.WriteRune(l.advance())
			}
			l.emit(TokInt, b.String(), line, col)
			return nil
		case 'b', 'B':
			b.WriteRune(l.advance())
			for !l.done() && (l.peek() == '0' || l.peek() == '1') {
				b.WriteRune(l.advance())
			}
			l.emit(TokInt, b.String(), line, col)
			return nil
		case 'o', 'O':
			b.WriteRune(l.advance())
			for !l.done() && l.peek() >= '0' && l.peek() <= '7' {
				b.WriteRune(l.advance())
			}
			l.emit(TokInt, b.String(), line, col)
			return nil
		}
	}

	// Decimal digits
	for !l.done() && unicode.IsDigit(l.peek()) {
		b.WriteRune(l.advance())
	}

	// Fractional part
	if !l.done() && l.peek() == '.' && l.peekAt(1) != '.' {
		isFloat = true
		b.WriteRune(l.advance()) // '.'
		for !l.done() && unicode.IsDigit(l.peek()) {
			b.WriteRune(l.advance())
		}
	}

	// Exponent
	if !l.done() && (l.peek() == 'e' || l.peek() == 'E') {
		isFloat = true
		b.WriteRune(l.advance())
		if !l.done() && (l.peek() == '+' || l.peek() == '-') {
			b.WriteRune(l.advance())
		}
		for !l.done() && unicode.IsDigit(l.peek()) {
			b.WriteRune(l.advance())
		}
	}

	if isFloat {
		l.emit(TokFloat, b.String(), line, col)
	} else {
		l.emit(TokInt, b.String(), line, col)
	}
	return nil
}

// --- String literals ---

func (l *lexer) scanString() error {
	line, col := l.line, l.col
	l.advance() // opening "
	var b strings.Builder
	b.WriteByte('"')

	for {
		if l.done() {
			return l.errorf(line, col, "unterminated string literal")
		}
		r := l.peek()
		if r == '\n' {
			return l.errorf(l.line, l.col, "unterminated string literal (newline in string)")
		}
		if r == '"' {
			b.WriteRune(l.advance())
			break
		}
		if r == '\\' {
			b.WriteRune(l.advance()) // backslash
			if l.done() {
				return l.errorf(line, col, "unterminated escape sequence")
			}
			b.WriteRune(l.advance()) // escaped char
			continue
		}
		b.WriteRune(l.advance())
	}

	l.emit(TokString, b.String(), line, col)
	return nil
}

// --- Helpers ---

func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}
