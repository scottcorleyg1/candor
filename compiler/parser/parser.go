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
	var pendingDirectives []string
	pendingDirectiveArgs := make(map[string]string)
	for !p.check(lexer.TokEOF) {
		// Collect file-scope directives immediately before a declaration.
		// #test, #mcp_tool "desc", #intent "desc", etc.
		if p.check(lexer.TokDirective) {
			word := p.peek().Lexeme
			p.advance()
			// Capture an optional string argument on the same directive line.
			if p.check(lexer.TokString) {
				pendingDirectiveArgs[word] = unquoteStr(p.peek().Lexeme)
				p.advance()
			}
			// Consume any remaining extra tokens on the directive line.
			for !p.check(lexer.TokEOF) &&
				!p.check(lexer.TokDirective) &&
				!p.check(lexer.TokFn) &&
				!p.check(lexer.TokStruct) &&
				!p.check(lexer.TokEnum) &&
				!p.check(lexer.TokImpl) &&
				!p.check(lexer.TokTrait) &&
				!p.check(lexer.TokCap) {
				p.advance()
			}
			pendingDirectives = append(pendingDirectives, word)
			continue
		}
		decl, err := p.parseDecl()
		if err != nil {
			return file, err
		}
		// Attach collected directives to the declaration.
		if len(pendingDirectives) > 0 {
			switch d := decl.(type) {
			case *FnDecl:
				d.Directives = append(d.Directives, pendingDirectives...)
				if d.DirectiveArgs == nil {
					d.DirectiveArgs = make(map[string]string)
				}
				for k, v := range pendingDirectiveArgs {
					d.DirectiveArgs[k] = v
				}
			case *StructDecl:
				d.Directives = append(d.Directives, pendingDirectives...)
			}
			pendingDirectives = nil
			pendingDirectiveArgs = make(map[string]string)
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
	case lexer.TokEnum:
		return p.parseEnumDecl()
	case lexer.TokModule:
		return p.parseModuleDecl()
	case lexer.TokUse:
		return p.parseUseDecl()
	case lexer.TokExtern:
		return p.parseExternFnDecl()
	case lexer.TokConst:
		return p.parseConstDecl()
	case lexer.TokImpl:
		return p.parseImplDecl()
	case lexer.TokTrait:
		return p.parseTraitDecl()
	case lexer.TokCap:
		return p.parseCapabilityDecl()
	default:
		t := p.peek()
		return nil, p.errorf(t, "expected declaration (fn, struct, enum, module, use, extern, const, impl, trait, or cap), got %v %q", t.Type, t.Lexeme)
	}
}

func (p *parser) parseCapabilityDecl() (*CapabilityDecl, error) {
	capTok := p.advance() // consume 'cap'
	name, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	return &CapabilityDecl{CapTok: capTok, Name: name}, nil
}

func (p *parser) parseExternFnDecl() (*ExternFnDecl, error) {
	externTok := p.advance() // consume 'extern'
	if _, err := p.expect(lexer.TokFn); err != nil {
		return nil, err
	}
	name, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokLParen); err != nil {
		return nil, err
	}
	var params []Param
	for !p.check(lexer.TokRParen) && !p.check(lexer.TokEOF) {
		if len(params) > 0 {
			if _, err := p.expect(lexer.TokComma); err != nil {
				return nil, err
			}
		}
		if p.check(lexer.TokRParen) {
			break
		}
		pName, err := p.expect(lexer.TokIdent)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.TokColon); err != nil {
			return nil, err
		}
		pType, err := p.parseType()
		if err != nil {
			return nil, err
		}
		params = append(params, Param{Name: pName, Type: pType})
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
	// Optional effects annotation.
	var effects *EffectsAnnotation
	if p.check(lexer.TokPure) || p.check(lexer.TokEffects) {
		effects, err = p.parseEffects()
		if err != nil {
			return nil, err
		}
	}
	return &ExternFnDecl{ExternTok: externTok, Name: name, Params: params, RetType: retType, Effects: effects}, nil
}

func (p *parser) parseModuleDecl() (*ModuleDecl, error) {
	modTok := p.advance() // consume 'module'
	name, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	return &ModuleDecl{ModuleTok: modTok, Name: name}, nil
}

