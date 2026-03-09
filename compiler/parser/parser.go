// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg1/candor

// Package parser produces a typed AST from the Candor Core grammar.
// Input is a token slice from the lexer. Output is a *File AST root.
//
// Grammar strategy: recursive descent.
// Expression parsing uses a standard precedence ladder (no Pratt table)
// so the grammar and the code stay in 1:1 correspondence.
//
// Precedence levels (lowest → highest):
//
//	or
//	and
//	== != < > <= >=
//	+ -
//	* / %
//	unary:   ! not - &
//	postfix: () [] . must{}
package parser

import (
	"fmt"

	"github.com/scottcorleyg1/candor/compiler/lexer"
)

// Error is a parse error with source position.
type Error struct {
	Tok lexer.Token
	Msg string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.Tok.File, e.Tok.Line, e.Tok.Col, e.Msg)
}

// Parse converts a token slice (from lexer.Tokenize) into a *File AST.
// The token slice must include a terminal TokEOF token.
func Parse(name string, tokens []lexer.Token) (*File, error) {
	p := &parser{tokens: tokens}
	return p.parseFile(name)
}

// ── parser state ─────────────────────────────────────────────────────────────

type parser struct {
	tokens []lexer.Token
	pos    int
}

func (p *parser) peek() lexer.Token {
	if p.pos >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1] // EOF sentinel
	}
	return p.tokens[p.pos]
}

func (p *parser) peekAt(offset int) lexer.Token {
	idx := p.pos + offset
	if idx >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[idx]
}

func (p *parser) peekType() lexer.TokenType {
	return p.peek().Type
}

func (p *parser) advance() lexer.Token {
	t := p.peek()
	if t.Type != lexer.TokEOF {
		p.pos++
	}
	return t
}

func (p *parser) check(typ lexer.TokenType) bool {
	return p.peekType() == typ
}

func (p *parser) match(types ...lexer.TokenType) bool {
	for _, typ := range types {
		if p.check(typ) {
			return true
		}
	}
	return false
}

func (p *parser) expect(typ lexer.TokenType) (lexer.Token, error) {
	if p.check(typ) {
		return p.advance(), nil
	}
	t := p.peek()
	return t, &Error{
		Tok: t,
		Msg: fmt.Sprintf("expected %v, got %v %q", typ, t.Type, t.Lexeme),
	}
}

func (p *parser) errorf(t lexer.Token, format string, args ...any) error {
	return &Error{Tok: t, Msg: fmt.Sprintf(format, args...)}
}

// ── File ─────────────────────────────────────────────────────────────────────

func (p *parser) parseFile(name string) (*File, error) {
	file := &File{Name: name}
	for !p.check(lexer.TokEOF) {
		// Skip file-scope directives (#use effects, #intent "...", etc.)
		// Consume the directive word and then any tokens that follow on the
		// same logical "directive line" — i.e. until we hit something that
		// starts a new declaration, another directive, or EOF.
		if p.check(lexer.TokDirective) {
			p.advance()
			for !p.check(lexer.TokEOF) &&
				!p.check(lexer.TokDirective) &&
				!p.check(lexer.TokFn) &&
				!p.check(lexer.TokStruct) {
				p.advance()
			}
			continue
		}
		decl, err := p.parseDecl()
		if err != nil {
			return file, err
		}
		file.Decls = append(file.Decls, decl)
	}
	return file, nil
}

// ── Declarations ─────────────────────────────────────────────────────────────

func (p *parser) parseDecl() (Decl, error) {
	switch p.peekType() {
	case lexer.TokFn:
		return p.parseFnDecl()
	case lexer.TokStruct:
		return p.parseStructDecl()
	default:
		t := p.peek()
		return nil, p.errorf(t, "expected declaration (fn or struct), got %v %q", t.Type, t.Lexeme)
	}
}

func (p *parser) parseFnDecl() (*FnDecl, error) {
	fnTok := p.advance() // consume 'fn'

	nameTok, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokLParen); err != nil {
		return nil, err
	}
	params, err := p.parseParams()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokRParen); err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokArrow); err != nil {
		return nil, err
	}
	retType, err := p.parseType()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &FnDecl{
		FnTok:   fnTok,
		Name:    nameTok,
		Params:  params,
		RetType: retType,
		Body:    body,
	}, nil
}

