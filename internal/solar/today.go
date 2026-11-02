package solar

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/date"
	"github.com/evandhoffman/trmnl-pws/internal/series"
	"github.com/evandhoffman/trmnl-pws/internal/sun"
)

const (
	chartBucket = 10 * time.Minute // chart bucket length
	peakBucket  = 5 * time.Minute  // header peak bucket length
	sunSep      = " \u00b7 "       // between sun times: space, middle dot, space
)

// Today is the Solar Today payload. The chart x-axis counts minutes from the
// window start, which is WindowMin minutes before now. The Str fields hold
// JSON text.
type Today struct {
	GeneratedKWh *string  `json:"generated_kwh"`
	PeakKW       *string  `json:"peak_kw"`
	PeakTime     *string  `json:"peak_time"`
	NowSolarKW   *string  `json:"now_solar_kw"`
	NowHomeKW    *string  `json:"now_home_kw"`
	BatteryPct   *string  `json:"battery_pct"`
	ReservePct   *float64 `json:"reserve_pct"`
	DateLabel    string   `json:"date_label"`
	Updated      string   `json:"updated"`

	// WindowMin is the real minutes from the window start to now.
	WindowMin       int     `json:"window_min"`
	YesterdayKWh    *string `json:"yesterday_kwh"`
	YesterdayPeakKW *string `json:"yesterday_peak_kw"`
	// SunText lists today's sun times, such as "rise 6:26 · noon 11:38 · set 4:51".
	SunText *string `json:"sun_text"`

	StrSolar      string `json:"str_solar"`
	StrHome       string `json:"str_home"`
	StrGrid       string `json:"str_grid"`
	StrBattery    string `json:"str_battery"`
	StrTicks      string `json:"str_ticks"`
	MidnightMin   int    `json:"midnight_min"`
	MidnightLabel string `json:"midnight_label"`
	StrSun        string `json:"str_sun"`
	StrBattEvents string `json:"str_batt_events"`
}

// chartMark is a labeled vertical line on the chart.
type chartMark struct {
	Label string `json:"label"`
	Min   int    `json:"min"`
	Time  string `json:"time"`
}

// bucket is the mean of a series over one slot.
type bucket struct {
	at  time.Time
	min int
	v   float64
}

// BuildToday builds the Solar Today payload for the window that ends at now.
// The window is real time, so a clock change inside it needs no special case.
// Events holds the sun events of yesterday and today. The chart keeps those
// inside the window, and the title text keeps today's.
func BuildToday(now time.Time, loc *time.Location, window time.Duration, pts map[string][]series.Point, e Entities, events []sun.Event) Today {
	now = now.In(loc)
	ws := now.Add(-window)
	today := date.Of(now, loc)
	mid := today.Start(loc)
	yesterday := today.AddDays(-1).Start(loc)

	t := Today{
		DateLabel:     longDate(now),
		Updated:       longTime(now),
		WindowMin:     minutesSince(ws, now),
		StrSolar:      bucketJSON(bucketize(pts[e.SolarKW], ws, now, chartBucket), 1),
		StrHome:       bucketJSON(bucketize(pts[e.LoadKW], ws, now, chartBucket), 1),
		StrGrid:       bucketJSON(bucketize(pts[e.GridKW], ws, now, chartBucket), 1),
		StrBattery:    bucketJSON(bucketize(pts[e.ChargePct], ws, now, chartBucket), 0),
		StrTicks:      jsonString(tickList(ws, now, loc)),
		MidnightMin:   minutesSince(ws, mid),
		MidnightLabel: dayLabel(mid),
	}

	if v, ok := counterTotal(pts[e.SolarKWh], mid, now); ok {
		t.GeneratedKWh = strp(f1(v))
	}
	// The peaks use 5-minute buckets over the whole day, not the chart window.
	t.PeakKW, t.PeakTime = peak(bucketize(pts[e.SolarKW], mid, now, peakBucket), loc)
	t.YesterdayPeakKW, _ = peak(bucketize(pts[e.SolarKW], yesterday, mid, peakBucket), loc)
	if v, ok := counterTotal(pts[e.SolarKWh], yesterday, mid); ok {
		t.YesterdayKWh = strp(f1(v))
	}
	t.NowSolarKW = nowValue(pts[e.SolarKW], now, f1)
	t.NowHomeKW = nowValue(pts[e.LoadKW], now, f1)
	t.BatteryPct = nowValue(pts[e.ChargePct], now, f0)
	if m, ok := series.Max(series.Held(pts[e.ReservePct], mid, now)); ok {
		t.ReservePct = &m
	}

	t.SunText = sunText(events, today, loc)
	t.StrSun = jsonString(sunMarks(events, ws, now, loc))
	t.StrBattEvents = jsonString(battMarks(pts, e, today, ws, now, loc))
	return t
}

// bucketize returns the mean of each slot of the given size from ws to end.
// Minutes count from ws. The last slot stops at end. A slot with no known
// value is left out.
func bucketize(pts []series.Point, ws, end time.Time, size time.Duration) []bucket {
	var out []bucket
	for w := ws; w.Before(end); w = w.Add(size) {
		stop := w.Add(size)
		if stop.After(end) {
			stop = end
		}
		if m, ok := series.Mean(series.Held(pts, w, stop)); ok {
			out = append(out, bucket{at: w, min: minutesSince(ws, w), v: m})
		}
	}
	return out
}

