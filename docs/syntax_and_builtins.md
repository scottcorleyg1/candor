# Candor Syntax and Builtins Cheat Sheet
**Last updated: 2026-04-08**

Dense reference for AI agents writing or modifying Candor code. No implicit magic.

---

## 1. Functions

```candor
fn name(a: i64, b: str) -> result<i64, str> {
    return ok(a * 2)
}

fn void_fn(x: i64) -> unit {
    print(_cnd_int_to_str(x))
    return unit
}

fn main() -> unit {
    ## main() emits as: int main(int argc, char** argv)
    ## _cnd_argc / _cnd_argv are set automatically
}
```

- Return type is mandatory. `unit` = void.
- `return unit` required at end of unit functions (or rely on implicit tail return).
- Last expression in a non-unit function is an implicit `return` — no explicit keyword needed.

---

## 2. Variables and Mutation

```candor
let x: i64 = 5          ## immutable
let mut y: i64 = 0      ## mutable
y = y + 1

let mut s: str = "hello"
if condition { s = "world" }   ## assign inside branch, not let = if (not supported)
```

**Important:** `let name = if cond { val } else { val }` is **NOT supported**. Use:
```candor
let mut name: T = default_val
if cond { name = val }
```

---

## 3. Control Flow

```candor
loop { if i >= 10 { break }; i = i + 1 }
while cond { }
for x in my_vec { }

match my_opt {
    some(val) => val
    none      => 0
}

let val = my_func() must {
    ok(v)  => v
    err(e) => return err(e)
}
```

- All match arms must return the same type.
- `must` is mandatory for `result<T,E>` and `option<T>` — ignoring them is a compile error.
- `break` and `continue` are terminal arms in match/must (no value).
- **`match` is for structural patterns only** (enum variants, `some`/`none`, `ok`/`err`, bool).
  Integer literal arms silently produce wrong output — use `if`/`else if` for integer switching.
- **`for x in v` is broken for single-file programs** — use an explicit `loop` with index instead.
  See `docs/known_compiler_bugs.md` Bugs 11 and 12.

---

## 4. Types

| Candor | C output | Notes |
|--------|----------|-------|
| `i64` | `int64_t` | default integer |
| `u64` | `uint64_t` | |
| `u8` | `uint8_t` | used for str_byte return |
| `bool` | `int` | |
| `str` | `const char*` | immutable, no indexing with `[]` |
| `unit` | `void` | |
| `vec<T>` | `_CndVec_T` (struct) | use builtins, not `.push()` |
| `map<str,V>` | `_CndMap_const_charptr_V` | str keys only currently |
| `option<T>` | `T*` (NULL = none) | |
| `result<T,E>` | `_CndRes_T_E` struct | `._ok`, `._ok_val`, `._err_val` |
| `box<T>` | `T*` | heap allocation |
| `ref<T>` | `T*` (read-only) | |
| `refmut<T>` | `T*` (mutable) | pass `refmut(x)` or `&x` |

---

## 5. Builtins — Complete List (as of 2026-04-08)

### Strings
```candor
str_len(s: str) -> i64
str_concat(a: str, b: str) -> str
str_eq(a: str, b: str) -> bool
str_substr(s: str, start: i64, len: i64) -> str
str_byte(s: str, i: i64) -> u8          ## ONLY way to index into str — NOT s[i]
str_starts_with(s: str, prefix: str) -> bool
str_find(s: str, needle: str, start: i64) -> option<i64>
str_split(s: str, sep: str) -> vec<str>
str_trim(s: str) -> str
str_replace(s: str, from: str, to: str) -> str
str_to_upper(s: str) -> str
str_to_lower(s: str) -> str
str_repeat(s: str, n: i64) -> str
str_from_u8(b: u8) -> str
```

### Integers / Conversion
```candor
_cnd_int_to_str(n: i64) -> str         ## NOT int_to_str — that doesn't exist
```

### Vectors (`vec<T>`)
```candor
vec_new() -> vec<T>                     ## returns {0} in C
vec_push(v: vec<T>, val: T)            ## Mutates v. Pass raw variable natively, macro extracts pointer automatically!
vec_pop(v: vec<T>) -> unit              ## pops last element in-place; return value is discarded
vec_len(v: vec<T>) -> u64              ## returns u64, cast to i64 for arithmetic
vec_drop(v: vec<T>)                    ## Manual cleanup: frees the internal data buffer to stop memory leaks
v[i as u64]                            ## index — i must be u64
```

### Maps (`map<str, V>`)
```candor
map_new() -> map<str, V>
map_insert(m: map<str,V>, k: str, v: V) ## Mutates m. Pass raw variable natively.
map_get(m: map<str,V>, k: str) -> option<V>
map_contains(m: map<str,V>, k: str) -> bool
map_drop(m: map<str,V>)                 ## Manual cleanup: frees bucket arrays and string nodes
```

### Box / Heap
```candor
box_new(val: T) -> box<T>
box_deref(b: box<T>) -> T
box_drop(b: box<T>)                     ## Manual cleanup: frees the heap pointer
```

### I/O
```candor
print(s: str)
println(s: str)
print_err(s: str)
read_file(path: str) -> option<str>
write_file(path: str, data: str) -> result<unit, str>
append_file(path: str, data: str) -> result<unit, str>
try_read_line() -> option<str>
read_line() -> str
flush_stdout()
```

### OS / System
```candor
os_exec(args: vec<str>) -> result<i64, str>
os_args() -> vec<str>                   ## reads _cnd_argc/_cnd_argv — requires int main()
os_getenv(name: str) -> option<str>
os_cwd() -> str
os_mkdir(path: str) -> result<unit, str>
path_join(a: str, b: str) -> str
path_dir(p: str) -> str
path_filename(p: str) -> str
path_ext(p: str) -> str
path_exists(p: str) -> bool
```

### Time / Random
```candor
time_now_ms() -> i64
time_now_mono_ns() -> i64
time_sleep_ms(ms: i64)
rand_u64() -> u64
rand_f64() -> f64
rand_set_seed(seed: u64)
```

---

## 6. Common Pitfalls

| Wrong | Right | Why |
|-------|-------|-----|
| `s[i]` | `str_byte(s, i)` | str is `const char*`, not indexable |
| `vec_len(v)` used as i64 | `vec_len(v) as i64` | returns u64 |
| `int_to_str(n)` | `_cnd_int_to_str(n)` | different name |
| `let x = if cond { a } else { b }` | `let mut x = b; if cond { x = a }` | if-as-value not supported |
| `Enum::Variant` in expression | `Enum__Variant()` in C (auto-called) | zero-arg constructor |
| `Enum::Variant(a,b)` as callee | `Enum__Variant` without extra `()` | callee position special case |
