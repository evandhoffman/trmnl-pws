package solar

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/series"

	"github.com/evandhoffman/trmnl-pws/internal/sun"
)

// buckets decodes a "[[minute, value], ...]" string.
func buckets(t *testing.T, s string) [][2]float64 {
	t.Helper()
	var out [][2]float64
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("bad JSON %q: %v", s, err)
	}
	return out
}

type tick struct {
	Min   int
	Label string
}

func ticks(t *testing.T, s string) []tick {
	t.Helper()
	var raw [][2]any
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		t.Fatalf("bad JSON %q: %v", s, err)
	}
	out := make([]tick, len(raw))
	for i, r := range raw {
		out[i] = tick{int(r[0].(float64)), r[1].(string)}
	}
	return out
}

type marker struct {
	Label string `json:"label"`
	Min   int    `json:"min"`
	Time  string `json:"time"`
}

func markers(t *testing.T, s string) []marker {
	t.Helper()
	var out []marker
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("bad JSON %q: %v", s, err)
	}
	return out
}

const (
	w12 = 12 * time.Hour
	w24 = 24 * time.Hour
)

// fridayNow is 2026-10-30 16:00 EDT. The 12-hour window starts 04:00 the same
// day. The 24-hour window starts 2026-10-29 16:00.
func fridayNow(t *testing.T) (time.Time, *time.Location) {
	loc := ny(t)
	return at(loc, 10, 30, 16, 0), loc
}

func TestTodayWindow(t *testing.T) {
	now, loc := fridayNow(t)
	// One solar reading before the window holds through all of it.
	pts := pointMap{DefaultEntities.SolarKW: {pt(at(loc, 10, 28, 12, 0), 0.5)}}
	got := BuildToday(now, loc, w12, pts, DefaultEntities, nil)
	b := buckets(t, got.StrSolar)
	if len(b) != 72 {
		t.Fatalf("len(solar) = %d, want 72", len(b))
	}
	if b[0][0] != 0 || b[len(b)-1][0] != 710 {
		t.Errorf("minutes run %v to %v, want 0 to 710", b[0][0], b[len(b)-1][0])
	}
	for _, k := range b {
		if int(k[0])%10 != 0 {
			t.Fatalf("bucket minute %v is not a multiple of 10", k[0])
		}
	}
	if got.WindowMin != 720 {
		t.Errorf("WindowMin = %d, want 720", got.WindowMin)
	}
	// Midnight is 4 hours before the window starts.
	if got.MidnightMin != -240 {
		t.Errorf("MidnightMin = %d, want -240", got.MidnightMin)
	}
	if got.MidnightLabel != "Fri 10/30" {
		t.Errorf("MidnightLabel = %q, want Fri 10/30", got.MidnightLabel)
	}
	if got.DateLabel != "Friday, October 30" || got.Updated != "4:00 PM" {
		t.Errorf("DateLabel = %q, Updated = %q", got.DateLabel, got.Updated)
	}
	// No home data: no buckets, and an empty array rather than null.
	if got.StrHome != "[]" || got.StrBattery != "[]" || got.StrSun != "[]" || got.StrBattEvents != "[]" {
		t.Errorf("empty arrays: home %s battery %s sun %s events %s", got.StrHome, got.StrBattery, got.StrSun, got.StrBattEvents)
	}
}

func TestTodayWindowLengths(t *testing.T) {
	now, loc := fridayNow(t)
	pts := pointMap{DefaultEntities.SolarKW: {pt(at(loc, 10, 28, 12, 0), 0.5)}}
	for _, c := range []struct {
		window  time.Duration
		min     int
		buckets int
	}{{time.Hour, 60, 6}, {6 * time.Hour, 360, 36}, {24 * time.Hour, 1440, 144}} {
		got := BuildToday(now, loc, c.window, pts, DefaultEntities, nil)
		if got.WindowMin != c.min || len(buckets(t, got.StrSolar)) != c.buckets {
			t.Errorf("%v: WindowMin %d, %d buckets, want %d and %d",
				c.window, got.WindowMin, len(buckets(t, got.StrSolar)), c.min, c.buckets)
		}
	}
}

