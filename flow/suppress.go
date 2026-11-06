package flow

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type FlowSuppression struct {
	Rule     string `yaml:"rule" json:"rule"`
	Source   string `yaml:"source" json:"source"`
	Sink     string `yaml:"sink" json:"sink"`
	PathHash string `yaml:"path_hash,omitempty" json:"path_hash,omitempty"`
	Reason   string `yaml:"reason" json:"reason"`
	Owner    string `yaml:"owner" json:"owner"`
	Expires  string `yaml:"expires,omitempty" json:"expires,omitempty"` // YYYY-MM-DD
}

type SuppressionFile struct {
	Suppressions []FlowSuppression `yaml:"suppressions"`
}

func LoadSuppressions(path string) (*SuppressionFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sf SuppressionFile
	// Allow bare list or wrapped.
	if err := yaml.Unmarshal(data, &sf); err != nil {
		var list []FlowSuppression
		if err2 := yaml.Unmarshal(data, &list); err2 != nil {
			return nil, err
		}
		sf.Suppressions = list
	}
	return &sf, nil
}

func WriteSuppression(path string, s FlowSuppression) error {
	var sf SuppressionFile
	if data, err := os.ReadFile(path); err == nil {
		_ = yaml.Unmarshal(data, &sf)
		if len(sf.Suppressions) == 0 {
			var list []FlowSuppression
			_ = yaml.Unmarshal(data, &list)
			sf.Suppressions = list
		}
	}
	sf.Suppressions = append(sf.Suppressions, s)
	sort.Slice(sf.Suppressions, func(i, j int) bool {
		a, b := sf.Suppressions[i], sf.Suppressions[j]
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Sink < b.Sink
	})
	out, err := yaml.Marshal(&sf)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func (s FlowSuppression) Validate() error {
	if s.Rule == "" || s.Source == "" || s.Sink == "" {
		return fmt.Errorf("rule, source, and sink are required")
	}
	if strings.TrimSpace(s.Reason) == "" || strings.TrimSpace(s.Owner) == "" {
		return fmt.Errorf("reason and owner are required")
	}
	return nil
}

func (s FlowSuppression) Expired(now time.Time) bool {
	if s.Expires == "" {
		return false
	}
	t, err := time.Parse("2006-01-02", s.Expires)
	if err != nil {
		return false
	}
	return now.After(t)
}

func (sf *SuppressionFile) Matches(rule, source, sink, pathHash string) bool {
	for _, s := range sf.Suppressions {
		if s.Rule != rule && s.Rule != "all" && !strings.HasPrefix(rule, s.Rule) {
			continue
		}
		if s.Source != source && s.Source != "*" {
			continue
		}
		if s.Sink != sink && s.Sink != "*" {
			continue
		}
		if s.PathHash != "" && s.PathHash != pathHash {
			continue
		}
		if s.Expired(time.Now().UTC()) {
			continue
		}
		return true
	}
	return false
}

// HygieneWarnings returns stale/expired suppression warnings.
func (sf *SuppressionFile) HygieneWarnings(active map[string]bool) []string {
	var out []string
	now := time.Now().UTC()
	for _, s := range sf.Suppressions {
		key := s.Rule + "|" + s.Source + "|" + s.Sink + "|" + s.PathHash
		if s.Expired(now) {
			out = append(out, fmt.Sprintf("expired suppression: %s (%s → %s)", s.Rule, s.Source, s.Sink))
		} else if !active[key] && !active[s.Rule+"|"+s.Source+"|"+s.Sink+"|"] {
			out = append(out, fmt.Sprintf("stale suppression: %s (%s → %s)", s.Rule, s.Source, s.Sink))
		}
	}
	sort.Strings(out)
	return out
}
