// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg1/candor

package typeck

// comptime.go — compile-time evaluation of pure (effects []) function calls.
//
// After the main typeck pass, runComptimePass scans every CallExpr in the
// program. If the callee is a pure function (effects []) and every argument
// evaluates to a compile-time constant, the function body is interpreted.
// The result is stored in checker.comptimeVals so emit_c can emit it as a
// literal constant instead of a function call.
//
// Supported value kinds: int64, float64, bool, string, nil (unit)
//
// If evaluation fails for any reason the call is left as a normal runtime call.

import (
	"strconv"
	"strings"

	"github.com/scottcorleyg1/candor/compiler/lexer"
	"github.com/scottcorleyg1/candor/compiler/parser"
)

// comptimeReturn is used as a panic value to signal a `return` statement.
type comptimeReturn struct{ val interface{} }

// runComptimePass walks all expressions in all files and evaluates pure calls.
func runComptimePass(c *checker, files []*parser.File) {
	for _, f := range files {
		for _, d := range f.Decls {
			if fn, ok := d.(*parser.FnDecl); ok {
				walkStmtsComptime(c, fn.Body.Stmts)
			}
		}
	}
}

func walkStmtsComptime(c *checker, stmts []parser.Stmt) {
	for _, s := range stmts {
		walkStmtComptime(c, s)
	}
}

func walkStmtComptime(c *checker, s parser.Stmt) {
	switch st := s.(type) {
	case *parser.LetStmt:
		walkExprComptime(c, st.Value)
	case *parser.ExprStmt:
		walkExprComptime(c, st.X)
	case *parser.ReturnStmt:
		walkExprComptime(c, st.Value)
	case *parser.AssignStmt:
		walkExprComptime(c, st.Value)
	case *parser.FieldAssignStmt:
		walkExprComptime(c, st.Value)
	case *parser.IndexAssignStmt:
		// index assignment — not evaluated at comptime
	case *parser.IfStmt:
		walkExprComptime(c, st.Cond)
		walkStmtsComptime(c, st.Then.Stmts)
		if st.Else != nil {
			walkStmtComptime(c, st.Else)
		}
	case *parser.BlockStmt:
		walkStmtsComptime(c, st.Stmts)
	case *parser.LoopStmt:
		walkStmtsComptime(c, st.Body.Stmts)
	case *parser.ForStmt:
		walkStmtsComptime(c, st.Body.Stmts)
	}
}

func walkExprComptime(c *checker, e parser.Expr) {
	if e == nil {
		return
	}
	switch ex := e.(type) {
	case *parser.CallExpr:
		for _, a := range ex.Args {
			walkExprComptime(c, a)
		}
		tryEvalCall(c, ex)

	case *parser.BinaryExpr:
		walkExprComptime(c, ex.Left)
		walkExprComptime(c, ex.Right)
	case *parser.UnaryExpr:
		walkExprComptime(c, ex.Operand)
	case *parser.FieldExpr:
		walkExprComptime(c, ex.Receiver)
	case *parser.IndexExpr:
		walkExprComptime(c, ex.Collection)
		walkExprComptime(c, ex.Index)
	case *parser.MatchExpr:
		walkExprComptime(c, ex.X)
		for _, arm := range ex.Arms {
			walkExprComptime(c, arm.Body)
		}
	case *parser.MustExpr:
		walkExprComptime(c, ex.X)
		for _, arm := range ex.Arms {
			walkExprComptime(c, arm.Body)
		}
	case *parser.StructLitExpr:
		for _, f := range ex.Fields {
			walkExprComptime(c, f.Value)
		}
	case *parser.ForallExpr:
		walkExprComptime(c, ex.Collection)
		walkExprComptime(c, ex.Pred)
	case *parser.ExistsExpr:
		walkExprComptime(c, ex.Collection)
		walkExprComptime(c, ex.Pred)
	}
}

