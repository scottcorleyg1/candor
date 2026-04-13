// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/candor-core/candor

// Package emit_go emits idiomatic Go from a type-checked Candor AST.
//
// Type mapping:
//   unit              → (no return value / return)
//   bool              → bool
//   str               → string
//   i64/i32/i16/i8    → int64/int32/int16/int8
//   u64/u32/u16/u8    → uint64/uint32/uint16/uint8
//   f64 / f32         → float64 / float32
//   vec<T>            → []T
//   option<T>         → *T  (nil = none)
//   result<T,E>       → (T, error)
//   map<K,V>          → map[K]V
//   set<T>            → map[T]struct{}
//   struct S          → type S struct
//   fn f(...)         → func f(...)
//
// Safety features that cannot be expressed in Go are written to the AuditLog
// and appear in the companion .audit.md report.
package emit_go

import (
	"fmt"
	"strings"

	"github.com/candor-core/candor/compiler/emit_c"
	"github.com/candor-core/candor/compiler/parser"
	"github.com/candor-core/candor/compiler/typeck"
)

// Emit translates a type-checked Candor file to a Go source string.
// Returns the Go source, the populated AuditLog, and any error.
func Emit(file *parser.File, res *typeck.Result, sourceName string) (string, *emit_c.AuditLog, error) {
	log := emit_c.NewAuditLogGo(sourceName)
	e := &emitter{res: res, audit: log}
	if err := e.emitFile(file); err != nil {
		return "", nil, err
	}
	return e.sb.String(), log, nil
}

// ── emitter ───────────────────────────────────────────────────────────────────

type emitter struct {
	res            *typeck.Result
	audit          *emit_c.AuditLog
	sb             strings.Builder
	tmpN           int
	indent         int
	curFnName      string
	curRetIsErr    bool   // result<T,E> return → (T, error) in Go
	curRetZeroVal  string // zero value for the T in result<T,E> (e.g. "Account{}", "0", `""`)
}

func (e *emitter) write(s string)            { e.sb.WriteString(s) }
func (e *emitter) writef(f string, a ...any) { fmt.Fprintf(&e.sb, f, a...) }
func (e *emitter) writeln(s string)          { e.sb.WriteString(s); e.sb.WriteByte('\n') }
func (e *emitter) nl()                       { e.sb.WriteByte('\n') }
func (e *emitter) pad() string               { return strings.Repeat("\t", e.indent) }

func (e *emitter) freshTmp() string {
	e.tmpN++
	return fmt.Sprintf("_t%d", e.tmpN)
}

// ── file ──────────────────────────────────────────────────────────────────────

func (e *emitter) emitFile(file *parser.File) error {
	pkgName := "main"
	for _, d := range file.Decls {
		if md, ok := d.(*parser.ModuleDecl); ok && md.Name.Lexeme != "" {
			pkgName = md.Name.Lexeme
			break
		}
	}
	e.writef("package %s\n\n", pkgName)

	imports := e.collectImports(file)
	if len(imports) > 0 {
		e.writeln("import (")
		for _, imp := range imports {
			e.writef("\t%q\n", imp)
		}
		e.writeln(")")
		e.nl()
	}

	for _, d := range file.Decls {
		if sd, ok := d.(*parser.StructDecl); ok {
			if err := e.emitStruct(sd); err != nil {
				return err
			}
		}
	}
	for _, d := range file.Decls {
		if ed, ok := d.(*parser.EnumDecl); ok {
			if err := e.emitEnum(ed); err != nil {
				return err
			}
		}
	}
	for _, d := range file.Decls {
		if fd, ok := d.(*parser.FnDecl); ok {
			if err := e.emitFnDecl(fd); err != nil {
				return err
			}
		}
	}
	return nil
}

// collectImports inspects the AST to determine needed import paths.
func (e *emitter) collectImports(file *parser.File) []string {
	need := map[string]bool{}

	var scanStmt func(parser.Stmt)
	var scanExpr func(parser.Expr)
	scanExpr = func(ex parser.Expr) {
		if ex == nil {
			return
		}
		switch v := ex.(type) {
		case *parser.CallExpr:
			if id, ok := v.Fn.(*parser.IdentExpr); ok {
				switch id.Tok.Lexeme {
				case "print", "print_err":
					need["fmt"] = true
				case "_cnd_int_to_str", "int_to_str":
					need["strconv"] = true
				case "str_starts_with", "str_contains":
					need["strings"] = true
				case "os_exit":
					need["os"] = true
				case "err":
					// err() constructor in result-returning fn → fmt.Errorf
					need["fmt"] = true
				}
			}
			scanExpr(v.Fn)
			for _, a := range v.Args {
				scanExpr(a)
			}
		case *parser.BinaryExpr:
			scanExpr(v.Left)
			scanExpr(v.Right)
		case *parser.UnaryExpr:
			scanExpr(v.Operand)
		case *parser.FieldExpr:
			scanExpr(v.Receiver)
		case *parser.IndexExpr:
			scanExpr(v.Collection)
			scanExpr(v.Index)
		case *parser.MustExpr:
			scanExpr(v.X)
			for _, arm := range v.Arms {
				scanExpr(arm.Body)
			}
		case *parser.MatchExpr:
			scanExpr(v.X)
			for _, arm := range v.Arms {
				scanExpr(arm.Body)
			}
		case *parser.BlockExpr:
			for _, s := range v.Stmts {
				scanStmt(s)
			}
		case *parser.StructLitExpr:
			for _, fi := range v.Fields {
				scanExpr(fi.Value)
			}
		case *parser.CastExpr:
			scanExpr(v.X)
		}
	}
	scanStmt = func(st parser.Stmt) {
		switch v := st.(type) {
		case *parser.ExprStmt:
			scanExpr(v.X)
		case *parser.LetStmt:
			scanExpr(v.Value)
		case *parser.ReturnStmt:
			scanExpr(v.Value)
		case *parser.IfStmt:
			scanExpr(v.Cond)
			scanStmt(v.Then)
			if v.Else != nil {
				scanStmt(v.Else)
			}
		case *parser.BlockStmt:
			for _, s := range v.Stmts {
				scanStmt(s)
			}
		case *parser.LoopStmt:
			scanStmt(v.Body)
		case *parser.WhileStmt:
			scanExpr(v.Cond)
			scanStmt(v.Body)
		case *parser.ForStmt:
			scanExpr(v.Collection)
			scanStmt(v.Body)
		case *parser.AssignStmt:
			scanExpr(v.Value)
		case *parser.FieldAssignStmt:
			scanExpr(v.Value)
		case *parser.IndexAssignStmt:
			scanExpr(v.Value)
		}
	}
	for _, d := range file.Decls {
		if fd, ok := d.(*parser.FnDecl); ok {
			scanStmt(fd.Body)
		}
	}

	var out []string
	for _, pkg := range []string{"fmt", "os", "strconv", "strings"} {
		if need[pkg] {
			out = append(out, pkg)
		}
	}
	return out
}

