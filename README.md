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

**A systems programming language designed for semantic density, unambiguous logic, and agentic AI collaboration.**

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Status: Pre-Alpha](https://img.shields.io/badge/Status-Pre--Alpha-orange.svg)]()
[![Compiler: v0.1.0](https://img.shields.io/badge/Compiler-v0.1.0-green.svg)]()

---

## What is Candor?

Candor is a systems programming language built on one principle:

> A Candor program is a complete, honest declaration of everything it does.
> Nothing is hidden. Nothing is assumed. Nothing is implicit.

Every side effect is declared. Every error must be handled. Every contract between caller and callee is machine-readable. Every piece of intent lives in the code itself — not in a comment, a wiki, or a programmer's memory.

This makes Candor unusually well-suited to the way software is being built today — where AI agents read, write, and reason about code alongside humans. A language that is honest with humans is honest with agents for exactly the same reasons.

---

## A Taste of Candor

```candor
fn divide(a: i64, b: i64) -> result<i64, str>
    requires b != 0
{
    return ok(a / b)
}

fn main() -> unit effects(io) {
    let result = divide(10, 2) must {
        ok(val) => val
        err(e)  => return unit
    }
    print_int(result)
    return unit
}
```

What the compiler knows from these declarations alone:

- `divide` has a `requires b != 0` contract — the compiler emits an assertion in debug builds.
- `main` carries `effects(io)` — it cannot be called from a `pure` function.
- The `must{}` block is required — silence on a `result<T,E>` is a compile error.
- `ok(val)` and `err(e)` are exhaustive pattern arms; the compiler rejects incomplete `must` blocks.

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
  c_interop     honest C boundary                                            [future]
  llvm_backend  direct IR emission                                           [future]
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

### Prerequisites

- Go 1.24 or later
- GCC or a C compiler reachable as `gcc` or `cc` (override with the `CC` environment variable)

### Build the Compiler

```bash
git clone https://github.com/scottcorleyg1/candor
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
```

The compiler writes a `.c` file alongside the source for inspection, then invokes the system C compiler to produce a binary.

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

Candor `v0.1.0` is a working compiler. The pipeline `.cnd` source → lex → parse → typecheck → emit C → binary is complete. Real programs compile and run.

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
| map\<K,V\> (insert, get, remove, len, contains) | Complete |
| stdin I/O (read_*, try_read_*) | Complete |
| File I/O (read_file, write_file, append_file) | Complete |
| String operations (concat, len, eq, substr) | Complete |
| User-defined enums (sum types with data) | Complete |
| Ownership annotations (ref, refmut, move, deref) | Complete |
| i128 / u128 | Complete |
| Comptime evaluation (pure / effects []) | Complete |
| secret\<T\> information-flow enforcement | Complete |
| Lambdas / closures | Not yet |
| set\<T\> | Not yet |
| Allocator layer | Future |
| LLVM backend | Future |

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

*Candor — compiler v0.1.0*
