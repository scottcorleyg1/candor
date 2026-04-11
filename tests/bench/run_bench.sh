#!/usr/bin/env bash
# Candor performance benchmark suite
# Usage: ./tests/bench/run_bench.sh
#
# Measures:
#   1. Compiler throughput (lines/sec, ms per compile)
#   2. Runtime performance vs Python for cpu-bound tasks

set -euo pipefail

REPO="$(cd "$(dirname "$0")/../.." && pwd)"
CANDORC="$REPO/candorc-stage1.exe"
RUNTIME="$REPO/src/compiler"
PYTHON="/c/Python314/python.exe"
BENCH="$REPO/tests/bench"
TMP="./tmp/cnd_bench"
RUNS=5

mkdir -p "$TMP"

timeit() {
    local cmd=("$@")
    local total=0
    for i in $(seq 1 $RUNS); do
        local start end
        start=$(date +%s%N)
        "${cmd[@]}" > /dev/null 2>&1
        end=$(date +%s%N)
        total=$(( total + (end - start) / 1000000 ))
    done
    echo $(( total / RUNS ))
}

echo "======================================"
echo "  Candor Benchmark Suite"
echo "  $(date '+%Y-%m-%d %H:%M')"
echo "  Runs per measurement: $RUNS"
echo "======================================"
echo ""

# ── 1. Compiler throughput ──────────────────────────────────────────────────
echo "## 1. Compiler Throughput"
echo ""

SRC_LINES=$(cat "$REPO"/src/compiler/*.cnd | wc -l | tr -d ' ')
SRC_WORDS=$(cat "$REPO"/src/compiler/*.cnd | wc -w | tr -d ' ')

FULL_MS=$(timeit "$CANDORC" \
    "$REPO/src/compiler/lexer.cnd" \
    "$REPO/src/compiler/parser.cnd" \
    "$REPO/src/compiler/typeck.cnd" \
    "$REPO/src/compiler/emit_c.cnd" \
    "$REPO/src/compiler/manifest.cnd" \
    "$REPO/src/compiler/main.cnd")

LINES_PER_SEC=$(( SRC_LINES * 1000 / FULL_MS ))

echo "  Full compiler self-compile ($SRC_LINES lines, $SRC_WORDS words):"
echo "    avg: ${FULL_MS}ms  →  ~${LINES_PER_SEC} lines/sec"
echo ""

# Single-file startup overhead
SMALL_MS=$(timeit "$CANDORC" "$BENCH/fib.cnd")
echo "  Single small file (fib.cnd, 8 lines):"
echo "    avg: ${SMALL_MS}ms  (startup overhead)"
echo ""

# ── 2. Runtime benchmarks ───────────────────────────────────────────────────
echo "## 2. Runtime Performance"
echo ""

compile_bench() {
    local name="$1"
    local src="$BENCH/$name.cnd"
    local c="$TMP/$name.c"
    local exe="$TMP/${name}_candor.exe"
    "$CANDORC" "$src" > "$c" 2>/dev/null
    PATH="/c/msys64/mingw64/bin:$PATH" /c/msys64/mingw64/bin/gcc.exe -std=gnu23 -O2 -o "$exe" "$c" -I "$RUNTIME" -lm > /dev/null 2>&1
    echo "$exe"
}

bench_pair() {
    local label="$1"
    local cnd_exe="$2"
    local py_script="$3"
    local cnd_ms py_ms ratio

    cnd_ms=$(timeit "$cnd_exe")
    py_ms=$(timeit "$PYTHON" "$py_script")
    ratio=$(( py_ms * 10 / (cnd_ms == 0 ? 1 : cnd_ms) ))

    printf "  %-28s  Candor: %4dms   Python: %5dms   speedup: %dx\n" \
        "$label" "$cnd_ms" "$py_ms" "$(( ratio / 10 ))"
}

FIB_EXE=$(compile_bench fib)
SIEVE_EXE=$(compile_bench sieve)
MAP_EXE=$(compile_bench map_bench)
STRUCT_EXE=$(compile_bench struct_bench)
STRING_EXE=$(compile_bench string_bench)

bench_pair "fib(40) recursive" "$FIB_EXE" "$BENCH/fib.py"
bench_pair "sieve(1,000,000)" "$SIEVE_EXE" "$BENCH/sieve.py"
bench_pair "map_insert(100,000) string keys" "$MAP_EXE" "$BENCH/map_bench.py"
bench_pair "struct pass-by-value (10M)" "$STRUCT_EXE" "$BENCH/struct_bench.py"
bench_pair "string builder (10,000)" "$STRING_EXE" "$BENCH/string_bench.py"

echo ""
echo "======================================"
echo "  Done."
echo "======================================"
