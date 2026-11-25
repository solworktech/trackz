package gio

import (
	"testing"

	"github.com/solworktech/trackz/internal/report"
)

func TestSeriesAxisMax(t *testing.T) {
	for _, c := range []struct{ max, want float64 }{
		{50 * 60, 60 * 60},      // 15-minute steps
		{29 * 60, 40 * 60},      // 10-minute steps
		{3 * 60, 4 * 60},        // 1-minute steps
		{2.6 * 3600, 4 * 3600},  // 1-hour steps
		{4.6 * 3600, 8 * 3600},  // 2-hour steps
		{19 * 3600, 20 * 3600},  // 5-hour steps
		{90 * 3600, 100 * 3600}, // 25-hour steps (2.5 x 10)
		{30, 50},                // under a minute: 1, 2, 2.5, 5 x 10^n
	} {
		s := report.Series{Unit: report.Seconds, Points: []report.Point{{Value: c.max}}}
		if got := seriesAxisMax(s); got != c.want {
			t.Errorf("max %v: axis %v, want %v", c.max, got, c.want)
		}
	}
}
