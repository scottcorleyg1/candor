// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0

// Package emit_llvm emits textual LLVM IR (.ll) from a type-checked Candor AST.
//
// Type mapping:
//
//	unit              → void  (local vars use i8 placeholder)
//	bool              → i1
//	i8/u8             → i8
//	i16/u16           → i16
//	i32/u32           → i32
//	i64/u64           → i64
//	f32               → float
//	f64               → double
//	str               → ptr
//	ref<T>/refmut<T>  → ptr
//	struct S          → %S  (named aggregate type)
//	enum E            → %E  (tagged union: { i32, [maxPayload x i8] })
//	fn main()->unit   → emits @_cnd_main (void) + @main wrapper returning i32 0
package emit_llvm

import (
	"fmt"
	"strings"

	"github.com/scottcorleyg1/candor/compiler/parser"
	"github.com/scottcorleyg1/candor/compiler/typeck"
)

// EmitLLVM produces textual LLVM IR from a type-checked Candor AST.
// target is an LLVM target triple (e.g. "aarch64-unknown-linux-gnu"); empty means host default.
func EmitLLVM(file *parser.File, res *typeck.Result, target string) (string, error) {
	em := &llEmitter{
		res:           res,
		target:        target,
		strPool:       make(map[string]string),
		structFields:  make(map[string][]string),
		enumPayload:   make(map[string]int),
		mapEntryTypes: make(map[string]bool),
	}
	em.buildLayouts(file)
	em.collectStrings(file)
	if err := em.emitFile(file); err != nil {
		return "", err
	}
	return em.hdr.String() + em.body.String(), nil
}

// ── emitter state ─────────────────────────────────────────────────────────────

type llEmitter struct {
	hdr  strings.Builder // type decls, string globals, extern decls
	body strings.Builder // function bodies
	res  *typeck.Result
	target string // LLVM target triple; empty = host default

	strPool  map[string]string // content → "@.str.N"
	strCount int

	structFields  map[string][]string // struct name → ordered field names
	enumPayload   map[string]int      // enum name → max payload bytes
	mapEntryTypes map[string]bool     // declared %_cnd_map_entry_K_V type names

	// per-function state (reset before each function)
	tmpCount   int
	blkCount   int
	locals     map[string]string // var name → "%name.addr" alloca ptr
	localTypes map[string]typeck.Type
	breakLabel string
	contLabel  string
	retType    typeck.Type
	isMain     bool
}

// buildLayouts populates structFields and enumPayload from the AST.
func (em *llEmitter) buildLayouts(file *parser.File) {
	for _, d := range file.Decls {
		switch dd := d.(type) {
		case *parser.StructDecl:
			names := make([]string, len(dd.Fields))
			for i, f := range dd.Fields {
				names[i] = f.Name.Lexeme
			}
			em.structFields[dd.Name.Lexeme] = names
		case *parser.EnumDecl:
			et := em.res.Enums[dd.Name.Lexeme]
			if et == nil {
				continue
			}
			maxBytes := 0
			for _, v := range et.Variants {
				sz := 0
				for _, ft := range v.Fields {
					sz += em.sizeBytes(ft)
				}
				if sz > maxBytes {
					maxBytes = sz
				}
			}
			em.enumPayload[dd.Name.Lexeme] = maxBytes
		}
	}
}

func (em *llEmitter) resetFn() {
	em.tmpCount = 0
	em.blkCount = 0
	em.locals = make(map[string]string)
	em.localTypes = make(map[string]typeck.Type)
	em.breakLabel = ""
	em.contLabel = ""
	em.retType = nil
	em.isMain = false
}

// ── output helpers ─────────────────────────────────────────────────────────────

func (em *llEmitter) h(s string)              { em.hdr.WriteString(s); em.hdr.WriteByte('\n') }
func (em *llEmitter) hf(f string, a ...any)   { fmt.Fprintf(&em.hdr, f, a...); em.hdr.WriteByte('\n') }
func (em *llEmitter) w(s string)              { em.body.WriteString(s); em.body.WriteByte('\n') }
func (em *llEmitter) wf(f string, a ...any)   { fmt.Fprintf(&em.body, f, a...); em.body.WriteByte('\n') }
func (em *llEmitter) wi(s string)             { em.body.WriteString("  "); em.body.WriteString(s); em.body.WriteByte('\n') }
func (em *llEmitter) wif(f string, a ...any)  { em.body.WriteString("  "); fmt.Fprintf(&em.body, f, a...); em.body.WriteByte('\n') }
func (em *llEmitter) lbl(name string)         { em.wf("%s:", name) }

// ── fresh name generators ──────────────────────────────────────────────────────

func (em *llEmitter) fresh() string {
	em.tmpCount++
	return fmt.Sprintf("%%t%d", em.tmpCount)
}

func (em *llEmitter) freshBlk(prefix string) string {
	em.blkCount++
	return fmt.Sprintf("%s.%d", prefix, em.blkCount)
}

// ── type mapping ───────────────────────────────────────────────────────────────

func (em *llEmitter) llType(t typeck.Type) string {
	if t == nil {
		return "void"
	}
	switch t {
	case typeck.TUnit, typeck.TNever:
		return "void"
	case typeck.TBool:
		return "i1"
	case typeck.TStr:
		return "ptr"
	case typeck.TI8, typeck.TU8:
		return "i8"
	case typeck.TI16, typeck.TU16:
		return "i16"
	case typeck.TI32, typeck.TU32:
		return "i32"
	case typeck.TI64, typeck.TU64:
		return "i64"
	case typeck.TI128, typeck.TU128:
		return "i128"
	case typeck.TF32:
		return "float"
	case typeck.TF64:
		return "double"
	case typeck.TIntLit:
		return "i64"
	case typeck.TFloatLit:
		return "double"
	}
	switch tt := t.(type) {
	case *typeck.StructType:
		return "%" + tt.Name
	case *typeck.EnumType:
		return "%" + tt.Name
	case *typeck.TupleType:
		parts := make([]string, len(tt.Elems))
		for i, e := range tt.Elems {
			parts[i] = em.llType(e)
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	case *typeck.GenType:
		switch tt.Con {
		case "vec":
			return "%_cnd_vec"
		case "ring":
			return "%_cnd_ring"
		case "ref", "refmut", "option", "result", "map", "set", "box":
			return "ptr"
		}
		return "ptr"
	case *typeck.FnType:
		return "ptr"
	}
	return "ptr"
}

// llVarType returns the type to use for alloca (never void).
func (em *llEmitter) llVarType(t typeck.Type) string {
	if isVoidTy(t) {
		return "i8"
	}
	return em.llType(t)
}

func (em *llEmitter) sizeBytes(t typeck.Type) int {
	switch t {
	case typeck.TBool, typeck.TI8, typeck.TU8:
		return 1
	case typeck.TI16, typeck.TU16:
		return 2
	case typeck.TI32, typeck.TU32, typeck.TF32:
		return 4
	case typeck.TI64, typeck.TU64, typeck.TF64:
		return 8
	case typeck.TI128, typeck.TU128:
		return 16
	}
	if st, ok := t.(*typeck.StructType); ok {
		total := 0
		for _, ft := range st.Fields {
			total += em.sizeBytes(ft)
		}
		return total
	}
	return 8
}

func isVoidTy(t typeck.Type) bool {
	return t == nil || t.Equals(typeck.TUnit) || t.Equals(typeck.TNever)
}

func isSignedTy(t typeck.Type) bool {
	p, ok := t.(*typeck.Prim)
	if !ok {
		return false
	}
	switch p.String() {
	case "i8", "i16", "i32", "i64", "i128":
		return true
	}
	return false
}

func isFloatTy(t typeck.Type) bool {
	return t == typeck.TF32 || t == typeck.TF64 || t == typeck.TFloatLit
}

// ── string pool ────────────────────────────────────────────────────────────────

func (em *llEmitter) internStr(s string) string {
	if name, ok := em.strPool[s]; ok {
		return name
	}
	name := fmt.Sprintf("@.str.%d", em.strCount)
	em.strCount++
	em.strPool[s] = name
	enc := llvmEscStr(s)
	em.hf(`%s = private unnamed_addr constant [%d x i8] c"%s\00", align 1`, name, len(s)+1, enc)
	return name
}

func llvmEscStr(s string) string {
	var sb strings.Builder
	for _, b := range []byte(s) {
		if b >= 0x20 && b < 0x7f && b != '"' && b != '\\' {
			sb.WriteByte(b)
		} else {
			fmt.Fprintf(&sb, "\\%02X", b)
		}
	}
	return sb.String()
}

func unquoteStr(lex string) string {
	if len(lex) >= 2 && lex[0] == '"' && lex[len(lex)-1] == '"' {
		return lex[1 : len(lex)-1]
	}
	return lex
}

// ── string pre-collection ─────────────────────────────────────────────────────

func (em *llEmitter) collectStrings(file *parser.File) {
	for _, d := range file.Decls {
		em.collectStrDecl(d)
	}
}

func (em *llEmitter) collectStrDecl(d parser.Decl) {
	switch dd := d.(type) {
	case *parser.FnDecl:
		if dd.Body != nil {
			em.collectStrStmts(dd.Body.Stmts)
		}
	case *parser.ImplDecl:
		for _, m := range dd.Methods {
			if m.Body != nil {
				em.collectStrStmts(m.Body.Stmts)
			}
		}
	case *parser.ImplForDecl:
		for _, m := range dd.Methods {
			if m.Body != nil {
				em.collectStrStmts(m.Body.Stmts)
			}
		}
	}
}

func (em *llEmitter) collectStrStmts(stmts []parser.Stmt) {
	for _, s := range stmts {
		em.collectStrStmt(s)
	}
}

func (em *llEmitter) collectStrStmt(s parser.Stmt) {
	switch ss := s.(type) {
	case *parser.LetStmt:
		em.collectStrExpr(ss.Value)
	case *parser.ReturnStmt:
		if ss.Value != nil {
			em.collectStrExpr(ss.Value)
		}
	case *parser.ExprStmt:
		em.collectStrExpr(ss.X)
	case *parser.IfStmt:
		em.collectStrExpr(ss.Cond)
		em.collectStrStmts(ss.Then.Stmts)
		if ss.Else != nil {
			em.collectStrStmt(ss.Else)
		}
	case *parser.WhileStmt:
		em.collectStrExpr(ss.Cond)
		em.collectStrStmts(ss.Body.Stmts)
	case *parser.LoopStmt:
		em.collectStrStmts(ss.Body.Stmts)
	case *parser.BlockStmt:
		em.collectStrStmts(ss.Stmts)
	case *parser.AssignStmt:
		em.collectStrExpr(ss.Value)
	case *parser.FieldAssignStmt:
		em.collectStrExpr(ss.Value)
	}
}

func (em *llEmitter) collectStrExpr(e parser.Expr) {
	if e == nil {
		return
	}
	switch ee := e.(type) {
	case *parser.StringLitExpr:
		em.internStr(unquoteStr(ee.Tok.Lexeme))
	case *parser.BinaryExpr:
		em.collectStrExpr(ee.Left)
		em.collectStrExpr(ee.Right)
	case *parser.UnaryExpr:
		em.collectStrExpr(ee.Operand)
	case *parser.CallExpr:
		em.collectStrExpr(ee.Fn)
		for _, a := range ee.Args {
			em.collectStrExpr(a)
		}
	case *parser.FieldExpr:
		em.collectStrExpr(ee.Receiver)
	case *parser.StructLitExpr:
		em.collectStrExpr(ee.Base)
		for _, fi := range ee.Fields {
			em.collectStrExpr(fi.Value)
		}
	case *parser.CastExpr:
		em.collectStrExpr(ee.X)
	case *parser.TupleLitExpr:
		for _, el := range ee.Elems {
			em.collectStrExpr(el)
		}
	case *parser.VecLitExpr:
		for _, el := range ee.Elems {
			em.collectStrExpr(el)
		}
	case *parser.MatchExpr:
		em.collectStrExpr(ee.X)
		for _, arm := range ee.Arms {
			em.collectStrExpr(arm.Body)
		}
	case *parser.MustExpr:
		em.collectStrExpr(ee.X)
		for _, arm := range ee.Arms {
			em.collectStrExpr(arm.Body)
		}
	}
}

// ── file emission ──────────────────────────────────────────────────────────────

func (em *llEmitter) emitFile(file *parser.File) error {
	triple := em.target
	if triple == "" {
		triple = "x86_64-unknown-linux-gnu"
	}
	em.h(`; LLVM IR generated by Candor compiler`)
	em.hf(`target triple = "%s"`, triple)
	em.h(``)

	// Built-in collection struct types (layout mirrors the C runtime).
	// %_cnd_vec  = { ptr _data, i64 _len, i64 _cap }
	// %_cnd_ring = { ptr _data, i64 _cap, i64 _head, i64 _len }
	// %_cnd_map  = { ptr _buckets, i64 _len, i64 _cap }
	em.h(`%_cnd_vec = type { ptr, i64, i64 }`)
	em.h(`%_cnd_ring = type { ptr, i64, i64, i64 }`)
	em.h(`%_cnd_map = type { ptr, i64, i64 }`)
	em.h(``)

	em.emitStructTypeDecls(file)
	em.emitEnumTypeDecls(file)
	em.emitLambdaTypeDecls()

	if err := em.emitExternDecls(file); err != nil {
		return err
	}
	if err := em.emitConstGlobals(); err != nil {
		return err
	}

	// Runtime declarations.
	em.h(`declare void @llvm.trap()`)
	em.h(`declare ptr @malloc(i64)`)
	em.h(`declare ptr @realloc(ptr, i64)`)
	em.h(`declare void @free(ptr)`)
	em.h(``)

	return em.emitFunctions(file)
}

func (em *llEmitter) emitStructTypeDecls(file *parser.File) {
	for _, d := range file.Decls {
		sd, ok := d.(*parser.StructDecl)
		if !ok {
			continue
		}
		st := em.res.Structs[sd.Name.Lexeme]
		if st == nil {
			continue
		}
		fields := em.structFields[sd.Name.Lexeme]
		parts := make([]string, len(fields))
		for i, fname := range fields {
			parts[i] = em.llType(st.Fields[fname])
		}
		if len(parts) == 0 {
			em.hf(`%%%s = type {}`, sd.Name.Lexeme)
		} else {
			em.hf(`%%%s = type { %s }`, sd.Name.Lexeme, strings.Join(parts, ", "))
		}
	}
}

func (em *llEmitter) emitEnumTypeDecls(file *parser.File) {
	for _, d := range file.Decls {
		ed, ok := d.(*parser.EnumDecl)
		if !ok {
			continue
		}
		payload := em.enumPayload[ed.Name.Lexeme]
		if payload == 0 {
			em.hf(`%%%s = type { i32 }`, ed.Name.Lexeme)
		} else {
			em.hf(`%%%s = type { i32, [%d x i8] }`, ed.Name.Lexeme, payload)
		}
	}
}

// emitLambdaTypeDecls declares %_cnd_lambda_N_env struct types for all lambdas.
func (em *llEmitter) emitLambdaTypeDecls() {
	for _, lam := range em.res.Lambdas {
		if len(lam.Captures) == 0 {
			continue
		}
		envTy := lam.Name + "_env"
		parts := make([]string, len(lam.Captures))
		for i, _ := range lam.Captures {
			if i >= len(lam.CaptureTypes) {
				parts[i] = "ptr"
				continue
			}
			if i < len(lam.CaptureByRef) && lam.CaptureByRef[i] {
				parts[i] = "ptr" // by-ref: store pointer to outer var
			} else {
				parts[i] = em.llType(lam.CaptureTypes[i])
			}
		}
		em.hf(`%%%s = type { %s }`, envTy, strings.Join(parts, ", "))
	}
}

// mangleLLType converts an LLVM type string to a valid identifier fragment.
func mangleLLType(s string) string {
	s = strings.ReplaceAll(s, "%", "S")
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "{", "L")
	s = strings.ReplaceAll(s, "}", "R")
	s = strings.ReplaceAll(s, ",", "C")
	s = strings.ReplaceAll(s, "*", "P")
	return s
}

