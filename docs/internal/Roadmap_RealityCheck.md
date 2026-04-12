# Candor — Reality Check

> **Purpose:** Honest gap analysis. Not to criticize what's been built —
> a huge amount of real work is here — but to draw a clear line between
> "compiles without crashing" and "correct, production-quality, and testable."
>
> Read this before deciding what to work on next.

---

## Part 1 — What We Are Currently Stuck On

### The Immediate Blocker: M9 Bootstrap

We are trying to compile the four Candor-written compiler source files
(`lexer.cnd`, `parser.cnd`, `typeck.cnd`, `emit_c.cnd`) together into a
working binary using the Go-compiled `candorc`. This should be straightforward.
It is not. Here is why, in root-cause order:

#### Blocker A — 3 failing typeck tests (module system incomplete)
The multi-module type checker was extended to support the module system needed
for bootstrap, but three tests are failing right now:
```
FAIL  TestModuleEnforcementCrossModuleStructBlocked
FAIL  TestModuleEnforcementCrossModuleStructAllowed
FAIL  TestModuleEnforcementUseRequiresPath
```
This means the module system work is unfinished and uncommitted. We cannot
safely compile multiple `.cnd` files together until this is correct.

#### Blocker B — Tail-match return bug in emit_c.go (unfixed in emitter)
When a Candor function's last expression is a `match` block without an explicit
`return` keyword, the Go emitter generates a bare statement instead of a return:

```c
// BUG — value computed, then thrown away
(__extension__ ({ if (x == 1) { _r = 10; } else { _r = 20; } _r; }));

// CORRECT
return (__extension__ ({ if (x == 1) { _r = 10; } else { _r = 20; } _r; }));
```

This bug has existed since `match` expressions were added. It was invisible
because **every test uses `return match ...`** (explicit return). The six functions
in `lexer.c` that use implicit tail-match were hand-patched in the generated file,
but the Go emitter itself was never fixed. The patch will be lost on next codegen.

#### Blocker C — emit_c.cnd is 732 lines, untracked, and untested
The M9.6 work (C code generator written in Candor) exists on disk but has never
been committed, has no test, and has never been run through the full pipeline.
We are trying to bootstrap with a component that has not been validated at all.

#### Why this matters beyond M9
These three blockers are symptoms of a broader pattern in the project:
**features were built to the depth required to pass their milestone tests,
but those tests were not deep enough to catch the real failure modes.**
The bootstrap work is the first time the entire compiler has been exercised
end-to-end with complex Candor programs — and that stress-test is revealing
gaps in the foundation.

---

## Part 2 — Full Gap Audit (v0.1 through current)

The following is an honest assessment of every milestone area.
**Green** = genuinely solid. **Yellow** = works for the tested cases, gaps exist.
**Red** = framework present, substance incomplete.

---

### v0.1 — Core Language ✅ Mostly Solid

The core language is real and works. However:

- **`match` tail expression missing `return`** (Red — affects all backends)
  Any Candor function whose last expression is a `match` block will compile but
  silently return garbage. This is a v0.1-era bug that survived to today because
  no test exercises it.

- **No end-to-end "compile + run" tests at the unit level**
  The emit_c tests verify C source text output. They do not compile that C and
  run it. A function can emit syntactically valid but semantically wrong C and
  all tests will still pass.

---

### M1 — Language Ergonomics ✅ Solid

Compound assignment, tuple destructuring, struct update, ring iteration — these
are all real and tested with both output verification and integration tests.
No significant gaps identified.

---

### M2 — Standard Library ⚠️ Architecture Mismatch

The stdlib (`std::math`, `std::str`, `std::io`, etc.) is **not implemented in
Candor source files**. It is hardcoded as `emitBuiltinCall()` branches inside
the Go emitter. The only actual `.cnd` stdlib file is `src/std/wasm.cnd`, which
contains only `extern fn` stubs for browser/WASI APIs.

**Why this matters:** The roadmap describes an M2 stdlib as if it is Candor code.
It is Go code that pattern-matches on function names and emits C directly. This:
- Cannot be bootstrapped (the Candor compiler written in Candor cannot call it)
- Cannot be extended in Candor
- Is invisible to the LSP, formatter, and doc generator
- Means the bootstrap compiler (`emit_c.cnd`) has to re-implement all these
  builtins from scratch

