package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func Render(w io.Writer, r *Report, format string) error {
	switch strings.ToLower(format) {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(r)
	case "sarif":
		return RenderSARIF(w, r)
	case "text", "":
		return RenderText(w, r)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}

func RenderText(w io.Writer, r *Report) error {
	if r.Mode == "check" {
		return renderCheckText(w, r)
	}
	return renderCrawlText(w, r)
}

func renderCrawlText(w io.Writer, r *Report) error {
	if len(r.Findings) == 0 {
		fmt.Fprintf(w, "gointegrity crawl: 0 findings\n")
		fmt.Fprintf(w, "packages=%d files=%d functions=%d suppressed=%d baselined=%d\n",
			r.Summary.Packages, r.Summary.Files, r.Summary.Functions, r.Summary.Suppressed, r.Summary.Baselined)
		return nil
	}
	var lastRule string
	for _, f := range r.Findings {
		if f.RuleID != lastRule {
			if lastRule != "" {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "%s %s (%s)\n", f.RuleID, f.Rule, f.Severity)
			lastRule = f.RuleID
		}
		fmt.Fprintf(w, "  %s  %s:%d:%d  [%s]\n", f.Symbol, f.Location.File, f.Location.Line, f.Location.Column, f.Confidence)
		fmt.Fprintf(w, "    %s\n", f.Message)
		for _, e := range f.Evidence {
			if e.Location != nil {
				fmt.Fprintf(w, "    - %s (%s:%d:%d)\n", e.Message, e.Location.File, e.Location.Line, e.Location.Column)
			} else {
				fmt.Fprintf(w, "    - %s\n", e.Message)
			}
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "summary: findings=%d error=%d warning=%d note=%d suppressed=%d baselined=%d\n",
		r.Summary.Findings,
		r.Summary.BySeverity["error"],
		r.Summary.BySeverity["warning"],
		r.Summary.BySeverity["note"],
		r.Summary.Suppressed,
		r.Summary.Baselined,
	)
	return nil
}

func renderCheckText(w io.Writer, r *Report) error {
	for _, c := range r.Checks {
		status := strings.ToUpper(c.Status)
		pass, fail, unres, errn := 0, 0, 0, 0
		for _, t := range c.Targets {
			switch t.Status {
			case "pass":
				pass++
			case "fail":
				fail++
			case "unresolved":
				unres++
			case "error":
				errn++
			}
		}
		fmt.Fprintf(w, "[%s] %s (%s): %d target(s): %d pass, %d fail, %d unresolved, %d error\n",
			status, c.ID, c.Kind, len(c.Targets), pass, fail, unres, errn)
		if c.Message != "" && len(c.Targets) == 0 {
			fmt.Fprintf(w, "  %s\n", c.Message)
		}
		for _, t := range c.Targets {
			loc := ""
			if t.Location != nil {
				loc = fmt.Sprintf("  %s:%d:%d", t.Location.File, t.Location.Line, t.Location.Column)
			}
			fmt.Fprintf(w, "  %-10s %s%s\n", t.Status, t.Symbol, loc)
			if t.Message != "" {
				fmt.Fprintf(w, "             %s\n", t.Message)
			}
			for _, e := range t.Evidence {
				if e.Location != nil {
					fmt.Fprintf(w, "             - %s (%s:%d:%d)\n", e.Message, e.Location.File, e.Location.Line, e.Location.Column)
				} else {
					fmt.Fprintf(w, "             - %s\n", e.Message)
				}
			}
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "summary: checks=%d pass=%d fail=%d unresolved=%d error=%d\n",
		r.Summary.Checks,
		r.Summary.ByStatus["pass"],
		r.Summary.ByStatus["fail"],
		r.Summary.ByStatus["unresolved"],
		r.Summary.ByStatus["error"],
	)
	return nil
}

func RenderSARIF(w io.Writer, r *Report) error {
	doc := map[string]any{
		"$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		"version": "2.1.0",
		"runs": []any{
			map[string]any{
				"tool": map[string]any{
					"driver": map[string]any{
						"name":           ToolName,
						"version":        ToolVersion,
						"informationUri": "https://github.com/gointegrity/gointegrity",
						"rules":          sarifRules(r),
					},
				},
				"results": sarifResults(r),
			},
		},
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}

func sarifRules(r *Report) []any {
	out := []any{}
	seen := map[string]bool{}
	for _, rule := range r.Rules {
		seen[rule.ID] = true
		out = append(out, map[string]any{
			"id":               rule.ID,
			"name":             rule.Name,
			"shortDescription": map[string]any{"text": rule.Description},
			"defaultConfiguration": map[string]any{
				"level": sarifLevel(rule.Severity),
			},
			"properties": map[string]any{
				"category": rule.Category,
			},
		})
	}
	if r.Mode == "check" {
		kinds := map[string]bool{}
		for _, c := range r.Checks {
			kinds[c.Kind] = true
		}
		for k := range kinds {
			out = append(out, map[string]any{
				"id":               k,
				"name":             k,
				"shortDescription": map[string]any{"text": "check kind " + k},
			})
		}
	}
	return out
}

func sarifResults(r *Report) []any {
	out := []any{}
	if r.Mode == "check" {
		for _, c := range r.Checks {
			if len(c.Targets) == 0 {
				kind, level := sarifStatus(c.Status)
				out = append(out, map[string]any{
					"ruleId": c.Kind,
					"kind":   kind,
					"level":  level,
					"message": map[string]any{"text": firstNonEmpty(c.Message, c.ID)},
					"properties": map[string]any{
						"check_id": c.ID,
						"status":   c.Status,
					},
				})
				continue
			}
			for _, t := range c.Targets {
				kind, level := sarifStatus(t.Status)
				res := map[string]any{
					"ruleId": c.Kind,
					"kind":   kind,
					"level":  level,
					"message": map[string]any{"text": firstNonEmpty(t.Message, c.ID)},
					"properties": map[string]any{
						"check_id": c.ID,
						"status":   t.Status,
						"symbol":   t.Symbol,
					},
				}
				if t.Location != nil {
					res["locations"] = []any{sarifLocation(*t.Location)}
				}
				out = append(out, res)
			}
		}
		return out
	}
	for _, f := range r.Findings {
		res := map[string]any{
			"ruleId": f.RuleID,
			"level":  sarifLevel(f.Severity),
			"message": map[string]any{"text": f.Message},
			"locations": []any{sarifLocation(f.Location)},
			"partialFingerprints": map[string]any{
				"gointegrity/v1": f.Fingerprint,
			},
			"properties": map[string]any{
				"confidence": f.Confidence,
				"symbol":     f.Symbol,
			},
		}
		if f.Suppressed {
			res["suppressions"] = []any{map[string]any{"kind": "external"}}
		}
		if len(f.CodeFlows) > 0 {
			res["codeFlows"] = f.CodeFlows
		}
		if len(f.Evidence) > 0 {
			rels := []any{}
			for _, e := range f.Evidence {
				if e.Location == nil {
					continue
				}
				rels = append(rels, map[string]any{
					"message": map[string]any{"text": e.Message},
					"physicalLocation": map[string]any{
						"artifactLocation": map[string]any{"uri": e.Location.File},
						"region": map[string]any{
							"startLine":   e.Location.Line,
							"startColumn": e.Location.Column,
						},
					},
				})
			}
			if len(rels) > 0 {
				res["relatedLocations"] = rels
			}
		}
		out = append(out, res)
	}
	return out
}

func sarifLocation(loc Location) map[string]any {
	return map[string]any{
		"physicalLocation": map[string]any{
			"artifactLocation": map[string]any{"uri": loc.File},
			"region": map[string]any{
				"startLine":   loc.Line,
				"startColumn": loc.Column,
			},
		},
	}
}

func sarifLevel(s Severity) string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	default:
		return "note"
	}
}

func sarifStatus(status string) (kind, level string) {
	switch status {
	case "pass":
		return "pass", "none"
	case "fail":
		return "fail", "error"
	case "unresolved":
		return "review", "warning"
	case "error":
		return "fail", "error"
	default:
		return "fail", "error"
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
