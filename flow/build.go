package flow

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"

	"github.com/GreyAssoc/code-tester/callgraph"
	"github.com/GreyAssoc/code-tester/loader"
)

type BuildOptions struct {
	ContextSens int // 0 = insensitive, 1 = one level call-site
	Sources     []SourceSpec
	Sinks       []SinkSpec
	RootDir     string
}

// Build constructs the whole-program flow graph from SSA + points-to.
func Build(idx *callgraph.Index, opts BuildOptions) (*Graph, *PointsTo) {
	if opts.Sources == nil {
		opts.Sources = DefaultSources()
	}
	if opts.Sinks == nil {
		opts.Sinks = DefaultSinks()
	}
	primaryPkgs := map[string]bool{}
	for _, p := range idx.Packages {
		if p != nil && p.PkgPath != "" {
			primaryPkgs[p.PkgPath] = true
		}
	}
	var funcs []*ssa.Function
	seenFn := map[*ssa.Function]bool{}
	for _, sym := range idx.Symbols {
		if sym.SSAFunc == nil || sym.SSAFunc.Blocks == nil || seenFn[sym.SSAFunc] {
			continue
		}
		seenFn[sym.SSAFunc] = true
		funcs = append(funcs, sym.SSAFunc)
	}
	// Package init only for primary packages.
	if idx.Prog != nil {
		for _, sp := range idx.Prog.AllPackages() {
			if sp == nil || sp.Pkg == nil || !primaryPkgs[sp.Pkg.Path()] {
				continue
			}
			if init := sp.Func("init"); init != nil && init.Blocks != nil && !seenFn[init] {
				seenFn[init] = true
				funcs = append(funcs, init)
			}
		}
	}
	pt := AnalyzePointsTo(funcs)
	g := NewGraph()

	valNode := func(v ssa.Value) string {
		if v == nil {
			return ""
		}
		key := fmt.Sprintf("global#%s", v.String())
		if p := v.Parent(); p != nil {
			key = fmt.Sprintf("%s#%s", p.String(), v.String())
		}
		id := StableNodeID(NodeValue, key)
		g.Ensure(&Node{ID: id, Kind: NodeValue, Symbol: v.String(), Role: key})
		return id
	}

	fnNodes := map[*ssa.Function]string{}
	for _, sym := range idx.Symbols {
		fn := sym.SSAFunc
		if fn == nil {
			continue
		}
		fid := StableNodeID(NodeFunction, sym.Qualified)
		g.Ensure(&Node{ID: fid, Kind: NodeFunction, Symbol: sym.Qualified})
		fnNodes[fn] = fid
		start := 0
		if fn.Signature.Recv() != nil {
			start = 0
		}
		for i, p := range fn.Params {
			role := fmt.Sprintf("param:%d", i)
			if fn.Signature.Recv() != nil && i == 0 {
				role = "recv"
			}
			pid := StableNodeID(NodeParam, sym.Qualified+"."+role)
			g.Ensure(&Node{ID: pid, Kind: NodeParam, Symbol: sym.Qualified, Role: role})
			if i >= start && sym.Exported {
				name := p.Name()
				if SecretName(name) {
					g.Nodes[pid].Labels = unionSorted(g.Nodes[pid].Labels, []string{string(LabelSecret)})
				}
			}
			vid := valNode(p)
			g.AddEdge(Edge{From: pid, To: vid, Label: EdgeAssign})
		}
		if results := fn.Signature.Results(); results != nil {
			for i := 0; i < results.Len(); i++ {
				rid := StableNodeID(NodeResult, fmt.Sprintf("%s.result:%d", sym.Qualified, i))
				g.Ensure(&Node{ID: rid, Kind: NodeResult, Symbol: sym.Qualified, Role: fmt.Sprintf("result:%d", i)})
			}
		}
	}

	for _, c := range pt.Cells {
		nid := StableNodeID(NodeCell, c.ID)
		n := g.Ensure(&Node{ID: nid, Kind: NodeCell, Symbol: c.ID, Role: c.Name, Approx: c.Approx})
		if c.Kind == CellGlobal || c.Kind == CellField {
			if SecretName(c.Name) {
				n.Labels = unionSorted(n.Labels, []string{string(LabelSecret)})
			}
		}
	}

	locPos := func(pos token.Pos) string {
		if idx.Fset == nil || !pos.IsValid() {
			return ""
		}
		file, line, col := loader.RelPath(idx.Fset, pos, opts.RootDir)
		if file == "" {
			return ""
		}
		return fmt.Sprintf("%s:%d:%d", file, line, col)
	}
	loc := func(v interface{ Pos() token.Pos }) string {
		if v == nil {
			return ""
		}
		return locPos(v.Pos())
	}

	for _, fn := range funcs {
		if fn.Blocks == nil {
			continue
		}
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				switch v := instr.(type) {
				case *ssa.Store:
					from := valNode(v.Val)
					for _, cid := range pt.cellsOf(v.Addr) {
						to := StableNodeID(NodeCell, cid)
						g.AddEdge(Edge{From: from, To: to, Label: EdgeStore, Location: loc(v), Approx: pt.Approx[cid]})
					}
				case *ssa.UnOp:
					if v.Op.String() == "*" {
						to := valNode(v)
						for _, cid := range pt.cellsOf(v.X) {
							from := StableNodeID(NodeCell, cid)
							g.AddEdge(Edge{From: from, To: to, Label: EdgeLoad, Location: loc(v), Approx: pt.Approx[cid]})
						}
					}
				case *ssa.Send:
					from := valNode(v.X)
					for _, cid := range pt.cellsOf(v.Chan) {
						content := ContentCell(CellChanContent, cid)
						pt.addCell(content)
						to := StableNodeID(NodeCell, content.ID)
						g.Ensure(&Node{ID: to, Kind: NodeCell, Symbol: content.ID})
						g.AddEdge(Edge{From: from, To: to, Label: EdgeSend, Location: loc(v)})
					}
				case *ssa.BinOp, *ssa.ChangeType, *ssa.Convert, *ssa.Extract, *ssa.Field, *ssa.Index, *ssa.Slice, *ssa.MakeInterface, *ssa.ChangeInterface, *ssa.TypeAssert:
					to := valNode(instr.(ssa.Value))
					for _, op := range operands(instr) {
						from := valNode(op)
						g.AddEdge(Edge{From: from, To: to, Label: EdgeAssign, Location: loc(instr.(ssa.Value))})
					}
				case *ssa.Phi:
					to := valNode(v)
					for _, e := range v.Edges {
						g.AddEdge(Edge{From: valNode(e), To: to, Label: EdgeAssign, Location: loc(v)})
					}
				case *ssa.Call:
					buildCallEdges(g, pt, idx, fn, v, valNode, loc, opts)
				case *ssa.Return:
					symQual := fn.String()
					if obj := fn.Object(); obj != nil {
						if s := idx.ByObj[obj]; s != nil {
							symQual = s.Qualified
						}
					}
					for i, r := range v.Results {
						rid := StableNodeID(NodeResult, fmt.Sprintf("%s.result:%d", symQual, i))
						g.Ensure(&Node{ID: rid, Kind: NodeResult, Symbol: symQual, Role: fmt.Sprintf("result:%d", i)})
						g.AddEdge(Edge{From: valNode(r), To: rid, Label: EdgeReturn, Location: loc(v)})
						if _, ok := r.(*ssa.Const); ok {
							vid := valNode(r)
							g.Nodes[vid].Labels = unionSorted(g.Nodes[vid].Labels, []string{string(LabelConstant)})
						}
					}
				}
			}
		}
	}

	// Write-to-read linking across cells.
	for _, pair := range pt.WriteReadLinks() {
		g.AddEdge(Edge{From: valNode(pair[0]), To: valNode(pair[1]), Label: EdgeAssign, Location: loc(pair[1])})
	}

	// Source / sink nodes.
	for _, sym := range idx.Symbols {
		if sym.SSAFunc == nil {
			continue
		}
		if lab, ok := MatchAny(sym.Qualified, opts.Sources); ok {
			sid := StableNodeID(NodeSource, sym.Qualified+":"+string(lab))
			g.Ensure(&Node{ID: sid, Kind: NodeSource, Symbol: sym.Qualified, Role: string(lab), Labels: []string{string(lab)}})
			fid := StableNodeID(NodeFunction, sym.Qualified)
			g.AddEdge(Edge{From: sid, To: fid, Label: EdgeAssign})
			// Also label results.
			if results := sym.SSAFunc.Signature.Results(); results != nil {
				for i := 0; i < results.Len(); i++ {
					rid := StableNodeID(NodeResult, fmt.Sprintf("%s.result:%d", sym.Qualified, i))
					g.Ensure(&Node{ID: rid, Kind: NodeResult, Symbol: sym.Qualified, Role: fmt.Sprintf("result:%d", i), Labels: []string{string(lab)}})
					g.AddEdge(Edge{From: sid, To: rid, Label: EdgeAssign})
				}
			}
		}
		if name, ok := MatchSink(sym.Qualified, opts.Sinks); ok {
			kid := StableNodeID(NodeSink, sym.Qualified+":"+name)
			g.Ensure(&Node{ID: kid, Kind: NodeSink, Symbol: sym.Qualified, Role: name})
			fid := StableNodeID(NodeFunction, sym.Qualified)
			for i := range sym.SSAFunc.Params {
				if sym.SSAFunc.Signature.Recv() != nil && i == 0 {
					continue
				}
				role := fmt.Sprintf("param:%d", i)
				pid := StableNodeID(NodeParam, sym.Qualified+"."+role)
				g.AddEdge(Edge{From: pid, To: kid, Label: EdgeArg})
			}
			_ = fid
		}
	}

	// Seed secret labels on globals/fields already marked; propagate.
	g.PropagateLabels()
	g.Finalize()
	return g, pt
}

