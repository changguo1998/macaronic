// Package python implements the Python language engine: contract
// variables must carry type annotations (x: int), missing annotation
// is a check error; Emit injects prologue reads / epilogue writes
// using an inline codec, and ParseDiagnostics understands Python
// tracebacks.
package python

import (
    "fmt"
    "os"
    "path/filepath"
    "regexp"
    "sort"
    "strconv"
    "strings"

    "github.com/changguo1998/macaronic/internal/ir"
)

// Engine is the Python backend.
type Engine struct{}

// Name returns "python".
func (Engine) Name() string { return "python" }

// varRefRe matches a contract var as a whole identifier token.
func varRefRe(name string) *regexp.Regexp {
    return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
}

// annoRe matches a declaration line "name: type" (optionally followed
// by "= ...").
func annoRe(name string) *regexp.Regexp {
    return regexp.MustCompile(`^\s*` + regexp.QuoteMeta(name) +
        `\s*:\s*(int|float|bool|str)\s*(=|$)`)
}

// assignRe matches an annotated assignment "name: type = ...".
func assignRe(name string) *regexp.Regexp {
    return regexp.MustCompile(`^\s*` + regexp.QuoteMeta(name) +
        `\s*:\s*(int|float|bool|str)\s*=`)
}

// plainAssignRe matches a non-annotated plain assignment
// "name = ..." (excludes "==" via character class).
func plainAssignRe(name string) *regexp.Regexp {
    return regexp.MustCompile(`^\s*` + regexp.QuoteMeta(name) + `\s*=[^=]`)
}

// augmentedAssignRe matches "name += ..." and friends.
func augmentedAssignRe(name string) *regexp.Regexp {
    return regexp.MustCompile(`^\s*` + regexp.QuoteMeta(name) +
        `\s*(?:\+=|-=|\*=|/=|//=|%=|&=|\|=|\^=|<<=|>>=)`)
}

// Analyze scans the block for contract-variable usage. Each contract
// variable referenced anywhere must have an annotation line; a bare
// use without annotation is a check error carrying the first used
// line. Sets follow the model: a variable is a write when the block
// has an annotated assignment “v: type = expr“ (not loaded from
// state; the stage produces it); otherwise, when referenced with a
// declaration “v: type“ or in expressions, it is a read and requires
// an annotation somewhere in the block.
func (Engine) Analyze(st *ir.Stage, c ir.Contract) (ir.VarSet, ir.VarSet, error) {
    reads := ir.VarSet{}
    writes := ir.VarSet{}
    names := make([]string, 0, len(c))
    for v := range c {
        names = append(names, v)
    }
    sort.Strings(names)
    for _, v := range names {
        refRe := varRefRe(v)
        anno := annoRe(v)
        ass := assignRe(v)
        hasAnno := false
        hasWrite := false
        firstIsWrite := false
        firstRef := -1
        for i, ln := range st.Body {
            if !refRe.MatchString(ln) {
                continue
            }
            if firstRef < 0 {
                // Sequential evaluation (architecture §6): the first
                // occurrence decides whether this block CONSUMES v
                // (read) or PRODUCES it (pure write). Only an
                // annotated/plain assignment whose RHS does not
                // reference v can be a pure write; augmented
                // assignments (x += ...) always read first.
                firstRef = i
                firstIsWrite = (ass.MatchString(ln) || plainAssignRe(v).MatchString(ln)) &&
                    !rhsHasRef(ln, v)
            }
            if anno.MatchString(ln) {
                hasAnno = true
            }
            // A write is an annotated assignment, a plain assignment
            // (x = ...), or an augmented assignment (x += ...) whose
            // block declares the variable. Only annotated lines count
            // against the missing-annotation error.
            if ass.MatchString(ln) || plainAssignRe(v).MatchString(ln) ||
                augmentedAssignRe(v).MatchString(ln) {
                hasWrite = true
            }
        }
        if firstRef < 0 {
            continue
        }
        if !hasAnno {
            return nil, nil, fmt.Errorf(
                "python: contract variable %q used without type annotation (line %d). Add `%s: %s`",
                v, st.StartLine+1+firstRef, v, c[v])
        }
        if !firstIsWrite {
            reads[v] = true
        }
        if hasWrite {
            writes[v] = true
        }
    }
    return reads, writes, nil
}

// rhsHasRef reports whether v appears after the first '=' on the
// line, i.e. on the right-hand side of an assignment.
func rhsHasRef(line, v string) bool {
    i := strings.Index(line, "=")
    if i < 0 {
        return false
    }
    return varRefRe(v).MatchString(line[i+1:])
}

// genFile is the emitted Python file name.
const genFile = "run.py"

// stateFileName is the state file naming convention shared by all
// engines: <name>.mac<type> (e.g. count.macint). Cross-engine state
// interop depends on this exact form.
func stateFileName(name string, t ir.BasicType) string {
    return name + ".mac" + string(t)
}