**Gap:** M2 was completed for the Go compiler. It is a blocking gap for M9.6+.
The bootstrap compiler needs a real Candor stdlib, or `emit_c.cnd` has to
bypass all stdlib functions entirely.

---

### M3 — Trait System ✅ Solid (C backend), ⚠️ Untested (LLVM backend)

Monomorphization works correctly in the C backend. Tests confirm mangled names
(`Point_fmt`, `Counter_fmt`, `show__Box`) are emitted correctly.

**Gap:** The LLVM backend trait dispatch has no dedicated tests.
Complex trait scenarios (trait objects, multiple implementations, generic
constraints with multiple bounds) are untested in LLVM.

---

### M4 — Developer Tooling ⚠️ Mixed

- **M4.1 Diagnostics** — real, tests pass ✅
- **M4.2 Build system** — `candorc build` works for single-module projects ✅
- **M4.3 LSP** — implementation exists, tests pass. However: the LSP has never
  been tested against a multi-file module project. Hover/go-to-def across module
  boundaries is untested. ⚠️
- **M4.4 Formatter** — exists, idempotent on tested cases ✅
- **M4.5 Test framework** — `#test` directive works, `candorc test` runs tests ✅

**Gap:** `candorc build` for multi-file projects uses `mergeFiles()` which
silently drops duplicate declarations. If two files accidentally declare the
same function, the second one is silently discarded. No warning is emitted.

---

### M5 — Compilation Backends

#### M5.1 LLVM Backend ⚠️ ~60% Complete

The LLVM backend has explicit TODOs for:
- `for-in` over `set<T>` — not supported
- Index-assign on map/set — not supported
- `map_new, map_insert, map_get, map_remove, map_len, map_contains` — not implemented
- `set_new, set_add, set_remove, set_contains, set_len` — not implemented
- `ring_pop_front` — not implemented
- `spawn` / `task<T>` — not supported in LLVM backend

The roadmap says "LLVM backend feature-complete" in the completed milestones table.
**That is not accurate.** Map and set operations are entirely absent.

#### M5.2–M5.5 ✅ Solid

Debug/release builds, sanitizers, cross-compilation, and WASM work as described.

---

### M6 — Formal Verification

#### M6.1 Symbolic Contract Evaluation 🔴 Framework Only

The `runComptimePass()` in `comptime.go` is structured correctly and walks the
AST. But it does **not** actually evaluate expressions at compile time. The
`requires` and `ensures` clauses are emitted as C `assert()` calls at runtime.
The milestone description says "eliminated runtime checks for proven-safe calls" —
this does not happen. All checks remain as runtime asserts regardless.

**Gap:** M6.1 as shipped = "emit contracts as runtime asserts" (which existed before
M6.1). The compile-time elimination part was not built.

#### M6.4 `forall`/`exists` ⚠️ Partial

Parses and type-checks correctly. C backend emits loop-based assertions.
The roadmap also mentions "SMT queries in verification mode" — that is M6.2
(not yet built), so this is the runtime-only half. That part is complete.

---

### M7 — AI Integration Layer

#### M7.1 MCP Tool Annotations ⚠️ Incomplete

**Syntax divergence:** Spec says `#[mcp_tool(name = "x", description = "y")]`.
Implementation uses `#mcp_tool "description"` — simpler, but different from spec.
This is a design choice; it should be documented as the actual syntax.

**JSON Schema type mapping is wrong:**
```go
func candorTypeToJsonSchema(t string) string {
    switch t {
    case "str":  return "string"
    case "bool": return "boolean"
    ...
    default:     return "integer"   // ← vec<T>, structs, option<T> all → "integer"
    }
}
```
Any function with a `vec<T>`, struct, or `result<T,E>` parameter will emit
a broken MCP schema. An AI agent consuming this schema would see wrong types.

**No tests for the JSON output itself.** The only tests that exist for this
feature are that `#mcp_tool` parses without error.

