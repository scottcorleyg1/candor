// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg1/candor

package emit_c

import (
	"strings"
	"testing"

	"github.com/scottcorleyg1/candor/compiler/lexer"
	"github.com/scottcorleyg1/candor/compiler/parser"
	"github.com/scottcorleyg1/candor/compiler/typeck"
)

// pipeline runs src through lex → parse → typeck → emit_c.
func pipeline(t *testing.T, src string) string {
	t.Helper()
	tokens, err := lexer.Tokenize("<test>", src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	file, err := parser.Parse("<test>", tokens)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	res, err := typeck.Check(file)
	if err != nil {
		t.Fatalf("typeck: %v", err)
	}
	out, err := Emit(file, res)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return out
}

func assertContains(t *testing.T, out, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Errorf("expected output to contain:\n  %q\ngot:\n%s", want, out)
	}
}

func assertNotContains(t *testing.T, out, want string) {
	t.Helper()
	if strings.Contains(out, want) {
		t.Errorf("expected output NOT to contain:\n  %q\ngot:\n%s", want, out)
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
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)

	assertContains(t, out, "#include <stdint.h>")
	assertContains(t, out, "uint32_t add(")
	assertContains(t, out, "int main(void)")
	assertContains(t, out, "return (a + b)")
	assertContains(t, out, "uint32_t x = add(1, 2)")
	// return unit in main → return 0
	assertContains(t, out, "return 0;")
	// implicit return 0 at end of main
	assertNotContains(t, out, "return unit")
}

// ── Type mapping ──────────────────────────────────────────────────────────────

func TestI32Mapping(t *testing.T) {
	out := pipeline(t, `fn f(x: i32) -> i32 { return x }`)
	assertContains(t, out, "int32_t f(")
	assertContains(t, out, "int32_t x")
}

func TestBoolMapping(t *testing.T) {
	out := pipeline(t, `fn f() -> bool { return true }`)
	assertContains(t, out, "int f(")
	assertContains(t, out, "return 1;")
}

func TestBoolFalse(t *testing.T) {
	out := pipeline(t, `fn f() -> bool { return false }`)
	assertContains(t, out, "return 0;")
}

func TestStrMapping(t *testing.T) {
	out := pipeline(t, `fn f() -> str { return "hello" }`)
	assertContains(t, out, "const char* f(")
}

func TestF64Mapping(t *testing.T) {
	out := pipeline(t, `fn f() -> f64 { return 1.0 }`)
	assertContains(t, out, "double f(")
}

func TestF32Mapping(t *testing.T) {
	out := pipeline(t, `fn f() -> f32 { return 1.0 }`)
	assertContains(t, out, "float f(")
}

func TestUnitReturnIsVoid(t *testing.T) {
	out := pipeline(t, `fn f() -> unit { return unit }`)
	assertContains(t, out, "void f(")
}

// ── Struct emission ───────────────────────────────────────────────────────────

func TestStructEmission(t *testing.T) {
	out := pipeline(t, `
struct Point { x: u32, y: u32 }
fn sum(p: Point) -> u32 { return p.x + p.y }
`)
	t.Logf("emitted C:\n%s", out)
	assertContains(t, out, "typedef struct Point {")
	assertContains(t, out, "uint32_t x;")
	assertContains(t, out, "uint32_t y;")
	assertContains(t, out, "} Point;")
	assertContains(t, out, "uint32_t sum(")
	assertContains(t, out, "Point p")
	assertContains(t, out, "p.x")
	assertContains(t, out, "p.y")
}

// ── ref<T> field access uses -> ───────────────────────────────────────────────

func TestRefFieldArrow(t *testing.T) {
	out := pipeline(t, `
struct Pt { x: u32 }
fn get_x(p: ref<Pt>) -> u32 { return p.x }
`)
	t.Logf("emitted C:\n%s", out)
	assertContains(t, out, "p->x")
}

// ── Arithmetic operators ──────────────────────────────────────────────────────

func TestArithmeticOps(t *testing.T) {
	src := `
fn f(a: i64, b: i64) -> i64 {
    let s = a + b
    let d = a - b
    let p = a * b
    let q = a / b
    let r = a % b
    return s
}`
	out := pipeline(t, src)
	assertContains(t, out, "(a + b)")
	assertContains(t, out, "(a - b)")
	assertContains(t, out, "(a * b)")
	assertContains(t, out, "(a / b)")
	assertContains(t, out, "(a % b)")
}

// ── Boolean operators map to C ────────────────────────────────────────────────

func TestAndOrNot(t *testing.T) {
	src := `
fn f(a: bool, b: bool) -> bool { return a and b }
fn g(a: bool, b: bool) -> bool { return a or b }
fn h(a: bool) -> bool { return not a }
`
	out := pipeline(t, src)
	assertContains(t, out, "(a && b)")
	assertContains(t, out, "(a || b)")
	assertContains(t, out, "(!a)")
}

// ── Comparison operators ──────────────────────────────────────────────────────

func TestComparisons(t *testing.T) {
	src := `
fn eq(a: u32, b: u32) -> bool { return a == b }
fn ne(a: u32, b: u32) -> bool { return a != b }
fn lt(a: u32, b: u32) -> bool { return a < b }
fn gt(a: u32, b: u32) -> bool { return a > b }
`
	out := pipeline(t, src)
	assertContains(t, out, "(a == b)")
	assertContains(t, out, "(a != b)")
	assertContains(t, out, "(a < b)")
	assertContains(t, out, "(a > b)")
}

// ── if / else ─────────────────────────────────────────────────────────────────

func TestIfElse(t *testing.T) {
	src := `
fn abs(x: i64) -> i64 {
    if x < 0 { return -x }
    return x
}`
	out := pipeline(t, src)
	assertContains(t, out, "if ((x < 0))")
	assertContains(t, out, "return (-x);")
}

// ── loop / break ──────────────────────────────────────────────────────────────

func TestLoopBreak(t *testing.T) {
	src := `fn f() -> unit { loop { break } return unit }`
	out := pipeline(t, src)
	assertContains(t, out, "for (;;)")
	assertContains(t, out, "break;")
}

// ── Forward declarations ──────────────────────────────────────────────────────

func TestForwardDeclarations(t *testing.T) {
	src := `
fn foo() -> u32 { return 1 }
fn bar() -> u32 { return foo() }
`
	out := pipeline(t, src)
	// Forward decl must appear before the definition
	fwdIdx := strings.Index(out, "uint32_t foo(")
	defIdx := strings.LastIndex(out, "uint32_t foo(")
	if fwdIdx == defIdx {
		t.Error("expected both a forward declaration and a definition for foo")
	}
}

// ── Includes ──────────────────────────────────────────────────────────────────

func TestIncludes(t *testing.T) {
	out := pipeline(t, `fn f() -> unit { return unit }`)
	assertContains(t, out, "#include <stdint.h>")
	assertContains(t, out, "#include <stdio.h>")
}
