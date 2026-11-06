package engine

import (
	"fmt"
	"regexp"
	"strings"

	"gointegrity/report"
)

// Analyze runs the deep JS/TS integrity engine (DEAD/STUB/COH/SUP/FLOW + types).
func Analyze(files map[string]string) []report.Finding {
	parsed := ParseProject(files)
	imports := map[string]bool{}
	for _, f := range parsed {
		for _, im := range f.Imports {
			for _, n := range im.Names {
				imports[n] = true
			}
		}
	}
	corpus := strings.Builder{}
	for _, s := range files {
		corpus.WriteString(s)
		corpus.WriteByte('\n')
	}
	text := corpus.String()
	funcs := map[string]*Func{}
	for _, f := range parsed {
		for _, fn := range f.Funcs {
			funcs[fn.Name] = fn
		}
	}
	graph := BuildCallGraph(parsed)
	reach := graph.Reachable()

	var out []report.Finding
	out = append(out, checkSuppressions(parsed)...)
	for _, f := range parsed {
		for _, fn := range f.Funcs {
			out = append(out, checkFunc(fn, text, imports, reach)...)
		}
		for _, c := range f.Classes {
			keyRef := strings.Count(text, c.Name)
			if c.Exported && !imports[c.Name] && keyRef <= 1 {
				out = append(out, find("JSDEAD003", "unused-type", "dead-code", report.SeverityWarning,
					"exported class "+c.Name+" is never referenced", c.Name, c.File, c.Line, c.Col))
			}
		}
		out = append(out, checkConsts(f, text)...)
		if f.IsTS {
			out = append(out, checkTSCalls(f, funcs)...)
		}
	}
	out = append(out, checkFlow(parsed)...)
	return out
}

func checkFunc(fn *Func, corpus string, imports map[string]bool, reach map[string]bool) []report.Finding {
	var out []report.Finding
	key := fn.File + "::" + fn.Name

	if !reach[key] && !fn.Exported {
		out = append(out, find("JSDEAD001", "unreachable-func", "dead-code", report.SeverityWarning,
			"function not reachable from any root", fn.Name, fn.File, fn.Line, fn.Col))
	}
	if fn.Exported && !imports[fn.Name] && !crossRef(corpus, fn) && !reach[key] {
		out = append(out, find("JSDEAD001", "unreachable-func", "dead-code", report.SeverityWarning,
			"exported symbol "+fn.Name+" is never referenced elsewhere", fn.Name, fn.File, fn.Line, fn.Col))
	}

	out = append(out, checkStubs(fn)...)
	out = append(out, checkCoh(fn)...)

	for _, p := range fn.Params {
		if p.Type != "" && p.Default != "" {
			want := ParseType(p.Type)
			got := inferExpr(p.Default)
			if !Compatible(got, want) {
				out = append(out, find("JSTYPE002", "param-default-mismatch", "type", report.SeverityWarning,
					fmt.Sprintf("default %s incompatible with type %s", p.Default, p.Type),
					fn.Name+"."+p.Name, fn.File, fn.Line, fn.Col))
			}
		}
	}
	if fn.RetType != "" {
		want := ParseType(fn.RetType)
		for _, got := range inferReturns(fn.Body) {
			if !Compatible(got, want) {
				out = append(out, find("JSTYPE001", "return-type-mismatch", "type", report.SeverityWarning,
					fmt.Sprintf("return type %s incompatible with annotated %s", got.Name, fn.RetType),
					fn.Name, fn.File, fn.Line, fn.Col))
				break
			}
		}
	}
	return out
}

