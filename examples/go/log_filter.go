// log_filter.go — Go equivalent of examples/log_filter.cnd
//
// What the signature CANNOT tell you that Candor signatures CAN:
//   - parseLevel, keepLine, filterLines: pure? the signature is silent
//   - runFilter: has I/O? (string, string, string) -> (int, error) gives no hint
//   - the int return from runFilter can be silently discarded: runFilter(a, b, c)
//
// In Candor:
//   fn parseLevel(line: str) -> option<str> pure   ← pure: enforced, not a comment
//   fn runFilter(in, out, level: str) -> result<i64, str> effects(io)

package main

import (
	"fmt"
	"os"
	"strings"
)

func parseLevel(line string) (string, bool) {
	if len(line) < 3 || line[0] != '[' {
		return "", false
	}
	close := strings.Index(line[1:], "]")
	if close < 0 {
		return "", false
	}
	return line[1 : close+1], true
}

func keepLine(line, level string) bool {
	lvl, ok := parseLevel(line)
	if !ok {
		return false
	}
	return lvl == level
}

func filterLines(lines []string, level string) []string {
	var out []string
	for _, line := range lines {
		if keepLine(line, level) {
			out = append(out, line)
		}
	}
	return out
}

func runFilter(pathIn, pathOut, level string) (int, error) {
	data, err := os.ReadFile(pathIn)
	if err != nil {
		return 0, fmt.Errorf("cannot read: %s", pathIn)
	}
	lines := strings.Split(string(data), "\n")
	kept := filterLines(lines, level)
	out := strings.Join(kept, "\n") + "\n"
	if err := os.WriteFile(pathOut, []byte(out), 0644); err != nil {
		return 0, fmt.Errorf("cannot write: %s", err)
	}
	return len(kept), nil
}

func main() {
	log := "[ERROR] disk full\n[INFO] server started\n[WARN] high memory\n[ERROR] connection refused\n[INFO] request received\n"
	if err := os.WriteFile("examples/sample.log", []byte(log), 0644); err != nil {
		fmt.Println("setup error:", err)
		return
	}
	n, err := runFilter("examples/sample.log", "examples/errors.log", "ERROR")
	if err != nil {
		fmt.Println("filter failed:", err)
		return
	}
	fmt.Printf("%d ERROR lines written to errors.log\n", n)
}
