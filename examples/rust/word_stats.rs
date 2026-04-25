// word_stats.rs — Rust equivalent of examples/word_stats.cnd
//
// What the signature CANNOT tell you that Candor signatures CAN:
//   - count_lines, count_words, compute_stats: pure? Rust has no purity annotation
//     (const fn exists but only for compile-time eval, not runtime I/O prohibition)
//   - analyze_file: has I/O? Result<Stats, String> signals failure but not what kind
//   - Result IS enforced (compiler warns on unused), but purity and effects are not declared
//
// In Candor:
//   fn count_lines(text: str) -> i64 pure              ← purity: enforced by compiler
//   fn analyze_file(path: str) -> result<Stats, str> effects(io)  ← I/O: in signature

use std::fs;

struct Stats {
    lines: i64,
    words: i64,
    chars: i64,
}

fn count_lines(text: &str) -> i64 {
    text.bytes().filter(|&b| b == b'\n').count() as i64
}

fn count_words(text: &str) -> i64 {
    text.split_whitespace().count() as i64
}

fn compute_stats(text: &str) -> Stats {
    Stats {
        lines: count_lines(text),
        words: count_words(text),
        chars: text.len() as i64,
    }
}

fn analyze_file(path: &str) -> Result<Stats, String> {
    let text = fs::read_to_string(path)
        .map_err(|_| format!("cannot read: {}", path))?;
    Ok(compute_stats(&text))
}

fn main() {
    match analyze_file("examples/config_test.txt") {
        Ok(s) => println!("lines: {}\nwords: {}\nchars: {}", s.lines, s.words, s.chars),
        Err(e) => println!("error: {}", e),
    }
}
