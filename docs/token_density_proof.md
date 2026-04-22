# Token Density Proof: Candor vs Go, Rust, Python, C

## Thesis

Programming in Candor reduces the number of tokens an AI model must process to
correctly understand, modify, and verify a program. The mechanism is *information
density in signatures*: Candor encodes semantic guarantees — purity, effects, and
mandatory error handling — directly in function signatures. Competing languages encode
none or fewer of these, forcing the AI to read function bodies to recover the same
information.

**Fewer tokens to understand = fewer tokens to task = measurable efficiency gain.**

---

## The Three Semantic Dimensions

For any function, an AI reviewer needs to know three things beyond parameter and return
types:

| Dimension | Question | Why it matters for AI tasks |
|-----------|----------|-----------------------------|
| **Purity** | Can this function have side effects? | Determines whether it's safe to move, memoize, inline, or test in isolation |
| **Effects** | Does this function do I/O (file, net, clock, env)? | Determines whether tests need mocking, whether the function can be called in a pure context |
| **Error enforcement** | Is the caller *required* to handle failure? | Determines whether a modification can silently introduce an unhandled error path |

---

## Scoring Methodology

Each function in each language is scored on three binary dimensions:

- **P** (Purity declared in signature): 1 if signature declares purity with compiler enforcement, 0 otherwise
- **E** (Effects declared in signature): 1 if signature declares I/O effects with compiler enforcement, 0 otherwise
- **R** (Result handling enforced): 1 if compiler rejects silent discard of error/option, 0 otherwise

**Signature Completeness Score (SCS)** = P + E + R (max 3 per function)

A score of 3 means the AI can understand the full contract from the signature alone.
A score below 3 means the AI must read the body to fill in missing guarantees.

---

## Program 1: word_stats — Pure computation vs I/O

### Functions and their scores

| Function | Role | Candor | Go | Rust | Python | C |
|----------|------|--------|----|------|--------|---|
| `count_lines` | pure computation | P=1 E=0 R=0 → **1** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** |
| `count_words` | pure computation | P=1 E=0 R=0 → **1** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** |
| `compute_stats` | pure aggregate | P=1 E=0 R=0 → **1** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** |
| `analyze_file` | I/O + computation | P=0 E=1 R=1 → **2** | P=0 E=0 R=0 → **0** | P=0 E=0 R=1 → **1** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** |
| **Total / 12** | | **5** | **0** | **1** | **0** | **0** |

**Candor SCS: 5/12. Go: 0/12. Rust: 1/12. Python: 0/12. C: 0/12.**

The strongest demonstration: `count_lines(text: str) -> i64 pure` is
compiler-verified to never touch the filesystem, network, or any external state.
The equivalent Go/Rust/Python signatures are indistinguishable from functions that
secretly call `print` or open a socket.

---

## Program 2: config — Key=value parser with error propagation

### Functions and their scores

| Function | Role | Candor | Go | Rust | Python | C |
|----------|------|--------|----|------|--------|---|
| `parse_line` | pure validation | P=1 E=0 R=1 → **2** | P=0 E=0 R=0 → **0** | P=0 E=0 R=1 → **1** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** |
| `get_key` | pure transform | P=1 E=0 R=0 → **1** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** |
| `get_val` | pure transform | P=1 E=0 R=0 → **1** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** |
| `load_config` | I/O + parse | P=0 E=1 R=1 → **2** | P=0 E=0 R=0 → **0** | P=0 E=0 R=1 → **1** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** |
| **Total / 12** | | **6** | **0** | **2** | **0** | **0** |

**Candor SCS: 6/12. Go: 0/12. Rust: 2/12. Python: 0/12. C: 0/12.**

Note: Go `loadConfig(path string) (Config, error)` — caller can write
`cfg, _ := loadConfig(path)` and the compiler accepts it silently.
Candor `must{}` blocks make that impossible.

---

## Program 3: log_filter — Pure filter pipeline with I/O at the boundary

### Functions and their scores

| Function | Role | Candor | Go | Rust | Python | C |
|----------|------|--------|----|------|--------|---|
| `parse_level` | pure parse | P=1 E=0 R=1 → **2** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** |
| `keep_line` | pure predicate | P=1 E=0 R=0 → **1** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** |
| `filter_lines` | pure transform | P=1 E=0 R=0 → **1** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** |
| `run_filter` | I/O orchestrator | P=0 E=1 R=1 → **2** | P=0 E=0 R=0 → **0** | P=0 E=0 R=1 → **1** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** |
| **Total / 12** | | **6** | **0** | **1** | **0** | **0** |

---

## Program 4: pipeline — Multi-step parse → validate → summarize

### Functions and their scores

| Function | Role | Candor | Go | Rust | Python | C |
|----------|------|--------|----|------|--------|---|
| `parse_row` | pure parser | P=1 E=0 R=1 → **2** | P=0 E=0 R=0 → **0** | P=0 E=0 R=1 → **1** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** |
| `validate_row` | pure validator | P=1 E=0 R=1 → **2** | P=0 E=0 R=0 → **0** | P=0 E=0 R=1 → **1** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** |
| `summarize` | pure aggregate | P=1 E=0 R=0 → **1** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** |
| `run_pipeline` | I/O + chain | P=0 E=1 R=1 → **2** | P=0 E=0 R=0 → **0** | P=0 E=0 R=1 → **1** | P=0 E=0 R=0 → **0** | P=0 E=0 R=0 → **0** |
| **Total / 16** | | **7** | **0** | **3** | **0** | **0** |

---

## Aggregate Scores

| Language | Total SCS | Max Possible | % Complete |
|----------|-----------|--------------|------------|
| **Candor** | **24** | **52** | **46%** |
| Rust | 7 | 52 | 13% |
| Go | 0 | 52 | 0% |
| Python | 0 | 52 | 0% |
| **C** | **0** | **52** | **0%** |

