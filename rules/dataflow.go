package rules

import (
	"fmt"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"

	"github.com/GreyAssoc/code-tester/callgraph"
)

// ParamSummary describes how a parameter is used inside a function.
type ParamSummary struct {
	Effect        bool // reaches side effect, branch, or effectful callee
	FlowsToReturn bool
}

// BuildParamSummaries computes interprocedural param→effect / param→return
// summaries to a monotone fixed point over the call graph.
func BuildParamSummaries(idx *callgraph.Index) map[string][]ParamSummary {
	out := map[string][]ParamSummary{}
	order := []*callgraph.Symbol{}
	for _, sym := range idx.Symbols {
		if sym.SSAFunc == nil {
			continue
		}
		order = append(order, sym)
		fn := sym.SSAFunc
		start := 0
		if fn.Signature.Recv() != nil {
			start = 1
		}
		sums := make([]ParamSummary, 0, len(fn.Params)-start)
		for i := start; i < len(fn.Params); i++ {
			sums = append(sums, summarizeParam(fn, fn.Params[i], nil, 0))
		}
		out[sym.Qualified] = sums
	}
	for iter := 0; iter < 8; iter++ {
		changed := false
		for _, sym := range order {
			fn := sym.SSAFunc
			start := 0
			if fn.Signature.Recv() != nil {
				start = 1
			}
			sums := out[sym.Qualified]
			for i := start; i < len(fn.Params); i++ {
				pi := i - start
				if pi >= len(sums) {
					continue
				}
				s := summarizeParam(fn, fn.Params[i], out, 0)
				if s != sums[pi] {
					sums[pi] = s
					changed = true
				}
			}
			out[sym.Qualified] = sums
		}
		if !changed {
			break
		}
	}
	return out
}

func summarizeParam(fn *ssa.Function, p ssa.Value, callees map[string][]ParamSummary, depth int) ParamSummary {
	if depth > 32 {
		return ParamSummary{Effect: true}
	}
	sum := ParamSummary{}
	seen := map[ssa.Value]bool{}
	var walk func(v ssa.Value)
	walk = func(v ssa.Value) {
		if v == nil || seen[v] {
			return
		}
		seen[v] = true
		refs := v.Referrers()
		if refs == nil {
			return
		}
		for _, ref := range *refs {
			switch instr := ref.(type) {
			case *ssa.Return:
				sum.FlowsToReturn = true
				sum.Effect = true
			case *ssa.Store:
				// Local alloc stores continue tracking via the alloc; non-local = effect.
				if isLocalAllocAddr(instr.Addr) {
					walk(instr.Addr)
					// FieldAddr/IndexAddr stores must also chase the root Alloc so
					// later append/return of that object counts as FlowsToReturn.
					if root := rootLocalAlloc(instr.Addr); root != nil {
						walk(root)
					}
				} else {
					sum.Effect = true
				}
			case *ssa.MapUpdate, *ssa.Send, *ssa.Panic:
				sum.Effect = true
			case *ssa.If:
				sum.Effect = true
			case *ssa.Go, *ssa.Defer:
				sum.Effect = true
			case *ssa.Call:
				common := instr.Common()
				if common.IsInvoke() || common.Value != nil && isFuncValue(common.Value) {
					sum.Effect = true
					continue
				}
				callee := common.StaticCallee()
				if callee == nil {
					sum.Effect = true
					continue
				}
				if callee == fn {
					// Self-recursion alone does not count as use.
					continue
				}
				if callee.Blocks == nil {
					sum.Effect = true
					continue
				}
				argIdx := argIndexOf(instr, v)
				if argIdx < 0 {
					continue
				}
				start := 0
				if callee.Signature.Recv() != nil {
					start = 1
				}
				logical := argIdx - start
				if callees != nil {
					qual := funcQual(callee)
					if sums, ok := callees[qual]; ok && logical >= 0 && logical < len(sums) {
						if sums[logical].Effect {
							sum.Effect = true
						}
						if sums[logical].FlowsToReturn {
							sum.FlowsToReturn = true
							sum.Effect = true
							walk(instr)
						}
						continue
					}
				}
				// No summary yet: conservative for external, else chase.
				if callee.Pkg == nil || !sameProg(fn, callee) {
					sum.Effect = true
					continue
				}
				if argIdx >= len(callee.Params) {
					sum.Effect = true
					continue
				}
				cs := summarizeParam(callee, callee.Params[argIdx], callees, depth+1)
				if cs.Effect {
					sum.Effect = true
				}
				if cs.FlowsToReturn {
					sum.FlowsToReturn = true
					sum.Effect = true
					walk(instr)
				}
			case *ssa.Extract, *ssa.Field, *ssa.FieldAddr, *ssa.Index, *ssa.IndexAddr,
				*ssa.Lookup, *ssa.MakeMap, *ssa.MakeSlice,
				*ssa.Slice, *ssa.ChangeType, *ssa.Convert, *ssa.MakeInterface,
				*ssa.ChangeInterface, *ssa.TypeAssert, *ssa.UnOp, *ssa.BinOp,
				*ssa.Phi, *ssa.MakeClosure, *ssa.Range, *ssa.Next:
				if val, ok := ref.(ssa.Value); ok {
					walk(val)
				}
			default:
				if val, ok := ref.(ssa.Value); ok {
					walk(val)
				}
			}
		}
	}
	walk(p)
	return sum
}