// mapEntryType lazily declares and returns the named LLVM type for a map entry.
// Layout: { K, V, ptr _next } — mirrors the C runtime linked-list entry.
func (em *llEmitter) mapEntryType(k, v typeck.Type) string {
	name := "_cnd_map_entry_" + mangleLLType(em.llType(k)) + "_" + mangleLLType(em.llType(v))
	if !em.mapEntryTypes[name] {
		em.mapEntryTypes[name] = true
		em.hf(`%%%s = type { %s, %s, ptr }`, name, em.llType(k), em.llType(v))
	}
	return "%" + name
}

func (em *llEmitter) emitExternDecls(file *parser.File) error {
	for _, d := range file.Decls {
		ed, ok := d.(*parser.ExternFnDecl)
		if !ok {
			continue
		}
		sig := em.res.FnSigs[ed.Name.Lexeme]
		if sig == nil {
			continue
		}
		var paramStrs []string
		variadic := false
		for i, p := range ed.Params {
			if p.Name.Lexeme == "..." {
				variadic = true
				continue
			}
			if i < len(sig.Params) {
				paramStrs = append(paramStrs, em.llType(sig.Params[i]))
			}
		}
		retStr := "void"
		if !isVoidTy(sig.Ret) {
			retStr = em.llType(sig.Ret)
		}
		paramList := strings.Join(paramStrs, ", ")
		if variadic {
			em.hf(`declare %s @%s(%s, ...)`, retStr, ed.Name.Lexeme, paramList)
		} else {
			em.hf(`declare %s @%s(%s)`, retStr, ed.Name.Lexeme, paramList)
		}
	}
	return nil
}

func (em *llEmitter) emitConstGlobals() error {
	for _, cd := range em.res.ConstDecls {
		ty := em.res.Consts[cd.Name.Lexeme]
		if ty == nil {
			continue
		}
		val, err := em.constLiteral(cd.Value)
		if err != nil {
			return err
		}
		em.hf(`@%s = private constant %s %s`, cd.Name.Lexeme, em.llType(ty), val)
	}
	return nil
}

func (em *llEmitter) constLiteral(e parser.Expr) (string, error) {
	switch ee := e.(type) {
	case *parser.IntLitExpr:
		return ee.Tok.Lexeme, nil
	case *parser.FloatLitExpr:
		return ee.Tok.Lexeme, nil
	case *parser.BoolLitExpr:
		if ee.Tok.Lexeme == "true" {
			return "1", nil
		}
		return "0", nil
	case *parser.StringLitExpr:
		return em.internStr(unquoteStr(ee.Tok.Lexeme)), nil
	}
	return "zeroinitializer", nil
}

// ── function emission ──────────────────────────────────────────────────────────

func (em *llEmitter) emitFunctions(file *parser.File) error {
	// Emit lambda impl functions first so they precede any callers.
	for _, lam := range em.res.Lambdas {
		if err := em.emitLambdaImpl(lam); err != nil {
			return err
		}
	}

	hasMain := false
	for _, d := range file.Decls {
		fd, ok := d.(*parser.FnDecl)
		if !ok || len(fd.TypeParams) != 0 {
			continue
		}
		if fd.Name.Lexeme == "main" {
			hasMain = true
		}
		if err := em.emitFnDecl(fd.Name.Lexeme, fd); err != nil {
			return err
		}
	}
	for _, impl := range em.res.ImplDecls {
		for _, m := range impl.Methods {
			if len(m.TypeParams) != 0 {
				continue
			}
			mn := impl.TypeName.Lexeme + "_" + m.Name.Lexeme
			if err := em.emitFnDecl(mn, m); err != nil {
				return err
			}
		}
	}
	for _, impl := range em.res.ImplForDecls {
		for _, m := range impl.Methods {
			if len(m.TypeParams) != 0 {
				continue
			}
			mn := impl.TypeName.Lexeme + "_" + m.Name.Lexeme
			if err := em.emitFnDecl(mn, m); err != nil {
				return err
			}
		}
	}
	for _, inst := range em.res.GenericInstances {
		if inst.Node != nil {
			if err := em.emitFnDecl(inst.MangledName, inst.Node); err != nil {
				return err
			}
		}
	}
	if hasMain {
		em.w(`define i32 @main() {`)
		em.w(`entry:`)
		em.wi(`call void @_cnd_main()`)
		em.wi(`ret i32 0`)
		em.w(`}`)
		em.w(``)
	}
	return nil
}

func (em *llEmitter) emitFnDecl(name string, fd *parser.FnDecl) error {
	em.resetFn()
	sig := em.res.FnSigs[name]
	if sig == nil {
		return fmt.Errorf("emit_llvm: no signature for %q", name)
	}
	em.retType = sig.Ret

	llName := name
	if name == "main" {
		llName = "_cnd_main"
		em.isMain = true
	}

	var paramStrs []string
	paramRegs := make([]string, len(fd.Params))
	for i, p := range fd.Params {
		if i >= len(sig.Params) {
			break
		}
		pt := sig.Params[i]
		reg := "%" + p.Name.Lexeme + ".in"
		paramStrs = append(paramStrs, em.llType(pt)+" "+reg)
		paramRegs[i] = reg
	}

	retStr := "void"
	if !isVoidTy(sig.Ret) {
		retStr = em.llType(sig.Ret)
	}

	em.wf(`define %s @%s(%s) {`, retStr, llName, strings.Join(paramStrs, ", "))
	em.w(`entry:`)

	// Alloca + store for each param.
	for i, p := range fd.Params {
		if i >= len(sig.Params) {
			break
		}
		pt := sig.Params[i]
		addr := "%" + p.Name.Lexeme + ".addr"
		em.locals[p.Name.Lexeme] = addr
		em.localTypes[p.Name.Lexeme] = pt
		em.wif(`%s = alloca %s`, addr, em.llVarType(pt))
		if !isVoidTy(pt) {
			em.wif(`store %s %s, ptr %s`, em.llType(pt), paramRegs[i], addr)
		}
	}

	if fd.Body != nil {
		if err := em.emitStmts(fd.Body.Stmts); err != nil {
			return err
		}
	}

	// Implicit terminator.
	if isVoidTy(sig.Ret) {
		em.wi(`ret void`)
	} else {
		em.wif(`ret %s zeroinitializer`, em.llType(sig.Ret))
	}
	em.w(`}`)
	em.w(``)
	return nil
}

// emitLambdaImpl emits a top-level @_cnd_lambda_N_impl function.
func (em *llEmitter) emitLambdaImpl(lam *typeck.LambdaInfo) error {
	em.resetFn()
	sig := lam.Sig
	em.retType = sig.Ret

	// Build param list: user params + ptr %_env.
	var paramStrs []string
	paramRegs := make([]string, len(lam.Node.Params))
	for i, p := range lam.Node.Params {
		if i >= len(sig.Params) {
			break
		}
		pt := sig.Params[i]
		reg := "%" + p.Name.Lexeme + ".in"
		paramStrs = append(paramStrs, em.llType(pt)+" "+reg)
		paramRegs[i] = reg
	}
	paramStrs = append(paramStrs, "ptr %_env")

	retStr := "void"
	if !isVoidTy(sig.Ret) {
		retStr = em.llType(sig.Ret)
	}

	implName := lam.Name + "_impl"
	em.wf(`define %s @%s(%s) {`, retStr, implName, strings.Join(paramStrs, ", "))
	em.w(`entry:`)

	// Alloca + store for each user param.
	for i, p := range lam.Node.Params {
		if i >= len(sig.Params) {
			break
		}
		pt := sig.Params[i]
		addr := "%" + p.Name.Lexeme + ".addr"
		em.locals[p.Name.Lexeme] = addr
		em.localTypes[p.Name.Lexeme] = pt
		em.wif(`%s = alloca %s`, addr, em.llVarType(pt))
		if !isVoidTy(pt) {
			em.wif(`store %s %s, ptr %s`, em.llType(pt), paramRegs[i], addr)
		}
	}

	// Unpack captures from _env.
	if len(lam.Captures) > 0 {
		envTy := "%" + lam.Name + "_env"
		for i, capName := range lam.Captures {
			if i >= len(lam.CaptureTypes) {
				break
			}
			capTy := lam.CaptureTypes[i]
			byRef := i < len(lam.CaptureByRef) && lam.CaptureByRef[i]
			slot := em.fresh()
			em.wif(`%s = getelementptr %s, ptr %%_env, i32 0, i32 %d`, slot, envTy, i)
			addr := "%" + capName + ".addr"
			em.locals[capName] = addr
			em.localTypes[capName] = capTy
			if byRef {
				// Env stores ptr to outer var; our alloca holds that ptr.
				em.wif(`%s = alloca ptr`, addr)
				pv := em.fresh()
				em.wif(`%s = load ptr, ptr %s`, pv, slot)
				em.wif(`store ptr %s, ptr %s`, pv, addr)
			} else {
				em.wif(`%s = alloca %s`, addr, em.llType(capTy))
				cv := em.fresh()
				em.wif(`%s = load %s, ptr %s`, cv, em.llType(capTy), slot)
				em.wif(`store %s %s, ptr %s`, em.llType(capTy), cv, addr)
			}
		}
	}

	if lam.Node.Body != nil {
		if err := em.emitStmts(lam.Node.Body.Stmts); err != nil {
			return err
		}
	}

	if isVoidTy(sig.Ret) {
		em.wi(`ret void`)
	} else {
		em.wif(`ret %s zeroinitializer`, em.llType(sig.Ret))
	}
	em.w(`}`)
	em.w(``)
	return nil
}

