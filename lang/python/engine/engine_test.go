package engine_test

import (
	"testing"

	"github.com/GreyAssoc/code-tester/lang/python/engine"
)

func TestReturnTypeMismatch(t *testing.T) {
	files := map[string]string{
		"m.py": "def f() -> int:\n    return \"x\"\n",
	}
	fs := engine.Analyze(files)
	found := false
	for _, f := range fs {
		if f.RuleID == "PYTYPE001" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected PYTYPE001, got %#v", fs)
	}
}

func TestParseTypeUnion(t *testing.T) {
	ty := engine.ParseTypeAnn("int | None")
	if ty.Name != "Union" || len(ty.Args) != 2 {
		t.Fatalf("got %#v", ty)
	}
}

func TestStubAndFlowAndDead(t *testing.T) {
	files := map[string]string{
		"main.py": `
def main():
    use_ok()

def use_ok():
    return 1

def dead_fn():
    pass

def stub():
    raise NotImplementedError()

def risky(user_input):
    eval(user_input)
`,
	}
	fs := engine.Analyze(files)
	want := map[string]bool{
		"PYDEAD001": false,
		"PYSTUB005": false,
		"PYFLOW001": false,
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
