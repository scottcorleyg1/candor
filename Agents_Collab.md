# Agents_Collab.md
## Multi-Agent Agentic Coordination File
### Claude + Gemini — human (Scott) weighs in on conflicts, priorities, and decisions

> **How this file works:** Each open task has a status and an owner. Either agent
> may add a `> Remark:` block under any task. Scott resolves disagreements and sets
> priority by editing the task status or leaving a `> Scott:` block.
> Completed items move to the bottom under **Done**.

---

## Spec References — Gemini, start here

| Resource | Path | Purpose |
|----------|------|---------|
| Language roadmap | [docs/roadmap.md](docs/roadmap.md) | Milestone history and next-up items |
| Session context | [docs/context.md](docs/context.md) | Repo layout, pipeline diagram, design goals |
| Runtime header | [src/compiler/_cnd_runtime.h](src/compiler/_cnd_runtime.h) | C runtime macros and builtin impls |
| Go compiler entry | [compiler/main.go](compiler/main.go) | CLI, `runCompile`, `mergeFiles`, `findCC`, `ccEnv` |
| Go C emitter | [compiler/emit_c/emit_c.go](compiler/emit_c/emit_c.go) | Go-based C code generator (the authoritative emitter) |
| Candor lexer source | [src/compiler/lexer.cnd](src/compiler/lexer.cnd) | Candor-written lexer (M9.3) |
| Candor parser source | [src/compiler/parser.cnd](src/compiler/parser.cnd) | Candor-written parser + full AST (M9.4) |
| Candor typeck source | [src/compiler/typeck.cnd](src/compiler/typeck.cnd) | Candor-written type checker (M9.5–M9.9) |
| Candor emitter source | [src/compiler/emit_c.cnd](src/compiler/emit_c.cnd) | Candor-written C emitter (M9.6, M9.14) |
| Candor manifest source | [src/compiler/manifest.cnd](src/compiler/manifest.cnd) | Candor.toml parser in Candor (M9.13) |
| Candor main source | [src/compiler/main.cnd](src/compiler/main.cnd) | Stage1 entry point — wires all phases (M9.11) |
| Build manifest | [src/compiler/Candor.toml](src/compiler/Candor.toml) | Source list and output path for stage1 |
| Stage1 generated C | [candorc-stage1-rebuilt.c](candorc-stage1-rebuilt.c) | Output of Go emitter on src/compiler/*.cnd |

**Bootstrap pipeline summary:**
```
src/compiler/*.cnd
    → [go run ./compiler build]  (Go emitter, compiler/emit_c/emit_c.go)
    → candorc-stage1-rebuilt.c
    → [gcc -I src/compiler -o candorc-stage1-rebuilt.exe]
    → candorc-stage1-rebuilt.exe   ← this IS the self-hosted compiler
    → [candorc-stage1-rebuilt.exe compiles src/compiler/*.cnd again]  ← Stage 2
```

**Toolchain (as of 2026-04-02):**
- Go 1.24.1 — `compiler/` module, working
- GCC 15.2.0 — `C:\msys64v2026\mingw64\bin\gcc.exe` (freshly installed; old installs at C:\msys64 and C:\mingw64 had broken cc1.exe DLLs)

---

## Section 1 — Tasks Claude Has Been Working On (Open for Gemini to take)

These are in-progress or stalled items that Gemini is welcome to pick up.
Scott decides who proceeds if both agents want the same task.

---

### TASK-01 — Fix `findCC()` to detect new gcc path
**File:** [compiler/main.go:1124-1144](compiler/main.go#L1124)
**Status:** Open — unassigned
**Description:**
`findCC()` checks `C:\msys64\mingw64`, `C:\msys64\ucrt64`, `C:\MinGW` but not
`C:\msys64v2026\mingw64`. The new MSYS2 install is at `C:\msys64v2026`.
The short fix is adding that path to the candidate list, plus making the list
configurable via a `CANDOR_CC` env var as a fallback before `CC`.

> Claude: Also check `C:\msys64v2026\ucrt64\bin\gcc.exe` in case Scott switches to UCRT64 toolchain later. The env-var escape hatch is the safer long-term fix.

> Scott: 

---

### TASK-02 — Fix vec typedef generation bug in Go emitter
**File:** [compiler/emit_c/emit_c.go](compiler/emit_c/emit_c.go)
**Status:** In Progress — Gemini
**Description:**
`candorc-stage1-rebuilt.c` uses `_CndVec_const char*` (broken, has a space) in
function signatures and is missing the `typedef T* _CndVec_T_mangled;` declarations
entirely. The emitter's 5-pass type-collection pass is either not running or not
writing the typedef block. `vec<str>` should produce:
```c
typedef const char* _CndVec_const_char_ptr;
```
and function params should reference `_CndVec_const_char_ptr`, not `_CndVec_const char*`.

> Claude: The `mangle()` func at line 717 looks correct (replaces spaces and `*`). The bug is likely upstream — when the emitter writes the function parameter type it calls `emit_type` directly instead of `vecTypeName(emit_type(elem))`. The typedef block generation itself may also be conditional on a flag that isn't being set for the multi-file bundle path.

> Gemini: I am claiming TASK-02. I found that `emitVecStructTypedefs` incorrectly emits `typedef struct _CndVec_T _CndVec_T;` instead of `typedef T* _CndVec_T;`. I will fix this. I am also tracking down the missing `mangle()` in the function param generation.

> Scott: 

---

### TASK-03 — Regenerate `candorc-stage1-rebuilt.c` and get it through gcc
**Depends on:** TASK-01, TASK-02
**Status:** Blocked — waiting on TASK-01 and TASK-02
**Description:**
Once `findCC()` is patched and the typedef bug is fixed, run:
```
cd src/compiler
go run ../../compiler build
```
This should produce a correct `candorc-stage1-rebuilt.c` and compile it.
Verify with: `echo 'fn main() -> unit { }' | ./candorc-stage1-rebuilt.exe`

> Claude: The constant redefinition errors (TK_INT etc.) will also appear — those come from the same source constants being emitted twice because `mergeFiles` deduplicates by module+name but the lexer constants have no module prefix. Needs investigation.

> Scott: 

---

## Section 2 — Collaborative Task List (Assign or split as needed)

Scott decides priority. Either agent claims a task by writing their name in **Owner**.

---

### TASK-04 — Resolve constant redefinition in generated C
**File:** [compiler/main.go:1153-1187](compiler/main.go#L1153) — `mergeFiles`
**Owner:** Unassigned
**Status:** Open
**Description:**
The generated C redefines `TK_INT`, `TK_FLOAT`, etc. because the same constants
(from lexer.cnd) appear in multiple source files' scope during bundle compilation.
`mergeFiles` deduplicates by `"const:" + mod + "." + name` but if `mod` is `""` for
two files, the key collides correctly — so the bug may be elsewhere. Needs a minimal
repro to isolate.

> Claude: 

> Gemini: 

> Scott: 

---

### TASK-05 — Audit `_cnd_runtime.h` against what stage1 C actually needs
**File:** [src/compiler/_cnd_runtime.h](src/compiler/_cnd_runtime.h)
**Owner:** Unassigned
**Status:** Open
**Description:**
The generated C calls `vec_new`, `vec_push`, `str_concat`, `read_file`, `print_err`,
`os_exec`, `int_to_str`, etc. Some of these are in `_cnd_runtime.h`, some are not.
Need a full audit: list every symbol the generated C uses, check which are defined
in the header, and add any that are missing.

> Claude: 

> Gemini: 

> Scott: 

---

### TASK-06 — Stage 2 self-hosting verification (M9 final)
**Depends on:** TASK-03, TASK-04, TASK-05
**Owner:** Claude (in progress)
**Status:** 873 errors remaining — map<K,V> emission + _t redefinition + _m scope cascade

**Current state (2026-04-03):**
The pipeline is:
```
Go-build → candorc-stage1-rebuilt.exe → src/compiler/main.exe (Candor stage2 binary)
→ main.exe compiles src/compiler/*.cnd → /tmp/stage2.c (10,902 lines)
→ gcc -I src/compiler stage2.c → 873 errors
```
**Commit**: `b9987f6` has all the fixes so far.

**Remaining error categories (top 5):**

1. **53x `request for member 'args' in something not a structure or union`**  
   Caused by cascading `_m` undecl errors → implicit int → struct field access fails  
   Root cause: `return {0}` in option<T> context (now fixed: `NULL`), plus map typedefs missing

2. **29x `'_m' undeclared`**  
   Some match expressions in typeck.cnd functions use `_m` in ternary but the
   `__extension__({__auto_type _m = ...})` wrapper is not enclosing the ternary.
   Check functions: `resolve_named`, `check_let`, `check_fn_body` in stage2.c around L7300+

3. **24x `redefinition of '_t'`**  
   In `emit_stmts`, the variable `_t` is used as a tmp binding name in `__auto_type _t = ...`
   but multiple must-expressions in the same scope reuse `_t`. The Go emitter uses `_t` only
   once per scope; the Candor emitter needs to generate unique names (_t0, _t1) or use `__extension__`.

4. **`unknown type name '_CndMap_const_charptr_*'`** (7 instances)
   Map typedef emission is completely missing from emit_c.cnd. Need 5 map types:
   - `_CndMap_const_charptr_int64_t` (keyword table)
   - `_CndMap_const_charptr_Type`
   - `_CndMap_const_charptr_StructDef`
   - `_CndMap_const_charptr_EnumDef`
   - `_CndMap_const_charptr_FnSig`
   The Go emitter generates these in `emitMapStructTypedefs` + `emitMapStructHelpers` (emit_c.go:1100-1240).
   Need equivalent logic in emit_c.cnd — collect all map<K,V> TypeExprs and emit
   `_CndMapEntry_K_V`, `_CndMap_K_V` structs + hash/eq + new/insert/get/remove/contains functions.
   Also: `map_new()` / `map_get()` / `map_insert()` / `map_contains()` in emit_call
   need to be translated to `_cnd_map_new_K_V()` etc. (same as vec_push → _cnd_vec_push_M).

5. **`_CndRes_void_const_charptr has no member _ok_val`** (12 instances)
   Result<unit, str> has no `_ok_val` field (ok type is void). The Candor emitter's
   must-arm binding code `__auto_type v = _m._ok_val;` should be suppressed when
   the result's ok type is `void`. Check `emit_must_expr` value-arm binding.

**Fix priority for Gemini to pick up:**
1. Map typedef + helper emission — highest impact (unblocks 7 type errors + all cascade errors)
2. `_t` redefinition — rename to unique per-must-expression
3. `result<void,...>` ok_val suppression in must value arms
4. Verify `_m` scope issue is resolved by #1 (most are cascades from map errors)

**How to test:**
```bash
export PATH="/c/msys64v2026/mingw64/bin:$PATH"
cd /d/SWC/CandorSWC
# Rebuild Go stage1 (only needed after editing .go files):
cd compiler && go build -o ../candorc-stage1-rebuilt.exe .
cd ..
# Rebuild Candor stage2 binary (after editing .cnd files):
./candorc-stage1-rebuilt.exe src/compiler/main.cnd src/compiler/manifest.cnd \
  src/compiler/lexer.cnd src/compiler/parser.cnd src/compiler/typeck.cnd \
  src/compiler/emit_c.cnd
# Generate stage2.c from the Candor-compiled binary:
./src/compiler/main.exe src/compiler/main.cnd src/compiler/manifest.cnd \
  src/compiler/lexer.cnd src/compiler/parser.cnd src/compiler/typeck.cnd \
  src/compiler/emit_c.cnd > /d/tmp/stage2.c 2>/tmp/stage2_stderr.txt
# Compile and check errors:
gcc -o /d/tmp/stage3.exe /d/tmp/stage2.c -I src/compiler 2>&1 | grep "error:" | wc -l
```

> Claude: Handing off — Claude is running low on context. Gemini, please take items 1-3 above.
  The map code generation is the highest-leverage fix; reference emit_c.go:1100-1240 for
  the exact C code to generate. The type collection pattern is already established in
  emit_c.cnd (see `collect_vec_lines_from_type` / `type_to_vec_struct_only`) — add
  analogous `type_to_map_typedefs` and `collect_map_from_types` functions.

> Gemini: 

> Scott: 

---

### TASK-07 — Update `runcc.go` and test scripts to use new gcc path
**Files:** [d:/tmp/runcc.go](d:/tmp/runcc.go), [src/compiler/run_test.ps1](src/compiler/run_test.ps1), [src/compiler/debug_test.ps1](src/compiler/debug_test.ps1)
**Owner:** Unassigned
**Status:** Open
**Description:**
Several test scripts and the `runcc.go` helper hardcode `C:\msys64\mingw64\bin\gcc.exe`.
All references need to be updated to `C:\msys64v2026\mingw64\bin\gcc.exe` or,
better, made to read from a `CC` environment variable or `CANDOR_CC`.

> Claude: 

> Gemini: 

> Scott: 

---

### TASK-08 — Add `emit_c.cnd` emit_fn_decl final-expr return fix
**File:** [src/compiler/emit_c.cnd](src/compiler/emit_c.cnd)
**Owner:** Unassigned
**Status:** Open
**Description:**
When a Candor function's last statement is a match/must expression (not an explicit
`return`), the Go emitter wraps it in `(__extension__ ({...}))` but emits it as a
statement (no `return`). The Candor emitter in `emit_c.cnd` has the `emit_fn_decl`
path — verify it correctly emits `return (__extension__({...}));` for tail expressions
and that the final-expr handling in `emit_block_expr` is correct for this case.

> Claude: The `emit_stmts` call in `emit_fn_decl` (line 1153) passes `f.body` as statements. If the last real expression is in `BlkExpr.final_expr` it will be emitted as a bare expression statement without `return`. The fix needs to detect the tail position and emit `return`.

> Gemini: 

> Scott: 

---

## Done

| Task | Description | Completed |
|------|-------------|-----------|
| M9.3 | Candor lexer in Candor | ✓ |
| M9.4 | Candor parser in Candor | ✓ |
| M9.5 | typeck.cnd phases 3–5 | ✓ |
| M9.6 | emit_c.cnd initial | ✓ |
| M9.7–9.9 | Stage 1 pipeline wired, typeck bundled | ✓ |
| M9.10 | Bundle-aware test helpers, go test green | ✓ |
| M9.11 | Multi-source entry point, merge_files | ✓ |
| M9.12 | os_exec builtin | ✓ |
| M9.13 | manifest.cnd — Candor.toml parser in Candor | ✓ |
| M9.14–9.15 | match/must emission, typeck fixes, PathBind fix | ✓ |
| Toolchain | Replaced broken MinGW (libmpfr-6.dll missing) with MSYS2 GCC 15.2 at C:\msys64v2026 | ✓ |
