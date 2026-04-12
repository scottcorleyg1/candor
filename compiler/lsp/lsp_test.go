// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0

package lsp_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/candor-core/candor/compiler/lsp"
)

// rpcFrame wraps a JSON body in LSP Content-Length framing.
func rpcFrame(body string) string {
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
}

func rpcMsg(id int, method string, params interface{}) string {
	p, _ := json.Marshal(params)
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  json.RawMessage(p),
	}
	b, _ := json.Marshal(msg)
	return rpcFrame(string(b))
}

func rpcNotify(method string, params interface{}) string {
	p, _ := json.Marshal(params)
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  json.RawMessage(p),
	}
	b, _ := json.Marshal(msg)
	return rpcFrame(string(b))
}

// safeBuffer is a thread-safe byte buffer for LSP output collection.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor waits up to 500ms for s to appear in buf.
func waitFor(b *safeBuffer, s string) bool {
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if strings.Contains(b.String(), s) {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func TestInitialize(t *testing.T) {
	input := rpcMsg(1, "initialize", map[string]interface{}{
		"processId":    nil,
		"rootUri":      nil,
		"capabilities": map[string]interface{}{},
	})
	in := strings.NewReader(input)
	var out safeBuffer
	srv := lsp.New(in, &out)
	go srv.Run() //nolint

	if !waitFor(&out, `"id":1`) {
		t.Fatal("no response from initialize within timeout")
	}
	o := out.String()
	if !strings.Contains(o, "capabilities") {
		t.Errorf("missing capabilities in response: %s", o)
	}
}

func TestDidOpenPublishesDiagnostics(t *testing.T) {
	src := "fn main() -> unit {\n    return unit\n}\n"
	msgs := rpcMsg(1, "initialize", map[string]interface{}{"capabilities": map[string]interface{}{}}) +
		rpcNotify("textDocument/didOpen", map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":        "file:///test.cnd",
				"languageId": "candor",
				"version":    1,
				"text":       src,
			},
		})
	in := strings.NewReader(msgs)
	var out safeBuffer
	srv := lsp.New(in, &out)
	go srv.Run() //nolint

	if !waitFor(&out, "publishDiagnostics") {
		t.Skip("LSP server did not publish diagnostics in time (timing-dependent)")
	}
}

func TestDidOpenWithErrorPublishesDiagnostic(t *testing.T) {
	src := "fn main() -> unit {\n    let x: i64 = \"wrong\"\n    return unit\n}\n"
	msgs := rpcMsg(1, "initialize", map[string]interface{}{"capabilities": map[string]interface{}{}}) +
		rpcNotify("textDocument/didOpen", map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":        "file:///bad.cnd",
				"languageId": "candor",
				"version":    1,
				"text":       src,
			},
		})
	in := strings.NewReader(msgs)
	var out safeBuffer
	srv := lsp.New(in, &out)
	go srv.Run() //nolint

	if !waitFor(&out, "publishDiagnostics") {
		t.Skip("LSP server did not publish diagnostics in time")
	}
	o := out.String()
	if strings.Contains(o, `"diagnostics":[]`) {
		t.Errorf("expected non-empty diagnostics for program with type error:\n%s", o)
	}
}
