// build / run subcommand drivers: shared emit pipeline with the
// state-clearing difference (`build` keeps state, `run` clears it),
// failure.json lifecycle and stale-source detection.
package cli

import (
    "encoding/json"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strconv"
    "strings"

    "github.com/changguo1998/macaronic/internal/analyze"
    "github.com/changguo1998/macaronic/internal/contract"
    "github.com/changguo1998/macaronic/internal/emit"
    "github.com/changguo1998/macaronic/internal/engine"
    "github.com/changguo1998/macaronic/internal/ir"
    "github.com/changguo1998/macaronic/internal/lock"
    "github.com/changguo1998/macaronic/internal/runner"
    "github.com/changguo1998/macaronic/internal/source"
    "github.com/changguo1998/macaronic/internal/sourcemap"
)

// load reads and validates one .mac file.
func loadMac(path string) (*ir.Program, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
    head, stages, err := source.Split(path, lines)
    if err != nil {
        return nil, err
    }
    c, err := contract.Parse(head)
    if err != nil {
        return nil, err
    }
    return &ir.Program{Path: path, Contract: c, Stages: stages}, nil
}

// runBuild emits every stage into the workspace. clearState decides
// whether state/ is wiped (run=yes) or preserved (build=no). Returns
// workspace, per-stage argv, and the filled builder for back-mapping.
func runBuild(p *ir.Program, clearState bool) (*emit.WS, [][]string, *sourcemap.Builder, error) {
    // Absolute paths: engines embed stateDir/into emitted scripts,
    // and runner chdirs to stageDir, so relative roots would break.
    dir, err := filepath.Abs(filepath.Dir(p.Path))
    if err != nil {
        return nil, nil, nil, err
    }
    base := strings.TrimSuffix(filepath.Base(p.Path), filepath.Ext(p.Path))
    ws := emit.NewWS(filepath.Join(dir, base+".mac.run"), len(p.Stages))
    if err := ws.Create(); err != nil {
        return nil, nil, nil, err
    }
    if clearState {
        if err := ws.ClearState(); err != nil {
            return nil, nil, nil, err
        }
    }

    um := sourcemap.New()
    cmds := make([][]string, 0, len(p.Stages))
    for i := range p.Stages {
        st := &p.Stages[i]
        eng, ok := engine.Get(st.Lang)
        if !ok {
            return nil, nil, nil, fmt.Errorf("stage %d: no engine registered for %q", st.Index, st.Lang)
        }
        if err := eng.Emit(st, p.Contract, ws.Stages[i], ws.StateDir, um.Raw()); err != nil {
            return nil, nil, nil, fmt.Errorf("stage %d emit: %v", st.Index, err)
        }
        cmds = append(cmds, eng.RunCommand(ws.Stages[i]))
    }

    f, err := os.Create(ws.RunPath)
    if err != nil {
        return nil, nil, nil, err
    }
    err = emit.WriteRunScript(f, cmds)
    f.Close()
    if err != nil {
        return nil, nil, nil, err
    }
    data, err := um.Marshal()
    if err != nil {
        return nil, nil, nil, err
    }
    if err := os.WriteFile(ws.MapPath, data, 0o644); err != nil {
        return nil, nil, nil, err
    }
    return ws, cmds, um, nil
}

// failure holds the failure.json payload (T9.5).
type failure struct {
    StageIndex int    `json:"stage_index"`
    ExitCode   int    `json:"exit_code"`
    StderrPath string `json:"stderr_path"`
}

