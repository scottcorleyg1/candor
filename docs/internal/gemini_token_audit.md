# Candor Token Density Audit — Request for Independent Review

**Prepared for:** Gemini  
**Prepared by:** Scott W. Corley, with Claude  
**Date:** 2026-04-25  
**Purpose:** Independent audit of methodology, measurement integrity, and interpretation of Candor's token density claims

---

## What We're Asking You to Audit

We have made several public claims about token savings in the Candor programming language. We want independent verification of:

1. **Measurement methodology** — are we measuring what we say we're measuring?
2. **Arithmetic** — are the percentages correct given the raw numbers?
3. **Interpretation** — are we framing the results honestly? Are there misleading comparisons?
4. **Corpus selection** — is the corpus representative, or cherry-picked?
5. **Missing context** — what are we not accounting for that we should be?

We are specifically NOT asking you to validate the language design decisions, only the empirical claims.

---

## The Claims We're Making

### Claim 1 — Keyword alignment
> "36 of 37 core Candor keywords = 1 BPE token each against claude-sonnet-4-6."

**Raw data source:** `benchmarks/tokenizer/results/2026-04-23_claude-sonnet-4-6_3.json`  
**Tool:** Anthropic `count_tokens` API, baseline overhead subtracted  
**One failure:** `refmut` = 3 tokens

### Claim 2 — `?` operator savings
> "`?` saves 83% per propagation site vs full match syntax."

**Measurement:** Single `?` = 4 tokens total (the operator + the expression around it).  
Full match equivalent = 24 tokens: `match expr { ok(v) => v   err(e) => return err(e) }`.  
Savings: (24-4)/24 = 83%.

### Claim 3 — Function-level savings (constructed example)
> "60% fewer tokens on a common IO function in Agent Form vs Verification Form."

**Verification Form (106 tokens):**
```candor
fn process(path: str) -> result<str, str> effects(io) {
    let f = match open(path) { ok(v) => v   err(e) => return err(e) }
    let s = match read(f)  { ok(v) => v   err(e) => return err(e) }
    let r = match parse(s) { ok(v) => v   err(e) => return err(e) }
    return ok(r)
}
```

**Agent Form (42 tokens):**
```candor
fn process(path: str) -> ?str io {
    let f = open(path)?
    let s = read(f)?
    let r = parse(s)?
    return ok(r)
}
```

Savings: (106-42)/106 = 60.4%.

### Claim 4 — Corpus measurements (4 real programs)
Three variants measured against 4 real Candor programs:

| Variant | What it applies | Mean savings |
|---|---|---|
| v1 — signature shorthand only | `->?T io` vs `-> result<T,str> effects(io)` | 24.0% ± 6.5% |
| v2 — signature + `?` (drop error context) | Above + `?` for all propagating errors | 31.0% ± 6.5% |
| v3 — signature + `?|f` (keep error context) | Above with error context preserved via transform | 25.8% ± 7.7% |

**Programs measured:**
- `log_filter.cnd` (103 lines) — file I/O, pure filtering, error handling
- `word_stats.cnd` (91 lines) — pure stats computation, one effectful read
- `config.cnd` (88 lines) — key=value parser, pure helpers, one effectful load
- `pipeline.cnd` (134 lines) — CSV processing, parse → validate → summarize

Raw data: `benchmarks/tokenizer/results/2026-04-25_corpus_*.json`

---

## Methodology

### Token counting
All measurements use the Anthropic `count_tokens` API against `claude-sonnet-4-6`.  
Request format: `messages=[{"role": "user", "content": "X " + text}]`  
Baseline subtracted: the token count of `"X"` alone (8 tokens overhead).  
Net tokens = `response.input_tokens - baseline`.

**Why this is accurate:** `count_tokens` uses the same tokenizer that Claude uses during inference. BPE tokenization is deterministic — these are not estimates.

### Agent Form definition
Agent Form uses compact syntax that is currently spec-defined but not yet fully parsed by the compiler (the `->?T io` return type shorthand). The text is fed to the tokenizer directly. The `?` operator and `?|f` operator ARE fully implemented and compilable.

