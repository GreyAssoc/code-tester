package rules

import (
	"fmt"
	"go/types"

	"github.com/GreyAssoc/code-tester/callgraph"
	"github.com/GreyAssoc/code-tester/report"
)

func CheckDead(ctx Context, en Enablement) []Finding {
	var out []Finding
	idx := ctx.Index
	live := ctx.Reach.Live
	roots := callgraph.NormalizeRoots(ctx.Config.Roots)
	testRoots := roots["tests"] || roots["benchmarks"] || roots["fuzz"] || roots["examples"]

	// Pass 1: collect dead methods belonging to dead types (fold into DEAD003).
	deadMethodsByType := map[types.Object][]*callgraph.Symbol{}
	for _, sym := range idx.Symbols {
		if sym.Kind != callgraph.KindMethod || sym.Generated || sym.IsBlank() {
			continue
		}
		if sym.IsTest && !testRoots {
			continue
		}
		if live[sym] {
			continue
		}
		if tn := typeOfMethod(sym); tn != nil {
			if tsym, ok := idx.ByObj[tn]; ok && !live[tsym] {
				deadMethodsByType[tn] = append(deadMethodsByType[tn], sym)
			}
		}
	}

	for _, sym := range idx.Symbols {
		if sym.Generated || sym.IsBlank() {
			continue
		}
		if sym.IsTest && !testRoots {
			continue
		}
		if live[sym] {
			continue
		}
		file, line, col := sym.Location(ctx.RootDir)
		switch sym.Kind {
		case callgraph.KindFunc:
			if !en.Enabled["DEAD001"] {
				continue
			}
			out = append(out, finding("DEAD001", "unreachable-func", "dead-code", en.Severity["DEAD001"],
				report.ConfidenceHigh, fmt.Sprintf("function %s is unreachable from configured roots", sym.Qualified),
				sym.Qualified, file, line, col))
		case callgraph.KindMethod:
			if !en.Enabled["DEAD002"] {
				continue
			}
			if tn := typeOfMethod(sym); tn != nil {
				if _, folded := deadMethodsByType[tn]; folded {
					continue
				}
			}
			conf := report.ConfidenceHigh
			if sym.Exported {
				conf = report.ConfidenceMedium
			}
			out = append(out, finding("DEAD002", "unreachable-method", "dead-code", en.Severity["DEAD002"],
				conf, fmt.Sprintf("method %s is unreachable from configured roots", sym.Qualified),
				sym.Qualified, file, line, col))
		case callgraph.KindType:
			if !en.Enabled["DEAD003"] {
				continue
			}
			// If any method of this type is live, the type is in use.
			if typeHasLiveMethod(idx, live, sym.Obj) {
				continue
			}
			nMethods := len(deadMethodsByType[sym.Obj])
			msg := fmt.Sprintf("type %s is unused", sym.Qualified)
			if nMethods > 0 {
				msg = fmt.Sprintf("type %s is unused (nor are its %d method(s))", sym.Qualified, nMethods)
			}
			out = append(out, finding("DEAD003", "unused-type", "dead-code", en.Severity["DEAD003"],
				report.ConfidenceHigh, msg, sym.Qualified, file, line, col))
		case callgraph.KindConst:
			if !en.Enabled["DEAD004"] {
				continue
			}
			conf := report.ConfidenceHigh
			if partiallyUsedConstBlock(idx, live, sym) {
				conf = report.ConfidenceMedium
			}
			out = append(out, finding("DEAD004", "unused-const", "dead-code", en.Severity["DEAD004"],
				conf, fmt.Sprintf("const %s is unused", sym.Qualified),
				sym.Qualified, file, line, col))
		case callgraph.KindVar:
			if !en.Enabled["DEAD005"] {
				continue
			}
			out = append(out, finding("DEAD005", "unused-var", "dead-code", en.Severity["DEAD005"],
				report.ConfidenceHigh, fmt.Sprintf("var %s is unused", sym.Qualified),
				sym.Qualified, file, line, col))
		}
	}
	return out
}

func typeOfMethod(sym *callgraph.Symbol) types.Object {
	fn, ok := sym.Obj.(*types.Func)
	if !ok {
		return nil
	}
	sig, _ := fn.Type().(*types.Signature)
	if sig == nil || sig.Recv() == nil {
		return nil
	}
	t := sig.Recv().Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		return n.Obj()
	}
	return nil
}

func typeHasLiveMethod(idx *callgraph.Index, live map[*callgraph.Symbol]bool, typeObj types.Object) bool {
	if typeObj == nil {
		return false
	}
	for _, sym := range idx.Symbols {
		if sym.Kind != callgraph.KindMethod || !live[sym] {
			continue
		}
		if typeOfMethod(sym) == typeObj {
			return true
		}
	}
	return false
}

func partiallyUsedConstBlock(idx *callgraph.Index, live map[*callgraph.Symbol]bool, sym *callgraph.Symbol) bool {
	for _, other := range idx.Symbols {
		if other.Kind != callgraph.KindConst || other == sym {
			continue
		}
		if other.PkgPath != sym.PkgPath {
			continue
		}
		if live[other] {
			return true
		}
	}
	return false
}

func finding(id, name, cat string, sev report.Severity, conf report.Confidence, msg, symbol, file string, line, col int) Finding {
	return Finding{
		RuleID:      id,
		Rule:        name,
		Category:    cat,
		Severity:    sev,
		Confidence:  conf,
		Message:     msg,
		Symbol:      symbol,
		Location:    report.Location{File: file, Line: line, Column: col},
		Fingerprint: report.Fingerprint(id, symbol, msg, file),
	}
}