// ── struct ────────────────────────────────────────────────────────────────────

func (e *emitter) emitStruct(sd *parser.StructDecl) error {
	e.writef("type %s struct {\n", sd.Name.Lexeme)
	for _, f := range sd.Fields {
		gt, err := e.goType(f.Type)
		if err != nil {
			return err
		}
		e.writef("\t%s %s\n", capitalize(f.Name.Lexeme), gt)
	}
	e.writeln("}\n")
	return nil
}

// ── enum ──────────────────────────────────────────────────────────────────────

func (e *emitter) emitEnum(ed *parser.EnumDecl) error {
	name := ed.Name.Lexeme
	e.writef("// %s is a Candor enum — variants implement is%s().\n", name, name)
	e.writef("type %s interface{ is%s() }\n\n", name, name)
	for _, v := range ed.Variants {
		vname := name + "_" + v.Name.Lexeme
		if len(v.Fields) == 0 {
			e.writef("type %s struct{}\n", vname)
			e.writef("func (%s) is%s() {}\n\n", vname, name)
		} else {
			e.writef("type %s struct {\n", vname)
			for i, f := range v.Fields {
				gt, err := e.goType(f)
				if err != nil {
					return err
				}
				e.writef("\tV%d %s\n", i, gt)
			}
			e.writef("}\n")
			e.writef("func (%s) is%s() {}\n\n", vname, name)
		}
	}
	return nil
}

// ── function ──────────────────────────────────────────────────────────────────

func (e *emitter) emitFnDecl(fd *parser.FnDecl) error {
	name := fd.Name.Lexeme
	e.curFnName = name
	e.curRetIsErr = false
	e.tmpN = 0

	// Audit: effects annotations.
	if fd.Effects != nil {
		switch fd.Effects.Kind {
		case parser.EffectsPure:
			e.audit.AddEntry(emit_c.AuditEntry{
				Category:    "pure",
				FnName:      name,
				Line:        fd.Name.Line,
				Detail:      "pure",
				CEquiv:      "none enforced (Go has no pure annotation)",
				Explanation: "Candor enforces at compile time that pure functions cannot call any function with effects. Go has no equivalent.",
			})
		case parser.EffectsDecl:
			for _, eff := range fd.Effects.Names {
				e.audit.AddEntry(emit_c.AuditEntry{
					Category:    "effects",
					FnName:      name,
					Line:        fd.Name.Line,
					Detail:      fmt.Sprintf("effects(%s)", eff),
					CEquiv:      "none (dropped)",
					Explanation: "Candor enforces that only functions declaring effects(" + eff + ") can perform these operations. Any Go function can perform them silently.",
				})
			}
		}
	}

	// Audit: requires/ensures contracts.
	for _, c := range fd.Contracts {
		src := exprText(c.Expr)
		if c.Kind == parser.ContractRequires {
			e.audit.AddEntry(emit_c.AuditEntry{
				Category:    "requires",
				FnName:      name,
				Line:        fd.Name.Line,
				Detail:      "requires " + src,
				CEquiv:      "// requires: " + src + " (comment only)",
				Explanation: "Candor requires clauses are in the function signature — machine-readable by every caller. In Go this becomes a comment, invisible to the type system.",
			})
		} else {
			e.audit.AddEntry(emit_c.AuditEntry{
				Category:    "ensures",
				FnName:      name,
				Line:        fd.Name.Line,
				Detail:      "ensures " + src,
				CEquiv:      "// ensures: " + src + " (comment only)",
				Explanation: "Candor ensures clauses are verified postconditions. In Go this becomes a comment with no enforcement.",
			})
		}
	}

	// Build parameter list.
	var params []string
	for _, p := range fd.Params {
		gt, err := e.goType(p.Type)
		if err != nil {
			return err
		}
		params = append(params, p.Name.Lexeme+" "+gt)
	}

	// Build return type.
	retGo, retIsErr, err := e.goReturnType(fd.RetType)
	if err != nil {
		return err
	}
	e.curRetIsErr = retIsErr
	if retIsErr {
		e.curRetZeroVal = goZeroVal(fd.RetType)
	} else {
		e.curRetZeroVal = ""
	}

	if name == "main" {
		e.writeln("func main() {")
	} else {
		paramStr := strings.Join(params, ", ")
		if retGo == "" {
			e.writef("func %s(%s) {\n", name, paramStr)
		} else {
			e.writef("func %s(%s) %s {\n", name, paramStr, retGo)
		}
	}

	// Emit requires contracts as runtime panics (closest Go equivalent).
	for _, c := range fd.Contracts {
		if c.Kind != parser.ContractRequires {
			continue
		}
		src := exprText(c.Expr)
		reqGo, err := e.emitExprStr(c.Expr)
		if err != nil {
			e.writef("\t// requires: %s\n", src)
		} else {
			e.writef("\tif !(%s) { panic(\"requires violated: %s\") }\n", reqGo, escStr(src))
		}
	}

	e.indent = 1
	stmts := fd.Body.Stmts
	// If the function has a non-unit return type and the last statement is a
	// bare expression, emit it as an implicit return.
	if retGo != "" && len(stmts) > 0 {
		if es, ok := stmts[len(stmts)-1].(*parser.ExprStmt); ok {
			for _, s := range stmts[:len(stmts)-1] {
				if err := e.emitStmt(s); err != nil {
					return err
				}
			}
			var retLine string
			if e.curRetIsErr {
				if inner, yes := isOkCall(es.X); yes {
					innerStr, err2 := e.emitExprStr(inner)
					if err2 != nil {
						return err2
					}
					retLine = fmt.Sprintf("return %s, nil", innerStr)
				} else if inner, yes := isErrCall(es.X); yes {
					innerStr, err2 := e.emitExprStr(inner)
					if err2 != nil {
						return err2
					}
					retLine = fmt.Sprintf("return %s, fmt.Errorf(\"%%s\", %s)", e.curRetZeroVal, innerStr)
				}
			}
			if retLine == "" {
				exprStr, err := e.emitExprStr(es.X)
				if err != nil {
					return err
				}
				retLine = "return " + exprStr
			}
			e.writef("%s%s\n", e.pad(), retLine)
			e.indent = 0
			e.writeln("}\n")
			return nil
		}
	}
	for _, s := range stmts {
		if err := e.emitStmt(s); err != nil {
			return err
		}
	}
	e.indent = 0
	e.writeln("}\n")
	return nil
}

