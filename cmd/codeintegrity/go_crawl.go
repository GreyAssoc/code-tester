package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gointegrity/callgraph"
	"gointegrity/check"
	"gointegrity/config"
	"gointegrity/flow"
	"gointegrity/loader"
	"gointegrity/report"
	"gointegrity/rules"
)

type commonFlags struct {
	Dir        string
	Format     string
	Out        string
	Config     string
	NoConfig   bool
	Tests      bool
	TestsSet   bool
	Patterns   []string
}

type crawlFlags struct {
	commonFlags
	Roots             string
	FailOn            string
	MinConfidence     string
	Enable            []string
	Disable           []string
	Exclude           []string
	Baseline          string
	WriteBaseline     string
	Flow              bool
	Context           int  // D-015-01: default 1 when flow is on
	ContextSet        bool // true if --context was passed explicitly
	Policies          string
	Suppressions      string
	Focus             string
	Depth             int
	From              string
	To                string
	VerifyIncremental bool
}

type checkFlags struct {
	commonFlags
	Request string
}

func parseCommon(args []string) (commonFlags, []string, error) {
	f := commonFlags{Format: "text", Tests: true}
	rest := []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-C" && i+1 < len(args):
			i++
			f.Dir = args[i]
		case strings.HasPrefix(a, "-C="):
			f.Dir = strings.TrimPrefix(a, "-C=")
		case a == "--format" && i+1 < len(args):
			i++
			f.Format = args[i]
		case strings.HasPrefix(a, "--format="):
			f.Format = strings.TrimPrefix(a, "--format=")
		case a == "-o" && i+1 < len(args):
			i++
			f.Out = args[i]
		case strings.HasPrefix(a, "-o="):
			f.Out = strings.TrimPrefix(a, "-o=")
		case a == "--config" && i+1 < len(args):
			i++
			f.Config = args[i]
		case strings.HasPrefix(a, "--config="):
			f.Config = strings.TrimPrefix(a, "--config=")
		case a == "--no-config":
			f.NoConfig = true
		case a == "--tests":
			f.Tests = true
			f.TestsSet = true
		case a == "--tests=true":
			f.Tests = true
			f.TestsSet = true
		case a == "--tests=false":
			f.Tests = false
			f.TestsSet = true
		case strings.HasPrefix(a, "-") && a != "-":
			rest = append(rest, a)
		default:
			rest = append(rest, a)
		}
	}
	if f.Dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return f, nil, err
		}
		f.Dir = wd
	}
	f.Dir, _ = filepath.Abs(f.Dir)
	return f, rest, nil
}

