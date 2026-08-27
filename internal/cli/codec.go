// CLI for the codec helper subcommand:
//
//	macaronic codec read  <state-file> <type>
//	macaronic codec write <state-file> <type> <value>
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
  macaronic codec read       <state-file> <type>
  macaronic codec write      <state-file> <type> <value>
  macaronic codec read-list  <state-file> <list-type>
  macaronic codec write-list <state-file> <list-type> <value>...

类型：int | float | bool | str | int[] | float[] | bool[] | str[]
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
	var file, typ string
	switch op {
	case "read", "read-list":
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
	case "write-list":
		if len(rest) < 3 {
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
	switch op {
	case "read":
		return codecRead(file, t, stdout, stderr)
	case "read-list":
		if !ir.IsList(t) {
			fmt.Fprintln(stderr, "macaronic codec: read-list requires a list type")
			return exitUsage
		}
		return codecReadList(file, t, stdout, stderr)
	case "write-list":
		if !ir.IsList(t) {
			fmt.Fprintln(stderr, "macaronic codec: write-list requires a list type")
			return exitUsage
		}
		return codecWriteList(file, t, rest[3:], stderr)
	default:
		return codecWrite(file, t, rest[3], stdout, stderr)
	}
}

func codecReadList(path string, t ir.BasicType, stdout, stderr io.Writer) int {
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
	emit := func(s string) error {
		if _, err := io.WriteString(stdout, s); err != nil {
			return err
		}
		_, err := stdout.Write([]byte{0})
		return err
	}
	switch x := v.(type) {
	case []int64:
		for _, n := range x {
			if err := emit(strconv.FormatInt(n, 10)); err != nil {
				fmt.Fprintf(stderr, "macaronic codec: %v\n", err)
				return exitFail
			}
		}
	case []float64:
		for _, n := range x {
			if err := emit(strconv.FormatFloat(n, 'g', -1, 64)); err != nil {
				fmt.Fprintf(stderr, "macaronic codec: %v\n", err)
				return exitFail
			}
		}
	case []bool:
		for _, b := range x {
			if err := emit(strconv.FormatBool(b)); err != nil {
				fmt.Fprintf(stderr, "macaronic codec: %v\n", err)
				return exitFail
			}
		}
	case []string:
		for _, s := range x {
			if err := emit(s); err != nil {
				fmt.Fprintf(stderr, "macaronic codec: %v\n", err)
				return exitFail
			}
		}
	}
	return exitOK
}

func codecWriteList(path string, t ir.BasicType, args []string, stderr io.Writer) int {
	elem, ok := ir.ElementType(t)
	if !ok {
		fmt.Fprintf(stderr, "macaronic codec: unknown list type %q\n", t)
		return exitUsage
	}
	var values any
	switch elem {
	case ir.Int:
		out := make([]int64, len(args))
		for i, arg := range args {
			v, err := parseValue(elem, arg)
			if err != nil {
				fmt.Fprintf(stderr, "macaronic codec: %v\n", err)
				return exitUsage
			}
			out[i] = v.(int64)
		}
		values = out
	case ir.Float:
		out := make([]float64, len(args))
		for i, arg := range args {
			v, err := parseValue(elem, arg)
			if err != nil {
				fmt.Fprintf(stderr, "macaronic codec: %v\n", err)
				return exitUsage
			}
			out[i] = v.(float64)
		}
		values = out
	case ir.Bool:
		out := make([]bool, len(args))
		for i, arg := range args {
			v, err := parseValue(elem, arg)
			if err != nil {
				fmt.Fprintf(stderr, "macaronic codec: %v\n", err)
				return exitUsage
			}
			out[i] = v.(bool)
		}
		values = out
	case ir.Str:
		values = append([]string(nil), args...)
	}
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(stderr, "macaronic codec: %v\n", err)
		return exitFail
	}
	defer f.Close()
	if err := codec.Write(f, t, values); err != nil {
		fmt.Fprintf(stderr, "macaronic codec: %v\n", err)
		return exitFail
	}
	return exitOK
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
	if elem, ok := ir.ElementType(ir.BasicType(s)); ok {
		return ir.ListOf(elem), nil
	}
	return "", fmt.Errorf("unknown type %q (want int|float|bool|str or one-dimensional list)", s)
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
