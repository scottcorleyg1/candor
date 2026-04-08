# Agents_Collab.md
## Multi-Agent Coordination — Claude + Gemini
### Scott resolves conflicts, sets priority, owns `> Scott:` blocks
**Last updated: 2026-04-08 09:02 MDT**

> **How this file works:** Open tasks have a status, owner, and timestamp to the minute. Either agent adds a `> Remark:` block. Scott resolves via `> Scott:` block. Completed items move to the Done table at the bottom.

---

## Quick Reference — Spec Resources

| Resource | Path | Purpose |
|----------|------|---------|
| **THIS file** | `Agents_Collab.md` (repo root) | Active tasks + handoffs |
| AI agent guide | [`docs/AI_GUIDE.md`](docs/AI_GUIDE.md) | Exact commands, GCC facts, two-compiler rule |
| Compiler architecture | [`docs/compiler_architecture.md`](docs/compiler_architecture.md) | 5-pass emission order |
| Syntax + builtins | [`docs/syntax_and_builtins.md`](docs/syntax_and_builtins.md) | Candor language cheat sheet |
| Known bugs | [`docs/known_compiler_bugs.md`](docs/known_compiler_bugs.md) | GCC error patterns + root causes |
| Language context | [`docs/context.md`](docs/context.md) | Repo layout, pipeline, type system (updated 2026-03-18) |
| Roadmap | [`docs/roadmap.md`](docs/roadmap.md) | Milestone history |

---

## Bootstrap Pipeline State (2026-04-08 09:02 MDT)

```
src/compiler/*.cnd
  → ./candorc-stage1-rebuilt.exe [.cnd files]   (Go-compiled binary)
  → src/compiler/lexer.exe                       (Candor-compiled binary, current name)

./src/compiler/lexer.exe [.cnd files] > /d/tmp/stage2.c
  → PATH="/c/msys64/mingw64/bin:$PATH" gcc.exe -std=gnu23 -O0 \
      -o /d/tmp/stage3.exe /d/tmp/stage2.c -I src/compiler -lm   ← 0 errors ✅
  → /d/tmp/stage3.exe [.cnd files] > /d/tmp/stage4.c             ← SEGFAULTS ❌ (TASK-09)
```

**Source file order (always this order):**
```
src/compiler/lexer.cnd  parser.cnd  typeck.cnd  emit_c.cnd  manifest.cnd  main.cnd
```

**Critical:** Re-append `_cnd_runtime.h` map macros before every gcc compile — VSCode strips them on save. Full block in `docs/AI_GUIDE.md` Step 4.

---

## Open Tasks

---

### TASK-09 — stage3.exe segfault in collect_params_or
**Opened: 2026-04-08 09:01 MDT**  
**Owner:** Unassigned  
**Status:** Open

**Symptom (2026-04-08 09:01 MDT):**
```
/d/tmp/stage3.exe src/compiler/lexer.cnd ... → Segmentation fault (exit 139)
```

**GDB backtrace (2026-04-08 09:01 MDT):**
```
#0  strlen() — b=0xe (invalid pointer) in _cnd_str_concat
#1  empty_params_with_err(prefix="fn is_alpha: ", e=0xe)
#2  collect_params_or(params, prefix, env)
#3  fill_fn(fd = fn "is_alpha" from lexer.cnd)
#4  pass2_decl / collect_signatures / typecheck / main
```

**Root cause hypothesis (2026-04-08 09:01 MDT):**
`collect_params_or` in `typeck.cnd` has a tail `must` expression returning `vec<ParamSig>`. Both arms are value-returning (not terminal). But `emit_must_expr` or `emit_fn_body` is incorrectly emitting `(void)(expr)` instead of `return expr` — so the function returns garbage, and the caller reads `0xe` as the `_err_val` string pointer.

**Candor source (`src/compiler/typeck.cnd`):**
```candor
fn collect_params_or(params: vec<Param>, prefix: str, env: refmut<TypeEnv>) -> vec<ParamSig> {
    collect_params(params, env) must {
        ok(ps) => ps
        err(e) => empty_params_with_err(prefix, e, env)
    }
}
```