func buildCallEdges(g *Graph, pt *PointsTo, idx *callgraph.Index, caller *ssa.Function, call *ssa.Call, valNode func(ssa.Value) string, loc func(interface{ Pos() token.Pos }) string, opts BuildOptions) {
	common := call.Common()
	callee := common.StaticCallee()
	callSite := loc(call)
	if callee == nil {
		approx := StableNodeID(NodeCell, "approx:dynamic:"+callSite)
		g.Ensure(&Node{ID: approx, Kind: NodeCell, Symbol: "dynamic", Approx: true})
		for _, a := range common.Args {
			g.AddEdge(Edge{From: valNode(a), To: approx, Label: EdgeArg, Location: callSite, Approx: true})
		}
		g.AddEdge(Edge{From: approx, To: valNode(call), Label: EdgeReturn, Location: callSite, Approx: true})
		return
	}
	calQual := callee.String()
	if obj := callee.Object(); obj != nil {
		if s := idx.ByObj[obj]; s != nil {
			calQual = s.Qualified
		}
	}
	// External sink recognition (fmt.Print, log.*, etc.).
	if sinkName, ok := MatchSink(calQual, opts.Sinks); ok {
		kid := StableNodeID(NodeSink, calQual+":"+sinkName)
		g.Ensure(&Node{ID: kid, Kind: NodeSink, Symbol: calQual, Role: sinkName})
		for _, a := range common.Args {
			g.AddEdge(Edge{From: valNode(a), To: kid, Label: EdgeArg, Location: callSite})
			// Variadic sinks often pass a []any: link cells and prior stores into the sink.
			for _, cid := range pt.cellsOf(a) {
				cellNode := StableNodeID(NodeCell, cid)
				g.AddEdge(Edge{From: cellNode, To: kid, Label: EdgeArg, Location: callSite})
				for _, w := range pt.Writes[cid] {
					g.AddEdge(Edge{From: valNode(w), To: kid, Label: EdgeArg, Location: callSite})
				}
				// Content cell for this container.
				for _, kind := range []CellKind{CellSliceContent, CellMapContent, CellChanContent} {
					content := ContentCell(kind, cid)
					if _, ok := pt.Cells[content.ID]; ok {
						cnode := StableNodeID(NodeCell, content.ID)
						g.AddEdge(Edge{From: cnode, To: kid, Label: EdgeArg, Location: callSite})
						for _, w := range pt.Writes[content.ID] {
							g.AddEdge(Edge{From: valNode(w), To: kid, Label: EdgeArg, Location: callSite})
						}
					}
				}
			}
		}
	}
	if srcLab, ok := MatchAny(calQual, opts.Sources); ok {
		sid := StableNodeID(NodeSource, calQual+":"+string(srcLab))
		g.Ensure(&Node{ID: sid, Kind: NodeSource, Symbol: calQual, Role: string(srcLab), Labels: []string{string(srcLab)}})
		g.AddEdge(Edge{From: sid, To: valNode(call), Label: EdgeReturn, Location: callSite})
	}
	for i, a := range common.Args {
		role := fmt.Sprintf("param:%d", i)
		if callee.Signature.Recv() != nil && i == 0 {
			role = "recv"
		}
		pid := StableNodeID(NodeParam, calQual+"."+role)
		g.Ensure(&Node{ID: pid, Kind: NodeParam, Symbol: calQual, Role: role})
		g.AddEdge(Edge{From: valNode(a), To: pid, Label: EdgeArg, Location: callSite})
		if opts.ContextSens >= 1 {
			ctxID := StableNodeID(NodeParam, calQual+"."+role+"@"+callSite)
			g.Ensure(&Node{ID: ctxID, Kind: NodeParam, Symbol: calQual, Role: role + "@ctx"})
			g.AddEdge(Edge{From: valNode(a), To: ctxID, Label: EdgeArg, Location: callSite})
			g.AddEdge(Edge{From: ctxID, To: pid, Label: EdgeAssign, Location: callSite})
		}
	}
	if results := callee.Signature.Results(); results != nil && results.Len() > 0 {
		for i := 0; i < results.Len(); i++ {
			rid := StableNodeID(NodeResult, fmt.Sprintf("%s.result:%d", calQual, i))
			g.Ensure(&Node{ID: rid, Kind: NodeResult, Symbol: calQual, Role: fmt.Sprintf("result:%d", i)})
			if results.Len() == 1 {
				g.AddEdge(Edge{From: rid, To: valNode(call), Label: EdgeReturn, Location: callSite})
			}
		}
	}
	if pkg := callee.Pkg; pkg != nil && pkg.Pkg != nil && isApproxPkg(pkg.Pkg.Path()) {
		for _, a := range common.Args {
			g.AddEdge(Edge{From: valNode(a), To: valNode(call), Label: EdgeAssign, Location: callSite, Approx: true})
		}
	}
	_ = pt
	_ = caller
}

func operands(instr ssa.Instruction) []ssa.Value {
	var out []ssa.Value
	switch v := instr.(type) {
	case *ssa.BinOp:
		out = []ssa.Value{v.X, v.Y}
	case *ssa.ChangeType:
		out = []ssa.Value{v.X}
	case *ssa.Convert:
		out = []ssa.Value{v.X}
	case *ssa.Extract:
		out = []ssa.Value{v.Tuple}
	case *ssa.Field:
		out = []ssa.Value{v.X}
	case *ssa.Index:
		out = []ssa.Value{v.X, v.Index}
	case *ssa.Slice:
		out = []ssa.Value{v.X}
	case *ssa.MakeInterface:
		out = []ssa.Value{v.X}
	case *ssa.ChangeInterface:
		out = []ssa.Value{v.X}
	case *ssa.TypeAssert:
		out = []ssa.Value{v.X}
	}
	return out
}

var _ = types.Universe
var _ = strings.Contains
