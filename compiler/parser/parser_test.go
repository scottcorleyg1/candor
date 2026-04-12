// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/candor-core/candor

package parser

import (
	"testing"

	"github.com/candor-core/candor/compiler/lexer"
)

// parse is a test helper: lex + parse src, fatal on any error.
func parse(t *testing.T, src string) *File {
	t.Helper()
	tokens, err := lexer.Tokenize("<test>", src)
	if err != nil {
		t.Fatalf("lex error: %v", err)
	}
	file, err := Parse("<test>", tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return file
}

// parseErr expects a parse error and returns it; fatal if parse succeeds.
func parseErr(t *testing.T, src string) error {
	t.Helper()
	tokens, err := lexer.Tokenize("<test>", src)
	if err != nil {
		return err
	}
	_, err = Parse("<test>", tokens)
	if err == nil {
		t.Fatal("expected parse error, got none")
	}
	return err
}

// ── Acceptance criterion ─────────────────────────────────────────────────────

// TestAcceptanceCriterionProgram verifies that the v0.0.1 target program
// parses into the correct AST shape end-to-end.
func TestAcceptanceCriterionProgram(t *testing.T) {
	src := `
fn add(a: u32, b: u32) -> u32 { return a + b }

fn main() -> unit {
    let x = add(1, 2)
    return unit
}
`
	file := parse(t, src)

	if len(file.Decls) != 2 {
		t.Fatalf("expected 2 declarations, got %d", len(file.Decls))
	}

	// ── fn add ────────────────────────────────────────────────────────────
	addFn, ok := file.Decls[0].(*FnDecl)
	if !ok {
		t.Fatal("first decl must be *FnDecl")
	}
	if addFn.Name.Lexeme != "add" {
		t.Errorf("fn name: want 'add', got %q", addFn.Name.Lexeme)
	}
	if len(addFn.Params) != 2 {
		t.Fatalf("fn add: want 2 params, got %d", len(addFn.Params))
	}
	if addFn.Params[0].Name.Lexeme != "a" {
		t.Errorf("param 0: want 'a', got %q", addFn.Params[0].Name.Lexeme)
	}
	if addFn.Params[1].Name.Lexeme != "b" {
		t.Errorf("param 1: want 'b', got %q", addFn.Params[1].Name.Lexeme)
	}
	// return type is NamedType("u32")
	retNamed, ok := addFn.RetType.(*NamedType)
	if !ok {
		t.Fatal("fn add return type must be *NamedType")
	}
	if retNamed.Name.Lexeme != "u32" {
		t.Errorf("fn add return type: want 'u32', got %q", retNamed.Name.Lexeme)
	}
	// body: single ReturnStmt with BinaryExpr(a + b)
	if len(addFn.Body.Stmts) != 1 {
		t.Fatalf("fn add body: want 1 stmt, got %d", len(addFn.Body.Stmts))
	}
	retStmt, ok := addFn.Body.Stmts[0].(*ReturnStmt)
	if !ok {
		t.Fatal("fn add body[0] must be *ReturnStmt")
	}
	binExpr, ok := retStmt.Value.(*BinaryExpr)
	if !ok {
		t.Fatal("return value must be *BinaryExpr")
	}
	if binExpr.Op.Lexeme != "+" {
		t.Errorf("binary op: want '+', got %q", binExpr.Op.Lexeme)
	}
	leftIdent, ok := binExpr.Left.(*IdentExpr)
	if !ok || leftIdent.Tok.Lexeme != "a" {
		t.Errorf("binary left: want IdentExpr('a')")
	}
	rightIdent, ok := binExpr.Right.(*IdentExpr)
	if !ok || rightIdent.Tok.Lexeme != "b" {
		t.Errorf("binary right: want IdentExpr('b')")
	}

	// ── fn main ───────────────────────────────────────────────────────────
	mainFn, ok := file.Decls[1].(*FnDecl)
	if !ok {
		t.Fatal("second decl must be *FnDecl")
	}
	if mainFn.Name.Lexeme != "main" {
		t.Errorf("fn name: want 'main', got %q", mainFn.Name.Lexeme)
	}
	if len(mainFn.Params) != 0 {
		t.Errorf("fn main: want 0 params, got %d", len(mainFn.Params))
	}
	retUnit, ok := mainFn.RetType.(*NamedType)
	if !ok || retUnit.Name.Lexeme != "unit" {
		t.Fatal("fn main return type must be NamedType('unit')")
	}
	if len(mainFn.Body.Stmts) != 2 {
		t.Fatalf("fn main body: want 2 stmts, got %d", len(mainFn.Body.Stmts))
	}
	// stmt 0: let x = add(1, 2)
	letStmt, ok := mainFn.Body.Stmts[0].(*LetStmt)
	if !ok {
		t.Fatal("main body[0] must be *LetStmt")
	}
	if letStmt.Name.Lexeme != "x" {
		t.Errorf("let name: want 'x', got %q", letStmt.Name.Lexeme)
	}
	callExpr, ok := letStmt.Value.(*CallExpr)
	if !ok {
		t.Fatal("let value must be *CallExpr")
	}
	callIdent, ok := callExpr.Fn.(*IdentExpr)
	if !ok || callIdent.Tok.Lexeme != "add" {
		t.Fatal("call fn must be IdentExpr('add')")
	}
	if len(callExpr.Args) != 2 {
		t.Fatalf("call args: want 2, got %d", len(callExpr.Args))
	}
	arg0, ok := callExpr.Args[0].(*IntLitExpr)
	if !ok || arg0.Tok.Lexeme != "1" {
		t.Errorf("arg 0: want IntLitExpr('1')")
	}
	arg1, ok := callExpr.Args[1].(*IntLitExpr)
	if !ok || arg1.Tok.Lexeme != "2" {
		t.Errorf("arg 1: want IntLitExpr('2')")
	}
	// stmt 1: return unit
	retMain, ok := mainFn.Body.Stmts[1].(*ReturnStmt)
	if !ok {
		t.Fatal("main body[1] must be *ReturnStmt")
	}
	unitIdent, ok := retMain.Value.(*IdentExpr)
	if !ok || unitIdent.Tok.Lexeme != "unit" {
		t.Fatal("return value must be IdentExpr('unit')")
	}
}

// ── Function declarations ────────────────────────────────────────────────────

func TestFnNoParams(t *testing.T) {
	file := parse(t, `fn greet() -> unit { return unit }`)
	if len(file.Decls) != 1 {
		t.Fatalf("want 1 decl, got %d", len(file.Decls))
	}
	fn := file.Decls[0].(*FnDecl)
	if len(fn.Params) != 0 {
		t.Errorf("want 0 params, got %d", len(fn.Params))
	}
}

func TestFnTrailingComma(t *testing.T) {
	// Trailing comma in parameter list is allowed
	parse(t, `fn f(a: u32,) -> u32 { return a }`)
}

func TestStructDecl(t *testing.T) {
	file := parse(t, `struct Point { x: f64, y: f64, }`)
	if len(file.Decls) != 1 {
		t.Fatalf("want 1 decl, got %d", len(file.Decls))
	}
	s, ok := file.Decls[0].(*StructDecl)
	if !ok {
		t.Fatal("expected *StructDecl")
	}
	if s.Name.Lexeme != "Point" {
		t.Errorf("struct name: want 'Point', got %q", s.Name.Lexeme)
	}
	if len(s.Fields) != 2 {
		t.Errorf("want 2 fields, got %d", len(s.Fields))
	}
}

// ── Types ─────────────────────────────────────────────────────────────────────

func TestGenericType(t *testing.T) {
	file := parse(t, `fn f(x: option<u64>) -> result<u64, str> { return ok(x) }`)
	fn := file.Decls[0].(*FnDecl)

	paramType, ok := fn.Params[0].Type.(*GenericType)
	if !ok {
		t.Fatal("param type must be *GenericType")
	}
	if paramType.Name.Lexeme != "option" {
		t.Errorf("generic name: want 'option', got %q", paramType.Name.Lexeme)
	}

	retType, ok := fn.RetType.(*GenericType)
	if !ok {
		t.Fatal("return type must be *GenericType")
	}
	if retType.Name.Lexeme != "result" {
		t.Errorf("return generic: want 'result', got %q", retType.Name.Lexeme)
	}
	if len(retType.Params) != 2 {
		t.Errorf("result params: want 2, got %d", len(retType.Params))
	}
}

func TestFnType(t *testing.T) {
	parse(t, `fn apply(f: fn(u32) -> u32, x: u32) -> u32 { return f(x) }`)
}

// ── Statements ───────────────────────────────────────────────────────────────

func TestLetWithTypeAnnotation(t *testing.T) {
	file := parse(t, `fn f() -> unit { let x: u64 = 42 return unit }`)
	fn := file.Decls[0].(*FnDecl)
	let := fn.Body.Stmts[0].(*LetStmt)
	if let.TypeAnn == nil {
		t.Fatal("expected type annotation")
	}
	ann, ok := let.TypeAnn.(*NamedType)
	if !ok || ann.Name.Lexeme != "u64" {
		t.Errorf("type annotation: want 'u64'")
	}
}

func TestIfElse(t *testing.T) {
	file := parse(t, `fn f(x: u32) -> u32 { if x > 0 { return x } else { return 0 } }`)
	fn := file.Decls[0].(*FnDecl)
	ifStmt, ok := fn.Body.Stmts[0].(*IfStmt)
	if !ok {
		t.Fatal("expected *IfStmt")
	}
	if ifStmt.Else == nil {
		t.Fatal("expected else branch")
	}
}

func TestIfElseIf(t *testing.T) {
	parse(t, `fn f(x: i32) -> i32 {
		if x > 0 { return 1 }
		else if x < 0 { return -1 }
		else { return 0 }
	}`)
}

func TestLoopBreak(t *testing.T) {
	file := parse(t, `fn f() -> unit { loop { break } }`)
	fn := file.Decls[0].(*FnDecl)
	loopStmt, ok := fn.Body.Stmts[0].(*LoopStmt)
	if !ok {
		t.Fatal("expected *LoopStmt")
	}
	_, ok = loopStmt.Body.Stmts[0].(*BreakStmt)
	if !ok {
		t.Fatal("expected *BreakStmt inside loop")
	}
}

// ── Expressions ──────────────────────────────────────────────────────────────

func TestBinaryPrecedenceMulBeforeAdd(t *testing.T) {
	// 1 + 2 * 3  should parse as  1 + (2 * 3)
	file := parse(t, `fn f() -> u32 { return 1 + 2 * 3 }`)
	fn := file.Decls[0].(*FnDecl)
	ret := fn.Body.Stmts[0].(*ReturnStmt)
	add, ok := ret.Value.(*BinaryExpr)
	if !ok || add.Op.Lexeme != "+" {
		t.Fatal("outer op must be +")
	}
	mul, ok := add.Right.(*BinaryExpr)
	if !ok || mul.Op.Lexeme != "*" {
		t.Fatal("right of + must be * (higher precedence)")
	}
}

func TestBinaryPrecedenceCmpAfterAdd(t *testing.T) {
	// a + b == c  should parse as  (a + b) == c
	file := parse(t, `fn f(a: u32, b: u32, c: u32) -> bool { return a + b == c }`)
	fn := file.Decls[0].(*FnDecl)
	ret := fn.Body.Stmts[0].(*ReturnStmt)
	eq, ok := ret.Value.(*BinaryExpr)
	if !ok || eq.Op.Lexeme != "==" {
		t.Fatal("outer op must be ==")
	}
	_, ok = eq.Left.(*BinaryExpr) // (a + b)
	if !ok {
		t.Fatal("left of == must be BinaryExpr (a + b)")
	}
}

func TestUnaryNot(t *testing.T) {
	file := parse(t, `fn f(x: bool) -> bool { return not x }`)
	fn := file.Decls[0].(*FnDecl)
	ret := fn.Body.Stmts[0].(*ReturnStmt)
	u, ok := ret.Value.(*UnaryExpr)
	if !ok || u.Op.Type != lexer.TokNot {
		t.Fatal("expected UnaryExpr with 'not'")
	}
}

func TestFieldAccess(t *testing.T) {
	file := parse(t, `fn f(p: Point) -> f64 { return p.x }`)
	fn := file.Decls[0].(*FnDecl)
	ret := fn.Body.Stmts[0].(*ReturnStmt)
	field, ok := ret.Value.(*FieldExpr)
	if !ok {
		t.Fatal("expected *FieldExpr")
	}
	if field.Field.Lexeme != "x" {
		t.Errorf("field: want 'x', got %q", field.Field.Lexeme)
	}
}

func TestIndexExpr(t *testing.T) {
	parse(t, `fn f(v: vec<u64>) -> u64 { return v[0] }`)
}

func TestBoolLiteral(t *testing.T) {
	file := parse(t, `fn f() -> bool { return true }`)
	fn := file.Decls[0].(*FnDecl)
	ret := fn.Body.Stmts[0].(*ReturnStmt)
	b, ok := ret.Value.(*BoolLitExpr)
	if !ok || b.Tok.Type != lexer.TokTrue {
		t.Fatal("expected BoolLitExpr(true)")
	}
}

func TestMustExpr(t *testing.T) {
	src := `
fn safe_div(a: u64, b: u64) -> result<u64, str> {
    let v = divide(a, b) must {
        ok(v)  => v
        err(e) => return err(e)
    }
    return ok(v)
}
`
	file := parse(t, src)
	fn := file.Decls[0].(*FnDecl)
	letStmt := fn.Body.Stmts[0].(*LetStmt)
	must, ok := letStmt.Value.(*MustExpr)
	if !ok {
		t.Fatal("let value must be *MustExpr")
	}
	if len(must.Arms) != 2 {
		t.Errorf("must arms: want 2, got %d", len(must.Arms))
	}
	// second arm body is ReturnExpr
	_, ok = must.Arms[1].Body.(*ReturnExpr)
	if !ok {
		t.Fatal("second arm body must be *ReturnExpr")
	}
}

func TestReferenceExpr(t *testing.T) {
	parse(t, `fn f(x: u64) -> unit { let r = &x return unit }`)
}

func TestNestedCalls(t *testing.T) {
	parse(t, `fn f() -> u64 { return foo(bar(1), baz(2, 3)) }`)
}

// ── File-scope directives ─────────────────────────────────────────────────────

func TestDirectivesSkipped(t *testing.T) {
	src := `
#use effects
fn f() -> unit { return unit }
`
	file := parse(t, src)
	if len(file.Decls) != 1 {
		t.Fatalf("expected 1 decl after skipping directive, got %d", len(file.Decls))
	}
}

// ── mut and assignment ────────────────────────────────────────────────────────

func TestLetMut(t *testing.T) {
	file := parse(t, `fn f() -> unit { let mut x: u32 = 0 return unit }`)
	fn := file.Decls[0].(*FnDecl)
	let := fn.Body.Stmts[0].(*LetStmt)
	if !let.Mut {
		t.Fatal("expected LetStmt.Mut == true")
	}
	if let.Name.Lexeme != "x" {
		t.Errorf("name: want 'x', got %q", let.Name.Lexeme)
	}
}

func TestAssignStmt(t *testing.T) {
	file := parse(t, `fn f() -> unit { let mut x: u32 = 0 x = 1 return unit }`)
	fn := file.Decls[0].(*FnDecl)
	if len(fn.Body.Stmts) != 3 {
		t.Fatalf("want 3 stmts, got %d", len(fn.Body.Stmts))
	}
	assign, ok := fn.Body.Stmts[1].(*AssignStmt)
	if !ok {
		t.Fatalf("stmt[1] must be *AssignStmt, got %T", fn.Body.Stmts[1])
	}
	if assign.Name.Lexeme != "x" {
		t.Errorf("assign name: want 'x', got %q", assign.Name.Lexeme)
	}
}

func TestMatchExpr(t *testing.T) {
	src := `fn f(x: bool) -> unit { match x { true => unit false => unit } return unit }`
	file := parse(t, src)
	fn := file.Decls[0].(*FnDecl)
	exprStmt, ok := fn.Body.Stmts[0].(*ExprStmt)
	if !ok {
		t.Fatalf("stmt[0] must be *ExprStmt, got %T", fn.Body.Stmts[0])
	}
	m, ok := exprStmt.X.(*MatchExpr)
	if !ok {
		t.Fatalf("expr must be *MatchExpr, got %T", exprStmt.X)
	}
	if len(m.Arms) != 2 {
		t.Errorf("match arms: want 2, got %d", len(m.Arms))
	}
}

// ── Effects annotations ───────────────────────────────────────────────────────

func TestPureAnnotation(t *testing.T) {
	file := parse(t, `fn add(a: u32, b: u32) -> u32 pure { return a + b }`)
	fn := file.Decls[0].(*FnDecl)
	if fn.Effects == nil {
		t.Fatal("expected Effects != nil")
	}
	if fn.Effects.Kind != EffectsPure {
		t.Errorf("kind: want EffectsPure, got %v", fn.Effects.Kind)
	}
}

func TestEffectsAnnotation(t *testing.T) {
	file := parse(t, `fn log(s: str) -> unit effects(io) { return unit }`)
	fn := file.Decls[0].(*FnDecl)
	if fn.Effects == nil {
		t.Fatal("expected Effects != nil")
	}
	if fn.Effects.Kind != EffectsDecl {
		t.Errorf("kind: want EffectsDecl, got %v", fn.Effects.Kind)
	}
	if len(fn.Effects.Names) != 1 || fn.Effects.Names[0] != "io" {
		t.Errorf("names: want [io], got %v", fn.Effects.Names)
	}
}

func TestEffectsMultiple(t *testing.T) {
	file := parse(t, `fn fetch(url: str) -> str effects(io, net) { return url }`)
	fn := file.Decls[0].(*FnDecl)
	if fn.Effects == nil || fn.Effects.Kind != EffectsDecl {
		t.Fatal("expected EffectsDecl annotation")
	}
	if len(fn.Effects.Names) != 2 {
		t.Errorf("want 2 effect names, got %v", fn.Effects.Names)
	}
}

func TestCapAnnotation(t *testing.T) {
	file := parse(t, `fn privileged() -> unit cap(Admin) { return unit }`)
	fn := file.Decls[0].(*FnDecl)
	if fn.Effects == nil || fn.Effects.Kind != EffectsCap {
		t.Fatal("expected EffectsCap annotation")
	}
	if fn.Effects.Names[0] != "Admin" {
		t.Errorf("cap name: want Admin, got %q", fn.Effects.Names[0])
	}
}

func TestNoAnnotation(t *testing.T) {
	file := parse(t, `fn f() -> unit { return unit }`)
	fn := file.Decls[0].(*FnDecl)
	if fn.Effects != nil {
		t.Errorf("want nil Effects, got %v", fn.Effects)
	}
}

// ── Struct literals ───────────────────────────────────────────────────────────

func TestStructLiteral(t *testing.T) {
	file := parse(t, `
struct Point { x: u32, y: u32 }
fn f() -> Point { return Point { x: 3, y: 4 } }`)
	fn := file.Decls[1].(*FnDecl)
	ret := fn.Body.Stmts[0].(*ReturnStmt)
	lit, ok := ret.Value.(*StructLitExpr)
	if !ok {
		t.Fatalf("expected *StructLitExpr, got %T", ret.Value)
	}
	if lit.TypeName.Lexeme != "Point" {
		t.Errorf("type name: want Point, got %q", lit.TypeName.Lexeme)
	}
	if len(lit.Fields) != 2 {
		t.Errorf("fields: want 2, got %d", len(lit.Fields))
	}
}

func TestStructLiteralInIf(t *testing.T) {
	// PascalCase struct literal as if condition should not cause issues;
	// lowercase variable followed by block should not be a struct literal.
	parse(t, `
struct Res { flag: bool }
fn f(cond: bool) -> unit {
    if cond { return unit }
    return unit
}`)
}

// ── Field assignment ──────────────────────────────────────────────────────────

func TestFieldAssignStmt(t *testing.T) {
	src := `
struct Point { x: u32, y: u32 }
fn f() -> unit {
    let mut p: Point = p
    p.x = 10
    return unit
}`
	file := parse(t, src)
	fn := file.Decls[1].(*FnDecl)
	assign, ok := fn.Body.Stmts[1].(*FieldAssignStmt)
	if !ok {
		t.Fatalf("stmt[1] must be *FieldAssignStmt, got %T", fn.Body.Stmts[1])
	}
	recv, ok := assign.Target.Receiver.(*IdentExpr)
	if !ok || recv.Tok.Lexeme != "p" {
		t.Errorf("receiver: want 'p', got %T", assign.Target.Receiver)
	}
	if assign.Target.Field.Lexeme != "x" {
		t.Errorf("field: want 'x', got %q", assign.Target.Field.Lexeme)
	}
}

func TestNestedFieldAssignStmt(t *testing.T) {
	// p.inner.x = 5 should parse as FieldAssignStmt{Target: p.inner.x}
	src := `
struct Inner { x: u32 }
struct Outer { inner: Inner }
fn f() -> unit {
    let mut o: Outer = o
    o.inner.x = 5
    return unit
}`
	file := parse(t, src)
	fn := file.Decls[2].(*FnDecl)
	assign, ok := fn.Body.Stmts[1].(*FieldAssignStmt)
	if !ok {
		t.Fatalf("stmt[1] must be *FieldAssignStmt, got %T", fn.Body.Stmts[1])
	}
	if assign.Target.Field.Lexeme != "x" {
		t.Errorf("field: want 'x', got %q", assign.Target.Field.Lexeme)
	}
	// receiver of the target should itself be a FieldExpr (o.inner)
	_, ok = assign.Target.Receiver.(*FieldExpr)
	if !ok {
		t.Errorf("receiver of nested assign must be *FieldExpr, got %T", assign.Target.Receiver)
	}
}

// ── module / use declarations ─────────────────────────────────────────────────

func TestModuleDecl(t *testing.T) {
	file := parse(t, `module mylib
fn f() -> unit { return unit }`)
	mod, ok := file.Decls[0].(*ModuleDecl)
	if !ok {
		t.Fatalf("Decls[0] must be *ModuleDecl, got %T", file.Decls[0])
	}
	if mod.Name.Lexeme != "mylib" {
		t.Errorf("module name: want 'mylib', got %q", mod.Name.Lexeme)
	}
}

func TestUseDecl(t *testing.T) {
	file := parse(t, `module app
use mylib
fn main() -> unit { return unit }`)
	use, ok := file.Decls[1].(*UseDecl)
	if !ok {
		t.Fatalf("Decls[1] must be *UseDecl, got %T", file.Decls[1])
	}
	if len(use.Path) != 1 || use.Path[0].Lexeme != "mylib" {
		t.Errorf("use path: want [mylib], got %v", use.Path)
	}
}

func TestUseDeclPath(t *testing.T) {
	file := parse(t, `use mylib::Point
fn f() -> unit { return unit }`)
	use, ok := file.Decls[0].(*UseDecl)
	if !ok {
		t.Fatalf("Decls[0] must be *UseDecl, got %T", file.Decls[0])
	}
	if len(use.Path) != 2 {
		t.Fatalf("use path len: want 2, got %d", len(use.Path))
	}
	if use.Path[0].Lexeme != "mylib" || use.Path[1].Lexeme != "Point" {
		t.Errorf("use path: want [mylib, Point], got %v", use.Path)
	}
}

func TestUseDeclDeepPath(t *testing.T) {
	file := parse(t, `use std::io::Writer
fn f() -> unit { return unit }`)
	use := file.Decls[0].(*UseDecl)
	if len(use.Path) != 3 {
		t.Fatalf("want path len 3, got %d", len(use.Path))
	}
}

// ── for loops ────────────────────────────────────────────────────────────────

func TestForStmt(t *testing.T) {
	file := parse(t, `fn f(v: vec<u32>) -> unit { for x in v { return unit } return unit }`)
	fn := file.Decls[0].(*FnDecl)
	fs, ok := fn.Body.Stmts[0].(*ForStmt)
	if !ok {
		t.Fatalf("stmt[0] must be *ForStmt, got %T", fn.Body.Stmts[0])
	}
	if fs.Var.Lexeme != "x" {
		t.Errorf("var: want 'x', got %q", fs.Var.Lexeme)
	}
	coll, ok := fs.Collection.(*IdentExpr)
	if !ok || coll.Tok.Lexeme != "v" {
		t.Errorf("collection: want IdentExpr 'v', got %T", fs.Collection)
	}
}

// ── Error cases ───────────────────────────────────────────────────────────────

func TestMissingReturnType(t *testing.T) {
	parseErr(t, `fn f() { return unit }`)
}

func TestMissingFnBody(t *testing.T) {
	parseErr(t, `fn f() -> unit`)
}

func TestUnexpectedTokenInExpr(t *testing.T) {
	parseErr(t, `fn f() -> unit { return @ }`)
}

// ── #c_header directive ───────────────────────────────────────────────────────

func TestCHeaderDecl(t *testing.T) {
	file := parse(t, `#c_header "sys/types.h"
fn main() -> unit { return unit }
`)
	if len(file.Decls) < 2 {
		t.Fatalf("expected at least 2 decls, got %d", len(file.Decls))
	}
	ch, ok := file.Decls[0].(*CHeaderDecl)
	if !ok {
		t.Fatalf("Decls[0] must be *CHeaderDecl, got %T", file.Decls[0])
	}
	if ch.Path != "sys/types.h" {
		t.Errorf("CHeaderDecl.Path = %q, want %q", ch.Path, "sys/types.h")
	}
	if _, ok := file.Decls[1].(*FnDecl); !ok {
		t.Errorf("Decls[1] must be *FnDecl, got %T", file.Decls[1])
	}
}

func TestCHeaderDeclDoesNotDecorateNextFn(t *testing.T) {
	// #c_header must NOT attach to the following fn as a directive.
	file := parse(t, `#c_header "foo.h"
fn f() -> unit { return unit }
`)
	fn, ok := file.Decls[1].(*FnDecl)
	if !ok {
		t.Fatalf("Decls[1] must be *FnDecl, got %T", file.Decls[1])
	}
	for _, d := range fn.Directives {
		if d == "c_header" {
			t.Errorf("c_header leaked into fn.Directives: %v", fn.Directives)
		}
	}
}

func TestMultipleCHeaderDecls(t *testing.T) {
	file := parse(t, `#c_header "a.h"
#c_header "b.h"
fn f() -> unit { return unit }
`)
	if len(file.Decls) < 3 {
		t.Fatalf("expected at least 3 decls, got %d", len(file.Decls))
	}
	for i, path := range []string{"a.h", "b.h"} {
		ch, ok := file.Decls[i].(*CHeaderDecl)
		if !ok {
			t.Fatalf("Decls[%d] must be *CHeaderDecl, got %T", i, file.Decls[i])
		}
		if ch.Path != path {
			t.Errorf("Decls[%d].Path = %q, want %q", i, ch.Path, path)
		}
	}
}