// tryEvalCall attempts to evaluate a CallExpr at compile time.
func tryEvalCall(c *checker, e *parser.CallExpr) {
	ident, ok := e.Fn.(*parser.IdentExpr)
	if !ok {
		return
	}
	name := ident.Tok.Lexeme
	eff := c.fnEffects[name]
	if eff == nil || eff.Kind != parser.EffectsPure {
		return
	}
	decl := c.fnDecls[name]
	if decl == nil {
		return
	}
	env := make(map[string]interface{}, len(decl.Params))
	for i, param := range decl.Params {
		if i >= len(e.Args) {
			return
		}
		v, ok := evalExpr(c, e.Args[i], nil)
		if !ok {
			return
		}
		env[param.Name.Lexeme] = v
	}
	result, ok := evalBlock(c, decl.Body.Stmts, env)
	if !ok {
		return
	}
	c.comptimeVals[e] = result

	// Evaluate requires clauses with the resolved arg values.
	// A clause that evaluates to false is a compile-time contract violation.
	for _, cc := range decl.Contracts {
		if cc.Kind != parser.ContractRequires {
			continue
		}
		val, ok2 := evalExpr(c, cc.Expr, env)
		if !ok2 {
			continue // can't evaluate — leave it as a runtime check
		}
		b, isBool := val.(bool)
		if !isBool {
			continue
		}
		if !b {
			c.comptimeErrs = append(c.comptimeErrs,
				&Error{Tok: e.LParen, Msg: "requires clause violated at compile time for call to " + name})
		}
	}
}

// evalBlock evaluates a sequence of statements, returning the final value.
// A `return` statement is signalled via panic(comptimeReturn{v}).
func evalBlock(c *checker, stmts []parser.Stmt, env map[string]interface{}) (val interface{}, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			if ret, isRet := r.(comptimeReturn); isRet {
				val = ret.val
				ok = true
			} else {
				ok = false
			}
		}
	}()
	return evalStmts(c, stmts, env)
}

func evalStmts(c *checker, stmts []parser.Stmt, env map[string]interface{}) (interface{}, bool) {
	var last interface{}
	for _, s := range stmts {
		v, ok := evalStmt(c, s, env)
		if !ok {
			return nil, false
		}
		last = v
	}
	return last, true
}

func evalStmt(c *checker, s parser.Stmt, env map[string]interface{}) (interface{}, bool) {
	switch st := s.(type) {
	case *parser.LetStmt:
		v, ok := evalExpr(c, st.Value, env)
		if !ok {
			return nil, false
		}
		env[st.Name.Lexeme] = v
		return nil, true

	case *parser.ReturnStmt:
		v, ok := evalExpr(c, st.Value, env)
		if !ok {
			return nil, false
		}
		panic(comptimeReturn{v})

	case *parser.ExprStmt:
		return evalExpr(c, st.X, env)

	case *parser.IfStmt:
		cond, ok := evalExpr(c, st.Cond, env)
		if !ok {
			return nil, false
		}
		b, ok := cond.(bool)
		if !ok {
			return nil, false
		}
		if b {
			return evalStmts(c, st.Then.Stmts, env)
		}
		if st.Else != nil {
			return evalStmt(c, st.Else, env)
		}
		return nil, true

	case *parser.BlockStmt:
		return evalStmts(c, st.Stmts, env)

	default:
		return nil, false
	}
}

func evalExpr(c *checker, e parser.Expr, env map[string]interface{}) (interface{}, bool) {
	switch ex := e.(type) {
	case *parser.IntLitExpr:
		n, err := strconv.ParseInt(ex.Tok.Lexeme, 10, 64)
		if err != nil {
			return nil, false
		}
		return n, true

	case *parser.FloatLitExpr:
		f, err := strconv.ParseFloat(ex.Tok.Lexeme, 64)
		if err != nil {
			return nil, false
		}
		return f, true

	case *parser.BoolLitExpr:
		return ex.Tok.Lexeme == "true", true

	case *parser.StringLitExpr:
		return unquoteString(ex.Tok.Lexeme), true

	case *parser.IdentExpr:
		name := ex.Tok.Lexeme
		if name == "unit" {
			return nil, true
		}
		if env != nil {
			if v, ok := env[name]; ok {
				return v, true
			}
		}
		return nil, false

	case *parser.CallExpr:
		// Already evaluated at the top-level pass.
		if v, ok := c.comptimeVals[ex]; ok {
			return v, true
		}
		// Try to evaluate a pure function call inline during body interpretation.
		if ident, ok2 := ex.Fn.(*parser.IdentExpr); ok2 {
			eff := c.fnEffects[ident.Tok.Lexeme]
			if eff != nil && eff.Kind == parser.EffectsPure {
				if decl := c.fnDecls[ident.Tok.Lexeme]; decl != nil {
					innerEnv := make(map[string]interface{}, len(decl.Params))
					allOk := true
					for i, param := range decl.Params {
						if i >= len(ex.Args) {
							allOk = false
							break
						}
						v, ok3 := evalExpr(c, ex.Args[i], env)
						if !ok3 {
							allOk = false
							break
						}
						innerEnv[param.Name.Lexeme] = v
					}
					if allOk {
						result, ok3 := evalBlock(c, decl.Body.Stmts, innerEnv)
						if ok3 {
							return result, true
						}
					}
				}
			}
		}
		return nil, false

	case *parser.BinaryExpr:
		return evalBinaryExpr(c, ex, env)

	case *parser.UnaryExpr:
		return evalUnaryExpr(c, ex, env)

	default:
		return nil, false
	}
}

