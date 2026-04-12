// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg/candor

// FormatCandor pretty-prints a parsed Candor AST back to canonical source text.
//
// Formatting rules (M4.4):
//   - 4-space indent
//   - { on same line as fn/if/loop/etc.
//   - Blank line between top-level declarations
//   - Normalised keyword spacing

package emit_c

import (
	"fmt"
	"strings"

	"github.com/candor-core/candor/compiler/parser"
)

// FormatCandor returns a canonical formatted version of the parsed file.
// It is idempotent: formatting the output again produces identical text.
func FormatCandor(file *parser.File) string {
	f := &candorFmt{}
	f.formatFile(file)
	return f.sb.String()
}

type candorFmt struct {
	sb     strings.Builder
	indent int
}

func (f *candorFmt) writeln(s string) {
	if s == "" {
		f.sb.WriteByte('\n')
		return
	}
	f.sb.WriteString(strings.Repeat("    ", f.indent))
	f.sb.WriteString(s)
	f.sb.WriteByte('\n')
}

func (f *candorFmt) formatFile(file *parser.File) {
	for i, decl := range file.Decls {
		if i > 0 {
			f.sb.WriteByte('\n') // blank line between top-level decls
		}
		f.formatDecl(decl)
	}
}

func (f *candorFmt) formatDecl(decl parser.Decl) {
	switch d := decl.(type) {
	case *parser.ModuleDecl:
		f.writeln(fmt.Sprintf("module %s", d.Name.Lexeme))
	case *parser.UseDecl:
		parts := make([]string, len(d.Path))
		for i, tok := range d.Path {
			parts[i] = tok.Lexeme
		}
		f.writeln(fmt.Sprintf("use %s", strings.Join(parts, "::")))
	case *parser.ConstDecl:
		f.writeln(fmt.Sprintf("const %s: %s = %s", d.Name.Lexeme, fmtTypeExpr(d.Type), fmtExpr(d.Value)))
	case *parser.StructDecl:
		f.formatStructDecl(d)
	case *parser.EnumDecl:
		f.formatEnumDecl(d)
	case *parser.FnDecl:
		f.formatFnDecl(d)
	case *parser.ImplDecl:
		f.formatImplDecl(d)
	case *parser.ImplForDecl:
		f.formatImplForDecl(d)
	case *parser.TraitDecl:
		f.formatTraitDecl(d)
	}
}

func (f *candorFmt) formatStructDecl(d *parser.StructDecl) {
	f.writeln(fmt.Sprintf("struct %s {", d.Name.Lexeme))
	f.indent++
	for _, field := range d.Fields {
		f.writeln(fmt.Sprintf("%s: %s,", field.Name.Lexeme, fmtTypeExpr(field.Type)))
	}
	f.indent--
	f.writeln("}")
}

func (f *candorFmt) formatEnumDecl(d *parser.EnumDecl) {
	f.writeln(fmt.Sprintf("enum %s {", d.Name.Lexeme))
	f.indent++
	for _, v := range d.Variants {
		if len(v.Fields) == 0 {
			f.writeln(fmt.Sprintf("%s,", v.Name.Lexeme))
		} else {
			parts := make([]string, len(v.Fields))
			for i, fld := range v.Fields {
				parts[i] = fmtTypeExpr(fld)
			}
			f.writeln(fmt.Sprintf("%s(%s),", v.Name.Lexeme, strings.Join(parts, ", ")))
		}
	}
	f.indent--
	f.writeln("}")
}

func (f *candorFmt) formatFnDecl(d *parser.FnDecl) {
	for _, dir := range d.Directives {
		f.writeln(fmt.Sprintf("#%s", dir))
	}
	sig := fmtFnSig(d)
	if d.Body == nil {
		f.writeln(fmt.Sprintf("extern %s", sig))
		return
	}
	eff := ""
	if d.Effects != nil {
		eff = " " + fmtEffects(d.Effects)
	}
	var contracts []string
	for _, cc := range d.Contracts {
		kindStr := "requires"
		if cc.Kind == parser.ContractEnsures {
			kindStr = "ensures"
		}
		contracts = append(contracts, fmt.Sprintf("%s %s", kindStr, fmtExpr(cc.Expr)))
	}
	header := sig + eff
	if len(contracts) > 0 {
		header += " " + strings.Join(contracts, " ")
	}
	f.writeln(header + " {")
	f.indent++
	for _, stmt := range d.Body.Stmts {
		f.formatStmt(stmt)
	}
	f.indent--
	f.writeln("}")
}

