package server

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/series"
)

// update rewrites every fixture (countdown and solar) from the server output. Run
//
//	go test ./internal/server -run Fixture -update
//
// then read the diff: the fixtures are the sample data for the TRMNL render.
var update = flag.Bool("update", false, "rewrite every fixture from the server output")

// fixturePath is the sample data for the TRMNL template.
const fixturePath = "../../templates/countdown/fixture.json"

// indented formats a response the way the fixture files are written.
func indented(t *testing.T, body []byte) []byte {
	t.Helper()
	var got bytes.Buffer
	if err := json.Indent(&got, body, "", "  "); err != nil {
		t.Fatalf("indent response: %v", err)
	}
	got.WriteByte('\n')
	return got.Bytes()
}

// compareFixture fails unless the file holds exactly the server output. With
// -update, it writes the output to the file instead.
func compareFixture(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("fixture %s differs from the server output (run with -update to rewrite).\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}

// TestFixtureMatchesServer keeps the template's sample data honest: the
// mockup scenario, served through the real handler with a fixed clock, must
// give the exact content of the fixture. The fixture file is indented for
// reading, so the test indents the response the same way before it compares.
func TestFixtureMatchesServer(t *testing.T) {
	h := newHandler(t)
	rec := do(h, http.MethodGet, "/v1/countdown", lan, keyed())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	compareFixture(t, fixturePath, indented(t, rec.Body.Bytes()))
}

// TestSolarFixturesMatchServer does the same for the two solar templates. The
// readings come from the invented house in solar_scenario_test.go. The coordinates
// are round numbers, not a real place.
func TestSolarFixturesMatchServer(t *testing.T) {
	cases := []struct {
		name string
		path string
		now  time.Time
		// mod changes the readings to add a gap.
		mod func(map[string][]series.Point)
	}{
		{
			// 9:30 PM on the day the clocks went back, so the window holds
			// the repeated hour. The solar counter has no readings, so
			// "generated" is null.
			name: "/v1/solar/today",
			path: "../../templates/solar-today/fixture.json",
			now:  time.Date(2026, 11, 1, 21, 30, 0, 0, ny),
			mod:  func(p map[string][]series.Point) { delete(p, "bellmore_solar_generated") },
		},
		{
			// 2:10 PM on Nov 2: the week holds the 25-hour day.
			name: "/v1/solar/week",
			path: "../../templates/solar-week/fixture.json",
			now:  time.Date(2026, 11, 2, 14, 10, 0, 0, ny),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pts := scenarioPoints(ny, c.now)
			if c.mod != nil {
				c.mod(pts)
			}
			r := &fakeReader{fn: func(int) (map[string][]series.Point, error) { return pts, nil }}
			lat, lon := 40.0, -75.0
			h := solarHandler(t, r, newClock(c.now), func(d *Deps) { d.Latitude, d.Longitude = &lat, &lon })
			rec := do(h, http.MethodGet, c.name, lan, keyed())
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
			}
			compareFixture(t, c.path, indented(t, rec.Body.Bytes()))
		})
	}
}
