// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg1/candor

// Benchmarks for the full candorc pipeline.
//
// Run with:
//   go test ./tests/... -run=^$ -bench=. -benchmem
//
// To track scaling over time, pipe to a file and diff:
//   go test ./tests/... -run=^$ -bench=. -benchmem -count=5 | tee bench.txt
//   benchstat old.txt bench.txt
package tests

import (
	"fmt"
	"strings"
	"testing"

	emit_c "github.com/scottcorleyg1/candor/compiler/emit_c"
	"github.com/scottcorleyg1/candor/compiler/lexer"
	"github.com/scottcorleyg1/candor/compiler/parser"
	"github.com/scottcorleyg1/candor/compiler/typeck"
)

// ── Source generators ─────────────────────────────────────────────────────────

// genIndependent generates n independent functions each doing one add.
// Shape: fn f_i(a: u32, b: u32) -> u32 { return a + b }
func genIndependent(n int) string {
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, "fn f%d(a: u32, b: u32) -> u32 { return a + b }\n", i)
	}
	sb.WriteString("fn main() -> unit { return unit }\n")
	return sb.String()
}

// genChain generates n functions where each calls the previous.
// f0 returns a literal; f_i calls f_{i-1}.
// Tests scope lookup and forward-declaration handling.
func genChain(n int) string {
	var sb strings.Builder
	sb.WriteString("fn f0(x: u32) -> u32 { return x }\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&sb, "fn f%d(x: u32) -> u32 { return f%d(x) }\n", i, i-1)
	}
	sb.WriteString("fn main() -> unit { return unit }\n")
	return sb.String()
}

// genDeepExpr generates a function with a deeply nested arithmetic expression.
// Tests emitter recursion and strings.Builder allocation depth.
func genDeepExpr(depth int) string {
	// Build: a + (a + (a + ... ))
	expr := "a"
	for i := 1; i < depth; i++ {
		expr = fmt.Sprintf("a + %s", expr)
	}
	return fmt.Sprintf("fn f(a: u32) -> u32 { return %s }\nfn main() -> unit { return unit }\n", expr)
}

// genManyLets generates a function with n sequential let bindings.
// Tests scope chain growth and exprTypes map scaling.
func genManyLets(n int) string {
	var sb strings.Builder
	sb.WriteString("fn f() -> unit {\n")
	sb.WriteString("    let v0: u32 = 1\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&sb, "    let v%d: u32 = v%d\n", i, i-1)
	}
	sb.WriteString("    return unit\n}\n")
	sb.WriteString("fn main() -> unit { return unit }\n")
	return sb.String()
}

// genManyStructs generates n struct declarations each with 4 fields.
func genManyStructs(n int) string {
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, "struct S%d { a: u32, b: u32, c: u32, d: u32 }\n", i)
	}
	sb.WriteString("fn main() -> unit { return unit }\n")
	return sb.String()
}

// ── Pipeline helpers ──────────────────────────────────────────────────────────

func pipelineSrc(src string) error {
	tokens, err := lexer.Tokenize("<bench>", src)
	if err != nil {
		return err
	}
	file, err := parser.Parse("<bench>", tokens)
	if err != nil {
		return err
	}
	res, err := typeck.Check(file)
	if err != nil {
		return err
	}
	_, err = emit_c.Emit(file, res)
	return err
}

func lexOnly(src string) error {
	_, err := lexer.Tokenize("<bench>", src)
	return err
}

func lexAndParse(src string) error {
	tokens, err := lexer.Tokenize("<bench>", src)
	if err != nil {
		return err
	}
	_, err = parser.Parse("<bench>", tokens)
	return err
}

func lexParseTypeck(src string) (error) {
	tokens, err := lexer.Tokenize("<bench>", src)
	if err != nil {
		return err
	}
	file, err := parser.Parse("<bench>", tokens)
	if err != nil {
		return err
	}
	_, err = typeck.Check(file)
	return err
}

