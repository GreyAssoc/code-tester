package report

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

const (
	SchemaID    = "gointegrity/report/v1"
	ToolName    = "codeintegrity"
	ToolVersion = "0.4.0"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityNote    Severity = "note"
)

type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
)

type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type Evidence struct {
	Kind     string    `json:"kind,omitempty"`
	Message  string    `json:"message"`
	Location *Location `json:"location,omitempty"`
}

type Finding struct {
	Language    string     `json:"language,omitempty"`
	RuleID      string     `json:"rule_id"`
	Rule        string     `json:"rule"`
	Category    string     `json:"category"`
	Severity    Severity   `json:"severity"`
	Confidence  Confidence `json:"confidence"`
	Message     string     `json:"message"`
	Symbol      string     `json:"symbol"`
	Location    Location   `json:"location"`
	Fingerprint string     `json:"fingerprint"`
	Evidence    []Evidence `json:"evidence,omitempty"`
	// CodeFlows is optional SARIF-shaped witness paths for flow findings.
	CodeFlows []map[string]any `json:"code_flows,omitempty"`
	Suppressed bool            `json:"suppressed,omitempty"`
}

type RuleInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Severity    Severity `json:"severity"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	Count       int      `json:"count"`
}

type TargetResult struct {
	Symbol   string     `json:"symbol"`
	Status   string     `json:"status"`
	Message  string     `json:"message,omitempty"`
	Location *Location  `json:"location,omitempty"`
	Evidence []Evidence `json:"evidence,omitempty"`
}

type CheckResult struct {
	ID      string         `json:"id"`
	Kind    string         `json:"kind"`
	Status  string         `json:"status"`
	Message string         `json:"message,omitempty"`
	Targets []TargetResult `json:"targets,omitempty"`
}

type ToolInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Summary struct {
	Packages     int            `json:"packages"`
	Files        int            `json:"files"`
	Functions    int            `json:"functions"`
	Findings     int            `json:"findings"`
	BySeverity   map[string]int `json:"by_severity,omitempty"`
	Suppressed   int            `json:"suppressed"`
	Baselined    int            `json:"baselined"`
	Checks       int            `json:"checks,omitempty"`
	ByStatus     map[string]int `json:"by_status,omitempty"`
	ByLanguage   map[string]int `json:"by_language,omitempty"`
}

type Report struct {
	Schema      string        `json:"schema"`
	Mode        string        `json:"mode"`
	Tool        ToolInfo      `json:"tool"`
	ConfigHash  string        `json:"config_hash,omitempty"`
	RequestID   string        `json:"request_id,omitempty"`
	RequestHash string        `json:"request_hash,omitempty"`
	Patterns    []string      `json:"patterns,omitempty"`
	Roots       string        `json:"roots,omitempty"`
	FailOn      string        `json:"fail_on,omitempty"`
	Summary     Summary       `json:"summary"`
	Rules       []RuleInfo    `json:"rules,omitempty"`
	Findings    []Finding     `json:"findings,omitempty"`
	Checks      []CheckResult `json:"checks,omitempty"`
}

func New(mode string) *Report {
	return &Report{
		Schema: SchemaID,
		Mode:   mode,
		Tool: ToolInfo{
			Name:    ToolName,
			Version: ToolVersion,
		},
		Summary: Summary{
			BySeverity: map[string]int{},
			ByStatus:   map[string]int{},
		},
	}
}

func Fingerprint(ruleID, symbol, message, file string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%s\n%s", ruleID, symbol, message, file)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func SortFindings(fs []Finding) {
	sort.Slice(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.Language != b.Language {
			return a.Language < b.Language
		}
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		if a.Location.File != b.Location.File {
			return a.Location.File < b.Location.File
		}
		if a.Location.Line != b.Location.Line {
			return a.Location.Line < b.Location.Line
		}
		if a.Location.Column != b.Location.Column {
			return a.Location.Column < b.Location.Column
		}
		if a.Symbol != b.Symbol {
			return a.Symbol < b.Symbol
		}
		return a.Message < b.Message
	})
}

func (r *Report) FinalizeCrawl() {
	SortFindings(r.Findings)
	r.Summary.Findings = len(r.Findings)
	r.Summary.BySeverity = map[string]int{}
	r.Summary.ByLanguage = map[string]int{}
	for _, f := range r.Findings {
		r.Summary.BySeverity[string(f.Severity)]++
		if f.Language != "" {
			r.Summary.ByLanguage[f.Language]++
		}
	}
	counts := map[string]int{}
	for _, f := range r.Findings {
		counts[f.RuleID]++
	}
	for i := range r.Rules {
		r.Rules[i].Count = counts[r.Rules[i].ID]
	}
	sort.Slice(r.Rules, func(i, j int) bool { return r.Rules[i].ID < r.Rules[j].ID })
}

func (r *Report) FinalizeCheck() {
	r.Summary.Checks = len(r.Checks)
	r.Summary.ByStatus = map[string]int{}
	for _, c := range r.Checks {
		r.Summary.ByStatus[c.Status]++
		for i := range c.Targets {
			sort.Slice(c.Targets[i].Evidence, func(a, b int) bool {
				return c.Targets[i].Evidence[a].Message < c.Targets[i].Evidence[b].Message
			})
		}
		sort.Slice(c.Targets, func(i, j int) bool {
			if c.Targets[i].Symbol != c.Targets[j].Symbol {
				return c.Targets[i].Symbol < c.Targets[j].Symbol
			}
			ai, aj := c.Targets[i].Location, c.Targets[j].Location
			if ai == nil && aj == nil {
				return false
			}
			if ai == nil {
				return true
			}
			if aj == nil {
				return false
			}
			if ai.File != aj.File {
				return ai.File < aj.File
			}
			return ai.Line < aj.Line
		})
	}
}

func SeverityRank(s Severity) int {
	switch s {
	case SeverityError:
		return 3
	case SeverityWarning:
		return 2
	case SeverityNote:
		return 1
	default:
		return 0
	}
}

func ParseSeverity(s string) (Severity, error) {
	switch strings.ToLower(s) {
	case "error":
		return SeverityError, nil
	case "warning":
		return SeverityWarning, nil
	case "note":
		return SeverityNote, nil
	case "never":
		return "", nil
	default:
		return "", fmt.Errorf("unknown severity %q", s)
	}
}
