package callgraph

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"

	"gointegrity/loader"
)

type Kind int

const (
	KindFunc Kind = iota
	KindMethod
	KindType
	KindConst
	KindVar
)

type Symbol struct {
	Obj       types.Object
	Kind      Kind
	PkgPath   string
	Name      string
	Recv      string // base type name for methods
	Qualified string
	Pos       token.Pos
	File      *ast.File
	Pkg       *packages.Package
	SSAFunc   *ssa.Function
	Exported  bool
	IsTest    bool
	Generated bool
	AddressTaken bool

	Refs       map[*Symbol]bool
	Ifaces     []*types.Named // named interfaces mentioned
}

type Index struct {
	Fset     *token.FileSet
	RootDir  string
	Symbols  []*Symbol
	ByObj    map[types.Object]*Symbol
	ByQual   map[string]*Symbol
	BySSA    map[*ssa.Function]*Symbol
	Ifaces   []*types.Named
	Prog     *ssa.Program
	Packages []*packages.Package

	// SSA-only roots such as package init with no types.Object.
	extraRootFns []*ssa.Function
}

func Build(res *loader.Result, rootDir string) *Index {
	idx := &Index{
		Fset:     res.Fset,
		RootDir:  rootDir,
		ByObj:    map[types.Object]*Symbol{},
		ByQual:   map[string]*Symbol{},
		BySSA:    map[*ssa.Function]*Symbol{},
		Prog:     res.Prog,
		Packages: res.Primary,
	}

	for _, pkg := range res.Primary {
		if pkg.Types == nil {
			continue
		}
		fileOf := indexFiles(pkg)
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if obj == nil {
				continue
			}
			if _, ok := idx.ByObj[obj]; ok {
				continue
			}
			file, gen, isTest := fileOf(obj.Pos())
			sym := symbolFromObj(obj, pkg, file, gen, isTest)
			if sym == nil {
				continue
			}
			idx.add(sym)
		}
		// Methods on named types in this package.
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			tn, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			if !ok {
				continue
			}
			for i := 0; i < named.NumMethods(); i++ {
				m := named.Method(i)
				if _, ok := idx.ByObj[m]; ok {
					continue
				}
				file, gen, isTest := fileOf(m.Pos())
				sym := symbolFromObj(m, pkg, file, gen, isTest)
				if sym == nil {
					continue
				}
				idx.add(sym)
			}
			if _, ok := named.Underlying().(*types.Interface); ok {
				idx.Ifaces = append(idx.Ifaces, named)
			}
		}
	}

	// Attach SSA functions.
	for _, sp := range res.SSAPkgs {
		if sp == nil {
			continue
		}
		for _, m := range sp.Members {
			fn, ok := m.(*ssa.Function)
			if !ok {
				continue
			}
			attachSSA(idx, fn)
			for _, anon := range fn.AnonFuncs {
				attachSSA(idx, anon)
			}
		}
	}

	// Collect references.
	for _, sym := range idx.Symbols {
		sym.Refs = map[*Symbol]bool{}
		collectRefs(idx, sym)
	}

	return idx
}

func (idx *Index) add(sym *Symbol) {
	idx.Symbols = append(idx.Symbols, sym)
	idx.ByObj[sym.Obj] = sym
	idx.ByQual[sym.Qualified] = sym
}

func attachSSA(idx *Index, fn *ssa.Function) {
	if fn == nil || fn.Object() == nil {
		return
	}
	if sym, ok := idx.ByObj[fn.Object()]; ok {
		sym.SSAFunc = fn
		idx.BySSA[fn] = sym
	}
}

func symbolFromObj(obj types.Object, pkg *packages.Package, file *ast.File, gen, isTest bool) *Symbol {
	sym := &Symbol{
		Obj:       obj,
		PkgPath:   pkg.PkgPath,
		Name:      obj.Name(),
		Pos:       obj.Pos(),
		File:      file,
		Pkg:       pkg,
		Exported:  obj.Exported(),
		IsTest:    isTest,
		Generated: gen,
	}
	switch o := obj.(type) {
	case *types.Func:
		sig, _ := o.Type().(*types.Signature)
		if sig != nil && sig.Recv() != nil {
			sym.Kind = KindMethod
			sym.Recv = recvBaseName(sig.Recv().Type())
		} else {
			sym.Kind = KindFunc
		}
		sym.Qualified = loader.ObjectString(obj)
	case *types.TypeName:
		sym.Kind = KindType
		sym.Qualified = pkg.PkgPath + "." + o.Name()
	case *types.Const:
		sym.Kind = KindConst
		sym.Qualified = pkg.PkgPath + "." + o.Name()
	case *types.Var:
		if o.Parent() != pkg.Types.Scope() {
			return nil
		}
		sym.Kind = KindVar
		sym.Qualified = pkg.PkgPath + "." + o.Name()
	default:
		return nil
	}
	return sym
}

