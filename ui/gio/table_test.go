package gio

import (
	"testing"

	"github.com/solworktech/trackz/internal/store"
)

func textV(s string) store.Value { return store.Value{Kind: store.Text, Text: s} }
func intV(n int64) store.Value   { return store.Value{Kind: store.Integer, Int: n} }

// Durations sort by length of time: as text, "10:00:00" < "9:59:59" and
// "100:00:00" < "20:00:00".
func TestSortByDuration(t *testing.T) {
	tbl := &store.Table{Columns: []string{"window_name", "duration"}}
	for _, d := range []string{"9:59:59", "10:00:00", "00:00:05", "100:00:00", "20:00:00", "01:02:03"} {
		tbl.Rows = append(tbl.Rows, []store.Value{textV("w " + d), textV(d)})
	}
	var tv tableView
	cols := defaultColumns(tbl)
	tv.sortCol, tv.desc = 1, false
	var got []string
	for _, r := range tv.rowOrder(tbl, cols) {
		got = append(got, tbl.Rows[r][1].Text)
	}
	want := []string{"00:00:05", "01:02:03", "9:59:59", "10:00:00", "20:00:00", "100:00:00"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ascending = %v, want %v", got, want)
		}
	}
	tv.desc = true
	if first := tbl.Rows[tv.rowOrder(tbl, cols)[0]][1].Text; first != "100:00:00" {
		t.Errorf("descending starts with %s", first)
	}
}

// The dashboard sorts its Duration column by the seconds column.
func TestSortByOtherColumn(t *testing.T) {
	tbl := &store.Table{Columns: []string{"process_name", "duration", "seconds"}, Rows: [][]store.Value{
		{textV("b"), textV("00:01:00"), intV(60)},
		{textV("A"), textV("02:00:00"), intV(7200)},
		{textV("c"), textV("00:10:00"), intV(600)},
	}}
	cols := []tableColumn{{src: 0, sortBy: 0}, {src: 1, sortBy: 2, right: true}}
	tv := tableView{sortCol: 1, desc: true}
	order := tv.rowOrder(tbl, cols)
	if order[0] != 1 || order[1] != 2 || order[2] != 0 {
		t.Errorf("by seconds, descending: %v", order)
	}
	// Text ignores case: A, b, c.
	tv.sortCol, tv.desc = 0, false
	order = tv.rowOrder(tbl, cols)
	if order[0] != 1 || order[1] != 0 || order[2] != 2 {
		t.Errorf("by name: %v", order)
	}
	// No sort chosen: the table's own order.
	tv.sortCol = -1
	order = tv.rowOrder(tbl, cols)
	if order[0] != 0 || order[1] != 1 || order[2] != 2 {
		t.Errorf("unsorted: %v", order)
	}
}

func TestCompareValuesNullsLast(t *testing.T) {
	null := store.Value{Kind: store.Null}
	if compareValues(null, intV(1)) <= 0 || compareValues(intV(1), null) >= 0 || compareValues(null, null) != 0 {
		t.Error("NULL must sort after values")
	}
}
