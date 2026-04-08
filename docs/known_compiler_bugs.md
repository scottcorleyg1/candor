# Known Compiler Bugs Catalog
**Last updated: 2026-04-08 09:02 MDT**

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
**Status: Not yet fixed in Go emitter**

**Symptoms:** Multiple `auto _t = ...` in the same C scope, then cascade of undeclared variables.

**Root cause:** Go emitter `emitMustOrMatch()` in `compiler/emit_c/emit_c.go` uses `e.freshTmp()` for the must subject (`_m`) but hardcodes `_t` for the outer let-binding of the must result. Two must-expressions in the same C scope both emit `auto _t = ...`.

**Fix:** Use `e.freshTmp()` for the outer binding around line 5458, or wrap each must result in an inner `{ }` C scope. Tracked in TASK-02.

**Note:** The Candor emitter (`emit_c.cnd`) avoids this — it emits must expressions as inline `__extension__` statement-expressions, not as `auto _t = ...` bindings.

---

## Bug 6 — `incomplete type '_CndMap_K_V'`
**First observed: ~2026-04-02**  
**Fixed by Gemini: 2026-04-04**

**Symptoms:** Map types fail during struct definition passes.

**Root cause:** Pass ordering dependency. Map entry body requires `V` fully defined, but `V` may need the map forward-declared.

**Fix:** Isolate map typedefs (Pass 2b) from map struct bodies (Pass 3d). See `docs/compiler_architecture.md` for authoritative pass order. Do not merge these passes.

---

## Bug 7 — stage3.exe segfault in collect_types_from_decl / collect_params_or
**First observed: 2026-04-08 ~09:01 MDT**  
**Status: Open — tracked in TASK-09**

**Symptoms:**
```
/d/tmp/stage3.exe src/compiler/lexer.cnd ... → Segmentation fault (exit 139)
```

**GDB backtrace:**
```
#0  strlen() — b=0xe (invalid pointer) in _cnd_str_concat
#1  empty_params_with_err(prefix="fn is_alpha: ", e=0xe)
#2  collect_params_or(...)
#3  fill_fn(fd=fn "is_alpha")
#4  pass2_decl / collect_signatures
#5  typecheck
#6  main
```

**Root cause hypothesis:** `collect_params_or` in `typeck.cnd` returns `vec<ParamSig>` via a tail `must` expression. The `emit_fn_body` void-suffix check incorrectly classifies this as void (Bug 4) and emits `(void)(expr)` instead of `return expr`. The function returns garbage from the stack, and the `_err_val` field contains `0xe` — which is then passed to `_cnd_str_concat` as a string.

**Candor source to investigate (`src/compiler/typeck.cnd`):**
```candor
fn collect_params_or(params: vec<Param>, prefix: str, env: refmut<TypeEnv>) -> vec<ParamSig> {
    collect_params(params, env) must {
        ok(ps) => ps
        err(e) => empty_params_with_err(prefix, e, env)
    }
}
```
Both arms return a value (`ps` and `empty_params_with_err(...)` both produce `vec<ParamSig>`). Neither is terminal. The must expression should NOT end with the void suffix — check why `emit_must_expr` is producing `((void)0);\n}))` here.

**File to edit:** `src/compiler/emit_c.cnd` — `emit_must_expr` and/or `emit_fn_body`
