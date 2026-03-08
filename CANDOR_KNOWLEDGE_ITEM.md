# Candor Language — Standing Context for Claude Code
## Antigravity Knowledge Item v0.1

---

## What This Project Is

**Candor** is a new systems programming language being designed and built by Scott W. Corley (© 2026, Apache 2.0). The GitHub repo is `scottcorleyg1/candor`.

The compiler is called `candorc`. It does not exist yet. We are building it.

The full language specification is in `SPEC.md` at the repo root. **Read it before generating any Candor-related code.**

---

## The Prime Directive

> A Candor program is a complete, honest declaration of everything it does. Nothing is hidden. Nothing is assumed. Nothing is implicit.

Every design decision, every compiler feature, every generated example must be evaluated against this principle. If a construct allows something to be implicit, hidden, or assumed — it does not belong in Candor.

---

## The Layer Stack (Complete)

Layers are strictly ordered. Each depends on all layers above it. All layers are opt-in via `#use`.

```
Core             primitives, ownership, control flow, result<T,E>, structs
effects          declare what a function touches — zero runtime cost
contracts        requires / ensures / invariants / assert
tags             @[pure] @[secret] @[retryable] — verified claims, not comments
natural          @[intent] @[explain] @[example] — intent-first development
collections      vec<T> map<K,V> set<T> ring<T,N> — formally specified
allocators       explicit, declared allocation strategies
channels         ownership-safe intra-process message passing
network          typed endpoints, auto-parallelism, latency resilience
realtime         WCET contracts, ISR constraints, priority inheritance
crypto           constant-time, zeroize-on-drop, verified entropy
semantic_index   compile-time .csi artifact — meaning-based search
c_interop        honest C boundary — @[opaque_effects] required always
llvm_backend     direct LLVM IR, typed inline assembly (Phase 2)
```

---

## Compiler Architecture

**Phase 1 (current):** Transpile `.cnd` → C → binary via system C compiler.  
**Phase 2 (future):** Direct LLVM IR emission.

**Implementation language:** Go (for Phase 1 velocity). Zig is under consideration for Phase 2 or the code generation backend.

### First Milestone: `candorc v0.0.1`

```
compiler/
├── lexer/       tokenize .cnd source files
├── parser/      produce AST from Core grammar
├── typeck/      Core type checking only (no layers yet)
├── emit_c/      emit valid C from typed AST
└── tests/       test suite — driven by @[example] blocks in spec
```

**Acceptance criteria:** `candorc` can compile and run this program:

```candor
fn add(a: u32, b: u32) -> u32 { return a + b }

fn main() -> unit {
    let x = add(1, 2)
    return unit
}
```

---

## Core Language Quick Reference

### Types
```candor
i8 i16 i32 i64 i128   ## signed integers
u8 u16 u32 u64 u128   ## unsigned integers
f32 f64               ## IEEE 754
bool                  ## true | false only. Never coerces to integer.
unit                  ## real type, not void
never                 ## type of non-returning expressions
option<T>             ## some(v) | none  — replaces null
result<T, E>          ## ok(v) | err(e)  — replaces exceptions
secret<T>             ## taint-tracked sensitive value
```

### Ownership
```candor
let x: u64 = 42           ## owned
let y = move(x)           ## ownership transfer. x is dead.
let r: ref<u64> = &y      ## read-only borrow
let m: refmut<u64> = &y   ## mutable borrow, exclusive
```

### Errors — must{} is always required
```candor
fn divide(a: u64, b: u64) -> result<u64, DivError> {
    if b == 0 { return err(DivError.ZeroDiv) }
    return ok(a / b)
}

let v = divide(10, 2) must {
    ok(v)  => v
    err(e) => return err(e)
}
## Silence is a compile error. There are no exceptions.
```

### Effects
```candor
fn hash(data: ref<u8>, len: u64) -> u64
    effects []          ## provably pure
{ ... }

fn save(r: ref<Record>) -> result<unit, DbError>
    effects [io.write, mem.alloc]
{ ... }

## Effect taxonomy:
## io   { read | write | read_write }
## mem  { alloc | free | alloc_free }
## sys  { call | signal }
## time { read | sleep }
## rand { read }
## net  { call | stream | subscribe | broadcast | probe }
## crypto { constant_time | zeroize | entropy }
## panic
```

### Contracts
```candor
fn transfer(src: refmut<Account>, dst: refmut<Account>, amount: u64)
    -> result<unit, TransferError>
    effects  [io.write]
    requires src.balance >= amount
    requires amount > 0
    ensures  ok(unit) => src.balance == old(src.balance) - amount
                     and dst.balance == old(dst.balance) + amount
    ensures  err(_)   => src.balance == old(src.balance)
                     and dst.balance == old(dst.balance)
```

