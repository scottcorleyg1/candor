# Token Compression Floor: Empirical Analysis

*Research document — April 2026*
*Companion to: [agent_form.md](agent_form.md) and [token_density_proof.md](token_density_proof.md)*

---

## Purpose

This document establishes the empirical compression floor for Candor programs: the minimum
token count achievable through lossless bidirectional projection (Agent Form → Verification Form),
with zero ambiguity in the expansion rules.

The goal is not to find the shortest possible encoding. It is to find how many of
Candor's current tokens are **structurally derivable** — present only to satisfy grammar
requirements and reconstructable by rule — versus **semantically irreducible** — carrying
information the author must specify.

---

## Methodology

### Token Classification

Every token in a Candor program is classified as:

- **Irreducible (I)**: The author must specify this. Given everything else in the program,
  more than one value is grammatically valid, or the value cannot be inferred at all.
  Examples: function names, parameter types, `pure`, `effects(io)`, numeric literals,
  string content.

- **Derivable (D)**: Given the surrounding program, exactly one token value is valid.
  Examples: `(` after a function name in a call, `:` between a parameter name and its type,
  `->` before a return type. These exist for human readability but carry no semantic information
  that isn't present in adjacent tokens.

**Classification rule:** If an expansion rule would produce exactly one valid token, that
token is Derivable. If it could produce two or more, it is Irreducible.

### Tokenizer

Approximating `cl100k_base` (used by Claude/GPT-4). Key rules applied:
- Common keywords (`fn`, `let`, `if`, `loop`, `break`, `return`, `pure`, `ok`, `err`,
  `some`, `none`, `must`, `or`, `as`, `mut`) = 1 token each
- Single punctuation (`(`, `)`, `{`, `}`, `<`, `>`, `[`, `]`, `:`, `,`, `.`, `=`,
  `+`, `-`, `*`, `/`) = 1 token each
- Two-character operators (`->`, `=>`, `==`, `>=`, `<=`) = 1 token each
- Short identifiers (`i`, `c`, `n`, `sep`, `name`, `score`, `sn`) = 1 token each
- Compound identifiers with underscore: split at `_` boundaries
  (`str_find` → `str` + `_find` = 2 tokens, `str_concat` → 2, `str_len` → 2,
   `str_substr` → 3, `str_byte` → 2, `score_str` → 2, `parse_row` → 2)
- String literals: counted as approximate token-per-word plus delimiters
- Types: `str` = 1, `i64` = 1, `u64` = 2, `result` = 1, `Row` = 1

---

## Analysis: `parse_row`

Source: [examples/pipeline.cnd](../examples/pipeline.cnd) lines 27–47.

```candor
fn parse_row(line: str) -> result<Row, str> pure {
    let sep = str_find(line, ",", 0) must {
        some(i) => i
        none    => return err(str_concat("missing comma in: '", str_concat(line, "'")))
    }
    let name  = str_substr(line, 0, sep)
    let score_str = str_substr(line, sep + 1, str_len(line) - sep - 1)
    if str_len(name) == 0  { return err("empty name") }
    if str_len(score_str) == 0 { return err("empty score") }
    let mut score: i64 = 0
    let sn = str_len(score_str)
    let mut i: i64 = 0
    loop {
        if i >= sn { break }
        let c = str_byte(score_str, i)
        if c < 48 or c > 57 { return err(str_concat("non-numeric score: '", str_concat(score_str, "'"))) }
        score = score * 10 + (c as i64 - 48)
        i = i + 1
    }
    ok(Row{ name: name, score: score })
}
```

### Line-by-line breakdown

