// Package ical turns an iCalendar feed into events with calendar dates.
package ical

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	golangical "github.com/arran4/golang-ical"

	"github.com/evandhoffman/trmnl-pws/internal/date"
)

// Event is one calendar event. Start and End are calendar dates in the
// location given to Parse. End is the last day the event covers, inclusive:
// an all-day DTEND is exclusive in iCal, so Parse subtracts one day from it.
type Event struct {
	UID         string
	Summary     string
	Location    string
	Description string
	Start       date.Date
	End         date.Date
	AllDay      bool
}

// Parse reads an iCalendar feed. Every date and time resolves to a calendar
// date in loc. A time with no zone (no TZID, no Z) is in loc.
//
// Parse returns an error when the body does not begin with BEGIN:VCALENDAR
// and end with END:VCALENDAR, so an empty or cut feed never looks valid.
//
// Parse skips an event with no DTSTART or with a time it cannot read. For an
// event with an RRULE it keeps only the first occurrence and logs a warning.
func Parse(r io.Reader, loc *time.Location) ([]Event, error) {
	// The feeds are small, so read the whole body. The library does not
	// check that the feed reaches END:VCALENDAR, so a cut body would parse
	// as a shorter feed.
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read calendar: %w", err)
	}
	trimmed := bytes.TrimSpace(body)
	if !bytes.HasPrefix(trimmed, []byte("BEGIN:VCALENDAR")) ||
		!bytes.HasSuffix(trimmed, []byte("END:VCALENDAR")) {
		return nil, errors.New("parse calendar: body is not a complete VCALENDAR")
	}
	cal, err := golangical.ParseCalendar(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse calendar: %w", err)
	}
	var events []Event
	for _, ve := range cal.Events() {
		ev, ok := convert(ve, loc)
		if ok {
			events = append(events, ev)
		}
	}
	return events, nil
}

func convert(ve *golangical.VEvent, loc *time.Location) (Event, bool) {
	uid := text(ve, golangical.ComponentPropertyUniqueId)
	startProp := ve.GetProperty(golangical.ComponentPropertyDtStart)
	if startProp == nil {
		return Event{}, false
	}
	if ve.GetProperty(golangical.ComponentPropertyRrule) != nil {
		slog.Warn("event has an RRULE; using only its first occurrence", "uid", uid)
	}

	start, allDay, err := dateOf(startProp, loc)
	if err != nil {
		slog.Warn("skip event with unreadable DTSTART", "uid", uid, "err", err)
		return Event{}, false
	}
	end := start
	if endProp := ve.GetProperty(golangical.ComponentPropertyDtEnd); endProp != nil {
		e, endAllDay, err := dateOf(endProp, loc)
		if err != nil {
			slog.Warn("skip event with unreadable DTEND", "uid", uid, "err", err)
			return Event{}, false
		}
		end = e
		// An all-day DTEND is exclusive. A timed DTEND is the moment the
		// event ends, so its date is already the last day.
		if endAllDay {
			end = end.AddDays(-1)
		}
	}
	if end.Before(start) {
		end = start
	}

	return Event{
		UID:         uid,
		Summary:     text(ve, golangical.ComponentPropertySummary),
		Location:    text(ve, golangical.ComponentPropertyLocation),
		Description: text(ve, golangical.ComponentPropertyDescription),
		Start:       start,
		End:         end,
		AllDay:      allDay,
	}, true
}

func text(ve *golangical.VEvent, p golangical.ComponentProperty) string {
	prop := ve.GetProperty(p)
	if prop == nil {
		return ""
	}
	return prop.Value
}

// dateOf returns the calendar date of a DTSTART or DTEND property in loc.
// allDay is true for a DATE value.
func dateOf(p *golangical.IANAProperty, loc *time.Location) (d date.Date, allDay bool, err error) {
	v := p.Value
	if isDateOnly(p, v) {
		t, err := time.Parse("20060102", v)
		if err != nil {
			return date.Date{}, false, fmt.Errorf("read date %q: %w", v, err)
		}
		return date.Of(t, time.UTC), true, nil
	}

	if strings.HasSuffix(v, "Z") {
		t, err := time.Parse("20060102T150405Z", v)
		if err != nil {
			return date.Date{}, false, fmt.Errorf("read UTC time %q: %w", v, err)
		}
		return date.Of(t, loc), false, nil
	}

	zone := loc
	if tzid := p.ICalParameters["TZID"]; len(tzid) == 1 {
		zone, err = time.LoadLocation(tzid[0])
		if err != nil {
			return date.Date{}, false, fmt.Errorf("load TZID %q: %w", tzid[0], err)
		}
	}
	t, err := time.ParseInLocation("20060102T150405", v, zone)
	if err != nil {
		return date.Date{}, false, fmt.Errorf("read time %q: %w", v, err)
	}
	return date.Of(t, loc), false, nil
}

// isDateOnly reports whether the property holds a DATE value (no time).
func isDateOnly(p *golangical.IANAProperty, v string) bool {
	if vt := p.ICalParameters["VALUE"]; len(vt) == 1 && vt[0] == "DATE" {
		return true
	}
	return len(v) == 8 && !strings.ContainsRune(v, 'T')
}
