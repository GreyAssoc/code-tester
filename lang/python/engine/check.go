package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/GreyAssoc/code-tester/report"
)

// Analyze runs the deep Python integrity engine (DEAD/STUB/COH/SUP/FLOW + types).
// files maps project-relative paths to source text.
func Analyze(files map[string]string) []report.Finding {
	parsed := ParseProject(files)
	syms := buildSymbols(parsed)
	graph := BuildCallGraph(parsed)
	reach := graph.Reachable()

	corpus := strings.Builder{}
	for _, src := range files {
		corpus.WriteString(src)
		corpus.WriteByte('\n')
	}
	text := corpus.String()

	var findings []report.Finding
	findings = append(findings, checkSuppressions(parsed)...)
	for _, f := range parsed {
		for _, d := range f.Defs {
			findings = append(findings, checkDef(d, text, syms, reach)...)
		}
		for _, a := range f.Assigns {
			findings = append(findings, checkAssign(a, text, reach)...)
		}
		findings = append(findings, checkModuleConsts(f, text)...)
	}
	findings = append(findings, checkCalls(parsed, syms)...)
	findings = append(findings, checkFlow(parsed)...)
	return findings
}

type symbolTable struct {
	funcs map[string]*Def
}

func buildSymbols(files []*File) *symbolTable {
	st := &symbolTable{funcs: map[string]*Def{}}
	for _, f := range files {
		for _, d := range f.Defs {
			st.funcs[d.Qual] = d
			st.funcs[d.Name] = d
		}
	}
	return st
}

func checkDef(d *Def, corpus string, st *symbolTable, reach map[string]bool) []report.Finding {
	var out []report.Finding
	if d.Kind == DefClass {
		if d.Exported && strings.Count(corpus, d.Name) <= 1 {
			out = append(out, find("PYDEAD003", "unused-type", "dead-code", report.SeverityWarning,
				"class "+d.Name+" is never referenced in the project", d.Qual, d.File, d.Line, d.Col))
		}
		return out
	}

	// Reachability-based dead code (Go DEAD001/002 depth).
	if d.Kind == DefFunc && !reach[d.Qual] && d.Name != "__init__" && d.Name != "__str__" && !strings.HasPrefix(d.Name, "test_") {
		out = append(out, find("PYDEAD001", "unreachable-func", "dead-code", report.SeverityWarning,
			"function not reachable from any root", d.Qual, d.File, d.Line, d.Col))
	}
	if d.Kind == DefMethod && !reach[d.Qual] && d.Name != "__init__" && d.Name != "__str__" && d.Name != "__repr__" &&
		!strings.HasPrefix(d.Name, "test_") {
		out = append(out, find("PYDEAD002", "unreachable-method", "dead-code", report.SeverityWarning,
			"method not reachable from any root", d.Qual, d.File, d.Line, d.Col))
	}

	out = append(out, checkStubs(d)...)
	out = append(out, checkCoh(d)...)

	// Types (extra depth beyond Go).
	if d.RetAnn != "" {
		want := ParseTypeAnn(d.RetAnn)
		for _, got := range inferReturns(d.Body) {
			if !Compatible(got, want) {
				out = append(out, find("PYTYPE001", "return-type-mismatch", "type", report.SeverityWarning,
					fmt.Sprintf("return type %s is incompatible with annotated %s", typeStr(got), d.RetAnn),
					d.Name, d.File, d.Line, d.Col))
				break
			}
		}
	}
	for _, p := range d.Args {
		if p.Ann == "" || p.Default == "" {
			continue
		}
		want := ParseTypeAnn(p.Ann)
		got := inferExpr(p.Default)
		if !Compatible(got, want) {
			out = append(out, find("PYTYPE002", "param-default-mismatch", "type", report.SeverityWarning,
				fmt.Sprintf("default %s incompatible with annotated %s for %s", p.Default, p.Ann, p.Name),
				d.Name+"."+p.Name, d.File, d.Line, d.Col))
		}
	}
	_ = st
	return out
}

