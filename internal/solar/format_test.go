package solar

import (
	"testing"
	"time"
)

func TestFormat(t *testing.T) {
	loc := ny(t)
	tm := at(loc, 10, 30, 13, 5)
	cases := []struct{ name, got, want string }{
		{"compact pm", compactTime(tm), "1:05p"},
		{"compact am", compactTime(at(loc, 10, 30, 0, 7)), "12:07a"},
		{"long", longTime(at(loc, 10, 30, 12, 50)), "12:50 PM"},
		{"clock", clockTime(at(loc, 10, 30, 7, 21)), "7:21"},
		{"clock noon", clockTime(at(loc, 10, 30, 12, 1)), "12:01"},
		{"day label", dayLabel(tm), "Fri 10/30"},
		{"long date", longDate(tm), "Friday, October 30"},
		{"one decimal", f1(11), "11.0"},
		{"negative zero", f1(-0.04), "0.0"},
		{"whole", f0(53.6), "54"},
		{"whole negative zero", f0(-0.2), "0"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestFormatUsesLocation(t *testing.T) {
	loc := ny(t)
	utc := time.Date(2026, 10, 30, 17, 5, 0, 0, time.UTC)
	if got := compactTime(utc.In(loc)); got != "1:05p" {
		t.Fatalf("compactTime = %q, want 1:05p", got)
	}
}
