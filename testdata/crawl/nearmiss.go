package crawl

import "fmt"

type printer interface{ String() string }

type named struct{ s string }

func (n named) String() string { return n.s }

func UsePrinter() string {
	var p printer = named{s: "ok"}
	return fmt.Sprintf("%v", p)
}

//gointegrity:ignore DEAD001 kept for a debugging session
func suppressedDead() int { return 1 }

// Deprecated: use Live instead.
func OldFlush() {}

type noopSink struct{}

func (noopSink) Write(_ []byte) (int, error) { return 0, nil }

func UseNoop() { var n noopSink; _, _ = n.Write(nil) }
