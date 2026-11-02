package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/countdown"
	"github.com/evandhoffman/trmnl-pws/internal/feeds"
	"github.com/evandhoffman/trmnl-pws/internal/holidays"
)

const testKey = "0123456789abcdef0123456789abcdef-test-key"

var ny = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic(err)
	}
	return loc
}()

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock(t time.Time) *clock { return &clock{t: t} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// 2026-11-01 12:00 EST.
func fixedNow() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, ny) }

// logBuf is a goroutine-safe buffer for captured log output.
type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// lines parses the captured JSON log lines.
func (l *logBuf) lines(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(l.String()), "\n") {
		if line == "" {
			continue
		}
		m := map[string]any{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func (l *logBuf) count(t *testing.T, msg string) int {
	t.Helper()
	n := 0
	for _, m := range l.lines(t) {
		if m["msg"] == msg {
			n++
		}
	}
	return n
}

// captureLogs replaces the default logger for one test.
func captureLogs(t *testing.T) *logBuf {
	t.Helper()
	buf := &logBuf{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf
}

func fixtureServer(t *testing.T, name string) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile("../ical/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func failingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testDeps(now func() time.Time, trips, school *feeds.Feed) Deps {
	return Deps{
		Key:         testKey,
		Loc:         ny,
		Now:         now,
		Trips:       trips,
		School:      school,
		SchoolLabel: "Binghamton",
		SchoolRules: countdown.DefaultSchoolRules,
		HolidayKeys: holidays.DefaultKeys,
		Version:     "1.2.3-test",
	}
}

// do sends one request straight to the handler.
func do(h http.Handler, method, target, remote string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = remote
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const lan = "192.168.1.20:41000"

func keyed() map[string]string { return map[string]string{"X-Poller-Key": testKey} }

func assertNotFound(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if got := rec.Body.String(); got != "404 page not found\n" {
		t.Fatalf("body = %q", got)
	}
}

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	captureLogs(t)
	trips := feeds.New("trips", fixtureServer(t, "tripit.ics").URL, time.Hour, fixedNow)
	school := feeds.New("school", fixtureServer(t, "school.ics").URL, time.Hour, fixedNow)
	return New(testDeps(fixedNow, trips, school))
}

func TestNoKeyIs404(t *testing.T) {
	h := newHandler(t)
	for _, p := range []string{"/v1/countdown", "/version", "/healthz", "/"} {
		assertNotFound(t, do(h, http.MethodGet, p, lan, nil))
	}
}

func TestWrongKeyIs404SameBody(t *testing.T) {
	h := newHandler(t)
	unknown := do(h, http.MethodGet, "/nope", lan, keyed())
	assertNotFound(t, unknown)

	wrong := map[string]string{"X-Poller-Key": testKey + "x"}
	short := map[string]string{"X-Poller-Key": "short"}
	cases := map[string]*httptest.ResponseRecorder{
		"wrong key":         do(h, http.MethodGet, "/v1/countdown", lan, wrong),
		"short key":         do(h, http.MethodGet, "/v1/countdown", lan, short),
		"no key":            do(h, http.MethodGet, "/v1/countdown", lan, nil),
		"post":              do(h, http.MethodPost, "/v1/countdown", lan, keyed()),
		"head":              do(h, http.MethodHead, "/v1/countdown", lan, keyed()),
		"healthz remote":    do(h, http.MethodGet, "/healthz", "203.0.113.9:5000", nil),
		"key in query only": do(h, http.MethodGet, "/v1/countdown?key="+testKey, lan, nil),
		"traversal":         do(h, http.MethodGet, "/v1/../v1/countdown/", lan, keyed()),
	}
	for name, rec := range cases {
		t.Run(name, func(t *testing.T) {
			assertNotFound(t, rec)
			if !reflect.DeepEqual(rec.Header(), unknown.Header()) {
				t.Fatalf("headers differ: %v vs %v", rec.Header(), unknown.Header())
			}
		})
	}
}

func TestEmptyConfiguredKeyRejectsEverything(t *testing.T) {
	captureLogs(t)
	d := testDeps(fixedNow, nil, nil)
	d.Key = ""
	h := New(d)
	assertNotFound(t, do(h, http.MethodGet, "/version", lan, nil))
	assertNotFound(t, do(h, http.MethodGet, "/version", lan, map[string]string{"X-Poller-Key": ""}))
}

func TestPostIs404(t *testing.T) {
	h := newHandler(t)
	assertNotFound(t, do(h, http.MethodPost, "/v1/countdown", lan, keyed()))
	assertNotFound(t, do(h, http.MethodPost, "/healthz", "127.0.0.1:9000", nil))
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) (cols []countdown.Column, updated string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
	var out struct {
		Columns []countdown.Column `json:"columns"`
		Updated string             `json:"updated"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad json %q: %v", rec.Body.String(), err)
	}
	return out.Columns, out.Updated
}

func TestCountdownOK(t *testing.T) {
	h := newHandler(t)
	cols, updated := decode(t, do(h, http.MethodGet, "/v1/countdown", lan, keyed()))
	if len(cols) != 3 {
		t.Fatalf("got %d columns: %+v", len(cols), cols)
	}
	first := cols[0]
	if first.Source != "Trip" || first.Name != "Denver" || first.Days != 13 ||
		first.State != countdown.StateUpcoming {
		t.Fatalf("first column = %+v", first)
	}
	if cols[1].Source != "Binghamton" {
		t.Fatalf("second column = %+v, want the school", cols[1])
	}
	if updated != "12:00 PM" {
		t.Fatalf("updated = %q", updated)
	}
}

func TestCountdownRawShape(t *testing.T) {
	h := newHandler(t)
	rec := do(h, http.MethodGet, "/v1/countdown", lan, keyed())
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 || raw["columns"] == nil || raw["updated"] == nil {
		t.Fatalf("keys = %v", raw)
	}
}

func TestCountdownWithTripFeedDown(t *testing.T) {
	logs := captureLogs(t)
	bad := failingServer(t)
	trips := feeds.New("trips", bad.URL, time.Hour, fixedNow)
	school := feeds.New("school", fixtureServer(t, "school.ics").URL, time.Hour, fixedNow)
	h := New(testDeps(fixedNow, trips, school))

	cols, _ := decode(t, do(h, http.MethodGet, "/v1/countdown", lan, keyed()))
	if len(cols) != 3 {
		t.Fatalf("got %d columns", len(cols))
	}
	for _, c := range cols {
		if c.Source == "Trip" {
			t.Fatalf("trip column present: %+v", c)
		}
	}
	var warned bool
	for _, m := range logs.lines(t) {
		if m["level"] == "WARN" && m["msg"] == "feed unavailable" {
			warned = true
			if got, _ := m["error"].(string); !strings.Contains(got, "status 500") {
				t.Fatalf("error text = %q", got)
			}
		}
	}
	if !warned {
		t.Fatalf("no WARN for the failed feed:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), bad.URL) {
		t.Fatalf("log holds the feed URL:\n%s", logs.String())
	}
}

func TestCountdownNilFeedsAreQuiet(t *testing.T) {
	logs := captureLogs(t)
	h := New(testDeps(fixedNow, nil, nil))
	cols, _ := decode(t, do(h, http.MethodGet, "/v1/countdown", lan, keyed()))
	if len(cols) != 3 {
		t.Fatalf("got %d columns", len(cols))
	}
	for _, c := range cols {
		if c.Source != "Holiday" {
			t.Fatalf("column %+v, want only holidays", c)
		}
	}
	for _, m := range logs.lines(t) {
		if m["level"] != "INFO" {
			t.Fatalf("unexpected log line: %v", m)
		}
	}
}

func TestHealthzLoopbackWithoutKey(t *testing.T) {
	h := newHandler(t)
	for _, remote := range []string{"127.0.0.1:5000", "[::1]:5000"} {
		rec := do(h, http.MethodGet, "/healthz", remote, nil)
		if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
			t.Fatalf("%s: got %d %q", remote, rec.Code, rec.Body.String())
		}
	}
}

func TestHealthzRemoteNeedsKey(t *testing.T) {
	h := newHandler(t)
	assertNotFound(t, do(h, http.MethodGet, "/healthz", "203.0.113.9:5000", nil))
	rec := do(h, http.MethodGet, "/healthz", "203.0.113.9:5000", keyed())
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("with key: %d %q", rec.Code, rec.Body.String())
	}
}

func TestHealthzIgnoresForwardingHeaders(t *testing.T) {
	h := newHandler(t)
	hdr := map[string]string{"X-Real-IP": "127.0.0.1", "X-Forwarded-For": "127.0.0.1"}
	assertNotFound(t, do(h, http.MethodGet, "/healthz", "203.0.113.9:5000", hdr))
}

func TestLoopbackDoesNotOpenOtherPaths(t *testing.T) {
	h := newHandler(t)
	assertNotFound(t, do(h, http.MethodGet, "/v1/countdown", "127.0.0.1:5000", nil))
	assertNotFound(t, do(h, http.MethodGet, "/version", "127.0.0.1:5000", nil))
}

func TestVersion(t *testing.T) {
	h := newHandler(t)
	rec := do(h, http.MethodGet, "/version", lan, keyed())
	if rec.Code != http.StatusOK || rec.Body.String() != "1.2.3-test" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

func TestAccessLogHasNoKey(t *testing.T) {
	logs := captureLogs(t)
	h := New(testDeps(fixedNow, nil, nil))

	hdr := keyed()
	hdr["X-Real-IP"] = "198.51.100.7"
	do(h, http.MethodGet, "/version", lan, hdr)
	do(h, http.MethodGet, "/v1/countdown?key="+testKey+"&x=1", lan, hdr)
	do(h, http.MethodGet, "/nope", lan, map[string]string{"X-Poller-Key": testKey + "-wrong"})

	if strings.Contains(logs.String(), testKey) {
		t.Fatalf("log holds the key:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), "x=1") {
		t.Fatalf("log holds the query string:\n%s", logs.String())
	}

	var access []map[string]any
	for _, m := range logs.lines(t) {
		if m["msg"] == "request" {
			access = append(access, m)
		}
	}
	if len(access) != 3 {
		t.Fatalf("got %d access lines, want 3:\n%s", len(access), logs.String())
	}
	first := access[0]
	if first["method"] != "GET" || first["path"] != "/version" ||
		first["status"] != float64(200) || first["ip"] != "198.51.100.7" {
		t.Fatalf("first line = %v", first)
	}
	if _, ok := first["duration_ms"]; !ok {
		t.Fatalf("no duration_ms: %v", first)
	}
	if access[1]["path"] != "/v1/countdown" {
		t.Fatalf("second line = %v", access[1])
	}
	if access[2]["status"] != float64(404) {
		t.Fatalf("third line = %v", access[2])
	}
}

func TestAccessLogFallsBackToRemoteAddr(t *testing.T) {
	logs := captureLogs(t)
	h := New(testDeps(fixedNow, nil, nil))
	do(h, http.MethodGet, "/version", lan, keyed())
	for _, m := range logs.lines(t) {
		if m["msg"] == "request" && m["ip"] != "192.168.1.20" {
			t.Fatalf("ip = %v", m["ip"])
		}
	}
}

// After the school fixture ends, no event starts within 120 days.
func TestSchoolSilenceWarnsOncePerHour(t *testing.T) {
	logs := captureLogs(t)
	clk := newClock(time.Date(2027, 6, 1, 9, 0, 0, 0, ny))
	school := feeds.New("school", fixtureServer(t, "school.ics").URL, time.Hour, clk.Now)
	h := New(testDeps(clk.Now, nil, school))
	const msg = "school feed has no upcoming events"

	decode(t, do(h, http.MethodGet, "/v1/countdown", lan, keyed()))
	if n := logs.count(t, msg); n != 1 {
		t.Fatalf("first request logged %d warnings:\n%s", n, logs.String())
	}
	for _, m := range logs.lines(t) {
		if m["msg"] == msg && m["level"] != "WARN" {
			t.Fatalf("level = %v", m["level"])
		}
	}

	clk.Advance(30 * time.Minute)
	decode(t, do(h, http.MethodGet, "/v1/countdown", lan, keyed()))
	if n := logs.count(t, msg); n != 1 {
		t.Fatalf("second request in the hour logged again (%d)", n)
	}

	clk.Advance(31 * time.Minute)
	decode(t, do(h, http.MethodGet, "/v1/countdown", lan, keyed()))
	if n := logs.count(t, msg); n != 2 {
		t.Fatalf("request after the hour logged %d warnings total, want 2", n)
	}
}

func TestSchoolSilenceQuietWhenEventsExist(t *testing.T) {
	logs := captureLogs(t)
	school := feeds.New("school", fixtureServer(t, "school.ics").URL, time.Hour, fixedNow)
	h := New(testDeps(fixedNow, nil, school))
	decode(t, do(h, http.MethodGet, "/v1/countdown", lan, keyed()))
	if n := logs.count(t, "school feed has no upcoming events"); n != 0 {
		t.Fatalf("logged %d warnings:\n%s", n, logs.String())
	}
}

func TestSchoolSilenceQuietWithoutSchoolFeed(t *testing.T) {
	logs := captureLogs(t)
	h := New(testDeps(fixedNow, nil, nil))
	decode(t, do(h, http.MethodGet, "/v1/countdown", lan, keyed()))
	if n := logs.count(t, "school feed has no upcoming events"); n != 0 {
		t.Fatalf("logged %d warnings", n)
	}
}

func TestSchoolSilenceQuietWhenFeedFails(t *testing.T) {
	logs := captureLogs(t)
	school := feeds.New("school", failingServer(t).URL, time.Hour, fixedNow)
	h := New(testDeps(fixedNow, nil, school))
	decode(t, do(h, http.MethodGet, "/v1/countdown", lan, keyed()))
	if n := logs.count(t, "school feed has no upcoming events"); n != 0 {
		t.Fatalf("a feed error also logged the silence warning")
	}
}