func (p *parser) parseUseDecl() (*UseDecl, error) {
	useTok := p.advance() // consume 'use'
	seg, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	path := []lexer.Token{seg}
	for p.check(lexer.TokColonColon) {
		p.advance() // consume '::'
		seg, err = p.expect(lexer.TokIdent)
		if err != nil {
			return nil, err
		}
		path = append(path, seg)
	}
	return &UseDecl{UseTok: useTok, Path: path}, nil
}

func (p *parser) parseFnDecl() (*FnDecl, error) {
	fnTok := p.advance() // consume 'fn'

	nameTok, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}

	// Optional type parameter list: <T, U: Trait, ...>
	typeParams, typeBounds, err := p.parseTypeParamsWithBounds()
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
	effects, err := p.parseEffects()
	if err != nil {
		return nil, err
	}
	contracts, err := p.parseContracts()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &FnDecl{
		FnTok:      fnTok,
		Name:       nameTok,
		TypeParams: typeParams,
		TypeBounds: typeBounds,
		Params:     params,
		RetType:    retType,
		Effects:    effects,
		Contracts:  contracts,
		Body:       body,
	}, nil
}

// parseEffects parses an optional pure / effects(...) / cap(...) clause.
// Returns nil if the next token is not an effects keyword.
func (p *parser) parseEffects() (*EffectsAnnotation, error) {
	switch p.peekType() {
	case lexer.TokPure:
		p.advance()
		return &EffectsAnnotation{Kind: EffectsPure}, nil

	case lexer.TokEffects:
		tok := p.advance()
		// effects [] means pure (no effects) — syntactic sugar for `pure`
		if p.check(lexer.TokLBracket) {
			p.advance()
			if _, err := p.expect(lexer.TokRBracket); err != nil {
				return nil, err
			}
			return &EffectsAnnotation{Kind: EffectsPure}, nil
		}
		if _, err := p.expect(lexer.TokLParen); err != nil {
			return nil, err
		}
		var names []string
		for !p.check(lexer.TokRParen) && !p.check(lexer.TokEOF) {
			if len(names) > 0 {
				if _, err := p.expect(lexer.TokComma); err != nil {
					return nil, err
				}
			}
			if p.check(lexer.TokRParen) {
				break // trailing comma
			}
			name, err := p.expect(lexer.TokIdent)
			if err != nil {
				return nil, err
			}
			names = append(names, name.Lexeme)
		}
		if _, err := p.expect(lexer.TokRParen); err != nil {
			return nil, err
		}
		if len(names) == 0 {
			return nil, p.errorf(tok, "effects() requires at least one effect name; use effects [] for pure")
		}
		return &EffectsAnnotation{Kind: EffectsDecl, Names: names}, nil

	case lexer.TokCap:
		p.advance()
		if _, err := p.expect(lexer.TokLParen); err != nil {
			return nil, err
		}
		capName, err := p.expect(lexer.TokIdent)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.TokRParen); err != nil {
			return nil, err
		}
		return &EffectsAnnotation{Kind: EffectsCap, Names: []string{capName.Lexeme}}, nil
	}
	return nil, nil // no annotation
}

