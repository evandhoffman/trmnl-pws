package feeds

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var ny = mustLoc("America/New_York")

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../ical/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// server serves the school fixture until fail is set, then answers 500.
type server struct {
	*httptest.Server
	hits atomic.Int32
	fail atomic.Bool
	body atomic.Value // []byte
	ua   atomic.Value // string

	stall   atomic.Bool
	release chan struct{} // closed at test end to free stalled handlers
}

func newServer(t *testing.T, body []byte) *server {
	t.Helper()
	s := &server{release: make(chan struct{})}
	s.body.Store(body)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		s.ua.Store(r.UserAgent())
		if s.stall.Load() {
			select {
			case <-s.release:
			case <-r.Context().Done():
			}
			return
		}
		if s.fail.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(s.body.Load().([]byte))
	}))
	t.Cleanup(s.Close)
	// Cleanups run last in, first out: this runs before Close, which waits
	// for stalled handlers.
	t.Cleanup(func() { close(s.release) })
	return s
}

func TestServesCacheWithinTTL(t *testing.T) {
	srv := newServer(t, fixture(t, "school.ics"))
	clock := newClock()
	f := New("school", srv.URL, time.Hour, clock.Now)

	first, err := f.Events(context.Background(), ny)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 {
		t.Fatal("no events from fixture")
	}
	clock.Advance(59 * time.Minute)
	second, err := f.Events(context.Background(), ny)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != len(first) {
		t.Errorf("cached call returned %d events, want %d", len(second), len(first))
	}
	if got := srv.hits.Load(); got != 1 {
		t.Errorf("upstream hits = %d, want 1", got)
	}
	if ua := srv.ua.Load().(string); ua != "trmnl-poller" {
		t.Errorf("User-Agent = %q, want trmnl-poller", ua)
	}
}

func TestRefetchesAfterTTL(t *testing.T) {
	srv := newServer(t, fixture(t, "school.ics"))
	clock := newClock()
	f := New("school", srv.URL, time.Hour, clock.Now)

	if _, err := f.Events(context.Background(), ny); err != nil {
		t.Fatal(err)
	}
	clock.Advance(61 * time.Minute)
	if _, err := f.Events(context.Background(), ny); err != nil {
		t.Fatal(err)
	}
	if got := srv.hits.Load(); got != 2 {
		t.Errorf("upstream hits = %d, want 2", got)
	}
}

func TestKeepsLastGoodOnFailure(t *testing.T) {
	srv := newServer(t, fixture(t, "school.ics"))
	clock := newClock()
	f := New("school", srv.URL, time.Hour, clock.Now)

	first, err := f.Events(context.Background(), ny)
	if err != nil {
		t.Fatal(err)
	}
	srv.fail.Store(true)
	clock.Advance(2 * time.Hour)
	got, err := f.Events(context.Background(), ny)
	if err != nil {
		t.Fatalf("want last good events and nil error, got %v", err)
	}
	if len(got) != len(first) {
		t.Errorf("got %d events, want %d", len(got), len(first))
	}
	if srv.hits.Load() != 2 {
		t.Errorf("upstream hits = %d, want 2", srv.hits.Load())
	}
}

func TestKeepsLastGoodOnUnparsableBody(t *testing.T) {
	srv := newServer(t, fixture(t, "school.ics"))
	clock := newClock()
	f := New("school", srv.URL, time.Hour, clock.Now)

	first, err := f.Events(context.Background(), ny)
	if err != nil {
		t.Fatal(err)
	}
	srv.body.Store([]byte("<html>maintenance</html>"))
	clock.Advance(2 * time.Hour)
	got, err := f.Events(context.Background(), ny)
	if err != nil {
		t.Fatalf("want last good events and nil error, got %v", err)
	}
	if len(got) != len(first) {
		t.Errorf("got %d events, want %d", len(got), len(first))
	}
}

func TestErrorWhenNeverFetched(t *testing.T) {
	srv := newServer(t, nil)
	srv.fail.Store(true)
	f := New("school", srv.URL, time.Hour, newClock().Now)

	if _, err := f.Events(context.Background(), ny); err == nil {
		t.Fatal("want an error with no last good copy")
	}
}

