package engine

// File is a JS/TS source unit.
type File struct {
	Path     string
	IsTS     bool
	Source   string
	Imports  []Import
	Exports  []Export
	Funcs    []*Func
	Classes  []Class
	TypeAliases []TypeAlias
}

type Import struct {
	From  string
	Names []string // local bound names
	Line  int
	Col   int
}

type Export struct {
	Name string
	Line int
	Col  int
}

type Func struct {
	Name     string
	File     string
	Line     int
	Col      int
	Params   []Param
	RetType  string
	Body     string
	Exported bool
	Async    bool
}

type Param struct {
	Name    string
	Type    string
	Optional bool
	Default string
}

type Class struct {
	Name     string
	File     string
	Line     int
	Col      int
	Exported bool
}

type TypeAlias struct {
	Name string
	Type string
	File string
	Line int
	Col  int
}

// Type is a simplified TS/JS type.
type Type struct {
	Name    string
	Args    []Type
	Union   []Type
	Optional bool
	Unknown bool
}

func Any() Type    { return Type{Name: "any", Unknown: true} }
func Void() Type   { return Type{Name: "void"} }
func Never() Type  { return Type{Name: "never"} }

func ParseType(s string) Type {
	s = trim(s)
	if s == "" || s == "any" || s == "unknown" {
		return Any()
	}
	if s == "void" {
		return Void()
	}
	if s == "string" || s == "number" || s == "boolean" || s == "null" || s == "undefined" || s == "object" {
		return Type{Name: s}
	}
	if len(s) > 0 && s[len(s)-1] == '|' {
		s = trim(s[:len(s)-1])
	}
	if parts := splitTop(s, '|'); len(parts) > 1 {
		var u []Type
		for _, p := range parts {
			u = append(u, ParseType(p))
		}
		return Type{Name: "union", Union: u}
	}
	if i := indexOf(s, '<'); i > 0 {
		name := s[:i]
		inner := s[i+1 : len(s)-1]
		var args []Type
		for _, p := range splitTop(inner, ',') {
			args = append(args, ParseType(p))
		}
		return Type{Name: name, Args: args}
	}
	if len(s) > 2 && s[len(s)-2:] == "[]" {
		return Type{Name: "Array", Args: []Type{ParseType(s[:len(s)-2])}}
	}
	return Type{Name: s}
}

func Compatible(got, want Type) bool {
	if want.Unknown || want.Name == "any" || got.Unknown || got.Name == "any" {
		return true
	}
	if want.Name == "union" {
		for _, u := range want.Union {
			if Compatible(got, u) {
				return true
			}
		}
		return false
	}
	if got.Name == "union" {
		for _, u := range got.Union {
			if !Compatible(u, want) {
				return false
			}
		}
		return true
	}
	if want.Name == "void" && (got.Name == "undefined" || got.Name == "void") {
		return true
	}
	return got.Name == want.Name
}

func trim(s string) string {
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
		case '<', '(', '[', '{':
			depth++
		case '>', ')', ']', '}':
			depth--
		default:
			if depth == 0 && s[i] == sep {
				parts = append(parts, trim(s[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, trim(s[start:]))
	return parts
}

func indexOf(s string, b byte) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<', '(', '[':
			depth++
			if b == s[i] && depth == 1 && b == '<' {
				return i
			}
		case '>', ')', ']':
			depth--
		default:
			if depth == 0 && s[i] == b {
				return i
			}
		}
		if b == '<' && s[i] == '<' && depth == 1 {
			// handled
		}
	}
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
