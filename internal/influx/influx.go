// Package influx reads step-hold readings from InfluxDB 2 with Flux queries.
//
// The token is a secret. No error from this package contains it, even when
// the server echoes it back.
package influx

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/evandhoffman/trmnl-pws/internal/series"
)

const (
	maxBodyBytes = 32 << 20 // 32 MiB
	maxMsgBytes  = 200      // longest server message kept in an error
	userAgent    = "trmnl-poller"
)

var entityRE = regexp.MustCompile(`^[a-z0-9_]+$`)

// ValidEntity reports whether id is safe to put in a Flux query.
func ValidEntity(id string) bool { return entityRE.MatchString(id) }

// Client queries one InfluxDB bucket. It is safe for concurrent use.
type Client struct {
	baseURL string
	org     string
	bucket  string
	token   string
	http    *http.Client
}

// New returns a Client. timeout bounds each query, including the body read.
func New(baseURL, org, bucket, token string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		org:     org,
		bucket:  bucket,
		token:   token,
		http: &http.Client{
			Timeout: timeout,
			// A redirect could send the token to another host.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Points returns the readings of each entity, sorted by time.
//
// For each entity it returns the last reading before start, which seeds the
// first span, if one exists within seedBack. It then returns every reading in
// [start, end). An entity with no readings is absent from the map.
func (c *Client) Points(ctx context.Context, entities []string, start, end time.Time, seedBack time.Duration) (map[string][]series.Point, error) {
	for _, id := range entities {
		if !ValidEntity(id) {
			return nil, fmt.Errorf("influx: invalid entity id %q", id)
		}
	}
	if len(entities) == 0 {
		return map[string][]series.Point{}, nil
	}
	if !start.Before(end) {
		return nil, errors.New("influx: start is not before end")
	}

	body, err := json.Marshal(map[string]any{
		"query":   c.query(entities, start, end, seedBack),
		"type":    "flux",
		"dialect": map[string]any{"header": true, "annotations": []string{}},
	})
	if err != nil {
		return nil, errors.New("influx: build request")
	}
	data, err := c.post(ctx, body)
	if err != nil {
		return nil, err
	}
	pts, err := parse(data)
	if err != nil {
		return nil, c.scrub(err)
	}
	return pts, nil
}

// query builds the Flux text. Entity IDs are already validated.
func (c *Client) query(entities []string, start, end time.Time, seedBack time.Duration) string {
	quoted := make([]string, len(entities))
	for i, id := range entities {
		quoted[i] = "r.entity_id == " + fluxString(id)
	}
	src := fmt.Sprintf(`from(bucket: %s)`, fluxString(c.bucket))
	filter := fmt.Sprintf(`filter(fn: (r) => r._field == "value" and (%s))`,
		strings.Join(quoted, " or "))
	seedFrom := start.Add(-seedBack)
	return fmt.Sprintf(`seed = %[1]s
  |> range(start: %[3]s, stop: %[4]s)
  |> %[2]s
  |> group(columns: ["entity_id"])
  |> last()
window = %[1]s
  |> range(start: %[4]s, stop: %[5]s)
  |> %[2]s
union(tables: [seed, window])
  |> group(columns: ["entity_id"])
  |> sort(columns: ["_time"])
  |> keep(columns: ["_time", "_value", "entity_id"])
`, src, filter, flux(seedFrom), flux(start), flux(end))
}

// fluxString quotes s as a Flux string literal. It escapes the backslash, the
// double quote, and the "${" that starts string interpolation.
func fluxString(s string) string {
	s = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "${", `\${`).Replace(s)
	return `"` + s + `"`
}

// flux formats t as a Flux time literal.
func flux(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

// post sends the query and returns the response body.
func (c *Client) post(ctx context.Context, body []byte) ([]byte, error) {
	u := c.baseURL + "/api/v2/query?org=" + url.QueryEscape(c.org)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("influx: build request")
	}
	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/csv")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		// A *url.Error holds the URL. Keep only its cause.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return nil, c.scrub(fmt.Errorf("influx: request: %w", err))
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, c.scrub(fmt.Errorf("influx: read body: %w", err))
	}
	if len(data) > maxBodyBytes {
		return nil, fmt.Errorf("influx: response over %d bytes", maxBodyBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, c.scrub(fmt.Errorf("influx: status %d%s", resp.StatusCode, apiMessage(data)))
	}
	return data, nil
}

// apiMessage returns ": code: message" from an InfluxDB JSON error body, or
// an empty string when the body has another shape.
func apiMessage(data []byte) string {
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &e) != nil || (e.Code == "" && e.Message == "") {
		return ""
	}
	msg := e.Message
	if len(msg) > maxMsgBytes {
		msg = msg[:maxMsgBytes]
	}
	return ": " + e.Code + ": " + msg
}

// scrub returns an error with the token removed from its text.
func (c *Client) scrub(err error) error {
	if c.token == "" || !strings.Contains(err.Error(), c.token) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), c.token, "[redacted]"))
}

// parse reads every table of an annotated or plain CSV response. It maps
// columns by header name, once per header row, and returns the points of each
// entity sorted by time.
func parse(data []byte) (map[string][]series.Point, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1 // tables differ in width
	r.ReuseRecord = true

	out := map[string][]series.Point{}
	var timeCol, valueCol, entityCol, errorCol = -1, -1, -1, -1
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("influx: parse csv: %w", err)
		}
		if len(rec) == 0 || strings.HasPrefix(rec[0], "#") {
			continue
		}
		if isHeader(rec) {
			timeCol, valueCol, entityCol, errorCol = column(rec, "_time"), column(rec, "_value"), column(rec, "entity_id"), column(rec, "error")
			continue
		}
		if errorCol >= 0 {
			if errorCol < len(rec) {
				return nil, fmt.Errorf("influx: query failed: %s", rec[errorCol])
			}
			return nil, errors.New("influx: query failed")
		}
		if timeCol < 0 || valueCol < 0 || entityCol < 0 {
			return nil, errors.New("influx: csv row before a header with _time, _value and entity_id")
		}
		if max(timeCol, valueCol, entityCol) >= len(rec) {
			return nil, errors.New("influx: short csv row")
		}
		ts, err := time.Parse(time.RFC3339Nano, rec[timeCol])
		if err != nil {
			return nil, fmt.Errorf("influx: bad _time %q", rec[timeCol])
		}
		v, err := strconv.ParseFloat(rec[valueCol], 64)
		if err != nil {
			return nil, fmt.Errorf("influx: bad _value %q", rec[valueCol])
		}
		id := rec[entityCol]
		out[id] = append(out[id], series.Point{T: ts, V: v})
	}
	for _, pts := range out {
		slices.SortStableFunc(pts, func(a, b series.Point) int { return a.T.Compare(b.T) })
	}
	return out, nil
}

// isHeader reports whether rec is a header row. A header names the result and
// table columns. A data row cannot, because its _time column holds a time.
func isHeader(rec []string) bool {
	if column(rec, "error") >= 0 && column(rec, "reference") >= 0 {
		return true
	}
	return column(rec, "result") >= 0 && column(rec, "table") >= 0 &&
		column(rec, "_time") >= 0 && column(rec, "_value") >= 0 && column(rec, "entity_id") >= 0
}

// column returns the index of name in rec, or -1.
func column(rec []string, name string) int { return slices.Index(rec, name) }
