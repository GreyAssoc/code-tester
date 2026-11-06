package engine_test

import (
	"testing"

	"gointegrity/lang/css/engine"
)

func TestUnusedSelectorAndEmpty(t *testing.T) {
	files := map[string]string{
		"a.css": ".used { color: red; }\n.unused { }\n.dup { color: blue; color: green; }\n",
	}
	fs := engine.Analyze(".", files, `<div class="used"></div>`)
	want := map[string]bool{"CSSDEAD001": false, "CSSEMPTY001": false, "CSSCOH004": false}
	for _, f := range fs {
		if _, ok := want[f.RuleID]; ok {
			want[f.RuleID] = true
		}
	}
	for id, ok := range want {
		if !ok {
			t.Fatalf("missing %s in %#v", id, fs)
		}
	}
}

func TestUndefinedVar(t *testing.T) {
	files := map[string]string{
		"a.css": ".x { color: var(--missing); }\n",
	}
	fs := engine.Analyze(".", files, `<div class="x"></div>`)
	found := false
	for _, f := range fs {
		if f.RuleID == "CSSCOH002" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected CSSCOH002, got %#v", fs)
	}
}
