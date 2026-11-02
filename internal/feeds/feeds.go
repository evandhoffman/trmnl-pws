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
	requestTimeout = 10 * time.Second
	maxBodyBytes   = 5 << 20 // 5 MiB
	userAgent      = "trmnl-poller"
)

// Feed is one remote iCal feed with a cache. It is safe for concurrent use.
type Feed struct {
	name   string
	url    string
	ttl    time.Duration
	client *http.Client
	now    func() time.Time

	// mu guards the fields below. Events holds mu during the HTTP call, so
	// callers that arrive while a fetch runs wait for it and share its result.
	mu        sync.Mutex
	body      []byte // last good body
	fetchedAt time.Time
	events    []ical.Event // body parsed in eventsLoc
	eventsLoc string
}

// New returns a Feed. name labels the feed in errors and logs. ttl is how long
// a fetched copy stays fresh. now is the clock.
func New(name, url string, ttl time.Duration, now func() time.Time) *Feed {
	return &Feed{
		name:   name,
		url:    url,
		ttl:    ttl,
		client: &http.Client{Timeout: requestTimeout},
		now:    now,
	}
}

// Events returns the events of the feed, with dates in loc. It fetches again
// when the cached copy is older than the TTL. When the fetch fails, it logs a
// warning and returns the last good events. With no last good copy, it returns
// the error.
func (f *Feed) Events(ctx context.Context, loc *time.Location) ([]ical.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.body == nil || f.now().Sub(f.fetchedAt) >= f.ttl {
		body, err := f.fetch(ctx)
		if err == nil {
			// Parse before the swap: a body that does not parse must not
			// replace the last good copy.
			var events []ical.Event
			events, err = ical.Parse(bytes.NewReader(body), loc)
			if err == nil {
				f.body, f.fetchedAt = body, f.now()
				f.events, f.eventsLoc = events, loc.String()
			}
		}
		if err != nil {
			err = fmt.Errorf("feed %s: %w", f.name, err)
			if f.body == nil {
				return nil, err
			}
			slog.Warn("feed fetch failed, serving last good copy",
				"feed", f.name, "error", err.Error(),
				"age", f.now().Sub(f.fetchedAt).Round(time.Second).String())
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

// fetch downloads the feed body. Its errors never contain the URL.
func (f *Feed) fetch(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return nil, errors.New("build request")
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		// A *url.Error holds the full URL. Keep only its cause.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
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
