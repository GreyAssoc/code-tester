package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/GreyAssoc/code-tester/report"
)

type sel struct {
	name, kind, file, head string
	line, col              int
}

var (
	reClassSel  = regexp.MustCompile(`\.([A-Za-z_][\w-]*)`)
	reIDSel     = regexp.MustCompile(`#([A-Za-z_][\w-]*)`)
	reVarDef    = regexp.MustCompile(`--([A-Za-z_][\w-]*)\s*:`)
	reVarUse    = regexp.MustCompile(`var\(\s*--([A-Za-z_][\w-]*)`)
	reRule      = regexp.MustCompile(`([^{}/]+)\{([^{}]*)\}`)
	reKeyframes = regexp.MustCompile(`(?i)@keyframes\s+([A-Za-z_][\w-]*)`)
	reAnimName  = regexp.MustCompile(`(?i)animation(?:-name)?\s*:\s*([^;]+);`)
	reFontFace  = regexp.MustCompile(`(?i)@font-face\s*\{([^}]*)\}`)
	reFontFam   = regexp.MustCompile(`(?i)font-family\s*:\s*['\"]?([^;'\"\}]+)`)
	reImport    = regexp.MustCompile(`(?i)@import\s+(?:url\()?['\"]([^'\"]+)['\"]`)
	reIgnoreOK  = regexp.MustCompile(`(?i)/\*\s*(?:code|go)?integrity:\s*ignore(?:\s+[A-Z]{2,}[0-9]{3})?\s*\*/`)
	reIgnoreBad = regexp.MustCompile(`(?i)/\*\s*(?:code|go)?integrity`)
	reTODO      = regexp.MustCompile(`(?i)/\*[^*]*\b(TODO|FIXME|XXX)\b[^*]*\*/`)
)

