package report

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/solworktech/trackz/internal/store"
)

var durationColumn = regexp.MustCompile(`(?i)(time|duration|sec)`)

// Column names with a meaning, as in the original viewer: name a column
// "label" (e.g. "window_name AS label") to choose the labels, and
// "value", "total_time" or "duration" to choose the values.
var (
	labelColumns = []string{"label", "app_name"}
	valueColumns = []string{"value", "total_time", "duration", "seconds", "focus_in_seconds"}
)

// columnKind classifies a result column for charting.
type columnKind int

const (
	colOther    columnKind = iota
	colNumber              // every non-NULL value is a number
	colDuration            // every non-NULL value is H:MM:SS text
)

func classify(t *store.Table, c int) columnKind {
	nums, durs := 0, 0
	for _, row := range t.Rows {
		v := row[c]
		if v.Kind == store.Null {
			continue
		}
		if _, ok := v.DurationSeconds(); ok {
			durs++
		} else if _, ok := v.Number(); ok {
			nums++
		} else {
			return colOther
		}
	}
	switch {
	case durs > 0 && nums == 0:
		return colDuration
	case nums > 0 && durs == 0:
		return colNumber
	}
	return colOther
}

// SeriesFromTable picks a label column and a value column from an arbitrary
// result so it can be charted:
//
//   - value: a column named value, total_time, duration (or seconds), else
//     the first numeric column (the last if every column is numeric, so the
//     first can be the label). Text in H:MM:SS form, as produced by
//     time(x, 'unixepoch'), counts as a duration.
//   - label: a column named label (or app_name), else the first other text
//     column, else the first other column.
//
// Values are durations in seconds when the column holds H:MM:SS text or its
// name mentions time, duration or seconds; otherwise plain numbers.
func SeriesFromTable(t *store.Table) (Series, error) {
	if t == nil || len(t.Columns) == 0 {
		return Series{}, fmt.Errorf("no columns")
	}
	kinds := make([]columnKind, len(t.Columns))
	textual := make([]bool, len(t.Columns))
	for c := range t.Columns {
		kinds[c] = classify(t, c)
		for _, row := range t.Rows {
			if row[c].Kind == store.Text {
				textual[c] = true
				break
			}
		}
	}
	chartable := func(c int) bool { return kinds[c] == colNumber || kinds[c] == colDuration }

	valueCol := -1
	for _, name := range valueColumns {
		if i := t.ColumnIndex(name); i >= 0 && chartable(i) {
			valueCol = i
			break
		}
	}
	if valueCol < 0 {
		all := true
		for c := range t.Columns {
			all = all && chartable(c)
		}
		for c := range t.Columns {
			if chartable(c) {
				valueCol = c
				if !all || len(t.Columns) == 1 {
					break
				}
			}
		}
	}
	if valueCol < 0 {
		return Series{}, fmt.Errorf("the query returned no numeric or H:MM:SS column to chart; use the Table view")
	}

	labelCol := -1
	for _, name := range labelColumns {
		if i := t.ColumnIndex(name); i >= 0 && i != valueCol {
			labelCol = i
			break
		}
	}
	for c := range t.Columns {
		if labelCol < 0 && c != valueCol && textual[c] && kinds[c] != colDuration {
			labelCol = c
		}
	}
	for c := range t.Columns {
		if labelCol < 0 && c != valueCol {
			labelCol = c
		}
	}
	procCol := t.ColumnIndex("process_name")
	appCol := t.ColumnIndex("app_name")

	s := Series{Title: t.Columns[valueCol], Unit: Number}
	if kinds[valueCol] == colDuration || durationColumn.MatchString(t.Columns[valueCol]) {
		s.Unit = Seconds
	}
	for i, row := range t.Rows {
		var v float64
		if secs, ok := row[valueCol].DurationSeconds(); ok {
			v = float64(secs)
		} else {
			v, _ = row[valueCol].Number() // non-numeric values chart as 0
		}
		p := Point{Value: v, Label: fmt.Sprintf("#%d", i+1)}
		if labelCol >= 0 {
			p.Label = row[labelCol].String()
		}
		if procCol >= 0 {
			p.Process = row[procCol].String()
		}
		if appCol >= 0 {
			p.App = row[appCol].String()
		}
		s.Points = append(s.Points, p)
	}
	return s, nil
}

// WriteCSV writes t with a header row.
func WriteCSV(w io.Writer, t *store.Table) error {
	if t == nil {
		return fmt.Errorf("nothing to export")
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(t.Columns); err != nil {
		return err
	}
	rec := make([]string, len(t.Columns))
	for _, row := range t.Rows {
		for i, v := range row {
			rec[i] = v.String()
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// ExportCSV writes t to path, adding a ".csv" extension if missing, and
// returns the path written.
func ExportCSV(path string, t *store.Table) (string, error) {
	if !strings.HasSuffix(strings.ToLower(path), ".csv") {
		path += ".csv"
	}
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	if err := WriteCSV(f, t); err != nil {
		_ = f.Close() // the write error is the one to report
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return path, nil
}

// TableFromSeries converts a series into a two-column table, so any chart
// can be exported or shown as a table.
func TableFromSeries(s Series) *store.Table {
	t := &store.Table{Columns: []string{"label", "value"}}
	if s.Unit == Seconds {
		t.Columns = []string{"label", "duration", "seconds"}
	}
	for _, p := range s.Points {
		row := []store.Value{text(p.Label)}
		if s.Unit == Seconds {
			row = append(row, text(FormatValue(Seconds, p.Value)), integer(int64(p.Value)))
		} else {
			row = append(row, store.Value{Kind: store.Float, Float: p.Value})
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}
