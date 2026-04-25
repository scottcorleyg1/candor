# Candor Language Reference for AI Code Generation

This document is the complete working knowledge needed to write correct Candor programs.
It represents what a model trained on Candor would know — equivalent to what a model
already knows about Go, Rust, or C from training data.

---

## 1. File Structure

A Candor program is one or more `.cnd` files. No imports, no modules in single-file
programs. Declarations can appear in any order.

```candor
struct Foo { x: i64, y: str }

fn main() -> unit effects(io, sys) {   ## sys required if calling os_args()
    print("hello")
    return unit
}
```

---

## 2. Types

| Type | Description |
|------|-------------|
| `i64` | 64-bit signed integer |
| `u64` | 64-bit unsigned integer |
| `u8` | 8-bit unsigned (byte) |
| `f64` | 64-bit float |
| `bool` | `true` / `false` |
| `str` | Immutable UTF-8 string |
| `unit` | Zero-size void type (not void — it is a real value) |
| `vec<T>` | Growable array |
| `map<K,V>` | Hash map (key must be `str` or integer) |
| `option<T>` | `some(value)` or `none` |
| `result<T,E>` | `ok(value)` or `err(error)` |
| `box<T>` | Heap-allocated owned pointer |

---

## 3. Functions

```candor
## Pure function — compiler enforces: cannot call I/O, no side effects
fn add(a: i64, b: i64) -> i64 pure {
    a + b
}

## Effectful function — must declare effects
fn read_config(path: str) -> result<str, str> effects(io) {
    read_file(path)
}

## Multiple effects
fn run(path: str) -> unit effects(io, sys) {
    ...
    return unit
}
```

Rules:
- `pure` functions may not call `print`, `read_file`, `write_file`, or any `effects(...)` function
- `effects(io)` propagates: a caller of an `effects(io)` function must also declare `effects(io)`
- The last expression in a function body is the implicit return value (no `return` needed for tail position)
- `return unit` is required to return early from a `-> unit` function

---

## 4. Variables and Mutation

```candor
let x: i64 = 42          ## immutable
let mut count: i64 = 0   ## mutable
count = count + 1

let name = "Alice"        ## type inferred
```

---

## 5. Structs

```candor
struct Stats {
    lines: i64
    errors: i64
    name: str
}

## Construction
let s = Stats{ lines: 10, errors: 2, name: "app.log" }

## Field access
let n = s.lines
```

---

## 6. Control Flow

```candor
## if / else
if x > 0 {
    print("positive")
} else {
    print("non-positive")
}

## loop with break
let mut i: i64 = 0
loop {
    if i >= 10 { break }
    i = i + 1
}

## while
while i < n {
    i = i + 1
}

## for-in over vec
for item in items {
    print(item)
}
```

---

## 7. Error Handling — `result<T,E>` and `option<T>`

### must{} — mandatory handling

```candor
## result<T,E>: both arms required
let content = read_file(path) must {
    ok(s)  => s
    err(e) => return err(str_concat("cannot read: ", e))
}

## option<T>: both arms required
let idx = str_find(line, "[", 0) must {
    some(i) => i
    none    => return none
}
```

### match — pattern matching on result/option

```candor
match run_pipeline(path) {
    ok(s)  => print(str_concat("count: ", int_to_str(s.count)))
    err(e) => print(str_concat("error: ", e))
}
```

### Constructing result/option values

```candor
return ok(my_value)
return err("something went wrong")
return some(x)
return none
```

---

## 8. Vectors

```candor
let mut v: vec<str> = vec_new()
vec_push(v, "hello")
vec_push(v, "world")

let n = vec_len(v) as i64       ## vec_len returns i64
let first = v[0 as u64]         ## index must be u64
```

**Important**: loop over a vec by index:
```candor
let n = vec_len(items) as i64
let mut i: i64 = 0
loop {
    if i >= n { break }
    let item = items[i as u64]
    ## use item
    i = i + 1
}
```

Or use `for-in`:
```candor
for item in items {
    ## use item
}
```

---

## 9. Maps

```candor
let mut counts: map<str, i64> = map_new()
map_insert(counts, "ERROR", 0)

let val = map_get(counts, "ERROR") must {
    some(v) => v
    none    => 0
}
map_insert(counts, "ERROR", val + 1)

let exists = map_contains(counts, "WARN")
```

---

## 10. String Builtins

```candor
str_len(s)                    ## i64 — length in bytes
str_byte(s, i)                ## u8  — byte at position i (i is i64)
str_concat(a, b)              ## str — concatenate two strings
str_substr(s, start, len)     ## str — substring (start and len are i64)
str_eq(a, b)                  ## bool — string equality
str_find(s, sub, start)       ## option<i64> — first occurrence of sub at/after start
str_starts_with(s, prefix)    ## bool
str_trim(s)                   ## str — strip leading/trailing whitespace
str_split(s, delim)           ## vec<str> — split by delimiter string
str_from_u8(b)                ## str — single byte as string
int_to_str(n)                 ## str — integer to decimal string
```

**Character comparisons** — Candor has no char type; use byte values:
```candor
if str_byte(s, 0) == 91 {   ## 91 = '[', 93 = ']', 10 = '\n', 32 = ' '
    ...
}
```

Common ASCII values: `[`=91, `]`=93, `\n`=10, `\r`=13, `\t`=9, ` `=32,
`0`=48..`9`=57, `A`=65..`Z`=90, `a`=97..`z`=122, `/`=47, `.`=46, `,`=44