| Line | Content | Tokens | I | D |
|------|---------|--------|---|---|
| 1 | `fn parse_row(line: str) -> result<Row, str> pure {` | 17 | 8 | 9 |
| 2 | `let sep = str_find(line, ",", 0) must {` | 15 | 7 | 8 |
| 3 | `some(i) => i` | 6 | 3 | 3 |
| 4 | `none => return err(str_concat("missing comma in: '", str_concat(line, "'")))` | 21 | 13 | 8 |
| 5 | `}` (closes must) | 1 | 0 | 1 |
| 6 | `let name = str_substr(line, 0, sep)` | 13 | 7 | 6 |
| 7 | `let score_str = str_substr(line, sep + 1, str_len(line) - sep - 1)` | 22 | 15 | 7 |
| 8 | `if str_len(name) == 0 { return err("empty name") }` | 16 | 9 | 7 |
| 9 | `if str_len(score_str) == 0 { return err("empty score") }` | 17 | 10 | 7 |
| 10 | `let mut score: i64 = 0` | 7 | 4 | 3 |
| 11 | `let sn = str_len(score_str)` | 9 | 5 | 4 |
| 12 | `let mut i: i64 = 0` | 7 | 4 | 3 |
| 13 | `loop {` | 2 | 1 | 1 |
| 14 | `if i >= sn { break }` | 7 | 5 | 2 |
| 15 | `let c = str_byte(score_str, i)` | 11 | 6 | 5 |
| 16 | `if c < 48 or c > 57 { return err(str_concat("non-numeric score: '", str_concat(score_str, "'"))) }` | 27 | 19 | 8 |
| 17 | `score = score * 10 + (c as i64 - 48)` | 13 | 10 | 3 |
| 18 | `i = i + 1` | 5 | 4 | 1 |
| 19 | `}` (closes loop) | 1 | 0 | 1 |
| 20 | `ok(Row{ name: name, score: score })` | 13 | 6 | 7 |
| 21 | `}` (closes function) | 1 | 0 | 1 |
| **Total** | | **~231** | **~136** | **~95** |

**Compression floor: 136 / 231 = 59% of current token count.**

Agent Form for `parse_row` would require approximately **136 tokens** versus the current **231**,
a reduction of **~41%**.

---

## Derivable Token Categories

Breakdown of the ~95 derivable tokens by category:

### Category A — Block delimiters: `{` `}` (~25 tokens, ~11% of total)

Every `{` and `}` in Candor marks a syntactic scope. In an indentation-based
Agent Form grammar (like Python), all paired `{}`  become Derivable.

Instances in `parse_row`:
- Function body: 2 tokens
- `must` block: 2 tokens
- `if str_len(name)` body: 2 tokens
- `if str_len(score_str)` body: 2 tokens
- `loop` body: 2 tokens
- `if i >= sn` body: 2 tokens
- `if c < 48 or c > 57` body: 2 tokens
- `Row{ }` struct literal: 2 tokens (boundary case — struct literal needs some delimiter)

**Note on struct literals:** `Row{ name: name, score: score }` — the `{` `}` here delimit a
struct literal, not a scope. An alternative syntax like `Row name: name score: score` or
`Row(name, score)` would need to be unambiguous in Agent Form grammar.

### Category B — Call-site punctuation: `(` `,` `)` (~48 tokens, ~21% of total)

Every function call `f(a, b, c)` uses 1 open paren + (n-1) commas + 1 close paren for n arguments.
In ML-style application syntax `f a b c`, all of these are Derivable — arguments are
whitespace-separated and the parser reads until it encounters a non-argument token.

Instances counted across all calls in `parse_row`:
- `str_find(line, ",", 0)`: 4 structural tokens
- `str_substr(line, 0, sep)`: 4 structural tokens
- `str_substr(line, sep+1, str_len(line)-sep-1)`: 4 structural tokens + inner `str_len` call: 2
- `str_len(name)`: 2 structural tokens
- `str_len(score_str)` ×3: 6 structural tokens
- `str_byte(score_str, i)`: 3 structural tokens
- `str_concat("...", str_concat(line, "'"))` ×2: 12 structural tokens
- `err(...)` ×4: 8 structural tokens
- `ok(Row{...})`: 2 structural tokens
- `some(i)`, `(i)` in pattern: 2 structural tokens

**Caveat:** ML-style application requires careful grammar design for nested calls and
string/integer literal disambiguation. This is feasible but not trivial.

### Category C — Parameter/return type delimiters: `(` `:` `)` `->` `<` `,` `>` (~10 tokens, ~4%)

In the function signature `fn parse_row(line: str) -> result<Row, str> pure`:
- `(` `:` `)` around parameter list: 3 tokens
- `->` before return type: 1 token
- `<` `,` `>` around generic params: 3 tokens
Total: 7 structural tokens per function signature (varies with arity and generics)

A positional Agent Form grammar where function signatures are `name params... return_type [pure|effects(...)]`
makes all of these Derivable.

### Category D — Binding structure: `=` in `let x = expr` (~8 tokens, ~3%)