#### M7.2 Semantic Context Embedding ⚠️ Same class of issue
Works for simple cases. No test verifies the `intent.json` format.

#### M7.3 Capability Tokens ✅ Solid
Six tests covering the meaningful scenarios. Real enforcement in typeck.

#### M7.4 `#export_json` ⚠️ Primitives Only

The implementation handles `str`, `bool`, and numeric types.
It does **not** handle:
- `vec<T>` fields — emitted as `%lld` (prints garbage)
- Nested structs — same
- `option<T>`, `result<T,E>` — treated as integers
- JSON buffer hardcoded to 16KB — silently truncates large structs
- `from_json` is a fragile hand-rolled character parser

The roadmap says "supports str, bool, integer, and float fields" — that is
accurate. But the feature is presented as a general serialization tool, and it
will produce corrupt output for any struct with collection or optional fields.

---

### M8 — Ecosystem

#### M8.1 Package Registry ✅ Solid
`Candor.toml`, `Candor.lock`, `candorc fetch` — real and tested.

#### M8.2 C/CUDA Header Interop ⚠️ Limited Scope

The `cheader` package uses a single regex to parse C function declarations.
It handles:
- Basic C types, pointer types, explicit-width integer types
- Single-line function prototypes

It does **not** handle:
- Multi-line signatures
- Typedef'd function pointers (explicitly skipped)
- Struct/union definitions
- Variadic functions (`...`)
- Complex C macros

For simple CUDA API headers this is often sufficient. For complex headers
(cuDNN, TensorRT) it will silently skip most declarations.

#### M8.3 Documentation Generator ✅ Solid

---

### M9 — Bootstrapping (The Main Event)

All of M9.1–M9.5 are genuinely done. The test coverage is real — these features
were built incrementally with tests at each phase.

**M9.6 (emit_c.cnd):** 732 lines exist but untracked, untested, unvalidated.

**M9.7–M9.8:** Blocked by everything above.

**Root cause of all current M9 pain:**
The compiler was never tested on the class of Candor program that the bootstrap
compiler is. The `.cnd` source files use:
- Multi-module compilation (first real use of the module system)
- Tail-match expressions (first real exercise of the emitter bug)
- Large programs (>500 lines each) that stress every corner of the emitter

---

### M10 — Concurrency

#### M10.1 `task<T>` / `spawn` ⚠️ Skeleton Only

The generated C calls `pthread_create` and `pthread_join`. This is real.
What is not real:
- **No result propagation:** The spawned function's return value is never
  extracted from the thread. `.join()` always returns a default value.
- **Memory leak:** The task struct heap-allocated for each spawn is never freed.
- **No error handling:** If `pthread_create` fails, the error is silently ignored.
- **No timeout:** `pthread_join` blocks forever.

The 5 tests for M10.1 verify that the C source *contains* `pthread_create` and
`pthread_join`. They do not compile and run the code to verify it works.

#### M10.3 Hardware Effect Tiers ✅ Solid
#### M10.4 `arc<T>` ✅ Solid for its scope

---

### M11 — Tensor and ML Primitives

#### M11.1 `f16`/`bf16` Types ⚠️ Types Without Literals

The type system recognizes `f16` and `bf16`. The C backend maps them to
`_Float16` and `__bf16`. However: **literal syntax is not implemented**.
You cannot write `1.5h16` in Candor source. The types exist but you cannot
construct values of those types with literals.

#### M11.2 `tensor<T>` ✅ Solid
Builtins work. C backend emission is real.

#### M11.3 SIMD Distance Intrinsics 🔴 Marketing vs. Reality

The milestone description says: "compiler intrinsics for dot product, L2 norm,
cosine similarity; LLVM backend emits `llvm.fmuladd` / vector reduce."

**What was actually built:** Scalar C loops. The functions `tensor_dot`,
`tensor_l2`, `tensor_cosine`, `tensor_matmul` emit nested `for` loops that
an optimizing compiler *may* auto-vectorize with `-O2`. They do not emit
LLVM vector intrinsics. They do not use AVX-512, NEON, or WASM SIMD explicitly.

`effects(simd)` is accepted by the type checker. It does not cause any
different code to be generated.

