// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg/candor

// Package doc implements the candorc doc --html documentation generator.
//
// It parses /// doc-comment lines from raw source text and associates them
// with the declaration that immediately follows. The GenHTML function produces
// a self-contained HTML reference page from a set of parsed files.
package doc

import (
	"fmt"
	"html"
	"strings"

	"github.com/candor-core/candor/compiler/parser"
)

// ExtractDocComments scans raw Candor source and returns a map from
// declaration name to the doc comment block immediately preceding it.
//
// Doc comments are lines that start with /// (after leading whitespace).
// A blank line between a doc block and the declaration discards the block.
// Only fn, struct, and enum declarations are recognised as association targets.
func ExtractDocComments(src string) map[string]string {
	docs := make(map[string]string)
	lines := strings.Split(src, "\n")
	var pending []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "///") {
			text := strings.TrimPrefix(trimmed, "///")
			text = strings.TrimPrefix(text, " ")
			pending = append(pending, text)
		} else if trimmed == "" {
			pending = nil
		} else {
			if name := declNameFromLine(trimmed); name != "" && len(pending) > 0 {
				docs[name] = strings.Join(pending, "\n")
			}
			// Non-doc, non-blank lines that are not declarations also clear pending.
			// We clear unconditionally so only directly-preceding blocks attach.
			pending = nil
		}
	}
	return docs
}

// declNameFromLine extracts the declaration name from a line starting with
// "fn ", "struct ", or "enum ". Returns "" if the line is not a declaration.
func declNameFromLine(line string) string {
	for _, kw := range []string{"fn ", "struct ", "enum "} {
		if strings.HasPrefix(line, kw) {
			rest := strings.TrimPrefix(line, kw)
			i := 0
			for i < len(rest) && isIdentChar(rest[i]) {
				i++
			}
			if i > 0 {
				return rest[:i]
			}
		}
	}
	return ""
}

