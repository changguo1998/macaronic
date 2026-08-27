// Package contract parses the .mac head block TOML into the cross-
// block variable contract.
package contract

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/changguo1998/macaronic/internal/ir"
)

// nameRe is the cross-language identifier rule: letters/digits/_
// starting with a letter or underscore. Keyword conflicts are resolved
// later (M3) per language.
var nameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// typeNames maps contract string values to ir types.
var typeNames = map[string]ir.BasicType{
	"int":     ir.Int,
	"float":   ir.Float,
	"bool":    ir.Bool,
	"str":     ir.Str,
	"int[]":   ir.ListOf(ir.Int),
	"float[]": ir.ListOf(ir.Float),
	"bool[]":  ir.ListOf(ir.Bool),
	"str[]":   ir.ListOf(ir.Str),
	// Accept the conventional spelling while keeping str[] canonical.
	"string[]": ir.ListOf(ir.Str),
}

// doc is the raw TOML shape accepted from the head block.
type doc struct {
	Contract map[string]string `toml:"contract"`
}

// Parse parses the head block lines into an ir.Contract. It returns
// only ir types (a map). Iteration order of the map is undefined;
// callers needing deterministic output must sort the keys themselves.
func Parse(head []string) (ir.Contract, error) {
	var d doc
	if err := toml.Unmarshal([]byte(strings.Join(head, "\n")), &d); err != nil {
		return nil, fmt.Errorf("head TOML: %v", err)
	}
	if len(d.Contract) == 0 {
		return nil, fmt.Errorf("no [contract] table")
	}
	contract := ir.Contract{}
	for name, typ := range d.Contract {
		if !nameRe.MatchString(name) {
			return nil, fmt.Errorf("contract key %q is not a valid identifier", name)
		}
		bt, ok := typeNames[typ]
		if !ok {
			return nil, fmt.Errorf("contract key %q has unknown type %q", name, typ)
		}
		contract[name] = bt
	}
	return contract, nil
}
