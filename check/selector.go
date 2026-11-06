package check

import (
	"encoding/json"
	"go/types"
	"path"
	"strings"

	"github.com/GreyAssoc/code-tester/callgraph"
)

type Selector struct {
	Symbol       string `json:"symbol,omitempty"`
	Package      string `json:"package,omitempty"`
	Function     string `json:"function,omitempty"`
	Receiver     string `json:"receiver,omitempty"`
	Kind         string `json:"kind,omitempty"` // func, method, any
	ExportedOnly bool   `json:"exported_only,omitempty"`
	IncludeTests bool   `json:"include_tests,omitempty"`
	HandlersSig  string `json:"-"`
	HandlersImpl string `json:"-"`
}

func ParseSelector(raw any) (Selector, error) {
	if raw == nil {
		return Selector{}, nil
	}
	switch v := raw.(type) {
	case string:
		return Selector{Symbol: v}, nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return Selector{}, err
		}
		var s Selector
		if err := json.Unmarshal(b, &s); err != nil {
			return Selector{}, err
		}
		var probe map[string]any
		_ = json.Unmarshal(b, &probe)
		if h, ok := probe["handlers"].(map[string]any); ok {
			if sig, ok := h["signature"].(string); ok {
				s.HandlersSig = sig
			}
			if impl, ok := h["implements"].(string); ok {
				s.HandlersImpl = impl
			}
		}
		return s, nil
	}
}

func Resolve(idx *callgraph.Index, sel Selector) []*callgraph.Symbol {
	var out []*callgraph.Symbol
	for _, sym := range idx.Symbols {
		if sym.Kind != callgraph.KindFunc && sym.Kind != callgraph.KindMethod {
			continue
		}
		if !sel.IncludeTests && sym.IsTest {
			continue
		}
		if sel.ExportedOnly && !sym.Exported {
			continue
		}
		if sel.Kind == "func" && sym.Kind != callgraph.KindFunc {
			continue
		}
		if sel.Kind == "method" && sym.Kind != callgraph.KindMethod {
			continue
		}
		if sel.Symbol != "" && !symbolMatch(sel.Symbol, sym.Qualified) {
			continue
		}
		if sel.Package != "" && !packageMatch(sel.Package, sym.PkgPath) {
			continue
		}
		if sel.Function != "" && !globMatch(sel.Function, sym.Name) {
			continue
		}
		if sel.Receiver != "" {
			if sym.Kind != callgraph.KindMethod || !globMatch(sel.Receiver, sym.Recv) {
				continue
			}
		}
		if sel.HandlersImpl != "" && !implementsHandler(idx, sym, sel.HandlersImpl) {
			continue
		}
		out = append(out, sym)
	}
	return out
}

func symbolMatch(want, got string) bool {
	norm := func(s string) string {
		s = strings.ReplaceAll(s, "(*", "")
		s = strings.ReplaceAll(s, ")", "")
		return s
	}
	return want == got || norm(want) == norm(got)
}

func packageMatch(pattern, pkgPath string) bool {
	if strings.HasSuffix(pattern, "/...") {
		base := strings.TrimSuffix(pattern, "/...")
		return pkgPath == base || strings.HasPrefix(pkgPath, base+"/")
	}
	if globMatch(pattern, pkgPath) || globMatch(pattern, path.Base(pkgPath)) {
		return true
	}
	return strings.Contains(pkgPath, pattern)
}

func globMatch(pattern, name string) bool {
	ok, _ := path.Match(pattern, name)
	return ok
}

func implementsHandler(idx *callgraph.Index, sym *callgraph.Symbol, ifaceQual string) bool {
	if sym.Kind != callgraph.KindMethod {
		return false
	}
	for _, iface := range idx.Ifaces {
		pkgPath := ""
		if iface.Obj().Pkg() != nil {
			pkgPath = iface.Obj().Pkg().Path()
		}
		qual := pkgPath + "." + iface.Obj().Name()
		if qual != ifaceQual && iface.Obj().Name() != ifaceQual {
			continue
		}
		it, ok := iface.Underlying().(*types.Interface)
		if !ok {
			continue
		}
		for i := 0; i < it.NumMethods(); i++ {
			if it.Method(i).Name() == sym.Name {
				return true
			}
		}
	}
	return false
}
