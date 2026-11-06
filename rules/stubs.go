package rules

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"gointegrity/callgraph"
	"gointegrity/report"
)

func CheckStubs(ctx Context, en Enablement) ([]Finding, map[string]bool) {
	var out []Finding
	stubbed := map[string]bool{}
	idx := ctx.Index

	for _, sym := range idx.Symbols {
		if sym.Kind != callgraph.KindFunc && sym.Kind != callgraph.KindMethod {
			continue
		}
		if sym.Generated {
			continue
		}
		if sym.IsTest && !ctx.Config.Stubs.IncludeTests {
			continue
		}
		fd := funcDecl(sym)
		if fd == nil || fd.Body == nil {
			continue
		}
		if isNoopMarked(fd, sym) && !ctx.Config.Stubs.Strict {
			continue
		}
		if isMarkerMethod(fd, sym) {
			continue
		}
		if isZeroStateConstructor(fd, sym) && !ctx.Config.Stubs.Strict {
			continue
		}
		if isDeprecated(fd) && !ctx.Config.Stubs.Strict {
			continue
		}
		if sym.AddressTaken && !ctx.Config.Stubs.Strict {
			continue
		}
		if !ctx.Config.Stubs.Strict {
			if isNoopFile(fileNameOf(sym)) || hasPlatformVariant(ctx, sym) {
				continue
			}
			if isInterfaceHook(ctx, sym, fd) {
				continue
			}
			if matchesNamedFuncType(ctx, sym) {
				continue
			}
		}

		file, line, col := sym.Location(ctx.RootDir)
		paramsIgnored := allParamsIgnored(fd)

		if en.Enabled["STUB005"] {
			if msg, ok := panicNotImplemented(fd); ok {
				f := finding("STUB005", "panic-not-implemented", "placeholder", en.Severity["STUB005"],
					report.ConfidenceHigh, msg, sym.Qualified, file, line, col)
				out = append(out, f)
				stubbed[sym.Qualified] = true
				continue
			}
		}
		if en.Enabled["STUB006"] {
			if todoOnlyBody(fd, sym.File) {
				f := finding("STUB006", "todo-only-body", "placeholder", en.Severity["STUB006"],
					report.ConfidenceHigh, "function body contains only a TODO/FIXME/XXX comment",
					sym.Qualified, file, line, col)
				out = append(out, f)
				stubbed[sym.Qualified] = true
				continue
			}
		}
		if en.Enabled["STUB007"] {
			if msg, ok := notImplementedError(fd); ok {
				f := finding("STUB007", "not-implemented-error", "placeholder", en.Severity["STUB007"],
					report.ConfidenceHigh, msg, sym.Qualified, file, line, col)
				out = append(out, f)
				stubbed[sym.Qualified] = true
				continue
			}
		}
		if en.Enabled["STUB001"] && emptyBody(fd) {
			f := finding("STUB001", "empty-body", "placeholder", en.Severity["STUB001"],
				report.ConfidenceMedium, "function body is empty",
				sym.Qualified, file, line, col)
			out = append(out, f)
			stubbed[sym.Qualified] = true
			continue
		}
		if kind, conf, ok := classifyReturnStub(fd, paramsIgnored); ok {
			id, name := "", ""
			switch kind {
			case 2:
				id, name = "STUB002", "zero-value-return"
			case 3:
				id, name = "STUB003", "empty-struct-return"
			case 4:
				id, name = "STUB004", "hardcoded-literal-return"
			}
			if id != "" && en.Enabled[id] {
				f := finding(id, name, "placeholder", en.Severity[id], conf,
					"function body looks like a placeholder ("+name+")",
					sym.Qualified, file, line, col)
				out = append(out, f)
				stubbed[sym.Qualified] = true
			}
		}
	}
	return out, stubbed
}

func funcDecl(sym *callgraph.Symbol) *ast.FuncDecl {
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
			// Match receiver type name.
			rn := recvName(fd.Recv)
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

func recvName(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	t := fl.List[0].Type
	switch v := t.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		if id, ok := v.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

func emptyBody(fd *ast.FuncDecl) bool {
	if fd.Body == nil {
		return false
	}
	for _, s := range fd.Body.List {
		if _, ok := s.(*ast.EmptyStmt); ok {
			continue
		}
		return false
	}
	return true
}

func todoOnlyBody(fd *ast.FuncDecl, file *ast.File) bool {
	if fd.Body == nil || len(fd.Body.List) > 0 {
		return false
	}
	hasTODO := false
	check := func(t string) {
		u := strings.ToUpper(t)
		if strings.Contains(u, "TODO") || strings.Contains(u, "FIXME") || strings.Contains(u, "XXX") {
			hasTODO = true
		}
	}
	if fd.Doc != nil {
		check(fd.Doc.Text())
	}
	lo, hi := fd.Body.Lbrace, fd.Body.Rbrace
	for _, cg := range file.Comments {
		if cg.Pos() > lo && cg.End() < hi {
			check(cg.Text())
		}
	}
	return hasTODO
}

func panicNotImplemented(fd *ast.FuncDecl) (string, bool) {
	found := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || id.Name != "panic" || len(call.Args) == 0 {
			return true
		}
		if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			s := strings.ToLower(strings.Trim(lit.Value, `"`))
			if isNotImplMsg(s) {
				found = true
				return false
			}
		}
		return true
	})
	if found {
		return "function panics with a not-implemented message", true
	}
	return "", false
}

