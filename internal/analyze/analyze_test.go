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
    p.Stages = append(p.Stages, *mkStage(1, "python", 5))
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
    if !strings.HasPrefix(out, "stage 1 var \"a\":") || !strings.Contains(out, "stage 2 line 9 var \"b\":") {
        t.Errorf("report output = %q", out)
    }
}