func (p *parser) parseContracts() ([]ContractClause, error) {
	var clauses []ContractClause
	for {
		switch p.peekType() {
		case lexer.TokRequires:
			tok := p.advance()
			expr, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			clauses = append(clauses, ContractClause{Kind: ContractRequires, Tok: tok, Expr: expr})
		case lexer.TokEnsures:
			tok := p.advance()
			expr, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			clauses = append(clauses, ContractClause{Kind: ContractEnsures, Tok: tok, Expr: expr})
		default:
			return clauses, nil
		}
	}
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

func (p *parser) parseEnumDecl() (*EnumDecl, error) {
	enumTok := p.advance() // consume 'enum'
	nameTok, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokLBrace); err != nil {
		return nil, err
	}
	var variants []EnumVariant
	for !p.check(lexer.TokRBrace) && !p.check(lexer.TokEOF) {
		varName, err := p.expect(lexer.TokIdent)
		if err != nil {
			return nil, err
		}
		var fields []TypeExpr
		if p.check(lexer.TokLParen) {
			p.advance() // consume '('
			for !p.check(lexer.TokRParen) && !p.check(lexer.TokEOF) {
				if len(fields) > 0 {
					if _, err := p.expect(lexer.TokComma); err != nil {
						return nil, err
					}
				}
				if p.check(lexer.TokRParen) {
					break
				}
				fieldType, err := p.parseType()
				if err != nil {
					return nil, err
				}
				fields = append(fields, fieldType)
			}
			if _, err := p.expect(lexer.TokRParen); err != nil {
				return nil, err
			}
		}
		variants = append(variants, EnumVariant{Name: varName, Fields: fields})
		if p.check(lexer.TokComma) {
			p.advance()
		}
	}
	if _, err := p.expect(lexer.TokRBrace); err != nil {
		return nil, err
	}
	return &EnumDecl{EnumTok: enumTok, Name: nameTok, Variants: variants}, nil
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
	case lexer.TokFor:
		return p.parseForStmt()
	case lexer.TokBreak:
		return &BreakStmt{BreakTok: p.advance()}, nil
	case lexer.TokContinue:
		return &ContinueStmt{ContinueTok: p.advance()}, nil
	case lexer.TokWhile:
		return p.parseWhileStmt()
	case lexer.TokAssert:
		assertTok := p.advance()
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &AssertStmt{AssertTok: assertTok, Expr: expr}, nil
	default:
		return p.parseExprOrAssignStmt()
	}
}

