# CANDOR
## Complete Language Specification — Working Draft v0.1

> *Code should mean exactly what it says, and say everything it means.*

**© 2026 Scott W. Corley**  
**SPDX-License-Identifier: Apache-2.0**  
https://github.com/candor-lang/candor

---

## Table of Contents

1. [Core Language Specification](#part-1-core-language-specification)
2. [Layer 1: effects](#part-2-layer-1-effects)
3. [Layer 2: contracts](#part-3-layer-2-contracts)
4. [Layer 3: tags](#part-4-layer-3-tags)
5. [Layer 4: natural](#part-5-layer-4-natural)
6. [Layer 5: collections](#part-6-layer-5-collections)
7. [Layer 6: network](#part-7-layer-6-network)
8. [Layer 7: realtime](#part-8-layer-7-realtime)
9. [Layer 8: c_interop](#part-9-layer-8-c_interop)
10. [The Semantic Index](#part-10-the-semantic-index)
11. [The #intent Methodology](#part-11-the-intent-methodology)
12. [Remaining Layers Roadmap](#part-12-remaining-layers-roadmap)

---

# PART 1: Core Language Specification

## 1. Origin & Philosophy

### 1.1 The Problem We Are Solving

For decades, programming language efficiency was achieved through ambiguity. Skilled programmers carried implicit knowledge — understanding why a cast was safe, why a pointer was valid, why an error could be ignored. The language trusted the human to know.

This worked when humans wrote and read all code. It breaks when AI agents enter the loop. AI agents fail most often not on logic, but on implicit knowledge. They misread implicit ownership transfers, miss silent error discards, and misinterpret context-dependent constructs. They generate code that is syntactically correct but semantically wrong — because the language gave them no way to know better.

> **Core Insight:** Efficiency no longer requires implicit knowledge held by the human. We can now externalize that knowledge into the language itself — making the implicit explicit, permanently. Semantic density replaces ambiguity as the path to efficiency.

### 1.2 The Candor Principle

Candor is named for the quality of complete honesty and transparency. The language is built on one principle:

> *A Candor program is a complete, honest declaration of everything it does. Nothing is hidden. Nothing is assumed. Nothing is implicit.*

- Every side effect is declared
- Every error must be handled — not just catchable, but structurally required
- Every ownership transfer is visible in source
- Every contract between caller and callee is machine-readable
- Every piece of intent lives in the code itself, not in a comment or programmer's memory

### 1.3 Design North Star

> **Design for reasoning transparency, not parsing efficiency.** A language where the intent of every construct is unambiguous will be AI-friendly at every stage of AI development — whether the AI is a 2024-era token predictor, a 2026 AST reasoner, or a 2030 formal verifier.

---

## 2. Compilation Strategy

### 2.1 Two-Phase Approach

| Phase | Description |
|-------|-------------|
| Phase 1 (Now) | Transpile to C. Fast iteration on language semantics. Human-readable output. Leverage mature C toolchains. |
| Phase 2 (Later) | Direct LLVM IR emission. First-class debug info mapped to Candor source. Typed inline assembly. Full optimization pipeline control. |

### 2.2 The Semantic IR

Below both compilation targets sits Candor's Semantic IR — a structured representation richer than the syntax. This IR is the real artifact. The `.cnd` syntax is the human and current-AI entry point. Future tools consume the Semantic IR directly.

```
FunctionNode {
    name:      "connect"
    inputs:    [{ name: "s", type: "ref<Socket>" }]
    output:    "result<unit, NetError>"
    effects:   ["io.write", "io.read"]
    contracts: { requires: ["s.state == Idle"], ensures: ["s.state == Connected"] }
    tags:      [{ key: "retryable", max: 3 }]
    natural:   { intent: "Connect a socket to a remote address..." }
}
```

### 2.3 Implementation Language

**Recommended: Go for Phase 1, evaluate Zig for Phase 2.**

Go advantages: known language, fast iteration, single static binary output, goroutines for parallel compilation, excellent standard library. Zig advantages: philosophical alignment with Candor (no hidden allocations, comptime, native C interop, explicit allocators). The middle path — Go for the compiler core, Zig for the code generation backend — is worth considering for long-term architecture.

---

## 3. Modular Architecture — The Layer System

> **Design Rule:** Nothing lives in Core that could live in a layer above it. The test: can you write a meaningful, safe, runnable program without this construct? If yes, it is a layer. If no, it is Core.

### 3.1 The Complete Layer Stack

| Layer | Purpose |
|-------|---------|
| Core | Primitives, memory ownership, control flow, result<>, structs |
| effects | Declare what a function touches. Pure functions enforced. |
| contracts | requires/ensures/invariants. Reasons over state effects can reach. |
| tags | Semantic metadata: @[pure], @[secret], @[retryable]. Verified claims. |
| natural | Natural language annotations. Intent verified against contracts. |
| collections | vec<T>, map<K,V>, set<T>, ring<T,N> |
| allocators | Explicit allocator control with effect-declared strategies |
| channels | Intra-process message passing. No shared mutable state. |
| network | Typed endpoints, auto-parallelism, latency resilience |
| realtime | WCET contracts, task priorities, ISR constraints |
| crypto | Constant-time, zeroize-on-drop, verified entropy |
| semantic_index | Compile-time .csi artifact. Meaning-based codebase navigation. |
| c_interop | C ABI compatibility. Opaque-effect boundary. |
| llvm_backend | Direct LLVM IR. Typed inline assembly with register contracts. |

---

## 4. Candor Core

### 4.1 Primitive Types

```candor
i8  i16  i32  i64  i128    ## signed integers
u8  u16  u32  u64  u128    ## unsigned integers
f32  f64                   ## IEEE 754 floats
bool                       ## true | false only, never coerces to integer
unit                       ## real type, not void
never                      ## type of non-returning functions
```

### 4.2 Memory Model

```candor
let x: u64 = 42           ## owned value
let y = move(x)           ## ownership transfer; x is now dead
let r:  ref<u64>    = &y  ## read-only borrow
let m:  refmut<u64> = &y  ## mutable borrow, exclusive
let a:  option<u64> = some(42)  ## explicit nullable
let b:  option<u64> = none
```

No null pointers. No pointer arithmetic. No implicit copies. Region-based ownership without borrow checker complexity.

### 4.3 Control Flow

```candor
if x > 0 { ... } else { ... }

match value {
    some(v) => ...
    none    => ...
}

loop { if condition { break } }

## No exceptions. No goto. No hidden exits.
```

### 4.4 Result — Errors as Values

```candor
fn divide(a: u64, b: u64) -> result<u64, DivError> {
    if b == 0 { return err(DivError.ZeroDiv) }
    return ok(a / b)
}

let val = divide(10, 2) must {
    ok(v)  => v
    err(e) => return err(e)   ## must handle both cases, always
}
```

Silence is a compile error. `must{}` is required on every `result<>`. There are no exceptions.

### 4.5 The secret<T> Core Type

```candor
let api_key: secret<str> = load_env("API_KEY")

## Compiler enforces:
## Cannot be printed, logged, or serialized
## Cannot be passed to functions not tagged @[handles_secret]
## Requires explicit declassify() to use as raw value
```

### 4.6 Structs

```candor
struct Point {
    x: f64,
    y: f64,
}

## With invariants (requires contracts layer):
struct BoundedCounter
    invariant self.value <= self.max
    invariant self.max > 0
{
    value: u64,
    max:   u64,
}
```

---

## 5. Human Ergonomics

> **The Human-First Corollary:** Candor is designed so that code is honest with whoever reads it — human or machine. "AI-friendly" is a consequence of clarity, not a replacement for it.

### 5.1 Progressive Complexity

| Stage | What the Programmer Gains |
|-------|--------------------------|
| Day 1 | Core only. Types, functions, structs, result<>. Familiar to anyone who knows Go or Rust. |
| Week 1 | Add `#use effects`. Signatures become self-documenting. Optimizer gets smarter. |
| Month 1 | Add `#use contracts`. Bugs move from runtime to compile time. |
| Month 3 | Add `#use tags`. Intent encoded in source. Reviews get faster. |

### 5.2 What Candor Keeps and Fixes

| Convention | Candor's Position |
|-----------|------------------|
| C-style braces | Kept |
| fn, let, -> syntax | Kept |
| result<T,E> errors | Upgraded — silence is a compile error |
| Pointer arithmetic | Gone |
| Implicit type coercion | Gone |
| Null pointers | Gone — option<T> replaces null |
| Silent error discard | Gone — must{} always required |

### 5.3 The Promise

> Candor will never make you learn something complex to do something simple. Every layer you add gives you more power and more safety — it never takes away the ability to write straightforward code straightforwardly.

---

# PART 2: Layer 1: effects

*Depends on: Core*

## 1. Purpose

Effects is the first and most foundational layer above Core. Every subsequent layer depends on effects being defined first — reasoning about what a program does requires knowing what it can touch. Effects are compile-time metadata only. They generate zero runtime overhead and actively help the optimizer.

> **Zero-Cost Rule:** Every Candor layer adds expressiveness at compile time. It must not add overhead at runtime unless the programmer explicitly opts in.

## 2. The Effect Taxonomy

```candor
effect io   { read | write | read_write }
effect mem  { alloc | free | alloc_free }
effect sys  { call | signal }
effect time { read | sleep }
effect rand { read }
effect panic
## [] = provably pure. The strongest guarantee Candor can give.

## Reserved for crypto layer:
effect crypto { constant_time | zeroize | entropy }
```

## 3. Optimizer Permissions by Effect

| Declaration | Optimizer Behavior |
|-------------|-------------------|
| `effects []` | Dead code elimination, CSE, free reordering, loop hoisting, memoization |
| `effects [io.*]` | IO barrier — cannot reorder across other IO operations |
| `effects [mem.alloc]` | Heap barrier — cannot hoist above allocation-sensitive code |
| `effects [sys.call]` | Full memory and IO fence. No reordering in either direction. |
| `effects [time.read]` | Non-determinism — compiler may not cache or deduplicate |

## 4. Key Constructs

### 4.1 Basic Declaration

```candor
fn hash(data: ref<u8>, len: u64) -> u64
    effects []          ## provably pure
{ ... }

fn connect(s: refmut<Socket>, port: u16) -> result<unit, NetError>
    effects [io.read_write, mem.alloc]
{ ... }
```

### 4.2 Transitivity

A function cannot declare fewer effects than the union of effects of all functions it calls. The compiler enforces this transitively through the entire call graph.

### 4.3 Pure Blocks & Effect Caps

```candor
## Pure block: everything inside must be effects []
pure {
    let h = hash(&data, data.len)   ## ok
    ## log(h)                       ## COMPILE ERROR: io.write
}

## Cap: restricts maximum effects in a region
cap [io.read, mem.alloc] {
    let data = fetch(url) must { ... }
    ## write_log(data)              ## COMPILE ERROR: io.write not in cap
}
```

> **Why Caps Matter for AI Agents:** An AI agent generating code inside a `cap []` block literally cannot generate code that performs unauthorized side effects. The compiler enforces the sandbox structurally — not at runtime.

### 4.4 Effect Polymorphism

```candor
## 'fx' is a compile-time effect variable
fn map<T, U, fx>(items: ref<vec<T>>, f: fn(ref<T>) -> U effects fx) -> vec<U>
    effects fx
{ ... }

## Pure mapper: map becomes pure
let doubled = map(&numbers, fn(x) -> u64 effects [] { x * 2 })

## IO mapper: map inherits io.write
let logged  = map(&numbers, fn(x) -> u64 effects [io.write] { log(x); x })
```

---

# PART 3: Layer 2: contracts

*Depends on: Core, Layer 1: effects*

## 1. Purpose

Where effects answer "what can this function touch?", contracts answer "what must be true before it runs, and what will be true when it finishes?" Together they give a complete behavioral specification of any function without requiring the reader to trace through the implementation.

> **A contract is documentation that cannot lie, a test that cannot be skipped, and a proof that costs nothing when it holds.**

### 1.1 Compiler Modes

| Flag | Behavior |
|------|----------|
| `--contracts=static` | Only statically verified contracts. Unverifiable = compile error. Zero runtime cost. |
| `--contracts=debug` | Static + runtime assertions in debug builds only. Default mode. |
| `--contracts=always` | Runtime assertions in all builds. For safety-critical code. |

## 2. The Four Contract Types

### 2.1 requires — Preconditions

```candor
fn divide(a: u64, b: u64) -> u64
    effects  []
    requires b != 0
    requires a <= u64.MAX / b
{ return a / b }

divide(100, 0)   ## COMPILE ERROR: requires b != 0 violated
divide(100, 5)   ## OK: proven statically
divide(x, y)     ## debug assertion emitted for runtime value
```

Violating a precondition is always the **caller's fault**.

### 2.2 ensures — Postconditions

```candor
fn sort(items: refmut<vec<u64>>) -> unit
    effects  [mem.alloc]
    ensures  items.len == old(items.len)
    ensures  forall i in 1..items.len: items[i] >= items[i-1]
{ ... }

## old(expr) captures value at function entry. ensures only.
## result refers to the return value. ensures only.
```

Violating a postcondition is always the **function's fault**.

### 2.3 invariant — Type Invariants

```candor
struct BoundedCounter
    invariant self.value <= self.max
    invariant self.max > 0
{
    value: u64,
    max:   u64,
}
## Enforced after every mutation. Direct violation = compile error.
```

### 2.4 assert — Inline Assertions

```candor
fn process(data: ref<vec<u8>>) -> result<u64, ProcessError>
    effects []
    requires data.len > 0
{
    ## ...
    assert header.version == 1 or header.version == 2
        message: "unsupported protocol version"
    ## ...
}
```

### 2.5 Error-Case Contracts

```candor
fn transfer(src: refmut<Account>, dst: refmut<Account>, amount: u64)
    -> result<unit, TransferError>
    effects  [io.write]
    requires src.balance >= amount
    ensures  ok(unit)  => src.balance == old(src.balance) - amount
                      and dst.balance == old(dst.balance) + amount
    ensures  err(_)    => src.balance == old(src.balance)  ## rollback guaranteed
                      and dst.balance == old(dst.balance)
{ ... }
```

Contracts specify what is true in **both success AND error cases**.

## 3. Contract Expression Language

Contract clauses are written in a restricted, pure subset of Candor Core — effects [] only. Available expressions: arithmetic, comparison, boolean, ranges, `forall`/`exists` quantifiers, `old(expr)`, `result`, field access, indexing, pure function calls.

**Forbidden in contracts:** any effectful function call, memory allocation, IO, loops or recursion, mutable bindings.

## 4. Static Verification

The compiler's constraint solver attempts to prove contracts statically. Three outcomes:

- **PROVEN:** contract holds statically. Zero runtime cost. Silent.
- **UNPROVEN:** compiler cannot determine. Runtime assertion in debug builds. Note emitted.
- **VIOLATED:** contract provably false. Hard compile error in all modes.

## 5. Contracts Enable Optimization

Every contract proven statically is a runtime check that does not exist. `requires b != 0` eliminates the division-by-zero check. A proven loop index invariant eliminates bounds checks. Contracts make code **faster** by giving the optimizer proven facts.

---

# PART 4: Layer 3: tags

*Depends on: Core, Layer 1: effects, Layer 2: contracts*

## 1. Purpose

Tags are structured, machine-readable metadata that bridges the gap between behavioral specification and semantic intent. They are verified claims grounded in effects and contracts — not comments. They are compile-time only, stripped entirely from the binary.

| Kind | Syntax | Difference |
|------|--------|------------|
| Comment | `## safe to retry` | No verification. Can go stale. Compiler ignores it. |
| Tag | `@[retryable(max: 3)]` | Compiler verifies idempotency. Agents generate retry logic. Never stale. |
| Comment | `## never log this` | No enforcement. Depends on discipline. |
| Tag | `@[secret]` | Compiler enforces: no logging, no serialization without declassify. |

## 2. Tag Categories

### 2.1 Behavioral Tags

```candor
@[pure]          ## verified: effects [] required
@[idempotent]    ## verified: calling twice = same ensures as once
@[deterministic] ## verified: no time.read or rand.read
@[total]         ## verified: all inputs handled, no panic paths
@[monotonic]     ## verified: ensures result >= old(result)
```

### 2.2 Operational Tags

```candor
@[retryable(max: 3, backoff: exponential)]  ## requires @[idempotent]
@[timeout(ms: 5000)]                        ## policy: documented expectation
@[rate_limit(calls: 100, per: second)]      ## policy: agent guidance
@[call_once]                                ## verified: compile error on >1 call
@[must_use]                                 ## verified: return value cannot be discarded
```

### 2.3 Data Sensitivity Tags — Taint Tracking

```candor
@[secret]              ## no logging, no serialization, requires declassify()
@[key_material]        ## superset of @[secret], move-only, must zeroize
@[pii]                 ## no logging without audit, no storage without encryption
@[source: user_input]  ## taint tracked to sanitization boundary
@[sanitized]           ## taint cleared, verified via @[sanitizes: user_input]
```

> **Security by Default:** Taint tracking makes SQL injection, command injection, and path traversal vulnerabilities compile errors, not runtime surprises. The compiler is the security boundary.

### 2.4 Structural Tags

```candor
@[constructor]               ## return type must be associated struct, invariants satisfied
@[destructor]                ## takes ownership, must release resources
@[deprecated(use: 'newFn')] ## warning at all call sites
@[platform(os: linux)]      ## compile error if called on wrong platform
@[entry_point]              ## exactly one per binary, compiler enforces uniqueness
```

### 2.5 Custom Tags

```candor
tags {
    @[no_alloc]
        policy: verified
        requires: effects does_not_contain mem.alloc
        description: "Function must not allocate heap memory."

    @[billing_critical]
        policy: advisory
        description: "Changes affect billing calculations."
}

@[no_alloc] @[billing_critical]
fn fast_path(req: ref<Request>) -> u32  effects [io.read]  { ... }
```

## 3. Tag Verification Model

| Tag | Class | Enforcement |
|-----|-------|-------------|
| `@[pure]` | Verified | Checks effects [] is declared. Hard error if not. |
| `@[idempotent]` | Verified | Checks ensures clauses satisfy idempotency definition. |
| `@[deterministic]` | Verified | Checks effects excludes time.read and rand.read. |
| `@[call_once]` | Verified | Tracks call sites statically, errors on multiple. |
| `@[must_use]` | Verified | Errors if return value discarded at any call site. |
| `@[secret]` | Verified | Taint tracking enforced through entire call graph. |
| `@[pii]` | Verified | Taint tracking with storage and logging constraints. |
| `@[deprecated(...)]` | Policy | Warning at call sites. Not a hard error. |
| `@[timeout(...)]` | Policy | Documents expectation. Not runtime enforced. |

---

# PART 5: Layer 4: natural

*Depends on: Core, Layer 1: effects, Layer 2: contracts, Layer 3: tags*

## 1. Purpose

The natural layer bridges the gap between human intent and formal specification. Natural language annotations in Candor are not comments — they are structured, compiler-aware declarations verified against the contracts and effects they accompany. A conflict between intent and contracts is a compile error.

> **The Core Distinction:** A natural language annotation in Candor is always grounded in formal specifications. If there is a conflict between intent and contracts, the formal spec wins — surfaced as a compiler diagnostic, not silently ignored.

## 2. The Five Annotation Types

### 2.1 @[intent] — Purpose Declaration

```candor
@[intent: "
    Search a sorted list for a target value.
    Return the position if found, or nothing if not present.
    The list must be sorted ascending before calling.
"]
fn binary_search(items: ref<vec<u64>>, target: u64) -> option<u64>
    effects  []
    requires forall i in 1..items.len: items[i] >= items[i-1]
    ensures  some(idx) => items[idx] == target
    ensures  none => forall i in 0..items.len: items[i] != target
{ ... }
## Compiler verifies intent claims map to contracts. Conflict = hard error.
```

### 2.2 @[explain] — Decision Documentation

```candor
@[explain: "
    Uses timsort not quicksort: input data contains long sorted runs.
    Timsort is O(n) on sorted input vs O(n log n) for quicksort.
    Benchmark: sort_benchmarks::ingestion_pattern shows 4x improvement.
"]
## Compiler verifies 'sort_benchmarks::ingestion_pattern' exists.
## Stale references are compiler warnings, not silent drift.
```

### 2.3 @[example] — Compiled and Executed as Tests

```candor
@[example: "
    divide(10, 2)  =>  ok(5)
    divide(7,  3)  =>  ok(2)
    divide(5,  0)  =>  err(DivError.ZeroDiv)
"]
fn divide(a: u64, b: u64) -> result<u64, DivError>  { ... }

## candorc test compiles and runs these. Failures = test failures.
## Documentation that cannot go stale: it is also the test suite.
```

### 2.4 @[warn_if] — Context-Sensitive Usage Warnings

```candor
@[warn_if: "called before comparing for equality with a value that may
            contain uppercase characters — normalize both sides."]
fn to_lowercase(s: str) -> str  effects []  { ... }
## Compiler emits advisory warning at misuse call sites.
```

### 2.5 @[vocabulary] — Domain Term Definitions

```candor
@[vocabulary: "
    account:     A financial entity with a balance and owner.
    transaction: An atomic transfer of funds between two accounts.
    frozen:      An account that cannot send or receive funds.
"]
module payments { ... }
```

## 3. The Intent-First Workflow

```candor
## STEP 1: Write intent (human or agent)
@[intent: "
    Send a message to a user by their ID.
    Fail gracefully if the user does not exist or channel is unavailable.
    Never send duplicate messages for the same message ID.
"]
fn send_message(user_id: UserId, msg_id: MessageId, body: str)
    -> result<unit, SendError>

## STEP 2: candorc infer-contracts
## Compiler reads intent, generates contract scaffolding for review.

## STEP 3: candorc infer-impl
## Agent reads intent + contracts, generates implementation.

## STEP 4: candorc build
## Compiler verifies: implementation satisfies contracts.
## Compiler verifies: contracts consistent with intent.
```

## 4. Toolchain Commands

| Command | Behavior |
|---------|----------|
| `candorc vibe` | Full pipeline: @[intent] → contracts → implementation scaffold |
| `candorc infer-contracts` | Reads @[intent], generates contract scaffolding only |
| `candorc infer-impl` | Reads @[intent] + contracts, generates implementation |
| `candorc explain` | Reads existing function, generates @[intent] and @[explain] |
| `candorc test` | Compiles and runs all @[example] blocks as tests |
| `candorc audit` | Checks all @[intent] for consistency with contracts |
| `candorc docs` | Extracts all natural annotations into always-current documentation |

> **The Candor Verification Loop:** Human writes intent → natural layer generates contracts → agent generates implementation → contracts layer verifies implementation → compiler confirms → binary ships. Every step is auditable. No step is implicit.

---

# PART 6: Layer 5: collections

*Depends on: Core, effects, contracts, tags, natural*

## 1. Purpose

The collections layer provides Candor's standard generic data structures. Unlike collections in most languages, Candor's collections are formally specified types whose safety properties are expressed as contracts, whose behaviors are expressed as effects, and whose semantic roles are expressible as tags.

Every collection operation that can be proven safe at compile time produces no bounds check in the binary. Collections in Candor are simultaneously safer than C arrays and — in proven contexts — equally fast.

## 2. The Core Collection Types

### 2.1 vec<T> — Growable Sequence

```candor
let nums: vec<u64> = vec.new()           ## effects [mem.alloc]
let nums: vec<u64> = vec.of(1, 2, 3)     ## effects [mem.alloc]
let nums: vec<u64> = vec.with_cap(16)    ## pre-allocated

nums.push(42)                ## effects [mem.alloc]: may grow
let x = nums[2]              ## effects []: bounds checked by contract
let x = nums.get(2) must {   ## effects []: returns option<T>
    some(v) => v
    none    => return err(OutOfBounds)
}
let n = nums.len             ## effects []: O(1)
```

### 2.2 map<K, V> — Key-Value Store

```candor
let m: map<str, u64> = map.new()
m.insert("alice", 42)              ## effects [mem.alloc]
let v = m.get("alice") must {       ## effects []: option<ref<V>>
    some(r) => r
    none    => return err(Missing)
}
```

### 2.3 set<T> — Unique Value Collection

```candor
let s: set<str> = set.of("a", "b", "c")
s.insert("d")                  ## effects [mem.alloc]
let has = s.contains("b")      ## effects []: bool, O(1)
```

## 3. Semantic Collection Tags

| Tag | Meaning and Enforcement |
|-----|------------------------|
| `@[non_empty]` | Verified: len > 0 always. `first()` returns T, not option<T>. |
| `@[sorted]` | Verified: ascending order. `binary_search()` valid without sort check. |
| `@[unique]` | Verified: no duplicate values. |
| `@[bounded(n)]` | Verified: len <= n always. `push()` returns result<> if at capacity. |
| `@[fixed_size(n)]` | Verified: len == n always. Array-like semantics. No push/pop. |

## 4. Bounds Safety Model

| Syntax | Behavior |
|--------|----------|
| `v[i]` | Requires `i < v.len` proven by contracts. Zero runtime check. Compile error if not proven. |
| `v.get(i)` | Returns `option<ref<T>>`. Always safe. Caller handles absent case. |
| `v.get_unchecked(i)` | No bounds check. Requires `#use unsafe_ops`. Explicit opt-in. |

## 5. ring<T, N> — The Zero-Allocation Collection

```candor
## N is a compile-time constant: no runtime allocation ever
let buf: ring<u8, 256> = ring.new()   ## effects []: stack allocated

buf.push(byte) must {
    ok(unit)  => unit
    err(Full) => handle_full()
}

## Usable in contexts where mem.alloc is prohibited:
cap [] {
    let mut buf: ring<u8, 64> = ring.new()
    buf.push(0x42)   ## OK: effects []
}
```

## 6. Effect-Aware Iterators

```candor
## Iterator pipeline: effects compose automatically
let result = nums
    .iter()                          ## effects []
    .filter(fn(x) -> bool effects [] { x > 10 })
    .map(fn(x) -> u64 effects []    { x * 2 })
    .collect::<vec<u64>>()           ## effects [mem.alloc]
## Overall pipeline: effects [mem.alloc]
```

---

# PART 7: Layer 6: network

*Depends on: Core, effects, contracts, tags, natural, collections, channels*

## 1. Purpose & Design Philosophy

Modern programs are nodes in distributed systems. Every language today handles this with libraries and conventions. Network calls look like function calls in source but behave fundamentally differently at runtime: latency, transient failures, serialization boundaries, trust boundaries.

The network layer gives Candor a native vocabulary for what remote calls **mean**. It does not implement a TCP stack. It provides typed endpoint declarations, network-specific effects, latency-resilient execution models, and compiler-driven automatic parallelization of safe concurrent network calls.

> **The Central Design Goal:** Network connectivity issues must never freeze program execution. When the environment is degraded, programs degrade gracefully. When healthy, the compiler automatically exploits parallelism declared structurally. Resilience is the floor. Parallelism is the ceiling.

## 2. The Network Effect Taxonomy

```candor
effect net {
    call        ## single request, single response. RPC-style.
    stream      ## bidirectional ongoing connection. Long-lived.
    subscribe   ## one-way event stream from remote. Push model.
    broadcast   ## fire and forget. No response expected.
    probe       ## lightweight availability check. No payload.
}
```

## 3. Automatic Parallelization

```candor
#intent "Load all context needed to render a user's dashboard
         as fast as the network allows."
fn load_dashboard_context(user_id: UserId)
    -> result<DashboardContext, NetError>
    effects [net.call, mem.alloc]
{
    ## Three @[idempotent] calls with no data dependency:
    ## Compiler detects: safe to parallelize.
    ## Emitted binary: three concurrent requests.
    ## Wall time: max(A, B, C) not A + B + C

    let user    = UserService.get_profile(user_id)
    let account = AccountService.get_balance(user_id)
    let notifs  = NotifService.get_count(user_id)

    let u = user    must { ok(u) => u   err(e) => return err(e) }
    let a = account must { ok(a) => a   err(e) => return err(e) }
    let n = notifs  must { ok(n) => n   err(e) => return err(e) }

    return ok(DashboardContext { user: u, balance: a, notif_count: n })
}
## No async/await. No thread management. No explicit join.
```

## 4. Endpoint Declarations

```candor
endpoint PaymentService {
    base_url:    "https://payments.internal"
    timeout:     ms(3000)
    auth:        @[secret] BearerToken
    version:     "v2"

    circuit_breaker {
        open_after:      failures(5) within(seconds(30))
        half_open_after: seconds(60)
    }

    bulkhead {
        max_concurrent: 20
        queue_depth:    10
    }

    @[idempotent]
    @[retryable(max: 3, backoff: exponential)]
    fn charge(amount: u64, card: @[pii] CardToken)
        -> result<ChargeId, PaymentError>
        effects   [net.call]
        requires  amount > 0
}
```

> **Resilience Is Structural:** Circuit breakers, bulkheads, and fallbacks are declared in the endpoint definition — not scattered across call sites. Every caller automatically gets the resilience behavior.

## 5. Latency Resilience Model

### 5.1 Latency Tiers

```candor
latency_profile AppProfile {
    fast:     ms(0)   ..  ms(50)
    normal:   ms(50)  ..  ms(500)
    slow:     ms(500) ..  ms(2000)
    timeout:  ms(2000) ..
}
```

### 5.2 Adaptive Calls

```candor
let recs = RecsService.fetch(product_id)
    adapt {
        fast   => full(recs)
        normal => full(recs)
        slow   => partial(recs, 3)  ## lighter payload
        timeout => cached_fallback(product_id)  ## effects []
    }
```

### 5.3 Latency Budgets

```candor
fn assemble_order_context(order_id: OrderId)
    -> result<OrderContext, NetError>
    effects   [net.call, mem.alloc]
    budget    ms(800)    ## total wall time allowed
{
    ## Compiler distributes budget across parallel calls.
    ## Budget-exhausted calls return their fallback, not block.
    let order    = OrderService.get(order_id)
    let customer = CustomerService.get(order.customer_id)
    let shipping = ShippingService.get_status(order_id)
    ## ...
}
```

## 6. Trust Boundaries

```candor
endpoint ExternalPartnerAPI {
    base_url:    "https://partner.external.com"
    trust_level: external   ## all responses tagged @[source: external]

    fn get_inventory(sku: str) -> result<InventoryData, ApiError>
        effects [net.call]
}

## Taint tracking: data from external endpoint must be validated
let data = ExternalPartnerAPI.get_inventory(sku) must { ... }
db.insert(data.quantity)  ## COMPILE ERROR: @[source: external] untrusted
let safe = validate_quantity(data.quantity) must { ... }  ## @[sanitized]
db.insert(safe)  ## OK
```

---

# PART 8: Layer 7: realtime

*Depends on: Core, effects, contracts, tags, natural, collections, allocators, channels, network*

## 1. Purpose & Design Philosophy

The realtime layer extends Candor's formal verification model into the time domain. Worst-case execution time is a contract. Priority inversion is a contract violation. ISR constraints are a specialized tag profile. The compiler enforces all of them.

> **What This Layer Is Not:** The realtime layer does not implement a scheduler, a tick clock, or a task switcher. It does not replace FreeRTOS, Zephyr, or any RTOS kernel. It provides the language-level vocabulary for declaring real-time constraints and verifying them statically.

### 1.1 Enforcement Modes

| Mode | Behavior |
|------|----------|
| `--realtime=annotate` | Emit timing metadata into .csi only. No enforcement. |
| `--realtime=verify` | Prove WCET statically where possible. Warn on unproven. Default. |
| `--realtime=strict` | All WCET on @[realtime_safe] functions must be statically proven. Unproven = compile error. |

## 2. The WCET Contract System

```candor
#intent "Read a sensor value within the control loop deadline."
@[realtime_safe]
@[priority(level: 10)]
@[deadline(us: 500)]
@[wcet(us: 120)]
fn read_pressure_sensor(channel: u8) -> result<u16, SensorError>
    effects   [sys.call]
    requires  channel < 8
    ensures   ok(v) => v <= 4095
{ ... }

## Compiler verifies:
## @[wcet(us: 120)] <= @[deadline(us: 500)]  ✓
## All called functions also declare @[wcet]  ✓
## Sum of call-graph WCET <= declared @[wcet] ✓
## effects does not include mem.alloc         ✓
```

### 2.1 WCET Units

| Unit | Use Case |
|------|----------|
| `@[wcet(us: N)]` | Microseconds. Standard for most real-time tasks. |
| `@[wcet(ms: N)]` | Milliseconds. For slower background tasks. |
| `@[wcet(cycles: N)]` | CPU cycles. Required for ISRs. |
| `@[wcet(ticks: N)]` | RTOS ticks. Portable across tick-rate configurations. |

### 2.2 WCET Transitivity

WCET propagates through the call graph. A function's declared WCET must be >= the sum of all called functions' WCET plus its own instruction time. The compiler verifies this transitively.

## 3. Task Priorities and Scheduling

```candor
realtime_config {
    priorities {
        critical:   255
        control:    100
        sensing:     50
        processing:  20
        comms:       10
        background:   1
    }
    tick_rate_hz: 1000
    scheduler:   preemptive
}

@[priority(level: control)]
@[deadline(us: 1000)]
@[wcet(us: 400)]
@[realtime_safe]
fn motor_control_loop(state: refmut<MotorState>) -> unit
    effects [sys.call]
{ ... }
```

### 3.1 Priority Inheritance Mutexes

```candor
## realtime_mutex<T> uses priority inheritance automatically.
## Prevents priority inversion structurally.
## Compiler performs static deadlock detection via lock acquisition graph.
let actuator_lock: realtime_mutex<ActuatorState> = realtime_mutex.new(initial)

@[realtime_safe] @[wcet(us: 50)]
fn update_actuator(new_pos: u16) -> result<unit, RTError>
    effects [sys.call]
{
    let guard = actuator_lock.acquire(timeout: us(30)) must {
        ok(g)                 => g
        err(RTError.Timeout)  => return err(RTError.Timeout)
        err(RTError.Deadlock) => return err(RTError.Deadlock)
    }
    guard.value.position = new_pos
    return ok(unit)
}
```

## 4. Interrupt Service Routines

```candor
#intent "Handle incoming UART byte at hardware interrupt level.
         Push to ring buffer for task-level processing.
         Must never block or allocate."
@[isr(vector: 0x28)]
@[wcet(cycles: 48)]
@[no_alloc]
@[no_float]
@[no_blocking]
fn uart1_rx_handler() -> unit
    effects [sys.signal]
{
    let byte = UART1.read_data_register()
    rx_buffer.push(byte) must {
        ok(unit)  => unit
        err(Full) => overflow_count.fetch_add(1)
    }
}
## rx_buffer is ring<u8, 256>: fixed size, effects []
```

ISR-to-task communication uses `ring<T,N>` — the only collection usable from ISR context. Lock-free and allocation-free. ISRs write, tasks read. Ownership transfers through the buffer.

## 5. The @[realtime_safe] Profile

The `@[realtime_safe]` tag is a composite profile implying:

| Constraint | Effect |
|-----------|--------|
| `@[no_alloc]` | No heap allocation anywhere in call graph |
| `@[no_blocking]` | No blocking channel operations or blocking I/O |
| `@[no_panic]` | No panic paths. All errors must be result<T,E> |
| `@[total]` | All inputs handled. No unreachable branches. |
| `@[wcet] required` | Every function in call graph must declare @[wcet] |
| `effects [net.*]` | Prohibited |
| `effects [mem.*]` | Prohibited |

> **The Transitive Guarantee:** If a function is tagged @[realtime_safe] and compiles without error in --realtime=strict mode, every function in its call graph satisfies all constraints. No exceptions.

---

# PART 9: Layer 8: c_interop

*Depends on: Core, effects, contracts, tags — usable without upper layers*

## 1. Purpose & Design Philosophy

No language exists in isolation. The c_interop layer is the formal bridge between Candor's verified world and C's unverified one.

The central design principle is **honesty**. When Candor calls a C function, the safety properties Candor provides do not silently disappear — they are explicitly suspended at a declared boundary. The programmer declares what they believe the C function does. The compiler trusts that declaration. The gap between declaration and reality is marked as a trust boundary, not hidden.

> **The Core Distinction:** In C++, calling C is invisible. In Rust, `unsafe` marks the boundary but provides no documentation mechanism. In Candor, the c_interop boundary is a formal declaration with declared effects, declared contracts, and an explicit acknowledgment of what cannot be verified.

## 2. Calling C from Candor

```candor
#intent "Wrap libc's memcpy for performance-critical buffer copies."
extern fn memcpy(
    dst: refmut<u8>,
    src: ref<u8>,
    n:   u64
) -> refmut<u8>
    effects   [mem.alloc_free]
    @[opaque_effects]              ## required on ALL extern declarations
    requires  n > 0
    requires  dst != src
    link:     "c"

## @[opaque_effects] means:
## 'I am declaring what I believe this C function does.
##  The compiler cannot verify this. I take responsibility.'
```

### 2.1 The @[opaque_effects] Tag

`@[opaque_effects]` is **mandatory** on every extern declaration. It:
- Marks the exact location of every trust boundary — searchable via semantic index
- Signals to agents this function's behavior cannot be statically verified
- Prevents calls inside `cap []` pure blocks
- Triggers a complete trust boundary listing in `--strict` mode

```candor
## Security audit — complete trust boundary surface:
candorc audit --trust-boundaries
## Returns every @[opaque_effects] site in the codebase.
```

### 2.2 Importing C Headers

```candor
#import_c_header "openssl/sha.h"
    as_module: crypto_sha
    trust_level: external   ## all imports are @[opaque_effects]

## Compiler generates stubs automatically.
## Programmer refines with known effect information.
#refine crypto_sha::SHA256_Init
    effects   [mem.alloc]
    requires  ctx is zeroed
    @[opaque_effects]
```

### 2.3 C Type Mapping

| C Type | Candor Mapping |
|--------|---------------|
| `int, long, size_t` | `i32/i64/u64` — explicit width required |
| `char*` | `ref<u8>` or `str` |
| `void*` | `ref<u8>` with `@[opaque_type]` |
| `NULL` | `option<T>` — always explicit |
| `struct (by value)` | `#[c_layout] struct` |
| `enum` | `#[c_enum]` |
| `variadic (...)` | `@[variadic] extern` — marked, cannot be type-checked |

## 3. Exporting Candor to C

```candor
@[export_c]
@[c_name: "candor_hash_bytes"]
fn hash_bytes(data: ref<u8>, len: u64, out: refmut<u8[32]>) -> i32
    effects   []
    requires  len > 0
{
    let hash = compute_hash(data, len)
    out.copy_from(hash)
    return 0   ## C convention: 0 = success
}
## Compiler generates: int candor_hash_bytes(const uint8_t*, uint64_t, uint8_t[32]);
```

## 4. C-Compatible Memory Layout

```candor
#[c_layout]
struct SensorReading {
    timestamp:  u64,    ## 8 bytes, offset 0
    channel:    u8,     ## 1 byte,  offset 8
    _pad:       u8[3],  ## 3 bytes padding
    value:      u32,    ## 4 bytes, offset 12
}
## Total: 16 bytes. Layout identical to equivalent C struct.
## Candor invariants still apply on the Candor side.
```

## 5. Phase 1 Compiler Bootstrap

The `candorc` compiler itself uses c_interop to call libc — the compiler demonstrates its own principles from day one.

```candor
## Copyright (c) 2026 Scott W. Corley
## SPDX-License-Identifier: Apache-2.0

#intent "Provide a minimal safe wrapper around the libc functions
         needed by the candorc compiler itself."
module sys::libc

extern fn fopen(path: ref<u8>, mode: ref<u8>) -> option<refmut<u8>>
    effects   [io.read_write, mem.alloc]
    @[opaque_effects]
    link: "c"

extern fn exit(code: i32) -> never
    effects   [sys.call]
    @[opaque_effects]
    link: "c"

## These are the only trust boundaries in the compiler itself.
## Everything above this level is pure Candor.
```

> **The Go vs. Zig Argument:** In Zig, every `@import("c")` call is the same philosophy as Candor's c_interop — explicit, declared, honest. Writing `candorc` in Zig means the compiler's own architecture demonstrates the principles Candor is built on.

---

# PART 10: The Semantic Index

*Compile-Time Vector Intelligence — enriched by all layers*

## 1. The Problem

Even with a full layer stack, an AI agent navigating a Candor codebase still faces a navigation problem — it must either read everything linearly or rely on external tooling. Both are bolted on. Neither is native.

> **The Key Insight:** The Semantic IR that all layers populate is already a rich, structured representation of every declaration in the codebase. The question is not whether to index it — it is whether that index should be a native compile-time artifact or an external afterthought. Candor makes it native.

## 2. The .csi File

The Semantic Index is a compile-time artifact — a `.csi` file produced automatically alongside the binary during every `candorc build`. It contains vector embeddings of every declaration, generated from the full Semantic IR node of each construct. Zero bytes added to the compiled binary.

```
## Every declaration produces a structured embedding:
embedding_input = {
    name:     "send_request",
    effects:  ["io.read", "io.write"],
    requires: ["payload.len > 0", "socket.is_connected"],
    ensures:  ["ok(r) => r.status >= 100"],
    tags:     ["idempotent", "retryable:3", "must_use"],
    intent:   "Send a payload over a socket and return the response...",
    goal:     "#intent: 'Single entry point for all outbound requests'"
}
```

Two functions with identical effects and overlapping contracts are geometrically close in embedding space even with different names. **Formal structure gives the embedding space meaning beyond word similarity.**

## 3. Querying the Index

```bash
# Natural language + formal filter:
candorc search "handle authentication" --effects [io.read] --tag @[secret]

# Formal-only:
candorc search --effects [io.write] --tag @[idempotent] --tag @[retryable]

# Semantic similarity:
candorc search --similar-to payments::transfer_funds --top 5

# Goal-level search (via #intent):
candorc search --goal "single entry point for session creation"

# Audit queries:
candorc audit --intent            ## redundancy and alignment check
candorc audit --trust-boundaries  ## complete @[opaque_effects] list
candorc audit --realtime          ## WCET coverage and priority inversion risks
```

## 4. How Richer Layers Improve the Index

| Active Layers | Index Capability |
|--------------|-----------------|
| Core only | Embeddings based on names, types, struct shapes |
| + effects | Behavioral profile. "Find pure functions" is a precise query. |
| + contracts | Pre/postconditions. "Find functions guaranteeing sorted output" works. |
| + tags | "Find retryable authenticated endpoints" is a single query. |
| + natural | Intent descriptions. Natural language matches formal structure simultaneously. |
| + #intent | Goal-level search. Find functions by architectural purpose, not just behavior. |
| All layers | Richest query surface of any programming language toolchain. |

## 5. The Embedding Model Is Pluggable

```bash
candorc build --embed-model local:nomic-embed-text
candorc build --embed-model api:openai/text-embedding-3-small
candorc build --embed-model none    # produces .csi without embeddings
```

The `.csi` format is stable. The model is not baked into the language.

## 6. Local AI Model Efficiency

The `.csi` file lets a local model consume the Semantic IR directly rather than parsing surface syntax. It operates on a pre-parsed, pre-resolved, richly annotated representation — the difference between giving a model raw HTML and giving it a clean structured JSON object.

Candor's formal layers reduce the inference cost of understanding any single function. The semantic index reduces the search cost of finding the right function. Together they address both the comprehension problem and the navigation problem that make large codebases expensive for local models.

---

# PART 11: The #intent Methodology

## The Three Annotation Levels

| Form | Declares | Stripped at Compile | Verified |
|------|----------|-------------------|---------|
| `## comment` | Explanation of implementation | Yes | No |
| `@[intent: '...']` | What the function does | Yes | Yes — vs. contracts |
| `#intent '...'` | Why the function needs to exist | Yes | Via audit tool |

## The Distinction That Matters

A **comment** explains what code does.  
`@[intent]` declares what the function **does** — verified against contracts.  
`#intent` declares why the function **needs to exist** — the goal it serves.

Code that correctly accomplishes the wrong goal is still wrong. `#intent` makes the goal explicit so it can be evaluated, not just the implementation.

## File Header Convention

Every Candor source file:

```candor
## Copyright (c) 2026 Scott W. Corley
## SPDX-License-Identifier: Apache-2.0
## https://github.com/candor-lang/candor

#intent "Provide authentication primitives — session creation,
         validation, and revocation. This module is the only
         path by which SessionTokens may be created."
module auth

#intent "Create a verified session for a user who has provided
         valid credentials. No other function should produce a
         SessionToken — this is the single creation point."
@[intent: "Validate credentials, return a time-limited session token."]
fn authenticate(username: str, password: @[secret] str)
    -> result<SessionToken, AuthError>
    effects   [io.read_write]
    requires  username.len > 0
    ensures   ok(token) => token.expires > now()
{ ... }
```

## The Intent Audit

```bash
candorc audit --intent
```

Scans every `#intent` block and asks:
1. Is this goal already achievable by composing existing functions? *(redundancy check via semantic index)*
2. Do the contracts actually deliver this goal? *(alignment check)*
3. Has the architecture changed such that this goal is now met differently? *(relevance check — human judgment)*

Functions that have been duplicated by agent generation, superseded by refactoring, or made redundant by new layers are surfaced. This keeps the codebase lean as it grows.

---

# PART 12: Remaining Layers Roadmap

| Layer | Description |
|-------|-------------|
| `allocators` | Explicit allocator control. Arena allocators, stack allocators, pool allocators for fixed-size objects. Declared per-collection and per-function. No hidden allocation source. |
| `channels` | Intra-process message passing between threads and async tasks. Ownership transfers through channels — no shared mutable state. Built on `ring<T,N>` internally. Data races are structurally impossible. |
| `crypto` | Formal cryptographic primitives. `crypto.constant_time` — execution path does not vary on secret values. `crypto.zeroize` — sensitive memory provably cleared on drop. `crypto.entropy` — verified entropy sourcing. Makes the difference between "we use a crypto library" and "the compiler proves our crypto code is correct." |
| `semantic_index` | Full specification of the `.csi` artifact — embedding schema, query grammar for `candorc search`, pluggable model interface, `#intent` goal-level indexing, audit tool formal definitions. |
| `llvm_backend` | Direct LLVM IR emission. Replaces C transpiler. Debug info mapped precisely to Candor source lines. Typed inline assembly with declared register contracts. Verifiably constant-time machine code for the crypto layer. Full optimization pipeline control. |

---

## Implementation Path

**Immediate actions:**
1. Open `candor-lang/candor` on GitHub — push this spec as `SPEC.md`
2. File a provisional patent on the novel technical combinations (semantic tag verification grounded in effects and contracts; natural layer consistency verification model; #intent goal-level audit system)
3. Start Core lexer/parser in Go (or Zig)
4. First milestone: compile `fn add(a: u32, b: u32) -> u32 { return a + b }` to C

**First compiler milestone (`candorc v0.0.1`):**
```
candorc/
├── lexer/       tokenizes .cnd files
├── parser/      produces AST from Core grammar  
├── typeck/      Core types only
├── emit_c/      produces valid C from AST
└── tests/       @[example] blocks from this spec
```

**The Durable Design Bet:**
Candor does not optimize for tokenization efficiency. It optimizes for reasoning transparency — semantic density, unambiguous constructs, and machine-readable intent. These properties are durable regardless of how AI processing evolves, from token prediction to AST reasoning to formal verification.

---

*Candor — Complete Specification — Working Draft v0.1*  
*© 2026 Scott W. Corley. All rights reserved.*  
*SPDX-License-Identifier: Apache-2.0*