// goReturnType maps a Candor return TypeExpr to its Go equivalent.
// Returns (goStr, isErrorReturn, error).
func (e *emitter) goReturnType(te parser.TypeExpr) (string, bool, error) {
	if te == nil {
		return "", false, nil
	}
	if nt, ok := te.(*parser.NamedType); ok && nt.Name.Lexeme == "unit" {
		return "", false, nil
	}
	if gt, ok := te.(*parser.GenericType); ok && gt.Name.Lexeme == "result" && len(gt.Params) >= 1 {
		inner, err := e.goType(gt.Params[0])
		if err != nil {
			return "", false, err
		}
		if inner == "" {
			return "error", true, nil
		}
		return fmt.Sprintf("(%s, error)", inner), true, nil
	}
	gs, err := e.goType(te)
	return gs, false, err
}

// ── statements ────────────────────────────────────────────────────────────────

func (e *emitter) emitStmt(s parser.Stmt) error {
	pad := e.pad()
	switch v := s.(type) {
	case *parser.LetStmt:
		return e.emitLet(v, pad)
	case *parser.ReturnStmt:
		return e.emitReturn(v, pad)
	case *parser.ExprStmt:
		// Match expressions used as statements emit direct if/else, not a closure.
		if mx, ok := v.X.(*parser.MatchExpr); ok {
			return e.emitMatchStmt(mx, pad)
		}
		es, err := e.emitExprStr(v.X)
		if err != nil {
			return err
		}
		e.writef("%s%s\n", pad, es)
	case *parser.IfStmt:
		return e.emitIf(v, pad)
	case *parser.BlockStmt:
		for _, st := range v.Stmts {
			if err := e.emitStmt(st); err != nil {
				return err
			}
		}
	case *parser.LoopStmt:
		e.writef("%sfor {\n", pad)
		e.indent++
		for _, st := range v.Body.Stmts {
			if err := e.emitStmt(st); err != nil {
				return err
			}
		}
		e.indent--
		e.writef("%s}\n", pad)
	case *parser.WhileStmt:
		cond, err := e.emitExprStr(v.Cond)
		if err != nil {
			return err
		}
		e.writef("%sfor %s {\n", pad, cond)
		e.indent++
		for _, st := range v.Body.Stmts {
			if err := e.emitStmt(st); err != nil {
				return err
			}
		}
		e.indent--
		e.writef("%s}\n", pad)
	case *parser.ForStmt:
		iter, err := e.emitExprStr(v.Collection)
		if err != nil {
			return err
		}
		if v.Var2 != nil {
			e.writef("%sfor %s, %s := range %s {\n", pad, v.Var.Lexeme, v.Var2.Lexeme, iter)
		} else {
			e.writef("%sfor _, %s := range %s {\n", pad, v.Var.Lexeme, iter)
		}
		e.indent++
		for _, st := range v.Body.Stmts {
			if err := e.emitStmt(st); err != nil {
				return err
			}
		}
		e.indent--
		e.writef("%s}\n", pad)
	case *parser.BreakStmt:
		e.writef("%sbreak\n", pad)
	case *parser.ContinueStmt:
		e.writef("%scontinue\n", pad)
	case *parser.AssignStmt:
		val, err := e.emitExprStr(v.Value)
		if err != nil {
			return err
		}
		e.writef("%s%s = %s\n", pad, v.Name.Lexeme, val)
	case *parser.FieldAssignStmt:
		obj, err := e.emitExprStr(v.Target.Receiver)
		if err != nil {
			return err
		}
		val, err := e.emitExprStr(v.Value)
		if err != nil {
			return err
		}
		e.writef("%s%s.%s = %s\n", pad, obj, capitalize(v.Target.Field.Lexeme), val)
	case *parser.IndexAssignStmt:
		obj, err := e.emitExprStr(v.Target.Collection)
		if err != nil {
			return err
		}
		idx, err := e.emitExprStr(v.Target.Index)
		if err != nil {
			return err
		}
		val, err := e.emitExprStr(v.Value)
		if err != nil {
			return err
		}
		e.writef("%s%s[%s] = %s\n", pad, obj, idx, val)
	case *parser.AssertStmt:
		cond, err := e.emitExprStr(v.Expr)
		if err != nil {
			return err
		}
		e.writef("%sif !(%s) { panic(\"assert failed\") }\n", pad, cond)
	case *parser.TupleDestructureStmt:
		val, err := e.emitExprStr(v.Value)
		if err != nil {
			return err
		}
		names := make([]string, len(v.Names))
		for i, n := range v.Names {
			names[i] = n.Lexeme
		}
		e.writef("%s%s := %s\n", pad, strings.Join(names, ", "), val)
	default:
		e.writef("%s// TODO: %T\n", pad, s)
	}
	return nil
}

