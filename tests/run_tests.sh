#!/usr/bin/env bash
# Candor compiler test runner
# Usage: ./tests/run_tests.sh
#
# For each tests/cases/<name>.cnd:
#   1. Compile with lexer.exe -> /tmp/cnd_test_<name>.c
#   2. Compile with GCC -> /tmp/cnd_test_<name>.exe
#   3. Run -> capture stdout
#   4. Compare against tests/cases/<name>.expected
#   5. Report PASS / FAIL

set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
CANDORC="$REPO/src/compiler/lexer.exe"
RUNTIME="$REPO/src/compiler"
GCC="PATH=/c/msys64/mingw64/bin:$PATH /c/msys64/mingw64/bin/gcc.exe"
CASES="$REPO/tests/cases"
TMP="$REPO/tmp/cnd_tests"

mkdir -p "$TMP"

pass=0
fail=0
errors=()

for cnd in "$CASES"/*.cnd; do
    name="$(basename "$cnd" .cnd)"
    expected="$CASES/$name.expected"
    c_out="$TMP/$name.c"
    exe_out="$TMP/$name.exe"

    # Step 1: Candor -> C
    if ! "$CANDORC" "$cnd" > "$c_out" 2>"$TMP/$name.cnd_err"; then
        fail=$((fail+1))
        errors+=("FAIL [$name]: candorc error")
        cat "$TMP/$name.cnd_err" >&2
        continue
    fi

    # Step 2: C -> exe
    if ! PATH="/c/msys64/mingw64/bin:$PATH" /c/msys64/mingw64/bin/gcc.exe \
        -std=gnu23 -O0 -o "$exe_out" "$c_out" -I "$RUNTIME" -lm \
        >"$TMP/$name.gcc_err" 2>&1; then
        fail=$((fail+1))
        errors+=("FAIL [$name]: gcc error")
        cat "$TMP/$name.gcc_err" >&2
        continue
    fi

    # Step 3: Run
    actual="$("$exe_out" 2>/dev/null | tr -d '\r')" || true

    # Step 4: Compare
    if [ ! -f "$expected" ]; then
        # No expected file: just verify it compiled and ran without crash
        exit_code=0
        "$exe_out" >/dev/null 2>/dev/null; exit_code=$?
        if [ "$exit_code" -eq 0 ]; then
            pass=$((pass+1))
            echo "PASS [$name] (no expected output — ran OK)"
        else
            fail=$((fail+1))
            errors+=("FAIL [$name]: exit $exit_code")
        fi
    else
        expected_content="$(cat "$expected" | tr -d '\r')"
        if [ "$actual" = "$expected_content" ]; then
            pass=$((pass+1))
            echo "PASS [$name]"
        else
            fail=$((fail+1))
            errors+=("FAIL [$name]: output mismatch")
            echo "  expected: $(echo "$expected_content" | head -5)"
            echo "  actual:   $(echo "$actual" | head -5)"
        fi
    fi
done

echo ""
echo "Results: $pass passed, $fail failed"
if [ ${#errors[@]} -gt 0 ]; then
    echo "Failures:"
    for e in "${errors[@]}"; do echo "  $e"; done
    exit 1
fi
