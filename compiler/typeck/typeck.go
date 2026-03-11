// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg1/candor

// Package typeck performs Core type checking on a parsed Candor AST.
//
// Strategy:
//  1. Collect all function signatures and struct definitions (pass 1).
//  2. Type-check each function body against its declared signature (pass 2).
//
// The output is a Result containing a map from every Expr node to its
// resolved Type. Downstream phases (emit_c) query this map instead of
// re-deriving types.
//
// Integer and float literals carry sentinel types (TIntLit, TFloatLit) until
// they are coerced to a concrete type by context (parameter type, return type,
// explicit annotation). Two uncoerced integer literals default to i64.
package typeck

import (
	"fmt"
	"strings"

	"github.com/scottcorleyg1/candor/compiler/lexer"
	"github.com/scottcorleyg1/candor/compiler/parser"
)

// Error is a type-check diagnostic with source position.
type Error struct {
	Tok lexer.Token
	Msg string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.Tok.File, e.Tok.Line, e.Tok.Col, e.Msg)
}

// Result is the output of a successful type-check pass.
type Result struct {
	// ExprTypes maps each Expr node to its resolved Type.
	ExprTypes map[parser.Expr]Type
	// FnSigs maps function names to their signature.
	FnSigs map[string]*FnType
	// Structs maps struct names to their StructType.
	Structs map[string]*StructType
	// Enums maps enum names to their EnumType.
	Enums map[string]*EnumType
	// FnEffects maps function names to their effects annotation (may be nil).
	FnEffects map[string]*parser.EffectsAnnotation
	// ComptimeValues maps CallExpr nodes to their compile-time evaluated values.
	// Only calls to pure (effects []) functions with all-constant args are included.
	// Values are int64, float64, bool, string, or nil (unit).
	ComptimeValues map[parser.Expr]interface{}
}

// Check type-checks a fully parsed File and returns a Result.
// Module declarations and use statements in the file are parsed but not
// enforced. Use CheckProgram for multi-file programs with enforcement.
func Check(file *parser.File) (*Result, error) {
	c := &checker{
		exprTypes:    make(map[parser.Expr]Type),
		fnSigs:       make(map[string]*FnType),
		structs:      make(map[string]*StructType),
		enums:        make(map[string]*EnumType),
		fnEffects:    make(map[string]*parser.EffectsAnnotation),
		fnDecls:      make(map[string]*parser.FnDecl),
		comptimeVals: make(map[parser.Expr]interface{}),
		// symModule intentionally nil — disables module enforcement
	}
	if err := c.checkFile(file); err != nil {
		return nil, err
	}
	runComptimePass(c, []*parser.File{file})
	return &Result{
		ExprTypes:      c.exprTypes,
		FnSigs:         c.fnSigs,
		Structs:        c.structs,
		Enums:          c.enums,
		FnEffects:      c.fnEffects,
		ComptimeValues: c.comptimeVals,
	}, nil
}

// CheckProgram type-checks a multi-file program with module enforcement.
// Each file may declare a module with `module name`. Top-level names from
// other modules are only accessible in files that import them with `use module::Name`.
// Files with no module declaration share the root namespace (always accessible).
func CheckProgram(files []*parser.File) (*Result, error) {
	c := &checker{
		exprTypes:    make(map[parser.Expr]Type),
		fnSigs:       make(map[string]*FnType),
		structs:      make(map[string]*StructType),
		enums:        make(map[string]*EnumType),
		fnEffects:    make(map[string]*parser.EffectsAnnotation),
		fnDecls:      make(map[string]*parser.FnDecl),
		comptimeVals: make(map[parser.Expr]interface{}),
		symModule:    make(map[string]string), // non-nil enables enforcement
	}
	if err := c.checkProgram(files); err != nil {
		return nil, err
	}
	runComptimePass(c, files)
	return &Result{
		ExprTypes:      c.exprTypes,
		FnSigs:         c.fnSigs,
		Structs:        c.structs,
		Enums:          c.enums,
		FnEffects:      c.fnEffects,
		ComptimeValues: c.comptimeVals,
	}, nil
}

func (c *checker) checkProgram(files []*parser.File) error {
	// Inject built-in signatures (root namespace, always visible).
	for name, sig := range Builtins {
		c.fnSigs[name] = sig
		c.symModule[name] = "" // builtins are root-level
	}
	for name, ann := range BuiltinEffects {
		c.fnEffects[name] = ann
	}

	// Pre-pass: determine each file's declared module name.
	fileModule := make(map[*parser.File]string)
	for _, f := range files {
		for _, d := range f.Decls {
			if md, ok := d.(*parser.ModuleDecl); ok {
				fileModule[f] = md.Name.Lexeme
				break
			}
		}
	}

	// Pass 1: collect all fn/struct signatures and record which module each belongs to.
	for _, f := range files {
		mod := fileModule[f]
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *parser.FnDecl:
				sig, err := c.buildFnSig(decl)
				if err != nil {
					return err
				}
				c.fnSigs[decl.Name.Lexeme] = sig
				c.symModule[decl.Name.Lexeme] = mod
				c.fnDecls[decl.Name.Lexeme] = decl
				if decl.Effects != nil {
					c.fnEffects[decl.Name.Lexeme] = decl.Effects
				}
			case *parser.StructDecl:
				st, err := c.buildStructType(decl)
				if err != nil {
					return err
				}
				c.structs[decl.Name.Lexeme] = st
				c.symModule[decl.Name.Lexeme] = mod
			case *parser.EnumDecl:
				et, err := c.buildEnumType(decl)
				if err != nil {
					return err
				}
				c.enums[decl.Name.Lexeme] = et
				c.symModule[decl.Name.Lexeme] = mod
			}
		}
	}

	// Use-validation pass: verify each UseDecl references a real module and name.
	// Build per-file import tables.
	fileUses := make(map[*parser.File]map[string]string)
	for _, f := range files {
		uses := make(map[string]string)
		for _, d := range f.Decls {
			ud, ok := d.(*parser.UseDecl)
			if !ok {
				continue
			}
			if len(ud.Path) < 2 {
				return &Error{Tok: ud.UseTok, Msg: "use path must have the form 'module::Name'"}
			}
			// Module is the first segment; imported name is the last segment.
			modName := ud.Path[0].Lexeme
			symName := ud.Path[len(ud.Path)-1].Lexeme
			// Validate: symName must exist and belong to modName.
			declMod, exists := c.symModule[symName]
			if !exists {
				return &Error{Tok: ud.UseTok,
					Msg: fmt.Sprintf("no symbol %q found in any module", symName)}
			}
			if declMod != modName {
				return &Error{Tok: ud.UseTok,
					Msg: fmt.Sprintf("symbol %q is from module %q, not %q", symName, declMod, modName)}
			}
			// Detect conflicting imports of the same name from different modules.
			if prev, dup := uses[symName]; dup && prev != modName {
				return &Error{Tok: ud.UseTok,
					Msg: fmt.Sprintf("conflicting imports: %q already imported from %q", symName, prev)}
			}
			uses[symName] = modName
		}
		fileUses[f] = uses
	}

	// Pass 2: type-check function bodies with per-file module context.
	for _, f := range files {
		c.currentModule = fileModule[f]
		c.currentUses = fileUses[f]
		c.errs = nil
		for _, d := range f.Decls {
			if fn, ok := d.(*parser.FnDecl); ok {
				if err := c.checkFnDecl(fn); err != nil {
					c.errs = append(c.errs, err)
				}
			}
		}
		if len(c.errs) > 0 {
			return multiError(c.errs)
		}
	}
	return nil
}

