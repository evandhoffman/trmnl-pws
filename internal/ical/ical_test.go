package ical

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/date"
)

func newYork(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return loc
}

func parseFile(t *testing.T, path string) []Event {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	events, err := Parse(f, newYork(t))
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return events
}

func parseInline(t *testing.T, vevents string) []Event {
	t.Helper()
	src := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
		strings.ReplaceAll(vevents, "\n", "\r\n") + "\r\nEND:VCALENDAR\r\n"
	events, err := Parse(strings.NewReader(src), newYork(t))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return events
}

func find(t *testing.T, events []Event, summary string) Event {
	t.Helper()
	for _, e := range events {
		if e.Summary == summary {
			return e
		}
	}
	t.Fatalf("no event with summary %q", summary)
	return Event{}
}

func d(y int, m time.Month, day int) date.Date {
	return date.Date{Year: y, Month: m, Day: day}
}

func TestParseSchoolAllDay(t *testing.T) {
	events := parseFile(t, "testdata/school.ics")
	if len(events) != 17 {
		t.Fatalf("got %d events, want 17", len(events))
	}
	e := find(t, events, "No classes (Thanksgiving break)")
	if e.Start != d(2026, 11, 25) || e.End != d(2026, 11, 29) || !e.AllDay {
		t.Errorf("got %+v, want 2026-11-25 to 2026-11-29 all-day", e)
	}
	if e.UID == "" {
		t.Error("UID is empty")
	}
	// A one-day event has End == Start.
	e = find(t, events, "Last day of classes")
	if e.Start != d(2026, 12, 8) || e.End != d(2026, 12, 8) {
		t.Errorf("got %v to %v, want 2026-12-08 both", e.Start, e.End)
	}
	e = find(t, events, "No classes (Spring break)")
	if e.Start != d(2027, 3, 6) || e.End != d(2027, 3, 14) {
		t.Errorf("got %v to %v, want 2027-03-06 to 2027-03-14", e.Start, e.End)
	}
}

func TestParseTripItTripLevelEvents(t *testing.T) {
	events := parseFile(t, "testdata/tripit.ics")
	if len(events) != 4 {
		t.Fatalf("got %d events, want 4", len(events))
	}
	trip := find(t, events, "Denver, CO, November 2026")
	if trip.Start != d(2026, 11, 14) || trip.End != d(2026, 11, 18) || !trip.AllDay {
		t.Errorf("got %+v, want 2026-11-14 to 2026-11-18 all-day", trip)
	}
	if trip.Location != "Denver, CO, United States" {
		t.Errorf("location = %q", trip.Location)
	}
	if strings.HasPrefix(trip.UID, "item-") {
		t.Errorf("trip UID has item- prefix: %q", trip.UID)
	}
	// The DESCRIPTION line is folded in the file; Parse unfolds it.
	want := "Example Traveler is in Denver, CO, United States from Nov 14 to 18, 2026\nExample trip description"
	if trip.Description != want {
		t.Errorf("description = %q, want %q", trip.Description, want)
	}

	none := find(t, events, "Your trip, February 2027")
	if none.Location != "No destination specified" {
		t.Errorf("location = %q", none.Location)
	}
	if none.Start != d(2027, 2, 12) || none.End != d(2027, 2, 15) {
		t.Errorf("got %v to %v, want 2027-02-12 to 2027-02-15", none.Start, none.End)
	}
}

func TestParseTripItItemEvents(t *testing.T) {
	events := parseFile(t, "testdata/tripit.ics")

	// Lodging: UTC time. 20:00Z on Nov 14 is 15:00 EST, same day.
	hotel := find(t, events, "Check-in: Example Hotel")
	if hotel.AllDay || hotel.Start != d(2026, 11, 14) || hotel.End != d(2026, 11, 14) {
		t.Errorf("hotel = %+v", hotel)
	}
	if !strings.HasPrefix(hotel.UID, "item-") {
		t.Errorf("item UID = %q, want item- prefix", hotel.UID)
	}
	if !strings.Contains(hotel.Description, "[Lodging]") {
		t.Errorf("description = %q, want [Lodging] tag", hotel.Description)
	}

	// Car: floating time, read in loc.
	car := find(t, events, "Pick Up Rental Car: Example Rentals")
	if car.AllDay || car.Start != d(2026, 11, 14) || car.Location != "" {
		t.Errorf("car = %+v", car)
	}
	if !strings.Contains(car.Description, "[Car Rental]") {
		t.Errorf("description = %q, want [Car Rental] tag", car.Description)
	}
}

func TestParseTimedEventUsesLocalDate(t *testing.T) {
	// 03:30Z on Nov 15 is 22:30 EST on Nov 14.
	events := parseInline(t, `BEGIN:VEVENT
UID:a
DTSTART:20261115T033000Z
DTEND:20261115T043000Z
SUMMARY:Late flight
END:VEVENT`)
	if len(events) != 1 {
		t.Fatalf("got %d events", len(events))
	}
	e := events[0]
	if e.Start != d(2026, 11, 14) || e.End != d(2026, 11, 14) || e.AllDay {
		t.Errorf("got %+v, want 2026-11-14 timed", e)
	}
}

