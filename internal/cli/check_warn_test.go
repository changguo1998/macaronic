package cli

import (
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/changguo1998/macaronic/internal/engine"
    "github.com/changguo1998/macaronic/internal/engine/shell"
)

// M11 CLI behavior: warnings are visible but never block; errors still
// block. The fixtures live in testdata/ and use the real shell engine
// (registered here, as main.go does in production) so the CLI path
// exercises actual inference, not mocks.

func TestCheckWarningOnlyExitsOK(t *testing.T) {
    engine.Register(shell.Engine{})
    path := filepath.Join("testdata", "warn_only.mac")
    var out, err strings.Builder
    if code := Run([]string{"check", path}, &out, &err); code != exitOK {
        t.Fatalf("check code = %d, want 0 (warnings do not block)\nstdout=%q stderr=%q",
            code, out.String(), err.String())
    }
    if !strings.Contains(out.String(), "warning:") {
        t.Errorf("stdout = %q, want a warning line", out.String())
    }
    if !strings.Contains(out.String(), `contract variable "total" declared but never`) {
        t.Errorf("stdout = %q, want unused-contract warning for total", out.String())
    }
}

func TestCheckErrorStillBlocks(t *testing.T) {
    engine.Register(shell.Engine{})
    path := filepath.Join("testdata", "read_before_write.mac")
    var out, err strings.Builder
    if code := Run([]string{"check", path}, &out, &err); code != exitFail {
        t.Fatalf("check code = %d, want 1 (errors still block)\nstdout=%q",
            code, out.String())
    }
    if !strings.Contains(out.String(), `read of "count" before any write`) {
        t.Errorf("stdout = %q, want read-before-write error", out.String())
    }
}

// TestBuildWarningOnlyExitsOK proves `build` is not blocked by warnings
// either (objective: build 仅被 error 阻断). The fixture is copied to a
// temp dir so emitted artifacts do not pollute testdata/.
func TestBuildWarningOnlyExitsOK(t *testing.T) {
    engine.Register(shell.Engine{})
    data, err := os.ReadFile(filepath.Join("testdata", "warn_only.mac"))
    if err != nil {
        t.Fatal(err)
    }
    path := filepath.Join(t.TempDir(), "warn_only.mac")
    if err := os.WriteFile(path, data, 0o644); err != nil {
        t.Fatal(err)
    }
    var out, errOut strings.Builder
    if code := Run([]string{"build", path}, &out, &errOut); code != exitOK {
        t.Fatalf("build code = %d, want 0 (warnings do not block)\nstdout=%q stderr=%q",
            code, out.String(), errOut.String())
    }
    if !strings.Contains(out.String(), "warning:") {
        t.Errorf("stdout = %q, want a warning line", out.String())
    }
}
