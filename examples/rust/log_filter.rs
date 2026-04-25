// log_filter.rs — Rust equivalent of examples/log_filter.cnd
//
// What the signature CANNOT tell you that Candor signatures CAN:
//   - parse_level, keep_line, filter_lines: pure? no Rust annotation for runtime purity
//   - run_filter: the signature Result<i64, String> says "can fail" but not "does I/O"
//     An AI reviewing this signature must read the body to know it touches the filesystem
//   - Result is enforced; purity and effects are not declared anywhere
//
// In Candor:
//   fn parse_level(line: str) -> option<str> pure   ← pure: compiler-verified
//   fn run_filter(in, out, level: str) -> result<i64, str> effects(io)

use std::fs;

fn parse_level(line: &str) -> Option<&str> {
    if line.len() < 3 || !line.starts_with('[') {
        return None;
    }
    let close = line[1..].find(']')?;
    Some(&line[1..close + 1])
}

fn keep_line(line: &str, level: &str) -> bool {
    parse_level(line).map_or(false, |lvl| lvl == level)
}

fn filter_lines<'a>(lines: &[&'a str], level: &str) -> Vec<&'a str> {
    lines.iter().filter(|&&l| keep_line(l, level)).copied().collect()
}

fn run_filter(path_in: &str, path_out: &str, level: &str) -> Result<i64, String> {
    let raw = fs::read_to_string(path_in)
        .map_err(|_| format!("cannot read: {}", path_in))?;
    let lines: Vec<&str> = raw.lines().collect();
    let kept = filter_lines(&lines, level);
    let out = kept.join("\n") + "\n";
    fs::write(path_out, out)
        .map_err(|e| format!("cannot write: {}", e))?;
    Ok(kept.len() as i64)
}

fn main() {
    let log = "[ERROR] disk full\n[INFO] server started\n[WARN] high memory\n[ERROR] connection refused\n[INFO] request received\n";
    fs::write("examples/sample.log", log).expect("setup error");
    match run_filter("examples/sample.log", "examples/errors.log", "ERROR") {
        Ok(n)  => println!("{} ERROR lines written to errors.log", n),
        Err(e) => println!("filter failed: {}", e),
    }
}
