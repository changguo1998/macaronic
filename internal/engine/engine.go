// Package engine defines the macaronic language engine interface and
// the built-in registry. Actual shell/python/go engines arrive in
// M6-M8; M3 only freezes the interface shape.
package engine

import "github.com/changguo1998/macaronic/internal/ir"

// Engine is one language backend. Analyze, Emit, RunCommand and
// ParseDiagnostics are the four operations the compile/run pipeline
// needs; see docs/architecture.md §8.
type Engine interface {
	// Name returns the block language id ("shell", "python", "go").
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
