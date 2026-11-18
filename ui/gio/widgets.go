// Package gio is the Gio (gioui.org) frontend for Trackz. It contains no
// business logic: everything it shows comes from report.Service, and user
// input is passed back to it.
package gio

import (
	"image"
	"image/color"
	"sync"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/solworktech/trackz/internal/assets"
	"github.com/solworktech/trackz/internal/icons"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

// palette is a set of distinguishable colours for chart series.
var palette = []color.NRGBA{
	{0x42, 0x85, 0xf4, 0xff}, // blue
	{0xea, 0x43, 0x35, 0xff}, // red
	{0xfb, 0xbc, 0x05, 0xff}, // yellow
	{0x34, 0xa8, 0x53, 0xff}, // green
	{0xab, 0x47, 0xbc, 0xff}, // purple
	{0x00, 0xac, 0xc1, 0xff}, // cyan
	{0xff, 0x70, 0x43, 0xff}, // orange
	{0x9e, 0x9d, 0x24, 0xff}, // olive
	{0x5c, 0x6b, 0xc0, 0xff}, // indigo
	{0xf0, 0x62, 0x92, 0xff}, // pink
	{0x26, 0xa6, 0x9a, 0xff}, // teal
	{0x8d, 0x6e, 0x63, 0xff}, // brown
}

func seriesColor(i int) color.NRGBA { return palette[i%len(palette)] }

// scheme holds every colour the UI uses apart from the chart palette.
type scheme struct {
	dark                   bool
	bg, fg                 color.NRGBA // window background, text
	border, grid, muted    color.NRGBA
	header, stripe         color.NRGBA // table header, alternate rows
	tooltip, tooltipFg     color.NRGBA
	err, ok                color.NRGBA
	active, onActive, idle color.NRGBA // buttons
	scrim, panel           color.NRGBA // modal backdrop and dialog
	editorSel              color.NRGBA
}

var lightScheme = scheme{
	bg:        color.NRGBA{0xff, 0xff, 0xff, 0xff},
	fg:        color.NRGBA{0x20, 0x20, 0x20, 0xff},
	border:    color.NRGBA{0xd0, 0xd0, 0xd0, 0xff},
	grid:      color.NRGBA{0xe8, 0xe8, 0xe8, 0xff},
	muted:     color.NRGBA{0x70, 0x70, 0x70, 0xff},
	header:    color.NRGBA{0xf2, 0xf4, 0xf7, 0xff},
	stripe:    color.NRGBA{0xfa, 0xfa, 0xfa, 0xff},
	tooltip:   color.NRGBA{0x30, 0x30, 0x30, 0xf0},
	tooltipFg: color.NRGBA{0xff, 0xff, 0xff, 0xff},
	err:       color.NRGBA{0xc6, 0x28, 0x28, 0xff},
	ok:        color.NRGBA{0x2e, 0x7d, 0x32, 0xff},
	active:    color.NRGBA{0x3f, 0x51, 0xb5, 0xff},
	onActive:  color.NRGBA{0xff, 0xff, 0xff, 0xff},
	idle:      color.NRGBA{0xe0, 0xe3, 0xea, 0xff},
	scrim:     color.NRGBA{0x00, 0x00, 0x00, 0x80},
	panel:     color.NRGBA{0xff, 0xff, 0xff, 0xff},
	editorSel: color.NRGBA{0x3f, 0x51, 0xb5, 0x70},
}

var darkScheme = scheme{
	dark:      true,
	bg:        color.NRGBA{0x1e, 0x1f, 0x22, 0xff},
	fg:        color.NRGBA{0xe6, 0xe6, 0xe6, 0xff},
	border:    color.NRGBA{0x48, 0x4a, 0x50, 0xff},
	grid:      color.NRGBA{0x33, 0x35, 0x3a, 0xff},
	muted:     color.NRGBA{0xa0, 0xa3, 0xa8, 0xff},
	header:    color.NRGBA{0x2b, 0x2d, 0x32, 0xff},
	stripe:    color.NRGBA{0x25, 0x26, 0x2a, 0xff},
	tooltip:   color.NRGBA{0xec, 0xec, 0xec, 0xf2},
	tooltipFg: color.NRGBA{0x1e, 0x1f, 0x22, 0xff},
	err:       color.NRGBA{0xef, 0x6c, 0x6c, 0xff},
	ok:        color.NRGBA{0x81, 0xc7, 0x84, 0xff},
	active:    color.NRGBA{0x5c, 0x6b, 0xc0, 0xff},
	onActive:  color.NRGBA{0xff, 0xff, 0xff, 0xff},
	idle:      color.NRGBA{0x33, 0x35, 0x3b, 0xff},
	scrim:     color.NRGBA{0x00, 0x00, 0x00, 0xa0},
	panel:     color.NRGBA{0x26, 0x27, 0x2b, 0xff},
	editorSel: color.NRGBA{0x7c, 0x8b, 0xe0, 0x80},
}

// cs is the active scheme. It is only read and written on the UI
// goroutine.
var cs = &lightScheme

// applyScheme makes sc current and updates the material theme to match.
func applyScheme(th *material.Theme, sc *scheme) {
	cs = sc
	th.Palette = material.Palette{
		Bg:         sc.bg,
		Fg:         sc.fg,
		ContrastBg: sc.active,
		ContrastFg: sc.onActive,
	}
}

// iconCache turns resolved icons into reusable image ops.
type iconCache struct {
	res *icons.Resolver
	mu  sync.Mutex
	ops map[string]*paint.ImageOp
}

func newIconCache() *iconCache {
	return &iconCache{res: icons.New(32), ops: map[string]*paint.ImageOp{}}
}

// prefetch resolves icons ahead of drawing (call from a goroutine).
func (c *iconCache) prefetch(process, app string) {
	c.get(process, app)
}

func (c *iconCache) get(process, app string) *paint.ImageOp {
	key := process + "\x00" + app
	c.mu.Lock()
	op, ok := c.ops[key]
	c.mu.Unlock()
	if ok {
		return op
	}
	img := c.res.Icon(process, app)
	if img != nil {
		o := paint.NewImageOp(img)
		op = &o
	}
	c.mu.Lock()
	c.ops[key] = op
	c.mu.Unlock()
	return op
}

// layoutIcon draws an icon of size dp (or reserves the space).
func layoutIcon(gtx C, op *paint.ImageOp, size unit.Dp) D {
	px := gtx.Dp(size)
	if op == nil {
		return D{Size: image.Pt(px, px)}
	}
	gtx.Constraints = layout.Exact(image.Pt(px, px))
	return widget.Image{Src: *op, Fit: widget.Contain, Position: layout.Center, Scale: 1}.Layout(gtx)
}

// labelAt draws text with its top-left corner at pt and returns its size.
func labelAt(gtx C, th *material.Theme, pt image.Point, size unit.Sp, col color.NRGBA, txt string) D {
	defer op.Offset(pt).Push(gtx.Ops).Pop()
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max = image.Pt(1<<20, 1<<20)
	l := material.Label(th, size, txt)
	l.Color = col
	l.MaxLines = 1
	return l.Layout(gtx)
}

// measure returns the size text would take without drawing it.
func measure(gtx C, th *material.Theme, size unit.Sp, txt string) image.Point {
	m := op.Record(gtx.Ops)
	d := labelAt(gtx, th, image.Point{}, size, cs.fg, txt)
	m.Stop()
	return d.Size
}

// truncLabel draws a single-line label that is cut with an ellipsis to fit.
func truncLabel(gtx C, th *material.Theme, size unit.Sp, col color.NRGBA, txt string) D {
	l := material.Label(th, size, txt)
	l.Color = col
	l.MaxLines = 1
	l.Truncator = "…"
	return l.Layout(gtx)
}

func fillRect(gtx C, r image.Rectangle, col color.NRGBA) {
	paint.FillShape(gtx.Ops, col, clip.Rect(r).Op())
}

func strokeRect(gtx C, r image.Rectangle, col color.NRGBA, width float32) {
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(float32(r.Min.X), float32(r.Min.Y)))
	p.LineTo(f32.Pt(float32(r.Max.X), float32(r.Min.Y)))
	p.LineTo(f32.Pt(float32(r.Max.X), float32(r.Max.Y)))
	p.LineTo(f32.Pt(float32(r.Min.X), float32(r.Max.Y)))
	p.Close()
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: width}.Op())
}

