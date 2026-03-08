// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg1/candor

package typeck

import (
	"strings"
	"testing"

	"github.com/scottcorleyg1/candor/compiler/lexer"
	"github.com/scottcorleyg1/candor/compiler/parser"
)

// compile parses src and runs the type checker, returning (result, error).
func compile(src string) (*Result, error) {
	tokens, err := lexer.Tokenize("<test>", src)
	if err != nil {
		return nil, err
	}
	file, err := parser.Parse("<test>", tokens)
	if err != nil {
		return nil, err
	}
	return Check(file)
}

// mustCompile fails the test if compilation returns an error.
func mustCompile(t *testing.T, src string) *Result {
	t.Helper()
	r, err := compile(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return r
}

// mustFail fails the test if compilation succeeds, or if the error message
// does not contain the expected substring.
func mustFail(t *testing.T, src, want string) {
	t.Helper()
	_, err := compile(src)
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got: %v", want, err)
	}
}

// ── Acceptance criterion ──────────────────────────────────────────────────────

func TestAcceptanceCriterionProgram(t *testing.T) {
	src := `
fn add(a: u32, b: u32) -> u32 { return a + b }

fn main() -> unit {
    let x = add(1, 2)
    return unit
}
`
	r := mustCompile(t, src)

	// add must have signature fn(u32, u32) -> u32
	sig, ok := r.FnSigs["add"]
	if !ok {
		t.Fatal("no signature for add")
	}
	if !sig.Equals(&FnType{Params: []Type{TU32, TU32}, Ret: TU32}) {
		t.Errorf("add sig: got %s", sig)
	}

	// main must have signature fn() -> unit
	sigMain, ok := r.FnSigs["main"]
	if !ok {
		t.Fatal("no signature for main")
	}
	if !sigMain.Equals(&FnType{Params: nil, Ret: TUnit}) {
		t.Errorf("main sig: got %s", sigMain)
	}
}

// ── Primitive type checks ─────────────────────────────────────────────────────

func TestIntLiteralCoercesToAnnotatedType(t *testing.T) {
	mustCompile(t, `fn f() -> unit { let x: i32 = 42 return unit }`)
}

func TestIntLiteralCoercesToReturnType(t *testing.T) {
	mustCompile(t, `fn f() -> i64 { return 99 }`)
}

func TestFloatLiteralCoercesToReturnType(t *testing.T) {
	mustCompile(t, `fn f() -> f64 { return 3.14 }`)
}

func TestBoolReturn(t *testing.T) {
	mustCompile(t, `fn f() -> bool { return true }`)
}

func TestStringReturn(t *testing.T) {
	mustCompile(t, `fn f() -> str { return "hello" }`)
}

func TestUnitReturn(t *testing.T) {
	mustCompile(t, `fn f() -> unit { return unit }`)
}

// ── Arithmetic ────────────────────────────────────────────────────────────────

func TestArithmetic(t *testing.T) {
	mustCompile(t, `fn f(a: u32, b: u32) -> u32 { return a + b }`)
}

func TestArithmeticAllOps(t *testing.T) {
	mustCompile(t, `
fn f(a: i64, b: i64) -> i64 {
    let s = a + b
    let d = a - b
    let p = a * b
    let q = a / b
    let r = a % b
    return s
}`)
}

func TestArithmeticTypeMismatch(t *testing.T) {
	mustFail(t,
		`fn f(a: u32, b: i32) -> u32 { return a + b }`,
		"cannot apply")
}

// ── Comparison and logic ──────────────────────────────────────────────────────

func TestEqEq(t *testing.T) {
	mustCompile(t, `fn f(a: i32, b: i32) -> bool { return a == b }`)
}

func TestBangEq(t *testing.T) {
	mustCompile(t, `fn f(a: i32, b: i32) -> bool { return a != b }`)
}

func TestOrderComparison(t *testing.T) {
	mustCompile(t, `fn f(a: u32, b: u32) -> bool { return a < b }`)
}

func TestAndOr(t *testing.T) {
	mustCompile(t, `fn f(a: bool, b: bool) -> bool { return a and b }`)
	mustCompile(t, `fn f(a: bool, b: bool) -> bool { return a or b }`)
}

