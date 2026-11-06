package rules

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"

	"github.com/GreyAssoc/code-tester/callgraph"
	"github.com/GreyAssoc/code-tester/report"
)

func CheckCoherence(ctx Context, en Enablement, stubbed map[string]bool) []Finding {
	var out []Finding
	idx := ctx.Index

	summaries := BuildParamSummaries(idx)

	for _, sym := range idx.Symbols {
		if sym.Kind != callgraph.KindFunc && sym.Kind != callgraph.KindMethod {
			continue
		}
		if stubbed[sym.Qualified] {
			continue
		}
		if sym.Generated {
			continue
		}
		if sym.IsTest && !ctx.Config.Coherence.IncludeTests {
			continue
		}
		if !sym.Exported {
			continue
		}
		if sym.AddressTaken {
			continue
		}
		fd := funcDecl(sym)
		if fd == nil {
			continue
		}
		if isDeprecated(fd) {
			continue
		}
		if matchesAnyInterface(idx, sym) {
			continue
		}

		file, line, col := sym.Location(ctx.RootDir)
		params := namedParams(fd)
		if len(params) == 0 {
			continue
		}

		usedAST := paramASTUses(fd)
		for _, p := range params {
			u := usedAST[p]
			if !u.referenced && en.Enabled["COH001"] {
				out = append(out, finding("COH001", "param-unused", "coherence", en.Severity["COH001"],
					report.ConfidenceHigh,
					fmt.Sprintf("parameter %q of %s is never referenced", p, sym.Qualified),
					sym.Qualified, file, line, col))
				continue
			}
			if u.onlyBlank && en.Enabled["COH002"] {
				out = append(out, finding("COH002", "param-blank-assigned", "coherence", en.Severity["COH002"],
					report.ConfidenceHigh,
					fmt.Sprintf("parameter %q of %s is only assigned to _", p, sym.Qualified),
					sym.Qualified, file, line, col))
				continue
			}
			if u.referenced && !u.onlyBlank && en.Enabled["COH003"] {
				if !paramHasEffect(sym, p, summaries) {
					out = append(out, finding("COH003", "param-no-effect", "coherence", en.Severity["COH003"],
						report.ConfidenceMedium,
						fmt.Sprintf("parameter %q of %s never reaches a result, side effect, or effectful callee", p, sym.Qualified),
						sym.Qualified, file, line, col))
				}
			}
		}

		if en.Enabled["COH004"] && sym.SSAFunc != nil {
			if msg, ok := constantResult(sym.SSAFunc); ok {
				out = append(out, finding("COH004", "constant-result", "coherence", en.Severity["COH004"],
					report.ConfidenceMedium, msg, sym.Qualified, file, line, col))
			}
		}
	}
	return out
}

type paramUse struct {
	referenced bool
	onlyBlank  bool
}

func namedParams(fd *ast.FuncDecl) []string {
	var names []string
	if fd.Type.Params == nil {
		return names
	}
	for _, f := range fd.Type.Params.List {
		for _, n := range f.Names {
			if n.Name != "" && n.Name != "_" {
				names = append(names, n.Name)
			}
		}
	}
	return names
}

func paramASTUses(fd *ast.FuncDecl) map[string]paramUse {
	out := map[string]paramUse{}
	params := map[string]bool{}
	for _, p := range namedParams(fd) {
		params[p] = true
		out[p] = paramUse{}
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.AssignStmt:
			// _ = param
			for i, lhs := range e.Lhs {
				lid, ok := lhs.(*ast.Ident)
				if !ok || lid.Name != "_" || i >= len(e.Rhs) {
					continue
				}
				if rid, ok := e.Rhs[i].(*ast.Ident); ok && params[rid.Name] {
					u := out[rid.Name]
					u.referenced = true
					u.onlyBlank = true
					out[rid.Name] = u
				}
			}
		case *ast.Ident:
			if !params[e.Name] {
				return true
			}
			u := out[e.Name]
			u.referenced = true
			// If we see any non-blank use, clear onlyBlank.
			// blank uses set onlyBlank true; a later real use clears it.
			if u.onlyBlank {
				// keep until we know — real use below
			}
			out[e.Name] = u
		}
		return true
	})
	// Second pass: if referenced in any non-assign-to-blank way, onlyBlank=false.
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || !params[id.Name] {
			return true
		}
		// Check parent via incomplete heuristic: if onlyBlank was set and there's another use, clear.
		u := out[id.Name]
		if u.referenced {
			// Count id occurrences vs blank assigns.
		}
		return true
	})
	for name, u := range out {
		if !u.referenced {
			continue
		}
		uses := 0
		blanks := 0
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if ok {
				for i, lhs := range as.Lhs {
					lid, ok := lhs.(*ast.Ident)
					if !ok || lid.Name != "_" || i >= len(as.Rhs) {
						continue
					}
					if rid, ok := as.Rhs[i].(*ast.Ident); ok && rid.Name == name {
						blanks++
					}
				}
			}
			return true
		})
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if ok && id.Name == name {
				uses++
			}
			return true
		})
		u.onlyBlank = uses > 0 && uses == blanks
		u.referenced = uses > 0
		out[name] = u
	}
	return out
}

