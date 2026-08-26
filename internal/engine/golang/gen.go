package golang

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/changguo1998/macaronic/internal/ir"
)

const (
	// binaryName is the built executable in each stage dir.
	binaryName = "stage"
	// goFile is the generated source name, also the sourcemap key.
	goFile = "main.go"
	// buildErrorsName is where Emit stores failed go build output.
	buildErrorsName = "build-errors.txt"
)

// codecHelpers is self-contained byte codec code (docs §10) injected
// into every generated stage because a stage cannot import
// internal/codec (internal package rule). It mirrors internal/codec
// byte for byte; golang tests assert golden-byte alignment (T8.3).
const codecHelpers = `// self-contained codec (ABI: docs/architecture.md §10)
func mReadFile(path string) ([]byte, error) { return os.ReadFile(path) }
func mWriteFile(path string, b []byte) error {
	return os.WriteFile(path, b, 0o644)
}
func mLeUint32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
func mLeUint64(b []byte) uint64 {
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}
func mPutUint32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}
func mPutUint64(b []byte, v uint64) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
	b[4] = byte(v >> 32)
	b[5] = byte(v >> 40)
	b[6] = byte(v >> 48)
	b[7] = byte(v >> 56)
}
func mReadInt64(path string) (int64, error) {
	b, err := mReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(b) < 8 {
		return 0, fmt.Errorf("state %s: %d bytes, want 8", path, len(b))
	}
	return int64(mLeUint64(b)), nil
}
func mReadFloat64(path string) (float64, error) {
	b, err := mReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(b) < 8 {
		return 0, fmt.Errorf("state %s: %d bytes, want 8", path, len(b))
	}
	return math.Float64frombits(mLeUint64(b)), nil
}
func mReadBool(path string) (bool, error) {
	b, err := mReadFile(path)
	if err != nil {
		return false, err
	}
	if len(b) < 1 {
		return false, fmt.Errorf("state %s: empty", path)
	}
	return b[0] != 0, nil
}
func mReadStr(path string) (string, error) {
	b, err := mReadFile(path)
	if err != nil {
		return "", err
	}
	if len(b) < 4 {
		return "", fmt.Errorf("state %s: %d bytes, want length prefix", path, len(b))
	}
	n := int(mLeUint32(b[:4]))
	if int(n) < 0 || 4+n > len(b) {
		return "", fmt.Errorf("state %s: bad string length %d", path, n)
	}
	return string(b[4 : 4+n]), nil
}
func mWriteInt64(path string, v int64) error {
	b := make([]byte, 8)
	mPutUint64(b, uint64(v))
	return mWriteFile(path, b)
}
func mWriteFloat64(path string, v float64) error {
	b := make([]byte, 8)
	mPutUint64(b, math.Float64bits(v))
	return mWriteFile(path, b)
}
func mWriteBool(path string, v bool) error {
	b := make([]byte, 1)
	if v {
		b[0] = 1
	}
	return mWriteFile(path, b)
}
func mWriteStr(path string, s string) error {
	b := make([]byte, 4+len(s))
	mPutUint32(b, uint32(len(s)))
	copy(b[4:], s)
	return mWriteFile(path, b)
}
func mFail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
`

// genLine is one line of generated source plus its origin tag.
type genLine struct {
	text string
	src  int
	kind ir.OriginKind
}

// RunCommand implements engine.Engine: run.sh invokes the built
// binary (not `go run`); the binary resolves state/ relative to its
// own location, so cwd does not matter.
func (Engine) RunCommand(stageDir string) []string {
	return []string{filepath.Join(stageDir, binaryName)}
}

