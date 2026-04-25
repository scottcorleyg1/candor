# log_filter.py — Python equivalent of examples/log_filter.cnd
#
# What the signature CANNOT tell you that Candor signatures CAN:
#   - parse_level, keep_line, filter_lines: pure? no language-level annotation exists
#   - run_filter: "-> int" hides that this reads AND writes files; invisible to caller
#   - errors from run_filter propagate as exceptions; callers have zero obligation to handle
#   - an AI reviewing only the signatures cannot determine which functions have side effects
#
# In Candor:
#   fn parse_level(line: str) -> option<str> pure   ← pure: enforced by compiler
#   fn run_filter(in, out, level: str) -> result<i64, str> effects(io)

from typing import Optional
import os


def parse_level(line: str) -> Optional[str]:
    if len(line) < 3 or line[0] != '[':
        return None
    close = line.find(']', 1)
    if close < 0:
        return None
    return line[1:close]


def keep_line(line: str, level: str) -> bool:
    lvl = parse_level(line)
    return lvl == level if lvl is not None else False


def filter_lines(lines: list[str], level: str) -> list[str]:
    return [line for line in lines if keep_line(line, level)]


def run_filter(path_in: str, path_out: str, level: str) -> int:
    with open(path_in) as f:
        raw = f.read()
    lines = raw.split('\n')
    kept = filter_lines(lines, level)
    with open(path_out, 'w') as f:
        f.write('\n'.join(kept) + '\n')
    return len(kept)


if __name__ == "__main__":
    log = ("[ERROR] disk full\n[INFO] server started\n[WARN] high memory\n"
           "[ERROR] connection refused\n[INFO] request received\n")
    with open("examples/sample.log", "w") as f:
        f.write(log)
    try:
        n = run_filter("examples/sample.log", "examples/errors.log", "ERROR")
        print(f"{n} ERROR lines written to errors.log")
    except Exception as e:
        print(f"filter failed: {e}")