func recvBaseName(t types.Type) string {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		return n.Obj().Name()
	}
	return types.TypeString(t, func(*types.Package) string { return "" })
}

// indexFiles returns a lookup from token.Pos → declaring file metadata.
func indexFiles(pkg *packages.Package) func(pos token.Pos) (file *ast.File, gen, isTest bool) {
	type meta struct {
		file          *ast.File
		gen, isTest   bool
		start, end    token.Pos
	}
	var metas []meta
	for i, file := range pkg.Syntax {
		if file == nil {
			continue
		}
		filename := ""
		if i < len(pkg.CompiledGoFiles) {
			filename = pkg.CompiledGoFiles[i]
		} else if pkg.Fset != nil && file.Pos().IsValid() {
			filename = pkg.Fset.Position(file.Pos()).Filename
		}
		metas = append(metas, meta{
			file: file, gen: loader.IsGenerated(file), isTest: loader.IsTestFile(filename),
			start: file.Pos(), end: file.End(),
		})
	}
	return func(pos token.Pos) (*ast.File, bool, bool) {
		if !pos.IsValid() {
			if len(metas) > 0 {
				return metas[0].file, metas[0].gen, metas[0].isTest
			}
			return nil, false, false
		}
		for _, m := range metas {
			if m.start <= pos && pos <= m.end {
				return m.file, m.gen, m.isTest
			}
		}
		// Fallback: match by filename from FileSet.
		if pkg.Fset != nil {
			want := pkg.Fset.Position(pos).Filename
			for _, m := range metas {
				if pkg.Fset.Position(m.file.Pos()).Filename == want {
					return m.file, m.gen, m.isTest
				}
			}
		}
		if len(metas) > 0 {
			return metas[0].file, metas[0].gen, metas[0].isTest
		}
		return nil, false, false
	}
}

func collectRefs(idx *Index, sym *Symbol) {
	switch sym.Kind {
	case KindFunc, KindMethod:
		fn, _ := sym.Obj.(*types.Func)
		if fn == nil {
			return
		}
		// Signature types (recv/params/results) count as references.
		if sig, _ := fn.Type().(*types.Signature); sig != nil {
			walkTypeRefs(idx, sym, sig)
		}
		body := findFuncBody(sym)
		if body == nil {
			return
		}
		ast.Inspect(body, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			obj := sym.Pkg.TypesInfo.ObjectOf(id)
			if obj == nil {
				return true
			}
			if ref, ok := idx.ByObj[obj]; ok {
				sym.Refs[ref] = true
			}
			if tn, ok := obj.(*types.TypeName); ok {
				if named, ok := tn.Type().(*types.Named); ok {
					if _, ok := named.Underlying().(*types.Interface); ok {
						sym.Ifaces = append(sym.Ifaces, named)
					}
				}
			}
			return true
		})
		// Refine address-taken: mark only non-call uses.
		for ref := range sym.Refs {
			if ref.Kind == KindFunc || ref.Kind == KindMethod {
				ref.AddressTaken = false
			}
		}
		ast.Inspect(body, func(n ast.Node) bool {
			switch e := n.(type) {
			case *ast.CallExpr:
				for _, a := range e.Args {
					markValueUse(idx, sym.Pkg, a)
				}
				if _, ok := e.Fun.(*ast.Ident); !ok {
					if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
						markValueUse(idx, sym.Pkg, sel.X)
					}
				}
				return false
			case *ast.Ident:
				markValueUse(idx, sym.Pkg, e)
			case *ast.SelectorExpr:
				markValueUse(idx, sym.Pkg, e)
			}
			return true
		})
	case KindType:
		tn := sym.Obj.(*types.TypeName)
		walkTypeRefs(idx, sym, tn.Type())
	case KindConst, KindVar:
		collectValueSpecRefs(idx, sym)
	}
}

func findFuncBody(sym *Symbol) *ast.BlockStmt {
	files := []*ast.File{}
	if sym.File != nil {
		files = append(files, sym.File)
	}
	if sym.Pkg != nil {
		for _, f := range sym.Pkg.Syntax {
			if f != nil && f != sym.File {
				files = append(files, f)
			}
		}
	}
	for _, file := range files {
		var body *ast.BlockStmt
		ast.Inspect(file, func(n ast.Node) bool {
			d, ok := n.(*ast.FuncDecl)
			if !ok || d.Name == nil || d.Name.Name != sym.Name {
				return true
			}
			if sym.Kind == KindMethod {
				if d.Recv == nil {
					return true
				}
			} else if d.Recv != nil {
				return true
			}
			// Prefer the decl whose position matches the object.
			if sym.Pos.IsValid() && d.Name.Pos().IsValid() && d.Name.Pos() != sym.Pos {
				// Still accept if this is the only match; keep scanning for exact pos.
				if body == nil {
					body = d.Body
				}
				return true
			}
			body = d.Body
			return false
		})
		if body != nil {
			return body
		}
	}
	return nil
}