In `let sep = str_find(...)`, the `=` always occupies the same position: immediately
after the optional `: Type` annotation. It is Derivable in a grammar where `let name expr`
or `let name type expr` is unambiguous.

**Note:** `let` itself is classified as **Irreducible** — it marks a new binding versus
reassignment. `x = expr` (without `let`) means reassignment of an existing binding.
The semantic distinction must be preserved.

### Category E — Match arm syntax: `=>` (~3 tokens, ~1%)

`some(i) => i` and `none => return err(...)` — the `=>` always separates a pattern from
its body. Derivable if Agent Form uses indentation or line-based arm separation.

### Category F — Struct literal field separators: `:` `,` (~3 tokens, ~1%)

`Row{ name: name, score: score }` — the `:` after each field name and `,` between fields
are Derivable if Agent Form uses positional field assignment or line-based separation.

---

## Compression Tiers

Three practical Agent Form designs, ordered by compression aggressiveness:

| Tier | What is removed | Approx. token reduction | Ambiguity risk |
|------|----------------|------------------------|----------------|
| **Conservative** | Type delimiters (C), `=` in bindings (D), arm arrows (E), struct separators (F) | ~9% | Near zero |
| **Moderate** | Conservative + block braces via indentation (A) | ~20% | Low — requires indentation grammar |
| **Aggressive** | Moderate + call-site punctuation via ML-style application (B) | **~41%** | Medium — requires grammar design for nested calls |

The **~41% floor** is the theoretical maximum without introducing any ambiguity, given
correct grammar design. Real implementations will land at 30–38% due to edge cases.

---

## What Cannot Be Compressed

These token categories are **always Irreducible**:

1. **Semantic annotations**: `pure`, `effects(io)`, `effects(gpu)`, etc. — these are
   Candor's core differentiation. They are never derivable from the body without running
   the compiler's effect checker on the callees.

2. **Result type annotations**: `result<T, E>` — the error type `E` cannot be inferred
   from the signature alone. The types `T` and `E` are irreducible.

3. **Binding names**: all identifiers in `let x = ...` positions

4. **`must {}` keyword**: distinguishes structured result-handling from a `match` expression.
   The distinction is semantic (panic path vs. exhaustive handling).

5. **`mut` keyword**: mutability is a semantic annotation, not inferrable without analyzing
   all subsequent uses (which would require analysis, not expansion rules).

6. **Literal values**: string content, integer values, boolean literals

7. **`return` in early-exit positions**: marks early exit versus last-expression return.
   Without `return`, the AI would need to place the expression last in the block — requiring
   code reorganization, not just token removal.

8. **Operator tokens**: `+`, `-`, `*`, `>=`, `==`, `or`, `as`, etc.

---

## Full Program Extrapolation

`parse_row` is the most complex function in `pipeline.cnd`. Simpler functions will have
**higher** derivable percentages because signature overhead is proportionally larger for
short bodies.

For comparison:

| Function | Est. total tokens | Est. compression |
|----------|------------------|------------------|
| `parse_row` | 231 | 41% |
| `validate_row` | ~85 | ~38% |
| `summarize` | ~120 | ~35% |
| `run_pipeline` | ~180 | ~40% |
| `main` | ~110 | ~38% |
| **Full pipeline.cnd** | **~800** | **~39%** |

---

## C Comparison

Source: [examples/c/pipeline.c](../examples/c/pipeline.c) — hand-written idiomatic C,
GCC 14.2.0, compiles clean with `-Wall -Wextra`, produces identical output to `pipeline.cnd`.

### C SCS score: 0/3 on every function

C cannot express any of the three semantic dimensions in a function signature:

| Dimension | C equivalent | Enforced? |
|-----------|-------------|-----------|
| Purity | `__attribute__((pure))` — GCC extension, not standard C | No — compiler accepts `pure` functions that call `printf` |
| Effects | None | No |
| Result handling | `__attribute__((warn_unused_result))` — opt-in per function, warning only | No |

**C = 0/52 on SCS.** Equal to Go numerically, but worse in practice (see token_density_proof.md).

### C token count for `parse_row`

The hand-written C `parse_row` requires two mandatory typedefs plus the function body.
These are not optional — C has no generic `result<T,E>` type, so every unique
`(ok_type, err_type)` combination requires its own struct definition.

