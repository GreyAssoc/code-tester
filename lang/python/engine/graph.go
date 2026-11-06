package engine

import (
	"path/filepath"
	"regexp"
	"strings"
)

// CallGraph is a name-based call graph over project defs.
type CallGraph struct {
	Defs  map[string]*Def // qual + short
	Edges map[string][]string
	Roots map[string]bool
}

var reCallName = regexp.MustCompile(`\b([A-Za-z_][\w]*)\s*\(`)

// BuildCallGraph links defs by simple name calls inside bodies.
func BuildCallGraph(files []*File) *CallGraph {
	g := &CallGraph{
		Defs:  map[string]*Def{},
		Edges: map[string][]string{},
		Roots: map[string]bool{},
	}
	for _, f := range files {
		for _, d := range f.Defs {
			if d.Kind == DefClass {
				continue
			}
			g.Defs[d.Qual] = d
			g.Defs[d.Name] = d
		}
	}
	for _, f := range files {
		isMain := strings.HasSuffix(f.Path, "__main__.py") ||
			filepath.Base(f.Path) == "main.py" ||
			strings.Contains(f.Source, `if __name__ == "__main__"`) ||
			strings.Contains(f.Source, `if __name__ == '__main__'`)
		for _, d := range f.Defs {
			if d.Kind == DefClass {
				continue
			}
			if isRootDef(d, isMain) {
				g.Roots[d.Qual] = true
			}
			seen := map[string]bool{}
			for _, m := range reCallName.FindAllStringSubmatch(d.Body, -1) {
				cal := m[1]
				if cal == d.Name || cal == "print" || cal == "len" || cal == "str" || cal == "int" || cal == "range" {
					continue
				}
				target := g.Defs[cal]
				if target == nil {
					continue
				}
				if seen[target.Qual] {
					continue
				}
				seen[target.Qual] = true
				g.Edges[d.Qual] = append(g.Edges[d.Qual], target.Qual)
			}
		}
		// Calls inside the __main__ guard seed additional roots.
		if isMain {
			for _, block := range mainBlocks(f.Source) {
				for _, m := range reCallName.FindAllStringSubmatch(block, -1) {
					if target := g.Defs[m[1]]; target != nil {
						g.Roots[target.Qual] = true
					}
				}
			}
		}
	}
	// If no roots found, treat all exported top-level funcs as roots (library mode).
	if len(g.Roots) == 0 {
		for _, d := range g.Defs {
			if d.Exported && d.Kind == DefFunc {
				g.Roots[d.Qual] = true
			}
		}
	}
	return g
}

func isRootDef(d *Def, fileIsMain bool) bool {
	if d.Name == "main" || d.Name == "cli" || d.Name == "run" {
		return true
	}
	for _, dec := range d.Decorators {
		if strings.Contains(dec, "app.route") || strings.Contains(dec, "click.command") ||
			strings.Contains(dec, ".command") || strings.Contains(dec, "pytest") {
			return true
		}
	}
	_ = fileIsMain
	return false
}

func mainBlocks(src string) []string {
	var blocks []string
	for _, marker := range []string{`if __name__ == "__main__":`, `if __name__ == '__main__':`} {
		i := strings.Index(src, marker)
		if i < 0 {
			continue
		}
		rest := src[i+len(marker):]
		var b strings.Builder
		for _, line := range strings.Split(rest, "\n") {
			if strings.TrimSpace(line) == "" {
				b.WriteByte('\n')
				continue
			}
			lead := len(line) - len(strings.TrimLeft(line, " \t"))
			if lead == 0 && b.Len() > 0 {
				break
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
		blocks = append(blocks, b.String())
	}
	return blocks
}

// Reachable returns quals reachable from roots.
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