// findLambda looks up the LambdaInfo for a given AST node.
func (em *llEmitter) findLambda(node *parser.LambdaExpr) *typeck.LambdaInfo {
	for _, lam := range em.res.Lambdas {
		if lam.Node == node {
			return lam
		}
	}
	return nil
}

// emitLambdaExpr emits a lambda expression as a heap-allocated fat pointer { ptr fnptr, ptr env }.
func (em *llEmitter) emitLambdaExpr(e *parser.LambdaExpr) (string, error) {
	lam := em.findLambda(e)
	if lam == nil {
		em.wi(`; lambda: no type info`)
		return "null", nil
	}
	implName := "@" + lam.Name + "_impl"

	var envReg string
	if len(lam.Captures) == 0 {
		envReg = "null"
	} else {
		envTy := "%" + lam.Name + "_env"
		// sizeof(env) via GEP-from-null trick.
		szPtr := em.fresh()
		szVal := em.fresh()
		env := em.fresh()
		em.wif(`%s = getelementptr %s, ptr null, i64 1`, szPtr, envTy)
		em.wif(`%s = ptrtoint ptr %s to i64`, szVal, szPtr)
		em.wif(`%s = call ptr @malloc(i64 %s)`, env, szVal)
		// Store each capture.
		for i, capName := range lam.Captures {
			if i >= len(lam.CaptureTypes) {
				break
			}
			capTy := lam.CaptureTypes[i]
			byRef := i < len(lam.CaptureByRef) && lam.CaptureByRef[i]
			slot := em.fresh()
			em.wif(`%s = getelementptr %s, ptr %s, i32 0, i32 %d`, slot, envTy, env, i)
			if byRef {
				capAddr, ok := em.locals[capName]
				if !ok {
					capAddr = "null"
				}
				em.wif(`store ptr %s, ptr %s`, capAddr, slot)
			} else {
				capAddr, ok := em.locals[capName]
				if ok {
					cv := em.fresh()
					em.wif(`%s = load %s, ptr %s`, cv, em.llType(capTy), capAddr)
					em.wif(`store %s %s, ptr %s`, em.llType(capTy), cv, slot)
				} else {
					em.wif(`store %s zeroinitializer, ptr %s`, em.llType(capTy), slot)
				}
			}
		}
		envReg = env
	}

	// Heap-allocate fat pointer { ptr fnptr, ptr env } (16 bytes on 64-bit).
	fat := em.fresh()
	em.wif(`%s = call ptr @malloc(i64 16)`, fat)
	fnSlot := em.fresh()
	envSlot := em.fresh()
	em.wif(`%s = getelementptr { ptr, ptr }, ptr %s, i32 0, i32 0`, fnSlot, fat)
	em.wif(`%s = getelementptr { ptr, ptr }, ptr %s, i32 0, i32 1`, envSlot, fat)
	em.wif(`store ptr %s, ptr %s`, implName, fnSlot)
	em.wif(`store ptr %s, ptr %s`, envReg, envSlot)
	return fat, nil
}

// ── statement emission ─────────────────────────────────────────────────────────

func (em *llEmitter) emitStmts(stmts []parser.Stmt) error {
	for _, s := range stmts {
		if err := em.emitStmt(s); err != nil {
			return err
		}
	}
	return nil
}

func (em *llEmitter) emitStmt(s parser.Stmt) error {
	switch ss := s.(type) {
	case *parser.LetStmt:
		return em.emitLetStmt(ss)
	case *parser.ReturnStmt:
		return em.emitReturnStmt(ss)
	case *parser.ExprStmt:
		_, err := em.emitExpr(ss.X)
		return err
	case *parser.IfStmt:
		return em.emitIfStmt(ss)
	case *parser.WhileStmt:
		return em.emitWhileStmt(ss)
	case *parser.LoopStmt:
		return em.emitLoopStmt(ss)
	case *parser.BreakStmt:
		if em.breakLabel == "" {
			return fmt.Errorf("emit_llvm: break outside loop")
		}
		em.wif(`br label %%%s`, em.breakLabel)
		em.lbl(em.freshBlk("dead"))
		return nil
	case *parser.ContinueStmt:
		if em.contLabel == "" {
			return fmt.Errorf("emit_llvm: continue outside loop")
		}
		em.wif(`br label %%%s`, em.contLabel)
		em.lbl(em.freshBlk("dead"))
		return nil
	case *parser.AssignStmt:
		return em.emitAssignStmt(ss)
	case *parser.FieldAssignStmt:
		return em.emitFieldAssignStmt(ss)
	case *parser.IndexAssignStmt:
		return em.emitIndexAssignStmt(ss)
	case *parser.BlockStmt:
		return em.emitStmts(ss.Stmts)
	case *parser.AssertStmt:
		return em.emitAssertStmt(ss)
	case *parser.ForStmt:
		return em.emitForStmt(ss)
	case *parser.TupleDestructureStmt:
		return em.emitTupleDestructure(ss)
	}
	return nil
}

func (em *llEmitter) emitLetStmt(s *parser.LetStmt) error {
	ty := em.res.ExprTypes[s.Value]
	if ty == nil {
		ty = typeck.TUnit
	}
	addr := "%" + s.Name.Lexeme + ".addr"
	em.locals[s.Name.Lexeme] = addr
	em.localTypes[s.Name.Lexeme] = ty

	if isVoidTy(ty) {
		em.wif(`%s = alloca i8`, addr)
		_, err := em.emitExpr(s.Value) // side effects
		return err
	}

	em.wif(`%s = alloca %s`, addr, em.llType(ty))
	val, err := em.emitExpr(s.Value)
	if err != nil {
		return err
	}
	em.wif(`store %s %s, ptr %s`, em.llType(ty), val, addr)
	return nil
}

func (em *llEmitter) emitReturnStmt(s *parser.ReturnStmt) error {
	if s.Value == nil || isVoidTy(em.retType) {
		em.wi(`ret void`)
	} else {
		val, err := em.emitExpr(s.Value)
		if err != nil {
			return err
		}
		em.wif(`ret %s %s`, em.llType(em.retType), val)
	}
	em.lbl(em.freshBlk("dead"))
	return nil
}

func (em *llEmitter) emitIfStmt(s *parser.IfStmt) error {
	cond, err := em.emitExpr(s.Cond)
	if err != nil {
		return err
	}
	// Ensure i1: if cond is not already i1, compare != 0
	condTy := em.res.ExprTypes[s.Cond]
	condBit := cond
	if condTy != nil && !condTy.Equals(typeck.TBool) {
		t := em.fresh()
		em.wif(`%s = icmp ne %s %s, 0`, t, em.llType(condTy), cond)
		condBit = t
	}

	thenLbl := em.freshBlk("then")
	mergeLbl := em.freshBlk("merge")

	if s.Else == nil {
		em.wif(`br i1 %s, label %%%s, label %%%s`, condBit, thenLbl, mergeLbl)
		em.lbl(thenLbl)
		if err := em.emitStmts(s.Then.Stmts); err != nil {
			return err
		}
		em.wif(`br label %%%s`, mergeLbl)
		em.lbl(mergeLbl)
		return nil
	}

	elseLbl := em.freshBlk("else")
	em.wif(`br i1 %s, label %%%s, label %%%s`, condBit, thenLbl, elseLbl)
	em.lbl(thenLbl)
	if err := em.emitStmts(s.Then.Stmts); err != nil {
		return err
	}
	em.wif(`br label %%%s`, mergeLbl)
	em.lbl(elseLbl)
	if err := em.emitStmt(s.Else); err != nil {
		return err
	}
	em.wif(`br label %%%s`, mergeLbl)
	em.lbl(mergeLbl)
	return nil
}

func (em *llEmitter) emitWhileStmt(s *parser.WhileStmt) error {
	hdrLbl := em.freshBlk("while.hdr")
	bodyLbl := em.freshBlk("while.body")
	exitLbl := em.freshBlk("while.exit")

	prevBreak, prevCont := em.breakLabel, em.contLabel
	em.breakLabel = exitLbl
	em.contLabel = hdrLbl

	em.wif(`br label %%%s`, hdrLbl)
	em.lbl(hdrLbl)
	cond, err := em.emitExpr(s.Cond)
	if err != nil {
		return err
	}
	em.wif(`br i1 %s, label %%%s, label %%%s`, cond, bodyLbl, exitLbl)
	em.lbl(bodyLbl)
	if err := em.emitStmts(s.Body.Stmts); err != nil {
		return err
	}
	em.wif(`br label %%%s`, hdrLbl)
	em.lbl(exitLbl)

	em.breakLabel, em.contLabel = prevBreak, prevCont
	return nil
}

func (em *llEmitter) emitLoopStmt(s *parser.LoopStmt) error {
	bodyLbl := em.freshBlk("loop.body")
	exitLbl := em.freshBlk("loop.exit")

	prevBreak, prevCont := em.breakLabel, em.contLabel
	em.breakLabel = exitLbl
	em.contLabel = bodyLbl

	em.wif(`br label %%%s`, bodyLbl)
	em.lbl(bodyLbl)
	if err := em.emitStmts(s.Body.Stmts); err != nil {
		return err
	}
	em.wif(`br label %%%s`, bodyLbl)
	em.lbl(exitLbl)

	em.breakLabel, em.contLabel = prevBreak, prevCont
	return nil
}

func (em *llEmitter) emitAssignStmt(s *parser.AssignStmt) error {
	addr, ok := em.locals[s.Name.Lexeme]
	if !ok {
		return fmt.Errorf("emit_llvm: assign to undefined %q", s.Name.Lexeme)
	}
	ty := em.res.ExprTypes[s.Value]
	if ty == nil {
		ty = typeck.TUnit
	}
	if isVoidTy(ty) {
		_, err := em.emitExpr(s.Value)
		return err
	}
	val, err := em.emitExpr(s.Value)
	if err != nil {
		return err
	}
	em.wif(`store %s %s, ptr %s`, em.llType(ty), val, addr)
	return nil
}

func (em *llEmitter) emitFieldAssignStmt(s *parser.FieldAssignStmt) error {
	recvTy := em.res.ExprTypes[s.Target.Receiver]
	if recvTy == nil {
		return fmt.Errorf("emit_llvm: field-assign on nil type")
	}
	st, ok := recvTy.(*typeck.StructType)
	if !ok {
		return fmt.Errorf("emit_llvm: field-assign on non-struct %s", recvTy)
	}

	recvAddr, err := em.emitAddr(s.Target.Receiver)
	if err != nil {
		return err
	}
	llRecv := em.llType(recvTy)
	old := em.fresh()
	em.wif(`%s = load %s, ptr %s`, old, llRecv, recvAddr)

	newVal, err := em.emitExpr(s.Value)
	if err != nil {
		return err
	}
	fieldTy := st.Fields[s.Target.Field.Lexeme]
	idx := em.fieldIdx(st.Name, s.Target.Field.Lexeme)
	updated := em.fresh()
	em.wif(`%s = insertvalue %s %s, %s %s, %d`, updated, llRecv, old, em.llType(fieldTy), newVal, idx)
	em.wif(`store %s %s, ptr %s`, llRecv, updated, recvAddr)
	return nil
}

func (em *llEmitter) emitAssertStmt(s *parser.AssertStmt) error {
	val, err := em.emitExpr(s.Expr)
	if err != nil {
		return err
	}
	passLbl := em.freshBlk("assert.ok")
	failLbl := em.freshBlk("assert.fail")
	em.wif(`br i1 %s, label %%%s, label %%%s`, val, passLbl, failLbl)
	em.lbl(failLbl)
	em.wi(`call void @llvm.trap()`)
	em.wi(`unreachable`)
	em.lbl(passLbl)
	return nil
}