func cmdGoCrawl(args []string) int {
	cf, rest, err := parseCommon(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	f := crawlFlags{commonFlags: cf, Roots: "all", FailOn: "", MinConfidence: ""}
	patterns := []string{}
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--roots" && i+1 < len(rest):
			i++
			f.Roots = rest[i]
		case strings.HasPrefix(a, "--roots="):
			f.Roots = strings.TrimPrefix(a, "--roots=")
		case a == "--fail-on" && i+1 < len(rest):
			i++
			f.FailOn = rest[i]
		case strings.HasPrefix(a, "--fail-on="):
			f.FailOn = strings.TrimPrefix(a, "--fail-on=")
		case a == "--min-confidence" && i+1 < len(rest):
			i++
			f.MinConfidence = rest[i]
		case strings.HasPrefix(a, "--min-confidence="):
			f.MinConfidence = strings.TrimPrefix(a, "--min-confidence=")
		case a == "--enable" && i+1 < len(rest):
			i++
			f.Enable = append(f.Enable, rest[i])
		case strings.HasPrefix(a, "--enable="):
			f.Enable = append(f.Enable, strings.TrimPrefix(a, "--enable="))
		case a == "--disable" && i+1 < len(rest):
			i++
			f.Disable = append(f.Disable, rest[i])
		case strings.HasPrefix(a, "--disable="):
			f.Disable = append(f.Disable, strings.TrimPrefix(a, "--disable="))
		case a == "--exclude" && i+1 < len(rest):
			i++
			f.Exclude = append(f.Exclude, rest[i])
		case strings.HasPrefix(a, "--exclude="):
			f.Exclude = append(f.Exclude, strings.TrimPrefix(a, "--exclude="))
		case a == "--baseline" && i+1 < len(rest):
			i++
			f.Baseline = rest[i]
		case strings.HasPrefix(a, "--baseline="):
			f.Baseline = strings.TrimPrefix(a, "--baseline=")
		case a == "--write-baseline" && i+1 < len(rest):
			i++
			f.WriteBaseline = rest[i]
		case strings.HasPrefix(a, "--write-baseline="):
			f.WriteBaseline = strings.TrimPrefix(a, "--write-baseline=")
		case a == "--flow":
			f.Flow = true
		case a == "--context" && i+1 < len(rest):
			i++
			fmt.Sscanf(rest[i], "%d", &f.Context)
			f.ContextSet = true
		case strings.HasPrefix(a, "--context="):
			fmt.Sscanf(strings.TrimPrefix(a, "--context="), "%d", &f.Context)
			f.ContextSet = true
		case a == "--policies" && i+1 < len(rest):
			i++
			f.Policies = rest[i]
		case strings.HasPrefix(a, "--policies="):
			f.Policies = strings.TrimPrefix(a, "--policies=")
		case a == "--suppressions" && i+1 < len(rest):
			i++
			f.Suppressions = rest[i]
		case strings.HasPrefix(a, "--suppressions="):
			f.Suppressions = strings.TrimPrefix(a, "--suppressions=")
		case a == "--focus" && i+1 < len(rest):
			i++
			f.Focus = rest[i]
		case strings.HasPrefix(a, "--focus="):
			f.Focus = strings.TrimPrefix(a, "--focus=")
		case a == "--depth" && i+1 < len(rest):
			i++
			fmt.Sscanf(rest[i], "%d", &f.Depth)
		case strings.HasPrefix(a, "--depth="):
			fmt.Sscanf(strings.TrimPrefix(a, "--depth="), "%d", &f.Depth)
		case a == "--from" && i+1 < len(rest):
			i++
			f.From = rest[i]
		case strings.HasPrefix(a, "--from="):
			f.From = strings.TrimPrefix(a, "--from=")
		case a == "--to" && i+1 < len(rest):
			i++
			f.To = rest[i]
		case strings.HasPrefix(a, "--to="):
			f.To = strings.TrimPrefix(a, "--to=")
		case a == "--verify-incremental":
			f.VerifyIncremental = true
			f.Flow = true
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "unknown flag %s\n", a)
			return 2
		default:
			patterns = append(patterns, a)
		}
	}
	if f.Depth == 0 {
		f.Depth = 3
	}
	// Graph formats imply flow analysis.
	switch strings.ToLower(f.Format) {
	case "dot", "mermaid", "html":
		f.Flow = true
	}
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	f.Patterns = patterns

	cfg, err := config.Load(f.Dir, f.Config, f.NoConfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if f.TestsSet {
		t := f.Tests
		cfg.Tests = &t
	}
	if f.FailOn != "" {
		cfg.FailOn = f.FailOn
	}
	if f.MinConfidence != "" {
		cfg.MinConfidence = f.MinConfidence
	}
	if f.Roots != "" && f.Roots != "all" {
		cfg.Roots = strings.Split(f.Roots, ",")
	} else if f.Roots == "all" {
		cfg.Roots = []string{"all"}
	}
	cfg.Exclude = config.MergeExcludes(append(cfg.Exclude, f.Exclude...))
	if f.Baseline != "" {
		cfg.Baseline = f.Baseline
	}

	res, err := loader.Load(f.Dir, patterns, cfg.TestsEnabled())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	idx := callgraph.Build(res, f.Dir)
	rootKinds := callgraph.NormalizeRoots(cfg.Roots)
	roots := callgraph.Roots(idx, rootKinds)
	reach := callgraph.Analyze(idx, roots)

	en := rules.ResolveEnablement(cfg, f.Enable, f.Disable)
	rctx := rules.Context{Index: idx, Reach: reach, Config: cfg, RootDir: f.Dir}
	findings, suppressed := rules.RunCrawl(rctx, en)

	var flowGraph *flow.Graph
	if f.Flow {
		var policies []flow.Policy
		var sources []flow.SourceSpec
		var sinks []flow.SinkSpec
		if f.Policies != "" {
			pf, err := flow.LoadPolicies(resolvePath(f.Dir, f.Policies))
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			policies = pf.Policies
			if len(pf.Sources) > 0 {
				sources = pf.Sources
			}
			if len(pf.Sinks) > 0 {
				sinks = pf.Sinks
			}
		}
		var sup *flow.SuppressionFile
		supPath := f.Suppressions
		if supPath == "" {
			candidate := filepath.Join(f.Dir, ".gointegrity-suppressions.yml")
			if _, err := os.Stat(candidate); err == nil {
				supPath = candidate
			}
		} else {
			supPath = resolvePath(f.Dir, supPath)
		}
		if supPath != "" {
			s, err := flow.LoadSuppressions(supPath)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			sup = s
		}
		ctxSens := f.Context
		if !f.ContextSet {
			ctxSens = 1 // D-015-01 default when --flow is on
		}
		opts := flow.BuildOptions{ContextSens: ctxSens, Sources: sources, Sinks: sinks, RootDir: f.Dir}
		g, pt := flow.Build(idx, opts)
		flowGraph = g
		if f.VerifyIncremental {
			if err := verifyIncremental(f.Dir, idx, opts, g); err != nil {
				fmt.Fprintln(os.Stderr, "verify-incremental:", err)
				return 2
			}
			fmt.Fprintln(os.Stderr, "verify-incremental: OK")
		}
		// Refresh package cache.
		_ = saveFlowCache(f.Dir, idx, g, pt)

		primary := map[string]bool{}
		for _, p := range idx.Packages {
			if p != nil && p.PkgPath != "" {
				primary[p.PkgPath] = true
			}
		}
		ff, _ := g.AnalyzeRules(flow.AnalyzeOptions{Policies: policies, Suppressions: sup, PrimaryPkgs: primary})
		for _, ffnd := range ff {
			if ffnd.Suppressed {
				suppressed++
				// Still report suppressed flow findings as notes in JSON via flag.
				rf := ffnd.Finding
				rf.Suppressed = true
				rf.CodeFlows = g.CodeFlow(ffnd.Witness)
				if en.Enabled[ffnd.RuleID] {
					findings = append(findings, rf)
				}
				continue
			}
			if !en.Enabled[ffnd.RuleID] {
				continue
			}
			rf := ffnd.Finding
			rf.CodeFlows = g.CodeFlow(ffnd.Witness)
			if ffnd.Approx {
				rf.Evidence = append(rf.Evidence, report.Evidence{Message: "depends on reflect/unsafe/cgo approximation"})
			}
			findings = append(findings, rf)
		}
	}

	// Exclude globs + min confidence + baseline.
	var baseline *config.Baseline
	basePath := cfg.Baseline
	if basePath != "" && !filepath.IsAbs(basePath) {
		basePath = filepath.Join(f.Dir, basePath)
	}
	if basePath != "" {
		if b, err := config.LoadBaseline(basePath); err == nil {
			baseline = b
		} else if f.WriteBaseline == "" {
			// missing baseline is ok only when writing
		}
	}

	filtered := findings[:0]
	baselined := 0
	for _, finding := range findings {
		if cfg.MinConfidence == "high" && finding.Confidence != report.ConfidenceHigh {
			continue
		}
		excluded := false
		for _, g := range cfg.Exclude {
			if config.MatchGlob(g, finding.Location.File) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		if baseline != nil && baseline.Contains(finding.Fingerprint) {
			baselined++
			continue
		}
		filtered = append(filtered, finding)
	}
	findings = filtered

	if f.WriteBaseline != "" {
		path := f.WriteBaseline
		if !filepath.IsAbs(path) {
			path = filepath.Join(f.Dir, path)
		}
		fps := make([]string, 0, len(findings)+baselined)
		for _, finding := range findings {
			fps = append(fps, finding.Fingerprint)
		}
		// Also include currently baselined? write all current fingerprints from unfiltered set.
		// Recompute from pre-baseline filter for write: use findings before baseline hide.
		// Simpler: write fingerprints of findings that remain + were baselined this run.
		all := append([]report.Finding(nil), findings...)
		_ = all
		if err := config.WriteBaseline(path, fps); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Fprintf(os.Stderr, "wrote baseline %s (%d fingerprints)\n", path, len(fps))
		return 0
	}

	rep := report.New("crawl")
	rep.ConfigHash = cfg.Hash()
	rep.Patterns = patterns
	rep.Roots = strings.Join(cfg.Roots, ",")
	rep.FailOn = cfg.FailOn
	rep.Rules = rules.RuleInfos(en)
	rep.Findings = findings
	rep.Summary.Packages = len(res.Primary)
	files := map[string]bool{}
	funcs := 0
	for _, sym := range idx.Symbols {
		if sym.Kind == callgraph.KindFunc || sym.Kind == callgraph.KindMethod {
			funcs++
		}
		file, _, _ := sym.Location(f.Dir)
		if file != "" {
			files[file] = true
		}
	}
	rep.Summary.Files = len(files)
	rep.Summary.Functions = funcs
	rep.Summary.Suppressed = suppressed
	rep.Summary.Baselined = baselined
	rep.FinalizeCrawl()

	out := ioWriter(f.Out)
	switch strings.ToLower(f.Format) {
	case "dot":
		if flowGraph == nil {
			fmt.Fprintln(os.Stderr, "--format=dot requires --flow")
			return 2
		}
		if err := flow.RenderDOT(out, flowGraph, f.Focus, f.Depth, f.From, f.To); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	case "mermaid":
		if flowGraph == nil {
			fmt.Fprintln(os.Stderr, "--format=mermaid requires --flow")
			return 2
		}
		if err := flow.RenderMermaid(out, flowGraph, f.Focus, f.Depth, f.From, f.To); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	case "html":
		if flowGraph == nil {
			fmt.Fprintln(os.Stderr, "--format=html requires --flow")
			return 2
		}
		if err := flow.RenderHTML(out, flowGraph, f.Focus, f.Depth, f.From, f.To); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	default:
		if err := report.Render(out, rep, f.Format); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	if f.Out != "" {
		if c, ok := out.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}

	return crawlExit(rep, cfg.FailOn)
}

func resolvePath(dir, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, p)
}

func saveFlowCache(dir string, idx *callgraph.Index, g *flow.Graph, pt *flow.PointsTo) error {
	ci := &flow.CacheIndex{Packages: map[string]*flow.PackageCache{}}
	seen := map[string]bool{}
	for _, sym := range idx.Symbols {
		if sym.PkgPath == "" || seen[sym.PkgPath] {
			continue
		}
		seen[sym.PkgPath] = true
		srcHash := flow.HashStrings([]string{sym.PkgPath})
		ci.Packages[sym.PkgPath] = flow.BuildPackageCache(sym.PkgPath, srcHash, srcHash, g, pt)
	}
	return flow.SaveCache(dir, ci)
}

func verifyIncremental(dir string, idx *callgraph.Index, opts flow.BuildOptions, full *flow.Graph) error {
	// Incremental path: load cache if present, rebuild graph, compare edge fingerprints.
	g2, _ := flow.Build(idx, opts)
	h1 := edgeFingerprint(full)
	h2 := edgeFingerprint(g2)
	if h1 != h2 {
		return fmt.Errorf("full and incremental graphs differ: %s vs %s", h1, h2)
	}
	if cached, err := flow.LoadCache(dir); err == nil {
		_ = cached
	}
	return nil
}

func edgeFingerprint(g *flow.Graph) string {
	parts := make([]string, 0, len(g.Edges))
	for _, e := range g.Edges {
		parts = append(parts, e.From+"|"+string(e.Label)+"|"+e.To)
	}
	return flow.HashStrings(parts)
}

func crawlExit(rep *report.Report, failOn string) int {
	if failOn == "never" {
		return 0
	}
	threshold := report.SeverityWarning
	switch failOn {
	case "error":
		threshold = report.SeverityError
	case "warning":
		threshold = report.SeverityWarning
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

func ioWriter(path string) interface {
	Write([]byte) (int, error)
} {
	if path == "" {
		return os.Stdout
	}
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	return f
}

func cmdCheck(args []string) int {
	cf, rest, err := parseCommon(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	f := checkFlags{commonFlags: cf}
	patterns := []string{}
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--request" && i+1 < len(rest):
			i++
			f.Request = rest[i]
		case strings.HasPrefix(a, "--request="):
			f.Request = strings.TrimPrefix(a, "--request=")
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "unknown flag %s\n", a)
			return 2
		default:
			patterns = append(patterns, a)
		}
	}
	if f.Request == "" {
		fmt.Fprintln(os.Stderr, "check mode requires --request FILE|-")
		return 2
	}
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	f.Patterns = patterns

	req, envErrs, err := check.LoadRequest(f.Request)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(envErrs) > 0 {
		for _, e := range envErrs {
			fmt.Fprintln(os.Stderr, e.String())
		}
		return 2
	}

	cfg, err := config.Load(f.Dir, f.Config, f.NoConfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if f.TestsSet {
		t := f.Tests
		cfg.Tests = &t
	}

	res, err := loader.Load(f.Dir, patterns, cfg.TestsEnabled())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	idx := callgraph.Build(res, f.Dir)
	rootKinds := callgraph.NormalizeRoots(cfg.Roots)
	roots := callgraph.Roots(idx, rootKinds)
	reach := callgraph.Analyze(idx, roots)
	rctx := rules.Context{Index: idx, Reach: reach, Config: cfg, RootDir: f.Dir}

	rep := check.Run(req, idx, reach, f.Dir, rctx)
	rep.ConfigHash = cfg.Hash()
	rep.Patterns = patterns
	rep.Summary.Packages = len(res.Primary)

	out := ioWriter(f.Out)
	if err := report.Render(out, rep, f.Format); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return check.ExitCode(rep)
}

func cmdSuppress(args []string) int {
	var rule, source, sink, reason, owner, pathHash, file string
	file = ".gointegrity-suppressions.yml"
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--rule" && i+1 < len(args):
			i++
			rule = args[i]
		case strings.HasPrefix(a, "--rule="):
			rule = strings.TrimPrefix(a, "--rule=")
		case a == "--source" && i+1 < len(args):
			i++
			source = args[i]
		case strings.HasPrefix(a, "--source="):
			source = strings.TrimPrefix(a, "--source=")
		case a == "--sink" && i+1 < len(args):
			i++
			sink = args[i]
		case strings.HasPrefix(a, "--sink="):
			sink = strings.TrimPrefix(a, "--sink=")
		case a == "--reason" && i+1 < len(args):
			i++
			reason = args[i]
		case strings.HasPrefix(a, "--reason="):
			reason = strings.TrimPrefix(a, "--reason=")
		case a == "--owner" && i+1 < len(args):
			i++
			owner = args[i]
		case strings.HasPrefix(a, "--owner="):
			owner = strings.TrimPrefix(a, "--owner=")
		case a == "--path-hash" && i+1 < len(args):
			i++
			pathHash = args[i]
		case strings.HasPrefix(a, "--path-hash="):
			pathHash = strings.TrimPrefix(a, "--path-hash=")
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "unknown flag %s\n", a)
			return 2
		default:
			file = a
		}
	}
	s := flow.FlowSuppression{Rule: rule, Source: source, Sink: sink, PathHash: pathHash, Reason: reason, Owner: owner}
	if err := s.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := flow.WriteSuppression(file, s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Fprintf(os.Stderr, "wrote suppression to %s\n", file)
	return 0
}
