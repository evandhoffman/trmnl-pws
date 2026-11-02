// Package date provides a calendar date with no time of day and no zone.
//
// Day counts use calendar dates, never 24-hour blocks, so a daylight saving
// change never shifts a count.
package date

import "time"

// Date is a calendar date.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// Of returns the calendar date of t in loc.
func Of(t time.Time, loc *time.Location) Date {
	y, m, day := t.In(loc).Date()
	return Date{Year: y, Month: m, Day: day}
}

// utc returns midnight UTC on d. UTC has no daylight saving time.
func (d Date) utc() time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

// DaysUntil returns the number of calendar days from d to o.
// The result is negative when o is before d.
func (d Date) DaysUntil(o Date) int {
	return int(o.utc().Sub(d.utc()) / (24 * time.Hour))
}

// AddDays returns the date n days after d. A negative n goes back.
func (d Date) AddDays(n int) Date {
	y, m, day := d.utc().AddDate(0, 0, n).Date()
	return Date{Year: y, Month: m, Day: day}
}

// Before reports whether d is earlier than o.
func (d Date) Before(o Date) bool {
	return d.utc().Before(o.utc())
}

// Weekday returns the day of the week of d.
func (d Date) Weekday() time.Weekday {
	return d.utc().Weekday()
}

// Label returns a short label such as "Sat Nov 14".
func (d Date) Label() string {
	return d.utc().Format("Mon Jan 2")
}

// String returns the date as "2026-11-14".
func (d Date) String() string {
	return d.utc().Format("2006-01-02")
}
