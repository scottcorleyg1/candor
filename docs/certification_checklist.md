# CandorCore Module Certification Checklist

**Version 0.1 | April 2026 | INTERNAL DRAFT**

---

## What Certification Means

A certified CandorCore module (`ccMod-` with cert, `ccPar-`, or `cc-`) carries one claim:

> A human auditor verified this module against a published checklist. Its declarations are honest. Its behavior matches what its API surface says it does.

Certification is not a guarantee of correctness or completeness. It is a guarantee of **transparency**. A certified module does not hide what it does. If it later develops a vulnerability, the cert lapses or is revoked and all subscribers are notified within 24 hours. Core will provide best-effort support to certified modules to return to certified condition.

Certification is **per release**. A new major release requires a full re-audit. A patch release requires a re-scan against the automated portion of this checklist. A cert that lapses is treated as unsigned — the runtime warns but does not block. A cert that is revoked is treated as compromised — the runtime blocks execution except in diagnostic or developer mode, user-initiated.

---

## Tier Definitions

| Prefix | Relationship | Cert Scope |
|--------|-------------|------------|
| `ccMod-username` | None — community author | Artifact audit only. No endorsement of author. |
| `ccPar-Name` | Formal partner agreement | Full audit + partnership verified by Core |
| `cc-module` | Core team | Implicit — all Core modules meet this standard by definition |

A `ccMod-` author may pay for certification. The fee funds the audit. Passing does not create a relationship with Core — it certifies the artifact, not the author.

---

## Audit Checklist

### Section 1 — Identity and Provenance

- [ ] **1.1** Module name matches the registered tier format (`ccMod-`, `ccPar-`, `cc-`)
- [ ] **1.2** Author identity is verifiable (GitHub account, legal name on file for `ccPar-`)
- [ ] **1.3** Source repository is public and matches the submitted artifact exactly (hash verified)
- [ ] **1.4** License is declared and compatible with CandorCore ecosystem requirements
- [ ] **1.5** No name conflict with existing registered modules (trademark law governs disputes)

---

### Section 2 — Effects Honesty

The core question: **does the module's declared surface match what it actually does?**

- [ ] **2.1** Every exported function declares its effects (`effects(io)`, `effects(network)`, `effects(fs)`, etc.)
- [ ] **2.2** No function performs filesystem operations without `effects(fs)` in its signature
- [ ] **2.3** No function makes network calls without `effects(network)` in its signature
- [ ] **2.4** No function spawns processes without `effects(process)` in its signature
- [ ] **2.5** No function accesses environment variables without `effects(env)` in its signature
- [ ] **2.6** Functions declared `pure` contain no side effects — verified by compiler and manual review
- [ ] **2.7** No undeclared global mutable state is accessed or modified

---

### Section 3 — Error Handling Completeness

- [ ] **3.1** Every operation that can fail returns `result<T, E>` — no silent defaults, no panics on failure
- [ ] **3.2** No `result<T, E>` is silently discarded anywhere in the module's internal logic
- [ ] **3.3** Error messages are specific and actionable — not generic strings like `"error"` or `"failed"`
- [ ] **3.4** The module does not call `os_exit` or equivalent without declaring it as an effect
- [ ] **3.5** Assertions (`assert`, `requires`) are used for invariants, not for error handling on external input

---

### Section 4 — Contract Honesty

- [ ] **4.1** All input constraints are expressed as `requires` clauses, not just comments
- [ ] **4.2** All postconditions that are part of the public API contract are expressed as `ensures` clauses
- [ ] **4.3** `requires` clauses are tight — they do not accept inputs the function cannot correctly handle
- [ ] **4.4** No `requires` clause is trivially always true (placeholder contracts are not accepted)

---

### Section 5 — Security Baseline

- [ ] **5.1** No hardcoded credentials, API keys, tokens, or secrets in source or compiled artifact
- [ ] **5.2** No network calls to unexpected or undocumented endpoints
- [ ] **5.3** No execution of shell commands constructed from user input without declared sanitization
- [ ] **5.4** No dynamic code loading or evaluation
- [ ] **5.5** Memory handling (if using `box<T>` or C interop) does not introduce obvious leak or corruption paths
- [ ] **5.6** If the module handles `secret<T>` values, they are not logged, printed, or transmitted without explicit declaration

---

### Section 6 — Ecosystem Compatibility

- [ ] **6.1** Module compiles cleanly against the current CandorCore runtime (`_cnd_runtime.h`)
- [ ] **6.2** Module passes its own test suite with 0 failures
- [ ] **6.3** Module does not redefine or shadow any `cc-` namespace builtins
- [ ] **6.4** Dependencies are declared explicitly — no undeclared transitive dependencies
- [ ] **6.5** All dependencies are themselves certified or explicitly marked as uncertified in the module manifest
  - A module that depends on an uncertified or lapsed module will be flagged accordingly

---

### Section 7 — Documentation Honesty

- [ ] **7.1** Public API documentation matches the actual implemented behavior
- [ ] **7.2** Known limitations are documented plainly — no omissions that would mislead a user
- [ ] **7.3** If behavior differs across platforms, it is documented
- [ ] **7.4** Examples in documentation compile and produce the documented output

---

## Audit Result

### Pass
All checklist items verified. Core signs the module manifest. Cert is active from the date of signing. Expiry is tied to the next major release of the module or the next CandorCore runtime version, whichever comes first.

### Conditional Pass
One or more items in Section 6 or 7 have minor findings. Author has 14 days to resolve. Cert issued on resolution.

### Fail
One or more items in Sections 1–5 have findings. Cert not issued. Findings disclosed to author. Author may resubmit after remediation. Findings are not made public unless the module was previously certified (in which case the revocation record is public).

### Revocation
A previously certified module is found to have violated any Section 1–5 item after cert issuance. Cert is immediately revoked. All subscribers notified within 24 hours. Module behavior in the runtime mimics a compromised module (hard block, developer mode required). Core provides best-effort support to return the module to certified condition.

---

## What Certification Does Not Cover

Being explicit about scope is part of the trust model.

- **Correctness** — the module may have bugs. Certification does not mean it produces correct results for all inputs.
- **Performance** — no performance guarantees are made.
- **Completeness** — the module may not implement everything it could. Certification checks what is there, not what is missing.
- **Future behavior** — certification applies to the reviewed release only.
- **Author identity beyond the registered account** — for `ccMod-` modules, Core verifies the GitHub identity, not the legal person behind it.

---

## Appeals

If a module author believes a certification decision was made in error:

1. Submit a written appeal with specific evidence to Core
2. Core reviews within 5 business days
3. Decision and reasoning posted publicly (anonymized if requested)
4. A second independent auditor may be requested at the author's cost

Core's decision after appeal is final for that release. The author may resubmit a revised release at any time.

---

## Key Management and Revocation Infrastructure

*(Placeholder — to be fleshed out on the roadmap)*

Core holds a signing key used to issue all certs. Key compromise procedure, rotation cadence, and revocation infrastructure are planned items. The system will require multi-party authorization for both cert issuance and module flagging to prevent single points of compromise. See `docs/roadmap.md`.

---

*The cert is not a trophy. It is a claim. Core stands behind that claim.*
