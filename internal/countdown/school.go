package countdown

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/evandhoffman/trmnl-pws/internal/date"
	"github.com/evandhoffman/trmnl-pws/internal/ical"
)

// SchoolRules choose which university calendar events to keep. Each entry is
// a regular expression, matched without regard to case. An event that matches
// an Exclude pattern is dropped, even when it matches an Include pattern.
type SchoolRules struct {
	Include, Exclude []string
}

// DefaultSchoolRules keep breaks, residence hall closings, the first and last
// day of classes, and commencement.
var DefaultSchoolRules = SchoolRules{
	Include: []string{
		`^Residence halls close`,
		`^No classes \(.*break\)`,
		`^Classes begin`,
		`^Last day of classes`,
		`^Commencement`,
	},
	Exclude: []string{`winter session`},
}

// Validate reports the first pattern that does not compile.
func (r SchoolRules) Validate() error {
	_, _, err := r.compile()
	return err
}

func compileAll(kind string, patterns []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile("(?i)" + p)
		if err != nil {
			return nil, fmt.Errorf("school %s pattern %q: %w", kind, p, err)
		}
		out = append(out, re)
	}
	return out, nil
}

func (r SchoolRules) compile() (include, exclude []*regexp.Regexp, err error) {
	if include, err = compileAll("include", r.Include); err != nil {
		return nil, nil, err
	}
	if exclude, err = compileAll("exclude", r.Exclude); err != nil {
		return nil, nil, err
	}
	return include, exclude, nil
}

func matchAny(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

var (
	breakTitle   = regexp.MustCompile(`(?i)^No classes \((.*)\)`)
	hallsTitle   = regexp.MustCompile(`(?i)^Residence halls close`)
	commencement = regexp.MustCompile(`(?i)^Commencement`)
)

// SchoolEvents keeps the events that match the rules and gives them short
// names. The result is in date order. A residence hall closing on the first
// day of a kept break joins that break. SchoolEvents panics when a pattern in
// r does not compile; call Validate first.
func SchoolEvents(evs []ical.Event, r SchoolRules) []Event {
	include, exclude, err := r.compile()
	if err != nil {
		panic(err)
	}

	var kept []Event
	var halls []ical.Event
	breakStarts := map[date.Date]bool{}
	for _, ev := range evs {
		title := strings.TrimSpace(ev.Summary)
		if matchAny(exclude, title) || !matchAny(include, title) {
			continue
		}
		switch {
		case hallsTitle.MatchString(title):
			halls = append(halls, ev)
		case breakTitle.MatchString(title):
			name := strings.TrimSpace(breakTitle.FindStringSubmatch(title)[1])
			kept = append(kept, Event{School, name, ev.Start, ev.End})
			breakStarts[ev.Start] = true
		case commencement.MatchString(title):
			kept = append(kept, Event{School, "Commencement", ev.Start, ev.End})
		default:
			kept = append(kept, Event{School, title, ev.Start, ev.End})
		}
	}
	for _, ev := range halls {
		if !breakStarts[ev.Start] {
			kept = append(kept, Event{School, "Halls close", ev.Start, ev.End})
		}
	}
	sort.SliceStable(kept, func(i, j int) bool {
		return kept[i].Start.Before(kept[j].Start)
	})
	return kept
}
