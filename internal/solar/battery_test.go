package solar

import (
	"testing"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/series"
)

func eq(got *time.Time, want time.Time) bool { return got != nil && got.Equal(want) }

func tstr(p *time.Time) string {
	if p == nil {
		return "<nil>"
	}
	return p.String()
}

// day returns the spans for 2026-10-30 from local midnight to the next.
func day(loc *time.Location, pts []series.Point) ([]series.Span, time.Time) {
	end := at(loc, 10, 31, 0, 0)
	return series.Held(pts, at(loc, 10, 30, 0, 0), end), end
}

func TestBatteryEvents(t *testing.T) {
	loc := ny(t)
	// Review Focus 4: 100 at 13:00, a dip to 98 at 14:00, back at 100 at
	// 14:30, and off 100 for good at 17:10.
	charge, end := day(loc, []series.Point{
		pt(at(loc, 10, 30, 6, 0), 40),
		pt(at(loc, 10, 30, 13, 0), 100),
		pt(at(loc, 10, 30, 14, 0), 98),
		pt(at(loc, 10, 30, 14, 30), 100),
		pt(at(loc, 10, 30, 17, 10), 90),
		pt(at(loc, 10, 30, 21, 50), 40),
		pt(at(loc, 10, 30, 23, 0), 30),
	})
	reserve, _ := day(loc, []series.Point{pt(at(loc, 10, 20, 0, 0), 40)})
	got := Battery(charge, reserve, end)
	if got.Max != 100 {
		t.Errorf("Max = %v, want 100", got.Max)
	}
	if !eq(got.Full, at(loc, 10, 30, 13, 0)) {
		t.Errorf("Full = %s, want 13:00", tstr(got.Full))
	}
	if !eq(got.Drain, at(loc, 10, 30, 17, 10)) {
		t.Errorf("Drain = %s, want 17:10", tstr(got.Drain))
	}
	// Charge reaches the reserve of 40 at 21:50.
	if !eq(got.Reserve, at(loc, 10, 30, 21, 50)) {
		t.Errorf("Reserve = %s, want 21:50", tstr(got.Reserve))
	}
}

func TestBatteryStillAtMax(t *testing.T) {
	loc := ny(t)
	now := at(loc, 10, 30, 16, 0)
	charge := series.Held([]series.Point{
		pt(at(loc, 10, 30, 6, 0), 40),
		pt(at(loc, 10, 30, 13, 0), 100),
	}, at(loc, 10, 30, 0, 0), now)
	reserve := series.Held([]series.Point{pt(at(loc, 10, 20, 0, 0), 40)}, at(loc, 10, 30, 0, 0), now)
	got := Battery(charge, reserve, now)
	if !eq(got.Full, at(loc, 10, 30, 13, 0)) {
		t.Errorf("Full = %s, want 13:00", tstr(got.Full))
	}
	if got.Drain != nil || got.Reserve != nil {
		t.Errorf("Drain = %s, Reserve = %s, want both nil", tstr(got.Drain), tstr(got.Reserve))
	}
}

func TestBatteryReserveUsesDayMax(t *testing.T) {
	loc := ny(t)
	charge, end := day(loc, []series.Point{
		pt(at(loc, 10, 30, 0, 0), 100),
		pt(at(loc, 10, 30, 17, 10), 90),
		pt(at(loc, 10, 30, 21, 50), 40),
		pt(at(loc, 10, 30, 23, 30), 20),
	})
	// The reserve is 40, then 20. The day uses the larger value.
	reserve, _ := day(loc, []series.Point{
		pt(at(loc, 10, 20, 0, 0), 40),
		pt(at(loc, 10, 30, 12, 0), 20),
	})
	got := Battery(charge, reserve, end)
	if !eq(got.Reserve, at(loc, 10, 30, 21, 50)) {
		t.Fatalf("Reserve = %s, want 21:50", tstr(got.Reserve))
	}
}

func TestBatteryNeverReachesReserve(t *testing.T) {
	loc := ny(t)
	charge, end := day(loc, []series.Point{
		pt(at(loc, 10, 30, 0, 0), 100),
		pt(at(loc, 10, 30, 17, 10), 50),
	})
	reserve, _ := day(loc, []series.Point{pt(at(loc, 10, 20, 0, 0), 40)})
	got := Battery(charge, reserve, end)
	if !eq(got.Drain, at(loc, 10, 30, 17, 10)) {
		t.Errorf("Drain = %s, want 17:10", tstr(got.Drain))
	}
	if got.Reserve != nil {
		t.Errorf("Reserve = %s, want nil", tstr(got.Reserve))
	}
}

func TestBatteryAlreadyBelowReserveAtDrain(t *testing.T) {
	loc := ny(t)
	charge, end := day(loc, []series.Point{
		pt(at(loc, 10, 30, 0, 0), 100),
		pt(at(loc, 10, 30, 17, 10), 35),
	})
	reserve, _ := day(loc, []series.Point{pt(at(loc, 10, 20, 0, 0), 40)})
	got := Battery(charge, reserve, end)
	if !eq(got.Reserve, at(loc, 10, 30, 17, 10)) {
		t.Fatalf("Reserve = %s, want the drain time 17:10", tstr(got.Reserve))
	}
}

func TestBatteryNoReserveData(t *testing.T) {
	loc := ny(t)
	charge, end := day(loc, []series.Point{
		pt(at(loc, 10, 30, 0, 0), 100),
		pt(at(loc, 10, 30, 17, 10), 5),
	})
	got := Battery(charge, nil, end)
	if got.Drain == nil || got.Reserve != nil {
		t.Fatalf("Drain = %s, Reserve = %s, want a drain and no reserve", tstr(got.Drain), tstr(got.Reserve))
	}
}

func TestBatteryNoChargeData(t *testing.T) {
	loc := ny(t)
	got := Battery(nil, nil, at(loc, 10, 31, 0, 0))
	if got.Full != nil || got.Drain != nil || got.Reserve != nil || got.Max != 0 {
		t.Fatalf("got %+v, want the zero value", got)
	}
}