func checkStubs(fn *Func) []report.Finding {
	var out []report.Finding
	trimBody := strings.TrimSpace(fn.Body)
	body := fn.Body

	if regexp.MustCompile(`(?i)throw new Error\(['\"]not implemented['\"]\)`).MatchString(body) ||
		regexp.MustCompile(`(?i)throw new Error\(['\"]TODO`).MatchString(body) {
		out = append(out, find("JSSTUB005", "panic-not-implemented", "placeholder", report.SeverityError,
			"throws not-implemented error", fn.Name, fn.File, fn.Line, fn.Col))
		return out
	}
	if todoOnlyJS(body) {
		out = append(out, find("JSSTUB006", "todo-only-body", "placeholder", report.SeverityError,
			"body is empty except TODO/FIXME comment", fn.Name, fn.File, fn.Line, fn.Col))
		return out
	}
	if regexp.MustCompile(`(?i)return new Error\(['\"]not implemented`).MatchString(body) ||
		regexp.MustCompile(`(?i)Promise\.reject\(['\"]not implemented`).MatchString(body) {
		out = append(out, find("JSSTUB007", "not-implemented-error", "placeholder", report.SeverityError,
			"returns not-implemented error", fn.Name, fn.File, fn.Line, fn.Col))
		return out
	}
	if trimBody == "" {
		out = append(out, find("JSSTUB001", "empty-body", "placeholder", report.SeverityWarning,
			"function body is empty", fn.Name, fn.File, fn.Line, fn.Col))
		return out
	}
	if onlyReturnsJS(body, func(e string) bool {
		e = strings.TrimSpace(e)
		return e == "{}" || e == "Object.create(null)"
	}) {
		out = append(out, find("JSSTUB003", "empty-struct-return", "placeholder", report.SeverityWarning,
			"function body looks like a placeholder (empty-struct-return)", fn.Name, fn.File, fn.Line, fn.Col))
		return out
	}
	if onlyReturnsJS(body, func(e string) bool {
		e = strings.TrimSpace(strings.TrimSuffix(e, ";"))
		return e != "null" && e != "undefined" && (isNum(e) || isJSString(e) || e == "true" || e == "false")
	}) {
		out = append(out, find("JSSTUB004", "hardcoded-literal-return", "placeholder", report.SeverityWarning,
			"function body looks like a placeholder (hardcoded-literal-return)", fn.Name, fn.File, fn.Line, fn.Col))
		return out
	}
	if onlyReturnsJS(body, func(e string) bool {
		e = strings.TrimSpace(strings.TrimSuffix(e, ";"))
		return e == "null" || e == "undefined" || e == "0" || e == "false" || e == "[]" || e == `""` || e == "''"
	}) {
		out = append(out, find("JSSTUB002", "zero-value-return", "placeholder", report.SeverityWarning,
			"function body looks like a placeholder (zero-value-return)", fn.Name, fn.File, fn.Line, fn.Col))
	}
	return out
}

