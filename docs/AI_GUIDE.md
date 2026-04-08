# Candor AI Agent Guide
**Last updated: 2026-04-08**

**IF YOU ARE AN AI AGENT READ THIS FILE BEFORE TOUCHING ANY CODE.**

---

## 1. Active State — Read This First

**Current goal:** Stage 2 self-hosting. `stage3.exe` (Candor-compiled compiler) must compile `src/compiler/*.cnd` without crashing.

**As of 2026-04-08:**
- `stage2.c` compiles to `stage3.exe` with **0 GCC errors** ✅
- `stage3.exe` runs and receives args correctly ✅
- `stage3.exe` **segfaults** in `collect_types_from_decl` while processing `lexer.cnd` ❌
- Crash site: ImplD arm accessing `im.methods._data[mi]` — see TASK-09 in `Agents_Collab.md`

**Active task file:** [`Agents_Collab.md`](../Agents_Collab.md) — root of repo, not docs/

---

## 2. The Two Compilers — Never Confuse These

| | Go Emitter (Stage 1) | Candor Emitter (Stage 2) |
|--|--|--|
| Source | `compiler/emit_c/emit_c.go` | `src/compiler/emit_c.cnd` |
| Binary | `candorc-stage1-rebuilt.exe` | `src/compiler/lexer.exe` (current name) |
| Role | Authoritative, used to build stage2 | Self-hosted, what we're debugging |

**Rule:** When fixing an emission bug, you almost always fix `src/compiler/emit_c.cnd`. Only touch `compiler/emit_c/emit_c.go` if the Go-compiled binary itself needs to change.

---

## 3. The Exact Commands (copy-paste ready)

### Rebuild everything from scratch
```bash
# Step 1 — Build Go stage1 (only if .go files changed)
cd compiler && go build -o ../candorc-stage1-rebuilt.exe . && cd ..

# Step 2 — Compile .cnd sources → src/compiler/lexer.exe
./candorc-stage1-rebuilt.exe src/compiler/lexer.cnd src/compiler/parser.cnd \
  src/compiler/typeck.cnd src/compiler/emit_c.cnd \
  src/compiler/manifest.cnd src/compiler/main.cnd

# Step 3 — Generate stage2.c using the Candor-compiled binary
./src/compiler/lexer.exe src/compiler/lexer.cnd src/compiler/parser.cnd \
  src/compiler/typeck.cnd src/compiler/emit_c.cnd \
  src/compiler/manifest.cnd src/compiler/main.cnd > /d/tmp/stage2.c 2>/dev/null

# Step 4 — Re-append runtime macros (VSCode strips them — see known_compiler_bugs.md #1)
cat >> src/compiler/_cnd_runtime.h << 'EOF'
typedef struct { int _ok; int64_t _ok_val; const char* _err_val; } _CndRes_int64_t_const_charptr;
static inline uint64_t _cnd_map_hash_str(const char* k) {
    uint64_t h = 5381; while (*k) h = ((h<<5)+h)^(unsigned char)*k++; return h;
}
#define _cnd_map_insert(mp, key, ...) __extension__ ({ \
    __auto_type _mp=(mp); __auto_type _k=(key); __auto_type _v=(__VA_ARGS__); \
    if(!_mp->_buckets||_mp->_len*4>=_mp->_cap*3){ \
        uint64_t _nc=_mp->_buckets?_mp->_cap*2:16; \
        __typeof__(*_mp->_buckets)* _nb=(__typeof__(*_mp->_buckets)*)calloc(_nc,sizeof(*_mp->_buckets)); \
        for(uint64_t _ri=0;_ri<_mp->_cap;_ri++){__auto_type _re=_mp->_buckets[_ri]; \
            while(_re){__auto_type _rn=_re->_next;uint64_t _rb=_cnd_map_hash_str(_re->_key)%_nc;_re->_next=_nb[_rb];_nb[_rb]=_re;_re=_rn;}} \
        free(_mp->_buckets);_mp->_buckets=_nb;_mp->_cap=_nc;} \
    uint64_t _bi=_cnd_map_hash_str(_k)%_mp->_cap; \
    __auto_type _en=_mp->_buckets[_bi];int _fd=0; \
    while(_en){if(strcmp(_en->_key,_k)==0){_en->_val=_v;_fd=1;break;}_en=_en->_next;} \
    if(!_fd){__auto_type _ne=(__typeof__(_mp->_buckets[0]))malloc(sizeof(*_mp->_buckets[0])); \
        _ne->_key=_k;_ne->_val=_v;_ne->_next=_mp->_buckets[_bi];_mp->_buckets[_bi]=_ne;_mp->_len++;} \
})
#define _cnd_map_get(m, key) __extension__ ({ \
    __auto_type _gm=(m);__auto_type _gk=(key); \
    __typeof__(&_gm._buckets[0]->_val) _gr=NULL; \
    if(_gm._buckets){uint64_t _gb=_cnd_map_hash_str(_gk)%_gm._cap; \
        __auto_type _ge=_gm._buckets[_gb]; \
        while(_ge){if(strcmp(_ge->_key,_gk)==0){__typeof__(_ge->_val)* _gp=(__typeof__(_ge->_val)*)malloc(sizeof(_ge->_val));*_gp=_ge->_val;_gr=_gp;break;}_ge=_ge->_next;}} \
    _gr; \
})
#define _cnd_map_contains(m, key) __extension__ ({ \
    __auto_type _cm=(m);__auto_type _ck=(key);int _cr=0; \
    if(_cm._buckets){uint64_t _cb=_cnd_map_hash_str(_ck)%_cm._cap; \
        __auto_type _ce=_cm._buckets[_cb]; \
        while(_ce){if(strcmp(_ce->_key,_ck)==0){_cr=1;break;}_ce=_ce->_next;}} \
    _cr; \
})
EOF

# Step 5 — Compile stage2.c → stage3.exe
PATH="/c/msys64/mingw64/bin:$PATH" /c/msys64/mingw64/bin/gcc.exe \
  -std=gnu23 -O0 -o /d/tmp/stage3.exe /d/tmp/stage2.c -I src/compiler -lm

# Step 6 — Test stage3.exe
/d/tmp/stage3.exe src/compiler/lexer.cnd src/compiler/parser.cnd \
  src/compiler/typeck.cnd src/compiler/emit_c.cnd \
  src/compiler/manifest.cnd src/compiler/main.cnd > /d/tmp/stage4.c
```

