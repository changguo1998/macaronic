package golang

import (
    "strings"
    "testing"

    "github.com/changguo1998/macaronic/internal/ir"
)

func TestAnalyzeReadWrite(t *testing.T) {
    c := ir.Contract{"count": ir.Int, "msg": ir.Str, "ok": ir.Bool}
    cases := []struct {
        name     string
        body     []string
        wantR    []string
        wantW    []string
        wantShad string // "" = no shadow
    }{
        {
            name:  "read only",
            body:  []string{`fmt.Println(count, msg)`},
            wantR: []string{"count", "msg"},
        },
        {
            name:  "plain assignment",
            body:  []string{`count = 7`, `msg = "hi"`},
            wantW: []string{"count", "msg"},
        },
        {
            name:  "compound and inc",
            body:  []string{`count += 1`, `count++`},
            wantR: []string{"count"},
            wantW: []string{"count"},
        },
        {
            name:  "read-modify-write",
            body:  []string{`count = count + 1`},
            wantR: []string{"count"},
            wantW: []string{"count"},
        },
        {
            name:     "shadow new binding",
            body:     []string{`count := 5`},
            wantShad: "count",
        },
        {
            name:  "comment ignored",
            body:  []string{`// count = 5`, `fmt.Println(msg)`, `count++`},
            wantR: []string{"count", "msg"},
            wantW: []string{"count"},
        },
        {
            name:  "comparison not a write",
            body:  []string{`fmt.Println(count == 3)`},
            wantR: []string{"count"},
        },
        {
            name:  "local assigned in := not shadow",
            body:  []string{`x := 1`, `fmt.Println(count, x)`},
            wantR: []string{"count"},
        },
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            st := &ir.Stage{Index: 1, Lang: "go", StartLine: 10, Body: tc.body}
            reads, writes, err := (Engine{}).Analyze(st, c)
            if tc.wantShad != "" {
                if err == nil || !strings.Contains(err.Error(), tc.wantShad) {
                    t.Fatalf("want shadow %q, got err=%v", tc.wantShad, err)
                }
                return
            }
            if err != nil {
                t.Fatalf("unexpected error: %v", err)
            }
            assertSet(t, "reads", reads, tc.wantR)
            assertSet(t, "writes", writes, tc.wantW)
        })
    }
}

func assertSet(t *testing.T, label string, got ir.VarSet, want []string) {
    t.Helper()
    if len(got) != len(want) {
        t.Fatalf("%s = %v, want %v", label, got, want)
    }
    for _, n := range want {
        if !got[n] {
            t.Errorf("%s missing %q (got %v)", label, n, got)
        }
    }
}