func checkCoh(fn *Func) []report.Finding {
	var out []report.Finding
	for _, p := range fn.Params {
		if p.Name == "" || p.Name == "_" || strings.HasPrefix(p.Name, "_") {
			continue
		}
		if !wordIn(fn.Body, p.Name) {
			out = append(out, find("JSCOH001", "param-unused", "coherence", report.SeverityWarning,
				"parameter "+p.Name+" never referenced", fn.Name+"."+p.Name, fn.File, fn.Line, fn.Col))
			continue
		}
		if regexp.MustCompile(`\b(?:const|let|var)\s+_?\s*=\s*`+regexp.QuoteMeta(p.Name)+`\b|\b`+regexp.QuoteMeta(p.Name)+`\b.*\/\/\s*unused`).MatchString(fn.Body) {
			out = append(out, find("JSCOH002", "param-blank-assigned", "coherence", report.SeverityWarning,
				"parameter "+p.Name+" only assigned to blank/_", fn.Name+"."+p.Name, fn.File, fn.Line, fn.Col))
		}
		affects := regexp.MustCompile(`\breturn\b.*\b`+regexp.QuoteMeta(p.Name)+`\b`).MatchString(fn.Body) ||
			regexp.MustCompile(`\([^)]*\b`+regexp.QuoteMeta(p.Name)+`\b[^)]*\)`).MatchString(fn.Body) ||
			regexp.MustCompile(`\bthrow\b.*\b`+regexp.QuoteMeta(p.Name)+`\b`).MatchString(fn.Body) ||
			regexp.MustCompile(`\.\w+\s*=\s*`+regexp.QuoteMeta(p.Name)+`\b`).MatchString(fn.Body)
		if !affects {
			out = append(out, find("JSCOH003", "param-no-effect", "coherence", report.SeverityWarning,
				"parameter "+p.Name+" value never affects results or side effects", fn.Name+"."+p.Name, fn.File, fn.Line, fn.Col))
		}
	}
	rets := returnExprsJS(fn.Body)
	if len(rets) > 0 {
		allLit := true
		first := strings.TrimSpace(strings.TrimSuffix(rets[0], ";"))
		for _, r := range rets {
			r = strings.TrimSpace(strings.TrimSuffix(r, ";"))
			if !(isNum(r) || isJSString(r) || r == "true" || r == "false" || r == "null" || r == "undefined") {
				allLit = false
				break
			}
			if r != first {
				allLit = false
				break
			}
		}
		if allLit && !regexp.MustCompile(`\b(console|fetch|write|emit|send)\b`).MatchString(fn.Body) {
			out = append(out, find("JSCOH004", "constant-result", "coherence", report.SeverityWarning,
				"result is constant on every return path", fn.Name, fn.File, fn.Line, fn.Col))
		}
	}
	return out
}

func checkConsts(f *File, corpus string) []report.Finding {
	var out []report.Finding
	re := regexp.MustCompile(`(?m)(?:export\s+)?const\s+([A-Z][A-Z0-9_]*)\s*=`)
	for _, m := range re.FindAllStringSubmatchIndex(f.Source, -1) {
		name := f.Source[m[2]:m[3]]
		line, col := lineCol(f.Source, m[0])
		if strings.Count(corpus, name) <= 1 {
			out = append(out, find("JSDEAD004", "unused-const", "dead-code", report.SeverityWarning,
				"const "+name+" not referenced by reachable code", name, f.Path, line, col))
		}
	}
	reVar := regexp.MustCompile(`(?m)(?:export\s+)?(?:let|var)\s+([a-zA-Z_][\w]*)\s*(?::[^=]+)?=`)
	for _, m := range reVar.FindAllStringSubmatchIndex(f.Source, -1) {
		name := f.Source[m[2]:m[3]]
		if strings.HasPrefix(name, "_") || name[0] >= 'A' && name[0] <= 'Z' {
			continue
		}
		line, col := lineCol(f.Source, m[0])
		// Only flag if appears once in whole corpus and looks module-level (col ~1).
		if col <= 2 && strings.Count(corpus, name) <= 1 {
			out = append(out, find("JSDEAD005", "unused-var", "dead-code", report.SeverityWarning,
				"var "+name+" not referenced by reachable code", name, f.Path, line, col))
		}
	}
	return out
}

var (
	reJSIgnoreOK  = regexp.MustCompile(`(?i)//\s*(?:code|go)?integrity:\s*ignore(?:\s+[A-Z]{2,}[0-9]{3})?\b`)
	reJSIgnoreBad = regexp.MustCompile(`(?i)//\s*(?:code|go)?integrity`)
)

func checkSuppressions(files []*File) []report.Finding {
	var out []report.Finding
	for _, f := range files {
		for i, line := range strings.Split(f.Source, "\n") {
			if !strings.Contains(strings.ToLower(line), "integrity") {
				continue
			}
			if reJSIgnoreOK.MatchString(line) {
				continue
			}
			if reJSIgnoreBad.MatchString(line) {
				out = append(out, find("JSSUP001", "invalid-suppression", "suppression", report.SeverityNote,
					"malformed codeintegrity:ignore directive", f.Path, f.Path, i+1, 1))
			}
		}
	}
	return out
}

