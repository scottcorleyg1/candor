// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg1/candor

package typeck

import (
	"fmt"
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

// ── mut and assignment ────────────────────────────────────────────────────────

func TestLetMut(t *testing.T) {
	mustCompile(t, `fn f() -> unit { let mut x: u32 = 0 return unit }`)
}

func TestAssignToMutable(t *testing.T) {
	mustCompile(t, `fn f() -> unit { let mut x: u32 = 0 x = 42 return unit }`)
}

func TestAssignToImmutable(t *testing.T) {
	mustFail(t,
		`fn f() -> unit { let x: u32 = 0 x = 42 return unit }`,
		"cannot assign to immutable")
}

// ── effects layer ─────────────────────────────────────────────────────────────

func TestPureFnCompiles(t *testing.T) {
	mustCompile(t, `fn add(a: u32, b: u32) -> u32 pure { return a + b }`)
}

func TestEffectsFnCompiles(t *testing.T) {
	mustCompile(t, `fn log(s: str) -> unit effects(io) { print(s) return unit }`)
}

func TestPureCannotCallIo(t *testing.T) {
	mustFail(t, `
fn add(a: u32, b: u32) -> u32 pure {
    print_u32(a)
    return a + b
}`, "pure function cannot call")
}

func TestEffectsSubset(t *testing.T) {
	// effects(io) can call effects(io) — equal set is fine
	mustCompile(t, `
fn log(s: str) -> unit effects(io) { print(s) return unit }
fn run() -> unit effects(io) { log("hi") return unit }
`)
}

func TestEffectsSubsetViolation(t *testing.T) {
	// effects(io) cannot call something that needs net
	mustFail(t, `
fn fetch() -> unit effects(net) { return unit }
fn run() -> unit effects(io) { fetch() return unit }
`, "cannot call")
}

func TestPureCallingPure(t *testing.T) {
	mustCompile(t, `
fn twice(n: u32) -> u32 pure { return n + n }
fn quad(n: u32) -> u32 pure { return twice(twice(n)) }
`)
}

func TestUnannotatedCanCallAnything(t *testing.T) {
	// No annotation = unchecked; can call effects(io) freely
	mustCompile(t, `
fn main() -> unit {
    print_u32(42)
    return unit
}`)
}

// ── effects in control flow ───────────────────────────────────────────────────

func TestPureCannotCallIoInIfBranch(t *testing.T) {
	mustFail(t, `
fn f(cond: bool) -> unit pure {
    if cond { print("bad") }
    return unit
}`, "pure function cannot call")
}

func TestPureCannotCallIoInLoop(t *testing.T) {
	mustFail(t, `
fn f() -> unit pure {
    loop { print("bad") break }
    return unit
}`, "pure function cannot call")
}

func TestEffectsInBothBranches(t *testing.T) {
	mustCompile(t, `
fn log(s: str) -> unit effects(io) {
    if true { print(s) }
    else { print(s) }
    return unit
}`)
}

func TestEffectsOnlyInOneBranch(t *testing.T) {
	// Declaring effects(io) allows IO in only one branch — that's fine
	mustCompile(t, `
fn log_if(cond: bool, s: str) -> unit effects(io) {
    if cond { print(s) }
    return unit
}`)
}

func TestPureCalleeMayCallPure(t *testing.T) {
	mustCompile(t, `
fn id(x: u32) -> u32 pure { return x }
fn wrap(x: u32) -> u32 pure { return id(x) }
`)
}

// ── error recovery ────────────────────────────────────────────────────────────

func TestMultipleErrors(t *testing.T) {
	_, err := compile(`fn f() -> unit {
    let x: bool = 42
    let y: u32 = true
    return unit
}`)
	if err == nil {
		t.Fatal("expected errors")
	}
	msg := err.Error()
	if !strings.Contains(msg, "type mismatch") {
		t.Errorf("expected 'type mismatch' in error, got: %s", msg)
	}
	// Should report both errors (two newlines = two errors reported)
	errCount := strings.Count(msg, "type mismatch")
	if errCount < 2 {
		t.Errorf("expected at least 2 type mismatch errors, got %d in:\n%s", errCount, msg)
	}
}

// ── contracts ─────────────────────────────────────────────────────────────────

func TestRequiresClause(t *testing.T) {
	mustCompile(t, `fn f(x: u32) -> u32 requires x > 0 { return x }`)
}

func TestEnsuresClause(t *testing.T) {
	mustCompile(t, `fn f(x: u32) -> u32 ensures result > 0 { return x + 1 }`)
}

func TestAssertStmt(t *testing.T) {
	mustCompile(t, `fn f(x: u32) -> u32 { assert x > 0 return x }`)
}

func TestRequiresNotBool(t *testing.T) {
	mustFail(t, `fn f(x: u32) -> u32 requires x { return x }`, "contract clause must be bool")
}

func TestAssertNotBool(t *testing.T) {
	mustFail(t, `fn f(x: u32) -> u32 { assert x return x }`, "assert requires bool")
}

// ── struct literals ───────────────────────────────────────────────────────────

func TestStructLiteral(t *testing.T) {
	mustCompile(t, `
struct Point { x: u32, y: u32 }
fn f() -> Point { return Point { x: 3, y: 4 } }`)
}

func TestStructLiteralMissingField(t *testing.T) {
	mustFail(t, `
struct Point { x: u32, y: u32 }
fn f() -> Point { return Point { x: 3 } }`, "missing field")
}

func TestStructLiteralUnknownField(t *testing.T) {
	mustFail(t, `
struct Point { x: u32 }
fn f() -> Point { return Point { x: 1, z: 2 } }`, "unknown field")
}

func TestStructLiteralTypeMismatch(t *testing.T) {
	mustFail(t, `
struct Point { x: u32 }
fn f() -> Point { return Point { x: true } }`, "type mismatch")
}

// ── struct field assignment ───────────────────────────────────────────────────

func TestFieldAssign(t *testing.T) {
	mustCompile(t, `
struct Point { x: u32, y: u32 }
fn f(p: Point) -> unit {
    let mut q: Point = p
    q.x = 10
    return unit
}`)
}

func TestFieldAssignImmutable(t *testing.T) {
	mustFail(t, `
struct Point { x: u32 }
fn f(p: Point) -> unit {
    p.x = 10
    return unit
}`, "cannot assign to field of immutable")
}

func TestFieldAssignUnknownField(t *testing.T) {
	mustFail(t, `
struct Point { x: u32 }
fn f(src: Point) -> unit {
    let mut p: Point = src
    p.z = 10
    return unit
}`, "unknown field")
}

func TestFieldAssignTypeMismatch(t *testing.T) {
	mustFail(t, `
struct Point { x: u32 }
fn f(src: Point) -> unit {
    let mut p: Point = src
    p.x = true
    return unit
}`, "type mismatch")
}

// ── match expression ──────────────────────────────────────────────────────────

func TestMatchOnBool(t *testing.T) {
	mustCompile(t, `fn f(b: bool) -> u32 { return match b { true => 1 false => 2 } }`)
}

func TestMatchOnOption(t *testing.T) {
	mustCompile(t, `
fn f(x: option<u32>) -> u32 {
    return match x {
        some(v) => v
        none    => 0
    }
}`)
}

// ── CheckProgram helpers and module enforcement tests ─────────────────────────

// compileProgram parses each src string as a separate file and runs CheckProgram.
func compileProgram(srcs ...string) (*Result, error) {
	files := make([]*parser.File, len(srcs))
	for i, src := range srcs {
		name := fmt.Sprintf("<test%d>", i)
		tokens, err := lexer.Tokenize(name, src)
		if err != nil {
			return nil, err
		}
		f, err := parser.Parse(name, tokens)
		if err != nil {
			return nil, err
		}
		files[i] = f
	}
	return CheckProgram(files)
}

func mustCompileProgram(t *testing.T, srcs ...string) {
	t.Helper()
	if _, err := compileProgram(srcs...); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func mustFailProgram(t *testing.T, want string, srcs ...string) {
	t.Helper()
	_, err := compileProgram(srcs...)
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got: %v", want, err)
	}
}

func TestCheckProgramSingleFile(t *testing.T) {
	mustCompileProgram(t, `
fn add(a: u32, b: u32) -> u32 { return a + b }
fn main() -> unit { let x = add(1, 2) return unit }`)
}

func TestCheckProgramMultiFileNoModules(t *testing.T) {
	mustCompileProgram(t,
		`fn add(a: u32, b: u32) -> u32 { return a + b }`,
		`fn main() -> unit { let x = add(1, 2) return unit }`,
	)
}

func TestModuleEnforcementCrossModuleFnBlocked(t *testing.T) {
	mustFailProgram(t, `"greet" is from module "greet"`,
		`module greet
fn greet() -> unit { return unit }`,
		`module app
fn main() -> unit { greet() return unit }`,
	)
}

func TestModuleEnforcementCrossModuleFnAllowed(t *testing.T) {
	mustCompileProgram(t,
		`module greet
fn greet() -> unit { return unit }`,
		`module app
use greet::greet
fn main() -> unit { greet() return unit }`,
	)
}

func TestModuleEnforcementSameModuleAlwaysVisible(t *testing.T) {
	mustCompileProgram(t,
		`module math
fn add(a: u32, b: u32) -> u32 { return a + b }`,
		`module math
fn double(x: u32) -> u32 { return add(x, x) }`,
	)
}

func TestModuleEnforcementRootNamespaceVisible(t *testing.T) {
	mustCompileProgram(t,
		`fn helper() -> u32 { return 42 }`,
		`module app
fn main() -> unit { let x = helper() return unit }`,
	)
}

func TestModuleEnforcementCrossModuleStructBlocked(t *testing.T) {
	mustFailProgram(t, `"Point" is from module "geo"`,
		`module geo
struct Point { x: i64, y: i64 }`,
		`module app
fn f() -> unit { let p = Point { x: 1, y: 2 } return unit }`,
	)
}

func TestModuleEnforcementCrossModuleStructAllowed(t *testing.T) {
	mustCompileProgram(t,
		`module geo
struct Point { x: i64, y: i64 }`,
		`module app
use geo::Point
fn f() -> unit { let p = Point { x: 1, y: 2 } return unit }`,
	)
}

func TestModuleEnforcementBadUseModule(t *testing.T) {
	mustFailProgram(t, `no symbol "Foo" found`,
		`module app
use nonexistent::Foo
fn main() -> unit { return unit }`,
	)
}

func TestModuleEnforcementBadUseSymbol(t *testing.T) {
	mustFailProgram(t, `no symbol "NotAPoint" found`,
		`module geo
struct Point { x: i64, y: i64 }`,
		`module app
use geo::NotAPoint
fn f() -> unit { return unit }`,
	)
}

func TestModuleEnforcementUseRequiresPath(t *testing.T) {
	mustFailProgram(t, "must have the form",
		`module app
use justname
fn main() -> unit { return unit }`,
	)
}

// ── module / use declarations ─────────────────────────────────────────────────

func TestModuleDeclCompiles(t *testing.T) {
	mustCompile(t, `
module mylib
fn add(a: u32, b: u32) -> u32 { return a + b }`)
}

func TestUseDeclCompiles(t *testing.T) {
	mustCompile(t, `
module app
use mylib
fn main() -> unit { return unit }`)
}

func TestUseDeclPathCompiles(t *testing.T) {
	mustCompile(t, `
use mylib::Point
fn f() -> unit { return unit }`)
}

// ── for loops and vec builtins ────────────────────────────────────────────────

func TestForLoop(t *testing.T) {
	mustCompile(t, `
fn sum(v: vec<u32>) -> u32 {
    let mut acc: u32 = 0
    for x in v { acc = acc + x }
    return acc
}`)
}

func TestForLoopNotVec(t *testing.T) {
	mustFail(t, `fn f(x: u32) -> unit { for i in x { } return unit }`, "for..in requires vec")
}

func TestVecNew(t *testing.T) {
	mustCompile(t, `
fn f() -> unit {
    let mut v: vec<u32> = vec_new()
    return unit
}`)
}

func TestVecNewNoAnnotation(t *testing.T) {
	mustFail(t, `fn f() -> unit { let v = vec_new() return unit }`, "requires a type annotation")
}

func TestVecPush(t *testing.T) {
	mustCompile(t, `
fn f() -> unit {
    let mut v: vec<u32> = vec_new()
    vec_push(v, 42)
    return unit
}`)
}

func TestVecPushTypeMismatch(t *testing.T) {
	mustFail(t, `
fn f() -> unit {
    let mut v: vec<u32> = vec_new()
    vec_push(v, true)
    return unit
}`, "does not match vec element type")
}

func TestVecLen(t *testing.T) {
	mustCompile(t, `
fn f(v: vec<u32>) -> u64 {
    return vec_len(v)
}`)
}

func TestVecLenNotVec(t *testing.T) {
	mustFail(t, `fn f(x: u32) -> u64 { return vec_len(x) }`, "requires vec<T>")
}

// ── first-class functions (non-capturing) ─────────────────────────────────────

func TestFnAsArgument(t *testing.T) {
	mustCompile(t, `
fn double(x: i64) -> i64 { return x * 2 }
fn apply(f: fn(i64) -> i64, x: i64) -> i64 { return f(x) }
fn main() -> unit {
    let result = apply(double, 5)
    return unit
}`)
}

func TestFnAsVariable(t *testing.T) {
	mustCompile(t, `
fn inc(x: i64) -> i64 { return x + 1 }
fn main() -> unit {
    let f: fn(i64) -> i64 = inc
    let y = f(10)
    return unit
}`)
}

func TestFnAsReturnValue(t *testing.T) {
	mustCompile(t, `
fn double(x: i64) -> i64 { return x * 2 }
fn get_double() -> fn(i64) -> i64 { return double }
fn main() -> unit {
    let f = get_double()
    let y = f(7)
    return unit
}`)
}

func TestFnTypeMismatch(t *testing.T) {
	mustFail(t, `
fn add(a: i64, b: i64) -> i64 { return a + b }
fn apply(f: fn(i64) -> i64, x: i64) -> i64 { return f(x) }
fn main() -> unit {
    let result = apply(add, 5)
    return unit
}`, "cannot use")
}

func TestFnCallThroughVariable(t *testing.T) {
	mustCompile(t, `
fn square(x: i64) -> i64 { return x * x }
fn main() -> unit {
    let f: fn(i64) -> i64 = square
    let a = f(3)
    let b = f(4)
    return unit
}`)
}

func TestFnZeroArgType(t *testing.T) {
	mustCompile(t, `
fn answer() -> i64 { return 42 }
fn call(f: fn() -> i64) -> i64 { return f() }
fn main() -> unit {
    let x = call(answer)
    return unit
}`)
}

// ── Integer / literal pattern matching ───────────────────────────────────────

func TestMatchIntLiteral(t *testing.T) {
	mustCompile(t, `
fn describe(n: i64) -> i64 {
    return match n {
        0 => 10
        1 => 20
        _ => 30
    }
}
fn main() -> unit { return unit }`)
}

func TestMatchIntMismatch(t *testing.T) {
	mustFail(t, `
fn main() -> unit {
    let n: i64 = 5
    let x = match n {
        true => 1
        _ => 0
    }
    return unit
}`, "incompatible")
}

func TestMatchStringLiteral(t *testing.T) {
	mustCompile(t, `
fn greet(s: str) -> i64 {
    return match s {
        "hello" => 1
        "bye"   => 2
        _       => 0
    }
}
fn main() -> unit { return unit }`)
}

func TestMatchNegativeInt(t *testing.T) {
	mustCompile(t, `
fn sign(n: i64) -> i64 {
    return match n {
        -1 => 0,
        0  => 1,
        _  => 2
    }
}
fn main() -> unit { return unit }`)
}

// ── stdin I/O builtins ────────────────────────────────────────────────────────

func TestReadLineType(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let s: str = read_line()
    return unit
}`)
}

func TestReadIntType(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let n: i64 = read_int()
    return unit
}`)
}