**Where to look:** `src/compiler/emit_c.cnd` — `emit_must_expr` and `emit_fn_body`. The void suffix check (`((void)0);\n}))`) in `emit_fn_body` may be firing incorrectly on this must expression even though both arms return values.

> Claude (2026-04-08 09:01 MDT): The same pattern in the Go-emitted C (`out_go_emit.c`) has identical `(void)(...)` wrapping — confirmed it's an emission logic bug, not a runtime data issue. The must block here should produce a non-void value since both arms are value arms.

> Gemini: 

> Scott: 

---

### TASK-02 — Fix `auto _t` redefinition in Go emitter
**Opened: 2026-04-04 (Gemini session)**  
**Owner:** Gemini  
**Status:** Open — not yet fixed  
**File:** `compiler/emit_c/emit_c.go` ~line 5458

**Problem (2026-04-04):** `emitMustOrMatch()` hardcodes `auto _t = ...` for the outer let-binding of a must result. Two must-expressions in the same C scope → `redefinition of '_t'` then cascade of undeclared `_m`, `v`.

**Fix:** Use `e.freshTmp()` for the outer binding (same as the subject `_m` already does). Low priority for bootstrap — Candor emitter doesn't have this bug.

> Claude (2026-04-08 09:02 MDT): Not blocking stage3. Fix before releasing stage1 as a standalone tool.

> Gemini (2026-04-04): Claimed. Will fix emitMustOrMatch outer binding.

> Scott: 

---

### TASK-05 — Commit map macros permanently into `_cnd_runtime.h`
**Opened: 2026-04-02**  
**Owner:** Unassigned  
**Status:** Open

The map macros (`_cnd_map_insert`, `_cnd_map_get`, `_cnd_map_contains`) and `_CndRes_int64_t_const_charptr` typedef are appended via `cat >>` before each compile but are stripped by VSCode on save (Bug 1 in known_compiler_bugs.md). They need to be committed into the file permanently. This is the single highest-leverage quality-of-life fix — eliminates a recurring manual step every session.

> Claude (2026-04-08 09:02 MDT): Also audit for any other symbols stage2.c uses that aren't in the header yet.

> Gemini: 

> Scott: 

---

## Done

| Task | Description | Completed |
|------|-------------|-----------|
| M9.3 | Candor lexer in Candor | 2026-03-xx |
| M9.4 | Candor parser in Candor | 2026-03-xx |
| M9.5 | typeck.cnd phases 3–5 | 2026-03-xx |
| M9.6 | emit_c.cnd initial | 2026-03-xx |
| M9.7–9.9 | Stage 1 pipeline wired | 2026-03-xx |
| M9.10 | Bundle-aware tests, go test green | 2026-03-xx |
| M9.11 | Multi-source entry point, merge_files | 2026-03-xx |
| M9.12 | os_exec builtin | 2026-03-xx |
| M9.13 | manifest.cnd — Candor.toml parser | 2026-03-xx |
| M9.14–9.15 | match/must emission, PathBind fix | 2026-04-02 ~18:00 MDT |
| M9.16–9.17 | Scope tracking, box builtins, match fixes | 2026-04-04 ~12:00 MDT |
| Toolchain | Replaced broken MinGW with MSYS2 GCC 15.2 at C:\msys64v2026 | 2026-04-04 ~10:00 MDT |
| TASK-01 | findCC() + test scripts updated to working gcc path | 2026-04-07 ~19:00 MDT |
| TASK-04 | Constant redefinition resolved by merge_files dedup | 2026-04-04 ~12:00 MDT |
| TASK-06 (map work) | Map typedef/struct emission, _cnd_map_* macros, 0 GCC errors on stage2.c | 2026-04-08 08:51 MDT |
| TASK-07 | Test scripts updated to use working gcc path | 2026-04-07 ~19:00 MDT |
| TASK-08 | emit_fn_body implicit tail return + void suffix detection | 2026-04-08 08:58 MDT |
