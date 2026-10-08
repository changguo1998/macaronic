// Package engine defines the macaronic language engine interface and
// the built-in registry. Actual bash/python/go engines arrive in
// M6-M8; M3 only freezes the interface shape.
package engine

import (
	"fmt"

	"github.com/changguo1998/macaronic/internal/ir"
)

// Analysis is the structured result of a stage analysis. Span line numbers
// are 1-based within the stage body; Analyzer converts them to .mac lines.
type Analysis struct {
	Reads       ir.VarSet
	Writes      ir.VarSet
	ReadSpans   map[string]*ir.Span
	WriteSpans  map[string]*ir.Span
	Diagnostics []ir.Diagnostic
}

// DetailedAnalyzer is optionally implemented by engines that provide source
// spans for static diagnostics. Engine keeps the legacy Analyze API so test
// adapters and external callers remain source-compatible.
type DetailedAnalyzer interface {
	AnalyzeDetailed(st *ir.Stage, c ir.Contract) Analysis
}

// RuntimeChecker is optionally implemented by engines that need an
// external runtime on PATH. All six built-in engines do (M20): a
// missing interpreter or toolchain is an environment problem, so it is
// reported once at check/build/run time rather than surfacing as an
// exec failure mid-run.
//
// The analyzer probes RequiredCommands before the stage's static
// analysis, so a missing dialect is reported as a precise
// "requires X on PATH" error. It is optional for the same reason as
// DetailedAnalyzer: mocks must not be forced to implement it.
type RuntimeChecker interface {
	// RequiredCommands returns the executables this engine needs on
	// PATH. The first entry must match RunCommand's argv[0], except for
	// a compiled language, which runs the artifact it built and
	// therefore declares the toolchain used during Emit (go -> "go").
	RequiredCommands() []string
}

// Error returns the first structured diagnostic as a legacy error, preserving
// the old Analyze contract for callers that still expect error text.
func (a Analysis) Error(st *ir.Stage) error {
	if len(a.Diagnostics) == 0 {
		return nil
	}
	d := a.Diagnostics[0]
	if d.Span != nil && d.Span.StartLine > 0 {
		return fmt.Errorf("%s (line %d)", d.Msg, st.StartLine+d.Span.StartLine)
	}
	return fmt.Errorf("%s", d.Msg)
}

// Engine is one language backend. Analyze, Emit, RunCommand and
// ParseDiagnostics are the four operations the compile/run pipeline
// needs; see docs/architecture.md §8.
type Engine interface {
	// Name returns the block language id ("bash", "python", "go").
	Name() string

	// Analyze runs intra-block type propagation and returns the sets
	// of contract variables this stage reads and writes. Returning a
	// non-nil error reports a stage-level compile problem (e.g. a
	// local binding that shadows a contract variable, or a missing
	// annotation); the framework turns it into an issue rather than
	// failing silently.
	Analyze(st *ir.Stage, c ir.Contract) (ir.VarSet, ir.VarSet, error)

	// Emit writes the runnable file for one stage into stageDir
	// (with injected read/write code), recording source-map entries
	// in sm for error back-mapping later.
	Emit(st *ir.Stage, c ir.Contract, stageDir, stateDir string,
		sm *ir.SourceMap) error

	// RunCommand returns the argv that run.sh uses to invoke the
	// stage's emitted file.
	RunCommand(stageDir string) []string

	// ParseDiagnostics extracts (genFile, line, message) from a
	// stage's stderr for cross-mapping back to .mac lines.
	ParseDiagnostics(stderr []byte) []ir.Diagnostic
}

// registry maps language id -> engine.
var registry = map[string]Engine{}

// Register adds e to the registry, keyed by e.Name().
func Register(e Engine) {
	registry[e.Name()] = e
}

// Get returns the engine for name, if registered.
func Get(name string) (Engine, bool) {
	e, ok := registry[name]
	return e, ok
}

// Registered returns all registered ids, sorted.
func Registered() []string {
	return nil // populated with real engines in M6-M8
}
