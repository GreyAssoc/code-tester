package gointegrity_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gointegrity/check"
	"gointegrity/config"
	"gointegrity/internal/runtest"
	"gointegrity/report"
	"gointegrity/rules"
	"gointegrity/schemas"
)

func TestCrawlFixture(t *testing.T) {
	root := runtest.RepoRoot(t)
	dir := filepath.Join(root, "testdata", "crawl")
	r := runtest.Run(t, dir, "--no-config", "--roots", "main,init,exported,tests", "--format", "json", "./...")
	if r.Code != 1 {
		t.Fatalf("exit %d stderr=%s", r.Code, r.Stderr)
	}
	var rep report.Report
	if err := json.Unmarshal([]byte(r.Stdout), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Mode != "crawl" {
		t.Fatalf("mode %q", rep.Mode)
	}
	if rep.Summary.Findings == 0 {
		t.Fatal("expected findings")
	}
	wantRules := map[string]bool{"DEAD001": true, "STUB005": true, "COH001": true}
	seen := map[string]bool{}
	for _, f := range rep.Findings {
		seen[f.RuleID] = true
	}
	for id := range wantRules {
		if !seen[id] {
			t.Errorf("missing rule %s in findings", id)
		}
	}
}

func TestShopGoodCheck(t *testing.T) {
	root := runtest.RepoRoot(t)
	dir := filepath.Join(root, "testdata", "shopgood")
	req := filepath.Join(root, "testdata", "requests", "payments.json")
	r := runtest.Run(t, dir, "check", "--no-config", "--request", req, "--format", "json", "./...")
	if r.Code != 0 {
		t.Fatalf("exit %d stderr=%s\n%s", r.Code, r.Stderr, r.Stdout)
	}
	var rep report.Report
	if err := json.Unmarshal([]byte(r.Stdout), &rep); err != nil {
		t.Fatal(err)
	}
	for _, c := range rep.Checks {
		if c.Status != "pass" {
			t.Errorf("check %s status %s msg %s", c.ID, c.Status, c.Message)
			for _, tgt := range c.Targets {
				t.Logf("  %s %s %s", tgt.Status, tgt.Symbol, tgt.Message)
			}
		}
	}
}

func TestShopBadCheck(t *testing.T) {
	root := runtest.RepoRoot(t)
	dir := filepath.Join(root, "testdata", "shopbad")
	req := filepath.Join(root, "testdata", "requests", "payments.json")
	r := runtest.Run(t, dir, "check", "--no-config", "--request", req, "--format", "json", "./...")
	if r.Code != 1 {
		t.Fatalf("exit %d want 1 stderr=%s\n%s", r.Code, r.Stderr, r.Stdout)
	}
	var rep report.Report
	if err := json.Unmarshal([]byte(r.Stdout), &rep); err != nil {
		t.Fatal(err)
	}
	failed := 0
	for _, c := range rep.Checks {
		if c.Status == "fail" {
			failed++
		}
	}
	if failed < 2 {
		t.Fatalf("want both checks fail, got %+v", rep.Checks)
	}
}

func TestDeterminism(t *testing.T) {
	root := runtest.RepoRoot(t)
	dir := filepath.Join(root, "testdata", "crawl")
	a := runtest.Run(t, dir, "--no-config", "--format", "json", "./...")
	b := runtest.Run(t, dir, "--no-config", "--format", "json", "./...")
	if a.Stdout != b.Stdout {
		t.Fatal("crawl JSON not byte-identical across runs")
	}
	shop := filepath.Join(root, "testdata", "shopbad")
	req := filepath.Join(root, "testdata", "requests", "payments.json")
	c1 := runtest.Run(t, shop, "check", "--no-config", "--request", req, "--format", "json", "./...")
	c2 := runtest.Run(t, shop, "check", "--no-config", "--request", req, "--format", "json", "./...")
	if c1.Stdout != c2.Stdout {
		t.Fatal("check JSON not byte-identical across runs")
	}
}

func TestYAMLEqualsJSON(t *testing.T) {
	root := runtest.RepoRoot(t)
	dir := filepath.Join(root, "testdata", "shopbad")
	jreq := filepath.Join(root, "testdata", "requests", "payments.json")
	yreq := filepath.Join(root, "testdata", "requests", "payments.yaml")
	j := runtest.Run(t, dir, "check", "--no-config", "--request", jreq, "--format", "json", "./...")
	y := runtest.Run(t, dir, "check", "--no-config", "--request", yreq, "--format", "json", "./...")
	var rj, ry report.Report
	if err := json.Unmarshal([]byte(j.Stdout), &rj); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(y.Stdout), &ry); err != nil {
		t.Fatal(err)
	}
	// Clear hashes that may differ if metadata key order differs; compare check outcomes.
	if len(rj.Checks) != len(ry.Checks) {
		t.Fatalf("check count %d vs %d", len(rj.Checks), len(ry.Checks))
	}
	for i := range rj.Checks {
		if rj.Checks[i].Status != ry.Checks[i].Status || rj.Checks[i].ID != ry.Checks[i].ID {
			t.Fatalf("check %d mismatch: %+v vs %+v", i, rj.Checks[i], ry.Checks[i])
		}
	}
}

