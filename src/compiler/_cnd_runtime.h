#pragma once
#include <stdint.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <assert.h>
#include <math.h>
#include <time.h>
#include <ctype.h>
#ifdef _WIN32
#  include <windows.h>
#  include <direct.h>
#  define _cnd_mkdir(p) _mkdir(p)
#  include <io.h>
#  include <process.h>
#else
#  include <unistd.h>
#  include <dirent.h>
#  include <sys/stat.h>
#  include <sys/wait.h>
#  define _cnd_mkdir(p) mkdir(p, 0755)
#endif

static int _cnd_argc = 0;
static char** _cnd_argv = NULL;

static const char* _cnd_read_line(void) {
    static char _buf[4096];
    if (!fgets(_buf, sizeof(_buf), stdin)) { _buf[0] = '\0'; }
    size_t _n = strlen(_buf);
    while (_n > 0 && (_buf[_n-1] == '\n' || _buf[_n-1] == '\r')) { _buf[--_n] = '\0'; }
    char* _out = (char*)malloc(_n + 1);
    memcpy(_out, _buf, _n + 1);
    return _out;
}
static const char** _cnd_try_read_line(void) {
    static char _buf[4096];
    if (!fgets(_buf, sizeof(_buf), stdin)) { return NULL; }
    size_t _n = strlen(_buf);
    while (_n > 0 && (_buf[_n-1] == '\n' || _buf[_n-1] == '\r')) { _buf[--_n] = '\0'; }
    char* _s = (char*)malloc(_n + 1);
    memcpy(_s, _buf, _n + 1);
    const char** _p = (const char**)malloc(sizeof(const char*));
    *_p = _s;
    return _p;
}
static int64_t* _cnd_try_read_int(void) {
    int64_t _v;
    if (scanf("%lld", &_v) != 1) { return NULL; }
    int64_t* _p = (int64_t*)malloc(sizeof(int64_t));
    *_p = _v;
    return _p;
}
static double* _cnd_try_read_f64(void) {
    double _v;
    if (scanf("%lf", &_v) != 1) { return NULL; }
    double* _p = (double*)malloc(sizeof(double));
    *_p = _v;
    return _p;
}
static const char* _cnd_str_concat(const char* a, const char* b) {
    size_t la = strlen(a), lb = strlen(b);
    char* _out = (char*)malloc(la + lb + 1);
    memcpy(_out, a, la);
    memcpy(_out + la, b, lb + 1);
    return _out;
}
static const char* _cnd_int_to_str(int64_t n) {
    char _buf[32];
    snprintf(_buf, sizeof(_buf), "%lld", (long long)n);
    char* _out = (char*)malloc(strlen(_buf) + 1);
    strcpy(_out, _buf);
    return _out;
}
static const char* _cnd_read_file(const char* path) {
    FILE* _f = fopen(path, "rb");
    if (!_f) { return NULL; }
    fseek(_f, 0, SEEK_END); long _sz = ftell(_f); fseek(_f, 0, SEEK_SET);
    char* _buf = (char*)malloc(_sz + 1);
    if (!_buf) { fclose(_f); return NULL; }
    fread(_buf, 1, _sz, _f); _buf[_sz] = '\0';
    fclose(_f); return _buf;
}
static int _cnd_write_file(const char* path, const char* data) {
    FILE* _f = fopen(path, "wb");
    if (!_f) { return -1; }
    size_t _n = strlen(data);
    int _ok = (fwrite(data, 1, _n, _f) == _n) ? 0 : -1;
    fclose(_f); return _ok;
}
static int _cnd_append_file(const char* path, const char* data) {
    FILE* _f = fopen(path, "ab");
    if (!_f) { return -1; }
    size_t _n = strlen(data);
    int _ok = (fwrite(data, 1, _n, _f) == _n) ? 0 : -1;
    fclose(_f); return _ok;
}
static const char* _cnd_str_repeat(const char* s, int64_t n) {
    size_t _l = strlen(s);
    if (n <= 0 || _l == 0) { char* _e = (char*)malloc(1); _e[0] = '\0'; return _e; }
    char* _out = (char*)malloc((size_t)n * _l + 1);
    for (int64_t _i = 0; _i < n; _i++) { memcpy(_out + (size_t)_i * _l, s, _l); }
    _out[(size_t)n * _l] = '\0'; return _out;
}
static const char* _cnd_str_trim(const char* s) {
    while (*s && isspace((unsigned char)*s)) { s++; }
    size_t _l = strlen(s);
    while (_l > 0 && isspace((unsigned char)s[_l-1])) { _l--; }
    char* _out = (char*)malloc(_l + 1);
    memcpy(_out, s, _l); _out[_l] = '\0'; return _out;
}
static const char* _cnd_str_replace(const char* s, const char* from, const char* to) {
    size_t _fl = strlen(from), _tl = strlen(to), _count = 0;
    if (_fl == 0) { char* _c = (char*)malloc(strlen(s)+1); strcpy(_c, s); return _c; }
    const char* _p = s;
    while ((_p = strstr(_p, from)) != NULL) { _count++; _p += _fl; }
    size_t _sl = strlen(s);
    char* _out = (char*)malloc(_sl + _count * (_tl - _fl) + 1 + (_tl > _fl ? _count * (_tl - _fl) : 0));
    char* _w = _out; _p = s;
    while (1) { const char* _f = strstr(_p, from); if (!_f) { strcpy(_w, _p); break; }
        size_t _pre = _f - _p; memcpy(_w, _p, _pre); _w += _pre;
        memcpy(_w, to, _tl); _w += _tl; _p = _f + _fl; } return _out;
}
static const char* _cnd_str_to_upper(const char* s) {
    size_t _l = strlen(s); char* _out = (char*)malloc(_l + 1);
    for (size_t _i = 0; _i <= _l; _i++) { _out[_i] = (char)toupper((unsigned char)s[_i]); }
    return _out;
}
static const char* _cnd_str_to_lower(const char* s) {
    size_t _l = strlen(s); char* _out = (char*)malloc(_l + 1);
    for (size_t _i = 0; _i <= _l; _i++) { _out[_i] = (char)tolower((unsigned char)s[_i]); }
    return _out;
}
static void _cnd_flush_stdout(void) { fflush(stdout); }
static const char* _cnd_os_cwd(void) {
#ifdef _WIN32
    char* _buf = _getcwd(NULL, 0);
#else
    char* _buf = getcwd(NULL, 0);
#endif
    if (!_buf) { char* _e = (char*)malloc(2); _e[0] = '.'; _e[1] = '\0'; return _e; }
    return _buf;
}
typedef struct { const char** _data; uint64_t _len; uint64_t _cap; } _CndStrVec;
static int64_t _cnd_os_exec(void* vp, int* ok_out, const char** err_out) {
    _CndStrVec* v = (_CndStrVec*)vp;
    int argc = (int)v->_len;
    char** argv_c = (char**)malloc((size_t)(argc + 1) * sizeof(char*));
    if (!argv_c) { *ok_out = 0; *err_out = "os_exec: malloc failed"; return 0; }
    for (int i = 0; i < argc; i++) { argv_c[i] = (char*)v->_data[i]; }
    argv_c[argc] = NULL;
#ifdef _WIN32
    int _r = _spawnvp(_P_WAIT, argv_c[0], (const char* const*)argv_c);
    free(argv_c);
    if (_r < 0) { *ok_out = 0; *err_out = "os_exec: spawn failed"; return 0; }
    *ok_out = 1; return (int64_t)_r;
#else
    pid_t _pid = fork();
    if (_pid < 0) { free(argv_c); *ok_out = 0; *err_out = "os_exec: fork failed"; return 0; }
    if (_pid == 0) { execvp(argv_c[0], argv_c); _exit(127); }
    free(argv_c);
    int _wstatus = 0;
    waitpid(_pid, &_wstatus, 0);
    *ok_out = 1;
    return (int64_t)(WIFEXITED(_wstatus) ? WEXITSTATUS(_wstatus) : 128 + WTERMSIG(_wstatus));
#endif
}
static int64_t _cnd_time_now_ms(void) {
#ifdef _WIN32
    return (int64_t)(clock() * 1000 / CLOCKS_PER_SEC);
#else
    struct timespec _ts; clock_gettime(CLOCK_REALTIME, &_ts);
    return (int64_t)_ts.tv_sec * 1000 + (int64_t)_ts.tv_nsec / 1000000;
#endif
}
static int64_t _cnd_time_now_mono_ns(void) {
#ifdef _WIN32
    return (int64_t)clock() * (1000000000 / CLOCKS_PER_SEC);
#else
    struct timespec _ts; clock_gettime(CLOCK_MONOTONIC, &_ts);
    return (int64_t)_ts.tv_sec * 1000000000 + (int64_t)_ts.tv_nsec;
#endif
}
static void _cnd_time_sleep_ms(int64_t ms) {
#ifdef _WIN32
    Sleep((DWORD)ms);
#else
    struct timespec _ts = { (time_t)(ms / 1000), (long)((ms % 1000) * 1000000) };
    nanosleep(&_ts, NULL);
#endif
}
static uint64_t _cnd_rand_u64(void) {
    return ((uint64_t)(unsigned)rand() << 32) | (uint64_t)(unsigned)rand();
}
static double _cnd_rand_f64(void) {
    return (double)rand() / ((double)RAND_MAX + 1.0);
}
static void _cnd_rand_set_seed(uint64_t seed) { srand((unsigned)seed); }
static const char* _cnd_path_join(const char* a, const char* b) {
    size_t _al = strlen(a), _bl = strlen(b);
    int _sep = (_al > 0 && a[_al-1] != '/' && a[_al-1] != '\\') ? 1 : 0;
    char* _out = (char*)malloc(_al + _sep + _bl + 1);
    memcpy(_out, a, _al);
    if (_sep) { _out[_al] = '/'; }
    memcpy(_out + _al + _sep, b, _bl + 1); return _out;
}
static const char* _cnd_path_dir(const char* p) {
    size_t _l = strlen(p), _i = _l;
    while (_i > 0 && p[_i-1] != '/' && p[_i-1] != '\\') { _i--; }
    if (_i == 0) { char* _d = (char*)malloc(2); _d[0] = '.'; _d[1] = '\0'; return _d; }
    size_t _dl = _i > 1 ? _i - 1 : 1;
    char* _d = (char*)malloc(_dl + 1); memcpy(_d, p, _dl); _d[_dl] = '\0'; return _d;
}
static const char* _cnd_path_filename(const char* p) {
    size_t _l = strlen(p), _i = _l;
    while (_i > 0 && p[_i-1] != '/' && p[_i-1] != '\\') { _i--; }
    char* _f = (char*)malloc(_l - _i + 1); memcpy(_f, p + _i, _l - _i + 1); return _f;
}
static const char* _cnd_path_ext(const char* p) {
    size_t _l = strlen(p), _i = _l;
    while (_i > 0 && p[_i-1] != '.' && p[_i-1] != '/' && p[_i-1] != '\\') { _i--; }
    if (_i == 0 || p[_i-1] != '.') { char* _e = (char*)malloc(1); _e[0] = '\0'; return _e; }
    char* _e = (char*)malloc(_l - _i + 2); _e[0] = '.'; memcpy(_e+1, p+_i, _l-_i+1); return _e;
}
static int _cnd_path_exists(const char* p) {
#ifdef _WIN32
    return _access(p, 0) == 0;
#else
    return access(p, F_OK) == 0;
#endif
}

