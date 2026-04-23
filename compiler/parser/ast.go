// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/candor-core/candor

package parser

import "github.com/candor-core/candor/compiler/lexer"

// Node is the base interface for all AST nodes.
// Every node records its opening token for source position reporting.
type Node interface {
	Pos() lexer.Token
}

// ── File ─────────────────────────────────────────────────────────────────────

// File is the root of the AST for a parsed .cnd source file.
type File struct {
	Name  string
	Decls []Decl
}

// ── Declarations ─────────────────────────────────────────────────────────────

type Decl interface {
	Node
	declNode()
}

// ModuleDecl: module name
// Declares the module this file belongs to. At most one per file, conventionally first.
type ModuleDecl struct {
	ModuleTok lexer.Token
	Name      lexer.Token
}

func (d *ModuleDecl) Pos() lexer.Token { return d.ModuleTok }
func (d *ModuleDecl) declNode()        {}

// UseDecl: use foo  or  use foo::Bar  or  use foo::bar::Baz
// Declares a dependency on names from another module.
// Path holds each segment in order; segments are separated by '::'.
type UseDecl struct {
	UseTok lexer.Token
	Path   []lexer.Token // e.g. [foo, Bar] for "use foo::Bar"
}

func (d *UseDecl) Pos() lexer.Token { return d.UseTok }
func (d *UseDecl) declNode()        {}

// FnDecl: fn name[<T, U>](params) -> RetType [pure | effects(...) | cap(...)] { body }
type FnDecl struct {
	FnTok      lexer.Token
	Name       lexer.Token
	TypeParams []lexer.Token       // non-nil when generic: fn foo<T, U>(...)
	TypeBounds map[string][]string // type param name → required trait names, e.g. {"T": ["Display"]}
	Params     []Param
	RetType    TypeExpr
	Effects    *EffectsAnnotation // nil = no annotation (unchecked)
	Contracts  []ContractClause   // requires/ensures; nil or empty = none
	Body       *BlockStmt
	Directives    []string          // directive words immediately preceding this fn, e.g. ["test"]
	DirectiveArgs map[string]string // optional string argument per directive, e.g. {"mcp_tool": "Search the web"}
}

func (d *FnDecl) Pos() lexer.Token { return d.FnTok }
func (d *FnDecl) declNode()        {}

// Param: name: Type
type Param struct {
	Name lexer.Token
	Type TypeExpr
}

// StructDecl: struct Name { fields }
type StructDecl struct {
	StructTok  lexer.Token
	Name       lexer.Token
	Fields     []Field
	Directives []string // directive words immediately preceding this struct, e.g. ["export_json"]
}

func (d *StructDecl) Pos() lexer.Token { return d.StructTok }
func (d *StructDecl) declNode()        {}

// Field: name: Type,
type Field struct {
	Name lexer.Token
	Type TypeExpr
}

// EnumDecl: enum Name { Variant, Variant(Type), ... }
// User-defined sum type. Variants may carry zero or more typed fields.
type EnumDecl struct {
	EnumTok  lexer.Token
	Name     lexer.Token
	Variants []EnumVariant
}

func (d *EnumDecl) Pos() lexer.Token { return d.EnumTok }
func (d *EnumDecl) declNode()        {}

// EnumVariant: Name  or  Name(Type, ...)

// CHeaderDecl: #c_header "path/to/header.h"
// File-scope directive: instructs the compiler to parse the C header and
// synthesize extern fn stubs for all recognised function prototypes.
// Path is relative to the directory containing the .cnd source file.
type CHeaderDecl struct {
	Tok  lexer.Token
	Path string // header file path as written in the directive
}

func (d *CHeaderDecl) Pos() lexer.Token { return d.Tok }
func (d *CHeaderDecl) declNode()        {}

// ExternFnDecl: extern fn name(params) -> ret [effects]
// No body — the function is defined in C.
type ExternFnDecl struct {
	ExternTok lexer.Token
	Name      lexer.Token
	Params    []Param
	RetType   TypeExpr
	Effects   *EffectsAnnotation // optional
}

func (d *ExternFnDecl) Pos() lexer.Token { return d.ExternTok }
func (d *ExternFnDecl) declNode()        {}

// ConstDecl: const NAME: Type = expr
// Module-level compile-time constant.
type ConstDecl struct {
	ConstTok lexer.Token
	Name     lexer.Token
	Type     TypeExpr
	Value    Expr
}

func (d *ConstDecl) Pos() lexer.Token { return d.ConstTok }
func (d *ConstDecl) declNode()        {}

