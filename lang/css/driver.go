package css

import (
	"os"
	"path/filepath"
	"strings"

	"gointegrity/config"
	"gointegrity/lang"
	"gointegrity/lang/css/engine"
	"gointegrity/report"
)

type Driver struct{}

func (Driver) ID() lang.ID { return lang.CSS }

func (Driver) Detect(root string) (bool, []string) {
	var paths []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			n := info.Name()
			if n == "node_modules" || n == ".git" || n == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(info.Name()))
		if ext == ".css" || ext == ".scss" || ext == ".sass" || ext == ".less" {
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
	cssFiles := map[string]string{}
	for _, rel := range paths {
		b, err := os.ReadFile(filepath.Join(opts.RootDir, rel))
		if err != nil {
			continue
		}
		cssFiles[rel] = string(b)
	}
	ref := buildRefCorpus(opts.RootDir, opts.Exclude)
	findings := engine.Analyze(opts.RootDir, cssFiles, ref)
	kept := findings[:0:0]
	for _, f := range findings {
		if config.Excluded(f.Location.File, opts.Exclude) {
			continue
		}
		kept = append(kept, f)
	}
	findings = kept
	rep := report.New("crawl")
	rep.Tool.Name = "codeintegrity"
	rep.Rules = engine.Rules()
	rep.Findings = findings
	rep.Summary.Files = len(cssFiles)
	rep.FinalizeCrawl()
	return rep, nil
}

func buildRefCorpus(root string, exclude []string) string {
	var b strings.Builder
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if info.IsDir() {
			n := info.Name()
			if n == "node_modules" || n == ".git" || n == "dist" {
				return filepath.SkipDir
			}
			if rel != "." && config.SkipDir(rel, exclude) {
				return filepath.SkipDir
			}
			return nil
		}
		if config.Excluded(rel, exclude) {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(info.Name()))
		switch ext {
		case ".html", ".htm", ".vue", ".svelte", ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs":
			data, err := os.ReadFile(path)
			if err == nil {
				b.Write(data)
				b.WriteByte('\n')
			}
		}
		return nil
	})
	return b.String()
}
