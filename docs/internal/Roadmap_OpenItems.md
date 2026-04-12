# Candor — Open Items

> For completed work, see [Roadmap_Completed.md](Roadmap_Completed.md).
> For the full technical reference, see [Roadmap_Combined.md](Roadmap_Combined.md).
>
> Items are ordered by dependency — earlier items must land before later ones.
> Status indicators:
> - 🔄 In Progress — work exists, not yet complete or tested
> - 🐛 Known Gap — bug or missing test discovered during M9 investigation
> - ⬜ Not Started

---

## Track 1 — Self-Hosting (M9.6 → M9.8)

This is the highest-priority track. Each item directly blocks the next.
The root cause of the current M9.7 failures was traced back to items 1–3 below,
all of which are gaps in the Go compiler that were never caught by the existing test suite.

---

### 🐛 1. Module System — Fix 3 Failing Tests

**What it is:** The Go typechecker was extended to support multi-module compilation
(needed so `lexer.cnd`, `parser.cnd`, `typeck.cnd`, and `emit_c.cnd` can coexist without
name collisions). That work is partially done but has three broken tests.

**Failing tests right now:**
- `TestModuleEnforcementCrossModuleStructBlocked`
- `TestModuleEnforcementCrossModuleStructAllowed`
- `TestModuleEnforcementUseRequiresPath`

**What's already done:** Module-qualified type parsing (`module.TypeName`),
module-aware struct/enum registration, wildcard `use module` imports,
`mergeFiles` with boundary markers in the emitter.

**What still needs to happen:** Diagnose and fix the three failing tests so the
full module system is solid before we rely on it for multi-file compilation.

```
📋 Requires:  Nothing (prerequisite — fix it first)
🔓 Unlocks:   Everything else in Track 1
```

---

### 🐛 2. emit_c.go — Missing `return` Before Tail-Match Expressions

**What it is:** When a Candor function's last expression is a `match` block
(with no explicit `return` keyword), the Go C-emitter generates a bare statement
instead of a return statement. This produces invalid C that gcc rejects.

**Broken output:**
```c
// BUG — this is a void statement, gcc sees no return
(__extension__ ({ if (...) { _r = 1; } else { _r = 2; } _r; }));
```

**Correct output:**
```c
return (__extension__ ({ if (...) { _r = 1; } else { _r = 2; } _r; }));
```

**Current workaround:** Six functions in `lexer.c` were hand-patched with the
correct `return`. This must be fixed in the Go emitter so the patch survives
the next code regeneration.

**Where the fix goes:** `compiler/emit_c/emit_c.go` — `emitFnDecl` /
`emitMustOrMatch`.

**New test needed:** Add to `emit_c_test.go`:
```candor
fn f(b: bool) -> u32 { match b { true => 1   false => 2 } }
// No explicit `return` — must emit: return (__extension__ ({...}));
```

```
📋 Requires:  Nothing (standalone emitter bug, fixable independently)
🔓 Unlocks:   Item 3, clean regeneration of lexer.c
```

---

### 🐛 3. Missing Test: Full Pipeline on M9 Source Files

**What it is:** The existing M9 source tests (`TestM9LexerSource`, `TestM9ParserSource`,
`TestM9TypeckSource`) only run lex → parse → typeck. The C emitter is never called,
and the emitted C is never compiled with gcc. This is why items 1 and 2 above went
undetected.

**What needs to be added:**

- `TestM9EmitCSource` — run all four stages (lex → parse → typeck → emit_c) on each
  `.cnd` source file and assert the output is non-empty and compiles clean with gcc.
- This test should cover: `lexer.cnd`, `parser.cnd`, `typeck.cnd`, `emit_c.cnd`.

**This is a process gap, not a code gap** — adding this test is fast once items 1 and
2 are fixed, but it's the guardrail that prevents these categories of bug from recurring.

```
📋 Requires:  Items 1 and 2 (module system working, tail-match fixed)
🔓 Unlocks:   Confidence in M9.6, clean entry point for M9.7
```

---

### 🔄 4. M9.6 — C Code Generator Written in Candor

**What it is:** `src/compiler/emit_c.cnd` — a 732-line Candor program that emits C
from a parsed and type-checked Candor AST. This is the fourth and final piece of the
bootstrap compiler pipeline, alongside `lexer.cnd`, `parser.cnd`, and `typeck.cnd`.

**Current state:** The file exists and is 732 lines, but it is **untracked** (not
committed), has no tests of its own, and has never been run through the full pipeline
to verify the C it produces is valid.

