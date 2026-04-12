# Candor Language Reference

**Compiler version:** v0.3.0
**Go module:** `github.com/candor-core/candor/compiler`
**Pipeline:** `.cnd` source → lex → parse → typeck → emit C → gcc/cc → binary

This is the primary reference for programmers writing Candor. It covers all syntax, semantics, types, operators, built-in functions, and known limitations of the current compiler.

---

## Table of Contents

1. [Compiler Invocation](#compiler-invocation)
2. [Program Structure](#program-structure)
3. [Types](#types)
4. [Declarations](#declarations)
5. [Statements](#statements)
6. [Expressions](#expressions)
7. [Effects System](#effects-system)
8. [Contracts](#contracts)
9. [Modules](#modules)
10. [Built-in Functions](#built-in-functions)
11. [Complete Examples](#complete-examples)
12. [Known Limitations](#known-limitations)

---

## Compiler Invocation

```bash
# Compile a single file
candorc program.cnd

# Compile a multi-file program
candorc main.cnd module1.cnd module2.cnd
```

The compiler writes a `.c` file alongside the first source file (for inspection), then invokes the system C compiler to produce a binary.

**Output binary naming:**
- Unix: `program` (no extension), placed next to the first source file
- Windows: `program.exe`

**Environment variables:**
- `CC` — override the C compiler used for the final link step (default: `gcc`, then `cc`)

---

## Program Structure

A Candor program is one or more `.cnd` source files. Every file is a sequence of top-level declarations: `fn` and `struct`. An optional `module` declaration and `use` imports may appear at the top of a file.

**Entry point:** The compiler looks for a function named `main` with signature `fn main() -> unit`.

**Comments:** Line comments begin with `//`.

```candor
// This is a comment
fn main() -> unit {
    print("hello")
    return unit
}
```

---

## Types

### Primitive Types

| Type | Description | C equivalent |
|---|---|---|
| `i8` | Signed 8-bit integer | `int8_t` |
| `i16` | Signed 16-bit integer | `int16_t` |
| `i32` | Signed 32-bit integer | `int32_t` |
| `i64` | Signed 64-bit integer | `int64_t` |
| `u8` | Unsigned 8-bit integer | `uint8_t` |
| `u16` | Unsigned 16-bit integer | `uint16_t` |
| `u32` | Unsigned 32-bit integer | `uint32_t` |
| `u64` | Unsigned 64-bit integer | `uint64_t` |
| `f32` | 32-bit IEEE 754 float | `float` |
| `f64` | 64-bit IEEE 754 float | `double` |
| `bool` | Boolean: `true` or `false` only | `bool` |
| `str` | Immutable string literal | `const char*` |
| `unit` | Zero-size type; return type of void functions | `void` (special) |

**`bool` never coerces to integer.** Using a `bool` where an integer is expected is a type error.

**`unit` is a real type.** Functions that perform side effects with no return value are declared `-> unit` and must `return unit`.

### Generic Types

| Type | Description |
|---|---|
| `option<T>` | A value that is either `some(v)` or `none`; null-safety without null |
| `result<T, E>` | A value that is either `ok(v)` or `err(e)`; errors as values |
| `vec<T>` | Growable array |
| `ring<T>` | Fixed-capacity circular buffer |
| `ref<T>` | Shared borrow |

### Function Types

Functions are first-class values. The type of a function is written `fn(T, U) -> R`.

```candor
let f: fn(i64) -> i64 = square
```

### Special Types

| Type | Description |
|---|---|
| `never` | The type of expressions that do not return (e.g., `return`, `break` used as values in `must` arms) |

---

## Declarations

### Function Declarations

```candor
fn name(param: Type, ...) -> ReturnType {
    // body
}
```

Function bodies must end with a `return` statement. The return type is required. Parameter types are required.

**Minimal example:**

```candor
fn add(a: u32, b: u32) -> u32 {
    return a + b
}
```

**Unit-returning function:**

```candor
fn greet(name: str) -> unit effects(io) {
    print("hello")
    return unit
}
```

**Pure function:**

```candor
fn pure_hash(x: i64) -> i64 pure {
    return x * 2654435761
}
```

**Function with contracts:**

```candor
fn divide(a: i64, b: i64) -> i64
    requires b != 0
    ensures result == a / b
{
    return a / b
}
```

Effects annotations are documented in the [Effects System](#effects-system) section. Contracts are documented in the [Contracts](#contracts) section.

### Struct Declarations

```candor
struct Name {
    field: Type,
    field: Type,
}
```

Trailing comma on the last field is allowed. Fields may be of any type including other struct types.

```candor
struct Point {
    x: f64,
    y: f64,
}

struct Rectangle {
    top_left: Point,
    width: f64,
    height: f64,
}
```

**Construction:** Use a struct literal with named fields.

```candor
let p = Point { x: 1.0, y: 2.0 }
```

All fields must be provided. There is no default initialization.

**Field access:**

```candor
let px = p.x
```

**Mutable field assignment:** The binding holding the struct must be declared `mut`.

```candor
let mut q = Point { x: 0.0, y: 0.0 }
q.x = 3.0
```

---

## Statements

Statements appear inside function bodies. They are executed in order.

### let — Variable Binding

```candor
let x: i64 = 42         // typed binding
let y = 42              // type inferred from initializer
let mut z: i64 = 0      // mutable binding
```

All bindings are immutable by default. Write `mut` to allow reassignment or field mutation on the bound value. The type annotation is optional when it can be inferred.

A `let` binding must have an initializer.

### return

```candor
return expression
return unit     // for functions declared -> unit
```

`return` exits the current function with the given value. The type of the returned expression must match the declared return type.

### if / else if / else

```candor
if condition {
    // ...
} else if other_condition {
    // ...
} else {
    // ...
}
```

The condition must be of type `bool`. There is no implicit truthiness for integers or other types. `else if` and `else` branches are optional.

### loop / break

```candor
loop {
    // ...
    if condition { break }
    // ...
}
```

`loop` runs indefinitely. `break` exits the enclosing loop. There is no `while` or `do-while` — use `loop` with an `if`/`break`.

### for ... in

```candor
for item in collection {
    // item is the element type of collection
}
```

Iterates over `vec<T>` or `ring<T>`. The loop variable `item` takes on each element in order. The loop variable is immutable.

### Assignment

Requires the binding to be declared `mut`:

```candor
let mut x = 0
x = x + 1
```

Struct field assignment also requires `mut` on the binding:

```candor
let mut p = Point { x: 0.0, y: 0.0 }
p.x = 1.5
```

### assert

```candor
assert expression
```

Evaluates `expression` (which must be `bool`) at runtime. If the expression is false, the program panics. Use for invariants and defensive checks.

---

## Expressions

### Literals

| Literal | Type | Example |
|---|---|---|
| Integer | Inferred (default `i64`) | `42`, `0`, `-1` |
| Float | Inferred (default `f64`) | `3.14`, `0.0`, `-1.5` |
| String | `str` | `"hello, world"` |
| Bool | `bool` | `true`, `false` |
| Unit | `unit` | `unit` |

### Arithmetic Operators

| Operator | Meaning |
|---|---|
| `a + b` | Addition |
| `a - b` | Subtraction |
| `a * b` | Multiplication |
| `a / b` | Division |
| `a % b` | Remainder |
| `-x` | Unary negation |

Both operands must be the same numeric type. There is no implicit numeric coercion.

### Comparison Operators

All comparison operators produce `bool`.

| Operator | Meaning |
|---|---|
| `a == b` | Equal |
| `a != b` | Not equal |
| `a < b` | Less than |
| `a > b` | Greater than |
| `a <= b` | Less than or equal |
| `a >= b` | Greater than or equal |

### Boolean Operators

| Operator | Meaning |
|---|---|
| `a and b` | Logical AND |
| `a or b` | Logical OR |
| `!a` | Logical NOT |
| `not a` | Logical NOT (alternate spelling) |

Operands must be `bool`.

### Function Calls

```candor
function_name(arg1, arg2, arg3)
```

Arguments are evaluated left to right. The number of arguments must match the function's parameter list exactly. Variadic functions are not supported.

### Field Access

```candor
value.field_name
```

Valid on struct values. Returns the value of the named field.

### Indexing

```candor
collection[index]
```

Valid on `vec<T>` and `ring<T>`. `index` must be an unsigned integer type. Returns the element at the given index. Out-of-bounds access is a runtime panic.

### Constructors for Generic Types

```candor
some(value)   // constructs option<T> — some variant
none          // constructs option<T> — none variant
ok(value)     // constructs result<T,E> — ok variant
err(error)    // constructs result<T,E> — err variant
```

The compiler infers the type parameter from context.

### must{} — Exhaustive Handling of option and result

`must{}` is **required** for any expression of type `option<T>` or `result<T,E>`. Failing to handle these types is a compile error.

```candor
let v = expression must {
    ok(val)  => value_expression
    err(e)   => value_expression
}

let x = expression must {
    some(v) => value_expression
    none    => value_expression
}
```

**Rules:**

- Both arms must be present and exhaustive.
- Both arms must produce the same type, or one arm may use `return` or `break` (which have type `never`).
- `return` in a `must` arm exits the enclosing function.
- `break` in a `must` arm exits the enclosing loop (only valid inside a `loop`).

```candor
// Using break in a must arm (inside a loop)
loop {
    let x = try_read_int() must {
        some(v) => v
        none    => break    // exits the loop when EOF
    }
    print_int(x)
}

// Using return in a must arm
fn process(r: result<i64, str>) -> i64 {
    let v = r must {
        ok(x)  => x
        err(_) => return -1    // exits the function
    }
    return v
}
```

### match{} — Pattern Matching

`match{}` performs pattern matching on a value. All arms must produce the same type.

```candor
let result_expr = match scrutinee {
    pattern => value_expression
    pattern => value_expression
    _       => value_expression    // wildcard: matches anything
}
```

**Supported pattern forms:**

| Scrutinee type | Pattern | Example |
|---|---|---|
| `bool` | `true`, `false` | `true => "yes"` |
| Integer | Integer literal | `0 => "zero"`, `1 => "one"` |
| Integer | Negative literal | `-1 => "negative"` |
| `str` | String literal | `"GET" => 0` |
| `option<T>` | `some(binding)` | `some(x) => x * 2` |
| `option<T>` | `none` | `none => 0` |
| `result<T,E>` | `ok(binding)` | `ok(x) => x` |
| `result<T,E>` | `err(binding)` | `err(e) => -1` |
| Any | `_` | `_ => 0` |

**Important:** When matching on negative integer literals, use a comma after the pattern to avoid parsing ambiguity:

```candor
let sign = match n {
    -1 => "negative",
    0  => "zero",
    _  => "positive"
}
```

**Nested match:**

```candor
let label = match a {
    true => match b {
        true  => "both"
        false => "a only"
    }
    false => match b {
        true  => "b only"
        false => "neither"
    }
}
```

### First-Class Functions

Functions can be passed as arguments, stored in variables, and called through function-type variables.

```candor
fn square(x: i64) -> i64 { return x * x }
fn apply(f: fn(i64) -> i64, x: i64) -> i64 { return f(x) }

let result = apply(square, 5)        // 25

let f: fn(i64) -> i64 = square
let y = f(10)                        // 100
```

The function type syntax is `fn(ParamType, ...) -> ReturnType`. The type must be written explicitly when storing a function in a `let` binding without a clear inferrable context.

### return and break as Expressions

Inside `must{}` arms, `return` and `break` are expressions of type `never`. This allows them to appear where a value of any type is expected, since `never` is a subtype of every type.

---

## Effects System

Effects annotations appear on function declarations between the return type and the opening brace.

### Annotation Forms

```candor
fn f() -> unit pure { ... }                  // pure — no side effects
fn g() -> unit effects(io) { ... }           // declares IO effects
fn h() -> unit effects(io, net) { ... }      // multiple effects
fn j() -> unit cap(SomeCap) { ... }          // capability-gated
// no annotation — unchecked (gradual adoption)
```

### Enforcement Rule

A function declared `pure` cannot call any function that carries `effects(...)`. The compiler checks this through the complete call graph. Calling an effectful function from a pure context is a compile error.

Functions with no annotation are unchecked — they may call anything. This supports gradual adoption: you can annotate part of a codebase without annotating all of it.

### Effect Labels

The current compiler recognizes `io` and `net` as effect labels for `effects(...)`. Capability names for `cap(...)` are user-defined identifiers.

---

## Contracts

Contracts are optional preconditions and postconditions on functions. They are written between the return type annotation and the opening brace.

```candor
fn function_name(params) -> ReturnType
    requires precondition_expression
    ensures  postcondition_expression
{
    // body
}
```

Multiple `requires` and `ensures` clauses are allowed.

### requires

`requires` specifies a precondition that must hold when the function is called. The expression has access to all parameters.

```candor
fn safe_div(a: i64, b: i64) -> i64
    requires b != 0
{
    return a / b
}
```

In the current compiler, `requires` clauses generate `assert()` calls at function entry in debug builds.

### ensures

`ensures` specifies a postcondition that must hold when the function returns. The expression has access to all parameters and to `result`, which refers to the return value.

```candor
fn non_negative(x: i64) -> i64
    requires x >= 0
    ensures result >= 0
{
    return x
}
```

In the current compiler, `ensures` clauses generate `assert()` calls before each `return` statement in debug builds.

### Combined example

```candor
fn divide(a: i64, b: i64) -> i64
    requires b != 0
    ensures result == a / b
{
    return a / b
}
```

---

## Modules

Multi-file programs use module declarations to organize code across files.

### module Declaration

A file may contain at most one `module` declaration, which must appear at the top of the file before any other declarations (except `use`).

```candor
module module_name
```

Files that share the same module name share a namespace. Functions and structs in a file without a module declaration are in the root namespace and are accessible everywhere without a `use` import.

### use Declaration

`use` imports a specific symbol from another module into the current file's scope.

```candor
use module_name::SymbolName
```

A symbol from module `foo` is only accessible to code that has `use foo::Name`. The compiler enforces this: referencing a symbol from another module without a corresponding `use` is a compile error.

### Example

```candor
// geometry.cnd
module geometry

struct Point {
    x: f64,
    y: f64,
}

fn distance(a: Point, b: Point) -> f64 {
    let dx = a.x - b.x
    let dy = a.y - b.y
    return dx * dx + dy * dy
}
```

```candor
// main.cnd
module app
use geometry::Point
use geometry::distance

fn main() -> unit {
    let p = Point { x: 0.0, y: 0.0 }
    let q = Point { x: 3.0, y: 4.0 }
    print_f64(distance(p, q))
    return unit
}
```

Compiled with: `candorc main.cnd geometry.cnd`

---

## Built-in Functions

These functions are always available without any import.

### Output

| Function | Signature | Description |
|---|---|---|
| `print` | `fn(str) -> unit` | Print string followed by newline |
| `print_int` | `fn(i64) -> unit` | Print signed integer followed by newline |
| `print_u32` | `fn(u32) -> unit` | Print unsigned 32-bit integer followed by newline |
| `print_bool` | `fn(bool) -> unit` | Print `true` or `false` followed by newline |
| `print_f64` | `fn(f64) -> unit` | Print float followed by newline |

All print functions carry `effects(io)`.

### Blocking Stdin Input

These functions block until a value is available on stdin. They do not handle EOF gracefully — use the `try_read_*` variants when EOF is possible.

| Function | Signature | Description |
|---|---|---|
| `read_line` | `fn() -> str` | Read one line, strip trailing newline |
| `read_int` | `fn() -> i64` | Read one integer from stdin |
| `read_f64` | `fn() -> f64` | Read one float from stdin |

All read functions carry `effects(io)`.

### EOF-Safe Stdin Input

These functions return `none` on EOF instead of panicking or blocking indefinitely.

| Function | Signature | Description |
|---|---|---|
| `try_read_line` | `fn() -> option<str>` | `some(s)` on success, `none` on EOF |
| `try_read_int` | `fn() -> option<i64>` | `some(n)` on success, `none` on EOF |
| `try_read_f64` | `fn() -> option<f64>` | `some(x)` on success, `none` on EOF |

All `try_read_*` functions carry `effects(io)`. Because they return `option<T>`, callers must handle them with `must{}`.

### Vec Operations

| Function | Signature | Description |
|---|---|---|
| `vec_new` | `fn() -> vec<T>` | Create an empty vec (requires explicit type annotation) |
| `vec_push` | `fn(vec<T>, T) -> unit` | Append an element to the end |
| `vec_len` | `fn(vec<T>) -> u64` | Return the number of elements |

Vec indexing uses bracket syntax: `v[i]` returns the element at index `i`. Index type must be an unsigned integer. Out-of-bounds access is a runtime panic.

`vec_new` requires an explicit type annotation on the `let` binding because the element type cannot be inferred from the call alone:

```candor
let mut data: vec<i64> = vec_new()
```

---

## Complete Examples

### Hello World

```candor
fn main() -> unit {
    print("hello, world")
    return unit
}
```

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

### Option handling

```candor
fn find_first(v: vec<i64>, target: i64) -> option<u64> {
    let mut i: u64 = 0
    loop {
        if i >= vec_len(v) { break }
        if v[i] == target { return some(i) }
        i = i + 1
    }
    return none
}

fn main() -> unit {
    let mut data: vec<i64> = vec_new()
    vec_push(data, 10)
    vec_push(data, 20)
    vec_push(data, 30)

    let pos = find_first(data, 20) must {
        some(i) => i
        none    => return unit
    }
    print_int(pos)    // prints 1
    return unit
}
```

### Result handling with effects and contracts

```candor
fn safe_div(a: i64, b: i64) -> result<i64, str>
    requires b != 0
{
    return ok(a / b)
}

fn main() -> unit effects(io) {
    let v = safe_div(10, 2) must {
        ok(x)  => x
        err(_) => return unit
    }
    print_int(v)    // prints 5
    return unit
}
```

### Higher-order functions

```candor
fn apply(f: fn(i64) -> i64, x: i64) -> i64 { return f(x) }
fn square(x: i64) -> i64 { return x * x }
fn double(x: i64) -> i64 { return x * 2 }

fn main() -> unit {
    print_int(apply(square, 5))    // 25
    print_int(apply(double, 5))    // 10
    let f: fn(i64) -> i64 = square
    print_int(f(7))                // 49
    return unit
}
```

### Multi-file program

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
    print_int(square(4))    // 16
    print_int(cube(3))      // 27
    return unit
}
```

```bash
candorc main.cnd math.cnd
```

---

## Known Limitations

The following features are not yet implemented in compiler v0.0.1.

**Types not yet supported:**
- `i128` / `u128` — 128-bit integers
- Lambdas and closures — anonymous function literals
- User-defined enums — only `option<T>` and `result<T,E>` variants exist
- `map<K,V>` and `set<T>` — associative and set collections
- String operations — concatenation, length, slicing, comparison beyond `==`

**Language features not yet supported:**
- Ownership transfers and `move` semantics
- `refmut<T>` — mutable borrows
- The `#use` layer declaration syntax
- Tags (`@[pure]`, `@[secret]`, etc.)
- `#intent` blocks
- `old()` in `ensures` clauses
- The `forall` quantifier in contracts

**Toolchain features not yet present:**
- A `.csi` semantic index file
- `candorc search`, `candorc audit`, `candorc test` subcommands
- A language server / LSP
- A syntax highlighter for any editor
- A standard library beyond the documented built-ins
- An allocator layer
- LLVM backend (planned for Phase 2)

---

*Candor Language Reference — compiler v0.0.1*
*Copyright © 2026 Scott W. Corley — Apache License 2.0*
