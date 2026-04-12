# Candor — Completed Milestones

> Everything listed here is shipped, tested, and merged to `main`.
> For open work, see [Roadmap_OpenItems.md](Roadmap_OpenItems.md).
> For the full technical reference, see [Roadmap_Combined.md](Roadmap_Combined.md).

---

## Foundation

### v0.1 — Core Language
The original working compiler. Everything else builds on this.

- All primitive types: `i8`–`i64`, `u8`–`u64`, `f32`, `f64`, `bool`, `str`, `unit`
- Structs, enums with payloads, generics
- Closures, effects system, contracts (`requires`/`ensures`), pattern matching
- Full C emission backend
- `result<T,E>` and `option<T>` built-in types

---

## M1 — Language Ergonomics
Quality-of-life additions that make day-to-day code cleaner.

- Compound assignment: `+=`, `-=`, `*=`, `/=`
- Tuple destructuring: `let (a, b) = pair`
- Struct update syntax: `Foo { x: 1, ..base }`
- Map index assignment: `m["key"] = val`
- Ring buffer iteration: `for x in ring { ... }`
- Closures captured by reference

---

## M2 — Standard Library
A practical stdlib so programs can actually do useful things.

- `std::math` — floor, ceil, sqrt, abs, min, max, trig
- `std::str` — split, join, contains, trim, format, parse
- `std::io` — read_file, write_file, print, stdin
- `std::os` — env vars, exit, args
- `std::time` — now, sleep
- `std::rand` — random number generation
- `std::path` — path join, exists, basename

---

## M3 — Trait System
Interfaces that work across the entire type system.

- `trait` declarations with method signatures
- `impl Trait for Type` implementations
- Trait bounds on generic type parameters: `fn foo<T: Display>(x: T)`
- Static dispatch via monomorphization (zero-cost at runtime)

---

## M4 — Developer Tooling
The four tools that make a language actually usable day-to-day.

### M4.1 — Diagnostic Quality
- Source snippets with caret pointers in error messages
- "Did you mean?" suggestions for typos
- Unused variable and variable shadowing warnings
- Multiple errors collected in a single pass (no stopping at first error)

### M4.2 — Build System
- `Candor.toml` project manifest
- `candorc build` with `--release` flag
- Automatic source file discovery

### M4.3 — LSP Server
- `candorc lsp` — JSON-RPC 2.0 language server
- Diagnostics, hover, go-to-definition, completion in any LSP-capable editor

### M4.4 — Formatter
- `candorc fmt` — canonical code formatter
- AST-based, idempotent (running it twice produces the same result)

### M4.5 — Test Framework
- `#test` directive to mark test functions
- `candorc test` runner with pass/fail reporting

---

## M5 — Compilation Backends

### M5.1 — LLVM IR Backend
A second backend targeting LLVM for optimized native code.

- Full `.ll` text emitter
- `--backend=llvm` flag
- No CGo dependency — pure Go implementation
- Feature-complete: enum payload binding, tuple destructure, closures (fat pointer),
  map iteration, vec/ring index read/write, `for-in` loops, `&expr` address-of

### M5.2 — Debug and Release Builds
- `--debug` flag: assertions enabled, no optimization
- `--release` flag: assertions elided, optimization on
- Shared `BuildConfig` struct used by both C and LLVM backends

### M5.3 — Sanitizer Integration
- `--sanitize=address` (ASan), `undefined` (UBSan), `memory` (MSan), `leak` (LSan), `thread` (TSan)
- Passed through to clang/gcc at compile time

### M5.4 — Cross-Compilation
- `--target=<triple>` flag
- Passed as `--target=` to clang and as `target triple` in LLVM IR
- Empty = host default

