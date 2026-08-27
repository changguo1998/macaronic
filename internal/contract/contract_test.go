package contract

import (
    "strings"
    "testing"

    "github.com/changguo1998/macaronic/internal/ir"
)

func TestParse(t *testing.T) {
    cases := []struct {
        name string
        head string
        want map[string]ir.BasicType // nil means expect error
        err  string                  // substring of error message
    }{
        {
            name: "valid",
            head: `[contract]
count = "int"
total = "float"
ok = "bool"
msg = "str"`,
            want: map[string]ir.BasicType{
                "count": ir.Int, "total": ir.Float,
                "ok": ir.Bool, "msg": ir.Str,
            },
        },
        {"missing table", "", nil, "no [contract] table"},
        {"unknown type", `[contract]
count = "long"`, nil, "unknown type"},
        {"bad name", `[contract]
1x = "int"`, nil, "not a valid identifier"},
        {"dup key", `[contract]
count = "int"
count = "str"`, nil, ""}, // toml rejects duplicate keys
        {"non string value", `[contract]
count = 123`, nil, ""}, // map[string]string decode error
    }

    for _, c := range cases {
        got, err := Parse(strings.Split(c.head, "\n"))
        if c.want == nil {
            if err == nil {
                t.Errorf("%s: Parse succeeded, want error", c.name)
                continue
            }
            if c.err != "" && !strings.Contains(err.Error(), c.err) {
                t.Errorf("%s: err = %v, want substring %q", c.name, err, c.err)
            }
            continue
        }
        if err != nil {
            t.Errorf("%s: unexpected error %v", c.name, err)
            continue
        }
        if len(got) != len(c.want) {
            t.Errorf("%s: contract = %v, want %v", c.name, got, c.want)
            continue
        }
        for k, v := range c.want {
            if got[k] != v {
                t.Errorf("%s: contract[%q] = %v, want %v", c.name, k, got[k], v)
            }
        }
    }
}
