# Contributing to CandorCore

Welcome. Before anything else, one principle:

> **Code should mean exactly what it says, and say everything it means.**

This is not a style guideline. It is the load-bearing idea behind every decision in this project. If you understand it, most contribution decisions will make themselves. If a change makes behavior less visible — to a human, to an AI agent, to a reviewer — it does not belong in CandorCore regardless of how technically correct it is.

---

## The Trust Principle

People are increasingly afraid of AI-generated code. Not because AI writes bad syntax — because they cannot see what the code is doing. CandorCore's answer to that fear is structural, not rhetorical:

- Every side effect is declared and compiler-enforced
- Every error must be handled explicitly — silence is a compile error
- Every precondition is machine-readable, not just a comment
- The ecosystem names tell you who wrote a module and whether it was audited
- The runtime refuses to execute flagged code without explicit developer consent

**Every contribution to CandorCore is a contribution to this guarantee.** A PR that adds implicit behavior — even useful, well-intentioned implicit behavior — weakens the foundation everything else is built on. This is why the trust principle is the first thing in this document, not a footnote.

---

## Ways to Contribute

### Compiler Bugs
Open an issue with a minimal `.cnd` reproducer. Include:
- The source that triggers the bug
- The actual output (generated C, error message, or behavior)
- The expected output
- Which stage fails: lex / parse / typecheck / emit / runtime

See `docs/known_compiler_bugs.md` for the current open bug list. Check there first.

### Language Design Feedback
Read `docs/syntax_and_builtins.md` and the spec before proposing changes. Design issues that are worth raising:
- A construct that requires implicit knowledge to understand
- An error that is silently swallowed somewhere
- A case where the compiler accepts code whose behavior is ambiguous
- A missing effect, contract, or declaration that would make intent more visible

Design issues that will be declined:
- Convenience features that introduce implicit behavior
- Syntactic sugar that hides what is happening
- Anything that makes the language "feel more like X" at the cost of explicitness

### New Builtins or Standard Library Additions
Every builtin must:
- Declare its effects in its signature (`effects(io)`, `effects(network)`, etc.)
- Return `result<T, str>` for any operation that can fail — no silent failures
- Have a precondition (`requires`) for any input constraint
- Be testable with a case in `tests/cases/`

### Performance and Benchmarking
See `tests/bench/` for the existing benchmark suite. Contributions that add:
- New benchmark programs in Candor and a reference language
- Compiler throughput measurements
- Runtime memory usage baselines

are welcome. Results go in `tests/bench/results.md`.

### Agent Evaluation
See `tests/agent_eval/` for the evaluation framework. Contributions that:
- Add new task cases that test Candor-specific constructs
- Record agent eval results in other languages for comparison
- Document where agents succeeded or failed and why

are directly valuable to the project's core thesis.

### Documentation
Documentation contributions must meet the same standard as code:
- Do not describe behavior that is not yet implemented as if it is
- Do not use vague language where precise language is possible
- If you are documenting a limitation or known bug, say so plainly

---

## Pull Request Checklist

Before submitting a PR, verify:

- [ ] The change does not introduce implicit behavior
- [ ] Any new function or builtin declares its effects
- [ ] Any new operation that can fail returns `result<T, E>` — not a silent default
- [ ] Any input constraints are expressed as `requires` clauses, not just comments
- [ ] A test case in `tests/cases/` covers the new behavior
- [ ] `tests/run_tests.sh` passes with 0 failures
- [ ] If the change touches `emit_c.cnd`, bootstrap idempotency is verified:
  `diff stage2.c stage4.c` must produce 0 lines

**If your PR is declined on trust principle grounds**, the review will explain specifically what implicit behavior was introduced and why it conflicts with the design. This is not a stylistic judgment — it is a design boundary.

---

## Commit Style

- One logical change per commit
- Message describes *why*, not just *what*
- Reference bug numbers where applicable (`Fix Bug 11: integer match arm_cond`)

---

## Code of Conduct

This project operates under one rule: be direct. Disagree with designs openly, in issues. Criticize code, not people. If you think a decision is wrong, say so with a specific argument. Candor the language values explicitness — so does this project as a community.

---

## The Bootstrap

CandorCore's compiler is written in Candor. Before submitting changes to `src/compiler/`:

1. Read `docs/AI_GUIDE.md` — it contains the exact build sequence and known GCC constraints
2. Any change to `emit_c.cnd` requires a full bootstrap verification
3. `lexer.exe` in the repo is the current self-hosting binary — do not replace it without verified idempotency

The bootstrap is the strongest proof that the language works. Protecting it is everyone's responsibility.

---

## License

By contributing to CandorCore, you agree that your contributions will be licensed under the [Apache License, Version 2.0](LICENSE).

---

*Thank you for helping make code more honest.*
