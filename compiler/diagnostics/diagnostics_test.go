// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0

package diagnostics_test

import (
	"strings"
	"testing"

	"github.com/candor-core/candor/compiler/diagnostics"
)

func TestRenderError_WithSnippet(t *testing.T) {
	sm := diagnostics.NewSourceMap(map[string]string{
		"main.cnd": "fn main() -> unit {\n    let x = foo()\n}\n",
	})
	d := diagnostics.Diag{
		Severity: diagnostics.SeverityError,
		File:     "main.cnd",
		Line:     2,
		Col:      13,
		Msg:      `undefined identifier "foo"`,
	}
	out := d.Render(sm)
	if !strings.Contains(out, "error:") {
		t.Errorf("missing severity label: %q", out)
	}
	if !strings.Contains(out, "let x = foo()") {
		t.Errorf("missing source line: %q", out)
	}
	if !strings.Contains(out, "^") {
		t.Errorf("missing caret: %q", out)
	}
}

func TestRenderWarning_WithSnippet(t *testing.T) {
	sm := diagnostics.NewSourceMap(map[string]string{
		"main.cnd": "fn main() -> unit {\n    let unused = 42\n}\n",
	})
	d := diagnostics.Diag{
		Severity: diagnostics.SeverityWarning,
		File:     "main.cnd",
		Line:     2,
		Col:      9,
		Msg:      `unused variable "unused"`,
	}
	out := d.Render(sm)
	if !strings.Contains(out, "warning:") {
		t.Errorf("missing warning label: %q", out)
	}
	if !strings.Contains(out, "let unused = 42") {
		t.Errorf("missing source line: %q", out)
	}
}

func TestRenderHint(t *testing.T) {
	d := diagnostics.Diag{
		Severity: diagnostics.SeverityError,
		File:     "a.cnd",
		Line:     1,
		Col:      1,
		Msg:      `undefined identifier "pint"`,
		Hint:     `did you mean "print"?`,
	}
	out := d.Render(nil)
	if !strings.Contains(out, "hint:") {
		t.Errorf("missing hint: %q", out)
	}
	if !strings.Contains(out, `did you mean "print"?`) {
		t.Errorf("missing hint text: %q", out)
	}
}

func TestRenderAll(t *testing.T) {
	sm := diagnostics.NewSourceMap(map[string]string{
		"a.cnd": "let x = 1\nlet y = 2\n",
	})
	diags := []diagnostics.Diag{
		{Severity: diagnostics.SeverityError, File: "a.cnd", Line: 1, Col: 1, Msg: "error one"},
		{Severity: diagnostics.SeverityWarning, File: "a.cnd", Line: 2, Col: 1, Msg: "warning two"},
	}
	out := diagnostics.RenderAll(diags, sm)
	if !strings.Contains(out, "error one") {
		t.Errorf("missing first diag: %q", out)
	}
	if !strings.Contains(out, "warning two") {
		t.Errorf("missing second diag: %q", out)
	}
}

func TestCountErrors(t *testing.T) {
	diags := []diagnostics.Diag{
		{Severity: diagnostics.SeverityError},
		{Severity: diagnostics.SeverityWarning},
		{Severity: diagnostics.SeverityError},
	}
	if n := diagnostics.CountErrors(diags); n != 2 {
		t.Errorf("expected 2 errors, got %d", n)
	}
}
