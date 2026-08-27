package analyze

import (
	"fmt"
	"strings"
	"testing"

	"github.com/changguo1998/macaronic/internal/engine"
	"github.com/changguo1998/macaronic/internal/ir"
)

// mockEngine simulates a language backend for framework tests.
type mockEngine struct {
	name       string
	reads      ir.VarSet
	writes     ir.VarSet
	analyzeErr error
}

func (m mockEngine) Name() string { return m.name }
func (m mockEngine) Analyze(st *ir.Stage, c ir.Contract) (ir.VarSet, ir.VarSet, error) {
	return m.reads, m.writes, m.analyzeErr
}
func (m mockEngine) Emit(st *ir.Stage, c ir.Contract, stageDir, stateDir string, sm *ir.SourceMap) error {
	return nil
}
func (m mockEngine) RunCommand(stageDir string) []string { return nil }
func (m mockEngine) ParseDiagnostics(stderr []byte) []ir.Diagnostic {
	return nil
}

func mkStage(idx int, lang string, line int) *ir.Stage {
	return &ir.Stage{Index: idx, Lang: lang, StartLine: line, EndLine: line}
}

func mockResolver(eds ...engine.Engine) func(string) (engine.Engine, bool) {
	m := map[string]engine.Engine{}
	for _, e := range eds {
		m[e.Name()] = e
	}
	return func(lang string) (engine.Engine, bool) { e, ok := m[lang]; return e, ok }
}

// errShadow builds the engine-reported shadowing error for tests.
func errShadow(v string) error {
	return fmt.Errorf("local binding %q shadows contract variable", v)
}

func TestReadBeforeWrite(t *testing.T) {
	p := &ir.Program{Contract: ir.Contract{"count": ir.Int}}
	p.Stages = append(p.Stages, *mkStage(1, "shell", 5))
	r := mockResolver(mockEngine{name: "shell", reads: ir.VarSet{"count": true}})
	got := (Analyzer{Engines: r}).Run(p)
	if len(got.Issues) != 1 {
		t.Fatalf("issues = %+v, want 1", got.Issues)
	}
	it := got.Issues[0]
	if it.Stage != 1 || it.Var != "count" || it.Line != 5 {
		t.Errorf("issue = %+v", it)
	}
	if !strings.Contains(it.Msg, "read of \"count\" before any write") ||
		!strings.Contains(it.Msg, "type int") {
		t.Errorf("msg = %q, want read-before-write + type", it.Msg)
	}
}

func TestOverwriteOK(t *testing.T) {
	p := &ir.Program{Contract: ir.Contract{"count": ir.Int}}
	sh := mockEngine{name: "shell", writes: ir.VarSet{"count": true}}
	py := mockEngine{name: "python", reads: ir.VarSet{"count": true}, writes: ir.VarSet{"count": true}}
	p.Stages = append(p.Stages, *mkStage(1, "shell", 5), *mkStage(2, "python", 9))
	got := (Analyzer{Engines: mockResolver(sh, py)}).Run(p)
	if !got.OK() {
		t.Errorf("multiple writers should be OK, issues = %+v", got.Issues)
	}
}

func TestMultiWriterNotConflict(t *testing.T) {
	p := &ir.Program{Contract: ir.Contract{"x": ir.Int}}
	p.Stages = append(p.Stages, *mkStage(1, "shell", 5), *mkStage(2, "shell", 9))
	r := mockResolver(
		mockEngine{name: "shell", writes: ir.VarSet{"x": true}},
		mockEngine{name: "shell", writes: ir.VarSet{"x": true}},
	)
	// both writers: overwrite is fine
	got := (Analyzer{Engines: r}).Run(p)
	if !got.OK() {
		t.Errorf("two writers should be OK, issues = %+v", got.Issues)
	}
}

func TestNoEngine(t *testing.T) {
	p := &ir.Program{Contract: ir.Contract{}}
	p.Stages = append(p.Stages, *mkStage(1, "ruby", 4))
	got := (Analyzer{Engines: mockResolver()}).Run(p)
	if len(got.Issues) != 1 || !strings.Contains(got.Issues[0].Msg, "no engine") {
		t.Errorf("issues = %+v", got.Issues)
	}
}

