package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCodecWriteRead(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "count.macint")

	var out, err strings.Builder
	if code := Run([]string{"codec", "write", state, "int", "42"}, &out, &err); code != exitOK {
		t.Fatalf("codec write: code %d stderr=%s", code, err.String())
	}
	out.Reset()
	err.Reset()
	if code := Run([]string{"codec", "read", state, "int"}, &out, &err); code != exitOK {
		t.Fatalf("codec read: code %d stderr=%s", code, err.String())
	}
	if strings.TrimSpace(out.String()) != "42" {
		t.Errorf("read output = %q", out.String())
	}
}

func TestCodecErrors(t *testing.T) {
	var out, err strings.Builder
	// missing args -> usage error
	if code := Run([]string{"codec", "read"}, &out, &err); code != exitUsage {
		t.Errorf("code  = %d, want usage", code)
	}
	out.Reset()
	err.Reset()
	// wrong type -> usage error, small output
	if code := Run([]string{"codec", "write", "x", "long", "1"}, &out, &err); code != exitUsage {
		t.Errorf("type code = %d", code)
	}
	if !strings.Contains(err.String(), "unknown type") {
		t.Errorf("stderr = %q", err.String())
	}
	out.Reset()
	err.Reset()
	// bad int value -> usage error
	if code := Run([]string{"codec", "write", "x", "int", "abc"}, &out, &err); code != exitUsage {
		t.Errorf("value code = %d", code)
	}
	out.Reset()
	err.Reset()
	// non existent file read -> fail
	if code := Run([]string{"codec", "read", filepath.Join(t.TempDir(), "nope"), "int"}, &out, &err); code != exitFail {
		t.Errorf("read missing code = %d", code)
	}
}

func TestCodecRoundTripAllTypes(t *testing.T) {
	cases := []struct {
		typ   string
		value string
	}{
		{"int", "-7"},
		{"float", "3.25"},
		{"bool", "true"},
		{"str", "héllo 世界"},
	}
	dir := t.TempDir()
	for _, c := range cases {
		state := filepath.Join(dir, "v.mac"+c.typ)
		var out, err strings.Builder
		if code := Run([]string{"codec", "write", state, c.typ, c.value}, &out, &err); code != exitOK {
			t.Errorf("write %s/%s: %s", c.typ, c.value, err.String())
			continue
		}
		out.Reset()
		err.Reset()
		if code := Run([]string{"codec", "read", state, c.typ}, &out, &err); code != exitOK {
			t.Errorf("read %s: %s", c.typ, err.String())
			continue
		}
		if strings.TrimSpace(out.String()) != c.value {
			t.Errorf("%s round-trip: got %q want %q", c.typ, out.String(), c.value)
		}
	}
}

func TestCodecListBridge(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "values.macint[]")
	var out, err strings.Builder
	if code := Run([]string{"codec", "write-list", state, "int[]", "1", "-2", "9"}, &out, &err); code != exitOK {
		t.Fatalf("write-list: code %d stderr=%s", code, err.String())
	}
	out.Reset()
	err.Reset()
	if code := Run([]string{"codec", "read-list", state, "int[]"}, &out, &err); code != exitOK {
		t.Fatalf("read-list: code %d stderr=%s", code, err.String())
	}
	if out.String() != "1\x00-2\x009\x00" {
		t.Errorf("read-list output = %q", out.String())
	}

	state = filepath.Join(dir, "words.macstr[]")
	out.Reset()
	err.Reset()
	if code := Run([]string{"codec", "write-list", state, "str[]", "hello world", "line\nbreak"}, &out, &err); code != exitOK {
		t.Fatalf("write-list strings: code %d stderr=%s", code, err.String())
	}
	out.Reset()
	if code := Run([]string{"codec", "read-list", state, "str[]"}, &out, &err); code != exitOK {
		t.Fatalf("read-list strings: code %d stderr=%s", code, err.String())
	}
	if out.String() != "hello world\x00line\nbreak\x00" {
		t.Errorf("read-list strings = %q", out.String())
	}
}