func (e *emitter) emitLet(v *parser.LetStmt, pad string) error {
	if v.Value == nil {
		if v.TypeAnn != nil {
			gt, err := e.goType(v.TypeAnn)
			if err != nil {
				return err
			}
			e.writef("%svar %s %s\n", pad, v.Name.Lexeme, gt)
		}
		return nil
	}
	if must, ok := v.Value.(*parser.MustExpr); ok {
		return e.emitLetMust(v.Name.Lexeme, must, pad)
	}
	val, err := e.emitExprStr(v.Value)
	if err != nil {
		return err
	}
	if v.TypeAnn != nil {
		gt, err := e.goType(v.TypeAnn)
		if err != nil {
			return err
		}
		e.writef("%svar %s %s = %s\n", pad, v.Name.Lexeme, gt, val)
	} else {
		e.writef("%s%s := %s\n", pad, v.Name.Lexeme, val)
	}
	return nil
}

func (e *emitter) emitLetMust(name string, must *parser.MustExpr, pad string) error {
	subTy := e.res.ExprTypes[must.X]
	typeStr := "result"
	if subTy != nil {
		typeStr = subTy.String()
	}
	e.audit.AddEntry(emit_c.AuditEntry{
		Category:    "must",
		FnName:      e.curFnName,
		Detail:      "must{} on " + typeStr,
		CEquiv:      "if err != nil { ... }",
		Explanation: "Candor enforces that discarding this " + typeStr + " is a compile error. In Go the caller can use _ to silently discard errors.",
	})

	subStr, err := e.emitExprStr(must.X)
	if err != nil {
		return err
	}

	okTmp := e.freshTmp()
	errTmp := e.freshTmp()
	e.writef("%s%s, %s := %s\n", pad, okTmp, errTmp, subStr)

	// Find err arm and ok arm.
	errBody := ""
	for _, arm := range must.Arms {
		if isErrOrNonePattern(arm.Pattern) {
			body, err2 := e.emitExprStr(arm.Body)
			if err2 == nil {
				errBody = body
			}
		}
	}

	if errBody != "" {
		e.writef("%sif %s != nil {\n", pad, errTmp)
		// Bind the err variable if the pattern names it.
		for _, arm := range must.Arms {
			if isErrOrNonePattern(arm.Pattern) {
				if call, ok := arm.Pattern.(*parser.CallExpr); ok {
					if len(call.Args) == 1 {
						if id, ok2 := call.Args[0].(*parser.IdentExpr); ok2 {
							// Bind as string (Candor err<str>) using .Error() on Go error.
							e.writef("%s\t%s := %s.Error()\n", pad, id.Tok.Lexeme, errTmp)
						}
					}
				}
				body, _ := e.emitExprStr(arm.Body)
				e.writef("%s\t%s\n", pad, body)
				break
			}
		}
		e.writef("%s}\n", pad)
	} else {
		e.writef("%s_ = %s\n", pad, errTmp)
	}

	e.writef("%s%s := %s\n", pad, name, okTmp)
	return nil
}

func isErrOrNonePattern(p parser.Expr) bool {
	if id, ok := p.(*parser.IdentExpr); ok {
		return id.Tok.Lexeme == "none"
	}
	if call, ok := p.(*parser.CallExpr); ok {
		if id, ok2 := call.Fn.(*parser.IdentExpr); ok2 {
			return id.Tok.Lexeme == "err"
		}
	}
	return false
}

func (e *emitter) emitReturn(v *parser.ReturnStmt, pad string) error {
	if v.Value == nil {
		e.writef("%sreturn\n", pad)
		return nil
	}
	if id, ok := v.Value.(*parser.IdentExpr); ok && id.Tok.Lexeme == "unit" {
		if e.curRetIsErr {
			e.writef("%sreturn %s, nil\n", pad, e.curRetZeroVal)
		} else {
			e.writef("%sreturn\n", pad)
		}
		return nil
	}
	// result-returning function: ok(v) → return v, nil; err(e) → return zero, errors.New(e)
	if e.curRetIsErr {
		if inner, yes := isOkCall(v.Value); yes {
			innerStr, err := e.emitExprStr(inner)
			if err != nil {
				return err
			}
			e.writef("%sreturn %s, nil\n", pad, innerStr)
			return nil
		}
		if inner, yes := isErrCall(v.Value); yes {
			innerStr, err := e.emitExprStr(inner)
			if err != nil {
				return err
			}
			e.writef("%sreturn %s, fmt.Errorf(\"%%s\", %s)\n", pad, e.curRetZeroVal, innerStr)
			return nil
		}
	}
	val, err := e.emitExprStr(v.Value)
	if err != nil {
		return err
	}
	e.writef("%sreturn %s\n", pad, val)
	return nil
}

func (e *emitter) emitIf(v *parser.IfStmt, pad string) error {
	cond, err := e.emitExprStr(v.Cond)
	if err != nil {
		return err
	}
	e.writef("%sif %s {\n", pad, cond)
	e.indent++
	for _, st := range v.Then.Stmts {
		if err := e.emitStmt(st); err != nil {
			return err
		}
	}
	e.indent--
	if v.Else != nil {
		if eif, ok := v.Else.(*parser.IfStmt); ok {
			e.writef("%s} else if ", pad)
			// emit condition inline without the "if" prefix
			cond2, err := e.emitExprStr(eif.Cond)
			if err != nil {
				return err
			}
			e.writef("%s {\n", cond2)
			e.indent++
			for _, st := range eif.Then.Stmts {
				if err := e.emitStmt(st); err != nil {
					return err
				}
			}
			e.indent--
			// recurse for further else-if chain (simplified: only one level for now)
			if eif.Else != nil {
				e.writef("%s} else {\n", pad)
				e.indent++
				if err := e.emitStmt(eif.Else); err != nil {
					return err
				}
				e.indent--
			}
			e.writef("%s}\n", pad)
		} else {
			e.writef("%s} else {\n", pad)
			e.indent++
			if err := e.emitStmt(v.Else); err != nil {
				return err
			}
			e.indent--
			e.writef("%s}\n", pad)
		}
	} else {
		e.writef("%s}\n", pad)
	}
	return nil
}

// ── expressions ───────────────────────────────────────────────────────────────

