package loader_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GreyAssoc/code-tester/loader"
)

func TestDetectWindowsOriented(t *testing.T) {
	dir := t.TempDir()
	if loader.DetectWindowsOriented(dir) {
		t.Fatal("empty tree should be lean")
	}
	mustWrite(t, filepath.Join(dir, "plain.go"), "package p\nfunc F() {}\n")
	if loader.DetectWindowsOriented(dir) {
		t.Fatal("plain Go should be lean")
	}
	mustWrite(t, filepath.Join(dir, "sys_windows.go"), "package p\nfunc Win() {}\n")
	if !loader.DetectWindowsOriented(dir) {
		t.Fatal("expected *_windows.go detection")
	}
}

func TestDetectWindowsBuildTag(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "w.go"), "//go:build windows\n\npackage p\nfunc W() {}\n")
	if !loader.DetectWindowsOriented(dir) {
		t.Fatal("expected go:build windows detection")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
