package flow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

type NodeKind string

const (
	NodeFunction NodeKind = "function"
	NodeParam    NodeKind = "param"
	NodeResult   NodeKind = "result"
	NodeCell     NodeKind = "memory_cell"
	NodeSource   NodeKind = "source"
	NodeSink     NodeKind = "sink"
	NodeValue    NodeKind = "value" // transient SSA value node
)

type EdgeLabel string

const (
	EdgeAssign EdgeLabel = "assign"
	EdgeArg    EdgeLabel = "arg"
	EdgeReturn EdgeLabel = "return"
	EdgeStore  EdgeLabel = "store"
	EdgeLoad   EdgeLabel = "load"
	EdgeSend   EdgeLabel = "send"
)

type Node struct {
	ID       string   `json:"id"`
	Kind     NodeKind `json:"kind"`
	Symbol   string   `json:"symbol,omitempty"`
	Role     string   `json:"role,omitempty"` // param:0, result:1, cell id, source kind
	Labels   []string `json:"labels,omitempty"`
	Approx   bool     `json:"approx,omitempty"`
}

type Edge struct {
	From     string    `json:"from"`
	To       string    `json:"to"`
	Label    EdgeLabel `json:"label"`
	Location string    `json:"location,omitempty"` // file:line:col
	Approx   bool      `json:"approx,omitempty"`
}

type Graph struct {
	Nodes map[string]*Node
	Edges []Edge
	// adjacency for queries
	out map[string][]int
	in  map[string][]int
}

func NewGraph() *Graph {
	return &Graph{
		Nodes: map[string]*Node{},
		out:   map[string][]int{},
		in:    map[string][]int{},
	}
}

func (g *Graph) Ensure(n *Node) *Node {
	if existing, ok := g.Nodes[n.ID]; ok {
		return existing
	}
	g.Nodes[n.ID] = n
	return n
}

func (g *Graph) AddEdge(e Edge) {
	if e.From == "" || e.To == "" || e.From == e.To {
		return
	}
	g.Ensure(&Node{ID: e.From})
	g.Ensure(&Node{ID: e.To})
	idx := len(g.Edges)
	g.Edges = append(g.Edges, e)
	g.out[e.From] = append(g.out[e.From], idx)
	g.in[e.To] = append(g.in[e.To], idx)
}

func NodeID(kind NodeKind, parts ...string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s", kind)
	for _, p := range parts {
		fmt.Fprintf(h, "\n%s", p)
	}
	sum := hex.EncodeToString(h.Sum(nil))[:16]
	return fmt.Sprintf("%s:%s:%s", kind, strings.Join(parts, "/"), sum)
}

func StableNodeID(kind NodeKind, key string) string {
	return fmt.Sprintf("%s:%s", kind, key)
}

// ShortestPath returns a deterministic BFS witness path (node IDs) from src to dst.
func (g *Graph) ShortestPath(src, dst string) []string {
	if src == dst {
		return []string{src}
	}
	type item struct {
		id   string
		path []string
	}
	q := []item{{id: src, path: []string{src}}}
	seen := map[string]bool{src: true}
	for len(q) > 0 {
		cur := q[0]
		q = q[1:]
		outs := append([]int(nil), g.out[cur.id]...)
		sort.Slice(outs, func(i, j int) bool {
			a, b := g.Edges[outs[i]], g.Edges[outs[j]]
			if a.To != b.To {
				return a.To < b.To
			}
			if a.Label != b.Label {
				return a.Label < b.Label
			}
			return a.Location < b.Location
		})
		for _, ei := range outs {
			e := g.Edges[ei]
			if seen[e.To] {
				continue
			}
			seen[e.To] = true
			np := append(append([]string(nil), cur.path...), e.To)
			if e.To == dst {
				return np
			}
			q = append(q, item{id: e.To, path: np})
		}
	}
	return nil
}

// CanReach reports whether dst is reachable from src.
func (g *Graph) CanReach(src, dst string) bool {
	return len(g.ShortestPath(src, dst)) > 0
}

// PathHash fingerprints a witness path for suppressions.
func PathHash(nodes []string) string {
	h := sha256.New()
	for _, n := range nodes {
		fmt.Fprintf(h, "%s\n", n)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// SortedNodeIDs returns deterministic node id list.
func (g *Graph) SortedNodeIDs() []string {
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (g *Graph) Finalize() {
	sort.SliceStable(g.Edges, func(i, j int) bool {
		a, b := g.Edges[i], g.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		if a.Label != b.Label {
			return a.Label < b.Label
		}
		return a.Location < b.Location
	})
	// rebuild adjacency after sort
	g.out = map[string][]int{}
	g.in = map[string][]int{}
	for i, e := range g.Edges {
		g.out[e.From] = append(g.out[e.From], i)
		g.in[e.To] = append(g.in[e.To], i)
	}
}
