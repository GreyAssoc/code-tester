package check

import (
	"github.com/GreyAssoc/code-tester/callgraph"
	"github.com/GreyAssoc/code-tester/report"
	"github.com/GreyAssoc/code-tester/rules"
)

func Run(req *Request, idx *callgraph.Index, reach *callgraph.Reachability, rootDir string, rctx rules.Context) *report.Report {
	rep := report.New("check")
	rep.RequestID = req.RequestID
	rep.RequestHash = req.Hash()
	eng := &Engine{Index: idx, Reach: reach, RootDir: rootDir, Config: rctx}
	for _, c := range req.Checks {
		rep.Checks = append(rep.Checks, eng.RunCheck(c))
	}
	rep.FinalizeCheck()
	return rep
}

func ExitCode(rep *report.Report) int {
	for _, c := range rep.Checks {
		if c.Status == "error" {
			return 2
		}
	}
	for _, c := range rep.Checks {
		if c.Status == "fail" || c.Status == "unresolved" {
			return 1
		}
	}
	return 0
}
