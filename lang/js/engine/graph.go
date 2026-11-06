package engine

import (
	"path/filepath"
	"regexp"
	"strings"
)

// CallGraph is a name-based call graph over JS/TS funcs.
type CallGraph struct {
	Funcs map[string]*Func
	Edges map[string][]string
	Roots map[string]bool
}

var reJSCall = regexp.MustCompile(`\b([A-Za-z_$][\w$]*)\s*\(`)

// BuildCallGraph links functions by simple name calls.
func BuildCallGraph(files []*File) *CallGraph {
	g := &CallGraph{
		Funcs: map[string]*Func{},
		Edges: map[string][]string{},
		Roots: map[string]bool{},
	}
	for _, f := range files {
		for _, fn := range f.Funcs {
			g.Funcs[fn.Name] = fn
			key := fn.File + "::" + fn.Name
			g.Funcs[key] = fn
			if fn.Exported || fn.Name == "main" || fn.Name == "handler" {
				g.Roots[key] = true
			}
		}
	}
	for _, f := range files {
		for _, fn := range f.Funcs {
			from := fn.File + "::" + fn.Name
			seen := map[string]bool{}
			for _, m := range reJSCall.FindAllStringSubmatch(fn.Body, -1) {
				cal := m[1]
				if cal == fn.Name || cal == "if" || cal == "for" || cal == "while" || cal == "switch" || cal == "catch" {
					continue
				}
				target := g.Funcs[cal]
				if target == nil {
					continue
				}
				to := target.File + "::" + target.Name
				if seen[to] {
					continue
				}
				seen[to] = true
				g.Edges[from] = append(g.Edges[from], to)
			}
		}
		// Top-level calls in entry files seed roots (skip function/class declarations).
		if isEntryFile(f.Path) {
			for _, m := range reJSCall.FindAllStringSubmatchIndex(f.Source, -1) {
				name := f.Source[m[2]:m[3]]
				if isDeclCallSite(f.Source, m[0]) {
					continue
				}
				if target := g.Funcs[name]; target != nil {
					g.Roots[target.File+"::"+target.Name] = true
				}
			}
		}
	}
	if len(g.Roots) == 0 {
		for _, fn := range g.Funcs {
			if fn.Exported {
				g.Roots[fn.File+"::"+fn.Name] = true
			}
		}
	}
	return g
}

func isEntryFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "index.js", "index.ts", "index.mjs", "main.js", "main.ts", "app.js", "app.ts", "server.js", "server.ts":
		return true
	}
	return false
}

func isDeclCallSite(src string, offset int) bool {
	// Look back on the same line for function/class/=> declaration keywords.
	start := offset
	for start > 0 && src[start-1] != '\n' {
		start--
	}
	prefix := strings.TrimSpace(src[start:offset])
	return strings.Contains(prefix, "function") || strings.Contains(prefix, "class") ||
		strings.HasSuffix(prefix, "function") || strings.HasPrefix(prefix, "if") ||
		strings.HasPrefix(prefix, "for") || strings.HasPrefix(prefix, "while") ||
		strings.HasPrefix(prefix, "switch") || strings.HasPrefix(prefix, "catch")
}

// Reachable returns file::name keys reachable from roots.
func (g *CallGraph) Reachable() map[string]bool {
	reach := map[string]bool{}
	var stack []string
	for r := range g.Roots {
		stack = append(stack, r)
		reach[r] = true
	}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, t := range g.Edges[n] {
			if reach[t] {
				continue
			}
			reach[t] = true
			stack = append(stack, t)
		}
	}
	return reach
}
