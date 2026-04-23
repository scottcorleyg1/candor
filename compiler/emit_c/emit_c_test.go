// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/candor-core/candor

package emit_c

import (
	"strings"
	"testing"

	"github.com/candor-core/candor/compiler/lexer"
	"github.com/candor-core/candor/compiler/parser"
	"github.com/candor-core/candor/compiler/typeck"
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
	assertContains(t, out, "int main(int argc, char** argv)")
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
	assertContains(t, out, "typedef struct Point Point;")
	assertContains(t, out, "struct Point {")
	assertContains(t, out, "uint32_t x;")
	assertContains(t, out, "uint32_t y;")
	assertContains(t, out, "};")
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

// ── Built-in print functions ──────────────────────────────────────────────────

func TestPrintStr(t *testing.T) {
	out := pipeline(t, `fn main() -> unit { print("hello") return unit }`)
	assertContains(t, out, `printf("%s\n"`)
	assertContains(t, out, `"hello"`)
}

func TestPrintInt(t *testing.T) {
	out := pipeline(t, `fn main() -> unit { print_int(42) return unit }`)
	assertContains(t, out, `printf("%lld\n"`)
	assertContains(t, out, `(long long)`)
}

func TestPrintU32(t *testing.T) {
	out := pipeline(t, `fn main() -> unit { print_u32(99) return unit }`)
	assertContains(t, out, `printf("%u\n"`)
}

func TestPrintBool(t *testing.T) {
	out := pipeline(t, `fn main() -> unit { print_bool(true) return unit }`)
	assertContains(t, out, `"true"`)
	assertContains(t, out, `"false"`)
}

func TestPrintF64(t *testing.T) {
	out := pipeline(t, `fn main() -> unit { print_f64(3.14) return unit }`)
	assertContains(t, out, `printf("%f\n"`)
}

// ── Includes ──────────────────────────────────────────────────────────────────

func TestIncludes(t *testing.T) {
	out := pipeline(t, `fn f() -> unit { return unit }`)
	assertContains(t, out, "#include <stdint.h>")
	assertContains(t, out, "#include <stdio.h>")
}

// ── mut and assignment ────────────────────────────────────────────────────────

func TestMutAndAssign(t *testing.T) {
	src := `fn f() -> unit { let mut x: u32 = 0 x = 42 return unit }`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	assertContains(t, out, "uint32_t x = 0")
	assertContains(t, out, "x = 42")
}

// ── match expression ──────────────────────────────────────────────────────────

func TestMatchOnBool(t *testing.T) {
	src := `fn f(b: bool) -> u32 { return match b { true => 1 false => 2 } }`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	assertContains(t, out, "__extension__")
	// should have an if condition for true
	assertContains(t, out, "if (")
}

// TestMatchImplicitTailReturn verifies that a match expression used as the last
// (implicit) return value of a function emits "return (__extension__ ({...}))"
// and not a bare statement. This was a silent bug: the value was computed but
// discarded, causing the function to return garbage.
// TestMatchImplicitTailReturn verifies that a match expression used as the last
// (implicit) return value of a function emits "return (__extension__ ({...}))"
// and not a bare statement. This was a silent bug: the value was computed but
// discarded, causing the function to return garbage.
// TestMatchImplicitTailReturn verifies that a match expression used as the last
// (implicit) return value of a function emits "return (__extension__ ({...}))"
// and not a bare statement. This was a silent bug: the value was computed but
// discarded, causing the function to return garbage.
//
// Note: arm bodies must have concrete types. Integer literals in tail-match
// arms require a typeck contextual-typing fix (tracked separately). Here we
// use bool arms which have concrete types without inference.
func TestMatchImplicitTailReturn(t *testing.T) {
	src := `fn f(b: bool) -> bool { match b { true => false   false => true } }`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	// Must emit a return, not a bare statement expression.
	assertContains(t, out, "return (__extension__")
}

// TestMustImplicitTailReturn verifies the same fix for must expressions.
func TestMustImplicitTailReturn(t *testing.T) {
	src := `fn unwrap(x: option<u32>) -> u32 { x must { some(v) => v   none => 0 } }`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	assertContains(t, out, "return (__extension__")
}

// TestMatchTailWithEnsures verifies that an implicit tail match still emits
// the ensures assert wrapper correctly.
func TestMatchTailWithEnsures(t *testing.T) {
	src := `fn f(x: option<u32>) -> u32 ensures result >= 0 { x must { some(v) => v   none => 0 } }`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	assertContains(t, out, "_cnd_result")
	assertContains(t, out, "assert(")
	assertContains(t, out, "return _cnd_result")
}

// ── effects annotations ───────────────────────────────────────────────────────

func TestPureComment(t *testing.T) {
	out := pipeline(t, `fn add(a: u32, b: u32) -> u32 pure { return a + b }`)
	assertContains(t, out, "/* pure */")
}

func TestEffectsComment(t *testing.T) {
	out := pipeline(t, `fn log(s: str) -> unit effects(io) { print(s) return unit }`)
	assertContains(t, out, "/* effects: io */")
}

func TestEffectsMultipleComment(t *testing.T) {
	out := pipeline(t, `fn fetch(url: str) -> str effects(io, net) { return url }`)
	assertContains(t, out, "/* effects: io, net */")
}

func TestNoAnnotationNoComment(t *testing.T) {
	out := pipeline(t, `fn f() -> unit { return unit }`)
	assertNotContains(t, out, "/* pure */")
	assertNotContains(t, out, "/* effects")
}

// ── contracts ─────────────────────────────────────────────────────────────────

func TestRequiresEmission(t *testing.T) {
	out := pipeline(t, `fn f(x: u32) -> u32 requires x > 0 { return x }`)
	assertContains(t, out, "#include <assert.h>")
	assertContains(t, out, "assert((x > 0))")
}

func TestEnsuresEmission(t *testing.T) {
	out := pipeline(t, `fn f(x: u32) -> u32 ensures result > 0 { return x + 1 }`)
	assertContains(t, out, "_cnd_result")
	assertContains(t, out, "assert((_cnd_result > 0))")
}

func TestAssertStmtEmission(t *testing.T) {
	out := pipeline(t, `fn f(x: u32) -> u32 { assert x > 0 return x }`)
	assertContains(t, out, "assert((x > 0))")
}

// ── struct literals ───────────────────────────────────────────────────────────

func TestStructLiteralEmission(t *testing.T) {
	src := `
struct Point { x: u32, y: u32 }
fn f() -> Point { return Point { x: 3, y: 4 } }
`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	assertContains(t, out, ".x = 3")
	assertContains(t, out, ".y = 4")
	assertContains(t, out, "(Point){")
}

// ── field assignment ──────────────────────────────────────────────────────────

func TestFieldAssign(t *testing.T) {
	src := `
struct Point { x: u32, y: u32 }
fn f(p: Point) -> u32 {
    let mut q: Point = p
    q.x = 99
    return q.x
}`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	assertContains(t, out, "q.x = 99")
}

func TestFieldAssignRef(t *testing.T) {
	src := `
struct Point { x: u32 }
fn set_x(p: ref<Point>) -> unit {
    p.x = 7
    return unit
}`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	assertContains(t, out, "p->x = 7")
}

func TestMatchOnOption(t *testing.T) {
	src := `
fn f(x: option<u32>) -> u32 {
    return match x {
        some(v) => v
        none    => 0
    }
}`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	assertContains(t, out, "!= NULL")
	assertContains(t, out, "== NULL")
}

// ── set<T> emission tests ─────────────────────────────────────────────────────

func TestSetTypedef(t *testing.T) {
	src := `
fn main() -> unit {
	let mut s: set<i64> = set_new()
	set_add(s, 42)
	return unit
}
`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	assertContains(t, out, "_CndSet_int64_t")
	assertContains(t, out, "_CndSetEntry_int64_t")
	assertContains(t, out, "_cnd_set_new_int64_t()")
	assertContains(t, out, "_cnd_set_add_int64_t")
}

func TestSetContainsEmit(t *testing.T) {
	src := `
fn main() -> unit {
	let mut s: set<i64> = set_new()
	set_add(s, 1)
	if set_contains(s, 1) {
		print("yes")
	}
	return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_cnd_set_contains_int64_t")
}

func TestSetLenEmit(t *testing.T) {
	src := `
fn main() -> unit {
	let mut s: set<i64> = set_new()
	let n: u64 = set_len(s)
	return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "._len")
}

func TestSetStrEmit(t *testing.T) {
	src := `
fn main() -> unit {
	let mut s: set<str> = set_new()
	set_add(s, "hello")
	return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_CndSet_const_charptr")
	assertContains(t, out, "strcmp")
}

// ── lambda emission tests ─────────────────────────────────────────────────────

func TestLambdaEmit(t *testing.T) {
	src := `
fn main() -> unit {
	let f: fn(i64) -> i64 = fn(x: i64) -> i64 { return x + 1 }
	return unit
}
`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	assertContains(t, out, "_cnd_lambda_1")
	assertContains(t, out, "static int64_t _cnd_lambda_1_impl")
	assertContains(t, out, "return (x + 1)")
}

func TestLambdaCallEmit(t *testing.T) {
	src := `
fn apply(f: fn(i64) -> i64, x: i64) -> i64 { return f(x) }
fn main() -> unit {
	let result: i64 = apply(fn(n: i64) -> i64 { return n * 2 }, 5)
	return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_cnd_lambda_1")
	// fat-pointer call: f._fn(x, f._env)
	assertContains(t, out, "f._fn(x, f._env)")
	// lambda passed as fat-pointer struct literal
	assertContains(t, out, "_cnd_lambda_1_impl")
}

func TestLambdaCapture(t *testing.T) {
	src := `
fn apply(f: fn(i64) -> i64, x: i64) -> i64 { return f(x) }
fn main() -> unit {
	let base: i64 = 10
	let adder: fn(i64) -> i64 = fn(x: i64) -> i64 { return x + base }
	let r: i64 = apply(adder, 5)
	return unit
}
`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	// Capture struct emitted
	assertContains(t, out, "_cnd_lambda_1_env")
	// Maker function emitted
	assertContains(t, out, "_cnd_lambda_1_make")
	// Env unpacked in impl
	assertContains(t, out, "_e->base")
}

func TestLambdaReturnUnit(t *testing.T) {
	src := `
fn main() -> unit {
	let f: fn(i64) -> unit = fn(x: i64) -> unit { return unit }
	return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "static void _cnd_lambda_1_impl")
}

// ── old() emission tests ─────────────────────────────────────────────────────

func TestOldEmit(t *testing.T) {
	src := `
fn add_one(x: i64) -> i64
    ensures result == old(x) + 1
{
    return x + 1
}
`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	// old(x) should be captured at function entry
	assertContains(t, out, "int64_t _cnd")
	// the ensures assert uses the captured variable, not x directly
	assertContains(t, out, "_cnd_result")
	assertContains(t, out, "assert(")
}

func TestOldMultiple(t *testing.T) {
	src := `
fn add(x: i64, y: i64) -> i64
    ensures result == old(x) + old(y)
{
    return x + y
}
`
	out := pipeline(t, src)
	// Two distinct temp vars should be emitted for the two old() calls
	assertContains(t, out, "_cnd1")
	assertContains(t, out, "_cnd2")
}

func TestOldOutsideEnsures(t *testing.T) {
	src := `fn f(x: i64) -> i64 requires old(x) > 0 { return x }`
	tokens, _ := lexer.Tokenize("<test>", src)
	file, _ := parser.Parse("<test>", tokens)
	_, err := typeck.Check(file)
	if err == nil {
		t.Fatal("expected error for old() in requires, got nil")
	}
}

// ── generic function tests ────────────────────────────────────────────────────

func TestGenericIdentity(t *testing.T) {
	src := `
fn identity<T>(x: T) -> T { return x }
fn main() -> unit {
	let a: i64 = identity(42)
	let b: bool = identity(true)
	return unit
}
`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	// Mangled names use Candor type names (i64, bool), not C names.
	assertContains(t, out, "identity__i64")
	assertContains(t, out, "identity__bool")
}

func TestGenericMap(t *testing.T) {
	src := `
fn map_fn<T>(v: vec<T>, f: fn(T) -> T) -> vec<T> {
	let result: vec<T> = vec_new()
	for x in v {
		vec_push(result, f(x))
	}
	return result
}
fn double(x: i64) -> i64 { return x * 2 }
fn main() -> unit {
	let v: vec<i64> = vec_new()
	vec_push(v, 1)
	let d: fn(i64) -> i64 = fn(x: i64) -> i64 { return x * 2 }
	let result: vec<i64> = map_fn(v, d)
	return unit
}
`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	assertContains(t, out, "map_fn__i64")
}

// ── New feature emit tests ────────────────────────────────────────────────────

func TestWhileLoopEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let mut i: i64 = 0
    while i < 3 { i = i + 1 }
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "while (")
}

func TestConstDeclEmit(t *testing.T) {
	src := `
const MAX: i64 = 100
fn main() -> unit { return unit }
`
	out := pipeline(t, src)
	assertContains(t, out, "static const int64_t MAX = 100")
}

func TestCastExprEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let x: i32 = 5
    let y: i64 = x as i64
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "((int64_t)(")
}

func TestVecLiteralEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let v: vec<i64> = [1, 2, 3]
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_CndVec_int64_t")
}

func TestImplMethodEmit(t *testing.T) {
	src := `
struct Point { x: i64, }
impl Point {
    fn getx(self: Point) -> i64 { return self.x }
}
fn main() -> unit {
    let p = Point { x: 7 }
    let v = p.getx()
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "int64_t Point_getx(")
	assertContains(t, out, "Point_getx(p")
}

func TestMapIndexAssignEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let mut m: map<i64, i64> = map_new()
    map_insert(m, 1, 10)
    m[1] = 42
    return unit
}
`
	out := pipeline(t, src)
	// m[1] = 42 should desugar to map_insert
	assertContains(t, out, "_cnd_map_insert_")
}

func TestTupleDestructureEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let t: (i64, i64) = (1, 2)
    let (a, b) = t
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "._0")
	assertContains(t, out, "._1")
}

func TestStructUpdateEmit(t *testing.T) {
	src := `
struct Point { x: i64, y: i64, }
fn main() -> unit {
    let p = Point { x: 1, y: 2 }
    let q = Point { ..p, x: 10 }
    return unit
}
`
	out := pipeline(t, src)
	// struct update emits a GNU statement expression with a temp copy
	assertContains(t, out, ".x = 10")
	assertContains(t, out, "Point ")
}

func TestClosureByRefEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let mut count: i64 = 0
    let inc = fn() -> unit { count += 1 }
    return unit
}
`
	out := pipeline(t, src)
	// by-ref capture: struct stores int64_t* count, maker passes &(count)
	assertContains(t, out, "int64_t* count")
	assertContains(t, out, "&(count)")
}