func TestParseTZIDEvent(t *testing.T) {
	// 22:00 PST on Nov 14 is 01:00 EST on Nov 15.
	events := parseInline(t, `BEGIN:VEVENT
UID:a
DTSTART;TZID=America/Los_Angeles:20261114T220000
DTEND;TZID=America/Los_Angeles:20261114T230000
SUMMARY:Pacific event
END:VEVENT`)
	if len(events) != 1 {
		t.Fatalf("got %d events", len(events))
	}
	if got := events[0].Start; got != d(2026, 11, 15) {
		t.Errorf("start = %v, want 2026-11-15", got)
	}
	// 23:00 PST on Nov 14 is 02:00 EST on Nov 15.
	if got := events[0].End; got != d(2026, 11, 15) {
		t.Errorf("end = %v, want 2026-11-15", got)
	}
}

func TestParseFloatingTimeUsesLoc(t *testing.T) {
	// No TZID and no Z: the time is in loc, so the date does not move.
	events := parseInline(t, `BEGIN:VEVENT
UID:a
DTSTART:20261114T233000
DTEND:20261115T003000
SUMMARY:Floating
END:VEVENT`)
	if len(events) != 1 {
		t.Fatalf("got %d events", len(events))
	}
	e := events[0]
	if e.Start != d(2026, 11, 14) || e.End != d(2026, 11, 15) {
		t.Errorf("got %v to %v, want 2026-11-14 to 2026-11-15", e.Start, e.End)
	}
}

func TestParseAllDayWithoutEnd(t *testing.T) {
	events := parseInline(t, `BEGIN:VEVENT
UID:a
DTSTART;VALUE=DATE:20261114
SUMMARY:No end
END:VEVENT`)
	if len(events) != 1 {
		t.Fatalf("got %d events", len(events))
	}
	if e := events[0]; e.Start != d(2026, 11, 14) || e.End != d(2026, 11, 14) || !e.AllDay {
		t.Errorf("got %+v, want one all-day on 2026-11-14", e)
	}
}

func TestParseUnescapesText(t *testing.T) {
	events := parseInline(t, `BEGIN:VEVENT
UID:a
DTSTART;VALUE=DATE:20261114
DTEND;VALUE=DATE:20261115
SUMMARY:Residence halls close at 10 a.m.\, east
LOCATION:A\; B
DESCRIPTION:line one\nline two\\end
END:VEVENT`)
	if len(events) != 1 {
		t.Fatalf("got %d events", len(events))
	}
	e := events[0]
	if want := "Residence halls close at 10 a.m., east"; e.Summary != want {
		t.Errorf("summary = %q, want %q", e.Summary, want)
	}
	if want := "A; B"; e.Location != want {
		t.Errorf("location = %q, want %q", e.Location, want)
	}
	if want := "line one\nline two\\end"; e.Description != want {
		t.Errorf("description = %q, want %q", e.Description, want)
	}
}

func TestParseSkipsEventWithoutDTSTART(t *testing.T) {
	events := parseInline(t, `BEGIN:VEVENT
UID:a
SUMMARY:No start
END:VEVENT
BEGIN:VEVENT
UID:b
DTSTART;VALUE=DATE:20261114
DTEND;VALUE=DATE:20261115
SUMMARY:Has start
END:VEVENT`)
	if len(events) != 1 || events[0].UID != "b" {
		t.Fatalf("got %+v, want only event b", events)
	}
}

func TestParseRRuleUsesFirstOccurrenceAndWarns(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	events := parseInline(t, `BEGIN:VEVENT
UID:weekly-1
DTSTART;VALUE=DATE:20261114
DTEND;VALUE=DATE:20261115
RRULE:FREQ=WEEKLY;COUNT=10
SUMMARY:Weekly
END:VEVENT`)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (first occurrence only)", len(events))
	}
	if events[0].Start != d(2026, 11, 14) {
		t.Errorf("start = %v", events[0].Start)
	}
	log := buf.String()
	if !strings.Contains(log, `"level":"WARN"`) || !strings.Contains(log, "weekly-1") {
		t.Errorf("log = %q, want a WARN naming the UID", log)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	_, err := Parse(strings.NewReader("<html><body>Not found</body></html>"), newYork(t))
	if err == nil {
		t.Fatal("want an error for HTML input, got nil")
	}
}

func TestParseEmptyCalendar(t *testing.T) {
	events, err := Parse(strings.NewReader("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nEND:VCALENDAR\r\n"), newYork(t))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(events) != 0 {
		t.Errorf("got %d events, want 0", len(events))
	}
}

func TestParseSkipsEventWithUnknownTZID(t *testing.T) {
	events := parseInline(t, `BEGIN:VEVENT
UID:a
DTSTART;TZID=Not/AZone:20261114T220000
SUMMARY:Bad zone
END:VEVENT
BEGIN:VEVENT
UID:b
DTSTART;VALUE=DATE:20261114
SUMMARY:Good
END:VEVENT`)
	if len(events) != 1 || events[0].UID != "b" {
		t.Fatalf("got %+v, want only event b", events)
	}
}

func TestParseRejectsEmptyInput(t *testing.T) {
	for _, in := range []string{"", "  \r\n\r\n"} {
		if _, err := Parse(strings.NewReader(in), newYork(t)); err == nil {
			t.Errorf("Parse(%q): want an error, got nil", in)
		}
	}
}

func TestParseRejectsTruncatedFeed(t *testing.T) {
	// The body stops at an event boundary: every line is valid, but
	// END:VCALENDAR never arrives.
	src := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:a\r\nDTSTART;VALUE=DATE:20261114\r\n" +
		"DTEND;VALUE=DATE:20261115\r\nSUMMARY:Cut\r\nEND:VEVENT\r\n"
	events, err := Parse(strings.NewReader(src), newYork(t))
	if err == nil {
		t.Fatalf("want an error for a feed with no END:VCALENDAR, got %+v", events)
	}
}