func (em *llEmitter) emitForStmt(s *parser.ForStmt) error {
	collTy := em.res.ExprTypes[s.Collection]
	gen, ok := collTy.(*typeck.GenType)
	if !ok {
		return fmt.Errorf("emit_llvm: for-in on non-collection type %v", collTy)
	}

	collVal, err := em.emitExpr(s.Collection)
	if err != nil {
		return err
	}

	hdrLbl := em.freshBlk("for.hdr")
	incrLbl := em.freshBlk("for.incr")
	bodyLbl := em.freshBlk("for.body")
	exitLbl := em.freshBlk("for.exit")

	oldBreak, oldCont := em.breakLabel, em.contLabel
	em.breakLabel = exitLbl
	em.contLabel = incrLbl // continue → increment step (not header)

	switch gen.Con {
	case "vec":
		elemTy := gen.Params[0]
		dataReg := em.fresh()
		lenReg := em.fresh()
		em.wif(`%s = extractvalue %%_cnd_vec %s, 0`, dataReg, collVal)
		em.wif(`%s = extractvalue %%_cnd_vec %s, 1`, lenReg, collVal)

		iAddr := em.fresh()
		em.wif(`%s = alloca i64`, iAddr)
		em.wif(`store i64 0, ptr %s`, iAddr)
		em.wif(`br label %%%s`, hdrLbl)

		em.lbl(hdrLbl)
		iReg := em.fresh()
		cond := em.fresh()
		em.wif(`%s = load i64, ptr %s`, iReg, iAddr)
		em.wif(`%s = icmp ult i64 %s, %s`, cond, iReg, lenReg)
		em.wif(`br i1 %s, label %%%s, label %%%s`, cond, bodyLbl, exitLbl)

		em.lbl(bodyLbl)
		epReg := em.fresh()
		elem := em.fresh()
		em.wif(`%s = getelementptr %s, ptr %s, i64 %s`, epReg, em.llType(elemTy), dataReg, iReg)
		em.wif(`%s = load %s, ptr %s`, elem, em.llType(elemTy), epReg)

		varAddr := em.fresh()
		em.locals[s.Var.Lexeme] = varAddr
		em.localTypes[s.Var.Lexeme] = elemTy
		em.wif(`%s = alloca %s`, varAddr, em.llType(elemTy))
		em.wif(`store %s %s, ptr %s`, em.llType(elemTy), elem, varAddr)

		if err := em.emitStmts(s.Body.Stmts); err != nil {
			return err
		}
		em.wif(`br label %%%s`, incrLbl)

		em.lbl(incrLbl)
		iReload := em.fresh()
		iNext := em.fresh()
		em.wif(`%s = load i64, ptr %s`, iReload, iAddr)
		em.wif(`%s = add i64 %s, 1`, iNext, iReload)
		em.wif(`store i64 %s, ptr %s`, iNext, iAddr)
		em.wif(`br label %%%s`, hdrLbl)

		em.lbl(exitLbl)
		delete(em.locals, s.Var.Lexeme)
		delete(em.localTypes, s.Var.Lexeme)

	case "ring":
		elemTy := gen.Params[0]
		// %_cnd_ring = { ptr _data, i64 _cap, i64 _head, i64 _len }
		dataReg := em.fresh()
		capReg := em.fresh()
		headReg := em.fresh()
		lenReg := em.fresh()
		em.wif(`%s = extractvalue %%_cnd_ring %s, 0`, dataReg, collVal)
		em.wif(`%s = extractvalue %%_cnd_ring %s, 1`, capReg, collVal)
		em.wif(`%s = extractvalue %%_cnd_ring %s, 2`, headReg, collVal)
		em.wif(`%s = extractvalue %%_cnd_ring %s, 3`, lenReg, collVal)

		iAddr := em.fresh()
		em.wif(`%s = alloca i64`, iAddr)
		em.wif(`store i64 0, ptr %s`, iAddr)
		em.wif(`br label %%%s`, hdrLbl)

		em.lbl(hdrLbl)
		iReg := em.fresh()
		cond := em.fresh()
		em.wif(`%s = load i64, ptr %s`, iReg, iAddr)
		em.wif(`%s = icmp ult i64 %s, %s`, cond, iReg, lenReg)
		em.wif(`br i1 %s, label %%%s, label %%%s`, cond, bodyLbl, exitLbl)

		em.lbl(bodyLbl)
		sumReg := em.fresh()
		modReg := em.fresh()
		em.wif(`%s = add i64 %s, %s`, sumReg, headReg, iReg)
		em.wif(`%s = urem i64 %s, %s`, modReg, sumReg, capReg)
		epReg := em.fresh()
		elem := em.fresh()
		em.wif(`%s = getelementptr %s, ptr %s, i64 %s`, epReg, em.llType(elemTy), dataReg, modReg)
		em.wif(`%s = load %s, ptr %s`, elem, em.llType(elemTy), epReg)

		varAddr := em.fresh()
		em.locals[s.Var.Lexeme] = varAddr
		em.localTypes[s.Var.Lexeme] = elemTy
		em.wif(`%s = alloca %s`, varAddr, em.llType(elemTy))
		em.wif(`store %s %s, ptr %s`, em.llType(elemTy), elem, varAddr)

		if err := em.emitStmts(s.Body.Stmts); err != nil {
			return err
		}
		em.wif(`br label %%%s`, incrLbl)

		em.lbl(incrLbl)
		iReload := em.fresh()
		iNext := em.fresh()
		em.wif(`%s = load i64, ptr %s`, iReload, iAddr)
		em.wif(`%s = add i64 %s, 1`, iNext, iReload)
		em.wif(`store i64 %s, ptr %s`, iNext, iAddr)
		em.wif(`br label %%%s`, hdrLbl)

		em.lbl(exitLbl)
		delete(em.locals, s.Var.Lexeme)
		delete(em.localTypes, s.Var.Lexeme)

	case "map":
		if s.Var2 == nil || len(gen.Params) < 2 {
			em.wi(`; for-in on map requires two loop variables (k, v)`)
			em.lbl(hdrLbl)
			em.lbl(incrLbl)
			em.lbl(bodyLbl)
			em.lbl(exitLbl)
			break
		}
		kTy := gen.Params[0]
		vTy := gen.Params[1]
		entryTy := em.mapEntryType(kTy, vTy)

		// collVal is a ptr to the C map struct { ptr _buckets, i64 _len, i64 _cap }.
		bucketsSlot := em.fresh()
		capSlot := em.fresh()
		bucketsReg := em.fresh()
		capReg := em.fresh()
		em.wif(`%s = getelementptr %%_cnd_map, ptr %s, i32 0, i32 0`, bucketsSlot, collVal)
		em.wif(`%s = getelementptr %%_cnd_map, ptr %s, i32 0, i32 2`, capSlot, collVal)
		em.wif(`%s = load ptr, ptr %s`, bucketsReg, bucketsSlot)
		em.wif(`%s = load i64, ptr %s`, capReg, capSlot)

		// Outer loop counter: bi = 0..cap.
		biAddr := em.fresh()
		em.wif(`%s = alloca i64`, biAddr)
		em.wif(`store i64 0, ptr %s`, biAddr)
		em.wif(`br label %%%s`, hdrLbl)

		em.lbl(hdrLbl)
		biReg := em.fresh()
		outerCond := em.fresh()
		em.wif(`%s = load i64, ptr %s`, biReg, biAddr)
		em.wif(`%s = icmp ult i64 %s, %s`, outerCond, biReg, capReg)
		em.wif(`br i1 %s, label %%%s, label %%%s`, outerCond, bodyLbl, exitLbl)

		em.lbl(bodyLbl)
		// Load entry* from buckets[bi].
		bucketSlot := em.fresh()
		entryAddr := em.fresh()
		firstEntry := em.fresh()
		em.wif(`%s = getelementptr ptr, ptr %s, i64 %s`, bucketSlot, bucketsReg, biReg)
		em.wif(`%s = load ptr, ptr %s`, firstEntry, bucketSlot)
		em.wif(`%s = alloca ptr`, entryAddr)
		em.wif(`store ptr %s, ptr %s`, firstEntry, entryAddr)

		// Inner loop: walk the _next linked-list chain.
		innerHdr := em.freshBlk("map.inner.hdr")
		innerBody := em.freshBlk("map.inner.body")
		innerIncr := em.freshBlk("map.inner.incr")

		em.wif(`br label %%%s`, innerHdr)
		em.lbl(innerHdr)
		curEntry := em.fresh()
		nonNull := em.fresh()
		em.wif(`%s = load ptr, ptr %s`, curEntry, entryAddr)
		em.wif(`%s = icmp ne ptr %s, null`, nonNull, curEntry)
		em.wif(`br i1 %s, label %%%s, label %%%s`, nonNull, innerBody, incrLbl)

		em.lbl(innerBody)
		// Load key, val, _next; pre-advance entryAddr so continue is safe.
		keySlot := em.fresh()
		valSlot := em.fresh()
		nextSlot := em.fresh()
		keyReg := em.fresh()
		valReg := em.fresh()
		nextReg := em.fresh()
		em.wif(`%s = getelementptr %s, ptr %s, i32 0, i32 0`, keySlot, entryTy, curEntry)
		em.wif(`%s = getelementptr %s, ptr %s, i32 0, i32 1`, valSlot, entryTy, curEntry)
		em.wif(`%s = getelementptr %s, ptr %s, i32 0, i32 2`, nextSlot, entryTy, curEntry)
		em.wif(`%s = load %s, ptr %s`, keyReg, em.llType(kTy), keySlot)
		em.wif(`%s = load %s, ptr %s`, valReg, em.llType(vTy), valSlot)
		em.wif(`%s = load ptr, ptr %s`, nextReg, nextSlot)
		em.wif(`store ptr %s, ptr %s`, nextReg, entryAddr)

		// Bind k and v.
		kAddr := em.fresh()
		vAddr := em.fresh()
		em.locals[s.Var.Lexeme] = kAddr
		em.localTypes[s.Var.Lexeme] = kTy
		em.locals[s.Var2.Lexeme] = vAddr
		em.localTypes[s.Var2.Lexeme] = vTy
		em.wif(`%s = alloca %s`, kAddr, em.llType(kTy))
		em.wif(`%s = alloca %s`, vAddr, em.llType(vTy))
		em.wif(`store %s %s, ptr %s`, em.llType(kTy), keyReg, kAddr)
		em.wif(`store %s %s, ptr %s`, em.llType(vTy), valReg, vAddr)

		// User body: continue → innerIncr, break → exitLbl.
		prevCont := em.contLabel
		em.contLabel = innerIncr
		if err := em.emitStmts(s.Body.Stmts); err != nil {
			return err
		}
		em.contLabel = prevCont
		em.wif(`br label %%%s`, innerIncr)

		em.lbl(innerIncr)
		em.wif(`br label %%%s`, innerHdr)

		// Outer incr: advance bucket index.
		em.lbl(incrLbl)
		biReload := em.fresh()
		biNext := em.fresh()
		em.wif(`%s = load i64, ptr %s`, biReload, biAddr)
		em.wif(`%s = add i64 %s, 1`, biNext, biReload)
		em.wif(`store i64 %s, ptr %s`, biNext, biAddr)
		em.wif(`br label %%%s`, hdrLbl)

		em.lbl(exitLbl)
		delete(em.locals, s.Var.Lexeme)
		delete(em.localTypes, s.Var.Lexeme)
		delete(em.locals, s.Var2.Lexeme)
		delete(em.localTypes, s.Var2.Lexeme)

	default:
		em.wi(`; TODO: for-in over set not yet supported in LLVM backend`)
		em.lbl(hdrLbl)
		em.lbl(incrLbl)
		em.lbl(bodyLbl)
		em.lbl(exitLbl)
	}

	em.breakLabel = oldBreak
	em.contLabel = oldCont
	return nil
}

func (em *llEmitter) emitIndexAssignStmt(s *parser.IndexAssignStmt) error {
	collTy := em.res.ExprTypes[s.Target.Collection]
	gen, ok := collTy.(*typeck.GenType)
	if !ok {
		return fmt.Errorf("emit_llvm: index-assign on non-collection type %v", collTy)
	}

	idx, err := em.emitExpr(s.Target.Index)
	if err != nil {
		return err
	}
	newVal, err := em.emitExpr(s.Value)
	if err != nil {
		return err
	}
	elemTy := em.res.ExprTypes[s.Target]
	if elemTy == nil {
		elemTy = typeck.TI64
	}

	// Get the alloca address of the collection so we can read its data pointer.
	collAddr, err := em.emitAddr(s.Target.Collection)
	if err != nil {
		return err
	}

	switch gen.Con {
	case "vec":
		// %_cnd_vec = { ptr _data, i64 _len, i64 _cap }
		dataPtrSlot := em.fresh()
		dataReg := em.fresh()
		em.wif(`%s = getelementptr %%_cnd_vec, ptr %s, i32 0, i32 0`, dataPtrSlot, collAddr)
		em.wif(`%s = load ptr, ptr %s`, dataReg, dataPtrSlot)
		epReg := em.fresh()
		em.wif(`%s = getelementptr %s, ptr %s, i64 %s`, epReg, em.llType(elemTy), dataReg, idx)
		em.wif(`store %s %s, ptr %s`, em.llType(elemTy), newVal, epReg)

	case "ring":
		// %_cnd_ring = { ptr _data, i64 _cap, i64 _head, i64 _len }
		dataPtrSlot := em.fresh()
		capSlot := em.fresh()
		headSlot := em.fresh()
		dataReg := em.fresh()
		capReg := em.fresh()
		headReg := em.fresh()
		em.wif(`%s = getelementptr %%_cnd_ring, ptr %s, i32 0, i32 0`, dataPtrSlot, collAddr)
		em.wif(`%s = getelementptr %%_cnd_ring, ptr %s, i32 0, i32 1`, capSlot, collAddr)
		em.wif(`%s = getelementptr %%_cnd_ring, ptr %s, i32 0, i32 2`, headSlot, collAddr)
		em.wif(`%s = load ptr, ptr %s`, dataReg, dataPtrSlot)
		em.wif(`%s = load i64, ptr %s`, capReg, capSlot)
		em.wif(`%s = load i64, ptr %s`, headReg, headSlot)
		sumReg := em.fresh()
		modReg := em.fresh()
		em.wif(`%s = add i64 %s, %s`, sumReg, headReg, idx)
		em.wif(`%s = urem i64 %s, %s`, modReg, sumReg, capReg)
		epReg := em.fresh()
		em.wif(`%s = getelementptr %s, ptr %s, i64 %s`, epReg, em.llType(elemTy), dataReg, modReg)
		em.wif(`store %s %s, ptr %s`, em.llType(elemTy), newVal, epReg)

	default:
		em.wi(`; TODO: index-assign on map/set not yet supported in LLVM backend`)
	}
	return nil
}

