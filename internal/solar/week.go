package solar

import (
	"math"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/date"
	"github.com/evandhoffman/trmnl-pws/internal/series"
)

// Day is one row of the week table. A nil field means "no data" and encodes
// as null.
type Day struct {
	Label          *string `json:"label"`
	SolarKWh       *string `json:"solar_kwh"`
	HomeKWh        *string `json:"home_kwh"`
	GridInKWh      *string `json:"grid_in_kwh"`
	GridOutKWh     *string `json:"grid_out_kwh"`
	SelfPoweredPct *string `json:"self_powered_pct"`
	NetZeroH       *string `json:"net_zero_h"`
	PeakPct        *string `json:"peak_pct"`
	FullTime       *string `json:"full_time"`
	DrainTime      *string `json:"drain_time"`
	ReserveTime    *string `json:"reserve_time"`
	CarKWh         *string `json:"car_kwh"`
}

// Week is the Solar Week payload. The Str fields hold JSON text for the chart.
type Week struct {
	Days          []Day  `json:"days"`
	WeekSolarKWh  string `json:"week_solar_kwh"`
	Updated       string `json:"updated"`
	StrCategories string `json:"str_categories"`
	StrSolar      string `json:"str_solar"`
	StrLoad       string `json:"str_load"`
	StrGrid       string `json:"str_grid"`
}

// BuildWeek builds eight days: the seven before today, then today up to now.
// Each day runs from local midnight to the next local midnight, so a day with
// a clock change has 23 or 25 hours.
func BuildWeek(today date.Date, now time.Time, loc *time.Location, pts map[string][]series.Point, e Entities, netZeroKW float64) Week {
	w := Week{Updated: longTime(now.In(loc))}
	var cats []string
	var solar, load, grid []float64
	var weekSolar float64

	for i := 0; i < 8; i++ {
		d := today.AddDays(i - 7)
		start := d.Start(loc)
		end := d.AddDays(1).Start(loc)
		isToday := i == 7
		if isToday || end.After(now) {
			end = now
		}
		row, c := buildDay(start, end, pts, e, netZeroKW)
		label := dayLabel(start)
		cats = append(cats, label)
		if isToday {
			label += " (now)"
		}
		row.Label = &label
		w.Days = append(w.Days, row)

		weekSolar += c.solar
		solar = append(solar, round(c.solar, 1))
		load = append(load, round(c.home, 1))
		grid = append(grid, round(c.gridIn-c.gridOut, 1))
	}

	w.WeekSolarKWh = f1(weekSolar)
	w.StrCategories = jsonString(cats)
	w.StrSolar = jsonString(solar)
	w.StrLoad = jsonString(load)
	w.StrGrid = jsonString(grid)
	return w
}

// counters holds the day totals for the bars. A missing total is 0 there.
type counters struct{ solar, home, gridIn, gridOut float64 }

// buildDay computes the metrics of one window [start, end).
func buildDay(start, end time.Time, pts map[string][]series.Point, e Entities, netZeroKW float64) (Day, counters) {
	var d Day
	var c counters

	// The Tesla counters reset at local midnight: the last reading is the total.
	total := func(id string) (float64, bool) {
		p, ok := series.LastIn(pts[id], start, end)
		return p.V, ok
	}
	kwh := func(id string, dst **string, val *float64) {
		if v, ok := total(id); ok {
			*dst = strp(f1(v))
			*val = v
		}
	}
	kwh(e.SolarKWh, &d.SolarKWh, &c.solar)
	kwh(e.HomeKWh, &d.HomeKWh, &c.home)
	kwh(e.GridInKWh, &d.GridInKWh, &c.gridIn)
	kwh(e.GridOutKWh, &d.GridOutKWh, &c.gridOut)

	home, homeOK := total(e.HomeKWh)
	gridIn, gridInOK := total(e.GridInKWh)
	if homeOK && gridInOK && home != 0 {
		pct := 100 * (1 - gridIn/home)
		d.SelfPoweredPct = strp(f0(math.Max(0, math.Min(100, pct))))
	}

	if grid := series.Held(pts[e.GridKW], start, end); len(grid) > 0 {
		near := func(v float64) bool { return math.Abs(v) < netZeroKW }
		d.NetZeroH = strp(f1(series.Hours(grid, near)))
	}

	if car := series.Held(pts[e.CarKW], start, end); len(car) > 0 {
		d.CarKWh = strp(f1(math.Max(0, series.KWh(car))))
	}

	b := dayBattery(pts, e, start, end)
	if b.Full != nil {
		d.PeakPct = strp(f0(b.Max))
		d.FullTime = timeCell(b.Full, start.Location())
		d.DrainTime = timeCell(b.Drain, start.Location())
		d.ReserveTime = timeCell(b.Reserve, start.Location())
	}
	return d, c
}

// timeCell formats an optional time as a table cell.
func timeCell(t *time.Time, loc *time.Location) *string {
	if t == nil {
		return nil
	}
	return strp(compactTime(t.In(loc)))
}
