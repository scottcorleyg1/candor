# Known Compiler Bugs Catalog
**Last updated: 2026-04-08 21:50 MDT**

When GCC throws something strange, check here before debugging from scratch.

---

## Bug 1 — `_cnd_runtime.h` map macros get stripped by VSCode
**First observed: 2026-04-07 ~20:00 MDT**  
**Confirmed recurring: 2026-04-08 ~08:30 MDT**

**Symptoms:** `_cnd_map_insert undeclared`, `_cnd_map_get undeclared`, `_CndRes_int64_t_const_charptr undeclared` on the next compile after opening the file in VSCode.

**Root cause:** VSCode or a clang-format hook silently removes content appended past the original EOF of `_cnd_runtime.h` on save. The map macros and `_CndRes_int64_t_const_charptr` typedef are not committed — they exist only until the next save event.

**Workaround:** Re-run Step 4 from `docs/AI_GUIDE.md` before every `gcc` compile of stage2.c. The full `cat >>` block is there verbatim.

**Permanent fix (not yet done):** Commit the macros into `_cnd_runtime.h` and configure or disable the linter rule. Tracked in TASK-05.

---

## Bug 2 — Hundreds of "type defaults to int" errors
**First observed: 2026-04-08 ~08:45 MDT**

**Symptoms:** Every `auto x = ...` line produces `error: type defaults to 'int' in declaration of 'x'`.

**Root cause:** `auto` for type deduction is a C23 feature. Without `-std=gnu23`, GCC treats `auto` as the old C storage class specifier.

**Fix:** Always compile with `-std=gnu23`:
```bash
gcc.exe -std=gnu23 -O0 -o stage3.exe stage2.c -I src/compiler -lm
```

---

## Bug 3 — GCC silently exits code 1, no error output
**First observed: 2026-04-08 ~08:50 MDT**

**Symptoms:** GCC returns exit code 1 but stderr is empty. `as.exe` or `ld.exe` not found.

**Root cause:** GCC needs its own `bin/` in PATH to find the assembler and linker. The shell PATH does not include `/c/msys64/mingw64/bin` by default.

**Fix:**
```bash
PATH="/c/msys64/mingw64/bin:$PATH" /c/msys64/mingw64/bin/gcc.exe ...
```

---

## Bug 4 — `void value not ignored as it ought to be` (8 errors in emit_c.cnd functions)
**First observed: 2026-04-08 ~09:00 MDT**  
**Fixed: 2026-04-08 ~09:00 MDT**

**Symptoms:** Functions like `type_to_vec_struct_only`, `emit_terminal_body_str`, `emit_else_branch` produce this error at the `return (...)` line.

**Root cause:** The implicit tail return logic in `emit_fn_body` wraps the last expression in `return expr;`. But if that expression is an all-terminal match/must block, it ends with the void suffix `((void)0);\n}))` — meaning the block has type void/never, not the function's declared return type.

**Void suffix signature:** A match or must block where every arm is terminal (return/break/continue) ends with exactly: `((void)0);\n}))` 

**Fix (in `src/compiler/emit_c.cnd`, `emit_fn_body`):**
Check `str_substr(e_str, e_len - vs_len, vs_len)` against `"((void)0);\n}))"`. If it matches, emit `(void)(expr);` instead of `return expr;`.

---

## Bug 5 — `redefinition of '_t'` + cascading `_m undeclared`, `v undeclared`
**First documented by Gemini: 2026-04-04**  
**Status: Fixed — verified 2026-04-10**

**Symptoms (historical):** Multiple `auto _t = ...` in the same C scope, then cascade of undeclared variables.

**Root cause (historical):** Go emitter `emitMustOrMatch()` used `e.freshTmp()` for the must subject but hardcoded `_t` for the outer binding. Two must-expressions in the same C scope both emitted `auto _t = ...`.

**Fix:** `emitMustOrMatch()` now calls `e.freshTmp()` for both `tmp` (the subject) and `res` (the result binding). Confirmed: two `must` expressions in the same function emit distinct `_cnd1`, `_cnd2`, `_cnd3`, `_cnd4` with zero redefinition conflicts.

