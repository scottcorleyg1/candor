# Audit Emitter Specification

**Version 0.1 | April 2026**
**Status: Design — M12.1 (C) ready to implement, M12.2 (Go) follows**

---

## What This Is

`candorc --emit=c-audit` (and later `--emit=go`) produces two outputs from a single Candor source:

1. **`<name>.c`** — the program logic in valid, compilable C (already produced by the existing emitter)
2. **`<name>.audit.md`** — a structured report of every Candor feature that has no equivalent in C, with per-instance line references back to the Candor source

The report is not a complaint about C. It is documentation: "here is what the Candor compiler enforced for you that the C output silently dropped."

---

## Architecture

The audit report is generated as a **side pass alongside C emission**, not a separate pipeline. Every time the C emitter encounters a feature it cannot express in the target, it writes an entry to an `AuditLog`. After emission completes, the log is rendered to Markdown.

```
Candor source
    │
    ▼
[Lex → Parse → Typecheck]   (existing pipeline, unchanged)
    │
    ▼
[C emitter]  ──────────────────────────────► file.c
    │  (side pass: log every untranslatable feature)
    ▼
[AuditLog]  ────────────────────────────────► file.audit.md
```

This means:
- No second pass over the AST
- No separate audit tool to maintain
- The report is always current with what was actually emitted
- Adding a new Candor feature automatically adds it to the audit scope if it logs entries

---

## AuditLog Structure

```go
type AuditEntry struct {
    Feature     string   // "effects declaration", "requires clause", etc.
    FnName      string   // enclosing function name
    Line        int      // Candor source line number
    CEquiv      string   // C equivalent, or "none"
    Explanation string   // one sentence: what Candor enforced that C cannot
}

type AuditLog struct {
    SourceFile string
    Entries    []AuditEntry
}
```

---

## Features That Generate Audit Entries

### 1. Effects declarations

**Trigger:** any `fn` with one or more `effects(...)` declarations  
**C equivalent:** none — dropped silently  
**Entry per function** (not per call site)

```
fn read_config(path: str) -> result<str, str> effects(fs)
→ Entry: effects(fs) on read_config, line 4
  C equivalent: none.
  Candor enforces: only functions declaring effects(fs) can read files.
  Any C function can call fopen() with no declaration.
```

### 2. `requires` clauses

**Trigger:** any `fn` with one or more `requires` clauses  
**C equivalent:** `assert(condition)` in debug builds (emitted as such)

```
fn divide(a: i64, b: i64) -> i64 requires b != 0
→ Entry: requires b != 0 on divide, line 8
  C equivalent: assert(b != 0) — debug only, compiled out with NDEBUG.
  Candor enforces: precondition is in the signature, machine-readable,
  visible to every caller without reading the body.
```

### 3. `ensures` clauses

**Trigger:** any `fn` with one or more `ensures` clauses  
**C equivalent:** `assert(result condition)` after return value computed

```
fn safe_sqrt(x: f64) -> f64 ensures result >= 0.0
→ Entry: ensures result >= 0.0 on safe_sqrt, line 14
  C equivalent: assert() on return value — debug only.
  Candor enforces: postcondition is part of the public contract.
```

### 4. `must{}` on `result<T,E>` or `option<T>`

**Trigger:** every `let x = expr must { ... }` site  
**C equivalent:** `if (_ok) { ... } else { ... }` — but Go/C allow silent discard  
**Entry per call site**

```
let val = parse(s) must { ok(v) => v  err(e) => return err(e) }
→ Entry: must{} on result<i64,str> at line 22
  C equivalent: if/else on _ok field.
  Candor enforces: silently discarding this result<T,E> is a compile error.
  In C, the caller can ignore the return value entirely.
```

### 5. `pure` functions

**Trigger:** any `fn` declared `pure`  
**C equivalent:** none (GCC has `__attribute__((pure))` but it is not enforced)

```
fn hash(x: i64) -> i64 pure
→ Entry: pure declaration on hash, line 31
  C equivalent: none enforced (GCC __attribute__((pure)) is advisory).
  Candor enforces: pure functions cannot call any function with effects.
  Verified through the entire call graph at compile time.
```

