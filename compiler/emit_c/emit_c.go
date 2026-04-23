// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/candor-core/candor

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

	"github.com/candor-core/candor/compiler/lexer"
	"github.com/candor-core/candor/compiler/parser"
	"github.com/candor-core/candor/compiler/typeck"
)

// Emit translates a type-checked Candor file to a C source string.
func Emit(file *parser.File, res *typeck.Result) (string, error) {
	e := &emitter{res: res, fileModule: fileModuleName(file)}
	if err := e.emitFile(file); err != nil {
		return "", err
	}
	return e.sb.String(), nil
}

// EmitAudit runs the C emitter with audit logging enabled. It returns the C
// source and a populated AuditLog. Call log.RenderMarkdown() for the report.
func EmitAudit(file *parser.File, res *typeck.Result, sourceName string) (string, *AuditLog, error) {
	log := &AuditLog{SourceFile: sourceName}
	e := &emitter{res: res, fileModule: fileModuleName(file), audit: log}
	if err := e.emitFile(file); err != nil {
		return "", nil, err
	}
	return e.sb.String(), log, nil
}

// fileModuleName returns the module name declared in a file, or "" for root.
func fileModuleName(f *parser.File) string {
	for _, d := range f.Decls {
		if md, ok := d.(*parser.ModuleDecl); ok {
			return md.Name.Lexeme
		}
	}
	return ""
}

// moduleCName returns the C-safe name for a declaration in the given module.
// For root-namespace declarations (mod == ""), the name is returned unchanged.
// For module-scoped declarations, the name is prefixed: "mod_name".
func moduleCName(mod, name string) string {
	if mod == "" {
		return name
	}
	return mod + "_" + name
}

// resLookupStruct finds a StructType in the result, preferring the current file's module.
func (e *emitter) resLookupStruct(name string) *typeck.StructType {
	if e.fileModule != "" {
		if st, ok := e.res.Structs[e.fileModule+"."+name]; ok {
			return st
		}
	}
	return e.res.Structs[name]
}

// resLookupEnum finds an EnumType in the result, preferring the current file's module.
func (e *emitter) resLookupEnum(name string) *typeck.EnumType {
	if e.fileModule != "" {
		if et, ok := e.res.Enums[e.fileModule+"."+name]; ok {
			return et
		}
	}
	return e.res.Enums[name]
}

// ── emitter ───────────────────────────────────────────────────────────────────

type emitter struct {
	sb         strings.Builder
	res        *typeck.Result
	fileModule string // module name of the file being emitted ("" = root)
	// current function context
	retIsUnit bool // true when emitting a fn returning unit (C void)
	isMain    bool // true when emitting the special main function
	tmpCount  int
	contracts []parser.ContractClause
	retType   typeck.Type
	inEnsures bool // true when emitting ensures expressions (result -> _cnd_result)
	oldVars   map[*parser.OldExpr]string // maps OldExpr node → C variable name

	emittedTypes  map[string]bool // tracks emitted structs/enums
	emittingTypes map[string]bool // detects cycles
	emitStack     []string        // DEBUG: call stack for cycle detection

	// declaredVars tracks C variable names already declared in the current
	// function scope. Re-declarations (same Candor name bound twice) are
	// emitted as plain assignments to avoid C "redefinition" errors.
	declaredVars map[string]bool

	// byRefCaptures holds variable names that are captured by reference in the
	// lambda currently being emitted. Reads emit (*name), writes emit (*name) = val.
	byRefCaptures map[string]bool

	// spawnTaskVar is non-empty when emitting a spawn thunk body.
	// It holds the C variable name of the _CndTask_T* pointer so that
	// return statements can store their value into _task->_result.
	spawnTaskVar string

	// namedFnTrampolines maps a Candor function name to its FnType for every
	// named function used as a first-class value.  The emitter generates a
	// thin C wrapper (trampoline) for each entry so the raw function pointer
	// can be stored in a _cnd_fn_T_T closure struct whose ._fn field has an
	// extra void* _env parameter.
	namedFnTrampolines map[string]*typeck.FnType

	// audit is non-nil when --emit=c-audit is active. Every Candor feature
	// that has no C equivalent logs an entry here during emission.
	audit *AuditLog

	// currentFnName tracks the enclosing function for audit entries.
	currentFnName string
}

func (e *emitter) freshTmp() string {
	e.tmpCount++
	return fmt.Sprintf("_cnd%d", e.tmpCount)
}

func (e *emitter) write(s string)              { e.sb.WriteString(s) }
func (e *emitter) writef(f string, a ...any)   { fmt.Fprintf(&e.sb, f, a...) }
func (e *emitter) writeln(s string)            { e.sb.WriteString(s); e.sb.WriteByte('\n') }

// collectNamedFnTrampolines scans ExprTypes for IdentExpr nodes that refer to
// user-defined named functions in a value context (FnType), and populates
// e.namedFnTrampolines so trampoline wrappers can be emitted before use.
// Only user-defined functions (declared in the file's AST) get trampolines;
// builtins and extern functions are skipped because they are not real C symbols.
func (e *emitter) collectNamedFnTrampolines(file *parser.File) {
	// Build a set of user-declared function names from the file.
	userFns := make(map[string]bool)
	for _, decl := range file.Decls {
		if fd, ok := decl.(*parser.FnDecl); ok {
			userFns[fd.Name.Lexeme] = true
		}
	}
	// Also include impl method names.
	for _, impl := range e.res.ImplDecls {
		for _, m := range impl.Methods {
			userFns[impl.TypeName.Lexeme+"_"+m.Name.Lexeme] = true
		}
	}

	e.namedFnTrampolines = make(map[string]*typeck.FnType)
	for expr, typ := range e.res.ExprTypes {
		ident, ok := expr.(*parser.IdentExpr)
		if !ok {
			continue
		}
		ft, isFn := typ.(*typeck.FnType)
		if !isFn {
			continue
		}
		name := ident.Tok.Lexeme
		if userFns[name] {
			e.namedFnTrampolines[name] = ft
		}
	}
}

// emitNamedFnTrampolines emits a thin C wrapper for each named function that
// is used as a first-class value.  The wrapper adapts the raw `ret fn(params)`
// signature to `ret fn(params..., void* _cnd_env)` expected by closure structs.
func (e *emitter) emitNamedFnTrampolines() error {
	for name, ft := range e.namedFnTrampolines {
		ret, err := e.cType(ft.Ret)
		if err != nil {
			return err
		}
		var paramDecls []string
		var paramNames []string
		for i, p := range ft.Params {
			ct, err := e.cType(p)
			if err != nil {
				return err
			}
			pName := fmt.Sprintf("_p%d", i)
			paramDecls = append(paramDecls, ct+" "+pName)
			paramNames = append(paramNames, pName)
		}
		paramDecls = append(paramDecls, "void* _cnd_env")
		allParams := strings.Join(paramDecls, ", ")
		callArgs := strings.Join(paramNames, ", ")
		if ret == "void" {
			e.writef("static void _cnd_tramp_%s(%s) { (void)_cnd_env; %s(%s); }\n",
				name, allParams, name, callArgs)
		} else {
			e.writef("static %s _cnd_tramp_%s(%s) { (void)_cnd_env; return %s(%s); }\n",
				ret, name, allParams, name, callArgs)
		}
	}
	return nil
}

// ── file ─────────────────────────────────────────────────────────────────────

