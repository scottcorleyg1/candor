// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg1/candor

package typeck

import "strings"

// Type is the resolved type of a Candor expression or declaration.
type Type interface {
	String() string
	Equals(other Type) bool
}

// ── Primitive types ───────────────────────────────────────────────────────────

// Prim is a built-in primitive type (u32, bool, unit, etc.).
type Prim struct{ name string }

func (p *Prim) String() string          { return p.name }
func (p *Prim) Equals(other Type) bool { o, ok := other.(*Prim); return ok && o.name == p.name }

// Canonical primitive type singletons.
var (
	TUnit  = &Prim{"unit"}
	TNever = &Prim{"never"}
	TBool  = &Prim{"bool"}
	TStr   = &Prim{"str"}

	TI8   = &Prim{"i8"}
	TI16  = &Prim{"i16"}
	TI32  = &Prim{"i32"}
	TI64  = &Prim{"i64"}
	TI128 = &Prim{"i128"}

	TU8   = &Prim{"u8"}
	TU16  = &Prim{"u16"}
	TU32  = &Prim{"u32"}
	TU64  = &Prim{"u64"}
	TU128 = &Prim{"u128"}

	TF32 = &Prim{"f32"}
	TF64 = &Prim{"f64"}

	// Sentinels for unresolved literals. Never appear in a fully-checked AST.
	TIntLit   = &Prim{"<int_lit>"}
	TFloatLit = &Prim{"<float_lit>"}
)

// BuiltinTypes maps source-level type names to their canonical Prim.
var BuiltinTypes = map[string]Type{
	"unit": TUnit, "never": TNever, "bool": TBool, "str": TStr,
	"i8": TI8, "i16": TI16, "i32": TI32, "i64": TI64, "i128": TI128,
	"u8": TU8, "u16": TU16, "u32": TU32, "u64": TU64, "u128": TU128,
	"f32": TF32, "f64": TF64,
}

// ── Generic / parameterised types ────────────────────────────────────────────

// GenType is a parameterised type: ref<T>, option<T>, result<T,E>, vec<T>, etc.
type GenType struct {
	Con    string // constructor name
	Params []Type
}

func (g *GenType) String() string {
	if len(g.Params) == 0 {
		return g.Con
	}
	var sb strings.Builder
	sb.WriteString(g.Con)
	sb.WriteByte('<')
	for i, p := range g.Params {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(p.String())
	}
	sb.WriteByte('>')
	return sb.String()
}

func (g *GenType) Equals(other Type) bool {
	o, ok := other.(*GenType)
	if !ok || o.Con != g.Con || len(o.Params) != len(g.Params) {
		return false
	}
	for i := range g.Params {
		if !g.Params[i].Equals(o.Params[i]) {
			return false
		}
	}
	return true
}

// ── Function type ─────────────────────────────────────────────────────────────

// FnType is a function signature type: fn(T, U) -> V
type FnType struct {
	Params []Type
	Ret    Type
}

func (f *FnType) String() string {
	var sb strings.Builder
	sb.WriteString("fn(")
	for i, p := range f.Params {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(p.String())
	}
	sb.WriteString(") -> ")
	sb.WriteString(f.Ret.String())
	return sb.String()
}

func (f *FnType) Equals(other Type) bool {
	o, ok := other.(*FnType)
	if !ok || len(o.Params) != len(f.Params) || !f.Ret.Equals(o.Ret) {
		return false
	}
	for i := range f.Params {
		if !f.Params[i].Equals(o.Params[i]) {
			return false
		}
	}
	return true
}

// ── Struct type ───────────────────────────────────────────────────────────────

// StructType is a named user-defined struct.
type StructType struct {
	Name   string
	Fields map[string]Type
}

func (s *StructType) String() string          { return s.Name }
func (s *StructType) Equals(other Type) bool {
	o, ok := other.(*StructType)
	return ok && o.Name == s.Name
}

// ── Enum type ─────────────────────────────────────────────────────────────────

// EnumVariantDef describes one variant of a user-defined enum.
type EnumVariantDef struct {
	Name   string
	Fields []Type // empty for unit variants
	Tag    int    // position index, used as discriminant in C
}

// EnumType is a user-defined sum type declared with `enum`.
type EnumType struct {
	Name     string
	Variants []*EnumVariantDef    // ordered — index == tag
	ByName   map[string]*EnumVariantDef
}

func (e *EnumType) String() string { return e.Name }
func (e *EnumType) Equals(other Type) bool {
	o, ok := other.(*EnumType)
	return ok && o.Name == e.Name
}

// ── Type predicates ───────────────────────────────────────────────────────────

func IsIntType(t Type) bool {
	p, ok := t.(*Prim)
	if !ok {
		return false
	}
	switch p.name {
	case "i8", "i16", "i32", "i64", "i128", "u8", "u16", "u32", "u64", "u128":
		return true
	}
	return false
}

func IsFloatType(t Type) bool {
	p, ok := t.(*Prim)
	if !ok {
		return false
	}
	return p.name == "f32" || p.name == "f64"
}

func IsNumericType(t Type) bool { return IsIntType(t) || IsFloatType(t) }

// ── Type coercion and unification ─────────────────────────────────────────────

// Coerce returns (resolved, true) if src is assignable to dst.
// Integer/float literal sentinels coerce to any matching concrete type.
// never coerces to anything (arms of type never don't constrain the result type).
func Coerce(src, dst Type) (Type, bool) {
	if src.Equals(dst) {
		return dst, true
	}
	if src == TIntLit && IsIntType(dst) {
		return dst, true
	}
	if src == TFloatLit && IsFloatType(dst) {
		return dst, true
	}
	if src.Equals(TNever) {
		return dst, true
	}
	// refmut<T> coerces to ref<T> — a mutable reference satisfies a read-only reference parameter
	if fgen, ok := src.(*GenType); ok && fgen.Con == "refmut" && len(fgen.Params) == 1 {
		if tgen, ok := dst.(*GenType); ok && tgen.Con == "ref" && len(tgen.Params) == 1 {
			if fgen.Params[0].Equals(tgen.Params[0]) {
				return dst, true
			}
		}
	}
	return nil, false
}

// Unify returns the single type that both a and b can be resolved to.
// Handles literal sentinels: TIntLit unifies with any integer type.
// Two TIntLit literals unify to i64 (Candor's default integer type).
func Unify(a, b Type) (Type, bool) {
	if a.Equals(b) {
		return a, true
	}
	if a == TIntLit && b == TIntLit {
		return TI64, true // default integer type
	}
	if a == TFloatLit && b == TFloatLit {
		return TF64, true // default float type
	}
	if a == TIntLit && IsIntType(b) {
		return b, true
	}
	if b == TIntLit && IsIntType(a) {
		return a, true
	}
	if a == TFloatLit && IsFloatType(b) {
		return b, true
	}
	if b == TFloatLit && IsFloatType(a) {
		return a, true
	}
	return nil, false
}

// UnifyNumeric is like Unify but additionally requires the result to be numeric.
func UnifyNumeric(a, b Type) (Type, bool) {
	t, ok := Unify(a, b)
	if !ok {
		return nil, false
	}
	if !IsNumericType(t) {
		return nil, false
	}
	return t, true
}