// ── M2: Standard library ──────────────────────────────────────────────────────

func TestMathAbsI64Emit(t *testing.T) {
	src := `
fn main() -> unit {
    let x: i64 = math_abs_i64(-5)
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_v < 0 ? -_v : _v")
}

func TestMathSqrtEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let x: f64 = math_sqrt(2.0)
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "sqrt(")
}

func TestMathPowEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let x: f64 = math_pow(2.0, 3.0)
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "pow(")
}

func TestMathMinMaxI64Emit(t *testing.T) {
	src := `
fn main() -> unit {
    let a: i64 = math_min_i64(1, 2)
    let b: i64 = math_max_i64(1, 2)
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_a<_b?_a:_b")
	assertContains(t, out, "_a>_b?_a:_b")
}

func TestMathClampI64Emit(t *testing.T) {
	src := `
fn main() -> unit {
    let x: i64 = math_clamp_i64(5, 0, 10)
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_x<_lo?_lo:_x>_hi?_hi:_x")
}

func TestStrRepeatEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let s: str = str_repeat("ab", 3)
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_cnd_str_repeat(")
}

func TestStrTrimEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let s: str = str_trim("  hi  ")
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_cnd_str_trim(")
}

func TestStrSplitEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let parts: vec<str> = str_split("a,b,c", ",")
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_cnd_vec_push_const_charptr")
	assertContains(t, out, "strstr")
}

func TestStrContainsEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let found: bool = str_contains("hello", "ell")
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "strstr(")
}

func TestStrToUpperLowerEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let u: str = str_to_upper("hello")
    let l: str = str_to_lower("WORLD")
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_cnd_str_to_upper(")
	assertContains(t, out, "_cnd_str_to_lower(")
}

func TestStrReplaceEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let s: str = str_replace("hello", "l", "r")
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_cnd_str_replace(")
}

func TestPrintErrEmit(t *testing.T) {
	src := `
fn main() -> unit {
    print_err("oops")
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "fprintf(stderr")
}

func TestFlushStdoutEmit(t *testing.T) {
	src := `
fn main() -> unit {
    flush_stdout()
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_cnd_flush_stdout()")
}

func TestOsArgsEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let args: vec<str> = os_args()
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_cnd_argc")
	assertContains(t, out, "_cnd_argv")
}

func TestOsGetenvEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let val: option<str> = os_getenv("HOME")
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "getenv(")
}

func TestOsExitEmit(t *testing.T) {
	src := `
fn main() -> unit {
    os_exit(1)
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "exit(")
}

func TestOsExecEmit(t *testing.T) {
	src := `
fn main() -> unit effects(io, sys) {
    let argv: vec<str> = vec_new()
    vec_push(argv, "echo")
    let r = os_exec(argv) must {
        ok(code) => code
        err(_e)  => return unit
    }
    print_int(r)
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_cnd_os_exec(")
}

func TestTimeNowMsEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let t: i64 = time_now_ms()
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_cnd_time_now_ms()")
}

func TestRandEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let r: u64 = rand_u64()
    let f: f64 = rand_f64()
    let n: i64 = rand_range(0, 100)
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_cnd_rand_u64()")
	assertContains(t, out, "_cnd_rand_f64()")
	assertContains(t, out, "_cnd_rand_u64() % (uint64_t)")
}

func TestPathJoinEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let p: str = path_join("/tmp", "file.txt")
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_cnd_path_join(")
}

func TestPathExistsEmit(t *testing.T) {
	src := `
fn main() -> unit {
    let fnd: bool = path_exists("/tmp")
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "_cnd_path_exists(")
}

func TestMainHasArgcArgv(t *testing.T) {
	src := `fn main() -> unit { return unit }`
	out := pipeline(t, src)
	assertContains(t, out, "int main(int argc, char** argv)")
	assertContains(t, out, "_cnd_argc = argc")
}

// ── M3: Trait / Interface System ─────────────────────────────────────────────

func TestTraitDeclParsed(t *testing.T) {
	// A trait declaration should parse and type-check without error.
	src := `
trait Display {
    fn fmt(self: str) -> str
}
fn main() -> unit { return unit }
`
	_ = pipeline(t, src) // just verifying no panic/error
}

func TestImplForBasic(t *testing.T) {
	// impl Trait for Type produces a method callable as TypeName_methodName in C.
	src := `
struct Point { x: i64, y: i64 }

trait Display {
    fn fmt(self: ref<Point>) -> str
}

impl Display for Point {
    fn fmt(self: ref<Point>) -> str {
        return "point"
    }
}

fn main() -> unit {
    let p = Point { x: 1, y: 2 }
    let s: str = Point_fmt(&p)
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "const char* Point_fmt(")
}

func TestImplForMethodCallViaDot(t *testing.T) {
	// Trait-impl methods are callable via dot syntax on the receiver.
	// Use a value receiver so the dot-call works without explicit & operator.
	src := `
struct Counter { val: i64 }

trait Display {
    fn fmt(self: Counter) -> str
}

impl Display for Counter {
    fn fmt(self: Counter) -> str {
        return int_to_str(self.val)
    }
}

fn main() -> unit {
    let c = Counter { val: 42 }
    let s: str = c.fmt()
    return unit
}
`
	out := pipeline(t, src)
	assertContains(t, out, "const char* Counter_fmt(")
	assertContains(t, out, "Counter_fmt(")
}

func TestGenericWithTraitBound(t *testing.T) {
	// Generic function with a trait bound; calling with a type that implements
	// the trait should monomorphize successfully.
	src := `
struct Box { val: i64 }

trait Display {
    fn fmt(self: ref<Box>) -> str
}

impl Display for Box {
    fn fmt(self: ref<Box>) -> str {
        return int_to_str(self.val)
    }
}

fn show<T: Display>(x: ref<T>) -> str {
    return x.fmt()
}

fn main() -> unit {
    let b = Box { val: 7 }
    let s: str = show(&b)
    return unit
}
`
	out := pipeline(t, src)
	// monomorphized as show__Box
	assertContains(t, out, "show__Box")
	// the trait impl body is emitted
	assertContains(t, out, "Box_fmt(")
}

func TestTraitBoundEnforced(t *testing.T) {
	// Calling a trait-bounded generic with a type that does NOT implement the
	// trait must produce a type-check error.
	src := `
struct Foo { x: i64 }
struct Bar { y: i64 }

trait Display {
    fn fmt(self: ref<Foo>) -> str
}

impl Display for Foo {
    fn fmt(self: ref<Foo>) -> str { return "foo" }
}

fn show<T: Display>(x: ref<T>) -> str {
    return x.fmt()
}

fn main() -> unit {
    let b = Bar { y: 1 }
    let s: str = show(&b)
    return unit
}
`
	tokens, _ := lexer.Tokenize("<test>", src)
	file, _ := parser.Parse("<test>", tokens)
	_, err := typeck.Check(file)
	if err == nil {
		t.Fatal("expected type error: Bar does not implement Display, got nil")
	}
	if !strings.Contains(err.Error(), "Display") {
		t.Errorf("error should mention 'Display', got: %v", err)
	}
}

// ── M4.4 Formatter tests ──────────────────────────────────────────────────────

func TestFormatCandorSimpleFn(t *testing.T) {
	src := `fn add(a: i64, b: i64) -> i64 {
	return a + b
}`
	tokens, err := lexer.Tokenize("<test>", src)
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.Parse("<test>", tokens)
	if err != nil {
		t.Fatal(err)
	}
	out := FormatCandor(file)
	if !strings.Contains(out, "fn add(a: i64, b: i64) -> i64 {") {
		t.Errorf("missing fn signature in formatted output:\n%s", out)
	}
	if !strings.Contains(out, "return a + b") {
		t.Errorf("missing return statement in formatted output:\n%s", out)
	}
}

func TestFormatCandorStruct(t *testing.T) {
	src := `struct Point { x: f64, y: f64, }`
	tokens, err := lexer.Tokenize("<test>", src)
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.Parse("<test>", tokens)
	if err != nil {
		t.Fatal(err)
	}
	out := FormatCandor(file)
	if !strings.Contains(out, "struct Point {") {
		t.Errorf("missing struct header: %q", out)
	}
	if !strings.Contains(out, "x: f64,") {
		t.Errorf("missing field x: %q", out)
	}
}

func TestFormatCandorBlankLineBetweenDecls(t *testing.T) {
	src := `fn a() -> unit { return unit }
fn b() -> unit { return unit }`
	tokens, err := lexer.Tokenize("<test>", src)
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.Parse("<test>", tokens)
	if err != nil {
		t.Fatal(err)
	}
	out := FormatCandor(file)
	if !strings.Contains(out, "\n\n") {
		t.Errorf("expected blank line between declarations:\n%q", out)
	}
}

// ── M4.5 Test directive tests ─────────────────────────────────────────────────

func TestTestDirectiveParsed(t *testing.T) {
	src := `#test
fn test_add() -> unit {
	assert 1 + 1 == 2
	return unit
}`
	tokens, err := lexer.Tokenize("<test>", src)
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.Parse("<test>", tokens)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Decls) == 0 {
		t.Fatal("expected at least one declaration")
	}
	fn, ok := file.Decls[0].(*parser.FnDecl)
	if !ok {
		t.Fatalf("expected FnDecl, got %T", file.Decls[0])
	}
	found := false
	for _, d := range fn.Directives {
		if d == "test" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'test' in Directives, got %v", fn.Directives)
	}
}

// ── M10.1 task<T> / spawn emit ────────────────────────────────────────────────

func TestSpawnEmitsPthread(t *testing.T) {
	src := `
fn main() -> unit {
    let t = spawn { return 42 }
    return unit
}
`
	c := pipeline(t, src)
	if !strings.Contains(c, "pthread_create") {
		t.Errorf("expected pthread_create in C output, got:\n%s", c)
	}
	if !strings.Contains(c, "pthread.h") {
		t.Errorf("expected pthread.h include, got:\n%s", c)
	}
}

func TestSpawnEmitsTaskStruct(t *testing.T) {
	src := `
fn main() -> unit {
    let t = spawn { return 42 }
    return unit
}
`
	c := pipeline(t, src)
	if !strings.Contains(c, "_CndTask_") {
		t.Errorf("expected _CndTask_ struct in C output, got:\n%s", c)
	}
}

func TestSpawnJoinEmitsPthreadJoin(t *testing.T) {
	src := `
fn main() -> unit {
    let t = spawn { return 42 }
    let r = t.join()
    return unit
}
`
	c := pipeline(t, src)
	if !strings.Contains(c, "pthread_join") {
		t.Errorf("expected pthread_join in C output, got:\n%s", c)
	}
}

func TestSpawnThunkFunctionEmitted(t *testing.T) {
	src := `
fn main() -> unit {
    let t = spawn { return 7 }
    return unit
}
`
	c := pipeline(t, src)
	if !strings.Contains(c, "_cnd_spawn_1_fn") {
		t.Errorf("expected _cnd_spawn_1_fn in C output, got:\n%s", c)
	}
}

// ── M11.2: tensor<T> emission ────────────────────────────────────────────────

func TestTensorTypedefEmitted(t *testing.T) {
	src := `
fn main() -> unit {
    let shape: vec<i64> = [2, 3]
    let t: tensor<f32> = tensor_zeros(shape)
    return unit
}
`
	c := pipeline(t, src)
	assertContains(t, c, "_CndTensor_float")
}

func TestTensorStructFieldsEmitted(t *testing.T) {
	src := `
fn main() -> unit {
    let shape: vec<i64> = [4]
    let t: tensor<f64> = tensor_zeros(shape)
    return unit
}
`
	c := pipeline(t, src)
	assertContains(t, c, "_data")
	assertContains(t, c, "_shape")
	assertContains(t, c, "_ndim")
}

func TestTensorZerosEmitsCalloc(t *testing.T) {
	src := `
fn main() -> unit {
    let shape: vec<i64> = [3]
    let t: tensor<f32> = tensor_zeros(shape)
    return unit
}
`
	c := pipeline(t, src)
	assertContains(t, c, "calloc")
}

func TestTensorFromVecEmitted(t *testing.T) {
	src := `
fn main() -> unit {
    let data: vec<f32> = [1.0, 2.0]
    let shape: vec<i64> = [2]
    let t: tensor<f32> = tensor_from_vec(data, shape)
    return unit
}
`
	c := pipeline(t, src)
	assertContains(t, c, "_CndTensor_float")
}

func TestTensorGetEmitted(t *testing.T) {
	src := `
fn main() -> unit {
    let data: vec<f32> = [1.0, 2.0, 3.0, 4.0]
    let shape: vec<i64> = [2, 2]
    let mut t: tensor<f32> = tensor_from_vec(data, shape)
    let idx: vec<i64> = [0, 1]
    let v: f32 = tensor_get(t, idx)
    return unit
}
`
	c := pipeline(t, src)
	assertContains(t, c, "_fi")
}

// ── M11.3: SIMD distance intrinsics ──────────────────────────────────────────

func TestTensorDotEmitted(t *testing.T) {
	src := `
fn main() -> unit {
    let a: tensor<f32> = tensor_zeros([4])
    let b: tensor<f32> = tensor_zeros([4])
    let d: f32 = tensor_dot(a, b)
    return unit
}
`
	c := pipeline(t, src)
	assertContains(t, c, "_acc")
}

func TestTensorL2EmittedSqrt(t *testing.T) {
	src := `
fn main() -> unit {
    let a: tensor<f64> = tensor_zeros([3])
    let n: f64 = tensor_l2(a)
    return unit
}
`
	c := pipeline(t, src)
	assertContains(t, c, "sqrt")
}

func TestTensorCosineEmitted(t *testing.T) {
	src := `
fn main() -> unit {
    let a: tensor<f32> = tensor_zeros([8])
    let b: tensor<f32> = tensor_zeros([8])
    let sim: f32 = tensor_cosine(a, b)
    return unit
}
`
	c := pipeline(t, src)
	assertContains(t, c, "_dot")
	assertContains(t, c, "1e-12")
}

func TestTensorMatmulEmitted(t *testing.T) {
	src := `
fn main() -> unit {
    let a: tensor<f32> = tensor_zeros([2, 3])
    let b: tensor<f32> = tensor_zeros([3, 4])
    let mut out: tensor<f32> = tensor_zeros([2, 4])
    tensor_matmul(a, b, out)
    return unit
}
`
	c := pipeline(t, src)
	assertContains(t, c, "_M")
	assertContains(t, c, "_K")
	assertContains(t, c, "_N")
}

// ── M12.1: mmap<T> emission ───────────────────────────────────────────────────

func TestMmapTypedefEmitted(t *testing.T) {
	src := `
fn f() -> unit {
    let r: result<mmap<u8>, str> = mmap_anon(64)
    return unit
}
`
	c := pipeline(t, src)
	assertContains(t, c, "_CndMmap_uint8_t")
}

func TestMmapStructFieldsEmitted(t *testing.T) {
	src := `
fn f(m: mmap<u8>) -> unit {
    mmap_close(m)
    return unit
}
`
	c := pipeline(t, src)
	assertContains(t, c, "_data")
	assertContains(t, c, "_fd")
}

func TestMmapAnonEmitsMmap(t *testing.T) {
	src := `
fn f() -> unit {
    let r: result<mmap<u8>, str> = mmap_anon(1024)
    return unit
}
`
	c := pipeline(t, src)
	assertContains(t, c, "MAP_ANONYMOUS")
}

func TestMmapOpenEmitsOpen(t *testing.T) {
	src := `
fn f() -> unit {
    let r: result<mmap<u8>, str> = mmap_open("/tmp/x", 4096)
    return unit
}
`
	c := pipeline(t, src)
	assertContains(t, c, "O_RDWR")
}

func TestMmapHeaderEmitted(t *testing.T) {
	src := `
fn f() -> unit {
    let r: result<mmap<u8>, str> = mmap_anon(64)
    return unit
}
`
	c := pipeline(t, src)
	assertContains(t, c, "sys/mman.h")
}

// ── ? propagation operator ────────────────────────────────────────────────────

func TestPropagateExprEmitsExtension(t *testing.T) {
	src := `
fn parse(s: str) -> result<u32, str> pure { return ok(0) }
fn run(s: str) -> result<u32, str> pure {
    let v: u32 = parse(s)?
    return ok(v)
}
`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	assertContains(t, out, "__extension__")
	assertContains(t, out, "_ok_val")
	assertContains(t, out, "_err_val")
}

func TestPropagateExprEarlyReturn(t *testing.T) {
	src := `
fn parse(s: str) -> result<u32, str> pure { return ok(0) }
fn run(s: str) -> result<u32, str> pure {
    let v: u32 = parse(s)?
    return ok(v)
}
`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	assertContains(t, out, "if (!")
	assertContains(t, out, "_cnd_early")
	assertContains(t, out, "return _cnd_early")
}

func TestPropagateExprTypeckRejectsWrongReturnType(t *testing.T) {
	src := `
fn parse(s: str) -> result<u32, str> pure { return ok(0) }
fn bad(s: str) -> u32 pure {
    let v: u32 = parse(s)?
    return v
}
`
	tokens, err := lexer.Tokenize("<test>", src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	file, err := parser.Parse("<test>", tokens)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = typeck.Check(file)
	if err == nil {
		t.Fatal("expected typeck error for ? in non-result function, got nil")
	}
	if !strings.Contains(err.Error(), "?") {
		t.Errorf("expected error mentioning '?', got: %v", err)
	}
}

// ── |> pipeline operator ──────────────────────────────────────────────────────

func TestPipeExprDesugar(t *testing.T) {
	src := `
fn double(x: u32) -> u32 pure { return x * 2 }
fn quad(x: u32) -> u32 pure { return x |> double |> double }
`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	assertContains(t, out, "double(")
}

func TestPipeExprChained(t *testing.T) {
	src := `
fn inc(x: u32) -> u32 pure { return x + 1 }
fn triple_inc(x: u32) -> u32 pure { return x |> inc |> inc |> inc }
`
	out := pipeline(t, src)
	t.Logf("emitted C:\n%s", out)
	assertContains(t, out, "inc(inc(inc(")
}

func TestPipeExprTypeckRejectsBadArg(t *testing.T) {
	src := `
fn takes_str(s: str) -> str pure { return s }
fn bad(x: u32) -> str pure { return x |> takes_str }
`
	tokens, err := lexer.Tokenize("<test>", src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	file, err := parser.Parse("<test>", tokens)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = typeck.Check(file)
	if err == nil {
		t.Fatal("expected typeck error for |> with wrong argument type, got nil")
	}
}