func (p *parser) parseLetStmt() (Stmt, error) {
	letTok := p.advance() // consume 'let'
	mut := false
	if p.check(lexer.TokMut) {
		p.advance()
		mut = true
	}

	// Tuple destructuring: let (a, b, ...) = expr
	if p.check(lexer.TokLParen) {
		p.advance() // consume '('
		var names []lexer.Token
		for !p.check(lexer.TokRParen) && !p.check(lexer.TokEOF) {
			if len(names) > 0 {
				if _, err := p.expect(lexer.TokComma); err != nil {
					return nil, err
				}
				if p.check(lexer.TokRParen) {
					break
				}
			}
			name, err := p.expect(lexer.TokIdent)
			if err != nil {
				return nil, err
			}
			names = append(names, name)
		}
		if _, err := p.expect(lexer.TokRParen); err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.TokEq); err != nil {
			return nil, err
		}
		value, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &TupleDestructureStmt{LetTok: letTok, Mut: mut, Names: names, Value: value}, nil
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

// compoundOpFor maps a compound-assignment token to its binary arithmetic token.
func compoundOpFor(t lexer.TokenType) (lexer.TokenType, bool) {
	switch t {
	case lexer.TokPlusEq:
		return lexer.TokPlus, true
	case lexer.TokMinusEq:
		return lexer.TokMinus, true
	case lexer.TokStarEq:
		return lexer.TokStar, true
	case lexer.TokSlashEq:
		return lexer.TokSlash, true
	case lexer.TokPercentEq:
		return lexer.TokPercent, true
	}
	return 0, false
}

// parseExprOrAssignStmt parses an expression and, if followed by '=' or a
// compound-assignment operator (+=, -=, *=, /=, %=), turns it into an
// assignment statement.  Compound assignments are desugared:
//
//	x += y  →  AssignStmt{ Name: x, Value: BinaryExpr{ x, +, y } }
func (p *parser) parseExprOrAssignStmt() (Stmt, error) {
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}

	// Check for compound assignment operators.
	if binOp, ok := compoundOpFor(p.peekType()); ok {
		opTok := p.advance() // consume += / -= / ...
		rhs, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		// Synthetic binary op token: same position as the compound-op token,
		// but with the simple arithmetic type.
		arithTok := opTok
		arithTok.Type = binOp
		arithTok.Lexeme = lexer.TokenType(binOp).String()
		switch t := expr.(type) {
		case *IdentExpr:
			value := &BinaryExpr{Left: &IdentExpr{Tok: t.Tok}, Op: arithTok, Right: rhs}
			return &AssignStmt{Name: t.Tok, Eq: opTok, Value: value}, nil
		case *FieldExpr:
			value := &BinaryExpr{Left: t, Op: arithTok, Right: rhs}
			return &FieldAssignStmt{Target: t, Eq: opTok, Value: value}, nil
		case *IndexExpr:
			value := &BinaryExpr{Left: t, Op: arithTok, Right: rhs}
			return &IndexAssignStmt{Target: t, Eq: opTok, Value: value}, nil
		default:
			return nil, p.errorf(opTok, "invalid compound-assignment target")
		}
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
	case *IndexExpr:
		return &IndexAssignStmt{Target: t, Eq: eq, Value: value}, nil
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

func (p *parser) parseForStmt() (*ForStmt, error) {
	forTok := p.advance() // consume 'for'
	varTok, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	var var2 *lexer.Token
	if p.check(lexer.TokComma) {
		p.advance() // consume ','
		v2, err := p.expect(lexer.TokIdent)
		if err != nil {
			return nil, err
		}
		var2 = &v2
	}
	inTok, err := p.expect(lexer.TokIn)
	if err != nil {
		return nil, err
	}
	coll, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ForStmt{ForTok: forTok, Var: varTok, Var2: var2, InTok: inTok, Collection: coll, Body: body}, nil
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
			// Tuple index: .0 .1 .2 ...
			if p.check(lexer.TokInt) {
				idx := p.advance()
				expr = &FieldExpr{Receiver: expr, Dot: dot, Field: idx}
				continue
			}
			field, err := p.expect(lexer.TokIdent)
			if err != nil {
				return nil, err
			}
			expr = &FieldExpr{Receiver: expr, Dot: dot, Field: field}

		case lexer.TokAs:
			asTok := p.advance()
			target, err := p.parseType()
			if err != nil {
				return nil, err
			}
			expr = &CastExpr{X: expr, AsTok: asTok, Target: target}

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

		case lexer.TokLBrace:
			// Struct literal: only when expr is a PascalCase identifier.
			ident, isIdent := expr.(*IdentExpr)
			if isIdent && len(ident.Tok.Lexeme) > 0 && ident.Tok.Lexeme[0] >= 'A' && ident.Tok.Lexeme[0] <= 'Z' {
				p.advance() // consume '{'
				fields, base, err := p.parseFieldInits()
				if err != nil {
					return nil, err
				}
				if _, err := p.expect(lexer.TokRBrace); err != nil {
					return nil, err
				}
				expr = &StructLitExpr{TypeName: ident.Tok, Base: base, Fields: fields}
				continue
			}
			return expr, nil

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

// parseFieldInits parses the body of a struct literal.
// Returns (fields, base, error) where base is non-nil when a ..expr spread is present.
func (p *parser) parseFieldInits() ([]FieldInit, Expr, error) {
	var fields []FieldInit
	var base Expr
	needComma := false
	for !p.check(lexer.TokRBrace) && !p.check(lexer.TokEOF) {
		if needComma {
			if _, err := p.expect(lexer.TokComma); err != nil {
				return nil, nil, err
			}
		}
		if p.check(lexer.TokRBrace) {
			break // trailing comma
		}
		// Spread: ..expr
		if p.check(lexer.TokDotDot) {
			p.advance() // consume '..'
			spreadExpr, err := p.parseExpr()
			if err != nil {
				return nil, nil, err
			}
			base = spreadExpr
			needComma = true
			continue
		}
		name, err := p.expect(lexer.TokIdent)
		if err != nil {
			return nil, nil, err
		}
		colon, err := p.expect(lexer.TokColon)
		if err != nil {
			return nil, nil, err
		}
		value, err := p.parseExpr()
		if err != nil {
			return nil, nil, err
		}
		fields = append(fields, FieldInit{Name: name, Colon: colon, Value: value})
		needComma = true
	}
	return fields, base, nil
}

func (p *parser) parseMustArms() ([]MustArm, error) {
	var arms []MustArm
	for !p.check(lexer.TokRBrace) && !p.check(lexer.TokEOF) {
		var pattern Expr
		if p.check(lexer.TokUScore) {
			tok := p.advance()
			pattern = &IdentExpr{Tok: tok}
		} else {
			var err error
			pattern, err = p.parseExpr()
			if err != nil {
				return nil, err
			}
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
		// Optional comma separator between arms (required when the next arm
		// pattern starts with '-' to avoid ambiguity with binary subtraction).
		if p.check(lexer.TokComma) {
			p.advance()
		}
	}
	return arms, nil
}

// parseMustArmBody handles `=> expr`, `=> return expr`, `=> break`, and `=> { stmts... }` arm bodies.
func (p *parser) parseMustArmBody() (Expr, error) {
	if p.check(lexer.TokLBrace) {
		return p.parseBlockExpr()
	}
	if p.check(lexer.TokReturn) {
		retTok := p.advance()
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &ReturnExpr{ReturnTok: retTok, Value: val}, nil
	}
	if p.check(lexer.TokBreak) {
		breakTok := p.advance()
		return &BreakExpr{BreakTok: breakTok}, nil
	}
	return p.parseExpr()
}

// parseBlockExpr parses a `{ stmts... }` block as an expression.
// Used in multi-statement match/must arm bodies.
func (p *parser) parseBlockExpr() (*BlockExpr, error) {
	lbrace, err := p.expect(lexer.TokLBrace)
	if err != nil {
		return nil, err
	}
	var stmts []Stmt
	for !p.check(lexer.TokRBrace) && !p.check(lexer.TokEOF) {
		stmt, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, stmt)
	}
	rbrace, err := p.expect(lexer.TokRBrace)
	if err != nil {
		return nil, err
	}
	return &BlockExpr{LBrace: lbrace, Stmts: stmts, RBrace: rbrace}, nil
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
	case lexer.TokIdent, lexer.TokUScore:
		p.advance()
		// Check for Enum::Variant path
		if t.Type == lexer.TokIdent && p.check(lexer.TokColonColon) {
			sep := p.advance() // consume '::'
			tail, err := p.expect(lexer.TokIdent)
			if err != nil {
				return nil, err
			}
			return &PathExpr{Head: t, Sep: sep, Tail: tail}, nil
		}
		return &IdentExpr{Tok: t}, nil
	// These keywords also appear as expression values.
	case lexer.TokSome, lexer.TokNone, lexer.TokOk, lexer.TokErr, lexer.TokMove, lexer.TokSecret, lexer.TokReveal:
		p.advance()
		return &IdentExpr{Tok: t}, nil
	case lexer.TokMatch:
		return p.parseMatchExpr()
	case lexer.TokLParen:
		lp := p.advance()
		first, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		// Tuple literal if followed by ','
		if p.check(lexer.TokComma) {
			elems := []Expr{first}
			for p.check(lexer.TokComma) {
				p.advance()
				if p.check(lexer.TokRParen) {
					break
				}
				e, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				elems = append(elems, e)
			}
			if _, err := p.expect(lexer.TokRParen); err != nil {
				return nil, err
			}
			return &TupleLitExpr{LParen: lp, Elems: elems}, nil
		}
		if _, err := p.expect(lexer.TokRParen); err != nil {
			return nil, err
		}
		return first, nil

	case lexer.TokLBracket:
		// Vec literal: [expr, expr, ...]
		lb := p.advance()
		var elems []Expr
		for !p.check(lexer.TokRBracket) && !p.check(lexer.TokEOF) {
			if len(elems) > 0 {
				if _, err := p.expect(lexer.TokComma); err != nil {
					return nil, err
				}
				if p.check(lexer.TokRBracket) {
					break
				}
			}
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			elems = append(elems, e)
		}
		rb, err := p.expect(lexer.TokRBracket)
		if err != nil {
			return nil, err
		}
		return &VecLitExpr{LBracket: lb, Elems: elems, RBracket: rb}, nil
	case lexer.TokAmp:
		// Reference: &expr
		amp := p.advance()
		operand, err := p.parseUnaryExpr()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: amp, Operand: operand}, nil
	case lexer.TokStar:
		// Deref: *expr
		star := p.advance()
		operand, err := p.parseUnaryExpr()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: star, Operand: operand}, nil
	case lexer.TokFn:
		return p.parseLambdaExpr()
	case lexer.TokSpawn:
		return p.parseSpawnExpr()
	case lexer.TokOld:
		oldTok := p.advance()
		if _, err := p.expect(lexer.TokLParen); err != nil {
			return nil, err
		}
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.TokRParen); err != nil {
			return nil, err
		}
		return &OldExpr{OldTok: oldTok, X: inner}, nil
	case lexer.TokForall:
		return p.parseForallExpr()
	case lexer.TokExists:
		return p.parseExistsExpr()
	default:
		return nil, p.errorf(t, "expected expression, got %v %q", t.Type, t.Lexeme)
	}
}