func TestTodayBucketMeanAndGaps(t *testing.T) {
	now, loc := fridayNow(t)
	ws := now.Add(-w12)
	pts := pointMap{
		// 2.0 until 5 minutes into bucket 0, then 4.0: the mean is 3.0.
		DefaultEntities.SolarKW: {
			pt(at(loc, 10, 28, 12, 0), 2.0),
			pt(ws.Add(5*time.Minute), 4.0),
		},
		// Home starts at 12:00 today with no earlier reading: no earlier buckets.
		DefaultEntities.LoadKW: {pt(at(loc, 10, 30, 12, 0), 1.25)},
	}
	got := BuildToday(now, loc, w12, pts, DefaultEntities, nil)
	s := buckets(t, got.StrSolar)
	if s[0] != [2]float64{0, 3.0} || s[1] != [2]float64{10, 4.0} {
		t.Errorf("solar head = %v %v, want [0 3] [10 4]", s[0], s[1])
	}
	h := buckets(t, got.StrHome)
	if h[0][0] != 480 {
		t.Errorf("home starts at minute %v, want 480", h[0][0])
	}
	if len(h) != 24 {
		t.Errorf("len(home) = %d, want 24", len(h))
	}
	if h[0][1] != 1.3 {
		t.Errorf("home value = %v, want 1.3", h[0][1])
	}
}

// Buckets count from the window start, so the last one ends at now.
func TestTodayLastBucketEndsAtNow(t *testing.T) {
	loc := ny(t)
	now := at(loc, 10, 30, 16, 4)
	pts := pointMap{DefaultEntities.SolarKW: {
		pt(at(loc, 10, 28, 12, 0), 1.0),
		pt(at(loc, 10, 30, 16, 0), 5.0),
	}}
	b := buckets(t, BuildToday(now, loc, w12, pts, DefaultEntities, nil).StrSolar)
	last := b[len(b)-1]
	if last != [2]float64{710, 2.6} {
		t.Fatalf("last bucket = %v, want [710 2.6]", last)
	}
}

// The chart uses 10-minute buckets. The header peak uses 5-minute buckets, so
// a 5-minute spike shows its full height in the header only.
func TestTodayPeakUsesFiveMinuteBuckets(t *testing.T) {
	now, loc := fridayNow(t)
	pts := pointMap{DefaultEntities.SolarKW: {
		pt(at(loc, 10, 30, 0, 0), 0.2),
		pt(at(loc, 10, 30, 12, 0), 6.0),
		pt(at(loc, 10, 30, 12, 5), 0.2),
	}}
	got := BuildToday(now, loc, w12, pts, DefaultEntities, nil)
	if str(got.PeakKW) != "6.0" || str(got.PeakTime) != "12:00 PM" {
		t.Errorf("peak = %s at %s, want 6.0 at 12:00 PM", str(got.PeakKW), str(got.PeakTime))
	}
	for _, k := range buckets(t, got.StrSolar) {
		if k[0] == 480 && k[1] != 3.1 {
			t.Errorf("chart bucket at minute 480 = %v, want 3.1", k[1])
		}
	}
}

// Midnight is outside the 12-hour window at 23:30, but the peak still covers
// the day from 00:00.
func TestTodayPeakReachesBeforeWindow(t *testing.T) {
	loc := ny(t)
	now := at(loc, 10, 30, 23, 30)
	pts := pointMap{DefaultEntities.SolarKW: {
		pt(at(loc, 10, 29, 12, 0), 8.0),
		pt(at(loc, 10, 30, 0, 0), 0.0),
		pt(at(loc, 10, 30, 5, 0), 6.0),
		pt(at(loc, 10, 30, 5, 5), 1.0),
	}}
	got := BuildToday(now, loc, w12, pts, DefaultEntities, nil)
	if str(got.PeakKW) != "6.0" || str(got.PeakTime) != "5:00 AM" {
		t.Errorf("peak = %s at %s, want 6.0 at 5:00 AM", str(got.PeakKW), str(got.PeakTime))
	}
	if got.MidnightMin != -690 {
		t.Errorf("MidnightMin = %d, want -690", got.MidnightMin)
	}
}

