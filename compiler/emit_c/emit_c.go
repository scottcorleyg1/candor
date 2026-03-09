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
	contracts []parser.ContractClause
	retType   typeck.Type
	inEnsures bool // true when emitting ensures expressions (result -> _cnd_result)
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
	e.writeln("#include <stdlib.h>")
	e.writeln("#include <assert.h>")
	e.writeln("")

	// Emit result<T,E> struct typedefs used in this file.
	if err := e.emitResultStructs(); err != nil {
		return err
	}

	// Emit vec<T> struct typedefs and push helpers used in this file.
	if err := e.emitVecStructs(); err != nil {
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

func (e *emitter) vecElemMangle(inner string) string {
	r := strings.NewReplacer(" ", "_", "*", "ptr", "<", "_", ">", "_", ",", "_")
	return r.Replace(inner)
}

func (e *emitter) vecTypeName(elemC string) string {
	return "_CndVec_" + e.vecElemMangle(elemC)
}

func (e *emitter) vecPushName(elemC string) string {
	return "_cnd_vec_push_" + e.vecElemMangle(elemC)
}

func (e *emitter) emitVecStructs() error {
	seen := map[string]bool{}
	for _, t := range e.res.ExprTypes {
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "vec" || len(gen.Params) != 1 {
			continue
		}
		elemC, err := e.cType(gen.Params[0])
		if err != nil {
			continue
		}
		name := e.vecTypeName(elemC)
		if seen[name] {
			continue
		}
		seen[name] = true
		pushFn := e.vecPushName(elemC)
		e.writef("typedef struct { %s* _data; uint64_t _len; uint64_t _cap; } %s;\n", elemC, name)
		e.writef("static inline void %s(%s* v, %s val) {\n", pushFn, name, elemC)
		e.writef("    if (v->_len >= v->_cap) {\n")
		e.writef("        uint64_t _nc = v->_cap ? v->_cap * 2 : 4;\n")
		e.writef("        v->_data = (%s*)realloc(v->_data, _nc * sizeof(%s));\n", elemC, elemC)
		e.writef("        v->_cap = _nc;\n")
		e.writef("    }\n")
		e.writef("    v->_data[v->_len++] = val;\n")
		e.writef("}\n")
	}
	if len(seen) > 0 {
		e.writeln("")
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

	// Emit effects annotation as a C comment before the definition.
	if ann := e.res.FnEffects[d.Name.Lexeme]; ann != nil {
		switch ann.Kind {
		case parser.EffectsPure:
			e.writeln("\n/* pure */")
		case parser.EffectsDecl:
			e.writef("\n/* effects: %s */\n", strings.Join(ann.Names, ", "))
		case parser.EffectsCap:
			e.writef("\n/* cap: %s */\n", strings.Join(ann.Names, ", "))
		}
		e.writef("%s {\n", proto)
	} else {
		e.writef("\n%s {\n", proto)
	}

	sig := e.res.FnSigs[d.Name.Lexeme]
	// Save and set function context.
	prevRetIsUnit := e.retIsUnit
	prevIsMain := e.isMain
	prevContracts := e.contracts
	prevRetType := e.retType
	e.retIsUnit = sig.Ret.Equals(typeck.TUnit)
	e.isMain = d.Name.Lexeme == "main"
	e.contracts = d.Contracts
	e.retType = sig.Ret

	// Emit requires assertions at the top of the function body.
	for _, cc := range d.Contracts {
		if cc.Kind == parser.ContractRequires {
			e.write("    assert(")
			if err := e.emitExpr(cc.Expr, &e.sb); err != nil {
				return err
			}
			e.write(");\n")
		}
	}

	isMain := e.isMain
	if err := e.emitBlock(d.Body, 1); err != nil {
		return err
	}

	e.retIsUnit = prevRetIsUnit
	e.isMain = prevIsMain
	e.contracts = prevContracts
	e.retType = prevRetType

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
		if isUnitValue(s.Value) {
			if e.isMain {
				e.writef("%sreturn 0;\n", ind)
			} else {
				e.writef("%sreturn;\n", ind)
			}
			return nil
		}
		// Collect ensures clauses.
		var ensures []parser.ContractClause
		for _, cc := range e.contracts {
			if cc.Kind == parser.ContractEnsures {
				ensures = append(ensures, cc)
			}
		}
		if len(ensures) > 0 {
			// Wrap: { RetType _cnd_result = val; assert(ensures...); return _cnd_result; }
			ct, err := e.cType(e.retType)
			if err != nil {
				return err
			}
			e.writef("%s{\n", ind)
			e.writef("%s    %s _cnd_result = ", ind, ct)
			if err := e.emitExpr(s.Value, &e.sb); err != nil {
				return err
			}
			e.write(";\n")
			for _, cc := range ensures {
				prevInEnsures := e.inEnsures
				e.inEnsures = true
				e.writef("%s    assert(", ind)
				if err := e.emitExpr(cc.Expr, &e.sb); err != nil {
					e.inEnsures = prevInEnsures
					return err
				}
				e.inEnsures = prevInEnsures
				e.write(");\n")
			}
			if e.isMain {
				e.writef("%s    return 0;\n", ind)
			} else {
				e.writef("%s    return _cnd_result;\n", ind)
			}
			e.writef("%s}\n", ind)
			return nil
		}
		e.write(ind + "return ")
		if err := e.emitExpr(s.Value, &e.sb); err != nil {
			return err
		}
		e.write(";\n")

	case *parser.ExprStmt:
		e.write(ind)
		if err := e.emitExpr(s.X, &e.sb); err != nil {
			return err
		}
		e.write(";\n")

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
		e.writef("%s%s = ", indent(depth), s.Name.Lexeme)
		if err := e.emitExpr(s.Value, &e.sb); err != nil {
			return err
		}
		e.write(";\n")

	case *parser.FieldAssignStmt:
		recvType := e.res.ExprTypes[s.Target.Receiver]
		e.write(ind)
		if err := e.emitExpr(s.Target.Receiver, &e.sb); err != nil {
			return err
		}
		if gen, ok := recvType.(*typeck.GenType); ok && (gen.Con == "ref" || gen.Con == "refmut") {
			e.write("->")
		} else {
			e.write(".")
		}
		e.write(s.Target.Field.Lexeme + " = ")
		if err := e.emitExpr(s.Value, &e.sb); err != nil {
			return err
		}
		e.write(";\n")

	case *parser.AssertStmt:
		e.write(ind + "assert(")
		if err := e.emitExpr(s.Expr, &e.sb); err != nil {
			return err
		}
		e.write(");\n")

	case *parser.ForStmt:
		return e.emitForStmt(s, depth)

	default:
		return fmt.Errorf("unhandled Stmt %T", stmt)
	}
	return nil
}

func (e *emitter) emitForStmt(s *parser.ForStmt, depth int) error {
	ind := indent(depth)
	collType := e.res.ExprTypes[s.Collection]
	gen := collType.(*typeck.GenType) // validated by typeck
	elemC, err := e.cType(gen.Params[0])
	if err != nil {
		return err
	}
	collC, err := e.cType(collType)
	if err != nil {
		return err
	}
	var collB strings.Builder
	if err := e.emitExpr(s.Collection, &collB); err != nil {
		return err
	}
	collTmp := e.freshTmp()
	iTmp := e.freshTmp()
	e.writef("%s{\n", ind)
	e.writef("%s    %s %s = %s;\n", ind, collC, collTmp, collB.String())
	e.writef("%s    for (uint64_t %s = 0; %s < %s._len; %s++) {\n",
		ind, iTmp, iTmp, collTmp, iTmp)
	e.writef("%s        %s %s = %s._data[%s];\n",
		ind, elemC, s.Var.Lexeme, collTmp, iTmp)
	if err := e.emitBlock(s.Body, depth+2); err != nil {
		return err
	}
	e.writef("%s    }\n", ind)
	e.writef("%s}\n", ind)
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
	e.writef("%s%s %s = ", indent(depth), ct, s.Name.Lexeme)
	if err := e.emitExpr(s.Value, &e.sb); err != nil {
		return err
	}
	e.write(";\n")
	return nil
}

func (e *emitter) emitIfStmt(s *parser.IfStmt, depth int) error {
	ind := indent(depth)
	e.write(ind + "if (")
	if err := e.emitExpr(s.Cond, &e.sb); err != nil {
		return err
	}
	e.write(") {\n")
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
		} else if e.inEnsures && name == "result" {
			sb.WriteString("_cnd_result")
		} else {
			sb.WriteString(name)
		}

	case *parser.BinaryExpr:
		op := ex.Op.Lexeme
		switch ex.Op.Type {
		case lexer.TokAnd:
			op = "&&"
		case lexer.TokOr:
			op = "||"
		}
		sb.WriteByte('(')
		if err := e.emitExpr(ex.Left, sb); err != nil {
			return err
		}
		sb.WriteByte(' ')
		sb.WriteString(op)
		sb.WriteByte(' ')
		if err := e.emitExpr(ex.Right, sb); err != nil {
			return err
		}
		sb.WriteByte(')')

	case *parser.UnaryExpr:
		op := ex.Op.Lexeme
		if ex.Op.Type == lexer.TokNot {
			op = "!"
		}
		sb.WriteByte('(')
		sb.WriteString(op)
		if err := e.emitExpr(ex.Operand, sb); err != nil {
			return err
		}
		sb.WriteByte(')')

	case *parser.MustExpr:
		return e.emitMustOrMatch(ex.X, ex.Arms, e.res.ExprTypes[ex], sb)

	case *parser.MatchExpr:
		return e.emitMustOrMatch(ex.X, ex.Arms, e.res.ExprTypes[ex], sb)

	case *parser.ReturnExpr:
		sb.WriteString("return ")
		if err := e.emitExpr(ex.Value, sb); err != nil {
			return err
		}

	case *parser.CallExpr:
		// Check for built-in print functions and vec builtins.
		if ident, ok := ex.Fn.(*parser.IdentExpr); ok {
			// vec_new() emits a zero-initialised struct literal.
			if ident.Tok.Lexeme == "vec_new" {
				t := e.res.ExprTypes[ident]
				if gen, ok2 := t.(*typeck.GenType); ok2 && gen.Con == "vec" {
					elemC, err := e.cType(gen.Params[0])
					if err != nil {
						return err
					}
					fmt.Fprintf(sb, "(%s){ NULL, 0, 0 }", e.vecTypeName(elemC))
					return nil
				}
			}
			if handled, err := e.emitBuiltinCall(ident.Tok.Lexeme, ex.Args, sb); handled {
				return err
			}
			// Result/option constructors
			if handled, err := e.emitConstructorCall(ex, ident, sb); handled {
				return err
			}
		}
		if err := e.emitExpr(ex.Fn, sb); err != nil {
			return err
		}
		sb.WriteByte('(')
		for i, arg := range ex.Args {
			if i > 0 {
				sb.WriteString(", ")
			}
			if err := e.emitExpr(arg, sb); err != nil {
				return err
			}
		}
		sb.WriteByte(')')

	case *parser.FieldExpr:
		if err := e.emitExpr(ex.Receiver, sb); err != nil {
			return err
		}
		recvType := e.res.ExprTypes[ex.Receiver]
		if gen, ok := recvType.(*typeck.GenType); ok &&
			(gen.Con == "ref" || gen.Con == "refmut") {
			sb.WriteString("->")
		} else {
			sb.WriteByte('.')
		}
		sb.WriteString(ex.Field.Lexeme)

	case *parser.IndexExpr:
		if err := e.emitExpr(ex.Collection, sb); err != nil {
			return err
		}
		sb.WriteByte('[')
		if err := e.emitExpr(ex.Index, sb); err != nil {
			return err
		}
		sb.WriteByte(']')

	case *parser.StructLitExpr:
		sb.WriteByte('(')
		sb.WriteString(ex.TypeName.Lexeme)
		sb.WriteString("){ ")
		for i, fi := range ex.Fields {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteByte('.')
			sb.WriteString(fi.Name.Lexeme)
			sb.WriteString(" = ")
			if err := e.emitExpr(fi.Value, sb); err != nil {
				return err
			}
		}
		sb.WriteString(" }")

	default:
		return fmt.Errorf("unhandled Expr %T in emit", expr)
	}
	return nil
}