// ImplDecl: impl StructName { fn method(...) -> R { body } ... }
// Associates methods with a named struct type.
type ImplDecl struct {
	ImplTok  lexer.Token
	TypeName lexer.Token
	Methods  []*FnDecl
}

func (d *ImplDecl) Pos() lexer.Token { return d.ImplTok }
func (d *ImplDecl) declNode()        {}

// CapabilityDecl: cap Name
// Declares a named capability token type. The type cap<Name> is a zero-size
// proof token that a function has been granted the named capability.
type CapabilityDecl struct {
	CapTok lexer.Token
	Name   lexer.Token
}

func (d *CapabilityDecl) Pos() lexer.Token { return d.CapTok }
func (d *CapabilityDecl) declNode()        {}

// TraitDecl: trait Name { fn method(self: ref<Self>, ...) -> RetType }
// Declares an interface that types can implement.
type TraitDecl struct {
	TraitTok lexer.Token
	Name     lexer.Token
	Methods  []*TraitMethod
}

func (d *TraitDecl) Pos() lexer.Token { return d.TraitTok }
func (d *TraitDecl) declNode()        {}

// TraitMethod is a method signature in a trait declaration (no body).
type TraitMethod struct {
	Name    lexer.Token
	Params  []Param
	RetType TypeExpr
}

// ImplForDecl: impl TraitName for TypeName { fn method(...) -> R { body } }
// Implements a trait for a concrete type.
type ImplForDecl struct {
	ImplTok   lexer.Token
	TraitName lexer.Token
	TypeName  lexer.Token
	Methods   []*FnDecl
}

func (d *ImplForDecl) Pos() lexer.Token { return d.ImplTok }
func (d *ImplForDecl) declNode()        {}

type EnumVariant struct {
	Name   lexer.Token
	Fields []TypeExpr // empty for unit variants
}

// ── Statements ───────────────────────────────────────────────────────────────

type Stmt interface {
	Node
	stmtNode()
}

// BlockStmt: { stmts }
type BlockStmt struct {
	LBrace lexer.Token
	Stmts  []Stmt
}

func (s *BlockStmt) Pos() lexer.Token { return s.LBrace }
func (s *BlockStmt) stmtNode()        {}

// LetStmt: let [mut] name [: Type] = value
type LetStmt struct {
	LetTok  lexer.Token
	Mut     bool
	Name    lexer.Token
	TypeAnn TypeExpr // nil when type is inferred
	Value   Expr
}

func (s *LetStmt) Pos() lexer.Token { return s.LetTok }
func (s *LetStmt) stmtNode()        {}

// ReturnStmt: return [value]
type ReturnStmt struct {
	ReturnTok lexer.Token
	Value     Expr // nil for bare return
}

func (s *ReturnStmt) Pos() lexer.Token { return s.ReturnTok }
func (s *ReturnStmt) stmtNode()        {}

// ExprStmt: an expression used as a statement (e.g. a function call)
type ExprStmt struct {
	X Expr
}

func (s *ExprStmt) Pos() lexer.Token { return s.X.Pos() }
func (s *ExprStmt) stmtNode()        {}

// IfStmt: if cond { then } [else { else }]
type IfStmt struct {
	IfTok lexer.Token
	Cond  Expr
	Then  *BlockStmt
	Else  Stmt // *IfStmt (else if), *BlockStmt (else), or nil
}

func (s *IfStmt) Pos() lexer.Token { return s.IfTok }
func (s *IfStmt) stmtNode()        {}

// LoopStmt: loop { body }
type LoopStmt struct {
	LoopTok lexer.Token
	Body    *BlockStmt
}

func (s *LoopStmt) Pos() lexer.Token { return s.LoopTok }
func (s *LoopStmt) stmtNode()        {}

// BreakStmt: break
type BreakStmt struct {
	BreakTok lexer.Token
}

func (s *BreakStmt) Pos() lexer.Token { return s.BreakTok }
func (s *BreakStmt) stmtNode()        {}

// ContinueStmt: continue
type ContinueStmt struct {
	ContinueTok lexer.Token
}

func (s *ContinueStmt) Pos() lexer.Token { return s.ContinueTok }
func (s *ContinueStmt) stmtNode()        {}

// WhileStmt: while cond { body }
type WhileStmt struct {
	WhileTok lexer.Token
	Cond     Expr
	Body     *BlockStmt
}

func (s *WhileStmt) Pos() lexer.Token { return s.WhileTok }
func (s *WhileStmt) stmtNode()        {}

