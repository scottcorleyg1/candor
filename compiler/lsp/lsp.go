// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg/candor

// Package lsp implements a Language Server Protocol server for Candor.
//
// Transport: JSON-RPC 2.0 over stdin/stdout with Content-Length framing.
// Capabilities: diagnostics, hover (type), go-to-definition, completion (fields/methods).
package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/candor-core/candor/compiler/diagnostics"
	"github.com/candor-core/candor/compiler/lexer"
	"github.com/candor-core/candor/compiler/parser"
	"github.com/candor-core/candor/compiler/typeck"
)

// ── JSON-RPC types ─────────────────────────────────────────────────────────────

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

// ── LSP types (minimal subset) ─────────────────────────────────────────────────

type Position struct {
	Line      int `json:"line"`      // 0-based
	Character int `json:"character"` // 0-based
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"` // 1=error, 2=warning, 3=info, 4=hint
	Message  string `json:"message"`
	Source   string `json:"source"`
}

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type HoverResult struct {
	Contents MarkupContent `json:"contents"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type CompletionItem struct {
	Label  string `json:"label"`
	Kind   int    `json:"kind"` // 2=method, 5=field, 6=variable, 3=function
	Detail string `json:"detail,omitempty"`
}

// ── Server state ──────────────────────────────────────────────────────────────

// Server holds per-document state.
type Server struct {
	docs map[string]string       // URI → source text
	res  map[string]*typeck.Result // URI → typeck result (nil if error)
	ast  map[string]*parser.File  // URI → parsed file

	in  *bufio.Reader
	out io.Writer
}

// New creates a new LSP server reading from r and writing to w.
func New(r io.Reader, w io.Writer) *Server {
	return &Server{
		docs: make(map[string]string),
		res:  make(map[string]*typeck.Result),
		ast:  make(map[string]*parser.File),
		in:   bufio.NewReader(r),
		out:  w,
	}
}

// Run is the main serve loop. It blocks until the client disconnects or sends shutdown.
func (s *Server) Run() error {
	for {
		req, err := s.readRequest()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		s.dispatch(req)
	}
}

// ── Transport ─────────────────────────────────────────────────────────────────

func (s *Server) readRequest() (*rpcRequest, error) {
	// Read Content-Length header(s).
	contentLen := -1
	for {
		line, err := s.in.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length: ") {
			n, _ := strconv.Atoi(strings.TrimPrefix(line, "Content-Length: "))
			contentLen = n
		}
	}
	if contentLen <= 0 {
		return nil, fmt.Errorf("invalid Content-Length")
	}
	body := make([]byte, contentLen)
	if _, err := io.ReadFull(s.in, body); err != nil {
		return nil, err
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

func (s *Server) send(v interface{}) {
	data, _ := json.Marshal(v)
	fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n%s", len(data), data)
}

func (s *Server) reply(id interface{}, result interface{}) {
	s.send(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Server) replyErr(id interface{}, code int, msg string) {
	s.send(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

func (s *Server) notify(method string, params interface{}) {
	s.send(rpcNotification{JSONRPC: "2.0", Method: method, Params: params})
}

// ── Dispatch ──────────────────────────────────────────────────────────────────

func (s *Server) dispatch(req *rpcRequest) {
	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "initialized":
		// no-op
	case "shutdown":
		s.reply(req.ID, nil)
	case "exit":
		// client wants us to exit — Run() will get EOF next
	case "textDocument/didOpen":
		s.handleDidOpen(req)
	case "textDocument/didChange":
		s.handleDidChange(req)
	case "textDocument/didClose":
		s.handleDidClose(req)
	case "textDocument/hover":
		s.handleHover(req)
	case "textDocument/definition":
		s.handleDefinition(req)
	case "textDocument/completion":
		s.handleCompletion(req)
	default:
		if req.ID != nil {
			s.replyErr(req.ID, -32601, "method not found: "+req.Method)
		}
	}
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (s *Server) handleInitialize(req *rpcRequest) {
	s.reply(req.ID, map[string]interface{}{
		"capabilities": map[string]interface{}{
			"textDocumentSync": 1, // full sync
			"hoverProvider":    true,
			"definitionProvider": true,
			"completionProvider": map[string]interface{}{
				"triggerCharacters": []string{"."},
			},
		},
		"serverInfo": map[string]string{
			"name":    "candorc-lsp",
			"version": "0.1.0",
		},
	})
}

func (s *Server) handleDidOpen(req *rpcRequest) {
	var p struct {
		TextDocument TextDocumentItem `json:"textDocument"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return
	}
	s.docs[p.TextDocument.URI] = p.TextDocument.Text
	s.recheck(p.TextDocument.URI)
}

