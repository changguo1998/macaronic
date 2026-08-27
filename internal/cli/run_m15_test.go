package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/changguo1998/macaronic/internal/emit"
	"github.com/changguo1998/macaronic/internal/engine"
	golangengine "github.com/changguo1998/macaronic/internal/engine/golang"
	pythonengine "github.com/changguo1998/macaronic/internal/engine/python"
	"github.com/changguo1998/macaronic/internal/engine/shell"
)

func registerAllEngines() {
	engine.Register(shell.Engine{})
	engine.Register(pythonengine.Engine{})
	engine.Register(golangengine.Engine{})
}

func buildCLITestBinary(t *testing.T) string {
	t.Helper()
	gomod, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(strings.TrimSpace(string(gomod)))
	bin := filepath.Join(t.TempDir(), "macaronic")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/macaronic")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func copyFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunFailurePreservesFailureJSONAndBackmap(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found")
	}
	registerAllEngines()
	bin := buildCLITestBinary(t)
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	path := copyFixture(t, "runtime_failure_m15.mac")
	var out, errOut strings.Builder
	if code := Run([]string{"run", path}, &out, &errOut); code != exitFail {
		t.Fatalf("run code = %d, want failure\nstdout=%q\nstderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), ".mac: line 10:") {
		t.Errorf("stderr = %q, want source-line backmap", errOut.String())
	}
	root := strings.TrimSuffix(path, ".mac") + ".mac.run"
	var failure struct {
		StageIndex int    `json:"stage_index"`
		ExitCode   int    `json:"exit_code"`
		StderrPath string `json:"stderr_path"`
	}
	data, err := os.ReadFile(filepath.Join(root, "failure.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &failure); err != nil {
		t.Fatal(err)
	}
	if failure.StageIndex != 2 || failure.ExitCode != 1 {
		t.Errorf("failure = %+v, want stage 2 exit 1", failure)
	}
	stderr, err := os.ReadFile(failure.StderrPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stderr), "RuntimeError: boom") {
		t.Errorf("failure.stderr = %q, want RuntimeError", stderr)
	}
}

func TestRunWarningOnlyReachesExecution(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	registerAllEngines()
	bin := buildCLITestBinary(t)
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	path := copyFixture(t, "warn_only.mac")
	var out, errOut strings.Builder
	if code := Run([]string{"run", path}, &out, &errOut); code != exitOK {
		t.Fatalf("run code = %d, want 0\nstdout=%q\nstderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "warning:") || !strings.Contains(out.String(), ": ok") {
		t.Errorf("stdout = %q, stderr = %q, want warning and ok", out.String(), errOut.String())
	}
	state := filepath.Join(strings.TrimSuffix(path, ".mac")+".mac.run", "state", "count.macint")
	if _, err := os.Stat(state); err != nil {
		t.Errorf("run did not write state %s: %v", state, err)
	}
}

func TestRunStaticErrorDoesNotBuild(t *testing.T) {
	registerAllEngines()
	path := copyFixture(t, "read_before_write.mac")
	var out, errOut strings.Builder
	if code := Run([]string{"run", path}, &out, &errOut); code != exitFail {
		t.Fatalf("run code = %d, want failure\nstdout=%q\nstderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), `read of "count" before any write`) {
		t.Errorf("stderr = %q, want static dependency error", errOut.String())
	}
	if _, err := os.Stat(strings.TrimSuffix(path, ".mac") + ".mac.run"); !os.IsNotExist(err) {
		t.Errorf("static error created build workspace: stat error=%v", err)
	}
}

func TestRunStagesReportsPersistenceError(t *testing.T) {
	root := t.TempDir()
	ws := &emit.WS{
		Root:   root,
		Stages: []string{filepath.Join(root, "missing-stage")},
	}
	fr, _, err := runStages(ws, []string{"shell"}, [][]string{{"true"}}, nil)
	if fr == nil || err == nil {
		t.Fatalf("fr=%+v err=%v, want process and persistence failures", fr, err)
	}
	if fr.ExitCode != 1 || fr.FailureStderrErr == nil {
		t.Errorf("failed = %+v, want primary exit 1 and persistence error", fr)
	}
	if !strings.Contains(err.Error(), "persist failure.stderr") {
		t.Errorf("error = %q, want persistence context", err)
	}
}
