// Package sourcemap records and queries the mapping from generated
// file lines back to .mac source lines, with content-hash validation
// on serialized form.
package sourcemap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/changguo1998/macaronic/internal/ir"
)

// Builder accumulates source-map entries.
type Builder struct {
	m ir.SourceMap
}

// New returns an empty builder.
func New() *Builder {
	return &Builder{m: ir.SourceMap{}}
}

// AddEntry records that genLine of genFile corresponds to srcLine of
// the .mac (kind decides whether it is verbatim source or synthetic).
// Repeated add of the same (genFile, genLine) overwrites; multiple
// generated lines mapping to one source line are kept separate.
func (b *Builder) AddEntry(genFile string, genLine, srcLine int, kind ir.OriginKind) {
	b.m[key(genFile, genLine)] = ir.SourceMapEntry{SourceLine: srcLine, Kind: kind}
}

// Resolve maps genLine of genFile back to its origin.
func (b *Builder) Resolve(genFile string, genLine int) (ir.SourceMapEntry, bool) {
	e, ok := b.m[key(genFile, genLine)]
	return e, ok
}

// Len reports the number of entries.
func (b *Builder) Len() int { return len(b.m) }

// raw mirrors the on-disk JSON shape.
type raw struct {
	Entries map[string]ir.SourceMapEntry `json:"entries"`
	Sha256  string                       `json:"sha256"`
}

// Marshal returns deterministic JSON (sorted keys) with a content
// hash of the entries block.
func (b *Builder) Marshal() ([]byte, error) {
	entries := make(map[string]ir.SourceMapEntry, len(b.m))
	for k, v := range b.m {
		entries[k] = v
	}
	body, err := json.Marshal(entries)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	return json.Marshal(raw{Entries: entries, Sha256: hex.EncodeToString(sum[:])})
}

// Parse validates the content hash and loads entries into a fresh
// Builder. Integrity errors are returned as errors.
func Parse(data []byte) (*Builder, error) {
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	body, err := json.Marshal(r.Entries)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != r.Sha256 {
		return nil, fmt.Errorf("sourcemap content hash mismatch")
	}
	b := New()
	for k, v := range r.Entries {
		genFile, genLine, err := unkey(k)
		if err != nil {
			return nil, err
		}
		b.AddEntry(genFile, genLine, v.SourceLine, v.Kind)
	}
	return b, nil
}

func key(genFile string, genLine int) string {
	return genFile + ":" + strconv.Itoa(genLine)
}

func unkey(k string) (string, int, error) {
	i := strings.LastIndexByte(k, ':')
	if i < 0 {
		return "", 0, fmt.Errorf("bad sourcemap key %q", k)
	}
	n, err := strconv.Atoi(k[i+1:])
	if err != nil {
		return "", 0, fmt.Errorf("bad sourcemap line in key %q", k)
	}
	return k[:i], n, nil
}
