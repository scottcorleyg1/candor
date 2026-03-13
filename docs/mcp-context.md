# Candor Language — Standing Context for Claude Code
## Knowledge Item v0.2

---

## What This Project Is

**Candor** is a new systems programming language being designed and built by Scott W. Corley (© 2026, Apache 2.0). The GitHub repo is `scottcorleyg1/candor`.

The compiler is called `candorc`. **It exists and works.** Version 0.0.1 is complete. It compiles real `.cnd` programs to binaries via a C transpilation pipeline.

The full language specification is in `SPEC.md` at the repo root. The compiler implementation is in `compiler/` (Go 1.24, module `github.com/scottcorleyg1/candor/compiler`). The language reference for the implemented language is in `docs/language-reference.md`.

---

## The Prime Directive

> A Candor program is a complete, honest declaration of everything it does. Nothing is hidden. Nothing is assumed. Nothing is implicit.

Every design decision, every compiler feature, every generated example must be evaluated against this principle. If a construct allows something to be implicit, hidden, or assumed — it does not belong in Candor.

---

## Compiler Pipeline

**Phase 1 (current):** `.cnd` source → lex → parse → typeck → emit C → gcc/cc → binary.

**Phase 2 (future):** Direct LLVM IR emission.

**CLI:**

```bash
candorc program.cnd                          # single file
candorc main.cnd module1.cnd module2.cnd    # multi-file
```

Output: binary placed next to the first source file. On Windows: `.exe`. The emitted `.c` file is also written for inspection. Override C compiler with `CC` environment variable.

---

## The Layer Stack

Layers are strictly ordered. Each depends on all layers above it.

```
Core             primitives, control flow, result<T,E>, option<T>, structs   [IMPLEMENTED]
effects          declare what a function touches — zero runtime cost          [IMPLEMENTED]
contracts        requires / ensures / assert                                  [IMPLEMENTED]
collections      vec<T> ring<T> — push, len, index, for                      [IMPLEMENTED]
tags             @[pure] @[secret] @[retryable] — verified claims             [future]
natural          @[intent] @[explain] @[example] — intent-first development   [future]
allocators       explicit, declared allocation strategies                      [future]
channels         ownership-safe intra-process message passing                 [future]
network          typed endpoints, auto-parallelism, latency resilience        [future]
realtime         WCET contracts, ISR constraints, priority inheritance        [future]
crypto           constant-time, zeroize-on-drop, verified entropy             [future]
semantic_index   compile-time .csi artifact — meaning-based search           [future]
c_interop        honest C boundary — @[opaque_effects] required               [future]
llvm_backend     direct LLVM IR, typed inline assembly (Phase 2)             [future]
```

---

## Core Language Quick Reference

### Types

```candor
i8 i16 i32 i64         // signed integers
u8 u16 u32 u64         // unsigned integers
f32 f64                // IEEE 754
bool                   // true | false only. Never coerces to integer.
str                    // immutable string (const char*)
unit                   // real type, not void. return unit; is required.
never                  // type of non-returning expressions
option<T>              // some(v) | none  — replaces null
result<T, E>           // ok(v) | err(e)  — replaces exceptions
vec<T>                 // growable array
ring<T>                // fixed-capacity circular buffer
ref<T>                 // shared borrow
fn(T, U) -> R          // first-class function type
```

**Not yet implemented:** `i128`, `u128`, lambdas/closures, user-defined enums, `map<K,V>`, `set<T>`.

### Variables

```candor
let x: i64 = 42        // typed, immutable
let y = 42             // type inferred, immutable
let mut z: i64 = 0     // mutable
```

### Functions

```candor
fn add(a: u32, b: u32) -> u32 { return a + b }

fn greet(name: str) -> unit effects(io) {
    print("hello")
    return unit
}

fn pure_hash(x: i64) -> i64 pure {
    return x * 2654435761
}
```

With contracts:

```candor
fn divide(a: i64, b: i64) -> i64
    requires b != 0
    ensures result == a / b
{
    return a / b
}
```

### Structs

```candor
struct Point { x: f64, y: f64 }

let p = Point { x: 1.0, y: 2.0 }
let px = p.x

let mut q = Point { x: 0.0, y: 0.0 }
q.x = 3.0
```

### Control Flow

```candor
if x > 0 { ... } else if x < 0 { ... } else { ... }

loop {
    if condition { break }
}

for item in my_vec {
    print_int(item)
}
```

### Errors — must{} is always required

```candor
let v = divide(10, 2) must {
    ok(val) => val
    err(e)  => return unit
}
// Silence is a compile error. There are no exceptions.

// break is allowed in must arms inside loops:
loop {
    let x = try_read_int() must {
        some(v) => v
        none    => break
    }
    print_int(x)
}
```

### Pattern Matching

```candor
let label = match n {
    0 => "zero"
    1 => "one"
    _ => "other"
}

let v = match opt {
    some(x) => x * 2
    none    => 0
}

let r = match res {
    ok(x)  => x
    err(_) => -1
}

// Negative literals: use trailing comma to avoid parsing ambiguity
let sign = match n {
    -1 => "negative",
    0  => "zero",
    _  => "positive"
}
```

### Effects

```candor
fn f() -> unit pure { ... }             // pure — no side effects
fn g() -> unit effects(io) { ... }      // declares IO effects
fn h() -> unit effects(io, net) { ... } // multiple effects
fn j() -> unit cap(SomeCap) { ... }     // capability-gated
// no annotation — unchecked (gradual adoption)
```

