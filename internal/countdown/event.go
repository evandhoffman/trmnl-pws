// Package countdown turns calendar events and holidays into countdown events.
package countdown

import (
	"github.com/evandhoffman/trmnl-pws/internal/date"
	"github.com/evandhoffman/trmnl-pws/internal/holidays"
)

// Source says where a countdown event comes from. The order of the values is
// the tie-break order when two events start on the same day.
type Source int

// The three event sources, in tie-break order.
const (
	Trip Source = iota
	School
	Holiday
)

// Label returns the short column label for the source. The school label comes
// from the settings, for example "Binghamton".
func (s Source) Label(schoolLabel string) string {
	switch s {
	case Trip:
		return "Trip"
	case School:
		return schoolLabel
	case Holiday:
		return "Holiday"
	}
	return ""
}

// Event is one countdown target. End is the last day, inclusive.
type Event struct {
	Source Source
	Name   string
	Start  date.Date
	End    date.Date
}

// HolidayEvents turns holidays into one-day countdown events.
func HolidayEvents(hs []holidays.Holiday) []Event {
	out := make([]Event, 0, len(hs))
	for _, h := range hs {
		out = append(out, Event{Holiday, h.Name, h.Date, h.Date})
	}
	return out
}