func (e *emitter) emitExprStr(ex parser.Expr) (string, error) {
	if ex == nil {
		return "", nil
	}
	switch v := ex.(type) {
	case *parser.IntLitExpr:
		return v.Tok.Lexeme, nil
	case *parser.FloatLitExpr:
		return v.Tok.Lexeme, nil
	case *parser.StringLitExpr:
		return v.Tok.Lexeme, nil
	case *parser.BoolLitExpr:
		return v.Tok.Lexeme, nil
	case *parser.IdentExpr:
		switch v.Tok.Lexeme {
		case "unit":
			return "", nil
		case "none":
			return "nil", nil
		}
		return v.Tok.Lexeme, nil
	case *parser.BinaryExpr:
		left, err := e.emitExprStr(v.Left)
		if err != nil {
			return "", err
		}
		right, err := e.emitExprStr(v.Right)
		if err != nil {
			return "", err
		}
		op := v.Op.Lexeme
		switch op {
		case "and":
			op = "&&"
		case "or":
			op = "||"
		}
		return fmt.Sprintf("(%s %s %s)", left, op, right), nil
	case *parser.UnaryExpr:
		operand, err := e.emitExprStr(v.Operand)
		if err != nil {
			return "", err
		}
		op := v.Op.Lexeme
		if op == "not" {
			op = "!"
		}
		return fmt.Sprintf("(%s%s)", op, operand), nil
	case *parser.CastExpr:
		inner, err := e.emitExprStr(v.X)
		if err != nil {
			return "", err
		}
		gt, err := e.goType(v.Target)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s(%s)", gt, inner), nil
	case *parser.CallExpr:
		return e.emitCall(v)
	case *parser.FieldExpr:
		obj, err := e.emitExprStr(v.Receiver)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s.%s", obj, capitalize(v.Field.Lexeme)), nil
	case *parser.IndexExpr:
		obj, err := e.emitExprStr(v.Collection)
		if err != nil {
			return "", err
		}
		idx, err := e.emitExprStr(v.Index)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s[%s]", obj, idx), nil
	case *parser.StructLitExpr:
		return e.emitStructLit(v)
	case *parser.BlockExpr:
		// Block expressions: emit last stmt as value, preceding as side effects.
		// For simple single-expr blocks, just emit that.
		if len(v.Stmts) == 1 {
			if es, ok := v.Stmts[0].(*parser.ExprStmt); ok {
				return e.emitExprStr(es.X)
			}
		}
		return "/* block */", nil
	case *parser.MustExpr:
		return e.emitMustExprInline(v)
	case *parser.MatchExpr:
		return e.emitMatchExpr(v)
	case *parser.ReturnExpr:
		if v.Value == nil {
			return "return", nil
		}
		if id, ok2 := v.Value.(*parser.IdentExpr); ok2 && id.Tok.Lexeme == "unit" {
			if e.curRetIsErr {
				return fmt.Sprintf("return %s, nil", e.curRetZeroVal), nil
			}
			return "return", nil
		}
		if e.curRetIsErr {
			if inner, yes := isOkCall(v.Value); yes {
				innerStr, err := e.emitExprStr(inner)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("return %s, nil", innerStr), nil
			}
			if inner, yes := isErrCall(v.Value); yes {
				innerStr, err := e.emitExprStr(inner)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("return %s, fmt.Errorf(\"%%s\", %s)", e.curRetZeroVal, innerStr), nil
			}
		}
		val, err := e.emitExprStr(v.Value)
		if err != nil {
			return "", err
		}
		if val == "" {
			return "return", nil
		}
		return "return " + val, nil
	case *parser.BreakExpr:
		return "break", nil
	case *parser.VecLitExpr:
		if len(v.Elems) == 0 {
			return "nil", nil
		}
		var elems []string
		for _, el := range v.Elems {
			es, err := e.emitExprStr(el)
			if err != nil {
				return "", err
			}
			elems = append(elems, es)
		}
		return "{" + strings.Join(elems, ", ") + "}", nil
	case *parser.PathExpr:
		// Enum variant construction: Enum::Variant → Enum_Variant{}
		return v.Head.Lexeme + "_" + v.Tail.Lexeme + "{}", nil
	default:
		return fmt.Sprintf("/* unsupported: %T */", ex), nil
	}
}

func (e *emitter) emitCall(v *parser.CallExpr) (string, error) {
	if id, ok := v.Fn.(*parser.IdentExpr); ok {
		if s, handled, err := e.emitBuiltin(id.Tok.Lexeme, v.Args); handled {
			return s, err
		}
	}
	// Path call: Enum::Variant(args) — enum constructor
	if pe, ok := v.Fn.(*parser.PathExpr); ok {
		var argStrs []string
		for _, a := range v.Args {
			as, err := e.emitExprStr(a)
			if err != nil {
				return "", err
			}
			argStrs = append(argStrs, as)
		}
		if len(argStrs) == 0 {
			return pe.Head.Lexeme + "_" + pe.Tail.Lexeme + "{}", nil
		}
		fields := make([]string, len(argStrs))
		for i, a := range argStrs {
			fields[i] = fmt.Sprintf("V%d: %s", i, a)
		}
		return fmt.Sprintf("%s_%s{%s}", pe.Head.Lexeme, pe.Tail.Lexeme, strings.Join(fields, ", ")), nil
	}
	callee, err := e.emitExprStr(v.Fn)
	if err != nil {
		return "", err
	}
	var argStrs []string
	for _, a := range v.Args {
		as, err := e.emitExprStr(a)
		if err != nil {
			return "", err
		}
		argStrs = append(argStrs, as)
	}
	return fmt.Sprintf("%s(%s)", callee, strings.Join(argStrs, ", ")), nil
}