### M5.5 — WebAssembly Target
- `--target=wasm32` normalizes to `wasm32-unknown-unknown`
- WASM-specific flags: `-nostdlib --no-entry --export-all`
- `.wasm` output extension
- `src/std/wasm.cnd` with browser and WASI bindings:
  `wasm_console_log`, `wasm_canvas_fill_rect`, `fd_write`, `proc_exit`

---

## M6 — Formal Verification (Partial)

### M6.1 — Symbolic Contract Evaluation
Contracts evaluated at compile time when all arguments are constants.

- `requires x > 0` checked before the binary is even built
- Violated clauses produce compile-time errors
- Proven-safe calls have their runtime asserts elided

### M6.4 — `forall` / `exists` Runtime Quantifiers
Loop-based quantifier assertions in debug/test mode.

- `forall x in coll : pred` syntax
- `exists x in coll : pred` syntax
- Typeck enforces `vec<T>`/`ring<T>` collection with `bool` predicate
- C backend emits GCC statement-expression loops

> **Note:** M6.2 (SMT solver) and M6.3 (refinement types) are still open.

---

## M7 — AI Integration Layer

### M7.1 — MCP Tool Annotations
- `#mcp_tool "description"` directive on functions
- `candorc mcp` emits a `tools.json` manifest with JSON Schema derived from Candor types

### M7.2 — Semantic Context Embedding
- `#intent "description"` directive on functions
- `candorc doc` emits `intent.json` for RAG/embedding indexes

### M7.3 — Capability Tokens
Type-level proof that a caller has been granted a named capability.

- `cap Name` declaration
- `cap<Name>` is a zero-size proof type at runtime
- `cap(X)` function annotation enforced at call sites
- C backend emits `typedef uint8_t cap_Name`

### M7.4 — `#export_json` Struct Directive
- Auto-generates `Struct_to_json(S) -> str` and `Struct_from_json(str) -> result<S,str>`
- Supports `str`, `bool`, integer, and float fields

---

## M8 — Ecosystem

### M8.1 — Package Registry
- `[dependencies]` section in `Candor.toml`
- `candorc fetch` downloads and pins to `Candor.lock`
- Local cache at `~/.candor/pkg/`

### M8.2 — C / CUDA Header Interop
- `#c_header "foo.h"` directive auto-generates `extern fn` stubs
- Struct layout compatibility for plain-old-data types
- CUDA runtime header support for direct GPU API calls

### M8.3 — Documentation Generator
- `///` doc-comment syntax
- `candorc doc --html` emits self-contained HTML reference docs
- Shows function signatures, effects tags, contract badges, intent strings

---

## M9 — Bootstrapping (In Progress)

### M9.1 — Growable Collections in LLVM Backend
- `vec_push` (realloc-based growth), `vec_pop`
- `ring_push_back` with linearize-copy growth
- `@realloc` declared in LLVM IR header

### M9.2 — `box<T>` Recursive Heap Types
The key prerequisite for writing tree-structured ASTs in Candor.

- `box_new`, `box_deref`, `box_drop` builtins
- C backend: `T*`; LLVM backend: `ptr`
- `none → option<T>` coercion added

### M9.3 — Candor Lexer Written in Candor
`src/compiler/lexer.cnd` — a complete Candor tokenizer written in Candor.

- Input: `str`; Output: `vec<Token>`
- All ~60 Candor token kinds
- No extern dependencies beyond `str` builtins

### M9.4 — Candor Parser Written in Candor
`src/compiler/parser.cnd` — 1116-line recursive-descent parser.

- Full AST: `TypeExpr`, `Expr`, `Stmt`, `Decl`
- `box<T>` for recursive AST nodes
- All statement and declaration forms
- `parse(tokens, name)` entry point

### M9.5 — Type Checker Written in Candor (All Phases)
`src/compiler/typeck.cnd` — the hardest bootstrapping phase, completed in 5 increments.

