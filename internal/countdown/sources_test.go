package countdown

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/date"
	"github.com/evandhoffman/trmnl-pws/internal/holidays"
	"github.com/evandhoffman/trmnl-pws/internal/ical"
)

func d(y int, m time.Month, day int) date.Date {
	return date.Date{Year: y, Month: m, Day: day}
}

func parseFixture(t *testing.T, name string) []ical.Event {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	f, err := os.Open("../ical/testdata/" + name)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	evs, err := ical.Parse(f, loc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return evs
}

func TestSourceLabel(t *testing.T) {
	cases := []struct {
		s    Source
		want string
	}{
		{Trip, "Trip"},
		{School, "Binghamton"},
		{Holiday, "Holiday"},
		{Source(99), ""},
	}
	for _, c := range cases {
		if got := c.s.Label("Binghamton"); got != c.want {
			t.Errorf("Source(%d).Label = %q, want %q", c.s, got, c.want)
		}
	}
}

func TestSourceTieBreakOrder(t *testing.T) {
	if !(Trip < School && School < Holiday) {
		t.Fatalf("tie-break order wrong: Trip=%d School=%d Holiday=%d", Trip, School, Holiday)
	}
}

func TestSchoolEventsFromFixture(t *testing.T) {
	got := SchoolEvents(parseFixture(t, "school.ics"), DefaultSchoolRules)
	want := []Event{
		{School, "Thanksgiving break", d(2026, 11, 25), d(2026, 11, 29)},
		{School, "Last day of classes", d(2026, 12, 8), d(2026, 12, 8)},
		{School, "Halls close", d(2026, 12, 17), d(2026, 12, 17)},
		{School, "Classes begin", d(2027, 1, 19), d(2027, 1, 19)},
		{School, "Spring break", d(2027, 3, 6), d(2027, 3, 14)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	for _, e := range got {
		for _, bad := range []string{"winter session", "passover", "final examinations", "open"} {
			if strings.Contains(strings.ToLower(e.Name), bad) {
				t.Errorf("name %q contains %q", e.Name, bad)
			}
		}
	}
}

func allDay(summary string, start date.Date) ical.Event {
	return ical.Event{UID: summary, Summary: summary, Start: start, End: start, AllDay: true}
}

func TestSchoolHallsCloseMergesWithBreakRegardlessOfOrder(t *testing.T) {
	day := d(2027, 3, 6)
	for _, evs := range [][]ical.Event{
		{allDay("No classes (Spring break)", day), allDay("Residence halls close at 10 a.m.", day)},
		{allDay("Residence halls close at 10 a.m.", day), allDay("No classes (Spring break)", day)},
	} {
		got := SchoolEvents(evs, DefaultSchoolRules)
		if len(got) != 1 || got[0].Name != "Spring break" {
			t.Errorf("got %+v, want one Spring break", got)
		}
	}
}

func TestSchoolHallsCloseNextToExcludedBreakStaysAlone(t *testing.T) {
	day := d(2026, 12, 17)
	got := SchoolEvents([]ical.Event{
		allDay("No classes (Winter Session break)", day),
		allDay("Residence halls close at 10 a.m.", day),
	}, DefaultSchoolRules)
	if len(got) != 1 || got[0].Name != "Halls close" {
		t.Errorf("got %+v, want one Halls close", got)
	}
}

func TestSchoolCommencementAndCaseInsensitive(t *testing.T) {
	got := SchoolEvents([]ical.Event{
		allDay("Commencement ceremonies", d(2027, 5, 15)),
		allDay("CLASSES BEGIN", d(2027, 1, 19)),
	}, DefaultSchoolRules)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Name != "CLASSES BEGIN" || got[1].Name != "Commencement" {
		t.Errorf("got %+v", got)
	}
}

func TestSchoolExcludeBeatsInclude(t *testing.T) {
	r := SchoolRules{Include: []string{`^Classes`}, Exclude: []string{`resume`}}
	got := SchoolEvents([]ical.Event{
		allDay("Classes begin", d(2027, 1, 19)),
		allDay("Classes resume", d(2027, 1, 20)),
	}, r)
	if len(got) != 1 || got[0].Name != "Classes begin" {
		t.Errorf("got %+v", got)
	}
}

func TestSchoolEmptyInput(t *testing.T) {
	if got := SchoolEvents(nil, DefaultSchoolRules); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestSchoolRulesValidate(t *testing.T) {
	if err := DefaultSchoolRules.Validate(); err != nil {
		t.Errorf("default rules: %v", err)
	}
	if err := (SchoolRules{Include: []string{"("}}).Validate(); err == nil {
		t.Error("want error for bad include pattern")
	}
	if err := (SchoolRules{Exclude: []string{"["}}).Validate(); err == nil {
		t.Error("want error for bad exclude pattern")
	}
}

func TestTripEventsKeepOnlyTrips(t *testing.T) {
	got := TripEvents(parseFixture(t, "tripit.ics"))
	want := []Event{
		{Trip, "Denver", d(2026, 11, 14), d(2026, 11, 18)},
		{Trip, "Trip", d(2027, 2, 12), d(2027, 2, 15)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestTripNoDestinationSentinelIsTrip(t *testing.T) {
	for _, loc := range []string{"No destination specified", "", "  ", ", CO"} {
		got := TripEvents([]ical.Event{{
			UID: "x@tripit.com", Summary: "Your trip, February 2027", Location: loc,
			Description: "Secret hotel", Start: d(2027, 2, 12), End: d(2027, 2, 15), AllDay: true,
		}})
		if len(got) != 1 || got[0].Name != "Trip" {
			t.Errorf("location %q: got %+v, want Name Trip", loc, got)
		}
	}
}

func TestTripNameHasNoBookingDetails(t *testing.T) {
	got := TripEvents([]ical.Event{{
		UID:         "55555555@tripit.com",
		Summary:     "Conference 2027 with Example Airline",
		Location:    "Chicago, IL, United States",
		Description: "Confirmation: ABC123\n[Lodging] Example Hotel",
		Start:       d(2027, 3, 1), End: d(2027, 3, 4), AllDay: true,
	}})
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	n := got[0].Name
	if n != "Chicago" {
		t.Errorf("Name = %q, want Chicago", n)
	}
	for _, bad := range []string{"ABC123", "Hotel", "Airline", "Conference", "2027", "IL"} {
		if strings.Contains(n, bad) {
			t.Errorf("Name %q contains %q", n, bad)
		}
	}
}

func TestTripRequiresAllDayAndNonItemUID(t *testing.T) {
	base := ical.Event{UID: "a@tripit.com", Location: "Denver, CO", Start: d(2026, 11, 14), End: d(2026, 11, 18), AllDay: true}
	timed := base
	timed.AllDay = false
	item := base
	item.UID = "item-a@tripit.com"
	if got := TripEvents([]ical.Event{timed, item}); len(got) != 0 {
		t.Errorf("got %+v, want none", got)
	}
	if got := TripEvents([]ical.Event{base}); len(got) != 1 {
		t.Errorf("got %+v, want one", got)
	}
}

func TestHolidayEvents(t *testing.T) {
	got := HolidayEvents([]holidays.Holiday{
		{Key: "xmas", Name: "Christmas", Date: d(2026, 12, 25)},
	})
	want := []Event{{Holiday, "Christmas", d(2026, 12, 25), d(2026, 12, 25)}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v want %+v", got, want)
	}
	if got := HolidayEvents(nil); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}
