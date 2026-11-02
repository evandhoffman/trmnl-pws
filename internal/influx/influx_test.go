package influx

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/series"
)

var (
	testStart = time.Date(2026, 10, 23, 4, 0, 0, 0, time.UTC)
	testEnd   = time.Date(2026, 10, 31, 4, 0, 0, 0, time.UTC)
	seedBack  = 400 * 24 * time.Hour
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func TestPointsParse(t *testing.T) {
	body, err := os.ReadFile("testdata/points.csv")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := New(srv.URL, "bellmore", "home", "tok", 5*time.Second)
	got, err := c.Points(context.Background(), []string{"battery_reserve", "solar_power"}, testStart, testEnd, seedBack)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]series.Point{
		"battery_reserve": {
			{T: mustParse(t, "2026-10-23T05:10:00Z"), V: 40.5},
			{T: mustParse(t, "2026-10-25T14:00:00Z"), V: 20},
		},
		"solar_power": {
			{T: mustParse(t, "2026-10-22T09:00:00Z"), V: 300},
			{T: mustParse(t, "2026-10-24T16:00:00.5Z"), V: 1500.25},
			{T: mustParse(t, "2026-10-24T23:30:00Z"), V: 0},
		},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entities, want %d: %v", len(got), len(want), got)
	}
	for id, pts := range want {
		if len(got[id]) != len(pts) {
			t.Fatalf("%s: got %d points, want %d: %v", id, len(got[id]), len(pts), got[id])
		}
		for i, p := range pts {
			if !got[id][i].T.Equal(p.T) || got[id][i].V != p.V {
				t.Errorf("%s[%d] = %v, want %v", id, i, got[id][i], p)
			}
		}
	}
}

func TestPointsRequest(t *testing.T) {
	var gotReq *http.Request
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(",result,table,_time,_value,entity_id\n"))
	}))
	defer srv.Close()

	c := New(srv.URL, "bellmore", "home", "secret-token", 5*time.Second)
	if _, err := c.Points(context.Background(), []string{"battery_reserve", "solar_power"}, testStart, testEnd, seedBack); err != nil {
		t.Fatal(err)
	}
	if gotReq.Method != http.MethodPost || gotReq.URL.Path != "/api/v2/query" {
		t.Errorf("request = %s %s", gotReq.Method, gotReq.URL.Path)
	}
	if org := gotReq.URL.Query().Get("org"); org != "bellmore" {
		t.Errorf("org = %q, want bellmore", org)
	}
	if a := gotReq.Header.Get("Authorization"); a != "Token secret-token" {
		t.Errorf("Authorization = %q", a)
	}
	if a := gotReq.Header.Get("Accept"); a != "application/csv" {
		t.Errorf("Accept = %q", a)
	}
	var q struct {
		Query string `json:"query"`
		Type  string `json:"type"`
	}
	if err := json.Unmarshal(gotBody, &q); err != nil {
		t.Fatal(err)
	}
	if q.Type != "flux" {
		t.Errorf("type = %q, want flux", q.Type)
	}
	for _, want := range []string{
		`bucket: "home"`,
		`"battery_reserve"`,
		`"solar_power"`,
		`r._field == "value"`,
		// start - 400 days, then start.
		`range(start: 2025-09-18T04:00:00Z, stop: 2026-10-23T04:00:00Z)`,
		`range(start: 2026-10-23T04:00:00Z, stop: 2026-10-31T04:00:00Z)`,
		`|> last()
  |> group(columns: ["entity_id"])
  |> sort(columns: ["_time"])
  |> last()`,
		`union(`,
		`sort(columns: ["_time"])`,
	} {
		if !strings.Contains(q.Query, want) {
			t.Errorf("query lacks %q:\n%s", want, q.Query)
		}
	}
	if strings.Contains(q.Query, "secret-token") {
		t.Error("query holds the token")
	}
}

func TestPointsRejectsBadEntity(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	c := New(srv.URL, "bellmore", "home", "tok", 5*time.Second)
	for _, id := range []string{"x\") or true //", "", "Solar", "a.b", "a b"} {
		if _, err := c.Points(context.Background(), []string{"ok_id", id}, testStart, testEnd, seedBack); err == nil {
			t.Errorf("entity %q: want error", id)
		}
	}
	if called {
		t.Error("server got a request")
	}
}