### Tags (selected)
```candor
@[pure]                              ## effects [] required
@[idempotent]                        ## calling twice = calling once
@[retryable(max: 3, backoff: exponential)]  ## requires @[idempotent]
@[must_use]                          ## return value cannot be discarded
@[secret]                            ## no log/serialize without declassify()
@[pii]                               ## taint-tracked PII
@[source: user_input]                ## taint source
@[sanitized]                         ## taint cleared
@[realtime_safe]                     ## no alloc, no block, WCET required
@[wcet(us: N)]                       ## worst-case execution time contract
@[isr(vector: N)]                    ## interrupt service routine
@[opaque_effects]                    ## required on ALL extern C declarations
@[non_empty] @[sorted] @[unique] @[bounded(N)]  ## collection tags
```

### The #intent Methodology
```candor
## #intent declares WHY a function needs to exist.
## @[intent] declares WHAT a function does (verified vs contracts).
## ## comments explain HOW.

#intent "Provide the single creation point for session tokens.
         No other path should produce a SessionToken."
@[intent: "Validate credentials and return a time-limited token."]
fn authenticate(username: str, password: @[secret] str)
    -> result<SessionToken, AuthError>
    effects [io.read_write]
    requires username.len > 0
    ensures  ok(token) => token.expires > now()
{ ... }
```

### File Header Convention
```candor
## Copyright (c) 2026 Scott W. Corley
## SPDX-License-Identifier: Apache-2.0
## https://github.com/scottcorleyg1/candor
```

---

## Key Design Rules

1. **Zero-cost layers.** Every layer adds compile-time expressiveness. No layer adds runtime overhead unless explicitly opted in.

2. **Silence is an error.** Discarding a `result<>` without `must{}` is a compile error. Discarding a `@[must_use]` return value is a compile error.

3. **@[opaque_effects] is mandatory.** Every `extern` C declaration must carry `@[opaque_effects]`. No exceptions. This marks every trust boundary in the codebase.

4. **Effects are transitive.** A function cannot declare fewer effects than the union of all functions it calls. The compiler verifies this through the entire call graph.

5. **Contracts are proofs, not documentation.** Statically proven contracts vanish from the binary. Unproven contracts become debug assertions. Provably false contracts are hard compile errors.

6. **Intent blocks earn functions their existence.** `candorc audit --intent` asks whether each function still needs to exist. Code that correctly accomplishes the wrong goal is still wrong.

7. **The .csi file is always current.** The semantic index is regenerated on every build from the Semantic IR. It cannot drift from the code.

8. **Wire format neutrality.** The network layer declares the shape of remote calls. It never dictates serialization format. JSON, Protobuf, custom binary — library choice, not language choice.

9. **No RTOS replacement.** The realtime layer provides WCET contracts and ISR constraints. It targets on top of an RTOS or bare metal. It does not implement a scheduler.

10. **The boundary is honest.** C interop does not hide where Candor's guarantees end. `@[opaque_effects]` marks every trust gap explicitly.

---

## Toolchain Commands Reference

```bash
candorc build                    # compile .cnd → binary + .csi index
candorc build --contracts=strict # all contracts must be statically proven
candorc build --realtime=strict  # all WCET must be statically proven
candorc test                     # compile and run all @[example] blocks
candorc audit                    # run all audit checks
candorc audit --intent           # check #intent redundancy and alignment
candorc audit --trust-boundaries # list all @[opaque_effects] sites
candorc audit --realtime         # WCET coverage + priority inversion risks
candorc search "query"           # semantic index search
candorc search --similar-to fn   # find semantically similar functions
candorc infer-contracts          # generate contract scaffolding from @[intent]
candorc infer-impl               # generate implementation from intent + contracts
candorc explain                  # annotate existing code with @[intent] + @[explain]
candorc docs                     # extract all natural annotations → documentation
candorc vibe                     # full pipeline: intent → contracts → implementation
```

---

## What Is NOT Done Yet

The following layers are **specified but not yet implemented in the compiler:**

- `allocators` — spec in progress
- `channels` — spec in progress  
- `crypto` — spec pending
- `semantic_index` — full formal spec pending
- `llvm_backend` — Phase 2, future

The following does not exist yet:
- The `candorc` compiler (any part of it)
- A `.cnd` file syntax highlighter
- A language server / LSP
- Any standard library

---

## Repo Structure (Target)

```
candor/
├── SPEC.md                    ← full language specification
├── README.md                  ← project introduction
├── LICENSE                    ← Apache 2.0
├── CONTRIBUTING.md
├── compiler/                  ← candorc source (Go)
│   ├── lexer/
│   ├── parser/
│   ├── typeck/
│   ├── emit_c/
│   └── tests/
├── spec/                      ← per-layer specification docs
│   ├── core.md
│   ├── layer1_effects.md
│   ├── layer2_contracts.md
│   └── ...
├── examples/                  ← .cnd example programs
└── tests/                     ← integration test suite
```

---

*Candor Knowledge Item v0.1 — © 2026 Scott W. Corley*  
*Paste this into Antigravity → Knowledge Items for persistent context across sessions.*
