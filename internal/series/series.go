// Package series does step-hold math on timestamped readings.
//
// Home Assistant writes a value to InfluxDB only when it changes. A reading
// therefore holds from its time until the next reading. No function here
// interpolates between readings.
package series

import "time"

// Point is one reading.
type Point struct {
	T time.Time
	V float64
}

// Span is a value held from Start up to, but not including, End.
type Span struct {
	V          float64
	Start, End time.Time
}

// dur returns the length of the span.
func (s Span) dur() time.Duration { return s.End.Sub(s.Start) }

// Held turns sorted readings into spans that cover [start, end).
//
// The last reading at or before start seeds the first span. With no seed,
// the spans begin at the first reading inside the window. Readings at or
// after end are ignored. Held drops spans of zero length.
func Held(pts []Point, start, end time.Time) []Span {
	if !start.Before(end) {
		return nil
	}
	var spans []Span
	var cur Point
	have := false
	for _, p := range pts {
		if !p.T.After(start) {
			cur, have = Point{T: start, V: p.V}, true
			continue
		}
		if !p.T.Before(end) {
			break
		}
		if have && p.T.After(cur.T) {
			spans = append(spans, Span{V: cur.V, Start: cur.T, End: p.T})
		}
		cur, have = p, true
	}
	if have && end.After(cur.T) {
		spans = append(spans, Span{V: cur.V, Start: cur.T, End: end})
	}
	return spans
}

// Hours returns the total hours of the spans whose value satisfies keep.
func Hours(spans []Span, keep func(v float64) bool) float64 {
	var h float64
	for _, s := range spans {
		if keep(s.V) {
			h += s.dur().Hours()
		}
	}
	return h
}

// KWh returns the energy in the spans. Each value is in kW.
func KWh(spans []Span) float64 {
	var e float64
	for _, s := range spans {
		// The conversion stops a fused multiply-add on arm64, so every CPU
		// gets the same bits and the fixtures match on amd64 CI.
		e += float64(s.V * s.dur().Hours())
	}
	return e
}

// Mean returns the time-weighted mean of the spans. It returns false when
// the spans cover no time.
func Mean(spans []Span) (float64, bool) {
	var sum, hours float64
	for _, s := range spans {
		h := s.dur().Hours()
		sum += float64(s.V * h) // no fused multiply-add; see KWh
		hours += h
	}
	if hours == 0 {
		return 0, false
	}
	return sum / hours, true
}

// Max returns the largest value in the spans. It returns false when there
// are no spans.
func Max(spans []Span) (float64, bool) {
	if len(spans) == 0 {
		return 0, false
	}
	m := spans[0].V
	for _, s := range spans[1:] {
		if s.V > m {
			m = s.V
		}
	}
	return m, true
}

// FirstAt returns the first instant at or after from where keep holds. It
// returns false when keep never holds.
func FirstAt(spans []Span, keep func(v float64) bool, from time.Time) (time.Time, bool) {
	for _, s := range spans {
		if !keep(s.V) || !s.End.After(from) {
			continue
		}
		if s.Start.Before(from) {
			return from, true
		}
		return s.Start, true
	}
	return time.Time{}, false
}

// LastIn returns the last reading in [start, end). It returns false when
// the window holds no reading.
func LastIn(pts []Point, start, end time.Time) (Point, bool) {
	for i := len(pts) - 1; i >= 0; i-- {
		p := pts[i]
		if p.T.Before(end) && !p.T.Before(start) {
			return p, true
		}
	}
	return Point{}, false
}
