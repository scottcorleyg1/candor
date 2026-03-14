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

	emittedTypes  map[string]bool // tracks emitted structs/enums
	emittingTypes map[string]bool // detects cycles
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
	e.writeln("#include <string.h>")
	e.writeln("#include <assert.h>")
	e.writeln("")
	e.emitRuntimeHelpers()
	e.writeln("")

	// Forward-declare all structs and enums first so they can reference each other via pointers.
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *parser.StructDecl:
			e.writef("typedef struct %s %s;\n", d.Name.Lexeme, d.Name.Lexeme)
		case *parser.EnumDecl:
			e.writef("typedef struct %s %s;\n", d.Name.Lexeme, d.Name.Lexeme)
		}
	}


	// Emit vec<T> struct typedefs after user structs are forward declared.
	if err := e.emitVecStructTypedefs(); err != nil {
		return err
	}
	if err := e.emitMapStructTypedefs(); err != nil {
		return err
	}
	if err := e.emitResultStructTypedefs(); err != nil {
		return err
	}


	// Emit enum definitions (tagged unions) and struct definitions.
	// Since structs can contain structs (and result/map entries) by value,
	// we must emit them in topological order.
	e.emittedTypes = make(map[string]bool)
	e.emittingTypes = make(map[string]bool)
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *parser.StructDecl:
			if err := e.ensureStructEmitted(e.res.Structs[d.Name.Lexeme]); err != nil {
				return err
			}
		case *parser.EnumDecl:
			if err := e.ensureEnumEmitted(e.res.Enums[d.Name.Lexeme]); err != nil {
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

	// Emit vec<T> push helpers and map<K,V> operation helpers now that all
	// user structs and enums are fully defined.
	if err := e.emitVecStructHelpers(); err != nil {
		return err
	}
	if err := e.emitMapStructHelpers(); err != nil {
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

func (e *emitter) mangle(s string) string {
	r := strings.NewReplacer(" ", "_", "*", "ptr", "<", "_", ">", "_", ",", "_", "(", "_", ")", "_", "-", "_")
	return r.Replace(s)
}

func (e *emitter) vecTypeName(elemC string) string {
	return "_CndVec_" + e.mangle(elemC)
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
		if len(ft.Params) == 0 {
			e.writef("typedef %s (*%s)(void);\n", ret, name)
			return nil
		}
		params := make([]string, len(ft.Params))
		for i, p := range ft.Params {
			ct, err := e.cType(p)
			if err != nil {
				return err
			}
			params[i] = ct
		}
		e.writef("typedef %s (*%s)(%s);\n", ret, name, strings.Join(params, ", "))
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
		e.writef("    return (%s){ _b, 0, _cap };\n", mapName)
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
		return fmt.Errorf("cyclic struct dependency involving %s without using ref<T>", st.Name)
	}
	e.emittingTypes[st.Name] = true

	// Ensure all fields are emitted first.
	for _, fType := range st.Fields {
		// Pointers (ref<T>, vec<T>, map<K,V>, option<T>) don't require the inner type
		// to be fully defined for the struct body. Only inline types do.
		if gen, ok := fType.(*typeck.GenType); ok {
			if gen.Con == "ref" || gen.Con == "refmut" || gen.Con == "option" {
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
		return fmt.Errorf("cyclic enum dependency involving %s without using ref<T>", et.Name)
	}
	e.emittingTypes[et.Name] = true

	for _, v := range et.Variants {
		for _, fType := range v.Fields {
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
			e.writef(" } _%s;\n", v.Name)
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
				e.writef("    _r._data._%s._%d = _%d;\n", v.Name, i, i)
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

	case *parser.IndexAssignStmt:
		collType := e.res.ExprTypes[s.Target.Collection]
		e.write(ind)
		if gen, ok := collType.(*typeck.GenType); ok && (gen.Con == "vec" || gen.Con == "ring") {
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

	// vec/ring iteration
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
		ee := &emitter{res: e.res, retIsUnit: e.retIsUnit, isMain: e.isMain}
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
		// This allows a block to appear in expression position in C.
		// Requires GCC or Clang (both already required by Candor's C backend).
		// emitExpr has no depth parameter; match arms always appear at body
		// depth 1 inside a function, so we write via the main sink at depth 2.
		sb.WriteString("({\n")
		for _, stmt := range ex.Stmts {
			if err := e.emitStmt(stmt, 2); err != nil {
				return err
			}
		}
		// Trailing 0 gives the GNU statement expression a value of type int.
		sb.WriteString(indent(2) + "0;\n")
		sb.WriteString(indent(1) + "})")

	case *parser.CallExpr:
		// Comptime-evaluated pure function call — emit constant directly.
		if v, ok := e.res.ComptimeValues[ex]; ok {
			emitComptimeConst(v, sb)
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
		collType := e.res.ExprTypes[ex.Collection]
		if gen, ok := collType.(*typeck.GenType); ok && (gen.Con == "vec" || gen.Con == "ring") {
			// vec/ring are structs; elements are in the ._data array.
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

	case *parser.PathExpr:
		// Unit enum variant: Shape::Point → Shape_Point()
		sb.WriteString(ex.Head.Lexeme)
		sb.WriteByte('_')
		sb.WriteString(ex.Tail.Lexeme)
		sb.WriteString("()")

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

	// Zero-argument stdin builtins.
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
		}
	}

	// Two-argument string builtins.
	if len(args) == 2 {
		switch name {
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
		cond = fmt.Sprintf("%s._tag == %s_tag_%s", tmp, p.Head.Lexeme, p.Tail.Lexeme)
		return cond, "", nil

	case *parser.CallExpr:
		// Enum variant pattern with bindings: Shape::Circle(r)
		if path, ok2 := p.Fn.(*parser.PathExpr); ok2 {
			cond = fmt.Sprintf("%s._tag == %s_tag_%s", tmp, path.Head.Lexeme, path.Tail.Lexeme)
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
								ct, v.Tok.Lexeme, tmp, path.Tail.Lexeme, i))
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
		case "secret":
			// secret<T> is transparent at runtime — same representation as T
			if len(tt.Params) == 1 {
				return e.cType(tt.Params[0])
			}
		}
		return "", fmt.Errorf("unsupported generic type: %s", t)

	case *typeck.StructType:
		return tt.Name, nil

	case *typeck.EnumType:
		return tt.Name, nil

	case *typeck.FnType:
		return e.fnTypeName(tt)
	}

	return "", fmt.Errorf("cannot map type %s to C", t)
}

// isUnitValue returns true if the expression is the identifier "unit".
func isUnitValue(e parser.Expr) bool {
	ident, ok := e.(*parser.IdentExpr)
	return ok && ident.Tok.Lexeme == "unit"
}
