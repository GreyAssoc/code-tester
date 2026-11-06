package engine

// File is a parsed Python source unit.
type File struct {
	Path    string
	Module  string // dotted module guess from path
	Source  string
	Imports []Import
	Defs    []*Def
	Assigns []Assign
}

type Import struct {
	Module string
	Name   string // bound name
	Line   int
	Col    int
}

type DefKind int

const (
	DefFunc DefKind = iota
	DefClass
	DefMethod
)

type Def struct {
	Kind    DefKind
	Name    string
	Qual    string
	File    string
	Line    int
	Col     int
	Args    []Param
	RetAnn  string // return annotation text
	Body    string
	Decorators []string
	Exported bool // top-level public (no leading _)
}

type Param struct {
	Name string
	Ann  string
	Default string
}

type Assign struct {
	Name string
	Ann  string
	File string
	Line int
	Col  int
}

// Type is a simplified Python type for the checker.
type Type struct {
	Name     string   // int, str, Any, Union, Name, etc.
	Args     []Type   // generics / union members
	Optional bool
	Unknown  bool
}

func AnyType() Type  { return Type{Name: "Any", Unknown: true} }
func NoneType() Type { return Type{Name: "None"} }

func ParseTypeAnn(s string) Type {
	s = trimSpace(s)
	if s == "" {
		return AnyType()
	}
	if s == "None" {
		return NoneType()
	}
	if len(s) >= 9 && s[:9] == "Optional[" {
		inner := s[9 : len(s)-1]
		t := ParseTypeAnn(inner)
		t.Optional = true
		return t
	}
	if len(s) > 5 && s[:5] == "list[" {
		return Type{Name: "list", Args: []Type{ParseTypeAnn(s[5 : len(s)-1])}}
	}
	if len(s) > 4 && s[:4] == "List[" {
		return Type{Name: "list", Args: []Type{ParseTypeAnn(s[5 : len(s)-1])}}
	}
	if len(s) > 5 && s[:5] == "dict[" {
		return Type{Name: "dict", Unknown: true}
	}
	if len(s) > 5 && s[:5] == "Union[" {
		inner := s[5 : len(s)-1]
		parts := splitTop(inner, ',')
		var args []Type
		for _, p := range parts {
			args = append(args, ParseTypeAnn(p))
		}
		return Type{Name: "Union", Args: args}
	}
	// X | Y
	if parts := splitTop(s, '|'); len(parts) > 1 {
		var args []Type
		for _, p := range parts {
			args = append(args, ParseTypeAnn(p))
		}
		return Type{Name: "Union", Args: args}
	}
	return Type{Name: s}
}

func Compatible(got, want Type) bool {
	if want.Unknown || want.Name == "Any" || got.Unknown || got.Name == "Any" {
		return true
	}
	if want.Optional && got.Name == "None" {
		return true
	}
	if want.Name == "Union" {
		for _, a := range want.Args {
			if Compatible(got, a) {
				return true
			}
		}
		return false
	}
	if got.Name == "Union" {
		for _, a := range got.Args {
			if !Compatible(a, want) {
				return false
			}
		}
		return true
	}
	return got.Name == want.Name
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func splitTop(s string, sep byte) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			depth--
		default:
			if depth == 0 && s[i] == sep {
				parts = append(parts, trimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, trimSpace(s[start:]))
	return parts
}
