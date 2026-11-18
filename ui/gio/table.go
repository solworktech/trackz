package gio

import (
	"image"

	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/solworktech/trackz/internal/store"
)

// tableView renders a store.Table with a header and scrolling rows.
type tableView struct {
	list widget.List
}

// columnWeights sizes columns by the length of their content, so long
// window titles get more room than numbers.
func columnWeights(t *store.Table) []float32 {
	w := make([]float32, len(t.Columns))
	for c, name := range t.Columns {
		w[c] = float32(len(name))
	}
	for i, row := range t.Rows {
		if i > 200 {
			break
		}
		for c, v := range row {
			if l := float32(len([]rune(v.String()))); l > w[c] {
				w[c] = l
			}
		}
	}
	for c := range w {
		if w[c] < 6 {
			w[c] = 6
		}
		if w[c] > 60 {
			w[c] = 60
		}
	}
	return w
}

func (tv *tableView) layout(gtx C, th *material.Theme, t *store.Table) D {
	if t == nil || len(t.Columns) == 0 {
		return emptyChart(gtx, th)
	}
	weights := columnWeights(t)
	numeric := make([]bool, len(t.Columns))
	for c := range t.Columns {
		numeric[c] = len(t.Rows) > 0
		for _, row := range t.Rows {
			if row[c].Kind != store.Null && !row[c].IsNumeric() {
				numeric[c] = false
				break
			}
		}
	}
	row := func(gtx C, cells []string, header bool, stripe bool) D {
		h := gtx.Dp(26)
		if header {
			fillRect(gtx, image.Rect(0, 0, gtx.Constraints.Max.X, h), cs.header)
		} else if stripe {
			fillRect(gtx, image.Rect(0, 0, gtx.Constraints.Max.X, h), cs.stripe)
		}
		children := make([]layout.FlexChild, len(cells))
		for c := range cells {
			c := c
			children[c] = layout.Flexed(weights[c], func(gtx C) D {
				gtx.Constraints.Min.Y = h
				return layout.Inset{Left: 6, Right: 6, Top: 4}.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					l := material.Label(th, 13, cells[c])
					l.MaxLines = 1
					l.Truncator = "…"
					if header {
						l.Font.Weight = 600
					}
					if numeric[c] {
						l.Alignment = text.End
					}
					return l.Layout(gtx)
				})
			})
		}
		return layout.Flex{}.Layout(gtx, children...)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return row(gtx, t.Columns, true, false) }),
		layout.Flexed(1, func(gtx C) D {
			tv.list.Axis = layout.Vertical
			return material.List(th, &tv.list).Layout(gtx, len(t.Rows), func(gtx C, i int) D {
				cells := make([]string, len(t.Columns))
				for c, v := range t.Rows[i] {
					cells[c] = v.String()
				}
				return row(gtx, cells, false, i%2 == 1)
			})
		}),
	)
}