func line(gtx C, a, b f32.Point, col color.NRGBA, width float32) {
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(a)
	p.LineTo(b)
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: width}.Op())
}

// tooltip draws lines of text in a dark box near pt, kept inside bounds,
// with icon (if not nil) on the left.
func tooltip(gtx C, th *material.Theme, pt image.Point, bounds image.Point, icon *paint.ImageOp, lines ...string) {
	pad := gtx.Dp(6)
	w, h := 0, 0
	sizes := make([]image.Point, len(lines))
	for i, l := range lines {
		sizes[i] = measure(gtx, th, 13, l)
		if sizes[i].X > w {
			w = sizes[i].X
		}
		h += sizes[i].Y
	}
	textX := pad
	if icon != nil {
		iconPx := gtx.Dp(tooltipIconSize)
		textX += iconPx + pad
		h = max(h, iconPx)
	}
	box := image.Rect(0, 0, textX+w+pad, h+2*pad)
	pos := pt.Add(image.Pt(gtx.Dp(12), gtx.Dp(12)))
	if pos.X+box.Dx() > bounds.X {
		pos.X = pt.X - box.Dx() - gtx.Dp(8)
	}
	if pos.Y+box.Dy() > bounds.Y {
		pos.Y = bounds.Y - box.Dy()
	}
	if pos.X < 0 {
		pos.X = 0
	}
	if pos.Y < 0 {
		pos.Y = 0
	}
	defer op.Offset(pos).Push(gtx.Ops).Pop()
	rr := clip.UniformRRect(box, gtx.Dp(4))
	paint.FillShape(gtx.Ops, cs.tooltip, rr.Op(gtx.Ops))
	if icon != nil {
		st := op.Offset(image.Pt(pad, pad)).Push(gtx.Ops)
		layoutIcon(gtx, icon, tooltipIconSize)
		st.Pop()
	}
	y := pad
	for i, l := range lines {
		labelAt(gtx, th, image.Pt(textX, y), 13, cs.tooltipFg, l)
		y += sizes[i].Y
	}
}

