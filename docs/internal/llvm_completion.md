# LLVM IR Emitter — Completion Tracker

> Tracks gaps in `compiler/emit_llvm/emit_llvm.go` from the initial M5.1 implementation.
> Items are ordered by effort and dependency. Check off each item as it lands.

---

## Quick wins (no runtime dependency)

### [x] 1. Enum payload binding in match arms
**What:** Match arms on data-carrying enum variants need to extract payload fields.
**Current state:** `switch i32` dispatch works; arm body emits correctly for unit variants.
Missing: when a variant has fields (e.g. `Some(x)`), the payload bytes need to be
`extractvalue`d from the tagged union and bound to the pattern variable in the arm scope.
**IR pattern:**
```llvm
; variant Some(i64) is at tag 1, payload at index 1
%raw = extractvalue %OptionI64 %val, 1          ; [payload x i8] bytes
%slot = alloca i64
store [N x i8] %raw, ptr %slot                  ; or bitcast+load
%x = load i64, ptr %slot
```
**Test case:**
```candor
enum Opt { None, Some(i64) }
fn unwrap(o: Opt) -> i64 {
    match o {
        Opt::Some(x) => x,
        _ => 0,
    }
}
```

---

### [x] 2. Tuple destructuring (`let (a, b) = t`)
**What:** `DestructureTupleStmt` currently emits a comment stub.
**Current state:** `emitStmt` hits the `; TODO: tuple destructure on non-tuple` path.
**IR pattern:** tuples are already lowered to named struct types `%Tuple_T0_T1`.
```llvm
%a = alloca i64
%b = alloca bool
%v0 = extractvalue %Tuple_i64_bool %t, 0
%v1 = extractvalue %Tuple_i64_bool %t, 1
store i64 %v0, ptr %a
store i1 %v1, ptr %b
```
**Test case:**
```candor
fn swap(a: i64, b: i64) -> i64 {
    let (x, y): (i64, i64) = (b, a)
    return x
}
```

---

### [x] 3. Address-of on non-local expressions (`&expr`)
**What:** `emitAddr` falls back to `; TODO: & on non-local` and returns `undef` for
anything that isn't a plain `IdentExpr` (which has a known alloca).
**Current state:** ref/refmut params that point at sub-expressions or struct fields
don't get a valid pointer.
**Fix:** materialize the value into a fresh alloca, return its address.
```llvm
%tmp = alloca i64
store i64 %val, ptr %tmp
; return %tmp as the address
```
**Test case:**
```candor
extern fn inc(p: refmut<i64>) -> unit
fn bump(x: i64) -> i64 {
    inc(&x)
    return x
}
```

---

## Medium items (self-contained, no vec/map runtime)

### [x] 4. `for`-in over `vec<T>` and `ring<T>`
Implemented natively — `vec<T>` and `ring<T>` are now LLVM named struct types (no C runtime
needed for read-only iteration):
- `%_cnd_vec = type { ptr, i64, i64 }` (data, len, cap)
- `%_cnd_ring = type { ptr, i64, i64, i64 }` (data, cap, head, len)
Loop: hdr → body → incr → hdr, with `continue` → incr so the increment always runs.
`break` → exit, `continue` → incr label.
**Still TODO:** `for k, v in map` (map struct is complex hash table; deferred).

---

### [x] 5. Index-assign `v[i] = x` and index-read `v[i]`
`IndexAssignStmt` and `IndexExpr` both implemented for `vec<T>` and `ring<T>`:
- vec: GEP into `_data` pointer extracted from struct
- ring: `(head + i) % cap` modular index

---

## Large items (require runtime strategy decision)

### [x] 6. `vec<T>` / `ring<T>` runtime strategy — decided: native LLVM struct types
`vec<T>` → `%_cnd_vec = type { ptr, i64, i64 }`, `ring<T>` → `%_cnd_ring = type { ptr, i64, i64, i64 }`.
Read operations (index, for-in) work natively. Write operations that grow the backing
array (push, pop) still need a `malloc`/`realloc` strategy.