func paramHasEffect(sym *callgraph.Symbol, paramName string, summaries map[string][]ParamSummary) bool {
	fd := funcDecl(sym)
	if fd == nil {
		return true
	}
	names := namedParams(fd)
	idx := -1
	for i, n := range names {
		if n == paramName {
			idx = i
			break
		}
	}
	if idx < 0 {
		return true
	}
	sums := summaries[sym.Qualified]
	if idx >= len(sums) {
		return true
	}
	if sums[idx].Effect || sums[idx].FlowsToReturn {
		return true
	}
	// AST fallback: param builds values that are returned (directly or via append).
	return paramFeedsResult(fd, paramName)
}

func paramFeedsResult(fd *ast.FuncDecl, paramName string) bool {
	if fd.Body == nil {
		return false
	}
	// Collect locals that appear in return statements.
	returned := map[string]bool{}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, res := range ret.Results {
			ast.Inspect(res, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					returned[id.Name] = true
					if id.Name == paramName {
						returned[paramName] = true
					}
				}
				return true
			})
		}
		return true
	})
	if returned[paramName] {
		return true
	}
	// Param used inside composite lit / call that assigns or appends into a returned local.
	feeds := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.AssignStmt:
			lhsReturned := false
			for _, lhs := range e.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && returned[id.Name] {
					lhsReturned = true
				}
			}
			if !lhsReturned {
				return true
			}
			for _, rhs := range e.Rhs {
				if exprUsesParam(rhs, paramName) {
					feeds = true
					return false
				}
			}
		case *ast.CallExpr:
			// append(out, …en…) where out is returned
			if id, ok := e.Fun.(*ast.Ident); ok && id.Name == "append" && len(e.Args) >= 2 {
				if dst, ok := e.Args[0].(*ast.Ident); ok && returned[dst.Name] {
					for _, a := range e.Args[1:] {
						if exprUsesParam(a, paramName) {
							feeds = true
							return false
						}
					}
				}
			}
		}
		return true
	})
	return feeds
}

func exprUsesParam(e ast.Expr, paramName string) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if ok && id.Name == paramName {
			found = true
			return false
		}
		return true
	})
	return found
}

func constantResult(fn *ssa.Function) (string, bool) {
	if fn.Blocks == nil {
		return "", false
	}
	sig := fn.Signature
	if sig.Params().Len() == 0 && sig.Recv() == nil {
		return "", false
	}
	results := sig.Results()
	if results.Len() == 0 {
		return "", false
	}
	// Find first non-error result.
	resIdx := -1
	for i := 0; i < results.Len(); i++ {
		t := results.At(i).Type()
		if isErrorType(t) {
			continue
		}
		resIdx = i
		break
	}
	if resIdx < 0 {
		return "", false
	}
	var constVal *ssa.Const
	have := false
	for _, b := range fn.Blocks {
		if len(b.Instrs) == 0 {
			continue
		}
		ret, ok := b.Instrs[len(b.Instrs)-1].(*ssa.Return)
		if !ok || resIdx >= len(ret.Results) {
			continue
		}
		c, ok := ret.Results[resIdx].(*ssa.Const)
		if !ok {
			return "", false
		}
		if !have {
			constVal = c
			have = true
			continue
		}
		if c.String() != constVal.String() {
			return "", false
		}
	}
	if !have {
		return "", false
	}
	return fmt.Sprintf("result %d is the constant %s on every return path", resIdx, constVal.String()), true
}

func isErrorType(t types.Type) bool {
	return types.Identical(t, types.Universe.Lookup("error").Type())
}

func matchesAnyInterface(idx *callgraph.Index, sym *callgraph.Symbol) bool {
	fn, ok := sym.Obj.(*types.Func)
	if !ok {
		return false
	}
	sig, _ := fn.Type().(*types.Signature)
	if sig == nil || sig.Recv() == nil {
		return false
	}
	for _, iface := range idx.Ifaces {
		it, ok := iface.Underlying().(*types.Interface)
		if !ok {
			continue
		}
		for i := 0; i < it.NumMethods(); i++ {
			m := it.Method(i)
			if m.Name() != fn.Name() {
				continue
			}
			if types.Identical(stripRecv(sig), stripRecv(m.Type().(*types.Signature))) {
				return true
			}
		}
	}
	return false
}

func stripRecv(sig *types.Signature) *types.Signature {
	return types.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
}

var _ = strings.Contains
