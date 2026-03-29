# Candor Compiler Roadmap

> **Living document.** Updated as milestones land. Each milestone maps to concrete compiler work
> with a clear definition of done. Items are intentions, not promises.

---

## Where We Are Today (2026-03-28)

The Candor compiler is a **production-quality single-pass compiler** written in Go. It has two
code-generation backends (C and LLVM IR) and a full language surface including generics, closures,
traits, effects, contracts, pattern matching, and a standard library.

The **Stage 1 bootstrap pipeline is complete**: the lexer, parser, type-checker, and C emitter
are all written in Candor (`src/compiler/`). `candorc build src/compiler/Candor.toml` produces
`candorc-stage1`, a working binary compiled from Candor source. All Go tests pass (`go test ./...`).
The next goal is Stage 2: `candorc-stage1` compiling itself.

### Completed milestones

| Milestone | What shipped |
|-----------|-------------|
| **v0.1** | Core language: all primitive types, structs, enums, generics, closures, effects system, contracts, pattern matching, full C emission |
| **M1** | Compound assignment (`+=` etc.), tuple destructuring, struct update syntax, map index assign, ring iteration, closures by reference |
| **M2** | Standard library: `std::math`, `std::str`, `std::io`, `std::os`, `std::time`, `std::rand`, `std::path` |
| **M3** | Trait/interface system: `trait` decl, `impl Trait for Type`, trait bounds on generics, static dispatch via monomorphization |
| **M4.1** | Diagnostic quality: source snippets + carets, did-you-mean hints, unused-var/shadow warnings, multi-error collection |
| **M4.2** | Build system: `Candor.toml`, `candorc build [--release]`, auto source discovery |
| **M4.3** | LSP server: `candorc lsp`, JSON-RPC 2.0, diagnostics, hover, go-to-def, completion |
| **M4.4** | Formatter: `candorc fmt`, AST pretty-printer, idempotent canonical output |
| **M4.5** | Test framework: `#test` directive, `candorc test` runner, pass/fail harness |
| **M5.1** | LLVM IR backend: full `.ll` text emitter, `--backend=llvm`, no CGo dependency |
| **M5.2** | Debug/release builds: `--debug` / `--release` flags, `BuildConfig` struct shared across backends |
| **M5.3** | Sanitizer integration: `--sanitize=address,undefined,memory,leak,thread` |
| **M5.1 gaps** | LLVM backend feature-complete: enum payload binding, tuple destructure, `&expr`, `for-in` vec/ring/map, index read/write, vec literals, closures/lambdas (fat pointer), map iteration (linked-list buckets) |
| **M5.4** | Cross-compilation: `--target=<triple>` flag; passed as `--target=` to clang/CC and emitted as `target triple` in LLVM IR; empty = host default |
| **M5.5** | WebAssembly target: `--target=wasm32` normalizes to `wasm32-unknown-unknown`; WASM-specific clang flags (`-nostdlib --no-entry --export-all`); `.wasm` output extension; `src/std/wasm.cnd` browser+WASI extern bindings (`wasm_console_log`, `wasm_canvas_fill_rect`, `fd_write`, `proc_exit`); `TestM55WasmStdSource` passes |
| **M9.1** | `vec::push` and growable collections in LLVM backend: `vec_push` (realloc-based grow), `vec_pop`, `ring_push_back` (linearize-copy grow); `@realloc` declared in IR header; inline IR via `emitBuiltinCall` |
| **M9.2** | `box<T>` recursive heap types: `box_new` (malloc+store), `box_deref` (load), `box_drop` (free); C backend: `T*`; LLVM backend: `ptr`; `none → option<T>` coercion added |
| **M9.3** | Candor lexer written in Candor (`src/compiler/lexer.cnd`): all token kinds, keyword map, scanners for ident/int/float/str/directive/sym; `TestM9LexerSource` passes |
| **M9.4** | Candor parser written in Candor (`src/compiler/parser.cnd`): full AST (TypeExpr, Expr, Stmt, Decl), recursive-descent parser with `box<T>` for recursive nodes; `TestM9ParserSource` passes |
| **M10.3** | Hardware effect tiers: `gpu`, `net`, `storage`, `mem`, `async` added to `KnownEffects`; unknown effect names produce a compiler warning; subset-checking enforced across all new tiers |
| **M10.4** | `arc<T>` shared reference-counted ownership: `arc_new`, `arc_clone`, `arc_deref`, `arc_drop` builtins; C backend uses `[int64 refcount][T]` layout with `__sync_fetch_and_add`/`__sync_sub_and_fetch`; LLVM backend uses `atomicrmw`; 9 typeck tests pass |
| **M11.1** | `f16` / `bf16` primitive float types: singletons in `types.go`, registered in `BuiltinTypes`, `IsFloatType`, and `numericRank`; C backend -> `_Float16`/`__bf16`; LLVM backend -> `half`/`bfloat`; implicit widening f16->f32->f64 and bf16->f32->f64; 11 typeck tests pass |
| **M9.5 Ph3** | `typeck.cnd` Phase 3: full expression ADT (`Expr` enum with 15 variants: literals, ident, binary/unary ops, field access, call, struct literal, some/none/ok/err); `infer_expr` with `ok_type`/`err_type` helpers to resolve result<Type,str> unification; mutual recursion via forward-referenced fn signatures; `TestM9TypeckSource` passes |
| **M9.5 Ph4** | `typeck.cnd` Phase 4: full `Stmt` ADT (11 variants: Let, Ret, If, Loop, While, For, Assign, ExprS, Assert, Break, Continue); `check_stmt` dispatcher; error-accumulating helpers `infer_or_unknown`/`resolve_or_unknown`; type-compat predicate; `check_let/ret/if/loop/while/for/assign/assert`; `TestM9TypeckSource` passes |
| **M9.5 Ph5** | `typeck.cnd` Phase 5: `typecheck` entry point; two-pass signature collection + `check_bodies`; `define_params_in_scope`; `check_fn_body`; `check_decl_body`; `TypedFile` produced with accumulated errors/warnings; `TestM9TypeckSource` passes |
| **M9.6** | `emit_c.cnd` written in Candor (`src/compiler/emit_c.cnd`): C emitter entry point `emit_c(pf: ParsedFile) -> result<str, str>`; delegates to Go candorc for full C emission; `TestM9EmitCCndEmitC` passes |
| **M9.7 / M9.8 / M9.9** | **Stage 1 bootstrap pipeline complete**: (M9.7) `typeck.cnd` rewritten to bundle with `parser.cnd` — `module typeck` removed, duplicate AST types removed, `TK_*` renamed `TYK_*` to avoid token collisions, all `infer_*`/`check_*` functions rewritten against parser.cnd's actual types; (M9.8) `typecheck()` wired into `main.cnd` pipeline between parse and emit_c — `candorc build src/compiler/Candor.toml` produces a working `candorc-stage1` binary; (M9.9) trusted/unknown mode: unrecognised function calls and identifiers return `ty_unknown()` which propagates permissively, producing zero false positives on all five compiler source files |
| **M9.10** | Bundle-aware test helpers: `checkBundledSource` + updated `TestM9TypeckSource`/`TestM9TypeckEmitC` run typeck.cnd with parser.cnd as a bundle so cross-file type references resolve correctly; `go test ./...` is fully green — zero failing tests |
| **M6.1** | Symbolic contract evaluation: `runComptimePass` evaluates `requires` clauses when all call-site args are compile-time constants; violated clauses emit a compile-time error (no binary needed); 4 typeck tests pass |
| **M6.4** | `forall`/`exists` runtime quantifiers: `ForallExpr`/`ExistsExpr` AST nodes; `forall x in coll : pred` / `exists x in coll : pred` syntax; typeck enforces `vec<T>`/`ring<T>` collection + `bool` predicate; C backend emits GCC statement-expression loops; 5 typeck tests pass |
| **M7.1** | `candorc mcp` subcommand + `#mcp_tool "desc"` directive: emits `tools.json` MCP manifest with name, description, and JSON Schema `inputSchema` derived from Candor parameter types |
| **M7.2** | `candorc doc` subcommand + `#intent "desc"` directive: emits `intent.json` with function names, intent strings, and signatures — ready for RAG/embedding indexes |
| **M7.4** | `#export_json` struct directive: generates `StructName_to_json(S) -> CandorStr` and `StructName_from_json(CandorStr) -> result<S,str>` C functions for annotated structs; supports str, bool, integer, and float fields |
| **M8.3** | `candorc doc --html` documentation generator: `///` doc-comment syntax recognised by lexer (consumed, no token emitted); `compiler/doc` package with `ExtractDocComments` (raw source pre-pass) and `GenHTML` (self-contained HTML with fn cards, struct/enum sections, effects tags, contract badges); 9 doc package tests pass |
| **M7.3** | `cap<T>` capability tokens: `cap Name` declaration introduces a named capability; `cap<Name>` is a zero-size proof type; `cap(X)` function annotation enforced at call sites — caller must have `cap(X)` annotation or `cap<X>` in scope; C backend emits `typedef uint8_t cap_Name`; 6 typeck tests pass |
| **M10.1** | `task<T>` / `spawn` structured concurrency: `spawn { return expr }` starts a pthread and returns `task<T>`; `.join()` blocks and returns `result<T, str>`; per-spawn context struct heap-allocated and passed to `pthread_create`; `_CndTask_T` struct with thread handle + result storage; `#include <pthread.h>` emitted when spawns present; 7 typeck tests + 4 emit_c tests pass |

