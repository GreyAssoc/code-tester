package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GreyAssoc/code-tester/assessor"
	"github.com/GreyAssoc/code-tester/config"
	"github.com/GreyAssoc/code-tester/lang"
	"github.com/GreyAssoc/code-tester/report"
	"github.com/GreyAssoc/code-tester/schemas"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		return cmdCrawl(nil)
	}
	switch args[0] {
	case "crawl":
		return cmdCrawl(args[1:])
	case "check":
		return cmdCheck(args[1:])
	case "rules":
		return cmdRules(args[1:])
	case "schema":
		return cmdSchema(args[1:])
	case "suppress":
		return cmdSuppress(args[1:])
	case "languages", "langs":
		fmt.Println("go")
		fmt.Println("python")
		fmt.Println("js")
		fmt.Println("css")
		return 0
	case "version", "--version":
		fmt.Println(report.ToolVersion)
		return 0
	case "help", "-h", "--help":
		printHelp(os.Stdout)
		return 0
	default:
		return cmdCrawl(args)
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintf(w, `codeintegrity — multi-language code integrity assessor

Usage:
  codeintegrity [crawl] [flags] [path]              multi-language crawl (default)
  codeintegrity [crawl] [go-flags] [packages]       deep Go crawl (package patterns / Go flags)
  codeintegrity check --request FILE|- [flags] [packages]
  codeintegrity rules [--lang go|python|js|css]
  codeintegrity schema request|report
  codeintegrity suppress --rule ID --source SRC --sink SNK --reason TEXT --owner WHO [--path-hash H] [file]
  codeintegrity languages
  codeintegrity version

Multi-language flags:
  -C dir              root directory (default .)
  --lang list         auto (default) or go,python,js,css
  --format text|json|sarif
  -o file             write report to file
  --flow              enable Go flow analysis when Go is selected
  --fail-on level     error|warning|note|never (default warning)
  --exclude glob      extra exclude glob (repeatable; defaults always include
                      testdata/**, node_modules/**, vendor/**, .git/**, dist/**, build/**)

Go deep crawl flags (also used by check):
  --config file       config path (default .gointegrity.yml)
  --no-config         ignore config file
  --tests=true|false  load _test.go (default true)
  --roots list        main,init,exported,tests,... or all
  --min-confidence level
  --enable/--disable sel
  --baseline / --write-baseline file
  --flow / --context=0|1 (default 1) / --policies file / --suppressions file
  --focus symbol / --depth N / --from/--to node
  --verify-incremental
  --format text|json|sarif|dot|mermaid|html
`)
}

// cmdCrawl routes to the deep Go engine when package patterns or Go-only flags
// are present; otherwise runs the multi-language assessor.
func cmdCrawl(args []string) int {
	if wantsGoEngine(args) {
		return cmdGoCrawl(args)
	}
	return cmdMultiCrawl(args)
}

func wantsGoEngine(args []string) bool {
	lang := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--lang" && i+1 < len(args):
			i++
			lang = args[i]
		case strings.HasPrefix(a, "--lang="):
			lang = strings.TrimPrefix(a, "--lang=")
		case a == "--no-config", a == "--roots", a == "--config", a == "--tests",
			a == "--tests=true", a == "--tests=false", a == "--enable", a == "--disable",
			a == "--baseline", a == "--write-baseline", a == "--min-confidence",
			a == "--policies", a == "--suppressions", a == "--focus", a == "--depth",
			a == "--from", a == "--to", a == "--context", a == "--verify-incremental":
			return true
		case strings.HasPrefix(a, "--roots="), strings.HasPrefix(a, "--config="),
			strings.HasPrefix(a, "--enable="), strings.HasPrefix(a, "--disable="),
			strings.HasPrefix(a, "--baseline="), strings.HasPrefix(a, "--write-baseline="),
			strings.HasPrefix(a, "--min-confidence="), strings.HasPrefix(a, "--policies="),
			strings.HasPrefix(a, "--suppressions="), strings.HasPrefix(a, "--focus="),
			strings.HasPrefix(a, "--depth="), strings.HasPrefix(a, "--from="),
			strings.HasPrefix(a, "--to="), strings.HasPrefix(a, "--context="):
			return true
		case a == "--format" && i+1 < len(args):
			i++
			switch strings.ToLower(args[i]) {
			case "dot", "mermaid", "html":
				return true
			}
		case strings.HasPrefix(a, "--format="):
			switch strings.ToLower(strings.TrimPrefix(a, "--format=")) {
			case "dot", "mermaid", "html":
				return true
			}
		case strings.HasPrefix(a, "-"):
			continue
		default:
			if a == "./..." || strings.HasSuffix(a, "/...") || strings.HasPrefix(a, "./") {
				return true
			}
		}
	}
	if lang == "go" {
		// Explicit Go-only with no multi path — still allow assessor for simple trees;
		// deep flags already returned true above.
		return false
	}
	return false
}

