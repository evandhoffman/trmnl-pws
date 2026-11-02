package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/series"
	"github.com/evandhoffman/trmnl-pws/internal/solar"
)

// readCall records the arguments of one Points call.
type readCall struct {
	entities []string
	start    time.Time
	end      time.Time
	seedBack time.Duration
}

// fakeReader stands in for the InfluxDB client. fn decides each answer from
// the 1-based call number.
type fakeReader struct {
	mu    sync.Mutex
	calls []readCall
	fn    func(n int) (map[string][]series.Point, error)
}

func (f *fakeReader) Points(_ context.Context, entities []string, start, end time.Time, seedBack time.Duration) (map[string][]series.Point, error) {
	f.mu.Lock()
	f.calls = append(f.calls, readCall{entities, start, end, seedBack})
	n := len(f.calls)
	f.mu.Unlock()
	return f.fn(n)
}

func (f *fakeReader) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// synthPoints returns a small invented day of readings for the default
// entities, all inside the fetch window of fixedNow.
func synthPoints() map[string][]series.Point {
	e := solar.DefaultEntities
	at := func(h, m int) time.Time { return time.Date(2026, 11, 1, h, m, 0, 0, ny) }
	return map[string][]series.Point{
		e.SolarKW:    {{T: at(0, 0), V: 0}, {T: at(9, 0), V: 2.5}, {T: at(11, 0), V: 4.0}},
		e.LoadKW:     {{T: at(0, 0), V: 1.0}, {T: at(10, 0), V: 1.5}},
		e.GridKW:     {{T: at(0, 0), V: 0.5}, {T: at(9, 30), V: -1.0}},
		e.CarKW:      {{T: at(0, 0), V: 0}},
		e.ChargePct:  {{T: at(0, 0), V: 40}, {T: at(11, 0), V: 55}},
		e.ReservePct: {{T: at(0, 0), V: 20}},
		e.SolarKWh:   {{T: at(0, 0), V: 0}, {T: at(11, 0), V: 12.0}},
		e.HomeKWh:    {{T: at(0, 0), V: 0}, {T: at(11, 0), V: 9.0}},
		e.GridInKWh:  {{T: at(0, 0), V: 0}, {T: at(11, 0), V: 1.0}},
		e.GridOutKWh: {{T: at(0, 0), V: 0}, {T: at(11, 0), V: 3.0}},
	}
}

func solarHandler(t *testing.T, r *fakeReader, clk *clock, mod func(*Deps)) http.Handler {
	t.Helper()
	captureLogs(t)
	d := testDeps(clk.Now, nil, nil)
	d.Influx = r
	d.Entities = solar.DefaultEntities
	d.NetZeroKW = 0.05
	d.TodayWindowHours = 12
	if mod != nil {
		mod(&d)
	}
	return New(d)
}

func okReader() *fakeReader {
	return &fakeReader{fn: func(int) (map[string][]series.Point, error) { return synthPoints(), nil }}
}

func TestSolarTodayShape(t *testing.T) {
	r := okReader()
	clk := newClock(fixedNow())
	h := solarHandler(t, r, clk, nil)
	rec := do(h, http.MethodGet, "/v1/solar/today", lan, keyed())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type = %q", ct)
	}
	var got solar.Today
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.NowSolarKW == nil || *got.NowSolarKW != "4.0" {
		t.Fatalf("now_solar_kw = %v", got.NowSolarKW)
	}
	if got.BatteryPct == nil || *got.BatteryPct != "55" {
		t.Fatalf("battery_pct = %v", got.BatteryPct)
	}
	if got.GeneratedKWh == nil || *got.GeneratedKWh != "12.0" {
		t.Fatalf("generated_kwh = %v", got.GeneratedKWh)
	}
	if got.Updated != "12:00 PM" || got.DateLabel == "" {
		t.Fatalf("updated %q, date label %q", got.Updated, got.DateLabel)
	}
	if got.StrSolar == "" || got.StrTicks == "" {
		t.Fatal("chart strings are empty")
	}
	// The handler builds with the same builder: the body must match it.
	want := solar.BuildToday(fixedNow(), ny, 12*time.Hour, synthPoints(), solar.DefaultEntities, nil)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body differs from BuildToday:\n got %+v\nwant %+v", got, want)
	}
}