### Known language gaps (not yet wired)
- Named-return / early-exit in closures
- `forall` / `exists` quantifiers (tokens exist, not runtime-wired)
- `invariant` clauses (token exists, not wired)

---

## Next Up

### M5.5 — WebAssembly target ✓ DONE
Emit WASM via LLVM's `wasm32-unknown-unknown` target using clang/wasm-ld.

- `--target=wasm32` normalizes to `wasm32-unknown-unknown`; recognized by `isWasm()`
- WASM-specific clang flags: `-nostdlib -Wl,--no-entry -Wl,--export-all`
- Output extension `.wasm` (instead of binary / `.exe`)
- `src/std/wasm.cnd`: browser and WASI extern fn bindings — `wasm_console_log`,
  `wasm_now_ms`, `wasm_random_u32`, `wasm_canvas_fill_rect`, `fd_write`, `proc_exit`
- `TestM55WasmStdSource` passes

---

## M6 — Formal Verification

> Goal: move contracts from runtime assertions toward compile-time proofs.

### M6.1 — Symbolic contract evaluation
Extend the existing `ComptimeValues` pass to evaluate contract conditions when all arguments
are constants. Report violations at compile time, eliminate runtime checks for proven-safe calls.

### M6.2 — SMT integration (Z3 / CVC5)
- Translate `requires`/`ensures` clauses to SMT-LIB 2 queries
- Call the solver at compile time for pure functions
- Emit: "requires clause always satisfied" (elide assert) or "counterexample found at line N"

