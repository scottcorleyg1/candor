// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/candor-core/candor

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
	"strconv"
	"strings"

	"github.com/candor-core/candor/compiler/lexer"
	"github.com/candor-core/candor/compiler/parser"
)

// Error is a type-check diagnostic with source position.
type Error struct {
	Tok  lexer.Token
	Msg  string
	Hint string // optional suggestion, e.g. "did you mean X?"
}

func (e *Error) Error() string {
	if e.Hint != "" {
		return fmt.Sprintf("%s:%d:%d: %s\n    hint: %s", e.Tok.File, e.Tok.Line, e.Tok.Col, e.Msg, e.Hint)
	}
	return fmt.Sprintf("%s:%d:%d: %s", e.Tok.File, e.Tok.Line, e.Tok.Col, e.Msg)
}

// Warning is a non-fatal diagnostic (unused variable, shadowing, etc.).
type Warning struct {
	Tok  lexer.Token
	Msg  string
}

func (w Warning) Error() string {
	return fmt.Sprintf("%s:%d:%d: warning: %s", w.Tok.File, w.Tok.Line, w.Tok.Col, w.Msg)
}

// varUsage tracks how many times a let-bound variable is read, for unused-var warnings.
type varUsage struct {
	tok  lexer.Token
	name string
	uses int
}

// LambdaInfo holds type and metadata for a lambda expression.
type LambdaInfo struct {
	Node         *parser.LambdaExpr
	Name         string   // generated C name, e.g. _cnd_lambda_1
	Sig          *FnType  // resolved signature
	Captures     []string // captured variable names (in order of first appearance)
	CaptureTypes []Type   // parallel to Captures: the type of each captured variable
	CaptureByRef []bool   // parallel to Captures: true if captured by pointer (outer var is mut)
}

// SpawnInfo holds type and metadata for a spawn expression.
type SpawnInfo struct {
	Node         *parser.SpawnExpr
	Name         string // generated C name, e.g. _cnd_spawn_1
	ResultType   Type   // T in task<T>
	Captures     []string
	CaptureTypes []Type
	CaptureByRef []bool
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
	// Lambdas holds all lambda expressions in declaration order.
	Lambdas []*LambdaInfo
	// ExternFns is the set of extern function names (declared with extern fn).
	ExternFns map[string]bool
	// GenericInstances holds all monomorphized generic function instances.
	GenericInstances []*GenericInstance
	// CallSiteGeneric maps a call's Fn expression (IdentExpr) to its generic instance.
	// Used by the emitter to choose the mangled name at each call site.
	CallSiteGeneric map[parser.Expr]*GenericInstance
	// MethodCalls maps a CallExpr to the mangled C name of the method being called.
	// Set when the call is x.method(args) where method is defined in an impl block.
	MethodCalls map[parser.Expr]string
	// ImplDecls holds all impl blocks for the emitter to emit method bodies.
	ImplDecls []*parser.ImplDecl
	// TraitDecls holds all trait declarations (for documentation/future use).
	TraitDecls []*parser.TraitDecl
	// ImplForDecls holds all impl-for blocks for the emitter.
	ImplForDecls []*parser.ImplForDecl
	// Consts maps top-level constant names to their resolved types.
	Consts map[string]Type
	// ConstDecls holds all const declarations for the emitter.
	ConstDecls []*parser.ConstDecl
	// Warnings holds all non-fatal diagnostics (unused variables, shadowing, etc.).
	Warnings []Warning
	// CapabilityDecls holds all `cap Name` declarations for the C emitter.
	CapabilityDecls []*parser.CapabilityDecl
	// Spawns holds all spawn expressions in declaration order.
	Spawns []*SpawnInfo
	// TaskJoins maps a CallExpr (task.join()) to the inner type T of task<T>.
	TaskJoins map[parser.Expr]Type
}

// Check type-checks a fully parsed File and returns a Result.
// Module declarations and use statements in the file are parsed but not
// enforced. Use CheckProgram for multi-file programs with enforcement.
func Check(file *parser.File) (*Result, error) {
	c := &checker{
		exprTypes:       make(map[parser.Expr]Type),
		fnSigs:          make(map[string]*FnType),
		structs:         make(map[string]*StructType),
		enums:           make(map[string]*EnumType),
		fnEffects:       make(map[string]*parser.EffectsAnnotation),
		fnDecls:         make(map[string]*parser.FnDecl),
		externFns:       make(map[string]bool),
		comptimeVals:    make(map[parser.Expr]interface{}),
		genericFns:      make(map[string]*parser.FnDecl),
		genInstances:    make(map[string]*GenericInstance),
		callSiteGeneric: make(map[parser.Expr]*GenericInstance),
		methods:         make(map[string]map[string]*FnType),
		methodCalls:     make(map[parser.Expr]string),
		consts:          make(map[string]Type),
		traits:          make(map[string]*TraitDef),
		traitImpls:      make(map[string]map[string]bool),
		capabilities:    make(map[string]bool),
		taskJoins:       make(map[parser.Expr]Type),
		// symModule intentionally nil — disables module enforcement
	}
	if err := c.checkFile(file); err != nil {
		return nil, err
	}
	runComptimePass(c, []*parser.File{file})
	if len(c.comptimeErrs) > 0 {
		return nil, multiError(c.comptimeErrs)
	}
	return &Result{
		ExprTypes:        c.exprTypes,
		FnSigs:           c.fnSigs,
		Structs:          c.structs,
		Enums:            c.enums,
		FnEffects:        c.fnEffects,
		ComptimeValues:   c.comptimeVals,
		Lambdas:          c.lambdas,
		ExternFns:        c.externFns,
		GenericInstances: c.genInstanceList,
		CallSiteGeneric:  c.callSiteGeneric,
		MethodCalls:      c.methodCalls,
		ImplDecls:        c.implDecls,
		TraitDecls:       c.traitDecls,
		ImplForDecls:     c.implForDecls,
		Consts:           c.consts,
		ConstDecls:       c.constDecls,
		Warnings:         c.warnings,
		CapabilityDecls:  c.capDecls,
		Spawns:           c.spawns,
		TaskJoins:        c.taskJoins,
	}, nil
}

// CheckProgram type-checks a multi-file program with module enforcement.
// Each file may declare a module with `module name`. Top-level names from
// other modules are only accessible in files that import them with `use module::Name`.
// Files with no module declaration share the root namespace (always accessible).
func CheckProgram(files []*parser.File) (*Result, error) {
	c := &checker{
		exprTypes:       make(map[parser.Expr]Type),
		fnSigs:          make(map[string]*FnType),
		structs:         make(map[string]*StructType),
		enums:           make(map[string]*EnumType),
		fnEffects:       make(map[string]*parser.EffectsAnnotation),
		fnDecls:         make(map[string]*parser.FnDecl),
		externFns:       make(map[string]bool),
		comptimeVals:    make(map[parser.Expr]interface{}),
		genericFns:      make(map[string]*parser.FnDecl),
		genInstances:    make(map[string]*GenericInstance),
		callSiteGeneric: make(map[parser.Expr]*GenericInstance),
		methods:         make(map[string]map[string]*FnType),
		methodCalls:     make(map[parser.Expr]string),
		consts:          make(map[string]Type),
		traits:          make(map[string]*TraitDef),
		traitImpls:      make(map[string]map[string]bool),
		capabilities:    make(map[string]bool),
		taskJoins:       make(map[parser.Expr]Type),
		symModule:       make(map[string]string), // non-nil enables enforcement
	}
	if err := c.checkProgram(files); err != nil {
		return nil, err
	}
	runComptimePass(c, files)
	if len(c.comptimeErrs) > 0 {
		return nil, multiError(c.comptimeErrs)
	}
	return &Result{
		ExprTypes:        c.exprTypes,
		FnSigs:           c.fnSigs,
		Structs:          c.structs,
		Enums:            c.enums,
		FnEffects:        c.fnEffects,
		ComptimeValues:   c.comptimeVals,
		Lambdas:          c.lambdas,
		ExternFns:        c.externFns,
		GenericInstances: c.genInstanceList,
		CallSiteGeneric:  c.callSiteGeneric,
		MethodCalls:      c.methodCalls,
		ImplDecls:        c.implDecls,
		TraitDecls:       c.traitDecls,
		ImplForDecls:     c.implForDecls,
		Consts:           c.consts,
		ConstDecls:       c.constDecls,
		Warnings:         c.warnings,
		CapabilityDecls:  c.capDecls,
		Spawns:           c.spawns,
		TaskJoins:        c.taskJoins,
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

	// Pass 1a: Pre-register struct and enum types with their names so they can be
	// referenced mutually in their own fields.
	for _, f := range files {
		mod := fileModule[f]
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *parser.StructDecl:
				key, cName := qualKey(mod, decl.Name.Lexeme)
				st := &StructType{Name: cName, Fields: make(map[string]Type)}
				c.structs[key] = st
				if _, exists := c.symModule[decl.Name.Lexeme]; !exists {
					c.symModule[decl.Name.Lexeme] = mod
				}
			case *parser.EnumDecl:
				key, cName := qualKey(mod, decl.Name.Lexeme)
				et := &EnumType{Name: cName, ByName: make(map[string]*EnumVariantDef)}
				c.enums[key] = et
				if _, exists := c.symModule[decl.Name.Lexeme]; !exists {
					c.symModule[decl.Name.Lexeme] = mod
				}
			}
		}
	}

	// Pass 1b: collect all fn signatures, resolve struct/enum fields.
	for _, f := range files {
		mod := fileModule[f]
		c.currentModule = mod // needed for module-aware lookups inside buildStructTypeFields etc.
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *parser.FnDecl:
				if len(decl.TypeParams) > 0 {
					// Generic function — defer until call-site monomorphization.
					c.genericFns[decl.Name.Lexeme] = decl
					if c.symModule != nil {
						if _, exists := c.symModule[decl.Name.Lexeme]; !exists {
							c.symModule[decl.Name.Lexeme] = mod
						}
					}
				} else {
					sig, err := c.buildFnSig(decl)
					if err != nil {
						return err
					}
					c.fnSigs[decl.Name.Lexeme] = sig
					if c.symModule != nil {
						if _, exists := c.symModule[decl.Name.Lexeme]; !exists {
							c.symModule[decl.Name.Lexeme] = mod
						}
					}
					c.fnDecls[decl.Name.Lexeme] = decl
					if decl.Effects != nil {
						c.fnEffects[decl.Name.Lexeme] = decl.Effects
					}
				}
			case *parser.StructDecl:
				if err := c.buildStructTypeFields(decl); err != nil {
					return err
				}
			case *parser.EnumDecl:
				if err := c.buildEnumTypeFields(decl); err != nil {
					return err
				}
			case *parser.ExternFnDecl:
				params := make([]Type, len(decl.Params))
				for i, p := range decl.Params {
					t, err := c.resolveTypeExpr(p.Type)
					if err != nil {
						return err
					}
					params[i] = t
				}
				ret, err := c.resolveTypeExpr(decl.RetType)
				if err != nil {
					return err
				}
				c.fnSigs[decl.Name.Lexeme] = &FnType{Params: params, Ret: ret}
				if _, exists := c.symModule[decl.Name.Lexeme]; !exists {
					c.symModule[decl.Name.Lexeme] = mod
				}
				c.fnEffects[decl.Name.Lexeme] = decl.Effects
				c.externFns[decl.Name.Lexeme] = true
			case *parser.ConstDecl:
				if err := c.checkConstDecl(decl); err != nil {
					return err
				}
			case *parser.ImplDecl:
				if err := c.collectImplDecl(decl); err != nil {
					return err
				}
			}
		}
	}

	// Use-validation pass: verify each UseDecl references a real module and name.
	// Build per-file import tables.
	fileUses := make(map[*parser.File]map[string]string)
	fileWildcardUses := make(map[*parser.File]map[string]bool)
	for _, f := range files {
		uses := make(map[string]string)
		wildcards := make(map[string]bool)
		for _, d := range f.Decls {
			ud, ok := d.(*parser.UseDecl)
			if !ok {
				continue
			}
			if len(ud.Path) == 1 {
				// `use module` — wildcard, but only valid if `module` is a known module name.
				modName := ud.Path[0].Lexeme
				known := false
				for _, m := range c.symModule {
					if m == modName {
						known = true
						break
					}
				}
				if !known {
					return &Error{Tok: ud.UseTok,
						Msg: fmt.Sprintf("use %q: must have the form 'module::Name'; no module named %q found", modName, modName)}
				}
				wildcards[modName] = true
				continue
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
		fileWildcardUses[f] = wildcards
	}

	// Pass 2: type-check function bodies with per-file module context.
	for _, f := range files {
		c.currentModule = fileModule[f]
		c.currentUses = fileUses[f]
		c.currentWildcardUses = fileWildcardUses[f]
		c.errs = nil
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *parser.FnDecl:
				if len(decl.TypeParams) == 0 {
					if err := c.checkFnDecl(decl); err != nil {
						c.errs = append(c.errs, err)
					}
				}
			case *parser.ImplDecl:
				for _, m := range decl.Methods {
					if len(m.TypeParams) == 0 {
						mangledName := decl.TypeName.Lexeme + "_" + m.Name.Lexeme
						if err := c.checkMethodBody(mangledName, m); err != nil {
							c.errs = append(c.errs, err)
						}
					}
				}
			}
		}
		if len(c.errs) > 0 {
			return multiError(c.errs)
		}
	}
	return nil
}

// qualKey returns the map key and C name for a type in a given module.
// For root-namespace types (mod == ""), both are just the plain name.
// For module types, the key is "mod.name" and the C name is "mod_name".
func qualKey(mod, name string) (key, cName string) {
	if mod == "" {
		return name, name
	}
	return mod + "." + name, mod + "_" + name
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
	// Cross-module: require an explicit use import or whole-module wildcard.
	if c.currentWildcardUses != nil && c.currentWildcardUses[symMod] {
		return nil
	}
	return &Error{Tok: tok,
		Msg: fmt.Sprintf("%q is from module %q; add 'use %s::%s'", name, symMod, symMod, name)}
}