func TestSolarFetchArguments(t *testing.T) {
	r := okReader()
	h := solarHandler(t, r, newClock(fixedNow()), nil)
	do(h, http.MethodGet, "/v1/solar/today", lan, keyed())
	if r.count() != 1 {
		t.Fatalf("calls = %d", r.count())
	}
	c := r.calls[0]
	if !reflect.DeepEqual(c.entities, solar.DefaultEntities.All()) {
		t.Fatalf("entities = %v", c.entities)
	}
	wantStart := time.Date(2026, 10, 25, 0, 0, 0, 0, ny)
	if !c.start.Equal(wantStart) || !c.end.Equal(fixedNow()) || c.seedBack != 400*24*time.Hour {
		t.Fatalf("window = %v to %v, seed %v", c.start, c.end, c.seedBack)
	}
}

func TestSolarWeekShape(t *testing.T) {
	r := okReader()
	h := solarHandler(t, r, newClock(fixedNow()), nil)
	rec := do(h, http.MethodGet, "/v1/solar/week", lan, keyed())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type = %q", ct)
	}
	var got solar.Week
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Days) != 8 {
		t.Fatalf("days = %d", len(got.Days))
	}
	if got.Updated != "12:00 PM" {
		t.Fatalf("updated = %q", got.Updated)
	}
	today := got.Days[len(got.Days)-1]
	if today.SolarKWh == nil || *today.SolarKWh != "12.0" {
		t.Fatalf("today solar_kwh = %v", today.SolarKWh)
	}
	// A day with no readings is null, never zero.
	if first := got.Days[0]; first.SolarKWh != nil {
		t.Fatalf("empty day solar_kwh = %v", *first.SolarKWh)
	}
	if !strings.Contains(rec.Body.String(), `"solar_kwh":null`) {
		t.Fatal("body has no null solar_kwh")
	}
}

func TestSolarTodayUsesSunEventsWhenCoordinatesSet(t *testing.T) {
	lat, lon := 40.0, -75.0
	h := solarHandler(t, okReader(), newClock(fixedNow()), func(d *Deps) {
		d.Latitude, d.Longitude = &lat, &lon
	})
	rec := do(h, http.MethodGet, "/v1/solar/today", lan, keyed())
	var got solar.Today
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.StrSun == "" || got.StrSun == "[]" || got.StrSun == "null" {
		t.Fatalf("str_sun = %q, want sun marks", got.StrSun)
	}
	// Without coordinates the chart has no sun marks.
	h = solarHandler(t, okReader(), newClock(fixedNow()), nil)
	rec = do(h, http.MethodGet, "/v1/solar/today", lan, keyed())
	got = solar.Today{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.StrSun != "[]" {
		t.Fatalf("str_sun without coordinates = %q", got.StrSun)
	}
}

func TestSolarPathsShareOneFetch(t *testing.T) {
	r := okReader()
	clk := newClock(fixedNow())
	h := solarHandler(t, r, clk, nil)
	do(h, http.MethodGet, "/v1/solar/today", lan, keyed())
	clk.Advance(59 * time.Second)
	do(h, http.MethodGet, "/v1/solar/week", lan, keyed())
	if r.count() != 1 {
		t.Fatalf("calls within 60 s = %d, want 1", r.count())
	}
	clk.Advance(2 * time.Second) // 61 s after the fetch
	do(h, http.MethodGet, "/v1/solar/week", lan, keyed())
	if r.count() != 2 {
		t.Fatalf("calls after 60 s = %d, want 2", r.count())
	}
}

func TestSolarCachedRequestUsesCurrentNow(t *testing.T) {
	r := okReader()
	clk := newClock(fixedNow())
	h := solarHandler(t, r, clk, nil)
	do(h, http.MethodGet, "/v1/solar/week", lan, keyed())
	clk.Advance(30 * time.Second)
	rec := do(h, http.MethodGet, "/v1/solar/week", lan, keyed())
	var got solar.Week
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Updated != "12:00 PM" {
		t.Fatalf("updated = %q", got.Updated)
	}
	clk.Advance(20 * time.Second)
	if r.count() != 1 {
		t.Fatalf("calls = %d", r.count())
	}
	rec = do(h, http.MethodGet, "/v1/solar/today", lan, keyed())
	var today solar.Today
	if err := json.Unmarshal(rec.Body.Bytes(), &today); err != nil {
		t.Fatal(err)
	}
	want := solar.BuildToday(fixedNow().Add(50*time.Second), ny, 12*time.Hour, synthPoints(), solar.DefaultEntities, nil)
	if !reflect.DeepEqual(today, want) {
		t.Fatal("cached body is not built with the current time")
	}
}

func TestSolarStale(t *testing.T) {
	logs := captureLogs(t)
	r := &fakeReader{fn: func(n int) (map[string][]series.Point, error) {
		if n == 1 {
			return synthPoints(), nil
		}
		return nil, errors.New("influx: request: dial tcp secret-host.invalid:8086: connection refused token=TOKEN-XYZ")
	}}
	clk := newClock(fixedNow())
	d := testDeps(clk.Now, nil, nil)
	d.Influx, d.Entities, d.NetZeroKW, d.TodayWindowHours = r, solar.DefaultEntities, 0.05, 12
	h := New(d)

	first := do(h, http.MethodGet, "/v1/solar/week", lan, keyed())
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}
	clk.Advance(2 * time.Minute) // refresh is due, and it fails
	second := do(h, http.MethodGet, "/v1/solar/today", lan, keyed())
	if second.Code != http.StatusOK {
		t.Fatalf("stale status = %d: %s", second.Code, second.Body)
	}
	if r.count() != 2 {
		t.Fatalf("calls = %d, want a refresh attempt", r.count())
	}
	if n := logs.count(t, "solar refresh failed"); n != 1 {
		t.Fatalf("refresh warnings = %d\n%s", n, logs.String())
	}
	clk.Advance(13 * time.Minute) // 15 min after the fetch: still served
	if rec := do(h, http.MethodGet, "/v1/solar/week", lan, keyed()); rec.Code != http.StatusOK {
		t.Fatalf("status at 15 min = %d", rec.Code)
	}
	clk.Advance(time.Second) // past 15 min
	rec := do(h, http.MethodGet, "/v1/solar/week", lan, keyed())
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status past 15 min = %d", rec.Code)
	}
	for _, secret := range []string{"secret-host", "TOKEN-XYZ"} {
		if strings.Contains(logs.String(), secret) || strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("output leaks %q", secret)
		}
	}
}

