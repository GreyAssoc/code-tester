package golang

import (
	"os"
	"path/filepath"
	"strings"

	"gointegrity/callgraph"
	"gointegrity/config"
	"gointegrity/flow"
	"gointegrity/lang"
	"gointegrity/loader"
	"gointegrity/report"
	"gointegrity/rules"
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
		// Only fall back to root if the root itself is not excluded.
		if !config.SkipDir(".", opts.Exclude) {
			modRoots = []string{opts.RootDir}
		}
	}

	rep := report.New("crawl")
	rep.Tool.Name = "codeintegrity"
	en := rules.ResolveEnablement(cfg, nil, nil)
	rep.Rules = rules.RuleInfos(en)
	var allFindings []report.Finding
	suppressed := 0
	packages, filesN, funcsN := 0, 0, 0

	for _, modRoot := range modRoots {
		res, err := loader.Load(modRoot, []string{"./..."}, cfg.TestsEnabled())
		if err != nil {
			// Skip modules that fail to load (e.g. incomplete fixtures).
			continue
		}
		idx := callgraph.Build(res, opts.RootDir)
		roots := callgraph.Roots(idx, callgraph.NormalizeRoots(cfg.Roots))
		reach := callgraph.Analyze(idx, roots)
		rctx := rules.Context{Index: idx, Reach: reach, Config: cfg, RootDir: opts.RootDir}
		findings, sup := rules.RunCrawl(rctx, en)
		suppressed += sup

		if opts.Flow {
			// D-015-01: default context sensitivity 1; D-015-02: DefaultSources/Sinks via Build.
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
		allFindings = append(allFindings, findings...)
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
