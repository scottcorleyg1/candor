// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/candor-core/candor

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

	TF16  = &Prim{"f16"}
	TBF16 = &Prim{"bf16"}
	TF32  = &Prim{"f32"}
	TF64  = &Prim{"f64"}

	// Sentinels for unresolved literals. Never appear in a fully-checked AST.
	TIntLit   = &Prim{"<int_lit>"}
	TFloatLit = &Prim{"<float_lit>"}
)

// BuiltinTypes maps source-level type names to their canonical Prim.
var BuiltinTypes = map[string]Type{
	"unit": TUnit, "never": TNever, "bool": TBool, "str": TStr,
	"i8": TI8, "i16": TI16, "i32": TI32, "i64": TI64, "i128": TI128,
	"u8": TU8, "u16": TU16, "u32": TU32, "u64": TU64, "u128": TU128,
	"f16": TF16, "bf16": TBF16, "f32": TF32, "f64": TF64,
}

// ── Type variable ─────────────────────────────────────────────────────────────

// TypeVar is a generic type parameter placeholder (e.g. T, R).
// Only exists transiently during monomorphization; never appears in final Result.
type TypeVar struct{ Name string }

func (t TypeVar) String() string       { return t.Name }
func (t TypeVar) Equals(other Type) bool {
	o, ok := other.(TypeVar)
	return ok && o.Name == t.Name
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

// ── Capability type ───────────────────────────────────────────────────────────

// CapabilityType is the zero-size proof token for a named capability.
// cap<Admin> resolves to GenType{Con:"cap", Params:[CapabilityType{Name:"Admin"}]}.
type CapabilityType struct{ Name string }

func (t *CapabilityType) String() string          { return t.Name }
func (t *CapabilityType) Equals(other Type) bool {
	o, ok := other.(*CapabilityType)
	return ok && o.Name == t.Name
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

// ── Tuple type ────────────────────────────────────────────────────────────────

// TupleType is an anonymous product type: (T0, T1, ...).
type TupleType struct {
	Elems []Type
}

func (t *TupleType) String() string {
	var sb strings.Builder
	sb.WriteByte('(')
	for i, e := range t.Elems {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(e.String())
	}
	sb.WriteByte(')')
	return sb.String()
}

func (t *TupleType) Equals(other Type) bool {
	o, ok := other.(*TupleType)
	if !ok || len(o.Elems) != len(t.Elems) {
		return false
	}
	for i := range t.Elems {
		if !t.Elems[i].Equals(o.Elems[i]) {
			return false
		}
	}
	return true
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

// TraitDef holds a collected trait declaration — its name and method signatures.
// The method signatures use a special *SelfType placeholder for the Self type.
type TraitDef struct {
	Name    string
	Methods map[string]*FnType // method name → signature (Self represented as SelfType)
}

// SelfType is a placeholder used inside trait method signatures to represent
// the implementing type. It is substituted with the concrete type during
// impl-for checking.
type SelfType struct{}

func (s *SelfType) String() string           { return "Self" }
func (s *SelfType) Equals(other Type) bool   { _, ok := other.(*SelfType); return ok }

// TSelf is the singleton SelfType placeholder.
var TSelf Type = &SelfType{}

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
	switch p.name {
	case "f16", "bf16", "f32", "f64":
		return true
	}
	return false
}

func IsNumericType(t Type) bool { return IsIntType(t) || IsFloatType(t) }

// numericRank returns an ordering for lossless widening:
//
//	i8(0) < i16(1) < i32(2) < i64(3) < i128(4)
//	u8(10) < u16(11) < u32(12) < u64(13) < u128(14)
//	f16(20) < bf16(21) < f32(22) < f64(23)
//
// Note: f16 and bf16 are in the same rank family (20s) but bf16 is not
// losslessly wider than f16 (different mantissa/exponent split), so widening
// between them is not implicit. They share the family index so IsNumericWider
// correctly rejects cross-format widening.
//
// Returns -1 for non-widening types.
func numericRank(t Type) int {
	p, ok := t.(*Prim)
	if !ok {
		return -1
	}
	switch p.name {
	case "i8":   return 0
	case "i16":  return 1
	case "i32":  return 2
	case "i64":  return 3
	case "i128": return 4
	case "u8":   return 10
	case "u16":  return 11
	case "u32":  return 12
	case "u64":  return 13
	case "u128": return 14
	case "f16":  return 20
	case "bf16": return 21
	case "f32":  return 22
	case "f64":  return 23
	}
	return -1
}

// IsNumericWider returns true if src can be implicitly widened to dst:
// same family (signed int / unsigned int / float) and strictly narrower rank.
func IsNumericWider(src, dst Type) bool {
	sr := numericRank(src)
	dr := numericRank(dst)
	if sr < 0 || dr < 0 {
		return false
	}
	return (sr / 10) == (dr / 10) && sr < dr
}

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
	// Implicit numeric widening: i8→i16→i32→i64→i128, u8→…→u128, f32→f64.
	if IsNumericWider(src, dst) {
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
	// bare none (GenType{Con:"none"}) coerces to any option<T>
	if fgen, ok := src.(*GenType); ok && fgen.Con == "none" {
		if tgen, ok := dst.(*GenType); ok && tgen.Con == "option" {
			return tgen, true
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
