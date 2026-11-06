package golang

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GreyAssoc/code-tester/callgraph"
	"github.com/GreyAssoc/code-tester/config"
	"github.com/GreyAssoc/code-tester/flow"
	"github.com/GreyAssoc/code-tester/lang"
	"github.com/GreyAssoc/code-tester/loader"
	"github.com/GreyAssoc/code-tester/report"
	"github.com/GreyAssoc/code-tester/rules"
)

// Driver implements lang.Driver for Go.
type Driver struct{}

func (Driver) ID() lang.ID { return lang.Go }

func (Driver) Detect(root string) (bool, []string) {
	var paths []string
	foundMod := false
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		base := info.Name()
		if info.IsDir() {
			if base == "vendor" || base == "node_modules" || base == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if base == "go.mod" {
			foundMod = true
		}
		if strings.HasSuffix(base, ".go") {
			rel, _ := filepath.Rel(root, path)
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	return foundMod || len(paths) > 0, paths
}

func (Driver) Rules() []report.RuleInfo {
	en := rules.ResolveEnablement(config.Defaults(), nil, nil)
	return rules.RuleInfos(en)
}

func (d Driver) Crawl(opts lang.Options) (*report.Report, error) {
	cfg, err := config.Load(opts.RootDir, "", false)
	if err != nil {
		return nil, err
	}
	cfg.Exclude = append(cfg.Exclude, opts.Exclude...)

	modRoots := findGoModRoots(opts.RootDir, opts.Exclude)
	if len(modRoots) == 0 {
		if !config.SkipDir(".", opts.Exclude) {
			modRoots = []string{opts.RootDir}
		}
	}

	// D-015-04: lean single-config unless Windows-oriented sources exist.
	gooses := []string{""} // host/default
	if loader.DetectWindowsOriented(opts.RootDir) {
		gooses = append(gooses, "windows")
		fmt.Fprintln(os.Stderr, "windows-oriented Go code detected; adding GOOS=windows analysis pass")
	}

	rep := report.New("crawl")
	rep.Tool.Name = "codeintegrity"
	en := rules.ResolveEnablement(cfg, nil, nil)
	rep.Rules = rules.RuleInfos(en)
	var allFindings []report.Finding
	seenFP := map[string]bool{}
	suppressed := 0
	packages, filesN, funcsN := 0, 0, 0

	for _, modRoot := range modRoots {
		for _, goos := range gooses {
			res, err := loader.LoadOpts(loader.LoadOptions{
				Dir: modRoot, Patterns: []string{"./..."}, Tests: cfg.TestsEnabled(), GOOS: goos,
			})
			if err != nil {
				continue
			}
			idx := callgraph.Build(res, opts.RootDir)
			roots := callgraph.Roots(idx, callgraph.NormalizeRoots(cfg.Roots))
			reach := callgraph.Analyze(idx, roots)
			rctx := rules.Context{Index: idx, Reach: reach, Config: cfg, RootDir: opts.RootDir}
			findings, sup := rules.RunCrawl(rctx, en)
			suppressed += sup

			if opts.Flow {
				g, _ := flow.Build(idx, flow.BuildOptions{RootDir: opts.RootDir, ContextSens: 1})
				primary := map[string]bool{}
				for _, p := range idx.Packages {
					if p != nil && p.PkgPath != "" {
						primary[p.PkgPath] = true
					}
				}
				ff, _ := g.AnalyzeRules(flow.AnalyzeOptions{PrimaryPkgs: primary})
				for _, f := range ff {
					if f.Suppressed {
						suppressed++
						continue
					}
					rf := f.Finding
					rf.CodeFlows = g.CodeFlow(f.Witness)
					findings = append(findings, rf)
				}
			}
			for _, f := range findings {
				if goos != "" && f.Message != "" && !strings.Contains(f.Message, "GOOS=") {
					f.Message = f.Message + " [GOOS=" + goos + "]"
				}
				if f.Fingerprint != "" {
					if seenFP[f.Fingerprint] {
						continue
					}
					seenFP[f.Fingerprint] = true
				}
				allFindings = append(allFindings, f)
			}
			if goos == "" {
				packages += len(res.Primary)
				files := map[string]bool{}
				for _, sym := range idx.Symbols {
					if sym.Kind == callgraph.KindFunc || sym.Kind == callgraph.KindMethod {
						funcsN++
					}
					file, _, _ := sym.Location(opts.RootDir)
					if file != "" && !config.Excluded(file, opts.Exclude) {
						files[file] = true
					}
				}
				filesN += len(files)
			}
		}
	}

	kept := allFindings[:0:0]
	for _, f := range allFindings {
		if config.Excluded(f.Location.File, opts.Exclude) {
			continue
		}
		f.Language = "go"
		kept = append(kept, f)
	}
	rep.Findings = kept
	rep.Summary.Packages = packages
	rep.Summary.Files = filesN
	rep.Summary.Functions = funcsN
	rep.Summary.Suppressed = suppressed
	rep.FinalizeCrawl()
	return rep, nil
}

func findGoModRoots(root string, exclude []string) []string {
	var roots []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if info.IsDir() {
			n := info.Name()
			if n == "vendor" || n == "node_modules" || n == ".git" {
				return filepath.SkipDir
			}
			if rel != "." && config.SkipDir(rel, exclude) {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() == "go.mod" {
			if config.Excluded(rel, exclude) || (filepath.Dir(rel) != "." && config.SkipDir(filepath.ToSlash(filepath.Dir(rel)), exclude)) {
				return nil
			}
			roots = append(roots, filepath.Dir(path))
		}
		return nil
	})
	return roots
}