// ── Acceptance criterion baseline ────────────────────────────────────────────

var acceptanceSrc = `
fn add(a: u32, b: u32) -> u32 { return a + b }
fn main() -> unit {
    let x = add(1, 2)
    return unit
}
`

func BenchmarkPipelineAcceptance(b *testing.B) {
	for b.Loop() {
		if err := pipelineSrc(acceptanceSrc); err != nil {
			b.Fatal(err)
		}
	}
}

// ── Scaling: independent functions ───────────────────────────────────────────

func BenchmarkPipelineIndependent10(b *testing.B)   { benchPipeline(b, genIndependent(10)) }
func BenchmarkPipelineIndependent50(b *testing.B)   { benchPipeline(b, genIndependent(50)) }
func BenchmarkPipelineIndependent100(b *testing.B)  { benchPipeline(b, genIndependent(100)) }
func BenchmarkPipelineIndependent500(b *testing.B)  { benchPipeline(b, genIndependent(500)) }
func BenchmarkPipelineIndependent1000(b *testing.B) { benchPipeline(b, genIndependent(1000)) }

func benchPipeline(b *testing.B, src string) {
	b.Helper()
	b.ReportAllocs()
	for b.Loop() {
		if err := pipelineSrc(src); err != nil {
			b.Fatal(err)
		}
	}
}

// ── Scaling: call chain ───────────────────────────────────────────────────────

func BenchmarkPipelineChain10(b *testing.B)  { benchPipeline(b, genChain(10)) }
func BenchmarkPipelineChain50(b *testing.B)  { benchPipeline(b, genChain(50)) }
func BenchmarkPipelineChain100(b *testing.B) { benchPipeline(b, genChain(100)) }

// ── Scaling: deep expression (emitter recursion / buffer allocation) ──────────

func BenchmarkPipelineDeepExpr10(b *testing.B)  { benchPipeline(b, genDeepExpr(10)) }
func BenchmarkPipelineDeepExpr50(b *testing.B)  { benchPipeline(b, genDeepExpr(50)) }
func BenchmarkPipelineDeepExpr100(b *testing.B) { benchPipeline(b, genDeepExpr(100)) }

// ── Scaling: many let bindings (scope chain + exprTypes map) ─────────────────

func BenchmarkPipelineManyLets10(b *testing.B)  { benchPipeline(b, genManyLets(10)) }
func BenchmarkPipelineManyLets50(b *testing.B)  { benchPipeline(b, genManyLets(50)) }
func BenchmarkPipelineManyLets100(b *testing.B) { benchPipeline(b, genManyLets(100)) }

// ── Scaling: struct declarations ──────────────────────────────────────────────

func BenchmarkPipelineManyStructs10(b *testing.B)  { benchPipeline(b, genManyStructs(10)) }
func BenchmarkPipelineManyStructs50(b *testing.B)  { benchPipeline(b, genManyStructs(50)) }
func BenchmarkPipelineManyStructs100(b *testing.B) { benchPipeline(b, genManyStructs(100)) }

// ── Phase isolation benchmarks ────────────────────────────────────────────────
// These isolate individual phases so we can see where time is spent.

func BenchmarkLexOnly100Fns(b *testing.B) {
	src := genIndependent(100)
	b.ReportAllocs()
	for b.Loop() {
		if err := lexOnly(src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLexParseOnly100Fns(b *testing.B) {
	src := genIndependent(100)
	b.ReportAllocs()
	for b.Loop() {
		if err := lexAndParse(src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLexParseTypeckOnly100Fns(b *testing.B) {
	src := genIndependent(100)
	b.ReportAllocs()
	for b.Loop() {
		if err := lexParseTypeck(src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFullPipeline100Fns(b *testing.B) {
	src := genIndependent(100)
	b.ReportAllocs()
	for b.Loop() {
		if err := pipelineSrc(src); err != nil {
			b.Fatal(err)
		}
	}
}