func TestUnaryNot(t *testing.T) {
	mustCompile(t, `fn f(a: bool) -> bool { return not a }`)
}

func TestUnaryMinus(t *testing.T) {
	mustCompile(t, `fn f(a: i64) -> i64 { return -a }`)
}

// ── Function calls ────────────────────────────────────────────────────────────

func TestCallCorrect(t *testing.T) {
	mustCompile(t, `
fn add(a: u32, b: u32) -> u32 { return a + b }
fn main() -> u32 { return add(1, 2) }
`)
}

func TestCallArgCountMismatch(t *testing.T) {
	mustFail(t,
		`fn f(a: u32) -> u32 { return a }
fn main() -> u32 { return f(1, 2) }`,
		"argument count mismatch")
}

func TestCallArgTypeMismatch(t *testing.T) {
	mustFail(t,
		`fn f(a: u32) -> u32 { return a }
fn main() -> unit { let x = f(true) return unit }`,
		"cannot use")
}

func TestMutualRecursionSignatures(t *testing.T) {
	// Both functions are visible after pass 1 — calls across functions work.
	mustCompile(t, `
fn is_even(n: u32) -> bool { return n == 0 }
fn is_odd(n: u32) -> bool  { return is_even(n) }
`)
}

// ── let statements ────────────────────────────────────────────────────────────

func TestLetInferred(t *testing.T) {
	mustCompile(t, `fn f() -> unit { let x = true return unit }`)
}

func TestLetAnnotated(t *testing.T) {
	mustCompile(t, `fn f() -> unit { let x: u32 = 42 return unit }`)
}

func TestLetTypeMismatch(t *testing.T) {
	mustFail(t,
		`fn f() -> unit { let x: bool = 42 return unit }`,
		"type mismatch")
}

func TestLetUsedInExpr(t *testing.T) {
	mustCompile(t, `
fn f() -> u32 {
    let a: u32 = 10
    let b: u32 = 20
    return a + b
}`)
}

// ── Undefined identifier ──────────────────────────────────────────────────────

func TestUndefinedIdent(t *testing.T) {
	mustFail(t,
		`fn f() -> unit { let x = bogus return unit }`,
		"undefined identifier")
}

// ── Struct field access ───────────────────────────────────────────────────────

func TestStructFieldAccess(t *testing.T) {
	mustCompile(t, `
struct Point { x: u32, y: u32 }
fn sum(p: Point) -> u32 { return p.x + p.y }
`)
}

func TestStructUnknownField(t *testing.T) {
	mustFail(t,
		`struct Point { x: u32 }
fn f(p: Point) -> u32 { return p.z }`,
		"unknown field")
}

func TestStructUnknownType(t *testing.T) {
	mustFail(t,
		`fn f() -> Phantom { return unit }`,
		"unknown type")
}

// ── If / else ─────────────────────────────────────────────────────────────────

func TestIfStmt(t *testing.T) {
	mustCompile(t, `
fn abs(x: i64) -> i64 {
    if x < 0 { return -x }
    return x
}`)
}

func TestIfCondNotBool(t *testing.T) {
	mustFail(t,
		`fn f(n: u32) -> unit { if n { return unit } return unit }`,
		"if condition must be bool")
}

// ── Loop / break ──────────────────────────────────────────────────────────────

func TestLoopBreak(t *testing.T) {
	mustCompile(t, `fn f() -> unit { loop { break } return unit }`)
}

// ── ref<T> field access ───────────────────────────────────────────────────────

func TestRefTransparentFieldAccess(t *testing.T) {
	mustCompile(t, `
struct Pt { x: u32 }
fn get_x(p: ref<Pt>) -> u32 { return p.x }
`)
}

// ── Return type mismatch ──────────────────────────────────────────────────────

func TestReturnTypeMismatch(t *testing.T) {
	mustFail(t,
		`fn f() -> u32 { return true }`,
		"return type mismatch")
}

func TestReturnWrongNumericType(t *testing.T) {
	mustFail(t,
		`fn f(x: u32) -> i32 { return x }`,
		"return type mismatch")
}