func crossRef(corpus string, fn *Func) bool {
	callCount := strings.Count(corpus, fn.Name+"(")
	nameCount := strings.Count(corpus, fn.Name)
	return callCount > 1 || nameCount > 3
}

func checkTSCalls(f *File, funcs map[string]*Func) []report.Finding {
	var out []report.Finding
	reCall := regexp.MustCompile(`\b([A-Za-z_$][\w$]*)\s*\(\s*\)`)
	seen := map[string]bool{}
	for _, fn := range f.Funcs {
		for _, m := range reCall.FindAllStringSubmatch(fn.Body, -1) {
			name := m[1]
			cal := funcs[name]
			if cal == nil {
				continue
			}
			req := 0
			for _, p := range cal.Params {
				if !p.Optional && p.Default == "" {
					req++
				}
			}
			if req == 0 {
				continue
			}
			key := fn.File + ":" + name
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, find("JSTYPE003", "call-arity", "type", report.SeverityWarning,
				fmt.Sprintf("call to %s() missing %d required argument(s)", name, req),
				name, fn.File, fn.Line, fn.Col))
		}
	}
	return out
}

func todoOnlyJS(body string) bool {
	trim := strings.TrimSpace(body)
	if !regexp.MustCompile(`(?i)\b(TODO|FIXME|XXX)\b`).MatchString(trim) {
		return false
	}
	meaningful := 0
	for _, line := range strings.Split(trim, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*") {
			continue
		}
		meaningful++
	}
	return meaningful == 0
}

