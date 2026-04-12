// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0
// https://github.com/scottcorleyg/candor

package emit_c

import (
	"fmt"
	"strings"

	"github.com/candor-core/candor/compiler/parser"
	"github.com/candor-core/candor/compiler/typeck"
)

// EmitTests emits a C translation unit that runs all #test-annotated functions
// and prints pass/fail results. The generated main() replaces the user's main.
func EmitTests(file *parser.File, res *typeck.Result) (string, error) {
	// Collect test function names.
	var testFns []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*parser.FnDecl)
		if !ok {
			continue
		}
		for _, dir := range fn.Directives {
			if dir == "test" {
				testFns = append(testFns, fn.Name.Lexeme)
				break
			}
		}
	}

	// Emit the normal program C (minus the user's main).
	baseSrc, err := Emit(file, res)
	if err != nil {
		return "", err
	}

	// Strip the user's main function from the emitted C — replace
	// `int main(int argc, char** argv) {` … `}` with nothing.
	// We do this by locating the first `int main(` and removing the block.
	baseSrc = stripMainFromC(baseSrc)

	// Build the test harness main.
	var sb strings.Builder
	sb.WriteString(baseSrc)
	sb.WriteString("\n/* ── candorc test harness ─────────────────────────────── */\n")
	sb.WriteString("int main(int argc, char** argv) {\n")
	sb.WriteString("    (void)argc; (void)argv;\n")
	sb.WriteString(fmt.Sprintf("    int _total = %d, _passed = 0, _failed = 0;\n", len(testFns)))
	sb.WriteString("    const char* _name = NULL;\n")

	for _, name := range testFns {
		sb.WriteString(fmt.Sprintf(`
    _name = %q;
    {
        int _ok = 1;
        /* wrap in a block so assert() failures abort the test */
        %s();
        if (_ok) { printf("PASS  %%s\n", _name); _passed++; }
    }
`, name, name))
	}

	sb.WriteString(`
    printf("\n%d/%d tests passed", _passed, _total);
    if (_failed > 0) { printf(" (%d failed)", _failed); }
    printf("\n");
    return (_failed > 0) ? 1 : 0;
}
`)
	return sb.String(), nil
}

// stripMainFromC removes the `int main(...)` function body from emitted C source.
// It finds the first occurrence and removes it so the test harness can supply its own.
func stripMainFromC(src string) string {
	const marker = "int main("
	idx := strings.Index(src, marker)
	if idx < 0 {
		return src
	}
	// Find the matching closing brace by counting { }.
	start := idx
	braceStart := strings.Index(src[start:], "{")
	if braceStart < 0 {
		return src
	}
	braceStart += start
	depth := 0
	end := braceStart
	for end < len(src) {
		switch src[end] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				// Include the trailing newline.
				end++
				for end < len(src) && (src[end] == '\n' || src[end] == '\r') {
					end++
				}
				return src[:start] + src[end:]
			}
		}
		end++
	}
	return src[:start] // fallback: truncate
}