// parseQuantifierExpr parses: KEYWORD varName in collection : pred
func (p *parser) parseForallExpr() (*ForallExpr, error) {
	tok := p.advance() // consume 'forall'
	varTok, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokIn); err != nil {
		return nil, err
	}
	coll, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokColon); err != nil {
		return nil, err
	}
	pred, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ForallExpr{ForallTok: tok, Var: varTok, Collection: coll, Pred: pred}, nil
}

func (p *parser) parseExistsExpr() (*ExistsExpr, error) {
	tok := p.advance() // consume 'exists'
	varTok, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokIn); err != nil {
		return nil, err
	}
	coll, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokColon); err != nil {
		return nil, err
	}
	pred, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ExistsExpr{ExistsTok: tok, Var: varTok, Collection: coll, Pred: pred}, nil
}

// unquoteStr strips outer quotes from a directive string argument.
func unquoteStr(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func (p *parser) parseLambdaExpr() (*LambdaExpr, error) {
	fnTok := p.advance() // consume 'fn'
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
	return &LambdaExpr{FnTok: fnTok, Params: params, RetType: retType, Body: body}, nil
}

func (p *parser) parseSpawnExpr() (*SpawnExpr, error) {
	spawnTok := p.advance() // consume 'spawn'
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &SpawnExpr{SpawnTok: spawnTok, Body: body}, nil
}

// ── Types ─────────────────────────────────────────────────────────────────────

func (p *parser) parseType() (TypeExpr, error) {
	t := p.peek()
	switch t.Type {
	case lexer.TokIdent, lexer.TokSecret, lexer.TokCap:
		// `secret` and `cap` are keywords but also valid as generic type constructors
		// in type position: e.g. secret<T>, cap<Admin>.
		name := p.advance()
		if p.check(lexer.TokLt) {
			return p.parseGenericType(name)
		}
		return &NamedType{Name: name}, nil
	case lexer.TokFn:
		return p.parseFnType()
	case lexer.TokLParen:
		// Tuple type: (T, U, ...) — must have 2+ elements.
		lp := p.advance()
		first, err := p.parseType()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.TokComma); err != nil {
			return nil, err
		}
		elems := []TypeExpr{first}
		for !p.check(lexer.TokRParen) && !p.check(lexer.TokEOF) {
			if len(elems) > 1 {
				if _, err := p.expect(lexer.TokComma); err != nil {
					return nil, err
				}
				if p.check(lexer.TokRParen) {
					break
				}
			}
			te, err := p.parseType()
			if err != nil {
				return nil, err
			}
			elems = append(elems, te)
		}
		if _, err := p.expect(lexer.TokRParen); err != nil {
			return nil, err
		}
		return &TupleTypeExpr{LParen: lp, Elems: elems}, nil
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

// ── New declaration parsers ───────────────────────────────────────────────────

func (p *parser) parseWhileStmt() (*WhileStmt, error) {
	whileTok := p.advance() // consume 'while'
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &WhileStmt{WhileTok: whileTok, Cond: cond, Body: body}, nil
}

func (p *parser) parseConstDecl() (*ConstDecl, error) {
	constTok := p.advance() // consume 'const'
	name, err := p.expect(lexer.TokIdent)
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
	if _, err := p.expect(lexer.TokEq); err != nil {
		return nil, err
	}
	value, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ConstDecl{ConstTok: constTok, Name: name, Type: typ, Value: value}, nil
}

// parseTypeParamsWithBounds parses an optional <T, U: Trait, V: A+B> list.
// Returns the param name tokens and a map of name→trait-bound names.
func (p *parser) parseTypeParamsWithBounds() ([]lexer.Token, map[string][]string, error) {
	if !p.check(lexer.TokLt) {
		return nil, nil, nil
	}
	p.advance() // consume '<'
	var typeParams []lexer.Token
	bounds := make(map[string][]string)
	for {
		tp, err := p.expect(lexer.TokIdent)
		if err != nil {
			return nil, nil, err
		}
		typeParams = append(typeParams, tp)
		// Optional : TraitName (+ TraitName)*
		if p.check(lexer.TokColon) {
			p.advance()
			for {
				traitTok, err := p.expect(lexer.TokIdent)
				if err != nil {
					return nil, nil, err
				}
				bounds[tp.Lexeme] = append(bounds[tp.Lexeme], traitTok.Lexeme)
				if p.check(lexer.TokPlus) {
					p.advance()
					continue
				}
				break
			}
		}
		if p.check(lexer.TokComma) {
			p.advance()
			continue
		}
		break
	}
	if _, err := p.expect(lexer.TokGt); err != nil {
		return nil, nil, err
	}
	if len(bounds) == 0 {
		bounds = nil
	}
	return typeParams, bounds, nil
}

// parseImplMethod parses a single method inside an impl or impl-for block.
func (p *parser) parseImplMethod() (*FnDecl, error) {
	if _, err := p.expect(lexer.TokFn); err != nil {
		return nil, err
	}
	nameTok, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	typeParams, typeBounds, err := p.parseTypeParamsWithBounds()
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
		Name:       nameTok,
		TypeParams: typeParams,
		TypeBounds: typeBounds,
		Params:     params,
		RetType:    retType,
		Body:       body,
	}, nil
}

func (p *parser) parseImplDecl() (Decl, error) {
	implTok := p.advance() // consume 'impl'
	firstName, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	// Distinguish: impl TraitName for TypeName  vs  impl TypeName { ... }
	if p.check(lexer.TokFor) {
		p.advance() // consume 'for'
		typeName, err := p.expect(lexer.TokIdent)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.TokLBrace); err != nil {
			return nil, err
		}
		var methods []*FnDecl
		for !p.check(lexer.TokRBrace) && !p.check(lexer.TokEOF) {
			m, err := p.parseImplMethod()
			if err != nil {
				return nil, err
			}
			methods = append(methods, m)
		}
		if _, err := p.expect(lexer.TokRBrace); err != nil {
			return nil, err
		}
		return &ImplForDecl{ImplTok: implTok, TraitName: firstName, TypeName: typeName, Methods: methods}, nil
	}
	// Regular impl TypeName { ... }
	if _, err := p.expect(lexer.TokLBrace); err != nil {
		return nil, err
	}
	var methods []*FnDecl
	for !p.check(lexer.TokRBrace) && !p.check(lexer.TokEOF) {
		m, err := p.parseImplMethod()
		if err != nil {
			return nil, err
		}
		methods = append(methods, m)
	}
	if _, err := p.expect(lexer.TokRBrace); err != nil {
		return nil, err
	}
	return &ImplDecl{ImplTok: implTok, TypeName: firstName, Methods: methods}, nil
}

func (p *parser) parseTraitDecl() (*TraitDecl, error) {
	traitTok := p.advance() // consume 'trait'
	name, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokLBrace); err != nil {
		return nil, err
	}
	var methods []*TraitMethod
	for !p.check(lexer.TokRBrace) && !p.check(lexer.TokEOF) {
		if _, err := p.expect(lexer.TokFn); err != nil {
			return nil, err
		}
		mName, err := p.expect(lexer.TokIdent)
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
		methods = append(methods, &TraitMethod{Name: mName, Params: params, RetType: retType})
	}
	if _, err := p.expect(lexer.TokRBrace); err != nil {
		return nil, err
	}
	return &TraitDecl{TraitTok: traitTok, Name: name, Methods: methods}, nil
}
