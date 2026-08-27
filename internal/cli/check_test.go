package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/changguo1998/macaronic/internal/engine"
	"github.com/changguo1998/macaronic/internal/ir"
)

// checkMock is a minimal engine backing the check command tests.
type checkMock struct {
	namese    string
	readVars  ir.VarSet
	writeVars ir.VarSet
}

func (m checkMock) Name() string { return m.namese }
func (m checkMock) Analyze(st *ir.Stage, c ir.Contract) (ir.VarSet, ir.VarSet, error) {
	// contract name "count" handled in mock: use a static analysis to
	// derive reads/writes trivially for tests.
	return m.readVars, m.writeVars, nil
}
func (m checkMock) Emit(st *ir.Stage, c ir.Contract, stageDir, stateDir string, sm *ir.SourceMap) error {
	return nil
}
func (m checkMock) RunCommand(stageDir string) []string { return []string{"true"} }
func (m checkMock) ParseDiagnostics(stderr []byte) []ir.Diagnostic {
	return nil
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCheckGoodFile(t *testing.T) {
	engine.Register(checkMock{namese: "shell", writeVars: ir.VarSet{"count": true}})
	path := writeFile(t, "ok.mac", "#!mac\n[contract]\ncount = \"int\"\n\n#!shell\ncount=$(wc -l < data.txt)\n")

	var out, err strings.Builder
	if code := Run([]string{"check", path}, &out, &err); code != exitOK {
		t.Errorf("check code = %d, want %d\nstdout=%q\nstderr=%q",
			code, exitOK, out.String(), err.String())
	}
	if out.Len() != 0 {
		t.Errorf("strict test: expected no stdout for clean file, got %q", out.String())
	}
}

func TestCheckReadBeforeWrite(t *testing.T) {
	engine.Register(checkMock{namese: "python", readVars: ir.VarSet{"count": true}})
	path := writeFile(t, "rbw.mac",
		"#!mac\n[contract]\ncount = \"int\"\n\n#!python\nprint(count)\n")
	var out strings.Builder
	var err strings.Builder
	if code := Run([]string{"check", path}, &out, &err); code != exitFail {
		t.Errorf("check code = %d, want %d\nstdout=%q", code, exitFail, out.String())
	}
	if !strings.Contains(out.String(), "read of \"count\" before any write") {
		t.Errorf("stdout = %q, want read-before-write issue", out.String())
	}
}