// Emit writes stageDir/run.py: prologue reads, user body, epilogue
// writes. State files live at stateDir/<var> and are encoded with the
// inline codec helpers below. Source-map entries: user lines are
// OrigSource, everything generated is OrigSynthetic.
func (e Engine) Emit(st *ir.Stage, c ir.Contract, stageDir, stateDir string, sm *ir.SourceMap) error {
    if sm != nil && *sm == nil {
        *sm = ir.SourceMap{}
    }
    reads, writes, err := e.Analyze(st, c)
    if err != nil {
        return err
    }

    var b strings.Builder
    line := 0
    emit := func(s string, srcLine int, kind ir.OriginKind) {
        b.WriteString(s)
        b.WriteString("\n")
        line++
        if sm != nil && srcLine > 0 {
            (*sm)[fmt.Sprintf("%s:%d", genFile, line)] = ir.SourceMapEntry{
                SourceLine: srcLine, Kind: kind}
        }
    }

    // Header + inline codec (synthetic).
    emit("# generated by macaronic - do not edit", 0, ir.OrigSynthetic)
    emit("import struct", 0, ir.OrigSynthetic)
    synthetic := []string{
        "def _mac_read(path, typ):",
        "    with open(path, 'rb') as f:",
        "        if typ == 'int':",
        "            return struct.unpack('<q', f.read(8))[0]",
        "        if typ == 'float':",
        "            return struct.unpack('<d', f.read(8))[0]",
        "        if typ == 'bool':",
        "            return struct.unpack('<?', f.read(1))[0]",
        "        if typ == 'str':",
        "            n = struct.unpack('<I', f.read(4))[0]",
        "            return f.read(n).decode('utf-8')",
        "        raise ValueError(typ)",
        "",
        "def _mac_write(path, typ, v):",
        "    if typ == 'int':",
        "        data = struct.pack('<q', int(v))",
        "    elif typ == 'float':",
        "        data = struct.pack('<d', float(v))",
        "    elif typ == 'bool':",
        "        data = struct.pack('<?', bool(v))",
        "    elif typ == 'str':",
        "        b = str(v).encode('utf-8')",
        "        data = struct.pack('<I', len(b)) + b",
        "    else:",
        "        raise ValueError(typ)",
        "    with open(path, 'wb') as f:",
        "        f.write(data)",
        "",
    }
    for _, l := range synthetic {
        emit(l, 0, ir.OrigSynthetic)
    }

    // Prologue reads (deterministic variable order).
    vars := sortedKeys(reads)
    if len(vars) > 0 {
        emit("", 0, ir.OrigSynthetic)
    }
    for _, name := range vars {
        emit(fmt.Sprintf("%s = _mac_read(%q, %q)", name,
            filepath.Join(stateDir, stateFileName(name, c[name])), string(c[name])), 0, ir.OrigSynthetic)
    }

    // User code (OrigSource per body line).
    for i, ln := range st.Body {
        emit(ln, st.StartLine+1+i, ir.OrigSource)
    }

    // Epilogue writes.
    vars = sortedKeys(writes)
    if len(vars) > 0 {
        emit("", 0, ir.OrigSynthetic)
    }
    for _, name := range vars {
        emit(fmt.Sprintf("_mac_write(%q, %q, %s)", filepath.Join(stateDir, stateFileName(name, c[name])),
            string(c[name]), name), 0, ir.OrigSynthetic)
    }

    if err := os.MkdirAll(stageDir, 0o755); err != nil {
        return err
    }
    return os.WriteFile(filepath.Join(stageDir, genFile), []byte(b.String()), 0o644)
}

// RunCommand returns the argv invoking the generated file.
func (Engine) RunCommand(stageDir string) []string {
    return []string{"python3", filepath.Join(stageDir, "run.py")}
}

// fileLineRe matches a traceback frame line like
//
//    File "run.py", line 3, in <module>
var fileLineRe = regexp.MustCompile(`^\s*File "([^"]+)", line (\d+)`)

// ParseDiagnostics extracts (genFile, line, message) from a Python
// traceback. The last File frame defines the failing generated line;
// the trailing exception line supplies the message.
func (Engine) ParseDiagnostics(stderr []byte) []ir.Diagnostic {
    var out []ir.Diagnostic
    lines := strings.Split(string(stderr), "\n")
    gen, line := "", 0
    for _, l := range lines {
        if m := fileLineRe.FindStringSubmatch(l); m != nil {
            gen, line = m[1], atoi(m[2])
            continue
        }
        if strings.HasPrefix(l, "Traceback") || strings.HasPrefix(l, "  ") ||
            l == "" || gen == "" {
            continue
        }
        if !strings.HasPrefix(l, "File ") {
            // top exception line
            msg := strings.TrimSpace(l)
            out = append(out, ir.Diagnostic{
                Msg: fmt.Sprintf("%s:%d: %s", gen, line, msg),
                Span: &ir.Span{StartLine: line, StartCol: 1,
                    EndLine: line, EndCol: 1},
            })
            return out
        }
    }
    if line == 0 {
        return nil
    }
    return out
}

func atoi(s string) int {
    n, _ := strconv.Atoi(s)
    return n
}

func sortedKeys(m ir.VarSet) []string {
    ks := make([]string, 0, len(m))
    for k := range m {
        ks = append(ks, k)
    }
    sort.Strings(ks)
    return ks
}