func isLocalAllocAddr(addr ssa.Value) bool {
	return rootLocalAlloc(addr) != nil
}

func rootLocalAlloc(addr ssa.Value) *ssa.Alloc {
	switch a := addr.(type) {
	case *ssa.Alloc:
		if a.Heap == false {
			return a
		}
		return nil
	case *ssa.FieldAddr:
		return rootLocalAlloc(a.X)
	case *ssa.IndexAddr:
		return rootLocalAlloc(a.X)
	}
	return nil
}

func isFuncValue(v ssa.Value) bool {
	_, ok := v.Type().Underlying().(*types.Signature)
	return ok
}

func argIndexOf(call *ssa.Call, v ssa.Value) int {
	for i, a := range call.Common().Args {
		if a == v {
			return i
		}
	}
	return -1
}

func funcQual(fn *ssa.Function) string {
	if fn.Object() != nil {
		return callgraphObjString(fn.Object())
	}
	return fn.String()
}

func callgraphObjString(obj types.Object) string {
	if obj == nil {
		return ""
	}
	pkg := ""
	if obj.Pkg() != nil {
		pkg = obj.Pkg().Path()
	}
	if f, ok := obj.(*types.Func); ok {
		if sig, _ := f.Type().(*types.Signature); sig != nil && sig.Recv() != nil {
			recv := types.TypeString(sig.Recv().Type(), func(p *types.Package) string {
				if p == nil {
					return ""
				}
				return p.Path()
			})
			recv = strings.TrimPrefix(recv, "*")
			if i := strings.LastIndex(recv, "."); i >= 0 {
				recv = recv[i+1:]
			}
			return fmt.Sprintf("%s.%s.%s", pkg, recv, f.Name())
		}
	}
	if pkg == "" {
		return obj.Name()
	}
	return pkg + "." + obj.Name()
}

func sameProg(a, b *ssa.Function) bool {
	return a != nil && b != nil && a.Prog == b.Prog
}

// DropReason describes why a value failed to reach a sink.
type DropReason struct {
	Message string
	Pos     string
}

// DataflowReaches traces fromResult of each static call to `from` inside
// `caller`, asking whether it reaches argument toArg of a call to `to`.
func DataflowReaches(caller, from, to *ssa.Function, fromResult, toArg, maxDepth int) (ok bool, drops []DropReason) {
	if caller == nil || from == nil || to == nil || caller.Blocks == nil {
		return false, []DropReason{{Message: "no SSA body"}}
	}
	if maxDepth <= 0 {
		maxDepth = 8
	}
	// Find call sites of `from` in caller.
	type site struct {
		call *ssa.Call
		val  ssa.Value
	}
	var sites []site
	for _, b := range caller.Blocks {
		for _, instr := range b.Instrs {
			call, ok := instr.(*ssa.Call)
			if !ok {
				continue
			}
			if call.Common().StaticCallee() != from {
				continue
			}
			var val ssa.Value = call
			if fromResult > 0 || (from.Signature.Results() != nil && from.Signature.Results().Len() > 1) {
				// Multi-result: prefer Extract of the desired index.
				extracted := false
				if refs := call.Referrers(); refs != nil {
					for _, r := range *refs {
						if ex, ok := r.(*ssa.Extract); ok && ex.Index == fromResult {
							val = ex
							extracted = true
							break
						}
					}
				}
				if !extracted && from.Signature.Results().Len() > 1 {
					drops = append(drops, DropReason{Message: "dropped: from_result not extracted"})
					continue
				}
			}
			sites = append(sites, site{call: call, val: val})
		}
	}
	if len(sites) == 0 {
		return false, []DropReason{{Message: "dropped: value is never used afterwards (discarded or overwritten)"}}
	}
	allOK := true
	for _, s := range sites {
		reached, why := traceToCallee(caller, s.val, to, toArg, maxDepth, map[ssa.Value]bool{})
		if !reached {
			allOK = false
			drops = append(drops, why...)
		}
	}
	return allOK, drops
}

