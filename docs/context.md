# Candor Compiler — Session Context

> Use this file to bootstrap a new chat with full project context.
> Generated 2026-03-18 after commit `a9f5d54` (M10.1).

---

## What is Candor?

Candor is a statically-typed, effects-annotated systems language designed for
high-assurance AI/ML infrastructure. It compiles to C (primary) and LLVM IR
(secondary). The compiler is written in Go and lives entirely in
`compiler/` (a single Go module: `github.com/scottcorleyg1/candor/compiler`).

Key design goals:
- **Effects system** — functions declare `effects(io, net, gpu, ...)` or `pure`;
  the compiler enforces subset-checking at call sites
- **Capability tokens** — `cap Name` / `cap<T>` zero-size proof types for
  hardware-tier enforcement (M7.3)
- **Contracts** — `requires`/`ensures` clauses on functions; symbolic evaluation
  at compile time when args are constants (M6.1)
- **Structured concurrency** — `spawn { return expr }` → `task<T>` backed by
  pthreads; `.join()` → `result<T, str>` (M10.1)
- **Bootstrapping** — the compiler itself is being rewritten in Candor
  (`src/compiler/*.cnd`)

---

## Repository layout

```
d:/SWC/CandorSWC/
├── compiler/               # Go compiler (primary)
│   ├── go.mod              # module github.com/scottcorleyg1/candor/compiler
│   ├── main.go             # CLI: build / test / fmt / lsp / mcp / doc
│   ├── lexer/              # Tokenizer (token.go, lexer.go)
│   ├── parser/             # Recursive-descent parser (ast.go, parser.go)
│   ├── typeck/             # Type checker (typeck.go, types.go)
│   ├── emit_c/             # C backend (emit_c.go, emit_c_test.go)
│   ├── emit_llvm/          # LLVM IR backend
│   ├── doc/                # candorc doc --html (doc.go, doc_test.go)
│   ├── lsp/                # JSON-RPC 2.0 LSP server
│   ├── diagnostics/        # Error rendering with source snippets
│   ├── manifest/           # Candor.toml project manifest
│   └── tests/              # End-to-end integration tests
├── src/                    # Candor standard library + bootstrapped compiler
│   ├── std/                # std::io, std::math, std::str, std::os, etc.
│   └── compiler/           # Bootstrapped lexer/parser/typeck written in Candor
├── docs/                   # Design docs and roadmap
│   ├── roadmap.md          # Living milestone tracker
│   ├── specification.md    # Language spec
│   ├── language-reference.md
│   └── context.md          # THIS FILE
└── examples/               # Example Candor programs
```

---

## Compiler pipeline

```
source (.cnd)
  → lexer.Tokenize()         → []Token
  → parser.Parse()           → *parser.File (AST)
  → typeck.Check()           → *typeck.Result
  → emit_c.Emit()            → C source string   [default backend]
  → emit_llvm.Emit()         → LLVM IR string    [--backend=llvm]
  → CC / clang               → binary
```

All packages share a single Go module. No CGo.

---

## Key source files and their roles

### `compiler/lexer/token.go`
Defines all `TokenType` constants (iota). Keywords currently include:
`fn let return if else match loop break continue for struct enum extern pure
cap must move some none ok err true false and or not forall exists old in
effects requires ensures invariant assert module use mut secret reveal while
const impl as trait spawn`

Each keyword has a `Tok*` constant, an entry in `keywords` map, and an entry
in `tokenNames` map.

### `compiler/parser/ast.go`
All AST node types. Relevant recent additions:
- `CapabilityDecl` — `cap Name` top-level declaration
- `SpawnExpr` — `spawn { Body *BlockStmt }` expression node

Important expression nodes:
```go
LambdaExpr   // fn(params) -> RetType { body }
SpawnExpr    // spawn { stmts }  → task<T>
MatchExpr    // match expr { arms }
MustExpr     // expr must { arms }
BlockExpr    // { stmts } as expression (match/must arm body)
CastExpr     // expr as Type
```