// bucketJSON encodes buckets as [[minute, value], ...]. It never returns null.
func bucketJSON(b []bucket, decimals int) string {
	rows := make([][2]float64, 0, len(b))
	for _, k := range b {
		rows = append(rows, [2]float64{float64(k.min), round(k.v, decimals)})
	}
	return jsonString(rows)
}

// peak finds the largest bucket. The first one wins a tie. With no bucket, it
// returns no peak. With only zeros, it returns a peak of 0.0 and no time.
func peak(solar []bucket, loc *time.Location) (kw, at *string) {
	var best *bucket
	for i := range solar {
		if best == nil || solar[i].v > best.v {
			best = &solar[i]
		}
	}
	if best == nil {
		return nil, nil
	}
	kw = strp(f1(best.v))
	if best.v > 0 {
		at = strp(longTime(best.at.In(loc)))
	}
	return kw, at
}

// nowValue formats the last reading at or before now.
func nowValue(pts []series.Point, now time.Time, format func(float64) string) *string {
	for i := len(pts) - 1; i >= 0; i-- {
		if !pts[i].T.After(now) {
			return strp(format(pts[i].V))
		}
	}
	return nil
}

// tickHours returns the hours between ticks: 2 for a window of 12 hours or
// less, and 3 for a longer one.
func tickHours(window time.Duration) int {
	if window <= 12*time.Hour {
		return 2
	}
	return 3
}

// tickList lists each minute of the window where the local clock reads a
// multiple of the tick spacing. A clock change moves the gap between ticks,
// not the labels.
func tickList(ws, now time.Time, loc *time.Location) [][2]any {
	every := tickHours(now.Sub(ws))
	ticks := [][2]any{}
	t := ws.Truncate(time.Minute)
	if t.Before(ws) {
		t = t.Add(time.Minute)
	}
	for ; t.Before(now); t = t.Add(time.Minute) {
		l := t.In(loc)
		if l.Minute() == 0 && l.Hour()%every == 0 {
			ticks = append(ticks, [2]any{minutesSince(ws, t), hourLabel(l.Hour())})
		}
	}
	return ticks
}

// hourLabel formats an hour of the day: "12a", "3a", "12p", "3p".
func hourLabel(h int) string {
	suffix := "a"
	if h >= 12 {
		suffix = "p"
	}
	h12 := h % 12
	if h12 == 0 {
		h12 = 12
	}
	return strconv.Itoa(h12) + suffix
}

// sunMarks keeps the sun events inside the window.
func sunMarks(events []sun.Event, ws, now time.Time, loc *time.Location) []chartMark {
	marks := []chartMark{}
	for _, ev := range events {
		if ev.At.Before(ws) || !ev.At.Before(now) {
			continue
		}
		marks = append(marks, chartMark{
			Label: ev.Label,
			Min:   minutesSince(ws, ev.At),
			Time:  clockTime(ev.At.In(loc)),
		})
	}
	return marks
}

// sunText lists today's sun events in time order: "rise 6:26 · noon 11:38 ·
// set 4:51". It returns nil when today has none.
func sunText(events []sun.Event, today date.Date, loc *time.Location) *string {
	var todays []sun.Event
	for _, ev := range events {
		if date.Of(ev.At, loc) == today {
			todays = append(todays, ev)
		}
	}
	if len(todays) == 0 {
		return nil
	}
	slices.SortFunc(todays, func(a, b sun.Event) int { return a.At.Compare(b.At) })
	parts := make([]string, len(todays))
	for i, ev := range todays {
		parts[i] = strings.ToLower(ev.Label) + " " + clockTime(ev.At.In(loc))
	}
	return strp(strings.Join(parts, sunSep))
}

// battMarks finds the battery events of yesterday and today and keeps the ones
// inside the window. Yesterday's events are final. Today's "full" shows only
// when it is final too.
func battMarks(pts map[string][]series.Point, e Entities, today date.Date, ws, now time.Time, loc *time.Location) []chartMark {
	marks := []chartMark{}
	mid := today.Start(loc)
	windows := [][2]time.Time{
		{today.AddDays(-1).Start(loc), mid},
		{mid, now},
	}
	for i, w := range windows {
		b := dayBattery(pts, e, w[0], w[1])
		// Today's full mark waits until it is final: the battery reached 100%,
		// or it has left its max. A battery still charging below 100% has not
		// reached its day max yet, so "full" would move as it charges.
		if i == len(windows)-1 && b.Max < 100 && b.Drain == nil {
			b.Full = nil
		}
		for _, ev := range []struct {
			label string
			at    *time.Time
		}{{"full", b.Full}, {"draining", b.Drain}, {"reserve", b.Reserve}} {
			if ev.at == nil || ev.at.Before(ws) || !ev.at.Before(now) {
				continue
			}
			marks = append(marks, chartMark{
				Label: ev.label,
				Min:   minutesSince(ws, *ev.at),
				Time:  compactTime(ev.at.In(loc)),
			})
		}
	}
	return marks
}

// minutesSince counts whole minutes from start to t.
func minutesSince(start, t time.Time) int { return int(t.Sub(start) / time.Minute) }