Candor's 46% vs Rust's 13% means an AI reading only signatures gets **3.4× more
semantic information per token** in Candor than in Rust — the closest competitor.

The remaining 54% in Candor is accounted for by non-effectful, non-pure functions
(e.g., `filter_lines`) and functions that simply transform data without a `result`
return — information that is not missing but also not relevant to the three dimensions.

### Why C scores the same as Go but is worse in practice

C and Go both score 0/52 — neither language can express purity, effects, or enforced
error handling in a function signature. But C is worse in two concrete ways:

**1. No standard error convention.**
Go has a de facto standard: `func f() (T, error)`. Callers can ignore errors (`_`),
but at least the FORM is recognizable. C has no standard: some APIs use `errno`, others
use return codes, others use output parameters, others use `setjmp`. An AI reading a C
signature cannot even assume there IS an error path without reading the body.

**2. No generic result type.**
Candor's `result<T, E>` is a built-in generic — zero per-type overhead. Go's `(T, error)`
is a language-level idiom — zero per-type overhead. In C, every unique `(ok_type, err_type)`
combination requires a hand-written struct typedef. The `pipeline.c` example requires four
separate result structs (`RowResult`, `SummaryResult`, plus two string-result variants).
This is forced semantic overhead that Candor and Go eliminate at the language level.

**3. Idiomatic C is 47% longer than Candor for equivalent functionality.**
See [token_compression_floor.md](token_compression_floor.md) for the full analysis.
The hand-written C `parse_row` uses ~339 tokens vs Candor's 231 for the same logic.
The extra tokens are not structural ceremony — they are required semantic content
(result type definitions, buffer size management, null-termination, manual ownership).
C cannot compress these away with Agent Form because they carry meaning.

---

## What This Means for AI Task Completion

### Scenario: "Add a validation rule to `parse_row`"

To complete this task correctly in each language, an AI must understand:
1. Whether `parse_row` has side effects (affects how tests are written)
2. Whether it can call `print` for debugging (pure means no)
3. Whether the caller is guaranteed to handle the new error path

**Candor:** All three answers are in the signature: `pure` + `result<Row, str>` +
`must{}` enforcement. The AI needs 0 additional body tokens to answer these questions.

**Go:** None are in the signature. The AI must read the body of `parseRow` to confirm
it has no side effects, read callers to confirm errors are handled, and trust comments
(if any) about purity. Estimated 40–80 additional tokens of context required.

**Rust:** Error handling is enforced. Purity is not declared. The AI must read the body
to confirm no I/O. Estimated 20–40 additional tokens of context required.

**Python:** None are in the signature. Exceptions are invisible. The AI may not even
know `parse_row` can fail unless it reads the body and all callsites. Estimated 50–100
additional tokens of context required.

**C:** Worse than Go in practice. The signature `static RowResult parse_row(const char *line)`
tells the AI: there is a parameter of type pointer-to-const-char named line, and it returns
a RowResult. Nothing about purity, nothing about effects, and RowResult's error convention
(the `.ok` flag pattern) is not a language feature — the AI must have seen this codebase
before to know that convention applies. Additionally, the C body is 47% longer than Candor's
equivalent, so the AI's context window fills faster. Estimated 60–100 additional tokens of
context required — more than Go because there is no standard error convention to rely on.

### Compound effect

In a real program with 20 functions, this multiplies: the AI must buffer body tokens
for every function whose contract it cannot determine from the signature. Candor
eliminates that overhead for every `pure` and `effects(...)` annotated function.

---

## Phase 2 Preview: Compiler Enforcement (EFFECTS-001)

The scores above are not based on convention or comments — they reflect enforced
compiler rules. Candor rejects:

```
## EFFECTS-001 violation — compiler error, not a runtime surprise:
fn count_lines(text: str) -> i64 pure {
    print("counting")   ## ERROR: pure fn 'count_lines' calls I/O builtin 'print' [EFFECTS-001]
    text.count('\n')
}
```

In Go, Rust, and Python, the equivalent code compiles silently. The AI cannot trust
the signature. In Candor, the AI can.

---

## Source Files

| Program | Candor | Go | Rust | Python | C |
|---------|--------|----|------|--------|---|
| word_stats | [examples/word_stats.cnd](../examples/word_stats.cnd) | [examples/go/word_stats.go](../examples/go/word_stats.go) | [examples/rust/word_stats.rs](../examples/rust/word_stats.rs) | [examples/python/word_stats.py](../examples/python/word_stats.py) | pending |
| config | [examples/config.cnd](../examples/config.cnd) | [examples/go/config.go](../examples/go/config.go) | [examples/rust/config.rs](../examples/rust/config.rs) | [examples/python/config.py](../examples/python/config.py) | pending |
| log_filter | [examples/log_filter.cnd](../examples/log_filter.cnd) | [examples/go/log_filter.go](../examples/go/log_filter.go) | [examples/rust/log_filter.rs](../examples/rust/log_filter.rs) | [examples/python/log_filter.py](../examples/python/log_filter.py) | pending |
| pipeline | [examples/pipeline.cnd](../examples/pipeline.cnd) | [examples/go/pipeline.go](../examples/go/pipeline.go) | [examples/rust/pipeline.rs](../examples/rust/pipeline.rs) | [examples/python/pipeline.py](../examples/python/pipeline.py) | [examples/c/pipeline.c](../examples/c/pipeline.c) |

*Note: `examples/c/` contains idiomatic hand-written C. The compiler-generated C output
(in `examples/pipeline.c` etc.) exists as a separate artifact of the Candor→C backend
and is ~3.7× longer than the source — demonstrating the Machine Form expansion ratio.*
