package flow

import (
	"fmt"
	"strings"

	"gointegrity/report"
)

const (
	RuleSensitiveFlow = "FLOW001"
	RuleDroppedInput  = "FLOW002"
)

// Finding is a flow-graph finding with optional witness path for SARIF codeFlows.
type Finding struct {
	report.Finding
	Witness   []string `json:"witness,omitempty"`
	PathHash  string   `json:"path_hash,omitempty"`
	SourceID  string   `json:"source_id,omitempty"`
	SinkID    string   `json:"sink_id,omitempty"`
	Approx    bool     `json:"approx,omitempty"`
	Suppressed bool    `json:"suppressed,omitempty"`
}

type AnalyzeOptions struct {
	Policies     []Policy
	Suppressions *SuppressionFile
	AllowedSinks map[string]bool // sink node roles/names allowed for secrets
	PrimaryPkgs  map[string]bool // limit findings to these import paths
}

// AnalyzeRules runs FLOW001/FLOW002 and evaluates policies.
func (g *Graph) AnalyzeRules(opts AnalyzeOptions) (findings []Finding, verdicts []PolicyVerdict) {
	verdicts = g.EvaluatePolicies(opts.Policies)

	// Build allowed sinks from policies.
	allowed := map[string]bool{}
	for k, v := range opts.AllowedSinks {
		allowed[k] = v
	}
	for _, p := range opts.Policies {
		if p.Kind == PolicyAllowedSinks {
			for _, s := range p.Sinks {
				for _, id := range g.resolveNode(s) {
					allowed[id] = true
				}
			}
		}
	}

	// FLOW001: labeled secret/user_input reaches sensitive sink without policy allow.
	for _, sid := range g.SortedNodeIDs() {
		src := g.Nodes[sid]
		if src == nil || (src.Kind != NodeSource && src.Kind != NodeParam && src.Kind != NodeCell) {
			continue
		}
		if src.Kind != NodeCell && !inPrimary(src.Symbol, opts.PrimaryPkgs) {
			continue
		}
		hasSensitive := false
		for _, l := range src.Labels {
			if l == string(LabelSecret) || l == string(LabelUserInput) {
				hasSensitive = true
				break
			}
		}
		if !hasSensitive {
			continue
		}
		for _, kid := range g.SortedNodeIDs() {
			sink := g.Nodes[kid]
			if sink == nil || sink.Kind != NodeSink {
				continue
			}
			path := g.ShortestPath(sid, kid)
			if len(path) == 0 {
				continue
			}
			if allowed[kid] {
				continue
			}
			// must_not_reach policies already cover explicit bans; FLOW001 is undeclared.
			ph := PathHash(path)
			msg := fmt.Sprintf("%s labeled value reaches sensitive sink %s",
				strings.Join(src.Labels, ","), sink.Symbol)
			f := Finding{
				Finding: report.Finding{
					RuleID:      RuleSensitiveFlow,
					Rule:        "undeclared-sensitive-flow",
					Category:    "flow",
					Severity:    report.SeverityWarning,
					Confidence:  report.ConfidenceMedium,
					Message:     msg,
					Symbol:      src.Symbol,
					Fingerprint: report.Fingerprint(RuleSensitiveFlow, src.Symbol+"->"+sink.Symbol, msg, ""),
				},
				Witness:  path,
				PathHash: ph,
				SourceID: sid,
				SinkID:   kid,
				Approx:   pathApprox(g, path),
			}
			if opts.Suppressions != nil && opts.Suppressions.Matches(RuleSensitiveFlow, sid, kid, ph) {
				f.Suppressed = true
			}
			findings = append(findings, f)
		}
	}

	// FLOW002: user_input / param never reaches any result, sink, or cell store effect.
	for _, sid := range g.SortedNodeIDs() {
		src := g.Nodes[sid]
		if src == nil {
			continue
		}
		if src.Kind != NodeParam && src.Kind != NodeSource {
			continue
		}
		if !inPrimary(src.Symbol, opts.PrimaryPkgs) {
			continue
		}
		isInput := src.Kind == NodeSource
		for _, l := range src.Labels {
			if l == string(LabelUserInput) {
				isInput = true
			}
		}
		if src.Kind == NodeParam && (src.Role == "recv" || strings.HasSuffix(src.Role, "@ctx")) {
			continue
		}
		if src.Kind == NodeParam {
			isInput = true
		}
		if !isInput {
			continue
		}
		reaches := false
		for _, kid := range g.SortedNodeIDs() {
			n := g.Nodes[kid]
			if n == nil {
				continue
			}
			if n.Kind == NodeResult || n.Kind == NodeSink || n.Kind == NodeCell {
				if g.CanReach(sid, kid) {
					reaches = true
					break
				}
			}
		}
		if reaches {
			continue
		}
		msg := fmt.Sprintf("input %s never reaches an output, sink, or memory cell", src.Symbol)
		f := Finding{
			Finding: report.Finding{
				RuleID:      RuleDroppedInput,
				Rule:        "dropped-input",
				Category:    "flow",
				// D-015-03: strict dropped-input — error + high confidence.
				Severity:    report.SeverityError,
				Confidence:  report.ConfidenceHigh,
				Message:     msg,
				Symbol:      src.Symbol,
				Fingerprint: report.Fingerprint(RuleDroppedInput, src.Symbol, msg, ""),
			},
			SourceID: sid,
		}
		if opts.Suppressions != nil && opts.Suppressions.Matches(RuleDroppedInput, sid, "", "") {
			f.Suppressed = true
		}
		findings = append(findings, f)
	}
	return findings, verdicts
}

func inPrimary(symbol string, primary map[string]bool) bool {
	if len(primary) == 0 {
		return true
	}
	for pkg := range primary {
		if symbol == pkg || strings.HasPrefix(symbol, pkg+".") {
			return true
		}
	}
	return false
}
