package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/date"
	"github.com/evandhoffman/trmnl-pws/internal/series"
	"github.com/evandhoffman/trmnl-pws/internal/solar"
	"github.com/evandhoffman/trmnl-pws/internal/sun"
)

const (
	// solarFreshFor is how long a fetch serves both solar paths.
	solarFreshFor = 60 * time.Second
	// solarStaleFor is how long after a fetch the handler still serves it
	// when a refresh fails.
	solarStaleFor = 15 * time.Minute
	// solarWeekDays is how many days before today the fetch reaches back.
	solarWeekDays = 7
	// solarSeedBack is how far before the window a reading can seed it. The
	// reserve changes rarely, so it needs a long look back.
	solarSeedBack = 400 * 24 * time.Hour
)

// PointReader reads step-hold readings. *influx.Client implements it.
type PointReader interface {
	Points(ctx context.Context, entities []string, start, end time.Time, seedBack time.Duration) (map[string][]series.Point, error)
}

// solarCache holds the last good fetch. Both solar paths share it. The mutex
// stays locked during a fetch, so concurrent requests make one query.
type solarCache struct {
	mu      sync.Mutex
	have    bool // true after the first good fetch
	pts     map[string][]series.Point
	fetched time.Time
}

// solarPoints returns the readings for one request. It reuses a fetch under
// 60 s old. When a refresh fails, it returns the last good fetch if that is
// at most 15 min old, and an error otherwise. The second result is the time
// the readings are good for: now for a fresh fetch, the fetch time for a
// stale copy.
func (h *handler) solarPoints(ctx context.Context, now time.Time) (map[string][]series.Point, time.Time, error) {
	c := &h.solar
	c.mu.Lock()
	defer c.mu.Unlock()

	if age := now.Sub(c.fetched); c.have && age >= 0 && age < solarFreshFor {
		return c.pts, now, nil
	}
	today := date.Of(now, h.d.Loc)
	start := today.AddDays(-solarWeekDays).Start(h.d.Loc)
	pts, err := h.d.Influx.Points(ctx, h.d.Entities.All(), start, now, solarSeedBack)
	if err == nil {
		c.have, c.pts, c.fetched = true, pts, now
		return pts, now, nil
	}
	// The log line holds a short reason, never the error text: the text can
	// name the InfluxDB host.
	if c.have && now.Sub(c.fetched) <= solarStaleFor {
		slog.Warn("solar refresh failed", "reason", failureReason(err), "serving", "stale",
			"age_s", int(now.Sub(c.fetched)/time.Second))
		return c.pts, c.fetched, nil
	}
	slog.Warn("solar refresh failed", "reason", failureReason(err), "serving", "none")
	return nil, time.Time{}, err
}

// failureReason names a read failure in a few safe words: "timeout",
// "status NNN", or "error".
func failureReason(err error) string {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return "timeout"
	}
	if rest, ok := strings.CutPrefix(err.Error(), "influx: status "); ok {
		n := 0
		for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
			n++
		}
		if n > 0 {
			return "status " + rest[:n]
		}
	}
	return "error"
}

func (h *handler) solarToday(w http.ResponseWriter, r *http.Request) {
	h.serveSolar(w, r, func(now time.Time, pts map[string][]series.Point) any {
		return solar.BuildToday(now, h.d.Loc, time.Duration(h.d.TodayWindowHours)*time.Hour, pts, h.d.Entities, h.sunEvents(now))
	})
}

func (h *handler) solarWeek(w http.ResponseWriter, r *http.Request) {
	h.serveSolar(w, r, func(now time.Time, pts map[string][]series.Point) any {
		return solar.BuildWeek(date.Of(now, h.d.Loc), now, h.d.Loc, pts, h.d.Entities, h.d.NetZeroKW)
	})
}

// serveSolar answers 404 with no InfluxDB reader, 503 with no usable
// readings, and the built payload otherwise. The payload uses the current
// time, even when the readings come from a fresh cache. A stale copy uses its
// fetch time, so the screen shows the age of the data.
func (h *handler) serveSolar(w http.ResponseWriter, r *http.Request, build func(time.Time, map[string][]series.Point) any) {
	if h.d.Influx == nil {
		http.NotFound(w, r)
		return
	}
	pts, now, err := h.solarPoints(r.Context(), h.d.Now())
	if err != nil {
		http.Error(w, "solar data unavailable", http.StatusServiceUnavailable)
		return
	}
	body, err := json.Marshal(build(now, pts))
	if err != nil {
		slog.Error("encode solar", "error", err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// sunEvents returns the sun events of yesterday and today, or nil when the
// coordinates are not set.
func (h *handler) sunEvents(now time.Time) []sun.Event {
	if h.d.Latitude == nil || h.d.Longitude == nil {
		return nil
	}
	today := date.Of(now, h.d.Loc)
	evs := sun.Events(today.AddDays(-1), *h.d.Latitude, *h.d.Longitude, h.d.Loc)
	return append(evs, sun.Events(today, *h.d.Latitude, *h.d.Longitude, h.d.Loc)...)
}
