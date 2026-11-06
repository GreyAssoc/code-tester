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

// Broad secret-name patterns (D-015-02): prefer recall; triage false positives later.
var secretNamePattern = regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|api[_-]?key|private[_-]?key|credential|auth|bearer|session|cookie|csrf|oauth|refresh[_-]?token|access[_-]?key|secret[_-]?key|client[_-]?secret|encryption[_-]?key|signing[_-]?key|ssh[_-]?key|pem|cert|keypair|connection[_-]?string|database[_-]?url|db[_-]?pass)`)

// DefaultSources returns built-in source matchers (D-015-02 broad lists).
func DefaultSources() []SourceSpec {
	return []SourceSpec{
		{Label: LabelUserInput, Match: "os.Args"},
		{Label: LabelUserInput, Match: "flag."},
		{Label: LabelUserInput, Match: "http.Request"},
		{Label: LabelUserInput, Match: "(*http.Request)"},
		{Label: LabelUserInput, Match: "Request.Form"},
		{Label: LabelUserInput, Match: "Request.PostForm"},
		{Label: LabelUserInput, Match: "Request.Body"},
		{Label: LabelUserInput, Match: "Request.URL"},
		{Label: LabelUserInput, Match: "Request.Header"},
		{Label: LabelUserInput, Match: "Request.Cookie"},
		{Label: LabelUserInput, Match: "Request.MultipartForm"},
		{Label: LabelUserInput, Match: "json.Decoder.Decode"},
		{Label: LabelUserInput, Match: "json.Unmarshal"},
		{Label: LabelUserInput, Match: "xml.Decoder.Decode"},
		{Label: LabelUserInput, Match: "xml.Unmarshal"},
		{Label: LabelUserInput, Match: "yaml.Unmarshal"},
		{Label: LabelUserInput, Match: "os.ReadFile"},
		{Label: LabelUserInput, Match: "os.ReadDir"},
		{Label: LabelUserInput, Match: "io.ReadAll"},
		{Label: LabelUserInput, Match: "io.Copy"},
		{Label: LabelUserInput, Match: "bufio.Scanner"},
		{Label: LabelUserInput, Match: "bufio.Reader"},
		{Label: LabelUserInput, Match: "mime/multipart"},
		{Label: LabelUserInput, Match: "gorilla/mux"},
		{Label: LabelUserInput, Match: "gin.Context"},
		{Label: LabelUserInput, Match: "echo.Context"},
		{Label: LabelUserInput, Match: "fiber.Ctx"},
		{Label: LabelUserInput, Match: "chi."},
		{Label: LabelConfig, Match: "os.Getenv"},
		{Label: LabelConfig, Match: "os.LookupEnv"},
		{Label: LabelConfig, Match: "os.Environ"},
		{Label: LabelSecret, Match: "crypto/tls"},
	}
}

// DefaultSinks returns built-in sensitive sinks (D-015-02 broad lists).
func DefaultSinks() []SinkSpec {
	return []SinkSpec{
		{Name: "log", Match: "log.Print"},
		{Name: "log", Match: "log.Fatal"},
		{Name: "log", Match: "log.Panic"},
		{Name: "log", Match: "log.Output"},
		{Name: "log", Match: "fmt.Print"},
		{Name: "log", Match: "fmt.Fprintf"},
		{Name: "log", Match: "(*log.Logger)"},
		{Name: "log", Match: "slog."},
		{Name: "log", Match: "zap."},
		{Name: "exec", Match: "os/exec"},
		{Name: "exec", Match: "exec.Command"},
		{Name: "exec", Match: "exec.CommandContext"},
		{Name: "exec", Match: "syscall.Exec"},
		{Name: "exec", Match: "syscall.StartProcess"},
		{Name: "eval", Match: "text/template"},
		{Name: "eval", Match: "html/template"},
		{Name: "eval", Match: "template.Execute"},
		{Name: "eval", Match: "vm.Program"},
		{Name: "network", Match: "http.Post"},
		{Name: "network", Match: "http.Get"},
		{Name: "network", Match: "http.Client"},
		{Name: "network", Match: "http.NewRequest"},
		{Name: "network", Match: "net.Dial"},
		{Name: "network", Match: "net.DialTimeout"},
		{Name: "network", Match: "websocket"},
		{Name: "network", Match: "grpc."},
		{Name: "db", Match: "database/sql"},
		{Name: "db", Match: "sql.DB"},
		{Name: "db", Match: "sql.Tx"},
		{Name: "db", Match: "ExecContext"},
		{Name: "db", Match: "QueryContext"},
		{Name: "db", Match: "QueryRow"},
		{Name: "db", Match: "gorm."},
		{Name: "db", Match: "mongo."},
		{Name: "fs", Match: "os.WriteFile"},
		{Name: "fs", Match: "os.Create"},
		{Name: "fs", Match: "os.OpenFile"},
		{Name: "fs", Match: "ioutil.WriteFile"},
		{Name: "crypto", Match: "crypto/rsa"},
		{Name: "crypto", Match: "crypto/aes"},
		{Name: "crypto", Match: "cipher."},
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
