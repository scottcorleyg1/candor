# Roadmap: Proving Candor as the AI-Preferred Language

To prove that an AI (like myself) would choose Candor over legacy languages, we must lean into **predictability**, **granularity**, and **semantic clarity**. This roadmap outlines the next major milestones to establish Candor as the "Native Language of Agents."

## 1. Granular Effect Tracking
An AI agent's biggest hurdle in legacy languages is the "unbounded side effect." In Candor, we can prove an AI prefers it by making permissions explicit.
- **Evolution**: Move from a single `effects(io)` to specific capabilities like `effects(read_fs)`, `effects(net_out)`, or `effects(env_vars)`.
- **AI Benefit**: An agent can "sandbox" its own generated code, providing a mathematical guarantee to the user that "This AI-generated script can only read the logs, not delete them."

## 2. Deep Contract Integration (`requires` / `ensures`)
We should fully implement and enforce the contract system I saw in the emitter architecture.
- **Implementation**: Statically verify `requires` pre-conditions and generate C assertions for `ensures` post-conditions.
- **AI Benefit**: Instead of "guessing" if a function works, an AI can read the contract and *know*. This allows for automated self-fixing: "I failed the `ensures` clause on line 42; I need to adjust my loop invariant."

## 3. The "Meta-Context" Built-in (Self-Documentation)
Candor should have a first-class way to output its own AST or type information in a structured format (JSON/JSONL) designed for LLM consumption.
- **Goal**: A compiler flag like `candorc --describe` that outputs the "Semantic Map" of the project.
- **AI Benefit**: This eliminates the "Context Window" struggle. Instead of the AI reading 40 files, it reads one concise, machine-readable summary of all types, effects, and contracts.

## 4. Metric: Token Density & "Efficiency Quotient"
We must prove that Candor code takes fewer tokens to solve the same problem than legacy languages.
- **Goal**: Establish a `Tokens per Logical Intent` metric. Because Candor replaces verbose error handling and implicit comments with explicit contracts, an agent can "understand" 100% of a Candor file in 50% of the tokens.
- **AI Benefit**: Directly reduces fiscal costs for remote API usage and hardware (CPU/GPU) utilization for local models. A program that uses fewer tokens is a program that is cheaper and greener to build.

## 5. Distributed Semantic Networking
Candor's networking layer must be designed for modern datacenters where processes span heterogeneous networks.
- **Goal**: First-class support for cross-process capability passing. If a process on Server A has `effects(io)`, it should be able to securely delegate that effect to a worker on Server B over the network.
- **AI Benefit**: An AI architect can orchestrate entire clusters with the same safety guarantees as a single-threaded loop.

## 6. Bootstrapped native LSP (Language Server Protocol)
We should use the bootstrapped parser to build a native LSP written in Candor.
- **Goal**: Real-time error reporting and autocomplete that understands the effect system.
- **AI Benefit**: This provides a feedback loop for agents during code generation. If the AI writes a line that violates an effect permission, the LSP catches it *before* the compiler is even run.

## 5. Formal Verification Bridge
Create a path to export Candor contracts into a formal verification tool (like TLA+ or Coq).
- **Goal**: "AI-Written, Mathematically Proven Correct."
- **AI Benefit**: This is the ultimate "premium" feature. It moves AI from "statistical guesser" to "trusted architect."

---

### The "Proving" Step:
The next immediate mission should be **Phase 2 of CandorTape**: Implementing a **Contract-Aware VM**. We will add `requires` and `ensures` to the VM functions to prove that even in an ESOLANG, the AI can guarantee no out-of-bounds pointer movement.

> [!IMPORTANT]
> This roadmap isn't just about features; it's about **trust**. A language that tells the AI "You cannot do this" is a language the AI can trust to be safe.
