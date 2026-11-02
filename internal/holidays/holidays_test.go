package holidays

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/date"
)

func d(y int, m time.Month, day int) date.Date {
	return date.Date{Year: y, Month: m, Day: day}
}

var want2026 = map[string]date.Date{
	"new_years": d(2026, 1, 1), "mlk": d(2026, 1, 19), "presidents": d(2026, 2, 16),
	"good_friday": d(2026, 4, 3), "memorial": d(2026, 5, 25), "juneteenth": d(2026, 6, 19),
	"independence": d(2026, 7, 4), "labor": d(2026, 9, 7), "thanksgiving": d(2026, 11, 26),
	"christmas": d(2026, 12, 25), "halloween": d(2026, 10, 31),
}

var want2027 = map[string]date.Date{
	"new_years": d(2027, 1, 1), "mlk": d(2027, 1, 18), "presidents": d(2027, 2, 15),
	"good_friday": d(2027, 3, 26), "memorial": d(2027, 5, 31), "juneteenth": d(2027, 6, 19),
	"independence": d(2027, 7, 4), "labor": d(2027, 9, 6), "thanksgiving": d(2027, 11, 25),
	"christmas": d(2027, 12, 25), "halloween": d(2027, 10, 31),
}

// 2025 and 2029 cross-check years: 2029 has Thanksgiving on Nov 22, the
// earliest possible date.
var want2025 = map[string]date.Date{
	"new_years": d(2025, 1, 1), "mlk": d(2025, 1, 20), "presidents": d(2025, 2, 17),
	"good_friday": d(2025, 4, 18), "memorial": d(2025, 5, 26), "juneteenth": d(2025, 6, 19),
	"independence": d(2025, 7, 4), "labor": d(2025, 9, 1), "thanksgiving": d(2025, 11, 27),
	"christmas": d(2025, 12, 25), "halloween": d(2025, 10, 31),
}

var want2029 = map[string]date.Date{
	"new_years": d(2029, 1, 1), "mlk": d(2029, 1, 15), "presidents": d(2029, 2, 19),
	"good_friday": d(2029, 3, 30), "memorial": d(2029, 5, 28), "juneteenth": d(2029, 6, 19),
	"independence": d(2029, 7, 4), "labor": d(2029, 9, 3), "thanksgiving": d(2029, 11, 22),
	"christmas": d(2029, 12, 25), "halloween": d(2029, 10, 31),
}

func TestEaster(t *testing.T) {
	cases := []struct {
		year int
		want date.Date
	}{
		{2024, d(2024, 3, 31)},
		{2025, d(2025, 4, 20)},
		{2026, d(2026, 4, 5)},
		{2027, d(2027, 3, 28)},
		{2028, d(2028, 4, 16)},
		{2029, d(2029, 4, 1)},
		{2030, d(2030, 4, 21)},
		{2038, d(2038, 4, 25)},
	}
	for _, c := range cases {
		if got := Easter(c.year); got != c.want {
			t.Errorf("Easter(%d) = %v, want %v", c.year, got, c.want)
		}
	}
}

func checkYear(t *testing.T, year int, want map[string]date.Date) {
	t.Helper()
	got := ForYear(year, DefaultKeys)
	if len(got) != len(DefaultKeys) {
		t.Fatalf("ForYear(%d) returned %d holidays, want %d", year, len(got), len(DefaultKeys))
	}
	for _, h := range got {
		w, ok := want[h.Key]
		if !ok {
			t.Errorf("unexpected key %q", h.Key)
			continue
		}
		if h.Date != w {
			t.Errorf("%d %s = %v, want %v", year, h.Key, h.Date, w)
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i].Date.Before(got[i-1].Date) {
			t.Errorf("%d: %s (%v) is listed after %s (%v)",
				year, got[i].Key, got[i].Date, got[i-1].Key, got[i-1].Date)
		}
	}
}

func TestForYear2025(t *testing.T) { checkYear(t, 2025, want2025) }
func TestForYear2026(t *testing.T) { checkYear(t, 2026, want2026) }
func TestForYear2027(t *testing.T) { checkYear(t, 2027, want2027) }
func TestForYear2029(t *testing.T) { checkYear(t, 2029, want2029) }

func TestNames(t *testing.T) {
	want := map[string]string{
		"new_years": "New Year's Day", "mlk": "MLK Day", "presidents": "Presidents' Day",
		"good_friday": "Good Friday", "memorial": "Memorial Day", "juneteenth": "Juneteenth",
		"independence": "Independence Day", "labor": "Labor Day", "thanksgiving": "Thanksgiving",
		"christmas": "Christmas", "halloween": "Halloween",
	}
	for _, h := range ForYear(2026, DefaultKeys) {
		if h.Name != want[h.Key] {
			t.Errorf("%s name = %q, want %q", h.Key, h.Name, want[h.Key])
		}
	}
}