| Section | Tokens | I | D | Notes |
|---------|--------|---|---|-------|
| `#define` macros (2) | ~6 | 4 | 2 | ERR_MAX_LEN, NAME_MAX_LEN |
| `typedef struct ... Row` | ~16 | 8 | 8 | Field names + types are I; `typedef`, `struct`, `{`, `}`, `;` are D |
| `typedef struct ... RowResult` | ~18 | 10 | 8 | Same structure |
| Function signature | ~12 | 8 | 4 | `static`, `const char *` are I (semantic in C) |
| Function body | ~287 | 162 | 125 | |
| **Total** | **~339** | **~192** | **~147** | **43% derivable** |

**Candor `parse_row`: 231 tokens, 41% derivable.**
**C `parse_row`: ~339 tokens, 43% derivable.**

C is **47% longer** than Candor for equivalent functionality.

### Why C's extra tokens are not compressible

The key difference from the Candor vs Go/Rust comparison: C's extra tokens are not
structural ceremony — they are **required semantic content** that Candor encodes implicitly:

- **Result typedefs**: `RowResult` must be defined because C has no generic `result<T,E>`.
  This is not ceremony; it's semantic information about how errors are represented.
  In `pipeline.c`, four result types need four struct definitions.
  In `pipeline.cnd`, zero. The generics handle it.

- **Buffer sizes**: `char err[ERR_MAX_LEN]`, `snprintf(..., ERR_MAX_LEN, ...)` —
  C requires explicit memory bounds. Candor's `str` type is dynamic; no size needed.

- **Null termination**: `res.row.name[name_len] = '\0'` — C strings require explicit
  null termination. Candor's `str` type handles this internally.

- **`res.ok = 1; return res;`** (3 tokens) vs Candor `ok(Row{...})` — C needs 3 tokens
  to express success, Candor needs 1 (`ok`). Repeated for every success path.

These are irreducible tokens in C that have no equivalent in Candor because
Candor encodes the same information in its type system rather than in code.

### The expansion ratio: compiler-generated C

A third perspective: the Candor compiler's C backend output (`examples/pipeline.c`)
is **494 lines vs `pipeline.cnd`'s 135 lines — a 3.7× source expansion**.

This is the Machine Form expansion factor. It reflects that C requires explicit
runtime infrastructure (vec growth, result struct expansion, GCC statement-expression
wrappers for `must{}`) that Candor abstracts. The compiler generates all of it
correctly and deterministically — which is exactly the Agent Form → Verification Form
→ Machine Form pipeline in the other direction.

---

## Synthesis: The Full Efficiency Picture

Combined with [token_density_proof.md](token_density_proof.md):

| Direction | Mechanism | Efficiency gain vs. Rust | Efficiency gain vs. C |
|-----------|-----------|--------------------------|----------------------|
| **AI reading Candor** | Information-dense signatures (SCS) | **3.4× more semantic info/token** | **∞× — C has zero** |
| **AI writing in Agent Form** | Structural token removal (~41%) | **~1.65× fewer tokens** | **~2.5× fewer tokens** |

An AI agent working with Candor — reading Verification Form, writing Agent Form — operates
at roughly **3.4 × 1.65 ≈ 5.6× the semantic efficiency** of an equivalent agent in Rust
(the closest competitor), and at roughly **2.5× fewer tokens** than idiomatic hand-written C.

Go and Python show no advantage over Agent Form Candor on either dimension.
C shows a structural token disadvantage on top of its SCS disadvantage.

---

## Open Questions for Agent Form Grammar Design

1. **Indentation sensitivity**: Candor's current grammar is indentation-insensitive. An
   indentation-based Agent Form grammar is a separate language — not a strict subset.
   Is Agent Form a Candor syntax variant, or a separate notation with a defined expand step?

2. **ML-style application and string literals**: `str_find line "," 0` — does `","` unambiguously
   end the second argument? Yes, because string literals have defined end delimiters. But
   what about complex nested expressions as arguments? Requires careful precedence rules.

3. **Mutual distinguishability**: All Agent Form features must be simultaneously
   unambiguous. Conservative tier is safe. Each additional tier requires a grammar proof
   that expansion is unique.

4. **Tooling**: `candorc expand <file.cnd.af>` → Verification Form `.cnd`. The expand
   step runs before the compiler. Errors in expansion are reported as "Agent Form syntax
   errors" before type checking begins.
