// Copyright (c) 2026 Scott W. Corley
// SPDX-License-Identifier: Apache-2.0

package cheader_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scottcorleyg1/candor/compiler/cheader"
)

func writeHeader(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.h")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseHeaderSimpleFn(t *testing.T) {
	path := writeHeader(t, `
int add(int a, int b);
void noop(void);
float scale(float x, int n);
`)
	src, err := cheader.ParseHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "extern fn add(") {
		t.Errorf("expected extern fn add, got:\n%s", src)
	}
	if !strings.Contains(src, "-> i32") {
		t.Errorf("expected -> i32, got:\n%s", src)
	}
	if !strings.Contains(src, "extern fn noop()") {
		t.Errorf("expected extern fn noop(), got:\n%s", src)
	}
	if !strings.Contains(src, "extern fn scale(") {
		t.Errorf("expected extern fn scale, got:\n%s", src)
	}
	if !strings.Contains(src, "-> f32") {
		t.Errorf("expected -> f32, got:\n%s", src)
	}
}

func TestParseHeaderPointerParams(t *testing.T) {
	path := writeHeader(t, `
void memcpy_wrap(void *dst, const void *src, size_t n);
int strlen_wrap(const char *s);
`)
	src, err := cheader.ParseHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "ptr<u8>") {
		t.Errorf("expected ptr<u8> for void*/char*, got:\n%s", src)
	}
	if !strings.Contains(src, "u64") {
		t.Errorf("expected u64 for size_t, got:\n%s", src)
	}
}

func TestParseHeaderSkipsPreprocessor(t *testing.T) {
	path := writeHeader(t, `
#ifndef MY_HEADER_H
#define MY_HEADER_H

// a comment
int foo(int x);

#endif
`)
	src, err := cheader.ParseHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "extern fn foo(") {
		t.Errorf("expected extern fn foo, got:\n%s", src)
	}
	// Preprocessor directives must not appear in output.
	if strings.Contains(src, "#ifndef") || strings.Contains(src, "#define") {
		t.Errorf("preprocessor leaked into output:\n%s", src)
	}
}

func TestParseHeaderCudaStyle(t *testing.T) {
	path := writeHeader(t, `
typedef int cudaError_t;
typedef void* cudaStream_t;

cudaError_t cudaMalloc(void **devPtr, size_t size);
cudaError_t cudaFree(void *devPtr);
cudaError_t cudaMemcpy(void *dst, const void *src, size_t count, int kind);
`)
	src, err := cheader.ParseHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range []string{"cudaMalloc", "cudaFree", "cudaMemcpy"} {
		if !strings.Contains(src, "extern fn "+fn+"(") {
			t.Errorf("expected extern fn %s, got:\n%s", fn, src)
		}
	}
}

func TestParseHeaderMissingFile(t *testing.T) {
	_, err := cheader.ParseHeader("/nonexistent/does_not_exist.h")
	if err == nil {
		t.Fatal("expected error for missing header file")
	}
}

func TestParseHeaderReturnTypes(t *testing.T) {
	path := writeHeader(t, `
int8_t  get_i8(void);
uint8_t get_u8(void);
int16_t get_i16(void);
uint16_t get_u16(void);
int32_t get_i32(void);
uint32_t get_u32(void);
int64_t get_i64(void);
uint64_t get_u64(void);
double get_f64(void);
`)
	src, err := cheader.ParseHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-> i8", "-> u8", "-> i16", "-> u16", "-> i32", "-> u32", "-> i64", "-> u64", "-> f64"} {
		if !strings.Contains(src, want) {
			t.Errorf("expected %q in output, got:\n%s", want, src)
		}
	}
}
