# Candor Ecosystem Naming Convention

**Version 0.2 | April 2026 | Scott Corley**

---

## Philosophy

> Code should mean exactly what it says, and say everything it means.

The Candor naming convention extends this principle to the ecosystem itself. Every prefix communicates governance, trust level, and author relationship. The name is the documentation — a developer reads a module name and immediately knows who wrote it, what relationship they have with the Core team, and whether it has been audited.

---

## The Three Tiers

| Prefix | Relationship to Core | Cert Eligible | Example |
|--------|---------------------|---------------|---------|
| `ccMod-username` | None — community author | Yes (paid audit) | `ccMod-alice-fastmath` |
| `ccPar-Name` | Formal partner agreement | Yes (included) | `ccPar-Nvidia-tensor` |
| `cc-module` | Core team | Implicit | `cc-collections` |

### `ccMod-username`

A community module published by an individual. No relationship with Core exists. The username is the GitHub identity of the author. A `ccMod-` module may apply for certification — the cert is paid for by the author, funds the audit, and certifies the artifact (not the author). Passing certification does not create a relationship with Core.

**Stability guarantee:** The name is stable for the lifetime of the author's relationship to the project. If the module is adopted as an official Core module, it is renamed to `cc-module` — which is an explicit, documented, lint-flagged rename that forces all consumers to acknowledge the change.

### `ccPar-Name`

A module from a formally verified partner. The partnership is documented and maintained by Core. A `ccPar-` module is subject to the full certification audit plus partnership verification. The Name is the partner's legal or trade name (e.g., `ccPar-Nvidia`, `ccPar-LLVM`). Sub-functions use a trailing dash: `ccPar-Nvidia-gpu`, `ccPar-Nvidia-tensor`.

### `cc-module`

An official Core module, maintained by the Core team. Meets all certification requirements by definition. No application required.

---

## Why Provenance-Based, Not Lifecycle-Based

An earlier design (v0.1, April 2026) used a four-stage lifecycle model:

```
ccStack-nvidia  →  ccv-Nvidia  →  cc-Nvidia  →  ccNvidia-fn
```

This was rejected because **graduation breaks import paths**. A module that graduates from `ccStack-nvidia` to `cc-Nvidia` requires every consumer to update their import statements. The provenance-based model avoids this: the name changes only when the relationship changes, and relationship changes are intentional, infrequent, and explicitly flagged by the toolchain.

The provenance-based model preserves the ecosystem's honesty principle: you always know what you have, and changes require acknowledgment.

---

## Name Conflict Resolution

Name conflicts within the `ccMod-username` tier are scoped by username, so conflicts are impossible. Conflicts within `ccPar-` are resolved by the partnership agreement (two partners cannot share a name). Conflicts within `cc-` are Core's responsibility.

If a `ccMod-` name is confusingly similar to a `cc-` or `ccPar-` name, Core may request a rename. Disputes over trade names are governed by US trademark law.

---

## Certification and the Name

The cert is tied to the name at the time of issuance. If a module is renamed after cert issuance, the cert must be reissued against the new name. See `docs/certification_checklist.md` for the full audit process.

---

## Searchability

The `CandorCore` brand creates a unique, searchable identity distinct from earlier Candor-named projects (e.g., indutny's 2012 JavaScript compiler).

- GitHub repo: `candor-core/candor`
- Domain: `candorcore.dev` or `candor-core.dev`
- Social handles: `@candorcore`
- Search phrase: `"CandorCore programming language"`

---

## Examples

| Module | Tier | Status |
|--------|------|--------|
| `cc-collections` | Core | Official collections stdlib |
| `ccPar-Nvidia-tensor` | Partner | Nvidia tensor support |
| `ccPar-LLVM-backend` | Partner | LLVM official integration |
| `ccMod-alice-fastmath` | Community | Uncertified community math lib |
| `ccMod-alice-fastmath` (certified) | Community + cert | Paid audit passed, no Core relationship |
| `cc-wasm` | Core | Official WebAssembly runtime layer |

---

*The name is not a badge. It is a declaration.*
