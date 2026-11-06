package check

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"

	"gointegrity/callgraph"
	"gointegrity/report"
	"gointegrity/rules"
)

type Engine struct {
	Index   *callgraph.Index
	Reach   *callgraph.Reachability
	RootDir string
	Config  rules.Context
}

func (e *Engine) RunCheck(c Check) report.CheckResult {
	if errMsg := ValidateCheck(c); errMsg != "" {
		return report.CheckResult{ID: c.ID, Kind: c.Kind, Status: "error", Message: errMsg}
	}
	expect := c.Expect
	if expect.Outcome == "" {
		expect.Outcome = "holds"
	}
	if expect.Quantifier == "" {
		expect.Quantifier = "all"
	}
	if expect.MinTargets == 0 {
		expect.MinTargets = 1
	}

	var targets []report.TargetResult
	var errMsg string

	switch c.Kind {
	case "no_placeholder":
		targets = e.kindNoPlaceholder(c)
	case "param_used":
		targets = e.kindParamUsed(c)
	case "reachable_from":
		targets = e.kindReachable(c, true)
	case "not_reachable":
		targets = e.kindReachable(c, false)
	case "calls":
		targets = e.kindCalls(c, true)
	case "never_calls":
		targets = e.kindCalls(c, false)
	case "returns_nonzero":
		targets = e.kindReturnsNonzero(c)
	case "pattern":
		targets = e.kindPattern(c)
	case "returns_error_on_invalid_input":
		targets = e.kindReturnsErrorOnInvalid(c)
	case "dataflow_reaches":
		targets = e.kindDataflowReaches(c)
	default:
		errMsg = "unknown kind"
	}
	if errMsg != "" {
		return report.CheckResult{ID: c.ID, Kind: c.Kind, Status: "error", Message: errMsg}
	}

	if expect.Outcome == "violated" {
		for i := range targets {
			switch targets[i].Status {
			case "pass":
				targets[i].Status = "fail"
			case "fail":
				targets[i].Status = "pass"
			}
		}
	}

	status := aggregate(targets, expect.Quantifier)
	if len(targets) < expect.MinTargets {
		status = "unresolved"
	}
	msg := fmt.Sprintf("%d target(s)", len(targets))
	return report.CheckResult{ID: c.ID, Kind: c.Kind, Status: status, Message: msg, Targets: targets}
}

func aggregate(targets []report.TargetResult, quantifier string) string {
	if len(targets) == 0 {
		return "unresolved"
	}
	hasFail, hasUnresolved, hasError := false, false, false
	allPass := true
	anyPass := false
	for _, t := range targets {
		switch t.Status {
		case "fail":
			hasFail = true
			allPass = false
		case "unresolved":
			hasUnresolved = true
			allPass = false
		case "error":
			hasError = true
			allPass = false
		case "pass":
			anyPass = true
		}
	}
	if hasError {
		return "error"
	}
	if quantifier == "any" {
		if anyPass {
			return "pass"
		}
		if hasFail {
			return "fail"
		}
		if hasUnresolved {
			return "unresolved"
		}
		return "fail"
	}
	// all
	if hasFail {
		return "fail"
	}
	if hasUnresolved {
		return "unresolved"
	}
	if allPass {
		return "pass"
	}
	return "unresolved"
}

func (e *Engine) resolveTarget(c Check) ([]*callgraph.Symbol, string) {
	sel, err := ParseSelector(c.Target)
	if err != nil {
		return nil, "invalid target selector"
	}
	syms := Resolve(e.Index, sel)
	return syms, ""
}

func locOf(sym *callgraph.Symbol, root string) *report.Location {
	f, l, c := sym.Location(root)
	return &report.Location{File: f, Line: l, Column: c}
}

