package gio

import (
	"time"
	"unicode"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// doubleClickInterval is how close clicks must be to count as a double or
// triple click. Gio's own gestures use a fixed 200 ms, shorter than the
// desktop defaults (400-500 ms), so ordinary double-clicks often went
// unnoticed.
const doubleClickInterval = 500 * time.Millisecond

// multiClick adds double-click (word) and triple-click (line) selection to
// a widget.Editor, with a desktop-like interval between clicks; every text
// field uses it (editorField, sqlEditor).
//
// The first click goes to the editor as usual (it focuses it and places the
// caret); watchTag only observes it through a pass-through handler. For
// doubleClickInterval after that click is released, an opaque handler
// (multiTag) covers the editor, so a second or third press reaches only
// this code and not the editor's own click and drag gestures. That press
// selects the word (or line) under the pointer and grabs the pointer; while
// the button is held, dragging extends the selection by whole words (or
// lines) from that anchor, and release ends it. Letting the editor see the
// second press is what broke selection before: it treated a press more than
// 200 ms after the first as a new drag, and the slightest motion of the mouse
// moved the selection end back to the pointer, leaving part of the word.
type multiClick struct {
	watchTag, multiTag int
	ed                 *widget.Editor
	text               string // ed's text, as runes
	runes              []rune
	regions            []widget.Region

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

// layout handles the clicks on ed, laid out with dims, and registers the
// handlers for the next ones. Call it right after the editor's Layout.
func (c *multiClick) layout(gtx C, ed *widget.Editor, dims D) {
	c.ed = ed
	if t := ed.Text(); t != c.text || c.runes == nil {
		c.text, c.runes = t, []rune(t)
	}
	c.handle(gtx)
	defer clip.Rect{Max: dims.Size}.Push(gtx.Ops).Pop()
	// Watch clicks without taking them from the editor underneath.
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, &c.watchTag)
	pass.Pop()
	// Shortly after a click, and while a multi-click selection is being
	// dragged, take presses from the editor.
	c.overlay = c.selecting || gtx.Now.Before(c.armedUntil)
	if c.overlay {
		event.Op(gtx.Ops, &c.multiTag)
	}
}

func (c *multiClick) handle(gtx C) {
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
				c.arm(gtx)
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
			gtx.Execute(key.FocusCmd{Tag: c.ed})
			d := e.Position.Sub(c.lastPos)
			if e.Time-c.lastPress <= doubleClickInterval && d.X*d.X+d.Y*d.Y <= nearSq(gtx) {
				c.clicks++
			} else {
				c.clicks = 1
			}
			c.lastPress, c.lastPos = e.Time, e.Position
			c.unit = min(c.clicks, 3)
			off := c.offsetAt(e.Position)
			if c.unit == 1 && e.Modifiers.Contain(key.ModShift) {
				// Extend the existing selection, as the editor would.
				from, to := c.ed.Selection()
				if abs(off-from) < abs(off-to) {
					from = to
				}
				c.anchorFrom, c.anchorTo = from, from
			} else {
				c.anchorFrom, c.anchorTo = c.unitAt(c.unit, off)
			}
			c.selecting = true
			c.extendTo(off)
		case pointer.Drag:
			if c.selecting {
				c.extendTo(c.offsetAt(e.Position))
			}
		case pointer.Release, pointer.Cancel:
			if c.selecting {
				c.selecting = false
				c.arm(gtx) // a further press may still be a triple click
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
func (c *multiClick) arm(gtx C) {
	c.armedUntil = gtx.Now.Add(doubleClickInterval)
	gtx.Execute(op.InvalidateCmd{At: c.armedUntil})
}

// unitAt returns the range of the character position, word or line at off.
func (c *multiClick) unitAt(unit, off int) (int, int) {
	switch unit {
	case 2:
		return wordAt(c.runes, off)
	case 3:
		return lineAt(c.runes, off)
	}
	return off, off
}

// extendTo selects from the anchor unit to the unit at off, with the caret
// at the end nearest off.
func (c *multiClick) extendTo(off int) {
	from, to := c.unitAt(c.unit, off)
	if from < c.anchorFrom {
		c.ed.SetCaret(from, c.anchorTo)
	} else {
		c.ed.SetCaret(max(to, c.anchorTo), c.anchorFrom)
	}
}

// offsetAt returns the rune offset nearest to pos, in the editor's
// coordinates, from the positions the editor reports for each rune.
func (c *multiClick) offsetAt(pos f32.Point) int {
	p := pos.Round()
	off := 0
	for i, r := range c.runes {
		c.regions = c.ed.Regions(i, i+1, c.regions[:0])
		if len(c.regions) == 0 {
			continue // scrolled out of view
		}
		b := c.regions[0].Bounds
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

// multiClicks holds the multi-click state of each editor laid out with
// editorField. Layout happens on one goroutine.
var multiClicks = map[*widget.Editor]*multiClick{}

// editorField lays out e with multi-click selection (see multiClick).
func editorField(gtx C, e material.EditorStyle) D {
	d := e.Layout(gtx)
	c := multiClicks[e.Editor]
	if c == nil {
		c = &multiClick{}
		multiClicks[e.Editor] = c
	}
	c.layout(gtx, e.Editor, d)
	return d
}
