// pipeline.rs — Rust equivalent of examples/pipeline.cnd
//
// What the signature CANNOT tell you that Candor signatures CAN:
//   - parse_row, validate_row, summarize: pure? Rust has no runtime purity annotation
//     const fn cannot call format! or iterators — useless for this purpose
//   - run_pipeline: Result<Summary, String> says "can fail" but not "does I/O"
//   - Result IS enforced by the compiler; purity and effects are undeclared
//   - An AI must read parse_row's body to confirm it never calls fs::read or println
//
// In Candor:
//   fn parse_row(line: str) -> result<Row, str> pure    ← no I/O possible, enforced
//   fn summarize(rows: vec<Row>) -> Summary pure        ← pure aggregate, enforced
//   fn run_pipeline(path: str) -> result<Summary, str> effects(io)

use std::fs;

struct Row {
    name: String,
    score: i64,
}

struct Summary {
    count: i64,
    total: i64,
    highest: String,
}

fn parse_row(line: &str) -> Result<Row, String> {
    let sep = line.find(',')
        .ok_or_else(|| format!("missing comma in: '{}'", line))?;
    let name = &line[..sep];
    let score_str = &line[sep + 1..];
    if name.is_empty() {
        return Err("empty name".to_string());
    }
    if score_str.is_empty() {
        return Err("empty score".to_string());
    }
    let score = score_str.parse::<i64>()
        .map_err(|_| format!("non-numeric score: '{}'", score_str))?;
    Ok(Row { name: name.to_string(), score })
}

fn validate_row(r: Row) -> Result<Row, String> {
    if r.name.len() > 50 {
        return Err(format!("name too long: {}", r.name));
    }
    if r.score < 0 {
        return Err(format!("negative score for: {}", r.name));
    }
    if r.score > 100 {
        return Err(format!("score over 100 for: {}", r.name));
    }
    Ok(r)
}

fn summarize(rows: &[Row]) -> Summary {
    let mut total = 0i64;
    let mut best_score = -1i64;
    let mut best_name = String::new();
    for r in rows {
        total += r.score;
        if r.score > best_score {
            best_score = r.score;
            best_name = r.name.clone();
        }
    }
    Summary { count: rows.len() as i64, total, highest: best_name }
}

fn run_pipeline(path: &str) -> Result<Summary, String> {
    let raw = fs::read_to_string(path)
        .map_err(|_| format!("cannot read: {}", path))?;
    let mut rows = Vec::new();
    for line in raw.lines() {
        let line = line.trim();
        if line.is_empty() {
            continue;
        }
        let parsed = parse_row(line)
            .map_err(|e| format!("parse error: {}", e))?;
        let valid = validate_row(parsed)
            .map_err(|e| format!("validation error: {}", e))?;
        rows.push(valid);
    }
    Ok(summarize(&rows))
}

fn main() {
    let csv = "Alice,95\nBob,82\nCarol,78\nDave,91\n";
    fs::write("examples/scores.csv", csv).expect("setup error");
    match run_pipeline("examples/scores.csv") {
        Ok(s)  => println!("count:   {}\ntotal:   {}\nhighest: {}", s.count, s.total, s.highest),
        Err(e) => println!("pipeline failed: {}", e),
    }
}