// The date is the holiday's own date, never the NYSE observed weekday.
// 2026-07-04 is a Saturday and 2027-06-19 is a Saturday.
func TestOwnDateNotObserved(t *testing.T) {
	for _, h := range ForYear(2026, []string{"independence"}) {
		if h.Date != d(2026, 7, 4) || h.Date.Weekday() != time.Saturday {
			t.Errorf("got %v (%v)", h.Date, h.Date.Weekday())
		}
	}
	for _, h := range ForYear(2027, []string{"juneteenth"}) {
		if h.Date != d(2027, 6, 19) || h.Date.Weekday() != time.Saturday {
			t.Errorf("got %v (%v)", h.Date, h.Date.Weekday())
		}
	}
}

func TestForYearSubsetSortedByDate(t *testing.T) {
	got := ForYear(2026, []string{"christmas", "mlk", "labor"})
	var keys []string
	for _, h := range got {
		keys = append(keys, h.Key)
	}
	if !slices.Equal(keys, []string{"mlk", "labor", "christmas"}) {
		t.Fatal(keys)
	}
}

func TestForYearEmptyKeys(t *testing.T) {
	if got := ForYear(2026, nil); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestForYearDuplicateKeys(t *testing.T) {
	got := ForYear(2026, []string{"mlk", "mlk"})
	if len(got) != 1 {
		t.Fatalf("got %d holidays, want 1: %v", len(got), got)
	}
}

func TestForYearUnknownKeyPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("no panic")
		}
		if !strings.Contains(fmt.Sprint(r), "festivus") {
			t.Fatalf("panic does not name the key: %v", r)
		}
	}()
	ForYear(2026, []string{"mlk", "festivus"})
}

func TestValidate(t *testing.T) {
	if err := Validate(DefaultKeys); err != nil {
		t.Fatal(err)
	}
	if err := Validate(nil); err != nil {
		t.Fatal(err)
	}
	err := Validate([]string{"mlk", "festivus", "bogus"})
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "festivus") {
		t.Fatalf("error does not name the first unknown key: %v", err)
	}
	if strings.Contains(err.Error(), "bogus") {
		t.Fatalf("error names a later key: %v", err)
	}
}

func TestUpcomingCrossesYear(t *testing.T) {
	got := Upcoming(d(2026, 12, 26), DefaultKeys, 2)
	if len(got) != 2 {
		t.Fatalf("got %d holidays, want 2", len(got))
	}
	if got[0].Key != "new_years" || got[0].Date != d(2027, 1, 1) {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Key != "mlk" || got[1].Date != d(2027, 1, 18) {
		t.Errorf("second = %+v", got[1])
	}
}

func TestUpcomingIncludesToday(t *testing.T) {
	got := Upcoming(d(2026, 11, 26), DefaultKeys, 1) // Thanksgiving itself
	if len(got) != 1 || got[0].Key != "thanksgiving" || got[0].Date != d(2026, 11, 26) {
		t.Fatal(got)
	}
}

func TestUpcomingSkipsYesterday(t *testing.T) {
	got := Upcoming(d(2026, 11, 27), DefaultKeys, 1)
	if len(got) != 1 || got[0].Key != "christmas" {
		t.Fatal(got)
	}
}

func TestUpcomingExactlyN(t *testing.T) {
	// 11 holidays a year: 25 items span three years.
	got := Upcoming(d(2026, 11, 2), DefaultKeys, 25)
	if len(got) != 25 {
		t.Fatalf("got %d holidays, want 25", len(got))
	}
	for i := 1; i < len(got); i++ {
		if !got[i-1].Date.Before(got[i].Date) {
			t.Errorf("item %d (%v) does not follow item %d (%v)",
				i, got[i].Date, i-1, got[i-1].Date)
		}
	}
	// Halloween 2026 is past on Nov 2, so the first is Thanksgiving.
	if got[0].Key != "thanksgiving" || got[0].Date != d(2026, 11, 26) {
		t.Errorf("first = %+v", got[0])
	}
	if got[24].Key != "new_years" || got[24].Date != d(2029, 1, 1) {
		t.Errorf("last = %+v", got[24])
	}
}

func TestUpcomingFewKeys(t *testing.T) {
	got := Upcoming(d(2026, 12, 26), []string{"christmas"}, 3)
	want := []date.Date{d(2027, 12, 25), d(2028, 12, 25), d(2029, 12, 25)}
	if len(got) != 3 {
		t.Fatalf("got %d holidays, want 3", len(got))
	}
	for i := range want {
		if got[i].Date != want[i] {
			t.Errorf("item %d = %v, want %v", i, got[i].Date, want[i])
		}
	}
}

func TestUpcomingNothingToReturn(t *testing.T) {
	if got := Upcoming(d(2026, 11, 2), DefaultKeys, 0); len(got) != 0 {
		t.Fatal(got)
	}
	if got := Upcoming(d(2026, 11, 2), nil, 3); len(got) != 0 {
		t.Fatal(got)
	}
}