### Just recompile after editing .cnd files
```bash
# Rebuild lexer.exe from .cnd sources
./candorc-stage1-rebuilt.exe src/compiler/lexer.cnd src/compiler/parser.cnd \
  src/compiler/typeck.cnd src/compiler/emit_c.cnd \
  src/compiler/manifest.cnd src/compiler/main.cnd
# Then run Steps 3–6 above
```

---

## 4. GCC Facts (hard-won, don't re-derive)

| Fact | Value |
|------|-------|
| Working GCC | `/c/msys64/mingw64/bin/gcc.exe` |
| Required flag | `-std=gnu23` (needed for `auto` type deduction) |
| Required PATH | `PATH="/c/msys64/mingw64/bin:$PATH"` (GCC needs its own bin for `as.exe`/`ld.exe`) |
| Full compile command | `gcc.exe -std=gnu23 -O0 -o output.exe input.c -I src/compiler -lm` |
| Why not msys64v2026 | Both installs exist; msys64 (GCC 14.2) works; msys64v2026 (GCC 15.2) also broken cc1 in this shell context |

---

## 5. Architecture Reference

See [`docs/compiler_architecture.md`](compiler_architecture.md) for the 5-pass C emission ordering.  
See [`docs/syntax_and_builtins.md`](syntax_and_builtins.md) for the Candor language cheat sheet.  
See [`docs/known_compiler_bugs.md`](known_compiler_bugs.md) for GCC error patterns before debugging from scratch.

---

## 6. General Rules

- **Never modify the wrong file.** The Go emitter and Candor emitter are mirrors. Changes to one do not affect the other.
- **`_cnd_runtime.h` gets stripped by VSCode** on every save. Always re-append the map macros before compiling stage2.c. The full block is in Step 4 above.
- **`auto` requires `-std=gnu23`.** The generated C uses C23 `auto` for type deduction. Without this flag you get hundreds of "type defaults to int" errors.
- **Don't use sed/grep to edit `.cnd` files.** Nested `str_concat` calls have quote nesting that breaks easily. Use the Edit tool.