// lookupStruct finds a struct type by unqualified name using the following
// priority order:
//  1. Current module (own declarations always visible)
//  2. Explicitly imported via `use mod::Name`
//  3. Wildcard-imported via `use mod`
//  4. Root namespace (no module prefix)
//  5. Any other module (cross-module fallback so checkModuleAccess can emit
//     "is from module X" instead of the less helpful "undefined struct")
func (c *checker) lookupStruct(name string) (*StructType, bool) {
	if c.currentModule != "" {
		if st, ok := c.structs[c.currentModule+"."+name]; ok {
			return st, true
		}
	}
	if c.currentUses != nil {
		if importedMod, ok := c.currentUses[name]; ok {
			if st, ok := c.structs[importedMod+"."+name]; ok {
				return st, true
			}
		}
	}
	if c.currentWildcardUses != nil {
		for modName := range c.currentWildcardUses {
			if st, ok := c.structs[modName+"."+name]; ok {
				return st, true
			}
		}
	}
	if st, ok := c.structs[name]; ok {
		return st, true
	}
	// Cross-module fallback: find in any module so checkModuleAccess can
	// produce "is from module X" rather than "undefined struct".
	if symMod, known := c.symModule[name]; known && symMod != "" {
		if st, ok := c.structs[symMod+"."+name]; ok {
			return st, true
		}
	}
	return nil, false
}

// lookupEnum finds an enum type by unqualified name using the same priority
// order as lookupStruct.
func (c *checker) lookupEnum(name string) (*EnumType, bool) {
	if c.currentModule != "" {
		if et, ok := c.enums[c.currentModule+"."+name]; ok {
			return et, true
		}
	}
	if c.currentUses != nil {
		if importedMod, ok := c.currentUses[name]; ok {
			if et, ok := c.enums[importedMod+"."+name]; ok {
				return et, true
			}
		}
	}
	if c.currentWildcardUses != nil {
		for modName := range c.currentWildcardUses {
			if et, ok := c.enums[modName+"."+name]; ok {
				return et, true
			}
		}
	}
	if et, ok := c.enums[name]; ok {
		return et, true
	}
	if symMod, known := c.symModule[name]; known && symMod != "" {
		if et, ok := c.enums[symMod+"."+name]; ok {
			return et, true
		}
	}
	return nil, false
}

// ── Internal checker state ────────────────────────────────────────────────────

// GenericInstance is a single concrete monomorphization of a generic function.
type GenericInstance struct {
	FnName      string            // original Candor name, e.g. "identity"
	MangledName string            // C name, e.g. "identity__int64_t"
	Sig         *FnType           // concrete resolved signature
	Node        *parser.FnDecl    // AST node (for body emission)
	Subst       map[string]Type   // type param name → concrete type
}

type checker struct {
	exprTypes    map[parser.Expr]Type
	fnSigs       map[string]*FnType
	structs      map[string]*StructType
	enums        map[string]*EnumType
	fnEffects    map[string]*parser.EffectsAnnotation // collected effects annotations
	fnDecls      map[string]*parser.FnDecl             // function bodies for comptime eval
	externFns    map[string]bool                       // extern fn names
	comptimeVals  map[parser.Expr]interface{}           // comptime-evaluated call results
	comptimeErrs  []error                               // compile-time contract violations
	curEffects   *parser.EffectsAnnotation             // effects of fn currently being checked
	curRetType   Type                                  // return type of fn currently being checked
	errs         []error                               // collected statement-level errors
	warnings     []Warning                             // collected non-fatal diagnostics
	letUsages    []*varUsage                           // let bindings in current function (reset per fn)

	// impl / method support
	methods     map[string]map[string]*FnType   // structName → methodName → sig
	methodCalls map[parser.Expr]string           // CallExpr → mangled C name
	implDecls   []*parser.ImplDecl               // all impl blocks (for emitter)

	// trait / impl-for support
	traits       map[string]*TraitDef          // trait name → definition
	traitImpls   map[string]map[string]bool    // typeName → traitName → implemented
	traitDecls   []*parser.TraitDecl           // all trait declarations (for Result)
	implForDecls []*parser.ImplForDecl         // all impl-for blocks (for emitter)

	// const support
	consts     map[string]Type           // name → type
	constDecls []*parser.ConstDecl       // all const decls (for emitter)

	// Lambda tracking
	lambdas     []*LambdaInfo
	lambdaCount int

	// Spawn / task tracking
	spawns     []*SpawnInfo
	spawnCount int
	taskJoins  map[parser.Expr]Type

	// Contract context
	inEnsures bool // true when checking an ensures clause; enables old()

	// Generic function tracking
	genericFns      map[string]*parser.FnDecl            // name → generic fn decl
	typeVarSubst    map[string]Type                       // active subst during monomorphization
	genInstances    map[string]*GenericInstance           // mangledName → instance (dedup)
	genInstanceList []*GenericInstance                    // ordered for emission
	callSiteGeneric map[parser.Expr]*GenericInstance      // e.Fn → instance for call sites

	// Module enforcement — nil in single-file (Check) mode, populated by CheckProgram.
	symModule            map[string]string // top-level name → declaring module ("" = root/no module)
	currentModule        string            // module of the file currently being checked
	currentUses          map[string]string // imported name → source module (for current file)
	currentWildcardUses  map[string]bool   // module names imported wholesale via `use module`

	// Capability system
	capabilities map[string]bool            // names declared with `cap Name`
	capDecls     []*parser.CapabilityDecl   // for the emitter
}