func TestReadF64Type(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let x: f64 = read_f64()
    return unit
}`)
}

func TestReadLineUsedInExpr(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let s = read_line()
    print(s)
    return unit
}`)
}

func TestReadIntUsedInArith(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let n = read_int()
    let doubled = n * 2
    print_int(doubled)
    return unit
}`)
}

// ── try_read_* / BreakExpr / vec indexing ─────────────────────────────────────

func TestTryReadIntType(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let n: option<i64> = try_read_int()
    return unit
}`)
}

func TestTryReadLineType(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let s: option<str> = try_read_line()
    return unit
}`)
}

func TestBreakExprInMust(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    loop {
        let n = try_read_int()
        let x = n must {
            some(v) => v
            none    => break
        }
        print_int(x)
    }
    return unit
}`)
}

func TestVecIndexing(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let mut v: vec<i64> = vec_new()
    vec_push(v, 10)
    vec_push(v, 20)
    let x: i64 = v[0]
    print_int(x)
    return unit
}`)
}

// ── String operations ─────────────────────────────────────────────────────────

func TestStrLen(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let n: i64 = str_len("hello")
    return unit
}`)
}

func TestStrConcat(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let s: str = str_concat("hello", " world")
    print(s)
    return unit
}`)
}

func TestStrEq(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let b: bool = str_eq("foo", "foo")
    print_bool(b)
    return unit
}`)
}