### `compiler/parser/parser.go`
Recursive-descent. `parsePrimaryExpr` is the bottom of the precedence ladder.
`TokSpawn` → `parseSpawnExpr()` was added in M10.1.

### `compiler/typeck/types.go`
Type system:
```go
type Type interface { String() string; Equals(Type) bool }

// Concrete types:
*Prim          // unit, bool, str, i8..i128, u8..u128, f16, bf16, f32, f64, never
TypeVar        // generic placeholder T, R (transient during monomorphization)
*GenType       // Con + []Params: ref<T>, option<T>, result<T,E>, vec<T>,
               //   map<K,V>, set<T>, ring<T>, box<T>, arc<T>, secret<T>,
               //   cap<T>, task<T>
*FnType        // Params []Type, Ret Type
*StructType    // Name string, Fields map[string]Type
*EnumType      // Name, Variants []EnumVariant{Name, Fields []Type}
*TupleType     // Elems []Type
*CapabilityType // Name string  (zero-size proof token)
```

Primitive singletons: `TUnit TBool TStr TI64 TF64` etc. (all in `types.go`).

### `compiler/typeck/typeck.go`
The type checker. Key exported types:

```go
type LambdaInfo struct {
    Node         *parser.LambdaExpr
    Name         string   // _cnd_lambda_N
    Sig          *FnType
    Captures     []string
    CaptureTypes []Type
    CaptureByRef []bool
}

type SpawnInfo struct {
    Node         *parser.SpawnExpr
    Name         string   // _cnd_spawn_N
    ResultType   Type     // T in task<T>
    Captures     []string
    CaptureTypes []Type
    CaptureByRef []bool
}

type Result struct {
    ExprTypes       map[parser.Expr]Type
    FnSigs          map[string]*FnType
    Structs         map[string]*StructType
    Enums           map[string]*EnumType
    Lambdas         []*LambdaInfo
    Spawns          []*SpawnInfo
    TaskJoins       map[parser.Expr]Type    // CallExpr → T in task<T>
    MethodCalls     map[parser.Expr]string  // CallExpr → mangled C name
    CapabilityDecls []*parser.CapabilityDecl
    // ... GenericInstances, ImplDecls, TraitDecls, ConstDecls, etc.
}
```

`Check(file)` for single-file, `CheckProgram(files)` for multi-file with
module enforcement.

`tryMethodCall` handles both struct impl methods AND built-in `task<T>.join()`.
The caller pattern was fixed in M10.1 to propagate errors: `if mt != nil || err2 != nil`.

### `compiler/emit_c/emit_c.go`
C backend. Emitter struct holds:
```go
type emitter struct {
    sb            strings.Builder
    res           *typeck.Result
    retIsUnit     bool
    isMain        bool
    spawnTaskVar  string  // non-empty when emitting a spawn thunk body
    byRefCaptures map[string]bool
    emittedTypes  map[string]bool
    // ...
}
```

**Emission order in `emitFile`:**
1. `#include` headers (pthread.h added if `len(res.Spawns) > 0`)
2. Runtime helpers
3. Forward-declare structs/enums/capabilities
4. Vec/map/set/ring/result struct typedefs
5. Struct and enum bodies (topological order via `ensureTypeDependenciesEmitted`)
6. Vec/map/set/ring operation helpers
7. Fn-type typedefs
8. Extern fn forward declarations
9. Non-generic fn forward declarations + impl/trait-impl forward decls
10. Lambda helper functions (`emitLambdaFn`)
11. **Spawn task structs + thunk functions** (`emitSpawnThunk`) ← M10.1
12. Generic function instances
13. Module-level constants
14. Function bodies (impl methods, trait-impl methods)

