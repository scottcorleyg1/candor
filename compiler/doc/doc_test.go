// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg/candor

package doc

import (
	"strings"
	"testing"

	"github.com/candor-core/candor/compiler/lexer"
	"github.com/candor-core/candor/compiler/parser"
)

// ── ExtractDocComments ────────────────────────────────────────────────────────

func TestExtractDocCommentsBasic(t *testing.T) {
	src := `
/// Add two numbers.
fn add(a: u32, b: u32) -> u32 { return a + b }
`
	docs := ExtractDocComments(src)
	if got := docs["add"]; got != "Add two numbers." {
		t.Errorf("expected doc for add, got %q", got)
	}
}

func TestExtractDocCommentsMultiLine(t *testing.T) {
	src := `
/// Compute the factorial.
/// Returns 1 for n = 0.
fn factorial(n: u64) -> u64 { return 1 }
`
	docs := ExtractDocComments(src)
	want := "Compute the factorial.\nReturns 1 for n = 0."
	if got := docs["factorial"]; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestExtractDocCommentsBlankLineResetsBlock(t *testing.T) {
	src := `
/// This should NOT attach.

fn foo() -> unit { return unit }
`
	docs := ExtractDocComments(src)
	if _, ok := docs["foo"]; ok {
		t.Errorf("blank line between doc and fn should discard doc block")
	}
}

func TestExtractDocCommentsStruct(t *testing.T) {
	src := `
/// A point in 2D space.
struct Point { x: f64, y: f64 }
`
	docs := ExtractDocComments(src)
	if got := docs["Point"]; got != "A point in 2D space." {
		t.Errorf("expected struct doc, got %q", got)
	}
}

func TestExtractDocCommentsEnum(t *testing.T) {
	src := `
/// Result of an operation.
enum MyResult { Ok(u32), Err }
`
	docs := ExtractDocComments(src)
	if got := docs["MyResult"]; got != "Result of an operation." {
		t.Errorf("expected enum doc, got %q", got)
	}
}

func TestExtractDocCommentsUndocumented(t *testing.T) {
	src := `fn bare() -> unit { return unit }`
	docs := ExtractDocComments(src)
	if _, ok := docs["bare"]; ok {
		t.Errorf("undocumented fn should not appear in doc map")
	}
}

// ── GenHTML ───────────────────────────────────────────────────────────────────

func parseSource(t *testing.T, src string) *parser.File {
	t.Helper()
	tokens, err := lexer.Tokenize("<test>", src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	file, err := parser.Parse("<test>", tokens)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return file
}

func TestGenHTMLContainsFnSignature(t *testing.T) {
	src := `
/// Add two integers.
fn add(a: i64, b: i64) -> i64 { return a + b }
`
	file := parseSource(t, src)
	docs := ExtractDocComments(src)
	html := GenHTML([]FileDoc{{File: file, DocComments: docs}})

	if !strings.Contains(html, "fn add(a: i64, b: i64) -&gt; i64") {
		t.Errorf("HTML missing fn signature, got:\n%s", html)
	}
	if !strings.Contains(html, "Add two integers.") {
		t.Errorf("HTML missing doc comment text")
	}
}

func TestGenHTMLContainsStructFields(t *testing.T) {
	src := `
/// A 2D point.
struct Point { x: f64, y: f64 }
`
	file := parseSource(t, src)
	docs := ExtractDocComments(src)
	html := GenHTML([]FileDoc{{File: file, DocComments: docs}})

	if !strings.Contains(html, "struct Point") {
		t.Errorf("HTML missing struct heading")
	}
	if !strings.Contains(html, "A 2D point.") {
		t.Errorf("HTML missing struct doc")
	}
	if !strings.Contains(html, "x") || !strings.Contains(html, "f64") {
		t.Errorf("HTML missing struct field")
	}
}

func TestGenHTMLEffectsTag(t *testing.T) {
	src := `fn pure_fn(x: i64) -> i64 effects [] { return x }`
	file := parseSource(t, src)
	html := GenHTML([]FileDoc{{File: file, DocComments: map[string]string{}}})

	if !strings.Contains(html, "effects: []") {
		t.Errorf("HTML missing effects tag, got:\n%s", html)
	}
}

func TestGenHTMLIsWellFormed(t *testing.T) {
	src := `fn main() -> unit { return unit }`
	file := parseSource(t, src)
	html := GenHTML([]FileDoc{{File: file, DocComments: map[string]string{}}})

	if !strings.HasPrefix(html, "<!DOCTYPE html>") {
		t.Errorf("HTML does not start with DOCTYPE")
	}
	if !strings.Contains(html, "</html>") {
		t.Errorf("HTML missing closing tag")
	}
}