**Known gap:** The v1/v3 Agent Form files include `->?T io` syntax that the compiler does not yet parse. They are measurement-only. The compiled equivalents use `-> result<T,str> effects(io)`. This is disclosed in each file header.

### Corpus selection
The 4 programs were chosen because they are the canonical examples cited in M14's definition-of-done. They were written before the benchmark was run — not selected to maximize savings numbers. They are the programs the language is designed for.

---

## What the Numbers Mean

The v1/v2/v3 distinction is important for honest interpretation:

- **v1 (24%)** is what you get today from Agent Form with only signature shorthand. No body changes.
- **v2 (31%)** is the maximum savings if you drop error context messages entirely and use bare `?`. This sacrifices error quality.
- **v3 (26%)** is the realistic middle ground using `?|f` to preserve error context. This is closer to v1 than v2 because the error helper functions add tokens back.

The 60% constructed example holds because it uses 3 `?` sites with no error transformation — valid for programs where error context is not needed (internal computation chains). It is NOT representative of all programs.

**Honest summary of the corpus data:**
- Real programs get 24–31% savings depending on how aggressively you apply Agent Form
- The 60% figure applies specifically to function bodies with direct error propagation chains
- These are different measurements of different things; both are accurate for their stated scope

---

## What We Are Uncertain About

1. **N=4 is small.** The ±6.5% std is based on 4 programs. We don't know if this is representative of all Candor programs. More corpus data is needed before making strong statistical claims.

2. **Selection bias.** All 4 programs are IO programs with result types. Programs that are purely computational (no result<T,E>, no effects) would show smaller savings from Agent Form.

3. **The 60% function-level claim is a constructed example.** It was designed to illustrate the operators in their best-case form. Real programs have mix of error patterns — some propagate unchanged (use `?`), some transform errors (use `?|f`), some need full `must{}`.

4. **Token counts are model-specific.** All measurements are against claude-sonnet-4-6. Different models with different tokenizers would produce different numbers. We have not tested GPT-4o, Gemini, or other tokenizers.

5. **Agent Form compiler sugar is pending.** The `->?T io` shorthand is in the spec but not yet compiled. Today an AI writing Agent Form would write this shorthand, but the compiler would reject it. The token measurement is valid; the "same compiled output" claim requires the sugar to be implemented.

---

## Questions for the Auditor

1. Is the baseline subtraction methodology sound? (Subtracting the 8-token overhead of the API call itself.)

2. Are the percentage calculations correct given the raw token counts in the JSON files?

3. Is the distinction between "constructed example 60%" and "corpus 24–31%" being drawn clearly enough? Is the framing misleading?

4. Is N=4 sufficient to make a statistical claim with ±6.5% std? What would you require to consider it publishable?

5. Are there any measurement artifacts we're missing? (e.g., does the API count tokens differently for code vs prose? Does indentation affect BPE splitting?)

6. The claim "same program. same semantics. same compiled output" — is there a gap here given that Agent Form `->?T io` is not yet compiled?

---

## Raw Data Access

All measurement tools and raw JSON results are in the public repository:

- Construct benchmark: `benchmarks/tokenizer/token_analysis.py`
- Corpus benchmark: `benchmarks/tokenizer/corpus_benchmark.py`
- All results: `benchmarks/tokenizer/results/`
- Agent Form examples: `examples/agent_form/`, `examples/agent_form_v2/`, `examples/agent_form_v3/`
- Verification Form originals: `examples/*.cnd`

The tools re-run against any model with a valid API key. Results are timestamped JSON.

---

## One Additional Question

We are currently deciding between two error-handling strategies for Agent Form:

- **Option B:** Keep `result<T, str>` everywhere; define stdlib helper functions (`err_parse(e: str) -> str`); use `?|f` to apply them.
- **Option C:** Use typed error enums (`result<T, AppErr>`); `?|f` adapts error types at module boundaries.

The measured savings are nearly identical between B and C at the call site (both use `?|f`). The difference is structural: C allows callers to branch on error type; B does not.

Is there a measurement approach that would help distinguish which is better for AI-assisted code generation specifically? (e.g., does a typed error type give the AI more information when it reads a function signature?)

---

*Document prepared for independent technical audit. All claims are intended to be falsifiable. Please flag any that are not.*