Enforcement: a `pure` function cannot call a function with `effects(...)`. The compiler verifies this through the call graph.

### Contracts

```candor
fn safe_div(a: i64, b: i64) -> i64
    requires b != 0
    ensures result >= 0
{
    return a / b
}
```

`requires` and `ensures` generate `assert()` calls in debug builds. `result` in `ensures` refers to the return value.

### Modules (multi-file)

```candor
// math.cnd
module math
fn square(x: i64) -> i64 { return x * x }

// main.cnd
module app
use math::square
fn main() -> unit {
    print_int(square(5))
    return unit
}
```

Compiled with: `candorc main.cnd math.cnd`

### First-Class Functions

```candor
fn square(x: i64) -> i64 { return x * x }
fn apply(f: fn(i64) -> i64, x: i64) -> i64 { return f(x) }

let result = apply(square, 5)
let f: fn(i64) -> i64 = square
let y = f(10)
```

---

## Built-in Functions

### Output (all carry effects(io))

| Function | Signature |
|---|---|
| `print` | `fn(str) -> unit` |
| `print_int` | `fn(i64) -> unit` |
| `print_u32` | `fn(u32) -> unit` |
| `print_bool` | `fn(bool) -> unit` |
| `print_f64` | `fn(f64) -> unit` |

### Blocking Stdin (all carry effects(io))

| Function | Signature |
|---|---|
| `read_line` | `fn() -> str` |
| `read_int` | `fn() -> i64` |
| `read_f64` | `fn() -> f64` |

### EOF-Safe Stdin (all carry effects(io), return option<T>)

| Function | Signature |
|---|---|
| `try_read_line` | `fn() -> option<str>` |
| `try_read_int` | `fn() -> option<i64>` |
| `try_read_f64` | `fn() -> option<f64>` |

### Vec Operations

| Function | Signature |
|---|---|
| `vec_new` | `fn() -> vec<T>` (requires explicit type annotation) |
| `vec_push` | `fn(vec<T>, T) -> unit` |
| `vec_len` | `fn(vec<T>) -> u64` |

Vec indexing: `v[i]`

---

## Key Design Rules

1. **Silence is a compile error.** Discarding a `result<>` or `option<>` without `must{}` is a compile error.

2. **`bool` is not an integer.** Using a bool where a number is expected is a type error. There is no implicit coercion.

3. **`unit` is a real type.** Functions with no meaningful return value are declared `-> unit` and must execute `return unit`.

4. **Effects are transitive.** A `pure` function cannot call any effectful function. The compiler verifies this through the entire call graph.

5. **Contracts are runtime assertions in debug builds.** `requires` checks at function entry, `ensures` checks before each `return`.

6. **must{} arms must be exhaustive.** For `result<T,E>`: both `ok` and `err` arms required. For `option<T>`: both `some` and `none` arms required.

7. **Mutations require `mut`.** Variable reassignment and struct field assignment both require the binding to be declared `mut`.

---

## What Is NOT Done Yet

The following **features are specified but not yet implemented** in the compiler:

**Types:**
- `i128` / `u128`
- Lambdas and closures (anonymous functions)
- User-defined enums (only `option<T>` and `result<T,E>` variants exist)
- `map<K,V>` and `set<T>`
- String operations (concat, len, slice, comparison beyond `==`)

**Language:**
- Ownership transfers (`move`), `refmut<T>`
- `old()` in `ensures` clauses
- `forall` quantifier in contracts
- Tags (`@[pure]`, `@[secret]`, `@[idempotent]`, etc.)
- `#intent` blocks
- `#use` layer declarations

**Toolchain:**
- `.csi` semantic index file
- `candorc search`, `candorc audit`, `candorc test` subcommands
- Language server / LSP
- Syntax highlighter for any editor
- Standard library beyond documented built-ins
- Allocator layer
- LLVM backend (Phase 2)

---

## Status Table

| Component | Status |
|---|---|
| Lexer | Complete |
| Parser | Complete |
| Type checker | Complete |
| Effects enforcement | Complete |
| Contracts (requires/ensures) | Emit assert() |
| Multi-file / module enforcement | Complete |
| C emitter | Complete |
| First-class functions | Complete |
| Literal pattern matching | Complete |
| vec\<T\> (push, len, index, for) | Complete |
| stdin I/O (read_*, try_read_*) | Complete |
| i128 / u128 | Not yet |
| Lambdas / closures | Not yet |
| User-defined enums | Not yet |
| String operations (concat, len) | Not yet |
| map\<K,V\> / set\<T\> | Not yet |
| Allocator layer | Future |
| LLVM backend | Future |

---

## Repo Structure (Actual)

```
candor/
├── SPEC.md                    <- full language specification (aspirational)
├── README.md                  <- project introduction (accurate to v0.0.1)
├── LICENSE                    <- Apache 2.0
├── docs/
│   └── language-reference.md  <- complete reference for implemented language
├── compiler/                  <- candorc source (Go 1.24)
│   ├── main.go
│   ├── go.mod
│   ├── lexer/
│   ├── parser/
│   ├── typeck/
│   ├── emit_c/
│   └── tests/
```

---

*Candor Knowledge Item v0.2 — © 2026 Scott W. Corley*
*Paste this into your knowledge base for persistent context across sessions.*
