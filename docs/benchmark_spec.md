# Token Usage Benchmark Spec: Candor vs Go, Rust, C, C++

## Purpose

Measure the tokens an AI model consumes to produce a correct, compiling implementation
of the same program in each language. The gap between languages is the empirical,
dollar-denominated cost of missing semantic information in signatures.

## What We Are Measuring

Not implementation complexity. Not language verbosity. **Disambiguation cost**: how many
tokens does the AI burn resolving questions that Candor answers in the signature?

- Is this function safe to call from a pure context?
- Does this function do I/O?
- If this function can fail, am I required to handle it?

In Candor, these answers are in the signature. In every other language in this benchmark,
they are not — the AI must infer them, hedge on them, or ask about them.

## Task: Log Batch Processor

A single, well-defined program that requires pure computation, I/O, error handling, and
per-record error tolerance. All five languages implement the exact same behavior.

### Behavior

1. Accept an input directory path as a command-line argument
2. Read every `.log` file in that directory
3. For each file, parse every non-empty line in the format `[LEVEL] message`
   - Valid levels: `ERROR`, `WARN`, `INFO`, `DEBUG`
   - A line with an unknown level is a parse error for that line (not the file)
   - A line missing the `[...]` prefix is a parse error for that line
4. Compute per-file statistics: count of each level, count of parse errors
5. Write one summary line per file to stdout:
   ```
   filename.log: ERROR=2 WARN=1 INFO=4 DEBUG=0 parse_errors=1
   ```
6. After all files, print a totals line:
   ```
   TOTAL: files=3 lines=47 parse_errors=2
   ```
7. Exit 0 on success. Exit 1 if the directory cannot be read.

### Constraints (enforced identically across all languages)

- No external libraries. Only the standard library of the language.
- The log file format parser must be a standalone function (not inlined into the I/O loop).
- The per-file statistics aggregator must be a standalone function.
- Error handling must be explicit: a parse error on one line must not abort the file.

### Why this task

| Property | Why it matters |
|----------|---------------|
| Pure parse function | Candor can declare it `pure`; others cannot — AI must infer |
| Pure aggregate function | Same — pure with no I/O, but other languages can't say so |
| Effectful I/O orchestrator | Candor declares `effects(io)`; others hide it in the return type or not at all |
| Per-line error tolerance | Tests error handling design: `result<T,E>` vs exceptions vs sentinel values |
| Directory + file I/O | Real I/O, not a toy; forces the AI to use the stdlib correctly |
| No external deps | Eliminates library knowledge as a variable |

---

## Benchmark Protocol

### Setup

1. Start a fresh Claude API session for each language (no context carryover)
2. Use the same model for all runs (claude-sonnet-4-6 or claude-opus-4-7)
3. Record: input tokens, output tokens, total tokens per run

### System prompt (identical for all runs)

```
You are an expert systems programmer. Implement the program described in the user
message. Produce a single, complete, compilable file with no placeholders or TODOs.
Do not explain the code. Output only the source file.
```

### User prompt (language-specific)

The core spec is identical. The language name and file extension are substituted.

```
Implement the following program in {LANGUAGE}. Output a single {EXTENSION} file.

PROGRAM: Log Batch Processor

Behavior:
1. Accept one command-line argument: an input directory path.
2. Read every file in that directory whose name ends with ".log".
3. For each file, parse every non-empty line. Line format: [LEVEL] message
   Valid levels: ERROR, WARN, INFO, DEBUG (uppercase only).
   A line with an unknown or missing level is a parse error for that line — 
   do not abort; count it and continue.
4. Compute per-file statistics: count of each level, count of parse errors.
5. Print one summary line per file (sorted by filename):
   filename.log: ERROR=2 WARN=1 INFO=4 DEBUG=0 parse_errors=1
6. After all files, print:
   TOTAL: files=3 lines=47 parse_errors=2
7. Exit code 0 on success. Exit code 1 if the directory cannot be read.

Requirements:
- No external libraries. Standard library only.
- The line parser must be a standalone named function (not inlined).
- The per-file statistics aggregator must be a standalone named function.
- A parse error on one line must not abort processing the rest of the file.
```

### Languages and compilers

| Language | Compiler/Runtime | Compile command |
|----------|-----------------|-----------------|
| Candor | candorc-windows-amd64.exe | `candorc output.cnd` |
| Go | go 1.22+ | `go run output.go` |
| Rust | rustc 1.77+ | `rustc output.rs -o output` |
| C | gcc 13+ | `gcc -o output output.c` |
| C++ | g++ 13+ | `g++ -std=c++20 -o output output.cpp` |

### Correctness test

Run each compiled binary against the same test directory:

```
test_logs/
  app.log:
    [INFO] server started
    [WARN] high memory usage
    [ERROR] disk full
    [ERROR] connection refused
    [INFO] request received
    [BADLEVEL] unknown thing
  db.log:
    [DEBUG] query executed
    [INFO] connection pool ready
    [ERROR] timeout
    not a valid line
```