func collectValueSpecRefs(idx *Index, sym *Symbol) {
	ast.Inspect(sym.File, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		match := false
		for _, name := range vs.Names {
			if name.Name == sym.Name {
				match = true
				break
			}
		}
		if !match {
			return true
		}
		for _, v := range vs.Values {
			ast.Inspect(v, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				obj := sym.Pkg.TypesInfo.ObjectOf(id)
				if obj == nil {
					return true
				}
				if ref, ok := idx.ByObj[obj]; ok {
					sym.Refs[ref] = true
				}
				return true
			})
		}
		if vs.Type != nil {
			if tv, ok := sym.Pkg.TypesInfo.Types[vs.Type]; ok {
				walkTypeRefs(idx, sym, tv.Type)
			}
		}
		return false
	})
}

func markValueUse(idx *Index, pkg *packages.Package, e ast.Expr) {
	switch v := e.(type) {
	case *ast.Ident:
		obj := pkg.TypesInfo.ObjectOf(v)
		if obj == nil {
			return
		}
		if ref, ok := idx.ByObj[obj]; ok {
			if ref.Kind == KindFunc || ref.Kind == KindMethod {
				ref.AddressTaken = true
			}
		}
	case *ast.SelectorExpr:
		obj := pkg.TypesInfo.ObjectOf(v.Sel)
		if obj == nil {
			return
		}
		if ref, ok := idx.ByObj[obj]; ok {
			if ref.Kind == KindFunc || ref.Kind == KindMethod {
				ref.AddressTaken = true
			}
		}
	}
}

func walkTypeRefs(idx *Index, sym *Symbol, t types.Type) {
	walkTypeRefsSeen(idx, sym, t, map[types.Type]bool{})
}

func walkTypeRefsSeen(idx *Index, sym *Symbol, t types.Type, seen map[types.Type]bool) {
	if t == nil || seen[t] {
		return
	}
	seen[t] = true
	switch t := t.(type) {
	case *types.Named:
		if obj := t.Obj(); obj != nil {
			if ref, ok := idx.ByObj[obj]; ok {
				sym.Refs[ref] = true
			}
		}
		if _, ok := t.Underlying().(*types.Interface); ok {
			sym.Ifaces = append(sym.Ifaces, t)
		}
		// Do not recurse into underlying for interfaces — method sigs cycle via Named.
		if _, ok := t.Underlying().(*types.Interface); ok {
			return
		}
		walkTypeRefsSeen(idx, sym, t.Underlying(), seen)
	case *types.Pointer:
		walkTypeRefsSeen(idx, sym, t.Elem(), seen)
	case *types.Slice:
		walkTypeRefsSeen(idx, sym, t.Elem(), seen)
	case *types.Array:
		walkTypeRefsSeen(idx, sym, t.Elem(), seen)
	case *types.Map:
		walkTypeRefsSeen(idx, sym, t.Key(), seen)
		walkTypeRefsSeen(idx, sym, t.Elem(), seen)
	case *types.Chan:
		walkTypeRefsSeen(idx, sym, t.Elem(), seen)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			walkTypeRefsSeen(idx, sym, t.Field(i).Type(), seen)
		}
	case *types.Signature:
		if t.Recv() != nil {
			walkTypeRefsSeen(idx, sym, t.Recv().Type(), seen)
		}
		tupleWalk := func(tup *types.Tuple) {
			if tup == nil {
				return
			}
			for i := 0; i < tup.Len(); i++ {
				walkTypeRefsSeen(idx, sym, tup.At(i).Type(), seen)
			}
		}
		tupleWalk(t.Params())
		tupleWalk(t.Results())
	case *types.Interface:
		// Avoid NumMethods recursion into method signatures that reference the interface.
		return
	}
}

func (s *Symbol) Location(root string) (file string, line, col int) {
	return loader.RelPath(s.Pkg.Fset, s.Pos, root)
}

func (s *Symbol) IsBlank() bool {
	return s.Name == "_"
}

func NormalizeRoots(roots []string) map[string]bool {
	out := map[string]bool{}
	for _, r := range roots {
		r = strings.TrimSpace(strings.ToLower(r))
		if r == "all" {
			for _, k := range []string{"main", "init", "exported", "tests", "benchmarks", "fuzz", "examples"} {
				out[k] = true
			}
			continue
		}
		for _, part := range strings.Split(r, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out[part] = true
			}
		}
	}
	return out
}
