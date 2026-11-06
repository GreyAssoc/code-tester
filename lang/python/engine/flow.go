package engine

import (
	"regexp"
	"strings"

	"github.com/GreyAssoc/code-tester/report"
)

// Heuristic sources / sinks for Python FLOW rules (mirrors Go FLOW001/002 depth).
var (
	pySources = []*regexp.Regexp{
		regexp.MustCompile(`\binput\s*\(`),
		regexp.MustCompile(`\bos\.environ\b`),
		regexp.MustCompile(`\bos\.getenv\s*\(`),
		regexp.MustCompile(`\brequest\.(args|form|json|data|cookies|headers|GET|POST)\b`),
		regexp.MustCompile(`\bflask\.request\b`),
		regexp.MustCompile(`\bsys\.argv\b`),
	}
	pySinks = []*regexp.Regexp{
		regexp.MustCompile(`\beval\s*\(`),
		regexp.MustCompile(`\bexec\s*\(`),
		regexp.MustCompile(`\bsubprocess\.(run|call|Popen|check_output)\s*\(`),
		regexp.MustCompile(`\bos\.system\s*\(`),
		regexp.MustCompile(`\bopen\s*\([^)]*['\"]w`),
		regexp.MustCompile(`\bcursor\.execute\s*\(`),
		regexp.MustCompile(`\bpickle\.loads\s*\(`),
		regexp.MustCompile(`\byaml\.load\s*\(`),
		regexp.MustCompile(`\brender_template_string\s*\(`),
	}
	pySanitize = regexp.MustCompile(`\b(escape|quote|shlex\.quote|html\.escape|bleach\.clean|int\(|float\()`)
)

func checkFlow(files []*File) []report.Finding {
	var out []report.Finding
	for _, f := range files {
		for _, d := range f.Defs {
			if d.Kind == DefClass {
				continue
			}
			body := d.Body
			hasSource := false
			for _, re := range pySources {
				if re.MatchString(body) {
					hasSource = true
					break
				}
			}
			// Params treated as user_input candidates when named like request payloads.
			inputParams := []string{}
			for _, p := range d.Args {
				if p.Name == "self" || p.Name == "cls" {
					continue
				}
				low := strings.ToLower(p.Name)
				if strings.Contains(low, "user") || strings.Contains(low, "input") ||
					strings.Contains(low, "req") || strings.Contains(low, "data") ||
					strings.Contains(low, "query") || strings.Contains(low, "password") ||
					strings.Contains(low, "token") || strings.Contains(low, "secret") {
					inputParams = append(inputParams, p.Name)
					hasSource = true
				}
			}
			hasSink := false
			for _, re := range pySinks {
				if re.MatchString(body) {
					hasSink = true
					break
				}
			}
			sanitized := pySanitize.MatchString(body)
			if hasSource && hasSink && !sanitized {
				out = append(out, find("PYFLOW001", "undeclared-sensitive-flow", "flow", report.SeverityWarning,
					"user/input-labeled value may reach a sensitive sink without an obvious sanitizer",
					d.Qual, d.File, d.Line, d.Col))
			}
			// FLOW002: input param never used in return, call, or assignment effect.
			for _, name := range inputParams {
				if !wordIn(body, name) {
					continue // already COH
				}
				usedEffectively := regexp.MustCompile(`\breturn\b[\s\S]*\b`+regexp.QuoteMeta(name)+`\b`).MatchString(body) ||
					regexp.MustCompile(`\b`+regexp.QuoteMeta(name)+`\b\s*[,)]`).MatchString(body) ||
					regexp.MustCompile(`\b`+regexp.QuoteMeta(name)+`\b\s*=`).MatchString(body) == false &&
						regexp.MustCompile(`[=(]\s*`+regexp.QuoteMeta(name)+`\b`).MatchString(body)
				// Simpler: if param appears only in a discarded local assign target side — flag when never in call args or return.
				inReturn := regexp.MustCompile(`(?m)^\s*return\b.*\b` + regexp.QuoteMeta(name) + `\b`).MatchString(body)
				inCallArg := regexp.MustCompile(`\([^)]*\b` + regexp.QuoteMeta(name) + `\b[^)]*\)`).MatchString(body)
				inStore := regexp.MustCompile(`\[[^\]]*\b` + regexp.QuoteMeta(name) + `\b`).MatchString(body) ||
					regexp.MustCompile(`\.\w+\s*=\s*`+regexp.QuoteMeta(name)+`\b`).MatchString(body)
				if !inReturn && !inCallArg && !inStore {
					_ = usedEffectively
					out = append(out, find("PYFLOW002", "dropped-input", "flow", report.SeverityWarning,
						"input parameter "+name+" never reaches a return, call argument, or store",
						d.Qual+"."+name, d.File, d.Line, d.Col))
				}
			}
		}
	}
	return out
}
