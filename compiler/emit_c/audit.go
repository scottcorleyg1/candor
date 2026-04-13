package emit_c

import (
	"fmt"
	"strings"
)

// AuditEntry records one instance of a Candor feature that has no equivalent
// in the target language (currently C).
type AuditEntry struct {
	Category    string // "effects", "pure", "requires", "ensures", "must", "secret"
	FnName      string // enclosing function name (empty for must{} at stmt level)
	Line        int    // Candor source line number
	Detail      string // e.g. "effects(fs, network)" or "requires b != 0"
	CEquiv      string // C equivalent, or "none"
	Explanation string // one sentence
}

// AuditLog accumulates entries during C emission.
type AuditLog struct {
	SourceFile string
	Target     string // "C" or "Go" — used in the report header
	Entries    []AuditEntry
}

func (l *AuditLog) add(e AuditEntry) {
	l.Entries = append(l.Entries, e)
}

// NewAuditLog creates an AuditLog ready for use outside this package.
func NewAuditLog(sourceFile string) *AuditLog {
	return &AuditLog{SourceFile: sourceFile, Target: "C"}
}

// NewAuditLogGo creates an AuditLog for the Go emitter.
func NewAuditLogGo(sourceFile string) *AuditLog {
	return &AuditLog{SourceFile: sourceFile, Target: "Go"}
}

// AddEntry appends an entry (exported for use by other emit packages).
func (l *AuditLog) AddEntry(e AuditEntry) {
	l.Entries = append(l.Entries, e)
}

// RenderMarkdown produces the audit report as a Markdown string.
func (l *AuditLog) RenderMarkdown() string {
	var sb strings.Builder

	target := l.Target
	if target == "" {
		target = "C"
	}
	fmt.Fprintf(&sb, "## Candor → %s Audit Report\n\n", target)
	fmt.Fprintf(&sb, "**Source:** `%s`\n\n", l.SourceFile)

	// Defined order for categories in the report.
	order := []string{"effects", "pure", "requires", "ensures", "must", "secret"}
	byCategory := make(map[string][]AuditEntry)
	for _, e := range l.Entries {
		byCategory[e.Category] = append(byCategory[e.Category], e)
	}

	totalEntries := len(l.Entries)
	fmt.Fprintf(&sb, "**Audit entries:** %d\n\n", totalEntries)

	if totalEntries == 0 {
		sb.WriteString("No Candor-specific safety features were found in this file.\n")
		fmt.Fprintf(&sb, "The %s output is structurally equivalent to the Candor source.\n", target)
		return sb.String()
	}

	sb.WriteString("---\n\n")

	for _, cat := range order {
		entries := byCategory[cat]
		if len(entries) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "### %s (%d)\n\n", categoryTitle(cat), len(entries))
		for _, e := range entries {
			if e.FnName != "" {
				fmt.Fprintf(&sb, "**`%s`**", e.FnName)
			} else {
				sb.WriteString("**call site**")
			}
			if e.Line > 0 {
				fmt.Fprintf(&sb, " — line %d", e.Line)
			}
			sb.WriteString("\n")
			fmt.Fprintf(&sb, "`%s`\n", e.Detail)
			fmt.Fprintf(&sb, "%s equivalent: %s\n", target, e.CEquiv)
			fmt.Fprintf(&sb, "%s\n\n", e.Explanation)
		}
	}

	// Summary table
	sb.WriteString("---\n\n### Summary\n\n")
	fmt.Fprintf(&sb, "| Feature | Instances | %s enforcement |\n", target)
	sb.WriteString("|---------|-----------|----------------|\n")

	var enforcementSummary map[string]string
	if target == "Go" {
		enforcementSummary = map[string]string{
			"effects":  "None — dropped",
			"pure":     "None — dropped (no pure in Go)",
			"requires": "panic() at runtime (not compile-time)",
			"ensures":  "Not emitted (no postcondition support)",
			"must":     "Not enforced — caller can use _ to discard",
			"secret":   "None — inner type emitted, wrapper dropped",
		}
	} else {
		enforcementSummary = map[string]string{
			"effects":  "None — dropped",
			"pure":     "None — dropped",
			"requires": "assert() in debug builds only",
			"ensures":  "assert() in debug builds only",
			"must":     "Not enforced — silent discard is valid C",
			"secret":   "None — inner type emitted, wrapper dropped",
		}
	}
	cEnforcementSummary := enforcementSummary

	for _, cat := range order {
		entries := byCategory[cat]
		if len(entries) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "| %s | %d | %s |\n",
			categoryTitle(cat), len(entries), cEnforcementSummary[cat])
	}

	fmt.Fprintf(&sb, "\n**What the %s output cannot tell you:** whether this program respects ", target)
	sb.WriteString("its own effect boundaries, whether callers can ignore errors, or whether ")
	sb.WriteString("preconditions hold at every call site. Those properties exist in the ")
	fmt.Fprintf(&sb, "Candor source. They do not exist in the %s output.\n", target)

	return sb.String()
}

func categoryTitle(cat string) string {
	switch cat {
	case "effects":
		return "effects declarations"
	case "pure":
		return "pure declarations"
	case "requires":
		return "requires clauses"
	case "ensures":
		return "ensures clauses"
	case "must":
		return "must{} error handling"
	case "secret":
		return "secret<T> values"
	}
	return cat
}
