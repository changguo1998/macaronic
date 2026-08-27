package contract

import (
	"strings"
	"testing"

	"github.com/changguo1998/macaronic/internal/ir"
	"github.com/changguo1998/macaronic/internal/source"
)

// TestIntegration runs the full M2 slicing + head parsing on a
// realistic .mac and asserts semantic equality (map key order is
// deliberately not part of the comparison).
func TestIntegration(t *testing.T) {
	src := `#!mac
[contract]
count = "int"
total = "float"
msg = "str"

#!shell
count=$(wc -l < data.txt)
total=$(wc -w < data.txt)

#!python
msg = f"count={count} total={total}"

#!go
_ = "placeholder until M8"
`
	lines := strings.Split(src, "\n")
	head, stages, err := source.Split("sample.mac", lines)
	if err != nil {
		t.Fatalf("source.Split: %v", err)
	}
	got, err := Parse(head)
	if err != nil {
		t.Fatalf("contract.Parse: %v", err)
	}
	// Determinism regression: slice and parse twice.
	head2, _, err := source.Split("sample.mac", lines)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := Parse(head2)
	if err != nil {
		t.Fatalf("contract.Parse: %v", err)
	}
	if len(got) != len(got2) {
		t.Fatalf("nondeterministic parse: %v vs %v", got, got2)
	}
	for k, v := range got2 {
		if got[k] != v {
			t.Errorf("nondeterministic parse at key %q: %v vs %v", k, got[k], v)
		}
	}

	want := ir.Contract{"count": ir.Int, "total": ir.Float, "msg": ir.Str}
	if len(got) != len(want) {
		t.Fatalf("contract = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("contract[%q] = %v, want %v", k, got[k], v)
		}
	}
	if n := len(stages); n != 3 {
		t.Fatalf("stages = %d, want 3", n)
	}
	if stages[0].Lang != "shell" || stages[1].Lang != "python" || stages[2].Lang != "go" {
		t.Errorf("stage langs = %+v", stages)
	}
}
