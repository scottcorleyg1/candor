// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg/candor

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	emit_c "github.com/scottcorleyg1/candor/compiler/emit_c"
	"github.com/scottcorleyg1/candor/compiler/diagnostics"
	"github.com/scottcorleyg1/candor/compiler/emit_llvm"
	"github.com/scottcorleyg1/candor/compiler/lexer"
	"github.com/scottcorleyg1/candor/compiler/lsp"
	"github.com/scottcorleyg1/candor/compiler/manifest"
	"github.com/scottcorleyg1/candor/compiler/parser"
	"github.com/scottcorleyg1/candor/compiler/typeck"
)

const usage = `Candor compiler toolchain

Usage:
  candorc <file.cnd> [file.cnd ...]          compile one or more source files directly
  candorc build                               build the project in the current directory
  candorc build --debug                       build with debug info (-g -O0); assertions on
  candorc build --release                     build with optimizations (-O2); assertions off
  candorc build --backend=llvm                build via LLVM IR text (requires clang)
  candorc build --sanitize=<kind>             enable sanitizer(s): address, undefined, memory, leak, thread
  candorc build --target=<triple>             cross-compile for a specific target triple
  candorc fmt   [file.cnd ...]                format source files in-place
  candorc test  [file.cnd ...]                run #test-annotated functions
  candorc lsp                                 start the LSP server (stdin/stdout, JSON-RPC 2.0)

Flags may be combined: candorc build --debug --backend=llvm --sanitize=address,undefined
Target examples: aarch64-unknown-linux-gnu  x86_64-apple-macosx14.0  wasm32-unknown-unknown

A project is identified by a Candor.toml manifest file.`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "build":
		err = cmdBuild(os.Args[2:])
	case "fmt":
		err = cmdFmt(os.Args[2:])
	case "test":
		err = cmdTest(os.Args[2:])
	case "lsp":
		srv := lsp.New(os.Stdin, os.Stdout)
		err = srv.Run()
	case "help", "--help", "-h":
		fmt.Println(usage)
	default:
		// Legacy: candorc file.cnd [...] [--debug] [--release] [--sanitize=...]
		err = runCompile(filterFlags(os.Args[1:]), "", BuildConfig{
			Release:    hasFlag(os.Args[1:], "--release"),
			Debug:      hasFlag(os.Args[1:], "--debug"),
			Backend:    "c",
			Sanitizers: parseSanitizers(os.Args[1:]),
			Target:     parseTarget(os.Args[1:]),
		})
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "candorc: %v\n", err)
		os.Exit(1)
	}
}

// ── candorc build ─────────────────────────────────────────────────────────────

func cmdBuild(args []string) error {
	cfg := BuildConfig{
		Release:    hasFlag(args, "--release"),
		Debug:      hasFlag(args, "--debug"),
		Backend:    "c",
		Sanitizers: parseSanitizers(args),
		Target:     parseTarget(args),
	}
	for _, a := range args {
		if a == "--backend=llvm" {
			cfg.Backend = "llvm"
		}
	}

	manifestPath, err := manifest.FindManifest(".")
	if err != nil {
		return err
	}
	if manifestPath == "" {
		return fmt.Errorf("no Candor.toml found in current directory or any parent")
	}

	m, err := manifest.Load(manifestPath)
	if err != nil {
		return err
	}

	srcFiles, err := m.SourceFiles()
	if err != nil {
		return err
	}
	if len(srcFiles) == 0 {
		return fmt.Errorf("no .cnd source files found for project %q", m.Name)
	}

	mode := "default"
	if cfg.Release {
		mode = "release"
	} else if cfg.Debug {
		mode = "debug"
	}
	outPath := m.OutputPath(isWindows())
	fmt.Printf("candorc build: %s v%s → %s (backend=%s, mode=%s)\n", m.Name, m.Version, outPath, cfg.Backend, mode)
	if cfg.Backend == "llvm" {
		return runCompileLLVM(srcFiles, outPath, cfg)
	}
	return runCompile(srcFiles, outPath, cfg)
}

