package emit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "demo.mac.run")
	ws := NewWS(root, 2)
	if err := ws.Create(); err != nil {
		t.Fatal(err)
	}
	for _, d := range append([]string{root, ws.StateDir}, ws.Stages...) {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Errorf("missing dir %s: %v", d, err)
		}
	}
}

func TestClearKeepsDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "demo.mac.run")
	ws := NewWS(root, 1)
	if err := ws.Create(); err != nil {
		t.Fatal(err)
	}
	st := filepath.Join(ws.StateDir, "count.macint")
	if err := os.WriteFile(st, []byte{1, 2, 3}, DefaultFileMode); err != nil {
		t.Fatal(err)
	}
	if err := ws.ClearState(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(st); !os.IsNotExist(err) {
		t.Errorf("state file should be removed: %v", err)
	}
	if fi, err := os.Stat(ws.StateDir); err != nil || !fi.IsDir() {
		t.Errorf("state dir should still exist: %v", err)
	}
}

func TestWriteRunScriptOrder(t *testing.T) {
	cmds := [][]string{
		{"bash", "stage1/run.sh"},
		{"python3", "stage2/run.py"},
		{},
		{"go run", "stage3/run.go"},
	}
	var b strings.Builder
	if err := WriteRunScript(&b, cmds); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	// order preserved, empty cmd skipped
	if !strings.Contains(out, "'bash' 'stage1/run.sh'") ||
		!strings.Contains(out, "'python3' 'stage2/run.py'") ||
		strings.Contains(out, "''") {
		t.Errorf("run.sh mismatch:\n%s", out)
	}
	if i1, i2 := strings.Index(out, "stage1"), strings.Index(out, "stage2"); i1 > i2 {
		t.Errorf("stage order not preserved")
	}
}
