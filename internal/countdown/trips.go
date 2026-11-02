package countdown

import (
	"strings"

	"github.com/evandhoffman/trmnl-pws/internal/ical"
)

const (
	tripItemPrefix = "item-"
	noDestination  = "No destination specified"
	genericTrip    = "Trip"
)

// TripEvents keeps the trip-level events of a TripIt feed. A trip-level event
// is all-day and its UID does not start with "item-". The name is the city:
// the LOCATION text before its first comma. The name never comes from SUMMARY
// or DESCRIPTION, which hold booking details. A trip with no destination is
// named "Trip".
func TripEvents(evs []ical.Event) []Event {
	var out []Event
	for _, ev := range evs {
		if !ev.AllDay || strings.HasPrefix(ev.UID, tripItemPrefix) {
			continue
		}
		out = append(out, Event{Trip, tripCity(ev.Location), ev.Start, ev.End})
	}
	return out
}

func tripCity(location string) string {
	loc := strings.TrimSpace(location)
	if loc == noDestination {
		return genericTrip
	}
	city, _, _ := strings.Cut(loc, ",")
	city = strings.TrimSpace(city)
	if city == "" {
		return genericTrip
	}
	return city
}