**What needs to happen:**
1. Commit `emit_c.cnd`
2. Add `TestM9EmitCCndSource` — run the full 4-stage pipeline on `emit_c.cnd` itself
3. Smoke-test: compile a simple `.cnd` program using only the Candor-written pipeline
   and verify the output C compiles and runs correctly

```
📋 Requires:  Items 1, 2, and 3
🔓 Unlocks:   M9.7
```

---

### ⬜ 5. M9.7 — Stage 1 Bootstrap

**What it is:** Use the Go-compiled `candorc` to compile the full Candor compiler
(all four `.cnd` source files) into a native binary called `candorc-stage1`.

**Definition of done:**
```
candorc build src/compiler/ --output candorc-stage1
echo 'fn main() -> unit { print("hello") }' | ./candorc-stage1
# → produces valid C that compiles and runs
```

A test suite comparing Go-compiled output vs Candor-compiled output on the same
input programs must pass identically.

```
📋 Requires:  M9.6 (item 4)
🔓 Unlocks:   M9.8
```

---

### ⬜ 6. M9.8 — Stage 2 Bootstrap (Self-Hosting)

**What it is:** Use `candorc-stage1` to compile itself, producing `candorc-stage2`.
Identical output from both proves the compiler is self-consistent. The Go toolchain
is no longer required for day-to-day Candor development.

**Definition of done:**
```
candorc-stage1 build src/compiler/ --output candorc-stage2
diff <(candorc-stage1 compile test.cnd) <(candorc-stage2 compile test.cnd)
# → empty diff
```

```
📋 Requires:  M9.7 (item 5)
🔓 Unlocks:   Language is self-hosting — this is the Track 1 goal
```

---

## Track 2 — Remaining Ownership Primitives (M10.4 partial)

M10.4 shipped `arc<T>` but the three other ownership types from that milestone are still open.
They are needed before the storage layer (Track 4) can be built.

---

### ⬜ 7. `pin<T>` — Non-Movable Allocation

**What it is:** A non-movable heap allocation for CUDA DMA and NIXL zero-copy transfers.
CUDA requires host buffers to stay at a fixed physical address; `pin<T>` enforces this
at the type level — passing it by value is a compile error.

- `pin_new(val: T) -> pin<T>` — calls `cudaMallocHost` (or equivalent)
- `pin_deref(p: ref<pin<T>>) -> ref<T>`
- `pin_addr(p: ref<pin<T>>) -> u64` — raw address for FFI / NIXL registration
- Drop calls `cudaFreeHost`

```
📋 Requires:  M10.4 arc<T> (done)
🔓 Unlocks:   M12.3 (NIXL bindings), M12.4 (KV cache)
```

---

### ⬜ 8. `weak<T>` — Non-Owning Reference to `arc<T>`

**What it is:** A weak reference that does not prevent deallocation. Essential for
cache eviction: a radix tree node can hold a `weak<KVPage>` so the eviction manager
can free the page even while the tree still has a node referencing it.

- `weak_new(a: ref<arc<T>>) -> weak<T>`
- `weak_upgrade(w: ref<weak<T>>) -> option<arc<T>>`

```
📋 Requires:  M10.4 arc<T> (done)
🔓 Unlocks:   M12.4 (KV cache radix tree)
```

---

### ⬜ 9. Inference Stdlib Types — `heap<T>`, `arena<T>`, `trie<K,V>`

**What it is:** Three data structures used in inference-serving workloads.

- **`heap<T>`** (min/max priority queue) — SLA-driven request scheduling
- **`arena<T>`** (slab allocator) — all KV blocks for one request freed atomically;
  pairs with `cap<arena>` so dropping the capability frees the whole arena
- **`trie<K, V>`** — radix-tree prefix matching for KV cache overlap scoring

```
📋 Requires:  arc<T> (done), pin<T> (item 7), weak<T> (item 8)
🔓 Unlocks:   M12.4 (KV cache)
```

---

## Track 3 — Async and Concurrency (M10.2, M10.5)

M10.1 (`task<T>` / `spawn` via pthreads) is done. The remaining items go deeper.

---

### ⬜ 10. M10.2 — `effects(async)` Compiler-Generated State Machines

**What it is:** True async functions with compiler-lowered continuations — not threads.
A function marked `effects(async)` can `await` another async operation; the compiler
generates a fixed-size state machine frame instead of allocating per-suspension.

- `await expr` is only valid inside `effects(async)` functions
- The effect propagates: calling an async fn from a sync context is a type error
- Compatible with `task<T>`: `spawn { async_fn() }` bridges the thread and async models

```
📋 Requires:  M10.1 task<T> (done)
🔓 Unlocks:   M10.5 (Dynamo endpoint), M12.3 (NIXL), M12.4 (KV cache)
```

