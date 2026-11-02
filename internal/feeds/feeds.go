// Package feeds fetches iCal feeds over HTTPS, caches each for a fixed time,
// and keeps serving the last good copy when a fetch fails.
//
// A feed URL can hold a secret token. No error and no log line from this
// package contains the URL.
package feeds

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/ical"
)

const (
	// defaultFullTimeout bounds a fetch when there is no last good copy.
	defaultFullTimeout = 10 * time.Second
	// defaultStaleTimeout bounds a refetch when a last good copy exists. The HTTP
	// server's write deadline is 30 s, so a refetch must leave time to answer.
	defaultStaleTimeout = 3 * time.Second
	// defaultBackoff is how long after a failed fetch the feed does not try again.
	defaultBackoff = 5 * time.Minute

	maxBodyBytes = 5 << 20 // 5 MiB
	userAgent    = "trmnl-poller"
)

var (
	errRedirectRefused = errors.New("redirect refused")
	errTooManyRedirect = errors.New("too many redirects")
)

// Feed is one remote iCal feed with a cache. It is safe for concurrent use.
type Feed struct {
	name   string
	url    string
	ttl    time.Duration
	client *http.Client
	now    func() time.Time

	// Tests shrink these three.
	fullTimeout  time.Duration
	staleTimeout time.Duration
	backoff      time.Duration

	// mu guards the fields below. Events holds mu during the HTTP call, so
	// callers that arrive while a fetch runs wait for it and share its result.
	mu        sync.Mutex
	body      []byte // last good body
	fetchedAt time.Time
	events    []ical.Event // body parsed in eventsLoc
	eventsLoc string

	// lastFailure is the time of the last failed fetch, or zero after a
	// success. lastErr is that failure.
	lastFailure time.Time
	lastErr     error
}

// New returns a Feed. name labels the feed in errors and logs. ttl is how long
// a fetched copy stays fresh. now is the clock.
func New(name, url string, ttl time.Duration, now func() time.Time) *Feed {
	return &Feed{
		name: name,
		url:  url,
		ttl:  ttl,
		client: &http.Client{
			Transport:     redirectGuard{next: http.DefaultTransport},
			CheckRedirect: checkRedirect,
		},
		now:          now,
		fullTimeout:  defaultFullTimeout,
		staleTimeout: defaultStaleTimeout,
		backoff:      defaultBackoff,
	}
}

// Events returns the events of the feed, with dates in loc.
//
// It fetches again when the cached copy is older than the TTL. A fetch that
// fails starts a back-off: for the next few minutes Events does not try
// again. During the back-off it returns the last good events, or, with no
// last good copy, the same error. A refetch with a last good copy gets a
// shorter timeout, so a stalled upstream still leaves time to answer.
//
// The fetch ignores cancellation of ctx. Callers share one fetch, and one
// caller that hangs up must not fail it.
func (f *Feed) Events(ctx context.Context, loc *time.Location) ([]ical.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := f.now()
	switch {
	case f.body != nil && now.Sub(f.fetchedAt) < f.ttl:
		// Fresh.
	case !f.lastFailure.IsZero() && now.Sub(f.lastFailure) < f.backoff:
		if f.body == nil {
			return nil, f.lastErr
		}
	default:
		f.refresh(ctx, loc)
		if f.body == nil {
			return nil, f.lastErr
		}
	}

	if f.eventsLoc != loc.String() {
		events, err := ical.Parse(bytes.NewReader(f.body), loc)
		if err != nil {
			return nil, fmt.Errorf("feed %s: %w", f.name, err)
		}
		f.events, f.eventsLoc = events, loc.String()
	}
	return append([]ical.Event(nil), f.events...), nil
}

// refresh fetches and parses the feed. The caller holds f.mu. On success it
// replaces the cached copy. On failure it starts the back-off and, when a
// last good copy exists, logs a warning.
func (f *Feed) refresh(ctx context.Context, loc *time.Location) {
	timeout := f.fullTimeout
	if f.body != nil {
		timeout = f.staleTimeout
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	body, err := f.fetch(ctx)
	var events []ical.Event
	if err == nil {
		// Parse before the swap: a body that does not parse must not
		// replace the last good copy.
		events, err = ical.Parse(bytes.NewReader(body), loc)
	}
	if err != nil {
		f.lastFailure = f.now()
		f.lastErr = fmt.Errorf("feed %s: %w", f.name, err)
		if f.body != nil {
			slog.Warn("feed fetch failed, serving last good copy",
				"feed", f.name, "error", f.lastErr.Error(),
				"age", f.now().Sub(f.fetchedAt).Round(time.Second).String())
		}
		return
	}
	f.body, f.fetchedAt = body, f.now()
	f.events, f.eventsLoc = events, loc.String()
	f.lastFailure, f.lastErr = time.Time{}, nil
}

// fetch downloads the feed body. Its errors never contain the URL.
func (f *Feed) fetch(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return nil, errors.New("build request")
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		// A *url.Error holds the full URL. Keep only its cause. The cause
		// can still hold text from the server, such as a Location value,
		// so redirect failures become one fixed message.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		if errors.Is(err, errRedirectRefused) || errors.Is(err, errTooManyRedirect) {
			return nil, err
		}
		return nil, fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if len(body) > maxBodyBytes {
		return nil, fmt.Errorf("body too large: over %d bytes", maxBodyBytes)
	}
	return body, nil
}

// checkRedirect follows a redirect only within the host and scheme of the
// first request. A redirect to another host could carry the token to a third
// party. A redirect from https to http on the same host would send it in
// cleartext.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errTooManyRedirect
	}
	if req.URL.Host != via[0].URL.Host || req.URL.Scheme != via[0].URL.Scheme {
		return errRedirectRefused
	}
	return nil
}

// redirectGuard checks the Location of a redirect before the client reads it.
// The client puts a Location it cannot parse into its error text, and that
// text can hold the token. The guard turns that failure into a fixed error.
type redirectGuard struct {
	next http.RoundTripper
}

func (g redirectGuard) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := g.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if loc := resp.Header.Get("Location"); loc != "" {
			if _, perr := req.URL.Parse(loc); perr != nil {
				_ = resp.Body.Close()
				return nil, errRedirectRefused
			}
		}
	}
	return resp, nil
}