// checkModuleAccess returns an error if name belongs to a different module
// that has not been imported via `use` in the current file.
// It is a no-op when symModule is nil (single-file / Check mode).
func (c *checker) checkModuleAccess(name string, tok lexer.Token) error {
	if c.symModule == nil {
		return nil
	}
	symMod, known := c.symModule[name]
	if !known {
		return nil // not a top-level symbol at all; let the caller produce the "undefined" error
	}
	if symMod == "" || symMod == c.currentModule {
		return nil // root namespace or same module — always accessible
	}
	// Cross-module: require an explicit use import.
	if importedFrom, ok := c.currentUses[name]; ok && importedFrom == symMod {
		return nil
	}
	return &Error{Tok: tok,
		Msg: fmt.Sprintf("%q is from module %q; add 'use %s::%s'", name, symMod, symMod, name)}
}

// ── Internal checker state ────────────────────────────────────────────────────

type checker struct {
	exprTypes    map[parser.Expr]Type
	fnSigs       map[string]*FnType
	structs      map[string]*StructType
	enums        map[string]*EnumType
	fnEffects    map[string]*parser.EffectsAnnotation // collected effects annotations
	fnDecls      map[string]*parser.FnDecl             // function bodies for comptime eval
	comptimeVals map[parser.Expr]interface{}           // comptime-evaluated call results
	curEffects   *parser.EffectsAnnotation             // effects of fn currently being checked
	errs         []error                               // collected statement-level errors

	// Module enforcement — nil in single-file (Check) mode, populated by CheckProgram.
	symModule     map[string]string // top-level name → declaring module ("" = root/no module)
	currentModule string            // module of the file currently being checked
	currentUses   map[string]string // imported name → source module (for current file)
}

// scope is a linked chain of variable bindings.
type varInfo struct {
	typ     Type
	mutable bool
}

type scope struct {
	vars   map[string]varInfo
	parent *scope
}

func newScope(parent *scope) *scope {
	return &scope{vars: make(map[string]varInfo), parent: parent}
}

func (s *scope) lookup(name string) (Type, bool) {
	if info, ok := s.vars[name]; ok {
		return info.typ, true
	}
	if s.parent != nil {
		return s.parent.lookup(name)
	}
	return nil, false
}

func (s *scope) lookupInfo(name string) (varInfo, bool) {
	if info, ok := s.vars[name]; ok {
		return info, true
	}
	if s.parent != nil {
		return s.parent.lookupInfo(name)
	}
	return varInfo{}, false
}

func (s *scope) define(name string, t Type) {
	s.vars[name] = varInfo{typ: t, mutable: false}
}

func (s *scope) defineMut(name string, t Type) {
	s.vars[name] = varInfo{typ: t, mutable: true}
}

func (c *checker) record(expr parser.Expr, t Type) Type {
	c.exprTypes[expr] = t
	return t
}

func (c *checker) errorf(tok lexer.Token, format string, args ...any) error {
	return &Error{Tok: tok, Msg: fmt.Sprintf(format, args...)}
}

// ── Pass 1: collect signatures ────────────────────────────────────────────────

// Builtins are built-in function signatures injected before user code is
// checked. They have no Candor source; the emitter special-cases their calls.
var Builtins = map[string]*FnType{
	"print":      {Params: []Type{TStr}, Ret: TUnit},
	"print_int":  {Params: []Type{TI64}, Ret: TUnit},
	"print_bool": {Params: []Type{TBool}, Ret: TUnit},
	"print_u32":  {Params: []Type{TU32}, Ret: TUnit},
	"print_f64":  {Params: []Type{TF64}, Ret: TUnit},
	// stdin I/O — blocking reads
	"read_line": {Params: []Type{}, Ret: TStr},
	"read_int":  {Params: []Type{}, Ret: TI64},
	"read_f64":  {Params: []Type{}, Ret: TF64},
	// stdin I/O — EOF-safe reads returning option<T>
	"try_read_line": {Params: []Type{}, Ret: &GenType{Con: "option", Params: []Type{TStr}}},
	"try_read_int":  {Params: []Type{}, Ret: &GenType{Con: "option", Params: []Type{TI64}}},
	"try_read_f64":  {Params: []Type{}, Ret: &GenType{Con: "option", Params: []Type{TF64}}},
	// String operations
	"str_len":    {Params: []Type{TStr}, Ret: TI64},
	"str_concat": {Params: []Type{TStr, TStr}, Ret: TStr},
	"str_eq":     {Params: []Type{TStr, TStr}, Ret: TBool},
	"int_to_str": {Params: []Type{TI64}, Ret: TStr},
	"str_to_int": {Params: []Type{TStr}, Ret: &GenType{Con: "result", Params: []Type{TI64, TStr}}},
	// File I/O — result<str, str> on error
	"read_file":   {Params: []Type{TStr}, Ret: &GenType{Con: "result", Params: []Type{TStr, TStr}}},
	"write_file":  {Params: []Type{TStr, TStr}, Ret: &GenType{Con: "result", Params: []Type{TUnit, TStr}}},
	"append_file": {Params: []Type{TStr, TStr}, Ret: &GenType{Con: "result", Params: []Type{TUnit, TStr}}},
}

