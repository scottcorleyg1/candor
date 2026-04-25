# config.py — Python equivalent of examples/config.cnd
#
# What the signature CANNOT tell you that Candor signatures CAN:
#   - parse_line, get_key, get_val: pure? Python has no such concept at the language level
#   - load_config: has I/O? "-> Config" reveals nothing; the caller must read the body
#   - errors: load_config raises ValueError/IOError — callers can ignore exceptions entirely
#   - no compile-time enforcement of any of the above
#
# In Candor:
#   fn parse_line(line: str) -> result<str, str> pure   ← pure: compiler enforced
#   fn load_config(path: str) -> result<Config, str> effects(io)  ← I/O in signature

from dataclasses import dataclass, field


@dataclass
class Config:
    host: str = ""
    port: str = ""
    debug: str = ""


def parse_line(line: str) -> str:
    if not line:
        raise ValueError("empty line")
    if '=' not in line:
        raise ValueError(f"missing '=' in: {line}")
    if line.startswith('='):
        raise ValueError(f"empty key in: {line}")
    return line


def get_key(line: str) -> str:
    idx = line.find('=')
    return line[:idx] if idx >= 0 else ""


def get_val(line: str) -> str:
    idx = line.find('=')
    return line[idx + 1:] if idx >= 0 else ""


def load_config(path: str) -> Config:
    with open(path) as f:
        raw = f.read()
    cfg = Config()
    for i, line in enumerate(raw.splitlines()):
        line = line.strip()
        if not line:
            continue
        try:
            parse_line(line)
        except ValueError as e:
            raise ValueError(f"line {i + 1}: {e}") from e
        k, v = get_key(line), get_val(line)
        if k == "host":
            cfg.host = v
        elif k == "port":
            cfg.port = v
        elif k == "debug":
            cfg.debug = v
    return cfg


if __name__ == "__main__":
    try:
        cfg = load_config("examples/config_test.txt")
        print(f"host={cfg.host}")
        print(f"port={cfg.port}")
        print(f"debug={cfg.debug}")
    except Exception as e:
        print(f"error: {e}")
