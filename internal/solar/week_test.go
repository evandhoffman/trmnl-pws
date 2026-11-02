package solar

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/date"
	"github.com/evandhoffman/trmnl-pws/internal/series"
)

const netZeroKW = 0.05

type pointMap = map[string][]series.Point

// weekFor builds the week for Friday 2026-10-30 at 16:00.
func weekFor(t *testing.T, pts pointMap) Week {
	t.Helper()
	loc := ny(t)
	now := at(loc, 10, 30, 16, 0)
	return BuildWeek(date.Of(now, loc), now, loc, pts, DefaultEntities, netZeroKW)
}

func TestWeekShape(t *testing.T) {
	w := weekFor(t, pointMap{})
	if len(w.Days) != 8 {
		t.Fatalf("len(Days) = %d, want 8", len(w.Days))
	}
	if got := str(w.Days[0].Label); got != "Fri 10/23" {
		t.Errorf("first label = %q, want Fri 10/23", got)
	}
	if got := str(w.Days[7].Label); got != "Fri 10/30 (now)" {
		t.Errorf("last label = %q, want Fri 10/30 (now)", got)
	}
	if w.Updated != "4:00 PM" {
		t.Errorf("Updated = %q, want 4:00 PM", w.Updated)
	}
	// No data at all: every metric is nil, never 0.
	d := w.Days[3]
	for name, p := range map[string]*string{
		"solar": d.SolarKWh, "home": d.HomeKWh, "grid in": d.GridInKWh,
		"grid out": d.GridOutKWh, "self": d.SelfPoweredPct, "net zero": d.NetZeroH,
		"peak": d.PeakPct, "full": d.FullTime, "drain": d.DrainTime,
		"reserve": d.ReserveTime, "car": d.CarKWh,
	} {
		if p != nil {
			t.Errorf("%s = %q, want nil", name, *p)
		}
	}
	if w.WeekSolarKWh != "0.0" {
		t.Errorf("WeekSolarKWh = %q, want 0.0", w.WeekSolarKWh)
	}
}

func TestWeekGapDay(t *testing.T) {
	loc := ny(t)
	// Home Assistant writes only on change, so the day has 5 rows.
	grid := []series.Point{
		pt(at(loc, 10, 27, 23, 0), 0.0),
		pt(at(loc, 10, 28, 7, 0), 1.5),
		pt(at(loc, 10, 28, 8, 30), 0.0),
		pt(at(loc, 10, 28, 17, 0), -2.0),
		pt(at(loc, 10, 28, 19, 0), 0.0),
	}
	w := weekFor(t, pointMap{DefaultEntities.GridKW: grid})
	// 7 + 8.5 + 5 hours.
	if got := str(w.Days[5].NetZeroH); got != "20.5" {
		t.Fatalf("net zero on 10/28 = %q, want 20.5", got)
	}
	// 10/26 has no reading yet: nil, not 0.
	if w.Days[3].NetZeroH != nil {
		t.Errorf("net zero on 10/26 = %q, want nil", *w.Days[3].NetZeroH)
	}
	// 10/27 holds 0.0 for the last hour only.
	if got := str(w.Days[4].NetZeroH); got != "1.0" {
		t.Errorf("net zero on 10/27 = %q, want 1.0", got)
	}
}

func TestWeekCounters(t *testing.T) {
	loc := ny(t)
	pts := pointMap{
		DefaultEntities.SolarKWh: {
			pt(at(loc, 10, 28, 6, 0), 0.4),
			pt(at(loc, 10, 28, 12, 0), 20.0),
			pt(at(loc, 10, 28, 18, 0), 47.8),
			pt(at(loc, 10, 29, 0, 5), 0.1),
		},
		DefaultEntities.HomeKWh:    {pt(at(loc, 10, 28, 23, 59), 40.0)},
		DefaultEntities.GridInKWh:  {pt(at(loc, 10, 28, 23, 59), 18.0)},
		DefaultEntities.GridOutKWh: {pt(at(loc, 10, 28, 23, 59), 3.2)},
	}
	w := weekFor(t, pts)
	d := w.Days[5]
	for name, c := range map[string][2]string{
		"solar": {str(d.SolarKWh), "47.8"}, "home": {str(d.HomeKWh), "40.0"},
		"grid in": {str(d.GridInKWh), "18.0"}, "grid out": {str(d.GridOutKWh), "3.2"},
		"self": {str(d.SelfPoweredPct), "55"},
	} {
		if c[0] != c[1] {
			t.Errorf("%s = %q, want %q", name, c[0], c[1])
		}
	}
	if got := str(w.Days[6].SolarKWh); got != "0.1" {
		t.Errorf("solar on 10/29 = %q, want 0.1", got)
	}
	// 10/27 has no reading inside the day: nil even though a later day has one.
	if w.Days[4].SolarKWh != nil {
		t.Errorf("solar on 10/27 = %q, want nil", *w.Days[4].SolarKWh)
	}
}

