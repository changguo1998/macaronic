package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func shellCommand(script string) []string {
	return []string{"sh", "-c", script}
}

func TestRunStopsAtFirstFailureAndPreservesOutput(t *testing.T) {
	root := t.TempDir()
	dirs := []string{filepath.Join(root, "stage1"), filepath.Join(root, "stage2"), filepath.Join(root, "stage3")}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	args := [][]string{
		shellCommand("printf one"),
		shellCommand("printf two >&2; exit 7"),
		shellCommand("touch ran"),
	}
	var forwarded bytes.Buffer
	got := Run(args, dirs, func(out []byte) { forwarded.Write(out) })
	if got.ExitCode != 7 || got.Failed == nil {
		t.Fatalf("result = %+v, want exit 7 with failure", got)
	}
	if got.Failed.Index != 2 || got.Failed.ExitCode != 7 || string(got.Failed.Stderr) != "two" {
		t.Errorf("failed = %+v, want stage 2, exit 7, stderr two", got.Failed)
	}
	if forwarded.String() != "onetwo" {
		t.Errorf("forwarded = %q, want %q", forwarded.String(), "onetwo")
	}
	stored, err := os.ReadFile(filepath.Join(dirs[1], "failure.stderr"))
	if err != nil || string(stored) != "two" {
		t.Errorf("failure.stderr = %q, err=%v", stored, err)
	}
	if _, err := os.Stat(filepath.Join(dirs[2], "ran")); !os.IsNotExist(err) {
		t.Errorf("later stage ran: stat error = %v", err)
	}
}

func TestRunPropagatesCommandNotFound(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")
	got := Run([][]string{{missing}}, []string{dir}, nil)
	if got.ExitCode != 1 || got.Failed == nil {
		t.Fatalf("result = %+v, want command-not-found exit 1", got)
	}
	if got.Failed.Index != 1 || got.Failed.ExitCode != 1 {
		t.Errorf("failed = %+v, want stage 1 exit 1", got.Failed)
	}
}

func TestRunReportsFailureStderrPersistenceError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing-stage-dir")
	got := Run([][]string{shellCommand("true")}, []string{dir}, nil)
	if got.Failed == nil {
		t.Fatalf("result = %+v, want failure from invalid cwd", got)
	}
	if got.Failed.FailureStderrErr == nil {
		t.Fatalf("failed = %+v, want failure.stderr persistence error", got.Failed)
	}
	if got.ExitCode != 1 || got.Failed.ExitCode != 1 {
		t.Errorf("result = %+v, want process/setup failure exit 1", got)
	}
}

func TestRunUsesStageDirectories(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "stage")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	got := Run([][]string{shellCommand("pwd > cwd")}, []string{dir}, nil)
	if got.Failed != nil {
		t.Fatalf("Run failed: %+v", got)
	}
	cwd, err := os.ReadFile(filepath.Join(dir, "cwd"))
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes.TrimSpace(cwd)) != dir {
		t.Errorf("stage cwd = %q, want %q", cwd, dir)
	}
}