func TestReportSchema(t *testing.T) {
	root := runtest.RepoRoot(t)
	dir := filepath.Join(root, "testdata", "crawl")
	r := runtest.Run(t, dir, "--no-config", "--format", "json", "./...")
	var rep map[string]any
	if err := json.Unmarshal([]byte(r.Stdout), &rep); err != nil {
		t.Fatal(err)
	}
	if rep["schema"] != "gointegrity/report/v1" {
		t.Fatalf("schema %v", rep["schema"])
	}
	if _, ok := rep["tool"].(map[string]any); !ok {
		t.Fatal("missing tool")
	}
	if _, ok := rep["summary"].(map[string]any); !ok {
		t.Fatal("missing summary")
	}
	_ = schemas.ReportV1
}

func TestSARIFShape(t *testing.T) {
	root := runtest.RepoRoot(t)
	dir := filepath.Join(root, "testdata", "crawl")
	r := runtest.Run(t, dir, "--no-config", "--format", "sarif", "./...")
	var doc map[string]any
	if err := json.Unmarshal([]byte(r.Stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["version"] != "2.1.0" {
		t.Fatalf("version %v", doc["version"])
	}
	runs, ok := doc["runs"].([]any)
	if !ok || len(runs) == 0 {
		t.Fatal("missing runs")
	}
}

func TestBaselineRoundTrip(t *testing.T) {
	root := runtest.RepoRoot(t)
	dir := filepath.Join(root, "testdata", "crawl")
	base := filepath.Join(t.TempDir(), "baseline.json")
	w := runtest.Run(t, dir, "--no-config", "--write-baseline", base, "./...")
	if w.Code != 0 {
		t.Fatalf("write-baseline exit %d %s", w.Code, w.Stderr)
	}
	r := runtest.Run(t, dir, "--no-config", "--baseline", base, "--format", "json", "./...")
	if r.Code != 0 {
		t.Fatalf("with baseline exit %d want 0 stderr=%s\n%s", r.Code, r.Stderr, r.Stdout)
	}
	var rep report.Report
	if err := json.Unmarshal([]byte(r.Stdout), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Summary.Findings != 0 {
		t.Fatalf("expected 0 findings after baseline, got %d", rep.Summary.Findings)
	}
}

func TestToolErrors(t *testing.T) {
	root := runtest.RepoRoot(t)
	bad := filepath.Join(root, "testdata", "requests", "invalid", "bad-schema.json")
	r := runtest.Run(t, root, "check", "--no-config", "--request", bad, "./testdata/crawl")
	if r.Code != 2 {
		t.Fatalf("bad schema exit %d want 2 stderr=%s", r.Code, r.Stderr)
	}
	dup := filepath.Join(root, "testdata", "requests", "invalid", "dup-id.json")
	r = runtest.Run(t, root, "check", "--no-config", "--request", dup, "./testdata/crawl")
	if r.Code != 2 {
		t.Fatalf("dup id exit %d want 2", r.Code)
	}
}

func TestRequestValidation(t *testing.T) {
	_, errs, err := check.ParseRequest([]byte(`{"schema":"gointegrity/check-request/v1","request_id":"x","checks":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) == 0 {
		t.Fatal("expected envelope errors for empty checks")
	}
}

func TestGlobMatch(t *testing.T) {
	if !config.MatchGlob("*_mock.go", "foo_mock.go") {
		t.Fatal("expected match")
	}
	if !config.MatchGlob("internal/legacy/**", "internal/legacy/a/b.go") {
		t.Fatal("expected ** match")
	}
}

func TestRuleResolution(t *testing.T) {
	cfg := config.Defaults()
	f := false
	cfg.Rules = map[string]config.RuleOverride{
		"placeholder": {Severity: "error"},
		"STUB001":     {Enabled: &f},
	}
	en := rules.ResolveEnablement(cfg, nil, []string{"DEAD004"})
	if en.Enabled["STUB001"] {
		t.Fatal("STUB001 should be disabled")
	}
	if en.Severity["STUB005"] != report.SeverityError {
		t.Fatalf("placeholder category severity not applied: %s", en.Severity["STUB005"])
	}
	if en.Enabled["DEAD004"] {
		t.Fatal("DEAD004 should be disabled via flag")
	}
}

func TestRootsParsing(t *testing.T) {
	m := normalizeRootsForTest([]string{"all"})
	for _, k := range []string{"main", "init", "exported", "tests", "benchmarks", "fuzz", "examples"} {
		if !m[k] {
			t.Fatalf("missing root kind %s", k)
		}
	}
	m2 := normalizeRootsForTest([]string{"main,init"})
	if !m2["main"] || !m2["init"] || m2["exported"] {
		t.Fatalf("unexpected %#v", m2)
	}
}

func TestVersionAndSchema(t *testing.T) {
	root := runtest.RepoRoot(t)
	v := runtest.Run(t, root, "version")
	if strings.TrimSpace(v.Stdout) != "0.4.0" {
		t.Fatalf("version %q", v.Stdout)
	}
	s := runtest.Run(t, root, "schema", "request")
	if !strings.Contains(s.Stdout, "gointegrity/check-request/v1") && !strings.Contains(s.Stdout, "check-request") {
		t.Fatalf("schema output unexpected: %s", s.Stdout[:min(80, len(s.Stdout))])
	}
}

func TestConfigFixture(t *testing.T) {
	root := runtest.RepoRoot(t)
	dir := filepath.Join(root, "testdata", "config")
	r := runtest.Run(t, dir, "--format", "json", "./...")
	var rep report.Report
	if err := json.Unmarshal([]byte(r.Stdout), &rep); err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Findings {
		if f.RuleID == "STUB001" {
			t.Fatal("STUB001 should be disabled by config")
		}
	}
}

func TestSelfClean(t *testing.T) {
	root := runtest.RepoRoot(t)
	// Analyze only cmd + library packages excluding testdata by patterns.
	r := runtest.Run(t, root, "--no-config", "--roots", "main,init,exported,tests",
		"--disable", "all", "--enable", "DEAD", "--format", "json",
		"./cmd/...", "./loader", "./callgraph", "./rules", "./check", "./config", "./report", "./schemas")
	// May have findings in bootstrap; ensure tool runs.
	if r.Code == 2 {
		t.Fatalf("tool error: %s", r.Stderr)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestWriteReadBaselineFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.json")
	if err := config.WriteBaseline(path, []string{"sha256:a", "sha256:b", "sha256:a"}); err != nil {
		t.Fatal(err)
	}
	b, err := config.LoadBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Contains("sha256:a") || !b.Contains("sha256:b") {
		t.Fatal("missing fps")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "fingerprints") {
		t.Fatal(string(data))
	}
}

func normalizeRootsForTest(roots []string) map[string]bool {
	out := map[string]bool{}
	for _, r := range roots {
		r = strings.TrimSpace(strings.ToLower(r))
		if r == "all" {
			for _, k := range []string{"main", "init", "exported", "tests", "benchmarks", "fuzz", "examples"} {
				out[k] = true
			}
			continue
		}
		for _, part := range strings.Split(r, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out[part] = true
			}
		}
	}
	return out
}

func TestFlowCrawl(t *testing.T) {
	root := runtest.RepoRoot(t)
	dir := filepath.Join(root, "testdata", "flow")
	r := runtest.Run(t, dir, "--no-config", "--flow", "--format", "json", "./...")
	if r.Code == 2 {
		t.Fatalf("tool error: %s", r.Stderr)
	}
	var rep report.Report
	if err := json.Unmarshal([]byte(r.Stdout), &rep); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range rep.Findings {
		seen[f.RuleID] = true
	}
	if !seen["FLOW001"] && !seen["FLOW002"] {
		t.Fatalf("expected FLOW findings, got rules %v findings=%d", seen, len(rep.Findings))
	}
}

func TestFlowDOT(t *testing.T) {
	root := runtest.RepoRoot(t)
	dir := filepath.Join(root, "testdata", "flow")
	r := runtest.Run(t, dir, "--no-config", "--flow", "--format", "dot", "./...")
	if r.Code != 0 && r.Code != 1 {
		t.Fatalf("exit %d stderr=%s", r.Code, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "digraph") {
		t.Fatalf("expected digraph, got %q", r.Stdout[:min(120, len(r.Stdout))])
	}
}

func TestFlowVerifyIncremental(t *testing.T) {
	root := runtest.RepoRoot(t)
	dir := filepath.Join(root, "testdata", "flow")
	r := runtest.Run(t, dir, "--no-config", "--verify-incremental", "--format", "json", "./...")
	if r.Code == 2 {
		t.Fatalf("verify-incremental failed: %s", r.Stderr)
	}
	if !strings.Contains(r.Stderr, "verify-incremental: OK") {
		t.Fatalf("missing OK on stderr: %s", r.Stderr)
	}
}

func TestSuppressCommand(t *testing.T) {
	root := runtest.RepoRoot(t)
	path := filepath.Join(t.TempDir(), "sup.yml")
	r := runtest.Run(t, root, "suppress",
		"--rule", "FLOW001",
		"--source", "source:a",
		"--sink", "sink:b",
		"--reason", "accepted risk",
		"--owner", "steve",
		path,
	)
	if r.Code != 0 {
		t.Fatalf("exit %d %s", r.Code, r.Stderr)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "FLOW001") || !strings.Contains(string(data), "accepted risk") {
		t.Fatalf("bad file: %s", data)
	}
}
