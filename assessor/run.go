package assessor

import (
	"fmt"
	"sort"

	"gointegrity/config"
	"gointegrity/lang"
	langcss "gointegrity/lang/css"
	langgo "gointegrity/lang/go"
	langjs "gointegrity/lang/js"
	langpy "gointegrity/lang/python"
	"gointegrity/report"
)

// Registry returns the built-in language drivers.
func Registry() map[lang.ID]lang.Driver {
	return map[lang.ID]lang.Driver{
		lang.Go:     langgo.Driver{},
		lang.Python: langpy.Driver{},
		lang.JS:     langjs.Driver{},
		lang.CSS:    langcss.Driver{},
	}
}

// RunOptions configures a multi-language crawl.
type RunOptions struct {
	RootDir   string
	Languages []lang.ID // empty = auto from detection
	Exclude   []string
	Flow      bool
	FailOn    string
}

// Run detects languages, dispatches drivers, and merges reports.
func Run(opts RunOptions) (*report.Report, *Detection, error) {
	opts.Exclude = config.MergeExcludes(opts.Exclude)
	det, err := Detect(opts.RootDir, opts.Exclude...)
	if err != nil {
		return nil, nil, err
	}
	wanted := opts.Languages
	if len(wanted) == 0 {
		for id := range det.Languages {
			wanted = append(wanted, id)
		}
		sort.Slice(wanted, func(i, j int) bool { return wanted[i] < wanted[j] })
	}
	reg := Registry()
	merged := report.New("crawl")
	merged.Tool.Name = "codeintegrity"
	merged.Summary.ByLanguage = map[string]int{}
	merged.Summary.BySeverity = map[string]int{}

	for _, id := range wanted {
		drv, ok := reg[id]
		if !ok {
			return nil, det, fmt.Errorf("unsupported language %q", id)
		}
		paths := config.FilterPaths(det.Languages[id], opts.Exclude)
		// Always try Go if requested and go.mod exists even with zero listed paths yet.
		if id != lang.Go && len(paths) == 0 {
			found, p := drv.Detect(opts.RootDir)
			if !found {
				continue
			}
			paths = config.FilterPaths(p, opts.Exclude)
			if len(paths) == 0 {
				continue
			}
		}
		if id == lang.Go {
			found, _ := drv.Detect(opts.RootDir)
			if !found && len(paths) == 0 {
				continue
			}
		} else if len(paths) == 0 {
			continue
		}
		rep, err := drv.Crawl(lang.Options{
			RootDir: opts.RootDir,
			Paths:   paths,
			Exclude: opts.Exclude,
			Flow:    opts.Flow && id == lang.Go,
			FailOn:  opts.FailOn,
		})
		if err != nil {
			return nil, det, fmt.Errorf("%s: %w", id, err)
		}
		if rep == nil {
			continue
		}
		for _, f := range rep.Findings {
			if config.Excluded(f.Location.File, opts.Exclude) {
				continue
			}
			if f.Language == "" {
				f.Language = string(id)
			}
			merged.Findings = append(merged.Findings, f)
		}
		for _, r := range rep.Rules {
			r.ID = string(id) + ":" + r.ID
			merged.Rules = append(merged.Rules, r)
		}
		merged.Summary.Packages += rep.Summary.Packages
		merged.Summary.Files += rep.Summary.Files
		merged.Summary.Functions += rep.Summary.Functions
		merged.Summary.Suppressed += rep.Summary.Suppressed
		merged.Summary.Baselined += rep.Summary.Baselined
	}
	merged.FinalizeCrawl()
	return merged, det, nil
}
