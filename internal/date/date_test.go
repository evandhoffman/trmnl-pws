package date

import (
	"testing"
	"time"
)

func d(y int, m time.Month, day int) Date { return Date{Year: y, Month: m, Day: day} }

func TestDaysUntilAcrossDSTEnd(t *testing.T) { // 2026-11-01 is a DST change day
	if got := d(2026, 11, 1).DaysUntil(d(2026, 11, 26)); got != 25 {
		t.Fatal(got)
	}
}

func TestDaysUntilAcrossDSTStart(t *testing.T) {
	if got := d(2027, 3, 13).DaysUntil(d(2027, 3, 15)); got != 2 {
		t.Fatal(got)
	}
}

func TestDaysUntilLeapDay(t *testing.T) {
	if got := d(2028, 2, 28).DaysUntil(d(2028, 3, 1)); got != 2 {
		t.Fatal(got)
	}
}

func TestDaysUntilSameAndNegative(t *testing.T) {
	if got := d(2026, 11, 2).DaysUntil(d(2026, 11, 2)); got != 0 {
		t.Fatal(got)
	}
	if got := d(2026, 11, 26).DaysUntil(d(2026, 11, 1)); got != -25 {
		t.Fatal(got)
	}
}

func TestDaysUntilAcrossYear(t *testing.T) {
	if got := d(2026, 12, 31).DaysUntil(d(2027, 1, 1)); got != 1 {
		t.Fatal(got)
	}
	if got := d(2026, 1, 1).DaysUntil(d(2027, 1, 1)); got != 365 {
		t.Fatal(got)
	}
}

func TestOfUsesLocalDate(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	utc := time.Date(2026, 11, 26, 4, 30, 0, 0, time.UTC) // 23:30 EST on Nov 25
	if got := Of(utc, ny); got != d(2026, 11, 25) {
		t.Fatal(got)
	}
}

func TestOfAfterMidnightLocal(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// 05:00 UTC on Nov 1 is 01:00 EDT, still Nov 1 in New York.
	utc := time.Date(2026, 11, 1, 5, 0, 0, 0, time.UTC)
	if got := Of(utc, ny); got != d(2026, 11, 1) {
		t.Fatal(got)
	}
}

func TestAddDays(t *testing.T) {
	cases := []struct {
		from Date
		n    int
		want Date
	}{
		{d(2026, 11, 1), 25, d(2026, 11, 26)},
		{d(2026, 12, 31), 1, d(2027, 1, 1)},
		{d(2027, 1, 1), -1, d(2026, 12, 31)},
		{d(2028, 2, 28), 1, d(2028, 2, 29)},
		{d(2027, 2, 28), 1, d(2027, 3, 1)},
		{d(2026, 11, 2), 0, d(2026, 11, 2)},
	}
	for _, c := range cases {
		if got := c.from.AddDays(c.n); got != c.want {
			t.Errorf("%v.AddDays(%d) = %v, want %v", c.from, c.n, got, c.want)
		}
	}
}

func TestBefore(t *testing.T) {
	if !d(2026, 11, 1).Before(d(2026, 11, 2)) {
		t.Error("Nov 1 should be before Nov 2")
	}
	if !d(2026, 12, 31).Before(d(2027, 1, 1)) {
		t.Error("Dec 31 should be before Jan 1")
	}
	if d(2026, 11, 2).Before(d(2026, 11, 2)) {
		t.Error("a date is not before itself")
	}
	if d(2026, 11, 2).Before(d(2026, 11, 1)) {
		t.Error("Nov 2 is not before Nov 1")
	}
}

func TestWeekday(t *testing.T) {
	if got := d(2026, 11, 26).Weekday(); got != time.Thursday {
		t.Fatal(got)
	}
	if got := d(2026, 11, 14).Weekday(); got != time.Saturday {
		t.Fatal(got)
	}
}

func TestLabel(t *testing.T) {
	if got := d(2026, 11, 14).Label(); got != "Sat Nov 14" {
		t.Fatal(got)
	}
	if got := d(2027, 3, 5).Label(); got != "Fri Mar 5" {
		t.Fatal(got) // day has no leading zero
	}
}

func TestString(t *testing.T) {
	if got := d(2026, 11, 14).String(); got != "2026-11-14" {
		t.Fatal(got)
	}
	if got := d(2027, 3, 5).String(); got != "2027-03-05" {
		t.Fatal(got)
	}
}