// Emit implements engine.Engine: it writes main.go into stageDir,
// records source-map entries, then runs `go build`. Build output is
// saved to build-errors.txt on failure so the caller can run
// ParseDiagnostics on it.
func (e Engine) Emit(st *ir.Stage, c ir.Contract, stageDir, stateDir string,
	sm *ir.SourceMap) error {
	reads, writes, shadow := analyzeBody(st.Body, c)
	if shadow != "" {
		return fmt.Errorf("go stage%d: `:=` new binding shadows %q", st.Index, shadow)
	}
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return err
	}
	gl := e.generate(st, c, reads, writes)
	mainPath := filepath.Join(stageDir, goFile)
	if err := writeGenLines(mainPath, gl); err != nil {
		return err
	}
	fillSourceMap(sm, gl)
	return e.build(stageDir)
}

// build compiles main.go in stageDir into the stage binary. On
// failure it persists stderr for ParseDiagnostics and returns a
// non-nil error.
func (Engine) build(stageDir string) error {
	cmd := exec.Command("go", "build", "-o", binaryName, goFile)
	cmd.Dir = stageDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.WriteFile(filepath.Join(stageDir, buildErrorsName), out, 0o644)
		return fmt.Errorf("go build stage failed:\n%s", out)
	}
	return nil
}

// generate assembles the stage source, tagging each line's origin.
func (Engine) generate(st *ir.Stage, c ir.Contract, reads, writes ir.VarSet) []genLine {
	syn := func(text string) genLine {
		return genLine{text: text, kind: ir.OrigSynthetic}
	}
	src := func(text string, line int) genLine {
		return genLine{text: text, src: line, kind: ir.OrigSource}
	}
	gl := []genLine{
		syn("package main"),
		syn(""),
		syn("import ("),
		syn("\t\"fmt\""),
		syn("\t\"math\""),
		syn("\t\"os\""),
		syn("\t\"path/filepath\""),
		syn(")"),
		syn(""),
	}
	for _, h := range strings.Split(strings.TrimSuffix(codecHelpers, "\n"), "\n") {
		gl = append(gl, syn(h))
	}
	gl = append(gl, syn(""), syn("func main() {"),
		syn(`	stateDir := filepath.Join(filepath.Dir(os.Args[0]), "..", "state")`))

	// deterministic declarations for reads ∪ writes
	for _, n := range sortedNames(union(reads, writes)) {
		gl = append(gl, syn(fmt.Sprintf("\tvar %s %s", n, goType(c[n]))))
	}

	// prologue reads (type-aware)
	for _, n := range sortedNames(reads) {
		gl = append(gl, readStmt(n, c[n])...)
	}

	// user body lines, mapped 1:1 to source lines
	for i, body := range st.Body {
		gl = append(gl, src("\t"+body, st.StartLine+1+i))
	}

	// epilogue writes (type-aware)
	for _, n := range sortedNames(writes) {
		gl = append(gl, writeStmt(n, c[n])...)
	}

	gl = append(gl, syn("}"))
	return gl
}

// readStmt emits prologue lines reading var from its state file.
func readStmt(name string, t ir.BasicType) []genLine {
	s := ir.OrigSynthetic
	return []genLine{
		{text: "\t{", kind: s},
		{text: fmt.Sprintf("\t\tv, err := %s(filepath.Join(stateDir, %q))", readHelper(t), name), kind: s},
		{text: "\t\tif err != nil {", kind: s},
		{text: "\t\t\tmFail(err)", kind: s},
		{text: "\t\t}", kind: s},
		{text: fmt.Sprintf("\t\t%s = v", name), kind: s},
		{text: "\t}", kind: s},
	}
}

// writeStmt emits epilogue lines writing var back to its state file.
func writeStmt(name string, t ir.BasicType) []genLine {
	s := ir.OrigSynthetic
	return []genLine{
		{text: fmt.Sprintf("\tif err := %s(filepath.Join(stateDir, %q), %s); err != nil {",
			writeHelper(t), name, name), kind: s},
		{text: "\t\tmFail(err)", kind: s},
		{text: "\t}", kind: s},
	}
}

