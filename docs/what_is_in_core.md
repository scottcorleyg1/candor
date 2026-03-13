# What is in Candor Core (v0.1)

Candor is a modern, systems-oriented language designed for clarity, safety, and performance. This document outlines the core features, syntax, and built-ins available in the initial public release.

## 1. Type System

Candor features a strong, static type system with advanced ownership semantics.

### Primitive Types
- `bool`: Boolean (`true`, `false`)
- `i8`, `i16`, `i32`, `i64`, `i128`: Signed integers
- `u8`, `u16`, `u32`, `u64`, `u128`: Unsigned integers
- `f32`, `f64`: Floating-point numbers
- `str`: UTF-8 string (read-only reference)
- `unit`: The empty type (similar to `void` or `()`)

### Collection Types
- `vec<T>`: Contiguous dynamic array
- `map<K, V>`: Hash map (K must be hashable)
- `option<T>`: Nullable pointer (heap-allocated `T` or `none`)
- `result<T, E>`: Error-handling type (`ok(T)` or `err(E)`)

### Ownership & References
- `ref<T>`: Immutable heap-allocated reference (shared ownership)
- `refmut<T>`: Mutable reference (exclusive ownership)
- `secret<T>`: Type-level wrapper for sensitive data (requires explicit `reveal`)

## 2. Syntax & Control Flow

### Declarations
```candor
let x = 42;             // Type inference
let y: i64 = 100;       // Explicit annotation

fn double(n: i64) -> i64 {
    n * 2               // Implicit return
}
```

### Control Flow
- `if`/`else`: Standard conditional branches.
- `match`: Pattern matching for enums and value types.
- `loop`: Infinite loop.
- `break`: Exit loop.
- `return`: Explicit return from function.

### Iteration
```candor
for x in my_vec { ... }
for k, v in my_map { ... }
```

## 3. Data Structures

### Structs
```candor
struct Point {
    x: f64,
    y: f64,
}
let p = Point { x: 1.0, y: 2.0 };
```

### Enums (Sum Types)
```candor
enum Shape {
    Circle(f64),
    Rect(f64, f64),
    Point,
}
```

## 4. Built-in Functions

### Printing
- `print(str)`: Print string with newline.
- `print_int(i64)`, `print_f64(f64)`, `print_bool(bool)`

### String Operations
- `str_concat(str, str) -> str`
- `str_len(str) -> i64`
- `str_eq(str, str) -> bool`
- `str_substr(str, start, len) -> str`
- `int_to_str(i64) -> str`

### I/O & Files
- `read_line() -> str`
- `read_file(path) -> result<str, str>`
- `write_file(path, data) -> result<unit, str>`

### Collection Built-ins
- `vec_new<T>()`, `vec_push(v, val)`, `vec_len(v)`, `vec_pop(v)`
- `map_new<K, V>()`, `map_insert(m, k, v)`, `map_get(m, k) -> option<V>`

## 5. Formal Verification (Contracts)

Candor supports basic contract-based reasoning:
```candor
fn divide(a: f64, b: f64) -> f64 
    requires b != 0.0
    ensures result != 0.0 // if a != 0
{
    a / b
}
```
Currently, these are compiled to runtime assertions.

## 6. Implementation Notes
- **C Emitter**: Candor compiles to readable C11 code.
- **Bootstrapped**: The Candor typechecker is written in Candor and compiles itself.