// hasFlag reports whether flag appears in args.
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// filterFlags returns args with all --flag entries removed.
func filterFlags(args []string) []string {
	var out []string
	for _, a := range args {
		if !strings.HasPrefix(a, "--") {
			out = append(out, a)
		}
	}
	return out
}

// ── candorc fmt ───────────────────────────────────────────────────────────────

func cmdFmt(args []string) error {
	targets, err := resolveTargets(args)
	if err != nil {
		return err
	}
	formatted := 0
	for _, path := range targets {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(raw)
		tokens, err := lexer.Tokenize(path, src)
		if err != nil {
			return err
		}
		file, err := parser.Parse(path, tokens)
		if err != nil {
			return err
		}
		out := emit_c.FormatCandor(file)
		if out == src {
			continue // already formatted
		}
		if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
			return err
		}
		fmt.Printf("fmt: %s\n", path)
		formatted++
	}
	if formatted == 0 {
		fmt.Println("fmt: all files already formatted")
	}
	return nil
}

// testFn identifies a #test-annotated function.
type testFn struct {
	file string
	name string
}

// ── candorc test ──────────────────────────────────────────────────────────────

func cmdTest(args []string) error {
	targets, err := resolveTargets(args)
	if err != nil {
		return err
	}

	// Parse and collect #test functions.
	var tests []testFn
	for _, path := range targets {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		tokens, err := lexer.Tokenize(path, string(raw))
		if err != nil {
			return err
		}
		file, err := parser.Parse(path, tokens)
		if err != nil {
			return err
		}
		for _, d := range file.Decls {
			if fn, ok := d.(*parser.FnDecl); ok {
				for _, dir := range fn.Directives {
					if dir == "test" {
						tests = append(tests, testFn{file: path, name: fn.Name.Lexeme})
					}
				}
			}
		}
	}

	if len(tests) == 0 {
		fmt.Println("test: no #test functions found")
		return nil
	}

	// Build a test harness that calls each test function and reports results.
	return runTestHarness(targets, tests)
}

// runTestHarness builds and runs a temporary test binary.
func runTestHarness(srcFiles []string, tests []testFn) error {
	// Parse + type-check all source files.
	srcs := make(map[string]string)
	files := make([]*parser.File, 0, len(srcFiles))
	for _, path := range srcFiles {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(raw)
		srcs[path] = src
		tokens, err := lexer.Tokenize(path, src)
		if err != nil {
			return err
		}
		file, err := parser.Parse(path, tokens)
		if err != nil {
			return err
		}
		files = append(files, file)
	}
	sm := diagnostics.NewSourceMap(srcs)
	res, err := typeck.CheckProgram(files)
	printWarnings(res, sm)
	if err != nil {
		return renderTypeckError(err, sm)
	}

	var allDecls []parser.Decl
	for _, f := range files {
		allDecls = append(allDecls, f.Decls...)
	}
	merged := &parser.File{Name: srcFiles[0], Decls: allDecls}

	cSrc, err := emit_c.EmitTests(merged, res)
	if err != nil {
		return err
	}

	// Write to a temp file.
	tmp, err := os.CreateTemp("", "candorc_test_*.c")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(cSrc); err != nil {
		return err
	}
	tmp.Close()

	binPath := strings.TrimSuffix(tmpPath, ".c")
	if isWindows() {
		binPath += ".exe"
	}
	defer os.Remove(binPath)

	cc := findCC()
	cmd := exec.Command(cc, "-o", binPath, tmpPath)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("test build failed: %w", err)
	}

	run := exec.Command(binPath)
	run.Stdout = os.Stdout
	run.Stderr = os.Stderr
	return run.Run()
}

// resolveTargets returns the .cnd files to operate on.
// If no args given, discovers all .cnd files via Candor.toml (or src/).
func resolveTargets(args []string) ([]string, error) {
	var targets []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			targets = append(targets, a)
		}
	}
	if len(targets) > 0 {
		return targets, nil
	}
	// Auto-discover via manifest.
	manifestPath, err := manifest.FindManifest(".")
	if err != nil {
		return nil, err
	}
	if manifestPath == "" {
		// Fall back to current directory glob.
		return filepath.Glob("*.cnd")
	}
	m, err := manifest.Load(manifestPath)
	if err != nil {
		return nil, err
	}
	return m.SourceFiles()
}

