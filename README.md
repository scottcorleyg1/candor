# Candor

> *Code should mean exactly what it says, and say everything it means.*

<!-- USER PERSONAL NOTE START -->
> [!NOTE]
> **A Note from the Author**: This is my first foray into actually making a public repo and my experience with git repositories has generally been from the perspective of a user, not a maintainer or a developer. So please go easy on me as I learn and grow. I, like a lot of people have been doing a lot with AI agents and I have been trying to think of areas where I can contribute to the open source community in a way that might improve the efficiency of AI agents in programming. In a personal research project (aka rabbit hole of what if's) I started thinking about human language vs AI vs programming language and the differences between them. Project Candor has emerged from the idea of wanting to limit the ambiguity of programming languages to make them more amenable to AI agents. I am not a language designer by trade, but I have been programming for a long time and I have a lot of ideas about what I would like to see in a programming language. I am open to feedback and suggestions.
>
> I may be off track with this project. I hope it inspires other ideas.
>
> Let the fun journey begin! (and hopefully not burn anything down in the process.)
>
> -Scott W. Corley
<!-- USER PERSONAL NOTE END -->

---
> 🤖 **FOR AI AGENTS (Claude, Gemini, etc.):** 
> Before writing any code or making architectural changes, **you MUST read [docs/AI_GUIDE.md](docs/AI_GUIDE.md)**. It contains critical instructions on how the self-hosting bootstrapping process works, pass-ordering requirements, and a list of known GCC bugs to prevent debugging rabbit holes.
---

**A systems programming language designed for semantic density, unambiguous logic, and agentic AI collaboration — built so that neither humans nor AI agents can hide what code does.**

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Status: Pre-Alpha](https://img.shields.io/badge/Status-Pre--Alpha-orange.svg)]()
[![Compiler: v0.2.0](https://img.shields.io/badge/Compiler-v0.2.0-green.svg)](https://github.com/candor-core/candor/releases/tag/v0.2.0)
[![Tests: passing](https://img.shields.io/badge/Tests-passing-brightgreen.svg)]()

---

## What is Candor?

Candor is a systems programming language built on one principle:

> A Candor program is a complete, honest declaration of everything it does.
> Nothing is hidden. Nothing is assumed. Nothing is implicit.

Every side effect is declared. Every error must be handled. Every contract between caller and callee is machine-readable. Every piece of intent lives in the code itself — not in a comment, a wiki, or a programmer's memory.

This makes Candor unusually well-suited to the way software is being built today — where AI agents read, write, and reason about code alongside humans. A language that is honest with humans is honest with agents for exactly the same reasons.

By minimizing ambiguity, we also minimize **Token Overhead**. Candor allows AI agents to operate more **fiscally and energy efficiently**, reducing operational costs for remote APIs and lowering CPU/GPU utilization for local models.

### The Human Intention Programming (HIP) Platform

Candor is not just a language; it is designed to be a **Cognitive Platform**. The ultimate goal of Candor is to bridge the gap between *Human Intent* and *Machine Execution*. Using our native "Intent Remark" structure currently in specification, Candor forces AI agents to adhere strictly to human-authored architectural boundaries (via implicit capabilities and `effects`), while the compiler mathematically proves the execution is safe. 
Read the full manifesto: [The HIP Vision](docs/HIP_VISION.md) and see how Candor stacks up: [Candor vs The Industry](docs/CANDOR_VS_INDUSTRY.md).

---

## A Taste of Candor

```candor
fn divide(a: i64, b: i64) -> result<i64, str>
    requires b != 0
{
    return ok(a / b)
}

fn main() -> unit effects(io) {
    let quotient = divide(10, 2) must {
        ok(val) => val
        err(e)  => return unit
    }
    print(_cnd_int_to_str(quotient))
    return unit
}
```

What the compiler knows from these declarations alone:

- `divide` has a `requires b != 0` contract — the compiler emits an assertion in debug builds.
- `main` carries `effects(io)` — it cannot be called from a `pure` function.
- The `must{}` block is required — silence on a `result<T,E>` is a compile error.
- `ok(val)` and `err(e)` are exhaustive pattern arms; the compiler rejects incomplete `must` blocks.

What a human reviewer can verify in under ten seconds:

- Does `divide` touch the filesystem? No — no `effects` declaration.
- Can `divide` fail silently? No — return type is `result<i64, str>`, caller must handle both arms.
- What are the preconditions? `b != 0` — in the signature, not a comment.

This is the same information the AI agent had when it wrote the function. Nothing is implicit. Nothing is hidden.

---

## The Layer System

Candor is built in composable layers. Core is simple enough for a first day. Each layer adds power and safety without adding complexity to what came before.

```
Core          primitives, control flow, result<T,E>, option<T>, structs     [IMPLEMENTED]
  effects       declare what a function can touch                            [IMPLEMENTED]
  contracts     requires / ensures / assert                                  [IMPLEMENTED]
  collections   vec<T> map<K,V> (push, len, insert, get)                    [IMPLEMENTED]
  ...
  tags          @[pure] @[secret] @[retryable] — verified claims            [future]
  natural       intent-first development, verified examples                  [future]
  allocators    explicit, declared allocation strategies                     [future]
  channels      ownership-safe intra-process messaging                       [future]
  network       typed endpoints, auto-parallelism                            [future]
  realtime      WCET contracts, ISR constraints                              [future]
  crypto        constant-time, zeroize-on-drop                               [future]
  semantic_index compile-time vector search                                  [future]
  c_interop     honest C boundary (#c_header, extern fn)                    [IMPLEMENTED]
  llvm_backend  direct IR emission (--backend=llvm)                         [IMPLEMENTED]
```

Every layer is opt-in. A program using only Core is valid, safe, and compilable.

---

## Key Ideas

### Effects — Behavioral Declarations on Functions

```candor
fn pure_hash(x: i64) -> i64 pure {
    return x * 2654435761
}

fn log_message(msg: str) -> unit effects(io) {
    print(msg)
    return unit
}
```

A function declared `pure` cannot call any function that carries `effects(...)`. The compiler enforces this through the entire call graph. A function with no annotation is unchecked — supporting gradual adoption.

### Errors — must{} Is Always Required

```candor
let v = divide(10, 2) must {
    ok(val) => val
    err(e)  => return unit
}
```

Discarding a `result<T,E>` or `option<T>` without `must{}` is a compile error. There are no exceptions. `break` is allowed in `must` arms when inside a loop.

### Contracts — Preconditions and Postconditions

```candor
fn safe_div(a: i64, b: i64) -> i64
    requires b != 0
    ensures result == a / b
{
    return a / b
}
```

Contracts generate `assert()` calls in debug builds. `requires` guards the function entry. `ensures` references the return value as `result`.

### Pattern Matching — Exhaustive, Typed

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
```

`match{}` works on integers, bools, strings, `option<T>`, and `result<T,E>`. Wildcard `_` matches anything. All arms must produce the same type.

---

## Getting Started

### Option A — Download a pre-built binary

Pre-built `candorc` binaries for Windows (x64) and Linux (x64) are on the
[Releases page](https://github.com/candor-core/candor/releases/latest).
Download, make executable, and add to your PATH.

You still need a C compiler (GCC or Clang) to compile the emitted C.

### Option B — Build from source

**Prerequisites**

- Go 1.24 or later
- GCC or a C compiler reachable as `gcc` or `cc` (override with the `CC` environment variable)

```bash
git clone https://github.com/candor-core/candor
cd candor/compiler
go build -o candorc .
```

### Write a Program

```candor
// hello.cnd
fn main() -> unit {
    print("hello, world")
    return unit
}
```

### Compile and Run

```bash
# Single file
./candorc hello.cnd
./hello          # on Unix
hello.exe        # on Windows

# Multi-file program
./candorc main.cnd math.cnd

# Inspect the emitted C
cat hello.c

# Emit LLVM IR instead (requires clang on PATH to link)
./candorc build --backend=llvm
cat hello.ll
```

The compiler writes a `.c` (or `.ll`) file alongside the source for inspection, then invokes the system C compiler (or clang) to produce a binary.

### LLVM IR Output

Given this Candor source:

```candor
fn add(a: i64, b: i64) -> i64 {
    return a + b
}

fn main() -> unit effects(io) {
    let sum = add(3, 4)
    print(int_to_str(sum))
    return unit
}
```

`candorc build --backend=llvm` emits:

```llvm
; LLVM IR generated by Candor compiler
target triple = "x86_64-unknown-linux-gnu"

define i64 @add(i64 %a.in, i64 %b.in) {
entry:
  %a.addr = alloca i64
  store i64 %a.in, ptr %a.addr
  %b.addr = alloca i64
  store i64 %b.in, ptr %b.addr
  %t1 = load i64, ptr %a.addr
  %t2 = load i64, ptr %b.addr
  %t3 = add i64 %t1, %t2
  ret i64 %t3
}

define void @_cnd_main() {
entry:
  %sum.addr = alloca i64
  %t1 = call i64 @add(i64 3, i64 4)
  store i64 %t1, ptr %sum.addr
  %t2 = load i64, ptr %sum.addr
  %t3 = call ptr @int_to_str(i64 %t2)
  call void @print(ptr %t3)
  ret void
}
```

Every function body is explicit, flat, and auditable — no hidden inlining, no implicit calls.

---

## Example Programs

### Sum integers from stdin until EOF

```candor
fn main() -> unit {
    let mut nums: vec<i64> = vec_new()
    loop {
        let x = try_read_int() must {
            some(v) => v
            none    => break
        }
        vec_push(nums, x)
    }
    let mut sum: i64 = 0
    for n in nums {
        sum = sum + n
    }
    print_int(sum)
    return unit
}
```

### FizzBuzz

```candor
fn fizzbuzz(n: i64) -> str {
    let div3 = n % 3 == 0
    let div5 = n % 5 == 0
    return match div3 {
        true => match div5 {
            true  => "FizzBuzz"
            false => "Fizz"
        }
        false => match div5 {
            true  => "Buzz"
            false => "?"
        }
    }
}

fn main() -> unit {
    let mut i: i64 = 1
    loop {
        if i > 20 { break }
        print(fizzbuzz(i))
        i = i + 1
    }
    return unit
}
```

### Multi-file with modules

```candor
// math.cnd
module math

fn square(x: i64) -> i64 { return x * x }
fn cube(x: i64) -> i64 { return x * x * x }
```

```candor
// main.cnd
module app
use math::square
use math::cube

fn main() -> unit {
    print_int(square(4))   // 16
    print_int(cube(3))     // 27
    return unit
}
```

```bash
candorc main.cnd math.cnd
```

---

## Status

There are two compilers in this repository. Read this section carefully — they are at different stages.

### Go Compiler (`candorc`) — Reference Implementation

The Go compiler is the full-featured reference implementation. It accepts `.cnd` source and targets C, LLVM IR, and WebAssembly. All language features below are complete and tested.

| Component | Status |
|---|---|
| Lexer / Parser / Type checker | Complete |
| Effects enforcement | Complete |
| Contracts (requires/ensures → assert) | Complete |
| Multi-file / module enforcement | Complete |
| C emitter | Complete |
| LLVM IR emitter | Complete |
| WebAssembly target | Complete |
| First-class functions & closures | Complete |
| Generics | Complete |
| Traits (trait, impl, bounds) | Complete |
| Literal pattern matching | Complete |
| vec\<T\>, map\<K,V\>, box\<T\> | Complete |
| arc\<T\> shared reference-counted ownership | Complete |
| task\<T\> + spawn structured concurrency | Complete |
| stdin / File I/O | Complete |
| String operations | Complete |
| User-defined enums (sum types with data) | Complete |
| Ownership annotations (ref, refmut, move, deref) | Complete |
| i128 / u128 / f16 / bf16 | Complete |
| Comptime evaluation (pure / effects []) | Complete |
| secret\<T\> information-flow enforcement | Complete |
| MCP tool annotations (#mcp_tool) | Complete |
| LSP server (diagnostics, hover, completion) | Complete |
| set\<T\> | Not yet |
| Allocator layer | Future |

### Candor Self-Hosted Compiler — Bootstrap

The self-hosted compiler is written in Candor. It is the proof that the language is real. The pipeline is: `lexer.exe` (Candor binary) compiles `.cnd` source → emits C → GCC → runnable binary.

| Component | Status |
|---|---|
| Lexer (`lexer.cnd`) | Complete |
| Parser (`parser.cnd`) | Complete |
| Type checker (`typeck.cnd`) | Complete |
| C emitter (`emit_c.cnd`) | Complete |
| Bootstrap idempotency (`diff stage2.c stage4.c` = 0) | **Verified** |
| 20/20 test cases passing via `lexer.exe` | **Verified** |
| LLVM IR / Wasm emitter in Candor | Future |
| Full generics in self-hosted path | Future |

---

## Trust by Design

People are split on AI-generated code. Some embrace it. Many fear it — not because AI writes bad syntax, but because **they can't see what the code is doing**. Hidden side effects. Silently swallowed errors. Implicit behavior that only makes sense if you already know the codebase.

Candor is designed to make that fear answerable. Not with reassurance — with structure.

**Every side effect is declared.**
```candor
fn send_report(data: str) -> unit effects(io, network) {
    write_file("log.txt", data)
    http_post("https://api.example.com/report", data)
    return unit
}
```
`effects(io, network)` is not a comment. It is enforced by the compiler across the entire call graph. An AI agent cannot write a function that silently touches the network without declaring it. A human reviewer sees it immediately.

**Every error must be handled explicitly.**
```candor
let result = parse_config("settings.cnd") must {
    ok(cfg) => cfg
    err(e)  => {
        print(str_concat("config error: ", e))
        return unit
    }
}
```
Discarding a `result<T,E>` without `must{}` is a compile error. There is no way for AI-generated code to silently swallow a failure. The compiler requires the decision to be in the code.

**Every precondition is machine-readable.**
```candor
fn transfer(amount: i64, balance: i64) -> result<i64, str>
    requires amount > 0
    requires amount <= balance
{
    return ok(balance - amount)
}
```
`requires` is not documentation. In debug builds it is an assertion. In future builds it will be a formal proof obligation. An AI agent writing a `transfer` function that forgets the balance check will have written an incomplete contract — the compiler knows.

**The ecosystem carries the same principle.**

The CandorCore module ecosystem extends this trust model beyond the language itself:

- **`ccMod-username`** — community modules, identity traceable to a real author
- **`ccPar-Name`** — formal partner relationship, Core-verified
- **`cc-module`** — official Core team, no external assertion needed
- **Paid audits** — any `ccMod-` author can pay for a Core security audit; a passing cert means a human reviewed it against a published checklist
- **Runtime hard blocks** — modules Core has flagged as malicious will not execute without explicit developer opt-in, regardless of what any AI agent installed

The name tells you who wrote it. The cert tells you whether a human audited it. The runtime enforces the rest.

This is not a bolt-on safety layer. It is the design. A language that cannot hide behavior from humans cannot hide behavior from the AI agents writing in it either. That is the guarantee Candor is built to provide.

---

## Why This Matters Right Now

In April 2026, Anthropic demonstrated that a frontier AI model — Claude Mythos — can find and chain software vulnerabilities that survived decades of human review and millions of automated tests. A 27-year-old remote crash in OpenBSD. A 16-year-old bug in FFmpeg that automated tools had exercised five million times without catching. Working exploits developed 181 times where the previous best model managed 2.

This is not a future threat. It is the current baseline.

A language where every side effect is declared and compiler-enforced is not just more readable — it is more auditable by AI security tools. When a function declares `effects(network)`, a Mythos-class auditor knows exactly where to look. When every `result<T,E>` must be explicitly handled, there is no silent failure path to exploit. When preconditions live in `requires` clauses rather than comments, they can be mechanically verified.

**Candor's explicitness is a structural defense. The same property that makes AI-generated Candor code trustworthy to humans makes it auditable by AI security tooling.** A language designed for the era of AI-assisted development turns out to be a language designed for the era of AI-assisted security. That is not a coincidence — it is the same principle applied consistently.

The cert system and runtime flag infrastructure described above are designed with this threat model in mind. A compromised module that a Mythos-class agent helped craft will not run silently in a CandorCore runtime. The ecosystem is being built to stay ahead of the tools that will probe it.

---

## Why Now?

The tools of software development are changing faster than the languages underneath them. AI agents can now write, read, and reason about code at scale — but they are doing so in languages designed for humans with implicit knowledge. The result is code that is syntactically correct but semantically wrong, because the language gave the agent no way to know better.

Candor is designed for the era where agents are collaborators, not just autocomplete. The same properties that make Candor honest with humans — explicit effects, verified contracts, declared intent — make it navigable and verifiable for agents. This is not a bolt-on feature. It is the design.

---

## Contributing

The most valuable contributions right now are:

- **Compiler bugs** — open issues with a minimal `.cnd` reproducer
- **Design feedback** — read the spec and open issues challenging assumptions
- **Prior art** — know of an existing language that solved a similar problem? Open an issue
- **Use cases** — describe a real program you'd want to write in Candor

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

The full development roadmap — including upcoming milestones, bootstrapping plan, and long-term vision — lives at **[docs/roadmap.md](docs/roadmap.md)**.

---

## Project Structure

```
candor/
├── docs/                      <- Documentation and Specifications
│   ├── specification.md       <- Full Language Specification
│   ├── language-reference.md  <- Implemented Language Reference
│   ├── welcome.md             <- Getting Started Guide
│   ├── roadmap.md             <- Future Plans
│   ├── mcp-context.md         <- AI/MCP Knowledge Context
│   └── what_is_in_core.md     <- Core v0.1.0 Feature Overview
├── compiler/                  <- candorc Source (Go 1.24)
│   ├── main.go
│   ├── lexer/
│   ├── parser/
│   ├── typeck/
│   └── emit_c/
├── tools/                     <- Development and Transformation Tools
├── examples/                  <- Sample Candor Programs
├── tests/                     <- Integration Test Suite
├── LICENSE                    <- Apache 2.0
├── README.md                  <- This File
└── .gitignore                 <- Repository Hygiene
```

---

## License

Copyright © 2026 Scott W. Corley
Licensed under the [Apache License, Version 2.0](LICENSE)

The Candor name, language specification, and compiler source are the property of Scott W. Corley. Contributions are accepted under the Apache 2.0 license.

---

*Candor — compiler v0.2.0 — Stage 1 bootstrap complete*
