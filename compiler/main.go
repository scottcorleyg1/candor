// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg/candor

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	emit_c "github.com/candor-core/candor/compiler/emit_c"
	"github.com/candor-core/candor/compiler/emit_go"
	"github.com/candor-core/candor/compiler/cheader"
	"github.com/candor-core/candor/compiler/diagnostics"
	"github.com/candor-core/candor/compiler/doc"
	"github.com/candor-core/candor/compiler/emit_llvm"
	"github.com/candor-core/candor/compiler/lexer"
	"github.com/candor-core/candor/compiler/lsp"
	"github.com/candor-core/candor/compiler/manifest"
	"github.com/candor-core/candor/compiler/parser"
	"github.com/candor-core/candor/compiler/typeck"
)

const usage = `Candor compiler toolchain

Usage:
  candorc <file.cnd> [file.cnd ...]          compile one or more source files directly
  candorc build                               build the project in the current directory
  candorc build --debug                       build with debug info (-g -O0); assertions on
  candorc build --release                     build with optimizations (-O2); assertions off
  candorc build --backend=llvm                build via LLVM IR text (requires clang)
  candorc build --simd                        enable auto-vectorization for effects(simd) functions (-O3 -ftree-vectorize -march=native)
  candorc build --sanitize=<kind>             enable sanitizer(s): address, undefined, memory, leak, thread
  candorc build --target=<triple>             cross-compile for a specific target triple
  candorc build --target=wasm32               compile to WebAssembly (.wasm via clang/wasm-ld)
  candorc fetch                               download and pin all [dependencies] → Candor.lock
  candorc fmt   [file.cnd ...]                format source files in-place
  candorc test  [file.cnd ...]                run #test-annotated functions
  candorc lsp                                 start the LSP server (stdin/stdout, JSON-RPC 2.0)
  candorc mcp   [file.cnd ...]                emit tools.json MCP manifest for #mcp_tool functions
  candorc doc   [file.cnd ...]                emit intent.json for #intent-annotated functions
  candorc doc   --html [file.cnd ...]         emit docs.html API reference from /// doc comments

Flags may be combined: candorc build --release --simd --backend=llvm --sanitize=address,undefined
Target examples: aarch64-unknown-linux-gnu  x86_64-apple-macosx14.0  wasm32 (alias for wasm32-unknown-unknown)

[dependencies] in Candor.toml:
  mylib      = "path:../mylib"
  remote-pkg = "git:https://github.com/user/repo@v1.0.0"

C/CUDA header interop — place at the top of any .cnd source file:
  #c_header "path/to/header.h"
  Generates extern fn stubs for every C function prototype in the header.
  Path is relative to the directory containing the .cnd file.
  Supports: int/uint/long/float/double/size_t/explicit-width types, pointer types,
            void params, preprocessor guards (skipped), line/block comments.

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
	case "mcp":
		err = cmdMcp(os.Args[2:])
	case "doc":
		err = cmdDoc(os.Args[2:])
	case "fetch":
		err = cmdFetch(os.Args[2:])
	case "audit":
		err = cmdAudit(os.Args[2:])
	case "emit-go":
		err = cmdEmitGo(os.Args[2:])
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
		SIMD:       hasFlag(args, "--simd"),
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

	// Include source files from locked dependencies (prepend so they are
	// compiled as library modules before the project's own sources).
	if len(m.Deps) > 0 {
		lf, err := manifest.LoadLock(manifest.LockPath(m.Dir))
		if err != nil {
			return fmt.Errorf("loading Candor.lock: %w (run 'candorc fetch')", err)
		}
		var depFiles []string
		for _, dep := range m.Deps {
			lp := lf.Find(dep.Name)
			if lp == nil {
				return fmt.Errorf("dependency %q not in Candor.lock — run 'candorc fetch'", dep.Name)
			}
			df, err := manifest.DepSourceFiles(lp.Resolved)
			if err != nil {
				return fmt.Errorf("dep %q: reading sources from %s: %w", dep.Name, lp.Resolved, err)
			}
			depFiles = append(depFiles, df...)
		}
		srcFiles = append(depFiles, srcFiles...)
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

// ── candorc fetch ─────────────────────────────────────────────────────────────

// cmdFetch downloads and pins all [dependencies] declared in Candor.toml,
// writing the resolved paths and git revisions to Candor.lock.
func cmdFetch(args []string) error {
	_ = args // no flags yet

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

	if len(m.Deps) == 0 {
		fmt.Println("candorc fetch: no dependencies declared.")
		return nil
	}

	// Load existing lock so we can skip already-resolved deps.
	lockPath := manifest.LockPath(m.Dir)
	lf, err := manifest.LoadLock(lockPath)
	if err != nil {
		return fmt.Errorf("loading Candor.lock: %w", err)
	}

	cacheDir, err := candorCacheDir()
	if err != nil {
		return err
	}

	for _, dep := range m.Deps {
		kind, loc, version := manifest.ParseDep(dep.Source)
		switch kind {
		case manifest.DepPath:
			resolved := loc
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(m.Dir, resolved)
			}
			if _, err := os.Stat(resolved); err != nil {
				return fmt.Errorf("dep %q: path %q does not exist", dep.Name, resolved)
			}
			upsertLocked(lf, manifest.LockedPackage{
				Name:     dep.Name,
				Source:   dep.Source,
				Resolved: resolved,
			})
			fmt.Printf("candorc fetch: %s → %s (path)\n", dep.Name, resolved)

		case manifest.DepGit:
			// Target dir: ~/.candor/pkg/<name>/<version>/
			safeVer := version
			if safeVer == "" {
				safeVer = "latest"
			}
			targetDir := filepath.Join(cacheDir, dep.Name, safeVer)

			// If already cloned, pull latest on the same ref instead of re-cloning.
			rev := ""
			if _, err := os.Stat(filepath.Join(targetDir, ".git")); err == nil {
				rev, _ = gitRevParse(targetDir, "HEAD")
				fmt.Printf("candorc fetch: %s already cached at %s (rev %s)\n", dep.Name, targetDir, shortRev(rev))
			} else {
				if err := os.MkdirAll(targetDir, 0o755); err != nil {
					return fmt.Errorf("dep %q: mkdir %s: %w", dep.Name, targetDir, err)
				}
				ref := version
				if ref == "" {
					ref = "HEAD"
				}
				fmt.Printf("candorc fetch: cloning %s@%s → %s\n", loc, ref, targetDir)
				cmd := exec.Command("git", "clone", "--depth=1", "--branch="+ref, loc, targetDir)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					// Try without --branch (bare clone HEAD) if tag not found.
					cmd2 := exec.Command("git", "clone", "--depth=1", loc, targetDir)
					cmd2.Stdout = os.Stdout
					cmd2.Stderr = os.Stderr
					if err2 := cmd2.Run(); err2 != nil {
						return fmt.Errorf("dep %q: git clone failed: %w", dep.Name, err)
					}
				}
				rev, _ = gitRevParse(targetDir, "HEAD")
			}
			upsertLocked(lf, manifest.LockedPackage{
				Name:     dep.Name,
				Source:   dep.Source,
				Resolved: targetDir,
				Rev:      rev,
			})
			fmt.Printf("candorc fetch: %s → %s (rev %s)\n", dep.Name, targetDir, shortRev(rev))

		default:
			return fmt.Errorf("dep %q: unrecognized source scheme %q (expected path: or git:)", dep.Name, dep.Source)
		}
	}

	if err := manifest.WriteLock(lockPath, lf); err != nil {
		return fmt.Errorf("writing Candor.lock: %w", err)
	}
	fmt.Printf("candorc fetch: wrote %s\n", lockPath)
	return nil
}

// upsertLocked replaces an existing entry or appends a new one.
func upsertLocked(lf *manifest.LockFile, pkg manifest.LockedPackage) {
	for i := range lf.Packages {
		if lf.Packages[i].Name == pkg.Name {
			lf.Packages[i] = pkg
			return
		}
	}
	lf.Packages = append(lf.Packages, pkg)
}

// candorCacheDir returns ~/.candor/pkg, creating it if needed.
func candorCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".candor", "pkg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create cache dir %s: %w", dir, err)
	}
	return dir, nil
}

// gitRevParse runs "git rev-parse <ref>" in dir and returns the SHA.
func gitRevParse(dir, ref string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", ref)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// shortRev returns the first 8 chars of a git SHA, or the full string if shorter.
func shortRev(rev string) string {
	if len(rev) > 8 {
		return rev[:8]
	}
	return rev
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

// ── candorc audit ─────────────────────────────────────────────────────────────

// cmdAudit compiles .cnd files and emits both a .c file and a .audit.md report.
// The report documents every Candor safety feature (effects, requires, ensures,
// must{}, pure, secret<T>) that has no equivalent in C, with per-instance line
// references back to the Candor source.
func cmdAudit(args []string) error {
	targets, err := resolveTargets(filterFlags(args))
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return fmt.Errorf("audit: no .cnd files specified")
	}

	srcs := make(map[string]string)
	var files []*parser.File
	for _, srcPath := range targets {
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

	files, err = injectCHeaders(files, srcs)
	if err != nil {
		return err
	}

	sm := diagnostics.NewSourceMap(srcs)
	res, err := typeck.CheckProgram(files)
	printWarnings(res, sm)
	if err != nil {
		return renderTypeckError(err, sm)
	}

	merged := mergeFiles(targets[0], files)
	sourceName := filepath.Base(targets[0])

	cSrc, log, err := emit_c.EmitAudit(merged, res, sourceName)
	if err != nil {
		return err
	}

	base := strings.TrimSuffix(targets[0], filepath.Ext(targets[0]))
	cPath := base + ".c"
	auditPath := base + ".audit.md"

	if err := os.WriteFile(cPath, []byte(cSrc), 0o644); err != nil {
		return err
	}
	// Write runtime header alongside the .c file.
	rtPath := filepath.Join(filepath.Dir(cPath), "_cnd_runtime.h")
	if err := os.WriteFile(rtPath, []byte(emit_c.RuntimeHeader()), 0o644); err != nil {
		return err
	}

	report := log.RenderMarkdown()
	if err := os.WriteFile(auditPath, []byte(report), 0o644); err != nil {
		return err
	}

	fmt.Printf("audit: %s → %s + %s (%d audit entries)\n",
		sourceName, filepath.Base(cPath), filepath.Base(auditPath), len(log.Entries))
	return nil
}

// ── candorc emit-go ───────────────────────────────────────────────────────────

// cmdEmitGo translates a Candor source file to idiomatic Go, producing
// file.go (compilable with `go build`) and file.audit.md (safety report).
func cmdEmitGo(args []string) error {
	targets, err := resolveTargets(filterFlags(args))
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return fmt.Errorf("emit-go: no .cnd files specified")
	}

	srcs := make(map[string]string)
	var files []*parser.File
	for _, srcPath := range targets {
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

	files, err = injectCHeaders(files, srcs)
	if err != nil {
		return err
	}

	sm := diagnostics.NewSourceMap(srcs)
	res, err := typeck.CheckProgram(files)
	printWarnings(res, sm)
	if err != nil {
		return renderTypeckError(err, sm)
	}

	merged := mergeFiles(targets[0], files)
	sourceName := filepath.Base(targets[0])

	goSrc, auditLog, err := emit_go.Emit(merged, res, sourceName)
	if err != nil {
		return err
	}

	base := strings.TrimSuffix(targets[0], filepath.Ext(targets[0]))
	goPath := base + ".go"
	auditPath := base + ".audit.md"

	if err := os.WriteFile(goPath, []byte(goSrc), 0o644); err != nil {
		return err
	}

	report := auditLog.RenderMarkdown()
	if err := os.WriteFile(auditPath, []byte(report), 0o644); err != nil {
		return err
	}

	fmt.Printf("emit-go: %s → %s + %s (%d audit entries)\n",
		sourceName, filepath.Base(goPath), filepath.Base(auditPath), len(auditLog.Entries))
	return nil
}

// ── candorc mcp ──────────────────────────────────────────────────────────────

// cmdMcp collects #mcp_tool-annotated functions and emits an MCP tools.json manifest.
func cmdMcp(args []string) error {
	targets, err := resolveTargets(args)
	if err != nil {
		return err
	}
	type prop struct {
		Type     string `json:"type"`
		Nullable bool   `json:"nullable,omitempty"`
		Items    *prop  `json:"items,omitempty"`
	}
	type inputSchema struct {
		Type       string          `json:"type"`
		Properties map[string]prop `json:"properties,omitempty"`
		Required   []string        `json:"required,omitempty"`
	}
	type tool struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		InputSchema inputSchema `json:"inputSchema"`
	}
	type mcpManifest struct {
		Tools []tool `json:"tools"`
	}
	// propFor converts a Candor type expression string to a JSON Schema prop.
	// Handles: vec<T>→array+items, option<T>→nullable, struct/unknown→object,
	// primitives→string/boolean/number/integer.
	var propFor func(t string) prop
	propFor = func(t string) prop {
		if strings.HasPrefix(t, "vec<") && strings.HasSuffix(t, ">") {
			inner := t[4 : len(t)-1]
			it := propFor(inner)
			return prop{Type: "array", Items: &it}
		}
		if strings.HasPrefix(t, "option<") && strings.HasSuffix(t, ">") {
			inner := t[7 : len(t)-1]
			p := propFor(inner)
			p.Nullable = true
			return p
		}
		return prop{Type: candorTypeToJsonSchema(t)}
	}

	var tools []tool
	for _, path := range targets {
		raw, err := os.ReadFile(path)
		if err != nil { return err }
		tokens, err := lexer.Tokenize(path, string(raw))
		if err != nil { return err }
		file, err := parser.Parse(path, tokens)
		if err != nil { return err }
		for _, d := range file.Decls {
			fn, ok := d.(*parser.FnDecl)
			if !ok { continue }
			isMcp := false
			for _, dir := range fn.Directives {
				if dir == "mcp_tool" { isMcp = true }
			}
			if !isMcp { continue }
			desc := ""
			if fn.DirectiveArgs != nil { desc = fn.DirectiveArgs["mcp_tool"] }
			props := make(map[string]prop)
			var required []string
			for _, p := range fn.Params {
				props[p.Name.Lexeme] = propFor(typeExprStr(p.Type))
				required = append(required, p.Name.Lexeme)
			}
			tools = append(tools, tool{
				Name: fn.Name.Lexeme, Description: desc,
				InputSchema: inputSchema{Type: "object", Properties: props, Required: required},
			})
		}
	}
	out, err := json.MarshalIndent(mcpManifest{Tools: tools}, "", "  ")
	if err != nil { return err }
	outPath := "tools.json"
	if err := os.WriteFile(outPath, out, 0o644); err != nil { return err }
	fmt.Printf("mcp: wrote %s (%d tool(s))\n", outPath, len(tools))
	return nil
}

// candorTypeToJsonSchema converts a Candor type expression string to a JSON Schema
// type name. Returns "string" for unknown/struct types (safe default).
func candorTypeToJsonSchema(t string) string {
	switch t {
	case "str":
		return "string"
	case "bool":
		return "boolean"
	case "f64", "f32", "f16", "bf16":
		return "number"
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "isize", "usize":
		return "integer"
	default:
		// Struct / enum / unknown types → object.
		return "object"
	}
}


// ── candorc doc ──────────────────────────────────────────────────────────────

// cmdDoc collects documentation from source files.
// Without --html: emits intent.json for #intent-annotated functions.
// With --html:    emits docs.html — a full API reference from /// doc comments.
func cmdDoc(args []string) error {
	if hasFlag(args, "--html") {
		return cmdDocHTML(args)
	}

	targets, err := resolveTargets(args)
	if err != nil { return err }
	type fnEntry struct {
		Name      string `json:"name"`
		Intent    string `json:"intent"`
		Signature string `json:"signature"`
	}
	type docManifest struct {
		Functions []fnEntry `json:"functions"`
	}
	var fns []fnEntry
	for _, path := range targets {
		raw, err := os.ReadFile(path)
		if err != nil { return err }
		tokens, err := lexer.Tokenize(path, string(raw))
		if err != nil { return err }
		file, err := parser.Parse(path, tokens)
		if err != nil { return err }
		for _, d := range file.Decls {
			fn, ok := d.(*parser.FnDecl)
			if !ok { continue }
			hasIntent := false
			for _, dir := range fn.Directives {
				if dir == "intent" { hasIntent = true }
			}
			if !hasIntent { continue }
			intent := ""
			if fn.DirectiveArgs != nil { intent = fn.DirectiveArgs["intent"] }
			fns = append(fns, fnEntry{
				Name: fn.Name.Lexeme, Intent: intent, Signature: buildFnSig(fn),
			})
		}
	}
	out, err := json.MarshalIndent(docManifest{Functions: fns}, "", "  ")
	if err != nil { return err }
	outPath := "intent.json"
	if err := os.WriteFile(outPath, out, 0o644); err != nil { return err }
	fmt.Printf("doc: wrote %s (%d function(s))\n", outPath, len(fns))
	return nil
}

// cmdDocHTML generates a self-contained HTML API reference from /// doc comments.
func cmdDocHTML(args []string) error {
	targets, err := resolveTargets(args)
	if err != nil { return err }

	var fileDocs []doc.FileDoc
	for _, path := range targets {
		raw, err := os.ReadFile(path)
		if err != nil { return err }
		src := string(raw)
		tokens, err := lexer.Tokenize(path, src)
		if err != nil { return err }
		file, err := parser.Parse(path, tokens)
		if err != nil { return err }
		fileDocs = append(fileDocs, doc.FileDoc{
			File:        file,
			DocComments: doc.ExtractDocComments(src),
		})
	}

	html := doc.GenHTML(fileDocs)
	outPath := "docs.html"
	if err := os.WriteFile(outPath, []byte(html), 0o644); err != nil { return err }
	fmt.Printf("doc: wrote %s (%d file(s))\n", outPath, len(fileDocs))
	return nil
}

// typeExprStr returns a human-readable string for a TypeExpr node.
func typeExprStr(te parser.TypeExpr) string {
	if te == nil {
		return "unit"
	}
	switch t := te.(type) {
	case *parser.NamedType:
		return t.Name.Lexeme
	case *parser.GenericType:
		s := t.Name.Lexeme + "<"
		for i, p := range t.Params {
			if i > 0 {
				s += ", "
			}
			s += typeExprStr(p)
		}
		return s + ">"
	case *parser.TupleTypeExpr:
		s := "("
		for i, p := range t.Elems {
			if i > 0 {
				s += ", "
			}
			s += typeExprStr(p)
		}
		return s + ")"
	default:
		return fmt.Sprintf("%T", te)
	}
}

func buildFnSig(fn *parser.FnDecl) string {
	sig := "fn " + fn.Name.Lexeme + "("
	for i, p := range fn.Params {
		if i > 0 {
			sig += ", "
		}
		sig += p.Name.Lexeme + ": " + typeExprStr(p.Type)
	}
	sig += ") -> " + typeExprStr(fn.RetType)
	return sig
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

	var errInject error
	files, errInject = injectCHeaders(files, srcs)
	if errInject != nil {
		return errInject
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
		switch {
		case cfg.isWasm():
			outPath += ".wasm"
		case isWindows():
			outPath += ".exe"
		}
	}

	clang := findClang()
	var clangArgs []string
	if cfg.isWasm() {
		// WebAssembly: use clang's wasm32 target with wasm-ld compatible flags.
		// -nostdlib: no host libc; --no-entry: no _start required (library-style);
		// --export-all: export every non-hidden function to the .wasm export table.
		clangArgs = []string{
			"--target=wasm32-unknown-unknown",
			"-nostdlib",
			"-Wl,--no-entry",
			"-Wl,--export-all",
			"-o", outPath, llPath,
		}
		if cfg.Release {
			clangArgs = append([]string{"-O2"}, clangArgs...)
		} else if cfg.Debug {
			clangArgs = append([]string{"-g", "-O0"}, clangArgs...)
		}
	} else {
		clangArgs = append(cfg.ccFlags(), "-o", outPath, llPath)
	}
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

// ── C header injection ────────────────────────────────────────────────────────

// injectCHeaders scans parsed files for CHeaderDecl nodes.  For each one, the
// referenced C header is parsed and the resulting extern fn stubs are prepended
// as a synthetic *parser.File so they are visible during type-checking.
// srcs is updated in-place so diagnostics can locate the synthetic source.
func injectCHeaders(files []*parser.File, srcs map[string]string) ([]*parser.File, error) {
	var stubs []*parser.File
	for _, f := range files {
		srcDir := filepath.Dir(f.Name)
		for _, decl := range f.Decls {
			ch, ok := decl.(*parser.CHeaderDecl)
			if !ok {
				continue
			}
			headerPath := ch.Path
			if !filepath.IsAbs(headerPath) {
				headerPath = filepath.Join(srcDir, headerPath)
			}
			candorSrc, err := cheader.ParseHeader(headerPath)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", f.Name, err)
			}
			synName := "<extern:" + ch.Path + ">"
			srcs[synName] = candorSrc
			tokens, err := lexer.Tokenize(synName, candorSrc)
			if err != nil {
				return nil, fmt.Errorf("c_header stub lex error: %w", err)
			}
			stub, err := parser.Parse(synName, tokens)
			if err != nil {
				return nil, fmt.Errorf("c_header stub parse error: %w", err)
			}
			stubs = append(stubs, stub)
		}
	}
	// Prepend stubs so extern fn declarations are visible to all source files.
	return append(stubs, files...), nil
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

	var err error
	files, err = injectCHeaders(files, srcs)
	if err != nil {
		return err
	}

	sm := diagnostics.NewSourceMap(srcs)

	res, err := typeck.CheckProgram(files)
	printWarnings(res, sm)
	if err != nil {
		return renderTypeckError(err, sm)
	}

	merged := mergeFiles(srcPaths[0], files)

	cSrc, err := emit_c.Emit(merged, res)
	if err != nil {
		return err
	}

	base := strings.TrimSuffix(srcPaths[0], filepath.Ext(srcPaths[0]))
	cPath := base + ".c"
	if err := os.WriteFile(cPath, []byte(cSrc), 0o644); err != nil {
		return err
	}

	// Write _cnd_runtime.h alongside the .c file so that Candor-level emitters
	// (emit_c.cnd) can #include it when compiling their own output.
	rtPath := filepath.Join(filepath.Dir(cPath), "_cnd_runtime.h")
	if err := os.WriteFile(rtPath, []byte(emit_c.RuntimeHeader()), 0o644); err != nil {
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
	cmd.Env = ccEnv(cc)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("c compiler %q failed: %w", cc, err)
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
	SIMD       bool     // enable compiler auto-vectorization for effects(simd) functions
}

// ccFlags returns the CC/clang flags implied by the config.
// release: -O2 -DNDEBUG
// debug or any sanitizer: -g -O0
// sanitizers: -fsanitize=<kind> [+ extra flags per sanitizer]
func (cfg BuildConfig) ccFlags() []string {
	var flags []string
	if cfg.SIMD {
		// Auto-vectorization: let the C compiler select AVX/NEON/WASM SIMD width.
		// Applied before release/debug so release+simd gets both -O2 and tree-vectorize.
		flags = append(flags, "-O3", "-ftree-vectorize", "-march=native")
	}
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
// "wasm32" is normalized to the canonical "wasm32-unknown-unknown" triple.
func parseTarget(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "--target=") {
			t := strings.TrimPrefix(a, "--target=")
			if t == "wasm32" {
				return "wasm32-unknown-unknown"
			}
			return t
		}
	}
	return ""
}

// isWasm reports whether the build targets WebAssembly.
func (cfg BuildConfig) isWasm() bool {
	return strings.HasPrefix(cfg.Target, "wasm32")
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
	// On Windows, check common MSYS2/MinGW installations.
	if isWindows() {
		for _, candidate := range []string{
			`C:\msys64v2026\mingw64\bin\gcc.exe`,
			`C:\msys64v2026\ucrt64\bin\gcc.exe`,
			`C:\msys64\mingw64\bin\gcc.exe`,
			`C:\msys64\ucrt64\bin\gcc.exe`,
			`C:\MinGW\bin\gcc.exe`,
		} {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return "cc"
}

// mergeFiles combines declarations from multiple parsed files into one, deduplicating
// fn/struct/enum/const definitions by name so that shared declarations re-declared
// across module files (e.g. token constants) are emitted only once.
//
// A synthetic ModuleDecl is inserted before each file's declarations so the emitter
// can track which module each struct/enum/fn belongs to (even for root-namespace files
// where the synthetic marker has an empty Name.Lexeme).
func mergeFiles(name string, files []*parser.File) *parser.File {
	var allDecls []parser.Decl
	seen := map[string]bool{}
	for _, f := range files {
		mod := fileModuleName(f)
		// Insert a module boundary marker before this file's declarations.
		allDecls = append(allDecls, &parser.ModuleDecl{Name: lexer.Token{Lexeme: mod}})
		for _, d := range f.Decls {
			var key string
			switch dd := d.(type) {
			case *parser.ModuleDecl:
				continue // skip original; synthetic boundary above replaces it
			case *parser.UseDecl:
				_ = dd
				continue // use-decls are for the typechecker only; not needed in merged output
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

// fileModuleName returns the module name declared in a file, or "" for root namespace.
func fileModuleName(f *parser.File) string {
	for _, d := range f.Decls {
		if md, ok := d.(*parser.ModuleDecl); ok {
			return md.Name.Lexeme
		}
	}
	return ""
}

// ccEnv returns the environment for running the C compiler, augmenting PATH
// so that a MinGW/MSYS2 gcc can find its own tools (ld.exe, as.exe, etc.).
func ccEnv(cc string) []string {
	env := os.Environ()
	dir := filepath.Dir(cc)
	// Check whether dir is already on PATH to avoid duplication.
	pathVar := os.Getenv("PATH")
	if dir != "." && !strings.Contains(pathVar, dir) {
		for i, e := range env {
			if strings.HasPrefix(strings.ToUpper(e), "PATH=") {
				env[i] = e + string(os.PathListSeparator) + dir
				break
			}
		}
	}
	return env
}

func isWindows() bool {
	return os.PathSeparator == '\\'
}