---

## Bug 6 — `incomplete type '_CndMap_K_V'`
**First observed: ~2026-04-02**  
**Fixed by Gemini: 2026-04-04**

**Symptoms:** Map types fail during struct definition passes.

**Root cause:** Pass ordering dependency. Map entry body requires `V` fully defined, but `V` may need the map forward-declared.

**Fix:** Isolate map typedefs (Pass 2b) from map struct bodies (Pass 3d). See `docs/compiler_architecture.md` for authoritative pass order. Do not merge these passes.

---

## Bug 7 — stage3.exe segfault: corrupted Decl tags in merged ParsedFile
**First observed: 2026-04-08 ~09:01 MDT**  
**Fixed: 2026-04-08 ~21:30 MDT**

**Symptoms:**
```
/d/tmp/stage3.exe src/compiler/lexer.cnd ... → Segmentation fault (exit 139)
GDB: collect_types_from_decl — im.methods._data = 0x1 (invalid pointer)
     d._tag = 6 (ImplD) for a decl whose pointer is actually an FnDecl ("is_alpha")
     Pattern: alternating tags 0,6,0,6,... across the merged decls vec
```

**Root cause — `arm_is_terminal_blk` was too permissive:**
`arm_is_terminal_blk` in `emit_c.cnd` returned `true` for ANY `BlkExpr` with `final_expr=none`. This includes blocks ending in side-effect statements like `vec_push(decls, d)`.

In `merge_files` in `main.cnd`, the FnD/StructD/EnumD match arms are blocks with no final expression — they do work (`vec_push`) and produce no value. With the permissive check, `emit_match_expr` classified them as terminal and emitted them as `if` blocks. But the unconditional ternary fallthrough (the ImplD default arm) still executed unconditionally after every arm, pushing each decl twice — once as FnD and once as ImplD.

**Fix (`src/compiler/emit_c.cnd` — `arm_is_terminal_blk`):**
A block is terminal only if its last statement is `ReturnS`, `BreakS`, `ContinueS`, or an `ExprS` whose expression is itself terminal (recursing through `arm_is_terminal`). Added helpers `match_all_arms_terminal(arms: vec<MatchArm>)` and `must_all_arms_terminal(arms: vec<MustArm>)`, and extended `arm_is_terminal` to recurse into `Expr::Match` and `Expr::Must`.

---

## Bug 8 — `emit_block_expr` discarded implicit tail expressions as void stmts
**First observed: 2026-04-08 ~21:00 MDT**  
**Fixed: 2026-04-08 ~21:30 MDT**

**Symptoms:**
```
D:/tmp/stage2.c:6956:28: error: void value not ignored as it ought to be
```
5 functions (`fill_variant_payload`, `fill_struct_field`, `arm_bindings_enum`, etc.) had `return (void_stmt_expr)` at the C level.

**Root cause:**
`emit_block_expr` only used `final_expr` (the explicit trailing expression) as the block value. But the Candor parser always sets `final_expr=none` — every expression in a block body is stored as `Stmt::ExprS`. The last `ExprS` in a block IS the implicit return value, but `emit_block_expr` was emitting it as `(void)(expr)` like any other statement.

Example arm body: `{ env_error(env, msg);  ty_unknown() }` — parsed as two `ExprS` stmts, `final_expr=none`. `ty_unknown()` was emitted `(void)(ty_unknown())`, making the block void-typed.

**Fix (`src/compiler/emit_c.cnd` — `emit_block_expr`):**
When `final_expr=none`, peel the last stmt if it is an `ExprS` and treat it as the block's value expression. Emit stmts `0..n-2` as statements; emit the peeled ExprS as the trailing value inside the `__extension__` stmt-expr.

---

## Bug 9 — Remaining void-suffix divergence in stage4.c (~150 lines, 1 GCC error)
**First observed: 2026-04-08 ~21:45 MDT**  
**Fixed: 2026-04-09 — TASK-10 closed. See Bug 10.**

Root cause was `stmt_to_expr` always returning NULL. See Bug 10 for full analysis.

---