func TestIntToStr(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let s: str = int_to_str(42)
    print(s)
    return unit
}`)
}

func TestStrToInt(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let r: result<i64, str> = str_to_int("42")
    let n = r must {
        ok(v)  => v
        err(_) => 0
    }
    print_int(n)
    return unit
}`)
}

func TestStrEqOperator(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let s = read_line()
    if s == "quit" { return unit }
    print(s)
    return unit
}`)
}

func TestStrToIntBadInput(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let r = str_to_int("not_a_number")
    let n = r must {
        ok(v)  => v
        err(_) => -1
    }
    print_int(n)
    return unit
}`)
}

// ── Enum tests ────────────────────────────────────────────────────────────────

func TestEnumUnitVariants(t *testing.T) {
	mustCompile(t, `
enum Direction { North, South, East, West }
fn main() -> unit {
    let d: Direction = Direction::North
    return unit
}`)
}

func TestEnumDataVariant(t *testing.T) {
	mustCompile(t, `
enum Shape {
    Circle(f64),
    Rect(f64, f64),
    Point,
}
fn area(s: Shape) -> f64 {
    return match s {
        Shape::Circle(r) => r * r * 3.14,
        Shape::Rect(w, h) => w * h,
        Shape::Point => 0.0,
    }
}
fn main() -> unit {
    let c = Shape::Circle(2.0)
    let a = area(c)
    return unit
}`)
}

