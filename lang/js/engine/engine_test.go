package engine_test

import (
	"testing"

	"gointegrity/lang/js/engine"
)

func TestTSReturnMismatch(t *testing.T) {
	files := map[string]string{
		"a.ts": "export function f(): number { return \"x\"; }\n",
	}
	fs := engine.Analyze(files)
	found := false
	for _, f := range fs {
		if f.RuleID == "JSTYPE001" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected JSTYPE001, got %#v", fs)
	}
}

func TestParseType(t *testing.T) {
	ty := engine.ParseType("string | number")
	if ty.Name != "union" || len(ty.Union) != 2 {
		t.Fatalf("%#v", ty)
	}
}

func TestStubFlowDead(t *testing.T) {
	files := map[string]string{
		"index.js": `
export function main() { helper(); }
function helper() { return 1; }
function dead() { }
function stub() { throw new Error("not implemented"); }
function risky(userInput) { eval(userInput); }
`,
	}
	fs := engine.Analyze(files)
	want := map[string]bool{
		"JSDEAD001": false,
		"JSSTUB005": false,
		"JSFLOW001": false,
	}
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