const tooltipIconSize = 28 // dp

// segButton is a toggle-style button used for tabs and presets.
func segButton(gtx C, th *material.Theme, btn *widget.Clickable, txt string, active bool) D {
	b := material.Button(th, btn, txt)
	b.TextSize = 14
	b.Inset = layout.Inset{Top: 7, Bottom: 7, Left: 12, Right: 12}
	if active {
		b.Background = cs.active
		b.Color = cs.onActive
	} else {
		b.Background = cs.idle
		b.Color = cs.fg
	}
	return b.Layout(gtx)
}

func smallButton(gtx C, th *material.Theme, btn *widget.Clickable, txt string) D {
	b := material.Button(th, btn, txt)
	b.TextSize = 14
	b.Inset = layout.Inset{Top: 7, Bottom: 7, Left: 14, Right: 14}
	return b.Layout(gtx)
}

// field draws a bordered single-line editor with a caption above it.
func field(gtx C, th *material.Theme, caption string, ed *widget.Editor, hint string, width unit.Dp) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			l := material.Caption(th, caption)
			l.Color = cs.muted
			return l.Layout(gtx)
		}),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Dp(width)
			gtx.Constraints.Max.X = gtx.Dp(width)
			return bordered(gtx, func(gtx C) D {
				return layout.UniformInset(6).Layout(gtx, func(gtx C) D {
					e := material.Editor(th, ed, hint)
					e.TextSize = 14
					return e.Layout(gtx)
				})
			})
		}),
	)
}

func bordered(gtx C, w layout.Widget) D {
	m := op.Record(gtx.Ops)
	d := w(gtx)
	call := m.Stop()
	call.Add(gtx.Ops)
	strokeRect(gtx, image.Rectangle{Max: d.Size}, cs.border, 1)
	return d
}

func logoOp() *paint.ImageOp {
	img := assets.Icon(64)
	if img == nil {
		return nil
	}
	op := paint.NewImageOp(img)
	return &op
}