func traceToCallee(fn *ssa.Function, v ssa.Value, to *ssa.Function, toArg, depth int, seen map[ssa.Value]bool) (bool, []DropReason) {
	if v == nil || depth < 0 {
		return false, []DropReason{{Message: "dropped: max depth exceeded"}}
	}
	if seen[v] {
		return false, nil
	}
	seen[v] = true
	refs := v.Referrers()
	if refs == nil || len(*refs) == 0 {
		return false, []DropReason{{Message: "dropped: value is never used afterwards (discarded or overwritten)"}}
	}
	reached := false
	var drops []DropReason
	used := false
	onlyCompared := true
	for _, ref := range *refs {
		used = true
		switch instr := ref.(type) {
		case *ssa.BinOp:
			// Comparison does not propagate.
			onlyCompared = onlyCompared && isCompare(instr.Op.String())
			if !isCompare(instr.Op.String()) {
				onlyCompared = false
				if ok, d := traceToCallee(fn, instr, to, toArg, depth-1, seen); ok {
					reached = true
				} else {
					drops = append(drops, d...)
				}
			}
		case *ssa.Call:
			callee := instr.Common().StaticCallee()
			if callee == to {
				for i, a := range instr.Common().Args {
					if toArg >= 0 && i != toArg {
						continue
					}
					if a == v {
						reached = true
						onlyCompared = false
					}
				}
				if !reached {
					drops = append(drops, DropReason{Message: "dropped: passed as the wrong argument"})
					onlyCompared = false
				}
				continue
			}
			onlyCompared = false
			argIdx := argIndexOf(instr, v)
			if argIdx < 0 {
				continue
			}
			if callee == nil || instr.Common().IsInvoke() {
				drops = append(drops, DropReason{Message: "dropped: passed to an external or dynamic call"})
				continue
			}
			if callee.Blocks == nil {
				drops = append(drops, DropReason{Message: "dropped: passed to an external or dynamic call"})
				continue
			}
			// Dive into callee: does param flow to return? then continue with result.
			ps := summarizeParam(callee, callee.Params[argIdx], nil, 0)
			if ps.FlowsToReturn {
				if ok, d := traceToCallee(fn, instr, to, toArg, depth-1, seen); ok {
					reached = true
				} else {
					drops = append(drops, d...)
				}
			} else if ps.Effect {
				drops = append(drops, DropReason{Message: "dropped: value consumed by callee without reaching sink"})
			} else {
				drops = append(drops, DropReason{Message: "dropped: value is never used afterwards (discarded or overwritten)"})
			}
		case *ssa.Return:
			onlyCompared = false
			// Returned — try static callers (limited).
			drops = append(drops, DropReason{Message: "dropped: returned from a function with no static callers tracked"})
		case *ssa.Store:
			onlyCompared = false
			if isLocalAllocAddr(instr.Addr) {
				if ok, d := traceToCallee(fn, instr.Addr, to, toArg, depth-1, seen); ok {
					reached = true
				} else {
					drops = append(drops, d...)
				}
			} else {
				drops = append(drops, DropReason{Message: "dropped: stored to non-local memory"})
			}
		case *ssa.Extract, *ssa.Field, *ssa.FieldAddr, *ssa.Index, *ssa.IndexAddr,
			*ssa.Slice, *ssa.ChangeType, *ssa.Convert, *ssa.MakeInterface,
			*ssa.ChangeInterface, *ssa.TypeAssert, *ssa.UnOp, *ssa.Phi, *ssa.MakeClosure:
			onlyCompared = false
			if val, ok := ref.(ssa.Value); ok {
				if ok, d := traceToCallee(fn, val, to, toArg, depth-1, seen); ok {
					reached = true
				} else {
					drops = append(drops, d...)
				}
			}
		case *ssa.If:
			onlyCompared = false
			drops = append(drops, DropReason{Message: "dropped: only used in a branch"})
		default:
			onlyCompared = false
			if val, ok := ref.(ssa.Value); ok {
				if ok, d := traceToCallee(fn, val, to, toArg, depth-1, seen); ok {
					reached = true
				} else {
					drops = append(drops, d...)
				}
			}
		}
	}
	if reached {
		return true, nil
	}
	if !used {
		return false, []DropReason{{Message: "dropped: value is never used afterwards (discarded or overwritten)"}}
	}
	if onlyCompared {
		return false, []DropReason{{Message: "dropped: only compared (==); a comparison result is not data flow"}}
	}
	if len(drops) == 0 {
		drops = []DropReason{{Message: "dropped: value does not reach callee"}}
	}
	return false, drops
}

func isCompare(op string) bool {
	switch op {
	case "==", "!=", "<", ">", "<=", ">=":
		return true
	}
	return false
}