func (e *emitter) emitFile(file *parser.File) error {
	// Pre-pass: find all named functions used as first-class values.
	e.collectNamedFnTrampolines(file)

	e.writeln("#include <stdint.h>")
	e.writeln("#include <stdio.h>")
	e.writeln("#include <stdlib.h>")
	e.writeln("#include <string.h>")
	e.writeln("#include <assert.h>")
	e.writeln("#include <math.h>")
	e.writeln("#include <time.h>")
	e.writeln("#include <ctype.h>")
	e.writeln("#ifdef _WIN32")
	e.writeln("#  include <windows.h>")
	e.writeln("#  include <direct.h>")
	e.writeln("#  define _cnd_mkdir(p) _mkdir(p)")
	e.writeln("#  include <io.h>")
	e.writeln("#  include <process.h>")
	e.writeln("#else")
	e.writeln("#  include <unistd.h>")
	e.writeln("#  include <dirent.h>")
	e.writeln("#  include <sys/stat.h>")
	e.writeln("#  include <sys/wait.h>")
	e.writeln("#  define _cnd_mkdir(p) mkdir(p, 0755)")
	e.writeln("#endif")
	if len(e.res.Spawns) > 0 {
		e.writeln("#include <pthread.h>")
	}
	if e.hasMmap() {
		e.writeln("#ifndef _WIN32")
		e.writeln("#  include <sys/mman.h>")
		e.writeln("#  include <fcntl.h>")
		e.writeln("#endif")
	}
	e.writeln("")
	e.emitRuntimeHelpers()
	e.writeln("")

	// Forward-declare all structs and enums first so they can reference each other via pointers.
	// Track the current module via ModuleDecl boundary markers inserted by mergeFiles.
	{
		curMod := ""
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *parser.ModuleDecl:
				curMod = d.Name.Lexeme
			case *parser.StructDecl:
				cn := moduleCName(curMod, d.Name.Lexeme)
				e.writef("typedef struct %s %s;\n", cn, cn)
			case *parser.EnumDecl:
				cn := moduleCName(curMod, d.Name.Lexeme)
				e.writef("typedef struct %s %s;\n", cn, cn)
			case *parser.CapabilityDecl:
				// cap<Name> is a zero-size proof token; uint8_t at runtime.
				e.writef("typedef uint8_t cap_%s;\n", d.Name.Lexeme)
			}
		}
	}


	// Emit vec<T> struct typedefs after user structs are forward declared.
	if err := e.emitVecStructTypedefs(); err != nil {
		return err
	}
	if err := e.emitMapStructTypedefs(); err != nil {
		return err
	}
	if err := e.emitSetStructTypedefs(); err != nil {
		return err
	}
	if err := e.emitRingStructTypedefs(); err != nil {
		return err
	}
	if err := e.emitResultStructTypedefs(); err != nil {
		return err
	}
	if err := e.emitTensorStructTypedefs(); err != nil {
		return err
	}
	if err := e.emitMmapStructTypedefs(); err != nil {
		return err
	}


	// Emit enum definitions (tagged unions) and struct definitions.
	// Since structs can contain structs (and result/map entries) by value,
	// we must emit them in topological order.
	// Track module context via ModuleDecl boundary markers inserted by mergeFiles.
	e.emittedTypes = make(map[string]bool)
	e.emittingTypes = make(map[string]bool)
	{
		curMod := ""
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *parser.ModuleDecl:
				curMod = d.Name.Lexeme
				e.fileModule = curMod
			case *parser.StructDecl:
				e.fileModule = curMod
				if err := e.ensureStructEmitted(e.resLookupStruct(d.Name.Lexeme)); err != nil {
					return err
				}
			case *parser.EnumDecl:
				e.fileModule = curMod
				if err := e.ensureEnumEmitted(e.resLookupEnum(d.Name.Lexeme)); err != nil {
					return err
				}
			case *parser.FnDecl:
				// Ensure all types used in the function body are emitted
				// (e.g. nested result types, local struct literals).
				for _, typ := range e.res.ExprTypes {
					if err := e.ensureTypeDependenciesEmitted(typ); err != nil {
						return err
					}
				}
			}
		}
	}

	// Ensure struct bodies are emitted for all types appearing in builtin FnSigs
	// (e.g. vec<str> from str_split) even when not referenced in ExprTypes.
	for _, t := range e.allUsedTypes() {
		if err := e.ensureTypeDependenciesEmitted(t); err != nil {
			return err
		}
	}

	// Emit vec<T> push helpers and map<K,V> operation helpers now that all
	// user structs and enums are fully defined.
	if err := e.emitVecStructHelpers(); err != nil {
		return err
	}
	if err := e.emitMapStructHelpers(); err != nil {
		return err
	}
	if err := e.emitSetStructHelpers(); err != nil {
		return err
	}
	if err := e.emitRingStructHelpers(); err != nil {
		return err
	}
	if err := e.emitTensorStructHelpers(); err != nil {
		return err
	}
	if err := e.emitMmapStructHelpers(); err != nil {
		return err
	}

	// Emit fn(...)->... function pointer typedefs.
	if err := e.emitFnTypeTypedefs(); err != nil {
		return err
	}

	// Forward-declare extern functions.
	for _, decl := range file.Decls {
		if d, ok := decl.(*parser.ExternFnDecl); ok {
			if err := e.emitExternFnDecl(d); err != nil {
				return err
			}
		}
	}

	// Forward-declare all non-generic functions.
	for _, decl := range file.Decls {
		if d, ok := decl.(*parser.FnDecl); ok && len(d.TypeParams) == 0 {
			if err := e.emitFnForward(d); err != nil {
				return err
			}
		}
	}
	// Forward-declare impl method functions.
	for _, impl := range e.res.ImplDecls {
		for _, m := range impl.Methods {
			if len(m.TypeParams) == 0 {
				mangledName := impl.TypeName.Lexeme + "_" + m.Name.Lexeme
				sig := e.res.FnSigs[mangledName]
				if sig != nil {
					proto, err := e.fnProto(mangledName, sig)
					if err != nil {
						return err
					}
					e.writef("%s;\n", proto)
				}
			}
		}
	}
	// Forward-declare impl-for (trait impl) method functions.
	for _, impl := range e.res.ImplForDecls {
		for _, m := range impl.Methods {
			if len(m.TypeParams) == 0 {
				mangledName := impl.TypeName.Lexeme + "_" + m.Name.Lexeme
				sig := e.res.FnSigs[mangledName]
				if sig != nil {
					proto, err := e.fnProto(mangledName, sig)
					if err != nil {
						return err
					}
					e.writef("%s;\n", proto)
				}
			}
		}
	}
	// Forward-declare generic instances.
	for _, inst := range e.res.GenericInstances {
		proto, err := e.fnProto(inst.MangledName, inst.Sig)
		if err != nil {
			return err
		}
		e.writef("%s;\n", proto)
	}
	if hasFnDecls(file) || len(e.res.GenericInstances) > 0 {
		e.writeln("")
	}

	// Emit trampolines for named functions used as first-class values.
	if err := e.emitNamedFnTrampolines(); err != nil {
		return err
	}

	// Emit lambda helper functions (before named functions so forward refs work).
	for _, lam := range e.res.Lambdas {
		if err := e.emitLambdaFn(lam); err != nil {
			return err
		}
	}

	// Emit spawn task structs and thunk functions.
	for _, sp := range e.res.Spawns {
		if err := e.emitSpawnThunk(sp); err != nil {
			return err
		}
	}

	// Emit generic function instances.
	for _, inst := range e.res.GenericInstances {
		if err := e.emitGenericInstance(inst); err != nil {
			return err
		}
	}

	// Emit module-level constants.
	for _, cd := range e.res.ConstDecls {
		if err := e.emitConstDecl(cd); err != nil {
			return err
		}
	}

	// Emit function bodies.
	for _, decl := range file.Decls {
		if d, ok := decl.(*parser.FnDecl); ok && len(d.TypeParams) == 0 {
			if err := e.emitFnDecl(d); err != nil {
				return err
			}
		}
	}

	// Emit impl method bodies.
	for _, impl := range e.res.ImplDecls {
		for _, m := range impl.Methods {
			if len(m.TypeParams) == 0 {
				mangledName := impl.TypeName.Lexeme + "_" + m.Name.Lexeme
				if err := e.emitMethodDecl(mangledName, m); err != nil {
					return err
				}
			}
		}
	}
	// Emit impl-for (trait impl) method bodies.
	for _, impl := range e.res.ImplForDecls {
		for _, m := range impl.Methods {
			if len(m.TypeParams) == 0 {
				mangledName := impl.TypeName.Lexeme + "_" + m.Name.Lexeme
				if err := e.emitMethodDecl(mangledName, m); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (e *emitter) emitLambdaFn(lam *typeck.LambdaInfo) error {
	sig := lam.Sig
	ret, err := e.cType(sig.Ret)
	if err != nil {
		return err
	}
	implName := lam.Name + "_impl"

	// Build param list for the impl function (user params + void* _env).
	params := make([]string, len(lam.Node.Params))
	for i, p := range lam.Node.Params {
		ct, err := e.cType(sig.Params[i])
		if err != nil {
			return err
		}
		params[i] = ct + " " + p.Name.Lexeme
	}
	params = append(params, "void* _env")
	e.writef("\nstatic %s %s(%s) {\n", ret, implName, strings.Join(params, ", "))

	// Build the by-ref capture set for this lambda.
	byRef := make(map[string]bool)
	for i, cap := range lam.Captures {
		if i < len(lam.CaptureByRef) && lam.CaptureByRef[i] {
			byRef[cap] = true
		}
	}

	// If there are captures, unpack them from _env at the top of the function.
	if len(lam.Captures) > 0 {
		envType := lam.Name + "_env"
		e.writef("    %s* _e = (%s*)_env;\n", envType, envType)
		for i, cap := range lam.Captures {
			if i < len(lam.CaptureByRef) && lam.CaptureByRef[i] {
				// By-ref capture: unpack as pointer; reads/writes use (*cap).
				ct, err := e.cType(lam.CaptureTypes[i])
				if err != nil {
					return err
				}
				e.writef("    %s* %s = _e->%s;\n", ct, cap, cap)
			} else {
				e.writef("    __auto_type %s = _e->%s;\n", cap, cap)
			}
		}
	}

	prevRetIsUnit := e.retIsUnit
	prevIsMain := e.isMain
	prevContracts := e.contracts
	prevRetType := e.retType
	prevByRef := e.byRefCaptures
	e.retIsUnit = sig.Ret.Equals(typeck.TUnit)
	e.isMain = false
	e.contracts = nil
	e.retType = sig.Ret
	e.byRefCaptures = byRef

	if err := e.emitFnBody(lam.Node.Body, 1); err != nil {
		e.retIsUnit = prevRetIsUnit
		e.isMain = prevIsMain
		e.contracts = prevContracts
		e.retType = prevRetType
		e.byRefCaptures = prevByRef
		return err
	}

	e.retIsUnit = prevRetIsUnit
	e.isMain = prevIsMain
	e.contracts = prevContracts
	e.retType = prevRetType
	e.byRefCaptures = prevByRef
	e.writeln("}")

	// Emit the capture struct and maker if needed.
	if len(lam.Captures) > 0 {
		envType := lam.Name + "_env"
		fnTypN, err := e.fnTypeName(sig)
		if err != nil {
			return err
		}
		// Capture struct definition.
		e.writef("typedef struct {\n")
		for i, cap := range lam.Captures {
			ct, err := e.cType(lam.CaptureTypes[i])
			if err != nil {
				return err
			}
			if i < len(lam.CaptureByRef) && lam.CaptureByRef[i] {
				// By-ref: store a pointer.
				e.writef("    %s* %s;\n", ct, cap)
			} else {
				e.writef("    %s %s;\n", ct, cap)
			}
		}
		e.writef("} %s;\n", envType)
		// Maker function — takes outer-scope values/addresses.
		makerParams := make([]string, len(lam.Captures))
		for i, cap := range lam.Captures {
			ct, err := e.cType(lam.CaptureTypes[i])
			if err != nil {
				return err
			}
			if i < len(lam.CaptureByRef) && lam.CaptureByRef[i] {
				makerParams[i] = ct + "* " + cap
			} else {
				makerParams[i] = ct + " " + cap
			}
		}
		e.writef("static %s %s_make(%s) {\n", fnTypN, lam.Name, strings.Join(makerParams, ", "))
		e.writef("    %s* _e = (%s*)malloc(sizeof(%s));\n", envType, envType, envType)
		for _, cap := range lam.Captures {
			e.writef("    _e->%s = %s;\n", cap, cap)
		}
		e.writef("    return (%s){ ._fn = %s, ._env = _e };\n", fnTypN, implName)
		e.writef("}\n")
	}
	return nil
}

// taskTypeName returns the C struct name for a task<T> given the inner type T.
func (e *emitter) taskTypeName(T typeck.Type) (string, error) {
	tC, err := e.cType(T)
	if err != nil {
		return "", err
	}
	return "_CndTask_" + e.mangle(tC), nil
}

// ensureTaskTypeEmitted emits the task struct definition (typedef + body) once.
func (e *emitter) ensureTaskTypeEmitted(T typeck.Type, name string) error {
	if e.emittedTypes[name] {
		return nil
	}
	e.emittedTypes[name] = true
	tC, err := e.cType(T)
	if err != nil {
		return err
	}
	e.writef("typedef struct %s %s;\n", name, name)
	e.writef("struct %s {\n", name)
	e.writef("    pthread_t _thread;\n")
	if tC != "void" {
		e.writef("    %s _result;\n", tC)
	}
	e.writef("    int _ok;\n")
	e.writef("    const char* _err;\n")
	e.writef("};\n")
	return nil
}

// emitSpawnThunk emits the context struct and thunk function for one SpawnInfo.
func (e *emitter) emitSpawnThunk(sp *typeck.SpawnInfo) error {
	T := sp.ResultType
	taskTyp, err := e.taskTypeName(T)
	if err != nil {
		return err
	}
	// Ensure task struct is defined.
	if err := e.ensureTaskTypeEmitted(T, taskTyp); err != nil {
		return err
	}

	ctxType := sp.Name + "_ctx"
	fnName := sp.Name + "_fn"

	// Emit context struct.
	e.writef("typedef struct {\n")
	e.writef("    %s* _task;\n", taskTyp)
	for i, cap := range sp.Captures {
		capC, err := e.cType(sp.CaptureTypes[i])
		if err != nil {
			return err
		}
		e.writef("    %s %s;\n", capC, cap)
	}
	e.writef("} %s;\n", ctxType)

	// Emit thunk function.
	e.writef("static void* %s(void* _raw) {\n", fnName)
	e.writef("    %s* _ctx = (%s*)_raw;\n", ctxType, ctxType)
	e.writef("    %s* _task = _ctx->_task;\n", taskTyp)
	for _, cap := range sp.Captures {
		e.writef("    __auto_type %s = _ctx->%s;\n", cap, cap)
	}
	e.writef("    free(_ctx);\n")

	// Save/restore emitter context.
	prevSpawn := e.spawnTaskVar
	prevRetIsUnit := e.retIsUnit
	prevIsMain := e.isMain
	prevContracts := e.contracts
	prevRetType := e.retType
	e.spawnTaskVar = "_task"
	e.retIsUnit = T.Equals(typeck.TUnit)
	e.isMain = false
	e.contracts = nil
	e.retType = T

	if err := e.emitBlock(sp.Node.Body, 1); err != nil {
		e.spawnTaskVar = prevSpawn
		e.retIsUnit = prevRetIsUnit
		e.isMain = prevIsMain
		e.contracts = prevContracts
		e.retType = prevRetType
		return err
	}

	e.spawnTaskVar = prevSpawn
	e.retIsUnit = prevRetIsUnit
	e.isMain = prevIsMain
	e.contracts = prevContracts
	e.retType = prevRetType

	// Default fall-through: unit tasks or bodies without explicit return.
	if T.Equals(typeck.TUnit) {
		e.writef("    _task->_ok = 1;\n")
	}
	e.writef("    return NULL;\n")
	e.writef("}\n")
	return nil
}

// safeVariantField returns the C union-member name for a Candor enum variant.
// It prefixes with "cnd_" when the variant name would produce a C11 reserved
// identifier of the form _Bool, _Generic, etc.
func safeVariantField(name string) string {
	switch name {
	case "Bool", "Complex", "Imaginary",
		"Alignas", "Alignof", "Atomic",
		"Generic", "Noreturn", "Static_assert", "Thread_local":
		return "cnd_" + name
	}
	return name
}

func (e *emitter) mangle(s string) string {
	r := strings.NewReplacer(" ", "_", "*", "ptr", "<", "_", ">", "_", ",", "_", "(", "_", ")", "_", "-", "_")
	return r.Replace(s)
}

func (e *emitter) vecTypeName(elemC string) string {
	return "_CndVec_" + e.mangle(elemC)
}

func (e *emitter) tensorTypeName(elemC string) string {
	return "_CndTensor_" + e.mangle(elemC)
}

func (e *emitter) mmapTypeName(elemC string) string {
	return "_CndMmap_" + e.mangle(elemC)
}

// hasMmap returns true if any mmap<T> type appears in the program.
func (e *emitter) hasMmap() bool {
	for _, t := range e.allUsedTypes() {
		if gen, ok := t.(*typeck.GenType); ok && gen.Con == "mmap" {
			return true
		}
	}
	return false
}

func (e *emitter) vecPushName(elemC string) string {
	return "_cnd_vec_push_" + e.mangle(elemC)
}

func (e *emitter) mapTypeName(kC, vC string) string {
	return "_CndMap_" + e.mangle(kC) + "_" + e.mangle(vC)
}

func (e *emitter) mapEntryName(kC, vC string) string {
	return "_CndMapEntry_" + e.mangle(kC) + "_" + e.mangle(vC)
}

func (e *emitter) mapHashFnName(kC string) string {
	return "_cnd_map_hash_" + e.mangle(kC)
}

func (e *emitter) mapEqFnName(kC string) string {
	return "_cnd_map_eq_" + e.mangle(kC)
}

func (e *emitter) mapNewFnName(kC, vC string) string {
	return "_cnd_map_new_" + e.mangle(kC) + "_" + e.mangle(vC)
}

func (e *emitter) mapInsertFnName(kC, vC string) string {
	return "_cnd_map_insert_" + e.mangle(kC) + "_" + e.mangle(vC)
}

func (e *emitter) mapGetFnName(kC, vC string) string {
	return "_cnd_map_get_" + e.mangle(kC) + "_" + e.mangle(vC)
}

func (e *emitter) mapRemoveFnName(kC, vC string) string {
	return "_cnd_map_remove_" + e.mangle(kC) + "_" + e.mangle(vC)
}

func (e *emitter) mapContainsFnName(kC, vC string) string {
	return "_cnd_map_contains_" + e.mangle(kC) + "_" + e.mangle(vC)
}

func (e *emitter) setTypeName(eC string) string {
	return "_CndSet_" + e.mangle(eC)
}

func (e *emitter) setEntryName(eC string) string {
	return "_CndSetEntry_" + e.mangle(eC)
}

func (e *emitter) setHashFnName(eC string) string {
	return "_cnd_set_hash_" + e.mangle(eC)
}

func (e *emitter) setEqFnName(eC string) string {
	return "_cnd_set_eq_" + e.mangle(eC)
}

func (e *emitter) setNewFnName(eC string) string {
	return "_cnd_set_new_" + e.mangle(eC)
}

func (e *emitter) setAddFnName(eC string) string {
	return "_cnd_set_add_" + e.mangle(eC)
}

func (e *emitter) setRemoveFnName(eC string) string {
	return "_cnd_set_remove_" + e.mangle(eC)
}

func (e *emitter) setContainsFnName(eC string) string {
	return "_cnd_set_contains_" + e.mangle(eC)
}

func (e *emitter) ringTypeName(elemC string) string {
	return "_CndRing_" + e.mangle(elemC)
}

func (e *emitter) ringNewFnName(elemC string) string {
	return "_cnd_ring_new_" + e.mangle(elemC)
}

func (e *emitter) ringPushBackFnName(elemC string) string {
	return "_cnd_ring_push_back_" + e.mangle(elemC)
}

func (e *emitter) ringPopFrontFnName(elemC string) string {
	return "_cnd_ring_pop_front_" + e.mangle(elemC)
}

// concreteResultType resolves a result<_ok,E> or result<T,_err> type by
// substituting the placeholder with the corresponding parameter from
// e.retType (the current function's declared return type).  This handles the
// common case where err(val)/ok(val) is used without a type-context hint and
// the typechecker leaves a _ok/_err sentinel in ExprTypes.
func (e *emitter) concreteResultType(gen *typeck.GenType) *typeck.GenType {
	if len(gen.Params) != 2 {
		return gen
	}
	ret, ok := e.retType.(*typeck.GenType)
	if !ok || ret.Con != "result" || len(ret.Params) != 2 {
		return gen
	}
	p0, p1 := gen.Params[0], gen.Params[1]
	if g, ok := p0.(*typeck.GenType); ok && (g.Con == "_ok" || g.Con == "_err") {
		p0 = ret.Params[0]
	}
	if g, ok := p1.(*typeck.GenType); ok && (g.Con == "_ok" || g.Con == "_err") {
		p1 = ret.Params[1]
	}
	if p0 == gen.Params[0] && p1 == gen.Params[1] {
		return gen
	}
	return &typeck.GenType{Con: "result", Params: []typeck.Type{p0, p1}}
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
	return fmt.Sprintf("_cnd_result_%s_%s", e.mangle(ok), e.mangle(er)), nil
}

func (e *emitter) fnTypeName(ft *typeck.FnType) (string, error) {
	ret, err := e.cType(ft.Ret)
	if err != nil {
		return "", err
	}
	params := make([]string, len(ft.Params))
	for i, p := range ft.Params {
		ct, err := e.cType(p)
		if err != nil {
			return "", err
		}
		params[i] = ct
	}
	name := fmt.Sprintf("_cnd_fn_%s_%s", ret, strings.Join(params, "_"))
	return e.mangle(name), nil
}

// ── type collection ───────────────────────────────────────────────────────────

func (e *emitter) allUsedTypes() []typeck.Type {
	var types []typeck.Type
	seen := map[typeck.Type]bool{}
	var add func(t typeck.Type)
	add = func(t typeck.Type) {
		if t == nil || seen[t] {
			return
		}
		seen[t] = true
		types = append(types, t)
		switch pt := t.(type) {
		case *typeck.GenType:
			for _, p := range pt.Params {
				add(p)
			}
		case *typeck.FnType:
			for _, p := range pt.Params {
				add(p)
			}
			add(pt.Ret)
		case *typeck.StructType:
			for _, ft := range pt.Fields {
				add(ft)
			}
		case *typeck.EnumType:
			for _, v := range pt.Variants {
				for _, ft := range v.Fields {
					add(ft)
				}
			}
		}
	}
	for _, t := range e.res.ExprTypes {
		add(t)
	}
	for _, sig := range e.res.FnSigs {
		for _, p := range sig.Params {
			add(p)
		}
		add(sig.Ret)
	}
	for _, s := range e.res.Structs {
		for _, ft := range s.Fields {
			add(ft)
		}
	}
	for _, en := range e.res.Enums {
		for _, v := range en.Variants {
			for _, ft := range v.Fields {
				add(ft)
			}
		}
	}
	return types
}

// ── fn(...)->... typedef helpers ──────────────────────────────────────────────


// emitFnTypeTypedefs emits C typedef for every fn(...)->... type used in
// the program. Dependencies (nested fn types) are emitted before dependents.
func (e *emitter) emitFnTypeTypedefs() error {
	// Collect all FnType instances reachable from ExprTypes and FnSig signatures.
	byName := map[string]*typeck.FnType{}
	var collect func(t typeck.Type)
	collect = func(t typeck.Type) {
		ft, ok := t.(*typeck.FnType)
		if !ok {
			return
		}
		name, err := e.fnTypeName(ft)
		if err != nil || byName[name] != nil {
			return
		}
		byName[name] = ft
		for _, p := range ft.Params {
			collect(p)
		}
		collect(ft.Ret)
	}
	for _, t := range e.allUsedTypes() {
		collect(t)
	}
	if len(byName) == 0 {
		return nil
	}

	// Emit in topological order: dependencies (inner fn types) before dependents.
	emitted := map[string]bool{}
	var emitOne func(name string, ft *typeck.FnType) error
	emitOne = func(name string, ft *typeck.FnType) error {
		if emitted[name] {
			return nil
		}
		// Emit any fn-typed parameters first.
		for _, p := range ft.Params {
			if dep, ok := p.(*typeck.FnType); ok {
				depName, err := e.fnTypeName(dep)
				if err != nil {
					return err
				}
				if err := emitOne(depName, dep); err != nil {
					return err
				}
			}
		}
		if dep, ok := ft.Ret.(*typeck.FnType); ok {
			depName, err := e.fnTypeName(dep)
			if err != nil {
				return err
			}
			if err := emitOne(depName, dep); err != nil {
				return err
			}
		}
		emitted[name] = true
		ret, err := e.cType(ft.Ret)
		if err != nil {
			return err
		}
		// Fat-pointer closure struct: { RetType (*_fn)(Params..., void*); void* _env; }
		params := make([]string, len(ft.Params))
		for i, p := range ft.Params {
			ct, err := e.cType(p)
			if err != nil {
				return err
			}
			params[i] = ct
		}
		var fnPtrParams string
		if len(params) == 0 {
			fnPtrParams = "void*"
		} else {
			fnPtrParams = strings.Join(params, ", ") + ", void*"
		}
		e.writef("typedef struct { %s (*_fn)(%s); void* _env; } %s;\n", ret, fnPtrParams, name)
		return nil
	}
	for name, ft := range byName {
		if err := emitOne(name, ft); err != nil {
			return err
		}
	}
	e.writeln("")
	return nil
}

func (e *emitter) emitVecStructTypedefs() error {
	seen := map[string]bool{}
	for _, t := range e.allUsedTypes() {
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
		e.writef("typedef struct %s %s;\n", name, name)
	}
	if len(seen) > 0 {
		e.writeln("")
	}
	return nil
}

func (e *emitter) emitVecStructHelpers() error {
	seen := map[string]bool{}
	for _, t := range e.allUsedTypes() {
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
		e.writef("static inline void %s(%s* v, %s val) {\n", pushFn, name, elemC)
		e.writef("    if (v->_len >= v->_cap) {\n")
		e.writef("        uint64_t _nc = v->_cap ? v->_cap * 2 : 4;\n")
		e.writef("        v->_data = (%s*)realloc(v->_data, _nc * sizeof(%s));\n", elemC, elemC)
		e.writef("        v->_cap = _nc;\n")
		e.writef("    }\n")
		e.writef("    v->_data[v->_len++] = val;\n")
		e.writef("}\n")
		popFn := "_cnd_vec_pop_" + e.mangle(elemC)
		e.writef("static inline %s %s(%s* v) {\n", elemC, popFn, name)
		e.writef("    assert(v->_len > 0);\n")
		e.writef("    return v->_data[--v->_len];\n")
		e.writef("}\n")
	}
	if len(seen) > 0 {
		e.writeln("")
	}
	return nil
}

func (e *emitter) emitMapStructTypedefs() error {
	seen := map[string]bool{}
	for _, t := range e.allUsedTypes() {
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "map" || len(gen.Params) != 2 {
			continue
		}
		kC, err := e.cType(gen.Params[0])
		if err != nil {
			continue
		}
		vC, err := e.cType(gen.Params[1])
		if err != nil {
			continue
		}
		name := e.mapTypeName(kC, vC)
		if seen[name] {
			continue
		}
		seen[name] = true
		entryName := e.mapEntryName(kC, vC)
		e.writef("typedef struct %s %s;\n", entryName, entryName)
		e.writef("typedef struct %s %s;\n", name, name)
	}
	if len(seen) > 0 {
		e.writeln("")
	}
	return nil
}

func (e *emitter) emitMapStructHelpers() error {
	seenMaps := map[string]bool{}
	seenKeys := map[string]bool{}
	for _, t := range e.allUsedTypes() {
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "map" || len(gen.Params) != 2 {
			continue
		}
		kC, err := e.cType(gen.Params[0])
		if err != nil {
			continue
		}
		vC, err := e.cType(gen.Params[1])
		if err != nil {
			continue
		}
		mapName := e.mapTypeName(kC, vC)
		if seenMaps[mapName] {
			continue
		}
		seenMaps[mapName] = true

		entryName := e.mapEntryName(kC, vC)
		hashFn := e.mapHashFnName(kC)
		eqFn := e.mapEqFnName(kC)
		newFn := e.mapNewFnName(kC, vC)
		insertFn := e.mapInsertFnName(kC, vC)
		getFn := e.mapGetFnName(kC, vC)
		removeFn := e.mapRemoveFnName(kC, vC)
		containsFn := e.mapContainsFnName(kC, vC)

		// Hash and equality helpers (once per key type)
		if !seenKeys[kC] {
			seenKeys[kC] = true
			if kC == "const char*" {
				e.writef("static inline uint64_t %s(const char* k) {\n", hashFn)
				e.writef("    uint64_t h = 5381; while (*k) h = ((h << 5) + h) ^ (unsigned char)*k++; return h;\n")
				e.writef("}\n")
				e.writef("static inline int %s(const char* a, const char* b) { return strcmp(a, b) == 0; }\n", eqFn)
			} else {
				e.writef("static inline uint64_t %s(%s k) {\n", hashFn, kC)
				e.writef("    uint64_t h = (uint64_t)k;\n")
				e.writef("    h ^= h >> 33; h *= 0xff51afd7ed558ccdULL; h ^= h >> 33; return h;\n")
				e.writef("}\n")
				e.writef("static inline int %s(%s a, %s b) { return a == b; }\n", eqFn, kC, kC)
			}
		}

		// map_new
		e.writef("static inline %s %s(void) {\n", mapName, newFn)
		e.writef("    uint64_t _cap = 16;\n")
		e.writef("    %s** _b = (%s**)calloc(_cap, sizeof(%s*));\n", entryName, entryName, entryName)
		e.writef("    return (%s){ _b, _cap, 0 };\n", mapName)
		e.writef("}\n")

		// map_insert
		e.writef("static inline void %s(%s* m, %s k, %s v) {\n", insertFn, mapName, kC, vC)
		e.writef("    if (m->_len * 4 >= m->_cap * 3) {\n")
		e.writef("        uint64_t _nc = m->_cap * 2;\n")
		e.writef("        %s** _nb = (%s**)calloc(_nc, sizeof(%s*));\n", entryName, entryName, entryName)
		e.writef("        for (uint64_t _i = 0; _i < m->_cap; _i++) {\n")
		e.writef("            %s* _en = m->_buckets[_i];\n", entryName)
		e.writef("            while (_en) { %s* _nx = _en->_next; uint64_t _bi2 = %s(_en->_key) %% _nc; _en->_next = _nb[_bi2]; _nb[_bi2] = _en; _en = _nx; }\n", entryName, hashFn)
		e.writef("        }\n")
		e.writef("        free(m->_buckets); m->_buckets = _nb; m->_cap = _nc;\n")
		e.writef("    }\n")
		e.writef("    uint64_t _bi = %s(k) %% m->_cap;\n", hashFn)
		e.writef("    %s* _en = m->_buckets[_bi];\n", entryName)
		e.writef("    while (_en) { if (%s(_en->_key, k)) { _en->_val = v; return; } _en = _en->_next; }\n", eqFn)
		e.writef("    %s* _ne = (%s*)malloc(sizeof(%s));\n", entryName, entryName, entryName)
		e.writef("    _ne->_key = k; _ne->_val = v; _ne->_next = m->_buckets[_bi]; m->_buckets[_bi] = _ne; m->_len++;\n")
		e.writef("}\n")

		// map_get → option<V> = V* (NULL = none)
		e.writef("static inline %s* %s(%s m, %s k) {\n", vC, getFn, mapName, kC)
		e.writef("    if (!m._buckets) return NULL;\n")
		e.writef("    uint64_t _bi = %s(k) %% m._cap;\n", hashFn)
		e.writef("    %s* _en = m._buckets[_bi];\n", entryName)
		e.writef("    while (_en) {\n")
		e.writef("        if (%s(_en->_key, k)) { %s* _p = (%s*)malloc(sizeof(%s)); *_p = _en->_val; return _p; }\n", eqFn, vC, vC, vC)
		e.writef("        _en = _en->_next;\n")
		e.writef("    }\n")
		e.writef("    return NULL;\n")
		e.writef("}\n")

		// map_remove → bool (int)
		e.writef("static inline int %s(%s* m, %s k) {\n", removeFn, mapName, kC)
		e.writef("    if (!m->_buckets) return 0;\n")
		e.writef("    uint64_t _bi = %s(k) %% m->_cap;\n", hashFn)
		e.writef("    %s** _pp = &m->_buckets[_bi];\n", entryName)
		e.writef("    while (*_pp) {\n")
		e.writef("        if (%s((*_pp)->_key, k)) { %s* _dead = *_pp; *_pp = _dead->_next; free(_dead); m->_len--; return 1; }\n", eqFn, entryName)
		e.writef("        _pp = &(*_pp)->_next;\n")
		e.writef("    }\n")
		e.writef("    return 0;\n")
		e.writef("}\n")

		// map_contains → bool (int), no allocation
		e.writef("static inline int %s(%s m, %s k) {\n", containsFn, mapName, kC)
		e.writef("    if (!m._buckets) return 0;\n")
		e.writef("    uint64_t _bi = %s(k) %% m._cap;\n", hashFn)
		e.writef("    %s* _en = m._buckets[_bi];\n", entryName)
		e.writef("    while (_en) { if (%s(_en->_key, k)) return 1; _en = _en->_next; }\n", eqFn)
		e.writef("    return 0;\n")
		e.writef("}\n")
	}
	if len(seenMaps) > 0 {
		e.writeln("")
	}
	return nil
}

func (e *emitter) emitSetStructTypedefs() error {
	seen := map[string]bool{}
	for _, t := range e.allUsedTypes() {
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "set" || len(gen.Params) != 1 {
			continue
		}
		eC, err := e.cType(gen.Params[0])
		if err != nil {
			continue
		}
		name := e.setTypeName(eC)
		if seen[name] {
			continue
		}
		seen[name] = true
		entryName := e.setEntryName(eC)
		e.writef("typedef struct %s %s;\n", entryName, entryName)
		e.writef("typedef struct %s %s;\n", name, name)
	}
	if len(seen) > 0 {
		e.writeln("")
	}
	return nil
}

func (e *emitter) emitSetStructHelpers() error {
	seenSets := map[string]bool{}
	seenKeys := map[string]bool{}
	for _, t := range e.allUsedTypes() {
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "set" || len(gen.Params) != 1 {
			continue
		}
		eC, err := e.cType(gen.Params[0])
		if err != nil {
			continue
		}
		setName := e.setTypeName(eC)
		if seenSets[setName] {
			continue
		}
		seenSets[setName] = true

		entryName := e.setEntryName(eC)
		hashFn := e.setHashFnName(eC)
		eqFn := e.setEqFnName(eC)
		newFn := e.setNewFnName(eC)
		addFn := e.setAddFnName(eC)
		removeFn := e.setRemoveFnName(eC)
		containsFn := e.setContainsFnName(eC)

		// Hash and equality helpers (once per element type)
		if !seenKeys[eC] {
			seenKeys[eC] = true
			if eC == "const char*" {
				e.writef("static inline uint64_t %s(const char* k) {\n", hashFn)
				e.writef("    uint64_t h = 5381; while (*k) h = ((h << 5) + h) ^ (unsigned char)*k++; return h;\n")
				e.writef("}\n")
				e.writef("static inline int %s(const char* a, const char* b) { return strcmp(a, b) == 0; }\n", eqFn)
			} else {
				e.writef("static inline uint64_t %s(%s k) {\n", hashFn, eC)
				e.writef("    uint64_t h = (uint64_t)k;\n")
				e.writef("    h ^= h >> 33; h *= 0xff51afd7ed558ccdULL; h ^= h >> 33; return h;\n")
				e.writef("}\n")
				e.writef("static inline int %s(%s a, %s b) { return a == b; }\n", eqFn, eC, eC)
			}
		}

		// Struct definitions
		e.writef("struct %s { %s _key; struct %s* _next; };\n", entryName, eC, entryName)
		e.writef("struct %s { struct %s** _buckets; uint64_t _len; uint64_t _cap; };\n", setName, entryName)

		// set_new
		e.writef("static inline %s %s(void) {\n", setName, newFn)
		e.writef("    uint64_t _cap = 16;\n")
		e.writef("    %s** _b = (%s**)calloc(_cap, sizeof(%s*));\n", entryName, entryName, entryName)
		e.writef("    return (%s){ _b, 0, _cap };\n", setName)
		e.writef("}\n")

		// set_add
		e.writef("static inline void %s(%s* s, %s k) {\n", addFn, setName, eC)
		e.writef("    if (s->_len * 4 >= s->_cap * 3) {\n")
		e.writef("        uint64_t _nc = s->_cap * 2;\n")
		e.writef("        %s** _nb = (%s**)calloc(_nc, sizeof(%s*));\n", entryName, entryName, entryName)
		e.writef("        for (uint64_t _i = 0; _i < s->_cap; _i++) {\n")
		e.writef("            %s* _en = s->_buckets[_i];\n", entryName)
		e.writef("            while (_en) { %s* _nx = _en->_next; uint64_t _bi2 = %s(_en->_key) %% _nc; _en->_next = _nb[_bi2]; _nb[_bi2] = _en; _en = _nx; }\n", entryName, hashFn)
		e.writef("        }\n")
		e.writef("        free(s->_buckets); s->_buckets = _nb; s->_cap = _nc;\n")
		e.writef("    }\n")
		e.writef("    uint64_t _bi = %s(k) %% s->_cap;\n", hashFn)
		e.writef("    %s* _en = s->_buckets[_bi];\n", entryName)
		e.writef("    while (_en) { if (%s(_en->_key, k)) return; _en = _en->_next; }\n", eqFn)
		e.writef("    %s* _ne = (%s*)malloc(sizeof(%s));\n", entryName, entryName, entryName)
		e.writef("    _ne->_key = k; _ne->_next = s->_buckets[_bi]; s->_buckets[_bi] = _ne; s->_len++;\n")
		e.writef("}\n")

		// set_remove
		e.writef("static inline int %s(%s* s, %s k) {\n", removeFn, setName, eC)
		e.writef("    if (!s->_buckets) return 0;\n")
		e.writef("    uint64_t _bi = %s(k) %% s->_cap;\n", hashFn)
		e.writef("    %s** _pp = &s->_buckets[_bi];\n", entryName)
		e.writef("    while (*_pp) {\n")
		e.writef("        if (%s((*_pp)->_key, k)) { %s* _dead = *_pp; *_pp = _dead->_next; free(_dead); s->_len--; return 1; }\n", eqFn, entryName)
		e.writef("        _pp = &(*_pp)->_next;\n")
		e.writef("    }\n")
		e.writef("    return 0;\n")
		e.writef("}\n")

		// set_contains
		e.writef("static inline int %s(%s s, %s k) {\n", containsFn, setName, eC)
		e.writef("    if (!s._buckets) return 0;\n")
		e.writef("    uint64_t _bi = %s(k) %% s._cap;\n", hashFn)
		e.writef("    %s* _en = s._buckets[_bi];\n", entryName)
		e.writef("    while (_en) { if (%s(_en->_key, k)) return 1; _en = _en->_next; }\n", eqFn)
		e.writef("    return 0;\n")
		e.writef("}\n")
	}
	if len(seenSets) > 0 {
		e.writeln("")
	}
	return nil
}

func (e *emitter) emitRingStructTypedefs() error {
	seen := map[string]bool{}
	for _, t := range e.allUsedTypes() {
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "ring" || len(gen.Params) != 1 {
			continue
		}
		elemC, err := e.cType(gen.Params[0])
		if err != nil {
			continue
		}
		name := e.ringTypeName(elemC)
		if seen[name] {
			continue
		}
		seen[name] = true
		e.writef("typedef struct %s %s;\n", name, name)
	}
	if len(seen) > 0 {
		e.writeln("")
	}
	return nil
}

func (e *emitter) emitRingStructHelpers() error {
	seen := map[string]bool{}
	for _, t := range e.allUsedTypes() {
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "ring" || len(gen.Params) != 1 {
			continue
		}
		elemC, err := e.cType(gen.Params[0])
		if err != nil {
			continue
		}
		name := e.ringTypeName(elemC)
		if seen[name] {
			continue
		}
		seen[name] = true

		newFn := e.ringNewFnName(elemC)
		pushFn := e.ringPushBackFnName(elemC)
		popFn := e.ringPopFrontFnName(elemC)

		// struct definition
		e.writef("struct %s { %s* _data; uint64_t _cap; uint64_t _head; uint64_t _len; };\n", name, elemC)

		// ring_new(cap) → allocate buffer
		e.writef("static inline %s %s(uint64_t cap) {\n", name, newFn)
		e.writef("    %s* _d = (%s*)calloc(cap ? cap : 1, sizeof(%s));\n", elemC, elemC, elemC)
		e.writef("    return (%s){ _d, cap ? cap : 1, 0, 0 };\n", name)
		e.writef("}\n")

		// ring_push_back(r, val) → overwrite oldest if full
		e.writef("static inline void %s(%s* r, %s val) {\n", pushFn, name, elemC)
		e.writef("    if (r->_len < r->_cap) {\n")
		e.writef("        r->_data[(r->_head + r->_len) %% r->_cap] = val;\n")
		e.writef("        r->_len++;\n")
		e.writef("    } else {\n")
		e.writef("        r->_data[r->_head] = val;\n")
		e.writef("        r->_head = (r->_head + 1) %% r->_cap;\n")
		e.writef("    }\n")
		e.writef("}\n")

		// ring_pop_front(r) → option<T> (T* or NULL)
		e.writef("static inline %s* %s(%s* r) {\n", elemC, popFn, name)
		e.writef("    if (r->_len == 0) return NULL;\n")
		e.writef("    %s* _p = (%s*)malloc(sizeof(%s));\n", elemC, elemC, elemC)
		e.writef("    *_p = r->_data[r->_head];\n")
		e.writef("    r->_head = (r->_head + 1) %% r->_cap;\n")
		e.writef("    r->_len--;\n")
		e.writef("    return _p;\n")
		e.writef("}\n")
	}
	if len(seen) > 0 {
		e.writeln("")
	}
	return nil
}

func (e *emitter) emitMmapStructTypedefs() error {
	seen := map[string]bool{}
	for _, t := range e.allUsedTypes() {
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "mmap" || len(gen.Params) != 1 {
			continue
		}
		elemC, err := e.cType(gen.Params[0])
		if err != nil {
			continue
		}
		name := e.mmapTypeName(elemC)
		if seen[name] {
			continue
		}
		seen[name] = true
		e.writef("typedef struct %s %s;\n", name, name)
	}
	if len(seen) > 0 {
		e.writeln("")
	}
	return nil
}

func (e *emitter) emitMmapStructHelpers() error {
	seen := map[string]bool{}
	for _, t := range e.allUsedTypes() {
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "mmap" || len(gen.Params) != 1 {
			continue
		}
		elemC, err := e.cType(gen.Params[0])
		if err != nil {
			continue
		}
		name := e.mmapTypeName(elemC)
		if seen[name] {
			continue
		}
		seen[name] = true
		// struct _CndMmap_T { T* _data; uint64_t _len; int _fd; };
		e.writef("struct %s { %s* _data; uint64_t _len; int _fd; };\n", name, elemC)
	}
	if len(seen) > 0 {
		e.writeln("")
	}
	return nil
}

func (e *emitter) emitTensorStructTypedefs() error {
	seen := map[string]bool{}
	for _, t := range e.allUsedTypes() {
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "tensor" || len(gen.Params) != 1 {
			continue
		}
		elemC, err := e.cType(gen.Params[0])
		if err != nil {
			continue
		}
		name := e.tensorTypeName(elemC)
		if seen[name] {
			continue
		}
		seen[name] = true
		e.writef("typedef struct %s %s;\n", name, name)
	}
	if len(seen) > 0 {
		e.writeln("")
	}
	return nil
}

// emitTensorStructHelpers emits the struct body for each distinct tensor<T> used.
// The struct is emitted here (after all user types) so the element type T is fully defined.
func (e *emitter) emitTensorStructHelpers() error {
	seen := map[string]bool{}
	for _, t := range e.allUsedTypes() {
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "tensor" || len(gen.Params) != 1 {
			continue
		}
		elemC, err := e.cType(gen.Params[0])
		if err != nil {
			continue
		}
		name := e.tensorTypeName(elemC)
		if seen[name] {
			continue
		}
		seen[name] = true
		// struct _CndTensor_T { T* _data; int64_t _size; int64_t _ndim; int64_t* _shape; };
		e.writef("struct %s { %s* _data; int64_t _size; int64_t _ndim; int64_t* _shape; };\n", name, elemC)
	}
	if len(seen) > 0 {
		e.writeln("")
	}
	return nil
}

func (e *emitter) emitResultStructTypedefs() error {
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
		e.writef("typedef struct %s %s;\n", name, name)
	}
	if len(seen) > 0 {
		e.writeln("")
	}
	return nil
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

// ── ensure topologies ────────────────────────────────────────────────────────

// ensureTypeDependenciesEmitted walks a Type and calls ensure*Emitted for contained
// structs, enums, and result types, so their C definition is written before usage.
func (e *emitter) ensureTypeDependenciesEmitted(t typeck.Type) error {
	switch t := t.(type) {
	case *typeck.StructType:
		return e.ensureStructEmitted(t)
	case *typeck.EnumType:
		return e.ensureEnumEmitted(t)
	case *typeck.GenType:
		if t.Con == "vec" && len(t.Params) == 1 {
			// vec<T> only needs T to be forward-declared because it stores T*.
			// Forward declarations are already emitted at the top of the file.
			elemC, err := e.cType(t.Params[0])
			if err != nil {
				return err
			}
			name := e.vecTypeName(elemC)
			if e.emittedTypes[name] || e.emittingTypes[name] {
				return nil
			}
			e.emittingTypes[name] = true
			e.writef("struct %s { %s* _data; uint64_t _len; uint64_t _cap; };\n", name, elemC)
			e.emittedTypes[name] = true
			e.emittingTypes[name] = false
			return nil
		}

		if t.Con == "ring" && len(t.Params) == 1 {
			elemC, err := e.cType(t.Params[0])
			if err != nil {
				return err
			}
			name := e.ringTypeName(elemC)
			if e.emittedTypes[name] || e.emittingTypes[name] {
				return nil
			}
			e.emittingTypes[name] = true
			e.writef("struct %s { %s* _data; uint64_t _cap; uint64_t _head; uint64_t _len; };\n", name, elemC)
			e.emittedTypes[name] = true
			e.emittingTypes[name] = false
			return nil
		}

		if t.Con == "tensor" && len(t.Params) == 1 {
			elemC, err := e.cType(t.Params[0])
			if err != nil {
				return err
			}
			name := e.tensorTypeName(elemC)
			if e.emittedTypes[name] || e.emittingTypes[name] {
				return nil
			}
			e.emittingTypes[name] = true
			e.writef("struct %s { %s* _data; int64_t _size; int64_t _ndim; int64_t* _shape; };\n", name, elemC)
			e.emittedTypes[name] = true
			e.emittingTypes[name] = false
			return nil
		}

		if t.Con == "mmap" && len(t.Params) == 1 {
			elemC, err := e.cType(t.Params[0])
			if err != nil {
				return err
			}
			name := e.mmapTypeName(elemC)
			if e.emittedTypes[name] || e.emittingTypes[name] {
				return nil
			}
			e.emittingTypes[name] = true
			e.writef("struct %s { %s* _data; uint64_t _len; int _fd; };\n", name, elemC)
			e.emittedTypes[name] = true
			e.emittingTypes[name] = false
			return nil
		}

		// For other generic types (map, result), we need params to be fully defined.
		for _, p := range t.Params {
			if err := e.ensureTypeDependenciesEmitted(p); err != nil {
				return err
			}
		}

		if t.Con == "map" && len(t.Params) == 2 {
			kC, err := e.cType(t.Params[0])
			if err != nil {
				return err
			}
			vC, err := e.cType(t.Params[1])
			if err != nil {
				return err
			}
			mapName := e.mapTypeName(kC, vC)
			if e.emittedTypes[mapName] || e.emittingTypes[mapName] {
				return nil
			}
			e.emittingTypes[mapName] = true

			entryName := e.mapEntryName(kC, vC)
			e.writef("struct %s {\n", entryName)
			e.writef("    %s _key;\n", kC)
			e.writef("    %s _val;\n", vC)
			e.writef("    struct %s* _next;\n", entryName)
			e.writef("};\n")

			e.writef("struct %s {\n", mapName)
			e.writef("    struct %s** _buckets;\n", entryName)
			e.writef("    uint64_t _cap;\n")
			e.writef("    uint64_t _len;\n")
			e.writef("};\n")

			e.emittedTypes[mapName] = true
			e.emittingTypes[mapName] = false
		}

		if t.Con == "result" && len(t.Params) == 2 {
			name, err := e.resultTypeName(t)
			if err != nil {
				return nil
			}
			if e.emittedTypes[name] || e.emittingTypes[name] {
				return nil
			}
			e.emittingTypes[name] = true
			okC, err := e.cType(t.Params[0])
			if err != nil {
				return err
			}
			errC, err := e.cType(t.Params[1])
			if err != nil {
				return err
			}
			e.writef("struct %s {\n", name)
			if okC == "void" {
				e.writef("    int _ok; %s _err_val;\n", errC)
			} else {
				e.writef("    int _ok; %s _ok_val; %s _err_val;\n", okC, errC)
			}
			e.writef("};\n")
			e.emittedTypes[name] = true
			e.emittingTypes[name] = false
		}
	case *typeck.FnType:
		for _, p := range t.Params {
			if err := e.ensureTypeDependenciesEmitted(p); err != nil {
				return err
			}
		}
		if err := e.ensureTypeDependenciesEmitted(t.Ret); err != nil {
			return err
		}
	}
	return nil
}

// ── struct ────────────────────────────────────────────────────────────────────

func (e *emitter) ensureStructEmitted(st *typeck.StructType) error {
	if e.emittedTypes[st.Name] {
		return nil
	}
	if e.emittingTypes[st.Name] {
		return fmt.Errorf("cyclic struct dependency involving %s without using ref<T> (stack: %v)", st.Name, e.emitStack)
	}
	e.emitStack = append(e.emitStack, "struct:"+st.Name)
	defer func() { e.emitStack = e.emitStack[:len(e.emitStack)-1] }()
	e.emittingTypes[st.Name] = true

	// Ensure all fields are emitted first.
	for _, fType := range st.Fields {
		// Pointers (ref<T>, vec<T>, map<K,V>, option<T>) don't require the inner type
		// to be fully defined for the struct body. Only inline types do.
		if gen, ok := fType.(*typeck.GenType); ok {
			if gen.Con == "ref" || gen.Con == "refmut" || gen.Con == "option" || gen.Con == "box" {
				// We don't strictly need it defined here, but we still ensure it.
			} else if err := e.ensureTypeDependenciesEmitted(gen); err != nil {
				return err
			}
		} else if err := e.ensureTypeDependenciesEmitted(fType); err != nil {
			return err
		}
	}

	e.writef("\nstruct %s {\n", st.Name)
	// Output fields.
	// NOTE: In Candor, map iteration over struct fields gives pseudo-random order,
	// but e.res.Structs uses map so order is non-deterministic... but emit_c
	// shouldn't break C if the fields are ordered randomly.
	for fname, ftype := range st.Fields {
		cType, err := e.cType(ftype)
		if err != nil {
			return err
		}
		e.writef("    %s %s;\n", cType, fname)
	}
	e.writef("};\n")

	e.emittedTypes[st.Name] = true
	e.emittingTypes[st.Name] = false
	return nil
}

// ── enum ──────────────────────────────────────────────────────────────────────

func (e *emitter) ensureEnumEmitted(et *typeck.EnumType) error {
	if e.emittedTypes[et.Name] {
		return nil
	}
	if e.emittingTypes[et.Name] {
		return fmt.Errorf("cyclic enum dependency involving %s without using ref<T> (stack: %v)", et.Name, e.emitStack)
	}
	e.emitStack = append(e.emitStack, "enum:"+et.Name)
	defer func() { e.emitStack = e.emitStack[:len(e.emitStack)-1] }()
	e.emittingTypes[et.Name] = true

	for _, v := range et.Variants {
		for _, fType := range v.Fields {
			// box<T>, ref<T>, refmut<T> are pointer-sized indirections — they do
			// not require T to be fully laid out before the enclosing enum can be
			// defined (the enum stores a pointer, not T inline).  Skip dependency
			// emission for these to allow mutually-recursive enum types.
			if gen, ok := fType.(*typeck.GenType); ok {
				if gen.Con == "box" || gen.Con == "ref" || gen.Con == "refmut" {
					continue
				}
			}
			if err := e.ensureTypeDependenciesEmitted(fType); err != nil {
				return err
			}
		}
	}

	name := et.Name

	// Determine if any variant carries data.
	hasData := false
	for _, v := range et.Variants {
		if len(v.Fields) > 0 {
			hasData = true
			break
		}
	}

	e.writef("\ntypedef struct %s %s;\n", name, name)
	e.writef("struct %s {\n    int _tag;\n", name)
	if hasData {
		e.writeln("    union {")
		for _, v := range et.Variants {
			if len(v.Fields) == 0 {
				continue
			}
			e.writef("        struct {")
			for i, ft := range v.Fields {
				ct, err := e.cType(ft)
				if err != nil {
					return err
				}
				e.writef(" %s _%d;", ct, i)
			}
			e.writef(" } _%s;\n", safeVariantField(v.Name))
		}
		e.writeln("    } _data;")
	}
	e.writef("};\n")

	// Tag constants.
	for _, v := range et.Variants {
		e.writef("static const int %s_tag_%s = %d;\n", name, v.Name, v.Tag)
	}

	// Constructor functions.
	for _, v := range et.Variants {
		if len(v.Fields) == 0 {
			e.writef("static inline %s %s_%s(void) { %s _r; _r._tag = %d; return _r; }\n",
				name, name, v.Name, name, v.Tag)
		} else {
			// Build param list.
			params := make([]string, len(v.Fields))
			for i, ft := range v.Fields {
				ct, err := e.cType(ft)
				if err != nil {
					return err
				}
				params[i] = fmt.Sprintf("%s _%d", ct, i)
			}
			e.writef("static inline %s %s_%s(%s) {\n", name, name, v.Name, strings.Join(params, ", "))
			e.writef("    %s _r; _r._tag = %d;\n", name, v.Tag)
			for i := range v.Fields {
				e.writef("    _r._data._%s._%d = _%d;\n", safeVariantField(v.Name), i, i)
			}
			e.writeln("    return _r;")
			e.writeln("}")
		}
	}

	e.emittedTypes[et.Name] = true
	e.emittingTypes[et.Name] = false
	return nil
}

// ── functions ─────────────────────────────────────────────────────────────────

func (e *emitter) emitExternFnDecl(d *parser.ExternFnDecl) error {
	sig := e.res.FnSigs[d.Name.Lexeme]
	retC, err := e.cType(sig.Ret)
	if err != nil {
		return err
	}
	if len(d.Params) == 0 {
		e.writef("extern %s %s(void);\n", retC, d.Name.Lexeme)
		return nil
	}
	params := make([]string, len(d.Params))
	for i, p := range d.Params {
		ct, err := e.cType(sig.Params[i])
		if err != nil {
			return err
		}
		params[i] = ct + " " + p.Name.Lexeme
	}
	e.writef("extern %s %s(%s);\n", retC, d.Name.Lexeme, strings.Join(params, ", "))
	return nil
}

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

	// Track current function name for audit entries.
	e.currentFnName = d.Name.Lexeme

	// Emit effects annotation as a C comment before the definition.
	if ann := e.res.FnEffects[d.Name.Lexeme]; ann != nil {
		switch ann.Kind {
		case parser.EffectsPure:
			e.writeln("\n/* pure */")
			if e.audit != nil {
				e.audit.add(AuditEntry{
					Category:    "pure",
					FnName:      d.Name.Lexeme,
					Line:        d.Name.Line,
					Detail:      "pure",
					CEquiv:      "none enforced (GCC __attribute__((pure)) is advisory)",
					Explanation: "Candor enforces at compile time that pure functions cannot call any function with effects. No C equivalent.",
				})
			}
		case parser.EffectsDecl:
			e.writef("\n/* effects: %s */\n", strings.Join(ann.Names, ", "))
			if e.audit != nil {
				e.audit.add(AuditEntry{
					Category:    "effects",
					FnName:      d.Name.Lexeme,
					Line:        d.Name.Line,
					Detail:      fmt.Sprintf("effects(%s)", strings.Join(ann.Names, ", ")),
					CEquiv:      "none (dropped — C comment only)",
					Explanation: fmt.Sprintf("Candor enforces that only functions declaring effects(%s) can perform these operations. Any C function can perform them silently.", strings.Join(ann.Names, ", ")),
				})
			}
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
	prevDeclaredVars := e.declaredVars
	e.retIsUnit = sig.Ret.Equals(typeck.TUnit)
	e.isMain = d.Name.Lexeme == "main"
	e.contracts = d.Contracts
	e.retType = sig.Ret
	e.declaredVars = make(map[string]bool)

	// Capture old() expressions from ensures clauses at function entry.
	oldExprs := collectOldExprs(d.Contracts)
	prevOldVars := e.oldVars
	if len(oldExprs) > 0 {
		e.oldVars = make(map[*parser.OldExpr]string)
		for _, oe := range oldExprs {
			varName := e.freshTmp()
			e.oldVars[oe] = varName
			innerType := e.res.ExprTypes[oe.X]
			ct, err := e.cType(innerType)
			if err != nil {
				e.oldVars = prevOldVars
				return err
			}
			e.writef("    %s %s = ", ct, varName)
			if err := e.emitExpr(oe.X, &e.sb); err != nil {
				e.oldVars = prevOldVars
				return err
			}
			e.write(";\n")
		}
	} else {
		e.oldVars = nil
	}

	// Emit requires assertions at the top of the function body.
	for _, cc := range d.Contracts {
		if cc.Kind == parser.ContractRequires {
			e.write("    assert(")
			var clauseSB strings.Builder
			if err := e.emitExpr(cc.Expr, &clauseSB); err != nil {
				return err
			}
			clauseStr := clauseSB.String()
			e.write(clauseStr)
			e.write(");\n")
			if e.audit != nil {
				e.audit.add(AuditEntry{
					Category:    "requires",
					FnName:      d.Name.Lexeme,
					Line:        d.Name.Line,
					Detail:      fmt.Sprintf("requires %s", clauseStr),
					CEquiv:      fmt.Sprintf("assert(%s) — debug builds only, elided with -DNDEBUG", clauseStr),
					Explanation: "Candor requires clauses are in the function signature — machine-readable by every caller and AI agent. C assert() is invisible to callers and can be compiled out.",
				})
			}
		} else if cc.Kind == parser.ContractEnsures {
			if e.audit != nil {
				var clauseSB strings.Builder
				_ = e.emitExpr(cc.Expr, &clauseSB)
				e.audit.add(AuditEntry{
					Category:    "ensures",
					FnName:      d.Name.Lexeme,
					Line:        d.Name.Line,
					Detail:      fmt.Sprintf("ensures %s", clauseSB.String()),
					CEquiv:      "assert() on return value — debug builds only",
					Explanation: "Candor ensures clauses are part of the public contract. In C, the postcondition assert is internal and not visible to callers.",
				})
			}
		}
	}

	isMain := e.isMain
	if isMain {
		e.writeln("    _cnd_argc = argc; _cnd_argv = argv;")
	}
	if err := e.emitFnBody(d.Body, 1); err != nil {
		return err
	}

	e.retIsUnit = prevRetIsUnit
	e.isMain = prevIsMain
	e.contracts = prevContracts
	e.retType = prevRetType
	e.oldVars = prevOldVars
	e.declaredVars = prevDeclaredVars

	// C main must end with return 0. Only add it when the body doesn't
	// already end in an explicit return statement.
	if isMain && !bodyEndsWithReturn(d.Body) {
		e.writeln("    return 0;")
	}
	e.writeln("}")

	return nil
}

// emitGenericInstance emits the C body for one monomorphized generic function.
func (e *emitter) emitGenericInstance(inst *typeck.GenericInstance) error {
	d := inst.Node
	sig := inst.Sig
	ret, err := e.cType(sig.Ret)
	if err != nil {
		return err
	}
	var params []string
	if len(d.Params) == 0 {
		params = []string{"void"}
	} else {
		params = make([]string, len(d.Params))
		for i, p := range d.Params {
			ct, err := e.cType(sig.Params[i])
			if err != nil {
				return err
			}
			params[i] = ct + " " + p.Name.Lexeme
		}
	}
	e.writef("\n%s %s(%s) {\n", ret, inst.MangledName, strings.Join(params, ", "))

	prevRetIsUnit := e.retIsUnit
	prevIsMain := e.isMain
	prevContracts := e.contracts
	prevRetType := e.retType
	e.retIsUnit = sig.Ret.Equals(typeck.TUnit)
	e.isMain = false
	e.contracts = nil
	e.retType = sig.Ret

	if err := e.emitFnBody(d.Body, 1); err != nil {
		e.retIsUnit = prevRetIsUnit
		e.isMain = prevIsMain
		e.contracts = prevContracts
		e.retType = prevRetType
		return err
	}

	e.retIsUnit = prevRetIsUnit
	e.isMain = prevIsMain
	e.contracts = prevContracts
	e.retType = prevRetType
	e.writeln("}")
	return nil
}

// collectOldExprs walks ensures clause expressions and collects all OldExpr nodes.
func collectOldExprs(clauses []parser.ContractClause) []*parser.OldExpr {
	var result []*parser.OldExpr
	var walk func(e parser.Expr)
	walk = func(e parser.Expr) {
		switch ex := e.(type) {
		case *parser.OldExpr:
			result = append(result, ex)
		case *parser.BinaryExpr:
			walk(ex.Left)
			walk(ex.Right)
		case *parser.UnaryExpr:
			walk(ex.Operand)
		case *parser.CallExpr:
			walk(ex.Fn)
			for _, a := range ex.Args {
				walk(a)
			}
		case *parser.FieldExpr:
			walk(ex.Receiver)
		case *parser.IndexExpr:
			walk(ex.Collection)
			walk(ex.Index)
		}
	}
	for _, cc := range clauses {
		if cc.Kind == parser.ContractEnsures {
			walk(cc.Expr)
		}
	}
	return result
}

// fnProto builds "rettype name(params)" for forward decls and definitions.
// The Candor main()->unit maps to C "int main(int argc, char** argv)".
func (e *emitter) fnProto(name string, sig *typeck.FnType) (string, error) {
	if name == "main" {
		return "int main(int argc, char** argv)", nil
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
		return "int main(int argc, char** argv)", nil
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
	// Nested C blocks (if/loop/match bodies at depth > 1) introduce a new C
	// scope. Save and restore declaredVars so inner declarations do not
	// pollute the outer scope and prevent it from declaring the same name.
	if depth > 1 && e.declaredVars != nil {
		saved := make(map[string]bool, len(e.declaredVars))
		for k, v := range e.declaredVars {
			saved[k] = v
		}
		defer func() { e.declaredVars = saved }()
	}
	for _, stmt := range block.Stmts {
		if err := e.emitStmt(stmt, depth); err != nil {
			return err
		}
	}
	return nil
}

// emitFnBody emits the statements of a function body.
// Unlike emitBlock, it handles implicit tail returns: when the last statement
// is a bare ExprStmt in a non-unit, non-main function, it is emitted as
// "return <expr>;" so that functions whose last expression is a match/must
// expression produce correct C instead of discarding the value.
func (e *emitter) emitFnBody(block *parser.BlockStmt, depth int) error {
	stmts := block.Stmts
	if len(stmts) == 0 {
		return nil
	}
	// Emit all but the last statement normally.
	for _, stmt := range stmts[:len(stmts)-1] {
		if err := e.emitStmt(stmt, depth); err != nil {
			return err
		}
	}
	// Emit the last statement. If it is a bare expression in a non-unit
	// function, wrap it in a return so that tail match/must expressions
	// return their value instead of silently discarding it.
	// Exception: if the expression has unit/never type (e.g. a match where all
	// arms explicitly return), emit it as a plain statement — wrapping a void
	// statement expression in "return ..." produces a GCC error.
	last := stmts[len(stmts)-1]
	if !e.retIsUnit && !e.isMain {
		if es, ok := last.(*parser.ExprStmt); ok {
			exprType := e.res.ExprTypes[es.X]
			isVoidLike := exprType == nil || exprType.Equals(typeck.TUnit) || exprType.Equals(typeck.TNever)
			if !isVoidLike {
				return e.emitTailReturn(es.X, depth)
			}
		}
	}
	return e.emitStmt(last, depth)
}

// emitTailReturn emits an implicit tail return for a function body expression:
//
//	return <expr>;
//
// It respects any ensures clauses on the current function, using the same
// wrapping logic as the explicit ReturnStmt path in emitStmt.
func (e *emitter) emitTailReturn(expr parser.Expr, depth int) error {
	ind := indent(depth)
	// Collect ensures clauses (mirrors ReturnStmt handling in emitStmt).
	var ensures []parser.ContractClause
	for _, cc := range e.contracts {
		if cc.Kind == parser.ContractEnsures {
			ensures = append(ensures, cc)
		}
	}
	if len(ensures) > 0 {
		ct, err := e.cType(e.retType)
		if err != nil {
			return err
		}
		e.writef("%s{\n", ind)
		e.writef("%s    %s _cnd_result = ", ind, ct)
		if err := e.emitExpr(expr, &e.sb); err != nil {
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
		e.writef("%s    return _cnd_result;\n", ind)
		e.writef("%s}\n", ind)
		return nil
	}
	e.write(ind + "return ")
	if err := e.emitExpr(expr, &e.sb); err != nil {
		return err
	}
	e.write(";\n")
	return nil
}

func (e *emitter) emitStmt(stmt parser.Stmt, depth int) error {
	ind := indent(depth)
	switch s := stmt.(type) {
	case *parser.LetStmt:
		return e.emitLetStmt(s, depth)

	case *parser.ReturnStmt:
		// Spawn thunk mode: store result in task struct instead of returning.
		if e.spawnTaskVar != "" {
			taskVar := e.spawnTaskVar
			if s.Value == nil || isUnitValue(s.Value) {
				e.writef("%s%s->_ok = 1;\n", ind, taskVar)
				e.writef("%sreturn NULL;\n", ind)
			} else {
				e.writef("%s%s->_result = ", ind, taskVar)
				if err := e.emitExpr(s.Value, &e.sb); err != nil {
					return err
				}
				e.write(";\n")
				e.writef("%s%s->_ok = 1;\n", ind, taskVar)
				e.writef("%sreturn NULL;\n", ind)
			}
			return nil
		}
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

	case *parser.ContinueStmt:
		e.writef("%scontinue;\n", ind)

	case *parser.BlockStmt:
		e.writef("%s{\n", ind)
		if err := e.emitBlock(s, depth+1); err != nil {
			return err
		}
		e.writef("%s}\n", ind)

	case *parser.AssignStmt:
		if e.byRefCaptures[s.Name.Lexeme] {
			// By-ref capture: write through the pointer.
			e.writef("%s*%s = ", indent(depth), s.Name.Lexeme)
		} else {
			e.writef("%s%s = ", indent(depth), s.Name.Lexeme)
		}
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

	case *parser.IndexAssignStmt:
		collType := e.res.ExprTypes[s.Target.Collection]
		e.write(ind)
		if gen, ok := collType.(*typeck.GenType); ok && gen.Con == "map" && len(gen.Params) == 2 {
			// m[k] = v  →  _cnd_map_insert_KM_VM(&(m), k, v);
			kC, err := e.cType(gen.Params[0])
			if err != nil {
				return err
			}
			vC, err := e.cType(gen.Params[1])
			if err != nil {
				return err
			}
			e.write(e.mapInsertFnName(kC, vC) + "(&(")
			if err := e.emitExpr(s.Target.Collection, &e.sb); err != nil {
				return err
			}
			e.write("), ")
			if err := e.emitExpr(s.Target.Index, &e.sb); err != nil {
				return err
			}
			e.write(", ")
			if err := e.emitExpr(s.Value, &e.sb); err != nil {
				return err
			}
			e.write(");\n")
		} else if gen, ok := collType.(*typeck.GenType); ok && gen.Con == "ring" {
			// ring[i] = v → { _CndRing_T* _r = &(coll); _r->_data[(_r->_head + i) % _r->_cap] = v; }
			rTmp := e.freshTmp()
			ringC, err := e.cType(collType)
			if err != nil {
				return err
			}
			e.writef("{ %s* %s = &(", ringC, rTmp)
			if err := e.emitExpr(s.Target.Collection, &e.sb); err != nil {
				return err
			}
			e.writef("); %s->_data[(%s->_head + (", rTmp, rTmp)
			if err := e.emitExpr(s.Target.Index, &e.sb); err != nil {
				return err
			}
			e.writef(")) %% %s->_cap] = ", rTmp)
			if err := e.emitExpr(s.Value, &e.sb); err != nil {
				return err
			}
			e.write("; }\n")
		} else if gen, ok := collType.(*typeck.GenType); ok && gen.Con == "vec" {
			e.write("(")
			if err := e.emitExpr(s.Target.Collection, &e.sb); err != nil {
				return err
			}
			e.write(")._data[")
			if err := e.emitExpr(s.Target.Index, &e.sb); err != nil {
				return err
			}
			e.write("] = ")
			if err := e.emitExpr(s.Value, &e.sb); err != nil {
				return err
			}
			e.write(";\n")
		} else {
			if err := e.emitExpr(s.Target.Collection, &e.sb); err != nil {
				return err
			}
			e.write("[")
			if err := e.emitExpr(s.Target.Index, &e.sb); err != nil {
				return err
			}
			e.write("] = ")
			if err := e.emitExpr(s.Value, &e.sb); err != nil {
				return err
			}
			e.write(";\n")
		}

	case *parser.AssertStmt:
		e.write(ind + "assert(")
		if err := e.emitExpr(s.Expr, &e.sb); err != nil {
			return err
		}
		e.write(");\n")

	case *parser.WhileStmt:
		var condB strings.Builder
		if err := e.emitExpr(s.Cond, &condB); err != nil {
			return err
		}
		e.writef("%swhile (%s) {\n", ind, condB.String())
		if err := e.emitBlock(s.Body, depth+1); err != nil {
			return err
		}
		e.writef("%s}\n", ind)

	case *parser.ForStmt:
		return e.emitForStmt(s, depth)

	case *parser.TupleDestructureStmt:
		// let (a, b) = expr  →  { TupleType _tmp = expr; T0 a = _tmp._0; T1 b = _tmp._1; }
		tupleType := e.res.ExprTypes[s.Value]
		tt := tupleType.(*typeck.TupleType)
		tC, err := e.cType(tupleType)
		if err != nil {
			return err
		}
		tmpName := e.freshTmp()
		e.writef("%s{\n", ind)
		var valB strings.Builder
		if err := e.emitExpr(s.Value, &valB); err != nil {
			return err
		}
		e.writef("%s    %s %s = %s;\n", ind, tC, tmpName, valB.String())
		for i, name := range s.Names {
			elemC, err := e.cType(tt.Elems[i])
			if err != nil {
				return err
			}
			e.writef("%s    %s %s = %s._%d;\n", ind, elemC, name.Lexeme, tmpName, i)
		}
		e.writef("%s}\n", ind)

	default:
		return fmt.Errorf("unhandled Stmt %T", stmt)
	}
	return nil
}

func (e *emitter) emitForStmt(s *parser.ForStmt, depth int) error {
	ind := indent(depth)
	collType := e.res.ExprTypes[s.Collection]
	gen := collType.(*typeck.GenType) // validated by typeck

	if s.Var2 != nil {
		// for k, v in map<K,V>
		kC, err := e.cType(gen.Params[0])
		if err != nil {
			return err
		}
		vC, err := e.cType(gen.Params[1])
		if err != nil {
			return err
		}
		mapTypeName := e.mapTypeName(kC, vC)
		entryName := e.mapEntryName(kC, vC)
		var collB strings.Builder
		if err := e.emitExpr(s.Collection, &collB); err != nil {
			return err
		}
		mTmp := e.freshTmp()
		biTmp := e.freshTmp()
		enTmp := e.freshTmp()
		e.writef("%s{\n", ind)
		e.writef("%s    %s %s = %s;\n", ind, mapTypeName, mTmp, collB.String())
		e.writef("%s    if (%s._buckets) {\n", ind, mTmp)
		e.writef("%s        for (uint64_t %s = 0; %s < %s._cap; %s++) {\n",
			ind, biTmp, biTmp, mTmp, biTmp)
		e.writef("%s            %s* %s = %s._buckets[%s];\n",
			ind, entryName, enTmp, mTmp, biTmp)
		e.writef("%s            while (%s) {\n", ind, enTmp)
		e.writef("%s                %s %s = %s->_key;\n", ind, kC, s.Var.Lexeme, enTmp)
		e.writef("%s                %s %s = %s->_val;\n", ind, vC, s.Var2.Lexeme, enTmp)
		if err := e.emitBlock(s.Body, depth+4); err != nil {
			return err
		}
		e.writef("%s                %s = %s->_next;\n", ind, enTmp, enTmp)
		e.writef("%s            }\n", ind)
		e.writef("%s        }\n", ind)
		e.writef("%s    }\n", ind)
		e.writef("%s}\n", ind)
		return nil
	}

	// set<T> iteration: walk all non-null bucket chains
	if gen.Con == "set" {
		eC, err := e.cType(gen.Params[0])
		if err != nil {
			return err
		}
		setTypeName := e.setTypeName(eC)
		entryName := e.setEntryName(eC)
		var collB strings.Builder
		if err := e.emitExpr(s.Collection, &collB); err != nil {
			return err
		}
		sTmp := e.freshTmp()
		biTmp := e.freshTmp()
		enTmp := e.freshTmp()
		e.writef("%s{\n", ind)
		e.writef("%s    %s %s = %s;\n", ind, setTypeName, sTmp, collB.String())
		e.writef("%s    if (%s._buckets) {\n", ind, sTmp)
		e.writef("%s        for (uint64_t %s = 0; %s < %s._cap; %s++) {\n",
			ind, biTmp, biTmp, sTmp, biTmp)
		e.writef("%s            %s* %s = %s._buckets[%s];\n",
			ind, entryName, enTmp, sTmp, biTmp)
		e.writef("%s            while (%s) {\n", ind, enTmp)
		e.writef("%s                %s %s = %s->_key;\n", ind, eC, s.Var.Lexeme, enTmp)
		if err := e.emitBlock(s.Body, depth+4); err != nil {
			return err
		}
		e.writef("%s                %s = %s->_next;\n", ind, enTmp, enTmp)
		e.writef("%s            }\n", ind)
		e.writef("%s        }\n", ind)
		e.writef("%s    }\n", ind)
		e.writef("%s}\n", ind)
		return nil
	}

	// ring<T> iteration — modular index arithmetic
	if gen.Con == "ring" {
		elemC, err := e.cType(gen.Params[0])
		if err != nil {
			return err
		}
		ringC, err := e.cType(collType)
		if err != nil {
			return err
		}
		var collB strings.Builder
		if err := e.emitExpr(s.Collection, &collB); err != nil {
			return err
		}
		rTmp := e.freshTmp()
		iTmp := e.freshTmp()
		e.writef("%s{\n", ind)
		e.writef("%s    %s %s = %s;\n", ind, ringC, rTmp, collB.String())
		e.writef("%s    for (uint64_t %s = 0; %s < %s._len; %s++) {\n",
			ind, iTmp, iTmp, rTmp, iTmp)
		e.writef("%s        %s %s = %s._data[(%s._head + %s) %% %s._cap];\n",
			ind, elemC, s.Var.Lexeme, rTmp, rTmp, iTmp, rTmp)
		if err := e.emitBlock(s.Body, depth+2); err != nil {
			return err
		}
		e.writef("%s    }\n", ind)
		e.writef("%s}\n", ind)
		return nil
	}

	// vec iteration
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
	// Unit-typed let bindings (e.g. `let _ = expr must { ... none => unit }`) should
	// not emit a C variable since void variables are invalid in C. Emit as a statement.
	if ct == "void" {
		e.write(indent(depth))
		if err := e.emitExpr(s.Value, &e.sb); err != nil {
			return err
		}
		e.write(";\n")
		return nil
	}
	name := s.Name.Lexeme
	if e.declaredVars != nil && e.declaredVars[name] {
		// Variable already declared in this C scope — emit as assignment.
		e.writef("%s%s = ", indent(depth), name)
	} else {
		e.writef("%s%s %s = ", indent(depth), ct, name)
		if e.declaredVars != nil {
			e.declaredVars[name] = true
		}
	}
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
		ee := &emitter{res: e.res, retIsUnit: e.retIsUnit, isMain: e.isMain, retType: e.retType, declaredVars: make(map[string]bool)}
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
		} else if name == "none" {
			// none is the absent value for option<T>; represented as NULL in C.
			sb.WriteString("NULL")
		} else if e.inEnsures && name == "result" {
			sb.WriteString("_cnd_result")
		} else if e.byRefCaptures[name] {
			// By-ref capture: the C variable is a T*, so dereference for reads.
			sb.WriteString("(*")
			sb.WriteString(name)
			sb.WriteByte(')')
		} else if ft, isFn := e.res.ExprTypes[ex].(*typeck.FnType); isFn {
			// Named function used as a first-class value (e.g. passed as argument
			// or returned).  Wrap via the trampoline so the C types match the
			// closure struct's (params..., void* _env) signature.
			if _, isNamedFn := e.res.FnSigs[name]; isNamedFn {
				fnTypN, err := e.fnTypeName(ft)
				if err != nil {
					return err
				}
				fmt.Fprintf(sb, "(%s){ ._fn = _cnd_tramp_%s, ._env = NULL }", fnTypN, name)
			} else {
				sb.WriteString(name)
			}
		} else {
			sb.WriteString(name)
		}

	case *parser.BinaryExpr:
		// String == and != must use strcmp, not pointer comparison.
		if ex.Op.Type == lexer.TokEqEq || ex.Op.Type == lexer.TokBangEq {
			if ltype := e.res.ExprTypes[ex.Left]; ltype != nil && ltype.Equals(typeck.TStr) {
				var lsb, rsb strings.Builder
				if err := e.emitExpr(ex.Left, &lsb); err != nil {
					return err
				}
				if err := e.emitExpr(ex.Right, &rsb); err != nil {
					return err
				}
				if ex.Op.Type == lexer.TokEqEq {
					sb.WriteString(fmt.Sprintf("(strcmp(%s, %s) == 0)", lsb.String(), rsb.String()))
				} else {
					sb.WriteString(fmt.Sprintf("(strcmp(%s, %s) != 0)", lsb.String(), rsb.String()))
				}
				break
			}
		}
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

	case *parser.PropagateExpr:
		return e.emitPropagateExpr(ex, sb)

	case *parser.PipeExpr:
		// |> desugars to fn(lhs). Apply the same direct-call vs fat-pointer
		// logic used by CallExpr so named functions emit without a trampoline.
		if inst, ok := e.res.CallSiteGeneric[ex.Fn]; ok {
			sb.WriteString(inst.MangledName)
			sb.WriteByte('(')
			if err := e.emitExpr(ex.X, sb); err != nil {
				return err
			}
			sb.WriteByte(')')
			break
		}
		isFatPtrPipe := false
		if ident, ok := ex.Fn.(*parser.IdentExpr); ok {
			name := ident.Tok.Lexeme
			if _, isNamed := e.res.FnSigs[name]; !isNamed {
				if _, isExtern := e.res.ExternFns[name]; !isExtern {
					if _, isFnType := e.res.ExprTypes[ex.Fn].(*typeck.FnType); isFnType {
						isFatPtrPipe = true
					}
				}
			}
		} else if _, isFnType := e.res.ExprTypes[ex.Fn].(*typeck.FnType); isFnType {
			isFatPtrPipe = true
		}
		if isFatPtrPipe {
			if ident, ok := ex.Fn.(*parser.IdentExpr); ok {
				name := ident.Tok.Lexeme
				sb.WriteString(name)
				sb.WriteString("._fn(")
				if err := e.emitExpr(ex.X, sb); err != nil {
					return err
				}
				sb.WriteString(", ")
				sb.WriteString(name)
				sb.WriteString("._env)")
			} else {
				fnTypN, _ := e.fnTypeName(e.res.ExprTypes[ex.Fn].(*typeck.FnType))
				sb.WriteString("(__extension__ ({ ")
				sb.WriteString(fnTypN)
				sb.WriteString(" _pf = ")
				if err := e.emitExpr(ex.Fn, sb); err != nil {
					return err
				}
				sb.WriteString("; _pf._fn(")
				if err := e.emitExpr(ex.X, sb); err != nil {
					return err
				}
				sb.WriteString(", _pf._env); }))")
			}
		} else {
			if ident, ok := ex.Fn.(*parser.IdentExpr); ok {
				sb.WriteString(ident.Tok.Lexeme)
			} else {
				if err := e.emitExpr(ex.Fn, sb); err != nil {
					return err
				}
			}
			sb.WriteByte('(')
			if err := e.emitExpr(ex.X, sb); err != nil {
				return err
			}
			sb.WriteByte(')')
		}

	case *parser.ReturnExpr:
		sb.WriteString("return ")
		if err := e.emitExpr(ex.Value, sb); err != nil {
			return err
		}

	case *parser.BreakExpr:
		sb.WriteString("break")

	case *parser.BlockExpr:
		// Multi-statement match arm block.
		// Emitted as a GNU statement expression: ({ stmt1; stmt2; ...; 0; })
		// emitStmt always writes to e.sb (the global builder), but we need
		// the content in the local `sb` (the expression's buffer).
		// Strategy: snapshot e.sb length, emit stmts, then slice the new bytes
		// out of e.sb and append them to `sb`, then roll back e.sb.
		sb.WriteString("({\n")
		before := e.sb.Len()
		for _, stmt := range ex.Stmts {
			if err := e.emitStmt(stmt, 2); err != nil {
				return err
			}
		}
		e.writeln(indent(2) + "0;")
		// Save both halves before touching e.sb — sb may be &e.sb itself.
		full := e.sb.String()
		stmtsStr := full[before:]
		oldStr := full[:before]
		e.sb.Reset()
		e.sb.WriteString(oldStr)
		sb.WriteString(stmtsStr)
		sb.WriteString(indent(1) + "})")

	case *parser.CallExpr:
		// Comptime-evaluated pure function call — emit constant directly.
		if v, ok := e.res.ComptimeValues[ex]; ok {
			emitComptimeConst(v, sb)
			return nil
		}
		// task<T>.join() — blocks until the thread finishes, returns result<T, str>.
		if T, ok := e.res.TaskJoins[ex]; ok {
			fe := ex.Fn.(*parser.FieldExpr)
			taskTyp, err := e.taskTypeName(T)
			if err != nil {
				return err
			}
			resultGen := &typeck.GenType{Con: "result", Params: []typeck.Type{T, typeck.TStr}}
			if err := e.ensureTypeDependenciesEmitted(resultGen); err != nil {
				return err
			}
			resTyp, err := e.resultTypeName(resultGen)
			if err != nil {
				return err
			}
			tC, err := e.cType(T)
			if err != nil {
				return err
			}
			var recvSB strings.Builder
			if err := e.emitExpr(fe.Receiver, &recvSB); err != nil {
				return err
			}
			sb.WriteString("(__extension__ ({\n")
			sb.WriteString(fmt.Sprintf("    %s* _jt = %s;\n", taskTyp, recvSB.String()))
			sb.WriteString("    pthread_join(_jt->_thread, NULL);\n")
			sb.WriteString(fmt.Sprintf("    %s _jr = {0};\n", resTyp))
			if tC == "void" {
				sb.WriteString("    if (_jt->_ok) { _jr._ok = 1; } else { _jr._ok = 0; _jr._err_val = _jt->_err; }\n")
			} else {
				sb.WriteString("    if (_jt->_ok) { _jr._ok = 1; _jr._ok_val = _jt->_result; } else { _jr._ok = 0; _jr._err_val = _jt->_err; }\n")
			}
			sb.WriteString("    free(_jt);\n")
			sb.WriteString("    _jr;\n")
			sb.WriteString("}))")
			return nil
		}
		// Method call: registered by typeck as StructName_method(receiver, args...)
		if mangledName, ok := e.res.MethodCalls[ex]; ok {
			sb.WriteString(mangledName)
			sb.WriteByte('(')
			// Emit receiver (self) first.
			if fe, isFE := ex.Fn.(*parser.FieldExpr); isFE {
				if err := e.emitExpr(fe.Receiver, sb); err != nil {
					return err
				}
			}
			for _, arg := range ex.Args {
				sb.WriteString(", ")
				if err := e.emitExpr(arg, sb); err != nil {
					return err
				}
			}
			sb.WriteByte(')')
			return nil
		}
		// Enum variant constructor: Shape::Circle(2.0) → Shape_Circle(_0)
		if path, ok := ex.Fn.(*parser.PathExpr); ok {
			fmt.Fprintf(sb, "%s_%s(", path.Head.Lexeme, path.Tail.Lexeme)
			for i, arg := range ex.Args {
				if i > 0 {
					sb.WriteString(", ")
				}
				if err := e.emitExpr(arg, sb); err != nil {
					return err
				}
			}
			sb.WriteByte(')')
			return nil
		}
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
			if ident.Tok.Lexeme == "map_new" {
				t := e.res.ExprTypes[ident]
				if gen, ok2 := t.(*typeck.GenType); ok2 && gen.Con == "map" && len(gen.Params) == 2 {
					kC, err := e.cType(gen.Params[0])
					if err != nil {
						return err
					}
					vC, err := e.cType(gen.Params[1])
					if err != nil {
						return err
					}
					fmt.Fprintf(sb, "%s()", e.mapNewFnName(kC, vC))
					return nil
				}
			}
			if ident.Tok.Lexeme == "set_new" {
				t := e.res.ExprTypes[ident]
				if gen, ok2 := t.(*typeck.GenType); ok2 && gen.Con == "set" && len(gen.Params) == 1 {
					eC, err := e.cType(gen.Params[0])
					if err != nil {
						return err
					}
					fmt.Fprintf(sb, "%s()", e.setNewFnName(eC))
					return nil
				}
			}
			if ident.Tok.Lexeme == "ring_new" {
				t := e.res.ExprTypes[ident]
				if gen, ok2 := t.(*typeck.GenType); ok2 && gen.Con == "ring" && len(gen.Params) == 1 {
					elemC, err := e.cType(gen.Params[0])
					if err != nil {
						return err
					}
					// ring_new(cap) → _cnd_ring_new_T(cap)
					var capB strings.Builder
					if len(ex.Args) == 1 {
						if err := e.emitExpr(ex.Args[0], &capB); err != nil {
							return err
						}
					}
					fmt.Fprintf(sb, "%s(%s)", e.ringNewFnName(elemC), capB.String())
					return nil
				}
			}
			if ident.Tok.Lexeme == "arc_new" {
				t := e.res.ExprTypes[ident]
				if gen, ok2 := t.(*typeck.GenType); ok2 && gen.Con == "arc" && len(gen.Params) == 1 {
					innerC, err := e.cType(gen.Params[0])
					if err != nil {
						return err
					}
					// arc_new(val) → alloc [int64 refcount][T value], set refcount=1, return T*
					fmt.Fprintf(sb, "({ int64_t* __arc = malloc(8+sizeof(%s)); *__arc = 1; %s* __p = (%s*)(__arc+1); *__p = ", innerC, innerC, innerC)
					if len(ex.Args) == 1 {
						if err := e.emitExpr(ex.Args[0], sb); err != nil {
							return err
						}
					}
					fmt.Fprintf(sb, "; __p; })")
					return nil
				}
			}
			if ident.Tok.Lexeme == "box_new" {
				t := e.res.ExprTypes[ident]
				if gen, ok2 := t.(*typeck.GenType); ok2 && gen.Con == "box" && len(gen.Params) == 1 {
					innerC, err := e.cType(gen.Params[0])
					if err != nil {
						return err
					}
					// box_new(val) → malloc + store using GCC statement expression
					fmt.Fprintf(sb, "({ %s* __b = malloc(sizeof(%s)); *__b = ", innerC, innerC)
					if len(ex.Args) == 1 {
						if err := e.emitExpr(ex.Args[0], sb); err != nil {
							return err
						}
					}
					fmt.Fprintf(sb, "; __b; })")
					return nil
				}
			}
			if ident.Tok.Lexeme == "tensor_zeros" {
				t := e.res.ExprTypes[ident]
				if gen, ok2 := t.(*typeck.GenType); ok2 && gen.Con == "tensor" && len(gen.Params) == 1 {
					elemC, err := e.cType(gen.Params[0])
					if err != nil {
						return err
					}
					name := e.tensorTypeName(elemC)
					vecI64 := e.vecTypeName("int64_t")
					var shapeB strings.Builder
					if len(ex.Args) == 1 {
						if err := e.emitExpr(ex.Args[0], &shapeB); err != nil {
							return err
						}
					}
					// tensor_zeros(shape) → allocate zero-filled data + copy shape dims
					fmt.Fprintf(sb,
						"(__extension__ ({ %s _shp = (%s); int64_t _sz = 1; "+
							"for (uint64_t _i = 0; _i < _shp._len; _i++) _sz *= _shp._data[_i]; "+
							"%s _t; _t._ndim = (int64_t)_shp._len; "+
							"_t._shape = (int64_t*)malloc(_shp._len * sizeof(int64_t)); "+
							"for (uint64_t _i = 0; _i < _shp._len; _i++) _t._shape[_i] = _shp._data[_i]; "+
							"_t._size = _sz; _t._data = (%s*)calloc((size_t)_sz, sizeof(%s)); _t; }))",
						vecI64, shapeB.String(), name, elemC, elemC)
					return nil
				}
			}
			if ident.Tok.Lexeme == "tensor_from_vec" {
				t := e.res.ExprTypes[ident]
				if gen, ok2 := t.(*typeck.GenType); ok2 && gen.Con == "tensor" && len(gen.Params) == 1 {
					elemC, err := e.cType(gen.Params[0])
					if err != nil {
						return err
					}
					name := e.tensorTypeName(elemC)
					vecI64 := e.vecTypeName("int64_t")
					vecElem := e.vecTypeName(elemC)
					var dataB, shapeB strings.Builder
					if len(ex.Args) >= 1 {
						if err := e.emitExpr(ex.Args[0], &dataB); err != nil {
							return err
						}
					}
					if len(ex.Args) >= 2 {
						if err := e.emitExpr(ex.Args[1], &shapeB); err != nil {
							return err
						}
					}
					fmt.Fprintf(sb,
						"(__extension__ ({ %s _d = (%s); %s _shp = (%s); "+
							"int64_t _sz = 1; "+
							"for (uint64_t _i = 0; _i < _shp._len; _i++) _sz *= _shp._data[_i]; "+
							"%s _t; _t._ndim = (int64_t)_shp._len; "+
							"_t._shape = (int64_t*)malloc(_shp._len * sizeof(int64_t)); "+
							"for (uint64_t _i = 0; _i < _shp._len; _i++) _t._shape[_i] = _shp._data[_i]; "+
							"_t._size = _sz; _t._data = (%s*)malloc((size_t)_sz * sizeof(%s)); "+
							"for (int64_t _i = 0; _i < _sz && _i < (int64_t)_d._len; _i++) _t._data[_i] = _d._data[_i]; "+
							"_t; }))",
						vecElem, dataB.String(), vecI64, shapeB.String(), name, elemC, elemC)
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
		// If this is a generic instance call, emit using the mangled name.
		if inst, ok := e.res.CallSiteGeneric[ex.Fn]; ok {
			sb.WriteString(inst.MangledName)
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
			break
		}

		// Check if this is a fat-pointer closure call (fn-typed variable, not a named fn).
		isFatPtrCall := false
		if ident, ok := ex.Fn.(*parser.IdentExpr); ok {
			name := ident.Tok.Lexeme
			if _, isNamed := e.res.FnSigs[name]; !isNamed {
				if _, isExtern := e.res.ExternFns[name]; !isExtern {
					if _, isFnType := e.res.ExprTypes[ex.Fn].(*typeck.FnType); isFnType {
						isFatPtrCall = true
					}
				}
			}
		} else if _, isFnType := e.res.ExprTypes[ex.Fn].(*typeck.FnType); isFnType {
			// Non-ident expression of fn type (e.g. field access returning a lambda).
			isFatPtrCall = true
		}

		if isFatPtrCall {
			// Fat-pointer call: f._fn(args..., f._env)
			// Emit fn expression into a temp via __extension__ statement expr to avoid double-eval.
			if ident, ok := ex.Fn.(*parser.IdentExpr); ok {
				// Simple ident — safe to reference twice.
				name := ident.Tok.Lexeme
				sb.WriteString(name)
				sb.WriteString("._fn(")
				for i, arg := range ex.Args {
					if i > 0 {
						sb.WriteString(", ")
					}
					if err := e.emitExpr(arg, sb); err != nil {
						return err
					}
				}
				if len(ex.Args) > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(name)
				sb.WriteString("._env)")
			} else {
				// Complex expression — use GNU statement expr to avoid double evaluation.
				fnTypN, _ := e.fnTypeName(e.res.ExprTypes[ex.Fn].(*typeck.FnType))
				sb.WriteString("(__extension__ ({ ")
				sb.WriteString(fnTypN)
				sb.WriteString(" _f = ")
				if err := e.emitExpr(ex.Fn, sb); err != nil {
					return err
				}
				sb.WriteString("; _f._fn(")
				for i, arg := range ex.Args {
					if i > 0 {
						sb.WriteString(", ")
					}
					if err := e.emitExpr(arg, sb); err != nil {
						return err
					}
				}
				if len(ex.Args) > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString("_f._env); }))")
			}
		} else {
			// Direct (non-fat-ptr) call.  Emit the callee name directly — do NOT
			// go through emitExpr for the callee because emitExpr would wrap a named
			// function ident in a trampoline struct literal, which is not callable.
			if ident, ok := ex.Fn.(*parser.IdentExpr); ok {
				sb.WriteString(ident.Tok.Lexeme)
			} else {
				if err := e.emitExpr(ex.Fn, sb); err != nil {
					return err
				}
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
		}

	case *parser.FieldExpr:
		recvType := e.res.ExprTypes[ex.Receiver]
		// Tuple field access: x.0 → x._0
		if ex.Field.Type == lexer.TokInt {
			sb.WriteByte('(')
			if err := e.emitExpr(ex.Receiver, sb); err != nil {
				return err
			}
			fmt.Fprintf(sb, ")._%s", ex.Field.Lexeme)
			break
		}
		if err := e.emitExpr(ex.Receiver, sb); err != nil {
			return err
		}
		if gen, ok := recvType.(*typeck.GenType); ok &&
			(gen.Con == "ref" || gen.Con == "refmut") {
			sb.WriteString("->")
		} else {
			sb.WriteByte('.')
		}
		sb.WriteString(ex.Field.Lexeme)

	case *parser.IndexExpr:
		collType := e.res.ExprTypes[ex.Collection]
		if gen, ok := collType.(*typeck.GenType); ok && gen.Con == "ring" {
			// ring[i] → ring._data[(ring._head + i) % ring._cap]
			// Use a GNU statement expression to avoid double-evaluation.
			var collB strings.Builder
			if err := e.emitExpr(ex.Collection, &collB); err != nil {
				return err
			}
			var idxB strings.Builder
			if err := e.emitExpr(ex.Index, &idxB); err != nil {
				return err
			}
			ringC, err := e.cType(collType)
			if err != nil {
				return err
			}
			fmt.Fprintf(sb, "(__extension__ ({ %s _r = %s; _r._data[(_r._head + (%s)) %% _r._cap]; }))",
				ringC, collB.String(), idxB.String())
		} else if gen, ok := collType.(*typeck.GenType); ok && gen.Con == "vec" {
			// vec are structs; elements are in the ._data array.
			sb.WriteByte('(')
			if err := e.emitExpr(ex.Collection, sb); err != nil {
				return err
			}
			sb.WriteString(")._data[")
			if err := e.emitExpr(ex.Index, sb); err != nil {
				return err
			}
			sb.WriteByte(']')
		} else if collType == typeck.TStr {
			// str[i] → (uint8_t)((s)[i])
			sb.WriteString("(uint8_t)((")
			if err := e.emitExpr(ex.Collection, sb); err != nil {
				return err
			}
			sb.WriteString(")[")
			if err := e.emitExpr(ex.Index, sb); err != nil {
				return err
			}
			sb.WriteString("])")
		} else {
			if err := e.emitExpr(ex.Collection, sb); err != nil {
				return err
			}
			sb.WriteByte('[')
			if err := e.emitExpr(ex.Index, sb); err != nil {
				return err
			}
			sb.WriteByte(']')
		}

	case *parser.StructLitExpr:
		// Use the resolved C type name (handles module-prefixed types like typeck_Type).
		exprType := e.res.ExprTypes[ex]
		cStructName, err := e.cType(exprType)
		if err != nil {
			return err
		}
		if ex.Base != nil {
			// Struct update: ({ TypeName _tmp = base; _tmp.f1 = v1; ...; _tmp; })
			tmpName := e.freshTmp()
			sb.WriteString("({ ")
			sb.WriteString(cStructName)
			sb.WriteString(" ")
			sb.WriteString(tmpName)
			sb.WriteString(" = ")
			if err := e.emitExpr(ex.Base, sb); err != nil {
				return err
			}
			sb.WriteString("; ")
			for _, fi := range ex.Fields {
				sb.WriteString(tmpName)
				sb.WriteByte('.')
				sb.WriteString(fi.Name.Lexeme)
				sb.WriteString(" = ")
				if err := e.emitExpr(fi.Value, sb); err != nil {
					return err
				}
				sb.WriteString("; ")
			}
			sb.WriteString(tmpName)
			sb.WriteString("; })")
		} else {
			sb.WriteByte('(')
			sb.WriteString(cStructName)
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
		}

	case *parser.PathExpr:
		// Unit enum variant: Shape::Point → Shape_Point()
		sb.WriteString(ex.Head.Lexeme)
		sb.WriteByte('_')
		sb.WriteString(ex.Tail.Lexeme)
		sb.WriteString("()")

	case *parser.LambdaExpr:
		// Lambda expression emits a fat-pointer struct value.
		for _, lam := range e.res.Lambdas {
			if lam.Node != ex {
				continue
			}
			fnTypN, err := e.fnTypeName(lam.Sig)
			if err != nil {
				return err
			}
			if len(lam.Captures) == 0 {
				// Non-capturing: wrap impl function in fat pointer with NULL env.
				fmt.Fprintf(sb, "(%s){ ._fn = %s_impl, ._env = NULL }", fnTypN, lam.Name)
			} else {
				// Capturing: call the maker which heap-allocates the env struct.
				sb.WriteString(lam.Name)
				sb.WriteString("_make(")
				for i, cap := range lam.Captures {
					if i > 0 {
						sb.WriteString(", ")
					}
					// By-ref captures pass a pointer to the outer variable.
					if i < len(lam.CaptureByRef) && lam.CaptureByRef[i] {
						sb.WriteString("&(")
						sb.WriteString(cap)
						sb.WriteString(")")
					} else {
						sb.WriteString(cap)
					}
				}
				sb.WriteByte(')')
			}
			return nil
		}
		return fmt.Errorf("lambda not found in result")

	case *parser.SpawnExpr:
		// spawn { body } — heap-allocate task struct, start pthread, return task pointer.
		var sp *typeck.SpawnInfo
		for _, s := range e.res.Spawns {
			if s.Node == ex {
				sp = s
				break
			}
		}
		if sp == nil {
			return fmt.Errorf("no SpawnInfo for spawn expression")
		}
		taskTyp, err := e.taskTypeName(sp.ResultType)
		if err != nil {
			return err
		}
		fnName := sp.Name + "_fn"
		ctxType := sp.Name + "_ctx"
		sb.WriteString("(__extension__ ({\n")
		sb.WriteString(fmt.Sprintf("    %s* _t = (%s*)malloc(sizeof(%s));\n", taskTyp, taskTyp, taskTyp))
		sb.WriteString("    _t->_ok = 0;\n")
		sb.WriteString(fmt.Sprintf("    %s* _ctx = (%s*)malloc(sizeof(%s));\n", ctxType, ctxType, ctxType))
		sb.WriteString("    _ctx->_task = _t;\n")
		for _, cap := range sp.Captures {
			sb.WriteString(fmt.Sprintf("    _ctx->%s = %s;\n", cap, cap))
		}
		sb.WriteString(fmt.Sprintf("    pthread_create(&_t->_thread, NULL, %s, _ctx);\n", fnName))
		sb.WriteString("    _t;\n")
		sb.WriteString("}))")
		return nil

	case *parser.OldExpr:
		// old(expr) emits the pre-captured variable name.
		if varName, ok := e.oldVars[ex]; ok {
			sb.WriteString(varName)
			return nil
		}
		return fmt.Errorf("old() used outside ensures context or not captured")

	case *parser.CastExpr:
		targetC, err := e.cType(e.res.ExprTypes[ex])
		if err != nil {
			return err
		}
		fmt.Fprintf(sb, "((%s)(", targetC)
		if err := e.emitExpr(ex.X, sb); err != nil {
			return err
		}
		sb.WriteString("))")

	case *parser.VecLitExpr:
		return e.emitVecLitExpr(ex, sb)

	case *parser.TupleLitExpr:
		return e.emitTupleLitExpr(ex, sb)

	case *parser.ForallExpr:
		return e.emitForallExpr(ex, sb)

	case *parser.ExistsExpr:
		return e.emitExistsExpr(ex, sb)

	default:
		return fmt.Errorf("unhandled Expr %T in emit", expr)
	}
	return nil
}

// ── forall / exists quantifier expressions ──────────────────────────────────

func (e *emitter) emitForallExpr(ex *parser.ForallExpr, sb *strings.Builder) error {
	collType := e.res.ExprTypes[ex.Collection]
	gen := collType.(*typeck.GenType)
	elemC, err := e.cType(gen.Params[0])
	if err != nil { return err }
	collC, err := e.cType(collType)
	if err != nil { return err }
	var collB strings.Builder
	if err := e.emitExpr(ex.Collection, &collB); err != nil { return err }
	cTmp := e.freshTmp()
	iTmp := e.freshTmp()
	sb.WriteString("({ " + collC + " " + cTmp + " = " + collB.String() + "; bool _all = true; ")
	if gen.Con == "ring" {
		sb.WriteString("for (uint64_t " + iTmp + " = 0; " + iTmp + " < " + cTmp + "._len; " + iTmp + "++) { ")
		sb.WriteString(elemC + " " + ex.Var.Lexeme + " = " + cTmp + "._data[(" + cTmp + "._head + " + iTmp + ") % " + cTmp + "._cap]; ")
	} else {
		sb.WriteString("for (uint64_t " + iTmp + " = 0; " + iTmp + " < " + cTmp + "._len; " + iTmp + "++) { ")
		sb.WriteString(elemC + " " + ex.Var.Lexeme + " = " + cTmp + "._data[" + iTmp + "]; ")
	}
	sb.WriteString("if (!(")
	if err := e.emitExpr(ex.Pred, sb); err != nil { return err }
	sb.WriteString(")) { _all = false; break; } } _all; })")
	return nil
}

func (e *emitter) emitExistsExpr(ex *parser.ExistsExpr, sb *strings.Builder) error {
	collType := e.res.ExprTypes[ex.Collection]
	gen := collType.(*typeck.GenType)
	elemC, err := e.cType(gen.Params[0])
	if err != nil { return err }
	collC, err := e.cType(collType)
	if err != nil { return err }
	var collB strings.Builder
	if err := e.emitExpr(ex.Collection, &collB); err != nil { return err }
	cTmp := e.freshTmp()
	iTmp := e.freshTmp()
	sb.WriteString("({ " + collC + " " + cTmp + " = " + collB.String() + "; bool _any = false; ")
	if gen.Con == "ring" {
		sb.WriteString("for (uint64_t " + iTmp + " = 0; " + iTmp + " < " + cTmp + "._len; " + iTmp + "++) { ")
		sb.WriteString(elemC + " " + ex.Var.Lexeme + " = " + cTmp + "._data[(" + cTmp + "._head + " + iTmp + ") % " + cTmp + "._cap]; ")
	} else {
		sb.WriteString("for (uint64_t " + iTmp + " = 0; " + iTmp + " < " + cTmp + "._len; " + iTmp + "++) { ")
		sb.WriteString(elemC + " " + ex.Var.Lexeme + " = " + cTmp + "._data[" + iTmp + "]; ")
	}
	sb.WriteString("if (")
	if err := e.emitExpr(ex.Pred, sb); err != nil { return err }
	sb.WriteString(") { _any = true; break; } } _any; })")
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
		sb.WriteString("(")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString(")._len")
		return true, nil

	case "vec_pop":
		// vec_pop(v) → _cnd_vec_pop_T(&(v))
		if len(args) != 1 {
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
		popFn := "_cnd_vec_pop_" + e.mangle(elemC)
		sb.WriteString(popFn)
		sb.WriteString("(&(")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString("))")
		return true, nil

	case "map_insert":
		// map_insert(m, k, v) → _cnd_map_insert_KM_VM(&(m), k, v)
		if len(args) != 3 {
			return false, nil
		}
		mapType, ok := e.res.ExprTypes[args[0]].(*typeck.GenType)
		if !ok || mapType.Con != "map" || len(mapType.Params) != 2 {
			return false, nil
		}
		kC, err := e.cType(mapType.Params[0])
		if err != nil {
			return true, err
		}
		vC, err := e.cType(mapType.Params[1])
		if err != nil {
			return true, err
		}
		sb.WriteString(e.mapInsertFnName(kC, vC))
		sb.WriteString("(&(")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString("), ")
		if err := e.emitExpr(args[1], sb); err != nil {
			return true, err
		}
		sb.WriteString(", ")
		if err := e.emitExpr(args[2], sb); err != nil {
			return true, err
		}
		sb.WriteByte(')')
		return true, nil

	case "map_get":
		// map_get(m, k) → _cnd_map_get_KM_VM(m, k)
		if len(args) != 2 {
			return false, nil
		}
		mapType, ok := e.res.ExprTypes[args[0]].(*typeck.GenType)
		if !ok || mapType.Con != "map" || len(mapType.Params) != 2 {
			return false, nil
		}
		kC, err := e.cType(mapType.Params[0])
		if err != nil {
			return true, err
		}
		vC, err := e.cType(mapType.Params[1])
		if err != nil {
			return true, err
		}
		sb.WriteString(e.mapGetFnName(kC, vC))
		sb.WriteByte('(')
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString(", ")
		if err := e.emitExpr(args[1], sb); err != nil {
			return true, err
		}
		sb.WriteByte(')')
		return true, nil

	case "map_remove":
		// map_remove(m, k) → _cnd_map_remove_KM_VM(&(m), k)
		if len(args) != 2 {
			return false, nil
		}
		mapType, ok := e.res.ExprTypes[args[0]].(*typeck.GenType)
		if !ok || mapType.Con != "map" || len(mapType.Params) != 2 {
			return false, nil
		}
		kC, err := e.cType(mapType.Params[0])
		if err != nil {
			return true, err
		}
		vC, err := e.cType(mapType.Params[1])
		if err != nil {
			return true, err
		}
		sb.WriteString(e.mapRemoveFnName(kC, vC))
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

	case "map_len":
		// map_len(m) → (m)._len
		if len(args) != 1 {
			return false, nil
		}
		sb.WriteByte('(')
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString(")._len")
		return true, nil

	case "map_contains":
		// map_contains(m, k) → _cnd_map_contains_KM_VM(m, k)
		if len(args) != 2 {
			return false, nil
		}
		mapType, ok := e.res.ExprTypes[args[0]].(*typeck.GenType)
		if !ok || mapType.Con != "map" || len(mapType.Params) != 2 {
			return false, nil
		}
		kC, err := e.cType(mapType.Params[0])
		if err != nil {
			return true, err
		}
		vC, err := e.cType(mapType.Params[1])
		if err != nil {
			return true, err
		}
		sb.WriteString(e.mapContainsFnName(kC, vC))
		sb.WriteByte('(')
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString(", ")
		if err := e.emitExpr(args[1], sb); err != nil {
			return true, err
		}
		sb.WriteByte(')')
		return true, nil

	case "set_add":
		// set_add(s, v) → _cnd_set_add_EC(&(s), v)
		if len(args) != 2 {
			return false, nil
		}
		setType, ok := e.res.ExprTypes[args[0]].(*typeck.GenType)
		if !ok || setType.Con != "set" || len(setType.Params) != 1 {
			return false, nil
		}
		eC, err := e.cType(setType.Params[0])
		if err != nil {
			return true, err
		}
		sb.WriteString(e.setAddFnName(eC))
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

	case "set_remove":
		// set_remove(s, v) → _cnd_set_remove_EC(&(s), v)
		if len(args) != 2 {
			return false, nil
		}
		setType, ok := e.res.ExprTypes[args[0]].(*typeck.GenType)
		if !ok || setType.Con != "set" || len(setType.Params) != 1 {
			return false, nil
		}
		eC, err := e.cType(setType.Params[0])
		if err != nil {
			return true, err
		}
		sb.WriteString(e.setRemoveFnName(eC))
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

	case "set_contains":
		// set_contains(s, v) → _cnd_set_contains_EC(s, v)
		if len(args) != 2 {
			return false, nil
		}
		setType, ok := e.res.ExprTypes[args[0]].(*typeck.GenType)
		if !ok || setType.Con != "set" || len(setType.Params) != 1 {
			return false, nil
		}
		eC, err := e.cType(setType.Params[0])
		if err != nil {
			return true, err
		}
		sb.WriteString(e.setContainsFnName(eC))
		sb.WriteByte('(')
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString(", ")
		if err := e.emitExpr(args[1], sb); err != nil {
			return true, err
		}
		sb.WriteByte(')')
		return true, nil

	case "set_len":
		// set_len(s) → (s)._len
		if len(args) != 1 {
			return false, nil
		}
		sb.WriteByte('(')
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString(")._len")
		return true, nil

	case "ring_new":
		// ring_new(cap) — handled in emitExpr via the ident type
		return false, nil

	case "ring_push_back":
		// ring_push_back(r, val) → _cnd_ring_push_back_T(&(r), val)
		if len(args) != 2 {
			return false, nil
		}
		ringType, ok := e.res.ExprTypes[args[0]].(*typeck.GenType)
		if !ok || ringType.Con != "ring" || len(ringType.Params) != 1 {
			return false, nil
		}
		elemC, err := e.cType(ringType.Params[0])
		if err != nil {
			return true, err
		}
		sb.WriteString(e.ringPushBackFnName(elemC))
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

	case "ring_pop_front":
		// ring_pop_front(r) → _cnd_ring_pop_front_T(&(r))
		if len(args) != 1 {
			return false, nil
		}
		ringType, ok := e.res.ExprTypes[args[0]].(*typeck.GenType)
		if !ok || ringType.Con != "ring" || len(ringType.Params) != 1 {
			return false, nil
		}
		elemC, err := e.cType(ringType.Params[0])
		if err != nil {
			return true, err
		}
		sb.WriteString(e.ringPopFrontFnName(elemC))
		sb.WriteString("(&(")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteByte(')')
		return true, nil

	case "ring_len":
		// ring_len(r) → (r)._len
		if len(args) != 1 {
			return false, nil
		}
		sb.WriteByte('(')
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString(")._len")
		return true, nil

	case "ring_is_empty":
		// ring_is_empty(r) → (r)._len == 0
		if len(args) != 1 {
			return false, nil
		}
		sb.WriteByte('(')
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString(")._len == 0")
		return true, nil

	case "box_deref":
		// box_deref(b) → (*b)
		if len(args) != 1 {
			return false, nil
		}
		sb.WriteString("(*")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteByte(')')
		return true, nil

	case "box_drop":
		// box_drop(b) → free(b)
		if len(args) != 1 {
			return false, nil
		}
		sb.WriteString("free(")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteByte(')')
		return true, nil

	case "arc_clone":
		// arc_clone(a) → atomically increment refcount, return same pointer
		if len(args) != 1 {
			return false, nil
		}
		sb.WriteString("({ __typeof__(")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString(") __ac = ")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString("; __sync_fetch_and_add((int64_t*)((char*)__ac - 8), 1); __ac; })")
		return true, nil

	case "arc_deref":
		// arc_deref(a) → (*a)
		if len(args) != 1 {
			return false, nil
		}
		sb.WriteString("(*")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteByte(')')
		return true, nil

	case "arc_drop":
		// arc_drop(a) → decrement refcount, free block if zero
		if len(args) != 1 {
			return false, nil
		}
		sb.WriteString("({ __typeof__(")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString(") __ad = ")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString("; if (__sync_sub_and_fetch((int64_t*)((char*)__ad - 8), 1) == 0) free((char*)__ad - 8); })")
		return true, nil

	case "tensor_to_vec":
		// tensor_to_vec(t) → copy _data into a new vec<T>
		if len(args) != 1 {
			return false, nil
		}
		t := e.res.ExprTypes[args[0]]
		// unwrap ref<tensor<T>>
		if ref, ok := t.(*typeck.GenType); ok && ref.Con == "ref" && len(ref.Params) == 1 {
			t = ref.Params[0]
		}
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "tensor" || len(gen.Params) != 1 {
			return false, nil
		}
		elemC, err := e.cType(gen.Params[0])
		if err != nil {
			return true, err
		}
		vecT := e.vecTypeName(elemC)
		pushFn := e.vecPushName(elemC)
		var argB strings.Builder
		if err := e.emitExpr(args[0], &argB); err != nil {
			return true, err
		}
		fmt.Fprintf(sb,
			"(__extension__ ({ __typeof__(%s) __ts = (%s); %s _v = {._data=NULL,._len=0,._cap=0}; "+
				"for (int64_t _i = 0; _i < __ts._size; _i++) { %s(&_v, __ts._data[_i]); } _v; }))",
			argB.String(), argB.String(), vecT, pushFn)
		return true, nil

	case "tensor_get":
		// tensor_get(t, idx) → t._data[flat_index(idx)]
		if len(args) != 2 {
			return false, nil
		}
		t := e.res.ExprTypes[args[0]]
		if ref, ok := t.(*typeck.GenType); ok && ref.Con == "ref" && len(ref.Params) == 1 {
			t = ref.Params[0]
		}
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "tensor" || len(gen.Params) != 1 {
			return false, nil
		}
		vecI64 := e.vecTypeName("int64_t")
		var tB, idxB strings.Builder
		if err := e.emitExpr(args[0], &tB); err != nil {
			return true, err
		}
		if err := e.emitExpr(args[1], &idxB); err != nil {
			return true, err
		}
		// Use address-of for value type; if already a pointer use directly.
		fmt.Fprintf(sb,
			"(__extension__ ({ __auto_type _tg = &(%s); %s _idx = (%s); "+
				"int64_t _fi = 0; "+
				"for (int64_t _d = 0; _d < _tg->_ndim; _d++) { "+
				"int64_t _str = 1; for (int64_t _k = _d+1; _k < _tg->_ndim; _k++) _str *= _tg->_shape[_k]; "+
				"_fi += _idx._data[_d] * _str; } "+
				"_tg->_data[_fi]; }))",
			tB.String(), vecI64, idxB.String())
		return true, nil

	case "tensor_set":
		// tensor_set(t, idx, val) → t._data[flat_index(idx)] = val
		if len(args) != 3 {
			return false, nil
		}
		t := e.res.ExprTypes[args[0]]
		if ref, ok := t.(*typeck.GenType); ok && ref.Con == "ref" && len(ref.Params) == 1 {
			t = ref.Params[0]
		}
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "tensor" || len(gen.Params) != 1 {
			return false, nil
		}
		vecI64 := e.vecTypeName("int64_t")
		var tB, idxB, valB strings.Builder
		if err := e.emitExpr(args[0], &tB); err != nil {
			return true, err
		}
		if err := e.emitExpr(args[1], &idxB); err != nil {
			return true, err
		}
		if err := e.emitExpr(args[2], &valB); err != nil {
			return true, err
		}
		fmt.Fprintf(sb,
			"(__extension__ ({ __auto_type _ts = &(%s); %s _idx = (%s); "+
				"int64_t _fi = 0; "+
				"for (int64_t _d = 0; _d < _ts->_ndim; _d++) { "+
				"int64_t _str = 1; for (int64_t _k = _d+1; _k < _ts->_ndim; _k++) _str *= _ts->_shape[_k]; "+
				"_fi += _idx._data[_d] * _str; } "+
				"_ts->_data[_fi] = (%s); }))",
			tB.String(), vecI64, idxB.String(), valB.String())
		return true, nil

	case "tensor_ndim":
		// tensor_ndim(t) → t._ndim
		if len(args) != 1 {
			return false, nil
		}
		var argB strings.Builder
		if err := e.emitExpr(args[0], &argB); err != nil {
			return true, err
		}
		fmt.Fprintf(sb, "((__auto_type _tn = &(%s), _tn->_ndim))", argB.String())
		return true, nil

	case "tensor_len":
		// tensor_len(t) → t._size
		if len(args) != 1 {
			return false, nil
		}
		var argB strings.Builder
		if err := e.emitExpr(args[0], &argB); err != nil {
			return true, err
		}
		fmt.Fprintf(sb, "((__auto_type _tl = &(%s), _tl->_size))", argB.String())
		return true, nil

	case "tensor_shape":
		// tensor_shape(t) → copy _shape array into vec<i64>
		if len(args) != 1 {
			return false, nil
		}
		vecI64 := e.vecTypeName("int64_t")
		pushFn := e.vecPushName("int64_t")
		var argB strings.Builder
		if err := e.emitExpr(args[0], &argB); err != nil {
			return true, err
		}
		fmt.Fprintf(sb,
			"(__extension__ ({ __auto_type _tsh = &(%s); %s _v = {._data=NULL,._len=0,._cap=0}; "+
				"for (int64_t _i = 0; _i < _tsh->_ndim; _i++) { %s(&_v, _tsh->_shape[_i]); } _v; }))",
			argB.String(), vecI64, pushFn)
		return true, nil

	case "tensor_free":
		// tensor_free(t) → free shape + data
		if len(args) != 1 {
			return false, nil
		}
		var argB strings.Builder
		if err := e.emitExpr(args[0], &argB); err != nil {
			return true, err
		}
		fmt.Fprintf(sb,
			"(__extension__ ({ __auto_type _tf = (%s); free(_tf._shape); free(_tf._data); }))",
			argB.String())
		return true, nil

	case "tensor_dot":
		// tensor_dot(a, b) → sum of a[i]*b[i] over all elements
		if len(args) != 2 {
			return false, nil
		}
		t := e.res.ExprTypes[args[0]]
		if ref, ok := t.(*typeck.GenType); ok && ref.Con == "ref" && len(ref.Params) == 1 {
			t = ref.Params[0]
		}
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "tensor" || len(gen.Params) != 1 {
			return false, nil
		}
		elemC, err := e.cType(gen.Params[0])
		if err != nil {
			return true, err
		}
		var aB, bB strings.Builder
		if err := e.emitExpr(args[0], &aB); err != nil {
			return true, err
		}
		if err := e.emitExpr(args[1], &bB); err != nil {
			return true, err
		}
		fmt.Fprintf(sb,
			"(__extension__ ({ __auto_type _da = &(%s); __auto_type _db = &(%s); "+
				"%s _acc = 0; "+
				"int64_t _n = _da->_size < _db->_size ? _da->_size : _db->_size; "+
				"_Pragma(\"GCC ivdep\") "+
				"for (int64_t _i = 0; _i < _n; _i++) _acc += _da->_data[_i] * _db->_data[_i]; "+
				"_acc; }))",
			aB.String(), bB.String(), elemC)
		return true, nil

	case "tensor_l2":
		// tensor_l2(a) → sqrt(sum(a[i]^2))
		if len(args) != 1 {
			return false, nil
		}
		t := e.res.ExprTypes[args[0]]
		if ref, ok := t.(*typeck.GenType); ok && ref.Con == "ref" && len(ref.Params) == 1 {
			t = ref.Params[0]
		}
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "tensor" || len(gen.Params) != 1 {
			return false, nil
		}
		elemC, err := e.cType(gen.Params[0])
		if err != nil {
			return true, err
		}
		var aB strings.Builder
		if err := e.emitExpr(args[0], &aB); err != nil {
			return true, err
		}
		fmt.Fprintf(sb,
			"(__extension__ ({ __auto_type _la = &(%s); "+
				"%s _acc = 0; "+
				"_Pragma(\"GCC ivdep\") "+
				"for (int64_t _i = 0; _i < _la->_size; _i++) _acc += _la->_data[_i] * _la->_data[_i]; "+
				"(%s)sqrt((double)_acc); }))",
			aB.String(), elemC, elemC)
		return true, nil

	case "tensor_cosine":
		// tensor_cosine(a, b) → dot(a,b) / (l2(a) * l2(b))
		if len(args) != 2 {
			return false, nil
		}
		t := e.res.ExprTypes[args[0]]
		if ref, ok := t.(*typeck.GenType); ok && ref.Con == "ref" && len(ref.Params) == 1 {
			t = ref.Params[0]
		}
		gen, ok := t.(*typeck.GenType)
		if !ok || gen.Con != "tensor" || len(gen.Params) != 1 {
			return false, nil
		}
		elemC, err := e.cType(gen.Params[0])
		if err != nil {
			return true, err
		}
		var aB, bB strings.Builder
		if err := e.emitExpr(args[0], &aB); err != nil {
			return true, err
		}
		if err := e.emitExpr(args[1], &bB); err != nil {
			return true, err
		}
		fmt.Fprintf(sb,
			"(__extension__ ({ __auto_type _ca = &(%s); __auto_type _cb = &(%s); "+
				"%s _dot = 0, _na = 0, _nb = 0; "+
				"int64_t _n = _ca->_size < _cb->_size ? _ca->_size : _cb->_size; "+
				"_Pragma(\"GCC ivdep\") "+
				"for (int64_t _i = 0; _i < _n; _i++) { "+
				"_dot += _ca->_data[_i] * _cb->_data[_i]; "+
				"_na += _ca->_data[_i] * _ca->_data[_i]; "+
				"_nb += _cb->_data[_i] * _cb->_data[_i]; } "+
				"(%s)(_dot / (sqrt((double)_na) * sqrt((double)_nb) + 1e-12)); }))",
			aB.String(), bB.String(), elemC, elemC)
		return true, nil

	case "tensor_matmul":
		// tensor_matmul(a, b, out) — a[M,K] * b[K,N] → out[M,N]  (row-major)
		// out is written, not returned; result is unit (no value to write to sb).
		if len(args) != 3 {
			return false, nil
		}
		var aB, bB, outB strings.Builder
		if err := e.emitExpr(args[0], &aB); err != nil {
			return true, err
		}
		if err := e.emitExpr(args[1], &bB); err != nil {
			return true, err
		}
		if err := e.emitExpr(args[2], &outB); err != nil {
			return true, err
		}
		fmt.Fprintf(sb,
			"(__extension__ ({ __auto_type _ma = &(%s); __auto_type _mb = &(%s); __auto_type _mo = &(%s); "+
				"int64_t _M = _ma->_shape[0], _K = _ma->_shape[1], _N = _mb->_shape[1]; "+
				"for (int64_t _i = 0; _i < _M; _i++) "+
				"for (int64_t _j = 0; _j < _N; _j++) { "+
				"__auto_type _s = _mo->_data[_i*_N+_j]; "+
				"_Pragma(\"GCC ivdep\") "+
				"for (int64_t _k = 0; _k < _K; _k++) _s += _ma->_data[_i*_K+_k] * _mb->_data[_k*_N+_j]; "+
				"_mo->_data[_i*_N+_j] = _s; } }))",
			aB.String(), bB.String(), outB.String())
		return true, nil

	case "mmap_open":
		// mmap_open(path, byte_len) -> result<mmap<u8>, str>
		if len(args) != 2 {
			return false, nil
		}
		resType := &typeck.GenType{Con: "result", Params: []typeck.Type{
			&typeck.GenType{Con: "mmap", Params: []typeck.Type{typeck.TU8}},
			typeck.TStr,
		}}
		structName, err := e.resultTypeName(resType)
		if err != nil {
			return true, err
		}
		mmapName := e.mmapTypeName("uint8_t")
		var pathB, lenB strings.Builder
		if err := e.emitExpr(args[0], &pathB); err != nil {
			return true, err
		}
		if err := e.emitExpr(args[1], &lenB); err != nil {
			return true, err
		}
		fmt.Fprintf(sb,
			"(__extension__ ({ const char* _mp = (%s); uint64_t _ml = (uint64_t)(%s); "+
				"%s _mr = {0}; "+
				"#ifndef _WIN32 "+
				"int _mfd = open(_mp, O_RDWR|O_CREAT, 0644); "+
				"if (_mfd < 0) { _mr._ok = 0; _mr._err_val = \"mmap_open: open failed\"; } "+
				"else { ftruncate(_mfd, (off_t)_ml); "+
				"void* _mptr = mmap(NULL, _ml, PROT_READ|PROT_WRITE, MAP_SHARED, _mfd, 0); "+
				"if (_mptr == MAP_FAILED) { close(_mfd); _mr._ok = 0; _mr._err_val = \"mmap_open: mmap failed\"; } "+
				"else { _mr._ok = 1; _mr._ok_val = (%s){ ._data=(uint8_t*)_mptr, ._len=_ml, ._fd=_mfd }; } } "+
				"#else _mr._ok = 0; _mr._err_val = \"mmap_open: not supported on Windows\"; "+
				"#endif "+
				"_mr; }))",
			pathB.String(), lenB.String(), structName, mmapName)
		return true, nil

	case "mmap_anon":
		// mmap_anon(byte_len) -> result<mmap<u8>, str>  — anonymous mapping
		if len(args) != 1 {
			return false, nil
		}
		resType := &typeck.GenType{Con: "result", Params: []typeck.Type{
			&typeck.GenType{Con: "mmap", Params: []typeck.Type{typeck.TU8}},
			typeck.TStr,
		}}
		structName, err := e.resultTypeName(resType)
		if err != nil {
			return true, err
		}
		mmapName := e.mmapTypeName("uint8_t")
		var lenB strings.Builder
		if err := e.emitExpr(args[0], &lenB); err != nil {
			return true, err
		}
		fmt.Fprintf(sb,
			"(__extension__ ({ uint64_t _ml = (uint64_t)(%s); "+
				"%s _mr = {0}; "+
				"#ifndef _WIN32 "+
				"void* _mptr = mmap(NULL, _ml, PROT_READ|PROT_WRITE, MAP_ANONYMOUS|MAP_PRIVATE, -1, 0); "+
				"if (_mptr == MAP_FAILED) { _mr._ok = 0; _mr._err_val = \"mmap_anon: mmap failed\"; } "+
				"else { _mr._ok = 1; _mr._ok_val = (%s){ ._data=(uint8_t*)_mptr, ._len=_ml, ._fd=-1 }; } "+
				"#else _mr._ok = 0; _mr._err_val = \"mmap_anon: not supported on Windows\"; "+
				"#endif "+
				"_mr; }))",
			lenB.String(), structName, mmapName)
		return true, nil

	case "mmap_deref":
		// mmap_deref(m) → m._data  (pointer to the mapped region)
		if len(args) != 1 {
			return false, nil
		}
		var argB strings.Builder
		if err := e.emitExpr(args[0], &argB); err != nil {
			return true, err
		}
		fmt.Fprintf(sb, "((__auto_type _md = &(%s), _md->_data))", argB.String())
		return true, nil

	case "mmap_flush":
		// mmap_flush(m) → msync
		if len(args) != 1 {
			return false, nil
		}
		var argB strings.Builder
		if err := e.emitExpr(args[0], &argB); err != nil {
			return true, err
		}
		fmt.Fprintf(sb,
			"(__extension__ ({ __auto_type _mf = &(%s); "+
				"#ifndef _WIN32 msync(_mf->_data, _mf->_len, MS_SYNC); #endif }))",
			argB.String())
		return true, nil

	case "mmap_close":
		// mmap_close(m) → munmap + close fd
		if len(args) != 1 {
			return false, nil
		}
		var argB strings.Builder
		if err := e.emitExpr(args[0], &argB); err != nil {
			return true, err
		}
		fmt.Fprintf(sb,
			"(__extension__ ({ __auto_type _mc = (%s); "+
				"#ifndef _WIN32 munmap(_mc._data, _mc._len); if (_mc._fd >= 0) close(_mc._fd); #endif }))",
			argB.String())
		return true, nil

	case "mmap_len":
		// mmap_len(m) → m._len
		if len(args) != 1 {
			return false, nil
		}
		var argB strings.Builder
		if err := e.emitExpr(args[0], &argB); err != nil {
			return true, err
		}
		fmt.Fprintf(sb, "((__auto_type _mln = &(%s), _mln->_len))", argB.String())
		return true, nil

	case "print_char":
		if len(args) != 1 {
			return false, nil
		}
		sb.WriteString("putchar(")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString(")")
		return true, nil
	}

	// Zero-argument builtins.
	if len(args) == 0 {
		switch name {
		case "read_line":
			sb.WriteString("_cnd_read_line()")
			return true, nil
		case "read_int":
			sb.WriteString("(__extension__ ({ int64_t _v; scanf(\"%lld\", &_v); _v; }))")
			return true, nil
		case "read_f64":
			sb.WriteString("(__extension__ ({ double _v; scanf(\"%lf\", &_v); _v; }))")
			return true, nil
		case "try_read_line":
			sb.WriteString("_cnd_try_read_line()")
			return true, nil
		case "try_read_int":
			sb.WriteString("_cnd_try_read_int()")
			return true, nil
		case "try_read_f64":
			sb.WriteString("_cnd_try_read_f64()")
			return true, nil
		// M2.3 io
		case "flush_stdout":
			sb.WriteString("_cnd_flush_stdout()")
			return true, nil
		case "read_all_lines":
			// Returns vec<str>: read stdin lines until EOF.
			vecT := e.vecTypeName("const char*")
			pushFn := e.vecPushName("const char*")
			sb.WriteString(fmt.Sprintf(
				"(__extension__ ({ %s _v = {._data=NULL,._len=0,._cap=0}; "+
					"static char _lbuf[4096]; "+
					"while (fgets(_lbuf, sizeof(_lbuf), stdin)) { "+
					"size_t _l = strlen(_lbuf); "+
					"while (_l > 0 && (_lbuf[_l-1] == '\\n' || _lbuf[_l-1] == '\\r')) { _lbuf[--_l] = '\\0'; } "+
					"char* _ls = (char*)malloc(_l+1); memcpy(_ls, _lbuf, _l+1); "+
					"%s(&_v, (const char*)_ls); } _v; }))",
				vecT, pushFn))
			return true, nil
		case "read_csv_line":
			// Returns vec<str>: read one CSV line from stdin, split by comma.
			vecT := e.vecTypeName("const char*")
			pushFn := e.vecPushName("const char*")
			sb.WriteString(fmt.Sprintf(
				"(__extension__ ({ %s _v = {._data=NULL,._len=0,._cap=0}; "+
					"static char _cbuf[4096]; "+
					"if (fgets(_cbuf, sizeof(_cbuf), stdin)) { "+
					"size_t _l = strlen(_cbuf); "+
					"while (_l > 0 && (_cbuf[_l-1] == '\\n' || _cbuf[_l-1] == '\\r')) { _cbuf[--_l] = '\\0'; } "+
					"char* _cp = _cbuf; char* _cf; "+
					"while ((_cf = strchr(_cp, ',')) != NULL) { "+
					"size_t _cl = _cf - _cp; char* _cs = (char*)malloc(_cl+1); "+
					"memcpy(_cs, _cp, _cl); _cs[_cl] = '\\0'; "+
					"%s(&_v, (const char*)_cs); _cp = _cf + 1; } "+
					"char* _cl2 = (char*)malloc(strlen(_cp)+1); strcpy(_cl2, _cp); "+
					"%s(&_v, (const char*)_cl2); } _v; }))",
				vecT, pushFn, pushFn))
			return true, nil
		// M2.4 os
		case "os_args":
			// Returns vec<str> built from _cnd_argc/_cnd_argv.
			vecT := e.vecTypeName("const char*")
			pushFn := e.vecPushName("const char*")
			sb.WriteString(fmt.Sprintf(
				"(__extension__ ({ %s _v = {._data=NULL,._len=0,._cap=0}; "+
					"for (int _i = 0; _i < _cnd_argc; _i++) { "+
					"%s(&_v, (const char*)_cnd_argv[_i]); } _v; }))",
				vecT, pushFn))
			return true, nil
		case "os_cwd":
			sb.WriteString("_cnd_os_cwd()")
			return true, nil
		// M2.5 time
		case "time_now_ms":
			sb.WriteString("_cnd_time_now_ms()")
			return true, nil
		case "time_now_mono_ns":
			sb.WriteString("_cnd_time_now_mono_ns()")
			return true, nil
		// M2.6 rand
		case "rand_u64":
			sb.WriteString("_cnd_rand_u64()")
			return true, nil
		case "rand_f64":
			sb.WriteString("_cnd_rand_f64()")
			return true, nil
		}
	}

	// Two-argument string builtins.
	if len(args) == 2 {
		switch name {
		case "str_byte":
			var s0, s1 strings.Builder
			if err := e.emitExpr(args[0], &s0); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &s1); err != nil {
				return true, err
			}
			sb.WriteString(fmt.Sprintf("(uint8_t)((%s)[%s])", s0.String(), s1.String()))
			return true, nil
		case "str_concat":
			sb.WriteString("_cnd_str_concat(")
			if err := e.emitExpr(args[0], sb); err != nil {
				return true, err
			}
			sb.WriteString(", ")
			if err := e.emitExpr(args[1], sb); err != nil {
				return true, err
			}
			sb.WriteByte(')')
			return true, nil
		case "str_eq":
			sb.WriteString("(strcmp(")
			if err := e.emitExpr(args[0], sb); err != nil {
				return true, err
			}
			sb.WriteString(", ")
			if err := e.emitExpr(args[1], sb); err != nil {
				return true, err
			}
			sb.WriteString(") == 0)")
			return true, nil
		case "str_starts_with":
			// str_starts_with(s, prefix) → strncmp(s, prefix, strlen(prefix)) == 0
			var a0, a1 strings.Builder
			if err := e.emitExpr(args[0], &a0); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &a1); err != nil {
				return true, err
			}
			sb.WriteString(fmt.Sprintf(
				"(__extension__ ({ const char* _pfx = (%s); (strncmp(%s, _pfx, strlen(_pfx)) == 0); }))",
				a1.String(), a0.String()))
			return true, nil
		// M2.1 math (two-arg)
		case "math_pow":
			var a0, a1 strings.Builder
			if err := e.emitExpr(args[0], &a0); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &a1); err != nil {
				return true, err
			}
			sb.WriteString(fmt.Sprintf("pow(%s, %s)", a0.String(), a1.String()))
			return true, nil
		case "math_min_i64":
			var a0, a1 strings.Builder
			if err := e.emitExpr(args[0], &a0); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &a1); err != nil {
				return true, err
			}
			sb.WriteString(fmt.Sprintf(
				"(__extension__ ({ int64_t _a=%s,_b=%s; _a<_b?_a:_b; }))",
				a0.String(), a1.String()))
			return true, nil
		case "math_max_i64":
			var a0, a1 strings.Builder
			if err := e.emitExpr(args[0], &a0); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &a1); err != nil {
				return true, err
			}
			sb.WriteString(fmt.Sprintf(
				"(__extension__ ({ int64_t _a=%s,_b=%s; _a>_b?_a:_b; }))",
				a0.String(), a1.String()))
			return true, nil
		case "math_min_f64":
			var a0, a1 strings.Builder
			if err := e.emitExpr(args[0], &a0); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &a1); err != nil {
				return true, err
			}
			sb.WriteString(fmt.Sprintf(
				"(__extension__ ({ double _a=%s,_b=%s; _a<_b?_a:_b; }))",
				a0.String(), a1.String()))
			return true, nil
		case "math_max_f64":
			var a0, a1 strings.Builder
			if err := e.emitExpr(args[0], &a0); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &a1); err != nil {
				return true, err
			}
			sb.WriteString(fmt.Sprintf(
				"(__extension__ ({ double _a=%s,_b=%s; _a>_b?_a:_b; }))",
				a0.String(), a1.String()))
			return true, nil
		// M2.2 str (two-arg)
		case "str_repeat":
			var a0, a1 strings.Builder
			if err := e.emitExpr(args[0], &a0); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &a1); err != nil {
				return true, err
			}
			sb.WriteString(fmt.Sprintf("_cnd_str_repeat(%s, %s)", a0.String(), a1.String()))
			return true, nil
		case "str_split":
			var a0, a1 strings.Builder
			if err := e.emitExpr(args[0], &a0); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &a1); err != nil {
				return true, err
			}
			vecT := e.vecTypeName("const char*")
			pushFn := e.vecPushName("const char*")
			sb.WriteString(fmt.Sprintf(
				"(__extension__ ({ const char* _ss = (%s); const char* _sep = (%s); "+
					"size_t _sl = strlen(_sep); "+
					"%s _v = {._data=NULL,._len=0,._cap=0}; "+
					"if (_sl == 0) { for (size_t _i = 0; _ss[_i]; _i++) { "+
					"char* _c = (char*)malloc(2); _c[0] = _ss[_i]; _c[1] = '\\0'; "+
					"%s(&_v, (const char*)_c); } } else { "+
					"const char* _cur = _ss; const char* _fnd; "+
					"while ((_fnd = strstr(_cur, _sep)) != NULL) { "+
					"size_t _cl = _fnd - _cur; char* _ch = (char*)malloc(_cl+1); "+
					"memcpy(_ch, _cur, _cl); _ch[_cl] = '\\0'; "+
					"%s(&_v, (const char*)_ch); _cur = _fnd + _sl; } "+
					"char* _last = (char*)malloc(strlen(_cur)+1); strcpy(_last, _cur); "+
					"%s(&_v, (const char*)_last); } _v; }))",
				a0.String(), a1.String(), vecT, pushFn, pushFn, pushFn))
			return true, nil
		case "str_contains":
			var a0, a1 strings.Builder
			if err := e.emitExpr(args[0], &a0); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &a1); err != nil {
				return true, err
			}
			sb.WriteString(fmt.Sprintf("(strstr(%s, %s) != NULL)", a0.String(), a1.String()))
			return true, nil
		// M2.7 path (two-arg)
		case "path_join":
			var a0, a1 strings.Builder
			if err := e.emitExpr(args[0], &a0); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &a1); err != nil {
				return true, err
			}
			sb.WriteString(fmt.Sprintf("_cnd_path_join(%s, %s)", a0.String(), a1.String()))
			return true, nil
		// M2.6 rand (two-arg)
		case "rand_range":
			var a0, a1 strings.Builder
			if err := e.emitExpr(args[0], &a0); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &a1); err != nil {
				return true, err
			}
			sb.WriteString(fmt.Sprintf(
				"(__extension__ ({ int64_t _lo = (%s), _hi = (%s); _lo + (int64_t)(_cnd_rand_u64() %% (uint64_t)(_hi - _lo)); }))",
				a0.String(), a1.String()))
			return true, nil
		case "write_file", "append_file":
			resType := &typeck.GenType{Con: "result", Params: []typeck.Type{typeck.TUnit, typeck.TStr}}
			structName, err := e.resultTypeName(resType)
			if err != nil {
				return true, err
			}
			var a0SB, a1SB strings.Builder
			if err := e.emitExpr(args[0], &a0SB); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &a1SB); err != nil {
				return true, err
			}
			helper := "_cnd_write_file"
			failMsg := "write_file failed"
			if name == "append_file" {
				helper = "_cnd_append_file"
				failMsg = "append_file failed"
			}
			sb.WriteString(fmt.Sprintf(
				"(__extension__ ({ int _r = %s(%s, %s); "+
					"_r == 0 ? (%s){ ._ok=1 } : (%s){ ._ok=0, ._err_val=\"%s\" }; }))",
				helper, a0SB.String(), a1SB.String(), structName, structName, failMsg))
			return true, nil
		}
	}

	// Three-argument builtins.
	if len(args) == 3 {
		switch name {
		case "str_replace":
			var a0, a1, a2 strings.Builder
			if err := e.emitExpr(args[0], &a0); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &a1); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[2], &a2); err != nil {
				return true, err
			}
			sb.WriteString(fmt.Sprintf("_cnd_str_replace(%s, %s, %s)", a0.String(), a1.String(), a2.String()))
			return true, nil
		case "math_clamp_i64":
			var a0, a1, a2 strings.Builder
			if err := e.emitExpr(args[0], &a0); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &a1); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[2], &a2); err != nil {
				return true, err
			}
			sb.WriteString(fmt.Sprintf(
				"(__extension__ ({ int64_t _x=%s,_lo=%s,_hi=%s; _x<_lo?_lo:_x>_hi?_hi:_x; }))",
				a0.String(), a1.String(), a2.String()))
			return true, nil
		case "math_clamp_f64":
			var a0, a1, a2 strings.Builder
			if err := e.emitExpr(args[0], &a0); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &a1); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[2], &a2); err != nil {
				return true, err
			}
			sb.WriteString(fmt.Sprintf(
				"(__extension__ ({ double _x=%s,_lo=%s,_hi=%s; _x<_lo?_lo:_x>_hi?_hi:_x; }))",
				a0.String(), a1.String(), a2.String()))
			return true, nil
		case "str_substr":
			// str_substr(s, start, len) -> str — heap copy of s[start..start+len]
			var a0, a1, a2 strings.Builder
			if err := e.emitExpr(args[0], &a0); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &a1); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[2], &a2); err != nil {
				return true, err
			}
			sb.WriteString(fmt.Sprintf(
				"(__extension__ ({ const char* _b = (%s); int64_t _st = (%s); int64_t _ln = (%s); char* _r = (char*)malloc(_ln + 1); memcpy(_r, _b + _st, (size_t)_ln); _r[_ln] = '\\0'; (const char*)_r; }))",
				a0.String(), a1.String(), a2.String()))
			return true, nil
		case "str_find":
			// str_find(haystack, needle, start) -> option<i64> (i64* or NULL)
			var a0, a1, a2 strings.Builder
			if err := e.emitExpr(args[0], &a0); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[1], &a1); err != nil {
				return true, err
			}
			if err := e.emitExpr(args[2], &a2); err != nil {
				return true, err
			}
			sb.WriteString(fmt.Sprintf(
				"(__extension__ ({ const char* _hay = (%s); const char* _ndl = (%s); int64_t _st = (%s); const char* _p = strstr(_hay + _st, _ndl); int64_t* _res = NULL; if (_p) { _res = (int64_t*)malloc(sizeof(int64_t)); *_res = (int64_t)(_p - _hay); } _res; }))",
				a0.String(), a1.String(), a2.String()))
			return true, nil
		}
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
	case "str_len":
		sb.WriteString("(int64_t)strlen(")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteByte(')')
	case "str_from_u8":
		// str_from_u8(c: u8) -> str  — heap-allocate a 1-char string
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf(
			"(__extension__ ({ uint8_t _c = (uint8_t)(%s); char* _s = (char*)malloc(2); _s[0] = (char)_c; _s[1] = '\\0'; (const char*)_s; }))",
			argSB.String()))
	case "str_to_int":
		resType := &typeck.GenType{Con: "result", Params: []typeck.Type{typeck.TI64, typeck.TStr}}
		structName, err := e.resultTypeName(resType)
		if err != nil {
			return true, err
		}
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		arg := argSB.String()
		sb.WriteString(fmt.Sprintf(
			"(__extension__ ({ char* _end; int64_t _v = (int64_t)strtoll(%s, &_end, 10); "+
				"(*_end == '\\0') ? (%s){ ._ok=1, ._ok_val=_v } : (%s){ ._ok=0, ._err_val=\"invalid integer\" }; }))",
			arg, structName, structName))
	case "int_to_str":
		sb.WriteString("_cnd_int_to_str(")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteByte(')')
	case "read_file":
		resType := &typeck.GenType{Con: "result", Params: []typeck.Type{typeck.TStr, typeck.TStr}}
		structName, err := e.resultTypeName(resType)
		if err != nil {
			return true, err
		}
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		arg := argSB.String()
		sb.WriteString(fmt.Sprintf(
			"(__extension__ ({ const char* _r = _cnd_read_file(%s); "+
				"_r ? (%s){ ._ok=1, ._ok_val=_r } : (%s){ ._ok=0, ._err_val=\"read_file failed\" }; }))",
			arg, structName, structName))
	// M9.12 os_exec
	case "os_exec":
		resType := &typeck.GenType{Con: "result", Params: []typeck.Type{typeck.TI64, typeck.TStr}}
		structName, err := e.resultTypeName(resType)
		if err != nil {
			return true, err
		}
		vecT := e.vecTypeName("const char*")
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		arg := argSB.String()
		sb.WriteString(fmt.Sprintf(
			"(__extension__ ({ %s _exec_argv = (%s); int _exec_ok = 0; const char* _exec_err = NULL; "+
				"int64_t _exec_code = _cnd_os_exec(&_exec_argv, &_exec_ok, &_exec_err); "+
				"_exec_ok ? (%s){ ._ok=1, ._ok_val=_exec_code } : (%s){ ._ok=0, ._err_val=_exec_err }; }))",
			vecT, arg, structName, structName))

	// M2.1 math (one-arg)
	case "math_abs_i64":
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf("(__extension__ ({ int64_t _v = (%s); _v < 0 ? -_v : _v; }))", argSB.String()))
	case "math_abs_f64":
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf("fabs(%s)", argSB.String()))
	case "math_sqrt":
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf("sqrt(%s)", argSB.String()))
	case "math_floor":
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf("floor(%s)", argSB.String()))
	case "math_ceil":
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf("ceil(%s)", argSB.String()))
	case "math_sin":
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf("sin(%s)", argSB.String()))
	case "math_cos":
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf("cos(%s)", argSB.String()))

	// M2.2 str (one-arg)
	case "str_trim":
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf("_cnd_str_trim(%s)", argSB.String()))
	case "str_to_upper":
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf("_cnd_str_to_upper(%s)", argSB.String()))
	case "str_to_lower":
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf("_cnd_str_to_lower(%s)", argSB.String()))

	// M2.3 io (one-arg)
	case "print_err":
		sb.WriteString("fprintf(stderr, \"%s\\n\", ")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteByte(')')

	// M2.4 os (one-arg)
	case "os_exit":
		sb.WriteString("exit((int)(")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString("))")
	case "os_getenv":
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf(
			"(__extension__ ({ const char* _eg = getenv(%s); "+
				"const char** _ep = NULL; "+
				"if (_eg) { _ep = (const char**)malloc(sizeof(const char*)); *_ep = _eg; } "+
				"_ep; }))",
			argSB.String()))

	// M2.5 time (one-arg)
	case "time_sleep_ms":
		sb.WriteString("_cnd_time_sleep_ms(")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteByte(')')

	// M2.6 rand (one-arg)
	case "rand_set_seed":
		sb.WriteString("_cnd_rand_set_seed((uint64_t)(")
		if err := e.emitExpr(args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString("))")

	// M2.7 path (one-arg)
	case "path_dir":
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf("_cnd_path_dir(%s)", argSB.String()))
	case "path_filename":
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf("_cnd_path_filename(%s)", argSB.String()))
	case "path_ext":
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf("_cnd_path_ext(%s)", argSB.String()))
	case "path_exists":
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf("_cnd_path_exists(%s)", argSB.String()))
	case "path_mkdir":
		resType := &typeck.GenType{Con: "result", Params: []typeck.Type{typeck.TUnit, typeck.TStr}}
		structName, err := e.resultTypeName(resType)
		if err != nil {
			return true, err
		}
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf(
			"(__extension__ ({ int _mr = _cnd_mkdir(%s); "+
				"_mr == 0 ? (%s){ ._ok=1 } : (%s){ ._ok=0, ._err_val=\"mkdir failed\" }; }))",
			argSB.String(), structName, structName))
	case "path_remove":
		resType := &typeck.GenType{Con: "result", Params: []typeck.Type{typeck.TUnit, typeck.TStr}}
		structName, err := e.resultTypeName(resType)
		if err != nil {
			return true, err
		}
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf(
			"(__extension__ ({ int _rr = remove(%s); "+
				"_rr == 0 ? (%s){ ._ok=1 } : (%s){ ._ok=0, ._err_val=\"remove failed\" }; }))",
			argSB.String(), structName, structName))
	case "path_list_dir":
		// Returns result<vec<str>, str>
		vecStrT := &typeck.GenType{Con: "vec", Params: []typeck.Type{typeck.TStr}}
		resType := &typeck.GenType{Con: "result", Params: []typeck.Type{vecStrT, typeck.TStr}}
		structName, err := e.resultTypeName(resType)
		if err != nil {
			return true, err
		}
		vecT := e.vecTypeName("const char*")
		pushFn := e.vecPushName("const char*")
		var argSB strings.Builder
		if err := e.emitExpr(args[0], &argSB); err != nil {
			return true, err
		}
		sb.WriteString(fmt.Sprintf(
			"(__extension__ ({ %s _pres; "+
				"#ifndef _WIN32 "+
				"DIR* _dir = opendir(%s); "+
				"if (!_dir) { _pres = (%s){ ._ok=0, ._err_val=\"opendir failed\" }; } else { "+
				"%s _vd = {._data=NULL,._len=0,._cap=0}; struct dirent* _de; "+
				"while ((_de = readdir(_dir)) != NULL) { "+
				"size_t _nl = strlen(_de->d_name)+1; char* _nc = (char*)malloc(_nl); memcpy(_nc, _de->d_name, _nl); "+
				"%s(&_vd, (const char*)_nc); } closedir(_dir); "+
				"_pres = (%s){ ._ok=1, ._ok_val=_vd }; } "+
				"#else "+
				"_pres = (%s){ ._ok=0, ._err_val=\"path_list_dir not supported on Windows\" }; "+
				"#endif "+
				"_pres; }))",
			structName, argSB.String(), structName, vecT, pushFn, structName, structName))

	// M2.1 math (two-arg)  — handled here because 1-arg section only runs when len(args)==1
	default:
		return false, nil
	}
	return true, nil
}