func evalBinaryExpr(c *checker, e *parser.BinaryExpr, env map[string]interface{}) (interface{}, bool) {
	lv, ok := evalExpr(c, e.Left, env)
	if !ok {
		return nil, false
	}
	rv, ok := evalExpr(c, e.Right, env)
	if !ok {
		return nil, false
	}
	op := e.Op.Type
	li, liOk := lv.(int64)
	ri, riOk := rv.(int64)
	if liOk && riOk {
		switch op {
		case lexer.TokPlus:
			return li + ri, true
		case lexer.TokMinus:
			return li - ri, true
		case lexer.TokStar:
			return li * ri, true
		case lexer.TokSlash:
			if ri == 0 {
				return nil, false
			}
			return li / ri, true
		case lexer.TokPercent:
			if ri == 0 {
				return nil, false
			}
			return li % ri, true
		case lexer.TokEqEq:
			return li == ri, true
		case lexer.TokBangEq:
			return li != ri, true
		case lexer.TokLt:
			return li < ri, true
		case lexer.TokLtEq:
			return li <= ri, true
		case lexer.TokGt:
			return li > ri, true
		case lexer.TokGtEq:
			return li >= ri, true
		}
	}
	lf, lfOk := lv.(float64)
	rf, rfOk := rv.(float64)
	if liOk && !lfOk {
		lf, lfOk = float64(li), true
	}
	if riOk && !rfOk {
		rf, rfOk = float64(ri), true
	}
	if lfOk && rfOk {
		switch op {
		case lexer.TokPlus:
			return lf + rf, true
		case lexer.TokMinus:
			return lf - rf, true
		case lexer.TokStar:
			return lf * rf, true
		case lexer.TokSlash:
			return lf / rf, true
		case lexer.TokEqEq:
			return lf == rf, true
		case lexer.TokBangEq:
			return lf != rf, true
		case lexer.TokLt:
			return lf < rf, true
		case lexer.TokLtEq:
			return lf <= rf, true
		case lexer.TokGt:
			return lf > rf, true
		case lexer.TokGtEq:
			return lf >= rf, true
		}
	}
	lb, lbOk := lv.(bool)
	rb, rbOk := rv.(bool)
	if lbOk && rbOk {
		switch op {
		case lexer.TokAnd:
			return lb && rb, true
		case lexer.TokOr:
			return lb || rb, true
		case lexer.TokEqEq:
			return lb == rb, true
		case lexer.TokBangEq:
			return lb != rb, true
		}
	}
	ls, lsOk := lv.(string)
	rs, rsOk := rv.(string)
	if lsOk && rsOk {
		switch op {
		case lexer.TokPlus:
			return ls + rs, true
		case lexer.TokEqEq:
			return ls == rs, true
		case lexer.TokBangEq:
			return ls != rs, true
		}
	}
	return nil, false
}

func evalUnaryExpr(c *checker, e *parser.UnaryExpr, env map[string]interface{}) (interface{}, bool) {
	v, ok := evalExpr(c, e.Operand, env)
	if !ok {
		return nil, false
	}
	switch e.Op.Type {
	case lexer.TokMinus:
		if i, ok := v.(int64); ok {
			return -i, true
		}
		if f, ok := v.(float64); ok {
			return -f, true
		}
	case lexer.TokBang, lexer.TokNot:
		if b, ok := v.(bool); ok {
			return !b, true
		}
	}
	return nil, false
}

func copyEnv(env map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(env))
	for k, v := range env {
		out[k] = v
	}
	return out
}

// unquoteString strips outer quotes from a Candor string literal.
func unquoteString(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		inner := s[1 : len(s)-1]
		inner = strings.ReplaceAll(inner, `\n`, "\n")
		inner = strings.ReplaceAll(inner, `\t`, "\t")
		inner = strings.ReplaceAll(inner, `\\`, "\\")
		inner = strings.ReplaceAll(inner, `\"`, "\"")
		return inner
	}
	return s
}