func TestTodayYesterday(t *testing.T) {
	now, loc := fridayNow(t)
	pts := pointMap{
		DefaultEntities.SolarKWh: {
			pt(at(loc, 10, 28, 23, 59), 41.0), // two days ago: ignored
			pt(at(loc, 10, 29, 12, 0), 20.0),
			pt(at(loc, 10, 29, 23, 58), 38.6),
			pt(at(loc, 10, 30, 0, 3), 0.1),
		},
		DefaultEntities.SolarKW: {
			pt(at(loc, 10, 28, 12, 0), 9.0), // two days ago: ignored
			pt(at(loc, 10, 29, 0, 0), 0.0),
			pt(at(loc, 10, 29, 12, 0), 5.0),
			pt(at(loc, 10, 29, 12, 5), 7.25),
			pt(at(loc, 10, 29, 12, 10), 2.0),
			pt(at(loc, 10, 29, 18, 0), 0.0),
		},
	}
	got := BuildToday(now, loc, w12, pts, DefaultEntities, nil)
	if str(got.YesterdayKWh) != "38.6" || str(got.YesterdayPeakKW) != "7.2" {
		t.Errorf("yesterday = %s kWh, %s kW, want 38.6 and 7.2", str(got.YesterdayKWh), str(got.YesterdayPeakKW))
	}
}

func TestTodayYesterdayWithoutRows(t *testing.T) {
	now, loc := fridayNow(t)
	cases := []struct {
		name string
		pts  []series.Point
		want string
	}{
		{"zero seed", []series.Point{pt(at(loc, 10, 28, 23, 59), 0)}, "0.0"},
		{"nonzero seed", []series.Point{pt(at(loc, 10, 28, 23, 59), 31.2)}, "<nil>"},
		{"no data", nil, "<nil>"},
	}
	for _, c := range cases {
		got := BuildToday(now, loc, w12, pointMap{DefaultEntities.SolarKWh: c.pts}, DefaultEntities, nil)
		if str(got.YesterdayKWh) != c.want {
			t.Errorf("%s: yesterday_kwh = %q, want %q", c.name, str(got.YesterdayKWh), c.want)
		}
		if got.YesterdayPeakKW != nil {
			t.Errorf("%s: yesterday_peak_kw = %q, want null", c.name, str(got.YesterdayPeakKW))
		}
	}
}

func TestTodaySunText(t *testing.T) {
	now, loc := fridayNow(t)
	events := []sun.Event{
		{Label: "Rise", At: at(loc, 10, 29, 6, 51)}, // yesterday
		{Label: "Set", At: at(loc, 10, 29, 17, 55)},
		{Label: "Set", At: at(loc, 10, 30, 16, 51)}, // out of order on purpose
		{Label: "Rise", At: at(loc, 10, 30, 6, 26)},
		{Label: "Noon", At: at(loc, 10, 30, 11, 38)},
	}
	got := BuildToday(now, loc, w12, pointMap{}, DefaultEntities, events)
	if want := "rise 6:26 \u00b7 noon 11:38 \u00b7 set 4:51"; str(got.SunText) != want {
		t.Errorf("SunText = %q, want %q", str(got.SunText), want)
	}
	for name, evs := range map[string][]sun.Event{
		"nil":            nil,
		"only yesterday": events[:2],
	} {
		if got := BuildToday(now, loc, w12, pointMap{}, DefaultEntities, evs); got.SunText != nil {
			t.Errorf("%s: SunText = %q, want null", name, str(got.SunText))
		}
	}
}

