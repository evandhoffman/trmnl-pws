package solar

import (
	"strconv"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/date"
	"github.com/evandhoffman/trmnl-pws/internal/series"
	"github.com/evandhoffman/trmnl-pws/internal/sun"
)

const (
	window     = 24 * time.Hour
	bucketSize = 5 * time.Minute
	tickEvery  = 3 // wall-clock hours between ticks
)

// Today is the Solar Today payload. The chart x-axis counts minutes from the
// window start, which is 24 hours before now. The Str fields hold JSON text.
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

// bucket is the mean of a series over one 5-minute slot.
type bucket struct {
	at  time.Time
	min int
	v   float64
}

// BuildToday builds the Solar Today payload for the 24 hours ending at now.
// Events holds the sun events of yesterday and today. The payload keeps those
// inside the window.
func BuildToday(now time.Time, loc *time.Location, pts map[string][]series.Point, e Entities, events []sun.Event) Today {
	now = now.In(loc)
	ws := now.Add(-window)
	today := date.Of(now, loc)
	mid := today.Start(loc)

	solar := bucketize(pts[e.SolarKW], ws, now)
	t := Today{
		DateLabel:     longDate(now),
		Updated:       longTime(now),
		StrSolar:      bucketJSON(solar, 1),
		StrHome:       bucketJSON(bucketize(pts[e.LoadKW], ws, now), 1),
		StrGrid:       bucketJSON(bucketize(pts[e.GridKW], ws, now), 1),
		StrBattery:    bucketJSON(bucketize(pts[e.ChargePct], ws, now), 0),
		StrTicks:      jsonString(tickList(ws, now, loc)),
		MidnightMin:   minutesSince(ws, mid),
		MidnightLabel: dayLabel(mid),
	}

	if p, ok := series.LastIn(pts[e.SolarKWh], mid, now); ok {
		t.GeneratedKWh = strp(f1(p.V))
	}
	t.PeakKW, t.PeakTime = peak(solar, mid, loc)
	t.NowSolarKW = nowValue(pts[e.SolarKW], now, f1)
	t.NowHomeKW = nowValue(pts[e.LoadKW], now, f1)
	t.BatteryPct = nowValue(pts[e.ChargePct], now, f0)
	if m, ok := series.Max(series.Held(pts[e.ReservePct], mid, now)); ok {
		t.ReservePct = &m
	}

	t.StrSun = jsonString(sunMarks(events, ws, now, loc))
	t.StrBattEvents = jsonString(battMarks(pts, e, today, ws, now, loc))
	return t
}

// bucketize returns the mean of each 5-minute slot from ws to now. A slot with
// no known value is left out.
func bucketize(pts []series.Point, ws, now time.Time) []bucket {
	var out []bucket
	for w := ws; w.Before(now); w = w.Add(bucketSize) {
		end := w.Add(bucketSize)
		if end.After(now) {
			end = now
		}
		if m, ok := series.Mean(series.Held(pts, w, end)); ok {
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

// peak finds the largest solar bucket since midnight. The first one wins a
// tie. With no solar, it returns a peak of 0.0 and no time.
func peak(solar []bucket, mid time.Time, loc *time.Location) (kw, at *string) {
	var best *bucket
	for i := range solar {
		if solar[i].at.Before(mid) {
			continue
		}
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

// tickList lists each minute of the window where the local clock reads a
// multiple of 3 hours. A clock change moves the gap between ticks, not the
// labels.
func tickList(ws, now time.Time, loc *time.Location) [][2]any {
	ticks := [][2]any{}
	t := ws.Truncate(time.Minute)
	if t.Before(ws) {
		t = t.Add(time.Minute)
	}
	for ; t.Before(now); t = t.Add(time.Minute) {
		l := t.In(loc)
		if l.Minute() == 0 && l.Hour()%tickEvery == 0 {
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

// battMarks finds the battery events of yesterday and today and keeps the ones
// inside the window.
func battMarks(pts map[string][]series.Point, e Entities, today date.Date, ws, now time.Time, loc *time.Location) []chartMark {
	marks := []chartMark{}
	mid := today.Start(loc)
	windows := [][2]time.Time{
		{today.AddDays(-1).Start(loc), mid},
		{mid, now},
	}
	for _, w := range windows {
		b := dayBattery(pts, e, w[0], w[1])
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