// ── runtime helpers ───────────────────────────────────────────────────────────

// emitComptimeConst emits a C literal for a comptime-evaluated value.
func emitComptimeConst(v interface{}, sb *strings.Builder) {
	switch val := v.(type) {
	case int64:
		fmt.Fprintf(sb, "%d", val)
	case float64:
		fmt.Fprintf(sb, "%g", val)
	case bool:
		if val {
			sb.WriteString("1")
		} else {
			sb.WriteString("0")
		}
	case string:
		sb.WriteByte('"')
		for _, ch := range val {
			switch ch {
			case '"':
				sb.WriteString(`\"`)
			case '\\':
				sb.WriteString(`\\`)
			case '\n':
				sb.WriteString(`\n`)
			case '\t':
				sb.WriteString(`\t`)
			default:
				sb.WriteRune(ch)
			}
		}
		sb.WriteByte('"')
	default:
		// unit / nil — should not appear in expression position
		sb.WriteString("/* unit */")
	}
}

// emitRuntimeHelpers emits small C helper functions that back Candor builtins.
// They are emitted once at the top of the translation unit.
func (e *emitter) emitRuntimeHelpers() {
	// os_args globals — set at the start of main so os_args() works anywhere.
	e.writeln("static int _cnd_argc = 0;")
	e.writeln("static char** _cnd_argv = NULL;")
	e.writeln("")
	// read_line: read one line from stdin, strip trailing \r\n, return heap copy.
	e.writeln("static const char* _cnd_read_line(void) {")
	e.writeln("    static char _buf[4096];")
	e.writeln("    if (!fgets(_buf, sizeof(_buf), stdin)) { _buf[0] = '\\0'; }")
	e.writeln("    size_t _n = strlen(_buf);")
	e.writeln("    while (_n > 0 && (_buf[_n-1] == '\\n' || _buf[_n-1] == '\\r')) { _buf[--_n] = '\\0'; }")
	e.writeln("    char* _out = (char*)malloc(_n + 1);")
	e.writeln("    memcpy(_out, _buf, _n + 1);")
	e.writeln("    return _out;")
	e.writeln("}")

	// try_read_line: returns option<str> = const char** (NULL on EOF).
	e.writeln("static const char** _cnd_try_read_line(void) {")
	e.writeln("    static char _buf[4096];")
	e.writeln("    if (!fgets(_buf, sizeof(_buf), stdin)) { return NULL; }")
	e.writeln("    size_t _n = strlen(_buf);")
	e.writeln("    while (_n > 0 && (_buf[_n-1] == '\\n' || _buf[_n-1] == '\\r')) { _buf[--_n] = '\\0'; }")
	e.writeln("    char* _s = (char*)malloc(_n + 1);")
	e.writeln("    memcpy(_s, _buf, _n + 1);")
	e.writeln("    const char** _p = (const char**)malloc(sizeof(const char*));")
	e.writeln("    *_p = _s;")
	e.writeln("    return _p;")
	e.writeln("}")

	// try_read_int: returns option<i64> = int64_t* (NULL on EOF/parse failure).
	e.writeln("static int64_t* _cnd_try_read_int(void) {")
	e.writeln("    int64_t _v;")
	e.writeln("    if (scanf(\"%lld\", &_v) != 1) { return NULL; }")
	e.writeln("    int64_t* _p = (int64_t*)malloc(sizeof(int64_t));")
	e.writeln("    *_p = _v;")
	e.writeln("    return _p;")
	e.writeln("}")

	// try_read_f64: returns option<f64> = double* (NULL on EOF/parse failure).
	e.writeln("static double* _cnd_try_read_f64(void) {")
	e.writeln("    double _v;")
	e.writeln("    if (scanf(\"%lf\", &_v) != 1) { return NULL; }")
	e.writeln("    double* _p = (double*)malloc(sizeof(double));")
	e.writeln("    *_p = _v;")
	e.writeln("    return _p;")
	e.writeln("}")

	// str_concat: allocate a new string that is a + b.
	e.writeln("static const char* _cnd_str_concat(const char* a, const char* b) {")
	e.writeln("    size_t la = strlen(a), lb = strlen(b);")
	e.writeln("    char* _out = (char*)malloc(la + lb + 1);")
	e.writeln("    memcpy(_out, a, la);")
	e.writeln("    memcpy(_out + la, b, lb + 1);")
	e.writeln("    return _out;")
	e.writeln("}")

	// int_to_str: convert i64 to a decimal string.
	e.writeln("static const char* _cnd_int_to_str(int64_t n) {")
	e.writeln("    char _buf[32];")
	e.writeln("    snprintf(_buf, sizeof(_buf), \"%lld\", (long long)n);")
	e.writeln("    char* _out = (char*)malloc(strlen(_buf) + 1);")
	e.writeln("    strcpy(_out, _buf);")
	e.writeln("    return _out;")
	e.writeln("}")

	// read_file: read entire file into a heap string. Returns NULL on error (used with result<str,str>).
	e.writeln("static const char* _cnd_read_file(const char* path) {")
	e.writeln("    FILE* _f = fopen(path, \"rb\");")
	e.writeln("    if (!_f) { return NULL; }")
	e.writeln("    fseek(_f, 0, SEEK_END); long _sz = ftell(_f); fseek(_f, 0, SEEK_SET);")
	e.writeln("    char* _buf = (char*)malloc(_sz + 1);")
	e.writeln("    if (!_buf) { fclose(_f); return NULL; }")
	e.writeln("    fread(_buf, 1, _sz, _f); _buf[_sz] = '\\0';")
	e.writeln("    fclose(_f); return _buf;")
	e.writeln("}")

	// write_file: write string to file (truncate). Returns 0 on success, -1 on error.
	e.writeln("static int _cnd_write_file(const char* path, const char* data) {")
	e.writeln("    FILE* _f = fopen(path, \"wb\");")
	e.writeln("    if (!_f) { return -1; }")
	e.writeln("    size_t _n = strlen(data);")
	e.writeln("    int _ok = (fwrite(data, 1, _n, _f) == _n) ? 0 : -1;")
	e.writeln("    fclose(_f); return _ok;")
	e.writeln("}")

	// append_file: append string to file. Returns 0 on success, -1 on error.
	e.writeln("static int _cnd_append_file(const char* path, const char* data) {")
	e.writeln("    FILE* _f = fopen(path, \"ab\");")
	e.writeln("    if (!_f) { return -1; }")
	e.writeln("    size_t _n = strlen(data);")
	e.writeln("    int _ok = (fwrite(data, 1, _n, _f) == _n) ? 0 : -1;")
	e.writeln("    fclose(_f); return _ok;")
	e.writeln("}")

	// M2.2 str helpers
	e.writeln("static const char* _cnd_str_repeat(const char* s, int64_t n) {")
	e.writeln("    size_t _l = strlen(s);")
	e.writeln("    if (n <= 0 || _l == 0) { char* _e = (char*)malloc(1); _e[0] = '\\0'; return _e; }")
	e.writeln("    char* _out = (char*)malloc((size_t)n * _l + 1);")
	e.writeln("    for (int64_t _i = 0; _i < n; _i++) { memcpy(_out + (size_t)_i * _l, s, _l); }")
	e.writeln("    _out[(size_t)n * _l] = '\\0'; return _out;")
	e.writeln("}")

	e.writeln("static const char* _cnd_str_trim(const char* s) {")
	e.writeln("    while (*s && isspace((unsigned char)*s)) { s++; }")
	e.writeln("    size_t _l = strlen(s);")
	e.writeln("    while (_l > 0 && isspace((unsigned char)s[_l-1])) { _l--; }")
	e.writeln("    char* _out = (char*)malloc(_l + 1);")
	e.writeln("    memcpy(_out, s, _l); _out[_l] = '\\0'; return _out;")
	e.writeln("}")

	e.writeln("static const char* _cnd_str_replace(const char* s, const char* from, const char* to) {")
	e.writeln("    size_t _fl = strlen(from), _tl = strlen(to), _count = 0;")
	e.writeln("    if (_fl == 0) { char* _c = (char*)malloc(strlen(s)+1); strcpy(_c, s); return _c; }")
	e.writeln("    const char* _p = s;")
	e.writeln("    while ((_p = strstr(_p, from)) != NULL) { _count++; _p += _fl; }")
	e.writeln("    size_t _sl = strlen(s);")
	e.writeln("    char* _out = (char*)malloc(_sl + _count * (_tl - _fl) + 1 + (_tl > _fl ? _count * (_tl - _fl) : 0));")
	e.writeln("    char* _w = _out; _p = s;")
	e.writeln("    while (1) { const char* _f = strstr(_p, from); if (!_f) { strcpy(_w, _p); break; }")
	e.writeln("        size_t _pre = _f - _p; memcpy(_w, _p, _pre); _w += _pre;")
	e.writeln("        memcpy(_w, to, _tl); _w += _tl; _p = _f + _fl; } return _out;")
	e.writeln("}")

	e.writeln("static const char* _cnd_str_to_upper(const char* s) {")
	e.writeln("    size_t _l = strlen(s); char* _out = (char*)malloc(_l + 1);")
	e.writeln("    for (size_t _i = 0; _i <= _l; _i++) { _out[_i] = (char)toupper((unsigned char)s[_i]); }")
	e.writeln("    return _out;")
	e.writeln("}")

	e.writeln("static const char* _cnd_str_to_lower(const char* s) {")
	e.writeln("    size_t _l = strlen(s); char* _out = (char*)malloc(_l + 1);")
	e.writeln("    for (size_t _i = 0; _i <= _l; _i++) { _out[_i] = (char)tolower((unsigned char)s[_i]); }")
	e.writeln("    return _out;")
	e.writeln("}")

	// M2.3 io helpers
	e.writeln("static void _cnd_flush_stdout(void) { fflush(stdout); }")

	// M2.4 os helpers
	e.writeln("static const char* _cnd_os_cwd(void) {")
	e.writeln("#ifdef _WIN32")
	e.writeln("    char* _buf = _getcwd(NULL, 0);")
	e.writeln("#else")
	e.writeln("    char* _buf = getcwd(NULL, 0);")
	e.writeln("#endif")
	e.writeln("    if (!_buf) { char* _e = (char*)malloc(2); _e[0] = '.'; _e[1] = '\\0'; return _e; }")
	e.writeln("    return _buf;")
	e.writeln("}")

	// M9.12 os_exec — launch a subprocess and wait for it.
	// Accepts void* so it can be emitted before vec type structs are defined.
	// The void* must point to a _CndVec_const_charptr (same layout as _CndStrVec).
	e.writeln("typedef struct { const char** _data; uint64_t _len; uint64_t _cap; } _CndStrVec;")
	e.writeln("static int64_t _cnd_os_exec(void* vp, int* ok_out, const char** err_out) {")
	e.writeln("    _CndStrVec* v = (_CndStrVec*)vp;")
	e.writeln("    int argc = (int)v->_len;")
	e.writeln("    char** argv_c = (char**)malloc((size_t)(argc + 1) * sizeof(char*));")
	e.writeln("    if (!argv_c) { *ok_out = 0; *err_out = \"os_exec: malloc failed\"; return 0; }")
	e.writeln("    for (int i = 0; i < argc; i++) { argv_c[i] = (char*)v->_data[i]; }")
	e.writeln("    argv_c[argc] = NULL;")
	e.writeln("#ifdef _WIN32")
	e.writeln("    int _r = _spawnvp(_P_WAIT, argv_c[0], (const char* const*)argv_c);")
	e.writeln("    free(argv_c);")
	e.writeln("    if (_r < 0) { *ok_out = 0; *err_out = \"os_exec: spawn failed\"; return 0; }")
	e.writeln("    *ok_out = 1; return (int64_t)_r;")
	e.writeln("#else")
	e.writeln("    pid_t _pid = fork();")
	e.writeln("    if (_pid < 0) { free(argv_c); *ok_out = 0; *err_out = \"os_exec: fork failed\"; return 0; }")
	e.writeln("    if (_pid == 0) { execvp(argv_c[0], argv_c); _exit(127); }")
	e.writeln("    free(argv_c);")
	e.writeln("    int _wstatus = 0;")
	e.writeln("    waitpid(_pid, &_wstatus, 0);")
	e.writeln("    *ok_out = 1;")
	e.writeln("    return (int64_t)(WIFEXITED(_wstatus) ? WEXITSTATUS(_wstatus) : 128 + WTERMSIG(_wstatus));")
	e.writeln("#endif")
	e.writeln("}")

	// M2.5 time helpers
	e.writeln("static int64_t _cnd_time_now_ms(void) {")
	e.writeln("#ifdef _WIN32")
	e.writeln("    return (int64_t)(clock() * 1000 / CLOCKS_PER_SEC);")
	e.writeln("#else")
	e.writeln("    struct timespec _ts; clock_gettime(CLOCK_REALTIME, &_ts);")
	e.writeln("    return (int64_t)_ts.tv_sec * 1000 + (int64_t)_ts.tv_nsec / 1000000;")
	e.writeln("#endif")
	e.writeln("}")

	e.writeln("static int64_t _cnd_time_now_mono_ns(void) {")
	e.writeln("#ifdef _WIN32")
	e.writeln("    return (int64_t)clock() * (1000000000 / CLOCKS_PER_SEC);")
	e.writeln("#else")
	e.writeln("    struct timespec _ts; clock_gettime(CLOCK_MONOTONIC, &_ts);")
	e.writeln("    return (int64_t)_ts.tv_sec * 1000000000 + (int64_t)_ts.tv_nsec;")
	e.writeln("#endif")
	e.writeln("}")

	e.writeln("static void _cnd_time_sleep_ms(int64_t ms) {")
	e.writeln("#ifdef _WIN32")
	e.writeln("    Sleep((DWORD)ms);")
	e.writeln("#else")
	e.writeln("    struct timespec _ts = { (time_t)(ms / 1000), (long)((ms % 1000) * 1000000) };")
	e.writeln("    nanosleep(&_ts, NULL);")
	e.writeln("#endif")
	e.writeln("}")

	// M2.6 rand helpers
	e.writeln("static uint64_t _cnd_rand_u64(void) {")
	e.writeln("    return ((uint64_t)(unsigned)rand() << 32) | (uint64_t)(unsigned)rand();")
	e.writeln("}")
	e.writeln("static double _cnd_rand_f64(void) {")
	e.writeln("    return (double)rand() / ((double)RAND_MAX + 1.0);")
	e.writeln("}")
	e.writeln("static void _cnd_rand_set_seed(uint64_t seed) { srand((unsigned)seed); }")

	// M2.7 path helpers
	e.writeln("static const char* _cnd_path_join(const char* a, const char* b) {")
	e.writeln("    size_t _al = strlen(a), _bl = strlen(b);")
	e.writeln("    int _sep = (_al > 0 && a[_al-1] != '/' && a[_al-1] != '\\\\') ? 1 : 0;")
	e.writeln("    char* _out = (char*)malloc(_al + _sep + _bl + 1);")
	e.writeln("    memcpy(_out, a, _al);")
	e.writeln("    if (_sep) { _out[_al] = '/'; }")
	e.writeln("    memcpy(_out + _al + _sep, b, _bl + 1); return _out;")
	e.writeln("}")

	e.writeln("static const char* _cnd_path_dir(const char* p) {")
	e.writeln("    size_t _l = strlen(p), _i = _l;")
	e.writeln("    while (_i > 0 && p[_i-1] != '/' && p[_i-1] != '\\\\') { _i--; }")
	e.writeln("    if (_i == 0) { char* _d = (char*)malloc(2); _d[0] = '.'; _d[1] = '\\0'; return _d; }")
	e.writeln("    size_t _dl = _i > 1 ? _i - 1 : 1;")
	e.writeln("    char* _d = (char*)malloc(_dl + 1); memcpy(_d, p, _dl); _d[_dl] = '\\0'; return _d;")
	e.writeln("}")

	e.writeln("static const char* _cnd_path_filename(const char* p) {")
	e.writeln("    size_t _l = strlen(p), _i = _l;")
	e.writeln("    while (_i > 0 && p[_i-1] != '/' && p[_i-1] != '\\\\') { _i--; }")
	e.writeln("    char* _f = (char*)malloc(_l - _i + 1); memcpy(_f, p + _i, _l - _i + 1); return _f;")
	e.writeln("}")

	e.writeln("static const char* _cnd_path_ext(const char* p) {")
	e.writeln("    size_t _l = strlen(p), _i = _l;")
	e.writeln("    while (_i > 0 && p[_i-1] != '.' && p[_i-1] != '/' && p[_i-1] != '\\\\') { _i--; }")
	e.writeln("    if (_i == 0 || p[_i-1] != '.') { char* _e = (char*)malloc(1); _e[0] = '\\0'; return _e; }")
	e.writeln("    char* _e = (char*)malloc(_l - _i + 2); _e[0] = '.'; memcpy(_e+1, p+_i, _l-_i+1); return _e;")
	e.writeln("}")

	e.writeln("static int _cnd_path_exists(const char* p) {")
	e.writeln("#ifdef _WIN32")
	e.writeln("    return _access(p, 0) == 0;")
	e.writeln("#else")
	e.writeln("    return access(p, F_OK) == 0;")
	e.writeln("#endif")
	e.writeln("}")
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
		sb.WriteString("(__extension__ ({ ")
		sb.WriteString(ct)
		sb.WriteString("* _p = (")
		sb.WriteString(ct)
		sb.WriteString("*)malloc(sizeof(")
		sb.WriteString(ct)
		sb.WriteString(")); *_p = ")
		if err := e.emitExpr(ex.Args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString("; _p; }))")
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
		resType = e.concreteResultType(resType)
		structName, err := e.resultTypeName(resType)
		if err != nil {
			return true, err
		}
		// When OK type is unit, result<unit,E> has no _ok_val field.
		okArgType := e.res.ExprTypes[ex.Args[0]]
		okArgC, err := e.cType(okArgType)
		if err != nil {
			return true, err
		}
		sb.WriteByte('(')
		sb.WriteString(structName)
		if okArgC == "void" {
			sb.WriteString("){ ._ok = 1 }")
		} else {
			sb.WriteString("){ ._ok = 1, ._ok_val = ")
			if err := e.emitExpr(ex.Args[0], sb); err != nil {
				return true, err
			}
			sb.WriteByte('}')
		}
		return true, nil

	case lexer.TokErr:
		if len(ex.Args) != 1 {
			return false, nil
		}
		resType, ok := e.res.ExprTypes[ex].(*typeck.GenType)
		if !ok || resType.Con != "result" {
			return false, nil
		}
		resType = e.concreteResultType(resType)
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

	case lexer.TokMove:
		// move(x) — semantic ownership transfer; in C just evaluates to x
		if len(ex.Args) != 1 {
			return false, nil
		}
		if err := e.emitExpr(ex.Args[0], sb); err != nil {
			return true, err
		}
		return true, nil

	case lexer.TokSecret:
		// secret(x) — wraps in secret<T>; transparent at runtime, just emits x
		if len(ex.Args) != 1 {
			return false, nil
		}
		if err := e.emitExpr(ex.Args[0], sb); err != nil {
			return true, err
		}
		return true, nil

	case lexer.TokReveal:
		// reveal(s) — explicitly unwraps secret<T> to T; transparent at runtime
		if len(ex.Args) != 1 {
			return false, nil
		}
		if err := e.emitExpr(ex.Args[0], sb); err != nil {
			return true, err
		}
		return true, nil
	}
	// refmut(x) — mutable reference constructor; in C emits &x
	if fn.Tok.Lexeme == "refmut" && len(ex.Args) == 1 {
		sb.WriteString("(&(")
		if err := e.emitExpr(ex.Args[0], sb); err != nil {
			return true, err
		}
		sb.WriteString("))")
		return true, nil
	}
	return false, nil
}

