// Package holidays computes the dates of the holidays the countdown screen shows.
//
// The NYSE closure rules pick the set (Good Friday is in, Halloween is an
// extra). Each date is the holiday's own date, never the NYSE observed
// weekday: Independence Day 2026 is Saturday, July 4.
package holidays

import (
	"fmt"
	"sort"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/date"
)

// Holiday is one holiday in one year.
type Holiday struct {
	Key  string
	Name string
	Date date.Date
}

// DefaultKeys lists every supported holiday.
var DefaultKeys = []string{
	"new_years", "mlk", "presidents", "good_friday", "memorial", "juneteenth",
	"independence", "labor", "thanksgiving", "christmas", "halloween",
}

type rule struct {
	name string
	on   func(year int) date.Date
}

var rules = map[string]rule{
	"new_years":    {"New Year's Day", fixed(time.January, 1)},
	"mlk":          {"MLK Day", nthWeekday(time.January, time.Monday, 3)},
	"presidents":   {"Presidents' Day", nthWeekday(time.February, time.Monday, 3)},
	"good_friday":  {"Good Friday", func(y int) date.Date { return Easter(y).AddDays(-2) }},
	"memorial":     {"Memorial Day", lastWeekday(time.May, time.Monday)},
	"juneteenth":   {"Juneteenth", fixed(time.June, 19)},
	"independence": {"Independence Day", fixed(time.July, 4)},
	"labor":        {"Labor Day", nthWeekday(time.September, time.Monday, 1)},
	"thanksgiving": {"Thanksgiving", nthWeekday(time.November, time.Thursday, 4)},
	"christmas":    {"Christmas", fixed(time.December, 25)},
	"halloween":    {"Halloween", fixed(time.October, 31)},
}

func fixed(m time.Month, day int) func(int) date.Date {
	return func(y int) date.Date { return date.Date{Year: y, Month: m, Day: day} }
}

// nthWeekday returns a rule for the nth given weekday of a month (n from 1).
func nthWeekday(m time.Month, wd time.Weekday, n int) func(int) date.Date {
	return func(y int) date.Date {
		first := date.Date{Year: y, Month: m, Day: 1}
		offset := (int(wd) - int(first.Weekday()) + 7) % 7
		return first.AddDays(offset + 7*(n-1))
	}
}

// lastWeekday returns a rule for the last given weekday of a month.
func lastWeekday(m time.Month, wd time.Weekday) func(int) date.Date {
	return func(y int) date.Date {
		// Day 0 of the next month is the last day of this month.
		t := time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC)
		last := date.Date{Year: y, Month: m, Day: t.Day()}
		return last.AddDays(-((int(last.Weekday()) - int(wd) + 7) % 7))
	}
}

// Easter returns the date of Easter Sunday in the Gregorian calendar.
// It uses the anonymous Gregorian algorithm (Meeus, Jones, Butcher).
func Easter(year int) date.Date {
	a := year % 19
	b := year / 100
	c := year % 100
	d := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i := c / 4
	k := c % 4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return date.Date{Year: year, Month: time.Month(month), Day: day}
}

// Validate returns an error that names the first unknown key, or nil.
func Validate(keys []string) error {
	for _, k := range keys {
		if _, ok := rules[k]; !ok {
			return fmt.Errorf("unknown holiday key %q", k)
		}
	}
	return nil
}

// ForYear returns the holidays for the keys in one year, sorted by date.
// A repeated key counts once. An unknown key panics: call Validate first.
func ForYear(year int, keys []string) []Holiday {
	if err := Validate(keys); err != nil {
		panic(err)
	}
	seen := make(map[string]bool, len(keys))
	out := make([]Holiday, 0, len(keys))
	for _, k := range keys {
		if seen[k] {
			continue
		}
		seen[k] = true
		r := rules[k]
		out = append(out, Holiday{Key: k, Name: r.name, Date: r.on(year)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}

// Upcoming returns the next n holidays on or after from, crossing years.
// It returns fewer than n only when n is not positive or keys is empty.
func Upcoming(from date.Date, keys []string, n int) []Holiday {
	if n <= 0 || len(keys) == 0 {
		return nil
	}
	out := make([]Holiday, 0, n)
	for year := from.Year; len(out) < n; year++ {
		for _, h := range ForYear(year, keys) {
			if h.Date.Before(from) {
				continue
			}
			out = append(out, h)
			if len(out) == n {
				break
			}
		}
	}
	return out
}
