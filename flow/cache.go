package flow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const CacheVersion = "0.2.0"

type PackageCache struct {
	PkgPath     string            `json:"pkg_path"`
	SourceHash  string            `json:"source_hash"`
	ExportHash  string            `json:"export_hash"`
	SummaryHash string            `json:"summary_hash"`
	Summaries   []FuncSummary     `json:"summaries"`
	CellRW      map[string]CellRW `json:"cell_rw"`
	CallEdges   []CachedEdge      `json:"call_edges"`
}

type FuncSummary struct {
	Symbol        string   `json:"symbol"`
	ParamToResult []string `json:"param_to_result"`
	ParamToCell   []string `json:"param_to_cell"`
	CellToResult  []string `json:"cell_to_result"`
}

type CellRW struct {
	Writes int `json:"writes"`
	Reads  int `json:"reads"`
}

type CachedEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"`
}

type CacheIndex struct {
	ToolVersion string                   `json:"tool_version"`
	Packages    map[string]*PackageCache `json:"packages"`
}

func CacheDir(root string) string {
	return filepath.Join(root, ".gointegrity-cache")
}

func LoadCache(root string) (*CacheIndex, error) {
	path := filepath.Join(CacheDir(root), "index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var idx CacheIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	if idx.ToolVersion != CacheVersion {
		return nil, fmt.Errorf("cache tool version %s != %s", idx.ToolVersion, CacheVersion)
	}
	return &idx, nil
}

func SaveCache(root string, idx *CacheIndex) error {
	dir := CacheDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	idx.ToolVersion = CacheVersion
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "index.json"), append(data, '\n'), 0o644)
}

func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func HashStrings(ss []string) string {
	sort.Strings(ss)
	h := sha256.New()
	for _, s := range ss {
		fmt.Fprintf(h, "%s\n", s)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// BuildPackageCache extracts cacheable summaries from a graph for one package.
func BuildPackageCache(pkgPath, sourceHash, exportHash string, g *Graph, pt *PointsTo) *PackageCache {
	pc := &PackageCache{
		PkgPath:    pkgPath,
		SourceHash: sourceHash,
		ExportHash: exportHash,
		CellRW:     map[string]CellRW{},
	}
	for id, c := range pt.Cells {
		pc.CellRW[id] = CellRW{Writes: len(pt.Writes[id]), Reads: len(pt.Reads[id])}
		_ = c
	}
	for _, e := range g.Edges {
		if e.Label == EdgeArg || e.Label == EdgeReturn {
			pc.CallEdges = append(pc.CallEdges, CachedEdge{From: e.From, To: e.To, Label: string(e.Label)})
		}
	}
	sort.Slice(pc.CallEdges, func(i, j int) bool {
		if pc.CallEdges[i].From != pc.CallEdges[j].From {
			return pc.CallEdges[i].From < pc.CallEdges[j].From
		}
		return pc.CallEdges[i].To < pc.CallEdges[j].To
	})
	pc.SummaryHash = HashStrings([]string{fmt.Sprintf("%d", len(pc.Summaries)), fmt.Sprintf("%d", len(pc.CallEdges))})
	return pc
}

// NeedsReanalyze reports whether pkg must be rebuilt given old cache entry.
func NeedsReanalyze(old *PackageCache, sourceHash, exportHash string) bool {
	if old == nil {
		return true
	}
	return old.SourceHash != sourceHash || old.ExportHash != exportHash
}
