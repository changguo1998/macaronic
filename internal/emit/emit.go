// Package emit creates the build workspace layout, manages state
// clearing and writes run.sh. Real stage-file emission is the
// engines' job (M6-M8); this package owns the framework-level bits.
package emit

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// Names of the well-known workspace members.
const (
	StateDirName    = "state"
	RunScriptName   = "run.sh"
	SourceMapName   = "sourcemap.json"
	RunScriptMode   = 0o755
	DirMode         = 0o755
	DefaultFileMode = 0o644
)

// WS names the fixed paths within one workspace.
type WS struct {
	Root     string
	StateDir string
	RunPath  string
	MapPath  string
	Stages   []string // stageN dirs, 1-based index position
}

// NewWS derives all paths for a script with nStages stages.
func NewWS(root string, nStages int) *WS {
	ws := &WS{Root: root}
	ws.StateDir = filepath.Join(root, StateDirName)
	ws.RunPath = filepath.Join(root, RunScriptName)
	ws.MapPath = filepath.Join(root, SourceMapName)
	for i := 1; i <= nStages; i++ {
		ws.Stages = append(ws.Stages, filepath.Join(root, "stage"+strconv.Itoa(i)))
	}
	return ws
}

// Create makes the directory tree, refusing to touch an existing
// conflicting path.
func (ws *WS) Create() error {
	for _, d := range append([]string{ws.Root, ws.StateDir}, ws.Stages...) {
		if err := os.MkdirAll(d, DirMode); err != nil {
			return fmt.Errorf("mkdir %s: %v", d, err)
		}
	}
	return nil
}

// ClearState removes the contents of state/ (keeping the directory
// itself), used by `run` before execution. `build` deliberately keeps
// state so a partial pipeline can be resumed.
func (ws *WS) ClearState() error {
	ents, err := os.ReadDir(ws.StateDir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if err := os.RemoveAll(filepath.Join(ws.StateDir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// WriteRunScript renders run.sh: a `set -eu` shell script invoking
// each cmds row in stage order. It is shared by build (used by run)
// so stage order is the single convention.
func WriteRunScript(w io.Writer, cmds [][]string) error {
	fmt.Fprintln(w, "#!/bin/sh")
	fmt.Fprintln(w, "set -eu")
	fmt.Fprintln(w, `cd "$(dirname "$0")"`)
	fmt.Fprintln(w)
	for _, cmd := range cmds {
		if len(cmd) == 0 {
			continue
		}
		fmt.Fprintln(w, shellQuote(cmd))
	}
	return nil
}

// shellQuote renders an argv slice as one shell command line.
func shellQuote(cmd []string) string {
	out := ""
	for _, a := range cmd {
		out += "'" + a + "' "
	}
	return out
}
