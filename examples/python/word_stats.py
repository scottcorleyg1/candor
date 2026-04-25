# word_stats.py — Python equivalent of examples/word_stats.cnd
#
# What the signature CANNOT tell you that Candor signatures CAN:
#   - count_lines, count_words, compute_stats: pure? Python has no such concept
#   - analyze_file: has I/O? may raise IOError? the annotation "-> Stats" hides both
#   - errors: analyze_file raises on failure — callers have no obligation to handle it
#     (no compile-time enforcement; silent propagation to caller stack)
#
# In Candor:
#   fn count_lines(text: str) -> i64 pure              ← zero side effects, enforced
#   fn analyze_file(path: str) -> result<Stats, str> effects(io)  ← I/O in signature

from dataclasses import dataclass


@dataclass
class Stats:
    lines: int
    words: int
    chars: int


def count_lines(text: str) -> int:
    return text.count('\n')


def count_words(text: str) -> int:
    return len(text.split())


def compute_stats(text: str) -> Stats:
    return Stats(
        lines=count_lines(text),
        words=count_words(text),
        chars=len(text),
    )


def analyze_file(path: str) -> Stats:
    with open(path) as f:
        text = f.read()
    return compute_stats(text)


if __name__ == "__main__":
    try:
        s = analyze_file("examples/config_test.txt")
        print(f"lines: {s.lines}")
        print(f"words: {s.words}")
        print(f"chars: {s.chars}")
    except Exception as e:
        print(f"error: {e}")
