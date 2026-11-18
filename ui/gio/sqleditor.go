package gio

import (
	"image"
	"image/color"
	"strings"
	"time"
	"unicode"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/font/opentype"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
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

	click clickSelect // double- and triple-click selection

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
	// After the editor has handled this frame's events.
	s.handleClicks(gtx)

	defer clip.Rect{Max: dims.Size}.Push(gtx.Ops).Pop()
	// Watch clicks without taking them from the editor underneath.
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, &s.click.watchTag)
	pass.Pop()
	// Shortly after a click, and while a multi-click selection is being
	// dragged, take presses from the editor (see clickSelect).
	s.click.overlay = s.click.selecting || gtx.Now.Before(s.click.armedUntil)
	if s.click.overlay {
		event.Op(gtx.Ops, &s.click.multiTag)
	}
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

// doubleClickInterval is how close clicks must be to count as a double or
// triple click. Gio's own gestures use a fixed 200 ms, shorter than the
// desktop defaults (400-500 ms), so ordinary double-clicks often went
// unnoticed.
const doubleClickInterval = 500 * time.Millisecond

// Multi-click selection.
//
// The first click goes to the editor as usual (it focuses it and places the
// caret); s.watchTag only observes it through a pass-through handler. For
// doubleClickInterval after that click is released, an opaque handler
// (s.multiTag) covers the editor, so a second or third press reaches only
// this code and not the editor's own click and drag gestures. That press
// selects the word (or line) under the pointer and grabs the pointer; while
// the button is held, dragging extends the selection by whole words (or
// lines) from that anchor, and release ends it. Letting the editor see the
// second press is what broke selection before: it treated a press more than
// 200 ms after the first as a new drag, and the slightest motion of the mouse
// moved the selection end back to the pointer, leaving part of the word.
type clickSelect struct {
	watchTag, multiTag int

	clicks     int
	lastPress  time.Duration // pointer time of the last press
	lastPos    f32.Point
	armedUntil time.Time // the overlay catches presses until then
	pressed    bool      // a press seen by watchTag is not released yet

	overlay    bool // multiTag was registered in the last frame
	selecting  bool // the button is held after an overlay press
	unit       int  // 1 character, 2 word, 3 line
	anchorFrom int  // the unit selected by the press
	anchorTo   int
}

func (s *sqlEditor) handleClicks(gtx C) {
	c := &s.click
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &c.watchTag, Kinds: pointer.Press | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		if c.overlay && e.Kind != pointer.Cancel {
			continue // multiTag gets this one too, and handles it
		}
		switch e.Kind {
		case pointer.Press:
			c.pressed = e.Buttons == pointer.ButtonPrimary && e.Source == pointer.Mouse
			c.clicks, c.lastPress, c.lastPos = 1, e.Time, e.Position
		case pointer.Release:
			if d := e.Position.Sub(c.lastPos); c.pressed && d.X*d.X+d.Y*d.Y <= nearSq(gtx) {
				s.arm(gtx)
			}
			c.pressed = false
		case pointer.Cancel: // the editor's drag gesture grabbed the pointer
			c.pressed, c.clicks = false, 0
		}
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &c.multiTag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Press:
			if e.Buttons != pointer.ButtonPrimary {
				continue
			}
			gtx.Execute(pointer.GrabCmd{Tag: &c.multiTag, ID: e.PointerID})
			gtx.Execute(key.FocusCmd{Tag: &s.ed})
			d := e.Position.Sub(c.lastPos)
			if e.Time-c.lastPress <= doubleClickInterval && d.X*d.X+d.Y*d.Y <= nearSq(gtx) {
				c.clicks++
			} else {
				c.clicks = 1
			}
			c.lastPress, c.lastPos = e.Time, e.Position
			c.unit = min(c.clicks, 3)
			off := s.offsetAt(e.Position)
			if c.unit == 1 && e.Modifiers.Contain(key.ModShift) {
				// Extend the existing selection, as the editor would.
				from, to := s.ed.Selection()
				if abs(off-from) < abs(off-to) {
					from = to
				}
				c.anchorFrom, c.anchorTo = from, from
			} else {
				c.anchorFrom, c.anchorTo = s.unitAt(c.unit, off)
			}
			c.selecting = true
			s.extendTo(off)
		case pointer.Drag:
			if c.selecting {
				s.extendTo(s.offsetAt(e.Position))
			}
		case pointer.Release, pointer.Cancel:
			if c.selecting {
				c.selecting = false
				s.arm(gtx) // a further press may still be a triple click
			}
		}
	}
}

func nearSq(gtx C) float32 { d := float32(gtx.Dp(6)); return d * d }

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// arm makes the opaque handler catch presses for the next interval, and
// schedules a frame to remove it afterwards.
func (s *sqlEditor) arm(gtx C) {
	s.click.armedUntil = gtx.Now.Add(doubleClickInterval)
	gtx.Execute(op.InvalidateCmd{At: s.click.armedUntil})
}

// unitAt returns the range of the character position, word or line at off.
func (s *sqlEditor) unitAt(unit, off int) (int, int) {
	switch unit {
	case 2:
		return wordAt(s.runes, off)
	case 3:
		return lineAt(s.runes, off)
	}
	return off, off
}

// extendTo selects from the anchor unit to the unit at off, with the caret
// at the end nearest off.
func (s *sqlEditor) extendTo(off int) {
	c := &s.click
	from, to := s.unitAt(c.unit, off)
	if from < c.anchorFrom {
		s.ed.SetCaret(from, c.anchorTo)
	} else {
		s.ed.SetCaret(max(to, c.anchorTo), c.anchorFrom)
	}
}

// offsetAt returns the rune offset nearest to pos, in the editor's
// coordinates, from the positions the editor reports for each rune.
func (s *sqlEditor) offsetAt(pos f32.Point) int {
	p := pos.Round()
	off := 0
	for i, r := range s.runes {
		s.regions = s.ed.Regions(i, i+1, s.regions[:0])
		if len(s.regions) == 0 {
			continue // scrolled out of view
		}
		b := s.regions[0].Bounds
		switch {
		case b.Max.Y <= p.Y: // on an earlier line
			off = i + 1
		case b.Min.Y > p.Y: // on a later line
			return off
		case r != '\n' && (b.Min.X+b.Max.X)/2 < p.X: // before the pointer
			off = i + 1
		default:
			return i
		}
	}
	return off
}

func isWordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// wordAt returns the range of the word at offset: a run of letters, digits
// and underscores, or else of other non-space characters (operators).
// Clicking just after a word selects that word.
func wordAt(text []rune, offset int) (int, int) {
	if offset > len(text) {
		offset = len(text)
	}
	at := offset
	if at == len(text) || (unicode.IsSpace(text[at]) && at > 0) {
		at-- // clicked just past the end of a word
	}
	if at < 0 || unicode.IsSpace(text[at]) {
		return offset, offset
	}
	same := func(r rune) bool { return isWordRune(r) == isWordRune(text[at]) && !unicode.IsSpace(r) }
	start, end := at, at+1
	for start > 0 && same(text[start-1]) {
		start--
	}
	for end < len(text) && same(text[end]) {
		end++
	}
	return start, end
}

// lineAt returns the range of the line containing offset, without its
// newline.
func lineAt(text []rune, offset int) (int, int) {
	if offset > len(text) {
		offset = len(text)
	}
	start, end := offset, offset
	for start > 0 && text[start-1] != '\n' {
		start--
	}
	for end < len(text) && text[end] != '\n' {
		end++
	}
	return start, end
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