// BuiltinEffects records the known effects of built-in functions.
// The print_* family performs I/O, so they carry effects(io).
var BuiltinEffects = map[string]*parser.EffectsAnnotation{
	"print":      {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"print_int":  {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"print_bool": {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"print_u32":  {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"print_f64":  {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"read_line":      {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"read_int":       {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"read_f64":       {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"try_read_line":  {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"try_read_int":   {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"try_read_f64":   {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"read_file":   {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"write_file":  {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"append_file": {Kind: parser.EffectsDecl, Names: []string{"io"}},
}

func (c *checker) checkFile(file *parser.File) error {
	// Inject built-in signatures and their known effects.
	for name, sig := range Builtins {
		c.fnSigs[name] = sig
	}
	for name, ann := range BuiltinEffects {
		c.fnEffects[name] = ann
	}
	// Pass 1: collect struct types, function signatures, and effects annotations.
	// ModuleDecl and UseDecl are recorded for future scope enforcement; currently skipped.
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *parser.ModuleDecl, *parser.UseDecl:
			_ = d // syntax accepted; enforcement is a future feature
		case *parser.FnDecl:
			sig, err := c.buildFnSig(d)
			if err != nil {
				return err
			}
			c.fnSigs[d.Name.Lexeme] = sig
			c.fnDecls[d.Name.Lexeme] = d
			if d.Effects != nil {
				c.fnEffects[d.Name.Lexeme] = d.Effects
			}
		case *parser.StructDecl:
			st, err := c.buildStructType(d)
			if err != nil {
				return err
			}
			c.structs[d.Name.Lexeme] = st
		case *parser.EnumDecl:
			et, err := c.buildEnumType(d)
			if err != nil {
				return err
			}
			c.enums[d.Name.Lexeme] = et
		}
	}
	// Pass 2: type-check function bodies. Non-code decls are skipped.
	for _, decl := range file.Decls {
		if d, ok := decl.(*parser.FnDecl); ok {
			if err := c.checkFnDecl(d); err != nil {
				return err
			}
		}
	}
	if len(c.errs) > 0 {
		return multiError(c.errs)
	}
	return nil
}

func (c *checker) buildFnSig(d *parser.FnDecl) (*FnType, error) {
	params := make([]Type, len(d.Params))
	for i, p := range d.Params {
		t, err := c.resolveTypeExpr(p.Type)
		if err != nil {
			return nil, err
		}
		params[i] = t
	}
	ret, err := c.resolveTypeExpr(d.RetType)
	if err != nil {
		return nil, err
	}
	return &FnType{Params: params, Ret: ret}, nil
}

func (c *checker) buildStructType(d *parser.StructDecl) (*StructType, error) {
	st := &StructType{Name: d.Name.Lexeme, Fields: make(map[string]Type)}
	for _, f := range d.Fields {
		t, err := c.resolveTypeExpr(f.Type)
		if err != nil {
			return nil, err
		}
		st.Fields[f.Name.Lexeme] = t
	}
	return st, nil
}

func (c *checker) buildEnumType(d *parser.EnumDecl) (*EnumType, error) {
	et := &EnumType{
		Name:   d.Name.Lexeme,
		ByName: make(map[string]*EnumVariantDef),
	}
	for i, v := range d.Variants {
		fields := make([]Type, len(v.Fields))
		for j, f := range v.Fields {
			t, err := c.resolveTypeExpr(f)
			if err != nil {
				return nil, err
			}
			fields[j] = t
		}
		vd := &EnumVariantDef{Name: v.Name.Lexeme, Fields: fields, Tag: i}
		et.Variants = append(et.Variants, vd)
		et.ByName[v.Name.Lexeme] = vd
	}
	return et, nil
}

func (c *checker) resolveTypeExpr(te parser.TypeExpr) (Type, error) {
	switch t := te.(type) {
	case *parser.NamedType:
		name := t.Name.Lexeme
		if builtin, ok := BuiltinTypes[name]; ok {
			return builtin, nil
		}
		if st, ok := c.structs[name]; ok {
			if err := c.checkModuleAccess(name, t.Name); err != nil {
				return nil, err
			}
			return st, nil
		}
		if et, ok := c.enums[name]; ok {
			return et, nil
		}
		return nil, c.errorf(t.Name, "unknown type %q", name)

	case *parser.GenericType:
		params := make([]Type, len(t.Params))
		for i, p := range t.Params {
			resolved, err := c.resolveTypeExpr(p)
			if err != nil {
				return nil, err
			}
			params[i] = resolved
		}
		return &GenType{Con: t.Name.Lexeme, Params: params}, nil

	case *parser.FnType:
		params := make([]Type, len(t.Params))
		for i, p := range t.Params {
			resolved, err := c.resolveTypeExpr(p)
			if err != nil {
				return nil, err
			}
			params[i] = resolved
		}
		ret, err := c.resolveTypeExpr(t.RetType)
		if err != nil {
			return nil, err
		}
		return &FnType{Params: params, Ret: ret}, nil

	default:
		return nil, fmt.Errorf("unhandled TypeExpr %T", te)
	}
}

// ── Pass 2: check function bodies ────────────────────────────────────────────

func (c *checker) checkFnDecl(d *parser.FnDecl) error {
	sig := c.fnSigs[d.Name.Lexeme]
	sc := newScope(nil)
	for i, p := range d.Params {
		sc.define(p.Name.Lexeme, sig.Params[i])
	}
	// Set the current function's effects for callee-checking within the body.
	prev := c.curEffects
	c.curEffects = c.fnEffects[d.Name.Lexeme] // nil if unannotated

	// Type-check contract clauses.
	retType := sig.Ret
	for _, cc := range d.Contracts {
		clauseSc := newScope(sc)
		if cc.Kind == parser.ContractEnsures {
			clauseSc.define("result", retType)
		}
		condType, err := c.checkExpr(cc.Expr, clauseSc, TBool)
		if err != nil {
			c.curEffects = prev
			return err
		}
		if !condType.Equals(TBool) {
			c.curEffects = prev
			return c.errorf(cc.Tok, "contract clause must be bool, got %s", condType)
		}
	}

	err := c.checkBlock(d.Body, sc, sig.Ret)
	c.curEffects = prev
	return err
}

func (c *checker) checkBlock(block *parser.BlockStmt, sc *scope, retType Type) error {
	inner := newScope(sc)
	for _, stmt := range block.Stmts {
		if err := c.checkStmt(stmt, inner, retType); err != nil {
			c.errs = append(c.errs, err)
			// Continue to the next statement — collect all errors.
		}
	}
	return nil
}

func (c *checker) checkStmt(stmt parser.Stmt, sc *scope, retType Type) error {
	switch s := stmt.(type) {
	case *parser.LetStmt:
		return c.checkLetStmt(s, sc)
	case *parser.ReturnStmt:
		return c.checkReturnStmt(s, sc, retType)
	case *parser.ExprStmt:
		_, err := c.checkExpr(s.X, sc, nil)
		return err
	case *parser.IfStmt:
		return c.checkIfStmt(s, sc, retType)
	case *parser.LoopStmt:
		return c.checkBlock(s.Body, sc, retType)
	case *parser.ForStmt:
		return c.checkForStmt(s, sc, retType)
	case *parser.BreakStmt:
		return nil
	case *parser.BlockStmt:
		return c.checkBlock(s, sc, retType)
	case *parser.AssignStmt:
		return c.checkAssignStmt(s, sc)
	case *parser.FieldAssignStmt:
		return c.checkFieldAssignStmt(s, sc)
	case *parser.AssertStmt:
		return c.checkAssertStmt(s, sc)
	default:
		return fmt.Errorf("unhandled Stmt %T", stmt)
	}
}

func (c *checker) checkAssignStmt(s *parser.AssignStmt, sc *scope) error {
	info, ok := sc.lookupInfo(s.Name.Lexeme)
	if !ok {
		return c.errorf(s.Name, "undefined identifier %q", s.Name.Lexeme)
	}
	if !info.mutable {
		return c.errorf(s.Name, "cannot assign to immutable variable %q", s.Name.Lexeme)
	}
	valType, err := c.checkExpr(s.Value, sc, info.typ)
	if err != nil {
		return err
	}
	coerced, ok := Coerce(valType, info.typ)
	if !ok {
		return c.errorf(s.Name, "type mismatch: cannot assign %s to %s", valType, info.typ)
	}
	c.exprTypes[s.Value] = coerced
	return nil
}

func (c *checker) checkFieldAssignStmt(s *parser.FieldAssignStmt, sc *scope) error {
	// Type-check the receiver.
	recvType, err := c.checkExpr(s.Target.Receiver, sc, nil)
	if err != nil {
		return err
	}

	// Mutability check: walk to the root identifier.
	if err := c.checkReceiverMutable(s.Target.Receiver, s.Target.Dot, sc); err != nil {
		return err
	}

	// Transparently dereference ref<T> / refmut<T>.
	if gen, ok := recvType.(*GenType); ok &&
		(gen.Con == "ref" || gen.Con == "refmut") && len(gen.Params) > 0 {
		recvType = gen.Params[0]
	}

	st, ok := recvType.(*StructType)
	if !ok {
		return c.errorf(s.Target.Dot, "field assignment on non-struct type %s", recvType)
	}
	fieldType, ok := st.Fields[s.Target.Field.Lexeme]
	if !ok {
		return c.errorf(s.Target.Field, "unknown field %q on %s", s.Target.Field.Lexeme, st.Name)
	}

	// Type-check the value.
	valType, err := c.checkExpr(s.Value, sc, fieldType)
	if err != nil {
		return err
	}
	coerced, ok := Coerce(valType, fieldType)
	if !ok {
		return c.errorf(s.Target.Field,
			"type mismatch: cannot assign %s to field %q of type %s", valType, s.Target.Field.Lexeme, fieldType)
	}
	c.exprTypes[s.Value] = coerced
	c.record(s.Target, fieldType)
	return nil
}

// checkReceiverMutable walks a field-access chain to the root identifier and
// verifies it is declared mutable (or accessed through a ref/refmut pointer).
func (c *checker) checkReceiverMutable(recv parser.Expr, errTok lexer.Token, sc *scope) error {
	switch r := recv.(type) {
	case *parser.IdentExpr:
		info, ok := sc.lookupInfo(r.Tok.Lexeme)
		if !ok {
			return c.errorf(r.Tok, "undefined identifier %q", r.Tok.Lexeme)
		}
		// Assigning through a ref/refmut pointer is always permitted.
		if gen, ok := info.typ.(*GenType); ok && (gen.Con == "ref" || gen.Con == "refmut") {
			return nil
		}
		if !info.mutable {
			return c.errorf(r.Tok, "cannot assign to field of immutable variable %q", r.Tok.Lexeme)
		}
		return nil
	case *parser.FieldExpr:
		return c.checkReceiverMutable(r.Receiver, errTok, sc)
	default:
		// Other expressions (calls, index) — allow; runtime semantics handle it.
		return nil
	}
}

func (c *checker) checkLetStmt(s *parser.LetStmt, sc *scope) error {
	var hint Type
	if s.TypeAnn != nil {
		var err error
		hint, err = c.resolveTypeExpr(s.TypeAnn)
		if err != nil {
			return err
		}
	}
	valType, err := c.checkExpr(s.Value, sc, hint)
	if err != nil {
		return err
	}
	if hint != nil {
		coerced, ok := Coerce(valType, hint)
		if !ok {
			return c.errorf(s.LetTok, "type mismatch: cannot use %s as %s", valType, hint)
		}
		valType = coerced
		c.exprTypes[s.Value] = coerced
	}
	if s.Mut {
		sc.defineMut(s.Name.Lexeme, valType)
	} else {
		sc.define(s.Name.Lexeme, valType)
	}
	return nil
}

func (c *checker) checkReturnStmt(s *parser.ReturnStmt, sc *scope, retType Type) error {
	if s.Value == nil {
		if !retType.Equals(TUnit) {
			return c.errorf(s.ReturnTok, "bare return in function returning %s", retType)
		}
		return nil
	}
	valType, err := c.checkExpr(s.Value, sc, retType)
	if err != nil {
		return err
	}
	coerced, ok := Coerce(valType, retType)
	if !ok {
		return c.errorf(s.ReturnTok, "return type mismatch: got %s, expected %s", valType, retType)
	}
	c.exprTypes[s.Value] = coerced
	return nil
}

func (c *checker) checkIfStmt(s *parser.IfStmt, sc *scope, retType Type) error {
	condType, err := c.checkExpr(s.Cond, sc, TBool)
	if err != nil {
		return err
	}
	if !condType.Equals(TBool) {
		return c.errorf(s.IfTok, "if condition must be bool, got %s", condType)
	}
	if err := c.checkBlock(s.Then, sc, retType); err != nil {
		return err
	}
	if s.Else != nil {
		return c.checkStmt(s.Else, sc, retType)
	}
	return nil
}

// ── Expressions ──────────────────────────────────────────────────────────────

// checkExpr infers the type of expr, records it, and returns it.
// hint is an optional expected type used to coerce literal sentinels.
func (c *checker) checkExpr(expr parser.Expr, sc *scope, hint Type) (Type, error) {
	t, err := c.inferExpr(expr, sc, hint)
	if err != nil {
		return nil, err
	}
	c.record(expr, t)
	return t, nil
}

func (c *checker) inferExpr(expr parser.Expr, sc *scope, hint Type) (Type, error) {
	switch e := expr.(type) {
	case *parser.IntLitExpr:
		if hint != nil && IsIntType(hint) {
			return hint, nil
		}
		return TIntLit, nil

	case *parser.FloatLitExpr:
		if hint != nil && IsFloatType(hint) {
			return hint, nil
		}
		return TFloatLit, nil

	case *parser.StringLitExpr:
		return TStr, nil

	case *parser.BoolLitExpr:
		return TBool, nil

	case *parser.IdentExpr:
		return c.inferIdentExpr(e, sc)

	case *parser.BinaryExpr:
		return c.inferBinaryExpr(e, sc, hint)

	case *parser.UnaryExpr:
		return c.inferUnaryExpr(e, sc, hint)

	case *parser.CallExpr:
		if ident, ok := e.Fn.(*parser.IdentExpr); ok {
			switch ident.Tok.Type {
			case lexer.TokOk, lexer.TokErr, lexer.TokSome, lexer.TokNone:
				return c.inferConstructorCall(e, ident, sc, hint)
			case lexer.TokMove:
				return c.inferMoveCall(e, sc)
			}
			switch ident.Tok.Lexeme {
			case "vec_new":
				return c.inferVecNew(e, ident, sc, hint)
			case "vec_push":
				return c.inferVecPush(e, ident, sc)
			case "vec_len":
				return c.inferVecLen(e, ident, sc)
			case "refmut":
				return c.inferRefmutCall(e, sc)
			}
		}
		return c.inferCallExpr(e, sc)

	case *parser.MatchExpr:
		return c.inferMatchExpr(e, sc, hint)

	case *parser.FieldExpr:
		return c.inferFieldExpr(e, sc)

	case *parser.IndexExpr:
		return c.inferIndexExpr(e, sc)

	case *parser.MustExpr:
		return c.inferMustExpr(e, sc, hint)

	case *parser.ReturnExpr:
		// return inside a must{} arm — type is never (exits the function)
		if _, err := c.checkExpr(e.Value, sc, nil); err != nil {
			return nil, err
		}
		return TNever, nil

	case *parser.BreakExpr:
		// break inside a must{} arm — type is never (exits the enclosing loop)
		return TNever, nil

	case *parser.StructLitExpr:
		return c.inferStructLitExpr(e, sc)

	case *parser.PathExpr:
		return c.inferPathExpr(e, sc)

	default:
		return nil, fmt.Errorf("unhandled Expr %T", expr)
	}
}

func (c *checker) inferIdentExpr(e *parser.IdentExpr, sc *scope) (Type, error) {
	name := e.Tok.Lexeme

	// Value-level keywords with known types
	switch name {
	case "unit":
		return TUnit, nil
	case "true", "false":
		return TBool, nil
	}

	// Variable lookup
	if t, ok := sc.lookup(name); ok {
		return t, nil
	}

	// Function name
	if sig, ok := c.fnSigs[name]; ok {
		if err := c.checkModuleAccess(name, e.Tok); err != nil {
			return nil, err
		}
		return sig, nil
	}

	// Built-in constructors (ok, err, some, none, move) — typed at call site
	switch e.Tok.Type {
	case lexer.TokOk, lexer.TokErr, lexer.TokSome, lexer.TokNone, lexer.TokMove:
		return &GenType{Con: name}, nil
	}

	return nil, c.errorf(e.Tok, "undefined identifier %q", name)
}

func (c *checker) inferBinaryExpr(e *parser.BinaryExpr, sc *scope, hint Type) (Type, error) {
	switch e.Op.Type {
	case lexer.TokPlus, lexer.TokMinus, lexer.TokStar, lexer.TokSlash, lexer.TokPercent:
		// Arithmetic: both sides should be the same numeric type.
		// Pass hint down so literals on either side can coerce.
		left, err := c.checkExpr(e.Left, sc, hint)
		if err != nil {
			return nil, err
		}
		right, err := c.checkExpr(e.Right, sc, left)
		if err != nil {
			return nil, err
		}
		unified, ok := UnifyNumeric(left, right)
		if !ok {
			return nil, c.errorf(e.Op, "cannot apply %s to %s and %s", e.Op.Lexeme, left, right)
		}
		// Pin both operands to the resolved concrete type
		c.exprTypes[e.Left] = unified
		c.exprTypes[e.Right] = unified
		return unified, nil

	case lexer.TokEqEq, lexer.TokBangEq:
		left, err := c.checkExpr(e.Left, sc, nil)
		if err != nil {
			return nil, err
		}
		right, err := c.checkExpr(e.Right, sc, left)
		if err != nil {
			return nil, err
		}
		if _, ok := Unify(left, right); !ok {
			return nil, c.errorf(e.Op, "cannot compare %s with %s", left, right)
		}
		return TBool, nil

	case lexer.TokLt, lexer.TokGt, lexer.TokLtEq, lexer.TokGtEq:
		left, err := c.checkExpr(e.Left, sc, nil)
		if err != nil {
			return nil, err
		}
		right, err := c.checkExpr(e.Right, sc, left)
		if err != nil {
			return nil, err
		}
		if _, ok := UnifyNumeric(left, right); !ok {
			return nil, c.errorf(e.Op, "cannot order-compare %s and %s", left, right)
		}
		return TBool, nil

	case lexer.TokAnd, lexer.TokOr:
		left, err := c.checkExpr(e.Left, sc, TBool)
		if err != nil {
			return nil, err
		}
		right, err := c.checkExpr(e.Right, sc, TBool)
		if err != nil {
			return nil, err
		}
		if !left.Equals(TBool) || !right.Equals(TBool) {
			return nil, c.errorf(e.Op, "%s requires bool operands, got %s and %s", e.Op.Lexeme, left, right)
		}
		return TBool, nil

	default:
		return nil, c.errorf(e.Op, "unknown binary operator %q", e.Op.Lexeme)
	}
}

func (c *checker) inferUnaryExpr(e *parser.UnaryExpr, sc *scope, hint Type) (Type, error) {
	switch e.Op.Type {
	case lexer.TokBang, lexer.TokNot:
		t, err := c.checkExpr(e.Operand, sc, TBool)
		if err != nil {
			return nil, err
		}
		if !t.Equals(TBool) {
			return nil, c.errorf(e.Op, "! / not requires bool, got %s", t)
		}
		return TBool, nil

	case lexer.TokMinus:
		t, err := c.checkExpr(e.Operand, sc, hint)
		if err != nil {
			return nil, err
		}
		if !IsNumericType(t) && t != TIntLit && t != TFloatLit {
			return nil, c.errorf(e.Op, "unary - requires numeric type, got %s", t)
		}
		return t, nil

	case lexer.TokAmp:
		t, err := c.checkExpr(e.Operand, sc, nil)
		if err != nil {
			return nil, err
		}
		return &GenType{Con: "ref", Params: []Type{t}}, nil

	case lexer.TokStar:
		t, err := c.checkExpr(e.Operand, sc, nil)
		if err != nil {
			return nil, err
		}
		gen, ok := t.(*GenType)
		if !ok || (gen.Con != "ref" && gen.Con != "refmut") || len(gen.Params) == 0 {
			return nil, c.errorf(e.Op, "unary * requires ref<T> or refmut<T>, got %s", t)
		}
		return gen.Params[0], nil

	default:
		return nil, c.errorf(e.Op, "unknown unary operator %q", e.Op.Lexeme)
	}
}

func (c *checker) inferCallExpr(e *parser.CallExpr, sc *scope) (Type, error) {
	fnType, err := c.checkExpr(e.Fn, sc, nil)
	if err != nil {
		return nil, err
	}
	sig, ok := fnType.(*FnType)
	if !ok {
		return nil, c.errorf(e.Fn.Pos(), "cannot call non-function type %s", fnType)
	}
	// Effects compatibility check.
	if ident, ok2 := e.Fn.(*parser.IdentExpr); ok2 {
		if err := c.checkEffectsCompat(ident.Tok, ident.Tok.Lexeme); err != nil {
			return nil, err
		}
	}
	if len(e.Args) != len(sig.Params) {
		return nil, c.errorf(e.LParen,
			"argument count mismatch: expected %d, got %d", len(sig.Params), len(e.Args))
	}
	for i, arg := range e.Args {
		argType, err := c.checkExpr(arg, sc, sig.Params[i])
		if err != nil {
			return nil, err
		}
		coerced, ok := Coerce(argType, sig.Params[i])
		if !ok {
			return nil, c.errorf(arg.Pos(),
				"argument %d: cannot use %s as %s", i+1, argType, sig.Params[i])
		}
		c.exprTypes[arg] = coerced
	}
	return sig.Ret, nil
}

// checkEffectsCompat enforces that callee's effects are compatible with the
// current function's declared effects.
//
//   - Caller unannotated (curEffects == nil): no check — gradual adoption.
//   - Caller pure: callee must also be pure (or unannotated ≡ trusted).
//   - Caller effects(X): callee's effects must be ⊆ X (unannotated = trusted).
func (c *checker) checkEffectsCompat(callTok lexer.Token, calleeName string) error {
	if c.curEffects == nil || c.curEffects.Kind == parser.EffectsNone {
		return nil
	}
	calleeEff := c.fnEffects[calleeName]
	if calleeEff == nil {
		return nil // callee unannotated — trusted
	}

	switch c.curEffects.Kind {
	case parser.EffectsPure:
		if calleeEff.Kind != parser.EffectsPure {
			return c.errorf(callTok,
				"pure function cannot call %q which has effects %v",
				calleeName, calleeEff.Names)
		}

	case parser.EffectsDecl:
		if calleeEff.Kind == parser.EffectsPure {
			return nil
		}
		if calleeEff.Kind == parser.EffectsDecl {
			allowed := make(map[string]bool, len(c.curEffects.Names))
			for _, e := range c.curEffects.Names {
				allowed[e] = true
			}
			for _, e := range calleeEff.Names {
				if !allowed[e] {
					return c.errorf(callTok,
						"function with effects(%v) cannot call %q which requires effect %q",
						c.curEffects.Names, calleeName, e)
				}
			}
		}
	}
	return nil
}

func (c *checker) inferFieldExpr(e *parser.FieldExpr, sc *scope) (Type, error) {
	recvType, err := c.checkExpr(e.Receiver, sc, nil)
	if err != nil {
		return nil, err
	}
	// Transparently dereference ref<T> and refmut<T>
	if gen, ok := recvType.(*GenType); ok &&
		(gen.Con == "ref" || gen.Con == "refmut") && len(gen.Params) > 0 {
		recvType = gen.Params[0]
	}
	st, ok := recvType.(*StructType)
	if !ok {
		return nil, c.errorf(e.Dot, "field access on non-struct type %s", recvType)
	}
	fieldType, ok := st.Fields[e.Field.Lexeme]
	if !ok {
		return nil, c.errorf(e.Field, "unknown field %q on %s", e.Field.Lexeme, st.Name)
	}
	return fieldType, nil
}

func (c *checker) inferIndexExpr(e *parser.IndexExpr, sc *scope) (Type, error) {
	collType, err := c.checkExpr(e.Collection, sc, nil)
	if err != nil {
		return nil, err
	}
	if _, err := c.checkExpr(e.Index, sc, TU64); err != nil {
		return nil, err
	}
	gen, ok := collType.(*GenType)
	if !ok || (gen.Con != "vec" && gen.Con != "ring") || len(gen.Params) == 0 {
		return nil, c.errorf(e.LBracket, "cannot index type %s", collType)
	}
	return gen.Params[0], nil
}

func (c *checker) inferMustExpr(e *parser.MustExpr, sc *scope, hint Type) (Type, error) {
	operandType, err := c.checkExpr(e.X, sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := operandType.(*GenType)
	if !ok || (gen.Con != "result" && gen.Con != "option") {
		return nil, c.errorf(e.MustTok,
			"must{} requires result<T,E> or option<T>, got %s", operandType)
	}
	// Check each arm body; collect non-never types
	var bodyType Type
	for _, arm := range e.Arms {
		armSc, err := c.checkPattern(arm.Pattern, gen, sc)
		if err != nil {
			return nil, err
		}
		armType, err := c.checkExpr(arm.Body, armSc, hint)
		if err != nil {
			return nil, err
		}
		if armType.Equals(TNever) {
			continue
		}
		if bodyType == nil {
			bodyType = armType
		} else {
			unified, ok := Unify(bodyType, armType)
			if !ok {
				return nil, c.errorf(arm.Arrow,
					"must{} arm type %s does not match expected %s", armType, bodyType)
			}
			bodyType = unified
		}
	}
	if bodyType == nil {
		bodyType = TUnit
	}
	return bodyType, nil
}

func (c *checker) inferMatchExpr(e *parser.MatchExpr, sc *scope, hint Type) (Type, error) {
	matchedType, err := c.checkExpr(e.X, sc, nil)
	if err != nil {
		return nil, err
	}
	var bodyType Type
	for _, arm := range e.Arms {
		armSc, err := c.checkPattern(arm.Pattern, matchedType, sc)
		if err != nil {
			return nil, err
		}
		armType, err := c.checkExpr(arm.Body, armSc, hint)
		if err != nil {
			return nil, err
		}
		if armType.Equals(TNever) {
			continue
		}
		if bodyType == nil {
			bodyType = armType
		} else {
			unified, ok := Unify(bodyType, armType)
			if !ok {
				return nil, c.errorf(arm.Arrow,
					"match arm type %s does not match expected %s", armType, bodyType)
			}
			bodyType = unified
		}
	}
	if bodyType == nil {
		bodyType = TUnit
	}
	return bodyType, nil
}

// checkPattern processes a match/must arm pattern, records types, and returns
// a new scope with any bound variables.
func (c *checker) checkPattern(pattern parser.Expr, matchedType Type, sc *scope) (*scope, error) {
	armSc := newScope(sc)
	switch p := pattern.(type) {
	case *parser.IdentExpr:
		name := p.Tok.Lexeme
		switch name {
		case "_":
			c.record(p, TUnit)
		case "none":
			c.record(p, matchedType)
		default:
			// bare identifier: treat as wildcard binding
			c.record(p, matchedType)
			armSc.define(name, matchedType)
		}
	case *parser.PathExpr:
		// Unit enum variant pattern: Shape::Point
		et, ok := c.enums[p.Head.Lexeme]
		if !ok {
			return nil, c.errorf(p.Head, "undefined enum %q in pattern", p.Head.Lexeme)
		}
		vd, ok := et.ByName[p.Tail.Lexeme]
		if !ok {
			return nil, c.errorf(p.Tail, "enum %s has no variant %q", et.Name, p.Tail.Lexeme)
		}
		if len(vd.Fields) != 0 {
			return nil, c.errorf(p.Tail, "variant %s::%s has fields — use %s::%s(...) pattern",
				et.Name, vd.Name, et.Name, vd.Name)
		}
		c.record(p, et)

	case *parser.CallExpr:
		// Check for enum variant pattern: Shape::Circle(r)
		if path, ok2 := p.Fn.(*parser.PathExpr); ok2 {
			et, ok := c.enums[path.Head.Lexeme]
			if !ok {
				return nil, c.errorf(path.Head, "undefined enum %q in pattern", path.Head.Lexeme)
			}
			vd, ok := et.ByName[path.Tail.Lexeme]
			if !ok {
				return nil, c.errorf(path.Tail, "enum %s has no variant %q", et.Name, path.Tail.Lexeme)
			}
			if len(p.Args) != len(vd.Fields) {
				return nil, c.errorf(p.LParen,
					"variant %s::%s has %d field(s), pattern binds %d",
					et.Name, vd.Name, len(vd.Fields), len(p.Args))
			}
			c.record(path, et)
			for i, arg := range p.Args {
				fieldType := vd.Fields[i]
				if v, ok3 := arg.(*parser.IdentExpr); ok3 && v.Tok.Lexeme != "_" {
					armSc.define(v.Tok.Lexeme, fieldType)
					c.record(v, fieldType)
				}
			}
			return armSc, nil
		}

		fn, ok := p.Fn.(*parser.IdentExpr)
		if !ok {
			return nil, c.errorf(p.Fn.Pos(), "invalid pattern")
		}
		c.record(fn, matchedType)
		switch fn.Tok.Lexeme {
		case "some":
			gen, ok := matchedType.(*GenType)
			if !ok || gen.Con != "option" || len(gen.Params) == 0 {
				return nil, c.errorf(fn.Tok, "some() pattern requires option<T>, got %s", matchedType)
			}
			innerType := gen.Params[0]
			if len(p.Args) == 1 {
				if v, ok2 := p.Args[0].(*parser.IdentExpr); ok2 {
					armSc.define(v.Tok.Lexeme, innerType)
					c.record(v, innerType)
				}
			}
		case "ok":
			gen, ok := matchedType.(*GenType)
			if !ok || gen.Con != "result" || len(gen.Params) == 0 {
				return nil, c.errorf(fn.Tok, "ok() pattern requires result<T,E>, got %s", matchedType)
			}
			innerType := gen.Params[0]
			if len(p.Args) == 1 {
				if v, ok2 := p.Args[0].(*parser.IdentExpr); ok2 {
					armSc.define(v.Tok.Lexeme, innerType)
					c.record(v, innerType)
				}
			}
		case "err":
			gen, ok := matchedType.(*GenType)
			if !ok || gen.Con != "result" || len(gen.Params) < 2 {
				return nil, c.errorf(fn.Tok, "err() pattern requires result<T,E>, got %s", matchedType)
			}
			innerType := gen.Params[1]
			if len(p.Args) == 1 {
				if v, ok2 := p.Args[0].(*parser.IdentExpr); ok2 {
					armSc.define(v.Tok.Lexeme, innerType)
					c.record(v, innerType)
				}
			}
		default:
			return nil, c.errorf(fn.Tok, "unknown pattern constructor %q", fn.Tok.Lexeme)
		}
	default:
		// Literal / expression pattern (int, float, string, negation).
		litType, err := c.checkExpr(pattern, sc, matchedType)
		if err != nil {
			return nil, err
		}
		if _, ok := Coerce(litType, matchedType); !ok {
			return nil, c.errorf(pattern.Pos(),
				"pattern type %s is incompatible with matched type %s", litType, matchedType)
		}
	}
	return armSc, nil
}

// ── vec built-in functions ────────────────────────────────────────────────────

func (c *checker) inferVecNew(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope, hint Type) (Type, error) {
	if len(e.Args) != 0 {
		return nil, c.errorf(e.LParen, "vec_new() takes no arguments")
	}
	var elemType Type
	if gen, ok := hint.(*GenType); ok && gen.Con == "vec" && len(gen.Params) > 0 {
		elemType = gen.Params[0]
	}
	if elemType == nil {
		return nil, c.errorf(fn.Tok, "vec_new() requires a type annotation to infer element type")
	}
	t := &GenType{Con: "vec", Params: []Type{elemType}}
	c.record(fn, t)
	return t, nil
}

func (c *checker) inferVecPush(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 2 {
		return nil, c.errorf(e.LParen, "vec_push() takes 2 arguments: (vec, value)")
	}
	vecType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := vecType.(*GenType)
	if !ok || gen.Con != "vec" || len(gen.Params) == 0 {
		return nil, c.errorf(e.Args[0].Pos(), "vec_push() first argument must be vec<T>, got %s", vecType)
	}
	elemType := gen.Params[0]
	valType, err := c.checkExpr(e.Args[1], sc, elemType)
	if err != nil {
		return nil, err
	}
	coerced, ok := Coerce(valType, elemType)
	if !ok {
		return nil, c.errorf(e.Args[1].Pos(),
			"vec_push() value type %s does not match vec element type %s", valType, elemType)
	}
	c.exprTypes[e.Args[1]] = coerced
	c.record(fn, TUnit)
	return TUnit, nil
}

func (c *checker) inferVecLen(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "vec_len() takes 1 argument")
	}
	vecType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := vecType.(*GenType)
	if !ok || (gen.Con != "vec" && gen.Con != "ring") {
		return nil, c.errorf(e.Args[0].Pos(), "vec_len() requires vec<T> or ring<T>, got %s", vecType)
	}
	c.record(fn, TU64)
	return TU64, nil
}

func (c *checker) inferConstructorCall(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope, hint Type) (Type, error) {
	switch fn.Tok.Type {
	case lexer.TokSome:
		if len(e.Args) != 1 {
			return nil, c.errorf(e.LParen, "some() takes 1 argument")
		}
		var innerHint Type
		if gen, ok := hint.(*GenType); ok && gen.Con == "option" && len(gen.Params) > 0 {
			innerHint = gen.Params[0]
		}
		argType, err := c.checkExpr(e.Args[0], sc, innerHint)
		if err != nil {
			return nil, err
		}
		t := &GenType{Con: "option", Params: []Type{argType}}
		c.record(fn, t)
		return t, nil

	case lexer.TokNone:
		if hint != nil {
			if gen, ok := hint.(*GenType); ok && gen.Con == "option" {
				c.record(fn, hint)
				return hint, nil
			}
		}
		t := &GenType{Con: "option"}
		c.record(fn, t)
		return t, nil

	case lexer.TokOk:
		if len(e.Args) != 1 {
			return nil, c.errorf(e.LParen, "ok() takes 1 argument")
		}
		var innerHint Type
		var errHintType Type
		if gen, ok := hint.(*GenType); ok && gen.Con == "result" {
			if len(gen.Params) > 0 {
				innerHint = gen.Params[0]
			}
			if len(gen.Params) > 1 {
				errHintType = gen.Params[1]
			}
		}
		argType, err := c.checkExpr(e.Args[0], sc, innerHint)
		if err != nil {
			return nil, err
		}
		if errHintType == nil {
			errHintType = &GenType{Con: "_err"}
		}
		t := &GenType{Con: "result", Params: []Type{argType, errHintType}}
		c.record(fn, t)
		return t, nil

	case lexer.TokErr:
		if len(e.Args) != 1 {
			return nil, c.errorf(e.LParen, "err() takes 1 argument")
		}
		var innerHint Type
		var okHintType Type
		if gen, ok := hint.(*GenType); ok && gen.Con == "result" {
			if len(gen.Params) > 1 {
				innerHint = gen.Params[1]
			}
			if len(gen.Params) > 0 {
				okHintType = gen.Params[0]
			}
		}
		argType, err := c.checkExpr(e.Args[0], sc, innerHint)
		if err != nil {
			return nil, err
		}
		if okHintType == nil {
			okHintType = &GenType{Con: "_ok"}
		}
		t := &GenType{Con: "result", Params: []Type{okHintType, argType}}
		c.record(fn, t)
		return t, nil
	}
	return nil, fmt.Errorf("unreachable constructor")
}

// inferMoveCall handles move(x) — semantically transfers ownership; type is T.
func (c *checker) inferMoveCall(e *parser.CallExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "move() takes 1 argument")
	}
	t, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	if fn, ok := e.Fn.(*parser.IdentExpr); ok {
		c.record(fn, &GenType{Con: "move", Params: []Type{t}})
	}
	c.record(e, t)
	return t, nil
}

// inferRefmutCall handles refmut(x) — creates a mutable reference; type is refmut<T>.
func (c *checker) inferRefmutCall(e *parser.CallExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "refmut() takes 1 argument")
	}
	t, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	refmutType := &GenType{Con: "refmut", Params: []Type{t}}
	if fn, ok := e.Fn.(*parser.IdentExpr); ok {
		c.record(fn, refmutType)
	}
	c.record(e, refmutType)
	return refmutType, nil
}

func (c *checker) checkForStmt(s *parser.ForStmt, sc *scope, retType Type) error {
	collType, err := c.checkExpr(s.Collection, sc, nil)
	if err != nil {
		return err
	}
	gen, ok := collType.(*GenType)
	if !ok || (gen.Con != "vec" && gen.Con != "ring") || len(gen.Params) == 0 {
		return c.errorf(s.ForTok, "for..in requires vec<T> or ring<T>, got %s", collType)
	}
	elemType := gen.Params[0]
	// Create a scope with the element variable bound, then check the body.
	forSc := newScope(sc)
	forSc.define(s.Var.Lexeme, elemType)
	return c.checkBlock(s.Body, forSc, retType)
}

func (c *checker) checkAssertStmt(s *parser.AssertStmt, sc *scope) error {
	condType, err := c.checkExpr(s.Expr, sc, TBool)
	if err != nil {
		return err
	}
	if !condType.Equals(TBool) {
		return c.errorf(s.AssertTok, "assert requires bool, got %s", condType)
	}
	return nil
}

// multiError joins multiple type-check errors into one.
type multiError []error

func (m multiError) Error() string {
	if len(m) == 1 {
		return m[0].Error()
	}
	msgs := make([]string, len(m))
	for i, e := range m {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}

// inferPathExpr handles Enum::Variant — a unit enum variant or the fn-position
// of a call expression that constructs a data-carrying variant.
func (c *checker) inferPathExpr(e *parser.PathExpr, sc *scope) (Type, error) {
	et, ok := c.enums[e.Head.Lexeme]
	if !ok {
		return nil, c.errorf(e.Head, "undefined enum %q", e.Head.Lexeme)
	}
	vd, ok := et.ByName[e.Tail.Lexeme]
	if !ok {
		return nil, c.errorf(e.Tail, "enum %s has no variant %q", et.Name, e.Tail.Lexeme)
	}
	// Unit variant used as a value — record and return the enum type.
	if len(vd.Fields) == 0 {
		c.record(e, et)
		return et, nil
	}
	// Data variant in non-call position: return a synthetic FnType so that
	// inferCallExpr can check the arguments when this PathExpr is the Fn.
	ft := &FnType{Params: vd.Fields, Ret: et}
	c.record(e, ft)
	return ft, nil
}

func (c *checker) inferStructLitExpr(e *parser.StructLitExpr, sc *scope) (Type, error) {
	st, ok := c.structs[e.TypeName.Lexeme]
	if !ok {
		return nil, c.errorf(e.TypeName, "undefined struct %q", e.TypeName.Lexeme)
	}
	if err := c.checkModuleAccess(e.TypeName.Lexeme, e.TypeName); err != nil {
		return nil, err
	}
	provided := make(map[string]bool, len(e.Fields))
	for _, fi := range e.Fields {
		if _, ok := st.Fields[fi.Name.Lexeme]; !ok {
			return nil, c.errorf(fi.Name, "unknown field %q on struct %s", fi.Name.Lexeme, st.Name)
		}
		if provided[fi.Name.Lexeme] {
			return nil, c.errorf(fi.Name, "duplicate field %q in struct literal", fi.Name.Lexeme)
		}
		provided[fi.Name.Lexeme] = true
		fieldType := st.Fields[fi.Name.Lexeme]
		valType, err := c.checkExpr(fi.Value, sc, fieldType)
		if err != nil {
			return nil, err
		}
		coerced, ok := Coerce(valType, fieldType)
		if !ok {
			return nil, c.errorf(fi.Name,
				"type mismatch: cannot use %s as %s for field %q", valType, fieldType, fi.Name.Lexeme)
		}
		c.exprTypes[fi.Value] = coerced
	}
	for name := range st.Fields {
		if !provided[name] {
			return nil, c.errorf(e.TypeName, "missing field %q in struct literal for %s", name, st.Name)
		}
	}
	return st, nil
}
