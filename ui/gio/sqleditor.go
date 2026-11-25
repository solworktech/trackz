package gio

import (
	"image"
	"image/color"
	"strings"
	"unicode"

	"gioui.org/font"
	"gioui.org/font/opentype"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/solworktech/trackz/internal/highlight"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
)

// sqlEditor is a multi-line editor with SQL syntax highlighting.
//
// Gio's editor paints all of its text with a single material, so the
// editor draws its glyphs transparently (it still handles input, selection
// and scrolling) and the highlighted text is drawn on top at the positions
// the editor reports through Regions. The font is monospaced (Go Mono,
// bundled with Gio), so the overlay matches the editor's own layout.
type sqlEditor struct {
	ed widget.Editor

	click multiClick // double- and triple-click selection

	text    string
	runes   []rune
	spans   []highlight.Span
	regions []widget.Region
}

var monoFont = font.Font{Typeface: "Go Mono"}

// monoFaces returns the Go Mono faces bundled with Gio.
func monoFaces() []font.FontFace {
	var faces []font.FontFace
	for _, ttf := range [][]byte{gomono.TTF, gomonobold.TTF, gomonoitalic.TTF, gomonobolditalic.TTF} {
		fs, err := opentype.ParseCollection(ttf)
		if err != nil {
			continue
		}
		faces = append(faces, fs...)
	}
	return faces
}

const sqlTextSize = unit.Sp(14)

// syntaxColor maps gohighlight groups to colours for the current scheme.
func syntaxColor(group string) color.NRGBA {
	dark := cs.dark
	pick := func(light, darkC color.NRGBA) color.NRGBA {
		if dark {
			return darkC
		}
		return light
	}
	switch {
	case group == "":
		return cs.fg
	case strings.HasPrefix(group, "comment"):
		return cs.muted
	case strings.HasPrefix(group, "constant.string"):
		return pick(color.NRGBA{0x2e, 0x7d, 0x32, 0xff}, color.NRGBA{0x9c, 0xd6, 0x7a, 0xff})
	case strings.HasPrefix(group, "constant"):
		return pick(color.NRGBA{0xc4, 0x56, 0x00, 0xff}, color.NRGBA{0xf2, 0xa6, 0x5a, 0xff})
	case strings.HasPrefix(group, "statement"):
		return pick(color.NRGBA{0x3f, 0x51, 0xb5, 0xff}, color.NRGBA{0x8a, 0xb4, 0xf8, 0xff})
	case strings.HasPrefix(group, "type"):
		return pick(color.NRGBA{0x00, 0x83, 0x8f, 0xff}, color.NRGBA{0x5e, 0xd3, 0xd8, 0xff})
	case strings.HasPrefix(group, "preproc"):
		return pick(color.NRGBA{0x8e, 0x24, 0xaa, 0xff}, color.NRGBA{0xd1, 0x8c, 0xe8, 0xff})
	}
	return cs.fg
}

func colorMaterial(gtx C, c color.NRGBA) op.CallOp {
	m := op.Record(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	return m.Stop()
}

func (s *sqlEditor) refresh() {
	if t := s.ed.Text(); t != s.text || s.runes == nil {
		s.text = t
		s.runes = []rune(t)
		s.spans = highlight.SQL(t)
	}
}

func (s *sqlEditor) layout(gtx C, th *material.Theme, hint string) D {
	s.refresh()
	transparent := colorMaterial(gtx, color.NRGBA{})
	selection := colorMaterial(gtx, cs.editorSel)
	dims := s.ed.Layout(gtx, th.Shaper, monoFont, sqlTextSize, transparent, selection)
	s.refresh() // the layout may have applied edits
	s.click.layout(gtx, &s.ed, dims)

	defer clip.Rect{Max: dims.Size}.Push(gtx.Ops).Pop()
	ascent, descent := s.metrics(gtx, th)
	if len(s.runes) == 0 {
		if hint != "" {
			s.drawRun(gtx, th, hint, image.Pt(0, ascent), cs.muted, ascent)
		}
	}
	for _, sp := range s.spans {
		run := s.runes[sp.Start:sp.End]
		if strings.TrimFunc(string(run), unicode.IsSpace) == "" {
			continue
		}
		col := syntaxColor(sp.Group)
		s.regions = s.ed.Regions(sp.Start, sp.End, s.regions[:0])
		if len(s.regions) == 1 {
			r := s.regions[0]
			s.drawRun(gtx, th, string(run), image.Pt(r.Bounds.Min.X, r.Bounds.Max.Y-r.Baseline), col, ascent)
			continue
		}
		// The run wraps over several visual lines: place rune by rune.
		for i := sp.Start; i < sp.End; i++ {
			s.regions = s.ed.Regions(i, i+1, s.regions[:0])
			if len(s.regions) == 0 || unicode.IsSpace(s.runes[i]) {
				continue
			}
			r := s.regions[0]
			s.drawRun(gtx, th, string(s.runes[i]), image.Pt(r.Bounds.Min.X, r.Bounds.Max.Y-r.Baseline), col, ascent)
		}
	}
	// The editor paints its caret with the (transparent) text material.
	if gtx.Focused(&s.ed) && s.ed.SelectionLen() == 0 && gtx.Enabled() {
		p := s.ed.CaretCoords().Round()
		w := max(gtx.Dp(1), 1)
		fillRect(gtx, image.Rect(p.X, p.Y-ascent, p.X+w, p.Y+descent), cs.fg)
	}
	return dims
}

// metrics returns the ascent and descent of the editor font in pixels.
func (s *sqlEditor) metrics(gtx C, th *material.Theme) (int, int) {
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints.Min = image.Point{}
	d := widget.Label{MaxLines: 1}.Layout(g, th.Shaper, monoFont, sqlTextSize, "Ag", colorMaterial(gtx, cs.fg))
	m.Stop()
	return d.Size.Y - d.Baseline, d.Baseline
}

// drawRun draws txt with its baseline at baseline (x, y).
func (s *sqlEditor) drawRun(gtx C, th *material.Theme, txt string, baseline image.Point, col color.NRGBA, ascent int) {
	defer op.Offset(image.Pt(baseline.X, baseline.Y-ascent)).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints.Min = image.Point{}
	g.Constraints.Max = image.Pt(1<<20, 1<<20)
	widget.Label{MaxLines: 1}.Layout(g, th.Shaper, monoFont, sqlTextSize, txt, colorMaterial(gtx, col))
}
