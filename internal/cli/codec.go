// CLI for the codec helper subcommand:
//
//    macaronic codec read  <state-file> <type>
//    macaronic codec write <state-file> <type> <value>
//
// Both directions talk to the same internal/codec abstraction so
// engines producing/consuming state files stay interoperable with the
// Go pipeline.
package cli

import (
    "fmt"
    "io"
    "os"
    "strconv"
    "strings"

    "github.com/changguo1998/macaronic/internal/codec"
    "github.com/changguo1998/macaronic/internal/ir"
)

const codecUsage = `macaronic codec：二进制状态文件读写辅助

用法：
  macaronic codec read  <state-file> <type>
  macaronic codec write <state-file> <type> <value>

类型：int | float | bool | str
`

func runCodec(rest []string, stdout, stderr io.Writer) int {
    for _, r := range rest {
        if r == "-h" || r == "--help" {
            fmt.Fprint(stdout, codecUsage)
            return exitOK
        }
    }
    if len(rest) < 1 {
        fmt.Fprint(stderr, codecUsage)
        return exitUsage
    }
    op := rest[0]
    // read:  [read file type]; write: [write file type value]
    var file, typ string
    switch op {
    case "read":
        if len(rest) != 3 {
            fmt.Fprint(stderr, codecUsage)
            return exitUsage
        }
        file, typ = rest[1], rest[2]
    case "write":
        if len(rest) != 4 {
            fmt.Fprint(stderr, codecUsage)
            return exitUsage
        }
        file, typ = rest[1], rest[2]
    default:
        fmt.Fprintf(stderr, "macaronic codec: unknown op %q\n", op)
        return exitUsage
    }

    t, err := parseType(typ)
    if err != nil {
        fmt.Fprintf(stderr, "macaronic codec: %v\n", err)
        return exitUsage
    }
    if op == "read" {
        return codecRead(file, t, stdout, stderr)
    }
    return codecWrite(file, t, rest[3], stdout, stderr)
}

func parseType(s string) (ir.BasicType, error) {
    switch s {
    case "int":
        return ir.Int, nil
    case "float":
        return ir.Float, nil
    case "bool":
        return ir.Bool, nil
    case "str":
        return ir.Str, nil
    }
    return "", fmt.Errorf("unknown type %q (want int|float|bool|str)", s)
}

func codecRead(path string, t ir.BasicType, stdout, stderr io.Writer) int {
    f, err := os.Open(path)
    if err != nil {
        fmt.Fprintf(stderr, "macaronic codec: %v\n", err)
        return exitFail
    }
    defer f.Close()
    v, err := codec.Read(f, t)
    if err != nil {
        fmt.Fprintf(stderr, "macaronic codec: %v\n", err)
        return exitFail
    }
    renderValue(stdout, v)
    return exitOK
}

func codecWrite(path string, t ir.BasicType, value string, _, stderr io.Writer) int {
    v, err := parseValue(t, value)
    if err != nil {
        fmt.Fprintf(stderr, "macaronic codec: %v\n", err)
        return exitUsage
    }
    f, err := os.Create(path)
    if err != nil {
        fmt.Fprintf(stderr, "macaronic codec: %v\n", err)
        return exitFail
    }
    defer f.Close()
    if err := codec.Write(f, t, v); err != nil {
        fmt.Fprintf(stderr, "macaronic codec: %v\n", err)
        return exitFail
    }
    return exitOK
}

func parseValue(t ir.BasicType, s string) (any, error) {
    switch t {
    case ir.Int:
        v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
        if err != nil {
            return nil, fmt.Errorf("int value %q: %v", s, err)
        }
        return v, nil
    case ir.Float:
        v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
        if err != nil {
            return nil, fmt.Errorf("float value %q: %v", s, err)
        }
        return v, nil
    case ir.Bool:
        v, err := strconv.ParseBool(strings.TrimSpace(s))
        if err != nil {
            return nil, fmt.Errorf("bool value %q: %v", s, err)
        }
        return v, nil
    case ir.Str:
        return s, nil
    }
    return nil, fmt.Errorf("unreachable: unknown type")
}

func renderValue(w io.Writer, v any) {
    switch x := v.(type) {
    case int64:
        fmt.Fprintln(w, strconv.FormatInt(x, 10))
    case float64:
        fmt.Fprintln(w, strconv.FormatFloat(x, 'g', -1, 64))
    case bool:
        fmt.Fprintln(w, strconv.FormatBool(x))
    case string:
        fmt.Fprintln(w, x)
    default:
        fmt.Fprintln(w, x)
    }
}