func (p *parser) parseParams() ([]Param, error) {
	var params []Param
	for !p.check(lexer.TokRParen) && !p.check(lexer.TokEOF) {
		if len(params) > 0 {
			if _, err := p.expect(lexer.TokComma); err != nil {
				return nil, err
			}
		}
		if p.check(lexer.TokRParen) {
			break // trailing comma
		}
		nameTok, err := p.expect(lexer.TokIdent)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.TokColon); err != nil {
			return nil, err
		}
		typ, err := p.parseType()
		if err != nil {
			return nil, err
		}
		params = append(params, Param{Name: nameTok, Type: typ})
	}
	return params, nil
}

func (p *parser) parseStructDecl() (*StructDecl, error) {
	structTok := p.advance() // consume 'struct'

	nameTok, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokLBrace); err != nil {
		return nil, err
	}
	var fields []Field
	for !p.check(lexer.TokRBrace) && !p.check(lexer.TokEOF) {
		fieldName, err := p.expect(lexer.TokIdent)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.TokColon); err != nil {
			return nil, err
		}
		typ, err := p.parseType()
		if err != nil {
			return nil, err
		}
		fields = append(fields, Field{Name: fieldName, Type: typ})
		if p.check(lexer.TokComma) {
			p.advance()
		}
	}
	if _, err := p.expect(lexer.TokRBrace); err != nil {
		return nil, err
	}
	return &StructDecl{
		StructTok: structTok,
		Name:      nameTok,
		Fields:    fields,
	}, nil
}

// ── Statements ───────────────────────────────────────────────────────────────

func (p *parser) parseBlock() (*BlockStmt, error) {
	lbrace, err := p.expect(lexer.TokLBrace)
	if err != nil {
		return nil, err
	}
	block := &BlockStmt{LBrace: lbrace}
	for !p.check(lexer.TokRBrace) && !p.check(lexer.TokEOF) {
		stmt, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		block.Stmts = append(block.Stmts, stmt)
	}
	if _, err := p.expect(lexer.TokRBrace); err != nil {
		return nil, err
	}
	return block, nil
}

func (p *parser) parseStmt() (Stmt, error) {
	switch p.peekType() {
	case lexer.TokLet:
		return p.parseLetStmt()
	case lexer.TokReturn:
		return p.parseReturnStmt()
	case lexer.TokIf:
		return p.parseIfStmt()
	case lexer.TokLoop:
		return p.parseLoopStmt()
	case lexer.TokBreak:
		return &BreakStmt{BreakTok: p.advance()}, nil
	default:
		return p.parseExprOrAssignStmt()
	}
}

func (p *parser) parseLetStmt() (*LetStmt, error) {
	letTok := p.advance() // consume 'let'
	mut := false
	if p.check(lexer.TokMut) {
		p.advance()
		mut = true
	}

	nameTok, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	var typeAnn TypeExpr
	if p.check(lexer.TokColon) {
		p.advance()
		typeAnn, err = p.parseType()
		if err != nil {
			return nil, err
		}
	}
	if _, err := p.expect(lexer.TokEq); err != nil {
		return nil, err
	}
	value, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &LetStmt{
		LetTok:  letTok,
		Mut:     mut,
		Name:    nameTok,
		TypeAnn: typeAnn,
		Value:   value,
	}, nil
}

func (p *parser) parseAssignStmt() (*AssignStmt, error) {
	name := p.advance() // ident
	eq := p.advance()   // =
	value, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &AssignStmt{Name: name, Eq: eq, Value: value}, nil
}

// parseExprOrAssignStmt parses an expression and, if followed by '=',
// turns it into an assignment statement (variable or field).
func (p *parser) parseExprOrAssignStmt() (Stmt, error) {
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if !p.check(lexer.TokEq) {
		return &ExprStmt{X: expr}, nil
	}
	eq := p.advance() // consume '='
	value, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	switch t := expr.(type) {
	case *IdentExpr:
		return &AssignStmt{Name: t.Tok, Eq: eq, Value: value}, nil
	case *FieldExpr:
		return &FieldAssignStmt{Target: t, Eq: eq, Value: value}, nil
	default:
		return nil, p.errorf(eq, "invalid assignment target")
	}
}

func (p *parser) parseReturnStmt() (*ReturnStmt, error) {
	retTok := p.advance() // consume 'return'

	// No value: bare return (or next token closes the block)
	var value Expr
	if !p.check(lexer.TokRBrace) && !p.check(lexer.TokEOF) {
		var err error
		value, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	}
	return &ReturnStmt{ReturnTok: retTok, Value: value}, nil
}

func (p *parser) parseIfStmt() (*IfStmt, error) {
	ifTok := p.advance() // consume 'if'

	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	then, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	var elseBody Stmt
	if p.check(lexer.TokElse) {
		p.advance()
		if p.check(lexer.TokIf) {
			elseBody, err = p.parseIfStmt()
		} else {
			elseBody, err = p.parseBlock()
		}
		if err != nil {
			return nil, err
		}
	}
	return &IfStmt{IfTok: ifTok, Cond: cond, Then: then, Else: elseBody}, nil
}

