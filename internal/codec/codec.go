// Package codec implements the on-disk binary encoding ("script-local
// ABI") for the four basic types. Every language engine converts
// contract variables through this exact byte format so cross-language
// state files interoperate; see docs/architecture.md §10.
package codec

import (
    "encoding/binary"
    "fmt"
    "io"
    "math"

    "github.com/changguo1998/macaronic/internal/ir"
)

// Byte layout (little-endian, fixed widths):
//
//    int64   8 bytes, little-endian two's complement
//    float64 8 bytes, IEEE-754 little-endian
//    bool    1 byte, 0 or 1
//    str     4-byte little-endian byte-length + raw UTF-8 bytes
const (
    intSize    = 8
    f64Size    = 8
    boolSize   = 1
    strLenSize = 4
)

// WriteInt writes v as 8-byte LE.
func WriteInt(w io.Writer, v int64) error {
    var b [intSize]byte
    binary.LittleEndian.PutUint64(b[:], uint64(v))
    _, err := w.Write(b[:])
    return err
}

// ReadInt reads an 8-byte LE value.
func ReadInt(r io.Reader) (int64, error) {
    var b [intSize]byte
    if _, err := io.ReadFull(r, b[:]); err != nil {
        return 0, err
    }
    return int64(binary.LittleEndian.Uint64(b[:])), nil
}

// WriteFloat64 writes v as 8-byte LE IEEE-754.
func WriteFloat64(w io.Writer, v float64) error {
    var b [f64Size]byte
    binary.LittleEndian.PutUint64(b[:], math.Float64bits(v))
    _, err := w.Write(b[:])
    return err
}

// ReadFloat64 reads an 8-byte LE IEEE-754 value.
func ReadFloat64(r io.Reader) (float64, error) {
    var b [f64Size]byte
    if _, err := io.ReadFull(r, b[:]); err != nil {
        return 0, err
    }
    return math.Float64frombits(binary.LittleEndian.Uint64(b[:])), nil
}

// WriteBool writes v as 1 byte (1/0).
func WriteBool(w io.Writer, v bool) error {
    b := byte(0)
    if v {
        b = 1
    }
    _, err := w.Write([]byte{b})
    return err
}

// ReadBool reads a 1-byte value (nonzero = true).
func ReadBool(r io.Reader) (bool, error) {
    var b [boolSize]byte
    if _, err := io.ReadFull(r, b[:]); err != nil {
        return false, err
    }
    return b[0] != 0, nil
}

// WriteStr writes 4-byte LE length + UTF-8 bytes.
func WriteStr(w io.Writer, s string) error {
    if len(s) > math.MaxUint32 {
        return fmt.Errorf("string too long for codec (length %d)", len(s))
    }
    var lb [strLenSize]byte
    binary.LittleEndian.PutUint32(lb[:], uint32(len(s)))
    if _, err := w.Write(lb[:]); err != nil {
        return err
    }
    _, err := io.WriteString(w, s)
    return err
}

// ReadStr reads length-prefixed UTF-8.
func ReadStr(r io.Reader) (string, error) {
    var lb [strLenSize]byte
    if _, err := io.ReadFull(r, lb[:]); err != nil {
        return "", err
    }
    n := binary.LittleEndian.Uint32(lb[:])
    if n > 1<<30 {
        return "", fmt.Errorf("implausible string length %d", n)
    }
    buf := make([]byte, n)
    if _, err := io.ReadFull(r, buf); err != nil {
        return "", err
    }
    return string(buf), nil
}

// Write dispatches a typed value to its codec. t must match the Go
// type of v (int64/float64/bool/string).
func Write(w io.Writer, t ir.BasicType, v any) error {
    switch t {
    case ir.Int:
        i, ok := v.(int64)
        if !ok {
            return fmt.Errorf("codec int: got %T", v)
        }
        return WriteInt(w, i)
    case ir.Float:
        f, ok := v.(float64)
        if !ok {
            return fmt.Errorf("codec float: got %T", v)
        }
        return WriteFloat64(w, f)
    case ir.Bool:
        b, ok := v.(bool)
        if !ok {
            return fmt.Errorf("codec bool: got %T", v)
        }
        return WriteBool(w, b)
    case ir.Str:
        s, ok := v.(string)
        if !ok {
            return fmt.Errorf("codec str: got %T", v)
        }
        return WriteStr(w, s)
    default:
        return fmt.Errorf("codec: unknown type %q", t)
    }
}

// Read dispatches a typed read.
func Read(r io.Reader, t ir.BasicType) (any, error) {
    switch t {
    case ir.Int:
        return ReadInt(r)
    case ir.Float:
        return ReadFloat64(r)
    case ir.Bool:
        return ReadBool(r)
    case ir.Str:
        return ReadStr(r)
    default:
        return nil, fmt.Errorf("codec: unknown type %q", t)
    }
}
