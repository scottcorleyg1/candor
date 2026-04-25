// config.go — Go equivalent of examples/config.cnd
//
// What the signature CANNOT tell you that Candor signatures CAN:
//   - parseLine, getKey, getVal: pure? unknown — must read body to confirm
//   - loadConfig: has I/O? the return type (Config, error) hints at failure but not I/O
//   - error from loadConfig can be silently discarded: cfg, _ := loadConfig(path)
//
// In Candor:
//   fn parseLine(line: str) -> result<str, str> pure   ← pure: compiler-enforced
//   fn loadConfig(path: str) -> result<Config, str> effects(io)  ← I/O in signature

package main

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Host  string
	Port  string
	Debug string
}

func parseLine(line string) (string, error) {
	if len(line) == 0 {
		return "", fmt.Errorf("empty line")
	}
	idx := strings.Index(line, "=")
	if idx < 0 {
		return "", fmt.Errorf("missing '=' in: %s", line)
	}
	if idx == 0 {
		return "", fmt.Errorf("empty key in: %s", line)
	}
	return line, nil
}

func getKey(line string) string {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return ""
	}
	return line[:idx]
}

func getVal(line string) string {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return ""
	}
	return line[idx+1:]
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("file not found: %s", path)
	}
	var cfg Config
	for i, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if _, err := parseLine(line); err != nil {
			return Config{}, fmt.Errorf("line %d: %s", i+1, err)
		}
		k, v := getKey(line), getVal(line)
		switch k {
		case "host":
			cfg.Host = v
		case "port":
			cfg.Port = v
		case "debug":
			cfg.Debug = v
		}
	}
	return cfg, nil
}

func main() {
	cfg, err := loadConfig("examples/config_test.txt")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Printf("host=%s\nport=%s\ndebug=%s\n", cfg.Host, cfg.Port, cfg.Debug)
}
