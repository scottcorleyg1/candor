# Candor Compiler Roadmap

> **Living document.** Updated as milestones land. Each milestone maps to concrete compiler work
> with a clear definition of done. Items are intentions, not promises.

---

## Where We Are Today (2026-03-16)

The Candor compiler is a **production-quality single-pass compiler** written in Go. It has two
code-generation backends (C and LLVM IR) and a full language surface including generics, closures,
traits, effects, contracts, pattern matching, and a standard library.

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
| **M9.1** | `vec::push` and growable collections in LLVM backend: `vec_push` (realloc-based grow), `vec_pop`, `ring_push_back` (linearize-copy grow); `@realloc` declared in IR header; inline IR via `emitBuiltinCall` |
| **M9.2** | `box<T>` recursive heap types: `box_new` (malloc+store), `box_deref` (load), `box_drop` (free); C backend: `T*`; LLVM backend: `ptr`; `none → option<T>` coercion added |
| **M9.3** | Candor lexer written in Candor (`src/compiler/lexer.cnd`): all token kinds, keyword map, scanners for ident/int/float/str/directive/sym; `TestM9LexerSource` passes |
| **M9.4** | Candor parser written in Candor (`src/compiler/parser.cnd`): full AST (TypeExpr, Expr, Stmt, Decl), recursive-descent parser with `box<T>` for recursive nodes; `TestM9ParserSource` passes |
| **M10.3** | Hardware effect tiers: `gpu`, `net`, `storage`, `mem`, `async` added to `KnownEffects`; unknown effect names produce a compiler warning; subset-checking enforced across all new tiers |

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

### M9.5 — Type checker written in Candor *(in progress — Phase 1 done)*
The hardest phase. The typeck pass is ~3000 lines of Go and maintains several maps over the AST.
Implemented incrementally; each phase verified by `TestM9TypeckSource`.

**Phase 1 ✓** (`src/compiler/typeck.cnd`, 450 lines): Type system representation
(`Type` struct with kind/prim/name/params/fn_ret), all primitive types, type constructors,
`ty_show`/`ty_eq`/`ty_coerce`, `ScopeStack` (linked-list scopes via `map<str,Type>`),
`TypeEnv` (struct/enum/fn registries + error/warning accumulators), `resolve_type` (TypeExpr → Type).

**Phase 2** (next): signature collection pass — register all struct/enum/fn declarations into TypeEnv.
**Phase 3**: expression type inference.
**Phase 4**: statement + declaration checking.
**Phase 5**: file-level entry point + full integration.

### M9.6 — Code generator written in Candor (C backend first)
Start with C emission since the output is plain text and easy to debug.
- A `str`-accumulator approach: build up C source as a `str` (or `vec<str>` lines)
- Emit each AST node form as a C fragment
- Write output via `std::io::write_file`

### M9.7 — Stage 1 bootstrap
Compile the Candor compiler written in Candor using the Go-compiled `candorc`:
```
candorc build src/compiler/  --output candorc-stage1
```
Run a test suite comparing Go-compiled output vs. Candor-compiled output on the same inputs.

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

### M10.4 — Stdlib: `heap<T>`, `arena<T>`, `trie<K, V>`

Inference serving needs data structures not in the current stdlib:

| Type | Primary use in Dynamo-style systems |
|------|-------------------------------------|
| `heap<T>` (min/max priority queue) | SLA-driven request scheduling; route highest-priority request first |
| `arena<T>` | Slab allocator; all KV blocks for one request lifetime freed atomically |
| `trie<K, V>` | Radix-tree prefix matching for KV cache overlap scoring |

`arena<T>` pairs with the capability model: `cap<arena>` is handed to a request handler;
dropping the cap frees all arena-allocated blocks without tracing individual pointers.

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

Near  ──── M5.5   WebAssembly target
           M6.1   Symbolic contract evaluation (extend ComptimeValues)
           M8.2   C/C++ / CUDA header interop  ← reprioritized (Dynamo GPU FFI)

Medium ─── M6.2   SMT integration (Z3 / CVC5)
           M6.3   Refinement types
           M7.1   MCP tool annotations
           M7.2   Semantic context embedding
           M7.3   Effects as capability tokens (cap<gpu>, cap<net>, cap<mem>)
           M8.1   Package registry
           M10.1  task<T> / spawn structured concurrency
           M10.3  Expanded hardware effects (gpu, net, storage, mem)  ✓ DONE
           M10.4  Stdlib: heap<T>, arena<T>, trie<K,V>

Far   ──── M6.4   forall / exists runtime + solver
           M7.4   export_json codegen
           M8.3   Doc generator
           M9.1   vec::push / growable collections in LLVM  ✓ DONE
           M9.2   box<T> recursive heap types  ✓ DONE
           M9.3   Candor lexer in Candor  ✓ DONE
           M9.4   Candor parser in Candor  ✓ DONE
           M9.5   Type checker in Candor
           M9.6   C code generator in Candor
           M10.2  effects(async) — compiler state machine lowering
           M10.5  #[dynamo_endpoint] annotation + DGDR codegen

Goal  ──── M9.7   Stage 1 bootstrap (Go candorc compiles Candor compiler)
           M9.8   Stage 2 bootstrap (Candor compiler compiles itself)
```

---

## Contribution priorities

| Item | Difficulty | Impact |
|------|------------|--------|
| `task<T>` / `spawn` concurrency (M10.1) | High | Very high — unlocks async inference pipelines |
| C/CUDA header interop (M8.2) | Medium | Very high — GPU FFI for Dynamo/TRT/vLLM |
| Expanded hardware effects (M10.3) | Low | High — ties effects system to real hardware tiers |
| Capability tokens `cap<T>` (M7.3) | Medium | High — compile-time hardware tier enforcement |
| Symbolic contract eval (M6.1) | Medium | High — improves correctness guarantees |
| MCP tool annotations (M7.1) | Medium | High — unlocks AI agent tooling |
| Package registry (M8.1) | High | Very high — unlocks ecosystem growth |
| `heap<T>`, `arena<T>`, `trie<K,V>` stdlib (M10.4) | Medium | High — inference scheduling primitives |
| `effects(async)` state machine (M10.2) | Very high | Very high — deep async integration |
| SMT integration (M6.2) | Very high | Medium — research milestone |

---

*Candor is open source. This roadmap reflects current priorities and will shift as the language
grows. The bootstrapping path (M9) is aspirational — every step is independently useful even
if full self-hosting is years away.*
