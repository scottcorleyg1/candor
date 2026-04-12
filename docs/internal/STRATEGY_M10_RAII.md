# Candor Strategic Roadmap: RAII & Edge Fortification

Following the successful M9.19 bootstrap and performance benchmarking, this document explicitly defines the strategy to cement Candor as the pinnacle AI-Systems programming language. 

We will introduce a Resource Acquisition Is Initialization (RAII) memory lifecycle, multiply our existing structural strengths, and systematically eliminate our known architectural bottlenecks.

---

## 1. Defining the Candor "RAII" Memory Lifecycle

Currently, Candor leans on C's `malloc` and `realloc` for dynamic structures (`box`, `vec`, `str`), but lacks automatic cleanup, resulting in continuous memory leaks in long-running processes. We deliberately refused to add a Garbage Collector to maintain our C-level performance. 

To achieve memory safety without a GC, we will implement **Scope-Based RAII and Move Semantics**:

1. **Move Semantics (No Implicit Copying):**
   * Currently, Candor passes structs by value. If a struct contains a `vec<T>` pointer, two variables end up holding the same pointer.
   * **Fix:** The compiler will enforce *Move Semantics* for heap-allocated types (`box<T>`, `vec<T>`, `str`). Assigning `let b = a` transfers ownership. `a` can no longer be used. If you want two copies, you must explicitly call `let b = vec_clone(a)`.
2. **Explicit Borrowing:**
   * To pass a heap type to a function without dropping ownership, the developer must already use `ref<T>` or `refmut<T>` (which perfectly aligns with what we currently have).
3. **Scope-Based Dropping:**
   * During the `emit_c` phase, the compiler will track variable lifetimes within `{ }` scopes. 
   * When an owned heap-type reaches the end of its lexical block, `emit_c` will automatically inject `Type_drop(&name);` under the hood. For `vec<T>`, this calls `free()` on the internal buffer. 

*Result:* Candor becomes fully memory safe and leak-free while executing with deterministic `free()` calls natively on the metal. 

---

## 2. Multiplying Our Strengths

We identified three massive competitive strengths: **Agent Code-Gen Viability**, **Raw Execution Speed**, and **Lightweight Data Structures**. Here is how we make them untouchable:

### A. Agent Zero-Shot Capability (M7 Evolution)
We proved AI agents succeed with Candor because the rules are simple and strict. 
* **The Plan:** Implement the **M7 Layer**. We will add `#mcp_tool` and `#[intent(desc)]` macro directives. The compiler will automatically parse these and emit `tools.json` and `intent.json` metadata files. Candor will become the very first programming language where an LLM agent natively understands the entire API context directly from the compiler's output, avoiding hallucination entirely.

### B. Raw C-Execution Speed (M11 Evolution)
Candor matches handwritten C code linearly through the GCC compiler.
* **The Plan:** We will launch the native LLVM Backend. By bypassing GCC entirely and emitting raw LLVM IR, we can introduce **M11 Tensor & SIMD primitives**. Native `f16`/`bf16` types and intrinsic dot-product SIMD instructions will allow Candor to out-benchmark C and C++ on native machine-learning inference calculations (crucial for vLLM architectures). 

### C. Data Structure Dominance (M12 Evolution)
Our maps are structurally faster than Python's dictionaries because of our C-macro simplicity. 
* **The Plan:** Rather than adding complex OOP libraries, we double down on native systems data. We will introduce `arc<T>` (Atomic Reference Counting) and immediately build `trie<K,V>` and HNSW (Vector Database) structs directly into the standard library to serve natively highly-concurrent inference queries.

---

## 3. Eliminating Our Structural Weaknesses

We identified three functional gaps: **Memory Leaks**, **String Processing Boundaries**, and **Memory Guardrails**. Here is how we destroy them:

### A. Fixing Memory Leaks
* **The Plan:** As defined in Phase 1, the rollout of the RAII scope-analyzer. This will be the immediate focus for improving `emit_c.cnd`. We will implement `str_drop`, `vec_drop`, and `box_drop` in `_cnd_runtime.h`, and modify `emit_c.cnd` to hook them in at the tail of block scopes.

### B. Resolving $O(N^2)$ String Concatenation 
Candor strings are immutable. `str = str_concat(str, "a")` forces endless `malloc` thrashing. 
* **The Plan:** We will introduce a standard library type `str_buf` (or `StringBuilder`). Structurally, it is just a `vec<u8>` under the hood, enabling highly optimized, pre-allocated dynamic appending. A final `.freeze()` method will cast the `vec<u8>` safely back into an fast, immutable `str` without allocating new memory. 

### C. Securing Panic Bounds
Currently, a rogue `arr[99]` on a 2-element array triggers a raw C Segfault.
* **The Plan:** We will introduce a `--safe` compilation flag. When enabled, `emit_c.cnd` will prepend every index-hit natively with `if (_idx >= _len) { _cnd_panic("out of bounds"); }`. Later, in M6 (Formal Verification), we will use SMT solvers to prove loops are safe mathematically at compile-time so those checks can be stripped silently!