// ForStmt: for name in collection { body }
//          for key, val in map    { body }  (map iteration)
// Collection must be vec<T>, ring<T>, or map<K,V>; names are bound to element types.
type ForStmt struct {
	ForTok     lexer.Token
	Var        lexer.Token
	Var2       *lexer.Token // non-nil for map: for k, v in m
	InTok      lexer.Token
	Collection Expr
	Body       *BlockStmt
}

func (s *ForStmt) Pos() lexer.Token { return s.ForTok }
func (s *ForStmt) stmtNode()        {}

// TupleDestructureStmt: let [mut] (a, b, ...) = expr
// Binds each tuple element to a separate variable.
type TupleDestructureStmt struct {
	LetTok lexer.Token
	Mut    bool
	Names  []lexer.Token // bound variable names, in order
	Value  Expr
}

func (s *TupleDestructureStmt) Pos() lexer.Token { return s.LetTok }
func (s *TupleDestructureStmt) stmtNode()        {}

// AssignStmt: name = value  (requires mutable binding)
type AssignStmt struct {
	Name  lexer.Token
	Eq    lexer.Token
	Value Expr
}

func (s *AssignStmt) Pos() lexer.Token { return s.Name }
func (s *AssignStmt) stmtNode()        {}

// FieldAssignStmt: receiver.field = value  (receiver must be mutable)
type FieldAssignStmt struct {
	Target *FieldExpr
	Eq     lexer.Token
	Value  Expr
}

func (s *FieldAssignStmt) Pos() lexer.Token { return s.Target.Pos() }
func (s *FieldAssignStmt) stmtNode()        {}

// IndexAssignStmt: collection[index] = value
type IndexAssignStmt struct {
	Target *IndexExpr
	Eq     lexer.Token
	Value  Expr
}

func (s *IndexAssignStmt) Pos() lexer.Token { return s.Target.Pos() }
func (s *IndexAssignStmt) stmtNode()        {}

// ── Expressions ──────────────────────────────────────────────────────────────

type Expr interface {
	Node
	exprNode()
}

// Literals
type IntLitExpr    struct{ Tok lexer.Token }
type FloatLitExpr  struct{ Tok lexer.Token }
type StringLitExpr struct{ Tok lexer.Token }
type BoolLitExpr   struct{ Tok lexer.Token }

func (e *IntLitExpr)    Pos() lexer.Token { return e.Tok }
func (e *IntLitExpr)    exprNode()        {}
func (e *FloatLitExpr)  Pos() lexer.Token { return e.Tok }
func (e *FloatLitExpr)  exprNode()        {}
func (e *StringLitExpr) Pos() lexer.Token { return e.Tok }
func (e *StringLitExpr) exprNode()        {}
func (e *BoolLitExpr)   Pos() lexer.Token { return e.Tok }
func (e *BoolLitExpr)   exprNode()        {}

// IdentExpr: a name — variable, type-as-value (unit), built-in (ok, err, some, none)
type IdentExpr struct {
	Tok lexer.Token
}

func (e *IdentExpr) Pos() lexer.Token { return e.Tok }
func (e *IdentExpr) exprNode()        {}

// BinaryExpr: left op right
type BinaryExpr struct {
	Left  Expr
	Op    lexer.Token
	Right Expr
}

func (e *BinaryExpr) Pos() lexer.Token { return e.Left.Pos() }
func (e *BinaryExpr) exprNode()        {}

// UnaryExpr: op operand  (prefix ! not -  or prefix &)
type UnaryExpr struct {
	Op      lexer.Token
	Operand Expr
}

func (e *UnaryExpr) Pos() lexer.Token { return e.Op }
func (e *UnaryExpr) exprNode()        {}

// CallExpr: fn(args)
type CallExpr struct {
	Fn     Expr
	LParen lexer.Token
	Args   []Expr
}

func (e *CallExpr) Pos() lexer.Token { return e.Fn.Pos() }
func (e *CallExpr) exprNode()        {}

// FieldExpr: receiver.field
type FieldExpr struct {
	Receiver Expr
	Dot      lexer.Token
	Field    lexer.Token
}

func (e *FieldExpr) Pos() lexer.Token { return e.Receiver.Pos() }
func (e *FieldExpr) exprNode()        {}

// IndexExpr: collection[index]
type IndexExpr struct {
	Collection Expr
	LBracket   lexer.Token
	Index      Expr
}

func (e *IndexExpr) Pos() lexer.Token { return e.Collection.Pos() }
func (e *IndexExpr) exprNode()        {}

// MustExpr: expr must { arms }
// The operand is a result<T,E>. Each arm handles one variant.
type MustExpr struct {
	X       Expr
	MustTok lexer.Token
	Arms    []MustArm
}