func (e *Engine) kindNoPlaceholder(c Check) []report.TargetResult {
	syms, _ := e.resolveTarget(c)
	if len(syms) == 0 && c.Target != nil {
		return []report.TargetResult{{Status: "unresolved", Message: "selector matched nothing"}}
	}
	en := rules.ResolveEnablement(e.Config.Config, nil, nil)
	findings, _ := rules.CheckStubs(e.Config, en)
	bySym := map[string][]report.Finding{}
	for _, f := range findings {
		bySym[f.Symbol] = append(bySym[f.Symbol], f)
	}
	var out []report.TargetResult
	if len(syms) == 0 {
		// whole program: fail if any stub
		for _, f := range findings {
			out = append(out, report.TargetResult{
				Symbol: f.Symbol, Status: "fail", Message: f.Message,
				Location: &f.Location,
			})
		}
		if len(out) == 0 {
			out = append(out, report.TargetResult{Symbol: "(all)", Status: "pass", Message: "no placeholders"})
		}
		return out
	}
	for _, sym := range syms {
		fs := bySym[sym.Qualified]
		if len(fs) == 0 {
			out = append(out, report.TargetResult{Symbol: sym.Qualified, Status: "pass", Location: locOf(sym, e.RootDir)})
		} else {
			out = append(out, report.TargetResult{
				Symbol: sym.Qualified, Status: "fail", Message: fs[0].Message, Location: locOf(sym, e.RootDir),
			})
		}
	}
	return out
}

func (e *Engine) kindParamUsed(c Check) []report.TargetResult {
	syms, _ := e.resolveTarget(c)
	if len(syms) == 0 {
		return []report.TargetResult{{Status: "unresolved", Message: "selector matched nothing"}}
	}
	en := rules.ResolveEnablement(e.Config.Config, nil, nil)
	findings := rules.CheckCoherence(e.Config, en, nil)
	bad := map[string]bool{}
	for _, f := range findings {
		if strings.HasPrefix(f.RuleID, "COH") {
			bad[f.Symbol] = true
		}
	}
	var out []report.TargetResult
	for _, sym := range syms {
		if bad[sym.Qualified] {
			out = append(out, report.TargetResult{Symbol: sym.Qualified, Status: "fail", Message: "parameter unused or no effect", Location: locOf(sym, e.RootDir)})
		} else {
			out = append(out, report.TargetResult{Symbol: sym.Qualified, Status: "pass", Location: locOf(sym, e.RootDir)})
		}
	}
	return out
}

func (e *Engine) kindReachable(c Check, wantReachable bool) []report.TargetResult {
	syms, _ := e.resolveTarget(c)
	if len(syms) == 0 {
		return []report.TargetResult{{Status: "unresolved", Message: "selector matched nothing"}}
	}
	var out []report.TargetResult
	for _, sym := range syms {
		live := e.Reach.IsLive(sym)
		pass := live == wantReachable
		status := "pass"
		msg := ""
		if !pass {
			status = "fail"
			if wantReachable {
				msg = "not reachable from configured roots"
			} else {
				msg = "reachable from configured roots"
			}
		}
		out = append(out, report.TargetResult{Symbol: sym.Qualified, Status: status, Message: msg, Location: locOf(sym, e.RootDir)})
	}
	return out
}

func (e *Engine) kindCalls(c Check, wantCalls bool) []report.TargetResult {
	syms, _ := e.resolveTarget(c)
	if len(syms) == 0 {
		return []report.TargetResult{{Status: "unresolved", Message: "selector matched nothing"}}
	}
	calleeRaw, _ := c.Params["callee"]
	calleeSel, err := ParseSelector(calleeRaw)
	if err != nil || (calleeSel.Symbol == "" && calleeSel.Function == "") {
		return []report.TargetResult{{Status: "error", Message: "params.callee is required"}}
	}
	callees := Resolve(e.Index, calleeSel)
	external := ""
	if s, ok := calleeRaw.(string); ok && strings.Contains(s, ".") && len(callees) == 0 {
		external = s
	}
	var out []report.TargetResult
	for _, sym := range syms {
		called := false
		evidence := []report.Evidence{}
		if external != "" {
			called = bodyContainsCall(sym, external)
		} else {
			for _, cal := range callees {
				if bodyContainsCall(sym, cal.Name) || bodyContainsCall(sym, cal.Qualified) {
					called = true
					evidence = append(evidence, report.Evidence{Message: "calls " + cal.Qualified})
					break
				}
				if e.Reach.CallGraph != nil && sym.SSAFunc != nil && cal.SSAFunc != nil {
					path := callgraph.ShortestPath(e.Reach, cal)
					_ = path
					// Approximate: RTA edge from sym.
					if node := e.Reach.CallGraph.Nodes[sym.SSAFunc]; node != nil {
						for _, edge := range node.Out {
							if edge.Callee != nil && edge.Callee.Func == cal.SSAFunc {
								called = true
								evidence = append(evidence, report.Evidence{Message: "calls " + cal.Qualified})
								break
							}
						}
					}
				}
			}
		}
		pass := called == wantCalls
		status := "pass"
		msg := ""
		if !pass {
			status = "fail"
			if wantCalls {
				msg = "does not call callee"
			} else {
				msg = "calls callee"
			}
		}
		out = append(out, report.TargetResult{Symbol: sym.Qualified, Status: status, Message: msg, Location: locOf(sym, e.RootDir), Evidence: evidence})
	}
	return out
}

