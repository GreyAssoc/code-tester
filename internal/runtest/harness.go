package runtest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

var (
	binOnce sync.Once
	binPath string
	binErr  error
)

func RepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

func Binary(t *testing.T) string {
	t.Helper()
	root := RepoRoot(t)
	binOnce.Do(func() {
		dir := filepath.Join(root, ".testbin")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			binErr = err
			return
		}
		binPath = filepath.Join(dir, "codeintegrity")
		cmd := exec.Command("go", "build", "-o", binPath, "./cmd/codeintegrity")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			binErr = err
			_ = out
		}
	})
	if binErr != nil {
		t.Fatalf("build codeintegrity: %v", binErr)
	}
	return binPath
}

type Result struct {
	Stdout string
	Stderr string
	Code   int
}

func Run(t *testing.T, dir string, args ...string) Result {
	t.Helper()
	bin := Binary(t)
	cmd := exec.Command(bin, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run: %v", err)
		}
	}
	return Result{Stdout: stdout.String(), Stderr: stderr.String(), Code: code}
}