func TestShadowReported(t *testing.T) {
	p := &ir.Program{Contract: ir.Contract{"count": ir.Int}}
	// body contains the contract name so it counts as observed and no
	// program-level unused warning is added on top of the shadow error
	p.Stages = append(p.Stages, ir.Stage{
		Index: 1, Lang: "python", StartLine: 5, EndLine: 6,
		Body: []string{"def f(count): return 1"},
	})
	r := mockResolver(mockEngine{
		name: "python", reads: ir.VarSet{},
		analyzeErr: errShadow("count"),
	})
	got := (Analyzer{Engines: r}).Run(p)
	if len(got.Issues) != 1 {
		t.Fatalf("issues = %+v, want 1", got.Issues)
	}
	if !strings.Contains(got.Issues[0].Msg, "shadow") {
		t.Errorf("msg = %q, want shadow mentioned", got.Issues[0].Msg)
	}
}

func TestZeroErrorSample(t *testing.T) {
	p := &ir.Program{Contract: ir.Contract{"count": ir.Int}}
	p.Stages = append(p.Stages, *mkStage(1, "shell", 5))
	r := mockResolver(mockEngine{name: "shell", reads: ir.VarSet{"count": true}, writes: ir.VarSet{"count": true}})
	if got := (Analyzer{Engines: r}).Run(p); !got.OK() {
		t.Errorf("expected clean program, got %+v", got.Issues)
	}

	rep := Report{Issues: []Issue{{Stage: 2, Var: "b", Line: 9, Msg: "x"},
		{Stage: 1, Var: "a", Line: 0, Msg: "y"}}}
	var sb strings.Builder
	rep.Print(&sb)
	out := sb.String()
	if !strings.HasPrefix(out, "error: stage 1 var \"a\":") || !strings.Contains(out, "error: stage 2 line 9 var \"b\":") {
		t.Errorf("report output = %q", out)
	}
}

func TestWarningDoesNotBlock(t *testing.T) {
	rep := Report{Issues: []Issue{{Var: "x", Severity: SevWarning, Msg: "w"}}}
	if rep.HasErrors() {
		t.Errorf("warning-only report must not have errors")
	}
	if !rep.OK() {
		t.Errorf("warning-only report must be OK (warnings never block)")
	}
	var sb strings.Builder
	rep.Print(&sb)
	// program-level warning (Stage 0): no stage clause, warning prefix
	if !strings.HasPrefix(sb.String(), "warning: var \"x\":") {
		t.Errorf("output = %q, want warning prefix without stage", sb.String())
	}
}

func TestUnusedContractWarning(t *testing.T) {
	// total is neither inferred by the stage nor lexically present ->
	// program-level unused warning; count is written -> no issue.
	p := &ir.Program{Contract: ir.Contract{"count": ir.Int, "total": ir.Float}}
	p.Stages = append(p.Stages, *mkStage(1, "shell", 5))
	r := mockResolver(mockEngine{name: "shell", writes: ir.VarSet{"count": true}})
	got := (Analyzer{Engines: r}).Run(p)
	if got.HasErrors() {
		t.Fatalf("unused warning must not be an error: %+v", got.Issues)
	}
	if len(got.Issues) != 1 {
		t.Fatalf("issues = %+v, want exactly 1 (unused warning)", got.Issues)
	}
	it := got.Issues[0]
	if it.Stage != 0 || it.Var != "total" || it.Severity != SevWarning {
		t.Errorf("issue = %+v, want program-level warning for total", it)
	}
	if !strings.Contains(it.Msg, "declared but never") {
		t.Errorf("msg = %q, want unused-contract wording", it.Msg)
	}
}

