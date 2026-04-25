// word_stats.go — Go equivalent of examples/word_stats.cnd
//
// What the signature CANNOT tell you that Candor signatures CAN:
//   - count_lines, count_words, compute_stats: pure? unknown — must read body
//   - analyze_file: has I/O? the signature is silent; only the body reveals it
//   - analyze_file error: can be silently ignored: s, _ := analyzeFile(path)
//
// In Candor:
//   fn count_lines(text: str) -> i64 pure          ← compiler-verified: zero side effects
//   fn analyze_file(path: str) -> result<Stats, str> effects(io)  ← I/O declared in signature

package main

import (
	"fmt"
	"os"
	"strings"
)

type Stats struct {
	Lines int
	Words int
	Chars int
}

func countLines(text string) int {
	return strings.Count(text, "\n")
}

func countWords(text string) int {
	return len(strings.Fields(text))
}

func computeStats(text string) Stats {
	return Stats{
		Lines: countLines(text),
		Words: countWords(text),
		Chars: len(text),
	}
}

func analyzeFile(path string) (Stats, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Stats{}, fmt.Errorf("cannot read: %s", path)
	}
	return computeStats(string(data)), nil
}

func main() {
	s, err := analyzeFile("examples/config_test.txt")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Printf("lines: %d\nwords: %d\nchars: %d\n", s.Lines, s.Words, s.Chars)
}