func checkStubs(d *Def) []report.Finding {
	var out []report.Finding
	trim := strings.TrimSpace(d.Body)
	body := d.Body

	if regexp.MustCompile(`(?i)raise\s+NotImplementedError\b`).MatchString(body) {
		out = append(out, find("PYSTUB005", "panic-not-implemented", "placeholder", report.SeverityError,
			"raises NotImplementedError", d.Qual, d.File, d.Line, d.Col))
		return out
	}
	if todoOnly(body) {
		out = append(out, find("PYSTUB006", "todo-only-body", "placeholder", report.SeverityError,
			"body is empty except TODO/FIXME comment", d.Qual, d.File, d.Line, d.Col))
		return out
	}
	if regexp.MustCompile(`(?i)return\s+NotImplemented\b`).MatchString(body) ||
		regexp.MustCompile(`(?i)raise\s+\w*Error\(['\"]not implemented`).MatchString(body) {
		out = append(out, find("PYSTUB007", "not-implemented-error", "placeholder", report.SeverityError,
			"returns/raises not-implemented error", d.Qual, d.File, d.Line, d.Col))
		return out
	}
	if trim == "" || trim == "pass" || trim == "..." || trim == "pass\n" {
		out = append(out, find("PYSTUB001", "empty-body", "placeholder", report.SeverityWarning,
			"function body is only pass/ellipsis", d.Qual, d.File, d.Line, d.Col))
		return out
	}
	if onlyReturns(body, func(e string) bool {
		e = strings.TrimSpace(e)
		return e == "{}" || e == "dict()"
	}) {
		out = append(out, find("PYSTUB003", "empty-struct-return", "placeholder", report.SeverityWarning,
			"function body looks like a placeholder (empty-struct-return)", d.Qual, d.File, d.Line, d.Col))
		return out
	}
	if onlyReturns(body, func(e string) bool {
		e = strings.TrimSpace(e)
		return e != "None" && (isNumber(e) || isStringLit(e) || e == "True" || e == "False")
	}) {
		out = append(out, find("PYSTUB004", "hardcoded-literal-return", "placeholder", report.SeverityWarning,
			"function body looks like a placeholder (hardcoded-literal-return)", d.Qual, d.File, d.Line, d.Col))
		return out
	}
	if onlyReturns(body, func(e string) bool {
		e = strings.TrimSpace(e)
		return e == "None" || e == "0" || e == "0.0" || e == "False" || e == "[]" || e == "()" || e == `""` || e == "''"
	}) {
		out = append(out, find("PYSTUB002", "zero-value-return", "placeholder", report.SeverityWarning,
			"function body looks like a placeholder (zero-value-return)", d.Qual, d.File, d.Line, d.Col))
	}
	return out
}

