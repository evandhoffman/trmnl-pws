// Package server is the HTTP face of the poller. TRMNL polls it through
// nginx on the public internet.
//
// Every request needs the secret header, with one exception: GET /healthz from
// a loopback address. Anything else gets one fixed 404, so a caller without
// the key learns nothing about which paths exist.
package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/countdown"
	"github.com/evandhoffman/trmnl-pws/internal/date"
	"github.com/evandhoffman/trmnl-pws/internal/feeds"
	"github.com/evandhoffman/trmnl-pws/internal/holidays"
	"github.com/evandhoffman/trmnl-pws/internal/ical"
)

const (
	keyHeader = "X-Poller-Key"

	columnCount = 3

	// silenceWindowDays is how far ahead the school feed must show an event.
	silenceWindowDays = 120
	// silenceWarnEvery is the least time between two silence warnings.
	silenceWarnEvery = time.Hour
)

// Deps holds what the handler needs. Trips and School may be nil. A nil feed
// is a source that is not configured: it is empty, with no error and no log.
type Deps struct {
	Key         string
	Loc         *time.Location
	Now         func() time.Time
	Trips       *feeds.Feed
	School      *feeds.Feed
	SchoolLabel string
	SchoolRules countdown.SchoolRules
	HolidayKeys []string
	Version     string
}

type handler struct {
	d               Deps
	keySum          [sha256.Size]byte
	hasKey          bool
	mu              sync.Mutex // guards lastSilenceWarn
	lastSilenceWarn time.Time
}

// New returns the handler for the poller.
func New(d Deps) http.Handler {
	h := &handler{d: d, hasKey: d.Key != ""}
	h.keySum = sha256.Sum256([]byte(d.Key))
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	h.route(rec, r)
	slog.Info("request",
		"method", r.Method,
		"path", r.URL.Path,
		"status", rec.status,
		"duration_ms", time.Since(start).Milliseconds(),
		"ip", clientIP(r))
}

// route checks the key before it looks at the path or the method, so a
// caller without the key cannot tell an unknown path from a known one.
func (h *handler) route(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/healthz" && fromLoopback(r) {
		writeText(w, "ok")
		return
	}
	if !h.keyOK(r) || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	switch r.URL.Path {
	case "/healthz":
		writeText(w, "ok")
	case "/version":
		writeText(w, h.d.Version)
	case "/v1/countdown":
		h.countdown(w, r)
	default:
		http.NotFound(w, r)
	}
}

// keyOK compares SHA-256 sums of the keys, so the compare takes the same time
// for any length of input. A server with no key configured accepts nothing.
func (h *handler) keyOK(r *http.Request) bool {
	if !h.hasKey {
		return false
	}
	got := sha256.Sum256([]byte(r.Header.Get(keyHeader)))
	return subtle.ConstantTimeCompare(got[:], h.keySum[:]) == 1
}

// fromLoopback looks at the TCP peer only. nginx connects from a LAN
// address, so public traffic never qualifies. Forwarding headers are not
// trusted here: the caller controls them.
func fromLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// clientIP is for the access log only, never for a decision.
func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeText(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(s))
}

type response struct {
	Columns []countdown.Column `json:"columns"`
	Updated string             `json:"updated"`
}

func (h *handler) countdown(w http.ResponseWriter, r *http.Request) {
	now := h.d.Now()
	today := date.Of(now, h.d.Loc)

	var tripEvs, schoolEvs []ical.Event
	var tripErr, schoolErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		tripEvs, tripErr = events(r.Context(), h.d.Trips, h.d.Loc)
	}()
	go func() {
		defer wg.Done()
		schoolEvs, schoolErr = events(r.Context(), h.d.School, h.d.Loc)
	}()
	wg.Wait()

	// The feed error text holds the feed name and never the URL.
	for _, err := range []error{tripErr, schoolErr} {
		if err != nil {
			slog.Warn("feed unavailable", "error", err.Error())
		}
	}

	trips := countdown.TripEvents(tripEvs)
	school := countdown.SchoolEvents(schoolEvs, h.d.SchoolRules)
	if h.d.School != nil && schoolErr == nil {
		h.checkSchoolSilence(school, today, now)
	}
	hols := countdown.HolidayEvents(holidays.Upcoming(today, h.d.HolidayKeys, columnCount))

	cols := countdown.Columns(today, trips, school, hols, h.d.SchoolLabel, columnCount)
	if cols == nil {
		cols = []countdown.Column{}
	}
	body, err := json.Marshal(response{
		Columns: cols,
		Updated: now.In(h.d.Loc).Format("3:04 PM"),
	})
	if err != nil {
		slog.Error("encode countdown", "error", err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// events reads one feed. A nil feed is an empty source.
func events(ctx context.Context, f *feeds.Feed, loc *time.Location) ([]ical.Event, error) {
	if f == nil {
		return nil, nil
	}
	return f.Events(ctx, loc)
}

// checkSchoolSilence warns when the school feed has no event that starts in
// the next 120 days. A feed that goes quiet looks the same as a calendar
// that is between terms, so the warning is the only sign. It repeats at most
// once an hour.
func (h *handler) checkSchoolSilence(school []countdown.Event, today date.Date, now time.Time) {
	for _, e := range school {
		if days := today.DaysUntil(e.Start); days >= 0 && days <= silenceWindowDays {
			return
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.lastSilenceWarn.IsZero() && now.Sub(h.lastSilenceWarn) < silenceWarnEvery {
		return
	}
	h.lastSilenceWarn = now
	slog.Warn("school feed has no upcoming events", "window_days", silenceWindowDays)
}

// statusRecorder keeps the status code for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
