## Candor → Go Audit Report

**Source:** `safety_demo.cnd`

**Audit entries:** 9

---

### effects declarations (2)

**`show_account`** — line 28
`effects(io)`
C equivalent: none (dropped)
Candor enforces that only functions declaring effects(io) can perform these operations. Any Go function can perform them silently.

**`main`** — line 73
`effects(io)`
C equivalent: none (dropped)
Candor enforces that only functions declaring effects(io) can perform these operations. Any Go function can perform them silently.

### pure declarations (1)

**`add`** — line 17
`pure`
C equivalent: none enforced (Go has no pure annotation)
Candor enforces at compile time that pure functions cannot call any function with effects. Go has no equivalent.

### requires clauses (4)

**`add`** — line 17
`requires a >= 0`
C equivalent: // requires: a >= 0 (comment only)
Candor requires clauses are in the function signature — machine-readable by every caller. In Go this becomes a comment, invisible to the type system.

**`add`** — line 17
`requires b >= 0`
C equivalent: // requires: b >= 0 (comment only)
Candor requires clauses are in the function signature — machine-readable by every caller. In Go this becomes a comment, invisible to the type system.

**`deposit`** — line 36
`requires amount > 0`
C equivalent: // requires: amount > 0 (comment only)
Candor requires clauses are in the function signature — machine-readable by every caller. In Go this becomes a comment, invisible to the type system.

**`withdraw`** — line 46
`requires amount > 0`
C equivalent: // requires: amount > 0 (comment only)
Candor requires clauses are in the function signature — machine-readable by every caller. In Go this becomes a comment, invisible to the type system.

### must{} error handling (2)

**`transfer`**
`must{} on result<Account, str>`
C equivalent: if err != nil { ... }
Candor enforces that discarding this result<Account, str> is a compile error. In Go the caller can use _ to silently discard errors.

**`transfer`**
`must{} on result<Account, str>`
C equivalent: if err != nil { ... }
Candor enforces that discarding this result<Account, str> is a compile error. In Go the caller can use _ to silently discard errors.

---

### Summary

| Feature | Instances | C enforcement |
|---------|-----------|---------------|
| effects declarations | 2 | None — dropped |
| pure declarations | 1 | None — dropped |
| requires clauses | 4 | assert() in debug builds only |
| must{} error handling | 2 | Not enforced — silent discard is valid C |

**What the Go output cannot tell you:** whether this program respects its own effect boundaries, whether callers can ignore errors, or whether preconditions hold at every call site. Those properties exist in the Candor source. They do not exist in the Go output.
