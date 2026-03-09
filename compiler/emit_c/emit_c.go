// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg1/candor

// Package emit_c emits valid C from a type-checked Candor AST.
//
// Mapping summary:
//   unit              → void  (return unit → return;)
//   bool              → int   (true→1, false→0)
//   str               → const char*
//   iN / uN           → intN_t / uintN_t  (via <stdint.h>)
//   f32 / f64         → float / double
//   ref<T> / refmut<T>→ T*
//   vec<T>            → T*   (raw pointer; full runtime is a later phase)
//   struct S          → struct S (emitted before functions)
//   fn main()->unit   → special: C main() returning int, body return unit → return 0
package emit_c

import (
	"fmt"
	"strings"

	"github.com/scottcorleyg1/candor/compiler/lexer"
	"github.com/scottcorleyg1/candor/compiler/parser"
	"github.com/scottcorleyg1/candor/compiler/typeck"
)

// Emit translates a type-checked Candor file to a C source string.
func Emit(file *parser.File, res *typeck.Result) (string, error) {
	e := &emitter{res: res}
	if err := e.emitFile(file); err != nil {
		return "", err
	}
	return e.sb.String(), nil
}

// ── emitter ───────────────────────────────────────────────────────────────────

type emitter struct {
	sb  strings.Builder
	res *typeck.Result
	// current function context
	retIsUnit bool // true when emitting a fn returning unit (C void)
	isMain    bool // true when emitting the special main function
	tmpCount  int
}

func (e *emitter) freshTmp() string {
	e.tmpCount++
	return fmt.Sprintf("_cnd%d", e.tmpCount)
}

func (e *emitter) write(s string)              { e.sb.WriteString(s) }
func (e *emitter) writef(f string, a ...any)   { fmt.Fprintf(&e.sb, f, a...) }
func (e *emitter) writeln(s string)            { e.sb.WriteString(s); e.sb.WriteByte('\n') }

// ── file ─────────────────────────────────────────────────────────────────────

func (e *emitter) emitFile(file *parser.File) error {
	e.writeln("#include <stdint.h>")
	e.writeln("#include <stdio.h>")
	e.writeln("")

	// Emit result<T,E> struct typedefs used in this file.
	if err := e.emitResultStructs(); err != nil {
		return err
	}

	// Forward-declare all structs first so they can reference each other.
	for _, decl := range file.Decls {
		if d, ok := decl.(*parser.StructDecl); ok {
			e.writef("struct %s;\n", d.Name.Lexeme)
		}
	}

	// Emit struct definitions.
	for _, decl := range file.Decls {
		if d, ok := decl.(*parser.StructDecl); ok {
			if err := e.emitStructDecl(d); err != nil {
				return err
			}
		}
	}

	// Forward-declare all functions.
	for _, decl := range file.Decls {
		if d, ok := decl.(*parser.FnDecl); ok {
			if err := e.emitFnForward(d); err != nil {
				return err
			}
		}
	}
	if hasFnDecls(file) {
		e.writeln("")
	}

	// Emit function bodies.
	for _, decl := range file.Decls {
		if d, ok := decl.(*parser.FnDecl); ok {
			if err := e.emitFnDecl(d); err != nil {
				return err
			}
		}
	}

	return nil
}

