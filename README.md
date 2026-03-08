# Candor

> *Code should mean exactly what it says, and say everything it means.*

**A systems programming language designed for semantic density, unambiguous logic, and agentic AI collaboration.**

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Status: Pre-Alpha](https://img.shields.io/badge/Status-Pre--Alpha-orange.svg)]()
[![Spec: v0.1](https://img.shields.io/badge/Spec-v0.1_Working_Draft-lightgrey.svg)](SPEC.md)

---

## What is Candor?

Candor is a systems programming language built on one principle:

> A Candor program is a complete, honest declaration of everything it does.  
> Nothing is hidden. Nothing is assumed. Nothing is implicit.

Every side effect is declared. Every error must be handled. Every ownership transfer is visible in source. Every contract between caller and callee is machine-readable. Every piece of intent lives in the code itself — not in a comment, a wiki, or a programmer's memory.

This makes Candor unusually well-suited to the way software is being built today — where AI agents read, write, and reason about code alongside humans. A language that is honest with humans is honest with agents for exactly the same reasons.

---

## A Taste of Candor

```candor
## Copyright (c) 2026 Scott W. Corley
## SPDX-License-Identifier: Apache-2.0

#intent "Transfer funds between two accounts atomically.
         Either both balances change or neither does."

@[intent: "Debit src and credit dst by amount. Rollback fully on any failure."]
fn transfer(src: refmut<Account>, dst: refmut<Account>, amount: u64)
    -> result<unit, TransferError>
    effects   [io.write]
    requires  src.balance >= amount
    requires  amount > 0
    ensures   ok(unit)  => src.balance == old(src.balance) - amount
                       and dst.balance == old(dst.balance) + amount
    ensures   err(_)    => src.balance == old(src.balance)
                       and dst.balance == old(dst.balance)
{
    src.balance -= amount
    dst.balance += amount
    return ok(unit)
}
```

What the compiler knows from this declaration alone — without reading the body:

- This function writes to IO (database). It cannot be called in a pure context.
- The caller is responsible for ensuring `src.balance >= amount`. The compiler verifies this at call sites.
- If the function returns `ok`, both balances changed exactly as declared.
- If the function returns `err`, neither balance changed. Rollback is guaranteed by contract, not by convention.
- `old(src.balance)` captures the value at entry. If the ensures clause is violated, it is a compile error.

---

## The Layer System

Candor is built in composable layers. Core is simple enough for a first day. Each layer adds power and safety without adding complexity to what came before.

```
Core          primitives, ownership, control flow, result<T,E>
  └─ effects      declare what a function can touch
      └─ contracts    requires / ensures / invariants
          └─ tags         @[pure] @[secret] @[retryable] — verified claims
              └─ natural      intent-first development, verified examples
                  └─ collections  vec<T> map<K,V> set<T> ring<T,N>
                      └─ allocators   explicit, declared allocation strategies
                          └─ channels      ownership-safe intra-process messaging
                              └─ network       typed endpoints, auto-parallelism
                                  └─ realtime     WCET contracts, ISR constraints
                                      └─ crypto       constant-time, zeroize-on-drop
                                          └─ semantic_index  compile-time vector search
                                              └─ c_interop    honest C boundary
                                                  └─ llvm_backend  direct IR emission
```

Every layer is opt-in. A program using only Core is valid, safe, and compilable. Adding a layer is a one-line `#use` declaration.

---

## Key Ideas

### Effects — Zero-Cost Behavioral Declarations

```candor
fn hash(data: ref<u8>, len: u64) -> u64
    effects []          ## provably pure: memoizable, reorderable, parallelizable
{ ... }

fn save(record: ref<Record>) -> result<unit, DbError>
    effects [io.write, mem.alloc]   ## compiler knows exactly what this touches
{ ... }
```

A function declared `effects []` is provably pure. The compiler can memoize it, reorder it, and eliminate dead calls. No annotation needed at the call site — the declaration is the contract.

### Contracts — Bugs at Compile Time, Not Runtime

```candor
fn binary_search(items: ref<vec<u64>>, target: u64) -> option<u64>
    effects  []
    requires forall i in 1..items.len: items[i] >= items[i-1]
    ensures  some(idx) => items[idx] == target
    ensures  none => forall i in 0..items.len: items[i] != target
```

Contracts proven statically vanish from the binary — they cost nothing at runtime. Contracts that cannot be proven statically become debug assertions. Provably false contracts are hard compile errors.

### Tags — Semantic Metadata That Cannot Lie

```candor
@[idempotent]
@[retryable(max: 3, backoff: exponential)]
@[secret]       ## compiler enforces: no logging, no serialization without declassify()
@[pii]          ## taint-tracked through entire call graph
@[realtime_safe] ## no allocation, no blocking, WCET verified
```

Tags are verified claims grounded in effects and contracts. `@[pure]` requires `effects []`. `@[idempotent]` is verified against `ensures` clauses. `@[secret]` taint-tracks data through the entire call graph. They cannot drift from reality — the compiler enforces them.

### Natural Layer — Intent-First Development

```candor
#intent "Provide a single entry point for all user data needed
         at login time so callers do not need to know which
         downstream services hold which pieces of user state."

@[intent: "Return profile, permissions, and preferences in one call."]
fn get_user_context(user_id: UserId) -> result<UserContext, NetError>
    effects [net.call, mem.alloc]
{ ... }
```

`#intent` declares *why a function needs to exist*. `candorc audit --intent` checks whether that goal is still necessary, still unmet by other functions, and still achievable given the current architecture. It is the mechanism by which Candor codebases stay lean as they grow.

### Network Layer — Structural Parallelism

```candor
## Three @[idempotent] calls with no data dependency:
## Compiler emits concurrent requests automatically.
## No async/await. No thread management. No explicit join.

let user    = UserService.get_profile(user_id)    ## net.call
let account = AccountService.get_balance(user_id) ## net.call
let notifs  = NotifService.get_count(user_id)     ## net.call
## Wall time: max(A, B, C) — not A + B + C
```

### Realtime Layer — WCET as a Contract

```candor
@[realtime_safe]
@[priority(level: control)]
@[deadline(us: 1000)]
@[wcet(us: 400)]        ## verified transitively through entire call graph
fn motor_control_loop(state: refmut<MotorState>) -> unit
    effects [sys.call]
```

`@[wcet]` is a formal contract verified through the entire call graph. If any called function lacks a WCET declaration, it is a compile error in `--realtime=strict` mode. Priority inversion prevention is verified statically via lock acquisition graph analysis.

### The Semantic Index — Meaning-Based Navigation

Every `candorc build` produces a `.csi` file alongside the binary — a vector-embedded index of every declaration in the codebase, derived from the full Semantic IR (effects, contracts, tags, intent, goals). Zero bytes added to the binary.

```bash
# Find functions by meaning, not just name:
candorc search "authenticate user and establish session" --tag @[secret]
candorc search --effects [io.write] --tag @[idempotent] --tag @[retryable]
candorc search --similar-to payments::transfer_funds --top 5
candorc search --goal "single entry point for session creation"
```

Agents working in a Candor codebase query the `.csi` index as their primary navigation mechanism — replacing linear file reads with meaning-based lookup. The formal layer stack makes the index richer than any text embedding alone.

---

## Why Now?

The tools of software development are changing faster than the languages underneath them. AI agents can now write, read, and reason about code at scale — but they are doing so in languages designed for humans with implicit knowledge. The result is code that is syntactically correct but semantically wrong, because the language gave the agent no way to know better.

Candor is designed for the era where agents are collaborators, not just autocomplete. The same properties that make Candor honest with humans — explicit effects, verified contracts, declared intent — make it navigable and verifiable for agents. This is not a bolt-on feature. It is the design.

---

## Status

Candor is in the **specification phase**. The language specification is complete through Layer 8 (`c_interop`). The compiler (`candorc`) is not yet written.

| Component | Status |
|-----------|--------|
| Language Specification | ✅ Working Draft v0.1 |
| Core grammar (EBNF) | 🔲 Planned |
| `candorc` lexer | 🔲 Planned |
| `candorc` parser | 🔲 Planned |
| C transpiler (Phase 1) | 🔲 Planned |
| Type checker | 🔲 Planned |
| Effects verifier | 🔲 Planned |
| LLVM backend (Phase 2) | 🔲 Future |

---

## Getting Started

The compiler does not exist yet. The best way to get started is to read the specification and contribute ideas, feedback, or design input.

- **Full Specification:** [SPEC.md](SPEC.md)
- **Layer-by-layer docs:** [`/spec`](spec/)
- **Discussion:** GitHub Issues — use the `design` label for language design questions

---

## Contributing

Candor is in its earliest stage. The most valuable contributions right now are:

- **Design feedback** — read the spec and open issues challenging assumptions
- **Prior art** — know of an existing language that solved a similar problem? Open an issue
- **Use cases** — describe a real program you'd want to write in Candor
- **Formal methods expertise** — the contracts and effects layers will benefit from people who know this space

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines. All contributors must follow the [Code of Conduct](CODE_OF_CONDUCT.md).

---

## License

Copyright © 2026 Scott W. Corley  
Licensed under the [Apache License, Version 2.0](LICENSE)

The Candor name, language specification, and compiler source are the property of Scott W. Corley. Contributions are accepted under the Apache 2.0 license.

---

*Candor — Working Draft v0.1*