func TestEnumMatchReturnType(t *testing.T) {
	mustCompile(t, `
enum Msg { Quit, Text(str) }
fn describe(m: Msg) -> str {
    return match m {
        Msg::Quit   => "quit",
        Msg::Text(s) => s,
    }
}
fn main() -> unit { return unit }`)
}

func TestEnumUnknownVariantError(t *testing.T) {
	mustFail(t, `
enum Color { Red, Green }
fn main() -> unit {
    let c = Color::Blue
    return unit
}`, "no variant")
}

func TestEnumWrongFieldCountError(t *testing.T) {
	mustFail(t, `
enum Shape { Circle(f64), Point }
fn main() -> unit {
    let s = Shape::Circle(1.0, 2.0)
    return unit
}`, "argument count")
}

// ── File I/O builtins ─────────────────────────────────────────────────────────

func TestReadFileTypeChecks(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let r = read_file("test.txt")
    return unit
}`)
}

func TestWriteFileTypeChecks(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let r = write_file("test.txt", "hello")
    return unit
}`)
}

func TestAppendFileTypeChecks(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let r = append_file("test.txt", "hello")
    return unit
}`)
}

func TestReadFileReturnType(t *testing.T) {
	// read_file returns result<str,str> — ok branch must yield str
	mustCompile(t, `
