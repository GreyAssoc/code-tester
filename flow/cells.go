package flow

import (
	"fmt"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// CellKind classifies an abstract memory cell.
type CellKind int

const (
	CellGlobal CellKind = iota
	CellField
	CellMapContent
	CellSliceContent
	CellChanContent
	CellAlloc
)

// Cell is a field-sensitive abstract memory location.
type Cell struct {
	ID      string
	Kind    CellKind
	PkgPath string
	Name    string // global name, or Type.Field
	TypeKey string // for field cells: named type path
	Approx  bool   // may-flow-anywhere approximation (reflect/unsafe/cgo)
}

func GlobalCell(pkgPath, name string) *Cell {
	id := fmt.Sprintf("global:%s.%s", pkgPath, name)
	return &Cell{ID: id, Kind: CellGlobal, PkgPath: pkgPath, Name: name}
}

func FieldCell(typeKey, field string) *Cell {
	id := fmt.Sprintf("field:%s.%s", typeKey, field)
	return &Cell{ID: id, Kind: CellField, TypeKey: typeKey, Name: field}
}

func AllocCell(fn string, site string) *Cell {
	id := fmt.Sprintf("alloc:%s@%s", fn, site)
	return &Cell{ID: id, Kind: CellAlloc, Name: site, PkgPath: fn}
}

func ContentCell(kind CellKind, key string) *Cell {
	prefix := "map"
	switch kind {
	case CellSliceContent:
		prefix = "slice"
	case CellChanContent:
		prefix = "chan"
	}
	id := fmt.Sprintf("%s:%s", prefix, key)
	return &Cell{ID: id, Kind: kind, Name: key}
}

// PointsTo maps SSA values to abstract cells they may point to.
type PointsTo struct {
	Cells   map[string]*Cell
	ValTo   map[ssa.Value]map[string]bool // value → cell IDs
	Writes  map[string][]ssa.Value        // cell ID → stored values
	Reads   map[string][]ssa.Value        // cell ID → loaded values
	Approx  map[string]bool               // cell IDs involved in unsound approx
}

func NewPointsTo() *PointsTo {
	return &PointsTo{
		Cells:  map[string]*Cell{},
		ValTo:  map[ssa.Value]map[string]bool{},
		Writes: map[string][]ssa.Value{},
		Reads:  map[string][]ssa.Value{},
		Approx: map[string]bool{},
	}
}

func (p *PointsTo) addCell(c *Cell) *Cell {
	if existing, ok := p.Cells[c.ID]; ok {
		return existing
	}
	p.Cells[c.ID] = c
	return c
}

func (p *PointsTo) points(v ssa.Value, cellID string) {
	if v == nil || cellID == "" {
		return
	}
	m := p.ValTo[v]
	if m == nil {
		m = map[string]bool{}
		p.ValTo[v] = m
	}
	m[cellID] = true
}

func (p *PointsTo) cellsOf(v ssa.Value) []string {
	m := p.ValTo[v]
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func typeKey(t types.Type) string {
	if t == nil {
		return "?"
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		pkg := ""
		if n.Obj().Pkg() != nil {
			pkg = n.Obj().Pkg().Path()
		}
		return pkg + "." + n.Obj().Name()
	}
	return types.TypeString(t, func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Path()
	})
}

func isApproxPkg(path string) bool {
	return path == "reflect" || path == "unsafe" || strings.HasPrefix(path, "C.") || path == "C"
}

// AnalyzePointsTo builds a field-sensitive, context-insensitive points-to
// relation and records store/load sets for write-to-read linking.
func AnalyzePointsTo(funcs []*ssa.Function) *PointsTo {
	pt := NewPointsTo()
	for _, fn := range funcs {
		if fn == nil || fn.Blocks == nil {
			continue
		}
		analyzeFuncPoints(pt, fn)
	}
	return pt
}

func analyzeFuncPoints(pt *PointsTo, fn *ssa.Function) {
	fnName := fn.String()
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			switch v := instr.(type) {
			case *ssa.Alloc:
				site := fmt.Sprintf("%v", v.Pos())
				c := pt.addCell(AllocCell(fnName, site))
				pt.points(v, c.ID)
			case *ssa.FieldAddr:
				for _, cid := range pt.cellsOf(v.X) {
					base := pt.Cells[cid]
					tk := base.TypeKey
					if tk == "" {
						tk = typeKey(derefType(v.X.Type()))
					}
					field := fieldName(v.X.Type(), v.Field)
					c := pt.addCell(FieldCell(tk, field))
					pt.points(v, c.ID)
				}
				if len(pt.cellsOf(v.X)) == 0 {
					tk := typeKey(derefType(v.X.Type()))
					field := fieldName(v.X.Type(), v.Field)
					c := pt.addCell(FieldCell(tk, field))
					pt.points(v, c.ID)
				}
			case *ssa.IndexAddr:
				for _, cid := range pt.cellsOf(v.X) {
					c := pt.addCell(ContentCell(CellSliceContent, cid))
					pt.points(v, c.ID)
				}
			case *ssa.MakeMap:
				c := pt.addCell(ContentCell(CellMapContent, fmt.Sprintf("%s@%v", fnName, v.Pos())))
				pt.points(v, c.ID)
			case *ssa.MakeSlice:
				c := pt.addCell(ContentCell(CellSliceContent, fmt.Sprintf("%s@%v", fnName, v.Pos())))
				pt.points(v, c.ID)
			case *ssa.MakeChan:
				c := pt.addCell(ContentCell(CellChanContent, fmt.Sprintf("%s@%v", fnName, v.Pos())))
				pt.points(v, c.ID)
			case *ssa.Store:
				for _, cid := range pt.cellsOf(v.Addr) {
					pt.Writes[cid] = append(pt.Writes[cid], v.Val)
					pt.points(v.Val, cid) // value may flow into cell identity for linking
				}
			case *ssa.UnOp:
				if v.Op.String() == "*" { // load
					for _, cid := range pt.cellsOf(v.X) {
						pt.Reads[cid] = append(pt.Reads[cid], v)
					}
				}
			case *ssa.MapUpdate:
				for _, cid := range pt.cellsOf(v.Map) {
					content := pt.addCell(ContentCell(CellMapContent, cid))
					pt.Writes[content.ID] = append(pt.Writes[content.ID], v.Value)
				}
			case *ssa.Send:
				for _, cid := range pt.cellsOf(v.Chan) {
					content := pt.addCell(ContentCell(CellChanContent, cid))
					pt.Writes[content.ID] = append(pt.Writes[content.ID], v.X)
				}
			case *ssa.Call:
				markApproxCall(pt, v)
			case *ssa.MakeInterface:
				// interface box — value still tracked; cells unchanged
			case *ssa.ChangeType:
				for _, cid := range pt.cellsOf(v.X) {
					pt.points(v, cid)
				}
			case *ssa.Convert:
				for _, cid := range pt.cellsOf(v.X) {
					pt.points(v, cid)
				}
			case *ssa.Slice:
				for _, cid := range pt.cellsOf(v.X) {
					pt.points(v, cid)
				}
			case *ssa.Phi:
				for _, e := range v.Edges {
					for _, cid := range pt.cellsOf(e) {
						pt.points(v, cid)
					}
				}
			}
			// Track globals referenced as values in this instruction's operands.
			if val, ok := instr.(ssa.Value); ok {
				_ = val
			}
			scanGlobalOperands(pt, instr)
		}
	}
}