// scope is a linked chain of variable bindings.
type varInfo struct {
	typ     Type
	mutable bool
	usage   *varUsage // non-nil for let bindings; nil for params and built-ins
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

// defineTracked is like define/defineMut but attaches a usage tracker for unused-variable warnings.
func (s *scope) defineTracked(name string, t Type, mutable bool, u *varUsage) {
	s.vars[name] = varInfo{typ: t, mutable: mutable, usage: u}
}

// allNames returns every name visible in this scope chain (deduplicated, innermost wins).
func (s *scope) allNames() []string {
	seen := make(map[string]bool)
	var names []string
	cur := s
	for cur != nil {
		for n := range cur.vars {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
		cur = cur.parent
	}
	return names
}

func (c *checker) record(expr parser.Expr, t Type) Type {
	c.exprTypes[expr] = t
	return t
}

func (c *checker) errorf(tok lexer.Token, format string, args ...any) error {
	return &Error{Tok: tok, Msg: fmt.Sprintf(format, args...)}
}

func (c *checker) warnf(tok lexer.Token, format string, args ...any) {
	c.warnings = append(c.warnings, Warning{Tok: tok, Msg: fmt.Sprintf(format, args...)})
}

// beginFnTracking resets the per-function let-usage list.
func (c *checker) beginFnTracking() {
	c.letUsages = nil
}

// flushUnusedWarnings emits unused-variable warnings for the current function and clears the list.
func (c *checker) flushUnusedWarnings() {
	for _, u := range c.letUsages {
		if u.uses == 0 {
			c.warnf(u.tok, "unused variable %q; prefix with '_' to suppress", u.name)
		}
	}
	c.letUsages = nil
}

// levenshtein returns the edit distance between two strings.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	m, n := len(ra), len(rb)
	prev := make([]int, n+1)
	curr := make([]int, n+1)
	for j := 0; j <= n; j++ {
		prev[j] = j
	}
	for i := 1; i <= m; i++ {
		curr[0] = i
		for j := 1; j <= n; j++ {
			if ra[i-1] == rb[j-1] {
				curr[j] = prev[j-1]
			} else {
				mn := prev[j]
				if prev[j-1] < mn {
					mn = prev[j-1]
				}
				if curr[j-1] < mn {
					mn = curr[j-1]
				}
				curr[j] = 1 + mn
			}
		}
		prev, curr = curr, prev
	}
	return prev[n]
}

// didYouMean returns a "did you mean X?" suggestion for name if a close candidate exists.
func didYouMean(name string, candidates []string) string {
	if len(name) < 2 {
		return ""
	}
	best := ""
	bestDist := 3 // max edit distance to suggest
	for _, c := range candidates {
		if c == name {
			continue
		}
		d := levenshtein(name, c)
		if d < bestDist {
			bestDist = d
			best = c
		}
	}
	if best != "" {
		return fmt.Sprintf("did you mean %q?", best)
	}
	return ""
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
	"str_len":         {Params: []Type{TStr}, Ret: TI64},
	"str_concat":      {Params: []Type{TStr, TStr}, Ret: TStr},
	"str_eq":          {Params: []Type{TStr, TStr}, Ret: TBool},
	"str_starts_with": {Params: []Type{TStr, TStr}, Ret: TBool},
	"str_find":        {Params: []Type{TStr, TStr, TI64}, Ret: &GenType{Con: "option", Params: []Type{TI64}}},
	"str_byte":        {Params: []Type{TStr, TI64}, Ret: TU8},
	"str_from_u8":     {Params: []Type{TU8}, Ret: TStr},
	"str_substr":      {Params: []Type{TStr, TI64, TI64}, Ret: TStr},
	"int_to_str": {Params: []Type{TI64}, Ret: TStr},
	"str_to_int": {Params: []Type{TStr}, Ret: &GenType{Con: "result", Params: []Type{TI64, TStr}}},
	// File I/O — result<str, str> on error
	"read_file":   {Params: []Type{TStr}, Ret: &GenType{Con: "result", Params: []Type{TStr, TStr}}},
	"write_file":  {Params: []Type{TStr, TStr}, Ret: &GenType{Con: "result", Params: []Type{TUnit, TStr}}},
	"append_file": {Params: []Type{TStr, TStr}, Ret: &GenType{Con: "result", Params: []Type{TUnit, TStr}}},
	"print_char":  {Params: []Type{TU8}, Ret: TUnit},

	// M2.1 std::math
	"math_abs_i64":   {Params: []Type{TI64}, Ret: TI64},
	"math_abs_f64":   {Params: []Type{TF64}, Ret: TF64},
	"math_sqrt":      {Params: []Type{TF64}, Ret: TF64},
	"math_pow":       {Params: []Type{TF64, TF64}, Ret: TF64},
	"math_floor":     {Params: []Type{TF64}, Ret: TF64},
	"math_ceil":      {Params: []Type{TF64}, Ret: TF64},
	"math_sin":       {Params: []Type{TF64}, Ret: TF64},
	"math_cos":       {Params: []Type{TF64}, Ret: TF64},
	"math_min_i64":   {Params: []Type{TI64, TI64}, Ret: TI64},
	"math_max_i64":   {Params: []Type{TI64, TI64}, Ret: TI64},
	"math_min_f64":   {Params: []Type{TF64, TF64}, Ret: TF64},
	"math_max_f64":   {Params: []Type{TF64, TF64}, Ret: TF64},
	"math_clamp_i64": {Params: []Type{TI64, TI64, TI64}, Ret: TI64},
	"math_clamp_f64": {Params: []Type{TF64, TF64, TF64}, Ret: TF64},

	// M2.2 std::str
	"str_repeat":   {Params: []Type{TStr, TI64}, Ret: TStr},
	"str_trim":     {Params: []Type{TStr}, Ret: TStr},
	"str_split":    {Params: []Type{TStr, TStr}, Ret: &GenType{Con: "vec", Params: []Type{TStr}}},
	"str_replace":  {Params: []Type{TStr, TStr, TStr}, Ret: TStr},
	"str_to_upper": {Params: []Type{TStr}, Ret: TStr},
	"str_to_lower": {Params: []Type{TStr}, Ret: TStr},
	"str_contains": {Params: []Type{TStr, TStr}, Ret: TBool},

	// M2.3 std::io
	"print_err":      {Params: []Type{TStr}, Ret: TUnit},
	"read_all_lines": {Params: []Type{}, Ret: &GenType{Con: "vec", Params: []Type{TStr}}},
	"read_csv_line":  {Params: []Type{}, Ret: &GenType{Con: "vec", Params: []Type{TStr}}},
	"flush_stdout":   {Params: []Type{}, Ret: TUnit},

	// M2.4 std::os
	"os_args":   {Params: []Type{}, Ret: &GenType{Con: "vec", Params: []Type{TStr}}},
	"os_getenv": {Params: []Type{TStr}, Ret: &GenType{Con: "option", Params: []Type{TStr}}},
	"os_exit":   {Params: []Type{TI64}, Ret: TUnit},
	"os_cwd":    {Params: []Type{}, Ret: TStr},
	"os_exec":   {Params: []Type{&GenType{Con: "vec", Params: []Type{TStr}}}, Ret: &GenType{Con: "result", Params: []Type{TI64, TStr}}},

	// M2.5 std::time
	"time_now_ms":      {Params: []Type{}, Ret: TI64},
	"time_now_mono_ns": {Params: []Type{}, Ret: TI64},
	"time_sleep_ms":    {Params: []Type{TI64}, Ret: TUnit},

	// M2.6 std::rand
	"rand_u64":      {Params: []Type{}, Ret: TU64},
	"rand_f64":      {Params: []Type{}, Ret: TF64},
	"rand_range":    {Params: []Type{TI64, TI64}, Ret: TI64},
	"rand_set_seed": {Params: []Type{TU64}, Ret: TUnit},

	// M2.7 std::path
	"path_join":     {Params: []Type{TStr, TStr}, Ret: TStr},
	"path_dir":      {Params: []Type{TStr}, Ret: TStr},
	"path_filename": {Params: []Type{TStr}, Ret: TStr},
	"path_ext":      {Params: []Type{TStr}, Ret: TStr},
	"path_exists":   {Params: []Type{TStr}, Ret: TBool},
	"path_list_dir": {Params: []Type{TStr}, Ret: &GenType{Con: "result", Params: []Type{&GenType{Con: "vec", Params: []Type{TStr}}, TStr}}},
	"path_mkdir":    {Params: []Type{TStr}, Ret: &GenType{Con: "result", Params: []Type{TUnit, TStr}}},
	"path_remove":   {Params: []Type{TStr}, Ret: &GenType{Con: "result", Params: []Type{TUnit, TStr}}},
}

// KnownEffects is the canonical set of recognized effect names.
// Using an unrecognized name produces a warning (typo guard).
// New effect tiers should be added here alongside documentation.
var KnownEffects = map[string]bool{
	// I/O and system
	"io":      true, // file, stdin/stdout, network sockets
	"sys":     true, // process control, environment, exit
	"time":    true, // wall clock, monotonic clock, sleep
	"rand":    true, // random number generation
	// Async / concurrency (M10)
	"async":   true, // suspendable / coroutine-style execution
	// Hardware tiers (M10.3) — motivated by disaggregated inference stacks
	"gpu":     true, // CUDA/VRAM access — prefill/decode compute workers
	"net":     true, // NIXL / InfiniBand / RoCE / NVLink transfers
	"storage": true, // SSD / object store (S3, VAST) — KV cache spill
	"mem":     true, // CPU RAM management — KV block manager, eviction logic
	// SIMD / vector compute (M11.3)
	"simd":    true, // SIMD width-dependent ops — vec_dot, vec_l2, tensor_matmul
}

// warnUnknownEffects emits a warning for any effect name not in KnownEffects.
func (c *checker) warnUnknownEffects(ann *parser.EffectsAnnotation, tok lexer.Token) {
	if ann == nil || ann.Kind != parser.EffectsDecl {
		return
	}
	for _, name := range ann.Names {
		if !KnownEffects[name] {
			c.warnf(tok, "unknown effect %q; known effects: io, sys, time, rand, async, gpu, net, storage, mem, simd", name)
		}
	}
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
	"print_char":  {Kind: parser.EffectsDecl, Names: []string{"io"}},
	// M2.2 str (pure operations — no effect annotation needed)
	// M2.3 io
	"print_err":      {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"read_all_lines": {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"read_csv_line":  {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"flush_stdout":   {Kind: parser.EffectsDecl, Names: []string{"io"}},
	// M2.4 os
	"os_args":   {Kind: parser.EffectsDecl, Names: []string{"sys"}},
	"os_getenv": {Kind: parser.EffectsDecl, Names: []string{"sys"}},
	"os_exit":   {Kind: parser.EffectsDecl, Names: []string{"sys"}},
	"os_cwd":    {Kind: parser.EffectsDecl, Names: []string{"sys"}},
	"os_exec":   {Kind: parser.EffectsDecl, Names: []string{"sys"}},
	// M2.5 time
	"time_now_ms":      {Kind: parser.EffectsDecl, Names: []string{"time"}},
	"time_now_mono_ns": {Kind: parser.EffectsDecl, Names: []string{"time"}},
	"time_sleep_ms":    {Kind: parser.EffectsDecl, Names: []string{"time"}},
	// M2.6 rand
	"rand_u64":      {Kind: parser.EffectsDecl, Names: []string{"rand"}},
	"rand_f64":      {Kind: parser.EffectsDecl, Names: []string{"rand"}},
	"rand_range":    {Kind: parser.EffectsDecl, Names: []string{"rand"}},
	"rand_set_seed": {Kind: parser.EffectsDecl, Names: []string{"rand"}},
	// M2.7 path
	"path_list_dir": {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"path_mkdir":    {Kind: parser.EffectsDecl, Names: []string{"io"}},
	"path_remove":   {Kind: parser.EffectsDecl, Names: []string{"io"}},
}

func (c *checker) checkFile(file *parser.File) error {
	// Inject built-in signatures and their known effects.
	for name, sig := range Builtins {
		c.fnSigs[name] = sig
	}
	for name, ann := range BuiltinEffects {
		c.fnEffects[name] = ann
	}
	// Pass 1a: Pre-register struct, enum types, trait definitions, and capabilities.
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *parser.StructDecl:
			c.structs[d.Name.Lexeme] = &StructType{Name: d.Name.Lexeme, Fields: make(map[string]Type)}
		case *parser.EnumDecl:
			c.enums[d.Name.Lexeme] = &EnumType{Name: d.Name.Lexeme, ByName: make(map[string]*EnumVariantDef)}
		case *parser.TraitDecl:
			if err := c.collectTraitDecl(d); err != nil {
				return err
			}
		case *parser.CapabilityDecl:
			c.capabilities[d.Name.Lexeme] = true
			c.capDecls = append(c.capDecls, d)
		}
	}

	// Pass 1b: collect function signatures, resolve struct types, and effects annotations.
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *parser.ModuleDecl, *parser.UseDecl:
			_ = d // syntax accepted; enforcement is a future feature
		case *parser.FnDecl:
			if len(d.TypeParams) > 0 {
				c.genericFns[d.Name.Lexeme] = d
			} else {
				sig, err := c.buildFnSig(d)
				if err != nil {
					return err
				}
				c.fnSigs[d.Name.Lexeme] = sig
				c.fnDecls[d.Name.Lexeme] = d
				if d.Effects != nil {
					c.fnEffects[d.Name.Lexeme] = d.Effects
					c.warnUnknownEffects(d.Effects, d.Name)
				}
			}
		case *parser.StructDecl:
			if err := c.buildStructTypeFields(d); err != nil {
				return err
			}
		case *parser.EnumDecl:
			if err := c.buildEnumTypeFields(d); err != nil {
				return err
			}
		case *parser.ExternFnDecl:
			params := make([]Type, len(d.Params))
			for i, p := range d.Params {
				t, err := c.resolveTypeExpr(p.Type)
				if err != nil {
					return err
				}
				params[i] = t
			}
			ret, err := c.resolveTypeExpr(d.RetType)
			if err != nil {
				return err
			}
			c.fnSigs[d.Name.Lexeme] = &FnType{Params: params, Ret: ret}
			if d.Effects != nil {
				c.fnEffects[d.Name.Lexeme] = d.Effects
				c.warnUnknownEffects(d.Effects, d.Name)
			}
			c.externFns[d.Name.Lexeme] = true
		case *parser.ConstDecl:
			if err := c.checkConstDecl(d); err != nil {
				return err
			}
		case *parser.ImplDecl:
			if err := c.collectImplDecl(d); err != nil {
				return err
			}
		case *parser.ImplForDecl:
			if err := c.collectImplForDecl(d); err != nil {
				return err
			}
		}
	}
	// Pass 2: type-check function bodies and impl / impl-for method bodies.
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *parser.FnDecl:
			if len(d.TypeParams) == 0 {
				if err := c.checkFnDecl(d); err != nil {
					return err
				}
			}
		case *parser.ImplDecl:
			for _, m := range d.Methods {
				if len(m.TypeParams) == 0 {
					mangledName := d.TypeName.Lexeme + "_" + m.Name.Lexeme
					if err := c.checkMethodBody(mangledName, m); err != nil {
						return err
					}
				}
			}
		case *parser.ImplForDecl:
			for _, m := range d.Methods {
				if len(m.TypeParams) == 0 {
					mangledName := d.TypeName.Lexeme + "_" + m.Name.Lexeme
					if err := c.checkMethodBody(mangledName, m); err != nil {
						return err
					}
				}
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

func (c *checker) buildStructTypeFields(d *parser.StructDecl) error {
	st, _ := c.lookupStruct(d.Name.Lexeme)
	for _, f := range d.Fields {
		t, err := c.resolveTypeExpr(f.Type)
		if err != nil {
			return err
		}
		st.Fields[f.Name.Lexeme] = t
	}
	return nil
}

func (c *checker) buildEnumTypeFields(d *parser.EnumDecl) error {
	et, _ := c.lookupEnum(d.Name.Lexeme)
	for i, v := range d.Variants {
		fields := make([]Type, len(v.Fields))
		for j, f := range v.Fields {
			t, err := c.resolveTypeExpr(f)
			if err != nil {
				return err
			}
			fields[j] = t
		}
		vd := &EnumVariantDef{Name: v.Name.Lexeme, Fields: fields, Tag: i}
		et.Variants = append(et.Variants, vd)
		et.ByName[v.Name.Lexeme] = vd
	}
	return nil
}

func (c *checker) resolveTypeExpr(te parser.TypeExpr) (Type, error) {
	switch t := te.(type) {
	case *parser.NamedType:
		// Module-qualified reference: parsed.Expr — look up directly, no access check needed.
		if t.Module != nil {
			qualK := t.Module.Lexeme + "." + t.Name.Lexeme
			if st, ok := c.structs[qualK]; ok {
				return st, nil
			}
			if et, ok := c.enums[qualK]; ok {
				return et, nil
			}
			return nil, c.errorf(t.Name, "unknown type %q.%q", t.Module.Lexeme, t.Name.Lexeme)
		}
		name := t.Name.Lexeme
		// Check active type-variable substitution (during monomorphization).
		if c.typeVarSubst != nil {
			if subst, ok := c.typeVarSubst[name]; ok {
				return subst, nil
			}
		}
		if builtin, ok := BuiltinTypes[name]; ok {
			return builtin, nil
		}
		// Prefer current module's version of the type.
		if st, ok := c.lookupStruct(name); ok {
			if err := c.checkModuleAccess(name, t.Name); err != nil {
				return nil, err
			}
			return st, nil
		}
		if et, ok := c.lookupEnum(name); ok {
			return et, nil
		}
		return nil, c.errorf(t.Name, "unknown type %q", name)

	case *parser.GenericType:
		// Special case: cap<Name> — Name is a capability, not a regular type.
		if t.Name.Lexeme == "cap" && len(t.Params) == 1 {
			if named, ok := t.Params[0].(*parser.NamedType); ok {
				capName := named.Name.Lexeme
				if !c.capabilities[capName] {
					return nil, c.errorf(named.Name,
						"unknown capability %q; declare it with `cap %s`", capName, capName)
				}
				return &GenType{Con: "cap", Params: []Type{&CapabilityType{Name: capName}}}, nil
			}
		}
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

	case *parser.TupleTypeExpr:
		elems := make([]Type, len(t.Elems))
		for i, te2 := range t.Elems {
			resolved, err := c.resolveTypeExpr(te2)
			if err != nil {
				return nil, err
			}
			elems[i] = resolved
		}
		return &TupleType{Elems: elems}, nil

	default:
		return nil, fmt.Errorf("unhandled TypeExpr %T", te)
	}
}

// ── Pass 2: check function bodies ────────────────────────────────────────────

func (c *checker) checkFnDecl(d *parser.FnDecl) error {
	c.beginFnTracking()
	sig := c.fnSigs[d.Name.Lexeme]
	sc := newScope(nil)
	for i, p := range d.Params {
		sc.define(p.Name.Lexeme, sig.Params[i])
	}
	// Set the current function's effects and return type for body checking.
	prev := c.curEffects
	prevRet := c.curRetType
	c.curEffects = c.fnEffects[d.Name.Lexeme] // nil if unannotated
	c.curRetType = sig.Ret

	// Type-check contract clauses.
	retType := sig.Ret
	for _, cc := range d.Contracts {
		clauseSc := newScope(sc)
		if cc.Kind == parser.ContractEnsures {
			clauseSc.define("result", retType)
			c.inEnsures = true
		}
		condType, err := c.checkExpr(cc.Expr, clauseSc, TBool)
		c.inEnsures = false
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
	c.flushUnusedWarnings()
	c.curEffects = prev
	c.curRetType = prevRet
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
	case *parser.WhileStmt:
		condType, err := c.checkExpr(s.Cond, sc, TBool)
		if err != nil {
			return err
		}
		if !condType.Equals(TBool) {
			return c.errorf(s.WhileTok, "while condition must be bool, got %s", condType)
		}
		return c.checkBlock(s.Body, sc, retType)
	case *parser.ForStmt:
		return c.checkForStmt(s, sc, retType)
	case *parser.BreakStmt:
		return nil
	case *parser.ContinueStmt:
		return nil
	case *parser.BlockStmt:
		return c.checkBlock(s, sc, retType)
	case *parser.TupleDestructureStmt:
		return c.checkTupleDestructureStmt(s, sc)
	case *parser.AssignStmt:
		return c.checkAssignStmt(s, sc)
	case *parser.FieldAssignStmt:
		return c.checkFieldAssignStmt(s, sc)
	case *parser.IndexAssignStmt:
		return c.checkIndexAssignStmt(s, sc)
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

func (c *checker) checkIndexAssignStmt(s *parser.IndexAssignStmt, sc *scope) error {
	collType, err := c.checkExpr(s.Target.Collection, sc, nil)
	if err != nil {
		return err
	}
	gen, ok := collType.(*GenType)
	if !ok || len(gen.Params) == 0 {
		return c.errorf(s.Target.Pos(), "index assignment requires vec<T>, ring<T>, or map<K,V>, got %s", collType)
	}

	switch gen.Con {
	case "map":
		// m[k] = v — desugars to map_insert(&m, k, v)
		if len(gen.Params) != 2 {
			return c.errorf(s.Target.Pos(), "invalid map type %s", collType)
		}
		keyType, valType := gen.Params[0], gen.Params[1]
		idxType, err := c.checkExpr(s.Target.Index, sc, keyType)
		if err != nil {
			return err
		}
		if _, ok2 := Coerce(idxType, keyType); !ok2 {
			return c.errorf(s.Target.Index.Pos(), "map key type mismatch: got %s, expected %s", idxType, keyType)
		}
		rhs, err := c.checkExpr(s.Value, sc, valType)
		if err != nil {
			return err
		}
		coerced, ok2 := Coerce(rhs, valType)
		if !ok2 {
			return c.errorf(s.Value.Pos(), "cannot assign %s to map value of type %s", rhs, valType)
		}
		c.exprTypes[s.Value] = coerced
		return nil

	case "vec", "ring":
		elemType := gen.Params[0]
		idxType, err := c.checkExpr(s.Target.Index, sc, TI64)
		if err != nil {
			return err
		}
		if !IsIntType(idxType) {
			return c.errorf(s.Target.Index.Pos(), "%s index must be an integer, got %s", gen.Con, idxType)
		}
		valType, err := c.checkExpr(s.Value, sc, elemType)
		if err != nil {
			return err
		}
		coerced, ok2 := Coerce(valType, elemType)
		if !ok2 {
			return c.errorf(s.Value.Pos(), "cannot assign %s to %s element of type %s", valType, gen.Con, elemType)
		}
		c.exprTypes[s.Value] = coerced
		return nil

	default:
		return c.errorf(s.Target.Pos(), "index assignment requires vec<T>, ring<T>, or map<K,V>, got %s", collType)
	}
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

	name := s.Name.Lexeme
	// Shadow warning: if the name already exists in any enclosing scope, warn.
	if name != "_" && !strings.HasPrefix(name, "_") {
		if _, ok := sc.lookup(name); ok {
			c.warnf(s.Name, "variable %q shadows a binding in an outer scope", name)
		}
	}

	// Track for unused-variable warnings (skip wildcard and _-prefixed names).
	var u *varUsage
	if name != "_" && !strings.HasPrefix(name, "_") {
		u = &varUsage{tok: s.Name, name: name}
		c.letUsages = append(c.letUsages, u)
	}

	sc.defineTracked(name, valType, s.Mut, u)
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
			case lexer.TokSecret:
				return c.inferSecretCall(e, sc)
			case lexer.TokReveal:
				return c.inferRevealCall(e, sc)
			}
			switch ident.Tok.Lexeme {
			case "vec_new":
				return c.inferVecNew(e, ident, sc, hint)
			case "vec_push":
				return c.inferVecPush(e, ident, sc)
			case "vec_len":
				return c.inferVecLen(e, ident, sc)
			case "vec_pop":
				return c.inferVecPop(e, ident, sc)
			case "map_new":
				return c.inferMapNew(e, ident, sc, hint)
			case "map_insert":
				return c.inferMapInsert(e, ident, sc)
			case "map_get":
				return c.inferMapGet(e, ident, sc)
			case "map_remove":
				return c.inferMapRemove(e, ident, sc)
			case "map_len":
				return c.inferMapLen(e, ident, sc)
			case "map_contains":
				return c.inferMapContains(e, ident, sc)
			case "set_new":
				return c.inferSetNew(e, ident, sc, hint)
			case "set_add":
				return c.inferSetAdd(e, ident, sc)
			case "set_remove":
				return c.inferSetRemove(e, ident, sc)
			case "set_contains":
				return c.inferSetContains(e, ident, sc)
			case "set_len":
				return c.inferSetLen(e, ident, sc)
			case "ring_new":
				return c.inferRingNew(e, ident, sc, hint)
			case "ring_push_back":
				return c.inferRingPushBack(e, ident, sc)
			case "ring_pop_front":
				return c.inferRingPopFront(e, ident, sc)
			case "ring_len":
				return c.inferRingLen(e, ident, sc)
			case "ring_is_empty":
				return c.inferRingIsEmpty(e, ident, sc)
			case "box_new":
				return c.inferBoxNew(e, ident, sc, hint)
			case "box_deref":
				return c.inferBoxDeref(e, ident, sc)
			case "box_drop":
				return c.inferBoxDrop(e, ident, sc)
			case "arc_new":
				return c.inferArcNew(e, ident, sc, hint)
			case "arc_clone":
				return c.inferArcClone(e, ident, sc)
			case "arc_deref":
				return c.inferArcDeref(e, ident, sc)
			case "arc_drop":
				return c.inferArcDrop(e, ident, sc)
			case "tensor_zeros":
				return c.inferTensorZeros(e, ident, sc, hint)
			case "tensor_from_vec":
				return c.inferTensorFromVec(e, ident, sc, hint)
			case "tensor_to_vec":
				return c.inferTensorToVec(e, ident, sc)
			case "tensor_get":
				return c.inferTensorGet(e, ident, sc)
			case "tensor_set":
				return c.inferTensorSet(e, ident, sc)
			case "tensor_ndim":
				return c.inferTensorNdim(e, ident, sc)
			case "tensor_shape":
				return c.inferTensorShape(e, ident, sc)
			case "tensor_len":
				return c.inferTensorLen(e, ident, sc)
			case "tensor_free":
				return c.inferTensorFree(e, ident, sc)
			case "tensor_dot":
				return c.inferTensorDot(e, ident, sc)
			case "tensor_l2":
				return c.inferTensorL2(e, ident, sc)
			case "tensor_cosine":
				return c.inferTensorCosine(e, ident, sc)
			case "tensor_matmul":
				return c.inferTensorMatmul(e, ident, sc)
			case "mmap_open":
				return c.inferMmapOpen(e, ident, sc)
			case "mmap_anon":
				return c.inferMmapAnon(e, ident, sc)
			case "mmap_deref":
				return c.inferMmapDeref(e, ident, sc)
			case "mmap_flush":
				return c.inferMmapFlush(e, ident, sc)
			case "mmap_close":
				return c.inferMmapClose(e, ident, sc)
			case "mmap_len":
				return c.inferMmapLen(e, ident, sc)
			case "refmut":
				return c.inferRefmutCall(e, sc)
			}
		}
		// Method call: x.method(args) where Fn is a FieldExpr
		if fe, ok := e.Fn.(*parser.FieldExpr); ok {
			if mt, err2 := c.tryMethodCall(e, fe, sc); mt != nil || err2 != nil {
				return mt, err2
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

	case *parser.BlockExpr:
		// Multi-statement match arm block.  The block yields the value of its
		// last expression (if the last statement is a bare ExprStmt), otherwise unit.
		inner := newScope(sc)
		stmts := e.Stmts
		// Check all but the last statement first (no hint propagation needed).
		for _, stmt := range stmts[:max(0, len(stmts)-1)] {
			if err := c.checkStmt(stmt, inner, c.curRetType); err != nil {
				return nil, err
			}
		}
		var blockType Type = TUnit
		if len(stmts) > 0 {
			last := stmts[len(stmts)-1]
			if es, ok := last.(*parser.ExprStmt); ok {
				// Propagate the hint so result<T,E> arms unify correctly.
				t, err := c.checkExpr(es.X, inner, hint)
				if err != nil {
					return nil, err
				}
				blockType = t
			} else {
				if err := c.checkStmt(last, inner, c.curRetType); err != nil {
					return nil, err
				}
				// Return/break as last stmt — this arm is never (control never reaches after it).
				switch last.(type) {
				case *parser.ReturnStmt, *parser.BreakStmt:
					blockType = TNever
				}
			}
		}
		return c.record(e, blockType), nil

	case *parser.LambdaExpr:
		return c.inferLambdaExpr(e, sc)

	case *parser.SpawnExpr:
		return c.inferSpawnExpr(e, sc)

	case *parser.OldExpr:
		return c.inferOldExpr(e, sc)

	case *parser.CastExpr:
		if _, err := c.checkExpr(e.X, sc, nil); err != nil {
			return nil, err
		}
		target, err := c.resolveTypeExpr(e.Target)
		if err != nil {
			return nil, err
		}
		return target, nil

	case *parser.VecLitExpr:
		return c.inferVecLitExpr(e, sc, hint)

	case *parser.TupleLitExpr:
		return c.inferTupleLitExpr(e, sc, hint)

	case *parser.ForallExpr:
		return c.inferQuantifierExpr(e.Var, e.Collection, e.Pred, sc)

	case *parser.ExistsExpr:
		return c.inferQuantifierExpr(e.Var, e.Collection, e.Pred, sc)

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
	if info, ok := sc.lookupInfo(name); ok {
		if info.usage != nil {
			info.usage.uses++
		}
		return info.typ, nil
	}

	// Constant lookup
	if t, ok := c.consts[name]; ok {
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

	// Build candidate list for "did you mean?" suggestion.
	candidates := sc.allNames()
	for n := range c.fnSigs {
		candidates = append(candidates, n)
	}
	for n := range c.consts {
		candidates = append(candidates, n)
	}
	hint := didYouMean(name, candidates)
	return nil, &Error{Tok: e.Tok, Msg: fmt.Sprintf("undefined identifier %q", name), Hint: hint}
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
	// Intercept calls to generic functions for monomorphization.
	if ident, ok := e.Fn.(*parser.IdentExpr); ok {
		if gDecl, ok := c.genericFns[ident.Tok.Lexeme]; ok {
			return c.monomorphize(e, ident, gDecl, sc)
		}
	}

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
		// Capability enforcement: cap(X) functions require cap<X> in scope.
		if err := c.checkCapCompat(ident.Tok, ident.Tok.Lexeme, sc); err != nil {
			return nil, err
		}
	}
	if len(e.Args) != len(sig.Params) {
		return nil, c.errorf(e.LParen,
			"argument count mismatch: expected %d, got %d", len(sig.Params), len(e.Args))
	}
	// Determine callee purity for secret<T> enforcement.
	var calleePure bool
	if ident, ok2 := e.Fn.(*parser.IdentExpr); ok2 {
		if eff := c.fnEffects[ident.Tok.Lexeme]; eff != nil && eff.Kind == parser.EffectsPure {
			calleePure = true
		}
	}
	for i, arg := range e.Args {
		argType, err := c.checkExpr(arg, sc, sig.Params[i])
		if err != nil {
			return nil, err
		}
		// secret<T> may only be passed to pure (effects []) functions.
		if gen, ok := argType.(*GenType); ok && gen.Con == "secret" && !calleePure {
			return nil, c.errorf(arg.Pos(),
				"secret<T> value cannot be passed to a non-pure function; use reveal() to unwrap explicitly")
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

// monomorphize instantiates a generic function call site.
// It infers type arguments from the actual argument types, checks the body with
// the substitution active (once per unique instantiation), and records the instance.
func (c *checker) monomorphize(e *parser.CallExpr, ident *parser.IdentExpr, gDecl *parser.FnDecl, sc *scope) (Type, error) {
	if len(e.Args) != len(gDecl.Params) {
		return nil, c.errorf(e.LParen,
			"%s: argument count mismatch: expected %d, got %d",
			gDecl.Name.Lexeme, len(gDecl.Params), len(e.Args))
	}

	// Build typeParams set.
	typeParams := make(map[string]bool, len(gDecl.TypeParams))
	for _, tp := range gDecl.TypeParams {
		typeParams[tp.Lexeme] = true
	}

	// Infer argument types.
	argTypes := make([]Type, len(e.Args))
	for i, arg := range e.Args {
		t, err := c.checkExpr(arg, sc, nil)
		if err != nil {
			return nil, err
		}
		// Default unresolved literals to their canonical type.
		if t == TIntLit {
			t = TI64
		}
		if t == TFloatLit {
			t = TF64
		}
		argTypes[i] = t
		c.exprTypes[arg] = t
	}

	// Unify param type exprs against arg types to build substitution.
	subst := make(map[string]Type)
	for i, param := range gDecl.Params {
		if err := c.unifyTypeExpr(param.Type, argTypes[i], typeParams, subst); err != nil {
			return nil, c.errorf(e.Args[i].Pos(),
				"cannot infer type param for argument %d of %s: %v", i+1, gDecl.Name.Lexeme, err)
		}
	}

	// Verify all type params were resolved.
	for _, tp := range gDecl.TypeParams {
		if _, ok := subst[tp.Lexeme]; !ok {
			return nil, c.errorf(ident.Tok,
				"cannot infer type parameter %q for %s", tp.Lexeme, gDecl.Name.Lexeme)
		}
	}

	// Check trait bounds: for each type param with bounds, verify the concrete
	// type implements all required traits.
	if len(gDecl.TypeBounds) > 0 {
		for tpName, requiredTraits := range gDecl.TypeBounds {
			concreteType := subst[tpName]
			// Extract the underlying type name for trait lookup.
			typeName := ""
			switch ct := concreteType.(type) {
			case *StructType:
				typeName = ct.Name
			case *EnumType:
				typeName = ct.Name
			}
			for _, traitName := range requiredTraits {
				if typeName == "" || !c.traitImpls[typeName][traitName] {
					return nil, c.errorf(ident.Tok,
						"type %s does not implement trait %s (required by type parameter %s of %s)",
						concreteType, traitName, tpName, gDecl.Name.Lexeme)
				}
			}
		}
	}

	// Build the mangled C name from the substituted types (ordered by TypeParams list).
	mangler := strings.NewReplacer("<", "_", ">", "_", ",", "_", " ", "_", "(", "_", ")", "_", "*", "ptr", "-", "_")
	var parts []string
	for _, tp := range gDecl.TypeParams {
		parts = append(parts, mangler.Replace(subst[tp.Lexeme].String()))
	}
	mangledName := gDecl.Name.Lexeme + "__" + strings.Join(parts, "__")

	// Apply substitution to build concrete signature.
	prevSubst := c.typeVarSubst
	c.typeVarSubst = subst

	sig, err := c.buildFnSig(gDecl)
	if err != nil {
		c.typeVarSubst = prevSubst
		return nil, err
	}

	// Instantiate (check body) if not already done for this mangled name.
	if _, exists := c.genInstances[mangledName]; !exists {
		inst := &GenericInstance{
			FnName:      gDecl.Name.Lexeme,
			MangledName: mangledName,
			Sig:         sig,
			Node:        gDecl,
			Subst:       subst,
		}
		c.genInstances[mangledName] = inst
		c.genInstanceList = append(c.genInstanceList, inst)

		// Type-check the body under the substitution.
		bodySc := newScope(nil)
		for i, p := range gDecl.Params {
			bodySc.define(p.Name.Lexeme, sig.Params[i])
		}
		if err := c.checkBlock(gDecl.Body, bodySc, sig.Ret); err != nil {
			c.typeVarSubst = prevSubst
			return nil, err
		}
	}

	c.typeVarSubst = prevSubst

	// Record the call expression's return type and the call site → instance mapping.
	ret := sig.Ret
	c.exprTypes[e] = ret
	c.callSiteGeneric[e.Fn] = c.genInstances[mangledName]

	return ret, nil
}

// unifyTypeExpr matches a declared TypeExpr (which may mention type param names)
// against a concrete resolved Type, populating subst.
func (c *checker) unifyTypeExpr(te parser.TypeExpr, concrete Type, typeParams map[string]bool, subst map[string]Type) error {
	switch t := te.(type) {
	case *parser.NamedType:
		name := t.Name.Lexeme
		if typeParams[name] {
			// Bind or check consistency.
			if existing, ok := subst[name]; ok {
				if !existing.Equals(concrete) {
					return fmt.Errorf("type parameter %q: conflicting types %s and %s", name, existing, concrete)
				}
			} else {
				subst[name] = concrete
			}
			return nil
		}
		// Concrete named type — must match.
		resolved, err := c.resolveTypeExpr(te)
		if err != nil {
			return err
		}
		if !resolved.Equals(concrete) {
			return fmt.Errorf("expected %s, got %s", resolved, concrete)
		}
		return nil

	case *parser.GenericType:
		gen, ok := concrete.(*GenType)
		if !ok || gen.Con != t.Name.Lexeme || len(gen.Params) != len(t.Params) {
			return fmt.Errorf("expected %s<...>, got %s", t.Name.Lexeme, concrete)
		}
		for i, p := range t.Params {
			if err := c.unifyTypeExpr(p, gen.Params[i], typeParams, subst); err != nil {
				return err
			}
		}
		return nil

	case *parser.FnType:
		ft, ok := concrete.(*FnType)
		if !ok || len(ft.Params) != len(t.Params) {
			return fmt.Errorf("expected fn type, got %s", concrete)
		}
		for i, p := range t.Params {
			if err := c.unifyTypeExpr(p, ft.Params[i], typeParams, subst); err != nil {
				return err
			}
		}
		return c.unifyTypeExpr(t.RetType, ft.Ret, typeParams, subst)

	default:
		return fmt.Errorf("unsupported type expression %T in unification", te)
	}
}

// checkEffectsCompat enforces that callee's effects are compatible with the
// current function's declared effects.
//
//   - Caller unannotated (curEffects == nil): no check — gradual adoption.
//   - Caller pure: callee must also be pure (or unannotated ≡ trusted).
//   - Caller effects(X): callee's effects must be ⊆ X (unannotated = trusted).
// checkCapCompat enforces that a call to a cap(X) function is made from a
// context that holds cap<X>. The caller qualifies if:
//   - The current function is itself annotated cap(X), OR
//   - A cap<X> token is visible in the current scope.
func (c *checker) checkCapCompat(callTok lexer.Token, calleeName string, sc *scope) error {
	calleeEff := c.fnEffects[calleeName]
	if calleeEff == nil || calleeEff.Kind != parser.EffectsCap || len(calleeEff.Names) == 0 {
		return nil
	}
	capName := calleeEff.Names[0]

	// Caller is itself a cap(capName) function.
	if c.curEffects != nil && c.curEffects.Kind == parser.EffectsCap &&
		len(c.curEffects.Names) > 0 && c.curEffects.Names[0] == capName {
		return nil
	}

	// A cap<capName> token is visible in scope.
	if c.hasCap(sc, capName) {
		return nil
	}

	return c.errorf(callTok,
		"call to cap(%s) function %q requires a cap<%s> token in scope; pass one as a parameter",
		capName, calleeName, capName)
}

// hasCap reports whether a variable of type cap<capName> is visible in sc.
func (c *checker) hasCap(sc *scope, capName string) bool {
	for s := sc; s != nil; s = s.parent {
		for _, info := range s.vars {
			gen, ok := info.typ.(*GenType)
			if !ok || gen.Con != "cap" || len(gen.Params) == 0 {
				continue
			}
			if ct, ok2 := gen.Params[0].(*CapabilityType); ok2 && ct.Name == capName {
				return true
			}
		}
	}
	return false
}

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
	// Tuple index: x.0, x.1, ...
	if tt, ok := recvType.(*TupleType); ok {
		idx, err2 := strconv.Atoi(e.Field.Lexeme)
		if err2 != nil || idx < 0 || idx >= len(tt.Elems) {
			return nil, c.errorf(e.Field, "tuple index %s out of range (len %d)", e.Field.Lexeme, len(tt.Elems))
		}
		return tt.Elems[idx], nil
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
	// str[i] → u8
	if collType == TStr {
		return TU8, nil
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
		et, ok := c.lookupEnum(p.Head.Lexeme)
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
			et, ok := c.lookupEnum(path.Head.Lexeme)
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

// ── map built-in functions ────────────────────────────────────────────────────

func (c *checker) inferVecPop(e *parser.CallExpr, ident *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(ident.Tok, "vec_pop() expects 1 argument, got %d", len(e.Args))
	}
	argType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := argType.(*GenType)
	if !ok || gen.Con != "vec" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "vec_pop() expects vec<T>, got %s", argType)
	}
	// No hint pinning needed for pop, but we return the element type.
	return gen.Params[0], nil
}

func (c *checker) inferMapNew(e *parser.CallExpr, ident *parser.IdentExpr, sc *scope, hint Type) (Type, error) {
	if len(e.Args) != 0 {
		return nil, c.errorf(e.LParen, "map_new() takes no arguments")
	}
	var keyType, valType Type
	if gen, ok := hint.(*GenType); ok && gen.Con == "map" && len(gen.Params) == 2 {
		keyType = gen.Params[0]
		valType = gen.Params[1]
	}
	if keyType == nil || valType == nil {
		return nil, c.errorf(ident.Tok, "map_new() requires a type annotation to infer key/value types")
	}
	t := &GenType{Con: "map", Params: []Type{keyType, valType}}
	c.record(ident, t)
	return t, nil
}

func (c *checker) inferMapInsert(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 3 {
		return nil, c.errorf(e.LParen, "map_insert() takes 3 arguments: (map, key, value)")
	}
	mapType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := mapType.(*GenType)
	if !ok || gen.Con != "map" || len(gen.Params) != 2 {
		return nil, c.errorf(e.Args[0].Pos(), "map_insert() first argument must be map<K,V>, got %s", mapType)
	}
	keyType, valType := gen.Params[0], gen.Params[1]
	kt, err := c.checkExpr(e.Args[1], sc, keyType)
	if err != nil {
		return nil, err
	}
	if _, ok := Coerce(kt, keyType); !ok {
		return nil, c.errorf(e.Args[1].Pos(), "map_insert() key type %s does not match map key type %s", kt, keyType)
	}
	vt, err := c.checkExpr(e.Args[2], sc, valType)
	if err != nil {
		return nil, err
	}
	if coerced, ok2 := Coerce(vt, valType); ok2 {
		c.exprTypes[e.Args[2]] = coerced
	} else {
		return nil, c.errorf(e.Args[2].Pos(), "map_insert() value type %s does not match map value type %s", vt, valType)
	}
	c.record(fn, TUnit)
	return TUnit, nil
}

func (c *checker) inferMapGet(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 2 {
		return nil, c.errorf(e.LParen, "map_get() takes 2 arguments: (map, key)")
	}
	mapType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := mapType.(*GenType)
	if !ok || gen.Con != "map" || len(gen.Params) != 2 {
		return nil, c.errorf(e.Args[0].Pos(), "map_get() first argument must be map<K,V>, got %s", mapType)
	}
	keyType, valType := gen.Params[0], gen.Params[1]
	kt, err := c.checkExpr(e.Args[1], sc, keyType)
	if err != nil {
		return nil, err
	}
	if _, ok := Coerce(kt, keyType); !ok {
		return nil, c.errorf(e.Args[1].Pos(), "map_get() key type %s does not match map key type %s", kt, keyType)
	}
	retType := &GenType{Con: "option", Params: []Type{valType}}
	c.record(fn, retType)
	return retType, nil
}

func (c *checker) inferMapRemove(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 2 {
		return nil, c.errorf(e.LParen, "map_remove() takes 2 arguments: (map, key)")
	}
	mapType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := mapType.(*GenType)
	if !ok || gen.Con != "map" || len(gen.Params) != 2 {
		return nil, c.errorf(e.Args[0].Pos(), "map_remove() first argument must be map<K,V>, got %s", mapType)
	}
	keyType := gen.Params[0]
	kt, err := c.checkExpr(e.Args[1], sc, keyType)
	if err != nil {
		return nil, err
	}
	if _, ok := Coerce(kt, keyType); !ok {
		return nil, c.errorf(e.Args[1].Pos(), "map_remove() key type %s does not match map key type %s", kt, keyType)
	}
	c.record(fn, TBool)
	return TBool, nil
}

func (c *checker) inferMapLen(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "map_len() takes 1 argument")
	}
	mapType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := mapType.(*GenType)
	if !ok || gen.Con != "map" || len(gen.Params) != 2 {
		return nil, c.errorf(e.Args[0].Pos(), "map_len() requires map<K,V>, got %s", mapType)
	}
	c.record(fn, TU64)
	return TU64, nil
}

func (c *checker) inferMapContains(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 2 {
		return nil, c.errorf(e.LParen, "map_contains() takes 2 arguments: (map, key)")
	}
	mapType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := mapType.(*GenType)
	if !ok || gen.Con != "map" || len(gen.Params) != 2 {
		return nil, c.errorf(e.Args[0].Pos(), "map_contains() requires map<K,V>, got %s", mapType)
	}
	keyType := gen.Params[0]
	kt, err := c.checkExpr(e.Args[1], sc, keyType)
	if err != nil {
		return nil, err
	}
	if _, ok := Coerce(kt, keyType); !ok {
		return nil, c.errorf(e.Args[1].Pos(), "map_contains() key type %s does not match map key type %s", kt, keyType)
	}
	c.record(fn, TBool)
	return TBool, nil
}

func (c *checker) inferLambdaExpr(e *parser.LambdaExpr, sc *scope) (Type, error) {
	// Resolve parameter types.
	params := make([]Type, len(e.Params))
	for i, p := range e.Params {
		t, err := c.resolveTypeExpr(p.Type)
		if err != nil {
			return nil, err
		}
		params[i] = t
	}
	retType, err := c.resolveTypeExpr(e.RetType)
	if err != nil {
		return nil, err
	}
	sig := &FnType{Params: params, Ret: retType}

	// Type-check the body in a new scope with the parameters defined.
	bodySc := newScope(sc)
	for i, p := range e.Params {
		bodySc.define(p.Name.Lexeme, params[i])
	}
	if err := c.checkBlock(e.Body, bodySc, retType); err != nil {
		return nil, err
	}

	// Register the lambda with capture analysis.
	c.lambdaCount++
	name := fmt.Sprintf("_cnd_lambda_%d", c.lambdaCount)

	// Determine the set of names that are lambda parameters or let-bindings inside.
	paramNames := make(map[string]bool)
	for _, p := range e.Params {
		paramNames[p.Name.Lexeme] = true
	}
	collectLetNames(e.Body.Stmts, paramNames)

	// Walk the body collecting free variable references.
	seen := make(map[string]bool)
	var captures []string
	var captureTypes []Type
	var captureByRef []bool
	for _, stmt := range e.Body.Stmts {
		walkIdentsInStmt(stmt, func(name string) {
			if seen[name] || paramNames[name] {
				return
			}
			if _, isFn := c.fnSigs[name]; isFn {
				return
			}
			if info, ok := sc.lookupInfo(name); ok {
				seen[name] = true
				captures = append(captures, name)
				captureTypes = append(captureTypes, info.typ)
				// Mutable outer variables are captured by pointer so mutations are visible.
				captureByRef = append(captureByRef, info.mutable)
			}
		})
	}

	info := &LambdaInfo{Node: e, Name: name, Sig: sig, Captures: captures, CaptureTypes: captureTypes, CaptureByRef: captureByRef}
	c.lambdas = append(c.lambdas, info)

	return c.record(e, sig), nil
}

// collectLetNames adds all names bound by let statements (at any depth) in stmts to out.
func collectLetNames(stmts []parser.Stmt, out map[string]bool) {
	for _, s := range stmts {
		switch v := s.(type) {
		case *parser.LetStmt:
			out[v.Name.Lexeme] = true
		case *parser.IfStmt:
			collectLetNames(v.Then.Stmts, out)
			if v.Else != nil {
				if blk, ok := v.Else.(*parser.BlockStmt); ok {
					collectLetNames(blk.Stmts, out)
				} else if elif, ok := v.Else.(*parser.IfStmt); ok {
					collectLetNames([]parser.Stmt{elif}, out)
				}
			}
		case *parser.LoopStmt:
			collectLetNames(v.Body.Stmts, out)
		case *parser.ForStmt:
			out[v.Var.Lexeme] = true
			collectLetNames(v.Body.Stmts, out)
		case *parser.BlockStmt:
			collectLetNames(v.Stmts, out)
		}
	}
}

// walkIdentsInStmt visits every IdentExpr leaf in a statement and calls fn with its name.
func walkIdentsInStmt(s parser.Stmt, fn func(string)) {
	switch v := s.(type) {
	case *parser.LetStmt:
		walkIdentsInExpr(v.Value, fn)
	case *parser.AssignStmt:
		walkIdentsInExpr(v.Value, fn)
	case *parser.ReturnStmt:
		if v.Value != nil {
			walkIdentsInExpr(v.Value, fn)
		}
	case *parser.ExprStmt:
		walkIdentsInExpr(v.X, fn)
	case *parser.IfStmt:
		walkIdentsInExpr(v.Cond, fn)
		for _, st := range v.Then.Stmts {
			walkIdentsInStmt(st, fn)
		}
		if v.Else != nil {
			walkIdentsInStmt(v.Else, fn)
		}
	case *parser.BlockStmt:
		for _, st := range v.Stmts {
			walkIdentsInStmt(st, fn)
		}
	case *parser.LoopStmt:
		for _, st := range v.Body.Stmts {
			walkIdentsInStmt(st, fn)
		}
	case *parser.ForStmt:
		walkIdentsInExpr(v.Collection, fn)
		for _, st := range v.Body.Stmts {
			walkIdentsInStmt(st, fn)
		}
	case *parser.BreakStmt, *parser.ContinueStmt:
		// no exprs
	}
}

// walkIdentsInExpr visits every IdentExpr leaf in an expression and calls fn.
func walkIdentsInExpr(e parser.Expr, fn func(string)) {
	if e == nil {
		return
	}
	switch v := e.(type) {
	case *parser.IdentExpr:
		fn(v.Tok.Lexeme)
	case *parser.IntLitExpr, *parser.FloatLitExpr, *parser.StringLitExpr, *parser.BoolLitExpr:
		// leaves — no idents
	case *parser.BinaryExpr:
		walkIdentsInExpr(v.Left, fn)
		walkIdentsInExpr(v.Right, fn)
	case *parser.UnaryExpr:
		walkIdentsInExpr(v.Operand, fn)
	case *parser.CallExpr:
		walkIdentsInExpr(v.Fn, fn)
		for _, a := range v.Args {
			walkIdentsInExpr(a, fn)
		}
	case *parser.FieldExpr:
		walkIdentsInExpr(v.Receiver, fn)
	case *parser.IndexExpr:
		walkIdentsInExpr(v.Collection, fn)
		walkIdentsInExpr(v.Index, fn)
	case *parser.StructLitExpr:
		for _, f := range v.Fields {
			walkIdentsInExpr(f.Value, fn)
		}
	case *parser.MatchExpr:
		walkIdentsInExpr(v.X, fn)
		for _, arm := range v.Arms {
			walkIdentsInExpr(arm.Body, fn)
		}
	case *parser.MustExpr:
		walkIdentsInExpr(v.X, fn)
		for _, arm := range v.Arms {
			walkIdentsInExpr(arm.Body, fn)
		}
	case *parser.LambdaExpr:
		// Don't recurse into nested lambdas — they have their own capture analysis.
	case *parser.SpawnExpr:
		// Don't recurse into nested spawns — they have their own capture analysis.
	case *parser.OldExpr:
		walkIdentsInExpr(v.X, fn)
	case *parser.BlockExpr:
		for _, st := range v.Stmts {
			walkIdentsInStmt(st, fn)
		}
	case *parser.PathExpr:
		// enum variant path — no free variables
	case *parser.ReturnExpr:
		walkIdentsInExpr(v.Value, fn)
	case *parser.BreakExpr:
		// no exprs
	}
}

// inferQuantifierExpr handles forall/exists boolean quantifiers over collections.
// The bound variable is scoped only within the predicate expression.
func (c *checker) inferQuantifierExpr(varTok lexer.Token, collection parser.Expr, pred parser.Expr, sc *scope) (Type, error) {
	collType, err := c.checkExpr(collection, sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := collType.(*GenType)
	if !ok || (gen.Con != "vec" && gen.Con != "ring") {
		return nil, c.errorf(varTok, "forall/exists requires vec<T> or ring<T>, got %s", collType)
	}
	elemType := gen.Params[0]
	// Evaluate predicate in a child scope with the bound variable.
	child := newScope(sc)
	child.define(varTok.Lexeme, elemType)
	predType, err := c.checkExpr(pred, child, TBool)
	if err != nil {
		return nil, err
	}
	if !predType.Equals(TBool) {
		return nil, c.errorf(varTok, "forall/exists predicate must be bool, got %s", predType)
	}
	return TBool, nil
}

func (c *checker) inferOldExpr(e *parser.OldExpr, sc *scope) (Type, error) {
	if !c.inEnsures {
		return nil, c.errorf(e.OldTok, "old() may only be used inside ensures clauses")
	}
	// Prevent nested old(old(...)).
	prev := c.inEnsures
	c.inEnsures = false
	innerType, err := c.checkExpr(e.X, sc, nil)
	c.inEnsures = prev
	if err != nil {
		return nil, err
	}
	return c.record(e, innerType), nil
}

func (c *checker) inferSetNew(e *parser.CallExpr, ident *parser.IdentExpr, sc *scope, hint Type) (Type, error) {
	if len(e.Args) != 0 {
		return nil, c.errorf(e.LParen, "set_new() takes no arguments")
	}
	var elemType Type
	if gen, ok := hint.(*GenType); ok && gen.Con == "set" && len(gen.Params) == 1 {
		elemType = gen.Params[0]
	}
	if elemType == nil {
		return nil, c.errorf(ident.Tok, "set_new() requires a type annotation to infer element type")
	}
	t := &GenType{Con: "set", Params: []Type{elemType}}
	c.record(ident, t)
	return t, nil
}

func (c *checker) inferSetAdd(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 2 {
		return nil, c.errorf(e.LParen, "set_add() takes 2 arguments: (set, value)")
	}
	setType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := setType.(*GenType)
	if !ok || gen.Con != "set" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "set_add() first argument must be set<T>, got %s", setType)
	}
	elemType := gen.Params[0]
	vt, err := c.checkExpr(e.Args[1], sc, elemType)
	if err != nil {
		return nil, err
	}
	if coerced, ok2 := Coerce(vt, elemType); ok2 {
		c.exprTypes[e.Args[1]] = coerced
	} else {
		return nil, c.errorf(e.Args[1].Pos(), "set_add() value type %s does not match set element type %s", vt, elemType)
	}
	c.record(fn, TUnit)
	return TUnit, nil
}

func (c *checker) inferSetRemove(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 2 {
		return nil, c.errorf(e.LParen, "set_remove() takes 2 arguments: (set, value)")
	}
	setType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := setType.(*GenType)
	if !ok || gen.Con != "set" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "set_remove() first argument must be set<T>, got %s", setType)
	}
	keyType := gen.Params[0]
	kt, err := c.checkExpr(e.Args[1], sc, keyType)
	if err != nil {
		return nil, err
	}
	if _, ok := Coerce(kt, keyType); !ok {
		return nil, c.errorf(e.Args[1].Pos(), "set_remove() value type %s does not match set element type %s", kt, keyType)
	}
	c.record(fn, TBool)
	return TBool, nil
}

func (c *checker) inferSetContains(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 2 {
		return nil, c.errorf(e.LParen, "set_contains() takes 2 arguments: (set, value)")
	}
	setType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := setType.(*GenType)
	if !ok || gen.Con != "set" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "set_contains() first argument must be set<T>, got %s", setType)
	}
	keyType := gen.Params[0]
	kt, err := c.checkExpr(e.Args[1], sc, keyType)
	if err != nil {
		return nil, err
	}
	if _, ok := Coerce(kt, keyType); !ok {
		return nil, c.errorf(e.Args[1].Pos(), "set_contains() value type %s does not match set element type %s", kt, keyType)
	}
	c.record(fn, TBool)
	return TBool, nil
}