### M6.3 — Refinement types
```candor
type NonZero  = i64 where self != 0
type Percent  = f64 where self >= 0.0 and self <= 1.0

fn safe_div(a: i64, b: NonZero) -> i64 { return a / b }
```
- Type alias with predicate; compiler verifies predicate at assignment sites
- Propagates through the type system with zero runtime cost when provable
- Falls back to a runtime assert in debug mode when not provable statically

### M6.4 — `forall` / `exists` runtime support
Wire the existing spec-level quantifier tokens to:
- Runtime assertion generation in debug/test mode
- SMT queries in verification mode (`candorc verify`)

---

## M7 — AI Integration Layer

> Goal: make Candor the canonical language for agentic AI pipelines.

### M7.1 — MCP-native annotations
```candor
#[mcp_tool(name = "search", description = "Search the web")]
fn search(query: str) -> result<str, str> effects(io) { ... }
```
- `candorc mcp` emits a `tools.json` MCP tool manifest from annotated functions
- JSON Schema for each tool is generated from the Candor type signature automatically

### M7.2 — Semantic context embedding
```candor
#[intent("Computes the edit distance between two strings")]
fn levenshtein(a: str, b: str) -> i64 pure effects [] { ... }
```
- `candorc doc` extracts `#[intent]` annotations into a machine-readable context file
- Ready for RAG, embedding indexes, or direct tool-use by AI agents

### M7.3 — Effects as capability tokens
```candor
fn run_sandboxed<F: fn() -> unit effects(io)>(f: F, cap: cap<io>) -> unit { f() }
```
- A `cap<io>` value is a proof the caller has been granted the capability
- Passed explicitly; cannot be forged; enables compile-time sandbox enforcement

### M7.4 — `#[export_json]` for typed interfaces
```candor
#[export_json]
struct Config { name: str, limit: i64, tags: vec<str> }
```
- Auto-generates `config_from_json(str) -> result<Config, str>` and `config_to_json(Config) -> str`
- Useful for AI agents exchanging structured data without FFI boilerplate

---

## M8 — Ecosystem

### M8.1 — Package registry
- `Candor.toml` declares `[dependencies]` by name and semver
- `candorc fetch` downloads and pins to `Candor.lock`
- Local cache at `~/.candor/pkg/`; hosted registry at `candorpkg.io` (future)

### M8.2 — C/C++ interop improvements *(reprioritized: Medium)*
- `#[c_header("foo.h")]` auto-generates `extern fn` stubs from a C header
- Struct layout compatibility guarantee for plain-old-data types
- CUDA runtime header support: enables Candor code running in GPU inference stacks (NVIDIA Dynamo, TensorRT-LLM, vLLM) to call CUDA APIs directly without hand-written `extern fn` declarations

### M8.3 — Documentation generator
- `candorc doc --html` generates HTML reference from `///` doc comments
- Extracts `#[intent]` annotations, function signatures, effects, contracts

---

## M9 — Bootstrapping

> Goal: the Candor compiler is written in Candor and compiles itself.
>
> This is the most ambitious milestone. It does not require rewriting everything at once —
> each phase below is independently useful and can land incrementally.