func TestValidEntity(t *testing.T) {
	for id, want := range map[string]bool{
		"solar_power": true, "ev_2": true, "": false, "Solar": false,
		"a-b": false, "a\n": false, `x") or true //`: false,
	} {
		if got := ValidEntity(id); got != want {
			t.Errorf("ValidEntity(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestErrorHidesToken(t *testing.T) {
	const token = "s3cr3t-token-value"
	for name, body := range map[string]string{
		"json": `{"code":"unauthorized","message":"bad token ` + token + `"}`,
		"text": "Token " + token + " is not valid",
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()

			c := New(srv.URL, "bellmore", "home", token, 5*time.Second)
			_, err := c.Points(context.Background(), []string{"solar_power"}, testStart, testEnd, seedBack)
			if err == nil {
				t.Fatal("want error")
			}
			if strings.Contains(err.Error(), token) {
				t.Errorf("error holds the token: %v", err)
			}
			if !strings.Contains(err.Error(), "401") {
				t.Errorf("error lacks the status: %v", err)
			}
		})
	}
}

func TestErrorHidesTokenAcrossCut(t *testing.T) {
	const token = "Zq7kP2mXw9TbV4nR"
	// The token starts at byte 190 and ends at byte 206, across the 200-byte cut.
	msg := strings.Repeat("a", 190) + token + strings.Repeat("b", 50)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		out, _ := json.Marshal(map[string]string{"code": "unauthorized", "message": msg})
		_, _ = w.Write(out)
	}))
	defer srv.Close()

	c := New(srv.URL, "bellmore", "home", token, 5*time.Second)
	_, err := c.Points(context.Background(), []string{"solar_power"}, testStart, testEnd, seedBack)
	if err == nil {
		t.Fatal("want error")
	}
	for i := 0; i+8 <= len(token); i++ {
		if strings.Contains(err.Error(), token[i:i+8]) {
			t.Errorf("error holds token run %q: %v", token[i:i+8], err)
		}
	}
}

func TestErrorTableHidesToken(t *testing.T) {
	const token = "s3cr3t-token-value"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(",error,reference\n,failed to read " + token + ",897\n"))
	}))
	defer srv.Close()

	c := New(srv.URL, "bellmore", "home", token, 5*time.Second)
	_, err := c.Points(context.Background(), []string{"solar_power"}, testStart, testEnd, seedBack)
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("error holds the token: %v", err)
	}
}

func TestBadValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(",result,table,_time,_value,entity_id\n,_result,0,2026-10-24T00:00:00Z,abc,solar_power\n"))
	}))
	defer srv.Close()

	c := New(srv.URL, "bellmore", "home", "tok", 5*time.Second)
	if _, err := c.Points(context.Background(), []string{"solar_power"}, testStart, testEnd, seedBack); err == nil {
		t.Fatal("want error")
	}
}

func TestNoEntitiesSendsNothing(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	c := New(srv.URL, "bellmore", "home", "tok", 5*time.Second)
	got, err := c.Points(context.Background(), nil, testStart, testEnd, seedBack)
	if err != nil || len(got) != 0 || called {
		t.Errorf("got %v, %v, called=%v", got, err, called)
	}
}

func TestTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	const timeout = 200 * time.Millisecond
	c := New(srv.URL, "bellmore", "home", "tok", timeout)
	begin := time.Now()
	_, err := c.Points(context.Background(), []string{"solar_power"}, testStart, testEnd, seedBack)
	if err == nil {
		t.Fatal("want error")
	}
	if took := time.Since(begin); took > timeout+time.Second {
		t.Errorf("took %v, want under %v", took, timeout+time.Second)
	}
}

func TestRedirectRefused(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("redirect target got a request")
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	c := New(srv.URL, "bellmore", "home", "tok", 5*time.Second)
	if _, err := c.Points(context.Background(), []string{"solar_power"}, testStart, testEnd, seedBack); err == nil {
		t.Fatal("want error")
	}
}

func TestFluxString(t *testing.T) {
	for in, want := range map[string]string{
		"home":   `"home"`,
		`a"b`:    `"a\"b"`,
		`a\b`:    `"a\\b"`,
		"a${x}":  `"a\${x}"`,
		"a$b{c}": `"a$b{c}"`,
	} {
		if got := fluxString(in); got != want {
			t.Errorf("fluxString(%q) = %s, want %s", in, got, want)
		}
	}
}