func onlyReturnsJS(body string, pred func(string) bool) bool {
	rets := returnExprsJS(body)
	if len(rets) == 0 {
		return false
	}
	nonEmpty := 0
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "return") {
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

func returnExprsJS(body string) []string {
	var out []string
	re := regexp.MustCompile(`(?m)\breturn\s+([^;/\n]+)`)
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

func isJSString(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return false
	}
	return (s[0] == '"' && s[len(s)-1] == '"') ||
		(s[0] == '\'' && s[len(s)-1] == '\'') ||
		(s[0] == '`' && s[len(s)-1] == '`')
}

func inferReturns(body string) []Type {
	var out []Type
	for _, e := range returnExprsJS(body) {
		out = append(out, inferExpr(e))
	}
	if regexp.MustCompile(`(?m)\breturn\s*;`).MatchString(body) {
		out = append(out, Void())
	}
	return out
}

func inferExpr(expr string) Type {
	expr = trim(expr)
	switch {
	case expr == "" || expr == "undefined":
		return Type{Name: "undefined"}
	case expr == "null":
		return Type{Name: "null"}
	case expr == "true" || expr == "false":
		return Type{Name: "boolean"}
	case isJSString(expr):
		return Type{Name: "string"}
	case isNum(expr):
		return Type{Name: "number"}
	case strings.HasPrefix(expr, "["):
		return Type{Name: "Array", Unknown: true}
	case strings.HasPrefix(expr, "{"):
		return Type{Name: "object", Unknown: true}
	default:
		return Any()
	}
}

func isNum(s string) bool {
	if s == "" {
		return false
	}
	dot := 0
	for i, c := range s {
		if c >= '0' && c <= '9' {
			continue
		}
		if c == '.' {
			dot++
			if dot > 1 {
				return false
			}
			continue
		}
		if c == '-' && i == 0 {
			continue
		}
		return false
	}
	return true
}

func find(id, name, cat string, sev report.Severity, msg, symbol, file string, line, col int) report.Finding {
	return report.Finding{
		Language: "js", RuleID: id, Rule: name, Category: cat, Severity: sev,
		Confidence: report.ConfidenceMedium, Message: msg, Symbol: symbol,
		Location: report.Location{File: file, Line: line, Column: col},
		Fingerprint: report.Fingerprint(id, symbol, msg, file),
	}
}

// Rules mirrors Go DEAD/STUB/COH/SUP/FLOW plus TS type rules.
func Rules() []report.RuleInfo {
	return []report.RuleInfo{
		{ID: "JSDEAD001", Name: "unreachable-func", Category: "dead-code", Severity: report.SeverityWarning, Description: "function not reachable from any root", Enabled: true},
		{ID: "JSDEAD002", Name: "unreachable-method", Category: "dead-code", Severity: report.SeverityWarning, Description: "method not reachable from any root", Enabled: true},
		{ID: "JSDEAD003", Name: "unused-type", Category: "dead-code", Severity: report.SeverityWarning, Description: "class/type not referenced", Enabled: true},
		{ID: "JSDEAD004", Name: "unused-const", Category: "dead-code", Severity: report.SeverityWarning, Description: "const not referenced", Enabled: true},
		{ID: "JSDEAD005", Name: "unused-var", Category: "dead-code", Severity: report.SeverityWarning, Description: "var not referenced", Enabled: true},
		{ID: "JSSTUB001", Name: "empty-body", Category: "placeholder", Severity: report.SeverityWarning, Description: "empty function body", Enabled: true},
		{ID: "JSSTUB002", Name: "zero-value-return", Category: "placeholder", Severity: report.SeverityWarning, Description: "body only returns zero/null/undefined", Enabled: true},
		{ID: "JSSTUB003", Name: "empty-struct-return", Category: "placeholder", Severity: report.SeverityWarning, Description: "body only returns empty object", Enabled: true},
		{ID: "JSSTUB004", Name: "hardcoded-literal-return", Category: "placeholder", Severity: report.SeverityWarning, Description: "body only returns hardcoded literals", Enabled: true},
		{ID: "JSSTUB005", Name: "panic-not-implemented", Category: "placeholder", Severity: report.SeverityError, Description: "throws not-implemented", Enabled: true},
		{ID: "JSSTUB006", Name: "todo-only-body", Category: "placeholder", Severity: report.SeverityError, Description: "TODO/FIXME-only body", Enabled: true},
		{ID: "JSSTUB007", Name: "not-implemented-error", Category: "placeholder", Severity: report.SeverityError, Description: "returns not-implemented error", Enabled: true},
		{ID: "JSCOH001", Name: "param-unused", Category: "coherence", Severity: report.SeverityWarning, Description: "parameter never referenced", Enabled: true},
		{ID: "JSCOH002", Name: "param-blank-assigned", Category: "coherence", Severity: report.SeverityWarning, Description: "parameter only assigned to blank", Enabled: true},
		{ID: "JSCOH003", Name: "param-no-effect", Category: "coherence", Severity: report.SeverityWarning, Description: "parameter never affects results or side effects", Enabled: true},
		{ID: "JSCOH004", Name: "constant-result", Category: "coherence", Severity: report.SeverityWarning, Description: "result is constant on every return path", Enabled: true},
		{ID: "JSSUP001", Name: "invalid-suppression", Category: "suppression", Severity: report.SeverityNote, Description: "malformed codeintegrity:ignore directive", Enabled: true},
		{ID: "JSFLOW001", Name: "undeclared-sensitive-flow", Category: "flow", Severity: report.SeverityWarning, Description: "input reaches sensitive sink without sanitizer", Enabled: true},
		{ID: "JSFLOW002", Name: "dropped-input", Category: "flow", Severity: report.SeverityWarning, Description: "input never reaches return, sink, or store", Enabled: true},
		{ID: "JSTYPE001", Name: "return-type-mismatch", Category: "type", Severity: report.SeverityWarning, Description: "return incompatible with annotation", Enabled: true},
		{ID: "JSTYPE002", Name: "param-default-mismatch", Category: "type", Severity: report.SeverityWarning, Description: "default incompatible with param type", Enabled: true},
		{ID: "JSTYPE003", Name: "call-arity", Category: "type", Severity: report.SeverityWarning, Description: "call missing required arguments", Enabled: true},
	}
}
