package store

import (
	"strconv"
	"strings"
	"time"
)

// Kind is the dynamic type of a Value.
type Kind int

const (
	Null Kind = iota
	Integer
	Float
	Text
	Blob
)

// Value is a single cell returned by Query. SQLite is dynamically typed, so
// each cell carries its own kind.
type Value struct {
	Kind  Kind
	Int   int64
	Float float64
	Text  string // also holds blobs, as raw bytes
}

// IsNumeric reports whether the value is an integer or a float.
func (v Value) IsNumeric() bool { return v.Kind == Integer || v.Kind == Float }

// Number returns the value as float64; text that parses as a number is
// converted too. ok is false for anything else.
func (v Value) Number() (f float64, ok bool) {
	switch v.Kind {
	case Integer:
		return float64(v.Int), true
	case Float:
		return v.Float, true
	case Text:
		f, err := strconv.ParseFloat(v.Text, 64)
		return f, err == nil
	}
	return 0, false
}

// DurationSeconds parses text of the form H:MM:SS (hours may exceed 24;
// seconds may have a fraction), as produced by SQLite's time(), and
// returns it in seconds. The original viewer treated such columns as
// durations too.
func (v Value) DurationSeconds() (int64, bool) {
	if v.Kind != Text {
		return 0, false
	}
	parts := strings.Split(strings.TrimSpace(v.Text), ":")
	if len(parts) != 3 {
		return 0, false
	}
	h, err1 := strconv.ParseInt(parts[0], 10, 64)
	m, err2 := strconv.ParseInt(parts[1], 10, 64)
	sec, err3 := strconv.ParseFloat(parts[2], 64)
	if err1 != nil || err2 != nil || err3 != nil || h < 0 || m < 0 || m > 59 || sec < 0 || sec >= 60 {
		return 0, false
	}
	return h*3600 + m*60 + int64(sec), true
}

// String renders the value for display or CSV export.
func (v Value) String() string {
	switch v.Kind {
	case Null:
		return ""
	case Integer:
		return strconv.FormatInt(v.Int, 10)
	case Float:
		return strconv.FormatFloat(v.Float, 'f', -1, 64)
	default:
		return v.Text
	}
}

// Table is a generic result set.
type Table struct {
	Columns []string
	Rows    [][]Value
}

// ColumnIndex returns the index of the named column, or -1.
func (t *Table) ColumnIndex(name string) int {
	for i, c := range t.Columns {
		if c == name {
			return i
		}
	}
	return -1
}

func toValue(v any) Value {
	switch x := v.(type) {
	case nil:
		return Value{Kind: Null}
	case int64:
		return Value{Kind: Integer, Int: x}
	case int:
		return Value{Kind: Integer, Int: int64(x)}
	case float64:
		return Value{Kind: Float, Float: x}
	case bool:
		if x {
			return Value{Kind: Integer, Int: 1}
		}
		return Value{Kind: Integer, Int: 0}
	case string:
		return Value{Kind: Text, Text: x}
	case []byte:
		return Value{Kind: Blob, Text: string(x)}
	case time.Time:
		return Value{Kind: Integer, Int: x.Unix()}
	default:
		return Value{Kind: Text}
	}
}
