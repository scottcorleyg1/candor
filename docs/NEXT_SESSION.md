# Next Session Handoff
**Written: 2026-04-08 09:15 MDT**

Read this first. It tells you exactly where we are and exactly what to do next.

---

## Current State

Stage 2 bootstrap is one bug away from working.

- `stage2.c` compiles to `stage3.exe` with **0 GCC errors** ✅
- `stage3.exe` runs and receives args correctly ✅  
- `stage3.exe` **segfaults** on first real input ❌

## The One Bug (TASK-09)

**Root cause:** `collect_params_or` in `src/compiler/typeck.cnd` has a tail `must` expression returning `vec<ParamSig>`. Both arms return values — neither is terminal. But `emit_fn_body` in `src/compiler/emit_c.cnd` is incorrectly emitting it as `(void)(expr)` instead of `return expr`, so the function returns garbage off the stack.

**GDB backtrace (2026-04-08 09:01 MDT):**
```
#0  strlen() — b=0xe (invalid pointer) in _cnd_str_concat
#1  empty_params_with_err(prefix="fn is_alpha: ", e=0xe)
#2  collect_params_or(...)
#3  fill_fn(fd = fn "is_alpha" from lexer.cnd)
#4  pass2_decl / collect_signatures / typecheck / main
```

**Candor source causing the crash (`src/compiler/typeck.cnd`):**
```candor
fn collect_params_or(params: vec<Param>, prefix: str, env: refmut<TypeEnv>) -> vec<ParamSig> {
    collect_params(params, env) must {
        ok(ps) => ps
        err(e) => empty_params_with_err(prefix, e, env)
    }
}
```

**Where the bug is:** `src/compiler/emit_c.cnd` — `emit_fn_body` (around line 1092) and/or `emit_must_expr`. The void-suffix check (`((void)0);\n}))`) is incorrectly firing on this must block even though both arms are value-returning.

**How to verify the bug:** Generate stage2.c and grep for `collect_params_or`:
```bash
grep -A 8 "collect_params_or" /d/tmp/stage2.c | head -20
```
If it shows `(void)((__extension__ ({` instead of `return (__extension__ ({`, that's the bug.

**The fix:** In `emit_must_expr` or `emit_fn_body`, the void suffix check needs to distinguish between "all arms are terminal (return/break/continue)" and "all arms return the same value type." A must block with `ok(ps) => ps` is NOT void — `ps` is a real value.

---

## How to Rebuild and Test

```bash
# Edit src/compiler/emit_c.cnd to fix the bug, then:

# 1. Rebuild Candor binary
./candorc-stage1-rebuilt.exe src/compiler/lexer.cnd src/compiler/parser.cnd \
  src/compiler/typeck.cnd src/compiler/emit_c.cnd \
  src/compiler/manifest.cnd src/compiler/main.cnd

# 2. Generate stage2.c
./src/compiler/lexer.exe src/compiler/lexer.cnd src/compiler/parser.cnd \
  src/compiler/typeck.cnd src/compiler/emit_c.cnd \
  src/compiler/manifest.cnd src/compiler/main.cnd > /d/tmp/stage2.c 2>/dev/null

# 3. Re-append runtime macros (VSCode strips them — see docs/known_compiler_bugs.md Bug 1)
#    Full block in docs/AI_GUIDE.md Step 4

# 4. Compile
PATH="/c/msys64/mingw64/bin:$PATH" /c/msys64/mingw64/bin/gcc.exe \
  -std=gnu23 -O0 -o /d/tmp/stage3.exe /d/tmp/stage2.c -I src/compiler -lm

# 5. Test
/d/tmp/stage3.exe src/compiler/lexer.cnd src/compiler/parser.cnd \
  src/compiler/typeck.cnd src/compiler/emit_c.cnd \
  src/compiler/manifest.cnd src/compiler/main.cnd > /d/tmp/stage4.c
echo "EXIT: $?"
wc -l /d/tmp/stage4.c
```

Success = exit 0, stage4.c has ~11000+ lines.

---

## Key Files

| File | Role |
|------|------|
| `src/compiler/emit_c.cnd` | **Primary fix target** — Candor-written C emitter |
| `src/compiler/lexer.exe` | Current Candor-compiled binary (replaces main.exe) |
| `src/compiler/_cnd_runtime.h` | C runtime — map macros get stripped by VSCode, see Bug 1 |
| `docs/AI_GUIDE.md` | Exact commands, GCC facts, two-compiler rule |
| `docs/known_compiler_bugs.md` | All known bugs with timestamps and root causes |
| `Agents_Collab.md` | Active task list with per-entry timestamps |

---

## Do Not Forget

- GCC requires `-std=gnu23` (auto type deduction)
- GCC requires `PATH="/c/msys64/mingw64/bin:$PATH"` (assembler/linker lookup)
- `_cnd_runtime.h` map macros must be re-appended before every gcc compile
- Source file order matters: `lexer parser typeck emit_c manifest main`
- The Candor binary output is named `lexer.exe` (candorc picks the first source file's name)

---

## After the Segfault Is Fixed

1. Verify `stage4.c` compiles: `gcc -std=gnu23 stage4.c -I src/compiler -lm -o stage4.exe`
2. Verify `stage4.exe` produces identical output to `stage3.exe` on the same inputs (idempotency)
3. Update `docs/roadmap.md` with M9.18 — Stage 2 self-hosting verified
4. Update `Agents_Collab.md` TASK-09 as Done with timestamp
5. Commit with message `feat: M9.18 Stage 2 self-hosting — stage3.exe compiles Candor compiler`