func checkCoh(d *Def) []report.Finding {
	var out []report.Finding
	for _, p := range d.Args {
		if p.Name == "self" || p.Name == "cls" || p.Name == "_" || strings.HasPrefix(p.Name, "_") {
			continue
		}
		if !wordIn(d.Body, p.Name) {
			out = append(out, find("PYCOH001", "param-unused", "coherence", report.SeverityWarning,
				"parameter "+p.Name+" never referenced", d.Qual+"."+p.Name, d.File, d.Line, d.Col))
			continue
		}
		// Only assigned to _  (Go COH002)
		if regexp.MustCompile(`_\s*,?\s*.*\b`+regexp.QuoteMeta(p.Name)+`\b|\b`+regexp.QuoteMeta(p.Name)+`\b.*=\s*_`).MatchString(d.Body) &&
			!regexp.MustCompile(`\breturn\b.*\b`+regexp.QuoteMeta(p.Name)+`\b`).MatchString(d.Body) &&
			strings.Count(d.Body, p.Name) <= 2 {
			out = append(out, find("PYCOH002", "param-blank-assigned", "coherence", report.SeverityWarning,
				"parameter "+p.Name+" only assigned to blank/_", d.Qual+"."+p.Name, d.File, d.Line, d.Col))
		}
	}
	// Constant result on every return path (COH004)
	rets := returnExprs(d.Body)
	if len(rets) > 0 {
		allLit := true
		first := strings.TrimSpace(rets[0])
		for _, r := range rets {
			r = strings.TrimSpace(r)
			if !(isNumber(r) || isStringLit(r) || r == "True" || r == "False" || r == "None") {
				allLit = false
				break
			}
			if r != first {
				allLit = false
				break
			}
		}
		// Skip if body has meaningful work beyond returns (assignments using params).
		if allLit && !hasSideEffectCall(d.Body) {
			out = append(out, find("PYCOH004", "constant-result", "coherence", report.SeverityWarning,
				"non-error result is constant on every return path", d.Qual, d.File, d.Line, d.Col))
		}
	}
	// COH003: param referenced but never affects results/side effects
	for _, p := range d.Args {
		if p.Name == "self" || p.Name == "cls" || p.Name == "_" || strings.HasPrefix(p.Name, "_") {
			continue
		}
		if !wordIn(d.Body, p.Name) {
			continue
		}
		affects := regexp.MustCompile(`\breturn\b[\s\S]*\b`+regexp.QuoteMeta(p.Name)+`\b`).MatchString(d.Body) ||
			regexp.MustCompile(`\([^)]*\b`+regexp.QuoteMeta(p.Name)+`\b[^)]*\)`).MatchString(d.Body) ||
			regexp.MustCompile(`\braise\b.*\b`+regexp.QuoteMeta(p.Name)+`\b`).MatchString(d.Body) ||
			regexp.MustCompile(`\.\w+\s*=\s*`+regexp.QuoteMeta(p.Name)+`\b`).MatchString(d.Body)
		if !affects {
			out = append(out, find("PYCOH003", "param-no-effect", "coherence", report.SeverityWarning,
				"parameter "+p.Name+" value never affects results or side effects", d.Qual+"."+p.Name, d.File, d.Line, d.Col))
		}
	}
	return out
}

func checkAssign(a Assign, corpus string, reach map[string]bool) []report.Finding {
	_ = reach
	if a.Ann == "" || strings.HasPrefix(a.Name, "_") {
		return nil
	}
	if strings.Count(corpus, a.Name) <= 1 {
		return []report.Finding{find("PYDEAD005", "unused-var", "dead-code", report.SeverityWarning,
			"package/module var "+a.Name+" not referenced by reachable code", a.Name, a.File, a.Line, a.Col)}
	}
	return nil
}

func checkModuleConsts(f *File, corpus string) []report.Finding {
	var out []report.Finding
	re := regexp.MustCompile(`(?m)^([A-Z][A-Z0-9_]*)\s*=\s*.+$`)
	for _, m := range re.FindAllStringSubmatchIndex(f.Source, -1) {
		name := f.Source[m[2]:m[3]]
		line, col := 1, 1
		for i := 0; i < m[0] && i < len(f.Source); i++ {
			if f.Source[i] == '\n' {
				line++
				col = 1
			} else {
				col++
			}
		}
		if strings.Count(corpus, name) <= 1 {
			out = append(out, find("PYDEAD004", "unused-const", "dead-code", report.SeverityWarning,
				"const "+name+" not referenced by reachable code", name, f.Path, line, col))
		}
	}
	return out
}

var (
	reIgnoreOK  = regexp.MustCompile(`(?i)#\s*(?:code|go)?integrity:\s*ignore(?:\s+[A-Z]{2,}[0-9]{3})?\b`)
	reIgnoreBad = regexp.MustCompile(`(?i)#\s*(?:code|go)?integrity\s*:\s*\S+`)
)

func checkSuppressions(files []*File) []report.Finding {
	var out []report.Finding
	for _, f := range files {
		for i, line := range strings.Split(f.Source, "\n") {
			if !strings.Contains(strings.ToLower(line), "integrity") {
				continue
			}
			if reIgnoreOK.MatchString(line) {
				continue
			}
			if reIgnoreBad.MatchString(line) || regexp.MustCompile(`(?i)#\s*(?:code|go)?integrity`).MatchString(line) {
				out = append(out, find("PYSUP001", "invalid-suppression", "suppression", report.SeverityNote,
					"malformed codeintegrity:ignore directive", f.Path, f.Path, i+1, 1))
			}
		}
	}
	return out
}