**`cType` mapping (Type → C string):**
```
unit       → void
bool       → int
str        → const char*
i8..i128   → int8_t .. __int128
u8..u128   → uint8_t .. unsigned __int128
f16        → _Float16
bf16       → __bf16
f32        → float
f64        → double
ref<T>     → T*
vec<T>     → _CndVec_T (struct with _data/_len/_cap)
map<K,V>   → _CndMap_K_V
result<T,E>→ _cnd_result_T_E (struct with _ok/_ok_val/_err_val)
option<T>  → T* (null = none)
box<T>     → T*
arc<T>     → T* (refcount at ptr-8)
cap<Name>  → cap_Name (uint8_t typedef)
task<T>    → _CndTask_T* (struct with pthread_t/_result/_ok/_err)
fn(T)->R   → _cnd_fn_R_T (fat pointer struct with ._fn and ._env)
```

### `compiler/doc/doc.go`
`candorc doc --html` — scans `///` doc comments from raw source (pre-pass,
no lexer involvement), associates with next `fn`/`struct`/`enum` declaration.
`GenHTML([]FileDoc)` produces self-contained HTML.

---

## Language surface (syntax summary)

```candor
// Declarations
fn name<T: Trait>(param: Type) -> RetType effects(io) requires pred ensures pred { body }
struct Name { field: Type, ... }
enum Name { Variant, Variant(Type, ...), ... }
trait Name { fn method(self: ref<Self>, ...) -> RetType }
impl Name { fn method(self: ref<Self>, ...) -> RetType { body } }
impl Trait for Type { fn method(...) { body } }
cap Name                          // capability token declaration
const NAME: Type = expr
extern fn name(params) -> RetType effects(io)
module name
use module::Name

// Statements
let [mut] name [: Type] = expr
let (a, b) = tuple_expr
name = expr                       // requires mut
name.field = expr
coll[i] = expr
return [expr]
if cond { } [else { }]
match expr { Pat => body, ... }
loop { }   while cond { }   for x in coll { }
break   continue
assert expr
#[directive]                      // #test #export_json #intent #mcp_tool

// Expressions
spawn { stmts }                   // → task<T>; body must contain return expr
task_val.join()                   // → result<T, str>
expr must { ok(v) => e, err(e) => e }
match expr { Pat => body, ... }
fn(params) -> RetType { body }   // lambda
some(expr)   none   ok(expr)   err(expr)
forall x in coll : pred
exists x in coll : pred
old(expr)                         // inside ensures clause
expr as Type                      // numeric cast
[e, e, ...]                      // vec literal
(e, e, ...)                      // tuple literal (2+ elements)
Type::Variant(args)               // enum construction
```

---

## Effects system

```candor
fn pure_fn(x: i64) -> i64 effects [] { ... }       // pure
fn io_fn() -> unit effects(io) { ... }              // io effect
fn net_fn() -> unit effects(io, net) { ... }        // multiple
fn gpu_fn() -> unit effects(gpu) { ... }            // hardware tier
```

Known effects: `io net gpu storage mem async`

Subset checking: a function can only call other functions whose effects are a
subset of its own declared effects (unless the callee is pure).

### Capability tokens (M7.3)

```candor
cap Admin
fn privileged(tok: cap<Admin>) -> unit cap(Admin) { ... }
fn caller(tok: cap<Admin>) -> unit { privileged(tok) }  // OK: has cap<Admin>
fn bad() -> unit { privileged(???) }  // error: missing cap<Admin>
```

C emission: `typedef uint8_t cap_Admin;` (zero runtime overhead).

---

## Concurrency (M10.1)

```candor
fn main() -> unit {
    let x: i64 = 42
    let t: task<i64> = spawn { return x }   // captures x, starts pthread
    let r: result<i64, str> = t.join()       // blocks, frees task
    r must {
        ok(v)  => print_int(v)
        err(e) => print(e)
    }
    return unit
}
```

