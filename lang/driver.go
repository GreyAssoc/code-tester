package lang

import "gointegrity/report"

// ID is a supported analysis language.
type ID string

const (
	Go     ID = "go"
	Python ID = "python"
	JS     ID = "js" // JS, JSX, TS, TSX
	CSS    ID = "css"
)

// Options passed from the assessor to a language driver.
type Options struct {
	RootDir   string
	Paths     []string // files or dirs scoped to this language (may be empty → driver discovers)
	Exclude   []string
	Flow      bool
	FailOn    string
	Extra     map[string]string
}

// Driver is the per-language analyzer contract.
type Driver interface {
	ID() ID
	// Detect reports whether this language is present under root and returns candidate paths.
	Detect(root string) (found bool, paths []string)
	// Crawl runs language-specific integrity analysis.
	Crawl(opts Options) (*report.Report, error)
	Rules() []report.RuleInfo
}