// emitBuiltin maps Candor builtins to Go idioms.
func (e *emitter) emitBuiltin(name string, args []parser.Expr) (string, bool, error) {
	arg := func(i int) (string, error) {
		if i >= len(args) {
			return "", fmt.Errorf("not enough args for %s", name)
		}
		return e.emitExprStr(args[i])
	}

	switch name {
	case "print":
		a, err := arg(0)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("fmt.Println(%s)", a), true, nil

	case "print_err":
		a, err := arg(0)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("fmt.Fprintln(os.Stderr, %s)", a), true, nil

	case "_cnd_int_to_str", "int_to_str":
		a, err := arg(0)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("strconv.FormatInt(int64(%s), 10)", a), true, nil

	case "str_concat":
		a0, err := arg(0)
		if err != nil {
			return "", true, err
		}
		a1, err := arg(1)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("(%s + %s)", a0, a1), true, nil

	case "str_len":
		a, err := arg(0)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("int64(len(%s))", a), true, nil

	case "str_eq":
		a0, err := arg(0)
		if err != nil {
			return "", true, err
		}
		a1, err := arg(1)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("(%s == %s)", a0, a1), true, nil

	case "str_starts_with":
		a0, err := arg(0)
		if err != nil {
			return "", true, err
		}
		a1, err := arg(1)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("strings.HasPrefix(%s, %s)", a0, a1), true, nil

	case "str_contains":
		a0, err := arg(0)
		if err != nil {
			return "", true, err
		}
		a1, err := arg(1)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("strings.Contains(%s, %s)", a0, a1), true, nil

	case "vec_new":
		return "nil", true, nil

	case "vec_len":
		a, err := arg(0)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("int64(len(%s))", a), true, nil

	case "vec_push":
		a0, err := arg(0)
		if err != nil {
			return "", true, err
		}
		a1, err := arg(1)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("%s = append(%s, %s)", a0, a0, a1), true, nil

	case "vec_pop":
		a, err := arg(0)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("%s = %s[:len(%s)-1]", a, a, a), true, nil

	case "vec_drop", "map_drop":
		return "/* GC handles this */", true, nil

	case "map_new":
		return "nil", true, nil

	case "map_insert":
		a0, err := arg(0)
		if err != nil {
			return "", true, err
		}
		a1, err := arg(1)
		if err != nil {
			return "", true, err
		}
		a2, err := arg(2)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("%s[%s] = %s", a0, a1, a2), true, nil

	case "map_get":
		a0, err := arg(0)
		if err != nil {
			return "", true, err
		}
		a1, err := arg(1)
		if err != nil {
			return "", true, err
		}
		// Returns option<V> in Candor. In Go: look up and wrap in pointer.
		tmp := e.freshTmp()
		ok := e.freshTmp()
		return fmt.Sprintf("func() interface{} { %s, %s := %s[%s]; if %s { return &%s }; return nil }()", tmp, ok, a0, a1, ok, tmp), true, nil

	case "map_contains":
		a0, err := arg(0)
		if err != nil {
			return "", true, err
		}
		a1, err := arg(1)
		if err != nil {
			return "", true, err
		}
		ok := e.freshTmp()
		return fmt.Sprintf("func() bool { _, %s := %s[%s]; return %s }()", ok, a0, a1, ok), true, nil

	case "set_new":
		return "nil", true, nil

	case "set_add":
		a0, err := arg(0)
		if err != nil {
			return "", true, err
		}
		a1, err := arg(1)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("%s[%s] = struct{}{}", a0, a1), true, nil

	case "set_contains":
		a0, err := arg(0)
		if err != nil {
			return "", true, err
		}
		a1, err := arg(1)
		if err != nil {
			return "", true, err
		}
		ok := e.freshTmp()
		return fmt.Sprintf("func() bool { _, %s := %s[%s]; return %s }()", ok, a0, a1, ok), true, nil

	case "set_remove":
		a0, err := arg(0)
		if err != nil {
			return "", true, err
		}
		a1, err := arg(1)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("delete(%s, %s)", a0, a1), true, nil

	case "set_len":
		a, err := arg(0)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("int64(len(%s))", a), true, nil

	case "box_new":
		a, err := arg(0)
		if err != nil {
			return "", true, err
		}
		tmp := e.freshTmp()
		return fmt.Sprintf("func() *interface{} { %s := interface{}(%s); return &%s }()", tmp, a, tmp), true, nil

	case "box_deref":
		a, err := arg(0)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("(*%s)", a), true, nil

	case "some":
		a, err := arg(0)
		if err != nil {
			return "", true, err
		}
		tmp := e.freshTmp()
		return fmt.Sprintf("func() interface{} { %s := %s; return %s }()", tmp, a, tmp), true, nil

	case "os_exit":
		a, err := arg(0)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("os.Exit(int(%s))", a), true, nil
	}

	return "", false, nil
}

func (e *emitter) emitStructLit(v *parser.StructLitExpr) (string, error) {
	var fields []string
	for _, fi := range v.Fields {
		val, err := e.emitExprStr(fi.Value)
		if err != nil {
			return "", err
		}
		fields = append(fields, capitalize(fi.Name.Lexeme)+": "+val)
	}
	return fmt.Sprintf("%s{%s}", v.TypeName.Lexeme, strings.Join(fields, ", ")), nil
}

func (e *emitter) emitMustExprInline(v *parser.MustExpr) (string, error) {
	subTy := e.res.ExprTypes[v.X]
	typeStr := "result"
	if subTy != nil {
		typeStr = subTy.String()
	}
	e.audit.AddEntry(emit_c.AuditEntry{
		Category:    "must",
		FnName:      e.curFnName,
		Detail:      "must{} on " + typeStr,
		CEquiv:      "if err != nil { ... }",
		Explanation: "Candor enforces that discarding this " + typeStr + " is a compile error. In Go the caller can use _ to silently discard errors.",
	})
	sub, err := e.emitExprStr(v.X)
	if err != nil {
		return "", err
	}
	return sub + " /* must{} */", nil
}

func (e *emitter) emitMatchExpr(v *parser.MatchExpr) (string, error) {
	subTy := e.res.ExprTypes[v.X]
	// result<T,E> match → if/else on error
	if subTy != nil && isResultType(subTy) {
		return e.emitResultMatch(v)
	}
	sub, err := e.emitExprStr(v.X)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("func() interface{} {\n")
	sb.WriteString(fmt.Sprintf("\t\t_m := %s\n", sub))
	sb.WriteString("\t\tswitch _m {\n")
	for _, arm := range v.Arms {
		pat, err := e.emitMatchPat(arm.Pattern)
		if err != nil {
			return "", err
		}
		body, err := e.emitExprStr(arm.Body)
		if err != nil {
			return "", err
		}
		if pat == "_" {
			sb.WriteString(fmt.Sprintf("\t\tdefault:\n\t\t\treturn %s\n", body))
		} else {
			sb.WriteString(fmt.Sprintf("\t\tcase %s:\n\t\t\treturn %s\n", pat, body))
		}
	}
	sb.WriteString("\t\t}\n\t\treturn nil\n\t}()")
	return sb.String(), nil
}