// ── LLVM compile pipeline ─────────────────────────────────────────────────────

func runCompileLLVM(srcPaths []string, outPath string, cfg BuildConfig) error {
	srcs := make(map[string]string, len(srcPaths))
	files := make([]*parser.File, 0, len(srcPaths))
	for _, srcPath := range srcPaths {
		raw, err := os.ReadFile(srcPath)
		if err != nil {
			return err
		}
		src := string(raw)
		srcs[srcPath] = src
		tokens, err := lexer.Tokenize(srcPath, src)
		if err != nil {
			return renderLexParseError(err, srcPath, src)
		}
		file, err := parser.Parse(srcPath, tokens)
		if err != nil {
			return renderLexParseError(err, srcPath, src)
		}
		files = append(files, file)
	}
	sm := diagnostics.NewSourceMap(srcs)

	res, err := typeck.CheckProgram(files)
	printWarnings(res, sm)
	if err != nil {
		return renderTypeckError(err, sm)
	}

	var allDecls []parser.Decl
	for _, f := range files {
		allDecls = append(allDecls, f.Decls...)
	}
	merged := &parser.File{Name: srcPaths[0], Decls: allDecls}

	llSrc, err := emit_llvm.EmitLLVM(merged, res, cfg.Target)
	if err != nil {
		return err
	}

	base := strings.TrimSuffix(srcPaths[0], filepath.Ext(srcPaths[0]))
	llPath := base + ".ll"
	if err := os.WriteFile(llPath, []byte(llSrc), 0o644); err != nil {
		return err
	}

	if outPath == "" {
		outPath = base
		if isWindows() {
			outPath += ".exe"
		}
	}

	clang := findClang()
	clangArgs := []string{"-o", outPath, llPath}
	clangArgs = append(cfg.ccFlags(), clangArgs...)
	cmd := exec.Command(clang, clangArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clang failed: %w", err)
	}
	fmt.Printf("candorc: wrote %s\n", outPath)
	return nil
}

func findClang() string {
	if cc := os.Getenv("CLANG"); cc != "" {
		return cc
	}
	if path, err := exec.LookPath("clang"); err == nil {
		return path
	}
	return "clang"
}

// ── Core compile pipeline ─────────────────────────────────────────────────────