func isIdentChar(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// ── HTML generation ──────────────────────────────────────────────────────────

// FileDoc holds the parsed file and its doc comment map.
type FileDoc struct {
	File        *parser.File
	DocComments map[string]string // decl name → doc string
}

// GenHTML generates a self-contained HTML reference page.
// Each FileDoc contributes a section; within each section, functions,
// structs, and enums are rendered as cards with signature and doc text.
func GenHTML(files []FileDoc) string {
	var sb strings.Builder
	sb.WriteString(htmlHeader())

	for _, fd := range files {
		sb.WriteString(renderFile(fd))
	}

	sb.WriteString(htmlFooter())
	return sb.String()
}

func renderFile(fd FileDoc) string {
	var sb strings.Builder
	name := fd.File.Name
	// Use just the base name for the heading.
	if idx := strings.LastIndexAny(name, "/\\"); idx >= 0 {
		name = name[idx+1:]
	}
	fmt.Fprintf(&sb, "<section>\n<h2 class=\"file-heading\">%s</h2>\n", html.EscapeString(name))

	for _, d := range fd.File.Decls {
		switch decl := d.(type) {
		case *parser.FnDecl:
			sb.WriteString(renderFn(decl, fd.DocComments))
		case *parser.StructDecl:
			sb.WriteString(renderStruct(decl, fd.DocComments))
		case *parser.EnumDecl:
			sb.WriteString(renderEnum(decl, fd.DocComments))
		}
	}

	sb.WriteString("</section>\n")
	return sb.String()
}

func renderFn(fn *parser.FnDecl, docs map[string]string) string {
	var sb strings.Builder
	name := fn.Name.Lexeme
	sig := buildSig(fn)
	doc := docs[name]

	fmt.Fprintf(&sb, "<div class=\"card fn-card\">\n")
	fmt.Fprintf(&sb, "  <div class=\"card-sig\"><code>%s</code></div>\n", html.EscapeString(sig))

	if doc != "" {
		fmt.Fprintf(&sb, "  <p class=\"card-doc\">%s</p>\n", html.EscapeString(doc))
	}

	// Effects annotation
	if fn.Effects != nil {
		eff := effectsStr(fn.Effects)
		fmt.Fprintf(&sb, "  <div class=\"tag tag-effects\">effects: %s</div>\n", html.EscapeString(eff))
	}

	// Contract clauses
	for _, cc := range fn.Contracts {
		kind := "requires"
		if cc.Kind == parser.ContractEnsures {
			kind = "ensures"
		}
		fmt.Fprintf(&sb, "  <div class=\"tag tag-contract\">%s: ...</div>\n", kind)
	}

	fmt.Fprintf(&sb, "</div>\n")
	return sb.String()
}

func renderStruct(st *parser.StructDecl, docs map[string]string) string {
	var sb strings.Builder
	name := st.Name.Lexeme
	doc := docs[name]

	fmt.Fprintf(&sb, "<div class=\"card struct-card\">\n")
	fmt.Fprintf(&sb, "  <div class=\"card-sig\"><code>struct %s</code></div>\n", html.EscapeString(name))

	if doc != "" {
		fmt.Fprintf(&sb, "  <p class=\"card-doc\">%s</p>\n", html.EscapeString(doc))
	}

	if len(st.Fields) > 0 {
		sb.WriteString("  <table class=\"fields\">\n")
		for _, f := range st.Fields {
			fmt.Fprintf(&sb, "    <tr><td class=\"field-name\">%s</td><td class=\"field-type\">%s</td></tr>\n",
				html.EscapeString(f.Name.Lexeme), html.EscapeString(typeExprStr(f.Type)))
		}
		sb.WriteString("  </table>\n")
	}

	fmt.Fprintf(&sb, "</div>\n")
	return sb.String()
}

func renderEnum(en *parser.EnumDecl, docs map[string]string) string {
	var sb strings.Builder
	name := en.Name.Lexeme
	doc := docs[name]

	fmt.Fprintf(&sb, "<div class=\"card enum-card\">\n")
	fmt.Fprintf(&sb, "  <div class=\"card-sig\"><code>enum %s</code></div>\n", html.EscapeString(name))

	if doc != "" {
		fmt.Fprintf(&sb, "  <p class=\"card-doc\">%s</p>\n", html.EscapeString(doc))
	}

	if len(en.Variants) > 0 {
		sb.WriteString("  <ul class=\"variants\">\n")
		for _, v := range en.Variants {
			var parts []string
			for _, p := range v.Fields {
				parts = append(parts, typeExprStr(p))
			}
			label := v.Name.Lexeme
			if len(parts) > 0 {
				label += "(" + strings.Join(parts, ", ") + ")"
			}
			fmt.Fprintf(&sb, "    <li><code>%s</code></li>\n", html.EscapeString(label))
		}
		sb.WriteString("  </ul>\n")
	}

	fmt.Fprintf(&sb, "</div>\n")
	return sb.String()
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func buildSig(fn *parser.FnDecl) string {
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

func effectsStr(eff *parser.EffectsAnnotation) string {
	if eff == nil {
		return ""
	}
	switch eff.Kind {
	case parser.EffectsPure:
		return "[]"
	default:
		if len(eff.Names) == 0 {
			return "io"
		}
		return "[" + strings.Join(eff.Names, ", ") + "]"
	}
}

// ── HTML template ─────────────────────────────────────────────────────────────

func htmlHeader() string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Candor API Reference</title>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: system-ui, sans-serif; background: #f8f9fa; color: #212529; line-height: 1.6; }
header { background: #1a1a2e; color: #e2e8f0; padding: 1.5rem 2rem; }
header h1 { font-size: 1.5rem; font-weight: 700; letter-spacing: 0.05em; }
header p { font-size: 0.85rem; color: #94a3b8; margin-top: 0.25rem; }
main { max-width: 960px; margin: 2rem auto; padding: 0 1.5rem; }
section { margin-bottom: 3rem; }
.file-heading { font-size: 1.1rem; font-weight: 600; color: #475569; border-bottom: 1px solid #e2e8f0;
  padding-bottom: 0.4rem; margin-bottom: 1.2rem; font-family: monospace; }
.card { background: #fff; border: 1px solid #e2e8f0; border-radius: 8px;
  padding: 1.1rem 1.4rem; margin-bottom: 1rem; }
.fn-card { border-left: 3px solid #3b82f6; }
.struct-card { border-left: 3px solid #10b981; }
.enum-card { border-left: 3px solid #8b5cf6; }
.card-sig code { font-size: 0.92rem; background: #f1f5f9; padding: 0.2em 0.5em;
  border-radius: 4px; color: #1e3a5f; }
.card-doc { margin-top: 0.6rem; font-size: 0.9rem; color: #4b5563; white-space: pre-wrap; }
.tag { display: inline-block; margin-top: 0.5rem; margin-right: 0.4rem;
  font-size: 0.75rem; padding: 0.15em 0.5em; border-radius: 999px; }
.tag-effects { background: #dbeafe; color: #1d4ed8; }
.tag-contract { background: #fef9c3; color: #854d0e; }
.fields { margin-top: 0.7rem; border-collapse: collapse; width: 100%; font-size: 0.88rem; }
.fields td { padding: 0.2rem 0.5rem; border-top: 1px solid #f1f5f9; }
.field-name { font-family: monospace; color: #0f172a; }
.field-type { font-family: monospace; color: #3b82f6; }
.variants { margin-top: 0.6rem; padding-left: 1.2rem; font-size: 0.88rem; }
.variants li { margin-bottom: 0.15rem; }
.variants code { background: #f8f9fa; padding: 0.1em 0.3em; border-radius: 3px; }
</style>
</head>
<body>
<header>
  <h1>Candor API Reference</h1>
  <p>Generated by <code>candorc doc --html</code></p>
</header>
<main>
`
}

func htmlFooter() string {
	return `</main>
</body>
</html>
`
}
