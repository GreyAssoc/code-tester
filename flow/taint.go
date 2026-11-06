package flow

import (
	"regexp"
	"sort"
	"strings"
)

type Label string

const (
	LabelUserInput Label = "user_input"
	LabelSecret    Label = "secret"
	LabelConfig    Label = "config"
	LabelConstant  Label = "constant"
)

var secretNamePattern = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|private[_-]?key|credential|auth)`)

// DefaultSources returns built-in source matchers (symbol substrings / patterns).
func DefaultSources() []SourceSpec {
	return []SourceSpec{
		{Label: LabelUserInput, Match: "os.Args"},
		{Label: LabelUserInput, Match: "flag."},
		{Label: LabelUserInput, Match: "http.Request"},
		{Label: LabelUserInput, Match: "(*http.Request)"},
		{Label: LabelUserInput, Match: "json.Decoder.Decode"},
		{Label: LabelUserInput, Match: "os.ReadFile"},
		{Label: LabelUserInput, Match: "bufio.Scanner"},
		{Label: LabelConfig, Match: "os.Getenv"},
		{Label: LabelConfig, Match: "os.LookupEnv"},
	}
}

func DefaultSinks() []SinkSpec {
	return []SinkSpec{
		{Name: "log", Match: "log.Print"},
		{Name: "log", Match: "log.Fatal"},
		{Name: "log", Match: "log.Panic"},
		{Name: "log", Match: "fmt.Print"},
		{Name: "log", Match: "(*log.Logger)"},
		{Name: "exec", Match: "os/exec"},
		{Name: "exec", Match: "exec.Command"},
		{Name: "network", Match: "http.Post"},
		{Name: "network", Match: "http.Get"},
		{Name: "network", Match: "net.Dial"},
	}
}

type SourceSpec struct {
	Label Label
	Match string
}

type SinkSpec struct {
	Name  string
	Match string
}

// PropagateLabels seeds labels on source nodes then pushes union sets along edges.
func (g *Graph) PropagateLabels() {
	changed := true
	for changed {
		changed = false
		for _, e := range g.Edges {
			from := g.Nodes[e.From]
			to := g.Nodes[e.To]
			if from == nil || to == nil {
				continue
			}
			before := len(to.Labels)
			to.Labels = unionSorted(to.Labels, from.Labels)
			if len(to.Labels) != before {
				changed = true
			}
		}
	}
}

func unionSorted(a, b []string) []string {
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	for _, x := range b {
		set[x] = true
	}
	out := make([]string, 0, len(set))
	for x := range set {
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}

func SecretName(name string) bool {
	return secretNamePattern.MatchString(name)
}

func MatchAny(symbol string, specs []SourceSpec) (Label, bool) {
	for _, s := range specs {
		if strings.Contains(symbol, s.Match) {
			return s.Label, true
		}
	}
	return "", false
}

func MatchSink(symbol string, specs []SinkSpec) (string, bool) {
	for _, s := range specs {
		if strings.Contains(symbol, s.Match) {
			return s.Name, true
		}
	}
	return "", false
}

// StripLabels removes the given labels from a node (sanitizer effect).
func (g *Graph) StripLabels(nodeID string, labels []string) {
	n := g.Nodes[nodeID]
	if n == nil {
		return
	}
	drop := map[string]bool{}
	for _, l := range labels {
		drop[l] = true
	}
	kept := n.Labels[:0]
	for _, l := range n.Labels {
		if !drop[l] {
			kept = append(kept, l)
		}
	}
	n.Labels = kept
}