func (c *checker) inferSetLen(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "set_len() takes 1 argument")
	}
	setType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := setType.(*GenType)
	if !ok || gen.Con != "set" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "set_len() requires set<T>, got %s", setType)
	}
	c.record(fn, TU64)
	return TU64, nil
}

// ── ring<T> built-in functions ────────────────────────────────────────────────

func (c *checker) inferRingNew(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope, hint Type) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "ring_new() takes 1 argument: capacity (u64)")
	}
	capType, err := c.checkExpr(e.Args[0], sc, TU64)
	if err != nil {
		return nil, err
	}
	coerced, ok := Coerce(capType, TU64)
	if !ok {
		return nil, c.errorf(e.Args[0].Pos(), "ring_new() capacity must be u64, got %s", capType)
	}
	c.exprTypes[e.Args[0]] = coerced
	var elemType Type
	if gen, ok2 := hint.(*GenType); ok2 && gen.Con == "ring" && len(gen.Params) > 0 {
		elemType = gen.Params[0]
	}
	if elemType == nil {
		return nil, c.errorf(fn.Tok, "ring_new() requires a type annotation to infer element type")
	}
	t := &GenType{Con: "ring", Params: []Type{elemType}}
	c.record(fn, t)
	return t, nil
}

func (c *checker) inferRingPushBack(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 2 {
		return nil, c.errorf(e.LParen, "ring_push_back() takes 2 arguments: (ring, value)")
	}
	ringType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := ringType.(*GenType)
	if !ok || gen.Con != "ring" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "ring_push_back() first argument must be ring<T>, got %s", ringType)
	}
	elemType := gen.Params[0]
	valType, err := c.checkExpr(e.Args[1], sc, elemType)
	if err != nil {
		return nil, err
	}
	coerced, ok := Coerce(valType, elemType)
	if !ok {
		return nil, c.errorf(e.Args[1].Pos(), "ring_push_back() value type %s does not match element type %s", valType, elemType)
	}
	c.exprTypes[e.Args[1]] = coerced
	c.record(fn, TUnit)
	return TUnit, nil
}