func TestTodayHeader(t *testing.T) {
	now, loc := fridayNow(t)
	pts := pointMap{
		DefaultEntities.SolarKW: {
			// Yesterday evening is inside the window but before midnight.
			pt(at(loc, 10, 29, 12, 0), 9.0),
			pt(at(loc, 10, 29, 20, 0), 0.0),
			pt(at(loc, 10, 30, 12, 50), 6.4),
			pt(at(loc, 10, 30, 13, 5), 5.0),
			pt(now, 3.0),
		},
		DefaultEntities.LoadKW:    {pt(at(loc, 10, 30, 15, 0), 2.5), pt(now, 3.3), pt(now.Add(time.Minute), 9.9)},
		DefaultEntities.ChargePct: {pt(at(loc, 10, 30, 15, 0), 83.4)},
		DefaultEntities.SolarKWh: {
			pt(at(loc, 10, 29, 23, 59), 40.0),
			pt(at(loc, 10, 30, 0, 5), 0.1),
			pt(at(loc, 10, 30, 12, 0), 11.04),
		},
		DefaultEntities.ReservePct: {
			pt(at(loc, 10, 20, 0, 0), 20),
			pt(at(loc, 10, 30, 9, 0), 30),
		},
	}
	got := BuildToday(now, loc, w12, pts, DefaultEntities, nil)
	for name, c := range map[string][2]string{
		"generated": {str(got.GeneratedKWh), "11.0"},
		"peak kw":   {str(got.PeakKW), "6.4"},
		"peak time": {str(got.PeakTime), "12:50 PM"},
		"solar now": {str(got.NowSolarKW), "3.0"},
		"home now":  {str(got.NowHomeKW), "3.3"},
		"battery":   {str(got.BatteryPct), "83"},
		"yesterday": {str(got.YesterdayKWh), "40.0"},
		"yday peak": {str(got.YesterdayPeakKW), "9.0"},
	} {
		if c[0] != c[1] {
			t.Errorf("%s = %q, want %q", name, c[0], c[1])
		}
	}
	if got.ReservePct == nil || *got.ReservePct != 30 {
		t.Errorf("ReservePct = %v, want 30", got.ReservePct)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if v, ok := m["reserve_pct"].(float64); !ok || v != 30 {
		t.Errorf("reserve_pct JSON = %v, want the number 30", m["reserve_pct"])
	}
}

func TestTodayHeaderNulls(t *testing.T) {
	now, loc := fridayNow(t)
	got := BuildToday(now, loc, w12, pointMap{}, DefaultEntities, nil)
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"generated_kwh", "peak_kw", "peak_time", "now_solar_kw", "now_home_kw", "battery_pct", "reserve_pct", "yesterday_kwh", "yesterday_peak_kw", "sun_text"} {
		if v, ok := m[k]; !ok || v != nil {
			t.Errorf("%s = %v (present %v), want null", k, v, ok)
		}
	}
}

func TestTodayTicks(t *testing.T) {
	now, loc := fridayNow(t)
	got := ticks(t, BuildToday(now, loc, w12, pointMap{}, DefaultEntities, nil).StrTicks)
	// The window starts at 04:00. Ticks fall every 2 hours: 04:00 to 14:00.
	labels := []string{"4a", "6a", "8a", "10a", "12p", "2p"}
	if len(got) != len(labels) {
		t.Fatalf("got %d ticks %v, want %d", len(got), got, len(labels))
	}
	for i, l := range labels {
		if got[i].Label != l || got[i].Min != 120*i {
			t.Errorf("tick %d = %v, want %d %s", i, got[i], 120*i, l)
		}
	}
}

// A window longer than 12 hours ticks every 3 hours.
func TestTodayTicks24h(t *testing.T) {
	now, loc := fridayNow(t)
	got := ticks(t, BuildToday(now, loc, w24, pointMap{}, DefaultEntities, nil).StrTicks)
	// The window starts at 16:00. Ticks fall at 18:00, 21:00, 00:00, ... 15:00.
	labels := []string{"6p", "9p", "12a", "3a", "6a", "9a", "12p", "3p"}
	if len(got) != len(labels) {
		t.Fatalf("got %d ticks %v, want %d", len(got), got, len(labels))
	}
	for i, l := range labels {
		if got[i].Label != l || got[i].Min != 120+180*i {
			t.Errorf("tick %d = %v, want %d %s", i, got[i], 120+180*i, l)
		}
	}
}

