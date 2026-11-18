// Package timefmt formats times using strftime(3)-style patterns.
//
// strftime patterns are used (rather than Go reference layouts) because they
// are what users already know from SQLite's strftime() and from the shell, and
// because they are what the original Trackz documentation uses.
package timefmt

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Supported lists the conversion specifiers understood by Format.
const Supported = "%a %A %b %B %d %e %F %H %I %j %k %l %m %M %p %P %S %s %T %u %w %y %Y %z %Z %R %D %%"

// Format renders t according to the strftime-style pattern.
// Unknown specifiers are emitted verbatim.
func Format(t time.Time, pattern string) string {
	var b strings.Builder
	b.Grow(len(pattern) + 16)
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c != '%' || i == len(pattern)-1 {
			b.WriteByte(c)
			continue
		}
		i++
		switch pattern[i] {
		case 'a':
			b.WriteString(t.Format("Mon"))
		case 'A':
			b.WriteString(t.Format("Monday"))
		case 'b', 'h':
			b.WriteString(t.Format("Jan"))
		case 'B':
			b.WriteString(t.Format("January"))
		case 'd':
			fmt.Fprintf(&b, "%02d", t.Day())
		case 'e':
			fmt.Fprintf(&b, "%2d", t.Day())
		case 'F':
			b.WriteString(t.Format("2006-01-02"))
		case 'D':
			b.WriteString(t.Format("01/02/06"))
		case 'H':
			fmt.Fprintf(&b, "%02d", t.Hour())
		case 'k':
			fmt.Fprintf(&b, "%2d", t.Hour())
		case 'I':
			fmt.Fprintf(&b, "%02d", hour12(t))
		case 'l':
			fmt.Fprintf(&b, "%2d", hour12(t))
		case 'j':
			fmt.Fprintf(&b, "%03d", t.YearDay())
		case 'm':
			fmt.Fprintf(&b, "%02d", int(t.Month()))
		case 'M':
			fmt.Fprintf(&b, "%02d", t.Minute())
		case 'p':
			b.WriteString(t.Format("PM"))
		case 'P':
			b.WriteString(strings.ToLower(t.Format("PM")))
		case 'S':
			fmt.Fprintf(&b, "%02d", t.Second())
		case 's':
			b.WriteString(strconv.FormatInt(t.Unix(), 10))
		case 'T':
			b.WriteString(t.Format("15:04:05"))
		case 'R':
			b.WriteString(t.Format("15:04"))
		case 'u':
			wd := int(t.Weekday())
			if wd == 0 {
				wd = 7
			}
			b.WriteString(strconv.Itoa(wd))
		case 'w':
			b.WriteString(strconv.Itoa(int(t.Weekday())))
		case 'y':
			fmt.Fprintf(&b, "%02d", t.Year()%100)
		case 'Y':
			b.WriteString(strconv.Itoa(t.Year()))
		case 'z':
			b.WriteString(t.Format("-0700"))
		case 'Z':
			b.WriteString(t.Format("MST"))
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteByte(pattern[i])
		}
	}
	return b.String()
}

// Validate reports an error if pattern contains an unsupported specifier.
func Validate(pattern string) error {
	if strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("format must not be empty")
	}
	for i := 0; i < len(pattern); i++ {
		if pattern[i] != '%' {
			continue
		}
		if i == len(pattern)-1 {
			return fmt.Errorf("dangling %% at end of format")
		}
		i++
		if !strings.Contains(Supported, "%"+string(pattern[i])) && pattern[i] != 'h' {
			return fmt.Errorf("unsupported specifier %%%c (supported: %s)", pattern[i], Supported)
		}
	}
	return nil
}

func hour12(t time.Time) int {
	h := t.Hour() % 12
	if h == 0 {
		h = 12
	}
	return h
}

// Duration renders a number of seconds as HH:MM:SS. Hours are not wrapped at
// 24, so long totals remain readable (e.g. "53:10:02").
func Duration(seconds int64) string {
	neg := seconds < 0
	if neg {
		seconds = -seconds
	}
	s := fmt.Sprintf("%02d:%02d:%02d", seconds/3600, (seconds%3600)/60, seconds%60)
	if neg {
		return "-" + s
	}
	return s
}

// Hours renders a number of seconds as decimal hours, e.g. "1.5h".
func Hours(seconds int64) string {
	return strconv.FormatFloat(float64(seconds)/3600, 'f', 1, 64) + "h"
}