func (f *candorFmt) formatImplDecl(d *parser.ImplDecl) {
	f.writeln(fmt.Sprintf("impl %s {", d.TypeName.Lexeme))
	f.indent++
	for i, m := range d.Methods {
		if i > 0 {
			f.writeln("")
		}
		f.formatFnDecl(m)
	}
	f.indent--
	f.writeln("}")
}

func (f *candorFmt) formatImplForDecl(d *parser.ImplForDecl) {
	f.writeln(fmt.Sprintf("impl %s for %s {", d.TraitName.Lexeme, d.TypeName.Lexeme))
	f.indent++
	for i, m := range d.Methods {
		if i > 0 {
			f.writeln("")
		}
		f.formatFnDecl(m)
	}
	f.indent--
	f.writeln("}")
}

func (f *candorFmt) formatTraitDecl(d *parser.TraitDecl) {
	f.writeln(fmt.Sprintf("trait %s {", d.Name.Lexeme))
	f.indent++
	for _, m := range d.Methods {
		params := make([]string, len(m.Params))
		for i, p := range m.Params {
			params[i] = fmt.Sprintf("%s: %s", p.Name.Lexeme, fmtTypeExpr(p.Type))
		}
		f.writeln(fmt.Sprintf("fn %s(%s) -> %s", m.Name.Lexeme, strings.Join(params, ", "), fmtTypeExpr(m.RetType)))
	}
	f.indent--
	f.writeln("}")
}

func (f *candorFmt) formatStmt(stmt parser.Stmt) {
	switch s := stmt.(type) {
	case *parser.LetStmt:
		mut := ""
		if s.Mut {
			mut = "mut "
		}
		ann := ""
		if s.TypeAnn != nil {
			ann = fmt.Sprintf(": %s", fmtTypeExpr(s.TypeAnn))
		}
		f.writeln(fmt.Sprintf("let %s%s%s = %s", mut, s.Name.Lexeme, ann, fmtExpr(s.Value)))
	case *parser.ReturnStmt:
		if s.Value == nil {
			f.writeln("return unit")
		} else {
			f.writeln(fmt.Sprintf("return %s", fmtExpr(s.Value)))
		}
	case *parser.ExprStmt:
		f.writeln(fmtExpr(s.X))
	case *parser.AssignStmt:
		f.writeln(fmt.Sprintf("%s = %s", s.Name.Lexeme, fmtExpr(s.Value)))
	case *parser.FieldAssignStmt:
		f.writeln(fmt.Sprintf("%s.%s = %s", fmtExpr(s.Target.Receiver), s.Target.Field.Lexeme, fmtExpr(s.Value)))
	case *parser.IndexAssignStmt:
		f.writeln(fmt.Sprintf("%s[%s] = %s", fmtExpr(s.Target.Collection), fmtExpr(s.Target.Index), fmtExpr(s.Value)))
	case *parser.IfStmt:
		f.formatIfStmt(s)
	case *parser.LoopStmt:
		f.writeln("loop {")
		f.indent++
		for _, st := range s.Body.Stmts {
			f.formatStmt(st)
		}
		f.indent--
		f.writeln("}")
	case *parser.WhileStmt:
		f.writeln(fmt.Sprintf("while %s {", fmtExpr(s.Cond)))
		f.indent++
		for _, st := range s.Body.Stmts {
			f.formatStmt(st)
		}
		f.indent--
		f.writeln("}")
	case *parser.ForStmt:
		if s.Var2.Lexeme != "" {
			f.writeln(fmt.Sprintf("for %s, %s in %s {", s.Var.Lexeme, s.Var2.Lexeme, fmtExpr(s.Collection)))
		} else {
			f.writeln(fmt.Sprintf("for %s in %s {", s.Var.Lexeme, fmtExpr(s.Collection)))
		}
		f.indent++
		for _, st := range s.Body.Stmts {
			f.formatStmt(st)
		}
		f.indent--
		f.writeln("}")
	case *parser.BreakStmt:
		f.writeln("break")
	case *parser.ContinueStmt:
		f.writeln("continue")
	case *parser.BlockStmt:
		f.writeln("{")
		f.indent++
		for _, st := range s.Stmts {
			f.formatStmt(st)
		}
		f.indent--
		f.writeln("}")
	case *parser.TupleDestructureStmt:
		names := make([]string, len(s.Names))
		for i, n := range s.Names {
			names[i] = n.Lexeme
		}
		f.writeln(fmt.Sprintf("let (%s) = %s", strings.Join(names, ", "), fmtExpr(s.Value)))
	case *parser.AssertStmt:
		f.writeln(fmt.Sprintf("assert %s", fmtExpr(s.Expr)))
	}
}

