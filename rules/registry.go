package rules

import (
	"strings"

	"gointegrity/callgraph"
	"gointegrity/config"
	"gointegrity/report"
)

type Rule struct {
	ID          string
	Name        string
	Category    string
	Severity    report.Severity
	Description string
}

type Context struct {
	Index   *callgraph.Index
	Reach   *callgraph.Reachability
	Config  config.Config
	RootDir string
}

type Finding = report.Finding

var All = []Rule{
	{ID: "DEAD001", Name: "unreachable-func", Category: "dead-code", Severity: report.SeverityWarning, Description: "function not reachable from any root"},
	{ID: "DEAD002", Name: "unreachable-method", Category: "dead-code", Severity: report.SeverityWarning, Description: "method not reachable from any root"},
	{ID: "DEAD003", Name: "unused-type", Category: "dead-code", Severity: report.SeverityWarning, Description: "type not referenced by reachable code"},
	{ID: "DEAD004", Name: "unused-const", Category: "dead-code", Severity: report.SeverityWarning, Description: "const not referenced by reachable code"},
	{ID: "DEAD005", Name: "unused-var", Category: "dead-code", Severity: report.SeverityWarning, Description: "package var not referenced by reachable code"},
	{ID: "STUB001", Name: "empty-body", Category: "placeholder", Severity: report.SeverityWarning, Description: "empty function body"},
	{ID: "STUB002", Name: "zero-value-return", Category: "placeholder", Severity: report.SeverityWarning, Description: "body only returns zero values"},
	{ID: "STUB003", Name: "empty-struct-return", Category: "placeholder", Severity: report.SeverityWarning, Description: "body only returns empty struct literal"},
	{ID: "STUB004", Name: "hardcoded-literal-return", Category: "placeholder", Severity: report.SeverityWarning, Description: "body only returns hardcoded literals"},
	{ID: "STUB005", Name: "panic-not-implemented", Category: "placeholder", Severity: report.SeverityError, Description: "panic with not-implemented message"},
	{ID: "STUB006", Name: "todo-only-body", Category: "placeholder", Severity: report.SeverityError, Description: "body is empty except TODO/FIXME comment"},
	{ID: "STUB007", Name: "not-implemented-error", Category: "placeholder", Severity: report.SeverityError, Description: "returns not-implemented error"},
	{ID: "COH001", Name: "param-unused", Category: "coherence", Severity: report.SeverityWarning, Description: "parameter never referenced"},
	{ID: "COH002", Name: "param-blank-assigned", Category: "coherence", Severity: report.SeverityWarning, Description: "parameter only assigned to blank identifier"},
	{ID: "COH003", Name: "param-no-effect", Category: "coherence", Severity: report.SeverityWarning, Description: "parameter value never affects results or side effects"},
	{ID: "COH004", Name: "constant-result", Category: "coherence", Severity: report.SeverityWarning, Description: "non-error result is constant on every return path"},
	{ID: "SUP001", Name: "invalid-suppression", Category: "suppression", Severity: report.SeverityNote, Description: "malformed gointegrity:ignore directive"},
	{ID: "FLOW001", Name: "undeclared-sensitive-flow", Category: "flow", Severity: report.SeverityWarning, Description: "labeled value reaches a sensitive sink without an allowing policy"},
	{ID: "FLOW002", Name: "dropped-input", Category: "flow", Severity: report.SeverityWarning, Description: "input value never reaches an output, sink, or memory cell"},
}

func ByID(id string) (Rule, bool) {
	for _, r := range All {
		if r.ID == id {
			return r, true
		}
	}
	return Rule{}, false
}

type Enablement struct {
	Enabled  map[string]bool
	Severity map[string]report.Severity
}

func ResolveEnablement(cfg config.Config, enable, disable []string) Enablement {
	e := Enablement{
		Enabled:  map[string]bool{},
		Severity: map[string]report.Severity{},
	}
	for _, r := range All {
		e.Enabled[r.ID] = true
		e.Severity[r.ID] = r.Severity
	}
	apply := func(sel string, enabled *bool, sev string) {
		for _, r := range All {
			if matchRule(sel, r) {
				if enabled != nil {
					e.Enabled[r.ID] = *enabled
				}
				if sev != "" {
					if s, err := report.ParseSeverity(sev); err == nil && s != "" {
						e.Severity[r.ID] = s
					}
				}
			}
		}
	}
	// Config first (broad then specific — map iteration nondeterministic; apply all).
	keys := make([]string, 0, len(cfg.Rules))
	for k := range cfg.Rules {
		keys = append(keys, k)
	}
	// Prefer longer / more specific keys last: sort by length ascending then id.
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if len(keys[j]) < len(keys[i]) || (len(keys[j]) == len(keys[i]) && keys[j] < keys[i]) {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	for _, k := range keys {
		ov := cfg.Rules[k]
		apply(k, ov.Enabled, ov.Severity)
	}
	for _, sel := range enable {
		t := true
		apply(sel, &t, "")
	}
	for _, sel := range disable {
		f := false
		apply(sel, &f, "")
	}
	return e
}

func matchRule(sel string, r Rule) bool {
	sel = strings.TrimSpace(sel)
	if sel == "all" {
		return true
	}
	if strings.EqualFold(sel, r.ID) || strings.EqualFold(sel, r.Name) || strings.EqualFold(sel, r.Category) {
		return true
	}
	if strings.HasPrefix(r.ID, strings.ToUpper(sel)) {
		return true
	}
	return strings.HasPrefix(strings.ToUpper(r.ID), strings.ToUpper(sel))
}

func RunCrawl(ctx Context, en Enablement) (findings []Finding, suppressed int) {
	var stubbed map[string]bool
	var all []Finding

	supFindings, supCount := CheckSuppressions(ctx, en)
	all = append(all, supFindings...)
	suppressed += supCount

	if anyEnabled(en, "DEAD") {
		all = append(all, CheckDead(ctx, en)...)
	}
	if anyEnabled(en, "STUB") {
		var stubs []Finding
		stubs, stubbed = CheckStubs(ctx, en)
		all = append(all, stubs...)
	}
	if anyEnabled(en, "COH") {
		all = append(all, CheckCoherence(ctx, en, stubbed)...)
	}

	// Apply inline suppressions on findings.
	out := make([]Finding, 0, len(all))
	for _, f := range all {
		if !en.Enabled[f.RuleID] {
			continue
		}
		if f.Severity == "" {
			f.Severity = en.Severity[f.RuleID]
		}
		if Ignored(ctx, f) {
			suppressed++
			continue
		}
		out = append(out, f)
	}
	return out, suppressed
}

func anyEnabled(en Enablement, prefix string) bool {
	for id, on := range en.Enabled {
		if on && strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
}

func RuleInfos(en Enablement) []report.RuleInfo {
	out := make([]report.RuleInfo, 0, len(All))
	for _, r := range All {
		out = append(out, report.RuleInfo{
			ID:          r.ID,
			Name:        r.Name,
			Category:    r.Category,
			Severity:    en.Severity[r.ID],
			Description: r.Description,
			Enabled:     en.Enabled[r.ID],
		})
	}
	return out
}