func (c *checker) inferRingPopFront(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "ring_pop_front() takes 1 argument: (ring)")
	}
	ringType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := ringType.(*GenType)
	if !ok || gen.Con != "ring" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "ring_pop_front() argument must be ring<T>, got %s", ringType)
	}
	ret := &GenType{Con: "option", Params: []Type{gen.Params[0]}}
	c.record(fn, ret)
	return ret, nil
}

func (c *checker) inferRingLen(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "ring_len() takes 1 argument")
	}
	ringType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := ringType.(*GenType)
	if !ok || gen.Con != "ring" {
		return nil, c.errorf(e.Args[0].Pos(), "ring_len() requires ring<T>, got %s", ringType)
	}
	c.record(fn, TU64)
	return TU64, nil
}

func (c *checker) inferRingIsEmpty(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "ring_is_empty() takes 1 argument")
	}
	ringType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := ringType.(*GenType)
	if !ok || gen.Con != "ring" {
		return nil, c.errorf(e.Args[0].Pos(), "ring_is_empty() requires ring<T>, got %s", ringType)
	}
	c.record(fn, TBool)
	return TBool, nil
}

// ── box built-in functions ─────────────────────────────────────────────────

func (c *checker) inferBoxNew(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope, hint Type) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "box_new() takes 1 argument: the value to heap-allocate")
	}
	var innerHint Type
	if gen, ok := hint.(*GenType); ok && gen.Con == "box" && len(gen.Params) > 0 {
		innerHint = gen.Params[0]
	}
	argType, err := c.checkExpr(e.Args[0], sc, innerHint)
	if err != nil {
		return nil, err
	}
	t := &GenType{Con: "box", Params: []Type{argType}}
	c.record(fn, t)
	return t, nil
}