This is the largest gap between what the roadmap says and what exists.

---

### M12 — Storage Layer

#### M12.1 `mmap<T>` ✅ Appears Solid
Struct emitted, `mmap_open`/`mmap_anon`/`mmap_deref`/`mmap_flush`/`mmap_close` work.

---

## Part 3 — The Pattern

Every gap above shares the same root cause:

> **The definition of "done" was "the milestone's named tests pass."
> It was not "the feature is correct for all inputs the spec describes."**

Concretely:
- M6.1 is "done" because 4 contract tests pass — but those 4 tests only verify
  that asserts are *emitted*, not that they are *eliminated* at compile time.
- M7.1 is "done" because `tools.json` is written — but the schema is wrong for
  non-primitive types, and there are no tests for the output.
- M10.1 is "done" because `pthread_create` appears in the output — but the
  spawned function's result is never actually retrieved.
- M11.3 is "done" because `tensor_dot` compiles — but it's a scalar loop,
  not a SIMD intrinsic.

The M9 bootstrap work is forcing correctness because you cannot hand-wave
a self-hosting compiler. Either the emitter is right or it crashes.

---

## Part 4 — The Game Plan

This is a proposed order of work. It is methodical: each phase fixes a
specific class of problem and leaves the codebase cleaner for the next.

---

### Phase 0 — Stop the Bleeding (1–2 sessions)
*Get all tests green. No new features.*

1. Fix the 3 failing typeck module enforcement tests
2. Apply Fix 3 (tail-match return) to `emit_c.go` — the emitter, not lexer.c
3. Add one test for implicit tail-match to `emit_c_test.go`
4. Commit `emit_c.cnd` with a `TestM9EmitCSource` test that runs the full
   pipeline and verifies output compiles with gcc

**Exit criterion:** `go test ./...` is green. All M9 source files produce
valid, compilable C when run through the full pipeline.

---

### Phase 1 — Harden the Emitter Foundation (2–3 sessions)
*Fix the class of bug that M9 revealed, not just the instance.*

1. **End-to-end test harness:** Add a test helper that takes Candor source,
   compiles it to C, compiles that C with gcc, runs the binary, and asserts
   stdout matches expected output. This is the missing layer.

2. **Coverage sweep of emit_c_test.go:** For each emitter function, add at
   least one test that uses the implicit tail-return form (no explicit `return`).
   The current suite only tests explicit returns.

3. **Fix M10.1 result propagation:** The `.join()` call must actually retrieve
   the return value from the spawned thread. Fix the C emission and add a test
   that runs a spawn and checks the joined result.

4. **Fix M7.1 JSON schema mapping:** Map `vec<T>` → `"array"`, structs →
   `"object"`, `option<T>` → nullable. Add a test that feeds a function with
   non-primitive parameters and checks the schema output.

5. **Fix M7.4 export_json:** Handle `vec<str>` at minimum. Remove the 16KB
   hardcoded buffer. Add a test with a struct that has a vec field.

**Exit criterion:** A Candor program with spawn/join, match tail expressions,
export_json with vec fields, and mcp_tool with vec parameters all compile,
run, and produce correct output.

---

### Phase 2 — LLVM Backend Completeness (2–3 sessions)
*Make the "feature-complete" claim accurate.*

The LLVM backend has explicit TODOs for map, set, and ring operations. These
need to be implemented before the LLVM backend can be used for any real program.

1. Implement `map_new`, `map_insert`, `map_get`, `map_remove`, `map_len`,
   `map_contains` in the LLVM backend
2. Implement `set_new`, `set_add`, `set_remove`, `set_contains`, `set_len`
3. Implement `ring_pop_front`
4. Add LLVM backend tests for each new operation
5. Test M3 trait dispatch end-to-end in the LLVM backend

**Exit criterion:** Every feature in the C backend is also in the LLVM backend.
The "feature-complete LLVM backend" claim is true.

---

### Phase 3 — Fix M6.1 Properly (1 session)
*Deliver what the milestone actually promised.*

M6.1 promised: "eliminate runtime checks for proven-safe calls." What shipped:
"emit runtime asserts for all calls." The difference is real and meaningful —
a divide-by-zero check that's provably safe at compile time should not add
runtime overhead.

