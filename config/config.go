package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type RuleOverride struct {
	Enabled  *bool  `yaml:"enabled" json:"enabled,omitempty"`
	Severity string `yaml:"severity" json:"severity,omitempty"`
}

type StubsConfig struct {
	IncludeTests bool `yaml:"include-tests" json:"include-tests"`
	Strict       bool `yaml:"strict" json:"strict"`
}

type CoherenceConfig struct {
	IncludeTests bool `yaml:"include-tests" json:"include-tests"`
}

type Config struct {
	Roots         []string                `yaml:"roots" json:"roots"`
	Tests         *bool                   `yaml:"tests" json:"tests,omitempty"`
	Rules         map[string]RuleOverride `yaml:"rules" json:"rules,omitempty"`
	Exclude       []string                `yaml:"exclude" json:"exclude,omitempty"`
	Baseline      string                  `yaml:"baseline" json:"baseline,omitempty"`
	FailOn        string                  `yaml:"fail-on" json:"fail-on,omitempty"`
	MinConfidence string                  `yaml:"min-confidence" json:"min-confidence,omitempty"`
	Stubs         StubsConfig             `yaml:"stubs" json:"stubs"`
	Coherence     CoherenceConfig         `yaml:"coherence" json:"coherence"`

	// Runtime (not from file)
	ConfigPath string `yaml:"-" json:"-"`
	NoConfig   bool   `yaml:"-" json:"-"`
}

func Defaults() Config {
	t := true
	return Config{
		Roots:         []string{"all"},
		Tests:         &t,
		FailOn:        "warning",
		MinConfidence: "medium",
		Rules:         map[string]RuleOverride{},
	}
}

func Load(dir, explicit string, noConfig bool) (Config, error) {
	cfg := Defaults()
	if noConfig {
		cfg.NoConfig = true
		return cfg, nil
	}
	path := explicit
	if path == "" {
		for _, name := range []string{".gointegrity.yml", ".gointegrity.yaml"} {
			candidate := filepath.Join(dir, name)
			if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
				path = candidate
				break
			}
		}
	}
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	known := map[string]bool{
		"roots": true, "tests": true, "rules": true, "exclude": true,
		"baseline": true, "fail-on": true, "min-confidence": true,
		"stubs": true, "coherence": true,
	}
	for k := range raw {
		if !known[k] {
			return cfg, fmt.Errorf("unknown config key %q in %s", k, path)
		}
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.ConfigPath = path
	if cfg.FailOn == "" {
		cfg.FailOn = "warning"
	}
	if cfg.MinConfidence == "" {
		cfg.MinConfidence = "medium"
	}
	if len(cfg.Roots) == 0 {
		cfg.Roots = []string{"all"}
	}
	if cfg.Tests == nil {
		t := true
		cfg.Tests = &t
	}
	return cfg, nil
}

func (c Config) TestsEnabled() bool {
	if c.Tests == nil {
		return true
	}
	return *c.Tests
}

func (c Config) Hash() string {
	type wire struct {
		Roots         []string                `json:"roots"`
		Tests         bool                    `json:"tests"`
		Rules         map[string]RuleOverride `json:"rules,omitempty"`
		Exclude       []string                `json:"exclude,omitempty"`
		Baseline      string                  `json:"baseline,omitempty"`
		FailOn        string                  `json:"fail_on"`
		MinConfidence string                  `json:"min_confidence"`
		Stubs         StubsConfig             `json:"stubs"`
		Coherence     CoherenceConfig         `json:"coherence"`
	}
	rules := map[string]RuleOverride{}
	keys := make([]string, 0, len(c.Rules))
	for k := range c.Rules {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		rules[k] = c.Rules[k]
	}
	w := wire{
		Roots:         append([]string(nil), c.Roots...),
		Tests:         c.TestsEnabled(),
		Rules:         rules,
		Exclude:       append([]string(nil), c.Exclude...),
		Baseline:      c.Baseline,
		FailOn:        c.FailOn,
		MinConfidence: c.MinConfidence,
		Stubs:         c.Stubs,
		Coherence:     c.Coherence,
	}
	sort.Strings(w.Roots)
	sort.Strings(w.Exclude)
	b, _ := json.Marshal(w)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func MatchGlob(pattern, path string) bool {
	pattern = filepath.ToSlash(pattern)
	path = filepath.ToSlash(strings.TrimPrefix(path, "./"))
	if pattern == "" || path == "" {
		return false
	}
	if strings.Contains(pattern, "**") {
		parts := strings.SplitN(pattern, "**", 2)
		prefix := strings.TrimSuffix(parts[0], "/")
		suffix := strings.TrimPrefix(parts[1], "/")
		if prefix != "" && !strings.HasPrefix(path, prefix+"/") && path != prefix {
			return false
		}
		if suffix == "" {
			return true
		}
		return strings.HasSuffix(path, suffix) || strings.Contains(path, "/"+suffix) || matchSimple(suffix, filepath.Base(path))
	}
	ok, _ := filepath.Match(pattern, path)
	if ok {
		return true
	}
	ok, _ = filepath.Match(pattern, filepath.Base(path))
	return ok
}

// Excluded reports whether path (slash-separated, relative) matches any exclude pattern.
func Excluded(path string, patterns []string) bool {
	path = filepath.ToSlash(strings.TrimPrefix(path, "./"))
	for _, p := range patterns {
		if MatchGlob(p, path) {
			return true
		}
		// Prefix forms: "testdata", "testdata/", "testdata/*" all cover the tree.
		p = filepath.ToSlash(p)
		p = strings.TrimSuffix(p, "/*")
		prefix := strings.TrimSuffix(p, "/")
		if prefix == "" || strings.Contains(prefix, "*") {
			continue
		}
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// SkipDir reports whether a directory relative path should be pruned during walks.
func SkipDir(rel string, patterns []string) bool {
	rel = filepath.ToSlash(strings.TrimPrefix(rel, "./"))
	if rel == "" || rel == "." {
		return false
	}
	if Excluded(rel, patterns) {
		return true
	}
	// testdata/** should prune the testdata directory itself.
	for _, p := range patterns {
		p = filepath.ToSlash(p)
		if strings.Contains(p, "**") {
			prefix := strings.TrimSuffix(strings.SplitN(p, "**", 2)[0], "/")
			if prefix != "" && (rel == prefix || strings.HasPrefix(rel, prefix+"/")) {
				return true
			}
		}
	}
	return false
}

// FilterPaths drops excluded relative paths.
func FilterPaths(paths []string, patterns []string) []string {
	if len(patterns) == 0 {
		return paths
	}
	out := paths[:0:0]
	for _, p := range paths {
		if !Excluded(p, patterns) {
			out = append(out, p)
		}
	}
	return out
}

func matchSimple(pattern, name string) bool {
	ok, _ := filepath.Match(pattern, name)
	return ok
}

type Baseline struct {
	Fingerprints []string `json:"fingerprints"`
	set          map[string]bool
}

func LoadBaseline(path string) (*Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	b.set = map[string]bool{}
	for _, fp := range b.Fingerprints {
		b.set[fp] = true
	}
	return &b, nil
}

func (b *Baseline) Contains(fp string) bool {
	if b == nil {
		return false
	}
	return b.set[fp]
}

func WriteBaseline(path string, fingerprints []string) error {
	sort.Strings(fingerprints)
	uniq := fingerprints[:0]
	var prev string
	for _, fp := range fingerprints {
		if fp == prev {
			continue
		}
		uniq = append(uniq, fp)
		prev = fp
	}
	data, err := json.MarshalIndent(map[string]any{"fingerprints": uniq}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
