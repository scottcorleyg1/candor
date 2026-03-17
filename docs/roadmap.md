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

### Known language gaps (not yet wired)
- Named-return / early-exit in closures
- `forall` / `exists` quantifiers (tokens exist, not runtime-wired)
- `invariant` clauses (token exists, not wired)

---

## Next Up

### M5.5 — WebAssembly target
Emit WASM via LLVM's `wasm32-unknown-unknown` target.

- `--target=wasm32` selects WASM emission
- Provide a `std::wasm` module with browser-side `extern fn` bindings
- Candor↔JS interop layer for passing strings and typed values

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

### M8.2 — C/C++ interop improvements
- `#[c_header("foo.h")]` auto-generates `extern fn` stubs from a C header
- Struct layout compatibility guarantee for plain-old-data types

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

### M9.5 — Type checker written in Candor
The hardest phase. The typeck pass is ~3000 lines of Go and maintains several maps over the AST.
- Requires `map<K,V>` runtime operations (M9.1)
- Requires passing `box<T>` values as keys (needs `Hash` trait — M3 already done)
- Can be done incrementally: start with type inference only, add effects/contracts later

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

Medium ─── M6.2   SMT integration (Z3 / CVC5)
           M6.3   Refinement types
           M7.1   MCP tool annotations
           M7.2   Semantic context embedding
           M8.1   Package registry

Far   ──── M6.4   forall / exists runtime + solver
           M7.3   Effects as capability tokens
           M7.4   export_json codegen
           M8.2   C/C++ header interop
           M8.3   Doc generator
           M9.1   vec::push / growable collections in LLVM  ✓ DONE
           M9.2   box<T> recursive heap types  ✓ DONE
           M9.3   Candor lexer in Candor  ✓ DONE
           M9.4   Candor parser in Candor  ✓ DONE
           M9.5   Type checker in Candor
           M9.6   C code generator in Candor

Goal  ──── M9.7   Stage 1 bootstrap (Go candorc compiles Candor compiler)
           M9.8   Stage 2 bootstrap (Candor compiler compiles itself)
```

---

## Contribution priorities

| Item | Difficulty | Impact |
|------|------------|--------|
| `vec::push` in LLVM backend (M9.1) | Medium | Very high — unlocks bootstrapping path |
| Cross-compilation `--target` (M5.4) | Low | High — just pass the triple through |
| Symbolic contract eval (M6.1) | Medium | High — improves correctness guarantees |
| `box<T>` heap type (M9.2) | High | Very high — required for bootstrapping |
| MCP tool annotations (M7.1) | Medium | High — unlocks AI agent tooling |
| Package registry (M8.1) | High | Very high — unlocks ecosystem growth |
| SMT integration (M6.2) | Very high | Medium — research milestone |

---

*Candor is open source. This roadmap reflects current priorities and will shift as the language
grows. The bootstrapping path (M9) is aspirational — every step is independently useful even
if full self-hosting is years away.*
