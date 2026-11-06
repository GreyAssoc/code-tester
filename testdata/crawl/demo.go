package crawl

func Live() int { return deadHelper() }

func deadHelper() int { return 42 }

func deadAlone() int { return 1 }

func (c circle) diameter() float64 { return 0 }

type circle struct{}
type unusedType struct{}

func (unusedType) m() {}

const deadConst = 4
const (
	modeA = iota
	modeB
)

var deadVar = "x"
var usedEnum = modeA

func Flush() {}

func Sync() { /* TODO: implement sync */ }

func Get(id string) (*int, error) { return nil, nil }

func Describe(it int) string { return "TODO" }

func Delete(id string) error { panic("not implemented") }

func Scale(v int, factor int) int { return v * 2 }

func Format(msg string, level int) string {
	_ = level
	return msg
}