func (s *Server) handleDidChange(req *rpcRequest) {
	var p struct {
		TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
		ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return
	}
	if len(p.ContentChanges) > 0 {
		s.docs[p.TextDocument.URI] = p.ContentChanges[len(p.ContentChanges)-1].Text
		s.recheck(p.TextDocument.URI)
	}
}

func (s *Server) handleDidClose(req *rpcRequest) {
	var p struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return
	}
	delete(s.docs, p.TextDocument.URI)
	delete(s.res, p.TextDocument.URI)
	delete(s.ast, p.TextDocument.URI)
	// Clear diagnostics.
	s.notify("textDocument/publishDiagnostics", PublishDiagnosticsParams{
		URI:         p.TextDocument.URI,
		Diagnostics: []Diagnostic{},
	})
}

func (s *Server) handleHover(req *rpcRequest) {
	var p struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
		Position     Position               `json:"position"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		s.reply(req.ID, nil)
		return
	}
	res, ok := s.res[p.TextDocument.URI]
	if !ok || res == nil {
		s.reply(req.ID, nil)
		return
	}
	src := s.docs[p.TextDocument.URI]
	tok := tokenAt(src, p.TextDocument.URI, p.Position)
	if tok == nil {
		s.reply(req.ID, nil)
		return
	}
	// Find the expression at this position in the type map.
	file := s.ast[p.TextDocument.URI]
	if file == nil {
		s.reply(req.ID, nil)
		return
	}
	typ := findTypeAt(res, file, tok)
	if typ == nil {
		s.reply(req.ID, nil)
		return
	}
	s.reply(req.ID, HoverResult{
		Contents: MarkupContent{Kind: "markdown", Value: fmt.Sprintf("```candor\n%s\n```", typ)},
	})
}

func (s *Server) handleDefinition(req *rpcRequest) {
	var p struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
		Position     Position               `json:"position"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		s.reply(req.ID, nil)
		return
	}
	src := s.docs[p.TextDocument.URI]
	tok := tokenAt(src, p.TextDocument.URI, p.Position)
	if tok == nil {
		s.reply(req.ID, nil)
		return
	}
	// Walk all files to find the declaration matching tok.Lexeme.
	file := s.ast[p.TextDocument.URI]
	if file == nil {
		s.reply(req.ID, nil)
		return
	}
	loc := findDefinition(file, tok.Lexeme)
	if loc == nil {
		s.reply(req.ID, nil)
		return
	}
	s.reply(req.ID, loc)
}

