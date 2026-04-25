# pipeline.py — Python equivalent of examples/pipeline.cnd
#
# What the signature CANNOT tell you that Candor signatures CAN:
#   - parse_row, validate_row, summarize: pure? Python has no purity concept
#   - run_pipeline: "-> Summary" hides all I/O; the caller cannot know from the signature
#   - errors: parse_row raises ValueError — callers have zero compile-time obligation
#   - summarize looks identical in signature to a function that logs to a database
#
# In Candor:
#   fn parse_row(line: str) -> result<Row, str> pure    ← no I/O possible, enforced
#   fn summarize(rows: vec<Row>) -> Summary pure        ← pure, enforced
#   fn run_pipeline(path: str) -> result<Summary, str> effects(io)

from dataclasses import dataclass
import os


@dataclass
class Row:
    name: str
    score: int


@dataclass
class Summary:
    count: int
    total: int
    highest: str


def parse_row(line: str) -> Row:
    sep = line.find(',')
    if sep < 0:
        raise ValueError(f"missing comma in: '{line}'")
    name = line[:sep]
    score_str = line[sep + 1:]
    if not name:
        raise ValueError("empty name")
    if not score_str:
        raise ValueError("empty score")
    if not score_str.isdigit():
        raise ValueError(f"non-numeric score: '{score_str}'")
    return Row(name=name, score=int(score_str))


def validate_row(r: Row) -> Row:
    if len(r.name) > 50:
        raise ValueError(f"name too long: {r.name}")
    if r.score < 0:
        raise ValueError(f"negative score for: {r.name}")
    if r.score > 100:
        raise ValueError(f"score over 100 for: {r.name}")
    return r


def summarize(rows: list[Row]) -> Summary:
    total = sum(r.score for r in rows)
    best = max(rows, key=lambda r: r.score, default=Row("", 0))
    return Summary(count=len(rows), total=total, highest=best.name)


def run_pipeline(path: str) -> Summary:
    with open(path) as f:
        raw = f.read()
    rows = []
    for line in raw.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            parsed = parse_row(line)
        except ValueError as e:
            raise ValueError(f"parse error: {e}") from e
        try:
            valid = validate_row(parsed)
        except ValueError as e:
            raise ValueError(f"validation error: {e}") from e
        rows.append(valid)
    return summarize(rows)


if __name__ == "__main__":
    with open("examples/scores.csv", "w") as f:
        f.write("Alice,95\nBob,82\nCarol,78\nDave,91\n")
    try:
        s = run_pipeline("examples/scores.csv")
        print(f"count:   {s.count}")
        print(f"total:   {s.total}")
        print(f"highest: {s.highest}")
    except Exception as e:
        print(f"pipeline failed: {e}")
