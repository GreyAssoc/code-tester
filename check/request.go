package check

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const SchemaID = "gointegrity/check-request/v1"

type Expect struct {
	Outcome     string `json:"outcome,omitempty" yaml:"outcome,omitempty"`
	Quantifier  string `json:"quantifier,omitempty" yaml:"quantifier,omitempty"`
	MinTargets  int    `json:"min_targets,omitempty" yaml:"min_targets,omitempty"`
}

type Check struct {
	ID          string         `json:"id" yaml:"id"`
	Kind        string         `json:"kind" yaml:"kind"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
	Target      any            `json:"target,omitempty" yaml:"target,omitempty"`
	Params      map[string]any `json:"params,omitempty" yaml:"params,omitempty"`
	Expect      Expect         `json:"expect,omitempty" yaml:"expect,omitempty"`
}

type Request struct {
	Schema    string         `json:"schema" yaml:"schema"`
	RequestID string         `json:"request_id" yaml:"request_id"`
	Metadata  map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Checks    []Check        `json:"checks" yaml:"checks"`
}

type EnvelopeError struct {
	Pointer string
	Message string
}

func (e EnvelopeError) String() string {
	if e.Pointer == "" {
		return e.Message
	}
	return e.Pointer + ": " + e.Message
}

func LoadRequest(path string) (*Request, []EnvelopeError, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = os.ReadFile("/dev/stdin")
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, nil, err
	}
	return ParseRequest(data)
}

func ParseRequest(data []byte) (*Request, []EnvelopeError, error) {
	trimmed := strings.TrimSpace(string(data))
	var raw map[string]any
	var err error
	if strings.HasPrefix(trimmed, "{") {
		err = json.Unmarshal(data, &raw)
	} else {
		err = yaml.Unmarshal(data, &raw)
		if err == nil {
			// Re-marshal via JSON for consistent structure.
			b, _ := json.Marshal(raw)
			_ = json.Unmarshal(b, &raw)
		}
	}
	if err != nil {
		return nil, []EnvelopeError{{Pointer: "", Message: "invalid JSON/YAML: " + err.Error()}}, nil
	}

	var errs []EnvelopeError
	allowed := map[string]bool{"schema": true, "request_id": true, "metadata": true, "checks": true}
	for k := range raw {
		if !allowed[k] {
			errs = append(errs, EnvelopeError{Pointer: "/" + k, Message: "unknown top-level key"})
		}
	}
	schema, _ := raw["schema"].(string)
	if schema != SchemaID {
		errs = append(errs, EnvelopeError{Pointer: "/schema", Message: fmt.Sprintf("must be %q", SchemaID)})
	}
	rid, _ := raw["request_id"].(string)
	if rid == "" {
		errs = append(errs, EnvelopeError{Pointer: "/request_id", Message: "required"})
	}
	checksRaw, ok := raw["checks"].([]any)
	if !ok || len(checksRaw) == 0 {
		errs = append(errs, EnvelopeError{Pointer: "/checks", Message: "must contain 1–1000 checks"})
	}
	if len(errs) > 0 {
		sort.Slice(errs, func(i, j int) bool { return errs[i].Pointer < errs[j].Pointer })
		return nil, errs, nil
	}

	// Decode into typed request via JSON round-trip for determinism.
	canonical, err := json.Marshal(raw)
	if err != nil {
		return nil, []EnvelopeError{{Message: err.Error()}}, nil
	}
	var req Request
	if err := json.Unmarshal(canonical, &req); err != nil {
		return nil, []EnvelopeError{{Message: err.Error()}}, nil
	}
	seen := map[string]bool{}
	for i, c := range req.Checks {
		if c.ID == "" {
			errs = append(errs, EnvelopeError{Pointer: fmt.Sprintf("/checks/%d/id", i), Message: "required"})
			continue
		}
		if seen[c.ID] {
			errs = append(errs, EnvelopeError{Pointer: fmt.Sprintf("/checks/%d/id", i), Message: "duplicate check id"})
		}
		seen[c.ID] = true
	}
	if len(errs) > 0 {
		return nil, errs, nil
	}
	return &req, nil, nil
}

func (r *Request) Hash() string {
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

var knownKinds = map[string]bool{
	"returns_error_on_invalid_input": true,
	"dataflow_reaches":               true,
	"param_used":                     true,
	"no_placeholder":                 true,
	"reachable_from":                 true,
	"not_reachable":                  true,
	"calls":                          true,
	"never_calls":                    true,
	"returns_nonzero":                true,
	"pattern":                        true,
}

func ValidateCheck(c Check) string {
	if !knownKinds[c.Kind] {
		return "unknown kind " + c.Kind
	}
	return ""
}
