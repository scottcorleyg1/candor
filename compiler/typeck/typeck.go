// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg1/candor

// Package typeck performs Core type checking on a parsed Candor AST.
//
// Strategy:
//  1. Collect all function signatures and struct definitions (pass 1).
//  2. Type-check each function body against its declared signature (pass 2).
//
// The output is a Result containing a map from every Expr node to its
// resolved Type. Downstream phases (emit_c) query this map instead of
// re-deriving types.
//
// Integer and float literals carry sentinel types (TIntLit, TFloatLit) until
// they are coerced to a concrete type by context (parameter type, return type,
// explicit annotation). Two uncoerced integer literals default to i64.
package typeck

import (
	"fmt"

	"github.com/scottcorleyg1/candor/compiler/lexer"
	"github.com/scottcorleyg1/candor/compiler/parser"
)

// Error is a type-check diagnostic with source position.
type Error struct {
	Tok lexer.Token
	Msg string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.Tok.File, e.Tok.Line, e.Tok.Col, e.Msg)
}

// Result is the output of a successful type-check pass.
type Result struct {
	// ExprTypes maps each Expr node to its resolved Type.
	ExprTypes map[parser.Expr]Type
	// FnSigs maps function names to their signature.
	FnSigs map[string]*FnType
	// Structs maps struct names to their StructType.
	Structs map[string]*StructType
}

// Check type-checks a fully parsed File and returns a Result.
func Check(file *parser.File) (*Result, error) {
	c := &checker{
		exprTypes: make(map[parser.Expr]Type),
		fnSigs:    make(map[string]*FnType),
		structs:   make(map[string]*StructType),
	}
	if err := c.checkFile(file); err != nil {
		return nil, err
	}
	return &Result{
		ExprTypes: c.exprTypes,
		FnSigs:    c.fnSigs,
		Structs:   c.structs,
	}, nil
}

// ── Internal checker state ────────────────────────────────────────────────────

type checker struct {
	exprTypes map[parser.Expr]Type
	fnSigs    map[string]*FnType
	structs   map[string]*StructType
}

// scope is a linked chain of variable bindings.
type scope struct {
	vars   map[string]Type
	parent *scope
}

func newScope(parent *scope) *scope {
	return &scope{vars: make(map[string]Type), parent: parent}
}

func (s *scope) lookup(name string) (Type, bool) {
	if t, ok := s.vars[name]; ok {
		return t, true
	}
	if s.parent != nil {
		return s.parent.lookup(name)
	}
	return nil, false
}

func (s *scope) define(name string, t Type) {
	s.vars[name] = t
}

func (c *checker) record(expr parser.Expr, t Type) Type {
	c.exprTypes[expr] = t
	return t
}

func (c *checker) errorf(tok lexer.Token, format string, args ...any) error {
	return &Error{Tok: tok, Msg: fmt.Sprintf(format, args...)}
}

// ── Pass 1: collect signatures ────────────────────────────────────────────────