// emitMatchStmt emits a match expression used as a statement (no closure wrapper).
func (e *emitter) emitMatchStmt(v *parser.MatchExpr, pad string) error {
	subTy := e.res.ExprTypes[v.X]
	if subTy == nil || !isResultType(subTy) {
		// Fallback: generic switch statement.
		sub, err := e.emitExprStr(v.X)
		if err != nil {
			return err
		}
		e.writef("%s_m := %s\n", pad, sub)
		e.writef("%sswitch _m {\n", pad)
		for _, arm := range v.Arms {
			pat, err := e.emitMatchPat(arm.Pattern)
			if err != nil {
				return err
			}
			body, err := e.emitExprStr(arm.Body)
			if err != nil {
				return err
			}
			if pat == "_" {
				e.writef("%sdefault:\n%s\t%s\n", pad, pad, body)
			} else {
				e.writef("%scase %s:\n%s\t%s\n", pad, pat, pad, body)
			}
		}
		e.writef("%s}\n", pad)
		return nil
	}
	// result<T,E> — emit as if/else on error.
	sub, err := e.emitExprStr(v.X)
	if err != nil {
		return err
	}
	valTmp := e.freshTmp()
	errTmp := e.freshTmp()
	e.writef("%s%s, %s := %s\n", pad, valTmp, errTmp, sub)

	var okBinding, errBinding string
	var okBody, errBody parser.Expr
	for _, arm := range v.Arms {
		c, ok := arm.Pattern.(*parser.CallExpr)
		if !ok {
			continue
		}
		fn, ok := c.Fn.(*parser.IdentExpr)
		if !ok {
			continue
		}
		switch fn.Tok.Lexeme {
		case "ok":
			okBinding = bindingOf(arm.Pattern)
			okBody = arm.Body
		case "err":
			errBinding = bindingOf(arm.Pattern)
			errBody = arm.Body
		}
	}

	e.writef("%sif %s != nil {\n", pad, errTmp)
	if errBody != nil {
		if errBinding != "" {
			e.writef("%s\t%s := %s.Error()\n", pad, errBinding, errTmp)
		}
		bodyStr, err := e.emitExprStr(errBody)
		if err != nil {
			return err
		}
		e.writef("%s\t%s\n", pad, bodyStr)
	}
	e.writef("%s} else {\n", pad)
	if okBody != nil {
		if okBinding != "" {
			e.writef("%s\t%s := %s\n", pad, okBinding, valTmp)
		}
		bodyStr, err := e.emitExprStr(okBody)
		if err != nil {
			return err
		}
		e.writef("%s\t%s\n", pad, bodyStr)
	}
	e.writef("%s}\n", pad)
	return nil
}

// emitResultMatch emits a result<T,E> match as Go if/else on error.
func (e *emitter) emitResultMatch(v *parser.MatchExpr) (string, error) {
	sub, err := e.emitExprStr(v.X)
	if err != nil {
		return "", err
	}
	valTmp := e.freshTmp()
	errTmp := e.freshTmp()

	var okBinding, errBinding string
	var okBody, errBody parser.Expr
	for _, arm := range v.Arms {
		if id, ok := arm.Pattern.(*parser.IdentExpr); ok && id.Tok.Lexeme == "none" {
			errBody = arm.Body
			continue
		}
		c, ok := arm.Pattern.(*parser.CallExpr)
		if !ok {
			continue
		}
		fn, ok := c.Fn.(*parser.IdentExpr)
		if !ok {
			continue
		}
		switch fn.Tok.Lexeme {
		case "ok":
			okBinding = bindingOf(arm.Pattern)
			okBody = arm.Body
		case "err":
			errBinding = bindingOf(arm.Pattern)
			errBody = arm.Body
		}
	}

	var sb strings.Builder
	sb.WriteString("func() interface{} {\n")
	sb.WriteString(fmt.Sprintf("\t\t%s, %s := %s\n", valTmp, errTmp, sub))
	sb.WriteString(fmt.Sprintf("\t\tif %s != nil {\n", errTmp))
	if errBody != nil {
		if errBinding != "" {
			sb.WriteString(fmt.Sprintf("\t\t\t%s := %s.Error()\n", errBinding, errTmp))
		}
		bodyStr, err := e.emitExprStr(errBody)
		if err != nil {
			return "", err
		}
		sb.WriteString(fmt.Sprintf("\t\t\treturn %s\n", bodyStr))
	}
	sb.WriteString("\t\t} else {\n")
	if okBody != nil {
		if okBinding != "" {
			sb.WriteString(fmt.Sprintf("\t\t\t%s := %s\n", okBinding, valTmp))
		}
		bodyStr, err := e.emitExprStr(okBody)
		if err != nil {
			return "", err
		}
		sb.WriteString(fmt.Sprintf("\t\t\treturn %s\n", bodyStr))
	}
	sb.WriteString("\t\t}\n\t\treturn nil\n\t}()")
	return sb.String(), nil
}

func (e *emitter) emitMatchPat(p parser.Expr) (string, error) {
	switch v := p.(type) {
	case *parser.IdentExpr:
		if v.Tok.Lexeme == "_" {
			return "_", nil
		}
		return v.Tok.Lexeme, nil
	case *parser.IntLitExpr:
		return v.Tok.Lexeme, nil
	case *parser.StringLitExpr:
		return v.Tok.Lexeme, nil
	case *parser.BoolLitExpr:
		return v.Tok.Lexeme, nil
	case *parser.PathExpr:
		// Enum variant without payload: Module::Variant
		return v.Head.Lexeme + "_" + v.Tail.Lexeme + "{}", nil
	case *parser.CallExpr:
		// Enum variant with payload: Variant(binding) — emit the variant name only;
		// Go switch-case on structs requires a type assertion, which we can't do
		// cleanly inline. Emit a comment so the file at least parses.
		if id, ok := v.Fn.(*parser.IdentExpr); ok {
			return fmt.Sprintf("_ /* %s(...) */", id.Tok.Lexeme), nil
		}
		return "_ /* unknown call pattern */", nil
	default:
		return fmt.Sprintf("_ /* unhandled pattern: %T */", p), nil
	}
}

