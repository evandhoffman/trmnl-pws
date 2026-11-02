package server

import (
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/series"
	"github.com/evandhoffman/trmnl-pws/internal/solar"
)

// The solar fixtures come from an invented house. This file builds its
// readings, one step per 5 minutes, from integer arithmetic only. Integers
// give the same readings on every CPU. The numbers are not real data.

const (
	scenarioStep    = 5 * time.Minute
	scenarioReserve = 30 // backup reserve, in percent
	scenarioFirst   = "2026-10-24"
)

// scenarioPeaks holds each day's solar peak in tenths of a kW. Oct 30 is cloudy.
var scenarioPeaks = map[string]int{
	"2026-10-24": 55, "2026-10-25": 60, "2026-10-26": 62, "2026-10-27": 58,
	"2026-10-28": 50, "2026-10-29": 66, "2026-10-30": 14, "2026-10-31": 52,
	"2026-11-01": 48, "2026-11-02": 46,
}

// scenarioFastDrain lists the days when the battery falls 1% every step in the
// evening. On the other days it falls 1% every second step.
var scenarioFastDrain = map[string]bool{
	"2026-10-27": true, "2026-10-29": true, "2026-10-31": true, "2026-11-01": true,
}

// scenarioSun returns sunrise and sunset in minutes after local midnight. The
// clocks went back on Nov 1, so the later days have earlier times.
func scenarioSun(day string) (rise, set int) {
	if day >= "2026-11-01" {
		return 390, 1010
	}
	return 449, 1071
}

// scenarioLoad is the house load in tenths of a kW at a local hour.
func scenarioLoad(hour int) int {
	switch {
	case hour < 6:
		return 6
	case hour < 9:
		return 14
	case hour < 17:
		return 9
	case hour < 22:
		return 24
	}
	return 10
}

// scenarioSolar is the solar output in tenths of a kW: a ramp up, a plateau,
// and a ramp down.
func scenarioSolar(day string, minute int) int {
	rise, set := scenarioSun(day)
	if minute <= rise || minute >= set {
		return 0
	}
	edge := min(minute-rise, set-minute, 240)
	return scenarioPeaks[day] * edge / 240
}

// scenarioPoints returns the invented readings from 2026-10-24 up to now.
//
// Some sensors start late on purpose, so the week table shows every kind of
// gap: no counters before Oct 27, no solar counter before Oct 28, and no car
// readings before Oct 29. Like Home Assistant, each sensor gets a reading
// only when its value changes.
func scenarioPoints(loc *time.Location, now time.Time) map[string][]series.Point {
	e := solar.DefaultEntities
	pts := map[string][]series.Point{}
	last := map[string]float64{}
	put := func(id string, t time.Time, v float64) {
		if prev, seen := last[id]; seen && prev == v {
			return
		}
		last[id] = v
		pts[id] = append(pts[id], series.Point{T: t, V: v})
	}

	// Reserve and car start at the first step.
	start := time.Date(2026, 10, 24, 0, 0, 0, 0, loc)
	put(e.ReservePct, start, scenarioReserve)
	put(e.CarKW, start.AddDate(0, 0, 5), 0)

	charge := scenarioReserve
	var solarAcc, homeAcc, inAcc, outAcc int // tenths of a kW times minutes
	prevDay := ""
	for t := start; !t.After(now); t = t.Add(scenarioStep) {
		l := t.In(loc)
		day := l.Format("2006-01-02")
		minute := l.Hour()*60 + l.Minute()
		if day != prevDay {
			solarAcc, homeAcc, inAcc, outAcc = 0, 0, 0, 0
			prevDay = day
		}

		sol := scenarioSolar(day, minute)
		load := scenarioLoad(l.Hour())
		net := sol - load
		drainStep := scenarioFastDrain[day] || (minute/5)%2 == 0
		grid := 0 // tenths of a kW, negative is export
		switch {
		case net > 0 && charge < 100:
			charge++
		case net > 0:
			grid = -net
		case charge > scenarioReserve:
			if drainStep {
				charge--
			}
		default:
			grid = -net
		}

		solarAcc += sol * 5
		homeAcc += load * 5
		if grid > 0 {
			inAcc += grid * 5
		} else {
			outAcc -= grid * 5
		}

		tenths := func(v int) float64 { return float64(v) / 10 }
		put(e.SolarKW, t, tenths(sol))
		put(e.LoadKW, t, tenths(load))
		put(e.GridKW, t, tenths(grid))
		put(e.ChargePct, t, float64(charge))

		// A counter in kWh holds hundredths: tenths of a kW times minutes
		// over 600 is kWh, and times 100 makes hundredths.
		kwh := func(acc int) float64 { return float64(acc*100/600) / 100 }
		if day >= "2026-10-28" {
			put(e.SolarKWh, t, kwh(solarAcc))
		}
		if day >= "2026-10-27" {
			put(e.HomeKWh, t, kwh(homeAcc))
			put(e.GridInKWh, t, kwh(inAcc))
			put(e.GridOutKWh, t, kwh(outAcc))
		}

		// The car charges at 7.4 kW from 10 PM to 2 AM on Oct 29 and Oct 31.
		if day >= "2026-10-29" {
			charging := (l.Hour() >= 22 && (day == "2026-10-29" || day == "2026-10-31")) ||
				(l.Hour() < 2 && (day == "2026-10-30" || day == "2026-11-01"))
			if charging {
				put(e.CarKW, t, 7.4)
			} else {
				put(e.CarKW, t, 0)
			}
		}
	}
	return pts
}