func bodyContainsCall(sym *callgraph.Symbol, name string) bool {
	found := false
	short := name
	if i := strings.LastIndex(name, "."); i >= 0 {
		short = name[i+1:]
	}
	ast.Inspect(sym.File, func(n ast.Node) bool {
		fd, ok := n.(*ast.FuncDecl)
		if !ok || fd.Name == nil || fd.Name.Name != sym.Name || fd.Body == nil {
			return true
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch f := call.Fun.(type) {
			case *ast.Ident:
				if f.Name == short || f.Name == name {
					found = true
				}
			case *ast.SelectorExpr:
				if f.Sel.Name == short {
					found = true
				}
			}
			return !found
		})
		return false
	})
	return found
}

func (e *Engine) kindReturnsNonzero(c Check) []report.TargetResult {
	syms, _ := e.resolveTarget(c)
	if len(syms) == 0 {
		return []report.TargetResult{{Status: "unresolved", Message: "selector matched nothing"}}
	}
	resIdx := 0
	if v, ok := c.Params["result"].(float64); ok {
		resIdx = int(v)
	}
	var out []report.TargetResult
	for _, sym := range syms {
		if sym.SSAFunc == nil {
			out = append(out, report.TargetResult{Symbol: sym.Qualified, Status: "unresolved", Message: "no SSA", Location: locOf(sym, e.RootDir)})
			continue
		}
		zero := false
		for _, b := range sym.SSAFunc.Blocks {
			if len(b.Instrs) == 0 {
				continue
			}
			ret, ok := b.Instrs[len(b.Instrs)-1].(*ssa.Return)
			if !ok || resIdx >= len(ret.Results) {
				continue
			}
			if cnst, ok := ret.Results[resIdx].(*ssa.Const); ok {
				if isZeroConst(cnst) {
					zero = true
					break
				}
			}
		}
		status := "pass"
		msg := ""
		if zero {
			status = "fail"
			msg = fmt.Sprintf("result %d is constant zero on a return path", resIdx)
		}
		out = append(out, report.TargetResult{Symbol: sym.Qualified, Status: status, Message: msg, Location: locOf(sym, e.RootDir)})
	}
	return out
}

func isZeroConst(c *ssa.Const) bool {
	s := c.String()
	return s == "0" || s == `""` || s == "nil" || s == "false" || c.IsNil()
}

func (e *Engine) kindPattern(c Check) []report.TargetResult {
	syms, _ := e.resolveTarget(c)
	if len(syms) == 0 {
		return []report.TargetResult{{Status: "unresolved", Message: "selector matched nothing"}}
	}
	op, _ := c.Params["op"].(string)
	var out []report.TargetResult
	for _, sym := range syms {
		switch op {
		case "contains_call", "not_contains_call":
			callee, _ := c.Params["callee"].(string)
			has := bodyContainsCall(sym, callee)
			pass := has
			if op == "not_contains_call" {
				pass = !has
			}
			status := "pass"
			msg := ""
			if !pass {
				status = "fail"
			}
			out = append(out, report.TargetResult{Symbol: sym.Qualified, Status: status, Message: msg, Location: locOf(sym, e.RootDir)})
		case "all_returns_nonnil":
			ok := allReturnsNonNil(sym)
			status := "pass"
			msg := ""
			if !ok {
				status = "fail"
				msg = "a return path yields nil"
			}
			out = append(out, report.TargetResult{Symbol: sym.Qualified, Status: status, Message: msg, Location: locOf(sym, e.RootDir)})
		default:
			out = append(out, report.TargetResult{Symbol: sym.Qualified, Status: "error", Message: "unknown pattern op"})
		}
	}
	return out
}