func (em *llEmitter) emitTupleDestructure(s *parser.TupleDestructureStmt) error {
	val, err := em.emitExpr(s.Value)
	if err != nil {
		return err
	}
	ty := em.res.ExprTypes[s.Value]
	tt, ok := ty.(*typeck.TupleType)
	if !ok {
		return fmt.Errorf("emit_llvm: tuple destructure on non-tuple type %v", ty)
	}
	for i, name := range s.Names {
		if i >= len(tt.Elems) {
			break
		}
		elemTy := tt.Elems[i]
		addr := "%" + name.Lexeme + ".addr"
		em.locals[name.Lexeme] = addr
		em.localTypes[name.Lexeme] = elemTy
		em.wif(`%s = alloca %s`, addr, em.llType(elemTy))
		ext := em.fresh()
		em.wif(`%s = extractvalue %s %s, %d`, ext, em.llType(ty), val, i)
		em.wif(`store %s %s, ptr %s`, em.llType(elemTy), ext, addr)
	}
	return nil
}

// ── expression emission ────────────────────────────────────────────────────────

// emitExpr computes e and returns the SSA register name holding its value.
func (em *llEmitter) emitExpr(e parser.Expr) (string, error) {
	if e == nil {
		return "undef", nil
	}
	switch ee := e.(type) {
	case *parser.IntLitExpr:
		return ee.Tok.Lexeme, nil
	case *parser.FloatLitExpr:
		return ee.Tok.Lexeme, nil
	case *parser.BoolLitExpr:
		if ee.Tok.Lexeme == "true" {
			return "1", nil
		}
		return "0", nil
	case *parser.StringLitExpr:
		return em.internStr(unquoteStr(ee.Tok.Lexeme)), nil
	case *parser.IdentExpr:
		return em.emitIdent(ee)
	case *parser.BinaryExpr:
		return em.emitBinary(ee)
	case *parser.UnaryExpr:
		return em.emitUnary(ee)
	case *parser.CallExpr:
		return em.emitCall(ee)
	case *parser.FieldExpr:
		return em.emitField(ee)
	case *parser.StructLitExpr:
		return em.emitStructLit(ee)
	case *parser.CastExpr:
		return em.emitCast(ee)
	case *parser.PathExpr:
		return em.emitPath(ee)
	case *parser.IndexExpr:
		return em.emitIndex(ee)
	case *parser.TupleLitExpr:
		return em.emitTupleLit(ee)
	case *parser.VecLitExpr:
		return em.emitVecLit(ee)
	case *parser.LambdaExpr:
		return em.emitLambdaExpr(ee)
	case *parser.MatchExpr:
		return em.emitMatch(ee)
	case *parser.MustExpr:
		return em.emitMust(ee)
	case *parser.ReturnExpr:
		val, err := em.emitExpr(ee.Value)
		if err != nil {
			return "", err
		}
		if isVoidTy(em.retType) {
			em.wi(`ret void`)
		} else {
			em.wif(`ret %s %s`, em.llType(em.retType), val)
		}
		em.lbl(em.freshBlk("dead"))
		return "undef", nil
	case *parser.BreakExpr:
		if em.breakLabel != "" {
			em.wif(`br label %%%s`, em.breakLabel)
		} else {
			em.wi(`unreachable`)
		}
		em.lbl(em.freshBlk("dead"))
		return "undef", nil
	case *parser.BlockExpr:
		if err := em.emitStmts(ee.Stmts); err != nil {
			return "", err
		}
		return "undef", nil
	case *parser.OldExpr:
		em.wi(`; TODO: old()`)
		return "undef", nil
	}
	return "undef", nil
}

func (em *llEmitter) emitIdent(e *parser.IdentExpr) (string, error) {
	switch e.Tok.Lexeme {
	case "unit":
		return "undef", nil
	case "true":
		return "1", nil
	case "false":
		return "0", nil
	}
	// Global constant.
	if ty, ok := em.res.Consts[e.Tok.Lexeme]; ok {
		t := em.fresh()
		em.wif(`%s = load %s, ptr @%s`, t, em.llType(ty), e.Tok.Lexeme)
		return t, nil
	}
	// Local variable.
	if addr, ok := em.locals[e.Tok.Lexeme]; ok {
		ty := em.localTypes[e.Tok.Lexeme]
		if isVoidTy(ty) {
			return "undef", nil
		}
		t := em.fresh()
		em.wif(`%s = load %s, ptr %s`, t, em.llType(ty), addr)
		return t, nil
	}
	// Function reference.
	if _, ok := em.res.FnSigs[e.Tok.Lexeme]; ok {
		return "@" + e.Tok.Lexeme, nil
	}
	return "undef", fmt.Errorf("emit_llvm: undefined %q", e.Tok.Lexeme)
}

func (em *llEmitter) emitBinary(e *parser.BinaryExpr) (string, error) {
	left, err := em.emitExpr(e.Left)
	if err != nil {
		return "", err
	}
	right, err := em.emitExpr(e.Right)
	if err != nil {
		return "", err
	}
	ty := em.res.ExprTypes[e.Left]
	if ty == nil {
		ty = typeck.TI64
	}
	llTy := em.llType(ty)
	t := em.fresh()
	op := e.Op.Lexeme

	switch op {
	case "+":
		if isFloatTy(ty) {
			em.wif(`%s = fadd %s %s, %s`, t, llTy, left, right)
		} else {
			em.wif(`%s = add %s %s, %s`, t, llTy, left, right)
		}
	case "-":
		if isFloatTy(ty) {
			em.wif(`%s = fsub %s %s, %s`, t, llTy, left, right)
		} else {
			em.wif(`%s = sub %s %s, %s`, t, llTy, left, right)
		}
	case "*":
		if isFloatTy(ty) {
			em.wif(`%s = fmul %s %s, %s`, t, llTy, left, right)
		} else {
			em.wif(`%s = mul %s %s, %s`, t, llTy, left, right)
		}
	case "/":
		if isFloatTy(ty) {
			em.wif(`%s = fdiv %s %s, %s`, t, llTy, left, right)
		} else if isSignedTy(ty) {
			em.wif(`%s = sdiv %s %s, %s`, t, llTy, left, right)
		} else {
			em.wif(`%s = udiv %s %s, %s`, t, llTy, left, right)
		}
	case "%":
		if isSignedTy(ty) {
			em.wif(`%s = srem %s %s, %s`, t, llTy, left, right)
		} else {
			em.wif(`%s = urem %s %s, %s`, t, llTy, left, right)
		}
	case "&":
		em.wif(`%s = and %s %s, %s`, t, llTy, left, right)
	case "|":
		em.wif(`%s = or %s %s, %s`, t, llTy, left, right)
	case "^":
		em.wif(`%s = xor %s %s, %s`, t, llTy, left, right)
	case "<<":
		em.wif(`%s = shl %s %s, %s`, t, llTy, left, right)
	case ">>":
		if isSignedTy(ty) {
			em.wif(`%s = ashr %s %s, %s`, t, llTy, left, right)
		} else {
			em.wif(`%s = lshr %s %s, %s`, t, llTy, left, right)
		}
	case "&&", "and":
		em.wif(`%s = and i1 %s, %s`, t, left, right)
	case "||", "or":
		em.wif(`%s = or i1 %s, %s`, t, left, right)
	case "==":
		if isFloatTy(ty) {
			em.wif(`%s = fcmp oeq %s %s, %s`, t, llTy, left, right)
		} else {
			em.wif(`%s = icmp eq %s %s, %s`, t, llTy, left, right)
		}
	case "!=":
		if isFloatTy(ty) {
			em.wif(`%s = fcmp one %s %s, %s`, t, llTy, left, right)
		} else {
			em.wif(`%s = icmp ne %s %s, %s`, t, llTy, left, right)
		}
	case "<":
		if isFloatTy(ty) {
			em.wif(`%s = fcmp olt %s %s, %s`, t, llTy, left, right)
		} else if isSignedTy(ty) {
			em.wif(`%s = icmp slt %s %s, %s`, t, llTy, left, right)
		} else {
			em.wif(`%s = icmp ult %s %s, %s`, t, llTy, left, right)
		}
	case "<=":
		if isFloatTy(ty) {
			em.wif(`%s = fcmp ole %s %s, %s`, t, llTy, left, right)
		} else if isSignedTy(ty) {
			em.wif(`%s = icmp sle %s %s, %s`, t, llTy, left, right)
		} else {
			em.wif(`%s = icmp ule %s %s, %s`, t, llTy, left, right)
		}
	case ">":
		if isFloatTy(ty) {
			em.wif(`%s = fcmp ogt %s %s, %s`, t, llTy, left, right)
		} else if isSignedTy(ty) {
			em.wif(`%s = icmp sgt %s %s, %s`, t, llTy, left, right)
		} else {
			em.wif(`%s = icmp ugt %s %s, %s`, t, llTy, left, right)
		}
	case ">=":
		if isFloatTy(ty) {
			em.wif(`%s = fcmp oge %s %s, %s`, t, llTy, left, right)
		} else if isSignedTy(ty) {
			em.wif(`%s = icmp sge %s %s, %s`, t, llTy, left, right)
		} else {
			em.wif(`%s = icmp uge %s %s, %s`, t, llTy, left, right)
		}
	default:
		return "undef", fmt.Errorf("emit_llvm: unknown binary op %q", op)
	}
	return t, nil
}

func (em *llEmitter) emitUnary(e *parser.UnaryExpr) (string, error) {
	val, err := em.emitExpr(e.Operand)
	if err != nil {
		return "", err
	}
	ty := em.res.ExprTypes[e.Operand]
	if ty == nil {
		ty = typeck.TI64
	}
	t := em.fresh()
	switch e.Op.Lexeme {
	case "-":
		if isFloatTy(ty) {
			em.wif(`%s = fneg %s %s`, t, em.llType(ty), val)
		} else {
			em.wif(`%s = sub %s 0, %s`, t, em.llType(ty), val)
		}
	case "!", "not":
		em.wif(`%s = xor i1 %s, 1`, t, val)
	case "&":
		addr, err := em.emitAddr(e.Operand)
		if err != nil {
			return "null", err
		}
		return addr, nil
	default:
		return "undef", fmt.Errorf("emit_llvm: unknown unary op %q", e.Op.Lexeme)
	}
	return t, nil
}

