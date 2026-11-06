package js

import (
	"os"
	"path/filepath"
	"strings"

	"gointegrity/config"
	"gointegrity/lang"
	"gointegrity/lang/js/engine"
	"gointegrity/report"
)

// Driver covers JavaScript and TypeScript variants via the deep typechecker engine.
type Driver struct{}

func (Driver) ID() lang.ID { return lang.JS }

var jsExt = map[string]bool{
	".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
	".ts": true, ".tsx": true, ".mts": true, ".cts": true,
}

func (Driver) Detect(root string) (bool, []string) {
	var paths []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			n := info.Name()
			if n == "node_modules" || n == ".git" || n == "dist" || n == "build" || n == ".next" {
				return filepath.SkipDir
			}
			return nil
		}
		if jsExt[strings.ToLower(filepath.Ext(info.Name()))] {
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
	rep.FinalizeCrawl()
	return rep, nil
}
