package gio

import (
	"gioui.org/layout"
	"gioui.org/widget/material"

	"github.com/solworktech/trackz/internal/report"
)

// layoutTaskGroups draws the Task Groups view: the time per group and
// Ungrouped, after a note when no groups are defined or they are invalid.
func (u *UI) layoutTaskGroups(gtx C, res *report.Result) D {
	var note string
	col := cs.muted
	if err := u.svc.TaskGroupsError(); err != nil {
		note, col = "The task groups are not used: "+err.Error(), cs.err
	} else if len(u.svc.Config().TaskGroups) == 0 {
		note = "No task groups are defined yet: Edit groups… adds them."
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return smallButton(gtx, u.th, &u.groupEdit.edit, "Edit groups…") }),
					layout.Flexed(1, func(gtx C) D {
						if note == "" {
							return D{}
						}
						l := material.Body2(u.th, note)
						l.Color = col
						return layout.Inset{Left: 12}.Layout(gtx, l.Layout)
					}),
				)
			})
		}),
		layout.Flexed(1, func(gtx C) D { return u.groupsPie.layout(gtx, u.th, u.icons, res.Series) }),
	)
}