// ── must/match emission ───────────────────────────────────────────────────────

func (e *emitter) emitMustOrMatch(x parser.Expr, arms []parser.MustArm, bodyType typeck.Type, sb *strings.Builder) error {
	tmp := e.freshTmp()
	res := e.freshTmp()

	xType := e.res.ExprTypes[x]

	// Audit: log must{} sites on result<T,E> or option<T>.
	if e.audit != nil {
		if xType != nil {
			if gen, ok := xType.(*typeck.GenType); ok {
				if gen.Con == "result" || gen.Con == "option" {
					typeName := gen.Con
					if len(gen.Params) > 0 {
						typeName = fmt.Sprintf("%s<%s>", gen.Con, gen.Params[0].String())
					}
					line := 0
					if pos, ok := x.(interface{ GetLine() int }); ok {
						line = pos.GetLine()
					}
					e.audit.add(AuditEntry{
						Category:    "must",
						FnName:      e.currentFnName,
						Line:        line,
						Detail:      fmt.Sprintf("must{} on %s", typeName),
						CEquiv:      "if/else on _ok or NULL check",
						Explanation: fmt.Sprintf("Candor enforces that discarding this %s is a compile error. In C, the caller can ignore the return value silently.", typeName),
					})
				}
			}
		}
	}

	var xb strings.Builder
	if err := e.emitExpr(x, &xb); err != nil {
		return err
	}

	var bodyC string
	if bodyType != nil && !bodyType.Equals(typeck.TUnit) && !bodyType.Equals(typeck.TNever) {
		if gen, ok := bodyType.(*typeck.GenType); ok && gen.Con == "result" {
			bodyType = e.concreteResultType(gen)
		}
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
		// BlockExpr arms emit their statements directly into sb.
		if blkArm, ok := arm.Body.(*parser.BlockExpr); ok {
			stmts := blkArm.Stmts
			// Each arm is a separate C scope (if/else-if body). Save declaredVars
			// so that variables declared in one arm don't appear as "already
			// declared" in sibling arms, which would cause C undeclared-variable
			// errors when the second arm emits `x = ...` instead of `T x = ...`.
			var savedArmVars map[string]bool
			if e.declaredVars != nil {
				savedArmVars = make(map[string]bool, len(e.declaredVars))
				for k, v := range e.declaredVars {
					savedArmVars[k] = v
				}
			}
			// When the match produces a non-unit value, the last statement in a
			// block arm must assign its expression value to `res`.  Detect whether
			// the tail statement is an ExprStmt so we can handle it specially.
			tailIdx := -1
			if bodyC != "" && len(stmts) > 0 {
				if _, isTail := stmts[len(stmts)-1].(*parser.ExprStmt); isTail {
					tailIdx = len(stmts) - 1
				}
			}
			for i, stmt := range stmts {
				if i == tailIdx {
					// Emit the tail expression as `res = expr;`
					tailExpr := stmt.(*parser.ExprStmt).X
					var tailSB strings.Builder
					if err := e.emitExpr(tailExpr, &tailSB); err != nil {
						return err
					}
					fmt.Fprintf(sb, "        %s = %s;\n", res, tailSB.String())
					continue
				}
				// emitStmt always writes to e.sb. We capture what it adds, then
				// forward it to sb (which may be &e.sb itself when this match is
				// emitted at statement level — so we must save both halves before
				// touching e.sb, otherwise the reset discards the new content).
				before := e.sb.Len()
				if err := e.emitStmt(stmt, 2); err != nil {
					return err
				}
				full := e.sb.String()
				stmtStr := full[before:] // what emitStmt wrote (already depth-2 indented)
				oldStr := full[:before]  // content that was there before emitStmt
				// Restore e.sb to its pre-emitStmt state.
				e.sb.Reset()
				e.sb.WriteString(oldStr)
				// Forward the stmt output to sb.  If sb == &e.sb this appends
				// correctly after the restored content; if sb is a different builder
				// it simply appends there.
				fmt.Fprint(sb, stmtStr)
			}
			// Restore declaredVars to what it was before this arm.
			if savedArmVars != nil {
				e.declaredVars = savedArmVars
			}
			_ = armType
		} else {
			// Skip unit-typed ident bodies (e.g. `ok(v) => v` when v:unit) — they have
			// no side effects and v may not be declared (void vars are invalid in C).
			isUnitIdent := func() bool {
				if _, isIdent := arm.Body.(*parser.IdentExpr); !isIdent {
					return false
				}
				return armType != nil && armType.Equals(typeck.TUnit)
			}
			if isUnitIdent() {
				// No-op: unit identity arm has no side effects and no C representation.
			} else {
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

// emitPropagateExpr emits the GCC statement expression for `expr?`.
// It evaluates the operand (a result<T,E>), early-returns an error result if
// !_ok, and otherwise yields the _ok_val to the enclosing expression.
func (e *emitter) emitPropagateExpr(ex *parser.PropagateExpr, sb *strings.Builder) error {
	operandType := e.res.ExprTypes[ex.X]
	gen := operandType.(*typeck.GenType) // typeck guarantees result<T,E>

	operandC, err := e.cType(operandType)
	if err != nil {
		return err
	}
	retC, err := e.cType(e.retType)
	if err != nil {
		return err
	}

	var xb strings.Builder
	if err := e.emitExpr(ex.X, &xb); err != nil {
		return err
	}

	tmp := e.freshTmp()
	okIsUnit := gen.Params[0].Equals(typeck.TUnit)

	fmt.Fprintf(sb, "(__extension__ ({\n")
	fmt.Fprintf(sb, "    %s %s = %s;\n", operandC, tmp, xb.String())
	fmt.Fprintf(sb, "    if (!%s._ok) { %s _cnd_early = {0}; _cnd_early._err_val = %s._err_val; return _cnd_early; }\n",
		tmp, retC, tmp)
	if !okIsUnit {
		fmt.Fprintf(sb, "    %s._ok_val;\n", tmp)
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
			return tmp, "", nil
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

	case *parser.IntLitExpr:
		return fmt.Sprintf("(%s) == %s", tmp, p.Tok.Lexeme), "", nil

	case *parser.FloatLitExpr:
		return fmt.Sprintf("(%s) == %s", tmp, p.Tok.Lexeme), "", nil

	case *parser.StringLitExpr:
		return fmt.Sprintf("strcmp(%s, %s) == 0", tmp, p.Tok.Lexeme), "", nil

	case *parser.UnaryExpr:
		if p.Op.Type == lexer.TokMinus {
			if inner, ok := p.Operand.(*parser.IntLitExpr); ok {
				return fmt.Sprintf("(%s) == -%s", tmp, inner.Tok.Lexeme), "", nil
			}
			if inner, ok := p.Operand.(*parser.FloatLitExpr); ok {
				return fmt.Sprintf("(%s) == -%s", tmp, inner.Tok.Lexeme), "", nil
			}
		}

	case *parser.PathExpr:
		// Unit enum variant pattern: Shape::Point
		// Use the resolved C name from xType (handles module-prefixed enums like typeck_Expr).
		enumCName, err := e.cType(xType)
		if err != nil {
			return "", "", err
		}
		cond = fmt.Sprintf("%s._tag == %s_tag_%s", tmp, enumCName, p.Tail.Lexeme)
		return cond, "", nil

	case *parser.CallExpr:
		// Enum variant pattern with bindings: Shape::Circle(r)
		if path, ok2 := p.Fn.(*parser.PathExpr); ok2 {
			enumCName, err := e.cType(xType)
			if err != nil {
				return "", "", err
			}
			cond = fmt.Sprintf("%s._tag == %s_tag_%s", tmp, enumCName, path.Tail.Lexeme)
			var bindings []string
			for i, arg := range p.Args {
				if v, ok3 := arg.(*parser.IdentExpr); ok3 && v.Tok.Lexeme != "_" {
					vType := e.res.ExprTypes[v]
					if vType != nil {
						ct, err := e.cType(vType)
						if err != nil {
							return "", "", err
						}
						bindings = append(bindings,
							fmt.Sprintf("%s %s = %s._data._%s._%d;",
								ct, v.Tok.Lexeme, tmp, safeVariantField(path.Tail.Lexeme), i))
					}
				}
			}
			return cond, strings.Join(bindings, " "), nil
		}

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
						// result<unit,E> has no _ok_val field — skip binding for void
						if ct != "void" {
							binding = fmt.Sprintf("%s %s = %s._ok_val;", ct, v.Tok.Lexeme, tmp)
						}
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
	case typeck.TF16:
		return "_Float16", nil // IEEE 754 half-precision; requires GCC/Clang >= 12
	case typeck.TBF16:
		return "__bf16", nil // Brain float 16; requires GCC >= 13 or Clang >= 15
	case typeck.TF32:
		return "float", nil
	case typeck.TF64:
		return "double", nil
	case typeck.TNever:
		return "void", nil
	}

	switch tt := t.(type) {
	case *typeck.GenType:
		// Resolve _ok/_err sentinels using the current function's return type.
		if tt.Con == "_ok" || tt.Con == "_err" {
			if ret, ok := e.retType.(*typeck.GenType); ok && ret.Con == "result" && len(ret.Params) == 2 {
				if tt.Con == "_ok" {
					return e.cType(ret.Params[0])
				}
				return e.cType(ret.Params[1])
			}
			return "", fmt.Errorf("_ok/_err placeholder unresolvable (retType=%v)", e.retType)
		}
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
		case "map":
			if len(tt.Params) == 2 {
				kC, err := e.cType(tt.Params[0])
				if err != nil {
					return "", err
				}
				vC, err := e.cType(tt.Params[1])
				if err != nil {
					return "", err
				}
				return e.mapTypeName(kC, vC), nil
			}
		case "set":
			if len(tt.Params) == 1 {
				eC, err := e.cType(tt.Params[0])
				if err != nil {
					return "", err
				}
				return e.setTypeName(eC), nil
			}
		case "ring":
			if len(tt.Params) == 1 {
				inner, err := e.cType(tt.Params[0])
				if err != nil {
					return "", err
				}
				return e.ringTypeName(inner), nil
			}
		case "option":
			if len(tt.Params) == 1 {
				inner, err := e.cType(tt.Params[0])
				if err != nil {
					return "", err
				}
				return inner + "*", nil // null == none
			}
		case "box":
			if len(tt.Params) == 1 {
				inner, err := e.cType(tt.Params[0])
				if err != nil {
					return "", err
				}
				return inner + "*", nil // owned heap pointer
			}
		case "arc":
			if len(tt.Params) == 1 {
				inner, err := e.cType(tt.Params[0])
				if err != nil {
					return "", err
				}
				return inner + "*", nil // arc<T>: T* with refcount at ptr-8
			}
		case "result":
			if len(tt.Params) == 2 {
				return e.resultTypeName(tt)
			}
		case "secret":
			// secret<T> is transparent at runtime — same representation as T
			if len(tt.Params) == 1 {
				return e.cType(tt.Params[0])
			}
		case "cap":
			// cap<Name> is a zero-size capability token; emitted as cap_Name (uint8_t typedef).
			if len(tt.Params) == 1 {
				if ct, ok := tt.Params[0].(*typeck.CapabilityType); ok {
					return "cap_" + ct.Name, nil
				}
			}
		case "tensor":
			// tensor<T> is a value-type multi-dimensional array.
			if len(tt.Params) == 1 {
				inner, err := e.cType(tt.Params[0])
				if err != nil {
					return "", err
				}
				return e.tensorTypeName(inner), nil
			}
		case "mmap":
			// mmap<T> is a file-backed memory-mapped region; emitted as a value struct.
			if len(tt.Params) == 1 {
				inner, err := e.cType(tt.Params[0])
				if err != nil {
					return "", err
				}
				return e.mmapTypeName(inner), nil
			}
		case "task":
			// task<T> is a heap-allocated thread handle; emitted as _CndTask_T*.
			if len(tt.Params) == 1 {
				name, err := e.taskTypeName(tt.Params[0])
				if err != nil {
					return "", err
				}
				return name + "*", nil
			}
		}
		return "", fmt.Errorf("unsupported generic type: %s", t)

	case *typeck.CapabilityType:
		return "cap_" + tt.Name, nil

	case *typeck.StructType:
		return tt.Name, nil

	case *typeck.EnumType:
		return tt.Name, nil

	case *typeck.FnType:
		return e.fnTypeName(tt)

	case *typeck.TupleType:
		return e.tupleTypeName(tt)
	}

	return "", fmt.Errorf("cannot map type %s to C", t)
}

// isUnitValue returns true if the expression is the identifier "unit".
func isUnitValue(e parser.Expr) bool {
	ident, ok := e.(*parser.IdentExpr)
	return ok && ident.Tok.Lexeme == "unit"
}

// ── New feature emitters ──────────────────────────────────────────────────────

// tupleTypeName returns the C typedef name for a tuple type, emitting the
// typedef on first use. E.g. (i64, bool) → _CndTuple_int64_t_int.
func (e *emitter) tupleTypeName(tt *typeck.TupleType) (string, error) {
	var parts []string
	for _, elem := range tt.Elems {
		ct, err := e.cType(elem)
		if err != nil {
			return "", err
		}
		parts = append(parts, e.mangle(ct))
	}
	name := "_CndTuple_" + strings.Join(parts, "_")
	if e.emittedTypes == nil {
		e.emittedTypes = make(map[string]bool)
	}
	if !e.emittedTypes[name] {
		e.emittedTypes[name] = true
		e.writef("typedef struct { ")
		for i, elem := range tt.Elems {
			ct, err := e.cType(elem)
			if err != nil {
				return "", err
			}
			e.writef("%s _%d; ", ct, i)
		}
		e.writef("} %s;\n", name)
	}
	return name, nil
}

// emitConstDecl emits a module-level constant as a C static const.
func (e *emitter) emitConstDecl(d *parser.ConstDecl) error {
	typ := e.res.Consts[d.Name.Lexeme]
	ct, err := e.cType(typ)
	if err != nil {
		return err
	}
	e.writef("static const %s %s = ", ct, d.Name.Lexeme)
	if err := e.emitExpr(d.Value, &e.sb); err != nil {
		return err
	}
	e.writeln(";")
	return nil
}

// emitMethodDecl emits the body of an impl method function.
func (e *emitter) emitMethodDecl(mangledName string, d *parser.FnDecl) error {
	sig := e.res.FnSigs[mangledName]
	if sig == nil {
		return fmt.Errorf("internal: method sig %q not found for emit", mangledName)
	}
	// Build named prototype (parameter names needed in definition).
	ret, err := e.cType(sig.Ret)
	if err != nil {
		return err
	}
	var proto string
	if len(d.Params) == 0 {
		proto = fmt.Sprintf("%s %s(void)", ret, mangledName)
	} else {
		params := make([]string, len(d.Params))
		for i, p := range d.Params {
			ct, err := e.cType(sig.Params[i])
			if err != nil {
				return err
			}
			params[i] = ct + " " + p.Name.Lexeme
		}
		proto = fmt.Sprintf("%s %s(%s)", ret, mangledName, strings.Join(params, ", "))
	}
	e.writef("\n%s {\n", proto)
	prevRetIsUnit := e.retIsUnit
	prevIsMain := e.isMain
	prevContracts := e.contracts
	prevRetType := e.retType
	e.retIsUnit = sig.Ret.Equals(typeck.TUnit)
	e.isMain = false
	e.contracts = d.Contracts
	e.retType = sig.Ret
	if err := e.emitFnBody(d.Body, 1); err != nil {
		e.retIsUnit = prevRetIsUnit
		e.isMain = prevIsMain
		e.contracts = prevContracts
		e.retType = prevRetType
		return err
	}
	e.retIsUnit = prevRetIsUnit
	e.isMain = prevIsMain
	e.contracts = prevContracts
	e.retType = prevRetType
	e.writeln("}")
	return nil
}

// emitVecLitExpr emits a vec literal [e0, e1, ...] as a GNU statement expr
// that creates a vec and pushes each element.
func (e *emitter) emitVecLitExpr(ex *parser.VecLitExpr, sb *strings.Builder) error {
	vecType, ok := e.res.ExprTypes[ex].(*typeck.GenType)
	if !ok || vecType.Con != "vec" || len(vecType.Params) == 0 {
		return fmt.Errorf("internal: VecLitExpr has non-vec type")
	}
	elemC, err := e.cType(vecType.Params[0])
	if err != nil {
		return err
	}
	vecC := e.vecTypeName(elemC)
	tmp := e.freshTmp()
	// Emit as GNU statement expression: ({ _CndVec_T _cndN = {NULL,0,0}; push each; _cndN; })
	fmt.Fprintf(sb, "(__extension__ ({ %s %s = {NULL, 0, 0}; ", vecC, tmp)
	for _, elem := range ex.Elems {
		fmt.Fprintf(sb, "%s(&%s, ", e.vecPushName(elemC), tmp)
		if err := e.emitExpr(elem, sb); err != nil {
			return err
		}
		sb.WriteString("); ")
	}
	fmt.Fprintf(sb, "%s; }))", tmp)
	return nil
}

// emitTupleLitExpr emits a tuple literal (e0, e1) as a C compound literal.
func (e *emitter) emitTupleLitExpr(ex *parser.TupleLitExpr, sb *strings.Builder) error {
	tt, ok := e.res.ExprTypes[ex].(*typeck.TupleType)
	if !ok {
		return fmt.Errorf("internal: TupleLitExpr has non-tuple type")
	}
	typeName, err := e.tupleTypeName(tt)
	if err != nil {
		return err
	}
	fmt.Fprintf(sb, "(%s){", typeName)
	for i, elem := range ex.Elems {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(sb, "._%-d = ", i)
		if err := e.emitExpr(elem, sb); err != nil {
			return err
		}
	}
	sb.WriteString("}")
	return nil
}
