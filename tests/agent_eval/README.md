# Candor Agent Productivity Evaluation

This directory contains benchmarks for evaluating how efficiently an AI agent
can write correct Candor programs — and how Candor compares to other languages
on agent-relevant dimensions.

## Dimensions

### 1. Ambiguity Score
How often does a task description produce a program that compiles and runs
correctly on the first attempt, with no clarification?

Lower ambiguity = fewer clarifying questions needed = fewer tokens wasted.

**Hypothesis:** Candor's explicit types, no-overloading, and mandatory return
types reduce ambiguity vs Python/Ruby but add more than TypeScript.

### 2. Token Efficiency
Tokens consumed per working line of output code.

Measured: (prompt tokens + generation tokens) / lines of correct Candor produced.

**Hypothesis:** Candor's keyword-dense syntax (match/must/loop/vec/option)
gives the agent high signal per token. No boilerplate class scaffolding needed.

### 3. Error Recovery Speed
After a GCC or candorc error, how many agent turns are needed to produce
a compiling program?

**Hypothesis:** Candor errors are specific (type mismatch, missing arm) and
map to single lines. C++ template errors and Python duck-type errors require
more context to resolve.

### 4. Feature Discovery
Can the agent correctly use a Candor-specific feature having only seen the docs?

Tests: `must` propagation, `match` exhaustion, `box<T>` semantics,
implicit tail return, catchall arm ordering.

---

## Correctness Test Tasks

Each task is a natural-language description. The agent produces Candor code.
The code is compiled and run against expected output.

Tasks are graded:
- **Pass on 1st attempt** (green)
- **Pass after N turns** (yellow, N>1)
- **Never pass** (red)

| ID | Task Description | Expected Output | Feature Tested |
|----|-----------------|----------------|----------------|
| A1 | "Write a function that returns the factorial of n using a loop" | `120` (n=5) | loop, implicit tail return |
| A2 | "Write a safe divide that returns an error string on div-by-zero, then call it twice" | `5\ndiv by zero` | result, must |
| A3 | "Define an enum Shape with Circle(r) and Rect(w,h) variants and a describe function" | `circle r=5\nrect 3x4` | enum, match exhaustion |
| A4 | "Accumulate 3 integers into a vec and print their sum" | `60` | vec, for |
| A5 | "Propagate an error through 2 nested must calls" | `error: bad input` | must chaining |
| A6 | "Write a function returning option<i64> that finds a target in a vec" | `1\nnot found` | option, match |
| A7 | "Write a recursive fibonacci (memoization not required)" | `55` (fib(10)) | recursion |
| A8 | "Use a match with a wildcard arm that returns early, then a value arm below it" | (correct output) | catchall arm ordering |

---

## Candor-Specific Ambiguity Tests

These test patterns that are unambiguous in Candor but commonly misused:

| ID | Pattern | What the agent must get right |
|----|---------|------------------------------|
| C1 | Wildcard-terminal arm ordering | `_ => return x` must come LAST in a match with value arms |
| C2 | Implicit tail return | No `return` keyword needed for last expression in non-unit fn |
| C3 | `must` vs `match` | `must` for propagating results inline; `match` for control flow |
| C4 | `let mut` requirement | Compiler rejects mutation of non-`mut` bindings explicitly |
| C5 | `box_deref` for box access | `box<T>` fields require explicit deref — no auto-deref |
| C6 | `vec_new()` initialization | `let mut v: vec<i64> = vec_new()` — type annotation required |

---

## Comparison Languages

The same logical tasks are given to an agent in:
- Candor
- Python 3
- Rust
- TypeScript (Node.js)

Metrics recorded per session:
- Did it compile/run on attempt 1?
- Total token count for the session
- Lines of code produced
- Number of agent turns to success
- First-attempt error type (syntax / type / logic / none)

---

## Running the Correctness Tests

```bash
# Run standard correctness tests
bash tests/run_tests.sh

# Future: automated agent eval harness
# For now: manual — give each task description to an agent, record metrics in results.csv
```

---

## Results Log

See `tests/agent_eval/results.csv` for recorded sessions.

Columns: `date,language,task_id,attempt_1_pass,total_turns,total_tokens,loc,first_error_type,notes`