func cmdMultiCrawl(args []string) int {
	dir, _ := os.Getwd()
	format := "text"
	outPath := ""
	langList := "auto"
	flowOn := false
	failOn := "warning"
	var exclude []string
	var paths []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-C" && i+1 < len(args):
			i++
			dir = args[i]
		case strings.HasPrefix(a, "-C="):
			dir = strings.TrimPrefix(a, "-C=")
		case a == "--lang" && i+1 < len(args):
			i++
			langList = args[i]
		case strings.HasPrefix(a, "--lang="):
			langList = strings.TrimPrefix(a, "--lang=")
		case a == "--format" && i+1 < len(args):
			i++
			format = args[i]
		case strings.HasPrefix(a, "--format="):
			format = strings.TrimPrefix(a, "--format=")
		case a == "-o" && i+1 < len(args):
			i++
			outPath = args[i]
		case strings.HasPrefix(a, "-o="):
			outPath = strings.TrimPrefix(a, "-o=")
		case a == "--flow":
			flowOn = true
		case a == "--fail-on" && i+1 < len(args):
			i++
			failOn = args[i]
		case strings.HasPrefix(a, "--fail-on="):
			failOn = strings.TrimPrefix(a, "--fail-on=")
		case a == "--exclude" && i+1 < len(args):
			i++
			exclude = append(exclude, args[i])
		case strings.HasPrefix(a, "--exclude="):
			exclude = append(exclude, strings.TrimPrefix(a, "--exclude="))
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "unknown flag %s\n", a)
			return 2
		default:
			paths = append(paths, a)
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(paths) == 1 && !strings.HasPrefix(paths[0], "-") {
		p := paths[0]
		if !filepath.IsAbs(p) {
			p = filepath.Join(abs, p)
		}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			abs = p
		}
	}

	exclude = config.MergeExcludes(exclude)
	before, _ := assessor.Detect(abs)
	langs := assessor.ParseLangList(langList)
	rep, det, err := assessor.Run(assessor.RunOptions{
		RootDir:   abs,
		Languages: langs,
		Exclude:   exclude,
		Flow:      flowOn,
		FailOn:    failOn,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if n := detectPathCount(before) - detectPathCount(det); n > 0 {
		fmt.Fprintf(os.Stderr, "excluded: %d path(s) (%s)\n", n, strings.Join(exclude, ", "))
	}
	if det != nil && format == "text" {
		var found []string
		for id, files := range det.Languages {
			found = append(found, fmt.Sprintf("%s=%d", id, len(files)))
		}
		if len(found) > 0 {
			fmt.Fprintf(os.Stderr, "detected: %s\n", strings.Join(found, " "))
		}
	}

	out := os.Stdout
	if outPath != "" {
		f, err := os.Create(outPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		defer f.Close()
		out = f
	}
	if err := report.Render(out, rep, format); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return exitCode(rep, failOn)
}

func detectPathCount(d *assessor.Detection) int {
	if d == nil {
		return 0
	}
	n := 0
	for _, files := range d.Languages {
		n += len(files)
	}
	return n
}

func exitCode(rep *report.Report, failOn string) int {
	if failOn == "never" {
		return 0
	}
	threshold := report.SeverityWarning
	switch failOn {
	case "error":
		threshold = report.SeverityError
	case "note":
		threshold = report.SeverityNote
	}
	for _, f := range rep.Findings {
		if report.SeverityRank(f.Severity) >= report.SeverityRank(threshold) {
			return 1
		}
	}
	return 0
}

func cmdRules(args []string) int {
	filter := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--lang" && i+1 < len(args):
			i++
			filter = strings.ToLower(args[i])
		case strings.HasPrefix(a, "--lang="):
			filter = strings.ToLower(strings.TrimPrefix(a, "--lang="))
		case a == "-h", a == "--help":
			fmt.Println("usage: codeintegrity rules [--lang go|python|js|css]")
			return 0
		default:
			fmt.Fprintf(os.Stderr, "unknown argument %s\n", a)
			return 2
		}
	}
	reg := assessor.Registry()
	ids := make([]string, 0, len(reg))
	for id := range reg {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	fmt.Printf("%-8s %-10s %-28s %-12s %s\n", "LANG", "ID", "NAME", "SEVERITY", "DESCRIPTION")
	for _, id := range ids {
		if filter != "" && filter != id && !(filter == "py" && id == "python") &&
			!(filter == "javascript" && id == "js") && !(filter == "ts" && id == "js") {
			continue
		}
		drv := reg[lang.ID(id)]
		for _, r := range drv.Rules() {
			fmt.Printf("%-8s %-10s %-28s %-12s %s\n", id, r.ID, r.Name, r.Severity, r.Description)
		}
	}
	return 0
}

func cmdSchema(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: codeintegrity schema request|report")
		return 2
	}
	switch args[0] {
	case "request":
		fmt.Print(schemas.CheckRequestV1)
		if !strings.HasSuffix(schemas.CheckRequestV1, "\n") {
			fmt.Println()
		}
	case "report":
		fmt.Print(schemas.ReportV1)
		if !strings.HasSuffix(schemas.ReportV1, "\n") {
			fmt.Println()
		}
	default:
		fmt.Fprintln(os.Stderr, "usage: codeintegrity schema request|report")
		return 2
	}
	return 0
}