// ── built-in print functions ──────────────────────────────────────────────────

// emitBuiltinCall handles the print_* built-ins, emitting printf calls.
// Returns (true, err) if the name was a built-in; (false, nil) otherwise.
func (e *emitter) emitBuiltinCall(name string, args []parser.Expr, sb *strings.Builder) (bool, error) {
	// vec builtins — argument count varies
	switch name {
	case "vec_new":
		// vec_new() — the type is recorded on the CallExpr's Fn ident
		// We need the full call expression to get the type; handled via the parent CallExpr.
		// This path won't be reached (emitExpr handles CallExpr specially before here).
		return false, nil

	case "vec_push":
		// vec_push(v, val) → _cnd_vec_push_T(&(v), val)
		if len(args) != 2 {
			return false, nil
		}
		vecType, ok := e.res.ExprTypes[args[0]].(*typeck.GenType)
		if !ok || vecType.Con != "vec" || len(vecType.Params) == 0 {
			return false, nil
		}
		elemC, err := e.cType(vecType.Params[0])
		if err != nil {
			return true, err
		}
		sb.WriteString(e.vecPushName(elemC))
		sb.WriteString("(&(")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString("), ")
		if err := e.emitExpr(args[1], sb); err != nil {
			return true, err
		}
		sb.WriteByte(')')
		return true, nil

	case "vec_len":
		// vec_len(v) → (v)._len
		if len(args) != 1 {
			return false, nil
		}
		sb.WriteByte('(')
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString(")._len")
		return true, nil
	}

	if len(args) != 1 {
		return false, nil
	}
	switch name {
	case "print":
		sb.WriteString("printf(\"%s\\n\", ")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteByte(')')
	case "print_int":
		sb.WriteString("printf(\"%lld\\n\", (long long)")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteByte(')')
	case "print_u32":
		sb.WriteString("printf(\"%u\\n\", ")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteByte(')')
	case "print_bool":
		sb.WriteString("printf(\"%s\\n\", (")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString(") ? \"true\" : \"false\")")
	case "print_f64":
		sb.WriteString("printf(\"%f\\n\", ")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteByte(')')
	default:
		return false, nil
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
		argType := e.res.ExprTypes[ex.Args[0]]
		if argType == nil {
			return false, nil
		}
		ct, err := e.cType(argType)
		if err != nil {
			return true, err
		}
		sb.WriteString("&(")
		sb.WriteString(ct)
		sb.WriteByte('{')
		if err := e.emitExpr(ex.Args[0], sb); err != nil {
			return true, err
		}
		sb.WriteByte('}')
		sb.WriteByte(')')
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
		sb.WriteByte('(')
		sb.WriteString(structName)
		sb.WriteString("){ ._ok = 1, ._ok_val = ")
		if err := e.emitExpr(ex.Args[0], sb); err != nil {
			return true, err
		}
		sb.WriteByte('}')
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
		sb.WriteByte('(')
		sb.WriteString(structName)
		sb.WriteString("){ ._ok = 0, ._err_val = ")
		if err := e.emitExpr(ex.Args[0], sb); err != nil {
			return true, err
		}
		sb.WriteByte('}')
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
		case "vec":
			if len(tt.Params) == 1 {
				inner, err := e.cType(tt.Params[0])
				if err != nil {
					return "", err
				}
				return e.vecTypeName(inner), nil
			}
		case "ring":
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

// isUnitValue returns true if the expression is the identifier "unit".
func isUnitValue(e parser.Expr) bool {
	ident, ok := e.(*parser.IdentExpr)
	return ok && ident.Tok.Lexeme == "unit"
}
