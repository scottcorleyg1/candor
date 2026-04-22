/* pipeline.c — idiomatic C equivalent of examples/pipeline.cnd
 *
 * What C signatures CANNOT tell you (all Candor signatures encode + enforce):
 *   parse_row, validate_row, summarize: pure? Unknown — must read the body
 *   run_pipeline: has I/O? Not visible in the signature
 *   RowResult return value can be silently ignored: parse_row(line); compiles, no warning
 *
 * In Candor:
 *   fn parse_row(line: str) -> result<Row, str> pure      <- I/O impossible, compiler-verified
 *   fn run_pipeline(path: str) -> result<Summary, str> effects(io)
 *   fn summarize(rows: vec<Row>) -> Summary pure
 *
 * Compile: gcc -Wall -Wextra -o pipeline examples/c/pipeline.c
 * Run:     ./pipeline
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define NAME_MAX_LEN 51
#define ERR_MAX_LEN  512

/* In Candor: struct Row { name: str  score: i64 } */
typedef struct {
    char      name[NAME_MAX_LEN];
    long long score;
} Row;

/* In Candor: struct Summary { count: i64  total: i64  highest: str } */
typedef struct {
    long long count;
    long long total;
    char      highest[NAME_MAX_LEN];
} Summary;

/* In Candor: result<Row, str> is a built-in generic — zero typedef boilerplate.
 * In C: every unique (ok_type, err_type) pair needs its own struct definition. */
typedef struct {
    int ok;
    Row row;
    char err[ERR_MAX_LEN];
} RowResult;

typedef struct {
    int     ok;
    Summary summary;
    char    err[ERR_MAX_LEN];
} SummaryResult;

/* In Candor: fn parse_row(line: str) -> result<Row, str> pure
 * The 'pure' keyword is compiler-verified: calling print() inside would be a compile error.
 * In C: no equivalent exists. Caller cannot know this is pure without reading the body. */
static RowResult parse_row(const char *line) {
    RowResult res = {0};
    const char *comma = strchr(line, ',');
    if (!comma) {
        snprintf(res.err, ERR_MAX_LEN, "missing comma in: '%s'", line);
        return res;
    }
    size_t name_len = (size_t)(comma - line);
    const char *score_str = comma + 1;
    size_t score_len = strlen(score_str);
    if (name_len == 0)  { strcpy(res.err, "empty name");  return res; }
    if (score_len == 0) { strcpy(res.err, "empty score"); return res; }
    for (size_t i = 0; i < score_len; i++) {
        if (score_str[i] < '0' || score_str[i] > '9') {
            snprintf(res.err, ERR_MAX_LEN, "non-numeric score: '%s'", score_str);
            return res;
        }
    }
    long long score = 0;
    for (size_t i = 0; i < score_len; i++)
        score = score * 10 + (score_str[i] - '0');
    if (name_len > 50) {
        snprintf(res.err, ERR_MAX_LEN, "name too long: %.*s", (int)name_len, line);
        return res;
    }
    strncpy(res.row.name, line, name_len);
    res.row.name[name_len] = '\0';
    res.row.score = score;
    res.ok = 1;
    return res;
}

/* In Candor: fn validate_row(r: Row) -> result<Row, str> pure */
static RowResult validate_row(Row r) {
    RowResult res = {0};
    if ((int)strlen(r.name) > 50) { snprintf(res.err, ERR_MAX_LEN, "name too long: %s",      r.name); return res; }
    if (r.score < 0)              { snprintf(res.err, ERR_MAX_LEN, "negative score for: %s",  r.name); return res; }
    if (r.score > 100)            { snprintf(res.err, ERR_MAX_LEN, "score over 100 for: %s",  r.name); return res; }
    res.row = r;
    res.ok = 1;
    return res;
}

/* In Candor: fn summarize(rows: vec<Row>) -> Summary pure */
static Summary summarize(Row *rows, size_t n) {
    Summary s = {(long long)n, 0, ""};
    long long best = -1;
    for (size_t i = 0; i < n; i++) {
        s.total += rows[i].score;
        if (rows[i].score > best) {
            best = rows[i].score;
            strncpy(s.highest, rows[i].name, NAME_MAX_LEN - 1);
            s.highest[NAME_MAX_LEN - 1] = '\0';
        }
    }
    return s;
}

/* In Candor: fn run_pipeline(path: str) -> result<Summary, str> effects(io)
 * The effects(io) annotation is compiler-verified: calling this from a pure fn is an error.
 * In C: no equivalent. Caller cannot know this touches the filesystem without reading it. */
static SummaryResult run_pipeline(const char *path) {
    SummaryResult res = {0};
    FILE *f = fopen(path, "r");
    if (!f) { snprintf(res.err, ERR_MAX_LEN, "cannot read: %s", path); return res; }
    Row rows[1024];
    size_t nrows = 0;
    char line[1024];
    while (fgets(line, sizeof(line), f)) {
        size_t len = strlen(line);
        while (len > 0 && (line[len-1] == '\n' || line[len-1] == '\r')) line[--len] = '\0';
        if (len == 0) continue;
        RowResult pr = parse_row(line);
        if (!pr.ok) { fclose(f); snprintf(res.err, ERR_MAX_LEN, "parse error: %.480s",      pr.err); return res; }
        RowResult vr = validate_row(pr.row);
        if (!vr.ok) { fclose(f); snprintf(res.err, ERR_MAX_LEN, "validation error: %.480s", vr.err); return res; }
        if (nrows < 1024) rows[nrows++] = vr.row;
    }
    fclose(f);
    res.summary = summarize(rows, nrows);
    res.ok = 1;
    return res;
}

int main(void) {
    FILE *setup = fopen("examples/scores.csv", "w");
    if (!setup) { fputs("setup error: cannot write scores.csv\n", stderr); return 1; }
    fputs("Alice,95\nBob,82\nCarol,78\nDave,91\n", setup);
    fclose(setup);
    SummaryResult r = run_pipeline("examples/scores.csv");
    if (!r.ok) { printf("pipeline failed: %s\n", r.err); return 1; }
    printf("count:   %lld\ntotal:   %lld\nhighest: %s\n",
           r.summary.count, r.summary.total, r.summary.highest);
    return 0;
}
