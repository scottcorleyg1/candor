// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/candor-core/candor

package emit_c

// End-to-end tests: Candor source → C → gcc → run → assert stdout.
//
// These tests require a working C compiler on the host. They are skipped
// automatically when none is found, so CI on environments without gcc/clang
// still passes cleanly.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/candor-core/candor/compiler/lexer"
	"github.com/candor-core/candor/compiler/parser"
	"github.com/candor-core/candor/compiler/typeck"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// e2eCC returns the C compiler to use, or "" if none is available.
func e2eCC() string {
	if cc := os.Getenv("CC"); cc != "" {
		return cc
	}
	if path, err := exec.LookPath("gcc"); err == nil {
		return path
	}
	if path, err := exec.LookPath("clang"); err == nil {
		return path
	}
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
	return ""
}

// e2eCCEnv builds an environment that lets a MinGW gcc find its own DLLs
// by prepending its own bin directory to PATH.
func e2eCCEnv(cc string) []string {
	env := os.Environ()
	dir := filepath.Dir(cc)
	pathVar := os.Getenv("PATH")
	if dir != "." && !strings.Contains(pathVar, dir) {
		for i, e := range env {
			if strings.EqualFold(strings.SplitN(e, "=", 2)[0], "PATH") {
				env[i] = e + string(os.PathListSeparator) + dir
				break
			}
		}
	}
	return env
}

// runE2E compiles src through the full pipeline, then builds and runs the
// resulting binary.  Returns (stdout, nil) on success, or ("", error).
//
// The test is skipped (not failed) if no C compiler is found.
func runE2E(t *testing.T, src string) string {
	t.Helper()

	cc := e2eCC()
	if cc == "" {
		t.Skip("no C compiler found — skipping e2e test")
	}

	// ── 1. Candor → C ─────────────────────────────────────────────────────
	tokens, err := lexer.Tokenize("<e2e>", src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	file, err := parser.Parse("<e2e>", tokens)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	res, err := typeck.Check(file)
	if err != nil {
		t.Fatalf("typeck: %v", err)
	}
	cSrc, err := Emit(file, res)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	// ── 2. Write C to a temp file ──────────────────────────────────────────
	tmp, err := os.CreateTemp("", "candor_e2e_*.c")
	if err != nil {
		t.Fatalf("tempfile: %v", err)
	}
	cPath := tmp.Name()
	defer os.Remove(cPath)
	if _, err := fmt.Fprint(tmp, cSrc); err != nil {
		t.Fatalf("write c: %v", err)
	}
	tmp.Close()

	binPath := strings.TrimSuffix(cPath, ".c")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	defer os.Remove(binPath)

	// ── 3. Compile C → binary ──────────────────────────────────────────────
	compileCmd := exec.Command(cc, "-o", binPath, cPath)
	compileCmd.Env = e2eCCEnv(cc)
	var compileStderr bytes.Buffer
	compileCmd.Stderr = &compileStderr
	if err := compileCmd.Run(); err != nil {
		t.Fatalf("gcc failed: %v\nstderr:\n%s\nC source:\n%s", err, compileStderr.String(), cSrc)
	}

	// ── 4. Run binary ──────────────────────────────────────────────────────
	runCmd := exec.Command(binPath)
	runCmd.Env = e2eCCEnv(cc) // runtime libs (e.g. libgcc_s on MinGW)
	var stdout, stderr bytes.Buffer
	runCmd.Stdout = &stdout
	runCmd.Stderr = &stderr
	if err := runCmd.Run(); err != nil {
		t.Fatalf("binary failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	return stdout.String()
}

// assertOutput checks that the e2e output equals want exactly.
func assertOutput(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("wrong output:\n  got:  %q\n  want: %q", got, want)
	}
}

// ── Tests ──────────────────────────────────────────────────────────────────────

func TestE2EHelloWorld(t *testing.T) {
	src := `
fn main() -> unit {
    print("hello world")
    return unit
}
`
	out := runE2E(t, src)
	assertOutput(t, strings.TrimRight(out, "\r\n"), "hello world")
}

func TestE2EArithmetic(t *testing.T) {
	src := `
fn add(a: i32, b: i32) -> i32 { return a + b }

fn main() -> unit {
    let r = add(3, 4)
    print(int_to_str(r))
    return unit
}
`
	out := runE2E(t, src)
	assertOutput(t, strings.TrimRight(out, "\r\n"), "7")
}

func TestE2EMatchReturn(t *testing.T) {
	// Exercises the tail-match implicit-return fix (Fix 1).
	src := `
fn flip(b: bool) -> bool { match b { true => false   false => true } }

fn main() -> unit {
    let x = flip(true)
    match x {
        true  => print("yes")
        false => print("no")
    }
    return unit
}
`
	out := runE2E(t, src)
	assertOutput(t, strings.TrimRight(out, "\r\n"), "no")
}

func TestE2EOptionMust(t *testing.T) {
	// Exercises must-expression tail return and option<T> builtins.
	src := `
fn unwrap_or(x: option<i32>, def: i32) -> i32 {
    x must { some(v) => v   none => def }
}

fn main() -> unit {
    let a = unwrap_or(some(42), 0)
    let b = unwrap_or(none, 99)
    print(int_to_str(a))
    print(int_to_str(b))
    return unit
}
`
	out := runE2E(t, src)
	if !strings.Contains(out, "42") || !strings.Contains(out, "99") {
		t.Errorf("expected output to contain '42' and '99', got: %q", out)
	}
}

func TestE2EVecPushAndLen(t *testing.T) {
	src := `
fn main() -> unit {
    let v: vec<i32> = vec_new()
    vec_push(v, 10)
    vec_push(v, 20)
    vec_push(v, 30)
    let n: i64 = vec_len(v) as i64
    print(int_to_str(n))
    return unit
}
`
	out := runE2E(t, src)
	assertOutput(t, strings.TrimRight(out, "\r\n"), "3")
}

func TestE2ESpawnJoinUnit(t *testing.T) {
	// spawn { } returning unit — verifies basic pthread round-trip.
	src := `
fn main() -> unit {
    let t = spawn { print("from thread")   return unit }
    let r = t.join()
    match r {
        ok(_)    => print("joined ok")
        err(msg) => print(msg)
    }
    return unit
}
`
	out := runE2E(t, src)
	if !strings.Contains(out, "from thread") {
		t.Errorf("expected 'from thread' in output, got: %q", out)
	}
	if !strings.Contains(out, "joined ok") {
		t.Errorf("expected 'joined ok' in output, got: %q", out)
	}
}

func TestE2ESpawnJoinResult(t *testing.T) {
	// spawn { } returning a value — verifies result propagation.
	src := `
fn main() -> unit {
    let t = spawn { return 42 }
    let r = t.join()
    match r {
        ok(v)    => print(int_to_str(v))
        err(msg) => print(msg)
    }
    return unit
}
`
	out := runE2E(t, src)
	assertOutput(t, strings.TrimRight(out, "\r\n"), "42")
}
