# I measured every keyword in my programming language against Claude's tokenizer. Here's what I found.

*Tags: #programming #ai #languages #compilers*

**TL;DR** — Built a systems programming language optimized for AI token efficiency. Measured every keyword against Claude's real tokenizer. 36/37 keywords = 1 token each. The `?` operator eliminates 83% of error-handling boilerplate per site. A complete IO function goes from 106 tokens to 42 — same program, same output. Also covers machine-verifiable purity in LLVM IR and why `|>` costs tokens but stays in anyway.

---

I want to be upfront about something: I'm not a professional programmer. I'm an automation engineer. I've written scripts and code on and off throughout my career — enough to get things done, not enough to call it my day job.

What changed recently is agentic AI. I started using AI agents to help automate parts of my work and the efficiency gains were real and immediate. That experience sent me down a rabbit hole I didn't expect.

The rabbit hole didn't start with programming languages. It started with human language — specifically, information density in spoken communication. I was reading research on how much semantic content different languages pack into a given unit of speech. Some languages are denser than others. The information-per-syllable ratio varies significantly and in measurable ways. That got me thinking: if we can measure information density in spoken language, what does that look like for programming languages? And more specifically — what does it look like for the tokens an AI actually processes?

From there the question narrowed fast. AI agents were helping me write code. But I kept watching them make a particular class of mistake — not syntax errors, those are boring. The subtler ones. Hidden side effects. Silently swallowed errors. Preconditions buried in a comment the model will never find when it needs them. The code was syntactically correct and semantically wrong, and the language gave the agent no way to know better. The ambiguity was structural.

So I started building Candor. Not because I had a plan to build a programming language, but because the rabbit hole bottomed out there.

---

## The idea: reduce ambiguity, reduce cost

The original goal was simple: make a language where neither humans nor AI agents can hide what code does. Every side effect declared. Every error handled. Every precondition machine-readable. If the language is unambiguous for a human reviewer, it is unambiguous for the model writing it — for the same reasons.

What I didn't anticipate — and what became obvious the moment I started using AI agents to build the compiler itself — is that ambiguity and token cost are the same problem from two different angles.

When a language forces you to write 24 tokens of boilerplate to propagate an error, those 24 tokens carry zero semantic information. The model has to read through them to learn one thing: "if this fails, return the error." Every one of those tokens costs compute. Every one of them costs electricity. Every one of them is GPU cycles and memory bandwidth and heat. At the scale AI-assisted development is moving toward — agentic loops, multi-model pipelines, continuous AI-driven iteration — those costs compound into something real.

That's when the token efficiency question moved from "nice to have" to the center of the design.

---

## I measured it

I used the Anthropic `count_tokens` API against `claude-sonnet-4-6` — the same tokenizer Claude actually uses when processing code. Not an approximation. Real API calls, baseline overhead subtracted, results saved as timestamped JSON.

I measured every keyword in Candor. Every operator. Every common signature pattern.

**36 of 37 core keywords = 1 BPE token each.** That's not luck — I verified each one before committing to it. The one failure is `refmut` (3 tokens), which has an Agent Form alias that reduces the common case by 33%.

**`pure` — the most important annotation in the language — is 1 token.** Free.

Then I measured what the `?` propagation operator actually saves:

```candor
// This is what error propagation looks like in full syntax — 24 tokens
match open(path) { ok(v) => v   err(e) => return err(e) }

// This is what it looks like with ? — 4 tokens
open(path)?
```

| Scenario | Full syntax | With `?` | Saved | Savings |
|---|---|---|---|---|
| Single propagation site | 24 tok | 4 tok | 20 | **83%** |
| 3 sites — typical IO function | 72 tok | 14 tok | 58 | **81%** |
| 5 sites — complex pipeline | 120 tok | 24 tok | 96 | **80%** |

At 5 propagation sites: **96 tokens eliminated from a single function.** Every one of those 96 tokens was routing boilerplate. No signal. Pure overhead.

---

## The complete function comparison

Here's the same IO function in both forms, measured end to end:

```candor
// Full syntax (Verification Form) — 106 tokens
fn process(path: str) -> result<str, str> effects(io) {
    let f = match open(path) { ok(v) => v   err(e) => return err(e) }
    let s = match read(f)  { ok(v) => v   err(e) => return err(e) }
    let r = match parse(s) { ok(v) => v   err(e) => return err(e) }
    return ok(r)
}

// Agent Form — 42 tokens
fn process(path: str) -> ?str io {
    let f = open(path)?
    let s = read(f)?
    let r = parse(s)?
    return ok(r)
}
```

**60% fewer tokens. Same program. Same semantics. Same compiled output.**

The transformation from Agent Form to full syntax is mechanical and one-pass. `?T` expands to `result<T, str>`. `io` expands to `effects(io)`. `expr?` expands to the full match block. No inference. Every rule is a substitution.

This means AI writes the dense form. Humans review the full form. Both are the same program.

---

## What the savings mean in practice

**Frontend:** If you're using AI in a code generation workflow — IDE assistant, AI review bot, agent writing components — every token the language doesn't need is context the model can spend on your actual problem. A 60% reduction in function-level token overhead means the same context window fits roughly 2.5× as many function definitions. That's not a developer-experience improvement. That's a capability increase.

**Backend:** At 100 concurrent AI requests on a 70B model, each token in the KV cache costs approximately 327 KB of VRAM. A 56% savings in function signature tokens across a coding context frees significant memory per request — which means more requests per GPU, lower latency, or smaller hardware for the same throughput.