fn main() -> unit {
    let r = read_file("test.txt")
    match r {
        ok(content) => print(content),
        err(msg)    => print(msg),
    }
    return unit
}`)
}

func TestWriteFileReturnType(t *testing.T) {
	// write_file returns result<unit,str> — err branch must yield str
	mustCompile(t, `
fn main() -> unit {
    let r = write_file("test.txt", "hello")
    match r {
        ok(_u)   => print("ok"),
        err(msg) => print(msg),
    }
    return unit
}`)
}

func TestReadFileWrongArgCount(t *testing.T) {
	mustFail(t, `
fn main() -> unit {
    let r = read_file("a", "b")
    return unit
}`, "argument count")
}

func TestWriteFileWrongArgCount(t *testing.T) {
	mustFail(t, `
fn main() -> unit {
    let r = write_file("a")
    return unit
}`, "argument count")
}

// ── Ownership Tier 1 ──────────────────────────────────────────────────────────

func TestRefParam(t *testing.T) {
	mustCompile(t, `
struct Point { x: i64, y: i64 }
fn magnitude_sq(p: ref<Point>) -> i64 {
    return p.x * p.x + p.y * p.y
}
fn main() -> unit {
    let p = Point { x: 3, y: 4 }
    let _m = magnitude_sq(&p)
    return unit
}`)
}

func TestRefmutParam(t *testing.T) {
	mustCompile(t, `