func (f *candorFmt) formatIfStmt(s *parser.IfStmt) {
	f.writeln(fmt.Sprintf("if %s {", fmtExpr(s.Cond)))
	f.indent++
	for _, st := range s.Then.Stmts {
		f.formatStmt(st)
	}
	f.indent--
	if s.Else != nil {
		if inner, ok := s.Else.(*parser.IfStmt); ok {
			// Trim trailing newline; write "} else if ..." on same line.
			buf := f.sb.String()
			f.sb.Reset()
			f.sb.WriteString(strings.TrimRight(buf, "\n"))
			f.sb.WriteString(" else ")
			f.formatIfStmt(inner)
			return
		}
		f.writeln("} else {")
		f.indent++
		if blk, ok := s.Else.(*parser.BlockStmt); ok {
			for _, st := range blk.Stmts {
				f.formatStmt(st)
			}
		} else {
			f.formatStmt(s.Else)
		}
		f.indent--
	}
	f.writeln("}")
}

// ── Expression and type formatters ────────────────────────────────────────────

func fmtExpr(e parser.Expr) string {
	if e == nil {
		return ""
	}
	switch x := e.(type) {
	case *parser.IntLitExpr:
		return x.Tok.Lexeme
	case *parser.FloatLitExpr:
		return x.Tok.Lexeme
	case *parser.BoolLitExpr:
		return x.Tok.Lexeme
	case *parser.StringLitExpr:
		return x.Tok.Lexeme
	case *parser.IdentExpr:
		return x.Tok.Lexeme
	case *parser.BinaryExpr:
		return fmt.Sprintf("%s %s %s", fmtExpr(x.Left), x.Op.Lexeme, fmtExpr(x.Right))
	case *parser.UnaryExpr:
		return fmt.Sprintf("%s%s", x.Op.Lexeme, fmtExpr(x.Operand))
	case *parser.CallExpr:
		args := make([]string, len(x.Args))
		for i, a := range x.Args {
			args[i] = fmtExpr(a)
		}
		return fmt.Sprintf("%s(%s)", fmtExpr(x.Fn), strings.Join(args, ", "))
	case *parser.FieldExpr:
		return fmt.Sprintf("%s.%s", fmtExpr(x.Receiver), x.Field.Lexeme)
	case *parser.IndexExpr:
		return fmt.Sprintf("%s[%s]", fmtExpr(x.Collection), fmtExpr(x.Index))
	case *parser.StructLitExpr:
		if len(x.Fields) == 0 && x.Base == nil {
			return fmt.Sprintf("%s {}", x.TypeName.Lexeme)
		}
		parts := make([]string, 0, len(x.Fields)+1)
		if x.Base != nil {
			parts = append(parts, fmt.Sprintf("..%s", fmtExpr(x.Base)))
		}
		for _, fi := range x.Fields {
			parts = append(parts, fmt.Sprintf("%s: %s", fi.Name.Lexeme, fmtExpr(fi.Value)))
		}
		return fmt.Sprintf("%s { %s }", x.TypeName.Lexeme, strings.Join(parts, ", "))
	case *parser.MatchExpr:
		arms := make([]string, len(x.Arms))
		for i, a := range x.Arms {
			arms[i] = fmt.Sprintf("%s => %s", fmtExpr(a.Pattern), fmtExpr(a.Body))
		}
		return fmt.Sprintf("match %s { %s }", fmtExpr(x.X), strings.Join(arms, ", "))
	case *parser.MustExpr:
		arms := make([]string, len(x.Arms))
		for i, a := range x.Arms {
			arms[i] = fmt.Sprintf("%s => %s", fmtExpr(a.Pattern), fmtExpr(a.Body))
		}
		return fmt.Sprintf("%s must { %s }", fmtExpr(x.X), strings.Join(arms, ", "))
	case *parser.LambdaExpr:
		params := make([]string, len(x.Params))
		for i, p := range x.Params {
			params[i] = fmt.Sprintf("%s: %s", p.Name.Lexeme, fmtTypeExpr(p.Type))
		}
		return fmt.Sprintf("fn(%s) -> %s { ... }", strings.Join(params, ", "), fmtTypeExpr(x.RetType))
	case *parser.CastExpr:
		return fmt.Sprintf("%s as %s", fmtExpr(x.X), fmtTypeExpr(x.Target))
	case *parser.TupleLitExpr:
		parts := make([]string, len(x.Elems))
		for i, el := range x.Elems {
			parts[i] = fmtExpr(el)
		}
		return fmt.Sprintf("(%s)", strings.Join(parts, ", "))
	case *parser.VecLitExpr:
		parts := make([]string, len(x.Elems))
		for i, el := range x.Elems {
			parts[i] = fmtExpr(el)
		}
		return fmt.Sprintf("[%s]", strings.Join(parts, ", "))
	case *parser.BlockExpr:
		stmts := make([]string, len(x.Stmts))
		for i, s := range x.Stmts {
			stmts[i] = fmtStmtInline(s)
		}
		return fmt.Sprintf("{ %s }", strings.Join(stmts, "; "))
	case *parser.PathExpr:
		return fmt.Sprintf("%s::%s", x.Head.Lexeme, x.Tail.Lexeme)
	case *parser.ReturnExpr:
		return fmt.Sprintf("return %s", fmtExpr(x.Value))
	case *parser.BreakExpr:
		return "break"
	case *parser.OldExpr:
		return fmt.Sprintf("old(%s)", fmtExpr(x.X))
	default:
		return "<?>"
	}
}

