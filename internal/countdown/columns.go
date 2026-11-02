package countdown

import (
	"sort"

	"github.com/evandhoffman/trmnl-pws/internal/date"
)

// State says how a column relates to today.
type State string

// The three column states.
const (
	StateUpcoming State = "upcoming"
	StateToday    State = "today"
	StateDuring   State = "during"
)

// Column is one countdown shown on the screen.
type Column struct {
	Source    string `json:"source"`
	Name      string `json:"name"`
	Days      int    `json:"days"` // 0 for today and during
	State     State  `json:"state"`
	DayOf     int    `json:"day_of,omitempty"`    // during only
	DayTotal  int    `json:"day_total,omitempty"` // during only
	DateLabel string `json:"date"`                // label of the start date
}

// Columns picks up to n columns for today. It never shows an event that is
// over, so no column has a negative count.
//
// A trip is a candidate until its last day. A school event or a holiday is a
// candidate from its first day only, so a school break counts down to its
// first day and then goes away. Each source that has a candidate gets its
// earliest one. The other slots go to the earliest candidates left. Columns
// sort by start date; on the same day, trip comes before school before
// holiday. Columns does not change its arguments.
func Columns(today date.Date, trips, school, hols []Event, schoolLabel string, n int) []Column {
	var cands []Event
	for _, e := range trips {
		if !e.End.Before(today) {
			cands = append(cands, e)
		}
	}
	for _, list := range [][]Event{school, hols} {
		for _, e := range list {
			if !e.Start.Before(today) {
				cands = append(cands, e)
			}
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return before(cands[i], cands[j]) })

	picked := make([]bool, len(cands))
	count := 0
	seen := map[Source]bool{}
	for i, e := range cands {
		if count == n {
			break
		}
		if !seen[e.Source] {
			seen[e.Source] = true
			picked[i] = true
			count++
		}
	}
	for i := range cands {
		if count == n {
			break
		}
		if !picked[i] {
			picked[i] = true
			count++
		}
	}

	cols := make([]Column, 0, count)
	for i, e := range cands {
		if picked[i] {
			cols = append(cols, column(today, e, schoolLabel))
		}
	}
	return cols
}

// before orders events by start date, then by source.
func before(a, b Event) bool {
	if a.Start != b.Start {
		return a.Start.Before(b.Start)
	}
	return a.Source < b.Source
}

func column(today date.Date, e Event, schoolLabel string) Column {
	c := Column{
		Source:    e.Source.Label(schoolLabel),
		Name:      e.Name,
		State:     StateUpcoming,
		DateLabel: e.Start.Label(),
	}
	switch {
	case e.Start == today:
		c.State = StateToday
	case e.Start.Before(today):
		// Only a trip reaches here: its start is past and its end is not.
		c.State = StateDuring
		c.DayOf = e.Start.DaysUntil(today) + 1
		c.DayTotal = e.Start.DaysUntil(e.End) + 1
	default:
		c.Days = today.DaysUntil(e.Start)
	}
	return c
}