func allReturnsNonNil(sym *callgraph.Symbol) bool {
	ok := true
	ast.Inspect(sym.File, func(n ast.Node) bool {
		fd, okfd := n.(*ast.FuncDecl)
		if !okfd || fd.Name == nil || fd.Name.Name != sym.Name || fd.Body == nil {
			return true
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			ret, isRet := n.(*ast.ReturnStmt)
			if !isRet {
				return true
			}
			for _, r := range ret.Results {
				if id, isID := r.(*ast.Ident); isID && id.Name == "nil" {
					ok = false
				}
			}
			return true
		})
		return false
	})
	return ok
}

func (e *Engine) kindReturnsErrorOnInvalid(c Check) []report.TargetResult {
	syms, _ := e.resolveTarget(c)
	if len(syms) == 0 {
		return []report.TargetResult{{Status: "unresolved", Message: "selector matched nothing"}}
	}
	onNoGuard := "fail"
	if v, ok := c.Params["on_no_guard"].(string); ok {
		onNoGuard = v
	}
	var out []report.TargetResult
	for _, sym := range syms {
		status, msg, ev := analyzeErrorOnInvalid(sym)
		if status == "unresolved" && msg == "no validation-failure branch found" {
			status = onNoGuard
			if onNoGuard == "fail" {
				msg = "no validation-failure branch found (an if on an input-derived failure condition ending in return)"
			}
		}
		out = append(out, report.TargetResult{Symbol: sym.Qualified, Status: status, Message: msg, Location: locOf(sym, e.RootDir), Evidence: ev})
	}
	return out
}

func analyzeErrorOnInvalid(sym *callgraph.Symbol) (status, msg string, ev []report.Evidence) {
	fd := findFunc(sym)
	if fd == nil || fd.Body == nil {
		return "unresolved", "no function body", nil
	}
	params := map[string]bool{}
	if fd.Type.Params != nil {
		for _, f := range fd.Type.Params.List {
			for _, n := range f.Names {
				if n.Name != "_" {
					params[n.Name] = true
				}
			}
		}
	}
	type branch struct {
		guard string
		nilErr bool
		pos   token.Pos
	}
	var branches []branch
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		if !condMentions(ifs.Cond, params) || !failureShaped(ifs.Cond) {
			return true
		}
		// Block ends in return?
		if ifs.Body == nil || len(ifs.Body.List) == 0 {
			return true
		}
		last := ifs.Body.List[len(ifs.Body.List)-1]
		ret, ok := last.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		b := branch{guard: exprString(ifs.Cond), pos: ifs.Pos()}
		if len(ret.Results) > 0 {
			lastRes := ret.Results[len(ret.Results)-1]
			if id, ok := lastRes.(*ast.Ident); ok && id.Name == "nil" {
				b.nilErr = true
			}
		}
		branches = append(branches, b)
		return true
	})
	if len(branches) == 0 {
		return "unresolved", "no validation-failure branch found", nil
	}
	failN := 0
	for _, b := range branches {
		if b.nilErr {
			failN++
			ev = append(ev, report.Evidence{Kind: "offending_return", Message: "returns nil as the error (nil literal)"})
			ev = append(ev, report.Evidence{Kind: "guard", Message: "guard: if " + b.guard})
		}
	}
	if failN > 0 {
		return "fail", fmt.Sprintf("%d of %d validation-failure branch(es) return a nil error", failN, len(branches)), ev
	}
	return "pass", "", nil
}

func findFunc(sym *callgraph.Symbol) *ast.FuncDecl {
	var found *ast.FuncDecl
	ast.Inspect(sym.File, func(n ast.Node) bool {
		fd, ok := n.(*ast.FuncDecl)
		if !ok || fd.Name == nil || fd.Name.Name != sym.Name {
			return true
		}
		if sym.Kind == callgraph.KindMethod {
			if fd.Recv == nil {
				return true
			}
			rn := recvTypeName(fd.Recv)
			if rn != "" && rn != sym.Recv {
				return true
			}
		} else if fd.Recv != nil {
			return true
		}
		found = fd
		return false
	})
	return found
}