func (c *checker) inferBoxDeref(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "box_deref() takes 1 argument: box<T>")
	}
	argType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := argType.(*GenType)
	if !ok || gen.Con != "box" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "box_deref() requires box<T>, got %s", argType)
	}
	inner := gen.Params[0]
	c.record(fn, inner)
	return inner, nil
}

func (c *checker) inferBoxDrop(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "box_drop() takes 1 argument: box<T>")
	}
	argType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := argType.(*GenType)
	if !ok || gen.Con != "box" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "box_drop() requires box<T>, got %s", argType)
	}
	_ = gen
	c.record(fn, TUnit)
	return TUnit, nil
}

// -- arc built-in functions -------------------------------------------------

func (c *checker) inferArcNew(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope, hint Type) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "arc_new() takes 1 argument: the value to reference-count")
	}
	var innerHint Type
	if gen, ok := hint.(*GenType); ok && gen.Con == "arc" && len(gen.Params) > 0 {
		innerHint = gen.Params[0]
	}
	argType, err := c.checkExpr(e.Args[0], sc, innerHint)
	if err != nil {
		return nil, err
	}
	t := &GenType{Con: "arc", Params: []Type{argType}}
	c.record(fn, t)
	return t, nil
}

func (c *checker) inferArcClone(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "arc_clone() takes 1 argument: arc<T>")
	}
	argType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := argType.(*GenType)
	if !ok || gen.Con != "arc" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "arc_clone() requires arc<T>, got %s", argType)
	}
	c.record(fn, gen)
	return gen, nil
}

func (c *checker) inferArcDeref(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "arc_deref() takes 1 argument: arc<T>")
	}
	argType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := argType.(*GenType)
	if !ok || gen.Con != "arc" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "arc_deref() requires arc<T>, got %s", argType)
	}
	inner := gen.Params[0]
	c.record(fn, inner)
	return inner, nil
}

func (c *checker) inferArcDrop(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "arc_drop() takes 1 argument: arc<T>")
	}
	argType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := argType.(*GenType)
	if !ok || gen.Con != "arc" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "arc_drop() requires arc<T>, got %s", argType)
	}
	_ = gen
	c.record(fn, TUnit)
	return TUnit, nil
}


// -- tensor built-in functions -----------------------------------------------

// tensor_zeros(shape: vec<i64>) -> tensor<T>   (T from type hint)
func (c *checker) inferTensorZeros(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope, hint Type) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "tensor_zeros() takes 1 argument: shape as vec<i64>")
	}
	shapeType, err := c.checkExpr(e.Args[0], sc, &GenType{Con: "vec", Params: []Type{TI64}})
	if err != nil {
		return nil, err
	}
	shapeGen, ok := shapeType.(*GenType)
	if !ok || shapeGen.Con != "vec" {
		return nil, c.errorf(e.Args[0].Pos(), "tensor_zeros() shape must be vec<i64>, got %s", shapeType)
	}
	// Derive element type from hint (e.g. let t: tensor<f32> = tensor_zeros(...))
	var elemType Type = TF64
	if gen, ok := hint.(*GenType); ok && gen.Con == "tensor" && len(gen.Params) == 1 {
		elemType = gen.Params[0]
	}
	t := &GenType{Con: "tensor", Params: []Type{elemType}}
	c.record(fn, t)
	return t, nil
}

// tensor_from_vec(data: vec<T>, shape: vec<i64>) -> tensor<T>
func (c *checker) inferTensorFromVec(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope, hint Type) (Type, error) {
	if len(e.Args) != 2 {
		return nil, c.errorf(e.LParen, "tensor_from_vec() takes 2 arguments: vec<T> data, vec<i64> shape")
	}
	var innerHint Type
	if gen, ok := hint.(*GenType); ok && gen.Con == "tensor" && len(gen.Params) == 1 {
		innerHint = gen.Params[0]
	}
	dataType, err := c.checkExpr(e.Args[0], sc, &GenType{Con: "vec", Params: []Type{innerHint}})
	if err != nil {
		return nil, err
	}
	dataGen, ok := dataType.(*GenType)
	if !ok || dataGen.Con != "vec" || len(dataGen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "tensor_from_vec() first arg must be vec<T>, got %s", dataType)
	}
	_, err = c.checkExpr(e.Args[1], sc, &GenType{Con: "vec", Params: []Type{TI64}})
	if err != nil {
		return nil, err
	}
	t := &GenType{Con: "tensor", Params: []Type{dataGen.Params[0]}}
	c.record(fn, t)
	return t, nil
}