func TestRejectsOversizeBody(t *testing.T) {
	big := append([]byte("BEGIN:VCALENDAR\r\n"), bytes.Repeat([]byte("X-PAD:a\r\n"), 700_000)...)
	big = append(big, "END:VCALENDAR\r\n"...)
	if len(big) < 6<<20 {
		t.Fatalf("test body is %d bytes, want at least 6 MiB", len(big))
	}
	srv := newServer(t, big)
	f := New("school", srv.URL, time.Hour, newClock().Now)

	_, err := f.Events(context.Background(), ny)
	if err == nil {
		t.Fatal("want an error for a 6 MiB body")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error %q does not say the body is too large", err)
	}
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func TestLogsNeverContainURL(t *testing.T) {
	const token = "secret-token"

	t.Run("status", func(t *testing.T) {
		buf := captureLogs(t)
		srv := newServer(t, fixture(t, "tripit.ics"))
		url := srv.URL + "/feed/" + token + "/calendar.ics"
		clock := newClock()
		f := New("trips", url, time.Hour, clock.Now)

		if _, err := f.Events(context.Background(), ny); err != nil {
			t.Fatal(err)
		}
		srv.fail.Store(true)
		clock.Advance(2 * time.Hour)
		if _, err := f.Events(context.Background(), ny); err != nil {
			t.Fatal(err)
		}
		// A second feed with no last good copy returns the error itself.
		g := New("trips", url, time.Hour, clock.Now)
		_, err := g.Events(context.Background(), ny)
		if err == nil {
			t.Fatal("want an error")
		}
		assertNoSecret(t, err.Error(), buf.String(), token, srv.URL)
		if !strings.Contains(buf.String(), "trips") {
			t.Errorf("log does not name the feed: %s", buf.String())
		}
		if !strings.Contains(err.Error(), "500") {
			t.Errorf("error %q does not give the status", err)
		}
	})

	t.Run("connection", func(t *testing.T) {
		buf := captureLogs(t)
		srv := newServer(t, fixture(t, "tripit.ics"))
		url := srv.URL + "/feed/" + token + "/calendar.ics"
		srv.Close() // refuse connections
		f := New("trips", url, time.Hour, newClock().Now)

		_, err := f.Events(context.Background(), ny)
		if err == nil {
			t.Fatal("want an error")
		}
		assertNoSecret(t, err.Error(), buf.String(), token, srv.URL)
	})

	t.Run("connection with last good", func(t *testing.T) {
		buf := captureLogs(t)
		srv := newServer(t, fixture(t, "tripit.ics"))
		url := srv.URL + "/feed/" + token + "/calendar.ics"
		clock := newClock()
		f := New("trips", url, time.Hour, clock.Now)
		if _, err := f.Events(context.Background(), ny); err != nil {
			t.Fatal(err)
		}
		srv.Close()
		clock.Advance(2 * time.Hour)
		if _, err := f.Events(context.Background(), ny); err != nil {
			t.Fatal(err)
		}
		if buf.Len() == 0 {
			t.Fatal("want a warning in the log")
		}
		assertNoSecret(t, "", buf.String(), token, srv.URL)
	})
}

func assertNoSecret(t *testing.T, errText, logs, token, base string) {
	t.Helper()
	for _, s := range []string{errText, logs} {
		if strings.Contains(s, token) || strings.Contains(s, base) {
			t.Errorf("output leaks the URL: %q", s)
		}
	}
}

func TestConcurrentCalls(t *testing.T) {
	srv := newServer(t, fixture(t, "school.ics"))
	clock := newClock()
	f := New("school", srv.URL, time.Hour, clock.Now)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			evs, err := f.Events(context.Background(), ny)
			if err != nil || len(evs) == 0 {
				t.Errorf("events = %d, err = %v", len(evs), err)
			}
		}()
	}
	wg.Wait()
	if got := srv.hits.Load(); got != 1 {
		t.Errorf("upstream hits = %d, want 1", got)
	}
}

