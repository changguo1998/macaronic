package sourcemap

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/changguo1998/macaronic/internal/ir"
)

func TestAddResolve(t *testing.T) {
	b := New()
	b.AddEntry("stage1/run.sh", 3, 42, ir.OrigSource)
	b.AddEntry("stage1/run.sh", 4, 0, ir.OrigSynthetic)
	// repeated same key overwrites
	b.AddEntry("stage1/run.sh", 3, 43, ir.OrigSource)

	if e, ok := b.Resolve("stage1/run.sh", 3); !ok || e.SourceLine != 43 || e.Kind != ir.OrigSource {
		t.Errorf("resolve 3 = %+v", e)
	}
	if e, ok := b.Resolve("stage1/run.sh", 4); !ok || e.Kind != ir.OrigSynthetic {
		t.Errorf("resolve 4 = %+v", e)
	}
	if _, ok := b.Resolve("stage1/run.sh", 99); ok {
		t.Errorf("line 99 should not resolve")
	}
	if b.Len() != 2 {
		t.Errorf("Len = %d, want 2 (overwrite, not merge)", b.Len())
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	b := New()
	b.AddEntry("a/gen.sh", 1, 7, ir.OrigSource)
	b.AddEntry("b/gen.py", 2, 9, ir.OrigSynthetic)

	data, err := b.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	// deterministic: two marshals byte-identical
	data2, err := b.Marshal()
	if err != nil || string(data) != string(data2) {
		t.Errorf("deterministic marshal broken: %q vs %q", data, data2)
	}

	// round trip preserves entries
	bb, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := bb.Resolve("a/gen.sh", 1); !ok || e.SourceLine != 7 {
		t.Errorf("after parse: %+v", e)
	}
	if e, ok := bb.Resolve("b/gen.py", 2); !ok || e.Kind != ir.OrigSynthetic {
		t.Errorf("after parse: %+v", e)
	}

	// tamper -> hash mismatch
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	ents := m["entries"].(map[string]any)
	delete(ents, "a/gen.sh:1")
	tampered, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(tampered); err == nil {
		t.Errorf("tampered sourcemap should fail parse")
	} else if !strings.Contains(err.Error(), "hash mismatch") {
		t.Errorf("err = %v", err)
	}
}

func TestSourceLineZeroSynthetic(t *testing.T) {
	b := New()
	b.AddEntry("gen", 1, 0, ir.OrigSynthetic)
	e, ok := b.Resolve("gen", 1)
	if !ok || e.SourceLine != 0 || e.Kind != ir.OrigSynthetic {
		t.Errorf("resolve = %+v, ok=%v", e, ok)
	}
}
