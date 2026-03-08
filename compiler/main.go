// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg1/candor

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	emit_c "github.com/scottcorleyg1/candor/compiler/emit_c"
	"github.com/scottcorleyg1/candor/compiler/lexer"
	"github.com/scottcorleyg1/candor/compiler/parser"
	"github.com/scottcorleyg1/candor/compiler/typeck"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: candorc <file.cnd>")
		os.Exit(1)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "candorc: %v\n", err)
		os.Exit(1)
	}
}

func run(srcPath string) error {
	// ── Read source ───────────────────────────────────────────────────────────
	src, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}

	// ── Lex ───────────────────────────────────────────────────────────────────
	tokens, err := lexer.Tokenize(srcPath, string(src))
	if err != nil {
		return err
	}

	// ── Parse ─────────────────────────────────────────────────────────────────
	file, err := parser.Parse(srcPath, tokens)
	if err != nil {
		return err
	}

	// ── Type-check ────────────────────────────────────────────────────────────
	res, err := typeck.Check(file)
	if err != nil {
		return err
	}

	// ── Emit C ────────────────────────────────────────────────────────────────
	cSrc, err := emit_c.Emit(file, res)
	if err != nil {
		return err
	}

	// Write the C file next to the source file.
	base := strings.TrimSuffix(srcPath, filepath.Ext(srcPath))
	cPath := base + ".c"
	if err := os.WriteFile(cPath, []byte(cSrc), 0o644); err != nil {
		return err
	}

	// ── Invoke system C compiler ──────────────────────────────────────────────
	outPath := base
	if isWindows() {
		outPath += ".exe"
	}

	cc := findCC()
	cmd := exec.Command(cc, "-o", outPath, cPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("C compiler %q failed: %w", cc, err)
	}

	fmt.Printf("candorc: wrote %s\n", outPath)
	return nil
}

// findCC returns the C compiler to use, preferring $CC, then gcc, then cc.
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
