package series

import (
	"math"
	"testing"
	"time"
)

func ny(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// at returns a time on 2026-11-02 in loc. Day 1 is the day before.
func at(loc *time.Location, day, hour, min int) time.Time {
	return time.Date(2026, 11, day, hour, min, 0, 0, loc)
}

func near(got, want float64) bool { return math.Abs(got-want) < 1e-9 }

func absBelow(v float64) bool { return math.Abs(v) < 0.05 }

func all(float64) bool { return true }

func TestHeldLongGap(t *testing.T) {
	loc := ny(t)
	pts := []Point{
		{at(loc, 1, 23, 50), 1.2},
		{at(loc, 2, 10, 0), 0},
		{at(loc, 2, 16, 0), 0.8},
	}
	spans := Held(pts, at(loc, 2, 0, 0), at(loc, 3, 0, 0))
	if got := Hours(spans, absBelow); !near(got, 6.0) {
		t.Fatalf("Hours = %v, want 6.0", got)
	}
	if len(spans) != 3 {
		t.Fatalf("len(spans) = %d, want 3", len(spans))
	}
	if !spans[0].Start.Equal(at(loc, 2, 0, 0)) || spans[0].V != 1.2 {
		t.Fatalf("first span = %+v, want 1.2 kW from midnight", spans[0])
	}
}

func TestHeldNoSeed(t *testing.T) {
	loc := ny(t)
	pts := []Point{{at(loc, 2, 6, 0), 2}}
	spans := Held(pts, at(loc, 2, 0, 0), at(loc, 3, 0, 0))
	if len(spans) != 1 || !spans[0].Start.Equal(at(loc, 2, 6, 0)) {
		t.Fatalf("spans = %+v, want one span from 06:00", spans)
	}
	if got := Hours(spans, all); !near(got, 18.0) {
		t.Fatalf("Hours = %v, want 18.0", got)
	}
}

func TestHeldIgnoresPointsAtOrAfterEnd(t *testing.T) {
	loc := ny(t)
	pts := []Point{
		{at(loc, 2, 6, 0), 1},
		{at(loc, 3, 0, 0), 9},
		{at(loc, 3, 5, 0), 9},
	}
	spans := Held(pts, at(loc, 2, 0, 0), at(loc, 3, 0, 0))
	if len(spans) != 1 || spans[0].V != 1 || !spans[0].End.Equal(at(loc, 3, 0, 0)) {
		t.Fatalf("spans = %+v, want one span of 1 ending at the window end", spans)
	}
}

func TestHeldSeedAtStart(t *testing.T) {
	loc := ny(t)
	pts := []Point{
		{at(loc, 2, 0, 0), 4},
		{at(loc, 2, 12, 0), 5},
	}
	spans := Held(pts, at(loc, 2, 0, 0), at(loc, 3, 0, 0))
	if len(spans) != 2 || spans[0].V != 4 {
		t.Fatalf("spans = %+v, want 4 then 5", spans)
	}
	if got := Hours(spans, all); !near(got, 24.0) {
		t.Fatalf("Hours = %v, want 24.0", got)
	}
}

func TestHeldEmpty(t *testing.T) {
	loc := ny(t)
	if spans := Held(nil, at(loc, 2, 0, 0), at(loc, 3, 0, 0)); len(spans) != 0 {
		t.Fatalf("spans = %+v, want none", spans)
	}
}

func TestKWhStep(t *testing.T) {
	loc := ny(t)
	pts := []Point{
		{at(loc, 2, 1, 0), 7.2},
		{at(loc, 2, 2, 30), 0},
	}
	spans := Held(pts, at(loc, 2, 0, 0), at(loc, 3, 0, 0))
	if got := KWh(spans); !near(got, 10.8) {
		t.Fatalf("KWh = %v, want 10.8", got)
	}
}

func TestMeanTimeWeighted(t *testing.T) {
	loc := ny(t)
	pts := []Point{
		{at(loc, 2, 12, 0), 0},
		{at(loc, 2, 12, 20), 3},
	}
	spans := Held(pts, at(loc, 2, 12, 0), at(loc, 2, 12, 30))
	got, ok := Mean(spans)
	if !ok || !near(got, 1.0) {
		t.Fatalf("Mean = %v, %v, want 1.0, true", got, ok)
	}
}

func TestMeanEmpty(t *testing.T) {
	if got, ok := Mean(nil); ok || got != 0 {
		t.Fatalf("Mean = %v, %v, want 0, false", got, ok)
	}
}

func TestMax(t *testing.T) {
	loc := ny(t)
	spans := Held([]Point{
		{at(loc, 2, 1, 0), 2},
		{at(loc, 2, 2, 0), 7.5},
		{at(loc, 2, 3, 0), 1},
	}, at(loc, 2, 0, 0), at(loc, 3, 0, 0))
	if got, ok := Max(spans); !ok || got != 7.5 {
		t.Fatalf("Max = %v, %v, want 7.5, true", got, ok)
	}
	if got, ok := Max(nil); ok || got != 0 {
		t.Fatalf("Max(nil) = %v, %v, want 0, false", got, ok)
	}
}

func TestFirstAt(t *testing.T) {
	loc := ny(t)
	spans := Held([]Point{
		{at(loc, 2, 8, 0), 3},
		{at(loc, 2, 14, 0), 0},
	}, at(loc, 2, 0, 0), at(loc, 3, 0, 0))
	positive := func(v float64) bool { return v > 0 }

	from := at(loc, 2, 10, 0)
	got, ok := FirstAt(spans, positive, from)
	if !ok || !got.Equal(from) {
		t.Fatalf("FirstAt = %v, %v, want %v, true", got, ok, from)
	}

	got, ok = FirstAt(spans, positive, at(loc, 2, 6, 0))
	if !ok || !got.Equal(at(loc, 2, 8, 0)) {
		t.Fatalf("FirstAt = %v, %v, want 08:00, true", got, ok)
	}

	// The span ends at 14:00, so 14:00 is not inside it.
	if got, ok = FirstAt(spans, positive, at(loc, 2, 14, 0)); ok {
		t.Fatalf("FirstAt = %v, true, want false", got)
	}
}

func TestLastIn(t *testing.T) {
	loc := ny(t)
	pts := []Point{
		{at(loc, 1, 23, 0), 1},
		{at(loc, 2, 5, 0), 2},
		{at(loc, 2, 9, 0), 3},
		{at(loc, 3, 0, 0), 4},
	}
	got, ok := LastIn(pts, at(loc, 2, 0, 0), at(loc, 3, 0, 0))
	if !ok || got.V != 3 || !got.T.Equal(at(loc, 2, 9, 0)) {
		t.Fatalf("LastIn = %+v, %v, want 3 at 09:00", got, ok)
	}
	if _, ok = LastIn(pts, at(loc, 2, 10, 0), at(loc, 2, 20, 0)); ok {
		t.Fatal("LastIn found a point in an empty window")
	}
}