func (e *MustExpr) Pos() lexer.Token { return e.X.Pos() }
func (e *MustExpr) exprNode()        {}

// MustArm: pattern => body
type MustArm struct {
	Pattern Expr
	Arrow   lexer.Token
	Body    Expr
}

// MatchExpr: match expr { arms }
// Arms reuse the MustArm type.
type MatchExpr struct {
	MatchTok lexer.Token
	X        Expr
	Arms     []MustArm
}

func (e *MatchExpr) Pos() lexer.Token { return e.MatchTok }
func (e *MatchExpr) exprNode()        {}

// ReturnExpr wraps `return value` when it appears inside a must{} arm body.
// Its type is `never` — it exits the enclosing function, not the arm.
type ReturnExpr struct {
	ReturnTok lexer.Token
	Value     Expr
}

func (e *ReturnExpr) Pos() lexer.Token { return e.ReturnTok }
func (e *ReturnExpr) exprNode()        {}

// BreakExpr wraps `break` when it appears inside a must{} arm body.
// Its type is `never` — it exits the enclosing loop, not the arm.
type BreakExpr struct {
	BreakTok lexer.Token
}

func (e *BreakExpr) Pos() lexer.Token { return e.BreakTok }
func (e *BreakExpr) exprNode()        {}

// BlockExpr: { stmts... }
// Used as the arm body in multi-statement match/must arms.
// It implements Expr so it can be used directly as a MustArm.Body.
// The block's value is unit; side effects are the stmts inside.
type BlockExpr struct {
	LBrace lexer.Token
	Stmts  []Stmt
	RBrace lexer.Token
}

func (e *BlockExpr) Pos() lexer.Token { return e.LBrace }
func (e *BlockExpr) exprNode()        {}

// PathExpr: Head::Tail — used for enum variant construction and patterns.
// e.g. Shape::Circle, Token::EOF
type PathExpr struct {
	Head lexer.Token // enum type name
	Sep  lexer.Token // ::
	Tail lexer.Token // variant name
}

func (e *PathExpr) Pos() lexer.Token { return e.Head }
func (e *PathExpr) exprNode()        {}

// StructLitExpr: TypeName { field: value, ... }
// When Base is non-nil, this is a struct update expression: TypeName { ..base, field: val, ... }
// Fields not listed in Fields are copied from Base.
type StructLitExpr struct {
	TypeName lexer.Token
	Base     Expr       // non-nil for struct update: ..base
	Fields   []FieldInit
}

func (e *StructLitExpr) Pos() lexer.Token { return e.TypeName }
func (e *StructLitExpr) exprNode()        {}

// FieldInit: name: value (inside a struct literal)
type FieldInit struct {
	Name  lexer.Token
	Colon lexer.Token
	Value Expr
}

// LambdaExpr: fn(params) -> RetType { body }
// An anonymous function literal in expression position.
type LambdaExpr struct {
	FnTok   lexer.Token
	Params  []Param
	RetType TypeExpr
	Body    *BlockStmt
}

func (e *LambdaExpr) Pos() lexer.Token { return e.FnTok }
func (e *LambdaExpr) exprNode()        {}

// SpawnExpr: spawn { stmts }
// Creates a task<T> that runs stmts concurrently in a new thread.
// The block must contain a `return expr` to produce the value T.
// Captured outer variables are copied into the task context.
type SpawnExpr struct {
	SpawnTok lexer.Token
	Body     *BlockStmt
}

func (e *SpawnExpr) Pos() lexer.Token { return e.SpawnTok }
func (e *SpawnExpr) exprNode()        {}

// PropagateExpr: expr? — extract ok(T) or early-return err(E) from enclosing fn.
// The enclosing function must return result<T2, E>.
type PropagateExpr struct {
	X           Expr
	QuestionTok lexer.Token
}

func (e *PropagateExpr) Pos() lexer.Token { return e.QuestionTok }
func (e *PropagateExpr) exprNode()        {}

// PipeExpr: expr |> fn — left-to-right function application.
// Desugars to fn(expr). Left-associative: a |> f |> g == g(f(a)).
type PipeExpr struct {
	X       Expr
	PipeTok lexer.Token
	Fn      Expr
}

func (e *PipeExpr) Pos() lexer.Token { return e.PipeTok }
func (e *PipeExpr) exprNode()        {}

// CastExpr: expr as Type — explicit numeric cast.
type CastExpr struct {
	X      Expr
	AsTok  lexer.Token
	Target TypeExpr
}

