package source

import (
	"strings"
	"testing"
)

// lineString converts source text into lines without the phantom
// trailing element that strings.Split creates for a final newline.
func lineString(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func TestSplitHeadRules(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // substring of error; "" = expect success
	}{
		{"head missing", "#!shell\nfoo\n", "must be the first line"},
		{"head not top", "#!shell\n#!mac\nfoo\n", "must be the first line"},
		{"duplicate head", "#!mac\n[contract]\ncount = \"int\"\n#!mac\n", "duplicate #!mac"},
		{"empty file", "", "empty file"},
		{"marker only", "#!mac\n#!shell\n", ""},
		{"unknown lang", "#!mac\n[contract]\n#!ruby\n", "unknown block language"},
	}
	for _, c := range cases {
		_, _, err := Split("t.mac", lineString(c.src))
		if c.want == "" {
			if err != nil {
				t.Errorf("%s: unexpected error %v", c.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error = %v, want substring %q", c.name, err, c.want)
		}
	}
}

func TestSplitAndContent(t *testing.T) {
	src := "#!mac\n" +
		"[contract]\n" +
		"count = \"int\"\n" +
		"\n" +
		"#!shell\n" +
		"count=$(wc -l < data.txt)\n" +
		"echo done\n" +
		"\n" +
		"#!python\n" +
		"count += 1\n" +
		"print(count)\n" // no trailing newline
	head, stages, err := Split("t.mac", lineString(src))
	if err != nil {
		t.Fatal(err)
	}
	// head = everything after #!mac until first code marker (blank
	// line included, per block semantics).
	if got := strings.Join(head, "\n"); !strings.Contains(got, `count = "int"`) {
		t.Errorf("head = %q, want substring count = %q", got, `count = "int"`)
	}
	if len(stages) != 2 {
		t.Fatalf("stages len = %d, want 2", len(stages))
	}
	s1, s2 := stages[0], stages[1]
	if s1.Lang != "shell" || s1.StartLine != 5 || s1.EndLine != 8 {
		t.Errorf("stage1 = %+v", s1)
	}
	if got := strings.Join(s1.Body, "\n"); got != "count=$(wc -l < data.txt)\necho done\n" {
		t.Errorf("stage1 body = %q", got)
	}
	if s2.Lang != "python" || s2.StartLine != 9 || s2.EndLine != 11 {
		t.Errorf("stage2 = %+v", s2)
	}
	if got := strings.Join(s2.Body, "\n"); got != "count += 1\nprint(count)" {
		t.Errorf("stage2 body = %q", got)
	}
}

func TestEmptyAndTrailingBlocks(t *testing.T) {
	src := "#!mac\n#!shell\n#!python\nx=1\n#!go\n" // shell empty, python body, go empty+EOF
	_, stages, err := Split("t.mac", lineString(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 3 {
		t.Fatalf("stages len = %d, want 3", len(stages))
	}
	if len(stages[0].Body) != 0 || stages[0].Lang != "shell" {
		t.Errorf("stage1 = %+v", stages[0])
	}
	if len(stages[2].Body) != 0 || stages[2].Lang != "go" {
		t.Errorf("stage3 = %+v", stages[2])
	}
}
