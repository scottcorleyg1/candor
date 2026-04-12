# M11.1 Neural-Engine Types Blueprint

*Date: April 2026*

## The Goal
To make Candor officially an AI systems language by bringing deterministic neural primitives down directly to the hardware via LLVM/GCC pipeline without bloated software wrappers.

## Phase 1: The Lexical & AST Groundwork
*   **Action:** Add `f16` (IEEE 754 half-precision) and `bf16` (Brain Float 16) into Candor's primitive type definitions. 

## Phase 2: Type Symmetry & Arithmetic Boundaries
*   **Action:** Teach the Candor Typechecker how to strictly handle `f16` and `bf16` math.
*   *Strict Guardrails:* 
    *   `f16 + f16` is legal. 
    *   `bf16 + bf16` is legal. 
    *   `f16 + f64` throws a strict compile-time error. Candor will never implicitly upcast or downcast floating-point types behind the developer's back. 
    *   Casting requires explicit capability syntax: `x as f64`.

## Phase 3: The GNU C Hardware Bridge (emit_c.cnd)
*   **Action:** Natively translate Candor's `f16` into GCC's raw `_Float16`, and `bf16` into `__bf16`. 
*   *The Result:* When compiled, GCC will deploy native CPU SIMD instructions to execute neural logic identically to how CUDA/TPUs behave.

## Phase 4: Output Verification
*   **Action:** Run robust unit benchmarks testing linear AI scaling metrics (`neural_bench.cnd`). 
*   Compile exactly via the `run_bench.sh` execution loop utilizing native Candor generation.
