package flow

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type PolicyKind string

const (
	PolicyMustReach       PolicyKind = "must_reach"
	PolicyMustNotReach    PolicyKind = "must_not_reach"
	PolicyMustPassThrough PolicyKind = "must_pass_through"
	PolicyAllowedSinks    PolicyKind = "allowed_sinks"
)

type Policy struct {
	ID       string     `yaml:"id" json:"id"`
	Kind     PolicyKind `yaml:"kind" json:"kind"`
	From     string     `yaml:"from" json:"from"`                             // node id or symbol
	To       string     `yaml:"to,omitempty" json:"to,omitempty"`             // node id or symbol
	Via      string     `yaml:"via,omitempty" json:"via,omitempty"`           // sanitizer for must_pass_through
	Label    string     `yaml:"label,omitempty" json:"label,omitempty"`       // optional label scope
	Sinks    []string   `yaml:"sinks,omitempty" json:"sinks,omitempty"`       // for allowed_sinks
	Reason   string     `yaml:"reason,omitempty" json:"reason,omitempty"`
}

type PolicyFile struct {
	Policies []Policy `yaml:"policies"`
	Sources  []SourceSpec `yaml:"sources,omitempty"`
	Sinks    []SinkSpec   `yaml:"sinks,omitempty"`
}

func LoadPolicies(path string) (*PolicyFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pf PolicyFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return nil, err
	}
	return &pf, nil
}

type PolicyVerdict struct {
	PolicyID string
	Kind     PolicyKind
	Status   string // pass, fail, unresolved
	Message  string
	Witness  []string
	Approx   bool
}

func (g *Graph) resolveNode(ref string) []string {
	if ref == "" {
		return nil
	}
	if _, ok := g.Nodes[ref]; ok {
		return []string{ref}
	}
	var ids []string
	for id, n := range g.Nodes {
		if n.Symbol == ref || strings.HasSuffix(id, ref) || strings.Contains(n.Symbol, ref) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func (g *Graph) EvaluatePolicies(policies []Policy) []PolicyVerdict {
	var out []PolicyVerdict
	for _, p := range policies {
		out = append(out, g.evalOne(p))
	}
	return out
}

func (g *Graph) evalOne(p Policy) PolicyVerdict {
	v := PolicyVerdict{PolicyID: p.ID, Kind: p.Kind}
	froms := g.resolveNode(p.From)
	tos := g.resolveNode(p.To)
	if len(froms) == 0 {
		v.Status = "unresolved"
		v.Message = "from not resolved: " + p.From
		return v
	}
	switch p.Kind {
	case PolicyMustReach:
		if len(tos) == 0 {
			v.Status = "unresolved"
			v.Message = "to not resolved: " + p.To
			return v
		}
		for _, f := range froms {
			if p.Label != "" && !nodeHasLabel(g.Nodes[f], p.Label) {
				continue
			}
			for _, t := range tos {
				if path := g.ShortestPath(f, t); len(path) > 0 {
					v.Status = "pass"
					v.Witness = path
					v.Message = fmt.Sprintf("%s reaches %s", f, t)
					return v
				}
			}
		}
		v.Status = "fail"
		v.Message = fmt.Sprintf("%s does not reach %s", p.From, p.To)
	case PolicyMustNotReach:
		if len(tos) == 0 {
			v.Status = "unresolved"
			v.Message = "to not resolved: " + p.To
			return v
		}
		for _, f := range froms {
			if p.Label != "" && !nodeHasLabel(g.Nodes[f], p.Label) {
				continue
			}
			for _, t := range tos {
				if path := g.ShortestPath(f, t); len(path) > 0 {
					v.Status = "fail"
					v.Witness = path
					v.Message = fmt.Sprintf("%s reaches %s", f, t)
					v.Approx = pathApprox(g, path)
					return v
				}
			}
		}
		v.Status = "pass"
		v.Message = fmt.Sprintf("%s does not reach %s", p.From, p.To)
	case PolicyMustPassThrough:
		if len(tos) == 0 || p.Via == "" {
			v.Status = "unresolved"
			v.Message = "to/via required"
			return v
		}
		vias := g.resolveNode(p.Via)
		if len(vias) == 0 {
			v.Status = "unresolved"
			v.Message = "via not resolved: " + p.Via
			return v
		}
		viaSet := map[string]bool{}
		for _, x := range vias {
			viaSet[x] = true
		}
		for _, f := range froms {
			for _, t := range tos {
				path := g.ShortestPath(f, t)
				if len(path) == 0 {
					v.Status = "fail"
					v.Message = "no path from from to to"
					return v
				}
				ok := false
				for _, n := range path {
					if viaSet[n] {
						ok = true
						break
					}
				}
				if !ok {
					v.Status = "fail"
					v.Witness = path
					v.Message = "path does not pass through sanitizer"
					return v
				}
			}
		}
		v.Status = "pass"
		v.Message = "all paths pass through sanitizer"
	case PolicyAllowedSinks:
		allowed := map[string]bool{}
		for _, s := range p.Sinks {
			for _, id := range g.resolveNode(s) {
				allowed[id] = true
			}
		}
		for _, f := range froms {
			if p.Label != "" && !nodeHasLabel(g.Nodes[f], p.Label) {
				continue
			}
			for id, n := range g.Nodes {
				if n.Kind != NodeSink {
					continue
				}
				if allowed[id] {
					continue
				}
				if path := g.ShortestPath(f, id); len(path) > 0 {
					v.Status = "fail"
					v.Witness = path
					v.Message = fmt.Sprintf("reaches disallowed sink %s", n.Symbol)
					return v
				}
			}
		}
		v.Status = "pass"
		v.Message = "only allowed sinks reached"
	default:
		v.Status = "unresolved"
		v.Message = "unknown policy kind"
	}
	return v
}

func nodeHasLabel(n *Node, label string) bool {
	if n == nil {
		return false
	}
	for _, l := range n.Labels {
		if l == label {
			return true
		}
	}
	return false
}

func pathApprox(g *Graph, path []string) bool {
	for _, id := range path {
		if n := g.Nodes[id]; n != nil && n.Approx {
			return true
		}
	}
	return false
}