**C emission pattern:**
```c
// Task struct (per distinct T):
typedef struct _CndTask_int64_t _CndTask_int64_t;
struct _CndTask_int64_t {
    pthread_t _thread;
    int64_t   _result;
    int        _ok;
    const char* _err;
};

// Per-spawn context struct:
typedef struct { _CndTask_int64_t* _task; int64_t x; } _cnd_spawn_1_ctx;

// Thunk function:
static void* _cnd_spawn_1_fn(void* _raw) {
    _cnd_spawn_1_ctx* _ctx = (_cnd_spawn_1_ctx*)_raw;
    _CndTask_int64_t* _task = _ctx->_task;
    __auto_type x = _ctx->x;
    free(_ctx);
    _task->_result = x;   // from: return x
    _task->_ok = 1;
    return NULL;
}

// Spawn expression (GCC statement expression):
(__extension__ ({
    _CndTask_int64_t* _t = (_CndTask_int64_t*)malloc(sizeof(_CndTask_int64_t));
    _t->_ok = 0;
    _cnd_spawn_1_ctx* _ctx = (_cnd_spawn_1_ctx*)malloc(sizeof(_cnd_spawn_1_ctx));
    _ctx->_task = _t; _ctx->x = x;
    pthread_create(&_t->_thread, NULL, _cnd_spawn_1_fn, _ctx);
    _t;
}))

// Join expression:
(__extension__ ({
    _CndTask_int64_t* _jt = t;
    pthread_join(_jt->_thread, NULL);
    _cnd_result_int64_t_const_char_ptr _jr = {0};
    if (_jt->_ok) { _jr._ok = 1; _jr._ok_val = _jt->_result; }
    else { _jr._ok = 0; _jr._err_val = _jt->_err; }
    free(_jt);
    _jr;
}))
```

---

## Completed milestones (as of 2026-03-18)

| Milestone | Summary |
|-----------|---------|
| **v0.1** | Core language: primitives, structs, enums, generics, closures, effects, contracts, pattern matching, C emission |
| **M1–M2** | Compound assignment, tuple destructure, struct update, standard library (math/str/io/os/time/rand/path) |
| **M3** | Trait system: `trait`, `impl Trait for Type`, trait bounds, monomorphization |
| **M4.1–M4.5** | Diagnostics, build system (Candor.toml), LSP, formatter, test runner |
| **M5.1–M5.5** | LLVM backend, debug/release/sanitizer, cross-compilation, WebAssembly |
| **M9.1–M9.2** | `vec::push` growable collections in LLVM, `box<T>` recursive heap types |
| **M9.3–M9.5** | Bootstrapped lexer, parser, typeck written in Candor (phases 1–5) |
| **M10.3** | Hardware effect tiers: `gpu net storage mem async` |
| **M10.4** | `arc<T>` shared reference-counted ownership |
| **M11.1** | `f16`/`bf16` float types |
| **M6.1** | Symbolic contract evaluation at compile time |
| **M6.4** | `forall`/`exists` runtime quantifiers |
| **M7.1** | `candorc mcp` + `#mcp_tool` → `tools.json` |
| **M7.2** | `candorc doc` + `#intent` → `intent.json` |
| **M7.3** | `cap<T>` capability tokens |
| **M7.4** | `#export_json` struct directive → JSON codegen |
| **M8.3** | `candorc doc --html` from `///` doc comments |
| **M10.1** | `task<T>` / `spawn` structured concurrency via pthreads |

---

## Commit conventions

- No `Co-Authored-By` lines in commits (user preference)
- Commit messages follow `feat: M<N.N> description` pattern
- Tests always accompany new features in the same commit

---

## Go module structure

```
go.mod: module github.com/scottcorleyg1/candor/compiler
        go 1.24
```

All imports use the full module path, e.g.:
```go
import "github.com/scottcorleyg1/candor/compiler/typeck"
import "github.com/scottcorleyg1/candor/compiler/parser"
import "github.com/scottcorleyg1/candor/compiler/lexer"
```

Run tests: `cd compiler && go test ./...`
Build: `cd compiler && go build ./...`

---

## Next milestone candidates

From the roadmap, likely next steps in priority order:

- **M10.2** — `effects(async)` state machine (coroutine-based async, not threads)
- **M11.2** — `tensor<T>` builtin type (ML data primitive)
- **M8.1** — Package registry / multi-package compilation
- **M12.1** — `mmap<T>` memory-mapped files
- **M6.2/M6.3** — SMT solver integration for contract verification

The roadmap is at `docs/roadmap.md`. The AI preference ordering is at
`docs/ai_preference_roadmap.md`.