func (p *parser) parseLoopStmt() (*LoopStmt, error) {
	loopTok := p.advance()
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &LoopStmt{LoopTok: loopTok, Body: body}, nil
}

func (p *parser) parseExprStmt() (Stmt, error) {
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ExprStmt{X: expr}, nil
}

// ── Expressions ──────────────────────────────────────────────────────────────

func (p *parser) parseExpr() (Expr, error) { return p.parseOrExpr() }

func (p *parser) parseOrExpr() (Expr, error) {
	left, err := p.parseAndExpr()
	if err != nil {
		return nil, err
	}
	for p.check(lexer.TokOr) {
		op := p.advance()
		right, err := p.parseAndExpr()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Left: left, Op: op, Right: right}
	}
	return left, nil
}

func (p *parser) parseAndExpr() (Expr, error) {
	left, err := p.parseCmpExpr()
	if err != nil {
		return nil, err
	}
	for p.check(lexer.TokAnd) {
		op := p.advance()
		right, err := p.parseCmpExpr()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Left: left, Op: op, Right: right}
	}
	return left, nil
}

func (p *parser) parseCmpExpr() (Expr, error) {
	left, err := p.parseAddExpr()
	if err != nil {
		return nil, err
	}
	for p.match(lexer.TokEqEq, lexer.TokBangEq, lexer.TokLt, lexer.TokGt, lexer.TokLtEq, lexer.TokGtEq) {
		op := p.advance()
		right, err := p.parseAddExpr()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Left: left, Op: op, Right: right}
	}
	return left, nil
}

func (p *parser) parseAddExpr() (Expr, error) {
	left, err := p.parseMulExpr()
	if err != nil {
		return nil, err
	}
	for p.match(lexer.TokPlus, lexer.TokMinus) {
		op := p.advance()
		right, err := p.parseMulExpr()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Left: left, Op: op, Right: right}
	}
	return left, nil
}

func (p *parser) parseMulExpr() (Expr, error) {
	left, err := p.parseUnaryExpr()
	if err != nil {
		return nil, err
	}
	for p.match(lexer.TokStar, lexer.TokSlash, lexer.TokPercent) {
		op := p.advance()
		right, err := p.parseUnaryExpr()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Left: left, Op: op, Right: right}
	}
	return left, nil
}

func (p *parser) parseUnaryExpr() (Expr, error) {
	if p.match(lexer.TokBang, lexer.TokNot, lexer.TokMinus) {
		op := p.advance()
		operand, err := p.parseUnaryExpr()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: op, Operand: operand}, nil
	}
	return p.parsePostfixExpr()
}

func (p *parser) parsePostfixExpr() (Expr, error) {
	expr, err := p.parsePrimaryExpr()
	if err != nil {
		return nil, err
	}
	for {
		switch p.peekType() {
		case lexer.TokLParen:
			lp := p.advance()
			args, err := p.parseArgs()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.TokRParen); err != nil {
				return nil, err
			}
			expr = &CallExpr{Fn: expr, LParen: lp, Args: args}

		case lexer.TokDot:
			dot := p.advance()
			field, err := p.expect(lexer.TokIdent)
			if err != nil {
				return nil, err
			}
			expr = &FieldExpr{Receiver: expr, Dot: dot, Field: field}

		case lexer.TokLBracket:
			lb := p.advance()
			index, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.TokRBracket); err != nil {
				return nil, err
			}
			expr = &IndexExpr{Collection: expr, LBracket: lb, Index: index}

		case lexer.TokMust:
			mustTok := p.advance()
			if _, err := p.expect(lexer.TokLBrace); err != nil {
				return nil, err
			}
			arms, err := p.parseMustArms()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.TokRBrace); err != nil {
				return nil, err
			}
			expr = &MustExpr{X: expr, MustTok: mustTok, Arms: arms}

		default:
			return expr, nil
		}
	}
}