1. In `comptime.go`, implement the constant folding for `requires` clauses:
   when all arguments to a function are compile-time constants and the clause
   can be evaluated at compile time, record the result
2. In `emit_c.go`, check the comptime result before emitting the `assert()`:
   if proven safe, skip the assert emission entirely
3. Add tests that confirm the assert is NOT emitted when provable

**Exit criterion:** `fn safe(x: i64) -> i64 requires x > 0 { return x }` called
with `safe(5)` emits NO `assert` in the C output.

---

### Phase 4 — M11.3 SIMD Reality (1–2 sessions)
*Either deliver actual SIMD or honestly document that it auto-vectorizes.*

The honest options are:

**Option A (Recommended):** Document that `effects(simd)` means "eligible for
SIMD auto-vectorization; compile with `-O2 -march=native` for best results."
Update the roadmap to reflect this. Add a `--simd` flag that passes
`-march=native -ffast-math` to gcc. This is honest and useful.

**Option B (Ambitious):** Actually emit LLVM vector intrinsics for `tensor_dot`
and `tensor_matmul` in the LLVM backend. This is the right long-term choice
but requires careful alignment and shape-checking.

**Exit criterion (Option A):** Roadmap accurately describes what `effects(simd)`
means. `--simd` flag exists and demonstrably improves benchmark performance.

---

### Phase 5 — M9 Bootstrap for Real (2–4 sessions)
*Now the foundation is solid enough to bootstrap.*

With Phases 0–1 complete:

1. `go test ./...` is green
2. All `.cnd` files produce valid, compilable C
3. The emitter is correct for all tested patterns

Now:
1. Run `candorc build src/compiler/ --output candorc-stage1`
2. Any failures will be real compiler bugs, not known gaps hiding in the emitter
3. Fix bugs as found, add tests for each fix
4. M9.7 complete when `candorc-stage1` can compile `test.cnd` correctly

**Exit criterion:** `candorc-stage1 compile fn main() -> unit { print("hello") }`
produces valid C that compiles and prints "hello".

---

### Phase 6 — Close the Remaining Open Milestones
*In dependency order, from Roadmap_OpenItems.md.*

After Phase 5:
- `pin<T>`, `weak<T>`, `heap<T>`, `arena<T>`, `trie<K,V>` (M10.4 remainder)
- `effects(async)` state machines (M10.2)
- `std/tensor.cnd` (M11.4)
- `std/vecdb.cnd` (M11.5)
- `std/nixl.cnd` (M12.3)
- `std/kvcache.cnd` (M12.4)
- M6.2 SMT integration (can proceed in parallel, independent)
- M9.8 Stage 2 bootstrap (after M9.7)

---

## Part 5 — Definition of Done (Going Forward)

Every milestone from this point forward must satisfy all four of these:

| Criterion | What it means |
|-----------|---------------|
| **Tests pass** | `go test ./...` green, no exceptions |
| **Edge cases covered** | Implicit tail returns, non-primitive types, multi-file, error paths |
| **End-to-end verified** | The emitted C compiles with gcc AND the resulting binary produces correct output |
| **Roadmap accurate** | The description in `Roadmap_Completed.md` matches what was actually built, not what was intended |

The gap between "M11.3 SIMD intrinsics done" (what the roadmap says) and
"M11.3 emits scalar loops that may auto-vectorize" (what exists) is the
kind of mismatch that erodes trust in the project's maturity.

Candor is a serious project. The roadmap should be a serious document.

---

## Summary: The 5 Most Important Things to Fix Right Now

| # | Fix | Unblocks |
|---|-----|----------|
| 1 | Tail-match return in emit_c.go | M9 bootstrap, correct programs |
| 2 | 3 failing typeck module tests | M9 bootstrap, multi-file programs |
| 3 | End-to-end compile+run test harness | Everything — visibility into correctness |
| 4 | M10.1 result propagation in spawn/join | Any real concurrent program |
| 5 | LLVM backend map/set operations | LLVM backend "feature-complete" claim |

The rest is real work with no fundamental blockers once these five are resolved.