/* ── single-program helpers ────────────────────────────────────────────────── */
static const char* _cnd_bool_to_str(_Bool b) { return b ? "true" : "false"; }
static int64_t _cnd_str_to_int(const char* s) { return (int64_t)strtoll(s, NULL, 10); }
static double _cnd_str_to_f64(const char* s) { return strtod(s, NULL); }

/* vec<i64> for single-file programs */
#ifndef _CNDVEC_int64_t
#define _CNDVEC_int64_t
typedef struct _CndVec_int64_t { int64_t* _data; uint64_t _len; uint64_t _cap; } _CndVec_int64_t;
#endif
#ifndef _CNDVEC_HELPERS_int64_t
#define _CNDVEC_HELPERS_int64_t
static inline void _cnd_vec_push_int64_t(_CndVec_int64_t* v, int64_t val) {
    if (v->_len >= v->_cap) { uint64_t _nc = v->_cap ? v->_cap*2 : 4;
        v->_data = (int64_t*)realloc(v->_data, _nc*sizeof(int64_t)); v->_cap = _nc; }
    v->_data[v->_len++] = val;
}
#endif

/* ── vec<str> ────────────────────────────────────────────────────────────────── */
#ifndef _CNDVEC_const_charptr
#define _CNDVEC_const_charptr
typedef struct _CndVec_const_charptr _CndVec_const_charptr;
struct _CndVec_const_charptr { const char** _data; uint64_t _len; uint64_t _cap; };
#endif