func (em *llEmitter) emitCall(e *parser.CallExpr) (string, error) {
	retTy := em.res.ExprTypes[e]
	if retTy == nil {
		retTy = typeck.TUnit
	}

	// Determine callee and signature.
	callee := ""
	var sig *typeck.FnType
	selfArg := ""
	selfTy := ""

	// Method call via MethodCalls map.
	if mn, ok := em.res.MethodCalls[e]; ok {
		callee = "@" + mn
		sig = em.res.FnSigs[mn]
		// Emit self as first arg.
		if fe, ok2 := e.Fn.(*parser.FieldExpr); ok2 {
			recv, err := em.emitExpr(fe.Receiver)
			if err != nil {
				return "", err
			}
			recvTy := em.res.ExprTypes[fe.Receiver]
			selfArg = recv
			if recvTy != nil {
				selfTy = em.llType(recvTy)
			}
		}
	}

	// Generic instance.
	if callee == "" {
		if gi, ok := em.res.CallSiteGeneric[e.Fn]; ok {
			callee = "@" + gi.MangledName
			sig = gi.Sig
		}
	}

	// Built-in collection operations (vec_push, vec_pop, ring_push_back, etc.).
	if callee == "" {
		if id, ok := e.Fn.(*parser.IdentExpr); ok {
			if result, handled, berr := em.emitBuiltinCall(e, id.Tok.Lexeme); handled {
				return result, berr
			}
		}
	}

	// Direct named function.
	if callee == "" {
		if id, ok := e.Fn.(*parser.IdentExpr); ok {
			name := id.Tok.Lexeme
			if s, ok2 := em.res.FnSigs[name]; ok2 {
				callee = "@" + name
				sig = s
			}
		}
	}

	// Lambda fat-pointer call: local variable with FnType.
	isLambdaCall := false
	var lambdaEnvReg string
	if callee == "" {
		if id, ok := e.Fn.(*parser.IdentExpr); ok {
			if _, isLocal := em.locals[id.Tok.Lexeme]; isLocal {
				if callTy, ok2 := em.res.ExprTypes[e.Fn]; ok2 {
					if _, isFn := callTy.(*typeck.FnType); isFn {
						fatAddr := em.locals[id.Tok.Lexeme]
						fatReg := em.fresh()
						em.wif(`%s = load ptr, ptr %s`, fatReg, fatAddr)
						fnSlot := em.fresh()
						envSlot := em.fresh()
						fnPtr := em.fresh()
						envPtr := em.fresh()
						em.wif(`%s = getelementptr { ptr, ptr }, ptr %s, i32 0, i32 0`, fnSlot, fatReg)
						em.wif(`%s = getelementptr { ptr, ptr }, ptr %s, i32 0, i32 1`, envSlot, fatReg)
						em.wif(`%s = load ptr, ptr %s`, fnPtr, fnSlot)
						em.wif(`%s = load ptr, ptr %s`, envPtr, envSlot)
						callee = fnPtr
						lambdaEnvReg = envPtr
						isLambdaCall = true
					}
				}
			}
		}
	}

	// Indirect call.
	if callee == "" {
		fnVal, err := em.emitExpr(e.Fn)
		if err != nil {
			return "", err
		}
		callee = fnVal
	}
	_ = isLambdaCall

	var argStrs []string
	if selfArg != "" {
		argStrs = append(argStrs, selfTy+" "+selfArg)
	}
	selfOffset := 0
	if selfArg != "" && sig != nil {
		selfOffset = 1
	}
	for i, arg := range e.Args {
		argVal, err := em.emitExpr(arg)
		if err != nil {
			return "", err
		}
		var llArgTy string
		if sig != nil && i+selfOffset < len(sig.Params) {
			llArgTy = em.llType(sig.Params[i+selfOffset])
		} else {
			argTy := em.res.ExprTypes[arg]
			if argTy != nil {
				llArgTy = em.llType(argTy)
			} else {
				llArgTy = "ptr"
			}
		}
		argStrs = append(argStrs, llArgTy+" "+argVal)
	}

	// Lambda calls append the env pointer as the last argument.
	if isLambdaCall && lambdaEnvReg != "" {
		argStrs = append(argStrs, "ptr "+lambdaEnvReg)
	}

	argList := strings.Join(argStrs, ", ")
	if isVoidTy(retTy) {
		em.wif(`call void %s(%s)`, callee, argList)
		return "undef", nil
	}
	t := em.fresh()
	em.wif(`%s = call %s %s(%s)`, t, em.llType(retTy), callee, argList)
	return t, nil
}

// emitBuiltinCall handles vec/ring/map/set built-in free functions.
// Returns (result, true, err) when handled; ("", false, nil) when not a known builtin.
func (em *llEmitter) emitBuiltinCall(e *parser.CallExpr, name string) (string, bool, error) {
	switch name {
	// ── vec ──────────────────────────────────────────────────────────────────

	case "vec_new":
		// vec_new() → zeroinitializer %_cnd_vec (empty vec, no allocation)
		return "zeroinitializer", true, nil

	case "vec_len":
		// vec_len(v) → extractvalue %_cnd_vec v, 1
		if len(e.Args) < 1 {
			return "undef", true, nil
		}
		v, err := em.emitExpr(e.Args[0])
		if err != nil {
			return "undef", true, err
		}
		t := em.fresh()
		em.wif(`%s = extractvalue %%_cnd_vec %s, 1`, t, v)
		return t, true, nil

	case "vec_push":
		// vec_push(v, elem): grow if needed, append elem, update v in-place via its alloca.
		if len(e.Args) < 2 {
			return "undef", true, nil
		}
		vecTy, ok := em.res.ExprTypes[e.Args[0]].(*typeck.GenType)
		if !ok || len(vecTy.Params) == 0 {
			return "undef", true, nil
		}
		elemTy := vecTy.Params[0]
		llElem := em.llType(elemTy)

		vecAddr, err := em.emitAddr(e.Args[0])
		if err != nil {
			return "undef", true, err
		}
		elemVal, err := em.emitExpr(e.Args[1])
		if err != nil {
			return "undef", true, err
		}

		// GEP into alloca to get slots for data, len, cap.
		dataSlot := em.fresh()
		lenSlot := em.fresh()
		capSlot := em.fresh()
		em.wif(`%s = getelementptr %%_cnd_vec, ptr %s, i32 0, i32 0`, dataSlot, vecAddr)
		em.wif(`%s = getelementptr %%_cnd_vec, ptr %s, i32 0, i32 1`, lenSlot, vecAddr)
		em.wif(`%s = getelementptr %%_cnd_vec, ptr %s, i32 0, i32 2`, capSlot, vecAddr)
		dataReg := em.fresh()
		lenReg := em.fresh()
		capReg := em.fresh()
		em.wif(`%s = load ptr, ptr %s`, dataReg, dataSlot)
		em.wif(`%s = load i64, ptr %s`, lenReg, lenSlot)
		em.wif(`%s = load i64, ptr %s`, capReg, capSlot)

		// If len == cap, grow.
		needGrow := em.fresh()
		growLbl := em.freshBlk("vec.grow")
		storeLbl := em.freshBlk("vec.store")
		em.wif(`%s = icmp eq i64 %s, %s`, needGrow, lenReg, capReg)
		em.wif(`br i1 %s, label %%%s, label %%%s`, needGrow, growLbl, storeLbl)

		em.lbl(growLbl)
		// new_cap = max(cap * 2, 4)
		newCap1 := em.fresh()
		newCap := em.fresh()
		isSmall := em.fresh()
		em.wif(`%s = mul i64 %s, 2`, newCap1, capReg)
		em.wif(`%s = icmp ult i64 %s, 4`, isSmall, newCap1)
		em.wif(`%s = select i1 %s, i64 4, i64 %s`, newCap, isSmall, newCap1)
		// sizeof(elemTy) via GEP-from-null.
		szPtr := em.fresh()
		szVal := em.fresh()
		em.wif(`%s = getelementptr %s, ptr null, i64 1`, szPtr, llElem)
		em.wif(`%s = ptrtoint ptr %s to i64`, szVal, szPtr)
		allocSz := em.fresh()
		newData := em.fresh()
		em.wif(`%s = mul i64 %s, %s`, allocSz, newCap, szVal)
		em.wif(`%s = call ptr @realloc(ptr %s, i64 %s)`, newData, dataReg, allocSz)
		// Write new data ptr and cap back into the alloca.
		em.wif(`store ptr %s, ptr %s`, newData, dataSlot)
		em.wif(`store i64 %s, ptr %s`, newCap, capSlot)
		em.wif(`br label %%%s`, storeLbl)

		// Store element at data[len], increment len.
		em.lbl(storeLbl)
		actualData := em.fresh()
		em.wif(`%s = load ptr, ptr %s`, actualData, dataSlot)
		ep := em.fresh()
		em.wif(`%s = getelementptr %s, ptr %s, i64 %s`, ep, llElem, actualData, lenReg)
		em.wif(`store %s %s, ptr %s`, llElem, elemVal, ep)
		newLen := em.fresh()
		em.wif(`%s = add i64 %s, 1`, newLen, lenReg)
		em.wif(`store i64 %s, ptr %s`, newLen, lenSlot)
		return "undef", true, nil

	case "vec_pop":
		// vec_pop(v): decrement len, return last element (UB if empty — matches C runtime).
		if len(e.Args) < 1 {
			return "undef", true, nil
		}
		vecTy, ok := em.res.ExprTypes[e.Args[0]].(*typeck.GenType)
		if !ok || len(vecTy.Params) == 0 {
			return "undef", true, nil
		}
		elemTy := vecTy.Params[0]
		llElem := em.llType(elemTy)

		vecAddr, err := em.emitAddr(e.Args[0])
		if err != nil {
			return "undef", true, err
		}
		dataSlot := em.fresh()
		lenSlot := em.fresh()
		em.wif(`%s = getelementptr %%_cnd_vec, ptr %s, i32 0, i32 0`, dataSlot, vecAddr)
		em.wif(`%s = getelementptr %%_cnd_vec, ptr %s, i32 0, i32 1`, lenSlot, vecAddr)
		dataReg := em.fresh()
		lenReg := em.fresh()
		em.wif(`%s = load ptr, ptr %s`, dataReg, dataSlot)
		em.wif(`%s = load i64, ptr %s`, lenReg, lenSlot)
		newLen := em.fresh()
		em.wif(`%s = sub i64 %s, 1`, newLen, lenReg)
		em.wif(`store i64 %s, ptr %s`, newLen, lenSlot)
		ep := em.fresh()
		elem := em.fresh()
		em.wif(`%s = getelementptr %s, ptr %s, i64 %s`, ep, llElem, dataReg, newLen)
		em.wif(`%s = load %s, ptr %s`, elem, llElem, ep)
		return elem, true, nil

	// ── ring ─────────────────────────────────────────────────────────────────

	case "ring_new":
		return "zeroinitializer", true, nil

	case "ring_len":
		if len(e.Args) < 1 {
			return "undef", true, nil
		}
		v, err := em.emitExpr(e.Args[0])
		if err != nil {
			return "undef", true, err
		}
		t := em.fresh()
		// %_cnd_ring = { ptr, i64 cap, i64 head, i64 len } → index 3
		em.wif(`%s = extractvalue %%_cnd_ring %s, 3`, t, v)
		return t, true, nil

	case "ring_push_back":
		// ring_push_back(r, elem): append to tail; grow+linearize if full.
		// %_cnd_ring = { ptr _data, i64 _cap, i64 _head, i64 _len }
		if len(e.Args) < 2 {
			return "undef", true, nil
		}
		ringTy, ok := em.res.ExprTypes[e.Args[0]].(*typeck.GenType)
		if !ok || len(ringTy.Params) == 0 {
			return "undef", true, nil
		}
		elemTy := ringTy.Params[0]
		llElem := em.llType(elemTy)

		ringAddr, err := em.emitAddr(e.Args[0])
		if err != nil {
			return "undef", true, err
		}
		elemVal, err := em.emitExpr(e.Args[1])
		if err != nil {
			return "undef", true, err
		}

		dataSlot := em.fresh()
		capSlot := em.fresh()
		headSlot := em.fresh()
		lenSlot := em.fresh()
		em.wif(`%s = getelementptr %%_cnd_ring, ptr %s, i32 0, i32 0`, dataSlot, ringAddr)
		em.wif(`%s = getelementptr %%_cnd_ring, ptr %s, i32 0, i32 1`, capSlot, ringAddr)
		em.wif(`%s = getelementptr %%_cnd_ring, ptr %s, i32 0, i32 2`, headSlot, ringAddr)
		em.wif(`%s = getelementptr %%_cnd_ring, ptr %s, i32 0, i32 3`, lenSlot, ringAddr)
		dataReg := em.fresh()
		capReg := em.fresh()
		headReg := em.fresh()
		rlenReg := em.fresh()
		em.wif(`%s = load ptr, ptr %s`, dataReg, dataSlot)
		em.wif(`%s = load i64, ptr %s`, capReg, capSlot)
		em.wif(`%s = load i64, ptr %s`, headReg, headSlot)
		em.wif(`%s = load i64, ptr %s`, rlenReg, lenSlot)

		// Grow if len == cap.
		needGrow := em.fresh()
		growLbl := em.freshBlk("ring.grow")
		copyHdr := em.freshBlk("ring.copy.hdr")
		copyBody := em.freshBlk("ring.copy.body")
		copyDone := em.freshBlk("ring.copy.done")
		storeLbl := em.freshBlk("ring.store")
		em.wif(`%s = icmp eq i64 %s, %s`, needGrow, rlenReg, capReg)
		em.wif(`br i1 %s, label %%%s, label %%%s`, needGrow, growLbl, storeLbl)

		em.lbl(growLbl)
		newCap1 := em.fresh()
		newCap := em.fresh()
		isSmall := em.fresh()
		em.wif(`%s = mul i64 %s, 2`, newCap1, capReg)
		em.wif(`%s = icmp ult i64 %s, 4`, isSmall, newCap1)
		em.wif(`%s = select i1 %s, i64 4, i64 %s`, newCap, isSmall, newCap1)
		szPtr := em.fresh()
		szVal := em.fresh()
		em.wif(`%s = getelementptr %s, ptr null, i64 1`, szPtr, llElem)
		em.wif(`%s = ptrtoint ptr %s to i64`, szVal, szPtr)
		allocSz := em.fresh()
		newBuf := em.fresh()
		em.wif(`%s = mul i64 %s, %s`, allocSz, newCap, szVal)
		em.wif(`%s = call ptr @malloc(i64 %s)`, newBuf, allocSz)

		// Copy all elements in logical order: newBuf[i] = data[(head+i)%cap] for i in 0..rlen.
		copyI := em.fresh()
		em.wif(`%s = alloca i64`, copyI)
		em.wif(`store i64 0, ptr %s`, copyI)
		em.wif(`br label %%%s`, copyHdr)

		em.lbl(copyHdr)
		ci := em.fresh()
		copyDone1 := em.fresh()
		em.wif(`%s = load i64, ptr %s`, ci, copyI)
		em.wif(`%s = icmp uge i64 %s, %s`, copyDone1, ci, rlenReg)
		em.wif(`br i1 %s, label %%%s, label %%%s`, copyDone1, copyDone, copyBody)

		em.lbl(copyBody)
		srcIdx := em.fresh()
		srcMod := em.fresh()
		srcPtr := em.fresh()
		srcElem := em.fresh()
		dstPtr := em.fresh()
		em.wif(`%s = add i64 %s, %s`, srcIdx, headReg, ci)
		em.wif(`%s = urem i64 %s, %s`, srcMod, srcIdx, capReg)
		em.wif(`%s = getelementptr %s, ptr %s, i64 %s`, srcPtr, llElem, dataReg, srcMod)
		em.wif(`%s = load %s, ptr %s`, srcElem, llElem, srcPtr)
		em.wif(`%s = getelementptr %s, ptr %s, i64 %s`, dstPtr, llElem, newBuf, ci)
		em.wif(`store %s %s, ptr %s`, llElem, srcElem, dstPtr)
		ciNext := em.fresh()
		em.wif(`%s = add i64 %s, 1`, ciNext, ci)
		em.wif(`store i64 %s, ptr %s`, ciNext, copyI)
		em.wif(`br label %%%s`, copyHdr)

		em.lbl(copyDone)
		// Update ring: new buf, new cap, head=0.
		em.wif(`store ptr %s, ptr %s`, newBuf, dataSlot)
		em.wif(`store i64 %s, ptr %s`, newCap, capSlot)
		em.wif(`store i64 0, ptr %s`, headSlot)
		em.wif(`br label %%%s`, storeLbl)

		// Append at tail = (head + len) % cap — reload in case we just grew.
		em.lbl(storeLbl)
		curData := em.fresh()
		curCap := em.fresh()
		curHead := em.fresh()
		curLen := em.fresh()
		em.wif(`%s = load ptr, ptr %s`, curData, dataSlot)
		em.wif(`%s = load i64, ptr %s`, curCap, capSlot)
		em.wif(`%s = load i64, ptr %s`, curHead, headSlot)
		em.wif(`%s = load i64, ptr %s`, curLen, lenSlot)
		tailIdx := em.fresh()
		tailMod := em.fresh()
		tailPtr := em.fresh()
		em.wif(`%s = add i64 %s, %s`, tailIdx, curHead, curLen)
		em.wif(`%s = urem i64 %s, %s`, tailMod, tailIdx, curCap)
		em.wif(`%s = getelementptr %s, ptr %s, i64 %s`, tailPtr, llElem, curData, tailMod)
		em.wif(`store %s %s, ptr %s`, llElem, elemVal, tailPtr)
		newRLen := em.fresh()
		em.wif(`%s = add i64 %s, 1`, newRLen, curLen)
		em.wif(`store i64 %s, ptr %s`, newRLen, lenSlot)
		return "undef", true, nil

	// ── box built-in functions ────────────────────────────────────────────────

	case "box_new":
		// box_new(val) → malloc sizeof(T), store val, return ptr
		if len(e.Args) < 1 {
			return "undef", true, nil
		}
		innerTy := em.res.ExprTypes[e.Args[0]]
		if innerTy == nil {
			return "undef", true, nil
		}
		llInner := em.llType(innerTy)
		valReg, err := em.emitExpr(e.Args[0])
		if err != nil {
			return "undef", true, err
		}
		sizePtr := em.fresh()
		sizeInt := em.fresh()
		rawPtr := em.fresh()
		em.wif(`%s = getelementptr %s, ptr null, i64 1`, sizePtr, llInner)
		em.wif(`%s = ptrtoint ptr %s to i64`, sizeInt, sizePtr)
		em.wif(`%s = call ptr @malloc(i64 %s)`, rawPtr, sizeInt)
		em.wif(`store %s %s, ptr %s`, llInner, valReg, rawPtr)
		return rawPtr, true, nil

	case "box_deref":
		// box_deref(b) → load T from ptr
		if len(e.Args) < 1 {
			return "undef", true, nil
		}
		argTy, ok := em.res.ExprTypes[e.Args[0]].(*typeck.GenType)
		if !ok || argTy.Con != "box" || len(argTy.Params) == 0 {
			return "undef", true, nil
		}
		llInner := em.llType(argTy.Params[0])
		ptrReg, err := em.emitExpr(e.Args[0])
		if err != nil {
			return "undef", true, err
		}
		result := em.fresh()
		em.wif(`%s = load %s, ptr %s`, result, llInner, ptrReg)
		return result, true, nil

	case "box_drop":
		// box_drop(b) → free(ptr)
		if len(e.Args) < 1 {
			return "undef", true, nil
		}
		ptrReg, err := em.emitExpr(e.Args[0])
		if err != nil {
			return "undef", true, err
		}
		em.wif(`call void @free(ptr %s)`, ptrReg)
		return "undef", true, nil

	// ── map / set stubs (not yet implemented in LLVM backend) ────────────────

	case "map_new", "map_insert", "map_get", "map_remove", "map_len", "map_contains",
		"set_new", "set_add", "set_remove", "set_contains", "set_len":
		em.wif(`; TODO: %s not yet implemented in LLVM backend`, name)
		return "undef", true, nil

	// ── ring ops not yet implemented ─────────────────────────────────────────

	case "ring_pop_front":
		em.wif(`; TODO: ring_pop_front not yet implemented in LLVM backend`)
		return "undef", true, nil
	}

	return "", false, nil
}

