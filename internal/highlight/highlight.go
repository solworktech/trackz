// Package highlight splits SQL text into syntax-highlighting spans using
// github.com/jessp01/gohighlight and its SQL definition
// (syntax_files/sql.yaml, copied unmodified from that repository). It
// returns plain data so any frontend can colour the spans its own way.
package highlight

import (
	_ "embed"
	"strings"
	"sync"

	hl "github.com/jessp01/gohighlight"
)

//go:embed syntax_files/sql.yaml
var sqlSyntax []byte

// Span is a run of text in one syntax group. Start and End are rune
// offsets into the whole text (End exclusive).
type Span struct {
	Start, End int
	Group      string // e.g. "statement", "constant.string"; "" for plain text
}

var (
	once   sync.Once
	sqlDef *hl.Def
	defErr error
	// gohighlight keeps group names in a package-level map that parsing
	// writes to; serialise all use of it.
	mu sync.Mutex
)

func load() {
	sqlDef, defErr = hl.ParseDef(sqlSyntax)
	if defErr == nil {
		hl.ResolveIncludes([]*hl.Def{sqlDef})
	}
}

// SQL returns the spans covering text, in order. Newlines are never part of
// a span; plain text has an empty Group. If the syntax definition cannot be
// loaded, the whole text is returned as plain spans.
func SQL(text string) []Span {
	once.Do(load)
	lines := strings.Split(text, "\n")
	var matches []hl.LineMatch
	if defErr == nil {
		mu.Lock()
		matches = hl.NewHighlighter(sqlDef).HighlightString(text)
		mu.Unlock()
	}
	var spans []Span
	offset := 0
	for i, line := range lines {
		runes := []rune(line)
		var m hl.LineMatch
		if i < len(matches) {
			m = matches[i]
		}
		group := ""
		start := 0
		for col := 0; col <= len(runes); col++ {
			g, changed := m[col]
			if !changed && col < len(runes) {
				continue
			}
			if col > start {
				spans = append(spans, Span{Start: offset + start, End: offset + col, Group: group})
			}
			if changed {
				mu.Lock()
				group = g.String()
				mu.Unlock()
			}
			start = col
		}
		offset += len(runes) + 1 // newline
	}
	return spans
}
