package rules

import (
	"go/ast"
	"path/filepath"
	"strings"

	"gointegrity/report"
)

const ignorePrefix = "gointegrity:ignore"

type ignoreDirective struct {
	Rules  []string
	Reason string
	Line   int
	File   string
	Valid  bool
	Raw    string
}

func CheckSuppressions(ctx Context, en Enablement) ([]Finding, int) {
	if !en.Enabled["SUP001"] {
		return nil, 0
	}
	var out []Finding
	for _, pkg := range ctx.Index.Packages {
		for i, file := range pkg.Syntax {
			filename := ""
			if i < len(pkg.CompiledGoFiles) {
				filename = pkg.CompiledGoFiles[i]
			}
			rel := filename
			if ctx.RootDir != "" {
				if r, err := filepath.Rel(ctx.RootDir, filename); err == nil {
					rel = filepath.ToSlash(r)
				}
			}
			for _, cg := range file.Comments {
				for _, c := range cg.List {
					if !strings.Contains(c.Text, ignorePrefix) {
						continue
					}
					d := parseIgnore(c.Text)
					pos := pkg.Fset.Position(c.Pos())
					d.Line = pos.Line
					d.File = rel
					if d.Valid {
						continue
					}
					msg := "malformed //gointegrity:ignore directive"
					if d.Reason == "" {
						msg = "malformed //gointegrity:ignore: reason is required"
					} else if len(d.Rules) == 0 {
						msg = "malformed //gointegrity:ignore: rule selector is required"
					} else {
						msg = "malformed //gointegrity:ignore: unknown rule selector"
					}
					out = append(out, finding("SUP001", "invalid-suppression", "suppression", en.Severity["SUP001"],
						report.ConfidenceHigh, msg, "", rel, pos.Line, pos.Column))
				}
			}
		}
	}
	return out, 0
}

func parseIgnore(text string) ignoreDirective {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "//")
	text = strings.TrimSpace(text)
	idx := strings.Index(text, ignorePrefix)
	if idx < 0 {
		return ignoreDirective{Valid: false, Raw: text}
	}
	rest := strings.TrimSpace(text[idx+len(ignorePrefix):])
	if rest == "" {
		return ignoreDirective{Valid: false, Raw: text}
	}
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		// need rule + reason
		d := ignoreDirective{Raw: text, Valid: false}
		if len(parts) == 1 {
			d.Rules = splitRules(parts[0])
		}
		return d
	}
	rules := splitRules(parts[0])
	reason := strings.Join(parts[1:], " ")
	d := ignoreDirective{Rules: rules, Reason: reason, Raw: text}
	if reason == "" || len(rules) == 0 {
		d.Valid = false
		return d
	}
	for _, r := range rules {
		if !knownRuleSelector(r) {
			d.Valid = false
			return d
		}
	}
	d.Valid = true
	return d
}

func splitRules(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func knownRuleSelector(sel string) bool {
	if strings.EqualFold(sel, "all") {
		return true
	}
	for _, r := range All {
		if matchRule(sel, r) {
			return true
		}
	}
	return false
}

// Ignored reports whether a finding is covered by a valid nearby ignore directive.
func Ignored(ctx Context, f Finding) bool {
	for _, pkg := range ctx.Index.Packages {
		for i, file := range pkg.Syntax {
			filename := ""
			if i < len(pkg.CompiledGoFiles) {
				filename = pkg.CompiledGoFiles[i]
			}
			rel := filename
			if ctx.RootDir != "" {
				if r, err := filepath.Rel(ctx.RootDir, filename); err == nil {
					rel = filepath.ToSlash(r)
				}
			}
			if rel != f.Location.File && filepath.Base(rel) != filepath.Base(f.Location.File) {
				continue
			}
			for _, cg := range file.Comments {
				for _, c := range cg.List {
					if !strings.Contains(c.Text, ignorePrefix) {
						continue
					}
					d := parseIgnore(c.Text)
					if !d.Valid {
						continue
					}
					pos := pkg.Fset.Position(c.Pos())
					// Covers own line and next line; doc comments cover whole decl (approx: within 2 lines above).
					if pos.Line == f.Location.Line || pos.Line+1 == f.Location.Line ||
						(pos.Line < f.Location.Line && f.Location.Line-pos.Line <= 3 && isDocComment(file, c)) {
						for _, sel := range d.Rules {
							if rule, ok := ByID(f.RuleID); ok && matchRule(sel, rule) {
								return true
							}
							if strings.EqualFold(sel, f.RuleID) || strings.EqualFold(sel, f.Rule) || strings.EqualFold(sel, f.Category) {
								return true
							}
						}
					}
				}
			}
		}
	}
	return false
}

func isDocComment(file *ast.File, c *ast.Comment) bool {
	for _, decl := range file.Decls {
		var doc *ast.CommentGroup
		switch d := decl.(type) {
		case *ast.FuncDecl:
			doc = d.Doc
		case *ast.GenDecl:
			doc = d.Doc
		}
		if doc == nil {
			continue
		}
		for _, cc := range doc.List {
			if cc == c {
				return true
			}
		}
	}
	return false
}