// Analyze runs deep CSS integrity checks over CSS paths plus an HTML/JS reference corpus.
func Analyze(root string, cssFiles map[string]string, refCorpus string) []report.Finding {
	var findings []report.Finding
	cssCorpus := strings.Builder{}
	var sels []sel
	varDefs := map[string]sel{}
	keyframes := map[string]sel{}
	fontFamilies := map[string]sel{}
	imports := []sel{}

	for rel, src := range cssFiles {
		cssCorpus.WriteString(src)
		cssCorpus.WriteByte('\n')
		findings = append(findings, checkSuppressions(rel, src)...)

		for _, m := range reRule.FindAllStringSubmatchIndex(src, -1) {
			head := strings.TrimSpace(src[m[2]:m[3]])
			body := strings.TrimSpace(src[m[4]:m[5]])
			line, col := lineCol(src, m[0])
			if strings.HasPrefix(head, "@") && !strings.HasPrefix(strings.ToLower(head), "@media") &&
				!strings.HasPrefix(strings.ToLower(head), "@supports") {
				continue
			}
			if body == "" {
				sels = append(sels, sel{name: head, kind: "empty", file: rel, line: line, col: col, head: head})
			}
			// Conflicting duplicate property declarations within one rule (last wins — still flag repeats).
			findings = append(findings, checkDupProps(rel, head, body, line, col)...)
			for _, cm := range reClassSel.FindAllStringSubmatch(head, -1) {
				sels = append(sels, sel{name: cm[1], kind: "class", file: rel, line: line, col: col})
			}
			for _, im := range reIDSel.FindAllStringSubmatch(head, -1) {
				sels = append(sels, sel{name: im[1], kind: "id", file: rel, line: line, col: col})
			}
		}
		for _, m := range reVarDef.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			line, col := lineCol(src, m[0])
			varDefs[name] = sel{name: name, kind: "var", file: rel, line: line, col: col}
		}
		for _, m := range reKeyframes.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			line, col := lineCol(src, m[0])
			keyframes[name] = sel{name: name, kind: "keyframes", file: rel, line: line, col: col}
		}
		for _, m := range reFontFace.FindAllStringSubmatchIndex(src, -1) {
			block := src[m[2]:m[3]]
			if fm := reFontFam.FindStringSubmatch(block); fm != nil {
				name := strings.TrimSpace(fm[1])
				line, col := lineCol(src, m[0])
				fontFamilies[name] = sel{name: name, kind: "font", file: rel, line: line, col: col}
			}
		}
		for _, m := range reImport.FindAllStringSubmatchIndex(src, -1) {
			path := src[m[2]:m[3]]
			line, col := lineCol(src, m[0])
			imports = append(imports, sel{name: path, kind: "import", file: rel, line: line, col: col})
		}
		// TODO-only comment stubs
		for _, m := range reTODO.FindAllStringIndex(src, -1) {
			block := src[m[0]:m[1]]
			line, col := lineCol(src, m[0])
			// Only flag when immediately followed by empty-ish rule or sits alone near empty block.
			findings = append(findings, find("CSSSTUB006", "todo-only-body", "placeholder", report.SeverityNote,
				"TODO/FIXME comment in stylesheet: "+trimMsg(block, 60), block, rel, line, col))
			_ = block
		}
	}

	cssText := cssCorpus.String()
	ref := refCorpus // HTML/JS/TS only — do not treat stylesheet text as a selector reference
	_ = root

	// Undefined var() uses (COH)
	usedVars := map[string]bool{}
	for _, m := range reVarUse.FindAllStringSubmatchIndex(cssText, -1) {
		name := cssText[m[2]:m[3]]
		usedVars[name] = true
		if _, ok := varDefs[name]; !ok {
			line, col := lineCol(cssText, m[0])
			findings = append(findings, find("CSSCOH002", "undefined-custom-property", "coherence", report.SeverityWarning,
				"var(--"+name+") references an undefined custom property", "--"+name, guessFile(cssFiles, m[0], cssText), line, col))
		}
	}

	seenSel := map[string]bool{}
	for _, s := range sels {
		if s.kind == "empty" {
			findings = append(findings, find("CSSEMPTY001", "empty-rule", "placeholder", report.SeverityWarning,
				"empty CSS rule for selector "+s.name, s.name, s.file, s.line, s.col))
			continue
		}
		key := s.kind + ":" + s.name
		if seenSel[key] {
			// Duplicate selector across files/rules — coherence signal.
			findings = append(findings, find("CSSCOH001", "duplicate-selector", "coherence", report.SeverityNote,
				"selector ."+s.name+" declared more than once", s.name, s.file, s.line, s.col))
			continue
		}
		seenSel[key] = true
		// Reference corpus is HTML/JS only (caller passes refCorpus without needing CSS self-hits).
		referenced := strings.Contains(refCorpus, s.name)
		if s.kind == "class" {
			referenced = referenced ||
				strings.Contains(refCorpus, `class="`+s.name) ||
				strings.Contains(refCorpus, `class='`+s.name) ||
				strings.Contains(refCorpus, `className="`+s.name) ||
				strings.Contains(refCorpus, `className='`+s.name) ||
				strings.Contains(refCorpus, `'`+s.name+`'`) ||
				strings.Contains(refCorpus, `"`+s.name+`"`)
		}
		if s.kind == "id" {
			referenced = referenced ||
				strings.Contains(refCorpus, `id="`+s.name+`"`) ||
				strings.Contains(refCorpus, `id='`+s.name+`'`) ||
				strings.Contains(refCorpus, `getElementById("`+s.name) ||
				strings.Contains(refCorpus, `getElementById('`+s.name)
		}
		if referenced {
			continue
		}
		switch s.kind {
		case "class":
			findings = append(findings, find("CSSDEAD001", "unused-selector", "dead-code", report.SeverityWarning,
				"class selector ."+s.name+" is never referenced in HTML/JS/TS", "."+s.name, s.file, s.line, s.col))
		case "id":
			findings = append(findings, find("CSSDEAD001", "unused-selector", "dead-code", report.SeverityWarning,
				"id selector #"+s.name+" is never referenced in HTML/JS/TS", "#"+s.name, s.file, s.line, s.col))
		}
	}

	for name, s := range varDefs {
		if !usedVars[name] {
			findings = append(findings, find("CSSDEAD002", "unused-custom-property", "dead-code", report.SeverityNote,
				"custom property --"+name+" is never used", "--"+name, s.file, s.line, s.col))
		}
	}

	// Keyframes usage
	animRefs := map[string]bool{}
	for _, m := range reAnimName.FindAllStringSubmatch(cssText, -1) {
		for _, part := range strings.Split(m[1], ",") {
			part = strings.TrimSpace(part)
			// animation: name duration ... — first token often the name
			tok := strings.Fields(part)
			if len(tok) > 0 {
				animRefs[tok[0]] = true
			}
		}
	}
	for name, s := range keyframes {
		if !animRefs[name] && strings.Count(ref, name) <= 1 {
			findings = append(findings, find("CSSDEAD003", "unused-keyframes", "dead-code", report.SeverityWarning,
				"@keyframes "+name+" is never referenced", name, s.file, s.line, s.col))
		}
	}

	for name, s := range fontFamilies {
		if strings.Count(ref, name) <= 1 {
			findings = append(findings, find("CSSDEAD004", "unused-font-face", "dead-code", report.SeverityNote,
				"@font-face family "+name+" is never referenced", name, s.file, s.line, s.col))
		}
	}

	// Broken @import paths
	for _, im := range imports {
		target := im.name
		if strings.HasPrefix(target, "http") {
			continue
		}
		full := filepath.Join(root, filepath.Dir(im.file), target)
		if _, err := os.Stat(full); err != nil {
			findings = append(findings, find("CSSCOH003", "broken-import", "coherence", report.SeverityWarning,
				"@import target not found: "+target, target, im.file, im.line, im.col))
		}
	}

	return findings
}