---

## 11. I/O Builtins

```candor
print(s)                              ## unit — print string + newline
read_file(path)                       ## result<str, str> — read entire file
write_file(path, data)                ## result<unit, str> — write entire file
os_args()                             ## vec<str> — command-line args (index 0 = program name)
```

**Critical**: `read_file` and `write_file` return `result<T,E>` but the result struct
typedef is only generated when a user-defined function explicitly declares that return
type. Always write wrapper functions:

```candor
fn read_log(path: str) -> result<str, str> effects(io) {
    read_file(path)
}

fn write_output(path: str, data: str) -> result<unit, str> effects(io) {
    write_file(path, data)
}
```

Then call the wrappers, not `read_file`/`write_file` directly in `must{}` expressions.

---

## 12. Main Function and Args

```candor
fn main() -> unit effects(io, sys) {
    let args = os_args()           ## os_args() requires effects(sys)
    let n = vec_len(args) as i64
    if n < 2 {
        print("usage: program <file1> [file2 ...]")
        return unit
    }
    ## args[0] is the program name; args[1] is the first real argument
    let path = args[1 as u64]
    ...
    return unit
}
```

---

## 13. Enums

```candor
enum Level {
    Error
    Warn
    Info
    Debug
    Unknown
}

## Pattern match on enum
match lvl {
    Level::Error => print("error")
    Level::Warn  => print("warn")
    _            => print("other")
}
```

---

## 14. Complete Idiom: Parse + Validate + I/O

```candor
struct ParsedLine {
    level: str
    message: str
}

## Pure: no I/O, compiler-enforced
fn parse_line(line: str) -> option<ParsedLine> pure {
    if str_len(line) == 0 { return none }
    if str_byte(line, 0) != 91 { return none }  ## must start with '['
    let close = str_find(line, "]", 1) must {
        some(i) => i
        none    => return none
    }
    let level = str_substr(line, 1, close - 1)
    let msg   = str_trim(str_substr(line, close + 1, str_len(line) - close - 1))
    some(ParsedLine{ level: level, message: msg })
}

fn read_log(path: str) -> result<str, str> effects(io) { read_file(path) }

fn main() -> unit effects(io) {
    let args = os_args()
    let path = args[1 as u64]
    let raw = read_log(path) must {
        ok(s)  => s
        err(e) => { print(str_concat("error: ", e))  return unit }
    }
    let lines = str_split(raw, "\n")
    let n = vec_len(lines) as i64
    let mut i: i64 = 0
    loop {
        if i >= n { break }
        let line = str_trim(lines[i as u64])
        match parse_line(line) {
            some(p) => print(str_concat(p.level, str_concat(": ", p.message)))
            none    => unit
        }
        i = i + 1
    }
    return unit
}
```

---

## 15. Path Basename (Filename Extraction)

Candor has no path library. Extract the filename by scanning for both `/` (47) and `\` (92):

```candor
fn path_basename(path: str) -> str pure {
    let n = str_len(path)
    let mut last_sep: i64 = -1
    let mut i: i64 = 0
    loop {
        if i >= n { break }
        let c = str_byte(path, i) as i64
        if c == 47 or c == 92 { last_sep = i }
        i = i + 1
    }
    if last_sep < 0 { return path }
    str_substr(path, last_sep + 1, n - last_sep - 1)
}
```

---

## 16. Idiomatic Candor — Always Declare `pure`

In Candor, `pure` is not optional style — it is the default expectation for any function
that performs no I/O. A function without `pure` or `effects(...)` is treated as
unknown-purity by the compiler, which is a code smell.

**Rule**: If a function does not call `print`, `read_file`, `write_file`, `os_args`, or
any `effects(...)` function, it MUST be declared `pure`. The compiler will enforce this
in both directions: a `pure` function that calls I/O is a compile error.

```candor
## CORRECT — parser is pure: takes str, returns result, no I/O
fn parse_line(line: str) -> option<ParsedLine> pure { ... }

## CORRECT — aggregator is pure: takes data, returns data, no I/O
fn compute_stats(lines: vec<str>) -> Stats pure { ... }

## CORRECT — only the I/O boundary declares effects
fn process_file(path: str) -> result<Stats, str> effects(io) { ... }

## WRONG — missing pure on a function that has no side effects
fn parse_line(line: str) -> option<ParsedLine> { ... }  ## should be pure
```

This is the core token density claim: an AI reading `parse_line(...) pure` knows
immediately — without reading the body — that this function is safe to call from
any context. In Go/Rust/C, the AI must read the body to determine the same thing.

---

## 17. Reserved Variable Names

Do not use these as variable names — they conflict with the C backend's generated code:
`argc`, `argv`, `result`, `err`, `ok`, `unit`

Use `n_args`, `arg_list`, `res`, `e`, `v` instead.

---

## 17. What Candor Does NOT Have

- No `import` or `use` in single-file programs
- No `null` / `nil` — use `option<T>`
- No exceptions — use `result<T,E>`
- No semicolons between statements — use newlines
- No `for (i = 0; i < n; i++)` — use `loop` with break or `for x in vec`
- No single-quoted char literals — use `str_byte` and integer comparisons
- No `++` / `--` — use `i = i + 1`
- No `!=` for strings — use `not str_eq(a, b)`
- No string interpolation — use `str_concat`
- No `printf` / `fmt.Sprintf` — use `str_concat` + `int_to_str`
- No `os.ReadDir` / directory listing — pass file paths as arguments
