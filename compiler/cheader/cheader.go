// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0

// Package cheader parses simplified C/C++/CUDA header files and generates
// Candor extern fn source text for use with #c_header directives.
//
// The parser handles the common subset of C headers needed for FFI:
//   - Function prototypes: `rettype funcname(params);`
//   - Typedef'd function prototypes: `typedef rettype (*FnPtr)(params);` (skipped)
//   - Preprocessor guards (#ifndef/#define/#endif) — skipped
//   - Line and block comments — stripped
//
// C types are mapped to Candor primitives; pointer types become ptr<T>.
// Unrecognised types are represented as u64 (opaque handles) or ptr<u8> for
// pointer-to-unknown.
package cheader

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// protoRe matches a simple one-line C function prototype (no macros, no
// multi-line signatures).  Groups:
//  1. return type (may contain spaces and *'s)
//  2. function name
//  3. parameter list (inside parens)
var protoRe = regexp.MustCompile(
	`^([a-zA-Z_][\w\s\*]*?)\s+([a-zA-Z_]\w*)\s*\(([^)]*)\)\s*;`,
)

// ParseHeader reads a C header file and returns a Candor source string
// containing one `extern fn` declaration per recognised function prototype.
// The returned source can be tokenised and parsed into a *parser.File.
func ParseHeader(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("c_header: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	fmt.Fprintf(&sb, "// extern stubs generated from %s\n", path)

	scanner := bufio.NewScanner(f)
	var acc strings.Builder // accumulates a logical line across line continuations
	for scanner.Scan() {
		line := scanner.Text()

		// Strip line comments.
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}

		// Skip preprocessor directives (#include, #ifndef, #define, …).
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			acc.Reset()
			continue
		}

		acc.WriteString(trimmed)
		acc.WriteByte(' ')

		combined := strings.TrimSpace(acc.String())

		// Only try to match when we reach a semicolon.
		if !strings.HasSuffix(combined, ";") {
			continue
		}

		// Strip block comments /* ... */ (single-line only after accumulation).
		combined = stripBlockComments(combined)

		if m := protoRe.FindStringSubmatch(combined); m != nil {
			retC := strings.TrimSpace(m[1])
			name := strings.TrimSpace(m[2])
			paramsC := strings.TrimSpace(m[3])

			retCandor := cTypeToCandor(retC)
			paramsCandor := parseParams(paramsC)

			fmt.Fprintf(&sb, "extern fn %s(%s) -> %s\n", name, paramsCandor, retCandor)
		}
		acc.Reset()
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("c_header: scanning %s: %w", path, err)
	}
	return sb.String(), nil
}

// stripBlockComments removes /* ... */ from a single-line string.
func stripBlockComments(s string) string {
	for {
		start := strings.Index(s, "/*")
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], "*/")
		if end < 0 {
			s = s[:start]
			break
		}
		s = s[:start] + " " + s[start+end+2:]
	}
	return s
}

// parseParams converts a comma-separated C parameter list to a Candor param list.
// "void" alone means no parameters.
func parseParams(paramsC string) string {
	paramsC = strings.TrimSpace(paramsC)
	if paramsC == "" || paramsC == "void" {
		return ""
	}
	parts := splitParams(paramsC)
	var out []string
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == "..." {
			continue
		}
		candorType := parseOneParam(p)
		out = append(out, fmt.Sprintf("_p%d: %s", i, candorType))
	}
	return strings.Join(out, ", ")
}

// splitParams splits a C param list on top-level commas (not inside < or ()).
func splitParams(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, ch := range s {
		switch ch {
		case '(', '<':
			depth++
		case ')', '>':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// parseOneParam extracts the Candor type from a single C parameter string
// (which may or may not include a name, e.g. "int x" or "int *" or "float").
func parseOneParam(param string) string {
	param = strings.TrimSpace(param)
	// Strip qualifiers.
	for _, q := range []string{"const", "volatile", "restrict", "__restrict__"} {
		param = strings.ReplaceAll(param, q, " ")
	}
	// Collect ALL '*' from the string (they may be attached to the type or the
	// name, e.g. "void *dst", "char* src", "int **pp").
	stars := strings.Count(param, "*")
	param = strings.ReplaceAll(param, "*", " ")
	param = strings.Join(strings.Fields(param), " ")

	// The last word is likely the parameter name; everything before is the type.
	fields := strings.Fields(param)
	var typeParts []string
	if len(fields) > 1 {
		last := fields[len(fields)-1]
		if isTypePart(last) {
			typeParts = fields
		} else {
			typeParts = fields[:len(fields)-1]
		}
	} else {
		typeParts = fields
	}

	baseC := strings.Join(typeParts, " ")
	candor := cTypeToCandor(baseC)

	// Apply pointer wrapping.
	for i := 0; i < stars; i++ {
		if candor == "unit" {
			candor = "ptr<u8>" // void* → ptr<u8>
		} else {
			candor = "ptr<" + candor + ">"
		}
	}
	return candor
}

// isTypePart returns true if the word looks like a type keyword (not a name).
func isTypePart(s string) bool {
	switch s {
	case "int", "long", "short", "char", "float", "double", "void",
		"unsigned", "signed", "const", "volatile", "restrict",
		"size_t", "ssize_t", "ptrdiff_t",
		"int8_t", "int16_t", "int32_t", "int64_t",
		"uint8_t", "uint16_t", "uint32_t", "uint64_t",
		"bool", "_Bool":
		return true
	}
	return false
}

// cTypeToCandor maps a normalised C type name to a Candor primitive type.
func cTypeToCandor(c string) string {
	c = strings.TrimSpace(c)
	// Normalise: strip const/volatile, collapse spaces.
	for _, q := range []string{"const", "volatile", "restrict", "signed", "__restrict__"} {
		c = strings.ReplaceAll(c, q+" ", "")
		c = strings.ReplaceAll(c, " "+q, "")
	}
	// Strip pointer on the type itself (handled by caller via stars counting, but
	// handle the case where it's embedded).
	stars := 0
	for strings.HasSuffix(c, "*") {
		stars++
		c = strings.TrimSuffix(c, "*")
		c = strings.TrimSpace(c)
	}

	var base string
	switch strings.Join(strings.Fields(c), " ") {
	case "void":
		base = "unit"
	case "int", "int32_t":
		base = "i32"
	case "unsigned int", "uint32_t":
		base = "u32"
	case "long", "long int", "int64_t", "long long", "long long int", "off_t":
		base = "i64"
	case "unsigned long", "unsigned long int", "uint64_t", "size_t",
		"unsigned long long", "unsigned long long int":
		base = "u64"
	case "short", "short int", "int16_t":
		base = "i16"
	case "unsigned short", "unsigned short int", "uint16_t":
		base = "u16"
	case "char", "int8_t":
		base = "i8"
	case "unsigned char", "uint8_t":
		base = "u8"
	case "float":
		base = "f32"
	case "double":
		base = "f64"
	case "bool", "_Bool":
		base = "bool"
	case "ssize_t", "ptrdiff_t", "intptr_t":
		base = "i64"
	case "uintptr_t":
		base = "u64"
	default:
		// Opaque types: CUDA handles (cudaStream_t, etc.), FILE*, etc.
		// Represent as u64 (fits a pointer-sized handle on 64-bit).
		base = "u64"
	}

	// Re-apply pointer levels stripped above.
	for i := 0; i < stars; i++ {
		if base == "unit" {
			base = "ptr<u8>"
		} else {
			base = "ptr<" + base + ">"
		}
	}
	return base
}