// tensor_to_vec(t: tensor<T>) -> vec<T>
func (c *checker) inferTensorToVec(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "tensor_to_vec() takes 1 argument: tensor<T>")
	}
	argType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := argType.(*GenType)
	if !ok || gen.Con != "tensor" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "tensor_to_vec() requires tensor<T>, got %s", argType)
	}
	t := &GenType{Con: "vec", Params: []Type{gen.Params[0]}}
	c.record(fn, t)
	return t, nil
}

// tensor_get(t: ref<tensor<T>>, idx: vec<i64>) -> T
func (c *checker) inferTensorGet(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 2 {
		return nil, c.errorf(e.LParen, "tensor_get() takes 2 arguments: ref<tensor<T>>, vec<i64>")
	}
	argType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	// Accept both tensor<T> and ref<tensor<T>>
	inner := argType
	if ref, ok := argType.(*GenType); ok && ref.Con == "ref" && len(ref.Params) == 1 {
		inner = ref.Params[0]
	}
	gen, ok := inner.(*GenType)
	if !ok || gen.Con != "tensor" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "tensor_get() first arg must be tensor<T> or ref<tensor<T>>, got %s", argType)
	}
	_, err = c.checkExpr(e.Args[1], sc, &GenType{Con: "vec", Params: []Type{TI64}})
	if err != nil {
		return nil, err
	}
	elemType := gen.Params[0]
	c.record(fn, elemType)
	return elemType, nil
}

// tensor_set(t: ref<tensor<T>>, idx: vec<i64>, val: T) -> unit
func (c *checker) inferTensorSet(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 3 {
		return nil, c.errorf(e.LParen, "tensor_set() takes 3 arguments: ref<tensor<T>>, vec<i64>, T")
	}
	argType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	inner := argType
	if ref, ok := argType.(*GenType); ok && ref.Con == "ref" && len(ref.Params) == 1 {
		inner = ref.Params[0]
	}
	gen, ok := inner.(*GenType)
	if !ok || gen.Con != "tensor" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "tensor_set() first arg must be tensor<T> or ref<tensor<T>>, got %s", argType)
	}
	_, err = c.checkExpr(e.Args[1], sc, &GenType{Con: "vec", Params: []Type{TI64}})
	if err != nil {
		return nil, err
	}
	_, err = c.checkExpr(e.Args[2], sc, gen.Params[0])
	if err != nil {
		return nil, err
	}
	c.record(fn, TUnit)
	return TUnit, nil
}

// tensor_ndim(t: ref<tensor<T>>) -> i64
func (c *checker) inferTensorNdim(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "tensor_ndim() takes 1 argument: tensor<T> or ref<tensor<T>>")
	}
	argType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	inner := argType
	if ref, ok := argType.(*GenType); ok && ref.Con == "ref" && len(ref.Params) == 1 {
		inner = ref.Params[0]
	}
	if gen, ok := inner.(*GenType); !ok || gen.Con != "tensor" {
		return nil, c.errorf(e.Args[0].Pos(), "tensor_ndim() requires tensor<T> or ref<tensor<T>>, got %s", argType)
	}
	c.record(fn, TI64)
	return TI64, nil
}

// tensor_shape(t: ref<tensor<T>>) -> vec<i64>
func (c *checker) inferTensorShape(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "tensor_shape() takes 1 argument: tensor<T> or ref<tensor<T>>")
	}
	argType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	inner := argType
	if ref, ok := argType.(*GenType); ok && ref.Con == "ref" && len(ref.Params) == 1 {
		inner = ref.Params[0]
	}
	if gen, ok := inner.(*GenType); !ok || gen.Con != "tensor" {
		return nil, c.errorf(e.Args[0].Pos(), "tensor_shape() requires tensor<T> or ref<tensor<T>>, got %s", argType)
	}
	t := &GenType{Con: "vec", Params: []Type{TI64}}
	c.record(fn, t)
	return t, nil
}

// tensor_len(t: ref<tensor<T>>) -> i64
func (c *checker) inferTensorLen(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "tensor_len() takes 1 argument: tensor<T> or ref<tensor<T>>")
	}
	argType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	inner := argType
	if ref, ok := argType.(*GenType); ok && ref.Con == "ref" && len(ref.Params) == 1 {
		inner = ref.Params[0]
	}
	if gen, ok := inner.(*GenType); !ok || gen.Con != "tensor" {
		return nil, c.errorf(e.Args[0].Pos(), "tensor_len() requires tensor<T> or ref<tensor<T>>, got %s", argType)
	}
	c.record(fn, TI64)
	return TI64, nil
}

// tensor_free(t: tensor<T>) -> unit
func (c *checker) inferTensorFree(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "tensor_free() takes 1 argument: tensor<T>")
	}
	argType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := argType.(*GenType)
	if !ok || gen.Con != "tensor" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "tensor_free() requires tensor<T>, got %s", argType)
	}
	_ = gen
	c.record(fn, TUnit)
	return TUnit, nil
}

// -- tensor SIMD intrinsics (M11.3) ------------------------------------------

// unwrapTensorElem extracts T from tensor<T> or ref<tensor<T>>; errors if neither.
func (c *checker) unwrapTensorElem(e *parser.CallExpr, argIdx int, argType Type) (Type, error) {
	t := argType
	if ref, ok := t.(*GenType); ok && ref.Con == "ref" && len(ref.Params) == 1 {
		t = ref.Params[0]
	}
	gen, ok := t.(*GenType)
	if !ok || gen.Con != "tensor" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[argIdx].Pos(), "expected tensor<T> or ref<tensor<T>>, got %s", argType)
	}
	return gen.Params[0], nil
}

// tensor_dot(a, b) -> T   (element-wise product sum)
func (c *checker) inferTensorDot(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 2 {
		return nil, c.errorf(e.LParen, "tensor_dot() takes 2 arguments: tensor<T>, tensor<T>")
	}
	at, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	elem, err := c.unwrapTensorElem(e, 0, at)
	if err != nil {
		return nil, err
	}
	_, err = c.checkExpr(e.Args[1], sc, nil)
	if err != nil {
		return nil, err
	}
	c.record(fn, elem)
	return elem, nil
}

// tensor_l2(a) -> T   (L2 norm = sqrt(sum(a[i]^2)))
func (c *checker) inferTensorL2(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "tensor_l2() takes 1 argument: tensor<T>")
	}
	at, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	elem, err := c.unwrapTensorElem(e, 0, at)
	if err != nil {
		return nil, err
	}
	c.record(fn, elem)
	return elem, nil
}

// tensor_cosine(a, b) -> T   (cosine similarity = dot(a,b) / (l2(a)*l2(b)))
func (c *checker) inferTensorCosine(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 2 {
		return nil, c.errorf(e.LParen, "tensor_cosine() takes 2 arguments: tensor<T>, tensor<T>")
	}
	at, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	elem, err := c.unwrapTensorElem(e, 0, at)
	if err != nil {
		return nil, err
	}
	_, err = c.checkExpr(e.Args[1], sc, nil)
	if err != nil {
		return nil, err
	}
	c.record(fn, elem)
	return elem, nil
}

// tensor_matmul(a, b, out) -> unit   (out[i,j] += a[i,k]*b[k,j])
func (c *checker) inferTensorMatmul(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 3 {
		return nil, c.errorf(e.LParen, "tensor_matmul() takes 3 arguments: tensor<T>, tensor<T>, ref<tensor<T>>")
	}
	for i := 0; i < 3; i++ {
		at, err := c.checkExpr(e.Args[i], sc, nil)
		if err != nil {
			return nil, err
		}
		if _, err := c.unwrapTensorElem(e, i, at); err != nil {
			return nil, err
		}
	}
	c.record(fn, TUnit)
	return TUnit, nil
}

// -- mmap built-in functions (M12.1) ----------------------------------------

// mmapResultType builds result<mmap<u8>, str>.
func mmapResultType() Type {
	return &GenType{Con: "result", Params: []Type{&GenType{Con: "mmap", Params: []Type{TU8}}, TStr}}
}

// mmap_open(path: str, byte_len: u64) -> result<mmap<u8>, str>  effects(storage)
func (c *checker) inferMmapOpen(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 2 {
		return nil, c.errorf(e.LParen, "mmap_open() takes 2 arguments: path str, byte_len u64")
	}
	if _, err := c.checkExpr(e.Args[0], sc, TStr); err != nil {
		return nil, err
	}
	if _, err := c.checkExpr(e.Args[1], sc, TU64); err != nil {
		return nil, err
	}
	t := mmapResultType()
	c.record(fn, t)
	return t, nil
}

// mmap_anon(byte_len: u64) -> result<mmap<u8>, str>  — anonymous (no-file) mapping
func (c *checker) inferMmapAnon(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "mmap_anon() takes 1 argument: byte_len u64")
	}
	if _, err := c.checkExpr(e.Args[0], sc, TU64); err != nil {
		return nil, err
	}
	t := mmapResultType()
	c.record(fn, t)
	return t, nil
}

// mmap_deref(m: ref<mmap<T>>) -> ref<T>
func (c *checker) inferMmapDeref(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "mmap_deref() takes 1 argument: mmap<T> or ref<mmap<T>>")
	}
	argType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	inner := argType
	if ref, ok := argType.(*GenType); ok && ref.Con == "ref" && len(ref.Params) == 1 {
		inner = ref.Params[0]
	}
	gen, ok := inner.(*GenType)
	if !ok || gen.Con != "mmap" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "mmap_deref() requires mmap<T> or ref<mmap<T>>, got %s", argType)
	}
	t := &GenType{Con: "ref", Params: []Type{gen.Params[0]}}
	c.record(fn, t)
	return t, nil
}

// mmap_flush(m: ref<mmap<T>>) -> unit  effects(storage)
func (c *checker) inferMmapFlush(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "mmap_flush() takes 1 argument: mmap<T> or ref<mmap<T>>")
	}
	argType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	inner := argType
	if ref, ok := argType.(*GenType); ok && ref.Con == "ref" && len(ref.Params) == 1 {
		inner = ref.Params[0]
	}
	if gen, ok := inner.(*GenType); !ok || gen.Con != "mmap" {
		return nil, c.errorf(e.Args[0].Pos(), "mmap_flush() requires mmap<T> or ref<mmap<T>>, got %s", argType)
	}
	c.record(fn, TUnit)
	return TUnit, nil
}

// mmap_close(m: mmap<T>) -> unit  effects(storage)
func (c *checker) inferMmapClose(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "mmap_close() takes 1 argument: mmap<T>")
	}
	argType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := argType.(*GenType)
	if !ok || gen.Con != "mmap" || len(gen.Params) != 1 {
		return nil, c.errorf(e.Args[0].Pos(), "mmap_close() requires mmap<T>, got %s", argType)
	}
	_ = gen
	c.record(fn, TUnit)
	return TUnit, nil
}

// mmap_len(m: ref<mmap<T>>) -> u64
func (c *checker) inferMmapLen(e *parser.CallExpr, fn *parser.IdentExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "mmap_len() takes 1 argument: mmap<T> or ref<mmap<T>>")
	}
	argType, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	inner := argType
	if ref, ok := argType.(*GenType); ok && ref.Con == "ref" && len(ref.Params) == 1 {
		inner = ref.Params[0]
	}
	if gen, ok := inner.(*GenType); !ok || gen.Con != "mmap" {
		return nil, c.errorf(e.Args[0].Pos(), "mmap_len() requires mmap<T> or ref<mmap<T>>, got %s", argType)
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

// inferSecretCall handles secret(x) — wraps x in secret<T>; prevents I/O leakage.
func (c *checker) inferSecretCall(e *parser.CallExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "secret() takes 1 argument")
	}
	t, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	secretType := &GenType{Con: "secret", Params: []Type{t}}
	if fn, ok := e.Fn.(*parser.IdentExpr); ok {
		c.record(fn, secretType)
	}
	c.record(e, secretType)
	return secretType, nil
}