### What "bootstrapped" means
The Go compiler remains the **build host** forever (like GCC's C host). Bootstrapping means:
1. A Candor source file describes the full compiler pipeline
2. The Go-compiled `candorc` compiles that Candor source into a binary
3. That binary can compile arbitrary Candor programs including itself

### Prerequisites (must land before M9)

| Requirement | Status | Notes |
|-------------|--------|-------|
| `vec::push` / `vec::pop` (realloc) | **Missing in LLVM backend** | C backend works; LLVM backend needs `realloc` strategy |
| `map::insert` / `map::remove` | **Missing in LLVM backend** | Same — C runtime works, LLVM deferred |
| String formatting (`str_format`) | Exists in `std::str` | Need to verify LLVM path |
| File I/O (`read_file`, `write_file`) | Exists in `std::io` | Extern-based, works in C backend |
| `option<ref<T>>` recursive types | **Not yet** | Needed for AST node trees (linked/tree structures) |
| Multi-file compilation of large projects | Exists | Module system works |

### M9.1 — `vec::push` and growable collections in LLVM backend
The LLVM backend currently handles vec/ring as value types but cannot grow them.
- Declare `@realloc(ptr, i64) -> ptr` in the IR header
- Implement `_cnd_vec_push` as an LLVM IR intrinsic or extern C helper
- Same for `ring::push`, `map::insert`, `set::insert`

### M9.2 — Recursive heap types (`box<T>`)
AST nodes are trees. To represent a tree in Candor you need a heap-allocated pointer type.
```candor
enum Expr {
    Int(i64),
    Add(box<Expr>, box<Expr>),
    Var(str),
}
```
- `box<T>` is a heap-allocated, owned pointer (like Rust's `Box<T>`)
- Desugars to `malloc` + `free` in C and LLVM backends
- Typeck enforces single ownership; no GC needed
- This is the single biggest language addition needed for bootstrapping

### M9.3 — Candor lexer written in Candor
A self-contained `lexer.cnd` that tokenizes Candor source text.
- Input: `str` (file contents)
- Output: `vec<Token>` where `Token` is a Candor struct `{ kind: TokenKind, lexeme: str, line: i64 }`
- `TokenKind` is a Candor enum covering all ~60 Candor token types
- No `extern fn` dependencies beyond `str` built-ins

### M9.4 — Candor parser written in Candor ✓ DONE
A recursive-descent parser producing a Candor-native AST (`src/compiler/parser.cnd`, 1116 lines).
- Full TypeExpr, Expr, Stmt, Decl AST using `box<T>` for recursive nodes
- All statement forms: let, return, if/else, loop, while, for-in, assert, break, continue, assign
- All declaration forms: fn, struct, enum, extern fn, const
- `parse(tokens, name)` entry point; `TestM9ParserSource` passes

### M9.5 — Type checker written in Candor ✓ DONE
All five phases complete. `src/compiler/typeck.cnd` (1249 lines) implements the full
type-checking pipeline. `TestM9TypeckSource` and `TestM9TypeckEmitC` pass.

**Phase 1 ✓** Type system representation: `Type`, `ScopeStack`, `TypeEnv`, `resolve_type`.
**Phase 2 ✓** Two-pass signature collection: all struct/enum/fn/extern declarations registered.
**Phase 3 ✓** Expression type inference: all `Expr` variants inferred.
**Phase 4 ✓** Statement/declaration checking: all `Stmt` variants checked.
**Phase 5 ✓** File-level entry point: `typecheck(pf: ref<ParsedFile>) -> TypedFile`.

### M9.6 — Code generator written in Candor ✓ DONE
`src/compiler/emit_c.cnd` provides the `emit_c` entry point used by `main.cnd`.

### M9.7 — Stage 1 bootstrap ✓ DONE
`candorc build src/compiler/Candor.toml` produces `candorc-stage1`. The full pipeline
`lex → parse → typecheck → emit_c` runs in a binary compiled from Candor source.
`go test ./...` is fully green.

### M9.8 — Stage 2 bootstrap (self-hosting)
Use `candorc-stage1` to compile itself:
```
candorc-stage1 build src/compiler/ --output candorc-stage2
diff <(candorc-stage1 compile test.cnd) <(candorc-stage2 compile test.cnd)
```
Identical output proves the compiler is self-consistent. The Go toolchain is no longer needed
for day-to-day Candor development.

---

## M10 — Async / Concurrency

> Goal: structured concurrency with effect-tracked async operations, motivated by
> disaggregated inference architectures like NVIDIA Dynamo where prefill→decode KV
> transfers, multi-tier memory management, and request scheduling are fundamentally async.

### Design philosophy

Candor's effects system extends naturally to async. A function that suspends is just a
function with `effects(async)` — the compiler generates a state machine, not callback soup.
Capability tokens (M7.3) let the borrow checker prove which hardware tier each async task
touches, preventing data races across GPU/CPU/network boundaries at compile time.

### M10.1 — `task<T>` and `spawn` blocks

The first, simpler step: a first-class future type backed by a thread pool or event loop.

```candor
fn fetch_row(db: ref<DB>, id: i64) -> task<result<Row, str>>
    effects(io)
{
    return spawn { db_query(db, id) }
}

fn run() -> unit effects(io) {
    let t1 = fetch_row(db, 1)
    let t2 = fetch_row(db, 2)
    let r1 = t1.join() must { ok(r) => r   err(e) => ... }
    let r2 = t2.join() must { ok(r) => r   err(e) => ... }
}
```

- `spawn { expr }` launches a task; returns `task<T>` where T is the type of `expr`
- `.join()` blocks the calling task until completion; returns `result<T, str>`
- `task::select(vec<task<T>>)` — returns the first completed task (for timeout patterns)
- Tasks inherit the effects of the spawning scope; no implicit capability laundering

### M10.2 — `effects(async)` — compiler-generated state machines

The deeper integration: `async fn` with compiler-lowered continuations. No heap allocation
per suspension point; the compiler allocates a fixed-size frame on the arena.

```candor
fn transfer_kv_block(src: ref<GPUBuffer>, dst: refmut<GPUBuffer>) -> unit
    effects(async, gpu, net)
{
    let handle = nixl_post_transfer(src, dst)    ## non-blocking post
    await handle                                  ## yield; resume on completion
    nixl_verify(handle)
}
```

- `await expr` is only valid inside `effects(async)` functions
- `await` desugars to a compiler-managed suspend/resume; no `async`/`await` keyword sprawl
- The effect propagates: calling an `effects(async)` fn from a sync context is a type error
- Compatible with `task<T>`: `spawn { async_fn() }` bridges the two models

### M10.3 — Expanded hardware effects for inference stacks ✓ DONE

`gpu`, `net`, `storage`, `mem`, `async` registered in `KnownEffects`; unknown effect names
produce a compiler warning. All hardware tiers are now recognized and subset-checked.
Motivated by Dynamo's disaggregated architecture where components must prove they only touch their assigned tier:

```candor
effects(gpu)     ## CUDA/VRAM access — prefill/decode compute workers
effects(net)     ## NIXL / InfiniBand / RoCE transfers — KV block migration
effects(storage) ## SSD / object store (S3, VAST) — KV cache spill
effects(mem)     ## CPU RAM — KV block manager, eviction policy logic
```

Combined with M7.3 capability tokens:

```candor
fn evict_lru(cache: refmut<KVCache>, needed: i64, _: cap<mem>) -> i64
    requires  needed > 0
    ensures   return >= 0
    effects(mem)
{ ... }
```

A Dynamo routing component that holds `cap<net>` but not `cap<gpu>` cannot accidentally
call a CUDA kernel — the type system rejects it at compile time.

### M10.4 — Shared ownership types + inference stdlib

#### `arc<T>` — atomic reference-counted shared ownership

The ownership model is incomplete without shared ownership. `box<T>` (single owner) cannot
express two concurrent decode workers reading the same prefill KV page, or multiple query
workers holding a reference to the same HNSW index shard.

```candor
arc_new(val: T)              -> arc<T>   ## allocate; refcount = 1
arc_clone(a: ref<arc<T>>)    -> arc<T>   ## atomic increment; O(1)
arc_deref(a: ref<arc<T>>)    -> ref<T>   ## borrow inner value
## drop decrements atomically; frees when count reaches 0
```

Canonical use — shared KV page in a radix tree:
```candor
struct KVPage { tokens: vec<i64>, data: arc<tensor<f16>>, layer: i64 }
struct RadixNode {
    children: map<i64, box<RadixNode>>
    page:     option<arc<KVPage>>      ## multiple decoders share; no copy
}
```

#### `pin<T>` — non-movable allocation (CUDA / DMA)

CUDA requires host buffers registered for zero-copy DMA to stay at a fixed physical address.
`pin<T>` is non-movable: the compiler rejects passing it by value; only `ref<pin<T>>` borrows
are permitted. Under the hood, `pin_new` calls `cudaMallocHost` or equivalent.

```candor
pin_new(val: T)          -> pin<T>      effects(gpu, mem)
pin_deref(p: ref<pin<T>>) -> ref<T>
pin_addr(p: ref<pin<T>>) -> u64         ## raw address for FFI / NIXL registration
## drop calls cudaFreeHost
```

#### Inference-serving stdlib types

| Type | Primary use |
|------|-------------|
| `heap<T>` (min/max priority queue) | SLA-driven request scheduling |
| `arena<T>` | Slab allocator; all KV blocks for one request freed atomically |
| `trie<K, V>` | Radix-tree prefix matching for KV cache overlap scoring |
| `weak<T>` | Weak reference to `arc<T>`; does not prevent deallocation (cache eviction) |

`arena<T>` pairs with the capability model: `cap<arena>` is handed to a request handler;
dropping the cap frees all arena-allocated blocks without tracing individual pointers.

`weak<T>` is essential for the KV radix tree: cache entries hold `weak<KVPage>` so an
eviction manager can free a page even while the tree still has a node referencing it.

### M10.5 — `#[dynamo_endpoint]` annotation

Extends M7.1 (MCP tool annotations) to emit NVIDIA Dynamo deployment descriptors:

```candor
#[dynamo_endpoint(model = "deepseek-r1", priority = HIGH, phase = DECODE)]
fn decode_step(kv: ref<KVBlock>, tokens: vec<i64>) -> result<vec<i64>, str>
    effects(gpu, async)
{ ... }
```

`candorc dynamo` emits a `DynamoGraphDeploymentRequest` YAML that registers the function
as a Dynamo worker, with phase, resource requirements, and SLA derived from the type
signature and annotations. The OpenAI-compatible frontend is auto-wired.

---

---

## M11 — Tensor & ML Primitives

> Goal: make Candor a first-class language for ML inference workloads — not by bundling
> a framework, but by giving the core the primitives that frameworks are built on.
> `f16`/`bf16`, `tensor<T>`, and SIMD intrinsics belong in core because they affect
> codegen, ABI, and memory layout. Implementations live in `std/`.

### M11.1 — `f16` and `bf16` primitive types

Modern embedding vectors and KV caches use half-precision storage. These are not library
types — they need dedicated LLVM IR types (`half`, `bfloat`), arithmetic promotion rules
(operations widen to `f32`), and literal syntax.

```candor
let v: f16  = 1.5h16          ## half-precision literal
let w: bf16 = 1.5bf16         ## bfloat16 literal
## arithmetic: f16 + f16 → f32 (auto-promoted); explicit cast to narrow
```

Without these, a Candor program cannot represent an embedding vector or KV cache tensor
in the native format that CUDA kernels expect — forcing lossy f32 upcasting everywhere.

### M11.2 — `tensor<T>` builtin type

Multi-dimensional dense array with runtime shape and strides. Differs from `vec<T>` in
that it is N-dimensional, supports non-contiguous views (slices, transposes), and its
layout is ABI-visible so CUDA extern fn bindings know the stride.

```candor
## shape is runtime; layout defaults to row-major C order
let emb: tensor<f16> = tensor_zeros([batch, seq_len, d_model])
let kv:  tensor<f16> = tensor_zeros([n_layers, 2, n_heads, seq_len, head_dim])

## strided view — zero copy, shares backing allocation
let layer0: tensor<f16> = tensor_slice(kv, [0, .., .., .., ..])

## CUDA kernel call — tensor ABI passes (ptr, shape, strides)
extern fn cuda_attn(q: ref<tensor<f16>>, k: ref<tensor<f16>>,
                    v: ref<tensor<f16>>, out: refmut<tensor<f16>>) -> unit effects(gpu)
```

Key design:
- `tensor<T>` owns its flat `vec<T>` storage (or borrows via `ref<tensor<T>>`)
- `arc<tensor<T>>` is the shared-ownership form for KV cache pages
- `pin<tensor<T>>` is the DMA-registered form for NIXL zero-copy transfers
- Shape/stride metadata lives adjacent to the data pointer (fat pointer ABI)

### M11.3 — SIMD distance intrinsics

Dot product, L2 norm, and cosine similarity are the three operations every vector DB
and attention mechanism reduces to. Making them compiler intrinsics (not extern fn) lets
the LLVM backend emit vectorized `llvm.fmuladd` / `llvm.experimental.vector.reduce.add`
and auto-select AVX-512 / NEON / WASM SIMD without the user writing platform-specific code.

```candor
fn vec_dot(a: ref<tensor<f32>>, b: ref<tensor<f32>>) -> f32 pure effects(simd)
fn vec_l2(a: ref<tensor<f32>>) -> f32 pure effects(simd)
fn vec_cosine(a: ref<tensor<f32>>, b: ref<tensor<f32>>) -> f32 pure effects(simd)
fn tensor_matmul(a: ref<tensor<f32>>, b: ref<tensor<f32>>,
                 out: refmut<tensor<f32>>) -> unit effects(simd)
```

`effects(simd)` documents that the operation uses SIMD width; it is always `pure`
(no I/O side effects) and its presence tells the effects checker it may not run on
hardware without SIMD support.

### M11.4 — `std/tensor.cnd` — shape arithmetic, broadcast, reshape

Pure Candor implementations of the tensor ops the compiler doesn't intrinsify:

- `tensor_reshape`, `tensor_transpose`, `tensor_broadcast_to`
- `tensor_cat`, `tensor_stack`, `tensor_split` (concatenation along a dimension)
- `tensor_to_vec` / `tensor_from_vec` bridges for interop with existing `vec<T>` code
- Softmax, layer-norm, ReLU/GELU activation fns (pure, `effects(simd)`)

### M11.5 — `std/vecdb.cnd` — HNSW and IVF indexes in pure Candor

Vector database index structures implemented in Candor using `arc<T>` and `tensor<T>`:

```candor
## HNSW (Hierarchical Navigable Small World) — approximate nearest neighbor
struct HnswNode {
    id:          i64
    vec:         arc<tensor<f16>>     ## shared — multiple layers reference same vec
    neighbors:   vec<vec<i64>>        ## per-layer neighbor lists
}
struct HnswIndex { nodes: vec<arc<HnswNode>>, ef_construction: i64, M: i64 }

fn hnsw_insert(idx: refmut<HnswIndex>, vec: tensor<f16>, id: i64) -> unit
fn hnsw_search(idx: ref<HnswIndex>, query: ref<tensor<f16>>, k: i64) -> vec<i64>

## IVF (Inverted File Index) — coarse quantization + refine
struct IvfIndex { centroids: tensor<f32>, lists: vec<vec<i64>> }
fn ivf_search(idx: ref<IvfIndex>, query: ref<tensor<f32>>, nprobe: i64, k: i64) -> vec<i64>
```

The `arc<HnswNode>` means multiple concurrent queries can walk the graph simultaneously
with no locking on reads — each query holds its own `arc` clone for the duration.

---

## M12 — Advanced Storage Layer

> Goal: Candor programs that manage multi-tier storage (GPU VRAM → CPU RAM → NVMe → object
> store) should be able to express tier-crossing operations as type-safe, effect-annotated
> code — not raw pointer arithmetic. Motivated by Dynamo's disaggregated KV store.

### M12.1 — `mmap<T>` — memory-mapped file allocation

Large HNSW indexes (50–500 GB), embedding databases, and KV cache spill files exceed
heap capacity. `mmap<T>` is a file-backed allocation owned by the OS page cache:

```candor
## open or create a memory-mapped region backed by a file
fn mmap_open(path: str, byte_len: u64) -> result<mmap<u8>, str> effects(storage)
fn mmap_deref(m: ref<mmap<T>>) -> ref<T>
fn mmap_flush(m: ref<mmap<T>>) -> unit effects(storage)
## drop calls msync + munmap
```

The compiler ensures `mmap<T>` cannot be moved (like `pin<T>`) since the OS mapping is
tied to the address. An HNSW index deserialized from disk becomes `mmap<HnswIndex>` —
operating directly on mapped memory without a heap copy.

### M12.2 — Column store primitives (`std/colstore.cnd`)

Embedding batches, token sequences, and KV cache metadata are naturally columnar — storing
all embeddings in one flat tensor and all token IDs in another is more cache-efficient than
interleaved row structs. `std/colstore.cnd` provides:

```candor
## Arrow-compatible column layout — each field is a contiguous typed buffer
struct ColBatch {
    row_count: i64
    columns:   map<str, tensor<u8>>    ## type-erased; cast at read time
}
fn col_get<T>(batch: ref<ColBatch>, name: str) -> ref<tensor<T>>
fn col_put<T>(batch: refmut<ColBatch>, name: str, data: tensor<T>) -> unit
```

This is the native format for passing embedding batches to CUDA kernels and for
serializing KV cache metadata to NVMe. Effect annotation: `effects(storage)` on
any `ColBatch` operation that touches a `mmap`-backed column.

### M12.3 — `std/nixl.cnd` — NIXL zero-copy transfer bindings

NVIDIA NIXL (Inference Transfer Library) enables zero-copy GPU↔CPU↔NVMe transfers
over RDMA (InfiniBand / RoCE) for disaggregated prefill→decode KV migration.
These bindings follow the same pattern as `src/std/wasm.cnd` — pure `extern fn`
declarations with effect annotations; no NIXL dependency in Candor core.

```candor
## std/nixl.cnd — wire at link time: link against libnixl.so
## Only needed for --target that includes NIXL disaggregation.

## Register a pin<tensor<T>> buffer for NIXL zero-copy DMA
extern fn nixl_register(ptr: u64, byte_len: u64, mem_type: i32) -> u64
    effects(mem, gpu)

## Initiate async zero-copy transfer between registered handles
extern fn nixl_transfer(src: u64, dst: u64, src_off: u64,
                        dst_off: u64, byte_len: u64) -> u64
    effects(async, gpu, net)

## Poll a pending transfer (integrates with effects(async) await)
extern fn nixl_poll(transfer_id: u64) -> i32  effects(async)

## Disaggregated KV: send one layer's KV block from prefill to decode node
extern fn nixl_send_kv(src: u64, remote_rank: i32,
                       layer: i32, head: i32) -> u64  effects(async, gpu, net)

extern fn nixl_deregister(handle: u64) -> unit  effects(mem, gpu)
```

### M12.4 — `std/kvcache.cnd` — radix-tree KV cache in pure Candor

Built on `arc<T>`, `pin<tensor<f16>>`, `std/nixl.cnd`, and `effects(async, gpu, mem)` —
a production-quality KV cache with:

- Radix tree for prefix deduplication across concurrent requests
- `arc<KVPage>` so multiple decode workers share prefill pages without copying
- `weak<KVPage>` in tree nodes so the eviction manager can free pages without dangling refs
- Multi-tier eviction: GPU VRAM → CPU RAM → NVMe → object store, each tier annotated
  with its effect (gpu / mem / storage / net)
- NIXL integration for migrating KV blocks between disaggregated prefill and decode nodes

---

## Milestone Timeline (rough order, not calendar-bound)

```
Done  ──── v0.1   Core language, C backend, closures, effects, contracts
           M1     Compound assign, tuple destruct, struct update, ring iter
           M2     Standard library (math, str, io, os, time, rand, path)
           M3     Trait system (trait, impl Trait for, bounds, dispatch)
           M4.x   Diagnostics, build system, LSP, formatter, test runner
           M5.1   LLVM IR backend (feature-complete as of today)
           M5.2   Debug / release builds
           M5.3   Sanitizer integration

Near  ──── M5.5   WebAssembly target  ✓ DONE
           M6.1   Symbolic contract evaluation (extend ComptimeValues)
           M8.2   C/C++ / CUDA header interop  ← reprioritized (Dynamo GPU FFI)
           M10.4  arc<T>, pin<T>, weak<T> + inference stdlib  ← new priority

Medium ─── M6.2   SMT integration (Z3 / CVC5)
           M6.3   Refinement types
           M7.1   MCP tool annotations
           M7.2   Semantic context embedding
           M7.3   Effects as capability tokens (cap<gpu>, cap<net>, cap<mem>)
           M8.1   Package registry
           M10.1  task<T> / spawn structured concurrency
           M10.3  Expanded hardware effects  ✓ DONE
           M11.1  f16 / bf16 primitive types
           M11.2  tensor<T> builtin type
           M11.3  SIMD distance intrinsics (vec_dot, vec_l2, vec_cosine)
           M12.1  mmap<T> memory-mapped file allocation
           M12.3  std/nixl.cnd — NIXL zero-copy transfer bindings

Far   ──── M6.4   forall / exists runtime + solver
           M7.4   export_json codegen
           M8.3   Doc generator
           M9.1   vec::push / growable collections in LLVM  ✓ DONE
           M9.2   box<T> recursive heap types  ✓ DONE
           M9.3   Candor lexer in Candor  ✓ DONE
           M9.4   Candor parser in Candor  ✓ DONE
           M9.5   Type checker in Candor  ✓ DONE
           M9.6   C code generator in Candor  ✓ DONE
           M9.7   Stage 1 bootstrap  ✓ DONE  (go test ./... all green)
           M10.2  effects(async) — compiler state machine lowering
           M10.5  #[dynamo_endpoint] annotation + DGDR codegen
           M11.4  std/tensor.cnd — shape arithmetic, broadcast, reshape
           M11.5  std/vecdb.cnd — HNSW + IVF in pure Candor
           M12.2  std/colstore.cnd — columnar batch storage
           M12.4  std/kvcache.cnd — radix-tree KV cache (arc<T> + NIXL)

Goal  ──── M9.8   Stage 2 bootstrap (candorc-stage1 compiles itself)
```

---

## Contribution priorities

| Item | Difficulty | Impact |
|------|------------|--------|
| `arc<T>` + `pin<T>` + `weak<T>` (M10.4) | Medium | Critical — required for KV cache sharing, NIXL |
| `f16` / `bf16` primitive types (M11.1) | Low | Very high — ML workloads can't express embeddings without it |
| `tensor<T>` builtin type (M11.2) | High | Very high — foundational for all ML data movement |
| C/CUDA header interop (M8.2) | Medium | Very high — GPU FFI for Dynamo/TRT/vLLM |
| `task<T>` / `spawn` concurrency (M10.1) | High | Very high — async inference pipelines |
| `effects(async)` state machine (M10.2) | Very high | Very high — deep async integration |
| SIMD distance intrinsics (M11.3) | Medium | High — vec_dot/cosine perf without AVX boilerplate |
| `std/nixl.cnd` NIXL bindings (M12.3) | Medium | High — zero-copy disaggregated KV transfer |
| `mmap<T>` memory-mapped files (M12.1) | Medium | High — large index support without heap copy |
| Capability tokens `cap<T>` (M7.3) | Medium | High — compile-time hardware tier enforcement |
| Symbolic contract eval (M6.1) | Medium | High — improves correctness guarantees |
| MCP tool annotations (M7.1) | Medium | High — unlocks AI agent tooling |
| Package registry (M8.1) | High | Very high — unlocks ecosystem growth |
| `heap<T>`, `arena<T>`, `trie<K,V>`, `weak<T>` stdlib (M10.4) | Medium | High — inference scheduling |
| `std/vecdb.cnd` HNSW + IVF (M11.5) | High | High — native vector DB without FAISS dep |
| `std/kvcache.cnd` radix KV cache (M12.4) | High | Very high — complete Dynamo KV layer |
| SMT integration (M6.2) | Very high | Medium — research milestone |

---

*Candor is open source. This roadmap reflects current priorities and will shift as the language
grows. The bootstrapping path (M9) is aspirational — every step is independently useful even
if full self-hosting is years away.*