---

### ⬜ 11. M10.5 — `#[dynamo_endpoint]` Annotation

**What it is:** Extends the MCP tool annotation system (M7.1) to emit NVIDIA Dynamo
deployment descriptors. Annotated functions become Dynamo workers with phase, resource
requirements, and SLA derived automatically from the Candor type signature.

```candor
#[dynamo_endpoint(model = "deepseek-r1", priority = HIGH, phase = DECODE)]
fn decode_step(kv: ref<KVBlock>, tokens: vec<i64>) -> result<vec<i64>, str>
    effects(gpu, async)
```

`candorc dynamo` emits a `DynamoGraphDeploymentRequest` YAML.

```
📋 Requires:  M10.2 effects(async), M7.1 MCP annotations (done)
🔓 Unlocks:   Nothing — capstone of the Dynamo integration track
```

---

## Track 4 — ML / Storage Layer (M11.4 → M12.4)

The lower items depend on the upper ones. M11.1–M11.3 and M12.1 are the foundation
for everything here.

> **M11.3 honest status:** `tensor_dot`, `tensor_l2`, `tensor_cosine`, `tensor_matmul`
> are implemented as scalar C loops with `_Pragma("GCC ivdep")` hints. The `--simd`
> flag enables C compiler auto-vectorization (`-O3 -ftree-vectorize -march=native`).
> Hand-coded LLVM vector intrinsics (AVX-512 / NEON / WASM SIMD) are **not** yet
> implemented and are tracked below as item 11b.

---

### ⬜ 11b. M11.3 (remaining) — LLVM Vector Intrinsics for Tensor Ops

**What it is:** Replace the current auto-vectorized scalar loops with explicit LLVM IR
vector intrinsics so the LLVM backend achieves the same throughput as the C backend
with `--simd`.

- Port `tensor_dot`, `tensor_l2`, `tensor_cosine`, `tensor_matmul` to `emit_llvm.go`
- Use `llvm.fmuladd`, `llvm.vector.reduce.fadd`, and fixed-width LLVM vector types
- Auto-select width: 8×f32 (AVX-256), 16×f32 (AVX-512), 4×f32 (NEON), 4×f32 (WASM SIMD)

```
📋 Requires:  M11.2 tensor<T> (done), LLVM backend map/set iteration (done)
🔓 Unlocks:   peak performance for --backend=llvm with ML workloads
```

---

### ⬜ 12. M11.4 — `std/tensor.cnd` — Shape Arithmetic and Tensor Ops

**What it is:** Pure Candor implementations of the tensor operations the compiler
does not intrinsify (M11.3 covered dot/l2/cosine/matmul at the compiler level).

- `tensor_reshape`, `tensor_transpose`, `tensor_broadcast_to`
- `tensor_cat`, `tensor_stack`, `tensor_split`
- `tensor_to_vec` / `tensor_from_vec` bridges
- Softmax, layer-norm, ReLU, GELU activations (pure, `effects(simd)`)

```
📋 Requires:  M11.2 tensor<T> (done), M11.3 SIMD intrinsics (done)
🔓 Unlocks:   M11.5 (vecdb)
```

---

### ⬜ 13. M11.5 — `std/vecdb.cnd` — HNSW and IVF Indexes

**What it is:** Vector database index structures implemented in pure Candor —
no FAISS dependency. Two indexes:

- **HNSW** (Hierarchical Navigable Small World) — approximate nearest neighbor with
  `arc<HnswNode>` so multiple concurrent queries walk the graph without locking
- **IVF** (Inverted File Index) — coarse quantization + refinement

```
📋 Requires:  M11.4 (tensor ops), M10.4 arc<T> (done)
🔓 Unlocks:   M12.4 (KV cache with vector scoring)
```

---

### ⬜ 14. M12.2 — `std/colstore.cnd` — Column Store Primitives

**What it is:** Arrow-compatible columnar layout for embedding batches, token sequences,
and KV cache metadata. Columnar storage is more cache-efficient than interleaved row
structs for the batch sizes inference workloads use.

- `ColBatch` struct with type-erased `map<str, tensor<u8>>` columns
- `col_get<T>` / `col_put<T>` for typed column access
- `effects(storage)` on any operation touching a `mmap`-backed column

```
📋 Requires:  M12.1 mmap<T> (done), M11.2 tensor<T> (done)
🔓 Unlocks:   M12.4 (KV cache metadata)
```

---

### ⬜ 15. M12.3 — `std/nixl.cnd` — NIXL Zero-Copy Transfer Bindings