func recvTypeName(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	switch t := fl.List[0].Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

func condMentions(e ast.Expr, params map[string]bool) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && params[id.Name] {
			found = true
		}
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && params[id.Name] {
				found = true
			}
		}
		return true
	})
	return found
}

func failureShaped(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.UnaryExpr:
		if v.Op == token.NOT {
			return true
		}
	case *ast.BinaryExpr:
		switch v.Op {
		case token.EQL, token.NEQ, token.LSS, token.GTR, token.LEQ, token.GEQ:
			return true
		}
	case *ast.CallExpr:
		s := strings.ToLower(exprString(e))
		if strings.Contains(s, "invalid") || strings.Contains(s, "missing") {
			return true
		}
	}
	s := strings.ToLower(exprString(e))
	return strings.Contains(s, "invalid") || strings.Contains(s, "missing")
}

func exprString(e ast.Expr) string {
	var b strings.Builder
	ast.Inspect(e, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Ident:
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			b.WriteString(v.Name)
		case *ast.BasicLit:
			b.WriteString(v.Value)
		}
		return true
	})
	return b.String()
}

func (e *Engine) kindDataflowReaches(c Check) []report.TargetResult {
	fromRaw := c.Params["from"]
	toRaw := c.Params["to"]
	fromSel, err1 := ParseSelector(fromRaw)
	toSel, err2 := ParseSelector(toRaw)
	if err1 != nil || err2 != nil || fromSel.Symbol == "" || toSel.Symbol == "" {
		return []report.TargetResult{{Status: "error", Message: "params.from and params.to are required"}}
	}
	froms := Resolve(e.Index, fromSel)
	tos := Resolve(e.Index, toSel)
	if len(froms) == 0 || len(tos) == 0 {
		return []report.TargetResult{{Status: "unresolved", Message: "from/to selector matched nothing"}}
	}
	fromResult := 0
	if v, ok := c.Params["from_result"].(float64); ok {
		fromResult = int(v)
	}
	toArg := -1
	if v, ok := c.Params["to_arg"].(float64); ok {
		toArg = int(v)
	}

	// Scope: callers of from, or target selector.
	var callers []*callgraph.Symbol
	if c.Target != nil {
		callers, _ = e.resolveTarget(c)
	} else {
		for _, sym := range e.Index.Symbols {
			if sym.Kind != callgraph.KindFunc && sym.Kind != callgraph.KindMethod {
				continue
			}
			for _, f := range froms {
				if bodyContainsCall(sym, f.Name) {
					callers = append(callers, sym)
					break
				}
			}
		}
	}
	if len(callers) == 0 {
		return []report.TargetResult{{Status: "unresolved", Message: "no caller sites found"}}
	}

	maxDepth := 8
	if v, ok := c.Params["max_depth"].(float64); ok {
		maxDepth = int(v)
	}
	var out []report.TargetResult
	for _, caller := range callers {
		ok, drops := false, []rules.DropReason{}
		if caller.SSAFunc != nil && froms[0].SSAFunc != nil && tos[0].SSAFunc != nil {
			ok, drops = rules.DataflowReaches(caller.SSAFunc, froms[0].SSAFunc, tos[0].SSAFunc, fromResult, toArg, maxDepth)
		} else {
			drops = []rules.DropReason{{Message: "dropped: missing SSA"}}
		}
		status := "pass"
		msg := ""
		ev := []report.Evidence{}
		if !ok {
			status = "fail"
			for _, d := range drops {
				ev = append(ev, report.Evidence{Kind: "dropped", Message: d.Message})
			}
			if len(drops) > 0 {
				msg = drops[0].Message
			}
		}
		out = append(out, report.TargetResult{Symbol: caller.Qualified, Status: status, Message: msg, Location: locOf(caller, e.RootDir), Evidence: ev})
	}
	return out
}

var _ = types.Universe