func checkCalls(files []*File, st *symbolTable) []report.Finding {
	var out []report.Finding
	seen := map[string]bool{}
	for _, f := range files {
		for _, d := range f.Defs {
			for _, cal := range st.funcs {
				if cal.Name == d.Name || cal.Kind == DefClass {
					continue
				}
				required := 0
				for _, p := range cal.Args {
					if p.Name == "self" || p.Name == "cls" || p.Default != "" {
						continue
					}
					required++
				}
				if required == 0 {
					continue
				}
				pat := regexp.MustCompile(`\b` + regexp.QuoteMeta(cal.Name) + `\s*\(\s*\)`)
				if pat.MatchString(d.Body) {
					key := d.File + cal.Name + "arity"
					if seen[key] {
						continue
					}
					seen[key] = true
					out = append(out, find("PYTYPE003", "call-arity", "type", report.SeverityWarning,
						fmt.Sprintf("call to %s() missing %d required argument(s)", cal.Name, required),
						cal.Name, d.File, d.Line, d.Col))
				}
			}
		}
	}
	return out
}

func onlyReturns(body string, pred func(string) bool) bool {
	rets := returnExprs(body)
	if len(rets) == 0 {
		return false
	}
	// Body should be mostly returns / pass
	nonEmpty := 0
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || t == "pass" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "return") {
			continue
		}
		nonEmpty++
	}
	if nonEmpty > 1 {
		return false
	}
	for _, r := range rets {
		if !pred(r) {
			return false
		}
	}
	return true
}

func returnExprs(body string) []string {
	var out []string
	re := regexp.MustCompile(`(?m)^\s*return\s+(.+?)(?:\s*#.*)?$`)
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

func hasSideEffectCall(body string) bool {
	return regexp.MustCompile(`\b(print|open|write|execute|send|emit|log)\s*\(`).MatchString(body)
}

func isStringLit(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return true
		}
	}
	return false
}

func inferReturns(body string) []Type {
	var out []Type
	for _, e := range returnExprs(body) {
		out = append(out, inferExpr(e))
	}
	if len(out) == 0 && regexp.MustCompile(`(?m)^\s*return\s*$`).MatchString(body) {
		out = append(out, NoneType())
	}
	return out
}

func inferExpr(expr string) Type {
	expr = strings.TrimSpace(expr)
	switch {
	case expr == "" || expr == "None":
		return NoneType()
	case expr == "True" || expr == "False":
		return Type{Name: "bool"}
	case isStringLit(expr) || (len(expr) >= 2 && expr[0] == 'f' && (expr[1] == '"' || expr[1] == '\'')):
		return Type{Name: "str"}
	case isNumber(expr):
		if strings.ContainsAny(expr, ".eE") {
			return Type{Name: "float"}
		}
		return Type{Name: "int"}
	case strings.HasPrefix(expr, "["):
		return Type{Name: "list", Unknown: true}
	case strings.HasPrefix(expr, "{"):
		return Type{Name: "dict", Unknown: true}
	default:
		return AnyType()
	}
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if c >= '0' && c <= '9' {
			continue
		}
		if c == '.' || c == '-' && i == 0 {
			continue
		}
		return false
	}
	return true
}

func typeStr(t Type) string {
	if t.Unknown {
		return "Any"
	}
	return t.Name
}

func todoOnly(body string) bool {
	trim := strings.TrimSpace(body)
	if !regexp.MustCompile(`(?i)\b(TODO|FIXME|XXX)\b`).MatchString(trim) {
		return false
	}
	meaningful := 0
	for _, line := range strings.Split(trim, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || t == "pass" || t == "..." {
			continue
		}
		meaningful++
	}
	return meaningful == 0
}

func wordIn(body, name string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).MatchString(body)
}

func find(id, name, cat string, sev report.Severity, msg, symbol, file string, line, col int) report.Finding {
	return report.Finding{
		Language: "python", RuleID: id, Rule: name, Category: cat, Severity: sev,
		Confidence: report.ConfidenceMedium, Message: msg, Symbol: symbol,
		Location: report.Location{File: file, Line: line, Column: col},
		Fingerprint: report.Fingerprint(id, symbol, msg, file),
	}
}