func checkDupProps(file, head, body string, line, col int) []report.Finding {
	seen := map[string]bool{}
	var out []report.Finding
	for _, part := range strings.Split(body, ";") {
		part = strings.TrimSpace(part)
		if part == "" || !strings.Contains(part, ":") {
			continue
		}
		prop := strings.TrimSpace(strings.SplitN(part, ":", 2)[0])
		low := strings.ToLower(prop)
		if seen[low] {
			out = append(out, find("CSSCOH004", "duplicate-property", "coherence", report.SeverityNote,
				"property "+prop+" declared twice in rule "+strings.TrimSpace(head), prop, file, line, col))
		}
		seen[low] = true
	}
	return out
}

func checkSuppressions(file, src string) []report.Finding {
	var out []report.Finding
	for i, line := range strings.Split(src, "\n") {
		if !strings.Contains(strings.ToLower(line), "integrity") {
			continue
		}
		if reIgnoreOK.MatchString(line) {
			continue
		}
		if reIgnoreBad.MatchString(line) {
			out = append(out, find("CSSSUP001", "invalid-suppression", "suppression", report.SeverityNote,
				"malformed codeintegrity:ignore directive", file, file, i+1, 1))
		}
	}
	return out
}

func lineCol(src string, offset int) (int, int) {
	line, col := 1, 1
	if offset < 0 {
		return 1, 1
	}
	for i := 0; i < offset && i < len(src); i++ {
		if src[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

func guessFile(files map[string]string, offset int, joined string) string {
	_ = offset
	_ = joined
	for p := range files {
		return p
	}
	return ""
}

func trimMsg(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func find(id, name, cat string, sev report.Severity, msg, symbol, file string, line, col int) report.Finding {
	return report.Finding{
		Language: "css", RuleID: id, Rule: name, Category: cat, Severity: sev,
		Confidence: report.ConfidenceMedium, Message: msg, Symbol: symbol,
		Location: report.Location{File: file, Line: line, Column: col},
		Fingerprint: report.Fingerprint(id, symbol, msg, file),
	}
}

// Rules mirrors Go categories adapted to CSS.
func Rules() []report.RuleInfo {
	return []report.RuleInfo{
		{ID: "CSSDEAD001", Name: "unused-selector", Category: "dead-code", Severity: report.SeverityWarning, Description: "class/id selector never referenced in HTML/JS/TS", Enabled: true},
		{ID: "CSSDEAD002", Name: "unused-custom-property", Category: "dead-code", Severity: report.SeverityNote, Description: "CSS variable defined but never used", Enabled: true},
		{ID: "CSSDEAD003", Name: "unused-keyframes", Category: "dead-code", Severity: report.SeverityWarning, Description: "@keyframes never referenced", Enabled: true},
		{ID: "CSSDEAD004", Name: "unused-font-face", Category: "dead-code", Severity: report.SeverityNote, Description: "@font-face family never referenced", Enabled: true},
		{ID: "CSSEMPTY001", Name: "empty-rule", Category: "placeholder", Severity: report.SeverityWarning, Description: "rule block is empty", Enabled: true},
		{ID: "CSSSTUB006", Name: "todo-only-body", Category: "placeholder", Severity: report.SeverityNote, Description: "TODO/FIXME comment in stylesheet", Enabled: true},
		{ID: "CSSCOH001", Name: "duplicate-selector", Category: "coherence", Severity: report.SeverityNote, Description: "selector declared more than once", Enabled: true},
		{ID: "CSSCOH002", Name: "undefined-custom-property", Category: "coherence", Severity: report.SeverityWarning, Description: "var() references undefined custom property", Enabled: true},
		{ID: "CSSCOH003", Name: "broken-import", Category: "coherence", Severity: report.SeverityWarning, Description: "@import target not found", Enabled: true},
		{ID: "CSSCOH004", Name: "duplicate-property", Category: "coherence", Severity: report.SeverityNote, Description: "property declared twice in one rule", Enabled: true},
		{ID: "CSSSUP001", Name: "invalid-suppression", Category: "suppression", Severity: report.SeverityNote, Description: "malformed codeintegrity:ignore directive", Enabled: true},
	}
}