// inferRevealCall handles reveal(s) — explicitly unwraps secret<T> to T.
// This is an auditable operation: every reveal() call is visible in source.
func (c *checker) inferRevealCall(e *parser.CallExpr, sc *scope) (Type, error) {
	if len(e.Args) != 1 {
		return nil, c.errorf(e.LParen, "reveal() takes 1 argument")
	}
	t, err := c.checkExpr(e.Args[0], sc, nil)
	if err != nil {
		return nil, err
	}
	gen, ok := t.(*GenType)
	if !ok || gen.Con != "secret" || len(gen.Params) == 0 {
		return nil, c.errorf(e.LParen, "reveal() requires secret<T>, got %s", t)
	}
	inner := gen.Params[0]
	if fn, ok := e.Fn.(*parser.IdentExpr); ok {
		c.record(fn, inner)
	}
	c.record(e, inner)
	return inner, nil
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
	loopSc := newScope(sc)

	if s.Var2 != nil {
		// for k, v in map<K,V>
		gen, ok := collType.(*GenType)
		if !ok || gen.Con != "map" || len(gen.Params) != 2 {
			return c.errorf(s.InTok, "for k, v in ... requires map<K,V>, got %s", collType)
		}
		loopSc.define(s.Var.Lexeme, gen.Params[0])
		c.record(&parser.IdentExpr{Tok: s.Var}, gen.Params[0])
		loopSc.define(s.Var2.Lexeme, gen.Params[1])
		c.record(&parser.IdentExpr{Tok: *s.Var2}, gen.Params[1])
	} else {
		gen, ok := collType.(*GenType)
		if !ok || len(gen.Params) == 0 {
			return c.errorf(s.InTok, "for...in requires vec<T>, ring<T>, or set<T>, got %s", collType)
		}
		if gen.Con != "vec" && gen.Con != "ring" && gen.Con != "set" {
			return c.errorf(s.InTok, "for...in requires vec<T>, ring<T>, or set<T>, got %s", collType)
		}
		loopSc.define(s.Var.Lexeme, gen.Params[0])
		c.record(&parser.IdentExpr{Tok: s.Var}, gen.Params[0])
	}
	return c.checkBlock(s.Body, loopSc, retType)
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
	st, ok := c.lookupStruct(e.TypeName.Lexeme)
	if !ok {
		return nil, c.errorf(e.TypeName, "undefined struct %q", e.TypeName.Lexeme)
	}
	if err := c.checkModuleAccess(e.TypeName.Lexeme, e.TypeName); err != nil {
		return nil, err
	}

	// Struct update syntax: Point { ..base, x: 1 }
	// When Base is present, fields not listed are copied from the base expression.
	if e.Base != nil {
		baseType, err := c.checkExpr(e.Base, sc, st)
		if err != nil {
			return nil, err
		}
		if !baseType.Equals(st) {
			return nil, c.errorf(e.TypeName,
				"struct update base type %s does not match struct %s", baseType, st.Name)
		}
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
	// Without a base, all fields must be provided.
	if e.Base == nil {
		for name := range st.Fields {
			if !provided[name] {
				return nil, c.errorf(e.TypeName, "missing field %q in struct literal for %s", name, st.Name)
			}
		}
	}
	return st, nil
}

// ── New feature helpers ───────────────────────────────────────────────────────

// checkConstDecl type-checks a module-level const declaration and registers it.
func (c *checker) checkConstDecl(d *parser.ConstDecl) error {
	typ, err := c.resolveTypeExpr(d.Type)
	if err != nil {
		return err
	}
	// Type-check value in an empty scope (consts must be literals / constant exprs).
	valType, err := c.checkExpr(d.Value, newScope(nil), typ)
	if err != nil {
		return err
	}
	coerced, ok := Coerce(valType, typ)
	if !ok {
		return c.errorf(d.ConstTok, "const %s: cannot use %s as %s", d.Name.Lexeme, valType, typ)
	}
	c.exprTypes[d.Value] = coerced
	c.consts[d.Name.Lexeme] = typ
	c.constDecls = append(c.constDecls, d)
	return nil
}

// collectImplDecl registers all method signatures from an impl block.
func (c *checker) collectImplDecl(d *parser.ImplDecl) error {
	typeName := d.TypeName.Lexeme
	if c.methods[typeName] == nil {
		c.methods[typeName] = make(map[string]*FnType)
	}
	for _, m := range d.Methods {
		sig, err := c.buildFnSig(m)
		if err != nil {
			return err
		}
		mangledName := typeName + "_" + m.Name.Lexeme
		c.fnSigs[mangledName] = sig
		c.methods[typeName][m.Name.Lexeme] = sig
	}
	c.implDecls = append(c.implDecls, d)
	return nil
}

// collectTraitDecl registers a trait definition so its methods can be verified
// against impl-for blocks and used as bounds on generic type parameters.
func (c *checker) collectTraitDecl(d *parser.TraitDecl) error {
	def := &TraitDef{Name: d.Name.Lexeme, Methods: make(map[string]*FnType)}
	for _, m := range d.Methods {
		params := make([]Type, len(m.Params))
		for i, p := range m.Params {
			t, err := c.resolveTypeExprWithSelf(p.Type)
			if err != nil {
				return err
			}
			params[i] = t
		}
		ret, err := c.resolveTypeExprWithSelf(m.RetType)
		if err != nil {
			return err
		}
		def.Methods[m.Name.Lexeme] = &FnType{Params: params, Ret: ret}
	}
	c.traits[d.Name.Lexeme] = def
	c.traitDecls = append(c.traitDecls, d)
	return nil
}

// collectImplForDecl registers an impl-for block: records that the type
// implements the trait, and registers each method as a regular struct method
// (same mangling as impl — TypeName_methodName).
func (c *checker) collectImplForDecl(d *parser.ImplForDecl) error {
	typeName := d.TypeName.Lexeme
	traitName := d.TraitName.Lexeme
	// Verify the trait exists.
	if _, ok := c.traits[traitName]; !ok {
		return c.errorf(d.TraitName, "unknown trait %q", traitName)
	}
	if c.methods[typeName] == nil {
		c.methods[typeName] = make(map[string]*FnType)
	}
	for _, m := range d.Methods {
		sig, err := c.buildFnSig(m)
		if err != nil {
			return err
		}
		mangledName := typeName + "_" + m.Name.Lexeme
		c.fnSigs[mangledName] = sig
		c.methods[typeName][m.Name.Lexeme] = sig
	}
	// Mark the type as implementing the trait.
	if c.traitImpls[typeName] == nil {
		c.traitImpls[typeName] = make(map[string]bool)
	}
	c.traitImpls[typeName][traitName] = true
	c.implForDecls = append(c.implForDecls, d)
	return nil
}

// resolveTypeExprWithSelf resolves a type expression, treating "Self" as TSelf.
func (c *checker) resolveTypeExprWithSelf(te parser.TypeExpr) (Type, error) {
	if nt, ok := te.(*parser.NamedType); ok && nt.Name.Lexeme == "Self" {
		return TSelf, nil
	}
	return c.resolveTypeExpr(te)
}

// checkMethodBody type-checks the body of an impl method using its mangled sig.
func (c *checker) checkMethodBody(mangledName string, d *parser.FnDecl) error {
	c.beginFnTracking()
	sig, ok := c.fnSigs[mangledName]
	if !ok {
		return fmt.Errorf("internal: method sig %q not found", mangledName)
	}
	sc := newScope(nil)
	for i, p := range d.Params {
		sc.define(p.Name.Lexeme, sig.Params[i])
	}
	prev := c.curEffects
	c.curEffects = nil
	err := c.checkBlock(d.Body, sc, sig.Ret)
	c.flushUnusedWarnings()
	c.curEffects = prev
	return err
}

// tryMethodCall checks if a CallExpr is a method call (Fn is FieldExpr with struct receiver).
// Returns (returnType, nil) if it is a valid method call, (nil, nil) if not a method call,
// or (nil, err) if it is a method call but has a type error.
func (c *checker) tryMethodCall(e *parser.CallExpr, fe *parser.FieldExpr, sc *scope) (Type, error) {
	// Check receiver type (without recording, so we can check below).
	recvType, err := c.checkExpr(fe.Receiver, sc, nil)
	if err != nil {
		return nil, nil // not a method call if receiver fails
	}
	// Dereference ref/refmut
	baseType := recvType
	if gen, ok := baseType.(*GenType); ok && (gen.Con == "ref" || gen.Con == "refmut") && len(gen.Params) > 0 {
		baseType = gen.Params[0]
	}

	// Special built-in method on task<T>*: .join() -> result<T, str>
	if gen, ok := baseType.(*GenType); ok && gen.Con == "task" && len(gen.Params) == 1 {
		methodName := fe.Field.Lexeme
		if methodName != "join" {
			return nil, c.errorf(fe.Field, "task<%s> has no method %q (only .join())", gen.Params[0], methodName)
		}
		if len(e.Args) != 0 {
			return nil, c.errorf(fe.Field, "task.join() takes no arguments")
		}
		T := gen.Params[0]
		retT := &GenType{Con: "result", Params: []Type{T, TStr}}
		c.taskJoins[e] = T
		return c.record(e, retT), nil
	}

	st, ok := baseType.(*StructType)
	if !ok {
		return nil, nil // not a struct, not a method call
	}
	methodName := fe.Field.Lexeme
	methodMap, hasImpl := c.methods[st.Name]
	if !hasImpl {
		return nil, nil
	}
	sig, hasMethod := methodMap[methodName]
	if !hasMethod {
		return nil, nil
	}
	// It is a method call. Check arity: sig.Params includes self as first param.
	if len(e.Args)+1 != len(sig.Params) {
		return nil, c.errorf(fe.Field, "method %s.%s expects %d argument(s), got %d",
			st.Name, methodName, len(sig.Params)-1, len(e.Args))
	}
	// Check self type matches receiver.
	if _, ok2 := Coerce(recvType, sig.Params[0]); !ok2 {
		return nil, c.errorf(fe.Dot, "receiver type %s cannot be used as %s for method %s",
			recvType, sig.Params[0], methodName)
	}
	// Check remaining args.
	for i, arg := range e.Args {
		argType, err := c.checkExpr(arg, sc, sig.Params[i+1])
		if err != nil {
			return nil, err
		}
		coerced, ok2 := Coerce(argType, sig.Params[i+1])
		if !ok2 {
			return nil, c.errorf(arg.Pos(), "argument %d: cannot use %s as %s", i+1, argType, sig.Params[i+1])
		}
		c.exprTypes[arg] = coerced
	}
	// Record the mangled C name for the emitter.
	mangledName := st.Name + "_" + methodName
	c.methodCalls[e] = mangledName
	c.record(e, sig.Ret)
	return sig.Ret, nil
}

// inferVecLitExpr type-checks a vec literal [e0, e1, ...].
func (c *checker) inferVecLitExpr(e *parser.VecLitExpr, sc *scope, hint Type) (Type, error) {
	// Derive element hint from context if available.
	var elemHint Type
	if hint != nil {
		if gen, ok := hint.(*GenType); ok && gen.Con == "vec" && len(gen.Params) == 1 {
			elemHint = gen.Params[0]
		}
	}
	if len(e.Elems) == 0 {
		if elemHint != nil {
			return &GenType{Con: "vec", Params: []Type{elemHint}}, nil
		}
		return nil, c.errorf(e.LBracket, "cannot infer element type of empty vec literal; provide a type annotation")
	}
	elemType, err := c.checkExpr(e.Elems[0], sc, elemHint)
	if err != nil {
		return nil, err
	}
	for _, elem := range e.Elems[1:] {
		et, err := c.checkExpr(elem, sc, elemType)
		if err != nil {
			return nil, err
		}
		unified, ok := Unify(elemType, et)
		if !ok {
			return nil, c.errorf(elem.Pos(), "vec literal element type mismatch: got %s, expected %s", et, elemType)
		}
		elemType = unified
		c.exprTypes[elem] = elemType
	}
	// Resolve sentinel types to defaults.
	if elemType == TIntLit {
		elemType = TI64
	} else if elemType == TFloatLit {
		elemType = TF64
	}
	c.exprTypes[e.Elems[0]] = elemType
	return &GenType{Con: "vec", Params: []Type{elemType}}, nil
}

// inferTupleLitExpr type-checks a tuple literal (e0, e1, ...).
func (c *checker) inferTupleLitExpr(e *parser.TupleLitExpr, sc *scope, hint Type) (Type, error) {
	elems := make([]Type, len(e.Elems))
	for i, elem := range e.Elems {
		var elemHint Type
		if hint != nil {
			if tt, ok := hint.(*TupleType); ok && i < len(tt.Elems) {
				elemHint = tt.Elems[i]
			}
		}
		t, err := c.checkExpr(elem, sc, elemHint)
		if err != nil {
			return nil, err
		}
		elems[i] = t
	}
	return &TupleType{Elems: elems}, nil
}

// checkTupleDestructureStmt type-checks: let [mut] (a, b, ...) = expr
func (c *checker) checkTupleDestructureStmt(s *parser.TupleDestructureStmt, sc *scope) error {
	valType, err := c.checkExpr(s.Value, sc, nil)
	if err != nil {
		return err
	}
	tt, ok := valType.(*TupleType)
	if !ok {
		return c.errorf(s.LetTok, "tuple destructure requires a tuple type, got %s", valType)
	}
	if len(s.Names) != len(tt.Elems) {
		return c.errorf(s.LetTok,
			"tuple destructure: %d names but tuple has %d elements", len(s.Names), len(tt.Elems))
	}
	for i, name := range s.Names {
		if s.Mut {
			sc.defineMut(name.Lexeme, tt.Elems[i])
		} else {
			sc.define(name.Lexeme, tt.Elems[i])
		}
	}
	return nil
}

// inferSpawnExpr type-checks a spawn { body } expression and returns task<T>.
// T is inferred from the first return statement in the body; unit if none.
func (c *checker) inferSpawnExpr(e *parser.SpawnExpr, sc *scope) (Type, error) {
	// Infer result type T from the first return statement in the body.
	T := Type(TUnit)
	for _, stmt := range e.Body.Stmts {
		if rs, ok := stmt.(*parser.ReturnStmt); ok {
			if rs.Value != nil && !isUnitIdent(rs.Value) {
				t, err := c.checkExpr(rs.Value, sc, nil)
				if err != nil {
					return nil, err
				}
				if t == TIntLit {
					t = TI64
				} else if t == TFloatLit {
					t = TF64
				}
				T = t
			}
			break
		}
	}

	// Type-check the body as a block returning T.
	bodySc := newScope(sc)
	if err := c.checkBlock(e.Body, bodySc, T); err != nil {
		return nil, err
	}

	// Register the spawn with capture analysis.
	c.spawnCount++
	name := fmt.Sprintf("_cnd_spawn_%d", c.spawnCount)

	// Collect names local to the spawn body.
	localNames := make(map[string]bool)
	collectLetNames(e.Body.Stmts, localNames)

	// Walk body collecting free variable references.
	seen := make(map[string]bool)
	var captures []string
	var captureTypes []Type
	var captureByRef []bool
	for _, stmt := range e.Body.Stmts {
		walkIdentsInStmt(stmt, func(n string) {
			if seen[n] || localNames[n] {
				return
			}
			if _, isFn := c.fnSigs[n]; isFn {
				return
			}
			if info, ok := sc.lookupInfo(n); ok {
				seen[n] = true
				captures = append(captures, n)
				captureTypes = append(captureTypes, info.typ)
				captureByRef = append(captureByRef, info.mutable)
			}
		})
	}

	info := &SpawnInfo{
		Node:         e,
		Name:         name,
		ResultType:   T,
		Captures:     captures,
		CaptureTypes: captureTypes,
		CaptureByRef: captureByRef,
	}
	c.spawns = append(c.spawns, info)

	return c.record(e, &GenType{Con: "task", Params: []Type{T}}), nil
}

// isUnitIdent reports whether an expression is the identifier "unit".
func isUnitIdent(e parser.Expr) bool {
	ident, ok := e.(*parser.IdentExpr)
	return ok && ident.Tok.Lexeme == "unit"
}