func runCompile(srcPaths []string, outPath string, cfg BuildConfig) error {
	srcs := make(map[string]string, len(srcPaths))
	files := make([]*parser.File, 0, len(srcPaths))
	for _, srcPath := range srcPaths {
		raw, err := os.ReadFile(srcPath)
		if err != nil {
			return err
		}
		src := string(raw)
		srcs[srcPath] = src
		tokens, err := lexer.Tokenize(srcPath, src)
		if err != nil {
			return renderLexParseError(err, srcPath, src)
		}
		file, err := parser.Parse(srcPath, tokens)
		if err != nil {
			return renderLexParseError(err, srcPath, src)
		}
		files = append(files, file)
	}
	sm := diagnostics.NewSourceMap(srcs)

	res, err := typeck.CheckProgram(files)
	printWarnings(res, sm)
	if err != nil {
		return renderTypeckError(err, sm)
	}

	var allDecls []parser.Decl
	for _, f := range files {
		allDecls = append(allDecls, f.Decls...)
	}
	merged := &parser.File{Name: srcPaths[0], Decls: allDecls}

	cSrc, err := emit_c.Emit(merged, res)
	if err != nil {
		return err
	}

	base := strings.TrimSuffix(srcPaths[0], filepath.Ext(srcPaths[0]))
	cPath := base + ".c"
	if err := os.WriteFile(cPath, []byte(cSrc), 0o644); err != nil {
		return err
	}

	if outPath == "" {
		outPath = base
		if isWindows() {
			outPath += ".exe"
		}
	}

	cc := findCC()
	ccArgs := []string{"-o", outPath, cPath}
	ccArgs = append(cfg.ccFlags(), ccArgs...)
	cmd := exec.Command(cc, ccArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("C compiler %q failed: %w", cc, err)
	}
	fmt.Printf("candorc: wrote %s\n", outPath)
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func printWarnings(res *typeck.Result, sm diagnostics.SourceMap) {
	if res == nil {
		return
	}
	for _, w := range res.Warnings {
		d := diagnostics.Diag{
			Severity: diagnostics.SeverityWarning,
			File:     w.Tok.File,
			Line:     w.Tok.Line,
			Col:      w.Tok.Col,
			Msg:      w.Msg,
		}
		fmt.Fprintln(os.Stderr, d.Render(sm))
	}
}

func renderLexParseError(err error, file, src string) error {
	sm := diagnostics.NewSourceMap(map[string]string{file: src})
	switch e := err.(type) {
	case *lexer.Error:
		d := &diagnostics.Diag{Severity: diagnostics.SeverityError, File: e.File, Line: e.Line, Col: e.Col, Msg: e.Msg}
		return fmt.Errorf("%s", d.Render(sm))
	default:
		return err
	}
}

func renderTypeckError(err error, sm diagnostics.SourceMap) error {
	type multiErr interface {
		Unwrap() []error
	}
	var errs []error
	if me, ok := err.(multiErr); ok {
		errs = me.Unwrap()
	} else {
		errs = []error{err}
	}
	var parts []string
	for _, e := range errs {
		if te, ok := e.(*typeck.Error); ok {
			d := diagnostics.Diag{
				Severity: diagnostics.SeverityError,
				File:     te.Tok.File,
				Line:     te.Tok.Line,
				Col:      te.Tok.Col,
				Msg:      te.Msg,
				Hint:     te.Hint,
			}
			parts = append(parts, d.Render(sm))
		} else {
			parts = append(parts, e.Error())
		}
	}
	return fmt.Errorf("%s", strings.Join(parts, "\n"))
}

// BuildConfig holds all user-selected build options.
type BuildConfig struct {
	Release    bool
	Debug      bool
	Backend    string   // "c" or "llvm"
	Sanitizers []string // e.g. ["address", "undefined"]
	Target     string   // LLVM target triple, e.g. "aarch64-unknown-linux-gnu" (empty = host)
}

// ccFlags returns the CC/clang flags implied by the config.
// release: -O2 -DNDEBUG
// debug or any sanitizer: -g -O0
// sanitizers: -fsanitize=<kind> [+ extra flags per sanitizer]
func (cfg BuildConfig) ccFlags() []string {
	var flags []string
	if cfg.Release {
		flags = append(flags, "-O2", "-DNDEBUG")
	} else if cfg.Debug || len(cfg.Sanitizers) > 0 {
		flags = append(flags, "-g", "-O0")
	}
	for _, s := range cfg.Sanitizers {
		switch s {
		case "address":
			flags = append(flags, "-fsanitize=address", "-fno-omit-frame-pointer")
		case "undefined", "ub":
			flags = append(flags, "-fsanitize=undefined")
		case "memory":
			flags = append(flags, "-fsanitize=memory")
		case "leak":
			flags = append(flags, "-fsanitize=leak")
		case "thread":
			flags = append(flags, "-fsanitize=thread")
		}
	}
	if cfg.Target != "" {
		flags = append(flags, "--target="+cfg.Target)
	}
	return flags
}

// parseTarget extracts the target triple from a --target=<triple> flag.
func parseTarget(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "--target=") {
			return strings.TrimPrefix(a, "--target=")
		}
	}
	return ""
}

// parseSanitizers extracts sanitizer names from --sanitize=a,b,c flags.
func parseSanitizers(args []string) []string {
	var out []string
	for _, a := range args {
		if strings.HasPrefix(a, "--sanitize=") {
			for _, s := range strings.Split(strings.TrimPrefix(a, "--sanitize="), ",") {
				if s != "" {
					out = append(out, s)
				}
			}
		}
	}
	return out
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

func isWindows() bool {
	return os.PathSeparator == '\\'
}
