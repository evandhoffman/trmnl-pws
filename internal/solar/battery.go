package solar

import (
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/series"
)

// BatteryEvents holds the three battery marks of one window.
//
// Full is nil when the window has no charge data. Drain is nil when the
// battery is still at its maximum at the end. Reserve is nil when the charge
// does not reach the reserve after the drain.
type BatteryEvents struct {
	Max                  float64
	Full, Drain, Reserve *time.Time
}

// Battery finds the battery events in the step-held charge and reserve of one
// window. End is the end of the window: the next midnight, or now.
//
//   - Full: the first time the charge reaches the window maximum.
//   - Drain: the end of the last span at that maximum. A dip that recovers
//     does not count.
//   - Reserve: the first time at or after Drain that the charge is at or
//     below the highest reserve value of the window.
func Battery(charge, reserve []series.Span, end time.Time) BatteryEvents {
	peak, ok := series.Max(charge)
	if !ok {
		return BatteryEvents{}
	}
	atPeak := func(v float64) bool { return v == peak }
	out := BatteryEvents{Max: peak}

	if t, found := series.FirstAt(charge, atPeak, time.Time{}); found {
		out.Full = &t
	}

	var drain time.Time
	for _, s := range charge {
		if atPeak(s.V) && s.End.After(drain) {
			drain = s.End
		}
	}
	if !drain.Before(end) {
		return out
	}
	out.Drain = &drain

	floor, ok := series.Max(reserve)
	if !ok {
		return out
	}
	belowFloor := func(v float64) bool { return v <= floor }
	if t, found := series.FirstAt(charge, belowFloor, drain); found {
		out.Reserve = &t
	}
	return out
}

// dayBattery runs Battery over one window of the point map.
func dayBattery(pts map[string][]series.Point, e Entities, start, end time.Time) BatteryEvents {
	charge := series.Held(pts[e.ChargePct], start, end)
	reserve := series.Held(pts[e.ReservePct], start, end)
	return Battery(charge, reserve, end)
}