/* ── map<str, i64> entry + map types (guard names match emitter output) ──────── */
#ifndef _CNDMAP_FWD_const_charptr_int64_t
#define _CNDMAP_FWD_const_charptr_int64_t
typedef struct _CndMapEntry_const_charptr_int64_t _CndMapEntry_const_charptr_int64_t;
typedef struct _CndMap_const_charptr_int64_t _CndMap_const_charptr_int64_t;
struct _CndMap_const_charptr_int64_t { struct _CndMapEntry_const_charptr_int64_t** _buckets; uint64_t _cap; uint64_t _len; };
#endif
#ifndef _CNDMAP_ENTRY_const_charptr_int64_t
#define _CNDMAP_ENTRY_const_charptr_int64_t
struct _CndMapEntry_const_charptr_int64_t { const char* _key; int64_t _val; struct _CndMapEntry_const_charptr_int64_t* _next; };
#endif

/* ── map macros ─────────────────────────────────────────────────────────────── */
#ifndef _CNDRES_int64_t_const_charptr
#define _CNDRES_int64_t_const_charptr
typedef struct { int _ok; int64_t _ok_val; const char* _err_val; } _CndRes_int64_t_const_charptr;
#endif

static inline uint64_t _cnd_map_hash_str(const char* k) {
    uint64_t h = 5381; while (*k) h = ((h<<5)+h)^(unsigned char)*k++; return h;
}
#define _cnd_map_insert(mp, key, ...) __extension__ ({ \
    __typeof__(mp)* _mp=&(mp); __auto_type _k=(key); __auto_type _v=(__VA_ARGS__); \
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
        _ne->_key=strdup(_k);_ne->_val=_v;_ne->_next=_mp->_buckets[_bi];_mp->_buckets[_bi]=_ne;_mp->_len++;} \
})
#define _cnd_map_get(m, key) __extension__ ({ \
    __auto_type _gm=(m);__auto_type _gk=(key); \
    __typeof__(&_gm._buckets[0]->_val) _gr=NULL; \
    if(_gm._buckets){uint64_t _gb=_cnd_map_hash_str(_gk)%_gm._cap; \
        __auto_type _ge=_gm._buckets[_gb]; \
        while(_ge){if(strcmp(_ge->_key,_gk)==0){__typeof__(_ge->_val)* _gp=(__typeof__(_ge->_val)*)malloc(sizeof(_ge->_val));*_gp=_ge->_val;_gr=_gp;break;}_ge=_ge->_next;} \
    } _gr; \
})
#define _cnd_map_drop(mp) __extension__ ({ \
    __typeof__(mp)* _md=&(mp); \
    if(_md->_buckets){ \
        for(uint64_t _di=0;_di<_md->_cap;_di++){ \
            __auto_type _de=_md->_buckets[_di]; \
            while(_de){__auto_type _dn=_de->_next;free((void*)_de->_key);free(_de);_de=_dn;} \
        } free(_md->_buckets);_md->_buckets=NULL;_md->_len=0;_md->_cap=0; \
    } \
})
#define _cnd_map_contains(m, key) __extension__ ({ \
    __auto_type _cm=(m);__auto_type _ck=(key);int _cr=0; \
    if(_cm._buckets){uint64_t _cb=_cnd_map_hash_str(_ck)%_cm._cap; \
        __auto_type _ce=_cm._buckets[_cb]; \
        while(_ce){if(strcmp(_ce->_key,_ck)==0){_cr=1;break;}_ce=_ce->_next;}} \
    _cr; \
})
