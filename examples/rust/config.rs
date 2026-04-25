// config.rs — Rust equivalent of examples/config.cnd
//
// What the signature CANNOT tell you that Candor signatures CAN:
//   - parse_line, get_key, get_val: pure? Rust has no way to annotate or enforce this
//   - load_config: the signature Result<Config, String> says "can fail" but not "does I/O"
//   - Result is enforced by the compiler, but an AI reviewing the signature must still
//     read the body to answer: "does this touch the filesystem?"
//
// In Candor:
//   fn parse_line(line: str) -> result<str, str> pure   ← pure: compiler enforced
//   fn load_config(path: str) -> result<Config, str> effects(io)  ← I/O in signature

use std::fs;

struct Config {
    host: String,
    port: String,
    debug: String,
}

fn parse_line(line: &str) -> Result<&str, String> {
    if line.is_empty() {
        return Err("empty line".to_string());
    }
    match line.find('=') {
        None => Err(format!("missing '=' in: {}", line)),
        Some(0) => Err(format!("empty key in: {}", line)),
        Some(_) => Ok(line),
    }
}

fn get_key(line: &str) -> &str {
    match line.find('=') {
        Some(i) => &line[..i],
        None => "",
    }
}

fn get_val(line: &str) -> &str {
    match line.find('=') {
        Some(i) => &line[i + 1..],
        None => "",
    }
}

fn load_config(path: &str) -> Result<Config, String> {
    let raw = fs::read_to_string(path)
        .map_err(|_| format!("file not found: {}", path))?;
    let mut host = String::new();
    let mut port = String::new();
    let mut debug = String::new();
    for (i, line) in raw.lines().enumerate() {
        let line = line.trim();
        if line.is_empty() {
            continue;
        }
        parse_line(line)
            .map_err(|e| format!("line {}: {}", i + 1, e))?;
        match get_key(line) {
            "host"  => host  = get_val(line).to_string(),
            "port"  => port  = get_val(line).to_string(),
            "debug" => debug = get_val(line).to_string(),
            _ => {}
        }
    }
    Ok(Config { host, port, debug })
}

fn main() {
    match load_config("examples/config_test.txt") {
        Ok(cfg) => println!("host={}\nport={}\ndebug={}", cfg.host, cfg.port, cfg.debug),
        Err(e)  => println!("error: {}", e),
    }
}
