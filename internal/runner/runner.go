// Package runner executes compiled stages in order, stops on the
// first failure, and maps runtime errors back to source lines via
// engine diagnostics + the persisted source map.
package runner

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/changguo1998/macaronic/internal/sourcemap"
)

// StageResult records one failing stage.
type StageResult struct {
	Index    int
	ExitCode int
	Stderr   []byte
}

// Result of a whole run.
type Result struct {
	ExitCode int
	Failed   *StageResult // first failing stage (nil if all ok)
}

// Run executes args in order with Dir as cwd, capturing combined
// output, stopping at the first non-zero exit (T9.1). The failing
// stage's output is parked in Dir/failure.stderr (T9.4 keeps the
// scene for inspection). stdout, when non-nil, drains each stage's
// output as it finishes.
func Run(args [][]string, dirs []string, stdout func([]byte)) Result {
	for i, argv := range args {
		if len(argv) == 0 {
			continue
		}
		c := exec.Command(argv[0], argv[1:]...)
		if i < len(dirs) {
			c.Dir = dirs[i]
		}
		out, err := c.CombinedOutput()
		if stdout != nil {
			stdout(out)
		}
		if err != nil {
			code := exitCode(err)
			os.WriteFile(filepath.Join(c.Dir, "failure.stderr"), out, 0o644)
			return Result{ExitCode: code, Failed: &StageResult{Index: i + 1, ExitCode: code, Stderr: out}}
		}
	}
	return Result{ExitCode: 0}
}

func exitCode(err error) int {
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return 1
}

// BackmapEntry is a one-stage lookup result: source line for a
// generated file line (T9.3).
type BackmapEntry struct {
	SourceLine int
	Kind       string // "source" | "synthetic"
}

// Backmap resolves a generated (genFile, genLine) pair to its source
// line through the map builder.
func Backmap(b *sourcemap.Builder, genFile string, genLine int) (BackmapEntry, bool) {
	e, ok := b.Resolve(genFile, genLine)
	if !ok {
		return BackmapEntry{}, false
	}
	k := "synthetic"
	if e.Kind == 0 {
		k = "source"
	}
	return BackmapEntry{SourceLine: e.SourceLine, Kind: k}, true
}
