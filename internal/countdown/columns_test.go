package countdown

import (
	"reflect"
	"testing"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/date"
	"github.com/evandhoffman/trmnl-pws/internal/holidays"
)

const testSchoolLabel = "Binghamton"

type scenario struct {
	trips, school, hols []Event
}

// fixtureScenario builds the three sources for today from the real fixtures.
func fixtureScenario(t *testing.T, today date.Date, withTrips, withSchool bool) scenario {
	t.Helper()
	var s scenario
	if withTrips {
		s.trips = TripEvents(parseFixture(t, "tripit.ics"))
	}
	if withSchool {
		s.school = SchoolEvents(parseFixture(t, "school.ics"), DefaultSchoolRules)
	}
	s.hols = HolidayEvents(holidays.Upcoming(today, holidays.DefaultKeys, 3))
	return s
}

func (s scenario) columns(today date.Date) []Column {
	return Columns(today, s.trips, s.school, s.hols, testSchoolLabel, 3)
}

func TestMockupScenario(t *testing.T) {
	today := d(2026, 11, 1)
	got := fixtureScenario(t, today, true, true).columns(today)
	want := []Column{
		{Source: "Trip", Name: "Denver", Days: 13, State: StateUpcoming, DateLabel: "Sat Nov 14"},
		{Source: "Binghamton", Name: "Thanksgiving break", Days: 24, State: StateUpcoming, DateLabel: "Wed Nov 25"},
		{Source: "Holiday", Name: "Thanksgiving", Days: 25, State: StateUpcoming, DateLabel: "Thu Nov 26"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestNoTripBackfills(t *testing.T) {
	today := d(2026, 11, 1)
	got := fixtureScenario(t, today, false, true).columns(today)
	want := []Column{
		{Source: "Binghamton", Name: "Thanksgiving break", Days: 24, State: StateUpcoming, DateLabel: "Wed Nov 25"},
		{Source: "Holiday", Name: "Thanksgiving", Days: 25, State: StateUpcoming, DateLabel: "Thu Nov 26"},
		{Source: "Binghamton", Name: "Last day of classes", Days: 37, State: StateUpcoming, DateLabel: "Tue Dec 8"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestOnlyHolidays(t *testing.T) {
	today := d(2026, 11, 1)
	got := fixtureScenario(t, today, false, false).columns(today)
	want := []Column{
		{Source: "Holiday", Name: "Thanksgiving", Days: 25, State: StateUpcoming, DateLabel: "Thu Nov 26"},
		{Source: "Holiday", Name: "Christmas", Days: 54, State: StateUpcoming, DateLabel: "Fri Dec 25"},
		{Source: "Holiday", Name: "New Year's Day", Days: 61, State: StateUpcoming, DateLabel: "Fri Jan 1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestTodayState(t *testing.T) {
	today := d(2026, 11, 26)
	got := fixtureScenario(t, today, false, false).columns(today)
	if len(got) == 0 {
		t.Fatal("no columns")
	}
	want := Column{Source: "Holiday", Name: "Thanksgiving", Days: 0, State: StateToday, DateLabel: "Thu Nov 26"}
	if got[0] != want {
		t.Fatalf("got %+v, want %+v", got[0], want)
	}
}

func TestEveningBeforeIsNotToday(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	// 04:30 UTC on Nov 26 is 23:30 on Nov 25 in New York.
	today := date.Of(time.Date(2026, 11, 26, 4, 30, 0, 0, time.UTC), ny)
	if today != d(2026, 11, 25) {
		t.Fatalf("today = %v, want 2026-11-25", today)
	}
	got := fixtureScenario(t, today, false, false).columns(today)
	want := Column{Source: "Holiday", Name: "Thanksgiving", Days: 1, State: StateUpcoming, DateLabel: "Thu Nov 26"}
	if len(got) == 0 || got[0] != want {
		t.Fatalf("got %+v, want first %+v", got, want)
	}
}

func TestDuringTrip(t *testing.T) {
	today := d(2026, 11, 15)
	got := fixtureScenario(t, today, true, true).columns(today)
	want := Column{
		Source: "Trip", Name: "Denver", Days: 0, State: StateDuring,
		DayOf: 2, DayTotal: 5, DateLabel: "Sat Nov 14",
	}
	if len(got) == 0 || got[0] != want {
		t.Fatalf("got %+v, want first %+v", got, want)
	}
	if len(got) != 3 {
		t.Errorf("got %d columns, want 3", len(got))
	}
}

func TestTripStartingTodayIsToday(t *testing.T) {
	today := d(2026, 11, 14)
	got := fixtureScenario(t, today, true, false).columns(today)
	want := Column{Source: "Trip", Name: "Denver", Days: 0, State: StateToday, DateLabel: "Sat Nov 14"}
	if len(got) == 0 || got[0] != want {
		t.Fatalf("got %+v, want first %+v", got, want)
	}
}

func TestTripLastDayIsDuring(t *testing.T) {
	today := d(2026, 11, 18)
	got := fixtureScenario(t, today, true, false).columns(today)
	if len(got) == 0 || got[0].State != StateDuring || got[0].DayOf != 5 || got[0].DayTotal != 5 {
		t.Fatalf("got %+v", got)
	}
}

func TestEndedTripNeverShows(t *testing.T) {
	today := d(2026, 11, 19)
	got := fixtureScenario(t, today, true, false).columns(today)
	for _, c := range got {
		if c.Name == "Denver" {
			t.Errorf("ended trip shown: %+v", c)
		}
	}
	// The later trip fills the Trip column.
	want := Column{Source: "Trip", Name: "Trip", Days: 85, State: StateUpcoming, DateLabel: "Fri Feb 12"}
	if len(got) != 3 || got[2] != want {
		t.Errorf("got %+v, want the 2027-02-12 trip last: %+v", got, want)
	}
}

func TestSchoolBreakCountsToFirstDayOnly(t *testing.T) {
	// Mid-break: the break started yesterday, so it is a past event.
	today := d(2026, 11, 26)
	school := SchoolEvents(parseFixture(t, "school.ics"), DefaultSchoolRules)
	got := Columns(today, nil, school, nil, testSchoolLabel, 3)
	for _, c := range got {
		if c.Name == "Thanksgiving break" {
			t.Errorf("break in progress shown: %+v", c)
		}
		if c.State == StateDuring {
			t.Errorf("school column in state during: %+v", c)
		}
	}
}

func TestPastEventsNeverShow(t *testing.T) {
	today := d(2026, 10, 11)
	school := []Event{{School, "Fall break", d(2026, 10, 10), d(2026, 10, 10)}}
	hols := HolidayEvents(holidays.Upcoming(today, holidays.DefaultKeys, 3))
	got := Columns(today, nil, school, hols, testSchoolLabel, 3)
	for _, c := range got {
		if c.Name == "Fall break" {
			t.Errorf("past event shown: %+v", c)
		}
		if c.Days < 0 {
			t.Errorf("negative days: %+v", c)
		}
	}
}

func TestPastHolidayEventsAreDropped(t *testing.T) {
	today := d(2026, 11, 27)
	hols := []Event{
		{Holiday, "Thanksgiving", d(2026, 11, 26), d(2026, 11, 26)},
		{Holiday, "Christmas", d(2026, 12, 25), d(2026, 12, 25)},
	}
	got := Columns(today, nil, nil, hols, testSchoolLabel, 3)
	if len(got) != 1 || got[0].Name != "Christmas" {
		t.Errorf("got %+v, want only Christmas", got)
	}
}

func TestSameDayTieOrder(t *testing.T) {
	today := d(2026, 11, 1)
	trips := []Event{{Trip, "Denver", d(2026, 11, 26), d(2026, 11, 28)}}
	school := []Event{{School, "Reading day", d(2026, 11, 26), d(2026, 11, 26)}}
	hols := HolidayEvents(holidays.Upcoming(today, holidays.DefaultKeys, 3))
	got := Columns(today, trips, school, hols, testSchoolLabel, 3)
	want := []string{"Trip", "Binghamton", "Holiday"}
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	for i, src := range want {
		if got[i].Source != src {
			t.Errorf("column %d source = %q, want %q (all: %+v)", i, got[i].Source, src, got)
		}
	}
}

func TestEarliestPerSourceBeatsEarlierBackfill(t *testing.T) {
	// The trip is far off, but it still takes a slot ahead of nearer school events.
	today := d(2026, 11, 1)
	trips := []Event{{Trip, "Rome", d(2027, 6, 1), d(2027, 6, 8)}}
	school := []Event{
		{School, "A", d(2026, 11, 2), d(2026, 11, 2)},
		{School, "B", d(2026, 11, 3), d(2026, 11, 3)},
		{School, "C", d(2026, 11, 4), d(2026, 11, 4)},
	}
	got := Columns(today, trips, school, nil, testSchoolLabel, 3)
	names := []string{got[0].Name, got[1].Name, got[2].Name}
	want := []string{"A", "B", "Rome"}
	if len(got) != 3 || !reflect.DeepEqual(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestFewerCandidatesThanSlots(t *testing.T) {
	today := d(2026, 11, 1)
	got := Columns(today, nil, nil, nil, testSchoolLabel, 3)
	if len(got) != 0 {
		t.Errorf("got %+v, want none", got)
	}
}

func TestColumnsDoesNotMutateInput(t *testing.T) {
	today := d(2026, 11, 1)
	trips := []Event{
		{Trip, "Late", d(2027, 2, 12), d(2027, 2, 15)},
		{Trip, "Early", d(2026, 11, 14), d(2026, 11, 18)},
	}
	school := []Event{
		{School, "Z", d(2026, 12, 8), d(2026, 12, 8)},
		{School, "Y", d(2026, 11, 25), d(2026, 11, 29)},
	}
	hols := HolidayEvents(holidays.Upcoming(today, holidays.DefaultKeys, 3))
	tripsCopy := append([]Event(nil), trips...)
	schoolCopy := append([]Event(nil), school...)
	holsCopy := append([]Event(nil), hols...)
	Columns(today, trips, school, hols, testSchoolLabel, 3)
	if !reflect.DeepEqual(trips, tripsCopy) || !reflect.DeepEqual(school, schoolCopy) || !reflect.DeepEqual(hols, holsCopy) {
		t.Errorf("Columns changed its input")
	}
}

func TestAlwaysThreeColumns(t *testing.T) {
	start := d(2026, 11, 1)
	for i := range 400 {
		today := start.AddDays(i)
		hols := HolidayEvents(holidays.Upcoming(today, holidays.DefaultKeys, 3))
		got := Columns(today, nil, nil, hols, testSchoolLabel, 3)
		if len(got) != 3 {
			t.Fatalf("%v: got %d columns, want 3: %+v", today, len(got), got)
		}
		for _, c := range got {
			if c.Days < 0 {
				t.Fatalf("%v: negative days: %+v", today, c)
			}
		}
		for _, e := range hols {
			if e.Start.Before(today) {
				t.Fatalf("%v: Upcoming returned a past holiday: %+v", today, e)
			}
		}
	}
}
