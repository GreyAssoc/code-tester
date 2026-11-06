package assessor_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/GreyAssoc/code-tester/assessor"
	"github.com/GreyAssoc/code-tester/config"
	"github.com/GreyAssoc/code-tester/lang"
)

func multiRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "testdata", "multi"))
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
}

func TestDetectMulti(t *testing.T) {
	det, err := assessor.Detect(multiRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []lang.ID{lang.Go, lang.Python, lang.JS, lang.CSS} {
		if _, ok := det.Languages[id]; !ok {
			t.Errorf("missing language %s", id)
		}
	}
}

func TestRunMulti(t *testing.T) {
	rep, _, err := assessor.Run(assessor.RunOptions{RootDir: multiRoot(t)})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.Findings == 0 {
		t.Fatal("expected findings")
	}
	langs := map[string]bool{}
	for _, f := range rep.Findings {
		langs[f.Language] = true
	}
	for _, want := range []string{"go", "python", "js", "css"} {
		if !langs[want] {
			t.Errorf("missing findings for %s (have %v)", want, langs)
		}
	}
}

func TestExcludeTestdata(t *testing.T) {
	root := repoRoot(t)
	ex := []string{"testdata/**"}
	det, err := assessor.Detect(root, ex...)
	if err != nil {
		t.Fatal(err)
	}
	for id, paths := range det.Languages {
		for _, p := range paths {
			if strings.HasPrefix(p, "testdata/") {
				t.Fatalf("detect still has %s path %s", id, p)
			}
		}
	}
	if _, ok := det.Languages[lang.Python]; ok && len(det.Languages[lang.Python]) > 0 {
		t.Fatalf("expected no python outside testdata, got %v", det.Languages[lang.Python])
	}
	if _, ok := det.Languages[lang.JS]; ok && len(det.Languages[lang.JS]) > 0 {
		t.Fatalf("expected no js outside testdata, got %v", det.Languages[lang.JS])
	}
	if _, ok := det.Languages[lang.CSS]; ok && len(det.Languages[lang.CSS]) > 0 {
		t.Fatalf("expected no css outside testdata, got %v", det.Languages[lang.CSS])
	}

	rep, _, err := assessor.Run(assessor.RunOptions{RootDir: root, Exclude: ex})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Findings {
		if strings.HasPrefix(filepath.ToSlash(f.Location.File), "testdata/") {
			t.Fatalf("finding under testdata despite exclude: %s %s", f.RuleID, f.Location.File)
		}
		if f.Language == "python" || f.Language == "js" || f.Language == "css" {
			t.Fatalf("unexpected %s finding with testdata excluded: %s", f.Language, f.Location.File)
		}
	}
}

func TestMatchGlobTestdata(t *testing.T) {
	if !config.Excluded("testdata/multi/web/app.py", []string{"testdata/**"}) {
		t.Fatal("expected excluded")
	}
	if !config.SkipDir("testdata", []string{"testdata/**"}) {
		t.Fatal("expected skip testdata dir")
	}
	if config.Excluded("lang/python/driver.go", []string{"testdata/**"}) {
		t.Fatal("product path should not be excluded")
	}
}