func (c *checker) checkFile(file *parser.File) error {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *parser.FnDecl:
			sig, err := c.buildFnSig(d)
			if err != nil {
				return err
			}
			c.fnSigs[d.Name.Lexeme] = sig
		case *parser.StructDecl:
			st, err := c.buildStructType(d)
			if err != nil {
				return err
			}
			c.structs[d.Name.Lexeme] = st
		}
	}
	for _, decl := range file.Decls {
		if d, ok := decl.(*parser.FnDecl); ok {
			if err := c.checkFnDecl(d); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *checker) buildFnSig(d *parser.FnDecl) (*FnType, error) {
	params := make([]Type, len(d.Params))
	for i, p := range d.Params {
		t, err := c.resolveTypeExpr(p.Type)
		if err != nil {
			return nil, err
		}
		params[i] = t
	}
	ret, err := c.resolveTypeExpr(d.RetType)
	if err != nil {
		return nil, err
	}
	return &FnType{Params: params, Ret: ret}, nil
}

func (c *checker) buildStructType(d *parser.StructDecl) (*StructType, error) {
	st := &StructType{Name: d.Name.Lexeme, Fields: make(map[string]Type)}
	for _, f := range d.Fields {
		t, err := c.resolveTypeExpr(f.Type)
		if err != nil {
			return nil, err
		}
		st.Fields[f.Name.Lexeme] = t
	}
	return st, nil
}

func (c *checker) resolveTypeExpr(te parser.TypeExpr) (Type, error) {
	switch t := te.(type) {
	case *parser.NamedType:
		name := t.Name.Lexeme
		if builtin, ok := BuiltinTypes[name]; ok {
			return builtin, nil
		}
		if st, ok := c.structs[name]; ok {
			return st, nil
		}
		return nil, c.errorf(t.Name, "unknown type %q", name)

	case *parser.GenericType:
		params := make([]Type, len(t.Params))
		for i, p := range t.Params {
			resolved, err := c.resolveTypeExpr(p)
			if err != nil {
				return nil, err
			}
			params[i] = resolved
		}
		return &GenType{Con: t.Name.Lexeme, Params: params}, nil

	case *parser.FnType:
		params := make([]Type, len(t.Params))
		for i, p := range t.Params {
			resolved, err := c.resolveTypeExpr(p)
			if err != nil {
				return nil, err
			}
			params[i] = resolved
		}
		ret, err := c.resolveTypeExpr(t.RetType)
		if err != nil {
			return nil, err
		}
		return &FnType{Params: params, Ret: ret}, nil

	default:
		return nil, fmt.Errorf("unhandled TypeExpr %T", te)
	}
}

// ── Pass 2: check function bodies ────────────────────────────────────────────

func (c *checker) checkFnDecl(d *parser.FnDecl) error {
	sig := c.fnSigs[d.Name.Lexeme]
	sc := newScope(nil)
	for i, p := range d.Params {
		sc.define(p.Name.Lexeme, sig.Params[i])
	}
	return c.checkBlock(d.Body, sc, sig.Ret)
}

func (c *checker) checkBlock(block *parser.BlockStmt, sc *scope, retType Type) error {
	inner := newScope(sc)
	for _, stmt := range block.Stmts {
		if err := c.checkStmt(stmt, inner, retType); err != nil {
			return err
		}
	}
	return nil
}

func (c *checker) checkStmt(stmt parser.Stmt, sc *scope, retType Type) error {
	switch s := stmt.(type) {
	case *parser.LetStmt:
		return c.checkLetStmt(s, sc)
	case *parser.ReturnStmt:
		return c.checkReturnStmt(s, sc, retType)
	case *parser.ExprStmt:
		_, err := c.checkExpr(s.X, sc, nil)
		return err
	case *parser.IfStmt:
		return c.checkIfStmt(s, sc, retType)
	case *parser.LoopStmt:
		return c.checkBlock(s.Body, sc, retType)
	case *parser.BreakStmt:
		return nil
	case *parser.BlockStmt:
		return c.checkBlock(s, sc, retType)
	default:
		return fmt.Errorf("unhandled Stmt %T", stmt)
	}
}

func (c *checker) checkLetStmt(s *parser.LetStmt, sc *scope) error {
	var hint Type
	if s.TypeAnn != nil {
		var err error
		hint, err = c.resolveTypeExpr(s.TypeAnn)
		if err != nil {
			return err
		}
	}
	valType, err := c.checkExpr(s.Value, sc, hint)
	if err != nil {
		return err
	}
	if hint != nil {
		coerced, ok := Coerce(valType, hint)
		if !ok {
			return c.errorf(s.LetTok, "type mismatch: cannot use %s as %s", valType, hint)
		}
		valType = coerced
		c.exprTypes[s.Value] = coerced
	}
	sc.define(s.Name.Lexeme, valType)
	return nil
}

func (c *checker) checkReturnStmt(s *parser.ReturnStmt, sc *scope, retType Type) error {
	if s.Value == nil {
		if !retType.Equals(TUnit) {
			return c.errorf(s.ReturnTok, "bare return in function returning %s", retType)
		}
		return nil
	}
	valType, err := c.checkExpr(s.Value, sc, retType)
	if err != nil {
		return err
	}
	coerced, ok := Coerce(valType, retType)
	if !ok {
		return c.errorf(s.ReturnTok, "return type mismatch: got %s, expected %s", valType, retType)
	}
	c.exprTypes[s.Value] = coerced
	return nil
}

func (c *checker) checkIfStmt(s *parser.IfStmt, sc *scope, retType Type) error {
	condType, err := c.checkExpr(s.Cond, sc, TBool)
	if err != nil {
		return err
	}
	if !condType.Equals(TBool) {
		return c.errorf(s.IfTok, "if condition must be bool, got %s", condType)
	}
	if err := c.checkBlock(s.Then, sc, retType); err != nil {
		return err
	}
	if s.Else != nil {
		return c.checkStmt(s.Else, sc, retType)
	}
	return nil
}

// ── Expressions ──────────────────────────────────────────────────────────────

// checkExpr infers the type of expr, records it, and returns it.
// hint is an optional expected type used to coerce literal sentinels.
func (c *checker) checkExpr(expr parser.Expr, sc *scope, hint Type) (Type, error) {
	t, err := c.inferExpr(expr, sc, hint)
	if err != nil {
		return nil, err
	}
	c.record(expr, t)
	return t, nil
}

func (c *checker) inferExpr(expr parser.Expr, sc *scope, hint Type) (Type, error) {
	switch e := expr.(type) {
	case *parser.IntLitExpr:
		if hint != nil && IsIntType(hint) {
			return hint, nil
		}
		return TIntLit, nil

	case *parser.FloatLitExpr:
		if hint != nil && IsFloatType(hint) {
			return hint, nil
		}
		return TFloatLit, nil

	case *parser.StringLitExpr:
		return TStr, nil

	case *parser.BoolLitExpr:
		return TBool, nil

	case *parser.IdentExpr:
		return c.inferIdentExpr(e, sc)

	case *parser.BinaryExpr:
		return c.inferBinaryExpr(e, sc, hint)

	case *parser.UnaryExpr:
		return c.inferUnaryExpr(e, sc, hint)

	case *parser.CallExpr:
		return c.inferCallExpr(e, sc)

	case *parser.FieldExpr:
		return c.inferFieldExpr(e, sc)

	case *parser.IndexExpr:
		return c.inferIndexExpr(e, sc)

	case *parser.MustExpr:
		return c.inferMustExpr(e, sc, hint)

	case *parser.ReturnExpr:
		// return inside a must{} arm — type is never (exits the function)
		if _, err := c.checkExpr(e.Value, sc, nil); err != nil {
			return nil, err
		}
		return TNever, nil

	default:
		return nil, fmt.Errorf("unhandled Expr %T", expr)
	}
}

func (c *checker) inferIdentExpr(e *parser.IdentExpr, sc *scope) (Type, error) {
	name := e.Tok.Lexeme

	// Value-level keywords with known types
	switch name {
	case "unit":
		return TUnit, nil
	case "true", "false":
		return TBool, nil
	}

	// Variable lookup
	if t, ok := sc.lookup(name); ok {
		return t, nil
	}

	// Function name
	if sig, ok := c.fnSigs[name]; ok {
		return sig, nil
	}

	// Built-in constructors (ok, err, some, none, move) — typed at call site
	switch e.Tok.Type {
	case lexer.TokOk, lexer.TokErr, lexer.TokSome, lexer.TokNone, lexer.TokMove:
		return &GenType{Con: name}, nil
	}

	return nil, c.errorf(e.Tok, "undefined identifier %q", name)
}

func (c *checker) inferBinaryExpr(e *parser.BinaryExpr, sc *scope, hint Type) (Type, error) {
	switch e.Op.Type {
	case lexer.TokPlus, lexer.TokMinus, lexer.TokStar, lexer.TokSlash, lexer.TokPercent:
		// Arithmetic: both sides should be the same numeric type.
		// Pass hint down so literals on either side can coerce.
		left, err := c.checkExpr(e.Left, sc, hint)
		if err != nil {
			return nil, err
		}
		right, err := c.checkExpr(e.Right, sc, left)
		if err != nil {
			return nil, err
		}
		unified, ok := UnifyNumeric(left, right)
		if !ok {
			return nil, c.errorf(e.Op, "cannot apply %s to %s and %s", e.Op.Lexeme, left, right)
		}
		// Pin both operands to the resolved concrete type
		c.exprTypes[e.Left] = unified
		c.exprTypes[e.Right] = unified
		return unified, nil

	case lexer.TokEqEq, lexer.TokBangEq:
		left, err := c.checkExpr(e.Left, sc, nil)
		if err != nil {
			return nil, err
		}
		right, err := c.checkExpr(e.Right, sc, left)
		if err != nil {
			return nil, err
		}
		if _, ok := Unify(left, right); !ok {
			return nil, c.errorf(e.Op, "cannot compare %s with %s", left, right)
		}
		return TBool, nil

	case lexer.TokLt, lexer.TokGt, lexer.TokLtEq, lexer.TokGtEq:
		left, err := c.checkExpr(e.Left, sc, nil)
		if err != nil {
			return nil, err
		}
		right, err := c.checkExpr(e.Right, sc, left)
		if err != nil {
			return nil, err
		}
		if _, ok := UnifyNumeric(left, right); !ok {
			return nil, c.errorf(e.Op, "cannot order-compare %s and %s", left, right)
		}
		return TBool, nil

	case lexer.TokAnd, lexer.TokOr:
		left, err := c.checkExpr(e.Left, sc, TBool)
		if err != nil {
			return nil, err
		}
		right, err := c.checkExpr(e.Right, sc, TBool)
		if err != nil {
			return nil, err
		}
		if !left.Equals(TBool) || !right.Equals(TBool) {
			return nil, c.errorf(e.Op, "%s requires bool operands, got %s and %s", e.Op.Lexeme, left, right)
		}
		return TBool, nil

	default:
		return nil, c.errorf(e.Op, "unknown binary operator %q", e.Op.Lexeme)
	}
}

func (c *checker) inferUnaryExpr(e *parser.UnaryExpr, sc *scope, hint Type) (Type, error) {
	switch e.Op.Type {
	case lexer.TokBang, lexer.TokNot:
		t, err := c.checkExpr(e.Operand, sc, TBool)
		if err != nil {
			return nil, err
		}
		if !t.Equals(TBool) {
			return nil, c.errorf(e.Op, "! / not requires bool, got %s", t)
		}
		return TBool, nil

	case lexer.TokMinus:
		t, err := c.checkExpr(e.Operand, sc, hint)
		if err != nil {
			return nil, err
		}
		if !IsNumericType(t) && t != TIntLit && t != TFloatLit {
			return nil, c.errorf(e.Op, "unary - requires numeric type, got %s", t)
		}
		return t, nil

	case lexer.TokAmp:
		t, err := c.checkExpr(e.Operand, sc, nil)
		if err != nil {
			return nil, err
		}
		return &GenType{Con: "ref", Params: []Type{t}}, nil

	default:
		return nil, c.errorf(e.Op, "unknown unary operator %q", e.Op.Lexeme)
	}
}

func (c *checker) inferCallExpr(e *parser.CallExpr, sc *scope) (Type, error) {
	fnType, err := c.checkExpr(e.Fn, sc, nil)
	if err != nil {
		return nil, err
	}
	sig, ok := fnType.(*FnType)
	if !ok {
		return nil, c.errorf(e.Fn.Pos(), "cannot call non-function type %s", fnType)
	}
	if len(e.Args) != len(sig.Params) {
		return nil, c.errorf(e.LParen,
			"argument count mismatch: expected %d, got %d", len(sig.Params), len(e.Args))
	}
	for i, arg := range e.Args {
		argType, err := c.checkExpr(arg, sc, sig.Params[i])
		if err != nil {
			return nil, err
		}
		coerced, ok := Coerce(argType, sig.Params[i])
		if !ok {
			return nil, c.errorf(arg.Pos(),
				"argument %d: cannot use %s as %s", i+1, argType, sig.Params[i])
		}
		c.exprTypes[arg] = coerced
	}
	return sig.Ret, nil
}

func (c *checker) inferFieldExpr(e *parser.FieldExpr, sc *scope) (Type, error) {
	recvType, err := c.checkExpr(e.Receiver, sc, nil)
	if err != nil {
		return nil, err
	}
	// Transparently dereference ref<T> and refmut<T>
	if gen, ok := recvType.(*GenType); ok &&
		(gen.Con == "ref" || gen.Con == "refmut") && len(gen.Params) > 0 {
		recvType = gen.Params[0]
	}
	st, ok := recvType.(*StructType)
	if !ok {
		return nil, c.errorf(e.Dot, "field access on non-struct type %s", recvType)
	}
	fieldType, ok := st.Fields[e.Field.Lexeme]
	if !ok {
		return nil, c.errorf(e.Field, "unknown field %q on %s", e.Field.Lexeme, st.Name)
	}
	return fieldType, nil
}

func (c *checker) inferIndexExpr(e *parser.IndexExpr, sc *scope) (Type, error) {
	collType, err := c.checkExpr(e.Collection, sc, nil)
	if err != nil {
		return nil, err
	}
	if _, err := c.checkExpr(e.Index, sc, TU64); err != nil {
		return nil, err
	}
	gen, ok := collType.(*GenType)
	if !ok || (gen.Con != "vec" && gen.Con != "ring") || len(gen.Params) == 0 {
		return nil, c.errorf(e.LBracket, "cannot index type %s", collType)
	}
	return gen.Params[0], nil
}

func (c *checker) inferMustExpr(e *parser.MustExpr, sc *scope, hint Type) (Type, error) {
	operandType, err := c.checkExpr(e.X, sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := operandType.(*GenType)
	if !ok || (gen.Con != "result" && gen.Con != "option") {
		return nil, c.errorf(e.MustTok,
			"must{} requires result<T,E> or option<T>, got %s", operandType)
	}
	// Check each arm body; collect non-never types
	var bodyType Type
	for _, arm := range e.Arms {
		if _, err := c.checkExpr(arm.Pattern, sc, nil); err != nil {
			return nil, err
		}
		armType, err := c.checkExpr(arm.Body, sc, hint)
		if err != nil {
			return nil, err
		}
		if armType.Equals(TNever) {
			continue
		}
		if bodyType == nil {
			bodyType = armType
		} else {
			unified, ok := Unify(bodyType, armType)
			if !ok {
				return nil, c.errorf(arm.Arrow,
					"must{} arm type %s does not match expected %s", armType, bodyType)
			}
			bodyType = unified
		}
	}
	if bodyType == nil {
		bodyType = TUnit
	}
	return bodyType, nil
}
