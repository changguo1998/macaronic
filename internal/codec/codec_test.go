package codec_test

import (
	"bytes"
	"math"
	"reflect"
	"testing"

	"github.com/changguo1998/macaronic/internal/codec"
	"github.com/changguo1998/macaronic/internal/ir"
)

// constantByteLayout pins the ABI bytes so any engine reimplementing
// codec can diff against these golden values (T5.5).
func TestConstantBytes(t *testing.T) {
	var b bytes.Buffer
	must(t, codec.Write(&b, ir.Int, int64(1)))
	wantInt := []byte{1, 0, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(b.Bytes(), wantInt) {
		t.Errorf("int(1) bytes = %v, want %v", b.Bytes(), wantInt)
	}

	b.Reset()
	must(t, codec.Write(&b, ir.Float, float64(1)))
	wantF := []byte{0, 0, 0, 0, 0, 0, 240, 63}
	if !bytes.Equal(b.Bytes(), wantF) {
		t.Errorf("float(1) bytes = %v, want %v", b.Bytes(), wantF)
	}

	b.Reset()
	must(t, codec.Write(&b, ir.Bool, true))
	if !bytes.Equal(b.Bytes(), []byte{1}) {
		t.Errorf("bool(true) bytes = %v", b.Bytes())
	}

	b.Reset()
	must(t, codec.Write(&b, ir.Str, "hé"))
	wantStr := []byte{3, 0, 0, 0, 0x68, 0xC3, 0xA9} // LE length 3 + UTF-8 "h","é"
	if !bytes.Equal(b.Bytes(), wantStr) {
		t.Errorf("str(hé) bytes = %v, want %v", b.Bytes(), wantStr)
	}
}

func TestRoundTripBoundaries(t *testing.T) {
	ints := []int64{0, 1, -1, math.MaxInt64, math.MinInt64}
	for _, v := range ints {
		assertInt(t, v)
	}
	floats := []float64{0, -0, 1.5, math.MaxFloat64, math.SmallestNonzeroFloat64,
		math.Inf(1), math.Inf(-1), math.NaN(), math.Copysign(math.NaN(), -1)}
	for _, f := range floats {
		assertFloat(t, f)
	}
	bools := []bool{true, false}
	for _, b := range bools {
		assertBool(t, b)
	}
	strs := []string{"", "a", "héllo", "你好", string([]byte{0, 1, 2})}
	for _, s := range strs {
		assertStr(t, s)
	}
}

func assertInt(t *testing.T, v int64) {
	t.Helper()
	var b bytes.Buffer
	must(t, codec.Write(&b, ir.Int, v))
	got, err := codec.Read(&b, ir.Int)
	if err != nil || got.(int64) != v {
		t.Errorf("int round-trip %d -> %v (%v)", v, got, err)
	}
}

func assertFloat(t *testing.T, v float64) {
	t.Helper()
	var b bytes.Buffer
	must(t, codec.Write(&b, ir.Float, v))
	got, err := codec.Read(&b, ir.Float)
	if err != nil {
		t.Fatal(err)
	}
	g := got.(float64)
	if math.IsNaN(v) && math.IsNaN(g) {
		return // NaN round-trips as NaN
	}
	if g != v {
		t.Errorf("float round-trip %v -> %v", v, g)
	}
}

func assertBool(t *testing.T, v bool) {
	t.Helper()
	var b bytes.Buffer
	must(t, codec.Write(&b, ir.Bool, v))
	got, err := codec.Read(&b, ir.Bool)
	if err != nil || got.(bool) != v {
		t.Errorf("bool round-trip %v -> %v (%v)", v, got, err)
	}
}

func assertStr(t *testing.T, s string) {
	t.Helper()
	var b bytes.Buffer
	must(t, codec.Write(&b, ir.Str, s))
	got, err := codec.Read(&b, ir.Str)
	if err != nil || got.(string) != s {
		t.Errorf("str round-trip %q -> %v (%v)", s, got, err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestListRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		typ  ir.BasicType
		in   any
	}{
		{"int", ir.ListOf(ir.Int), []int64{0, -2, 9}},
		{"float", ir.ListOf(ir.Float), []float64{1.5, -0.25}},
		{"bool", ir.ListOf(ir.Bool), []bool{true, false, true}},
		{"str", ir.ListOf(ir.Str), []string{"", "héllo", "世界"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			if err := codec.Write(&b, tc.typ, tc.in); err != nil {
				t.Fatal(err)
			}
			got, err := codec.Read(&b, tc.typ)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.in) {
				t.Errorf("got %#v, want %#v", got, tc.in)
			}
		})
	}
}

func TestListRejectsCorruptionAndOversizedCount(t *testing.T) {
	truncated := []byte{2, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0}
	if _, err := codec.Read(bytes.NewReader(truncated), ir.ListOf(ir.Int)); err == nil {
		t.Fatal("truncated list decoded successfully")
	}
	oversized := []byte{0xff, 0xff, 0xff, 0xff}
	if _, err := codec.Read(bytes.NewReader(oversized), ir.ListOf(ir.Int)); err == nil {
		t.Fatal("oversized list decoded successfully")
	}
	var b bytes.Buffer
	if err := codec.Write(&b, ir.ListOf(ir.Str), []string{"ok", "bad\x00"}); err == nil {
		t.Fatal("NUL string element accepted")
	}
}
