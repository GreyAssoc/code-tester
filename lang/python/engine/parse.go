package engine

import (
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reImport    = regexp.MustCompile(`(?m)^(?:from[ \t]+([\w.]+)[ \t]+)?import[ \t]+(.+)$`)
	reDef       = regexp.MustCompile(`(?m)^([ \t]*)def[ \t]+([A-Za-z_][\w]*)\s*\((.*)\)\s*(?:->[ \t]*([^:]+))?:`)
	reClass     = regexp.MustCompile(`(?m)^([ \t]*)class[ \t]+([A-Za-z_][\w]*)`)
	reAssignAnn = regexp.MustCompile(`(?m)^([ \t]*)([A-Za-z_][\w]*)\s*:\s*([^=\n]+?)(?:\s*=\s*.*)?$`)
)

// ParseFile builds an IR File from Python source.
func ParseFile(path, src string) *File {
	mod := strings.TrimSuffix(filepath.ToSlash(path), ".py")
	mod = strings.ReplaceAll(mod, "/", ".")
	f := &File{Path: path, Module: mod, Source: src}
	lines := strings.Split(src, "\n")

	for i, line := range lines {
		if m := reImport.FindStringSubmatch(line); m != nil {
			from, names := m[1], m[2]
			for _, part := range strings.Split(names, ",") {
				part = strings.TrimSpace(part)
				if part == "" || part == "(" {
					continue
				}
				part = strings.Split(part, " as ")[0]
				part = strings.TrimSpace(strings.Split(part, "#")[0])
				bound := part
				if from != "" {
					bound = part
				}
				if i := strings.LastIndex(bound, "."); i >= 0 {
					bound = bound[i+1:]
				}
				f.Imports = append(f.Imports, Import{Module: from, Name: bound, Line: i + 1, Col: 1})
			}
		}
		if m := reClass.FindStringSubmatch(line); m != nil {
			name := m[2]
			f.Defs = append(f.Defs, &Def{
				Kind: DefClass, Name: name, Qual: mod + "." + name,
				File: path, Line: i + 1, Col: len(m[1]) + 1,
				Exported: !strings.HasPrefix(name, "_"),
			})
		}
		if m := reDef.FindStringSubmatch(line); m != nil {
			indent, name, args, ret := m[1], m[2], m[3], strings.TrimSpace(m[4])
			kind := DefFunc
			if len(indent) > 0 {
				kind = DefMethod
			}
			body := collectBody(lines, i+1, len(indent))
			var decs []string
			for j := i - 1; j >= 0; j-- {
				t := strings.TrimSpace(lines[j])
				if strings.HasPrefix(t, "@") {
					decs = append([]string{t}, decs...)
					continue
				}
				if t == "" {
					continue
				}
				break
			}
			d := &Def{
				Kind: kind, Name: name, Qual: mod + "." + name,
				File: path, Line: i + 1, Col: len(indent) + 1,
				Args: parseParams(args), RetAnn: ret, Body: body,
				Decorators: decs,
				Exported:   len(indent) == 0 && !strings.HasPrefix(name, "_"),
			}
			f.Defs = append(f.Defs, d)
		}
		if m := reAssignAnn.FindStringSubmatch(line); m != nil {
			if strings.HasPrefix(strings.TrimSpace(line), "def ") || strings.HasPrefix(strings.TrimSpace(line), "class ") {
				continue
			}
			f.Assigns = append(f.Assigns, Assign{
				Name: m[2], Ann: strings.TrimSpace(m[3]), File: path, Line: i + 1, Col: len(m[1]) + 1,
			})
		}
	}
	return f
}

func parseParams(s string) []Param {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []Param
	for _, part := range splitTop(s, ',') {
		part = strings.TrimSpace(part)
		if part == "" || part == "*" || part == "/" {
			continue
		}
		def := ""
		if i := strings.Index(part, "="); i >= 0 {
			def = strings.TrimSpace(part[i+1:])
			part = strings.TrimSpace(part[:i])
		}
		ann := ""
		name := part
		if i := strings.Index(part, ":"); i >= 0 {
			name = strings.TrimSpace(part[:i])
			ann = strings.TrimSpace(part[i+1:])
		}
		name = strings.TrimPrefix(name, "**")
		name = strings.TrimPrefix(name, "*")
		if name == "" {
			continue
		}
		out = append(out, Param{Name: name, Ann: ann, Default: def})
	}
	return out
}

func collectBody(lines []string, start, indent int) string {
	var b strings.Builder
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			b.WriteByte('\n')
			continue
		}
		lead := len(line) - len(strings.TrimLeft(line, " \t"))
		if lead <= indent {
			break
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// ParseProject parses all given (relPath -> source) maps.
func ParseProject(files map[string]string) []*File {
	var out []*File
	for path, src := range files {
		out = append(out, ParseFile(path, src))
	}
	return out
}
