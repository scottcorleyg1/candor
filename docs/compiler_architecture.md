# Candor Compiler Architecture

This document serves as the self-reinforcing schema of the Candor compiler. It maps strictly how code flows from text to execution.

## 1. High-Level Pipeline
Candor follows a strict sequential phase architecture. There is NO intertwined parsing and type-checking.

1. **Lexical Analysis** (`lexer.cnd`) -> Returns a `vec<Token>`
2. **Parser** (`parser.cnd`) -> Returns a complete `AST` (`ParsedFile`).
3. **Type Checking** (`typeck.cnd`) -> Decorates the AST. Resolves symbols. Fails on invalid types or missing `must{}` blocks.
4. **Code Emission** (`emit_c.cnd` or `emit_llvm.cnd`) -> Converts the typed AST directly to C code or LLVM IR.

## 2. Emission Architecture (The 5-Pass C Emitter)

A frequent source of bugs when compiling Candor to C is **Pass Ordering**. Because Candor allows functions and structs to be declared in any order, C requires forward declarations and specific type completions. 

The emitter (`emit_c.cnd` and the Go counterpart `emit_c.go`) uses a strictly enforced 5-pass mechanism. **Do not deviate from this ordering when adding new data structures.**

### Emission Passes Order:
**Pass 1: Forward Declarations**
- `typedef struct X_s X;` for structs.
- `typedef struct _en_X X;` for enums.

**Pass 2: Generic Pointer Typedefs**
- **Pass 2a**: `vec<T>` forward typedefs (`typedef T* _CndVec_T`). Unconditionally happens here since it's just a pointer.
- **Pass 2b**: `map<K,V>` forward typedefs (`typedef struct _CndMap_K_V...`) and map struct bodies (since they only hold pointers to buckets).

**Pass 3: Concrete Types and Struct Bodies**
- **Pass 3a**: Enum bodies. Must occur before structs, since a struct might contain an enum by-value.
- **Pass 3b**: Struct + Const bodies.
- **Pass 3c**: `vec<T>` push/pop helpers. Placed here so elements passed by-value have completed types.
- **Pass 3d**: `map<K,V>` entry struct bodies and helpers. 

**Pass 4: Results & Options**
- `result<T,E>` and `option<T>` struct bodies. Must occur after Pass 3 so `T` and `E` are complete.

**Pass 4.5: Function Prototypes**
- Forward declarations of all functions (`fn name(T arg) -> U;`) to enable mutual recursion.

**Pass 5: Function Implementation**
- Emission of all function bodies.

### Understanding `auto _t` and `must` bindings
Candor heavily uses GCC's `__extension__ ({ ... })` to transpile Candor blocks/expressions into C expressions. When parsing `must` bindings with no explicit variable, the compiler generates a unique throwaway temporary (e.g. `_cnd1`, `_cnd2`). 

*(See `docs/known_compiler_bugs.md` for historical bugs regarding variable redefinition during `must` expression evaluation).*