func derefType(t types.Type) types.Type {
	if p, ok := t.(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

func fieldName(t types.Type, index int) string {
	t = derefType(t)
	if n, ok := t.(*types.Named); ok {
		t = n.Underlying()
	}
	if s, ok := t.(*types.Struct); ok && index >= 0 && index < s.NumFields() {
		return s.Field(index).Name()
	}
	return fmt.Sprintf("#%d", index)
}

func markApproxCall(pt *PointsTo, call *ssa.Call) {
	common := call.Common()
	if common == nil {
		return
	}
	if callee := common.StaticCallee(); callee != nil && callee.Pkg != nil && callee.Pkg.Pkg != nil {
		if isApproxPkg(callee.Pkg.Pkg.Path()) {
			id := "approx:" + callee.String()
			c := pt.addCell(&Cell{ID: id, Kind: CellAlloc, Name: callee.String(), Approx: true})
			pt.Approx[c.ID] = true
			pt.points(call, c.ID)
		}
	}
}


func scanGlobalOperands(pt *PointsTo, instr ssa.Instruction) {
	var ops []ssa.Value
	switch v := instr.(type) {
	case *ssa.Store:
		ops = []ssa.Value{v.Addr, v.Val}
	case *ssa.Call:
		ops = append([]ssa.Value{}, v.Common().Args...)
	case *ssa.FieldAddr:
		ops = []ssa.Value{v.X}
	case *ssa.UnOp:
		ops = []ssa.Value{v.X}
	}
	for _, op := range ops {
		g, ok := op.(*ssa.Global)
		if !ok || g == nil {
			continue
		}
		pkg, name := "", g.Name()
		if g.Package() != nil && g.Package().Pkg != nil {
			pkg = g.Package().Pkg.Path()
		}
		c := pt.addCell(GlobalCell(pkg, name))
		pt.points(g, c.ID)
	}
}

// WriteReadLinks returns pairs (writeValue, readValue) linked through the same cell.
func (p *PointsTo) WriteReadLinks() [][2]ssa.Value {
	var out [][2]ssa.Value
	ids := make([]string, 0, len(p.Cells))
	for id := range p.Cells {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		writes := p.Writes[id]
		reads := p.Reads[id]
		for _, w := range writes {
			for _, r := range reads {
				out = append(out, [2]ssa.Value{w, r})
			}
		}
	}
	return out
}
