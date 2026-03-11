// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg1/candor

// Integration tests for candorc v0.0.1.
// Each test writes a .cnd file to a temp directory, runs the full pipeline
// (lex → parse → typeck → emit_c → CC), executes the resulting binary, and
// checks the exit code.
package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	emit_c "github.com/scottcorleyg1/candor/compiler/emit_c"
	"github.com/scottcorleyg1/candor/compiler/lexer"
	"github.com/scottcorleyg1/candor/compiler/parser"
	"github.com/scottcorleyg1/candor/compiler/typeck"
)

// compile runs the full candorc pipeline on src, writes a binary to dir,
// and returns its path.
func compile(t *testing.T, dir, name, src string) string {
	t.Helper()

	cndPath := filepath.Join(dir, name+".cnd")
	if err := os.WriteFile(cndPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	tokens, err := lexer.Tokenize(cndPath, src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	file, err := parser.Parse(cndPath, tokens)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	res, err := typeck.Check(file)
	if err != nil {
		t.Fatalf("typeck: %v", err)
	}
	cSrc, err := emit_c.Emit(file, res)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	cPath := filepath.Join(dir, name+".c")
	if err := os.WriteFile(cPath, []byte(cSrc), 0o644); err != nil {
		t.Fatalf("write C: %v", err)
	}
	t.Logf("emitted C:\n%s", cSrc)

	binPath := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	cc := findCC()
	out, err := exec.Command(cc, "-o", binPath, cPath).CombinedOutput()
	if err != nil {
		t.Fatalf("CC failed:\n%s\n%v", out, err)
	}

	return binPath
}

func findCC() string {
	if cc := os.Getenv("CC"); cc != "" {
		return cc
	}
	if path, err := exec.LookPath("gcc"); err == nil {
		return path
	}
	return "cc"
}

func skipIfNoCC(t *testing.T) {
	t.Helper()
	cc := findCC()
	if _, err := exec.LookPath(cc); err != nil {
		t.Skipf("no C compiler found (%s not in PATH)", cc)
	}
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestAcceptanceCriterionProgram is the v0.0.1 acceptance criterion.
// The compiled binary must exit 0.
func TestAcceptanceCriterionProgram(t *testing.T) {
	skipIfNoCC(t)

	src := `
fn add(a: u32, b: u32) -> u32 { return a + b }

fn main() -> unit {
    let x = add(1, 2)
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "acceptance", src)

	cmd := exec.Command(bin)
	if err := cmd.Run(); err != nil {
		t.Fatalf("binary exited non-zero: %v", err)
	}
}

// TestArithmeticProgram verifies a program that does arithmetic and exits.
func TestArithmeticProgram(t *testing.T) {
	skipIfNoCC(t)

	// Compute 6*7 and exit with that value mod 100.
	// We can't easily capture output yet (no print in Core), so we just
	// verify exit code 0 for a well-typed program.
	src := `
fn mul(a: u32, b: u32) -> u32 { return a * b }
fn main() -> unit {
    let result = mul(6, 7)
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "arith", src)

	if err := exec.Command(bin).Run(); err != nil {
		t.Fatalf("binary exited non-zero: %v", err)
	}
}

// TestBooleanLogicProgram verifies boolean operators compile and run.
func TestBooleanLogicProgram(t *testing.T) {
	skipIfNoCC(t)

	src := `
fn all(a: bool, b: bool) -> bool { return a and b }
fn main() -> unit {
    let v = all(true, true)
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "bool", src)

	if err := exec.Command(bin).Run(); err != nil {
		t.Fatalf("binary exited non-zero: %v", err)
	}
}

// TestIfElseProgram verifies if/else compiles and runs.
func TestIfElseProgram(t *testing.T) {
	skipIfNoCC(t)

	src := `
fn max(a: u32, b: u32) -> u32 {
    if a < b { return b }
    return a
}
fn main() -> unit {
    let m = max(3, 7)
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "ifelse", src)

	if err := exec.Command(bin).Run(); err != nil {
		t.Fatalf("binary exited non-zero: %v", err)
	}
}

// TestStructProgram verifies struct definition and field access compile.
func TestStructProgram(t *testing.T) {
	skipIfNoCC(t)

	src := `
struct Point { x: u32, y: u32 }
fn sum(p: Point) -> u32 { return p.x + p.y }
fn main() -> unit {
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "struct", src)

	if err := exec.Command(bin).Run(); err != nil {
		t.Fatalf("binary exited non-zero: %v", err)
	}
}

// TestPrintBuiltins compiles and runs programs using the built-in print
// functions and verifies their stdout output.
func TestPrintBuiltins(t *testing.T) {
	skipIfNoCC(t)

	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "print_str",
			src: `fn main() -> unit {
    print("hello candor")
    return unit
}`,
			want: "hello candor\n",
		},
		{
			name: "print_int",
			src: `fn main() -> unit {
    print_int(42)
    return unit
}`,
			want: "42\n",
		},
		{
			name: "print_u32",
			src: `fn main() -> unit {
    print_u32(99)
    return unit
}`,
			want: "99\n",
		},
		{
			name: "print_bool_true",
			src: `fn main() -> unit {
    print_bool(true)
    return unit
}`,
			want: "true\n",
		},
		{
			name: "print_bool_false",
			src: `fn main() -> unit {
    print_bool(false)
    return unit
}`,
			want: "false\n",
		},
		{
			name: "print_computed",
			src: `
fn add(a: u32, b: u32) -> u32 { return a + b }
fn main() -> unit {
    let x = add(3, 4)
    print_u32(x)
    return unit
}`,
			want: "7\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := compile(t, dir, tc.name, tc.src)

			out, err := exec.Command(bin).Output()
			if err != nil {
				t.Fatalf("binary failed: %v", err)
			}
			// Normalize Windows \r\n to \n.
			got := strings.ReplaceAll(string(out), "\r\n", "\n")
			if got != tc.want {
				t.Errorf("stdout: got %q, want %q", got, tc.want)
			}
		})
	}
}

// compileMulti merges multiple named sources, runs the full pipeline, and
// returns the path to the compiled binary.
func compileMulti(t *testing.T, dir, name string, srcs map[string]string) string {
	t.Helper()

	var allDecls []parser.Decl
	var firstName string
	// Stable order: sort the keys.
	keys := make([]string, 0, len(srcs))
	for k := range srcs {
		keys = append(keys, k)
	}
	// sort alphabetically for determinism
	for i := 0; i < len(keys)-1; i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	files := make([]*parser.File, 0, len(keys))
	for _, k := range keys {
		src := srcs[k]
		fakePath := filepath.Join(dir, k+".cnd")
		if err := os.WriteFile(fakePath, []byte(src), 0o644); err != nil {
			t.Fatalf("write source %s: %v", k, err)
		}
		tokens, err := lexer.Tokenize(fakePath, src)
		if err != nil {
			t.Fatalf("lex %s: %v", k, err)
		}
		file, err := parser.Parse(fakePath, tokens)
		if err != nil {
			t.Fatalf("parse %s: %v", k, err)
		}
		files = append(files, file)
		allDecls = append(allDecls, file.Decls...)
		if firstName == "" {
			firstName = fakePath
		}
	}

	res, err := typeck.CheckProgram(files)
	if err != nil {
		t.Fatalf("typeck: %v", err)
	}
	merged := &parser.File{Name: firstName, Decls: allDecls}
	cSrc, err := emit_c.Emit(merged, res)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	cPath := filepath.Join(dir, name+".c")
	if err := os.WriteFile(cPath, []byte(cSrc), 0o644); err != nil {
		t.Fatalf("write C: %v", err)
	}
	t.Logf("emitted C:\n%s", cSrc)

	binPath := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	cc := findCC()
	out, err := exec.Command(cc, "-o", binPath, cPath).CombinedOutput()
	if err != nil {
		t.Fatalf("CC failed:\n%s\n%v", out, err)
	}
	return binPath
}

// TestMutableVariable tests a loop counting to 5, printing result.
func TestMutableVariable(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
    let mut count: u32 = 0
    count = 5
    print_u32(count)
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "mut_var", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("binary failed: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "5\n" {
		t.Errorf("stdout: got %q, want %q", got, "5\n")
	}
}

// TestMatchExpression tests matching on a bool value.
func TestMatchExpression(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn f(b: bool) -> u32 {
    return match b {
        true  => 1
        false => 2
    }
}
fn main() -> unit {
    print_u32(f(true))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "match_bool", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("binary failed: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "1\n" {
		t.Errorf("stdout: got %q, want %q", got, "1\n")
	}
}

// TestMultiFile tests two source files merged together.
func TestMultiFile(t *testing.T) {
	skipIfNoCC(t)
	srcs := map[string]string{
		"a_helpers": `fn add(a: u32, b: u32) -> u32 { return a + b }`,
		"b_main": `
fn main() -> unit {
    print_u32(add(10, 32))
    return unit
}
`,
	}
	dir := t.TempDir()
	bin := compileMulti(t, dir, "multifile", srcs)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("binary failed: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "42\n" {
		t.Errorf("stdout: got %q, want %q", got, "42\n")
	}
}

// TestPureProgram verifies a pure function compiles and the binary runs.
func TestPureProgram(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn twice(n: u32) -> u32 pure { return n + n }
fn quad(n: u32) -> u32 pure { return twice(twice(n)) }
fn main() -> unit {
    print_u32(quad(3))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "pure_fn", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("binary failed: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "12\n" {
		t.Errorf("stdout: got %q, want %q", got, "12\n")
	}
}

// TestEffectsIoProgram verifies an effects(io) function compiles and runs.
func TestEffectsIoProgram(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn greet(name: str) -> unit effects(io) { print(name) return unit }
fn main() -> unit {
    greet("candor")
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "effects_io", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("binary failed: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "candor\n" {
		t.Errorf("stdout: got %q, want %q", got, "candor\n")
	}
}

// TestContractsProgram verifies requires/assert compile and run.
func TestContractsProgram(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn add_positive(a: u32, b: u32) -> u32
    requires a > 0
    ensures result > a
{
    assert b > 0
    return a + b
}
fn main() -> unit {
    print_u32(add_positive(3, 4))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "contracts", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("binary failed: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "7\n" {
		t.Errorf("stdout: got %q, want %q", got, "7\n")
	}
}

// TestStructLiteralProgram verifies struct literal syntax compiles and runs.
func TestStructLiteralProgram(t *testing.T) {
	skipIfNoCC(t)
	src := `
struct Point { x: u32, y: u32 }
fn sum(p: Point) -> u32 { return p.x + p.y }
fn main() -> unit {
    let p = Point { x: 3, y: 4 }
    print_u32(sum(p))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "struct_lit", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("binary failed: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "7\n" {
		t.Errorf("stdout: got %q, want %q", got, "7\n")
	}
}

// TestFieldAssignProgram verifies struct field mutation compiles and runs.
// A helper copies the struct and mutates the copy; main just calls it.
func TestFieldAssignProgram(t *testing.T) {
	skipIfNoCC(t)
	src := `
struct Point { x: u32, y: u32 }
fn copy_set_x(p: Point, val: u32) -> u32 {
    let mut q: Point = p
    q.x = val
    return q.x
}
fn main() -> unit {
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "field_assign", src)
	if err := exec.Command(bin).Run(); err != nil {
		t.Fatalf("binary exited non-zero: %v", err)
	}
}

// TestForLoopProgram verifies for..in over a vec compiles and produces correct output.
func TestForLoopProgram(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
    let mut v: vec<u32> = vec_new()
    vec_push(v, 10)
    vec_push(v, 20)
    vec_push(v, 30)
    let mut sum: u32 = 0
    for x in v { sum = sum + x }
    print_u32(sum)
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "for_loop", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("binary failed: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "60\n" {
		t.Errorf("stdout: got %q, want %q", got, "60\n")
	}
}

// TestVecLenProgram verifies vec_len returns the correct count.
func TestVecLenProgram(t *testing.T) {
	skipIfNoCC(t)
	// vec_len returns u64; compare against a u64 literal and print a bool.
	src := `
fn main() -> unit {
    let mut v: vec<u32> = vec_new()
    vec_push(v, 1)
    vec_push(v, 2)
    vec_push(v, 3)
    print_bool(vec_len(v) == 3)
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "vec_len", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("binary failed: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "true\n" {
		t.Errorf("stdout: got %q, want %q", got, "true\n")
	}
}

// TestMultiFileModuleProgram verifies that a two-file program with module
// declarations and use imports compiles and produces the right output.
func TestMultiFileModuleProgram(t *testing.T) {
	skipIfNoCC(t)

	srcs := map[string]string{
		"a_math": `
module math
fn add(a: i64, b: i64) -> i64 { return a + b }
fn mul(a: i64, b: i64) -> i64 { return a * b }
`,
		"b_main": `
module app
use math::add
use math::mul
fn main() -> unit {
    print_int(add(3, 4))
    print_int(mul(3, 4))
    return unit
}
`,
	}
	dir := t.TempDir()
	bin := compileMulti(t, dir, "multimod", srcs)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("binary failed: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "7\n12\n" {
		t.Errorf("stdout: got %q, want %q", got, "7\n12\n")
	}
}

// TestHigherOrderFunction verifies named functions passed as arguments and
// called through a function-typed parameter produce correct output.
func TestHigherOrderFunction(t *testing.T) {
	skipIfNoCC(t)

	src := `
fn twice(x: i64) -> i64 { return x * 2 }
fn square(x: i64) -> i64 { return x * x }
fn apply(f: fn(i64) -> i64, x: i64) -> i64 { return f(x) }

fn main() -> unit {
    print_int(apply(twice, 5))
    print_int(apply(square, 4))
    let f: fn(i64) -> i64 = twice
    print_int(f(7))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "higher_order", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("binary failed: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "10\n16\n14\n" {
		t.Errorf("stdout: got %q, want %q", got, "10\n16\n14\n")
	}
}

// TestFnReturnValue verifies a function can return another function as a
// value and it can be called through the result.
func TestFnReturnValue(t *testing.T) {
	skipIfNoCC(t)

	src := `
fn triple(x: i64) -> i64 { return x * 3 }
fn get_fn() -> fn(i64) -> i64 { return triple }

fn main() -> unit {
    let f = get_fn()
    print_int(f(6))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "fn_return", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("binary failed: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "18\n" {
		t.Errorf("stdout: got %q, want %q", got, "18\n")
	}
}

// TestEmittedCIsValidC verifies the C output for the acceptance criterion
// contains no obvious invalid patterns.
func TestEmittedCIsValidC(t *testing.T) {
	src := `
fn add(a: u32, b: u32) -> u32 { return a + b }
fn main() -> unit {
    let x = add(1, 2)
    return unit
}
`
	tokens, _ := lexer.Tokenize("<test>", src)
	file, _ := parser.Parse("<test>", tokens)
	res, _ := typeck.Check(file)
	c, err := emit_c.Emit(file, res)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	checks := []string{
		"#include <stdint.h>",
		"uint32_t add(",
		"int main(void)",
		"return 0;",
	}
	for _, want := range checks {
		if !strings.Contains(c, want) {
			t.Errorf("C output missing %q\n%s", want, c)
		}
	}
}

func TestMatchIntPattern(t *testing.T) {
	skipIfNoCC(t)
	dir := t.TempDir()
	src := `
fn classify(n: i64) -> i64 {
    return match n {
        0  => 100,
        1  => 200,
        -1 => 300,
        _  => 999
    }
}
fn main() -> unit {
    print_int(classify(0))
    print_int(classify(1))
    print_int(classify(-1))
    print_int(classify(42))
    return unit
}
`
	bin := compile(t, dir, "match_int", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "100\n200\n300\n999\n" {
		t.Fatalf("got %q, want %q", got, "100\n200\n300\n999\n")
	}
}

func TestMatchStringPattern(t *testing.T) {
	skipIfNoCC(t)
	dir := t.TempDir()
	src := `
fn greet(s: str) -> i64 {
    return match s {
        "hello" => 1
        "bye"   => 2
        _       => 0
    }
}
fn main() -> unit {
    print_int(greet("hello"))
    print_int(greet("bye"))
    print_int(greet("other"))
    return unit
}
`
	bin := compile(t, dir, "match_str", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "1\n2\n0\n" {
		t.Fatalf("got %q, want %q", got, "1\n2\n0\n")
	}
}

// ── stdin I/O builtins ────────────────────────────────────────────────────────

func TestReadLineEcho(t *testing.T) {
	skipIfNoCC(t)
	dir := t.TempDir()
	src := `
fn main() -> unit {
    let s = read_line()
    print(s)
    return unit
}
`
	bin := compile(t, dir, "read_line_echo", src)
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader("hello candor\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "hello candor\n" {
		t.Fatalf("got %q, want %q", got, "hello candor\n")
	}
}

func TestReadIntDouble(t *testing.T) {
	skipIfNoCC(t)
	dir := t.TempDir()
	src := `
fn main() -> unit {
    let n = read_int()
    print_int(n * 2)
    return unit
}
`
	bin := compile(t, dir, "read_int_double", src)
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader("21\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "42\n" {
		t.Fatalf("got %q, want %q", got, "42\n")
	}
}

func TestReadMultipleLines(t *testing.T) {
	skipIfNoCC(t)
	dir := t.TempDir()
	src := `
fn main() -> unit {
    let a = read_int()
    let b = read_int()
    print_int(a + b)
    return unit
}
`
	bin := compile(t, dir, "read_multi", src)
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader("10\n32\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "42\n" {
		t.Fatalf("got %q, want %q", got, "42\n")
	}
}

// TestSumUntilEOF is the first "human/AI use test" program: read integers until
// EOF using try_read_int + must{ break }, accumulate in a vec, print sum and count.
func TestSumUntilEOF(t *testing.T) {
	skipIfNoCC(t)
	dir := t.TempDir()
	src := `
fn main() -> unit {
    let mut nums: vec<i64> = vec_new()
    loop {
        let n = try_read_int()
        let x = n must {
            some(v) => v
            none    => break
        }
        vec_push(nums, x)
    }
    let mut sum: i64 = 0
    let mut count: i64 = 0
    for n in nums {
        sum = sum + n
        count = count + 1
    }
    print_int(sum)
    print_int(count)
    return unit
}
`
	bin := compile(t, dir, "sum_eof", src)
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader("3\n7\n10\n20\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "40\n4\n" {
		t.Fatalf("got %q, want %q", got, "40\n4\n")
	}
}

// TestVecIndex verifies vec element access via v[i] syntax.
func TestVecIndex(t *testing.T) {
	skipIfNoCC(t)
	dir := t.TempDir()
	src := `
fn main() -> unit {
    let mut v: vec<i64> = vec_new()
    vec_push(v, 100)
    vec_push(v, 200)
    vec_push(v, 300)
    print_int(v[0])
    print_int(v[1])
    print_int(v[2])
    return unit
}
`
	bin := compile(t, dir, "vec_index", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "100\n200\n300\n" {
		t.Fatalf("got %q, want %q", got, "100\n200\n300\n")
	}
}

// ── String operations ─────────────────────────────────────────────────────────

func TestStrLen(t *testing.T) {
	skipIfNoCC(t)
	dir := t.TempDir()
	src := `
fn main() -> unit {
    print_int(str_len("hello"))
    print_int(str_len(""))
    return unit
}
`
	bin := compile(t, dir, "str_len", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "5\n0\n" {
		t.Fatalf("got %q, want %q", got, "5\n0\n")
	}
}

func TestStrConcat(t *testing.T) {
	skipIfNoCC(t)
	dir := t.TempDir()
	src := `
fn main() -> unit {
    let s = str_concat("hello", ", world")
    print(s)
    return unit
}
`
	bin := compile(t, dir, "str_concat", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "hello, world\n" {
		t.Fatalf("got %q, want %q", got, "hello, world\n")
	}
}

func TestIntToStr(t *testing.T) {
	skipIfNoCC(t)
	dir := t.TempDir()
	src := `
fn main() -> unit {
    let s = int_to_str(42)
    print(s)
    let s2 = int_to_str(-7)
    print(s2)
    return unit
}
`
	bin := compile(t, dir, "int_to_str", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "42\n-7\n" {
		t.Fatalf("got %q, want %q", got, "42\n-7\n")
	}
}

func TestStrToInt(t *testing.T) {
	skipIfNoCC(t)
	dir := t.TempDir()
	src := `
fn main() -> unit {
    let a = str_to_int("123") must {
        ok(v)  => v
        err(_) => 0
    }
    print_int(a)
    let b = str_to_int("bad") must {
        ok(v)  => v
        err(_) => -1
    }
    print_int(b)
    return unit
}
`
	bin := compile(t, dir, "str_to_int", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "123\n-1\n" {
		t.Fatalf("got %q, want %q", got, "123\n-1\n")
	}
}

func TestStrEqOperator(t *testing.T) {
	skipIfNoCC(t)
	dir := t.TempDir()
	src := `
fn main() -> unit {
    let s = read_line()
    if s == "hello" {
        print("yes")
        return unit
    }
    print("no")
    return unit
}
`
	bin := compile(t, dir, "str_eq_op", src)
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader("hello\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "yes\n" {
		t.Fatalf("got %q, want %q", got, "yes\n")
	}
}

// ── Enum integration tests ────────────────────────────────────────────────────

func TestEnumUnitMatch(t *testing.T) {
	skipIfNoCC(t)
	src := `
enum Dir { North, South, East, West }
fn label(d: Dir) -> str {
    return match d {
        Dir::North => "N",
        Dir::South => "S",
        Dir::East  => "E",
        Dir::West  => "W",
    }
}
fn main() -> unit {
    print(label(Dir::North))
    print(label(Dir::West))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "enum_unit", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "N\nW\n" {
		t.Fatalf("got %q, want %q", got, "N\nW\n")
	}
}

func TestEnumDataMatch(t *testing.T) {
	skipIfNoCC(t)
	src := `
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
    let c = Shape::Circle(1.0)
    let r = Shape::Rect(3.0, 4.0)
    let p = Shape::Point
    print_f64(area(c))
    print_f64(area(r))
    print_f64(area(p))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "enum_data", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	// 1.0*1.0*3.14=3.14, 3.0*4.0=12.0, 0.0
	if got != "3.140000\n12.000000\n0.000000\n" {
		t.Fatalf("got %q", got)
	}
}

func TestEnumInLoop(t *testing.T) {
	skipIfNoCC(t)
	// Extract value from enum variant using must{}, accumulate in loop.
	src := `
enum Cmd { Stop, Value(i64) }
fn make_cmd(i: i64) -> Cmd {
    return match i {
        0 => Cmd::Value(10),
        1 => Cmd::Value(20),
        2 => Cmd::Value(30),
        _ => Cmd::Stop,
    }
}
fn get_value(c: Cmd) -> i64 {
    return match c {
        Cmd::Value(v) => v,
        Cmd::Stop     => -1,
    }
}
fn is_stop(c: Cmd) -> bool {
    return match c {
        Cmd::Stop    => true,
        Cmd::Value(_) => false,
    }
}
fn main() -> unit {
    let mut i: i64 = 0
    let mut sum: i64 = 0
    loop {
        let cmd = make_cmd(i)
        if is_stop(cmd) { break }
        sum = sum + get_value(cmd)
        i = i + 1
    }
    print_int(sum)
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "enum_loop", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "60\n" {
		t.Fatalf("got %q, want %q", got, "60\n")
	}
}

// ── File I/O integration tests ────────────────────────────────────────────────

func TestReadFileRoundTrip(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
    let wr = write_file("hello.txt", "candor works")
    match wr {
        ok(_u)  => print("wrote"),
        err(e)  => print(e),
    }
    let rd = read_file("hello.txt")
    match rd {
        ok(content) => print(content),
        err(e)      => print(e),
    }
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "file_io", src)
	cmd := exec.Command(bin)
	cmd.Dir = dir // run in temp dir so hello.txt is created there
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "wrote\ncandor works\n" {
		t.Fatalf("got %q, want %q", got, "wrote\ncandor works\n")
	}
}

func TestAppendFileRoundTrip(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
    let w = write_file("log.txt", "line1\n")
    match w {
        ok(_u) => print("ok1"),
        err(e) => print(e),
    }
    let a = append_file("log.txt", "line2\n")
    match a {
        ok(_u) => print("ok2"),
        err(e) => print(e),
    }
    let rd = read_file("log.txt")
    match rd {
        ok(content) => print(content),
        err(e)      => print(e),
    }
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "append_io", src)
	cmd := exec.Command(bin)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "ok1\nok2\nline1\nline2\n\n" {
		t.Fatalf("got %q, want %q", got, "ok1\nok2\nline1\nline2\n\n")
	}
}

func TestReadFileMissing(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
    let rd = read_file("does_not_exist.txt")
    match rd {
        ok(content) => print(content),
        err(_e)     => print("missing"),
    }
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "file_missing", src)
	cmd := exec.Command(bin)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "missing\n" {
		t.Fatalf("got %q, want %q", got, "missing\n")
	}
}