struct Point { x: i64, y: i64 }
fn scale(p: refmut<Point>, factor: i64) -> unit {
    p.x = p.x * factor
    p.y = p.y * factor
    return unit
}
fn main() -> unit {
    let mut p = Point { x: 3, y: 4 }
    scale(refmut(p), 2)
    return unit
}`)
}

func TestMoveCall(t *testing.T) {
	mustCompile(t, `
fn consume(x: i64) -> i64 { return x + 1 }
fn main() -> unit {
    let n: i64 = 42
    let _r = consume(move(n))
    return unit
}`)
}

func TestDerefRef(t *testing.T) {
	mustCompile(t, `
fn main() -> unit {
    let x: i64 = 10
    let r = &x
    let _v: i64 = *r
    return unit
}`)
}

func TestDerefNonRef(t *testing.T) {
	mustFail(t, `
fn main() -> unit {
    let x: i64 = 10
    let _v = *x
    return unit
}`, "ref<T>")
}

// ── Comptime evaluation ───────────────────────────────────────────────────────

func TestComptimeSimple(t *testing.T) {
	src := `
fn square(x: i64) -> i64 effects [] { return x * x }
fn main() -> unit {
    let _s = square(7)
    return unit
}`
	res := mustCompile(t, src)
	// Find the CallExpr for square(7) and check it was evaluated.
	found := false
	for _, v := range res.ComptimeValues {
		if i, ok := v.(int64); ok && i == 49 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected comptime value 49, got %v", res.ComptimeValues)
	}
}

func TestComptimeBool(t *testing.T) {
	src := `
fn is_positive(x: i64) -> bool effects [] { return x > 0 }
fn main() -> unit {
    let _b = is_positive(5)
    return unit
}`
	res := mustCompile(t, src)
	found := false
	for _, v := range res.ComptimeValues {
		if b, ok := v.(bool); ok && b {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected comptime value true")
	}
}

func TestComptimeNotEvaluatedWithRuntimeArg(t *testing.T) {
	// When an argument is not a literal, the call must NOT be evaluated.
	src := `
fn square(x: i64) -> i64 effects [] { return x * x }
fn main() -> unit {
    let n: i64 = 5
    let _s = square(n)
    return unit
}`
	res := mustCompile(t, src)
	if len(res.ComptimeValues) != 0 {
		t.Fatalf("expected no comptime values for runtime arg, got %v", res.ComptimeValues)
	}
}

func TestComptimeChained(t *testing.T) {
	// Pure calls with pure-call results as args should chain.
	src := `
fn double(x: i64) -> i64 effects [] { return x * 2 }
fn quad(x: i64) -> i64 effects [] { return double(double(x)) }
fn main() -> unit {
    let _q = quad(3)
    return unit
}`
	res := mustCompile(t, src)
	found := false
	for _, v := range res.ComptimeValues {
		if i, ok := v.(int64); ok && i == 12 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected comptime value 12, got %v", res.ComptimeValues)
	}
}
