// Package ir defines the intermediate representation shared across
// compile pipeline stages.
package ir

// BasicType names a scalar or one-dimensional homogeneous cross-block type.
type BasicType string

const (
	Int   BasicType = "int"
	Float BasicType = "float"
	Bool  BasicType = "bool"
	Str   BasicType = "str"
)

// ListOf returns the canonical one-dimensional list type for a scalar type.
func ListOf(t BasicType) BasicType {
	if _, ok := ElementType(t); ok {
		return ""
	}
	return t + "[]"
}

// ElementType returns the scalar element type for a supported list type.
func ElementType(t BasicType) (BasicType, bool) {
	switch t {
	case BasicType("int[]"):
		return Int, true
	case BasicType("float[]"):
		return Float, true
	case BasicType("bool[]"):
		return Bool, true
	case BasicType("str[]"), BasicType("string[]"):
		return Str, true
	default:
		return "", false
	}
}

// IsList reports whether t is a supported one-dimensional list type.
func IsList(t BasicType) bool {
	_, ok := ElementType(t)
	return ok
}

// Contract maps cross-block variable names to their type. Order of
// iteration over map is undefined; deterministic outputs must sort
// keys at use site (printing/serialization/product generation).
type Contract map[string]BasicType

// VarSet reports which variables a stage reads/writes.
type VarSet map[string]bool

// Stage holds one source block.
type Stage struct {
	Index     int      // insertion order, 1-based
	Lang      string   // "shell" | "python" | "go"
	StartLine int      // 1-based source line of first body line
	EndLine   int      // 1-based source line of the last body line
	Body      []string // verbatim body lines; may be empty
}

// Span locates a region in the source. nil means "synthetic"
// (generated region, no source counterpart).
type Span struct {
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
}

// Diagnostic reports a compile error or warning from an engine.
type Diagnostic struct {
	Var  string
	Msg  string
	Span *Span
}

// OriginKind classifies how a generated line maps back to source.
type OriginKind int

const (
	// OrigSource: generated line maps 1:1 to a source line.
	OrigSource OriginKind = iota
	// OrigSynthetic: generated line has no source counterpart.
	OrigSynthetic
)

// SourceMapEntry is the origin of one generated line.
type SourceMapEntry struct {
	SourceLine int
	Kind       OriginKind
}

// SourceMap maps "genFile:genLine" -> origin entry.
type SourceMap map[string]SourceMapEntry

// Program assembles compile inputs.
type Program struct {
	Path     string
	Contract Contract
	Stages   []Stage
}