func (em *llEmitter) emitField(e *parser.FieldExpr) (string, error) {
	recvTy := em.res.ExprTypes[e.Receiver]
	if recvTy == nil {
		return "undef", nil
	}
	st, ok := recvTy.(*typeck.StructType)
	if !ok {
		// Not a struct field — might be a method reference (handled by call).
		return "undef", nil
	}
	recvVal, err := em.emitExpr(e.Receiver)
	if err != nil {
		return "", err
	}
	idx := em.fieldIdx(st.Name, e.Field.Lexeme)
	t := em.fresh()
	em.wif(`%s = extractvalue %s %s, %d`, t, em.llType(recvTy), recvVal, idx)
	return t, nil
}

func (em *llEmitter) emitStructLit(e *parser.StructLitExpr) (string, error) {
	stName := e.TypeName.Lexeme
	st := em.res.Structs[stName]
	if st == nil {
		return "undef", fmt.Errorf("emit_llvm: unknown struct %q", stName)
	}
	fields := em.structFields[stName]
	llTy := "%" + stName

	// Compute provided values.
	provided := make(map[string]string)
	for _, fi := range e.Fields {
		val, err := em.emitExpr(fi.Value)
		if err != nil {
			return "", err
		}
		provided[fi.Name.Lexeme] = val
	}

	// Handle struct update: extract missing fields from base.
	baseVals := make(map[string]string)
	if e.Base != nil {
		baseVal, err := em.emitExpr(e.Base)
		if err != nil {
			return "", err
		}
		for i, fname := range fields {
			if _, ok := provided[fname]; !ok {
				ext := em.fresh()
				em.wif(`%s = extractvalue %s %s, %d`, ext, llTy, baseVal, i)
				baseVals[fname] = ext
			}
		}
	}

	cur := "zeroinitializer"
	for i, fname := range fields {
		val := "zeroinitializer"
		if v, ok := provided[fname]; ok {
			val = v
		} else if v, ok := baseVals[fname]; ok {
			val = v
		}
		t := em.fresh()
		em.wif(`%s = insertvalue %s %s, %s %s, %d`,
			t, llTy, cur, em.llType(st.Fields[fname]), val, i)
		cur = t
	}
	return cur, nil
}

func (em *llEmitter) emitCast(e *parser.CastExpr) (string, error) {
	srcTy := em.res.ExprTypes[e.X]
	dstTy := em.res.ExprTypes[e]
	val, err := em.emitExpr(e.X)
	if err != nil {
		return "", err
	}
	if srcTy == nil || dstTy == nil {
		return val, nil
	}
	srcLL := em.llType(srcTy)
	dstLL := em.llType(dstTy)
	if srcLL == dstLL {
		return val, nil
	}

	srcInt := typeck.IsIntType(srcTy) || srcTy == typeck.TBool
	dstInt := typeck.IsIntType(dstTy) || dstTy == typeck.TBool
	srcF := isFloatTy(srcTy)
	dstF := isFloatTy(dstTy)

	t := em.fresh()
	switch {
	case srcInt && dstInt:
		srcBits := em.sizeBytes(srcTy) * 8
		dstBits := em.sizeBytes(dstTy) * 8
		if dstBits < srcBits {
			em.wif(`%s = trunc %s %s to %s`, t, srcLL, val, dstLL)
		} else if isSignedTy(srcTy) {
			em.wif(`%s = sext %s %s to %s`, t, srcLL, val, dstLL)
		} else {
			em.wif(`%s = zext %s %s to %s`, t, srcLL, val, dstLL)
		}
	case srcF && dstF:
		if em.sizeBytes(dstTy) < em.sizeBytes(srcTy) {
			em.wif(`%s = fptrunc %s %s to %s`, t, srcLL, val, dstLL)
		} else {
			em.wif(`%s = fpext %s %s to %s`, t, srcLL, val, dstLL)
		}
	case srcF && dstInt:
		if isSignedTy(dstTy) {
			em.wif(`%s = fptosi %s %s to %s`, t, srcLL, val, dstLL)
		} else {
			em.wif(`%s = fptoui %s %s to %s`, t, srcLL, val, dstLL)
		}
	case srcInt && dstF:
		if isSignedTy(srcTy) {
			em.wif(`%s = sitofp %s %s to %s`, t, srcLL, val, dstLL)
		} else {
			em.wif(`%s = uitofp %s %s to %s`, t, srcLL, val, dstLL)
		}
	default:
		em.wif(`%s = bitcast %s %s to %s`, t, srcLL, val, dstLL)
	}
	return t, nil
}

func (em *llEmitter) emitPath(e *parser.PathExpr) (string, error) {
	// Enum variant constructor: Shape::Circle
	ty := em.res.ExprTypes[e]
	if ty == nil {
		return "undef", nil
	}
	et, ok := ty.(*typeck.EnumType)
	if !ok {
		return "undef", nil
	}
	v := et.ByName[e.Tail.Lexeme]
	if v == nil {
		return "undef", fmt.Errorf("emit_llvm: unknown variant %q", e.Tail.Lexeme)
	}
	llTy := em.llType(ty)
	t := em.fresh()
	em.wif(`%s = insertvalue %s zeroinitializer, i32 %d, 0`, t, llTy, v.Tag)
	return t, nil
}

