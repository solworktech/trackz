package gio

import (
	"fmt"
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/solworktech/trackz/internal/report"
)

// datePicker is a month calendar. Picking a day sets Value and reports it.
type datePicker struct {
	Value  report.Date
	shown  report.Date // first day of the displayed month
	prev   widget.Clickable
	next   widget.Clickable
	today  widget.Clickable
	days   [42]widget.Clickable
	inited bool
	// rangeFrom..rangeTo, if set, are shaded to show a selected range.
	rangeFrom, rangeTo report.Date
}

func (dp *datePicker) inRange(d report.Date) bool {
	if dp.rangeFrom == (report.Date{}) || dp.rangeTo.Before(dp.rangeFrom) {
		return false
	}
	return !d.Before(dp.rangeFrom) && !dp.rangeTo.Before(d)
}

func (dp *datePicker) set(d report.Date) {
	dp.Value = d
	dp.shown = report.Date{Year: d.Year, Month: d.Month, Day: 1}
	dp.inited = true
}

// layout draws the calendar; it returns true when a day was picked.
func (dp *datePicker) layout(gtx C, th *material.Theme, todayDate report.Date) (D, bool) {
	if !dp.inited {
		dp.set(todayDate)
	}
	picked := false
	if dp.prev.Clicked(gtx) {
		t := dp.shown.In(time.UTC).AddDate(0, -1, 0)
		dp.shown = report.DateOf(t)
	}
	if dp.next.Clicked(gtx) {
		t := dp.shown.In(time.UTC).AddDate(0, 1, 0)
		dp.shown = report.DateOf(t)
	}
	if dp.today.Clicked(gtx) {
		dp.set(todayDate)
		picked = true
	}
	first := dp.shown.In(time.UTC)
	// Monday-first grid.
	offset := (int(first.Weekday()) + 6) % 7
	gridStart := report.DateOf(first.AddDate(0, 0, -offset))
	for i := range dp.days {
		if dp.days[i].Clicked(gtx) {
			dp.Value = gridStart.AddDays(i)
			if dp.Value.Month != dp.shown.Month {
				dp.shown = report.Date{Year: dp.Value.Year, Month: dp.Value.Month, Day: 1}
			}
			picked = true
		}
	}
	cell := gtx.Dp(30)
	// Keep the calendar compact rather than stretching to the window width.
	gtx.Constraints.Min.X = 0
	gtx.Constraints.Max.X = cell*7 + gtx.Dp(12)
	d := bordered(gtx, func(gtx C) D {
		return layout.UniformInset(6).Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return smallButton(gtx, th, &dp.prev, "‹") }),
						layout.Rigid(func(gtx C) D {
							gtx.Constraints.Min.X = cell*7 - gtx.Dp(110)
							l := material.Body1(th, fmt.Sprintf("%s %d", dp.shown.Month, dp.shown.Year))
							l.Alignment = text.Middle
							return l.Layout(gtx)
						}),
						layout.Rigid(func(gtx C) D { return smallButton(gtx, th, &dp.next, "›") }),
					)
				}),
				layout.Rigid(func(gtx C) D {
					var children []layout.FlexChild
					for _, n := range []string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"} {
						n := n
						children = append(children, layout.Rigid(func(gtx C) D {
							gtx.Constraints = layout.Exact(image.Pt(cell, cell*3/4))
							return layout.Center.Layout(gtx, func(gtx C) D {
								l := material.Caption(th, n)
								l.Color = cs.muted
								return l.Layout(gtx)
							})
						}))
					}
					return layout.Flex{}.Layout(gtx, children...)
				}),
				layout.Rigid(func(gtx C) D {
					var rows []layout.FlexChild
					for r := 0; r < 6; r++ {
						r := r
						rows = append(rows, layout.Rigid(func(gtx C) D {
							var cols []layout.FlexChild
							for c := 0; c < 7; c++ {
								i := r*7 + c
								day := gridStart.AddDays(i)
								cols = append(cols, layout.Rigid(func(gtx C) D {
									gtx.Constraints = layout.Exact(image.Pt(cell, cell))
									return material.Clickable(gtx, &dp.days[i], func(gtx C) D {
										sz := image.Pt(cell, cell)
										switch {
										case day == dp.Value:
											fillRect(gtx, image.Rectangle{Max: sz}, cs.active)
										case dp.inRange(day):
											fillRect(gtx, image.Rectangle{Max: sz}, cs.editorSel)
										case day == todayDate:
											strokeRect(gtx, image.Rectangle{Max: sz}.Inset(2), cs.active, 1)
										}
										return layout.Center.Layout(gtx, func(gtx C) D {
											l := material.Body2(th, fmt.Sprint(day.Day))
											switch {
											case day == dp.Value:
												l.Color = cs.onActive
											case day.Month != dp.shown.Month:
												l.Color = cs.border
											}
											return l.Layout(gtx)
										})
									})
								}))
							}
							return layout.Flex{}.Layout(gtx, cols...)
						}))
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D {
						return layout.Flex{}.Layout(gtx, layout.Rigid(func(gtx C) D {
							return smallButton(gtx, th, &dp.today, "Today")
						}))
					})
				}),
			)
		})
	})
	return d, picked
}