Expected stdout:
```
app.log: ERROR=2 WARN=1 INFO=2 DEBUG=0 parse_errors=1
db.log: ERROR=1 WARN=0 INFO=1 DEBUG=1 parse_errors=1
TOTAL: files=2 lines=10 parse_errors=2
```

A run counts as **correct** only if the output matches exactly (modulo trailing newline).

### Scoring

For each language run, record:

| Metric | Description |
|--------|-------------|
| `input_tokens` | Tokens in system + user prompt |
| `output_tokens` | Tokens in the generated source file |
| `total_tokens` | input + output |
| `compiles` | Does it compile without modification? (bool) |
| `correct` | Does the output match expected? (bool) |
| `rounds` | How many API calls to reach a correct, compiling result |
| `total_tokens_to_correct` | Sum of all tokens across all rounds |

The headline number is **total_tokens_to_correct**: the full AI cost of getting a
working implementation in each language.

---

## Expected Results (Hypothesis)

Based on the Signature Completeness Score analysis in `token_density_proof.md`:

| Language | Hypothesis | Reasoning |
|----------|-----------|-----------|
| Candor | Fewest output tokens, 1 round | `pure` on parser/aggregator, `effects(io)` on orchestrator — AI has zero ambiguity about function contracts |
| Rust | Second fewest, 1 round | `Result<T,E>` enforced; purity undeclared but Rust idioms are tight |
| Go | More than Rust, likely 1 round | `(T, error)` pattern familiar; error handling bypassable but AI won't bypass it |
| C++ | More than Go, possible 2 rounds | No enforcement; AI may hedge with extra defensive code; template/exception choices to make |
| C | Most tokens, possible 2 rounds | Manual error propagation, no standard Result type, AI writes defensive boilerplate |

The critical measurement: **does Candor's output token count beat Go/Rust not because
the language is "smaller" but because the AI needs fewer tokens to express the same
guarantees?**

In Go, the AI will likely add a comment `// pure: no side effects` above the parser
function. In Candor, that comment is the signature. The difference is measurable.

---

## Harness Implementation Plan

The benchmark harness lives in `benchmarks/`:

```
benchmarks/
  run_benchmark.py        — calls Claude API, records tokens, runs correctness check
  benchmark_config.json   — model, languages, prompts, expected output
  test_logs/              — the correctness test fixture (app.log, db.log)
  results/                — one JSON file per run, timestamped
  summarize.py            — reads results/, prints comparison table
```

`run_benchmark.py` spec:
- Uses `anthropic` Python SDK
- One API call per language (system + user prompt as defined above)
- Writes generated source to `results/{lang}_{timestamp}.{ext}`
- Attempts compile; records exit code
- Runs correctness test; records pass/fail
- If compile fails: optional second round with compiler error appended to conversation
- Writes `results/run_{timestamp}.json` with all metrics

---

## C and C++ Additions to token_density_proof.md

Once the benchmark runs, the scores feed back into the proof doc. For now, the
static SCS scores for C and C++ are:

### C

| Function | P | E | R | Score |
|----------|---|---|---|-------|
| `parse_line` (returns int status) | 0 | 0 | 0 | 0 |
| `aggregate_stats` (updates struct) | 0 | 0 | 0 | 0 |
| `process_directory` (all I/O) | 0 | 0 | 0 | 0 |
| **Total / 9** | | | | **0** |

C has no error enforcement (`int` return codes are silently ignorable), no purity
concept, and no effects declarations. C is strictly worse than Go on all three
dimensions because even Go's `(T, error)` convention is visible in the return type.

### C++

| Function | P | E | R | Score |
|----------|---|---|---|-------|
| `parse_line` | 0 | 0 | 0* | 0 |
| `aggregate_stats` | 0 | 0 | 0* | 0 |
| `process_directory` | 0 | 0 | 0* | 0 |
| **Total / 9** | | | | **0** |

`*` C++ `[[nodiscard]]` on a return type achieves R=1, but it is opt-in and not
standard practice for this kind of function. `const` member functions do not mean
"pure" — a `const` method can call `printf`. No effects system exists.

### Updated aggregate table (after adding C and C++)

| Language | Total SCS | Max | % Complete | vs Candor |
|----------|-----------|-----|------------|-----------|
| **Candor** | **24** | **52** | **46%** | baseline |
| Rust | 7 | 52 | 13% | 3.5× less |
| Go | 0 | 52 | 0% | ∞× less |
| C++ | 0 | 52 | 0% | ∞× less |
| C | 0 | 52 | 0% | ∞× less |

Go, C++, and C score identically on the static metric — the empirical benchmark
is what separates them. C is expected to cost the most tokens; C++ slightly less
due to STL familiarity and exception-based error patterns the AI knows well.
