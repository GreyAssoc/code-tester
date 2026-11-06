package callgraph

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"
)

func Roots(idx *Index, enabled map[string]bool) []*Symbol {
	var roots []*Symbol
	seen := map[*Symbol]bool{}
	add := func(s *Symbol) {
		if s == nil || seen[s] || s.IsBlank() {
			return
		}
		seen[s] = true
		roots = append(roots, s)
	}

	for _, sym := range idx.Symbols {
		switch sym.Kind {
		case KindFunc:
			name := sym.Name
			pkgName := ""
			if sym.Pkg != nil {
				pkgName = sym.Pkg.Name
			}
			if enabled["main"] && name == "main" && pkgName == "main" {
				add(sym)
			}
			if enabled["init"] && name == "init" {
				add(sym)
			}
			if enabled["exported"] && sym.Exported && pkgName != "main" {
				add(sym)
			}
			if sym.IsTest {
				if enabled["tests"] && (strings.HasPrefix(name, "Test") || name == "TestMain") {
					add(sym)
				}
				if enabled["benchmarks"] && strings.HasPrefix(name, "Benchmark") {
					add(sym)
				}
				if enabled["fuzz"] && strings.HasPrefix(name, "Fuzz") {
					add(sym)
				}
				if enabled["examples"] && strings.HasPrefix(name, "Example") {
					add(sym)
				}
			}
		case KindMethod:
			if enabled["exported"] && sym.Exported {
				if sym.Pkg != nil && sym.Pkg.Name != "main" {
					add(sym)
				}
			}
		}

		if hasLinknameOrExport(sym) {
			add(sym)
		}
	}

	// Seed SSA package init (covers var initializer code) without marking every var live.
	if enabled["init"] {
		for _, fn := range packageInits(idx) {
			if sym := idx.BySSA[fn]; sym != nil {
				add(sym)
			} else {
				idx.extraRootFns = append(idx.extraRootFns, fn)
			}
		}
		// Initializers always run: symbols referenced from package-level var/const
		// initializers are live even when the var itself is unused.
		for _, sym := range idx.Symbols {
			if sym.Kind != KindVar && sym.Kind != KindConst {
				continue
			}
			for ref := range sym.Refs {
				add(ref)
			}
		}
	}

	// Compile-time interface assertions: var _ I = T{}
	for _, pkg := range idx.Packages {
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok {
					continue
				}
				for _, sp := range gd.Specs {
					vs, ok := sp.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range vs.Names {
						if name.Name != "_" {
							continue
						}
						if vs.Type != nil {
							if obj := pkg.TypesInfo.Types[vs.Type].Type; obj != nil {
								seedType(idx, add, obj)
							}
						}
						for _, v := range vs.Values {
							if tv, ok := pkg.TypesInfo.Types[v]; ok {
								seedType(idx, add, tv.Type)
							}
						}
					}
				}
			}
		}
	}
	return roots
}

func packageInits(idx *Index) []*ssa.Function {
	var out []*ssa.Function
	if idx.Prog == nil {
		return out
	}
	for _, sp := range idx.Prog.AllPackages() {
		if sp == nil {
			continue
		}
		if init := sp.Func("init"); init != nil {
			out = append(out, init)
		}
	}
	return out
}

func seedType(idx *Index, add func(*Symbol), t types.Type) {
	switch t := t.(type) {
	case *types.Named:
		if sym, ok := idx.ByObj[t.Obj()]; ok {
			add(sym)
		}
	case *types.Pointer:
		seedType(idx, add, t.Elem())
	}
}

func hasLinknameOrExport(sym *Symbol) bool {
	if sym.File == nil {
		return false
	}
	docComments := []*ast.CommentGroup{}
	ast.Inspect(sym.File, func(n ast.Node) bool {
		fd, ok := n.(*ast.FuncDecl)
		if !ok || fd.Name == nil || fd.Name.Name != sym.Name {
			return true
		}
		if fd.Doc != nil {
			docComments = append(docComments, fd.Doc)
		}
		return false
	})
	for _, cg := range append(sym.File.Comments, docComments...) {
		if cg == nil {
			continue
		}
		for _, c := range cg.List {
			t := c.Text
			if strings.Contains(t, "//go:linkname "+sym.Name) || strings.HasPrefix(strings.TrimPrefix(t, "//"), "export "+sym.Name) {
				return true
			}
		}
	}
	return false
}
