package solar

import (
	"testing"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/series"
)

func ny(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// at returns a local time. The synthetic fixtures all use October and
// November 2026.
func at(loc *time.Location, month time.Month, day, hour, min int) time.Time {
	return time.Date(2026, month, day, hour, min, 0, 0, loc)
}

func pt(t time.Time, v float64) series.Point { return series.Point{T: t, V: v} }

// str shows a *string for failure messages and comparisons.
func str(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}
