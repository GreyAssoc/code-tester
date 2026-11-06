package loader

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

type Result struct {
	Fset     *token.FileSet
	Packages []*packages.Package
	Prog     *ssa.Program
	SSAPkgs  []*ssa.Package
	Primary  []*packages.Package // analyzed packages matching patterns (non-dependency)
}

func Load(dir string, patterns []string, loadTests bool) (*Result, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	mode := packages.NeedName |
		packages.NeedFiles |
		packages.NeedCompiledGoFiles |
		packages.NeedImports |
		packages.NeedDeps |
		packages.NeedTypes |
		packages.NeedTypesSizes |
		packages.NeedSyntax |
		packages.NeedTypesInfo |
		packages.NeedModule
	cfg := &packages.Config{
		Mode:  mode,
		Dir:   dir,
		Tests: loadTests,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("packages.Load: %w", err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		return nil, fmt.Errorf("package load errors")
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages matched %v", patterns)
	}

	// Build SSA per-package with recover. A single bad dependency / syntax
	// edge (x/tools ssa can panic on some *ast.KeyValueExpr shapes) must not
	// crash the whole CLI.
	prog, ssaPkgs := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	if err := buildSSA(prog); err != nil {
		return nil, err
	}

	primary := filterPrimary(pkgs, patterns)
	return &Result{
		Fset:     pkgs[0].Fset,
		Packages: pkgs,
		Prog:     prog,
		SSAPkgs:  ssaPkgs,
		Primary:  primary,
	}, nil
}

func buildSSA(prog *ssa.Program) error {
	built := 0
	var firstPanic error
	for _, pkg := range prog.AllPackages() {
		if pkg == nil {
			continue
		}
		err := buildOne(pkg)
		if err != nil {
			path := "?"
			if pkg.Pkg != nil {
				path = pkg.Pkg.Path()
			}
			fmt.Fprintf(os.Stderr, "warning: skipping SSA for %s: %v\n", path, err)
			if firstPanic == nil {
				firstPanic = fmt.Errorf("ssa.Build %s: %v", path, err)
			}
			continue
		}
		built++
	}
	if built == 0 {
		if firstPanic != nil {
			return firstPanic
		}
		return fmt.Errorf("ssa.Build: no packages built")
	}
	return nil
}

func buildOne(pkg *ssa.Package) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	pkg.Build()
	return nil
}

func filterPrimary(pkgs []*packages.Package, patterns []string) []*packages.Package {
	// Prefer packages that are not in the module cache / GOROOT-style deps.
	// Heuristic: include packages whose PkgPath matches loaded roots or whose
	// GoFiles live under the load Dir (packages already scoped by patterns).
	out := []*packages.Package{}
	seen := map[string]bool{}
	for _, p := range pkgs {
		if p.Types == nil || len(p.Syntax) == 0 {
			continue
		}
		key := p.ID
		if seen[key] {
			continue
		}
		// Skip stdlib-ish packages without module.
		if p.Module == nil && !strings.Contains(p.PkgPath, ".") && p.PkgPath != "main" && !strings.HasPrefix(p.PkgPath, "command-line-arguments") {
			// Still include local main/test packages without a module.
			if !isLocalPatternMatch(p, patterns) {
				continue
			}
		}
		seen[key] = true
		out = append(out, p)
	}
	if len(out) == 0 {
		return pkgs
	}
	return out
}

func isLocalPatternMatch(p *packages.Package, patterns []string) bool {
	for _, pat := range patterns {
		if pat == "./..." || pat == "." {
			return true
		}
		if strings.HasSuffix(pat, "/...") {
			base := strings.TrimSuffix(pat, "/...")
			base = strings.TrimPrefix(base, "./")
			if strings.Contains(p.PkgPath, base) || strings.HasPrefix(p.Name, base) {
				return true
			}
		}
	}
	return p.Name == "main"
}

func RelPath(fset *token.FileSet, pos token.Pos, root string) (file string, line, col int) {
	if !pos.IsValid() {
		return "", 0, 0
	}
	p := fset.Position(pos)
	file = p.Filename
	if root != "" {
		if rel, err := relSlash(root, file); err == nil {
			file = rel
		}
	}
	file = slash(file)
	return file, p.Line, p.Column
}

func slash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

func relSlash(base, target string) (string, error) {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", err
	}
	return slash(rel), nil
}

// IsGenerated reports whether file has a Code generated header.
func IsGenerated(file *ast.File) bool {
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			t := c.Text
			if strings.Contains(t, "Code generated") && strings.Contains(t, "DO NOT EDIT") {
				return true
			}
		}
	}
	return false
}

func IsTestFile(filename string) bool {
	return strings.HasSuffix(filename, "_test.go")
}

func ObjectString(obj types.Object) string {
	if obj == nil {
		return ""
	}
	pkg := ""
	if obj.Pkg() != nil {
		pkg = obj.Pkg().Path()
	}
	switch o := obj.(type) {
	case *types.Func:
		sig, _ := o.Type().(*types.Signature)
		if sig != nil && sig.Recv() != nil {
			recv := types.TypeString(sig.Recv().Type(), (*types.Package).Path)
			recv = strings.TrimPrefix(recv, pkg+".")
			return fmt.Sprintf("%s.%s.%s", pkg, stripPointer(recv), o.Name())
		}
		if pkg == "" {
			return o.Name()
		}
		return pkg + "." + o.Name()
	default:
		if pkg == "" {
			return obj.Name()
		}
		return pkg + "." + obj.Name()
	}
}

func stripPointer(s string) string {
	s = strings.TrimPrefix(s, "*")
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}