func TestTodayTicksDST(t *testing.T) {
	loc := ny(t)
	// 2026-11-01 is 25 hours long. The window runs from 7 PM EDT on Oct 31
	// to 6 AM EST.
	now := at(loc, 11, 1, 6, 0)
	got := BuildToday(now, loc, w12, pointMap{}, DefaultEntities, nil)
	if got.WindowMin != 720 {
		t.Errorf("WindowMin = %d, want 720", got.WindowMin)
	}
	if got.MidnightMin != 300 {
		t.Errorf("MidnightMin = %d, want 300", got.MidnightMin)
	}
	if got.MidnightLabel != "Sun 11/1" {
		t.Errorf("MidnightLabel = %q, want Sun 11/1", got.MidnightLabel)
	}
	tk := ticks(t, got.StrTicks)
	// 2a comes 3 real hours after 12a because the 1 AM hour repeats.
	want := []tick{{60, "8p"}, {180, "10p"}, {300, "12a"}, {480, "2a"}, {600, "4a"}}
	if len(tk) != len(want) {
		t.Fatalf("ticks = %v, want %v", tk, want)
	}
	for i := range want {
		if tk[i] != want[i] {
			t.Errorf("tick %d = %v, want %v", i, tk[i], want[i])
		}
	}
}

// The 24-hour window crosses the same change, with 3-hour ticks.
func TestTodayTicksDST24h(t *testing.T) {
	loc := ny(t)
	// 2026-11-01 is 25 hours long. The window starts 2026-10-31 17:00 EDT.
	now := at(loc, 11, 1, 16, 0)
	got := BuildToday(now, loc, w24, pointMap{}, DefaultEntities, nil)
	if got.MidnightMin != 420 {
		t.Errorf("MidnightMin = %d, want 420", got.MidnightMin)
	}
	tk := ticks(t, got.StrTicks)
	// 3a comes 4 real hours after 12a because the 1 AM hour repeats.
	want := []tick{
		{60, "6p"}, {240, "9p"}, {420, "12a"}, {660, "3a"},
		{840, "6a"}, {1020, "9a"}, {1200, "12p"}, {1380, "3p"},
	}
	if len(tk) != len(want) {
		t.Fatalf("ticks = %v, want %v", tk, want)
	}
	for i := range want {
		if tk[i] != want[i] {
			t.Errorf("tick %d = %v, want %v", i, tk[i], want[i])
		}
	}
}

func TestTodayTickAtWindowStart(t *testing.T) {
	loc := ny(t)
	now := at(loc, 10, 30, 14, 0) // window starts at 02:00, a tick
	tk := ticks(t, BuildToday(now, loc, w12, pointMap{}, DefaultEntities, nil).StrTicks)
	if len(tk) != 6 || tk[0] != (tick{0, "2a"}) {
		t.Fatalf("ticks = %v, want 6 ticks starting {0 2a}", tk)
	}
}