func TestWeekSelfPowered(t *testing.T) {
	loc := ny(t)
	cases := []struct {
		name         string
		home, gridIn []series.Point
		want         string
	}{
		{"grid above home clamps to 0", []series.Point{pt(at(loc, 10, 28, 20, 0), 10)}, []series.Point{pt(at(loc, 10, 28, 20, 0), 12)}, "0"},
		{"negative grid clamps to 100", []series.Point{pt(at(loc, 10, 28, 20, 0), 10)}, []series.Point{pt(at(loc, 10, 28, 20, 0), -1)}, "100"},
		{"zero home is nil", []series.Point{pt(at(loc, 10, 28, 20, 0), 0)}, []series.Point{pt(at(loc, 10, 28, 20, 0), 1)}, "<nil>"},
		{"missing home is nil", nil, []series.Point{pt(at(loc, 10, 28, 20, 0), 1)}, "<nil>"},
		{"missing grid in is nil", []series.Point{pt(at(loc, 10, 28, 20, 0), 10)}, nil, "<nil>"},
	}
	for _, c := range cases {
		w := weekFor(t, pointMap{DefaultEntities.HomeKWh: c.home, DefaultEntities.GridInKWh: c.gridIn})
		if got := str(w.Days[5].SelfPoweredPct); got != c.want {
			t.Errorf("%s: self = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestWeekDSTFallBack(t *testing.T) {
	loc := ny(t)
	now := at(loc, 11, 2, 16, 0)
	// The grid sits at 0 all of 2026-11-01, a 25-hour day. Only the seed exists.
	pts := pointMap{DefaultEntities.GridKW: {pt(at(loc, 10, 30, 12, 0), 0)}}
	w := BuildWeek(date.Of(now, loc), now, loc, pts, DefaultEntities, netZeroKW)
	if got := str(w.Days[6].NetZeroH); got != "25.0" {
		t.Fatalf("net zero on 11/01 = %q, want 25.0", got)
	}
	if got := str(w.Days[5].NetZeroH); got != "24.0" {
		t.Errorf("net zero on 10/31 = %q, want 24.0", got)
	}
}

func TestWeekDSTSpringForward(t *testing.T) {
	loc := ny(t)
	now := at(loc, 3, 9, 16, 0)
	pts := pointMap{DefaultEntities.GridKW: {pt(time.Date(2026, 3, 7, 12, 0, 0, 0, loc), 0)}}
	w := BuildWeek(date.Of(now, loc), now, loc, pts, DefaultEntities, netZeroKW)
	if got := str(w.Days[6].NetZeroH); got != "23.0" {
		t.Fatalf("net zero on 03/08 = %q, want 23.0", got)
	}
}

func TestWeekNoSeed(t *testing.T) {
	loc := ny(t)
	// The charger has no history before day 3 (10/25).
	pts := pointMap{DefaultEntities.CarKW: {
		pt(at(loc, 10, 25, 8, 0), 7.0),
		pt(at(loc, 10, 25, 10, 0), 0),
	}}
	w := weekFor(t, pts)
	for i := 0; i < 2; i++ {
		if w.Days[i].CarKWh != nil {
			t.Errorf("day %d car = %q, want nil", i+1, *w.Days[i].CarKWh)
		}
	}
	if got := str(w.Days[2].CarKWh); got != "14.0" {
		t.Errorf("day 3 car = %q, want 14.0", got)
	}
	// After the charger stops it holds 0: a known zero, not nil.
	if got := str(w.Days[3].CarKWh); got != "0.0" {
		t.Errorf("day 4 car = %q, want 0.0", got)
	}
}

func TestWeekCarFlooredAtZero(t *testing.T) {
	loc := ny(t)
	pts := pointMap{DefaultEntities.CarKW: {pt(at(loc, 10, 28, 0, 0), -0.1)}}
	w := weekFor(t, pts)
	if got := str(w.Days[5].CarKWh); got != "0.0" {
		t.Fatalf("car = %q, want 0.0", got)
	}
}

func TestWeekToday(t *testing.T) {
	loc := ny(t)
	// The grid is at 0 from the day before yesterday. Today counts to now (16:00), not to midnight.
	pts := pointMap{DefaultEntities.GridKW: {pt(at(loc, 10, 28, 12, 0), 0)}}
	w := weekFor(t, pts)
	if got := str(w.Days[7].NetZeroH); got != "16.0" {
		t.Fatalf("net zero today = %q, want 16.0", got)
	}
	if got := str(w.Days[6].NetZeroH); got != "24.0" {
		t.Errorf("net zero yesterday = %q, want 24.0", got)
	}
	// A reading after now does not count.
	pts[DefaultEntities.SolarKWh] = []series.Point{
		pt(at(loc, 10, 30, 10, 0), 5.0),
		pt(at(loc, 10, 30, 17, 0), 9.0),
	}
	w = weekFor(t, pts)
	if got := str(w.Days[7].SolarKWh); got != "5.0" {
		t.Errorf("solar today = %q, want 5.0", got)
	}
}

func TestWeekBattery(t *testing.T) {
	loc := ny(t)
	pts := pointMap{
		DefaultEntities.ChargePct: {
			pt(at(loc, 10, 28, 6, 0), 40),
			pt(at(loc, 10, 28, 13, 0), 100),
			pt(at(loc, 10, 28, 14, 0), 98),
			pt(at(loc, 10, 28, 14, 30), 100),
			pt(at(loc, 10, 28, 17, 10), 90),
			pt(at(loc, 10, 28, 21, 50), 40),
		},
		DefaultEntities.ReservePct: {pt(at(loc, 10, 1, 0, 0), 40)},
	}
	w := weekFor(t, pts)
	d := w.Days[5]
	if str(d.PeakPct) != "100" || str(d.FullTime) != "1:00p" ||
		str(d.DrainTime) != "5:10p" || str(d.ReserveTime) != "9:50p" {
		t.Fatalf("peak %s full %s drain %s reserve %s", str(d.PeakPct), str(d.FullTime), str(d.DrainTime), str(d.ReserveTime))
	}
}

func TestWeekBars(t *testing.T) {
	loc := ny(t)
	pts := pointMap{
		DefaultEntities.SolarKWh:   {pt(at(loc, 10, 28, 18, 0), 47.8), pt(at(loc, 10, 29, 18, 0), 12.0)},
		DefaultEntities.HomeKWh:    {pt(at(loc, 10, 28, 18, 0), 40.04)},
		DefaultEntities.GridInKWh:  {pt(at(loc, 10, 28, 18, 0), 5.2)},
		DefaultEntities.GridOutKWh: {pt(at(loc, 10, 28, 18, 0), 20.0)},
	}
	w := weekFor(t, pts)
	var cats []string
	var solar, load, grid []float64
	for _, c := range []struct {
		text string
		dst  any
	}{{w.StrCategories, &cats}, {w.StrSolar, &solar}, {w.StrLoad, &load}, {w.StrGrid, &grid}} {
		if err := json.Unmarshal([]byte(c.text), c.dst); err != nil {
			t.Fatalf("bad JSON %q: %v", c.text, err)
		}
	}
	if len(cats) != 8 || cats[0] != "Fri 10/23" || cats[7] != "Fri 10/30" {
		t.Errorf("categories = %v", cats)
	}
	if solar[5] != 47.8 || solar[6] != 12.0 || solar[0] != 0 {
		t.Errorf("solar = %v", solar)
	}
	if load[5] != 40.0 {
		t.Errorf("load[5] = %v, want 40", load[5])
	}
	// 5.2 - 20.0, rounded to one decimal.
	if grid[5] != -14.8 {
		t.Errorf("grid[5] = %v, want -14.8", grid[5])
	}
	if grid[0] != 0 {
		t.Errorf("grid[0] = %v, want 0", grid[0])
	}
	if w.WeekSolarKWh != "59.8" {
		t.Errorf("WeekSolarKWh = %q, want 59.8", w.WeekSolarKWh)
	}
}