func (e *emitter) emitResultStructs() error {
	seen := map[string]bool{}
	for _, t := range e.res.ExprTypes {
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "result" || len(gen.Params) != 2 {
			continue
		}
		name, err := e.resultTypeName(gen)
		if err != nil {
			continue // skip unsupported combinations
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		okC, err := e.cType(gen.Params[0])
		if err != nil {
			return err
		}
		errC, err := e.cType(gen.Params[1])
		if err != nil {
			return err
		}
		e.writef("typedef struct { int _ok; %s _ok_val; %s _err_val; } %s;\n", okC, errC, name)
	}
	if len(seen) > 0 {
		e.writeln("")
	}
	return nil
}

func (e *emitter) resultTypeName(gen *typeck.GenType) (string, error) {
	if len(gen.Params) != 2 {
		return "", fmt.Errorf("result needs 2 params")
	}
	ok, err := e.cType(gen.Params[0])
	if err != nil {
		return "", err
	}
	er, err := e.cType(gen.Params[1])
	if err != nil {
		return "", err
	}
	mangle := func(s string) string {
		r := strings.NewReplacer(" ", "_", "*", "ptr", "<", "_", ">", "_", ",", "_")
		return r.Replace(s)
	}
	return fmt.Sprintf("_cnd_result_%s_%s", mangle(ok), mangle(er)), nil
}

func bodyEndsWithReturn(block *parser.BlockStmt) bool {
	if len(block.Stmts) == 0 {
		return false
	}
	_, ok := block.Stmts[len(block.Stmts)-1].(*parser.ReturnStmt)
	return ok
}

func hasFnDecls(file *parser.File) bool {
	for _, d := range file.Decls {
		if _, ok := d.(*parser.FnDecl); ok {
			return true
		}
	}
	return false
}

// ── struct ────────────────────────────────────────────────────────────────────

func (e *emitter) emitStructDecl(d *parser.StructDecl) error {
	st := e.res.Structs[d.Name.Lexeme]
	e.writef("\ntypedef struct %s {\n", d.Name.Lexeme)
	for _, f := range d.Fields {
		cType, err := e.cType(st.Fields[f.Name.Lexeme])
		if err != nil {
			return err
		}
		e.writef("    %s %s;\n", cType, f.Name.Lexeme)
	}
	e.writef("} %s;\n", d.Name.Lexeme)
	return nil
}

// ── functions ─────────────────────────────────────────────────────────────────

func (e *emitter) emitFnForward(d *parser.FnDecl) error {
	sig := e.res.FnSigs[d.Name.Lexeme]
	proto, err := e.fnProto(d.Name.Lexeme, sig)
	if err != nil {
		return err
	}
	e.writef("%s;\n", proto)
	return nil
}

func (e *emitter) emitFnDecl(d *parser.FnDecl) error {
	proto, err := e.fnProtoNamed(d)
	if err != nil {
		return err
	}

	e.writef("\n%s {\n", proto)

	sig := e.res.FnSigs[d.Name.Lexeme]
	// Save and set function context.
	prevRetIsUnit := e.retIsUnit
	prevIsMain := e.isMain
	e.retIsUnit = sig.Ret.Equals(typeck.TUnit)
	e.isMain = d.Name.Lexeme == "main"

	isMain := e.isMain
	if err := e.emitBlock(d.Body, 1); err != nil {
		return err
	}

	e.retIsUnit = prevRetIsUnit
	e.isMain = prevIsMain

	// C main must end with return 0. Only add it when the body doesn't
	// already end in an explicit return statement.
	if isMain && !bodyEndsWithReturn(d.Body) {
		e.writeln("    return 0;")
	}
	e.writeln("}")

	return nil
}

// fnProto builds "rettype name(params)" for forward decls and definitions.
// The Candor main()->unit maps to C "int main(void)".
func (e *emitter) fnProto(name string, sig *typeck.FnType) (string, error) {
	if name == "main" {
		return "int main(void)", nil
	}

	ret, err := e.cType(sig.Ret)
	if err != nil {
		return "", err
	}

	var params []string
	if len(sig.Params) == 0 {
		params = []string{"void"}
	} else {
		// We need parameter names. Re-extract them from the FnDecl params by
		// reconstructing from the Result. Since emit_c only has the sig, we
		// return a placeholder here and override in emitFnDecl.
		// Actually fnProto is called with the decl in scope — but this helper
		// only receives the sig. We'll handle names at the call sites.
		//
		// This function is called from emitFnForward (no names needed — C
		// forward decls can omit names) and emitFnDecl (names needed).
		// We'll emit "type" only here; emitFnDecl will build its own proto.
		for _, p := range sig.Params {
			ct, err := e.cType(p)
			if err != nil {
				return "", err
			}
			params = append(params, ct)
		}
	}
	return fmt.Sprintf("%s %s(%s)", ret, name, strings.Join(params, ", ")), nil
}

// emitFnDecl needs parameter names for the definition. Override fnProto there.
func (e *emitter) fnProtoNamed(d *parser.FnDecl) (string, error) {
	if d.Name.Lexeme == "main" {
		return "int main(void)", nil
	}
	sig := e.res.FnSigs[d.Name.Lexeme]
	ret, err := e.cType(sig.Ret)
	if err != nil {
		return "", err
	}
	if len(d.Params) == 0 {
		return fmt.Sprintf("%s %s(void)", ret, d.Name.Lexeme), nil
	}
	params := make([]string, len(d.Params))
	for i, p := range d.Params {
		ct, err := e.cType(sig.Params[i])
		if err != nil {
			return "", err
		}
		params[i] = ct + " " + p.Name.Lexeme
	}
	return fmt.Sprintf("%s %s(%s)", ret, d.Name.Lexeme, strings.Join(params, ", ")), nil
}

// Rewrite emitFnDecl to use fnProtoNamed (with names) for the definition and
// emitFnForward to use the nameless form for the forward declaration.
// We already wrote emitFnDecl above calling fnProto — let's fix that by
// inlining fnProtoNamed there. The forward decl uses fnProto (no names).

// ── blocks and statements ─────────────────────────────────────────────────────

func indent(depth int) string { return strings.Repeat("    ", depth) }

func (e *emitter) emitBlock(block *parser.BlockStmt, depth int) error {
	for _, stmt := range block.Stmts {
		if err := e.emitStmt(stmt, depth); err != nil {
			return err
		}
	}
	return nil
}

func (e *emitter) emitStmt(stmt parser.Stmt, depth int) error {
	ind := indent(depth)
	switch s := stmt.(type) {
	case *parser.LetStmt:
		return e.emitLetStmt(s, depth)

	case *parser.ReturnStmt:
		if s.Value == nil {
			// bare return in a unit function
			if e.isMain {
				e.writef("%sreturn 0;\n", ind)
			} else {
				e.writef("%sreturn;\n", ind)
			}
			return nil
		}
		// return unit  → return (void) / return 0 for main
		if ident, ok := s.Value.(*parser.IdentExpr); ok && ident.Tok.Lexeme == "unit" {
			if e.isMain {
				e.writef("%sreturn 0;\n", ind)
			} else {
				e.writef("%sreturn;\n", ind)
			}
			return nil
		}
		var sb strings.Builder
		if err := e.emitExpr(s.Value, &sb); err != nil {
			return err
		}
		e.writef("%sreturn %s;\n", ind, sb.String())

	case *parser.ExprStmt:
		var sb strings.Builder
		if err := e.emitExpr(s.X, &sb); err != nil {
			return err
		}
		e.writef("%s%s;\n", ind, sb.String())

	case *parser.IfStmt:
		return e.emitIfStmt(s, depth)

	case *parser.LoopStmt:
		e.writef("%sfor (;;) {\n", ind)
		if err := e.emitBlock(s.Body, depth+1); err != nil {
			return err
		}
		e.writef("%s}\n", ind)

	case *parser.BreakStmt:
		e.writef("%sbreak;\n", ind)

	case *parser.BlockStmt:
		e.writef("%s{\n", ind)
		if err := e.emitBlock(s, depth+1); err != nil {
			return err
		}
		e.writef("%s}\n", ind)

	case *parser.AssignStmt:
		var vb strings.Builder
		if err := e.emitExpr(s.Value, &vb); err != nil {
			return err
		}
		e.writef("%s%s = %s;\n", indent(depth), s.Name.Lexeme, vb.String())

	case *parser.FieldAssignStmt:
		var recv, val strings.Builder
		if err := e.emitExpr(s.Target.Receiver, &recv); err != nil {
			return err
		}
		if err := e.emitExpr(s.Value, &val); err != nil {
			return err
		}
		recvType := e.res.ExprTypes[s.Target.Receiver]
		if gen, ok := recvType.(*typeck.GenType); ok && (gen.Con == "ref" || gen.Con == "refmut") {
			e.writef("%s%s->%s = %s;\n", ind, recv.String(), s.Target.Field.Lexeme, val.String())
		} else {
			e.writef("%s%s.%s = %s;\n", ind, recv.String(), s.Target.Field.Lexeme, val.String())
		}

	default:
		return fmt.Errorf("unhandled Stmt %T", stmt)
	}
	return nil
}

func (e *emitter) emitLetStmt(s *parser.LetStmt, depth int) error {
	t := e.res.ExprTypes[s.Value]
	if t == nil {
		return fmt.Errorf("no type recorded for let %s value", s.Name.Lexeme)
	}
	ct, err := e.cType(t)
	if err != nil {
		return err
	}
	var vb strings.Builder
	if err := e.emitExpr(s.Value, &vb); err != nil {
		return err
	}
	e.writef("%s%s %s = %s;\n", indent(depth), ct, s.Name.Lexeme, vb.String())
	return nil
}

func (e *emitter) emitIfStmt(s *parser.IfStmt, depth int) error {
	ind := indent(depth)
	var cb strings.Builder
	if err := e.emitExpr(s.Cond, &cb); err != nil {
		return err
	}
	e.writef("%sif (%s) {\n", ind, cb.String())
	if err := e.emitBlock(s.Then, depth+1); err != nil {
		return err
	}
	if s.Else == nil {
		e.writef("%s}\n", ind)
		return nil
	}
	e.writef("%s} else ", ind)
	switch el := s.Else.(type) {
	case *parser.IfStmt:
		// else if — emit without leading indent (we already wrote "} else ")
		var sub strings.Builder
		sub.WriteString(indent(depth))
		ee := &emitter{res: e.res, sb: sub, retIsUnit: e.retIsUnit, isMain: e.isMain}
		if err := ee.emitIfStmt(el, depth); err != nil {
			return err
		}
		// strip the leading indent that emitIfStmt wrote, because we already wrote "} else "
		result := strings.TrimPrefix(ee.sb.String(), indent(depth))
		e.write(result)
	case *parser.BlockStmt:
		e.writeln("{")
		if err := e.emitBlock(el, depth+1); err != nil {
			return err
		}
		e.writef("%s}\n", ind)
	}
	return nil
}

// ── expressions ───────────────────────────────────────────────────────────────

func (e *emitter) emitExpr(expr parser.Expr, sb *strings.Builder) error {
	switch ex := expr.(type) {
	case *parser.IntLitExpr:
		sb.WriteString(ex.Tok.Lexeme)

	case *parser.FloatLitExpr:
		sb.WriteString(ex.Tok.Lexeme)

	case *parser.StringLitExpr:
		sb.WriteString(ex.Tok.Lexeme) // already quoted

	case *parser.BoolLitExpr:
		if ex.Tok.Type == lexer.TokTrue {
			sb.WriteString("1")
		} else {
			sb.WriteString("0")
		}

	case *parser.IdentExpr:
		name := ex.Tok.Lexeme
		if name == "unit" {
			// Should not reach here (handled at call sites), but be safe.
			sb.WriteString("/* unit */")
		} else {
			sb.WriteString(name)
		}

	case *parser.BinaryExpr:
		var l, r strings.Builder
		if err := e.emitExpr(ex.Left, &l); err != nil {
			return err
		}
		if err := e.emitExpr(ex.Right, &r); err != nil {
			return err
		}
		op := ex.Op.Lexeme
		switch ex.Op.Type {
		case lexer.TokAnd:
			op = "&&"
		case lexer.TokOr:
			op = "||"
		}
		fmt.Fprintf(sb, "(%s %s %s)", l.String(), op, r.String())

	case *parser.UnaryExpr:
		var operand strings.Builder
		if err := e.emitExpr(ex.Operand, &operand); err != nil {
			return err
		}
		op := ex.Op.Lexeme
		if ex.Op.Type == lexer.TokNot {
			op = "!"
		}
		fmt.Fprintf(sb, "(%s%s)", op, operand.String())

	case *parser.MustExpr:
		return e.emitMustOrMatch(ex.X, ex.Arms, e.res.ExprTypes[ex], sb)

	case *parser.MatchExpr:
		return e.emitMustOrMatch(ex.X, ex.Arms, e.res.ExprTypes[ex], sb)

	case *parser.ReturnExpr:
		var vb strings.Builder
		if err := e.emitExpr(ex.Value, &vb); err != nil {
			return err
		}
		fmt.Fprintf(sb, "return %s", vb.String())

	case *parser.CallExpr:
		// Check for built-in print functions and emit printf directly.
		if ident, ok := ex.Fn.(*parser.IdentExpr); ok {
			if handled, err := e.emitBuiltinCall(ident.Tok.Lexeme, ex.Args, sb); handled {
				return err
			}
			// Result/option constructors
			if handled, err := e.emitConstructorCall(ex, ident, sb); handled {
				return err
			}
		}
		var fn strings.Builder
		if err := e.emitExpr(ex.Fn, &fn); err != nil {
			return err
		}
		args := make([]string, len(ex.Args))
		for i, arg := range ex.Args {
			var ab strings.Builder
			if err := e.emitExpr(arg, &ab); err != nil {
				return err
			}
			args[i] = ab.String()
		}
		fmt.Fprintf(sb, "%s(%s)", fn.String(), strings.Join(args, ", "))

	case *parser.FieldExpr:
		var recv strings.Builder
		if err := e.emitExpr(ex.Receiver, &recv); err != nil {
			return err
		}
		// If receiver type is ref<T>/refmut<T>, use ->; otherwise use .
		recvType := e.res.ExprTypes[ex.Receiver]
		if gen, ok := recvType.(*typeck.GenType); ok &&
			(gen.Con == "ref" || gen.Con == "refmut") {
			fmt.Fprintf(sb, "%s->%s", recv.String(), ex.Field.Lexeme)
		} else {
			fmt.Fprintf(sb, "%s.%s", recv.String(), ex.Field.Lexeme)
		}

	case *parser.IndexExpr:
		var coll, idx strings.Builder
		if err := e.emitExpr(ex.Collection, &coll); err != nil {
			return err
		}
		if err := e.emitExpr(ex.Index, &idx); err != nil {
			return err
		}
		fmt.Fprintf(sb, "%s[%s]", coll.String(), idx.String())

	default:
		return fmt.Errorf("unhandled Expr %T in emit", expr)
	}
	return nil
}

// ── built-in print functions ──────────────────────────────────────────────────

// emitBuiltinCall handles the print_* built-ins, emitting printf calls.
// Returns (true, err) if the name was a built-in; (false, nil) otherwise.
func (e *emitter) emitBuiltinCall(name string, args []parser.Expr, sb *strings.Builder) (bool, error) {
	if len(args) != 1 {
		return false, nil
	}
	var fmt_str string
	var cast string
	switch name {
	case "print":
		fmt_str = "%s\\n"
	case "print_int":
		fmt_str = "%lld\\n"
		cast = "(long long)"
	case "print_u32":
		fmt_str = "%u\\n"
	case "print_bool":
		// handled specially below
	case "print_f64":
		fmt_str = "%f\\n"
	default:
		return false, nil
	}

	var ab strings.Builder
	if err := e.emitExpr(args[0], &ab); err != nil {
		return true, err
	}
	arg := ab.String()

	if name == "print_bool" {
		fmt.Fprintf(sb, `printf("%%s\n", (%s) ? "true" : "false")`, arg)
	} else {
		fmt.Fprintf(sb, `printf("%s", %s%s)`, fmt_str, cast, arg)
	}
	return true, nil
}

// ── constructor emission ──────────────────────────────────────────────────────

func (e *emitter) emitConstructorCall(ex *parser.CallExpr, fn *parser.IdentExpr, sb *strings.Builder) (bool, error) {
	switch fn.Tok.Type {
	case lexer.TokSome:
		if len(ex.Args) != 1 {
			return false, nil
		}
		var ab strings.Builder
		if err := e.emitExpr(ex.Args[0], &ab); err != nil {
			return true, err
		}
		argType := e.res.ExprTypes[ex.Args[0]]
		if argType == nil {
			return false, nil
		}
		ct, err := e.cType(argType)
		if err != nil {
			return true, err
		}
		fmt.Fprintf(sb, "&(%s){%s}", ct, ab.String())
		return true, nil

	case lexer.TokNone:
		sb.WriteString("NULL")
		return true, nil

	case lexer.TokOk:
		if len(ex.Args) != 1 {
			return false, nil
		}
		resType, ok := e.res.ExprTypes[ex].(*typeck.GenType)
		if !ok || resType.Con != "result" {
			return false, nil
		}
		structName, err := e.resultTypeName(resType)
		if err != nil {
			return true, err
		}
		var ab strings.Builder
		if err := e.emitExpr(ex.Args[0], &ab); err != nil {
			return true, err
		}
		fmt.Fprintf(sb, "(%s){ ._ok = 1, ._ok_val = %s }", structName, ab.String())
		return true, nil

	case lexer.TokErr:
		if len(ex.Args) != 1 {
			return false, nil
		}
		resType, ok := e.res.ExprTypes[ex].(*typeck.GenType)
		if !ok || resType.Con != "result" {
			return false, nil
		}
		structName, err := e.resultTypeName(resType)
		if err != nil {
			return true, err
		}
		var ab strings.Builder
		if err := e.emitExpr(ex.Args[0], &ab); err != nil {
			return true, err
		}
		fmt.Fprintf(sb, "(%s){ ._ok = 0, ._err_val = %s }", structName, ab.String())
		return true, nil
	}
	return false, nil
}

// ── must/match emission ───────────────────────────────────────────────────────

func (e *emitter) emitMustOrMatch(x parser.Expr, arms []parser.MustArm, bodyType typeck.Type, sb *strings.Builder) error {
	tmp := e.freshTmp()
	res := e.freshTmp()

	xType := e.res.ExprTypes[x]

	var xb strings.Builder
	if err := e.emitExpr(x, &xb); err != nil {
		return err
	}

	var bodyC string
	if bodyType != nil && !bodyType.Equals(typeck.TUnit) && !bodyType.Equals(typeck.TNever) {
		ct, err := e.cType(bodyType)
		if err != nil {
			return err
		}
		bodyC = ct
	}

	fmt.Fprintf(sb, "(__extension__ ({\n")

	xC, err := e.cType(xType)
	if err != nil {
		return err
	}
	fmt.Fprintf(sb, "    %s %s = %s;\n", xC, tmp, xb.String())

	if bodyC != "" {
		fmt.Fprintf(sb, "    %s %s;\n", bodyC, res)
	}

	for i, arm := range arms {
		cond, binding, err := e.patternCondAndBinding(arm.Pattern, xType, tmp)
		if err != nil {
			return err
		}
		if i == 0 {
			if cond != "" {
				fmt.Fprintf(sb, "    if (%s) {\n", cond)
			} else {
				fmt.Fprintf(sb, "    {\n")
			}
		} else {
			if cond != "" {
				fmt.Fprintf(sb, "    } else if (%s) {\n", cond)
			} else {
				fmt.Fprintf(sb, "    } else {\n")
			}
		}

		if binding != "" {
			fmt.Fprintf(sb, "        %s\n", binding)
		}

		armType := e.res.ExprTypes[arm.Body]
		var bodyExpr strings.Builder
		if err := e.emitExpr(arm.Body, &bodyExpr); err != nil {
			return err
		}
		if armType != nil && armType.Equals(typeck.TNever) {
			fmt.Fprintf(sb, "        %s;\n", bodyExpr.String())
		} else if bodyC != "" {
			fmt.Fprintf(sb, "        %s = %s;\n", res, bodyExpr.String())
		} else {
			fmt.Fprintf(sb, "        %s;\n", bodyExpr.String())
		}
	}
	fmt.Fprintf(sb, "    }\n")

	if bodyC != "" {
		fmt.Fprintf(sb, "    %s;\n", res)
	} else {
		fmt.Fprintf(sb, "    (void)0;\n")
	}
	fmt.Fprintf(sb, "}))")
	return nil
}

func (e *emitter) patternCondAndBinding(pattern parser.Expr, xType typeck.Type, tmp string) (cond string, binding string, err error) {
	switch p := pattern.(type) {
	case *parser.IdentExpr:
		switch p.Tok.Lexeme {
		case "_":
			return "", "", nil
		case "none":
			return fmt.Sprintf("%s == NULL", tmp), "", nil
		case "true":
			return fmt.Sprintf("%s", tmp), "", nil
		case "false":
			return fmt.Sprintf("!%s", tmp), "", nil
		default:
			bt := e.res.ExprTypes[p]
			if bt != nil {
				ct, err := e.cType(bt)
				if err != nil {
					return "", "", err
				}
				return "", fmt.Sprintf("%s %s = %s;", ct, p.Tok.Lexeme, tmp), nil
			}
			return "", "", nil
		}

	case *parser.BoolLitExpr:
		if p.Tok.Type == lexer.TokTrue {
			return tmp, "", nil
		}
		return fmt.Sprintf("!%s", tmp), "", nil

	case *parser.CallExpr:
		fn, ok := p.Fn.(*parser.IdentExpr)
		if !ok {
			return "", "", fmt.Errorf("invalid pattern")
		}
		switch fn.Tok.Lexeme {
		case "some":
			cond = fmt.Sprintf("%s != NULL", tmp)
			if len(p.Args) == 1 {
				if v, ok2 := p.Args[0].(*parser.IdentExpr); ok2 {
					vType := e.res.ExprTypes[v]
					if vType != nil {
						ct, err := e.cType(vType)
						if err != nil {
							return "", "", err
						}
						binding = fmt.Sprintf("%s %s = *%s;", ct, v.Tok.Lexeme, tmp)
					}
				}
			}
			return cond, binding, nil

		case "ok":
			cond = fmt.Sprintf("%s._ok", tmp)
			if len(p.Args) == 1 {
				if v, ok2 := p.Args[0].(*parser.IdentExpr); ok2 {
					vType := e.res.ExprTypes[v]
					if vType != nil {
						ct, err := e.cType(vType)
						if err != nil {
							return "", "", err
						}
						binding = fmt.Sprintf("%s %s = %s._ok_val;", ct, v.Tok.Lexeme, tmp)
					}
				}
			}
			return cond, binding, nil

		case "err":
			cond = fmt.Sprintf("!%s._ok", tmp)
			if len(p.Args) == 1 {
				if v, ok2 := p.Args[0].(*parser.IdentExpr); ok2 {
					vType := e.res.ExprTypes[v]
					if vType != nil {
						ct, err := e.cType(vType)
						if err != nil {
							return "", "", err
						}
						binding = fmt.Sprintf("%s %s = %s._err_val;", ct, v.Tok.Lexeme, tmp)
					}
				}
			}
			return cond, binding, nil
		}
	}
	return "", "", nil
}

// ── type mapping ──────────────────────────────────────────────────────────────

func (e *emitter) cType(t typeck.Type) (string, error) {
	switch t {
	case typeck.TUnit:
		return "void", nil
	case typeck.TBool:
		return "int", nil
	case typeck.TStr:
		return "const char*", nil
	case typeck.TI8:
		return "int8_t", nil
	case typeck.TI16:
		return "int16_t", nil
	case typeck.TI32:
		return "int32_t", nil
	case typeck.TI64:
		return "int64_t", nil
	case typeck.TI128:
		return "__int128", nil
	case typeck.TU8:
		return "uint8_t", nil
	case typeck.TU16:
		return "uint16_t", nil
	case typeck.TU32:
		return "uint32_t", nil
	case typeck.TU64:
		return "uint64_t", nil
	case typeck.TU128:
		return "unsigned __int128", nil
	case typeck.TF32:
		return "float", nil
	case typeck.TF64:
		return "double", nil
	case typeck.TNever:
		return "void", nil
	}

	switch tt := t.(type) {
	case *typeck.GenType:
		switch tt.Con {
		case "ref", "refmut":
			if len(tt.Params) == 1 {
				inner, err := e.cType(tt.Params[0])
				if err != nil {
					return "", err
				}
				return inner + "*", nil
			}
		case "vec", "ring":
			if len(tt.Params) == 1 {
				inner, err := e.cType(tt.Params[0])
				if err != nil {
					return "", err
				}
				return inner + "*", nil
			}
		case "option":
			if len(tt.Params) == 1 {
				inner, err := e.cType(tt.Params[0])
				if err != nil {
					return "", err
				}
				return inner + "*", nil // null == none
			}
		case "result":
			if len(tt.Params) == 2 {
				return e.resultTypeName(tt)
			}
		}
		return "", fmt.Errorf("unsupported generic type: %s", t)

	case *typeck.StructType:
		return tt.Name, nil

	case *typeck.FnType:
		// Function pointer type — emitting inline is complex; use void* for now.
		return "void*", nil
	}

	return "", fmt.Errorf("cannot map type %s to C", t)
}
