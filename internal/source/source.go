// Package source splits .mac source text into the head block and
// code stages according to #!lang block markers.
package source

import (
    "fmt"

    "github.com/changguo1998/macaronic/internal/ir"
)

// headMarker is the marker of the head (contract) block.
const headMarker = "#!mac"

// allowedLangs are block languages known in M2; unknown markers are
// reported as errors. Engines are glued to these names in later
// milestones (M6-M8).
var allowedLangs = map[string]bool{
    "shell":  true,
    "python": true,
    "go":     true,
}

// Split scans raw source lines and returns the head block body (the
// TOML-carrying lines after "#!mac", before the first code block) and
// the ordered code stages.
//
// Structural rules enforced here:
//   - "#!mac" must be line 1, and must appear exactly once
//   - every other line starting with "#!" is a block marker whose
//     language must be known
//   - a block ends at the next marker line or at EOF; empty blocks are
//     allowed; a trailing block needs no terminator
func Split(path string, lines []string) (head []string, stages []ir.Stage, err error) {
    if len(lines) == 0 {
        return nil, nil, fmt.Errorf("%s:1 empty file", path)
    }
    if lines[0] != headMarker {
        return nil, nil, fmt.Errorf("%s:1 head #!mac must be the first line", path)
    }
    for i := 0; i < len(lines); i++ {
        if lines[i] == headMarker && i != 0 {
            return nil, nil, fmt.Errorf("%s:%d duplicate #!mac", path, i+1)
        }
    }

    // Head body is everything after line 1 until the first code marker.
    var codeStart int
    for i := 1; i < len(lines); i++ {
        if isMarker(lines[i]) {
            codeStart = i
            break
        }
    }
    head = lines[1:codeStart]

    // Code blocks: each marker starts a stage; body runs to the next
    // marker or EOF.
    for i := codeStart; i < len(lines); {
        if !isMarker(lines[i]) {
            return nil, nil, fmt.Errorf("%s:%d non-marker line in code region", path, i+1)
        }
        lang := lines[i][2:]
        if !allowedLangs[lang] {
            return nil, nil, fmt.Errorf("%s:%d unknown block language %q", path, i+1, lang)
        }
        st := ir.Stage{
            Index:     len(stages) + 1,
            Lang:      lang,
            StartLine: i + 1,
            EndLine:   i + 1,
            Body:      []string{},
        }
        i++
        for i < len(lines) && !isMarker(lines[i]) {
            st.Body = append(st.Body, lines[i])
            st.EndLine = i + 1
            i++
        }
        stages = append(stages, st)
    }
    return head, stages, nil
}

// isMarker reports whether the line starts a block marker like "#!shell".
// A line longer than 1 char starting with "#!" counts (len>=2 guard);
// "#!" alone is treated as a marker too, and resolved as unknown lang.
func isMarker(line string) bool {
    return len(line) >= 2 && line[0] == '#' && line[1] == '!'
}