func (s *Server) handleCompletion(req *rpcRequest) {
	var p struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
		Position     Position               `json:"position"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		s.reply(req.ID, []CompletionItem{})
		return
	}
	res, ok := s.res[p.TextDocument.URI]
	if !ok || res == nil {
		s.reply(req.ID, []CompletionItem{})
		return
	}

	// Basic completion: struct fields and method names.
	items := buildCompletions(res)
	s.reply(req.ID, items)
}

// ── Type-check and publish diagnostics ────────────────────────────────────────

func (s *Server) recheck(uri string) {
	src, ok := s.docs[uri]
	if !ok {
		return
	}
	filePath := uriToPath(uri)
	var diags []Diagnostic

	tokens, err := lexer.Tokenize(filePath, src)
	if err != nil {
		if le, ok := err.(*lexer.Error); ok {
			diags = append(diags, Diagnostic{
				Range:    lineColRange(le.Line, le.Col),
				Severity: 1,
				Message:  le.Msg,
				Source:   "candorc",
			})
		}
		s.res[uri] = nil
		s.ast[uri] = nil
		s.notify("textDocument/publishDiagnostics", PublishDiagnosticsParams{URI: uri, Diagnostics: diags})
		return
	}

	file, err := parser.Parse(filePath, tokens)
	if err != nil {
		diags = append(diags, Diagnostic{
			Range:    lineColRange(1, 1),
			Severity: 1,
			Message:  err.Error(),
			Source:   "candorc",
		})
		s.res[uri] = nil
		s.ast[uri] = nil
		s.notify("textDocument/publishDiagnostics", PublishDiagnosticsParams{URI: uri, Diagnostics: diags})
		return
	}
	s.ast[uri] = file

	sm := diagnostics.NewSourceMap(map[string]string{filePath: src})
	_ = sm

	res, err := typeck.Check(file)
	if res != nil {
		// Warnings.
		for _, w := range res.Warnings {
			diags = append(diags, Diagnostic{
				Range:    lineColRange(w.Tok.Line, w.Tok.Col),
				Severity: 2,
				Message:  w.Msg,
				Source:   "candorc",
			})
		}
		s.res[uri] = res
	} else {
		s.res[uri] = nil
	}

	if err != nil {
		// Unwrap multi-errors.
		type unwrapper interface{ Unwrap() []error }
		var errs []error
		if me, ok := err.(unwrapper); ok {
			errs = me.Unwrap()
		} else {
			errs = []error{err}
		}
		for _, e := range errs {
			if te, ok := e.(*typeck.Error); ok {
				d := Diagnostic{
					Range:    lineColRange(te.Tok.Line, te.Tok.Col),
					Severity: 1,
					Message:  te.Msg,
					Source:   "candorc",
				}
				if te.Hint != "" {
					d.Message += "\n" + te.Hint
				}
				diags = append(diags, d)
			} else {
				diags = append(diags, Diagnostic{
					Range:    lineColRange(1, 1),
					Severity: 1,
					Message:  e.Error(),
					Source:   "candorc",
				})
			}
		}
	}

	s.notify("textDocument/publishDiagnostics", PublishDiagnosticsParams{URI: uri, Diagnostics: diags})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func lineColRange(line, col int) Range {
	pos := Position{Line: line - 1, Character: col - 1}
	return Range{Start: pos, End: pos}
}

// uriToPath converts a file:// URI to a local filesystem path.
func uriToPath(uri string) string {
	path := strings.TrimPrefix(uri, "file://")
	// On Windows, file:///C:/... → C:/...
	if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return path
}

// pathToURI converts a local path to a file:// URI.
func pathToURI(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	if len(path) > 1 && path[1] == ':' {
		return "file:///" + path
	}
	return "file://" + path
}

// tokenAt returns the lexer Token at (line, col) in src (1-based).
func tokenAt(src, file string, pos Position) *lexer.Token {
	tokens, err := lexer.Tokenize(file, src)
	if err != nil {
		return nil
	}
	line := pos.Line + 1   // convert to 1-based
	col := pos.Character + 1
	var best *lexer.Token
	for i := range tokens {
		t := &tokens[i]
		if t.Line == line && t.Col <= col && col <= t.Col+len(t.Lexeme) {
			best = t
		}
	}
	return best
}

// findTypeAt looks up the type of the expression whose primary token matches tok.
func findTypeAt(res *typeck.Result, file *parser.File, tok *lexer.Token) typeck.Type {
	for expr, typ := range res.ExprTypes {
		if exprMatchesTok(expr, tok) {
			return typ
		}
	}
	return nil
}

func exprMatchesTok(e parser.Expr, tok *lexer.Token) bool {
	switch x := e.(type) {
	case *parser.IdentExpr:
		return x.Tok.Line == tok.Line && x.Tok.Col == tok.Col
	case *parser.FieldExpr:
		return x.Field.Line == tok.Line && x.Field.Col == tok.Col
	}
	return false
}

// findDefinition searches for the declaration of name in file and returns its location.
func findDefinition(file *parser.File, name string) *Location {
	_ = pathToURI // used below
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *parser.FnDecl:
			if d.Name.Lexeme == name {
				return &Location{
					URI:   pathToURI(d.Name.File),
					Range: lineColRange(d.Name.Line, d.Name.Col),
				}
			}
		case *parser.StructDecl:
			if d.Name.Lexeme == name {
				return &Location{
					URI:   pathToURI(d.Name.File),
					Range: lineColRange(d.Name.Line, d.Name.Col),
				}
			}
		case *parser.EnumDecl:
			if d.Name.Lexeme == name {
				return &Location{
					URI:   pathToURI(d.Name.File),
					Range: lineColRange(d.Name.Line, d.Name.Col),
				}
			}
		case *parser.ConstDecl:
			if d.Name.Lexeme == name {
				return &Location{
					URI:   pathToURI(d.Name.File),
					Range: lineColRange(d.Name.Line, d.Name.Col),
				}
			}
		}
	}
	return nil
}

// buildCompletions returns all completion items from a typeck result.
func buildCompletions(res *typeck.Result) []CompletionItem {
	var items []CompletionItem
	seen := make(map[string]bool)

	// Function names.
	for name, sig := range res.FnSigs {
		if seen[name] {
			continue
		}
		seen[name] = true
		items = append(items, CompletionItem{
			Label:  name,
			Kind:   3, // function
			Detail: sig.String(),
		})
	}

	// Struct fields (via methods map — structs expose fields as completions).
	for typeName, fields := range res.Structs {
		for fieldName, fieldType := range fields.Fields {
			label := fieldName
			if seen[label] {
				label = typeName + "." + fieldName
			}
			items = append(items, CompletionItem{
				Label:  label,
				Kind:   5, // field
				Detail: fieldType.String(),
			})
		}
	}

	// Method names.
	for _, sig := range res.FnSigs {
		_ = sig
	}

	return items
}