func TestTodaySun(t *testing.T) {
	now, loc := fridayNow(t)
	events := []sun.Event{
		{Label: "Rise", At: at(loc, 10, 29, 7, 0)},  // before the window
		{Label: "Noon", At: at(loc, 10, 29, 12, 0)}, // before the window
		{Label: "Set", At: at(loc, 10, 29, 17, 55)},
		{Label: "Rise", At: at(loc, 10, 30, 6, 52)},
		{Label: "Noon", At: at(loc, 10, 30, 12, 1)},
		{Label: "Set", At: at(loc, 10, 30, 17, 54)}, // after now
	}
	got := markers(t, BuildToday(now, loc, w12, pointMap{}, DefaultEntities, events).StrSun)
	want := []marker{
		{"Rise", 172, "6:52"},
		{"Noon", 481, "12:01"},
	}
	if len(got) != len(want) {
		t.Fatalf("sun = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sun %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestTodayEventsTwoDays(t *testing.T) {
	now, loc := fridayNow(t)
	pts := pointMap{
		DefaultEntities.ChargePct: {
			pt(at(loc, 10, 29, 6, 0), 50),
			pt(at(loc, 10, 29, 11, 0), 100), // full yesterday: before the window
			pt(at(loc, 10, 29, 19, 0), 90),  // draining yesterday
			pt(at(loc, 10, 29, 23, 0), 20),  // reserve yesterday
			pt(at(loc, 10, 30, 10, 0), 100), // full today, still at max
		},
		DefaultEntities.ReservePct: {pt(at(loc, 10, 1, 0, 0), 20)},
	}
	got := markers(t, BuildToday(now, loc, w24, pts, DefaultEntities, nil).StrBattEvents)
	want := []marker{
		{"draining", 180, "7:00p"},
		{"reserve", 420, "11:00p"},
		{"full", 1080, "10:00a"},
	}
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestTodayBatteryStrip(t *testing.T) {
	now, loc := fridayNow(t)
	pts := pointMap{DefaultEntities.ChargePct: {pt(at(loc, 10, 29, 6, 0), 83.4)}}
	got := BuildToday(now, loc, w12, pts, DefaultEntities, nil)
	b := buckets(t, got.StrBattery)
	if len(b) != 72 || b[0][1] != 83 {
		t.Fatalf("battery = %d buckets, first value %v, want 72 and 83", len(b), b[0][1])
	}
}

func TestTodayNoSolarPeak(t *testing.T) {
	now, loc := fridayNow(t)
	pts := pointMap{DefaultEntities.SolarKW: {pt(at(loc, 10, 28, 12, 0), 0)}}
	got := BuildToday(now, loc, w12, pts, DefaultEntities, nil)
	if str(got.PeakKW) != "0.0" || got.PeakTime != nil {
		t.Fatalf("peak = %s at %s, want 0.0 and no time", str(got.PeakKW), str(got.PeakTime))
	}
}

func TestTodayGeneratedWithoutRows(t *testing.T) {
	now, loc := fridayNow(t)
	cases := []struct {
		name string
		pts  []series.Point
		want string
	}{
		{"zero seed", []series.Point{pt(at(loc, 10, 29, 23, 59), 0)}, "0.0"},
		{"nonzero seed", []series.Point{pt(at(loc, 10, 29, 23, 59), 31.2)}, "<nil>"},
		{"no seed", nil, "<nil>"},
	}
	for _, c := range cases {
		got := BuildToday(now, loc, w12, pointMap{DefaultEntities.SolarKWh: c.pts}, DefaultEntities, nil)
		if str(got.GeneratedKWh) != c.want {
			t.Errorf("%s: generated = %q, want %q", c.name, str(got.GeneratedKWh), c.want)
		}
	}
}

// yesterdayCycle is a full cycle on 2026-10-29: full at 11:00 (before the
// window), draining at 19:00, reserve at 23:00.
func yesterdayCycle(loc *time.Location) []series.Point {
	return []series.Point{
		pt(at(loc, 10, 29, 6, 0), 50),
		pt(at(loc, 10, 29, 11, 0), 100),
		pt(at(loc, 10, 29, 19, 0), 90),
		pt(at(loc, 10, 29, 23, 0), 20),
	}
}

func eventLabels(ms []marker) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Label + " " + m.Time
	}
	return out
}

func checkEvents(t *testing.T, got []marker, want []marker) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// R9: today's "full" mark waits until it is final.
func TestTodayFullWaitsWhileCharging(t *testing.T) {
	now, loc := fridayNow(t)
	charge := append(yesterdayCycle(loc),
		pt(at(loc, 10, 30, 6, 0), 40),
		pt(at(loc, 10, 30, 10, 0), 50), // still charging at 16:00, below 100
	)
	pts := pointMap{
		DefaultEntities.ChargePct:  charge,
		DefaultEntities.ReservePct: {pt(at(loc, 10, 1, 0, 0), 20)},
	}
	got := markers(t, BuildToday(now, loc, w24, pts, DefaultEntities, nil).StrBattEvents)
	checkEvents(t, got, []marker{
		{"draining", 180, "7:00p"},
		{"reserve", 420, "11:00p"},
	})
	for _, m := range got {
		if m.Label == "full" {
			t.Fatalf("a charging battery has a full mark: %v", eventLabels(got))
		}
	}
}

func TestTodayFullShownAt100WhileStillFull(t *testing.T) {
	now, loc := fridayNow(t)
	pts := pointMap{
		DefaultEntities.ChargePct: {
			pt(at(loc, 10, 30, 6, 0), 60),
			pt(at(loc, 10, 30, 13, 0), 100), // still 100 at 16:00
		},
		DefaultEntities.ReservePct: {pt(at(loc, 10, 1, 0, 0), 20)},
	}
	got := markers(t, BuildToday(now, loc, w12, pts, DefaultEntities, nil).StrBattEvents)
	checkEvents(t, got, []marker{{"full", 540, "1:00p"}})
}

func TestTodayFullShownOnceDraining(t *testing.T) {
	now, loc := fridayNow(t)
	pts := pointMap{
		DefaultEntities.ChargePct: {
			pt(at(loc, 10, 30, 6, 0), 50),
			pt(at(loc, 10, 30, 12, 0), 95), // day max, below 100
			pt(at(loc, 10, 30, 14, 0), 80), // left its max for good
		},
		DefaultEntities.ReservePct: {pt(at(loc, 10, 1, 0, 0), 20)},
	}
	got := markers(t, BuildToday(now, loc, w12, pts, DefaultEntities, nil).StrBattEvents)
	checkEvents(t, got, []marker{
		{"full", 480, "12:00p"},
		{"draining", 600, "2:00p"},
	})
}

// R13: today's marks are final only when the battery has really left its max.
func TestTodayMarksDipAndRecoverStaysHidden(t *testing.T) {
	loc := ny(t)
	now := at(loc, 10, 30, 11, 20)
	pts := pointMap{
		DefaultEntities.ChargePct: {
			pt(at(loc, 10, 30, 6, 0), 50),
			pt(at(loc, 10, 30, 11, 0), 55),
			pt(at(loc, 10, 30, 11, 10), 54), // 1 point below max, no Set known
		},
		DefaultEntities.ReservePct: {pt(at(loc, 10, 1, 0, 0), 20)},
	}
	got := markers(t, BuildToday(now, loc, w12, pts, DefaultEntities, nil).StrBattEvents)
	checkEvents(t, got, []marker{})
}

func TestTodayMarksShownThreePointsBelowMax(t *testing.T) {
	loc := ny(t)
	now := at(loc, 10, 30, 11, 20)
	pts := pointMap{
		DefaultEntities.ChargePct: {
			pt(at(loc, 10, 30, 6, 0), 50),
			pt(at(loc, 10, 30, 11, 0), 55),
			pt(at(loc, 10, 30, 11, 10), 51), // 4 points below max
		},
		DefaultEntities.ReservePct: {pt(at(loc, 10, 1, 0, 0), 20)},
	}
	got := markers(t, BuildToday(now, loc, w12, pts, DefaultEntities, nil).StrBattEvents)
	checkEvents(t, got, []marker{
		{"full", 700, "11:00a"},
		{"draining", 710, "11:10a"},
	})
}

func TestTodayMarksShownAfterSunset(t *testing.T) {
	loc := ny(t)
	now := at(loc, 10, 30, 17, 30)
	pts := pointMap{
		DefaultEntities.ChargePct: {
			pt(at(loc, 10, 30, 6, 0), 50),
			pt(at(loc, 10, 30, 14, 0), 80),
			pt(at(loc, 10, 30, 15, 0), 79), // 1 point below max
		},
		DefaultEntities.ReservePct: {pt(at(loc, 10, 1, 0, 0), 20)},
	}
	events := []sun.Event{{Label: "Set", At: at(loc, 10, 30, 16, 51)}}
	got := markers(t, BuildToday(now, loc, w12, pts, DefaultEntities, events).StrBattEvents)
	checkEvents(t, got, []marker{
		{"full", 510, "2:00p"},
		{"draining", 570, "3:00p"},
	})
	// Before sunset, the same data shows nothing.
	early := at(loc, 10, 30, 16, 30)
	got = markers(t, BuildToday(early, loc, w12, pts, DefaultEntities, events).StrBattEvents)
	checkEvents(t, got, []marker{})
}