func TestCallerCannotChangeCache(t *testing.T) {
	srv := newServer(t, fixture(t, "school.ics"))
	f := New("school", srv.URL, time.Hour, newClock().Now)

	first, err := f.Events(context.Background(), ny)
	if err != nil {
		t.Fatal(err)
	}
	want := first[0].Summary
	first[0].Summary = "changed"
	second, err := f.Events(context.Background(), ny)
	if err != nil {
		t.Fatal(err)
	}
	if second[0].Summary != want {
		t.Errorf("cache changed: %q, want %q", second[0].Summary, want)
	}
}

func TestRedirectErrorsNeverContainToken(t *testing.T) {
	const token = "secret-token"

	t.Run("malformed Location", func(t *testing.T) {
		buf := captureLogs(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "http://%zz-"+token)
			w.WriteHeader(http.StatusFound)
		}))
		defer srv.Close()
		f := New("trips", srv.URL+"/feed/"+token, time.Hour, newClock().Now)

		_, err := f.Events(context.Background(), ny)
		if err == nil {
			t.Fatal("want an error")
		}
		assertNoSecret(t, err.Error(), buf.String(), token, srv.URL)
		if !strings.Contains(err.Error(), "redirect refused") {
			t.Errorf("error %q is not the generic redirect message", err)
		}
	})

	t.Run("other host", func(t *testing.T) {
		buf := captureLogs(t)
		other := newServer(t, fixture(t, "school.ics"))
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", other.URL+"/"+token)
			w.WriteHeader(http.StatusFound)
		}))
		defer srv.Close()
		f := New("trips", srv.URL+"/feed/"+token, time.Hour, newClock().Now)

		_, err := f.Events(context.Background(), ny)
		if err == nil {
			t.Fatal("want an error")
		}
		assertNoSecret(t, err.Error(), buf.String(), token, srv.URL)
		if !strings.Contains(err.Error(), "redirect refused") {
			t.Errorf("error %q is not the generic redirect message", err)
		}
		if other.hits.Load() != 0 {
			t.Error("the client followed a redirect to another host")
		}
	})

	t.Run("scheme change on the same host", func(t *testing.T) {
		// The test server speaks http. A redirect to https on the same
		// host:port changes only the scheme. The reverse (https to http)
		// would send the token in cleartext. Both are refused.
		buf := captureLogs(t)
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.Header().Set("Location", "https://"+r.Host+"/"+token)
			w.WriteHeader(http.StatusFound)
		}))
		defer srv.Close()
		f := New("trips", srv.URL+"/feed/"+token, time.Hour, newClock().Now)

		_, err := f.Events(context.Background(), ny)
		if err == nil {
			t.Fatal("want an error")
		}
		assertNoSecret(t, err.Error(), buf.String(), token, srv.URL)
		if err.Error() != "feed trips: redirect refused" {
			t.Errorf("error %q is not the generic redirect message", err)
		}
		if hits.Load() != 1 {
			t.Errorf("server hits = %d, want 1: the client followed the redirect", hits.Load())
		}
	})

	t.Run("https to http downgrade is refused", func(t *testing.T) {
		first, err := http.NewRequest(http.MethodGet, "https://feeds.example.invalid/x", nil)
		if err != nil {
			t.Fatal(err)
		}
		next, err := http.NewRequest(http.MethodGet, "http://feeds.example.invalid/x", nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := checkRedirect(next, []*http.Request{first}); !errors.Is(got, errRedirectRefused) {
			t.Errorf("checkRedirect = %v, want errRedirectRefused", got)
		}
		same, _ := http.NewRequest(http.MethodGet, "https://feeds.example.invalid/y", nil)
		if got := checkRedirect(same, []*http.Request{first}); got != nil {
			t.Errorf("same scheme and host refused: %v", got)
		}
	})

	t.Run("same host is followed", func(t *testing.T) {
		body := fixture(t, "school.ics")
		mux := http.NewServeMux()
		mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/new", http.StatusFound)
		})
		mux.HandleFunc("/new", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) })
		srv := httptest.NewServer(mux)
		defer srv.Close()
		f := New("school", srv.URL+"/old", time.Hour, newClock().Now)

		if _, err := f.Events(context.Background(), ny); err != nil {
			t.Fatal(err)
		}
	})
}