- **Phase 1:** Type system representation — `Type` struct, `ScopeStack`, `TypeEnv`, `resolve_type`
- **Phase 2:** Two-pass signature collection — all structs, enums, fns, externs registered
- **Phase 3:** Expression type inference — 15-variant `Expr` ADT, `infer_expr`
- **Phase 4:** Statement checking — 11-variant `Stmt` ADT, `check_stmt`
- **Phase 5:** File-level entry point — `typecheck(file)` producing `TypedFile`

> **Still open:** M9.6 (C emitter in Candor), M9.7 (stage-1 bootstrap), M9.8 (self-hosting).

---

## M10 — Concurrency and Ownership (Partial)

### M10.1 — `task<T>` and `spawn` Structured Concurrency
- `spawn { return expr }` starts a pthread; returns `task<T>`
- `.join()` blocks and returns `result<T, str>`
- Per-spawn context struct heap-allocated and passed to `pthread_create`
- `#include <pthread.h>` emitted automatically when spawns are present

### M10.3 — Hardware Effect Tiers
- `gpu`, `net`, `storage`, `mem`, `async` added to `KnownEffects`
- Unknown effect names produce a compiler warning
- Subset-checking enforced: a `effects(gpu)` caller cannot call `effects(gpu, net)` unless it also declares `net`

### M10.4 — `arc<T>` Shared Reference-Counted Ownership
Atomic reference counting for shared data (e.g. shared KV cache pages).

- `arc_new`, `arc_clone`, `arc_deref`, `arc_drop` builtins
- C backend: `[int64 refcount][T]` layout with `__sync_fetch_and_add`
- LLVM backend: `atomicrmw` instructions

> **Still open within M10.4:** `pin<T>`, `weak<T>`, `heap<T>`, `arena<T>`, `trie<K,V>` —
> see Open Items.

---

## M11 — Tensor and ML Primitives (Partial)

### M11.1 — `f16` and `bf16` Primitive Float Types
- `f16` → `_Float16` in C, `half` in LLVM IR
- `bf16` → `__bf16` in C, `bfloat` in LLVM IR
- Implicit widening: `f16 → f32 → f64` and `bf16 → f32 → f64`
- `1.5h16` and `1.5bf16` literal syntax

### M11.2 — `tensor<T>` Builtin Type
N-dimensional dense array with runtime shape and strides.

- `tensor_zeros([d0, d1, ...])`, `tensor_from_vec`, `tensor_to_vec`
- `tensor_get`, `tensor_set`, `tensor_ndim`, `tensor_shape`, `tensor_len`, `tensor_free`
- ABI-visible layout so CUDA `extern fn` bindings know the stride

### M11.3 — SIMD Distance Intrinsics
Compiler intrinsics for the three core vector operations.

- `tensor_dot`, `tensor_l2`, `tensor_cosine`, `tensor_matmul`
- `effects(simd)` — documents SIMD requirement; always pure
- C backend emits scalar loops with `_Pragma("GCC ivdep")` auto-vectorization hints
- `candorc build --simd` adds `-O3 -ftree-vectorize -march=native` for the C compiler to
  select the best SIMD width (AVX-512, NEON, WASM SIMD) automatically

> **Honest status:** The implementation uses C compiler auto-vectorization, not hand-coded
> LLVM vector intrinsics. The LLVM backend does not yet implement tensor operations.
> Hand-written `llvm.fmuladd` / vector-reduce intrinsics are tracked as a future open item.

---

## M12 — Advanced Storage Layer (Partial)

### M12.1 — `mmap<T>` Memory-Mapped File Allocation
File-backed allocations for datasets too large for the heap.

- `mmap_open(path, byte_len)`, `mmap_anon(byte_len)`, `mmap_deref`, `mmap_flush`, `mmap_close`, `mmap_len`
- Non-movable (like `pin<T>`) — OS mapping is tied to the address
- `effects(storage)` on all operations

> **Still open:** M12.2 (colstore), M12.3 (NIXL), M12.4 (kvcache) — see Open Items.