// readHelper returns the generated read function for a type.
func readHelper(t ir.BasicType) string {
	switch t {
	case ir.Int:
		return "mReadInt64"
	case ir.Float:
		return "mReadFloat64"
	case ir.Bool:
		return "mReadBool"
	case ir.Str:
		return "mReadStr"
	}
	return "mReadStr"
}

// writeHelper returns the generated write function for a type.
func writeHelper(t ir.BasicType) string {
	switch t {
	case ir.Int:
		return "mWriteInt64"
	case ir.Float:
		return "mWriteFloat64"
	case ir.Bool:
		return "mWriteBool"
	case ir.Str:
		return "mWriteStr"
	}
	return "mWriteStr"
}

// goType maps a contract type to the Go type used in static decls.
func goType(t ir.BasicType) string {
	switch t {
	case ir.Int:
		return "int64"
	case ir.Float:
		return "float64"
	case ir.Bool:
		return "bool"
	case ir.Str:
		return "string"
	}
	return "string"
}

// union merges two variable sets.
func union(a, b ir.VarSet) ir.VarSet {
	m := ir.VarSet{}
	for k := range a {
		m[k] = true
	}
	for k := range b {
		m[k] = true
	}
	return m
}

// sortedNames returns set keys in sorted order.
func sortedNames(set ir.VarSet) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// writeGenLines writes the tagged line list to path.
func writeGenLines(path string, gl []genLine) error {
	var b strings.Builder
	for _, l := range gl {
		b.WriteString(l.text)
		b.WriteString("\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// fillSourceMap records origin entries under "main.go:<line>".
func fillSourceMap(sm *ir.SourceMap, gl []genLine) {
	if *sm == nil {
		*sm = ir.SourceMap{}
	}
	for i, l := range gl {
		(*sm)[goFile+":"+strconv.Itoa(i+1)] = ir.SourceMapEntry{
			SourceLine: l.src, Kind: l.kind,
		}
	}
}

// compile diagnostics format: [./]main.go:line:col: msg
var goCompileRe = regexp.MustCompile(`^(?:\./)?([A-Za-z0-9_./~-]+\.go):(\d+)(?::(\d+))?:? *(.*)$`)

// runtime stack frame format: "\t/path/to/main.go:line +0x.."
var goStackRe = regexp.MustCompile(`^\s*([A-Za-z0-9_./~-]+\.go):(\d+)(?:\s+\+0x[0-9a-f]+)?$`)

// panic message line.
var goPanicRe = regexp.MustCompile(`^panic:\s*(.*)$`)

// ParseDiagnostics implements engine.Engine: it handles both go
// build errors (main.go:line:col: msg) and runtime panics (stack
// frames). Spans carry the generated line/col; the caller maps them
// through the source map.
func (Engine) ParseDiagnostics(stderr []byte) []ir.Diagnostic {
	lines := strings.Split(string(stderr), "\n")
	panicMsg := ""
	for _, l := range lines {
		if m := goPanicRe.FindStringSubmatch(l); m != nil {
			panicMsg = m[1]
			break
		}
	}
	var out []ir.Diagnostic
	for _, l := range lines {
		if m := goCompileRe.FindStringSubmatch(l); m != nil {
			line, _ := strconv.Atoi(m[2])
			col, _ := strconv.Atoi(m[3])
			msg := strings.TrimSpace(m[4])
			if msg == "" {
				msg = l
			}
			out = append(out, mkDiag(m[1], line, col, msg))
		} else if m := goStackRe.FindStringSubmatch(l); m != nil {
			line, _ := strconv.Atoi(m[2])
			msg := panicMsg
			if msg == "" {
				msg = "runtime error"
			}
			out = append(out, mkDiag(m[1], line, 0, msg))
		}
	}
	return out
}

func mkDiag(file string, line, col int, msg string) ir.Diagnostic {
	span := &ir.Span{StartLine: line, StartCol: col, EndLine: line, EndCol: col}
	return ir.Diagnostic{
		Msg:  fmt.Sprintf("%s:%d:%d: %s", file, line, col, msg),
		Span: span,
	}
}