func TestSolarRecoversAfterFailure(t *testing.T) {
	r := &fakeReader{fn: func(n int) (map[string][]series.Point, error) {
		if n == 2 || n >= 4 {
			return nil, errors.New("boom")
		}
		return synthPoints(), nil
	}}
	clk := newClock(fixedNow())
	h := solarHandler(t, r, clk, nil)
	do(h, http.MethodGet, "/v1/solar/week", lan, keyed())
	clk.Advance(2 * time.Minute)
	do(h, http.MethodGet, "/v1/solar/week", lan, keyed()) // call 2 fails, serves stale
	clk.Advance(10 * time.Minute)
	if rec := do(h, http.MethodGet, "/v1/solar/week", lan, keyed()); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code) // call 3 succeeds at 12 min
	}
	// Call 4 fails at 26 min. The copy from 12 min is 14 min old: served.
	// A copy from the first fetch would be 26 min old: 503.
	clk.Advance(14 * time.Minute)
	if rec := do(h, http.MethodGet, "/v1/solar/week", lan, keyed()); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestSolarDown(t *testing.T) {
	logs := captureLogs(t)
	r := &fakeReader{fn: func(int) (map[string][]series.Point, error) {
		return nil, errors.New("influx: status 500: token=TOKEN-XYZ at http://secret-host.invalid:8086")
	}}
	clk := newClock(fixedNow())
	d := testDeps(clk.Now, nil, nil)
	d.Influx, d.Entities, d.NetZeroKW, d.TodayWindowHours = r, solar.DefaultEntities, 0.05, 12
	h := New(d)
	for _, path := range []string{"/v1/solar/today", "/v1/solar/week"} {
		rec := do(h, http.MethodGet, path, lan, keyed())
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d", path, rec.Code)
		}
		if json.Valid(rec.Body.Bytes()) {
			t.Fatalf("%s body is JSON: %s", path, rec.Body)
		}
		for _, secret := range []string{"TOKEN-XYZ", "secret-host"} {
			if strings.Contains(rec.Body.String(), secret) || strings.Contains(logs.String(), secret) {
				t.Fatalf("output leaks %q", secret)
			}
		}
	}
	if !strings.Contains(logs.String(), "status 500") {
		t.Fatalf("log lacks the status:\n%s", logs.String())
	}
}

func TestSolarNeedsKey(t *testing.T) {
	r := okReader()
	h := solarHandler(t, r, newClock(fixedNow()), nil)
	for _, path := range []string{"/v1/solar/today", "/v1/solar/week"} {
		assertNotFound(t, do(h, http.MethodGet, path, lan, nil))
		assertNotFound(t, do(h, http.MethodGet, path, lan, map[string]string{"X-Poller-Key": "wrong"}))
		assertNotFound(t, do(h, http.MethodPost, path, lan, keyed()))
	}
	if r.count() != 0 {
		t.Fatalf("reader called %d times without a key", r.count())
	}
}

