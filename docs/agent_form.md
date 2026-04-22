# Agent Form: Candor's Token-Efficient Projection

*Design document — April 2026*

---

## The Paradigm Shift

Programming language design has always optimized against a cost function. That cost function now has a new variable that didn't exist before: **token cost**.

Before AI as a force multiplier, the variables were:
- Human writing cost (man-hours)
- Human reading cost (man-hours)
- Compile time
- Runtime performance
- Memory usage

Token cost was zero. Every language ever designed, including Candor, was optimized against a cost function where token cost = 0.

That is no longer the cost function.

At AI scale — agentic coding, multi-agent pipelines, continuous AI-assisted development — tokens represent real money, real energy, real hardware capacity. The efficiency argument that applied to C (dense machine instructions, fast execution) now applies to the *generation pipeline* that produces the program. Hardware doesn't care if the code was right the first time or the thousandth time. But the AI infrastructure that generates it pays a real cost for every iteration.

This changes what "efficient language design" means.

---

## The Three-Audience Problem

Old world: a programming language serves two audiences.
- **Humans** — who write and read it
- **Machines** — which execute it

New world: there are three audiences with different optimal representations.
- **AI agents** — who generate code
- **Human verifiers** — who audit that the AI's output is correct
- **Machines** — which execute it

Candor was designed as if audience 1 and audience 2 were the same person doing the same task. They are not.

**The inversion:** Candor's explicitness is currently optimized for human *writing* — making intent visible so a human author doesn't make mistakes. But writing is now AI's job. The human task that remains is *verification*: auditing that what the AI produced matches intent. Those are different cognitive tasks.

Explicit, verbose Candor is correct for verification. The generation side — where tokens are spent — does not need to look like the verification side.

---

## Three-Layer Model

| Layer | Audience | Optimization target | When used |
|-------|----------|--------------------|-----------| 
| **Agent Form** | AI agents | Token density (lossless) | AI authoring, AI↔AI communication |
| **Verification Form** | Human auditors | Explicit semantics, auditability | Code review, commits, the `.cnd` source of record |
| **Machine Form** | Hardware | Execution efficiency | Runtime (LLVM IR, native binary) |

Verification Form is Candor Core as it exists today. Machine Form is already handled by the LLVM/C backend. Agent Form is the new layer.

**Agent Form is not an abbreviation.** Abbreviation implies lossy compression — something is dropped. Agent Form is a *lossless bidirectional projection*: every piece of information omitted from Agent Form is derivable from the remaining content via deterministic rules. The expansion from Agent Form → Verification Form produces exactly one valid result. If any expansion rule can produce two valid outputs, the omission is disqualified.

This preserves AXIOM-001 (One Meaning Per Expression). The rules are explicit, auditable, and part of the language spec. There is no magic.

---

## Token Density: The Right Metric

Token density is not tokens-per-line. It is **tokens per unit of correct output**.

A denser Agent Form that introduces ambiguity — any point where the AI must guess at an expansion — creates correction cycles. Each correction cycle costs tokens. A 30% compressed syntax that causes a 10% error rate may cost *more* total tokens than the uncompressed form, because errors compound: the AI regenerates, the verifier re-audits, the pipeline re-runs.

The design target for Agent Form is therefore: **maximum compression while preserving zero ambiguity in the expansion rules.**

Candor's explicitness already pays dividends here. The token_density_proof.md demonstrates that Candor signatures carry 3.4× more semantic information per token than Rust (the nearest competitor) because purity, effects, and error handling are encoded in signatures. An AI reading Candor needs fewer body tokens to understand a function's contract.

Agent Form extends this logic to the generation direction: the AI should be able to *write* programs with maximum information density per token, while the expansion to Verification Form handles structural ceremony automatically.

---

## The Compression Floor

The compression floor is the information-theoretic minimum for a Candor program: the set of tokens that cannot be omitted without ambiguity.

Every token in a Candor program is either:
- **Irreducible (I)**: The author must specify it. Given the rest of the program, there is more than one valid value, or no valid value at all. Example: a function name, a parameter type, `pure`, `effects(io)`.
- **Derivable (D)**: Given the surrounding program, exactly one value is valid. Example: the `{` that opens a function body (there is no other valid token at that position), the `->` separator between parameters and return type.

Agent Form omits all Derivable tokens. The expansion rules reconstruct them. The AI only authors Irreducible tokens.

See [token_compression_floor.md](token_compression_floor.md) for the empirical analysis.

---

## Relationship to Existing Work

- **HIP_VISION.md** defined the Intent → AI Agentic → Candor Core → Hardware layering. Agent Form is the formalization of the "AI Agentic Layer" in that model: it is the concrete syntax that layer uses.
- **M7.6 agent-json** demonstrated 33× token reduction on the *output* side (compiler diagnostics to AI). Agent Form applies the same principle to the *input* side (AI generating Candor programs).
- **EFFECTS-001 / token_density_proof.md** established that Candor signatures are information-dense. Agent Form builds on that foundation by making the structural ceremony around those signatures compressible.
- **L0-AXIOMS.md** is not threatened by Agent Form. The axioms apply to Verification Form (the committed `.cnd` source). Agent Form is a pre-commit authoring representation; the compiler never sees it directly — only the expanded Verification Form is compiled.

---

## Design Principles for Agent Form

1. **Omit only what is uniquely reconstructable.** If an expansion rule has one output, the token can be omitted. If it has more than one, it cannot.

2. **Preserve all semantic annotations.** `pure`, `effects(...)`, `result<T,E>`, `must{}` patterns — these are Irreducible. They are Candor's differentiation. Omitting them defeats the purpose.

3. **Exploit the tokenizer's existing vocabulary.** BPE tokenizers assign single tokens to common English words. `pure`, `let`, `fn`, `ok`, `err` are likely single tokens already. Structural punctuation (`{`, `}`, `->`, `=>`, `<`, `>`) each consume a token. Compression gain comes primarily from eliminating punctuation and repeated keywords, not from shortening identifiers.

4. **Positional convention over keyword markers.** Where keyword markers exist only to delimit structure (not to carry meaning), positional rules can replace them. Example: if the expansion rule for a parameter list always produces `(name: type, ...)`, the `(`, `:`, `,`, `)` tokens are all Derivable.

5. **Agent Form is write-optimized; Verification Form is read-optimized.** They serve different cognitive tasks. A human auditor should never need to read Agent Form. A commit should always contain Verification Form.

---

## Open Questions

- What is the empirical compression floor percentage? (see analysis in token_compression_floor.md)
- Should Agent Form be a formal syntax with a grammar, or a well-defined transformation on a parsed AST?
- How does Agent Form interact with the `#intent` directive from HIP_VISION? (Intent blocks may reduce required body tokens further — the AI specifies intent, the body is AI-generated and not human-authored)
- What tooling enforces the Agent Form → Verification Form expansion? (`candorc expand`? A pre-commit hook?)
