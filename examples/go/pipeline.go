// pipeline.go — Go equivalent of examples/pipeline.cnd
//
// What the signature CANNOT tell you that Candor signatures CAN:
//   - parseRow, validateRow, summarize: pure? must read the body
//   - runPipeline: has I/O? (string) -> (Summary, error) doesn't say
//   - error from runPipeline can be silently dropped: s, _ := runPipeline(path)
//   - no way to express "this function only transforms data, never touches disk/net"
//
// In Candor:
//   fn parseRow(line: str) -> result<Row, str> pure     ← no I/O possible, enforced
//   fn summarize(rows: vec<Row>) -> Summary pure        ← pure aggregate, enforced
//   fn runPipeline(path: str) -> result<Summary, str> effects(io)

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Row struct {
	Name  string
	Score int64
}

type Summary struct {
	Count   int64
	Total   int64
	Highest string
}

func parseRow(line string) (Row, error) {
	sep := strings.Index(line, ",")
	if sep < 0 {
		return Row{}, fmt.Errorf("missing comma in: '%s'", line)
	}
	name := line[:sep]
	scoreStr := line[sep+1:]
	if len(name) == 0 {
		return Row{}, fmt.Errorf("empty name")
	}
	if len(scoreStr) == 0 {
		return Row{}, fmt.Errorf("empty score")
	}
	score, err := strconv.ParseInt(scoreStr, 10, 64)
	if err != nil {
		return Row{}, fmt.Errorf("non-numeric score: '%s'", scoreStr)
	}
	return Row{Name: name, Score: score}, nil
}

func validateRow(r Row) (Row, error) {
	if len(r.Name) > 50 {
		return Row{}, fmt.Errorf("name too long: %s", r.Name)
	}
	if r.Score < 0 {
		return Row{}, fmt.Errorf("negative score for: %s", r.Name)
	}
	if r.Score > 100 {
		return Row{}, fmt.Errorf("score over 100 for: %s", r.Name)
	}
	return r, nil
}

func summarize(rows []Row) Summary {
	var total int64
	bestScore := int64(-1)
	bestName := ""
	for _, r := range rows {
		total += r.Score
		if r.Score > bestScore {
			bestScore = r.Score
			bestName = r.Name
		}
	}
	return Summary{Count: int64(len(rows)), Total: total, Highest: bestName}
}

func runPipeline(path string) (Summary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Summary{}, fmt.Errorf("cannot read: %s", path)
	}
	var rows []Row
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		parsed, err := parseRow(line)
		if err != nil {
			return Summary{}, fmt.Errorf("parse error: %s", err)
		}
		valid, err := validateRow(parsed)
		if err != nil {
			return Summary{}, fmt.Errorf("validation error: %s", err)
		}
		rows = append(rows, valid)
	}
	return summarize(rows), nil
}

func main() {
	csv := "Alice,95\nBob,82\nCarol,78\nDave,91\n"
	if err := os.WriteFile("examples/scores.csv", []byte(csv), 0644); err != nil {
		fmt.Println("setup error:", err)
		return
	}
	s, err := runPipeline("examples/scores.csv")
	if err != nil {
		fmt.Println("pipeline failed:", err)
		return
	}
	fmt.Printf("count:   %d\ntotal:   %d\nhighest: %s\n", s.Count, s.Total, s.Highest)
}