func fmtStmtInline(s parser.Stmt) string {
	switch st := s.(type) {
	case *parser.LetStmt:
		return fmt.Sprintf("let %s = %s", st.Name.Lexeme, fmtExpr(st.Value))
	case *parser.ReturnStmt:
		if st.Value == nil {
			return "return unit"
		}
		return fmt.Sprintf("return %s", fmtExpr(st.Value))
	case *parser.ExprStmt:
		return fmtExpr(st.X)
	default:
		return "<?>"
	}
}

func fmtTypeExpr(te parser.TypeExpr) string {
	if te == nil {
		return "unit"
	}
	switch t := te.(type) {
	case *parser.NamedType:
		return t.Name.Lexeme
	case *parser.GenericType:
		params := make([]string, len(t.Params))
		for i, p := range t.Params {
			params[i] = fmtTypeExpr(p)
		}
		return fmt.Sprintf("%s<%s>", t.Name.Lexeme, strings.Join(params, ", "))
	case *parser.FnType:
		params := make([]string, len(t.Params))
		for i, p := range t.Params {
			params[i] = fmtTypeExpr(p)
		}
		return fmt.Sprintf("fn(%s) -> %s", strings.Join(params, ", "), fmtTypeExpr(t.RetType))
	case *parser.TupleTypeExpr:
		parts := make([]string, len(t.Elems))
		for i, e := range t.Elems {
			parts[i] = fmtTypeExpr(e)
		}
		return fmt.Sprintf("(%s)", strings.Join(parts, ", "))
	default:
		return "<?>"
	}
}

func fmtFnSig(d *parser.FnDecl) string {
	params := make([]string, len(d.Params))
	for i, p := range d.Params {
		params[i] = fmt.Sprintf("%s: %s", p.Name.Lexeme, fmtTypeExpr(p.Type))
	}
	tpStr := ""
	if len(d.TypeParams) > 0 {
		tps := make([]string, len(d.TypeParams))
		for i, tp := range d.TypeParams {
			tps[i] = tp.Lexeme
			if bounds, ok := d.TypeBounds[tp.Lexeme]; ok {
				tps[i] += ": " + strings.Join(bounds, "+")
			}
		}
		tpStr = fmt.Sprintf("<%s>", strings.Join(tps, ", "))
	}
	return fmt.Sprintf("fn %s%s(%s) -> %s", d.Name.Lexeme, tpStr, strings.Join(params, ", "), fmtTypeExpr(d.RetType))
}

func fmtEffects(e *parser.EffectsAnnotation) string {
	if e == nil {
		return ""
	}
	switch e.Kind {
	case parser.EffectsPure:
		return "pure effects []"
	case parser.EffectsDecl:
		return fmt.Sprintf("effects(%s)", strings.Join(e.Names, ", "))
	case parser.EffectsCap:
		if len(e.Names) > 0 {
			return fmt.Sprintf("cap(%s)", e.Names[0])
		}
	}
	return ""
}