func (em *llEmitter) emitIndex(e *parser.IndexExpr) (string, error) {
	coll, err := em.emitExpr(e.Collection)
	if err != nil {
		return "", err
	}
	idx, err := em.emitExpr(e.Index)
	if err != nil {
		return "", err
	}
	elemTy := em.res.ExprTypes[e]
	if elemTy == nil {
		elemTy = typeck.TI64
	}

	collTy := em.res.ExprTypes[e.Collection]
	if gen, ok := collTy.(*typeck.GenType); ok {
		switch gen.Con {
		case "vec":
			dataReg := em.fresh()
			em.wif(`%s = extractvalue %%_cnd_vec %s, 0`, dataReg, coll)
			ptr := em.fresh()
			em.wif(`%s = getelementptr %s, ptr %s, i64 %s`, ptr, em.llType(elemTy), dataReg, idx)
			loaded := em.fresh()
			em.wif(`%s = load %s, ptr %s`, loaded, em.llType(elemTy), ptr)
			return loaded, nil
		case "ring":
			dataReg := em.fresh()
			capReg := em.fresh()
			headReg := em.fresh()
			em.wif(`%s = extractvalue %%_cnd_ring %s, 0`, dataReg, coll)
			em.wif(`%s = extractvalue %%_cnd_ring %s, 1`, capReg, coll)
			em.wif(`%s = extractvalue %%_cnd_ring %s, 2`, headReg, coll)
			sumReg := em.fresh()
			modReg := em.fresh()
			em.wif(`%s = add i64 %s, %s`, sumReg, headReg, idx)
			em.wif(`%s = urem i64 %s, %s`, modReg, sumReg, capReg)
			ptr := em.fresh()
			em.wif(`%s = getelementptr %s, ptr %s, i64 %s`, ptr, em.llType(elemTy), dataReg, modReg)
			loaded := em.fresh()
			em.wif(`%s = load %s, ptr %s`, loaded, em.llType(elemTy), ptr)
			return loaded, nil
		}
	}

	// Raw pointer fallback (str[i] → u8, etc.)
	ptr := em.fresh()
	em.wif(`%s = getelementptr %s, ptr %s, i64 %s`, ptr, em.llType(elemTy), coll, idx)
	loaded := em.fresh()
	em.wif(`%s = load %s, ptr %s`, loaded, em.llType(elemTy), ptr)
	return loaded, nil
}

func (em *llEmitter) emitVecLit(e *parser.VecLitExpr) (string, error) {
	ty := em.res.ExprTypes[e]
	gen, ok := ty.(*typeck.GenType)
	if !ok || gen.Con != "vec" || len(gen.Params) == 0 {
		return "null", fmt.Errorf("emit_llvm: vec literal has non-vec type %v", ty)
	}
	elemTy := gen.Params[0]
	n := len(e.Elems)

	// Empty vec: return zeroinitializer directly (no allocation needed).
	if n == 0 {
		return "zeroinitializer", nil
	}

	// sizeof(elemTy) via GEP-from-null trick — correct for any alignment.
	szPtr := em.fresh()
	szVal := em.fresh()
	em.wif(`%s = getelementptr %s, ptr null, i64 1`, szPtr, em.llType(elemTy))
	em.wif(`%s = ptrtoint ptr %s to i64`, szVal, szPtr)

	// total = sizeof(elemTy) * n
	totalReg := em.fresh()
	em.wif(`%s = mul i64 %s, %d`, totalReg, szVal, n)

	// data = malloc(total)
	dataReg := em.fresh()
	em.wif(`%s = call ptr @malloc(i64 %s)`, dataReg, totalReg)

	// Store each element.
	for i, elem := range e.Elems {
		val, err := em.emitExpr(elem)
		if err != nil {
			return "", err
		}
		epReg := em.fresh()
		em.wif(`%s = getelementptr %s, ptr %s, i64 %d`, epReg, em.llType(elemTy), dataReg, i)
		em.wif(`store %s %s, ptr %s`, em.llType(elemTy), val, epReg)
	}

	// Build %_cnd_vec { ptr data, i64 len, i64 cap }.
	v1 := em.fresh()
	v2 := em.fresh()
	v3 := em.fresh()
	em.wif(`%s = insertvalue %%_cnd_vec zeroinitializer, ptr %s, 0`, v1, dataReg)
	em.wif(`%s = insertvalue %%_cnd_vec %s, i64 %d, 1`, v2, v1, n)
	em.wif(`%s = insertvalue %%_cnd_vec %s, i64 %d, 2`, v3, v2, n)
	return v3, nil
}

func (em *llEmitter) emitTupleLit(e *parser.TupleLitExpr) (string, error) {
	ty := em.res.ExprTypes[e]
	tt, ok := ty.(*typeck.TupleType)
	if !ok {
		return "undef", nil
	}
	llTy := em.llType(ty)
	cur := "zeroinitializer"
	for i, elem := range e.Elems {
		val, err := em.emitExpr(elem)
		if err != nil {
			return "", err
		}
		t := em.fresh()
		em.wif(`%s = insertvalue %s %s, %s %s, %d`, t, llTy, cur, em.llType(tt.Elems[i]), val, i)
		cur = t
	}
	return cur, nil
}

func (em *llEmitter) emitMatch(e *parser.MatchExpr) (string, error) {
	matchVal, err := em.emitExpr(e.X)
	if err != nil {
		return "", err
	}
	matchTy := em.res.ExprTypes[e.X]
	retTy := em.res.ExprTypes[e]
	if retTy == nil {
		retTy = typeck.TUnit
	}

	mergeLbl := em.freshBlk("match.merge")

	var resultAddr string
	if !isVoidTy(retTy) {
		resultAddr = em.fresh()
		em.wif(`%s = alloca %s`, resultAddr, em.llType(retTy))
	}

	et, isEnum := matchTy.(*typeck.EnumType)

	if isEnum {
		// Extract tag.
		tagReg := em.fresh()
		em.wif(`%s = extractvalue %s %s, 0`, tagReg, em.llType(matchTy), matchVal)

		armLabels := make([]string, len(e.Arms))
		for i := range e.Arms {
			armLabels[i] = em.freshBlk("match.arm")
		}

		// Pre-pass: find wildcard arm so the switch default is correct.
		defaultLbl := mergeLbl
		for i, arm := range e.Arms {
			if id, ok := arm.Pattern.(*parser.IdentExpr); ok && id.Tok.Lexeme == "_" {
				defaultLbl = armLabels[i]
			}
			// bare identifier binding (e.g. x => ...) also acts as wildcard
			if id, ok := arm.Pattern.(*parser.IdentExpr); ok && id.Tok.Lexeme != "_" {
				_ = i // handled as wildcard default
				defaultLbl = armLabels[i]
			}
		}

		em.wif(`switch i32 %s, label %%%s [`, tagReg, defaultLbl)
		for i, arm := range e.Arms {
			switch pat := arm.Pattern.(type) {
			case *parser.PathExpr:
				// Unit variant: Enum::Variant
				if v := et.ByName[pat.Tail.Lexeme]; v != nil {
					em.wif(`  i32 %d, label %%%s`, v.Tag, armLabels[i])
				}
			case *parser.CallExpr:
				// Payload variant: Enum::Variant(bindings...)
				if path, ok := pat.Fn.(*parser.PathExpr); ok {
					if v := et.ByName[path.Tail.Lexeme]; v != nil {
						em.wif(`  i32 %d, label %%%s`, v.Tag, armLabels[i])
					}
				}
			}
		}
		em.wi(`]`)

		// We may need to read payload fields — store matchVal to a temp alloca once.
		var matchAddr string
		for _, arm := range e.Arms {
			if _, ok := arm.Pattern.(*parser.CallExpr); ok {
				matchAddr = em.fresh()
				em.wif(`%s = alloca %s`, matchAddr, em.llType(matchTy))
				em.wif(`store %s %s, ptr %s`, em.llType(matchTy), matchVal, matchAddr)
				break
			}
		}

		for i, arm := range e.Arms {
			em.lbl(armLabels[i])

			// Bind payload variables for Enum::Variant(x, y) patterns.
			var boundNames []string
			if call, ok := arm.Pattern.(*parser.CallExpr); ok {
				if path, ok2 := call.Fn.(*parser.PathExpr); ok2 {
					if vd := et.ByName[path.Tail.Lexeme]; vd != nil {
						byteOffset := 0
						for j, arg := range call.Args {
							fieldTy := vd.Fields[j]
							if id, ok3 := arg.(*parser.IdentExpr); ok3 && id.Tok.Lexeme != "_" {
								varName := id.Tok.Lexeme
								fieldPtr := em.fresh()
								// GEP into payload byte array at byteOffset.
								em.wif(`%s = getelementptr %s, ptr %s, i32 0, i32 1, i32 %d`,
									fieldPtr, em.llType(matchTy), matchAddr, byteOffset)
								varAddr := em.fresh()
								em.wif(`%s = alloca %s`, varAddr, em.llType(fieldTy))
								loaded := em.fresh()
								em.wif(`%s = load %s, ptr %s`, loaded, em.llType(fieldTy), fieldPtr)
								em.wif(`store %s %s, ptr %s`, em.llType(fieldTy), loaded, varAddr)
								em.locals[varName] = varAddr
								em.localTypes[varName] = fieldTy
								boundNames = append(boundNames, varName)
							}
							byteOffset += em.sizeBytes(fieldTy)
						}
					}
				}
			}

			armVal, err := em.emitExpr(arm.Body)
			if err != nil {
				return "", err
			}

			// Remove arm-scoped bindings.
			for _, name := range boundNames {
				delete(em.locals, name)
				delete(em.localTypes, name)
			}

			if resultAddr != "" && !isVoidTy(retTy) {
				em.wif(`store %s %s, ptr %s`, em.llType(retTy), armVal, resultAddr)
			}
			em.wif(`br label %%%s`, mergeLbl)
		}
	} else {
		// Non-enum match: equality comparison chain.
		for _, arm := range e.Arms {
			armLbl := em.freshBlk("match.arm")
			nextLbl := em.freshBlk("match.next")

			if id, ok := arm.Pattern.(*parser.IdentExpr); ok && id.Tok.Lexeme == "_" {
				// Wildcard — always matches.
				em.wif(`br label %%%s`, armLbl)
				em.lbl(armLbl)
			} else {
				patVal, err := em.emitExpr(arm.Pattern)
				if err != nil {
					return "", err
				}
				cmp := em.fresh()
				if matchTy != nil {
					em.wif(`%s = icmp eq %s %s, %s`, cmp, em.llType(matchTy), matchVal, patVal)
				} else {
					em.wif(`%s = icmp eq i64 %s, %s`, cmp, matchVal, patVal)
				}
				em.wif(`br i1 %s, label %%%s, label %%%s`, cmp, armLbl, nextLbl)
				em.lbl(armLbl)
			}

			armVal, err := em.emitExpr(arm.Body)
			if err != nil {
				return "", err
			}
			if resultAddr != "" && !isVoidTy(retTy) {
				em.wif(`store %s %s, ptr %s`, em.llType(retTy), armVal, resultAddr)
			}
			em.wif(`br label %%%s`, mergeLbl)
			em.lbl(nextLbl)
		}
		em.wif(`br label %%%s`, mergeLbl)
	}

	em.lbl(mergeLbl)
	if resultAddr != "" && !isVoidTy(retTy) {
		t := em.fresh()
		em.wif(`%s = load %s, ptr %s`, t, em.llType(retTy), resultAddr)
		return t, nil
	}
	return "undef", nil
}

func (em *llEmitter) emitMust(e *parser.MustExpr) (string, error) {
	// Simplified: just evaluate the operand.
	em.wi(`; must expr — simplified`)
	return em.emitExpr(e.X)
}

// ── lvalue helpers ─────────────────────────────────────────────────────────────

// emitAddr returns the alloca pointer for an lvalue expression.
func (em *llEmitter) emitAddr(e parser.Expr) (string, error) {
	switch ee := e.(type) {
	case *parser.IdentExpr:
		if addr, ok := em.locals[ee.Tok.Lexeme]; ok {
			return addr, nil
		}
		return "null", fmt.Errorf("emit_llvm: no alloca for %q", ee.Tok.Lexeme)
	case *parser.FieldExpr:
		recvAddr, err := em.emitAddr(ee.Receiver)
		if err != nil {
			// Materialize a temp.
			recvTy := em.res.ExprTypes[ee.Receiver]
			recvVal, err2 := em.emitExpr(ee.Receiver)
			if err2 != nil {
				return "null", err2
			}
			tmp := em.fresh()
			em.wif(`%s = alloca %s`, tmp, em.llType(recvTy))
			em.wif(`store %s %s, ptr %s`, em.llType(recvTy), recvVal, tmp)
			recvAddr = tmp
		}
		recvTy := em.res.ExprTypes[ee.Receiver]
		st, ok := recvTy.(*typeck.StructType)
		if !ok {
			return recvAddr, nil
		}
		idx := em.fieldIdx(st.Name, ee.Field.Lexeme)
		fptr := em.fresh()
		em.wif(`%s = getelementptr %%%s, ptr %s, i32 0, i32 %d`, fptr, st.Name, recvAddr, idx)
		return fptr, nil
	}
	// Materialize.
	ty := em.res.ExprTypes[e]
	if ty == nil {
		ty = typeck.TI64
	}
	val, err := em.emitExpr(e)
	if err != nil {
		return "null", err
	}
	tmp := em.fresh()
	em.wif(`%s = alloca %s`, tmp, em.llType(ty))
	em.wif(`store %s %s, ptr %s`, em.llType(ty), val, tmp)
	return tmp, nil
}

// fieldIdx returns the 0-based index of fieldName in the struct.
func (em *llEmitter) fieldIdx(structName, fieldName string) int {
	for i, f := range em.structFields[structName] {
		if f == fieldName {
			return i
		}
	}
	return 0
}
