package callgraph

import (
	"fmt"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/ssa"
)

type Reachability struct {
	Live     map[*Symbol]bool
	CallGraph *callgraph.Graph
	RootFns  []*ssa.Function
}

func Analyze(idx *Index, roots []*Symbol) *Reachability {
	live := map[*Symbol]bool{}
	var rootFns []*ssa.Function
	for _, r := range roots {
		live[r] = true
		if r.SSAFunc != nil {
			rootFns = append(rootFns, r.SSAFunc)
		}
	}
	rootFns = append(rootFns, idx.extraRootFns...)

	var cg *callgraph.Graph
	if len(rootFns) > 0 {
		res := rta.Analyze(rootFns, true)
		cg = res.CallGraph
		for fn, node := range cg.Nodes {
			if fn == nil {
				continue
			}
			if sym := idx.BySSA[fn]; sym != nil {
				live[sym] = true
			} else if fn.Object() != nil {
				if sym := idx.ByObj[fn.Object()]; sym != nil {
					live[sym] = true
					idx.BySSA[fn] = sym
				}
			}
			_ = node
		}
	}

	// Fixed point: RTA + symbol refs + interface satisfaction.
	for changed := true; changed; {
		changed = false

		// Symbol reference walk.
		for sym := range live {
			for ref := range sym.Refs {
				if !live[ref] {
					live[ref] = true
					changed = true
				}
			}
			// A live method keeps its receiver type live (bodies often never name it).
			if sym.Kind == KindMethod && sym.Obj != nil {
				if fn, ok := sym.Obj.(*types.Func); ok {
					if sig, _ := fn.Type().(*types.Signature); sig != nil && sig.Recv() != nil {
						if tn := namedTypeName(sig.Recv().Type()); tn != nil {
							if tsym := idx.ByObj[tn]; tsym != nil && !live[tsym] {
								live[tsym] = true
								changed = true
							}
						}
					}
				}
			}
		}

		// Address-taken functions become live when their containing context is live — already via refs.
		for _, sym := range idx.Symbols {
			if sym.AddressTaken && !live[sym] {
				// Only mark address-taken if referenced from live code (refs walk handles it).
			}
		}

		// Interface satisfaction: live type T implementing live/referenced interface → methods live.
		liveIfaces := map[*types.Named]bool{}
		for sym := range live {
			for _, iface := range sym.Ifaces {
				liveIfaces[iface] = true
			}
			if sym.Kind == KindType {
				if named, ok := sym.Obj.Type().(*types.Named); ok {
					if _, ok := named.Underlying().(*types.Interface); ok {
						liveIfaces[named] = true
					}
				}
			}
		}
		// Also consider error and Stringer.
		for _, sym := range idx.Symbols {
			if sym.Kind != KindType {
				continue
			}
			if !live[sym] {
				continue
			}
			named, ok := sym.Obj.(*types.TypeName).Type().(*types.Named)
			if !ok {
				continue
			}
			for iface := range allRelevantIfaces(idx, liveIfaces) {
				ifaceType, ok := iface.Underlying().(*types.Interface)
				if !ok {
					continue
				}
				if !types.Implements(named, ifaceType) && !types.Implements(types.NewPointer(named), ifaceType) {
					continue
				}
				for i := 0; i < named.NumMethods(); i++ {
					m := named.Method(i)
					if ms, ok := idx.ByObj[m]; ok && !live[ms] {
						// Method must match an interface method.
						if ifaceHasMethod(ifaceType, m.Name()) {
							live[ms] = true
							changed = true
						}
					}
				}
			}
		}
	}

	return &Reachability{Live: live, CallGraph: cg, RootFns: rootFns}
}

func allRelevantIfaces(idx *Index, live map[*types.Named]bool) map[*types.Named]bool {
	out := map[*types.Named]bool{}
	for k, v := range live {
		out[k] = v
	}
	for _, iface := range idx.Ifaces {
		out[iface] = true
	}
	return out
}

func ifaceHasMethod(iface *types.Interface, name string) bool {
	for i := 0; i < iface.NumMethods(); i++ {
		if iface.Method(i).Name() == name {
			return true
		}
	}
	return false
}

func namedTypeName(t types.Type) *types.TypeName {
	if t == nil {
		return nil
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		return n.Obj()
	}
	return nil
}

type PathEdge struct {
	Caller string
	Callee string
	Site   string
}

// ShortestPath returns a deterministic BFS path from any root to target.
func ShortestPath(r *Reachability, target *Symbol) []PathEdge {
	if r.CallGraph == nil || target.SSAFunc == nil {
		return nil
	}
	targetNode := r.CallGraph.Nodes[target.SSAFunc]
	if targetNode == nil {
		return nil
	}

	type item struct {
		node *callgraph.Node
		path []PathEdge
	}
	// Start from root functions.
	q := []item{}
	seen := map[*callgraph.Node]bool{}
	for _, rf := range r.RootFns {
		n := r.CallGraph.Nodes[rf]
		if n == nil {
			continue
		}
		q = append(q, item{node: n})
		seen[n] = true
	}
	// Deterministic queue order by function name.
	sort.Slice(q, func(i, j int) bool {
		return q[i].node.Func.String() < q[j].node.Func.String()
	})

	for len(q) > 0 {
		cur := q[0]
		q = q[1:]
		if cur.node == targetNode {
			return cur.path
		}
		edges := append([]*callgraph.Edge(nil), cur.node.Out...)
		sort.Slice(edges, func(i, j int) bool {
			a, b := edges[i], edges[j]
			an, bn := "", ""
			if a.Callee != nil && a.Callee.Func != nil {
				an = a.Callee.Func.String()
			}
			if b.Callee != nil && b.Callee.Func != nil {
				bn = b.Callee.Func.String()
			}
			if an != bn {
				return an < bn
			}
			return edgePos(a) < edgePos(b)
		})
		for _, e := range edges {
			if e.Callee == nil || seen[e.Callee] {
				continue
			}
			seen[e.Callee] = true
			pe := PathEdge{
				Caller: cur.node.Func.String(),
				Callee: e.Callee.Func.String(),
				Site:   edgePos(e),
			}
			np := append(append([]PathEdge(nil), cur.path...), pe)
			q = append(q, item{node: e.Callee, path: np})
		}
	}
	return nil
}

func edgePos(e *callgraph.Edge) string {
	if e == nil || e.Site == nil {
		return ""
	}
	pos := e.Site.Pos()
	if !pos.IsValid() {
		return ""
	}
	// Best-effort without fset here.
	return fmt.Sprintf("%v", pos)
}

func (r *Reachability) IsLive(sym *Symbol) bool {
	return r.Live[sym]
}

func FormatPath(path []PathEdge) string {
	if len(path) == 0 {
		return ""
	}
	parts := make([]string, 0, len(path)+1)
	parts = append(parts, path[0].Caller)
	for _, e := range path {
		parts = append(parts, e.Callee)
	}
	return strings.Join(parts, " -> ")
}