func (e *CastExpr) Pos() lexer.Token { return e.X.Pos() }
func (e *CastExpr) exprNode()        {}

// VecLitExpr: [expr, expr, ...] — vec literal.
type VecLitExpr struct {
	LBracket lexer.Token
	Elems    []Expr
	RBracket lexer.Token
}

func (e *VecLitExpr) Pos() lexer.Token { return e.LBracket }
func (e *VecLitExpr) exprNode()        {}

// TupleLitExpr: (expr, expr, ...) — tuple literal (2+ elements).
type TupleLitExpr struct {
	LParen lexer.Token
	Elems  []Expr
}

func (e *TupleLitExpr) Pos() lexer.Token { return e.LParen }
func (e *TupleLitExpr) exprNode()        {}

// OldExpr: old(expr) — valid only in ensures clauses.
// Evaluates expr using the parameter values at function entry.
type OldExpr struct {
	OldTok lexer.Token
	X      Expr
}

func (e *OldExpr) Pos() lexer.Token { return e.OldTok }
func (e *OldExpr) exprNode()        {}

// ForallExpr: forall x in collection : pred
// A boolean expression that is true iff pred holds for every element of collection.
// collection must be vec<T> or ring<T>; x is bound in pred with type T.
type ForallExpr struct {
	ForallTok  lexer.Token
	Var        lexer.Token
	Collection Expr
	Pred       Expr
}

func (e *ForallExpr) Pos() lexer.Token { return e.ForallTok }
func (e *ForallExpr) exprNode()        {}

// ExistsExpr: exists x in collection : pred
// A boolean expression that is true iff pred holds for at least one element of collection.
// collection must be vec<T> or ring<T>; x is bound in pred with type T.
type ExistsExpr struct {
	ExistsTok  lexer.Token
	Var        lexer.Token
	Collection Expr
	Pred       Expr
}

func (e *ExistsExpr) Pos() lexer.Token { return e.ExistsTok }
func (e *ExistsExpr) exprNode()        {}

// AssertStmt: assert expr  — runtime precondition check inside a function body.
type AssertStmt struct {
	AssertTok lexer.Token
	Expr      Expr
}

func (s *AssertStmt) Pos() lexer.Token { return s.AssertTok }
func (s *AssertStmt) stmtNode()        {}

// ── Contracts ──────────────────────────────────────────────────────────────────

// ContractKind distinguishes requires from ensures clauses.
type ContractKind uint8

const (
	ContractRequires ContractKind = iota
	ContractEnsures
)

// ContractClause is a single requires or ensures predicate on a function.
type ContractClause struct {
	Kind ContractKind
	Tok  lexer.Token // the 'requires' or 'ensures' token
	Expr Expr
}

// ── Effects annotations ────────────────────────────────────────────────────────

// EffectsKind classifies a function's effects annotation.
type EffectsKind uint8

const (
	EffectsNone EffectsKind = iota // no annotation — unchecked
	EffectsPure                    // pure
	EffectsDecl                    // effects(io, net, ...)
	EffectsCap                     // cap(SomeCap)
)

// EffectsAnnotation holds the parsed effects or capability clause on a fn.
type EffectsAnnotation struct {
	Kind  EffectsKind
	Names []string // effect labels (EffectsDecl) or single cap name (EffectsCap)
}

// ── Types ─────────────────────────────────────────────────────────────────────

type TypeExpr interface {
	Node
	typeNode()
}

// NamedType: u32, unit, bool, str, MyStruct
// For module-qualified references (parsed.Expr), Module is non-nil.
type NamedType struct {
	Module *lexer.Token // non-nil for "module.TypeName" references
	Name   lexer.Token
}

func (t *NamedType) Pos() lexer.Token { return t.Name }
func (t *NamedType) typeNode()        {}

// GenericType: ref<T>, option<T>, result<T, E>, vec<T>
type GenericType struct {
	Name   lexer.Token
	Params []TypeExpr
}

func (t *GenericType) Pos() lexer.Token { return t.Name }
func (t *GenericType) typeNode()        {}

// TupleTypeExpr: (T, U, V) — tuple type with 2+ elements.
type TupleTypeExpr struct {
	LParen lexer.Token
	Elems  []TypeExpr
}

func (t *TupleTypeExpr) Pos() lexer.Token { return t.LParen }
func (t *TupleTypeExpr) typeNode()        {}

// FnType: fn(T, U) -> V
type FnType struct {
	FnTok   lexer.Token
	Params  []TypeExpr
	RetType TypeExpr
}

func (t *FnType) Pos() lexer.Token { return t.FnTok }
func (t *FnType) typeNode()        {}