**What it is:** `extern fn` bindings for NVIDIA NIXL (Inference Transfer Library),
which enables zero-copy GPU↔CPU↔NVMe transfers over InfiniBand / RoCE for
disaggregated prefill→decode KV migration. Follows the same pattern as `std/wasm.cnd`
— pure `extern fn` declarations, no NIXL dependency in Candor core.

- `nixl_register(ptr, byte_len, mem_type)` — register a `pin<tensor<T>>` buffer
- `nixl_transfer(src, dst, src_off, dst_off, byte_len)` — async zero-copy transfer
- `nixl_poll(transfer_id)` — poll a pending transfer
- `nixl_send_kv(src, remote_rank, layer, head)` — send one KV layer block

```
📋 Requires:  M10.2 effects(async), pin<T> (item 7)
🔓 Unlocks:   M12.4 (KV cache NIXL integration)
```

---

### ⬜ 16. M12.4 — `std/kvcache.cnd` — Radix-Tree KV Cache

**What it is:** The capstone ML milestone. A production-quality KV cache built on
every other piece of the type system and stdlib:

- Radix tree for prefix deduplication across concurrent requests
- `arc<KVPage>` — multiple decode workers share prefill pages without copying
- `weak<KVPage>` in tree nodes — eviction manager can free pages without dangling refs
- Multi-tier eviction: GPU VRAM → CPU RAM → NVMe → object store,
  each tier annotated with its effect (`gpu` / `mem` / `storage` / `net`)
- NIXL integration for migrating KV blocks between disaggregated prefill and decode nodes

```
📋 Requires:  M12.2 (colstore), M12.3 (NIXL), M11.5 (vecdb),
              weak<T> (item 8), arena<T> (item 9), M10.2 effects(async)
🔓 Unlocks:   Nothing — this is the capstone ML milestone
```

---

## Track 5 — Formal Verification (M6.2, M6.3)

These are independent of Tracks 1–4 and can proceed in parallel.

---

### ⬜ 17. M6.2 — SMT Integration

**What it is:** Translate `requires`/`ensures` clauses to SMT-LIB 2 and call Z3 or
CVC5 at compile time. For pure functions with constant arguments: either "clause always
satisfied" (elide the runtime assert) or "counterexample found at line N".

```
📋 Requires:  M6.1 symbolic evaluation (done)
🔓 Unlocks:   M6.3 (refinement types)
```

---

### ⬜ 18. M6.3 — Refinement Types

**What it is:** Type aliases with inline predicates. The compiler verifies the predicate
at assignment sites and propagates the refinement through the type system.

```candor
type NonZero  = i64 where self != 0
type Percent  = f64 where self >= 0.0 and self <= 1.0

fn safe_div(a: i64, b: NonZero) -> i64 { return a / b }
```

When statically provable: zero runtime cost. When not provable: falls back to a runtime
assert in debug mode.

```
📋 Requires:  M6.2 (SMT solver)
🔓 Unlocks:   Nothing immediately — research milestone
```

---

## Known Language Gaps (No Milestone Number)

These are gaps in the language surface that don't map to a numbered milestone yet
but are worth tracking. They surfaced as constraints when writing the `.cnd` compiler
source files.

| Gap | Impact | Notes |
|-----|--------|-------|
| Named-return / early-exit in closures | Medium | Can't `return` from an outer function inside a closure |
| `invariant` clauses | Low | Token exists in the lexer; not wired to typeck or codegen |
| Consistent `forall`/`exists` tooling | Low | Runtime (M6.4) is done; SMT path (M6.2) is open |

---

## Dependency Map (Quick Reference)

```
Module System fix  ──┐
emit_c.go Fix 3    ──┼──▶  TestM9EmitCSource  ──▶  M9.6  ──▶  M9.7  ──▶  M9.8
                     │                                                (self-hosting goal)
                     └── (prerequisite for all of Track 1)

arc<T> [done] ──▶  pin<T>  ──▶  M12.3 (NIXL)  ──┐
               └─  weak<T> ──────────────────────┤
               └─  heap/arena/trie ──────────────┼──▶  M12.4 (KV cache)
M10.2 (async)  ──────────────────────────────────┤
M11.5 (vecdb)  ──────────────────────────────────┘
M12.2 (colstore) ────────────────────────────────┘

M11.2 [done] ──▶  M11.4 (tensor ops) ──▶  M11.5 (vecdb)
M10.1 [done] ──▶  M10.2 (async)      ──▶  M10.5 (Dynamo)
M6.1  [done] ──▶  M6.2  (SMT)        ──▶  M6.3  (refinement types)
```