**Real-world electrical and mechanical:** This is the part people don't talk about enough. GPU compute is not free. It is electricity, heat, cooling systems, mechanical stress on hardware. A token that doesn't need to be processed doesn't burn a watt. At the scale AI is scaling to — inference farms, always-on coding agents, continuous pipeline generation — token efficiency is energy efficiency. A language that eliminates 80% of error-routing boilerplate per function isn't just cleaner. It is a measurably lower power draw per unit of work.

---

## Trust and verification: the other side of the coin

Token efficiency was the measurement that surprised me. But it's not why I started building Candor.

The deeper problem is trust. People are split on AI-generated code — not because AI writes bad syntax, but because they can't see what the code is doing. Hidden side effects. Silently swallowed errors. Preconditions buried in a comment the agent never reads. The fear is reasonable. And reassurance doesn't answer it. Structure does.

Candor is built so that neither a human nor an AI agent can hide what a function does.

**Every side effect is declared and compiler-enforced:**

```candor
fn send_report(data: str) -> unit effects(io, net) {
    write_file("log.txt", data)
    http_post("https://api.example.com/report", data)
    return unit
}
```

`effects(io, net)` is not a comment. It is enforced by the type checker across the entire call graph. A pure function that tries to call `send_report` is a compile error. An AI agent cannot write a function that silently touches the network without declaring it. A human reviewer sees it in the signature without reading the body.

**Every error must be handled — silence is a compile error:**

```candor
let config = load_config("settings.cnd") must {
    ok(cfg) => cfg
    err(e)  => {
        print(str_concat("error: ", e))
        return unit
    }
}
```

Discarding a `result<T,E>` without handling both arms is rejected by the compiler. There is no way for AI-generated code to silently swallow a failure.

**And then there's the LLVM layer.**

This is the part that goes deeper than type checking. Candor's `pure` annotation doesn't just tell the compiler to check the call graph. It emits a machine-verifiable attribute in LLVM IR:

```llvm
; pure function — LLVM's own verifier rejects this if it contains a load or store
define i64 @add(i64 %a.in, i64 %b.in) memory(none) nounwind {
  ...

; effectful function — bare define, no machine-verifiable purity claim
define ptr @process(ptr %path.in) {
  ...
```

`memory(none) nounwind` is not a Candor invention. It is an LLVM attribute that LLVM's own verifier enforces independently of the Candor compiler. If a `pure` function emits a load or store at the IR level, LLVM rejects it. The guarantee travels all the way from source to hardware without requiring trust in any single tool.

The transparency chain looks like this:

```
AI writes:       fn add(a: i64, b: i64) -> i64 pure { return a + b }
                        ↓ EFFECTS-001 (type checker)
Candor checks:   pure callers may not call effectful code
                        ↓ emit_llvm
LLVM IR:         define i64 @add(...) memory(none) nounwind { ... }
                        ↓ LLVM verifier
Hardware:        guaranteed — no memory side effects
```

No step in that chain requires trust. Each layer is independently auditable. That is the goal: a language where the safety guarantees aren't words in a README, they're machine-checkable at every level.

---

## The honest `|>` result

I measured the pipeline operator too, and I'm including this because I think honesty about negative results matters:

`|>` is 2 tokens. BPE splits `|` and `>` separately. It costs tokens compared to nested calls:

| Pattern | Nested | Pipeline | Delta |
|---|---|---|---|
| 3-step, snake_case | 14 tok | 16 tok | **-2** |
| 5-step, snake_case | 23 tok | 26 tok | **-3** |

I kept it anyway. The value is structural, not arithmetic: `x |> parse |> filter |> render` is linear. `render(filter(parse(x)))` requires parsing depth before understanding sequence. Transformer attention is sequential — linear structure matches how a model reads code. That's worth 2–3 tokens per pipeline. But it's a reasoning benefit, not a token savings, and I document it as such.

If a design decision doesn't save tokens, you shouldn't claim it does.

---

## This was built with AI, for a world that uses AI

I want to credit the tools that made this possible, because it's relevant to the story. Claude and Gemini have been significant collaborators on this project — not just autocomplete, but architectural reasoning, debugging, and in some cases pushing back on my design decisions in ways that made the language better. The experience of building a language alongside AI is part of why the language exists. Working that way makes the gaps in existing languages visible fast.

Local models contributed too. GitHub Copilot. Several others. The whole point of Candor is that it should be a better surface for that kind of collaboration.

---

## Where to find everything

The benchmark tool, the raw JSON data, and the full methodology are all in the repo:

- Full benchmark report: [docs/token_benchmark.md](https://github.com/candor-core/candor/blob/main/docs/token_benchmark.md)
- Three Forms spec (Agent / Verification / Machine): [docs/three_forms.md](https://github.com/candor-core/candor/blob/main/docs/three_forms.md)
- Measurement tool: `benchmarks/tokenizer/token_analysis.py`
- Raw results: `benchmarks/tokenizer/results/2026-04-23_claude-sonnet-4-6_3.json`
- Source: [github.com/candor-core/candor](https://github.com/candor-core/candor)

The tool re-runs against any model. I'll post updated baselines when major models ship — measurements shift as tokenizers evolve, and that's by design.

I may be off track with parts of this. I hope it inspires something useful regardless.

— Scott W. Corley
