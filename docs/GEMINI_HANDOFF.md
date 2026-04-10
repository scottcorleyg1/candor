# Gemini Handoff Document
**Written: 2026-04-09**
**From:** Claude (Anthropic, Sonnet 4.6)
**To:** Gemini

Read this before touching anything. It gives you complete context.

---

## What Candor Is

Candor is a self-hosted compiled systems language. The compiler is written
in Candor itself and compiles to C, which GCC then compiles to a native binary.

**Bootstrap chain (all working as of M9.19):**
```
candorc-stage1-rebuilt.exe (Go-built)
  → compiles src/compiler/*.cnd
  → src/compiler/lexer.exe  (Candor-compiled binary)
  → emits /d/tmp/stage2.c  (11,755 lines, 0 errors)
  → gcc -std=gnu23 → stage3.exe
  → stage3.exe emits stage4.c
  → diff stage2.c stage4.c = 0 lines  ← FULLY IDEMPOTENT
```

Candor is self-hosting. stage4.c == stage2.c exactly.

---

## Current State

| Area | Status |
|------|--------|
| Bootstrap idempotency | ✅ Complete (M9.19) |
| Correctness test suite | ✅ 10/10 passing (`bash tests/run_tests.sh`) |
| Runtime benchmarks | ✅ Measured (fib 65x Python, sieve 3x Python) |
| Agent eval (Candor) | ✅ 7/9 first-attempt, 2 known bugs found |
| Bug 11: `match` on int literals | ✅ Fixed |
| Bug 12: `for x in v` single-file | ✅ Fixed |
| Agent eval other languages | ❌ Not started — Python/Rust/TypeScript needed |
| TASK-02: Go emitter `auto _t` | ❌ Open (separate from bootstrap) |

---

## Open Tasks For You

### ✅ Priority 1 — Fix Bug 11: integer match (Completed)

**File:** `src/compiler/emit_c.cnd`
**Function:** `arm_cond` (~line 763)

Add a case for `Expr::Int`:
```candor
Expr::Int(s) => str_concat("_m == ", s)
```

**Verification:** After fix, rebuild lexer.exe, verify `diff stage2.c stage4.c` = 0,
then run `bash tests/run_tests.sh` — all 10 should still pass.
Add a new test: `tests/cases/11_int_match.cnd` with arms `0 => "zero"` etc.

---

### ✅ Priority 2 — Fix Bug 12: `for` loop single-file (Completed) (emit_c.cnd or _cnd_runtime.h)

**Option A (easier):** Add `_cnd_vec_len` and `_cnd_vec_get` to `_cnd_runtime.h`.
They must use the same guard naming as the emitter.

**Option B (cleaner):** Change the `for` loop emitter to emit inline index access
instead of helper function calls. Find `emit_for_stmt` in `emit_c.cnd`.

After fix: the `for x in v` pattern should work in single-file programs.
Update task A4 in `tests/agent_eval/tasks.md` to use `for x in v` syntax.

---

### Priority 3 — Agent eval: run tasks in Python/Rust/TypeScript

Run the same 8 tasks from `tests/agent_eval/tasks.md` in Python and Rust.
Record results in `tests/agent_eval/results.csv`.

Columns: `date,language,task_id,attempt_1_pass,total_turns,total_tokens,loc,first_error_type,notes`

---

## How to Build

```bash
# Rebuild Candor binary after editing .cnd files
./candorc-stage1-rebuilt.exe src/compiler/lexer.cnd src/compiler/parser.cnd \
  src/compiler/typeck.cnd src/compiler/emit_c.cnd \
  src/compiler/manifest.cnd src/compiler/main.cnd

# Regenerate stage2.c
./src/compiler/lexer.exe src/compiler/lexer.cnd src/compiler/parser.cnd \
  src/compiler/typeck.cnd src/compiler/emit_c.cnd \
  src/compiler/manifest.cnd src/compiler/main.cnd > /d/tmp/stage2.c 2>/dev/null

# Compile stage3
PATH="/c/msys64/mingw64/bin:$PATH" /c/msys64/mingw64/bin/gcc.exe \
  -std=gnu23 -O0 -o /d/tmp/stage3.exe /d/tmp/stage2.c -I src/compiler -lm

# Verify idempotency
/d/tmp/stage3.exe src/compiler/lexer.cnd src/compiler/parser.cnd \
  src/compiler/typeck.cnd src/compiler/emit_c.cnd \
  src/compiler/manifest.cnd src/compiler/main.cnd > /d/tmp/stage4.c 2>/dev/null
diff /d/tmp/stage2.c /d/tmp/stage4.c  # must be 0 lines

# Run correctness tests
bash tests/run_tests.sh
```

---

## Critical Facts

- GCC: always `-std=gnu23` (C23 `auto`), always `PATH="/c/msys64/mingw64/bin:$PATH"`
- `_cnd_runtime.h` is committed with map macros — no manual append needed anymore
- Source file order: `lexer parser typeck emit_c manifest main` (always)
- `src/compiler/lexer.exe` is the current Candor binary
- All known bugs: `docs/known_compiler_bugs.md`
- Architecture: `docs/compiler_architecture.md`
- Language reference: `docs/syntax_and_builtins.md`
- Task list: `Agents_Collab.md`

---

## What NOT To Do

- Do not modify `_cnd_runtime.h` via `cat >>` — edit the file directly and commit
- Always verify `diff stage2.c stage4.c = 0` after any change to emit_c.cnd
- Do not amend published commits