func TestStalledUpstreamServesLastGood(t *testing.T) {
	srv := newServer(t, fixture(t, "school.ics"))
	clock := newClock()
	f := New("school", srv.URL, time.Hour, clock.Now)
	f.staleTimeout = 100 * time.Millisecond

	first, err := f.Events(context.Background(), ny)
	if err != nil {
		t.Fatal(err)
	}
	srv.stall.Store(true)
	clock.Advance(2 * time.Hour)

	start := time.Now()
	got, err := f.Events(context.Background(), ny)
	if err != nil {
		t.Fatalf("want last good events, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("stalled refetch took %v, want about the stale timeout", elapsed)
	}
	if len(got) != len(first) {
		t.Errorf("got %d events, want %d", len(got), len(first))
	}
}

func TestStalledUpstreamWithNoCopyUsesFullTimeout(t *testing.T) {
	srv := newServer(t, fixture(t, "school.ics"))
	srv.stall.Store(true)
	f := New("school", srv.URL, time.Hour, newClock().Now)
	f.fullTimeout = 100 * time.Millisecond
	f.staleTimeout = time.Millisecond // must not apply with no copy

	_, err := f.Events(context.Background(), ny)
	if err == nil {
		t.Fatal("want an error")
	}
	if srv.hits.Load() != 1 {
		t.Errorf("upstream hits = %d, want 1", srv.hits.Load())
	}
}

func TestBackoffAfterFailureWithLastGood(t *testing.T) {
	srv := newServer(t, fixture(t, "school.ics"))
	clock := newClock()
	f := New("school", srv.URL, time.Hour, clock.Now)
	f.backoff = 5 * time.Minute

	if _, err := f.Events(context.Background(), ny); err != nil {
		t.Fatal(err)
	}
	srv.fail.Store(true)
	clock.Advance(2 * time.Hour)
	for range 2 {
		if _, err := f.Events(context.Background(), ny); err != nil {
			t.Fatal(err)
		}
	}
	if got := srv.hits.Load(); got != 2 {
		t.Fatalf("hits = %d, want 2 (one fetch, one failed refetch)", got)
	}
	clock.Advance(4 * time.Minute)
	if _, err := f.Events(context.Background(), ny); err != nil {
		t.Fatal(err)
	}
	if got := srv.hits.Load(); got != 2 {
		t.Fatalf("hits = %d inside the back-off, want 2", got)
	}
	clock.Advance(2 * time.Minute)
	if _, err := f.Events(context.Background(), ny); err != nil {
		t.Fatal(err)
	}
	if got := srv.hits.Load(); got != 3 {
		t.Errorf("hits = %d after the back-off, want 3", got)
	}
}

func TestBackoffAfterFailureWithNoCopy(t *testing.T) {
	srv := newServer(t, nil)
	srv.fail.Store(true)
	clock := newClock()
	f := New("school", srv.URL, time.Hour, clock.Now)

	_, first := f.Events(context.Background(), ny)
	if first == nil {
		t.Fatal("want an error")
	}
	clock.Advance(time.Minute)
	_, second := f.Events(context.Background(), ny)
	if second == nil || second.Error() != first.Error() {
		t.Errorf("second error = %v, want the cached %v", second, first)
	}
	if got := srv.hits.Load(); got != 1 {
		t.Fatalf("hits = %d inside the back-off, want 1", got)
	}

	srv.fail.Store(false)
	srv.body.Store(fixture(t, "school.ics"))
	clock.Advance(5 * time.Minute)
	if _, err := f.Events(context.Background(), ny); err != nil {
		t.Fatalf("want a retry after the back-off to succeed, got %v", err)
	}
	if got := srv.hits.Load(); got != 2 {
		t.Errorf("hits = %d, want 2", got)
	}
}

func TestCallerCancelDoesNotFailFetch(t *testing.T) {
	srv := newServer(t, fixture(t, "school.ics"))
	f := New("school", srv.URL, time.Hour, newClock().Now)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Events(ctx, ny); err != nil {
		t.Fatalf("a hung-up caller failed the shared fetch: %v", err)
	}
}