// writeFailure parks failure.json in the workspace.
func writeFailure(ws *emit.WS, fr *runner.StageResult) error {
    stderrPath := filepath.Join(ws.Stages[fr.Index-1], "failure.stderr")
    fp := filepath.Join(ws.Root, "failure.json")
    data, err := json.MarshalIndent(failure{StageIndex: fr.Index, ExitCode: fr.ExitCode, StderrPath: stderrPath}, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(fp, data, 0o644)
}

// removeFailure deletes failure.json on a fresh run.
func removeFailure(ws *emit.WS) {
    os.Remove(filepath.Join(ws.Root, "failure.json"))
}

// acquireLock takes the script-level exclusive lock (fail-fast).
func acquireLock(path string) (func(), error) {
    return lock.LockFile(path + ".run.lock")
}

// checkOK runs the analyzers; on issues, prints and returns false.
func checkOK(p *ir.Program, stdout io.Writer) bool {
    rep := (analyze.Analyzer{Engines: engine.Get}).Run(p)
    rep.Print(stdout)
    return rep.OK()
}

// runStages executes per-stage argv, mapping a failing stage's
// diagnostics to source lines (T9.3) before returning. langs[i] is
// the language of stage i+1.
func runStages(ws *emit.WS, langs []string, cmds [][]string, stdout io.Writer) (*runner.StageResult, string, error) {
    res := runner.Run(cmds, ws.Stages, func(out []byte) {
        if stdout != nil {
            stdout.Write(out)
        }
    })
    if res.Failed == nil {
        return nil, "", nil
    }
    backmapped := backmapFailed(ws, langs, res.Failed)
    return res.Failed, backmapped, nil
}

// backmapFailed uses the failing stage's engine (via langs) to parse
// its stderr into diagnostics, then resolves each generated
// "genFile:genLine" through the persisted source map, producing a
// .mac line reference. Falls back to raw stderr on any miss.
func backmapFailed(ws *emit.WS, langs []string, fr *runner.StageResult) string {
    raw := string(fr.Stderr)
    idx := fr.Index - 1
    if idx < 0 || idx >= len(langs) {
        return raw
    }
    eng, ok := engine.Get(langs[idx])
    if !ok {
        return raw
    }
    diags := eng.ParseDiagnostics(fr.Stderr)
    if len(diags) == 0 {
        return raw
    }
    data, err := os.ReadFile(ws.MapPath)
    if err != nil {
        return raw
    }
    bm, err := sourcemap.Parse(data)
    if err != nil {
        return raw
    }
    var b strings.Builder
    for _, d := range diags {
        gen, line, msg := splitDiag(d.Msg)
        if e, ok := bm.Resolve(gen, line); ok && e.Kind == 0 {
            b.WriteString(fmt.Sprintf(".mac: line %d: %s\n", e.SourceLine, msg))
        } else {
            b.WriteString(d.Msg + "\n")
        }
    }
    if b.Len() == 0 {
        return raw
    }
    return b.String()
}

// splitDiag decodes a "genFile:genLine:message" diagnostic into its
// parts; genFile and line are used only when parseable.
func splitDiag(m string) (gen string, line int, msg string) {
    a := strings.SplitN(m, ":", 3)
    if len(a) < 3 {
        return "", 0, m
    }
    n, err := strconv.Atoi(a[1])
    if err != nil {
        return a[0], 0, a[2]
    }
    return a[0], n, a[2]
}

// runBuildCmd implements `macaronic build <script>`: emits stages,
// keeps state/, and fails fast on check errors.
func runBuildCmd(path string, stdout, stderr io.Writer) int {
    release, err := acquireLock(path)
    if err != nil {
        fmt.Fprintf(stderr, "macaronic build: %v\n", err)
        return exitFail
    }
    defer release()

    p, err := loadMac(path)
    if err != nil {
        fmt.Fprintf(stderr, "macaronic build: %v\n", err)
        return exitFail
    }
    if !checkOK(p, stdout) {
        return exitFail
    }
    if _, _, _, err := runBuild(p, false); err != nil {
        fmt.Fprintf(stderr, "macaronic build: %v\n", err)
        return exitFail
    }
    fmt.Fprintf(stdout, "built %s\n", strings.TrimSuffix(filepath.Base(path), ".mac")+".mac.run")
    return exitOK
}

// runCmd implements `macaronic run <script>`: parse → check → build →
// clear state → execute run.sh-equivalent stages (T9.4). Stale or
// missing artifacts are rebuilt each run, so stale execution cannot
// happen (T9.6). failure.json is removed at start and written on
// failure (T9.5).
func runCmd(path string, stdout, stderr io.Writer) int {
    release, err := acquireLock(path)
    if err != nil {
        fmt.Fprintf(stderr, "macaronic run: %v\n", err)
        return exitFail
    }
    defer release()

    p, err := loadMac(path)
    if err != nil {
        fmt.Fprintf(stderr, "macaronic run: %v\n", err)
        return exitFail
    }
    if !checkOK(p, stderr) {
        return exitFail
    }
    ws, cmds, _, err := runBuild(p, true)
    if err != nil {
        fmt.Fprintf(stderr, "macaronic run: %v\n", err)
        return exitFail
    }
    removeFailure(ws)

    langs := make([]string, len(p.Stages))
    for i := range p.Stages {
        langs[i] = p.Stages[i].Lang
    }
    fmt.Fprintf(stdout, "%s: running %d stage(s)\n", filepath.Base(path), len(p.Stages))
    fr, backmapped, err := runStages(ws, langs, cmds, stdout)
    if fr == nil {
        fmt.Fprintf(stdout, "%s: ok\n", filepath.Base(path))
        return exitOK
    }
    if err != nil {
        fmt.Fprintf(stderr, "macaronic run: %v\n", err)
        return exitFail
    }
    // failure.json + back-mapped diagnostic
    if werr := writeFailure(ws, fr); werr != nil {
        fmt.Fprintf(stderr, "macaronic run: %v\n", werr)
        return exitFail
    }
    fmt.Fprintf(stderr, "stage %d failed (exit %d):\n%s", fr.Index, fr.ExitCode, backmapped)
    return exitFail
}
