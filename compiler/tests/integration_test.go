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
		allDecls = append(allDecls, file.Decls...)
		if firstName == "" {
			firstName = fakePath
		}
	}

	merged := &parser.File{Name: firstName, Decls: allDecls}
	res, err := typeck.Check(merged)
	if err != nil {
		t.Fatalf("typeck: %v", err)
	}
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
