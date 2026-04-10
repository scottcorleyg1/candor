# Agents_Collab.md
## Multi-Agent Coordination — Claude + Gemini
### Scott resolves conflicts, sets priority, owns `> Scott:` blocks
**Last updated: 2026-04-08 22:00 MDT**

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

## Bootstrap Pipeline State (2026-04-08 22:00 MDT)

```
src/compiler/*.cnd
  → ./candorc-stage1-rebuilt.exe [.cnd files]   (Go-compiled binary)
  → src/compiler/lexer.exe                       (Candor-compiled binary)

./src/compiler/lexer.exe [.cnd files] > /d/tmp/stage2.c   (11,616 lines)
  → gcc -std=gnu23 -O0 -o /d/tmp/stage3.exe stage2.c      ← 0 errors ✅
  → /d/tmp/stage3.exe [.cnd files] > /d/tmp/stage4.c      ← EXIT 0, 11,616 lines ✅ ← M9.18 ACHIEVED

./stage4.c → gcc -std=gnu23 → stage4.exe                  ← 0 errors ✅ ← M9.19 ACHIEVED
diff stage2.c stage4.c                                     ← 0 diff lines ✅ IDEMPOTENT
```

**MILESTONE M9.18 ACHIEVED 2026-04-08:** `stage3.exe` successfully compiles the full Candor compiler source and exits 0. Stage 2 self-hosting is working.

**MILESTONE M9.19 ACHIEVED 2026-04-09:** stage4.c == stage2.c (0 diff lines). Full bootstrap idempotency proven. Candor is fully self-hosting.

**Source file order (always this order):**
```
src/compiler/lexer.cnd  parser.cnd  typeck.cnd  emit_c.cnd  manifest.cnd  main.cnd
```

**Critical:** Re-append `_cnd_runtime.h` map macros before every gcc compile — VSCode strips them on save. Full block in `docs/AI_GUIDE.md` Step 4.

---

## Open Tasks

---

### TASK-09 — stage3.exe segfault — CLOSED ✅
**Opened: 2026-04-08 09:01 MDT**  
**Closed: 2026-04-08 22:00 MDT**  
**Root cause documented:** `known_compiler_bugs.md` Bugs 7 and 8.

Two bugs fixed in `src/compiler/emit_c.cnd`:
1. `arm_is_terminal_blk` returned `true` for any no-final-expr block → merge_files pushed every decl twice → corrupted tag alternation (0,6,0,6...)
2. `emit_block_expr` didn't extract implicit tail ExprS → Candor `{ side_effect(); value }` blocks emitted as void

Result: `stage3.exe` runs, exits 0, produces 11,616 lines. **M9.18 achieved.**

---

### TASK-10 — stage4.c idempotency — CLOSED ✅
**Opened: 2026-04-08 22:00 MDT**  
**Closed: 2026-04-09 ~00:30 MDT**  
**Root cause documented:** `known_compiler_bugs.md` Bug 10.

**Actual root cause (deeper than originally documented):**
`stmt_to_expr` always returned NULL — not because of `emit_fn_body`'s void-suffix check, but because the `_ => return none` catchall terminal arm was emitted as `if (1 /*bind*/) { return NULL; }` BEFORE the `Stmt::ExprS` value arm in the C ternary. The always-true condition shadowed the ExprS arm completely. This caused `emit_fn_body` to never take the `some(e_node)` branch — every function tail was emitted as `(void)(expr)` via `emit_expr_stmt`.

**Fix in `emit_c.cnd`:**
- Added `arm_cond_is_catchall` helper
- Changed `emit_match_expr` and `emit_must_expr` to exclude catchall terminal arms from the early if-block phase
- Catchall terminals now emit as the final else of the ternary: `(__extension__({ terminal_body; (void*)0; }))`

**Result:** stage4.c == stage2.c (0 diff lines). stage4.exe compiles (0 GCC errors). **M9.19 achieved.**

---

### TASK-02 — Fix `auto _t` redefinition in Go emitter — CLOSED ✅
**Opened: 2026-04-04 (Gemini session)**  
**Owner:** Gemini  
**Status:** Closed — Audit of `emit_c.go` confirms `emitMustOrMatch` now uses `res := e.freshTmp()`. The bug was resolved under the hood previously!  
**File:** `compiler/emit_c/emit_c.go` ~line 5458

**Problem (2026-04-04):** `emitMustOrMatch()` hardcodes `auto _t = ...` for the outer let-binding of a must result. Two must-expressions in the same C scope → `redefinition of '_t'` then cascade of undeclared `_m`, `v`.

**Fix:** Use `e.freshTmp()` for the outer binding (same as the subject `_m` already does). Low priority for bootstrap — Candor emitter doesn't have this bug.

> Claude (2026-04-08 09:02 MDT): Not blocking stage3. Fix before releasing stage1 as a standalone tool.

> Gemini (2026-04-04): Claimed. Will fix emitMustOrMatch outer binding.

> Scott: 

---

### TASK-05 — Commit map macros permanently into `_cnd_runtime.h` — CLOSED ✅
**Opened: 2026-04-02**  
**Owner:** Gemini  
**Status:** Closed — Macros formally appended into the file and tracked securely by Git in M9.19.

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
| TASK-09 | stage3.exe segfault — arm_is_terminal_blk + emit_block_expr implicit tail | 2026-04-08 22:00 MDT |
| **M9.18** | **Stage 2 self-hosting: stage3.exe exits 0, 11,616 lines** | **2026-04-08 22:00 MDT** |
