package gio

import (
	"image"
	"slices"
	"strings"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/solworktech/trackz/internal/store"
)

// tableColumn is a column as shown: which column of the table it shows,
// under what title, and how it sorts.
type tableColumn struct {
	title  string
	src    int     // index in store.Table.Columns
	sortBy int     // column compared when sorting (e.g. seconds for a duration)
	weight float32 // share of the width
	right  bool    // right-aligned (numbers, durations)
	// icon, if set, returns an icon for the table's row (unsorted index).
	icon func(row int) *paint.ImageOp
}

// tableView renders a store.Table with a header and scrolling rows. A click
// on a column title sorts by that column; another click reverses the order.
type tableView struct {
	list    widget.List
	headers []widget.Clickable

	sortCol int // index in the shown columns; -1: the table's own order
	desc    bool

	// order of the rows, for the table and sort it was computed for
	order    []int
	orderFor *store.Table
	orderCol int
	orderDsc bool
}

// defaultColumns shows every column of t under its own name, sized by the
// length of its content so long window titles get more room than numbers.
func defaultColumns(t *store.Table) []tableColumn {
	cols := make([]tableColumn, len(t.Columns))
	for c, name := range t.Columns {
		w := float32(len(name))
		numeric := len(t.Rows) > 0
		for i, row := range t.Rows {
			if i <= 200 {
				if l := float32(len([]rune(row[c].String()))); l > w {
					w = l
				}
			}
			if row[c].Kind != store.Null && !row[c].IsNumeric() {
				numeric = false
			}
		}
		cols[c] = tableColumn{title: name, src: c, sortBy: c, weight: min(max(w, 6), 60), right: numeric}
	}
	return cols
}

// compareValues orders two cells: numbers numerically, durations
// ("1:02:03") by their length in time, anything else as text ignoring
// case. Empty (NULL) cells come last.
func compareValues(a, b store.Value) int {
	switch an, bn := a.Kind == store.Null, b.Kind == store.Null; {
	case an && bn:
		return 0
	case an:
		return 1
	case bn:
		return -1
	}
	if x, ok := a.Number(); ok {
		if y, ok := b.Number(); ok {
			return cmpFloat(x, y)
		}
	}
	if x, ok := a.DurationSeconds(); ok {
		if y, ok := b.DurationSeconds(); ok {
			return cmpFloat(float64(x), float64(y))
		}
	}
	return strings.Compare(strings.ToLower(a.String()), strings.ToLower(b.String()))
}

func cmpFloat(x, y float64) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

// rowOrder returns the order to show t's rows in, sorted by the column
// chosen (stable, so equal cells keep the table's order).
func (tv *tableView) rowOrder(t *store.Table, cols []tableColumn) []int {
	if tv.orderFor == t && tv.orderCol == tv.sortCol && tv.orderDsc == tv.desc && len(tv.order) == len(t.Rows) {
		return tv.order
	}
	order := make([]int, len(t.Rows))
	for i := range order {
		order[i] = i
	}
	if tv.sortCol >= 0 && tv.sortCol < len(cols) {
		by := cols[tv.sortCol].sortBy
		slices.SortStableFunc(order, func(i, j int) int {
			c := compareValues(t.Rows[i][by], t.Rows[j][by])
			if tv.desc {
				c = -c
			}
			return c
		})
	}
	tv.order, tv.orderFor, tv.orderCol, tv.orderDsc = order, t, tv.sortCol, tv.desc
	return order
}

// layout draws t with the given columns (nil: every column as is).
func (tv *tableView) layout(gtx C, th *material.Theme, t *store.Table, cols []tableColumn) D {
	if t == nil || len(t.Columns) == 0 {
		return emptyChart(gtx, th)
	}
	if cols == nil {
		cols = defaultColumns(t)
	}
	if len(tv.headers) != len(cols) {
		tv.headers = make([]widget.Clickable, len(cols))
		tv.sortCol = -1
	}
	for c := range tv.headers {
		if tv.headers[c].Clicked(gtx) {
			if tv.sortCol == c {
				tv.desc = !tv.desc
			} else {
				// Numbers and durations: largest first; text: A to Z.
				tv.sortCol, tv.desc = c, cols[c].right
			}
		}
	}
	order := tv.rowOrder(t, cols)

	rowH := gtx.Dp(26)
	cell := func(gtx C, c int, label string, header bool, icon *paint.ImageOp) D {
		gtx.Constraints.Min.Y = rowH
		return layout.Inset{Left: 6, Right: 6, Top: 4}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			l := material.Label(th, 13, label)
			l.MaxLines = 1
			l.Truncator = "…"
			if header {
				l.Font.Weight = 600
			}
			if cols[c].right {
				l.Alignment = text.End
			}
			if icon == nil {
				return l.Layout(gtx)
			}
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Right: 6}.Layout(gtx, func(gtx C) D { return layoutIcon(gtx, icon, 16) })
				}),
				layout.Flexed(1, l.Layout),
			)
		})
	}
	header := func(gtx C) D {
		fillRect(gtx, image.Rect(0, 0, gtx.Constraints.Max.X, rowH), cs.header)
		children := make([]layout.FlexChild, len(cols))
		for c := range cols {
			children[c] = layout.Flexed(cols[c].weight, func(gtx C) D {
				title := cols[c].title
				if tv.sortCol == c {
					if tv.desc {
						title += " ▼"
					} else {
						title += " ▲"
					}
				}
				return tv.headers[c].Layout(gtx, func(gtx C) D {
					pointer.CursorPointer.Add(gtx.Ops)
					return cell(gtx, c, title, true, nil)
				})
			})
		}
		return layout.Flex{}.Layout(gtx, children...)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(header),
		layout.Flexed(1, func(gtx C) D {
			tv.list.Axis = layout.Vertical
			return material.List(th, &tv.list).Layout(gtx, len(order), func(gtx C, i int) D {
				r := order[i]
				if i%2 == 1 {
					fillRect(gtx, image.Rect(0, 0, gtx.Constraints.Max.X, rowH), cs.stripe)
				}
				children := make([]layout.FlexChild, len(cols))
				for c := range cols {
					children[c] = layout.Flexed(cols[c].weight, func(gtx C) D {
						var icon *paint.ImageOp
						if cols[c].icon != nil {
							icon = cols[c].icon(r)
						}
						return cell(gtx, c, t.Rows[r][cols[c].src].String(), false, icon)
					})
				}
				return layout.Flex{}.Layout(gtx, children...)
			})
		}),
	)
}
