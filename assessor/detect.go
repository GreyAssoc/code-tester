package assessor

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gointegrity/config"
	"gointegrity/lang"
)

var defaultSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true,
	".gointegrity-cache": true, ".testbin": true, "__pycache__": true, ".venv": true,
	"venv": true, "coverage": true, ".next": true, "target": true,
}

// Detection is the result of scanning a tree for languages.
type Detection struct {
	Languages map[lang.ID][]string // language → relative file paths
}

// Detect walks root and classifies source files by language.
// exclude patterns (e.g. testdata/**) prune matching dirs and files.
func Detect(root string, exclude ...string) (*Detection, error) {
	d := &Detection{Languages: map[lang.ID][]string{}}
	root = filepath.Clean(root)
	err := filepath.WalkDir(root, func(path string, de fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := de.Name()
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if de.IsDir() {
			if rel != "." && config.SkipDir(rel, exclude) {
				return fs.SkipDir
			}
			if defaultSkipDirs[name] || (strings.HasPrefix(name, ".") && name != ".") {
				if name == ".git" || defaultSkipDirs[name] {
					return fs.SkipDir
				}
			}
			return nil
		}
		if config.Excluded(rel, exclude) {
			return nil
		}
		id := classify(rel, name)
		if id == "" {
			return nil
		}
		d.Languages[id] = append(d.Languages[id], rel)
		return nil
	})
	// Marker-boost: presence of go.mod alone counts as Go even if only in subdir.
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		if _, ok := d.Languages[lang.Go]; !ok {
			d.Languages[lang.Go] = []string{}
		}
	}
	return d, err
}

func classify(rel, base string) lang.ID {
	ext := strings.ToLower(filepath.Ext(base))
	switch ext {
	case ".go":
		return lang.Go
	case ".py", ".pyi":
		return lang.Python
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts":
		return lang.JS
	case ".css", ".scss", ".sass", ".less":
		return lang.CSS
	}
	return ""
}

// ParseLangList parses "auto" or "go,python,js,css".
func ParseLangList(s string) []lang.ID {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "auto" {
		return nil // nil means auto
	}
	var out []lang.ID
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		switch p {
		case "go":
			out = append(out, lang.Go)
		case "python", "py":
			out = append(out, lang.Python)
		case "js", "javascript", "ts", "typescript", "tsx", "jsx":
			out = append(out, lang.JS)
		case "css", "scss", "sass", "less":
			out = append(out, lang.CSS)
		}
	}
	return out
}