## Bug 10 — `stmt_to_expr` always returned NULL: catchall terminal arm shadowed value arm
**First observed: 2026-04-09 ~00:00 MDT**  
**Fixed: 2026-04-09 — M9.19 achieved**

**Root cause:**
`emit_match_expr` emits terminal arms as `if (cond) { ... }` blocks BEFORE the ternary for value arms. The `_ => return none` arm in `stmt_to_expr` has `arm_cond = "1 /*bind*/"` (always-true), so its if-block `if (1) { return NULL; }` fires unconditionally before the ExprS value arm is ever evaluated. Result: `stmt_to_expr` always returned NULL in stage3, so `emit_fn_body` never took the `some(e_node)` branch — all function tail expressions were emitted via `emit_expr_stmt` as `(void)(expr)` instead of `return expr`.

**Pattern that triggers the bug:**
```candor
match s {
    Stmt::ExprS(es) => some(box_deref(es))  // value arm
    _               => return none           // catchall terminal (always-true cond)
}
```

Emitted in stage2.c as: `if (1 /*bind*/) { return NULL; }  /* ExprS arm dead */`

**Fix (`src/compiler/emit_c.cnd`):**
1. Added `arm_cond_is_catchall(pat)` helper — returns true when `arm_cond` produces `"1 /*bind*/"` or `"1 /*default*/"`.
2. In `emit_match_expr` and `emit_must_expr`: changed `any_terminal` detection to exclude catchall terminal arms. Catchall terminals are handled separately as the final else of the ternary value chain (wrapped in `(__extension__({ terminal_body; (void*)0; }))`).
3. `(void*)0` is used as the dummy value (not `((void)0)`) to avoid ternary type mismatch — all catchall terminal arms in Candor source return pointer types.

**Impact:** Fixed all ~75 functions whose bodies were pure-value match/must expressions. `stmt_to_expr` now correctly returns `some(es)` for ExprS stmts. `emit_fn_body` correctly emits `return expr;` for all function tails. stage4.c == stage2.c (0 diff lines). M9.19 — full bootstrap idempotency achieved.

---

## Bug 11 — `match` on integer literals silently produces wrong output
**First observed: 2026-04-09 (agent eval A8)**
**Status: Fixed: 2026-04-09**

**Symptoms:**
```candor
match n {
    0 => "zero"
    1 => "one"
    _ => "other"
}
```
Every input returns "zero" (the first arm). No compile error.

**Root cause:**
`arm_cond` in `emit_c.cnd` has no case for `Expr::Int`. Integer literal arms fall through to `_ => "1 /*default*/"` — an always-true condition. Every arm fires for every input. The `_` wildcard arm at the end is unreachable dead code.

**Workaround:**
Use `if`/`else if` chains for integer switching:
```candor
let mut result: str = "other"
if n == 0 { result = "zero" }
if n == 1 { result = "one" }
result
```

**Fix:** Added `Expr::Int(s) => str_concat("_m == ", s)` to `arm_cond` in `emit_c.cnd`.

---

## Bug 12 — `for x in v` emits `_cnd_vec_len`/`_cnd_vec_get` not defined for single-file programs
**First observed: 2026-04-09 (agent eval A4)**
**Status: Fixed: 2026-04-09**

**Symptoms:**
```
error: implicit declaration of function '_cnd_vec_len'
error: implicit declaration of function '_cnd_vec_get'
```
Only affects single-file programs. Multi-file compiler compilation is unaffected because `vec_len` is a builtin that emits inline.

**Root cause:**
The `for x in v` loop emitter generates calls to `_cnd_vec_len(v)` and `_cnd_vec_get(v, i)` helper functions. These are not in `_cnd_runtime.h` and are not emitted as inline code for single-file programs.

**Workaround:**
Use an explicit loop with index:
```candor
let n = vec_len(v) as i64
let mut i: i64 = 0
loop {
    if i >= n { break }
    let x = v[i as u64]
    ...
    i = i + 1
}
```

**Fix:** Changed `emit_for_stmt` in `emit_c.cnd` to emit inline index access `_iter._len` and `_iter._data[_i]` instead of helper calls.
