package engine

import (
	"regexp"
	"strings"

	"gointegrity/report"
)

var (
	jsSources = []*regexp.Regexp{
		regexp.MustCompile(`\bprocess\.env\b`),
		regexp.MustCompile(`\breq\.(body|query|params|headers|cookies)\b`),
		regexp.MustCompile(`\brequest\.(body|query)\b`),
		regexp.MustCompile(`\bwindow\.location\b`),
		regexp.MustCompile(`\blocation\.(search|hash)\b`),
		regexp.MustCompile(`\blocalStorage\.getItem\s*\(`),
		regexp.MustCompile(`\bdocument\.cookie\b`),
	}
	jsSinks = []*regexp.Regexp{
		regexp.MustCompile(`\beval\s*\(`),
		regexp.MustCompile(`\bnew\s+Function\s*\(`),
		regexp.MustCompile(`\binnerHTML\s*=`),
		regexp.MustCompile(`\bouterHTML\s*=`),
		regexp.MustCompile(`\bdocument\.write\s*\(`),
		regexp.MustCompile(`\bchild_process\.(exec|spawn|execSync)\s*\(`),
		regexp.MustCompile(`\bexec\s*\(`),
		regexp.MustCompile(`\bfs\.writeFile(?:Sync)?\s*\(`),
	}
	jsSanitize = regexp.MustCompile(`\b(DOMPurify\.sanitize|escapeHtml|encodeURIComponent|JSON\.parse|Number\(|parseInt\()`)
)

func checkFlow(files []*File) []report.Finding {
	var out []report.Finding
	for _, f := range files {
		for _, fn := range f.Funcs {
			body := fn.Body
			hasSource := false
			for _, re := range jsSources {
				if re.MatchString(body) {
					hasSource = true
					break
				}
			}
			var inputParams []string
			for _, p := range fn.Params {
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
			for _, re := range jsSinks {
				if re.MatchString(body) {
					hasSink = true
					break
				}
			}
			if hasSource && hasSink && !jsSanitize.MatchString(body) {
				out = append(out, find("JSFLOW001", "undeclared-sensitive-flow", "flow", report.SeverityWarning,
					"user/input-labeled value may reach a sensitive sink without an obvious sanitizer",
					fn.Name, fn.File, fn.Line, fn.Col))
			}
			for _, name := range inputParams {
				if !wordIn(body, name) {
					continue
				}
				inReturn := regexp.MustCompile(`\breturn\b.*\b` + regexp.QuoteMeta(name) + `\b`).MatchString(body)
				inCallArg := regexp.MustCompile(`\([^)]*\b` + regexp.QuoteMeta(name) + `\b[^)]*\)`).MatchString(body)
				inStore := regexp.MustCompile(`\.\w+\s*=\s*` + regexp.QuoteMeta(name) + `\b`).MatchString(body)
				if !inReturn && !inCallArg && !inStore {
					out = append(out, find("JSFLOW002", "dropped-input", "flow", report.SeverityWarning,
						"input parameter "+name+" never reaches a return, call argument, or store",
						fn.Name+"."+name, fn.File, fn.Line, fn.Col))
				}
			}
		}
	}
	return out
}

func wordIn(body, name string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).MatchString(body)
}
