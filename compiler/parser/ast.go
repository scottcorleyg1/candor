// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg1/candor

package parser

import "github.com/scottcorleyg1/candor/compiler/lexer"

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

// FnDecl: fn name(params) -> RetType [pure | effects(...) | cap(...)] { body }
type FnDecl struct {
	FnTok     lexer.Token
	Name      lexer.Token
	Params    []Param
	RetType   TypeExpr
	Effects   *EffectsAnnotation // nil = no annotation (unchecked)
	Contracts []ContractClause   // requires/ensures; nil or empty = none
	Body      *BlockStmt
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
	StructTok lexer.Token
	Name      lexer.Token
	Fields    []Field
}

func (d *StructDecl) Pos() lexer.Token { return d.StructTok }
func (d *StructDecl) declNode()        {}

// Field: name: Type,
type Field struct {
	Name lexer.Token
	Type TypeExpr
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

// ForStmt: for name in collection { body }
// Collection must be vec<T> or ring<T>; name is bound to element type T.
type ForStmt struct {
	ForTok     lexer.Token
	Var        lexer.Token
	InTok      lexer.Token
	Collection Expr
	Body       *BlockStmt
}

func (s *ForStmt) Pos() lexer.Token { return s.ForTok }
func (s *ForStmt) stmtNode()        {}

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

// StructLitExpr: TypeName { field: value, ... }
type StructLitExpr struct {
	TypeName lexer.Token
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
type NamedType struct {
	Name lexer.Token
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

// FnType: fn(T, U) -> V
type FnType struct {
	FnTok   lexer.Token
	Params  []TypeExpr
	RetType TypeExpr
}

func (t *FnType) Pos() lexer.Token { return t.FnTok }
func (t *FnType) typeNode()        {}