### 6. `secret<T>` values

**Trigger:** any variable, parameter, or return type of type `secret<T>`  
**C equivalent:** the inner type `T` — the secret wrapper is dropped

```
fn check_token(tok: secret<str>) -> bool effects(auth)
→ Entry: secret<str> parameter on check_token, line 45
  C equivalent: const char* — information-flow enforcement dropped.
  Candor enforces: secret<T> values cannot be printed, logged, or
  transmitted without explicit declaration.
```

---

## Report Format

```markdown
## Candor → C Audit Report

**Source:** `divide.cnd`
**Compiled:** 2026-04-12
**Functions converted:** 3
**Structs converted:** 1
**Audit entries:** 6

---

### effects declarations (2)

**`read_config`** — line 4
`effects(fs)`
C equivalent: none (dropped).
Candor enforces: only functions declaring `effects(fs)` can touch the
filesystem. In C, any function can call `fopen()` silently.

**`post_result`** — line 12
`effects(network)`
C equivalent: none (dropped).
Candor enforces: network access requires an explicit declaration in the
function signature, visible to every caller and auditing tool.

---

### requires clauses (1)

**`divide`** — line 8
`requires b != 0`
C equivalent: `assert(b != 0)` (debug builds only; elided with `-DNDEBUG`).
Candor enforces: precondition is machine-readable in the signature.
Callers and AI agents see it without reading the function body.

---

### must{} error handling (3)

**line 15** — `result<i64, str>` from `parse()`
**line 22** — `result<Config, str>` from `load_config()`
**line 31** — `option<str>` from `lookup()`
C equivalent: if/else on `_ok` field.
Candor enforces: silently discarding any of these is a compile error.
In C, the caller can ignore the return value with no warning.

---

### Summary

| Feature | Instances | C enforcement |
|---------|-----------|---------------|
| effects declarations | 2 | None |
| requires clauses | 1 | assert() debug-only |
| must{} error handling | 3 | Not enforced |

**What the C output cannot tell you:** whether this program respects its
own effect boundaries, whether callers can ignore errors, or whether
preconditions hold at every call site. Those properties exist in the
Candor source. They do not exist in the C output.
```

---

## Implementation Plan

### Phase 1 — AuditLog infrastructure (Go compiler)

1. Add `AuditLog` and `AuditEntry` structs to `compiler/emit_c/`
2. Thread `*AuditLog` through `emitter` struct (nil = no audit pass)
3. Add `--emit=c-audit` flag to `main.go` — sets up `AuditLog` before emission

### Phase 2 — Logging hooks in the existing C emitter

Wire log entries at the existing emit points:

| Emit function | Hook location |
|---------------|--------------|
| `emitFnDecl` | after effects list parsed — log each effect |
| `emitFnDecl` | after requires/ensures parsed — log each clause |
| `emitMustOrMatch` | at entry — log the must site and subject type |
| `emitFnDecl` | if `pure` annotation — log pure declaration |
| `emitLetStmt` / params | if type is `secret<T>` — log the secret usage |

No changes to emission logic — logging is purely additive.

### Phase 3 — Report renderer

`func (log *AuditLog) RenderMarkdown() string` — groups entries by feature category, renders the format above, appends the summary table.

### Phase 4 — Wire to CLI

```bash
candorc --emit=c-audit divide.cnd
# Produces:
#   divide.c       (existing C output, unchanged)
#   divide.audit.md  (new audit report)
```

JSON variant for tooling integration:
```bash
candorc --emit=c-audit-json divide.cnd
# Produces divide.audit.json
```

---

## What Comes Next (M12.2)

The Go emitter reuses the same `AuditLog` infrastructure. The only difference is:
- A new `emit_go.go` backend (produces valid `.go` output)
- The audit hooks are identical — same feature categories, same entry format
- Go-specific notes in some entries (e.g., `must{}` → "Go allows `_, err = f()`")

The report format is identical across all target languages by design — a consistent audit surface regardless of what you're compiling to.

---

*The goal is not to prove C is bad. It is to make the safety properties of Candor visible and legible to someone who only knows C. The report is the proof.*
