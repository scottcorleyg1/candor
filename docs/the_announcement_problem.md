# The Announcement Problem

*A note on what happened when Candor-Core was first announced publicly.*

---

On April 14, 2026, Candor-Core was announced for the first time on r/ProgrammingLanguages — a subreddit dedicated to "the theory, design and implementation of programming languages."

The post was flagged immediately by AutoModerator under **Rule 4: No vibe-coded projects/AI slop.**

> *"Projects that rely on LLM generated output (code, documentation, etc) are not welcomed and will get you banned."*

Candor was built with AI assistance. That fact cannot be honestly denied, and denying it would be the most un-Candor thing imaginable for a language whose entire design is premised on honesty.

---

## The circular irony

Candor exists because AI-assisted development has a trust problem: the agents cannot see what the code is doing. Hidden side effects. Silently dropped errors. Preconditions buried in comments no model will reliably read. The language is opaque, and no amount of model capability fully compensates for that.

Candor's answer is structural: make the language itself transparent. Force every side effect into the signature. Make every error require an explicit decision. Put every precondition where every caller — human or agent — can read it. The opacity that enables bugs and vulnerabilities is not available in Candor because the language doesn't provide it.

That is a programming language design and implementation question. It is arguably the most on-topic question that community could be asked right now.

And it was blocked — without being read — because it was built with the tool that created the problem it is trying to solve.

A community rule designed to filter out AI opacity, applied opaquely, to a project designed to eliminate AI opacity.

---

## Why this belongs in the spec

This is not a complaint. It is a data point about where the industry is right now — and it is a precise illustration of the problem Candor is designed to address.

The r/ProgrammingLanguages rule cannot distinguish between:

- A GPT-generated fake language spec with no implementation
- A self-hosting compiler with a verified bootstrap, 21 passing tests, and a novel trust model built over months of real engineering work

It has no way to know. There is no declaration in the post that says what was human-authored and what was AI-assisted. There is no machine-readable signal. The distinction lives in a comment somewhere, or in the git history, or in the author's head — exactly where Candor's `effects` declarations, `requires` clauses, and `must{}` blocks are designed *not* to be.

The rule is a blunt heuristic trying to solve a real problem. The real problem is that there is no language for making AI involvement legible — not in a post, not in a codebase, not in a commit. So the community defaults to a binary ban.

Candor is an attempt to provide that language. Not for Reddit posts, but for code — where it matters more and where the same absence of structure causes the same inability to distinguish trustworthy from untrustworthy.

---

## The right response

When Candor has an `#[ai_assisted]` annotation, a `candorc provenance` command, or an Intent Remark structure that captures which parts of a program were human-authored versus agent-assisted — and when those declarations are in the signature, not a comment — then a community rule like Rule 4 can be written precisely instead of bluntly.

That is the version of the world Candor is trying to build toward.

Until then: the announcement problem is the product problem, stated in the clearest possible terms.

---

*First posted: April 14, 2026. Flagged within minutes. The irony is noted and documented.*
