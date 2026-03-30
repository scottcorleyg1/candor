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
	cmd := exec.Command(cc, "-o", binPath, cPath)
	cmd.Env = ccEnv(cc)
	out, err := cmd.CombinedOutput()
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
	// Windows: check common MSYS2/MinGW installations.
	if runtime.GOOS == "windows" {
		for _, candidate := range []string{
			`C:\msys64\mingw64\bin\gcc.exe`,
			`C:\msys64\ucrt64\bin\gcc.exe`,
			`C:\MinGW\bin\gcc.exe`,
			`C:\mingw64\bin\gcc.exe`,
		} {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return "cc"
}

// ccEnv returns the environment for the C compiler with the compiler's own
// bin directory prepended to PATH (needed for MinGW DLL resolution).
func ccEnv(cc string) []string {
	env := os.Environ()
	dir := filepath.Dir(cc)
	pathVar := os.Getenv("PATH")
	if dir != "." && !strings.Contains(pathVar, dir) {
		sep := string(os.PathListSeparator)
		for i, e := range env {
			if strings.EqualFold(strings.SplitN(e, "=", 2)[0], "PATH") {
				env[i] = e + sep + dir
				break
			}
		}
	}
	return env
}

func skipIfNoCC(t *testing.T) {
	t.Helper()
	cc := findCC()
	if _, err := os.Stat(cc); err != nil {
		if _, err2 := exec.LookPath(cc); err2 != nil {
			t.Skipf("no C compiler found (%s)", cc)
		}
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
fn larger(a: u32, b: u32) -> u32 {
    if a < b { return b }
    return a
}
fn main() -> unit {
    let m = larger(3, 7)
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
	cmd2 := exec.Command(cc, "-o", binPath, cPath)
	cmd2.Env = ccEnv(cc)
	out, err := cmd2.CombinedOutput()
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
		"int main(int argc, char** argv)",
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

// ── Ownership Tier 1 integration tests ───────────────────────────────────────

func TestRefParamIntegration(t *testing.T) {
	skipIfNoCC(t)
	src := `
struct Point { x: i64, y: i64 }
fn magnitude_sq(p: ref<Point>) -> i64 {
    return p.x * p.x + p.y * p.y
}
fn main() -> unit {
    let p = Point { x: 3, y: 4 }
    print_int(magnitude_sq(&p))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "ref_param", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "25\n" {
		t.Fatalf("got %q, want %q", got, "25\n")
	}
}

func TestRefmutParamIntegration(t *testing.T) {
	skipIfNoCC(t)
	src := `
struct Point { x: i64, y: i64 }
fn scale(p: refmut<Point>, factor: i64) -> unit {
    p.x = p.x * factor
    p.y = p.y * factor
    return unit
}
fn main() -> unit {
    let mut p = Point { x: 3, y: 4 }
    scale(refmut(p), 2)
    print_int(p.x)
    print_int(p.y)
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "refmut_param", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "6\n8\n" {
		t.Fatalf("got %q, want %q", got, "6\n8\n")
	}
}

func TestDerefIntegration(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
    let x: i64 = 42
    let r = &x
    let v: i64 = *r
    print_int(v)
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "deref", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "42\n" {
		t.Fatalf("got %q, want %q", got, "42\n")
	}
}

// ── Comptime integration tests ────────────────────────────────────────────────

// ── secret<T> integration tests ──────────────────────────────────────────────

func TestSecretRevealRoundTrip(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
    let s = secret("classified")
    let plain: str = reveal(s)
    print(plain)
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "secret_reveal", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "classified\n" {
		t.Fatalf("got %q, want %q", got, "classified\n")
	}
}

func TestSecretPureFunction(t *testing.T) {
	skipIfNoCC(t)
	// A pure function can accept secret<str> — no reveal needed inside.
	src := `
fn secret_len(s: secret<str>) -> i64 effects [] {
    return str_len(reveal(s))
}
fn main() -> unit {
    let s = secret("hello")
    print_int(secret_len(s))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "secret_pure", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "5\n" {
		t.Fatalf("got %q, want %q", got, "5\n")
	}
}

func TestComptimeSquare(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn square(x: i64) -> i64 effects [] { return x * x }
fn main() -> unit {
    print_int(square(7))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "comptime_sq", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "49\n" {
		t.Fatalf("got %q, want %q", got, "49\n")
	}
	// Verify the C source contains the constant 49 and main() does not call square().
	cSrc, _ := os.ReadFile(filepath.Join(dir, "comptime_sq.c"))
	if !strings.Contains(string(cSrc), "49") {
		t.Fatalf("expected constant 49 in emitted C, got:\n%s", cSrc)
	}
	// main() body should not contain a call to square — just the literal.
	if strings.Contains(string(cSrc), "square(49)") || strings.Contains(string(cSrc), "square(7)") {
		t.Fatalf("expected no square() call in main body (should be constant), got:\n%s", cSrc)
	}
}

func TestComptimeChainedIntegration(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn twice(x: i64) -> i64 effects [] { return x * 2 }
fn quad(x: i64) -> i64 effects [] { return twice(twice(x)) }
fn main() -> unit {
    print_int(quad(3))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "comptime_chain", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "12\n" {
		t.Fatalf("got %q, want %q", got, "12\n")
	}
}

func TestComptimeFallbackToRuntime(t *testing.T) {
	skipIfNoCC(t)
	// When the argument is not a literal, the call must produce the correct
	// result via normal runtime execution.
	src := `
fn square(x: i64) -> i64 effects [] { return x * x }
fn main() -> unit {
    let n: i64 = 9
    print_int(square(n))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "comptime_fallback", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "81\n" {
		t.Fatalf("got %q, want %q", got, "81\n")
	}
}

func TestMoveIntegration(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn twice(x: i64) -> i64 { return x * 2 }
fn main() -> unit {
    let n: i64 = 21
    print_int(twice(move(n)))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "move_call", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "42\n" {
		t.Fatalf("got %q, want %q", got, "42\n")
	}
}

// ── map<K,V> integration tests ────────────────────────────────────────────────

func TestMapBasicInsertGet(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
	let mut m: map<str, i64> = map_new()
	map_insert(m, "x", 42)
	let v = map_get(m, "x") must {
		some(n) => n
		none    => 0
	}
	print_int(v)
	return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "map_insert_get", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "42\n" {
		t.Fatalf("got %q, want %q", got, "42\n")
	}
}

func TestMapMissReturnsNone(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
	let mut m: map<str, i64> = map_new()
	let v = map_get(m, "missing") must {
		some(n) => n
		none    => -1
	}
	print_int(v)
	return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "map_miss", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "-1\n" {
		t.Fatalf("got %q, want %q", got, "-1\n")
	}
}

func TestMapOverwrite(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
	let mut m: map<str, i64> = map_new()
	map_insert(m, "k", 1)
	map_insert(m, "k", 2)
	let v = map_get(m, "k") must {
		some(n) => n
		none    => 0
	}
	print_int(v)
	return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "map_overwrite", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "2\n" {
		t.Fatalf("got %q, want %q", got, "2\n")
	}
}

func TestMapLen(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
	let mut m: map<str, i64> = map_new()
	map_insert(m, "a", 1)
	map_insert(m, "b", 2)
	map_insert(m, "c", 3)
	print_bool(map_len(m) == 3)
	return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "map_len", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "true\n" {
		t.Fatalf("got %q, want %q", got, "true\n")
	}
}

func TestMapContains(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
	let mut m: map<str, i64> = map_new()
	map_insert(m, "yes", 1)
	if map_contains(m, "yes") { print("found") }
	if map_contains(m, "no") { print("should not print") }
	return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "map_contains", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "found\n" {
		t.Fatalf("got %q, want %q", got, "found\n")
	}
}

func TestMapRemove(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
	let mut m: map<str, i64> = map_new()
	map_insert(m, "k", 99)
	let _removed: bool = map_remove(m, "k")
	let v = map_get(m, "k") must {
		some(n) => n
		none    => -1
	}
	print_int(v)
	return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "map_remove", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "-1\n" {
		t.Fatalf("got %q, want %q", got, "-1\n")
	}
}

func TestMapIntKey(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
	let mut m: map<i64, str> = map_new()
	map_insert(m, 1, "one")
	map_insert(m, 2, "two")
	let v = map_get(m, 1) must {
		some(s) => s
		none    => "?"
	}
	print(v)
	return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "map_int_key", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "one\n" {
		t.Fatalf("got %q, want %q", got, "one\n")
	}
}

// ── Feature: vec index assignment ───────────────────────────────────────────

func TestVecIndexAssignIntegration(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
	let mut v: vec<i64> = vec_new()
	vec_push(v, 10)
	vec_push(v, 20)
	v[0] = 99
	print_int(v[0])
	print_int(v[1])
	return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "vec_index_assign", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "99\n20\n" {
		t.Fatalf("got %q, want %q", got, "99\n20\n")
	}
}

// ── Feature: for k, v in map ─────────────────────────────────────────────────

func TestForKVInMapIntegration(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
	let mut m: map<str, i64> = map_new()
	map_insert(m, "hello", 42)
	for k, v in m {
		print(k)
		print_int(v)
	}
	return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "for_kv_map", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "hello\n42\n" {
		t.Fatalf("got %q, want %q", got, "hello\n42\n")
	}
}

// ── Feature: set<T> ──────────────────────────────────────────────────────────

func TestSetBasic(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
	let mut s: set<i64> = set_new()
	set_add(s, 1)
	set_add(s, 2)
	set_add(s, 3)
	set_add(s, 2)
	print_bool(set_len(s) == 3)
	if set_contains(s, 2) {
		print("yes")
	}
	if set_contains(s, 99) {
		print("no")
	}
	set_remove(s, 2)
	if set_contains(s, 2) {
		print("bad")
	}
	print_bool(set_len(s) == 2)
	return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "set_basic", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "true\nyes\ntrue\n" {
		t.Fatalf("got %q, want %q", got, "true\nyes\ntrue\n")
	}
}

func TestSetForIteration(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
	let mut s: set<i64> = set_new()
	set_add(s, 10)
	set_add(s, 20)
	set_add(s, 30)
	let mut sum: i64 = 0
	for x in s {
		sum = sum + x
	}
	print_int(sum)
	return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "set_for", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "60\n" {
		t.Fatalf("got %q, want %q", got, "60\n")
	}
}

func TestSetStrKeys(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
	let mut s: set<str> = set_new()
	set_add(s, "apple")
	set_add(s, "banana")
	set_add(s, "apple")
	print_bool(set_len(s) == 2)
	if set_contains(s, "banana") {
		print("found")
	}
	return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "set_str", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "true\nfound\n" {
		t.Fatalf("got %q, want %q", got, "true\nfound\n")
	}
}

// ── Feature: lambdas ─────────────────────────────────────────────────────────

func TestLambdaBasic(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
	let f: fn(i64) -> i64 = fn(x: i64) -> i64 { return x + 1 }
	print_int(f(10))
	return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "lambda_basic", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "11\n" {
		t.Fatalf("got %q, want %q", got, "11\n")
	}
}

func TestLambdaHigherOrder(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn apply(f: fn(i64) -> i64, x: i64) -> i64 { return f(x) }
fn main() -> unit {
	let result: i64 = apply(fn(n: i64) -> i64 { return n * 2 }, 7)
	print_int(result)
	return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "lambda_ho", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "14\n" {
		t.Fatalf("got %q, want %q", got, "14\n")
	}
}

func TestLambdaInLoop(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
	let twice: fn(i64) -> i64 = fn(x: i64) -> i64 { return x * 2 }
	let mut v: vec<i64> = vec_new()
	vec_push(v, 1)
	vec_push(v, 2)
	vec_push(v, 3)
	for x in v {
		print_int(twice(x))
	}
	return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "lambda_loop", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "2\n4\n6\n" {
		t.Fatalf("got %q, want %q", got, "2\n4\n6\n")
	}
}

// ── Feature: old() in contracts ──────────────────────────────────────────────

func TestOldIncrement(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn increment(x: i64) -> i64
    ensures result == old(x) + 1
{
    return x + 1
}
fn main() -> unit {
    print_int(increment(5))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "old_incr", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "6\n" {
		t.Fatalf("got %q, want %q", got, "6\n")
	}
}

func TestOldWithResult(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn add(x: i64, y: i64) -> i64
    ensures result == old(x) + old(y)
{
    return x + y
}
fn main() -> unit {
    print_int(add(3, 4))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "old_add", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "7\n" {
		t.Fatalf("got %q, want %q", got, "7\n")
	}
}

// ── New feature tests (features 1-8) ─────────────────────────────────────────

// TestWhileLoop verifies the while loop construct.
func TestWhileLoop(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
    let mut i: i64 = 0
    while i < 5 {
        i = i + 1
    }
    print_int(i)
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "while_loop", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "5\n" {
		t.Fatalf("got %q, want %q", got, "5\n")
	}
}

// TestConstDecl verifies module-level const declarations.
func TestConstDecl(t *testing.T) {
	skipIfNoCC(t)
	src := `
const ANSWER: i64 = 42

fn main() -> unit {
    print_int(ANSWER)
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "const_decl", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "42\n" {
		t.Fatalf("got %q, want %q", got, "42\n")
	}
}

// TestCastOperator verifies the `as` explicit cast operator.
func TestCastOperator(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
    let x: i32 = 7
    let y: i64 = x as i64
    print_int(y)
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "cast_op", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "7\n" {
		t.Fatalf("got %q, want %q", got, "7\n")
	}
}

// TestVecLiteral verifies vec literal syntax [a, b, c].
func TestVecLiteral(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn main() -> unit {
    let v: vec<i64> = [10, 20, 30]
    let n: i64 = vec_len(v) as i64
    print(int_to_str(n))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "vec_lit", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "3\n" {
		t.Fatalf("got %q, want %q", got, "3\n")
	}
}

// TestImplMethodCall verifies impl blocks and method call syntax.
func TestImplMethodCall(t *testing.T) {
	skipIfNoCC(t)
	src := `
struct Counter {
    val: i64,
}

impl Counter {
    fn get(self: Counter) -> i64 {
        return self.val
    }
    fn inc(self: Counter) -> Counter {
        return Counter { val: self.val + 1 }
    }
}

fn main() -> unit {
    let c = Counter { val: 0 }
    let c2 = c.inc()
    let c3 = c2.inc()
    print_int(c3.get())
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "impl_method", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != "2\n" {
		t.Fatalf("got %q, want %q", got, "2\n")
	}
}

// TestLiteralPatternMatch verifies integer and string literal patterns in match.
func TestLiteralPatternMatch(t *testing.T) {
	skipIfNoCC(t)
	src := `
fn classify(n: i64) -> i64 {
    return match n {
        0 => 100,
        1 => 200,
        _ => 300,
    }
}

fn main() -> unit {
    print_int(classify(0))
    print_int(classify(1))
    print_int(classify(99))
    return unit
}
`
	dir := t.TempDir()
	bin := compile(t, dir, "lit_pattern", src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	want := "100\n200\n300\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ── M9 bootstrap sources: lex/parse/typeck only (no CC invocation) ──────────

// checkSource runs lex → parse → typeck on a file from the src/ tree and
// fails the test if any stage rejects it.
func checkSource(t *testing.T, path string) {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	tokens, err := lexer.Tokenize(path, string(src))
	if err != nil {
		t.Fatalf("lex %s: %v", path, err)
	}
	file, err := parser.Parse(path, tokens)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	_, err = typeck.Check(file)
	if err != nil {
		t.Fatalf("typeck %s: %v", path, err)
	}
}

func TestM9LexerSource(t *testing.T) {
	checkSource(t, filepath.Join("..", "..", "src", "compiler", "lexer.cnd"))
}

func TestM9ParserSource(t *testing.T) {
	checkSource(t, filepath.Join("..", "..", "src", "compiler", "parser.cnd"))
}

func TestM9TypeckSource(t *testing.T) {
	// typeck.cnd is a bundle file — it references TypeExpr and other types
	// declared in parser.cnd.  Check both together.
	checkBundledSource(t,
		filepath.Join("..", "..", "src", "compiler", "parser.cnd"),
		filepath.Join("..", "..", "src", "compiler", "typeck.cnd"),
	)
}

// checkBundledSource parses deps+target together and type-checks them as a
// program so that cross-file type references (e.g. TypeExpr from parser.cnd
// used in typeck.cnd) resolve correctly.
func checkBundledSource(t *testing.T, paths ...string) {
	t.Helper()
	var files []*parser.File
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		tokens, err := lexer.Tokenize(path, string(src))
		if err != nil {
			t.Fatalf("lex %s: %v", path, err)
		}
		file, err := parser.Parse(path, tokens)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files = append(files, file)
	}
	_, err := typeck.CheckProgram(files)
	if err != nil {
		t.Fatalf("typeck bundle %v: %v", paths, err)
	}
}

func TestM55WasmStdSource(t *testing.T) {
	checkSource(t, filepath.Join("..", "..", "src", "std", "wasm.cnd"))
}

// ── M9 bootstrap sources: full pipeline including emit_c + gcc ────────────────
//
// emitSource runs lex → parse → typeck → emit_c on a file from the src/ tree.
// It asserts:
//   1. All four stages succeed without error.
//   2. The emitted C is non-empty.
//   3. gcc compiles the emitted C to a binary without errors.
//
// This is the guardrail that was missing when the M9 source files were first
// written — it catches emitter bugs that checkSource cannot see.
func emitSource(t *testing.T, path string) {
	t.Helper()
	skipIfNoCC(t)

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	tokens, err := lexer.Tokenize(path, string(src))
	if err != nil {
		t.Fatalf("lex %s: %v", path, err)
	}
	file, err := parser.Parse(path, tokens)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	// Use CheckProgram (multi-file mode) so that module-qualified C names
	// (e.g. typeck_Type instead of plain Type) match what the emitter produces.
	// typeck.Check (single-file mode) registers structs with plain names, which
	// disagrees with the emitter's module-aware moduleCName() prefix.
	res, err := typeck.CheckProgram([]*parser.File{file})
	if err != nil {
		t.Fatalf("typeck %s: %v", path, err)
	}
	cSrc, err := emit_c.Emit(file, res)
	if err != nil {
		t.Fatalf("emit_c %s: %v", path, err)
	}
	if len(cSrc) == 0 {
		t.Fatalf("emit_c %s: produced empty output", path)
	}

	// Write C to a temp file and compile it.
	dir := t.TempDir()
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	cPath := filepath.Join(dir, base+".c")
	if err := os.WriteFile(cPath, []byte(cSrc), 0o644); err != nil {
		t.Fatalf("write C: %v", err)
	}

	cc := findCC()
	binPath := filepath.Join(dir, base)
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	// -c: compile only (no link) — the M9 sources are libraries, not executables.
	ccCmd := exec.Command(cc, "-c", "-o", binPath+".o", cPath)
	ccCmd.Env = ccEnv(cc)
	out, err := ccCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gcc failed on %s:\n%s\nEmitted C:\n%s", path, out, cSrc)
	}
}

func TestM9LexerEmitC(t *testing.T) {
	emitSource(t, filepath.Join("..", "..", "src", "compiler", "lexer.cnd"))
}

func TestM9ParserEmitC(t *testing.T) {
	emitSource(t, filepath.Join("..", "..", "src", "compiler", "parser.cnd"))
}

func TestM9TypeckEmitC(t *testing.T) {
	// typeck.cnd depends on types from parser.cnd — emit and compile as a merged bundle.
	srcDir := filepath.Join("..", "..", "src", "compiler")
	emitBundle(t,
		filepath.Join(srcDir, "parser.cnd"),
		filepath.Join(srcDir, "typeck.cnd"),
	)
}

// emitBundledSource is like emitSource but type-checks and emits C for all
// paths as a bundle, then compiles the concatenated C output.  Used for bundle
// files that reference types declared in sibling source files.
func emitBundledSource(t *testing.T, paths ...string) {
	t.Helper()
	skipIfNoCC(t)

	var files []*parser.File
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		tokens, err := lexer.Tokenize(path, string(src))
		if err != nil {
			t.Fatalf("lex %s: %v", path, err)
		}
		file, err := parser.Parse(path, tokens)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files = append(files, file)
	}

	res, err := typeck.CheckProgram(files)
	if err != nil {
		t.Fatalf("typeck bundle %v: %v", paths, err)
	}

	// Emit C for every file and concatenate — this mirrors candorc build behaviour
	// and ensures cross-file type references resolve at the C level.
	var combined strings.Builder
	for i, f := range files {
		cSrc, err := emit_c.Emit(f, res)
		if err != nil {
			t.Fatalf("emit_c %s: %v", paths[i], err)
		}
		combined.WriteString(cSrc)
		combined.WriteByte('\n')
	}
	bundleC := combined.String()
	if len(bundleC) == 0 {
		t.Fatalf("emit_c bundle: produced empty output")
	}

	dir := t.TempDir()
	cPath := filepath.Join(dir, "bundle.c")
	if err := os.WriteFile(cPath, []byte(bundleC), 0o644); err != nil {
		t.Fatalf("write C: %v", err)
	}

	cc := findCC()
	objPath := filepath.Join(dir, "bundle.o")
	ccCmd := exec.Command(cc, "-c", "-o", objPath, cPath)
	ccCmd.Env = ccEnv(cc)
	out, err := ccCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gcc failed on bundle %v:\n%s", paths, out)
	}
}

// bundleFileModuleName returns the module name declared in a file, or "" for root namespace.
func bundleFileModuleName(f *parser.File) string {
	for _, d := range f.Decls {
		if md, ok := d.(*parser.ModuleDecl); ok {
			return md.Name.Lexeme
		}
	}
	return ""
}

// mergeFilesForTest combines declarations from multiple parsed files into one
// synthetic File, inserting ModuleDecl boundary markers so the emitter can track
// which module each declaration belongs to.  This mirrors mergeFiles in main.go.
func mergeFilesForTest(name string, files []*parser.File) *parser.File {
	var allDecls []parser.Decl
	seen := map[string]bool{}
	for _, f := range files {
		mod := bundleFileModuleName(f)
		allDecls = append(allDecls, &parser.ModuleDecl{Name: lexer.Token{Lexeme: mod}})
		for _, d := range f.Decls {
			var key string
			switch dd := d.(type) {
			case *parser.ModuleDecl:
				continue
			case *parser.UseDecl:
				_ = dd
				continue
			case *parser.FnDecl:
				key = "fn:" + mod + "." + dd.Name.Lexeme
			case *parser.StructDecl:
				key = "struct:" + mod + "." + dd.Name.Lexeme
			case *parser.EnumDecl:
				key = "enum:" + mod + "." + dd.Name.Lexeme
			case *parser.ConstDecl:
				key = "const:" + mod + "." + dd.Name.Lexeme
			}
			if key != "" && seen[key] {
				continue
			}
			if key != "" {
				seen[key] = true
			}
			allDecls = append(allDecls, d)
		}
	}
	return &parser.File{Name: name, Decls: allDecls}
}

// emitBundle compiles multiple .cnd files together (as a merged bundle) through the
// full pipeline.  Files must be given in dependency order.  All parsed ASTs are merged
// into a single synthetic File (matching the cmdBuild mergeFiles approach) before
// calling emit_c.Emit, producing a single well-ordered C translation unit.
func emitBundle(t *testing.T, paths ...string) {
	t.Helper()
	skipIfNoCC(t)

	var files []*parser.File
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		toks, err := lexer.Tokenize(path, string(src))
		if err != nil {
			t.Fatalf("lex %s: %v", path, err)
		}
		f, err := parser.Parse(path, toks)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files = append(files, f)
	}

	res, err := typeck.CheckProgram(files)
	if err != nil {
		t.Fatalf("typeck bundle: %v", err)
	}

	merged := mergeFilesForTest("bundle", files)
	cAll, err := emit_c.Emit(merged, res)
	if err != nil {
		t.Fatalf("emit_c bundle: %v", err)
	}
	if len(cAll) == 0 {
		t.Fatal("emitBundle: produced empty output")
	}

	dir := t.TempDir()
	cPath := filepath.Join(dir, "bundle.c")
	if err := os.WriteFile(cPath, []byte(cAll), 0o644); err != nil {
		t.Fatalf("write C: %v", err)
	}

	cc := findCC()
	oPath := filepath.Join(dir, "bundle.o")
	ccCmd := exec.Command(cc, "-c", "-o", oPath, cPath)
	ccCmd.Env = ccEnv(cc)
	out, err := ccCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gcc failed on bundle:\n%s\nEmitted C (first 4000 chars):\n%.4000s", out, cAll)
	}
}

func TestM9EmitCCndEmitC(t *testing.T) {
	srcDir := filepath.Join("..", "..", "src", "compiler")
	emitBundle(t,
		filepath.Join(srcDir, "lexer.cnd"),
		filepath.Join(srcDir, "parser.cnd"),
		filepath.Join(srcDir, "typeck.cnd"),
		filepath.Join(srcDir, "emit_c.cnd"),
	)
}

// ── M9.11: multi-source entry point — main.cnd added to the full bundle ───────

// TestM9MainCndSource verifies that the complete stage1 compiler source bundle
// (lexer + parser + typeck + emit_c + main) type-checks without errors.
func TestM9MainCndSource(t *testing.T) {
	srcDir := filepath.Join("..", "..", "src", "compiler")
	checkBundledSource(t,
		filepath.Join(srcDir, "lexer.cnd"),
		filepath.Join(srcDir, "parser.cnd"),
		filepath.Join(srcDir, "typeck.cnd"),
		filepath.Join(srcDir, "emit_c.cnd"),
		filepath.Join(srcDir, "main.cnd"),
	)
}

// TestM9MainCndEmitC verifies that the complete stage1 compiler source bundle
// emits valid C that gcc can compile.
func TestM9MainCndEmitC(t *testing.T) {
	srcDir := filepath.Join("..", "..", "src", "compiler")
	emitBundle(t,
		filepath.Join(srcDir, "lexer.cnd"),
		filepath.Join(srcDir, "parser.cnd"),
		filepath.Join(srcDir, "typeck.cnd"),
		filepath.Join(srcDir, "emit_c.cnd"),
		filepath.Join(srcDir, "main.cnd"),
	)
}
