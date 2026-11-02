package sun

import (
	"testing"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/date"
)

// New York City Hall is a public point. The expected values come from
// app/utils/solar.py (get_solar_events_for_date) for that point.
const (
	nycLat = 40.7128
	nycLon = -74.0060
)

func nyc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestEventsMatchPython(t *testing.T) {
	loc := nyc(t)
	cases := []struct {
		name string
		d    date.Date
		ms   [3]int64 // Rise, Noon, Set as Unix ms
	}{
		{"summer solstice", date.Date{Year: 2026, Month: 6, Day: 21},
			[3]int64{1782033964895, 1782061131495, 1782088298095}},
		{"dst end day", date.Date{Year: 2026, Month: 11, Day: 1},
			[3]int64{1793532375545, 1793551252245, 1793570128945}},
		{"winter solstice", date.Date{Year: 2026, Month: 12, Day: 21},
			[3]int64{1797855472097, 1797872122248, 1797888772398}},
	}
	labels := [3]string{"Rise", "Noon", "Set"}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Events(c.d, nycLat, nycLon, loc)
			if len(got) != 3 {
				t.Fatalf("got %d events, want 3", len(got))
			}
			for i, ev := range got {
				if ev.Label != labels[i] {
					t.Errorf("event %d label %q, want %q", i, ev.Label, labels[i])
				}
				want := time.UnixMilli(c.ms[i])
				diff := ev.At.Sub(want)
				if diff < 0 {
					diff = -diff
				}
				if diff > time.Minute {
					t.Errorf("%s at %v, want %v (off by %v)", ev.Label, ev.At, want, diff)
				}
				if ev.At.Location() != loc {
					t.Errorf("%s location %v, want %v", ev.Label, ev.At.Location(), loc)
				}
			}
		})
	}
}

func TestEventsAreInOrderOnTheLocalDate(t *testing.T) {
	loc := nyc(t)
	d := date.Date{Year: 2026, Month: 11, Day: 1}
	got := Events(d, nycLat, nycLon, loc)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}
	if !got[0].At.Before(got[1].At) || !got[1].At.Before(got[2].At) {
		t.Errorf("events out of order: %v", got)
	}
	for _, ev := range got {
		if date.Of(ev.At, loc) != d {
			t.Errorf("%s on %v, want %v", ev.Label, date.Of(ev.At, loc), d)
		}
	}
}

func TestEventsPolarNightAndDay(t *testing.T) {
	utc := time.UTC
	// Svalbard in December: the sun stays down.
	if got := Events(date.Date{Year: 2026, Month: 12, Day: 21}, 78.2, 15.6, utc); got != nil {
		t.Errorf("polar night: got %v, want nil", got)
	}
	// Svalbard in June: the sun stays up.
	if got := Events(date.Date{Year: 2026, Month: 6, Day: 21}, 78.2, 15.6, utc); got != nil {
		t.Errorf("polar day: got %v, want nil", got)
	}
}