func TestSolarWithoutInfluxIs404(t *testing.T) {
	captureLogs(t)
	h := New(testDeps(fixedNow, nil, nil))
	for _, path := range []string{"/v1/solar/today", "/v1/solar/week"} {
		assertNotFound(t, do(h, http.MethodGet, path, lan, keyed()))
	}
}

func TestSolarConcurrentRequestsFetchOnce(t *testing.T) {
	release := make(chan struct{})
	r := &fakeReader{fn: func(int) (map[string][]series.Point, error) {
		<-release
		return synthPoints(), nil
	}}
	h := solarHandler(t, r, newClock(fixedNow()), nil)
	var wg sync.WaitGroup
	codes := make(chan int, 6)
	for i := range 6 {
		path := "/v1/solar/today"
		if i%2 == 1 {
			path = "/v1/solar/week"
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- do(h, http.MethodGet, path, lan, keyed()).Code
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	close(codes)
	for c := range codes {
		if c != http.StatusOK {
			t.Fatalf("status = %d", c)
		}
	}
	if r.count() != 1 {
		t.Fatalf("calls = %d, want 1", r.count())
	}
}

func TestSolarAccessLogHasNoToken(t *testing.T) {
	logs := captureLogs(t)
	r := okReader()
	clk := newClock(fixedNow())
	d := testDeps(clk.Now, nil, nil)
	d.Influx, d.Entities, d.NetZeroKW, d.TodayWindowHours = r, solar.DefaultEntities, 0.05, 12
	h := New(d)
	do(h, http.MethodGet, "/v1/solar/today", lan, keyed())
	if logs.count(t, "request") != 1 {
		t.Fatalf("want one request line:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), testKey) {
		t.Fatal("log holds the key")
	}
}

func TestFailureReason(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "timeout"},
		{fmt.Errorf("influx: request: %w", context.DeadlineExceeded), "timeout"},
		{errors.New("influx: status 503: unavailable: body text"), "status 503"},
		{errors.New("influx: request: dial tcp 10.0.0.1:8086: refused"), "error"},
	}
	for _, tt := range tests {
		if got := failureReason(tt.err); got != tt.want {
			t.Errorf("failureReason(%q) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

// The window length comes from the configured hours.
func TestSolarTodayWindowFromConfig(t *testing.T) {
	h := solarHandler(t, okReader(), newClock(fixedNow()), func(d *Deps) { d.TodayWindowHours = 6 })
	rec := do(h, http.MethodGet, "/v1/solar/today", lan, keyed())
	var got solar.Today
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.WindowMin != 360 {
		t.Fatalf("window_min = %d, want 360", got.WindowMin)
	}
}

// A stale copy is built as of its fetch time, so the screen shows how old the
// data is and step-hold does not carry readings past the fetch.
func TestSolarStaleUsesFetchTime(t *testing.T) {
	r := &fakeReader{fn: func(n int) (map[string][]series.Point, error) {
		if n == 1 {
			return synthPoints(), nil
		}
		return nil, errors.New("boom")
	}}
	clk := newClock(fixedNow()) // 12:00 PM
	h := solarHandler(t, r, clk, nil)
	do(h, http.MethodGet, "/v1/solar/today", lan, keyed())
	clk.Advance(5 * time.Minute)

	rec := do(h, http.MethodGet, "/v1/solar/today", lan, keyed())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var today solar.Today
	if err := json.Unmarshal(rec.Body.Bytes(), &today); err != nil {
		t.Fatal(err)
	}
	if today.Updated != "12:00 PM" {
		t.Fatalf("today updated = %q, want 12:00 PM", today.Updated)
	}
	want := solar.BuildToday(fixedNow(), ny, 12*time.Hour, synthPoints(), solar.DefaultEntities, nil)
	if !reflect.DeepEqual(today, want) {
		t.Fatal("stale body is not built with the fetch time")
	}

	rec = do(h, http.MethodGet, "/v1/solar/week", lan, keyed())
	var week solar.Week
	if err := json.Unmarshal(rec.Body.Bytes(), &week); err != nil {
		t.Fatal(err)
	}
	if week.Updated != "12:00 PM" {
		t.Fatalf("week updated = %q, want 12:00 PM", week.Updated)
	}
}