// ── types ─────────────────────────────────────────────────────────────────────

func (e *emitter) goType(te parser.TypeExpr) (string, error) {
	if te == nil {
		return "", nil
	}
	switch v := te.(type) {
	case *parser.NamedType:
		return goNamedType(v.Name.Lexeme), nil
	case *parser.GenericType:
		return e.goGenericType(v)
	default:
		return fmt.Sprintf("interface{} /* %T */", te), nil
	}
}

func goNamedType(name string) string {
	switch name {
	case "i64":
		return "int64"
	case "i32":
		return "int32"
	case "i16":
		return "int16"
	case "i8":
		return "int8"
	case "u64":
		return "uint64"
	case "u32":
		return "uint32"
	case "u16":
		return "uint16"
	case "u8":
		return "uint8"
	case "f64":
		return "float64"
	case "f32":
		return "float32"
	case "bool":
		return "bool"
	case "str":
		return "string"
	case "unit":
		return ""
	default:
		return name
	}
}

func (e *emitter) goGenericType(v *parser.GenericType) (string, error) {
	switch v.Name.Lexeme {
	case "vec":
		if len(v.Params) == 0 {
			return "[]interface{}", nil
		}
		inner, err := e.goType(v.Params[0])
		if err != nil {
			return "", err
		}
		return "[]" + inner, nil
	case "option":
		if len(v.Params) == 0 {
			return "interface{}", nil
		}
		inner, err := e.goType(v.Params[0])
		if err != nil {
			return "", err
		}
		return "*" + inner, nil
	case "result":
		if len(v.Params) == 0 {
			return "error", nil
		}
		inner, err := e.goType(v.Params[0])
		if err != nil {
			return "", err
		}
		if inner == "" {
			return "error", nil
		}
		return inner, nil
	case "map":
		if len(v.Params) < 2 {
			return "map[interface{}]interface{}", nil
		}
		k, err := e.goType(v.Params[0])
		if err != nil {
			return "", err
		}
		val, err := e.goType(v.Params[1])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("map[%s]%s", k, val), nil
	case "set":
		if len(v.Params) == 0 {
			return "map[interface{}]struct{}", nil
		}
		inner, err := e.goType(v.Params[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("map[%s]struct{}", inner), nil
	case "box", "ref", "refmut", "ptr":
		if len(v.Params) == 0 {
			return "*interface{}", nil
		}
		inner, err := e.goType(v.Params[0])
		if err != nil {
			return "", err
		}
		return "*" + inner, nil
	case "secret":
		// secret<T> → T  (information-flow enforcement dropped; noted in audit report)
		if len(v.Params) == 0 {
			return "interface{}", nil
		}
		inner, err := e.goType(v.Params[0])
		if err != nil {
			return "", err
		}
		return inner, nil
	}
	return "interface{} /* " + v.Name.Lexeme + " */", nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// goZeroVal returns the Go zero-value literal for the T in result<T,E>.
// Used when emitting `return zeroVal, errors.New(...)`.
func goZeroVal(te parser.TypeExpr) string {
	gt, ok := te.(*parser.GenericType)
	if !ok || gt.Name.Lexeme != "result" || len(gt.Params) == 0 {
		return ""
	}
	inner := gt.Params[0]
	switch v := inner.(type) {
	case *parser.NamedType:
		switch v.Name.Lexeme {
		case "unit":
			return ""
		case "str":
			return `""`
		case "bool":
			return "false"
		case "i64", "i32", "i16", "i8", "u64", "u32", "u16", "u8", "f64", "f32":
			return "0"
		default:
			return v.Name.Lexeme + "{}"
		}
	case *parser.GenericType:
		switch v.Name.Lexeme {
		case "vec":
			return "nil"
		case "option":
			return "nil"
		case "map":
			return "nil"
		default:
			return "nil"
		}
	}
	return ""
}

// isOkCall returns (inner-arg, true) if ex is ok(x).
func isOkCall(ex parser.Expr) (parser.Expr, bool) {
	c, ok := ex.(*parser.CallExpr)
	if !ok {
		return nil, false
	}
	id, ok := c.Fn.(*parser.IdentExpr)
	if !ok || id.Tok.Lexeme != "ok" || len(c.Args) != 1 {
		return nil, false
	}
	return c.Args[0], true
}

// isErrCall returns (inner-arg, true) if ex is err(x).
func isErrCall(ex parser.Expr) (parser.Expr, bool) {
	c, ok := ex.(*parser.CallExpr)
	if !ok {
		return nil, false
	}
	id, ok := c.Fn.(*parser.IdentExpr)
	if !ok || id.Tok.Lexeme != "err" || len(c.Args) != 1 {
		return nil, false
	}
	return c.Args[0], true
}

// bindingOf returns the name bound by ok(name) or err(name) patterns.
func bindingOf(ex parser.Expr) string {
	c, ok := ex.(*parser.CallExpr)
	if !ok {
		return ""
	}
	if len(c.Args) != 1 {
		return ""
	}
	id, ok := c.Args[0].(*parser.IdentExpr)
	if !ok {
		return ""
	}
	return id.Tok.Lexeme
}

// isResultType returns true if ty is result<T,E>.
func isResultType(ty typeck.Type) bool {
	g, ok := ty.(*typeck.GenType)
	return ok && g.Con == "result"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func escStr(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// exprText reconstructs a readable source snippet from an expression (best-effort).
func exprText(ex parser.Expr) string {
	switch v := ex.(type) {
	case *parser.IdentExpr:
		return v.Tok.Lexeme
	case *parser.BinaryExpr:
		return exprText(v.Left) + " " + v.Op.Lexeme + " " + exprText(v.Right)
	case *parser.IntLitExpr:
		return v.Tok.Lexeme
	case *parser.BoolLitExpr:
		return v.Tok.Lexeme
	case *parser.UnaryExpr:
		return v.Op.Lexeme + " " + exprText(v.Operand)
	case *parser.CallExpr:
		if id, ok := v.Fn.(*parser.IdentExpr); ok {
			return id.Tok.Lexeme + "(...)"
		}
	}
	return "..."
}
