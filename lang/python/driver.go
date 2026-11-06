package python

import (
	"os"
	"path/filepath"
	"strings"

	"gointegrity/config"
	"gointegrity/lang"
	"gointegrity/lang/python/engine"
	"gointegrity/report"
)

// Driver implements lang.Driver for Python using the deep typechecker engine.
type Driver struct{}

func (Driver) ID() lang.ID { return lang.Python }

func (Driver) Detect(root string) (bool, []string) {
	var paths []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			n := info.Name()
			if n == ".git" || n == ".venv" || n == "venv" || n == "node_modules" || n == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(info.Name(), ".py") {
			rel, _ := filepath.Rel(root, path)
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	return len(paths) > 0, paths
}

func (Driver) Rules() []report.RuleInfo { return engine.Rules() }

func (d Driver) Crawl(opts lang.Options) (*report.Report, error) {
	paths := opts.Paths
	if len(paths) == 0 {
		_, paths = d.Detect(opts.RootDir)
	}
	paths = config.FilterPaths(paths, opts.Exclude)
	files := map[string]string{}
	for _, rel := range paths {
		b, err := os.ReadFile(filepath.Join(opts.RootDir, rel))
		if err != nil {
			continue
		}
		files[rel] = string(b)
	}
	findings := engine.Analyze(files)
	rep := report.New("crawl")
	rep.Tool.Name = "codeintegrity"
	rep.Rules = engine.Rules()
	rep.Findings = findings
	rep.Summary.Files = len(files)
	rep.Summary.Functions = countFuncs(findings)
	rep.FinalizeCrawl()
	return rep, nil
}

func countFuncs(fs []report.Finding) int {
	n := 0
	seen := map[string]bool{}
	for _, f := range fs {
		if !seen[f.Symbol] {
			seen[f.Symbol] = true
			n++
		}
	}
	return n
}