### [x] 7. `vec<T>` literals `[a, b, c]`
GEP-sizeof trick for correct allocation size, `@malloc` declared in header, elements stored
via indexed GEP, struct assembled via `insertvalue` chain.
Empty literal `[]` returns `zeroinitializer` directly (no allocation).

### [x] 8. `for k, v in map<K,V>`
`%_cnd_map = type { ptr, i64, i64 }` (buckets ptr, len, cap).
Per K,V pair, a named entry type `%_cnd_map_entry_K_V = type { K, V, ptr }` is lazily declared.
Nested loop: outer iterates `bi = 0..cap`; inner walks `_next` linked-list chain per bucket.
`continue` → `map.inner.incr` (next entry); `break` → outer `for.exit`.
Entry fields are loaded and `entryAddr` pre-advanced before the user body so `continue` is safe.

---

## Complex items

### [x] 9. Closures / lambdas
**IR strategy:** heap fat pointer `{ ptr fnptr, ptr env }`.
- **Env type** `%_cnd_lambda_N_env = type { T0, T1, ... }` declared in header; by-ref fields use `ptr`.
- **Impl function** `@_cnd_lambda_N_impl(params..., ptr %_env)`: top-level function; GEP-unpacks captures from env.
- **Callsite**: malloc env struct, store captures (by-value or address-of for by-ref), malloc 16-byte fat struct `{ ptr, ptr }`, store fnptr + env, return fat `ptr`.
- **Indirect call**: when callee is a local `FnType` variable, GEP into fat struct to extract fnptr and env, then `call retTy %fnptr(args..., ptr %env)`.

---

## Completed

- [x] All primitive types, arithmetic, comparison, bitwise ops
- [x] Control flow: if/else, while, loop, break, continue
- [x] Let bindings (alloca-based SSA)
- [x] Structs: `insertvalue`/`extractvalue`, field access, field assign
- [x] Enums: tagged union, unit variant construction, tag dispatch in match
- [x] String globals (`@.str.N`)
- [x] Extern fn declarations
- [x] Casts: trunc/sext/zext/sitofp/fptosi/fpext/fptrunc/fptoui/uitofp
- [x] Function calls (direct, method, generic instances)
- [x] `fn main()->unit` → `@_cnd_main` + `@main` i32 wrapper
- [x] Assert → `@llvm.trap` + `unreachable`
- [x] Tuple literals (emit as named struct type, field-by-field insertvalue)
- [x] Non-enum match (equality chain, wildcard)
- [x] Enum payload binding in match arms (GEP into tagged union payload bytes, per-field alloca)
- [x] Tuple destructuring `let (a, b) = t` (extractvalue per element, alloca per binding)
- [x] Address-of `&expr` on any lvalue (delegates to `emitAddr`, which materializes non-locals)
- [x] `vec<T>` / `ring<T>` as native LLVM struct types (`%_cnd_vec`, `%_cnd_ring`)
- [x] `for x in vec<T>` / `for x in ring<T>` — index loop with hdr/body/incr/exit blocks
- [x] `v[i]` index-read for vec and ring (extractvalue data ptr, GEP, load)
- [x] `v[i] = x` index-assign for vec and ring (GEP into alloca data ptr, store)
- [x] `vec<T>` literals `[a, b, c]` — `@malloc` + GEP-sizeof + element stores + `insertvalue` struct
- [x] `for k, v in map<K,V>` — `%_cnd_map` struct + per-KV entry type + nested outer/inner linked-list loop
- [x] Closures / lambdas — heap fat pointer `{ ptr fnptr, ptr env }`, `@_cnd_lambda_N_impl`, capture env struct, indirect call via GEP into fat struct

---

## Notes

- All locals use alloca/load/store (non-SSA); LLVM's `mem2reg` pass promotes to registers.
- Tuple types are named: `%Tuple_i64_bool = type { i64, i1 }`.
- Enum payload: `%E = type { i32, [N x i8] }` where N = max variant field size in bytes.
- Struct field order is taken from AST declaration order (not map iteration order).
