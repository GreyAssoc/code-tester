package flow

import (
	"fmt"
	"io"
	"strings"
)

// RenderDOT writes a GraphViz DOT graph, optionally focused.
func RenderDOT(w io.Writer, g *Graph, focus string, depth int, from, to string) error {
	ids := g.viewNodes(focus, depth, from, to)
	fmt.Fprintln(w, "digraph gointegrity_flow {")
	fmt.Fprintln(w, `  rankdir=LR;`)
	fmt.Fprintln(w, `  node [shape=box,fontname="Helvetica"];`)
	for _, id := range ids {
		n := g.Nodes[id]
		if n == nil {
			continue
		}
		label := string(n.Kind)
		if n.Symbol != "" {
			label = string(n.Kind) + "\\n" + escape(n.Symbol)
		}
		if len(n.Labels) > 0 {
			label += "\\n[" + strings.Join(n.Labels, ",") + "]"
		}
		shape := "box"
		switch n.Kind {
		case NodeSource:
			shape = "ellipse"
		case NodeSink:
			shape = "hexagon"
		case NodeCell:
			shape = "cylinder"
		}
		fmt.Fprintf(w, "  %q [label=%q,shape=%s];\n", id, label, shape)
	}
	idSet := map[string]bool{}
	for _, id := range ids {
		idSet[id] = true
	}
	for _, e := range g.Edges {
		if !idSet[e.From] || !idSet[e.To] {
			continue
		}
		lbl := string(e.Label)
		if e.Approx {
			lbl += " (approx)"
		}
		fmt.Fprintf(w, "  %q -> %q [label=%q];\n", e.From, e.To, lbl)
	}
	fmt.Fprintln(w, "}")
	return nil
}

// RenderMermaid writes a Mermaid flowchart.
func RenderMermaid(w io.Writer, g *Graph, focus string, depth int, from, to string) error {
	ids := g.viewNodes(focus, depth, from, to)
	fmt.Fprintln(w, "flowchart LR")
	idSet := map[string]bool{}
	safe := func(id string) string {
		return "n" + strings.ReplaceAll(strings.ReplaceAll(id, ":", "_"), "/", "_")
	}
	for _, id := range ids {
		idSet[id] = true
		n := g.Nodes[id]
		if n == nil {
			continue
		}
		label := string(n.Kind)
		if n.Symbol != "" {
			label += " " + n.Symbol
		}
		fmt.Fprintf(w, "  %s[\"%s\"]\n", safe(id), strings.ReplaceAll(label, `"`, "'"))
	}
	for _, e := range g.Edges {
		if !idSet[e.From] || !idSet[e.To] {
			continue
		}
		fmt.Fprintf(w, "  %s -->|%s| %s\n", safe(e.From), e.Label, safe(e.To))
	}
	return nil
}

// RenderHTML writes a minimal HTML viewer with embedded Mermaid.
func RenderHTML(w io.Writer, g *Graph, focus string, depth int, from, to string) error {
	fmt.Fprintln(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>gointegrity flow</title>`)
	fmt.Fprintln(w, `<script type="module">import mermaid from "https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.esm.min.mjs"; mermaid.initialize({startOnLoad:true});</script>`)
	fmt.Fprintln(w, `</head><body><h1>gointegrity flow graph</h1><pre class="mermaid">`)
	_ = RenderMermaid(w, g, focus, depth, from, to)
	fmt.Fprintln(w, `</pre></body></html>`)
	return nil
}

func (g *Graph) viewNodes(focus string, depth int, from, to string) []string {
	if from != "" && to != "" {
		fids := g.resolveNode(from)
		tids := g.resolveNode(to)
		set := map[string]bool{}
		for _, f := range fids {
			for _, t := range tids {
				for _, n := range g.ShortestPath(f, t) {
					set[n] = true
				}
			}
		}
		return sortedKeys(set)
	}
	if focus == "" {
		// Collapse to function/source/sink/cell by default (skip raw values).
		set := map[string]bool{}
		for id, n := range g.Nodes {
			if n.Kind == NodeValue {
				continue
			}
			set[id] = true
		}
		return sortedKeys(set)
	}
	seeds := g.resolveNode(focus)
	if len(seeds) == 0 {
		return nil
	}
	if depth <= 0 {
		depth = 3
	}
	set := map[string]bool{}
	type item struct {
		id    string
		depth int
	}
	q := []item{}
	for _, s := range seeds {
		q = append(q, item{s, 0})
		set[s] = true
	}
	for len(q) > 0 {
		cur := q[0]
		q = q[1:]
		if cur.depth >= depth {
			continue
		}
		for _, ei := range g.out[cur.id] {
			to := g.Edges[ei].To
			if !set[to] {
				set[to] = true
				q = append(q, item{to, cur.depth + 1})
			}
		}
		for _, ei := range g.in[cur.id] {
			from := g.Edges[ei].From
			if !set[from] {
				set[from] = true
				q = append(q, item{from, cur.depth + 1})
			}
		}
	}
	return sortedKeys(set)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	// already have sort via Graph.SortedNodeIDs pattern
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func escape(s string) string {
	return strings.ReplaceAll(s, `"`, `'`)
}

// CodeFlow converts a witness path to SARIF-like locations using edge locations.
func (g *Graph) CodeFlow(witness []string) []map[string]any {
	var steps []map[string]any
	for i := 0; i+1 < len(witness); i++ {
		from, to := witness[i], witness[i+1]
		loc := ""
		label := ""
		for _, ei := range g.out[from] {
			e := g.Edges[ei]
			if e.To == to {
				loc = e.Location
				label = string(e.Label)
				break
			}
		}
		n := g.Nodes[to]
		msg := label
		if n != nil {
			msg = fmt.Sprintf("%s → %s (%s)", from, n.Symbol, label)
		}
		step := map[string]any{
			"message": map[string]any{"text": msg},
		}
		if loc != "" {
			parts := strings.Split(loc, ":")
			if len(parts) >= 3 {
				step["location"] = map[string]any{
					"physicalLocation": map[string]any{
						"artifactLocation": map[string]any{"uri": parts[0]},
						"region": map[string]any{
							"startLine":   atoi(parts[1]),
							"startColumn": atoi(parts[2]),
						},
					},
				}
			}
		}
		steps = append(steps, step)
	}
	if len(steps) == 0 {
		return nil
	}
	return []map[string]any{
		{"threadFlows": []map[string]any{{"locations": steps}}},
	}
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
