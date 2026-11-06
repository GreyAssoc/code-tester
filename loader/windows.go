package loader

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// DetectWindowsOriented reports whether root contains Go sources that are
// Windows-specific (D-015-04): *_windows.go files or windows build constraints.
func DetectWindowsOriented(root string) bool {
	found := false
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || found {
			return nil
		}
		if info.IsDir() {
			n := info.Name()
			if n == "vendor" || n == "node_modules" || n == ".git" || n == "testdata" ||
				n == ".gointegrity-cache" {
				return filepath.SkipDir
			}
			return nil
		}
		base := info.Name()
		if !strings.HasSuffix(base, ".go") {
			return nil
		}
		if strings.HasSuffix(base, "_windows.go") || strings.Contains(base, "_windows_") {
			found = true
			return filepath.SkipAll
		}
		// Build tags in the first ~1KB.
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for i := 0; i < 40 && sc.Scan(); i++ {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "//") {
				break // past header comments
			}
			low := strings.ToLower(line)
			if strings.Contains(low, "go:build") && strings.Contains(low, "windows") &&
				!strings.Contains(low, "!windows") {
				found = true
				return filepath.SkipAll
			}
			if strings.Contains(low, "+build") && strings.Contains(low, "windows") &&
				!strings.Contains(low, "!windows") {
				found = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	return found
}