func TestObservedNotUnused(t *testing.T) {
	// total only appears in the body (comment) but is not inferred:
	// lexical presence suppresses the unused warning (over-approximation).
	p := &ir.Program{Contract: ir.Contract{"total": ir.Float}}
	st := mkStage(1, "shell", 5)
	st.Body = []string{"# total is handled elsewhere"}
	p.Stages = append(p.Stages, *st)
	r := mockResolver(mockEngine{name: "shell"})
	got := (Analyzer{Engines: r}).Run(p)
	for _, it := range got.Issues {
		if it.Var == "total" && strings.Contains(it.Msg, "declared but never") {
			t.Errorf("observed name must not get unused warning: %+v", it)
		}
	}
}

func TestReadBeforeWriteStillError(t *testing.T) {
	p := &ir.Program{Contract: ir.Contract{"count": ir.Int}}
	p.Stages = append(p.Stages, *mkStage(1, "shell", 5))
	r := mockResolver(mockEngine{name: "shell", reads: ir.VarSet{"count": true}})
	got := (Analyzer{Engines: r}).Run(p)
	if !got.HasErrors() {
		t.Fatalf("read-before-write must remain a blocking error: %+v", got.Issues)
	}
	if got.Issues[0].Severity != SevError {
		t.Errorf("read-before-write severity = %v, want error", got.Issues[0].Severity)
	}
}

func TestObservedNotInferredWarning(t *testing.T) {
	// M12: count appears in the stage source but the engine inferred
	// neither read nor write -> per-stage warning (no error).
	p := &ir.Program{Contract: ir.Contract{"count": ir.Int}}
	st := mkStage(1, "shell", 5)
	st.Body = []string{"echo $count"}
	p.Stages = append(p.Stages, *st)
	r := mockResolver(mockEngine{name: "shell"})
	got := (Analyzer{Engines: r}).Run(p)
	if got.HasErrors() {
		t.Fatalf("M12 warning must not be an error: %+v", got.Issues)
	}
	if len(got.Issues) != 1 {
		t.Fatalf("issues = %+v, want exactly 1 (M12 warning)", got.Issues)
	}
	it := got.Issues[0]
	if it.Stage != 1 || it.Var != "count" || it.Severity != SevWarning {
		t.Errorf("issue = %+v, want stage-1 warning for count", it)
	}
	if !strings.Contains(it.Msg, "was not inferred as read or write") {
		t.Errorf("msg = %q, want M12 wording", it.Msg)
	}
}

func TestM12SuppressedWhenEngineErrors(t *testing.T) {
	// M12 + M11 interaction: the engine error explains the stage, so the
	// M12 warning is suppressed; the body mention keeps the unused
	// warning away too. Exactly one issue: the engine error.
	p := &ir.Program{Contract: ir.Contract{"count": ir.Int}}
	st := mkStage(1, "python", 5)
	st.Body = []string{"def f(count): return 1"}
	p.Stages = append(p.Stages, *st)
	r := mockResolver(mockEngine{name: "python", analyzeErr: errShadow("count")})
	got := (Analyzer{Engines: r}).Run(p)
	if len(got.Issues) != 1 {
		t.Fatalf("issues = %+v, want exactly 1 (engine error only)", got.Issues)
	}
	if got.Issues[0].Severity != SevError {
		t.Errorf("issue = %+v, want the engine error", got.Issues[0])
	}
}

func TestM12NoWarningWhenInferred(t *testing.T) {
	// count is observed AND inferred (read+write in the same stage:
	// writes are recorded before the read check, so no
	// read-before-write error) -> no M12 warning, no unused
	// warning: clean report.
	p := &ir.Program{Contract: ir.Contract{"count": ir.Int}}
	st := mkStage(1, "shell", 5)
	st.Body = []string{"echo $count"}
	p.Stages = append(p.Stages, *st)
	r := mockResolver(mockEngine{name: "shell", reads: ir.VarSet{"count": true},
		writes: ir.VarSet{"count": true}})
	got := (Analyzer{Engines: r}).Run(p)
	if len(got.Issues) != 0 {
		t.Errorf("issues = %+v, want none (observed and inferred)", got.Issues)
	}
}
