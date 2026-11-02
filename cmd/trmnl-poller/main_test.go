package main

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/config"
)

const goodKey = "0123456789abcdef0123456789abcdef"

func TestVersionFlag(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"--version"}, &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := out.String(); got != "trmnl-poller dev\n" {
		t.Fatalf("got %q", got)
	}
}

func TestUnknownSubcommand(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"bogus"}, &out); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(out.String(), "unknown command: bogus") {
		t.Fatalf("got %q", out.String())
	}
}

func TestServeRefusesMissingKey(t *testing.T) {
	t.Setenv("TRMNL_POLLER_KEY", "")
	absent := filepath.Join(t.TempDir(), "absent.yml")
	cases := [][]string{
		nil,
		{"serve"},
		{"serve", "--config", absent},
		{"--config", absent}, // no command: serve
	}
	for _, args := range cases {
		var out bytes.Buffer
		if code := run(args, &out); code != 1 {
			t.Fatalf("%v: exit %d, want 1", args, code)
		}
		if !strings.Contains(out.String(), "TRMNL_POLLER_KEY") {
			t.Fatalf("%v: log does not name the setting: %q", args, out.String())
		}
		if !strings.HasPrefix(out.String(), "{") {
			t.Fatalf("%v: log is not JSON: %q", args, out.String())
		}
	}
}

func TestServeLogNeverHoldsKeyOrURL(t *testing.T) {
	const short = "short-secret-key-value"
	const feedURL = "https://example.invalid/secret-token-path.ics"
	t.Setenv("TRMNL_POLLER_KEY", short)
	t.Setenv("SCHOOL_ICAL_URL", feedURL)
	var out bytes.Buffer
	code := run([]string{"serve", "--config", filepath.Join(t.TempDir(), "absent.yml")}, &out)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	for _, secret := range []string{short, feedURL, "secret-token-path"} {
		if strings.Contains(out.String(), secret) {
			t.Fatalf("log leaks %q: %s", secret, out.String())
		}
	}
}

func TestServeRejectsBadFlag(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"serve", "--nope"}, &out); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestNewHTTPServerSettings(t *testing.T) {
	srv := newHTTPServer(":0", http.NotFoundHandler())
	if srv.ReadTimeout != 10*time.Second ||
		srv.WriteTimeout != 30*time.Second ||
		srv.ReadHeaderTimeout != 5*time.Second ||
		srv.IdleTimeout != 60*time.Second ||
		srv.MaxHeaderBytes != 16<<10 {
		t.Fatalf("timeouts: %+v", srv)
	}
	if !srv.DisableGeneralOptionsHandler {
		t.Fatal("OPTIONS * must reach the handler, which checks the key")
	}
}

func testConfig() config.Config {
	return config.Config{
		Listen:      "127.0.0.1:0",
		Timezone:    "America/New_York",
		SchoolLabel: "School",
		Key:         goodKey,
	}
}

func TestServeAnswersAndShutsDown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveOn(ctx, ln, testConfig(), "test-version") }()

	base := "http://" + ln.Addr().String()
	req, _ := http.NewRequest(http.MethodGet, base+"/v1/countdown", nil)
	req.Header.Set("X-Poller-Key", goodKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("with key: status %d", resp.StatusCode)
	}

	resp, err = http.Get(base + "/v1/countdown")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("without key: status %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveOn: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no shutdown")
	}
}

func TestOptionsStarNeedsKey(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveOn(ctx, ln, testConfig(), "test-version") }()
	defer func() { cancel(); <-done }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte("OPTIONS * HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, " 404 ") {
		t.Fatalf("OPTIONS * answered %q, want 404", line)
	}
}

func TestBuildDepsFeeds(t *testing.T) {
	cfg := testConfig()
	d, err := buildDeps(cfg, "v")
	if err != nil {
		t.Fatal(err)
	}
	if d.Trips != nil || d.School != nil {
		t.Fatal("no URL must mean a nil feed")
	}
	cfg.TripURL = "https://example.invalid/t.ics"
	cfg.SchoolURL = "https://example.invalid/s.ics"
	d, err = buildDeps(cfg, "v")
	if err != nil {
		t.Fatal(err)
	}
	if d.Trips == nil || d.School == nil {
		t.Fatal("a URL must create a feed")
	}
	if d.Loc.String() != "America/New_York" || d.Version != "v" || d.Key != goodKey {
		t.Fatalf("deps: %+v", d)
	}
}

func TestHealthcheck(t *testing.T) {
	tests := []struct {
		name   string
		status int
		code   int
	}{
		{"200", http.StatusOK, 0},
		{"404", http.StatusNotFound, 1},
		{"500", http.StatusInternalServerError, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/healthz" {
					t.Errorf("path %q", r.URL.Path)
				}
				w.WriteHeader(tt.status)
			}))
			defer ts.Close()
			if code := healthcheck(ts.URL + "/healthz"); code != tt.code {
				t.Fatalf("exit %d, want %d", code, tt.code)
			}
		})
	}
	// Nothing listens: the check fails.
	ts := httptest.NewServer(http.NotFoundHandler())
	url := ts.URL + "/healthz"
	ts.Close()
	if code := healthcheck(url); code != 1 {
		t.Fatalf("closed port: exit %d, want 1", code)
	}
}

func TestHealthcheckNeedsNoConfig(t *testing.T) {
	t.Setenv("TRMNL_POLLER_KEY", "")
	var out bytes.Buffer
	// The result depends on whether port 8790 is in use. The point is that
	// healthcheck does not read the config.
	code := run([]string{"healthcheck"}, &out)
	if code != 0 && code != 1 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(out.String(), "TRMNL_POLLER_KEY") {
		t.Fatalf("healthcheck read config: %q", out.String())
	}
}
