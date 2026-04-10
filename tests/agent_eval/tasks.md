# Agent Evaluation Tasks

Give each task description verbatim to the agent. Provide only:
1. The task description
2. The Candor syntax reference: `docs/syntax_and_builtins.md`

Do NOT show the agent any existing test cases or expected outputs.

Record: did the first attempt compile and produce correct output?

---

## A1 — Factorial (loop + implicit tail return)

**Task:** Write a Candor program with a `factorial` function that takes an `i64`
and returns `i64`. Use a loop. Then call `factorial(5)` and print the result.

**Expected output:**
```
120
```

**Pass criteria:** Compiles, prints exactly `120`.

---

## A2 — Safe divide (result type + must)

**Task:** Write a Candor function `safe_div(a: i64, b: i64) -> result<i64, str>`
that returns `err("div by zero")` if b is 0, otherwise `ok(a / b)`.
Call it with (10, 2) and (7, 0), print the result or error each time.

**Expected output:**
```
5
div by zero
```

---

## A3 — Shape enum (enum + match exhaustion)

**Task:** Define an enum `Shape` with two variants: `Circle(i64)` (radius) and
`Rect(i64, i64)` (width, height). Write a `describe` function that returns a
string like `"circle r=5"` or `"rect 3x4"`. Print both.

**Expected output:**
```
circle r=5
rect 3x4
```

---

## A4 — Vec sum (vec + for loop)

**Task:** Create a `vec<i64>`, push 10, 20, and 30 into it. Use a `for` loop
to sum all elements. Print the sum.

**Expected output:**
```
60
```

---

## A5 — Nested must (error propagation)

**Task:** Write two Candor functions:
- `inner(s: str) -> result<i64, str>`: returns `err("bad input")` if s equals `"bad"`, else `ok(42)`
- `outer(s: str) -> result<str, str>`: calls `inner(s)` using `must`, multiplies result by 2, returns `ok("result: " + str(n))`

Call `outer("bad")` and `outer("ok")`, print the result or error.

**Expected output:**
```
error: bad input
result: 84
```

---

## A6 — Option search

**Task:** Write a function `find(v: vec<i64>, target: i64) -> option<i64>` that
returns `some(index)` of the first match, or `none`. Test with target present
and not present. Print the index or "not found".

**Expected output:**
```
1
not found
```

---

## A7 — Recursive fibonacci

**Task:** Write a recursive `fib(n: i64) -> i64` function. Print `fib(10)`.

**Expected output:**
```
55
```

---

## A8 — Catchall arm ordering (Candor-specific)

**Task:** Write a function `label(n: i64) -> str` using a `match` expression.
The match should have arms for specific values (0 -> "zero", 1 -> "one") and
a wildcard arm `_ => "other"`. Call with 0, 1, and 5.

**Expected output:**
```
zero
one
other
```

**Note for evaluator:** The wildcard arm must be LAST. If the agent puts it first,
the program will either fail to compile (if Candorc catches it) or return "other"
for every input. This tests awareness of Candor's arm ordering rule.

---

## C1 — Candor-specific: wildcard-terminal ordering

**Task:** Write `first_even(v: vec<i64>) -> option<i64>` using `match` over
each element. Return `some(x)` for the first even number, `none` at end.

**Pass criteria:** Compiles and returns correct answer. Wildcard arm (`_`) must
be after the value arm, not before.

---

## Scoring Sheet

```
| Agent | Date | Language | Task | Attempt 1 Pass | Turns | Tokens | Notes |
|-------|------|----------|------|---------------|-------|--------|-------|
```
