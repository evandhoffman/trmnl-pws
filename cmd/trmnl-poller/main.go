// Command trmnl-poller serves the TRMNL plugin data.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // zone data for images that have no /usr/share/zoneinfo

	"github.com/evandhoffman/trmnl-pws/internal/config"
	"github.com/evandhoffman/trmnl-pws/internal/feeds"
	"github.com/evandhoffman/trmnl-pws/internal/influx"
	"github.com/evandhoffman/trmnl-pws/internal/server"
)

// version is set at build time: -ldflags "-X main.version=<sha>".
var version = "dev"

const (
	defaultConfigPath = "/etc/trmnl-poller/config.yml"
	healthURL         = "http://127.0.0.1:8790/healthz"
	feedTTL           = time.Hour
	influxTimeout     = 10 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

// run executes one command and returns the process exit code.
// With no command it behaves as serve.
func run(args []string, stdout io.Writer) int {
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	} else if len(args) > 0 && args[0] == "--version" {
		cmd, args = "--version", args[1:]
	}
	switch cmd {
	case "--version":
		fmt.Fprintf(stdout, "trmnl-poller %s\n", version)
		return 0
	case "serve":
		return serve(args, stdout)
	case "healthcheck":
		return healthcheck(healthURL)
	default:
		fmt.Fprintf(stdout, "unknown command: %s\n", cmd)
		return 2
	}
}

// serve loads the config and serves until SIGTERM or SIGINT.
func serve(args []string, stdout io.Writer) int {
	slog.SetDefault(slog.New(slog.NewJSONHandler(stdout, nil)))

	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stdout)
	path := fs.String("config", defaultConfigPath, "path to the optional YAML config file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := config.Load(*path, os.Getenv)
	if err != nil {
		slog.Error("config", "error", err)
		return 1
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		slog.Error("listen", "addr", cfg.Listen, "error", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := serveOn(ctx, ln, cfg, version); err != nil {
		slog.Error("serve", "error", err)
		return 1
	}
	return 0
}

// buildDeps turns a checked config into the handler's dependencies. A feed
// exists only when its URL is set. The same holds for the InfluxDB reader.
func buildDeps(cfg config.Config, ver string) (server.Deps, error) {
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return server.Deps{}, fmt.Errorf("timezone: %w", err)
	}
	d := server.Deps{
		Key:              cfg.Key,
		Loc:              loc,
		Now:              time.Now,
		SchoolLabel:      cfg.SchoolLabel,
		SchoolRules:      cfg.SchoolRules,
		HolidayKeys:      slices.Clone(cfg.HolidayKeys),
		Version:          ver,
		Entities:         cfg.Entities,
		NetZeroKW:        cfg.NetZeroKW,
		TodayWindowHours: cfg.TodayWindowHours,
		Latitude:         cfg.Latitude,
		Longitude:        cfg.Longitude,
	}
	if cfg.TripURL != "" {
		d.Trips = feeds.New("tripit", cfg.TripURL, feedTTL, time.Now)
	}
	if cfg.SchoolURL != "" {
		d.School = feeds.New("school", cfg.SchoolURL, feedTTL, time.Now)
	}
	if cfg.InfluxURL != "" {
		d.Influx = influx.New(cfg.InfluxURL, cfg.InfluxOrg, cfg.InfluxBucket, cfg.InfluxToken, influxTimeout)
	}
	return d, nil
}

// newHTTPServer sets the limits for a server on the public internet.
// The write timeout allows a stalled feed up to 10 s per feed.
func newHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		// Without this, net/http answers "OPTIONS *" with 200 before the
		// handler runs, and that reveals the endpoint without the key.
		DisableGeneralOptionsHandler: true,
	}
}

// serveOn serves on ln until ctx ends, then shuts down within 10 s.
func serveOn(ctx context.Context, ln net.Listener, cfg config.Config, ver string) error {
	deps, err := buildDeps(cfg, ver)
	if err != nil {
		_ = ln.Close()
		return err
	}
	srv := newHTTPServer(ln.Addr().String(), server.New(deps))

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	slog.Info("listening", "addr", ln.Addr().String(), "version", ver)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// healthcheck returns 0 when GET url answers 200 within 3 s, else 1.
// It reads no config, so a container can run it with an empty environment.
func healthcheck(url string) int {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
