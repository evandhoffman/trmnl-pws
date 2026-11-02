// Package sun computes sunrise, solar noon, and sunset for a date.
// It ports the NOAA math in app/utils/solar.py, so the times match.
package sun

import (
	"math"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/date"
)

const (
	zenithDegrees        = -0.83
	julianUnixEpoch      = 2440587.5
	j2000                = 2451545.0
	transitLongitudeBias = 102.9372
)

// Event is one sun marker on a day.
type Event struct {
	Label string
	At    time.Time
}

// Events returns the Rise, Noon, and Set events for the local date d.
// It returns nil when the sun does not rise or does not set that day.
func Events(d date.Date, lat, lon float64, loc *time.Location) []Event {
	latRad := rad(lat)
	westLon := -lon
	midnight := d.Start(loc)
	julianDay := float64(midnight.UnixNano())/1e9/86400 + julianUnixEpoch

	// Python round() rounds half to even, so this does too.
	cycle := math.RoundToEven(julianDay - j2000 - 0.0009 - westLon/360)
	noonGuess := j2000 + 0.0009 + westLon/360 + cycle

	anomaly := rad(mod360(357.5291 + 0.98560028*(noonGuess-j2000)))
	center := 1.9148*math.Sin(anomaly) +
		0.0200*math.Sin(2*anomaly) +
		0.0003*math.Sin(3*anomaly)
	eclLon := rad(mod360(deg(anomaly) + center + 180 + transitLongitudeBias))

	transit := noonGuess +
		0.0053*math.Sin(anomaly) -
		0.0069*math.Sin(2*eclLon)
	decl := math.Asin(math.Sin(eclLon) * math.Sin(rad(23.44)))

	cosH := (math.Sin(rad(zenithDegrees)) - math.Sin(latRad)*math.Sin(decl)) /
		(math.Cos(latRad) * math.Cos(decl))
	// The Python code clamps this value. Here the sun never crosses the
	// horizon, so there are no events.
	if cosH < -1 || cosH > 1 || math.IsNaN(cosH) {
		return nil
	}
	hourFrac := deg(math.Acos(cosH)) / 360

	rise := fromJulian(transit-hourFrac, loc)
	noon := fromJulian(transit, loc)
	set := fromJulian(transit+hourFrac, loc)

	// Move the events onto the requested local date. The raw math can land
	// one day off near timezone boundaries.
	if n := date.Of(noon, loc).DaysUntil(d); n != 0 {
		shift := time.Duration(n) * 24 * time.Hour
		rise, noon, set = rise.Add(shift), noon.Add(shift), set.Add(shift)
	}

	return []Event{{"Rise", rise}, {"Noon", noon}, {"Set", set}}
}

func fromJulian(jd float64, loc *time.Location) time.Time {
	sec := (jd - julianUnixEpoch) * 86400
	whole := math.Floor(sec)
	return time.Unix(int64(whole), int64((sec-whole)*1e9)).In(loc)
}

func rad(d float64) float64 { return d * math.Pi / 180 }
func deg(r float64) float64 { return r * 180 / math.Pi }

// mod360 matches Python's % for a positive divisor.
func mod360(x float64) float64 {
	r := math.Mod(x, 360)
	if r < 0 {
		r += 360
	}
	return r
}