// Rules exported for the driver — mirrors Go DEAD/STUB/COH/SUP/FLOW plus type rules.
func Rules() []report.RuleInfo {
	return []report.RuleInfo{
		{ID: "PYDEAD001", Name: "unreachable-func", Category: "dead-code", Severity: report.SeverityWarning, Description: "function not reachable from any root", Enabled: true},
		{ID: "PYDEAD002", Name: "unreachable-method", Category: "dead-code", Severity: report.SeverityWarning, Description: "method not reachable from any root", Enabled: true},
		{ID: "PYDEAD003", Name: "unused-type", Category: "dead-code", Severity: report.SeverityWarning, Description: "class not referenced by reachable code", Enabled: true},
		{ID: "PYDEAD004", Name: "unused-const", Category: "dead-code", Severity: report.SeverityWarning, Description: "module const not referenced", Enabled: true},
		{ID: "PYDEAD005", Name: "unused-var", Category: "dead-code", Severity: report.SeverityWarning, Description: "annotated module var not referenced", Enabled: true},
		{ID: "PYSTUB001", Name: "empty-body", Category: "placeholder", Severity: report.SeverityWarning, Description: "pass/ellipsis body", Enabled: true},
		{ID: "PYSTUB002", Name: "zero-value-return", Category: "placeholder", Severity: report.SeverityWarning, Description: "body only returns zero/None values", Enabled: true},
		{ID: "PYSTUB003", Name: "empty-struct-return", Category: "placeholder", Severity: report.SeverityWarning, Description: "body only returns empty dict/struct", Enabled: true},
		{ID: "PYSTUB004", Name: "hardcoded-literal-return", Category: "placeholder", Severity: report.SeverityWarning, Description: "body only returns hardcoded literals", Enabled: true},
		{ID: "PYSTUB005", Name: "panic-not-implemented", Category: "placeholder", Severity: report.SeverityError, Description: "raises NotImplementedError", Enabled: true},
		{ID: "PYSTUB006", Name: "todo-only-body", Category: "placeholder", Severity: report.SeverityError, Description: "body is empty except TODO/FIXME", Enabled: true},
		{ID: "PYSTUB007", Name: "not-implemented-error", Category: "placeholder", Severity: report.SeverityError, Description: "returns/raises not-implemented error", Enabled: true},
		{ID: "PYCOH001", Name: "param-unused", Category: "coherence", Severity: report.SeverityWarning, Description: "parameter never referenced", Enabled: true},
		{ID: "PYCOH002", Name: "param-blank-assigned", Category: "coherence", Severity: report.SeverityWarning, Description: "parameter only assigned to blank", Enabled: true},
		{ID: "PYCOH003", Name: "param-no-effect", Category: "coherence", Severity: report.SeverityWarning, Description: "parameter never affects results or side effects", Enabled: true},
		{ID: "PYCOH004", Name: "constant-result", Category: "coherence", Severity: report.SeverityWarning, Description: "result is constant on every return path", Enabled: true},
		{ID: "PYSUP001", Name: "invalid-suppression", Category: "suppression", Severity: report.SeverityNote, Description: "malformed codeintegrity:ignore directive", Enabled: true},
		{ID: "PYFLOW001", Name: "undeclared-sensitive-flow", Category: "flow", Severity: report.SeverityWarning, Description: "input reaches sensitive sink without sanitizer", Enabled: true},
		{ID: "PYFLOW002", Name: "dropped-input", Category: "flow", Severity: report.SeverityWarning, Description: "input never reaches return, sink, or store", Enabled: true},
		{ID: "PYTYPE001", Name: "return-type-mismatch", Category: "type", Severity: report.SeverityWarning, Description: "return incompatible with annotation", Enabled: true},
		{ID: "PYTYPE002", Name: "param-default-mismatch", Category: "type", Severity: report.SeverityWarning, Description: "default incompatible with annotation", Enabled: true},
		{ID: "PYTYPE003", Name: "call-arity", Category: "type", Severity: report.SeverityWarning, Description: "call missing required arguments", Enabled: true},
	}
}