func notImplementedError(fd *ast.FuncDecl) (string, bool) {
	found := false
	medium := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, r := range ret.Results {
			switch e := r.(type) {
			case *ast.CallExpr:
				name := callName(e)
				if name == "errors.New" || name == "fmt.Errorf" || strings.HasSuffix(name, "Error") || strings.HasSuffix(name, "Errorf") {
					if len(e.Args) > 0 {
						if lit, ok := e.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							if isNotImplMsg(strings.ToLower(strings.Trim(lit.Value, `"`))) {
								found = true
								return false
							}
						}
					}
				}
			case *ast.Ident:
				if strings.Contains(strings.ToLower(e.Name), "notimplemented") || e.Name == "ErrNotImplemented" {
					found = true
					medium = true
					return false
				}
			}
		}
		return true
	})
	if found {
		msg := "function returns a not-implemented error"
		_ = medium
		return msg, true
	}
	return "", false
}

func callName(c *ast.CallExpr) string {
	switch f := c.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		if id, ok := f.X.(*ast.Ident); ok {
			return id.Name + "." + f.Sel.Name
		}
		return f.Sel.Name
	}
	return ""
}

func isNotImplMsg(s string) bool {
	for _, p := range []string{"not implemented", "notimplemented", "unimplemented", "implement me", "todo"} {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func classifyReturnStub(fd *ast.FuncDecl, paramsIgnored bool) (kind int, conf report.Confidence, ok bool) {
	if fd.Body == nil || len(fd.Body.List) != 1 {
		return 0, "", false
	}
	ret, ok := fd.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) == 0 {
		return 0, "", false
	}
	allZero := true
	allEmptyStruct := true
	allLit := true
	hasPlaceholderStr := false
	for _, r := range ret.Results {
		if !isZeroExpr(r) {
			allZero = false
		}
		if !isEmptyStruct(r) && !isZeroExpr(r) {
			allEmptyStruct = false
		}
		if lit, isLit := r.(*ast.BasicLit); isLit {
			if lit.Kind == token.STRING {
				v := strings.ToLower(strings.Trim(lit.Value, `"`))
				if v == "todo" || strings.HasPrefix(v, "todo ") || v == "not implemented" {
					hasPlaceholderStr = true
				}
			}
		} else if !isZeroExpr(r) && !isEmptyStruct(r) {
			allLit = false
		}
	}
	conf = report.ConfidenceMedium
	if paramsIgnored || hasPlaceholderStr {
		conf = report.ConfidenceHigh
	}
	if allEmptyStruct && !allZero {
		return 3, conf, true
	}
	if allZero {
		return 2, conf, true
	}
	if (allLit || hasPlaceholderStr) && (paramsIgnored || hasPlaceholderStr) {
		return 4, conf, true
	}
	return 0, "", false
}

func isZeroExpr(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name == "nil" || v.Name == "false"
	case *ast.BasicLit:
		return v.Value == "0" || v.Value == `""` || v.Value == "``"
	}
	return false
}

func isEmptyStruct(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.CompositeLit:
		return len(v.Elts) == 0
	case *ast.UnaryExpr:
		if v.Op == token.AND {
			return isEmptyStruct(v.X)
		}
	}
	return false
}

func allParamsIgnored(fd *ast.FuncDecl) bool {
	if fd.Type.Params == nil {
		return false
	}
	names := []string{}
	for _, f := range fd.Type.Params.List {
		for _, n := range f.Names {
			if n.Name != "" && n.Name != "_" {
				names = append(names, n.Name)
			}
		}
	}
	if len(names) == 0 {
		return false
	}
	used := map[string]bool{}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if ok {
			used[id.Name] = true
		}
		return true
	})
	for _, n := range names {
		if used[n] {
			return false
		}
	}
	return true
}

func isNoopMarked(fd *ast.FuncDecl, sym *callgraph.Symbol) bool {
	texts := []string{}
	if fd.Doc != nil {
		texts = append(texts, strings.ToLower(fd.Doc.Text()))
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if cg, ok := n.(*ast.CommentGroup); ok {
			texts = append(texts, strings.ToLower(cg.Text()))
		}
		return true
	})
	markers := []string{"no-op", "noop", "nop", "intentionally", "deliberately", "by design",
		"does nothing", "do nothing", "nothing to do", "not needed", "gointegrity:noop"}
	for _, t := range texts {
		for _, m := range markers {
			if strings.Contains(t, m) {
				return true
			}
		}
	}
	recv := strings.ToLower(sym.Recv)
	for _, m := range []string{"noop", "nop", "null", "discard", "dummy", "fake", "mock", "stub", "unimplemented"} {
		if strings.Contains(recv, m) {
			return true
		}
	}
	return false
}

