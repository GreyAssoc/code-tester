package engine

import (
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reImport = regexp.MustCompile(`(?m)^import\s+(?:type\s+)?(?:(\*\s+as\s+(\w+))|(\{[^}]+\})|(\w+))\s+from\s+['"]([^'"]+)['"]`)
	reRequire = regexp.MustCompile(`(?m)(?:const|let|var)\s+(?:\{([^}]+)\}|(\w+))\s*=\s*require\(['"]([^'"]+)['"]\)`)
	reFn = regexp.MustCompile(`(?m)(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*\(([^)]*)\)(?:\s*:\s*([^{]+))?`)
	reArrow = regexp.MustCompile(`(?m)(?:export\s+)?(?:const|let|var)\s+(\w+)\s*(?::\s*[^=]+)?=\s*(?:async\s*)?\(([^)]*)\)\s*(?::\s*([^=]+))?\s*=>`)
	reClass = regexp.MustCompile(`(?m)(?:export\s+)?class\s+(\w+)`)
	reTypeAlias = regexp.MustCompile(`(?m)(?:export\s+)?type\s+(\w+)\s*=\s*([^;]+);`)
	reExportName = regexp.MustCompile(`(?m)export\s+(?:const|let|var|class|function|async function|type|interface)\s+(\w+)`)
)

func ParseFile(path, src string) *File {
	ext := strings.ToLower(filepath.Ext(path))
	f := &File{
		Path: path,
		IsTS: ext == ".ts" || ext == ".tsx" || ext == ".mts" || ext == ".cts",
		Source: src,
	}
	for _, m := range reImport.FindAllStringSubmatch(src, -1) {
		from := m[5]
		var names []string
		switch {
		case m[2] != "":
			names = []string{m[2]}
		case m[3] != "":
			for _, p := range strings.Split(strings.Trim(m[3], "{}"), ",") {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				if i := strings.Index(p, " as "); i >= 0 {
					p = strings.TrimSpace(p[i+4:])
				}
				names = append(names, p)
			}
		case m[4] != "":
			names = []string{m[4]}
		}
		line, col := lineCol(src, strings.Index(src, m[0]))
		f.Imports = append(f.Imports, Import{From: from, Names: names, Line: line, Col: col})
	}
	for _, m := range reRequire.FindAllStringSubmatch(src, -1) {
		from := m[3]
		var names []string
		if m[1] != "" {
			for _, p := range strings.Split(m[1], ",") {
				names = append(names, strings.TrimSpace(p))
			}
		} else {
			names = []string{m[2]}
		}
		f.Imports = append(f.Imports, Import{From: from, Names: names})
	}

	addFn := func(name, params, ret string, exported bool, idx int) {
		body, _ := extractBody(src, idx)
		line, col := lineCol(src, idx)
		f.Funcs = append(f.Funcs, &Func{
			Name: name, File: path, Line: line, Col: col,
			Params: parseParams(params), RetType: trim(ret), Body: body, Exported: exported,
		})
	}

	for _, m := range reFn.FindAllStringSubmatchIndex(src, -1) {
		full := src[m[0]:m[1]]
		name := src[m[2]:m[3]]
		params := src[m[4]:m[5]]
		ret := ""
		if m[6] >= 0 {
			ret = src[m[6]:m[7]]
		}
		addFn(name, params, ret, strings.HasPrefix(strings.TrimSpace(full), "export"), m[0])
	}
	for _, m := range reArrow.FindAllStringSubmatchIndex(src, -1) {
		full := src[m[0]:m[1]]
		name := src[m[2]:m[3]]
		params := src[m[4]:m[5]]
		ret := ""
		if m[6] >= 0 {
			ret = src[m[6]:m[7]]
		}
		// body may be block or expression
		body := ""
		rest := src[m[1]:]
		rest = strings.TrimLeft(rest, " \t")
		if strings.HasPrefix(rest, "{") {
			body, _ = extractBody(src, m[1])
		} else {
			if i := strings.IndexAny(rest, ";\n"); i >= 0 {
				body = rest[:i]
			} else {
				body = rest
			}
		}
		line, col := lineCol(src, m[0])
		f.Funcs = append(f.Funcs, &Func{
			Name: name, File: path, Line: line, Col: col,
			Params: parseParams(params), RetType: trim(ret), Body: body,
			Exported: strings.Contains(full, "export"),
		})
	}
	for _, m := range reClass.FindAllStringSubmatchIndex(src, -1) {
		full := src[m[0]:m[1]]
		name := src[m[2]:m[3]]
		line, col := lineCol(src, m[0])
		f.Classes = append(f.Classes, Class{
			Name: name, File: path, Line: line, Col: col,
			Exported: strings.HasPrefix(strings.TrimSpace(full), "export"),
		})
	}
	for _, m := range reTypeAlias.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		typ := src[m[4]:m[5]]
		line, col := lineCol(src, m[0])
		f.TypeAliases = append(f.TypeAliases, TypeAlias{Name: name, Type: trim(typ), File: path, Line: line, Col: col})
	}
	for _, m := range reExportName.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		line, col := lineCol(src, m[0])
		f.Exports = append(f.Exports, Export{Name: name, Line: line, Col: col})
	}
	return f
}

func parseParams(s string) []Param {
	var out []Param
	for _, part := range splitTop(s, ',') {
		part = trim(part)
		if part == "" {
			continue
		}
		def := ""
		if i := strings.Index(part, "="); i >= 0 {
			def = trim(part[i+1:])
			part = trim(part[:i])
		}
		opt := false
		typ := ""
		name := part
		if i := strings.Index(part, ":"); i >= 0 {
			name = trim(part[:i])
			typ = trim(part[i+1:])
		}
		if strings.HasSuffix(name, "?") {
			opt = true
			name = strings.TrimSuffix(name, "?")
		}
		name = strings.TrimPrefix(name, "...")
		// destructure skip deep
		if strings.HasPrefix(name, "{") || strings.HasPrefix(name, "[") {
			continue
		}
		out = append(out, Param{Name: name, Type: typ, Optional: opt, Default: def})
	}
	return out
}

func extractBody(src string, from int) (string, int) {
	i := from
	for i < len(src) && src[i] != '{' {
		i++
	}
	if i >= len(src) {
		return "", -1
	}
	depth := 0
	for j := i; j < len(src); j++ {
		switch src[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[i+1 : j], j
			}
		}
	}
	return "", -1
}

func lineCol(src string, offset int) (int, int) {
	if offset < 0 {
		return 1, 1
	}
	line, col := 1, 1
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

func ParseProject(files map[string]string) []*File {
	var out []*File
	for p, s := range files {
		out = append(out, ParseFile(p, s))
	}
	return out
}