func (p *parser) parseArgs() ([]Expr, error) {
	var args []Expr
	for !p.check(lexer.TokRParen) && !p.check(lexer.TokEOF) {
		if len(args) > 0 {
			if _, err := p.expect(lexer.TokComma); err != nil {
				return nil, err
			}
		}
		if p.check(lexer.TokRParen) {
			break // trailing comma
		}
		arg, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	return args, nil
}

func (p *parser) parseMustArms() ([]MustArm, error) {
	var arms []MustArm
	for !p.check(lexer.TokRBrace) && !p.check(lexer.TokEOF) {
		pattern, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		arrow, err := p.expect(lexer.TokFatArrow)
		if err != nil {
			return nil, err
		}
		body, err := p.parseMustArmBody()
		if err != nil {
			return nil, err
		}
		arms = append(arms, MustArm{Pattern: pattern, Arrow: arrow, Body: body})
	}
	return arms, nil
}

// parseMustArmBody handles both `=> expr` and `=> return expr` arm bodies.
func (p *parser) parseMustArmBody() (Expr, error) {
	if p.check(lexer.TokReturn) {
		retTok := p.advance()
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &ReturnExpr{ReturnTok: retTok, Value: val}, nil
	}
	return p.parseExpr()
}

func (p *parser) parseMatchExpr() (*MatchExpr, error) {
	matchTok := p.advance() // consume 'match'
	x, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokLBrace); err != nil {
		return nil, err
	}
	arms, err := p.parseMustArms()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokRBrace); err != nil {
		return nil, err
	}
	return &MatchExpr{MatchTok: matchTok, X: x, Arms: arms}, nil
}

func (p *parser) parsePrimaryExpr() (Expr, error) {
	t := p.peek()
	switch t.Type {
	case lexer.TokInt:
		p.advance()
		return &IntLitExpr{Tok: t}, nil
	case lexer.TokFloat:
		p.advance()
		return &FloatLitExpr{Tok: t}, nil
	case lexer.TokString:
		p.advance()
		return &StringLitExpr{Tok: t}, nil
	case lexer.TokTrue, lexer.TokFalse:
		p.advance()
		return &BoolLitExpr{Tok: t}, nil
	case lexer.TokIdent:
		p.advance()
		return &IdentExpr{Tok: t}, nil
	// These keywords also appear as expression values.
	case lexer.TokSome, lexer.TokNone, lexer.TokOk, lexer.TokErr, lexer.TokMove:
		p.advance()
		return &IdentExpr{Tok: t}, nil
	case lexer.TokMatch:
		return p.parseMatchExpr()
	case lexer.TokLParen:
		p.advance()
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.TokRParen); err != nil {
			return nil, err
		}
		return expr, nil
	case lexer.TokAmp:
		// Reference: &expr
		amp := p.advance()
		operand, err := p.parseUnaryExpr()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: amp, Operand: operand}, nil
	default:
		return nil, p.errorf(t, "expected expression, got %v %q", t.Type, t.Lexeme)
	}
}

// ── Types ─────────────────────────────────────────────────────────────────────

func (p *parser) parseType() (TypeExpr, error) {
	t := p.peek()
	switch t.Type {
	case lexer.TokIdent:
		name := p.advance()
		if p.check(lexer.TokLt) {
			return p.parseGenericType(name)
		}
		return &NamedType{Name: name}, nil
	case lexer.TokFn:
		return p.parseFnType()
	default:
		return nil, p.errorf(t, "expected type, got %v %q", t.Type, t.Lexeme)
	}
}

func (p *parser) parseGenericType(name lexer.Token) (*GenericType, error) {
	p.advance() // consume '<'
	var params []TypeExpr
	for !p.check(lexer.TokGt) && !p.check(lexer.TokEOF) {
		if len(params) > 0 {
			if _, err := p.expect(lexer.TokComma); err != nil {
				return nil, err
			}
		}
		if p.check(lexer.TokGt) {
			break
		}
		param, err := p.parseType()
		if err != nil {
			return nil, err
		}
		params = append(params, param)
	}
	if _, err := p.expect(lexer.TokGt); err != nil {
		return nil, err
	}
	return &GenericType{Name: name, Params: params}, nil
}

func (p *parser) parseFnType() (*FnType, error) {
	fnTok := p.advance() // consume 'fn'
	if _, err := p.expect(lexer.TokLParen); err != nil {
		return nil, err
	}
	var params []TypeExpr
	for !p.check(lexer.TokRParen) && !p.check(lexer.TokEOF) {
		if len(params) > 0 {
			if _, err := p.expect(lexer.TokComma); err != nil {
				return nil, err
			}
		}
		if p.check(lexer.TokRParen) {
			break
		}
		param, err := p.parseType()
		if err != nil {
			return nil, err
		}
		params = append(params, param)
	}
	if _, err := p.expect(lexer.TokRParen); err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokArrow); err != nil {
		return nil, err
	}
	retType, err := p.parseType()
	if err != nil {
		return nil, err
	}
	return &FnType{FnTok: fnTok, Params: params, RetType: retType}, nil
}