func isMarkerMethod(fd *ast.FuncDecl, sym *callgraph.Symbol) bool {
	if sym.Kind != callgraph.KindMethod || sym.Exported {
		return false
	}
	if fd.Type.Params != nil && fieldCount(fd.Type.Params) > 0 {
		return false
	}
	if fd.Type.Results != nil && fieldCount(fd.Type.Results) > 0 {
		return false
	}
	return emptyBody(fd)
}

func fieldCount(fl *ast.FieldList) int {
	if fl == nil {
		return 0
	}
	n := 0
	for _, f := range fl.List {
		if len(f.Names) == 0 {
			n++
		} else {
			n += len(f.Names)
		}
	}
	return n
}

func isZeroStateConstructor(fd *ast.FuncDecl, sym *callgraph.Symbol) bool {
	if sym.Kind != callgraph.KindFunc {
		return false
	}
	name := sym.Name
	okPrefix := strings.HasPrefix(name, "New") || strings.HasPrefix(name, "Default") ||
		strings.HasPrefix(name, "Empty") || strings.HasPrefix(name, "Zero")
	if !okPrefix {
		return false
	}
	return fieldCount(fd.Type.Params) == 0
}

func isDeprecated(fd *ast.FuncDecl) bool {
	if fd.Doc == nil {
		return false
	}
	for _, line := range strings.Split(fd.Doc.Text(), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Deprecated") {
			return true
		}
	}
	return false
}

func fileNameOf(sym *callgraph.Symbol) string {
	if sym.Pkg == nil || !sym.Pos.IsValid() {
		return ""
	}
	return sym.Pkg.Fset.Position(sym.Pos).Filename
}

func isNoopFile(filename string) bool {
	base := filename
	if i := strings.LastIndex(filename, "/"); i >= 0 {
		base = filename[i+1:]
	}
	base = strings.ToLower(base)
	if base == "dummy.go" || base == "noop.go" {
		return true
	}
	if strings.HasPrefix(base, "fake_") || strings.HasSuffix(base, "_mock.go") {
		return true
	}
	return false
}

func hasPlatformVariant(ctx Context, sym *callgraph.Symbol) bool {
	// Same package declares another function with the same name in a different file
	// that is not in CompiledGoFiles (build-tagged out) — approximate via file name
	// suffixes like _windows.go, _linux.go, _darwin.go, _js.go.
	name := sym.Name
	pkg := sym.Pkg
	if pkg == nil {
		return false
	}
	mine := fileNameOf(sym)
	for _, other := range ctx.Index.Symbols {
		if other.PkgPath != sym.PkgPath || other.Name != name || other == sym {
			continue
		}
		if sym.Kind == callgraph.KindMethod && other.Recv != sym.Recv {
			continue
		}
		of := fileNameOf(other)
		if of != "" && of != mine && platformSuffix(of) != platformSuffix(mine) {
			return true
		}
	}
	_ = pkg
	return false
}

func platformSuffix(filename string) string {
	base := filename
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(base, ".go")
	for _, s := range []string{"_windows", "_linux", "_darwin", "_freebsd", "_js", "_android", "_ios", "_unix"} {
		if strings.HasSuffix(base, s) {
			return s
		}
	}
	return ""
}

func isInterfaceHook(ctx Context, sym *callgraph.Symbol, fd *ast.FuncDecl) bool {
	if sym.Kind != callgraph.KindMethod {
		return false
	}
	if !matchesAnyInterface(ctx.Index, sym) {
		return false
	}
	// Empty body (no results) or returns constant bool/number/string capability flags.
	if emptyBody(fd) && fieldCount(fd.Type.Results) == 0 {
		return true
	}
	if len(fd.Body.List) == 1 {
		ret, ok := fd.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return false
		}
		switch r := ret.Results[0].(type) {
		case *ast.Ident:
			if r.Name == "true" || r.Name == "false" {
				return true
			}
		case *ast.BasicLit:
			if r.Kind == token.INT || r.Kind == token.STRING || r.Kind == token.FLOAT {
				return true
			}
		}
		// return nil / nil,nil from interface method still reported — not a hook exclusion.
	}
	return false
}

func matchesNamedFuncType(ctx Context, sym *callgraph.Symbol) bool {
	fn, ok := sym.Obj.(*types.Func)
	if !ok {
		return false
	}
	sig, _ := fn.Type().(*types.Signature)
	if sig == nil || sig.Recv() != nil {
		return false
	}
	for _, other := range ctx.Index.Symbols {
		if other.Kind != callgraph.KindType || other.PkgPath != sym.PkgPath {
			continue
		}
		tn, ok := other.Obj.(*types.TypeName)
		if !ok {
			continue
		}
		named, ok := tn.Type().(*types.Named)
		if !ok {
			continue
		}
		ft, ok := named.Underlying().(*types.Signature)
		if !ok {
			continue
		}
		if types.Identical(sig, ft) {
			return true
		}
	}
	return false
}
