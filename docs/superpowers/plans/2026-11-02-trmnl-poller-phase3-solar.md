# trmnl-poller Phase 3 (solar) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Put both solar screens on the wall from `trmnl-poller`: the
7-hour power chart, ported, and a 7-day summary that adds Evan's new
metrics. Every duration and energy figure carries the last value forward
through gaps.

**Architecture:** The service reads raw Home Assistant points from
InfluxDB with one Flux query per request, plus a seed point from before
the window. Go code computes every metric with step-hold math: each
reading holds until the next one. Two new paths, `/v1/solar/today` and
`/v1/solar/week`, feed two new polling plugins. The old webhook plugins
stay on the playlist until Evan has seen the new ones on the wall.

**Tech Stack:** Go 1.27 (`net/http`, `encoding/csv`, `log/slog`), the
InfluxDB 2 HTTP query API (no client library), Liquid with Highcharts,
the `trmnl` CLI v0.1.1, Docker Compose and nginx on lunix.

**Spec:** `docs/superpowers/specs/2026-11-01-trmnl-overhaul-design.md`,
sections 4.2, 5.2, 5.3, and 8 (phase 3). Read it first. The phase 1 plan,
`docs/superpowers/plans/2026-11-01-trmnl-poller-phase1-countdown.md`,
shows the patterns this plan reuses.

## Global Constraints

Every phase 1 global constraint still holds (public repo, small PRs,
squash-merge, `America/New_York`, `X-Poller-Key`, port `8790`, `log/slog`
JSON, CI rules, fixture-only previews, the pinned CLI with timeouts, the
lunix `AGENTS.md` rules). In addition:

- **The gap rule (spec 5.3).** Home Assistant writes a value only when it
  changes. Every duration, energy, mean, and peak uses step-hold: a reading
  holds from its time until the next reading. Each query also fetches the
  last reading **before** the window, so the window starts with a known
  value.
- **No reading before the window means no data, not zero.** A metric with
  no known value shows as `null` in JSON and as an em dash on the screen.
- **InfluxDB:** org `bellmore`, bucket `home_assistant/autogen`, field
  `value`. The token comes from env `INFLUX_TOKEN` (1Password item
  `trmnl-pws-influx-readonly-token`: read-only, this one bucket). The base
  URL comes from env `INFLUX_URL`. On lunix it is `http://influxdb2:8086`
  over the `monitoring` Docker network. Never log the token, and never put
  it in an error.
- **Flux injection guard.** Entity IDs come from config and go into Flux
  text. Accept only `^[a-z0-9_]+$`, checked at startup.
- **Real energy data is not committed.** Test fixtures are synthetic. A
  week of grid and car data shows when the house is empty, so real data
  stays in the session scratchpad and is used only in the Task 8 check.
- Numbers on the screen: kWh and hours with one decimal, percent as a whole
  number, times as `1:05 PM`.

## Decisions taken while writing this plan

Each line names what it costs if it is wrong.

- **D1. Go computes the metrics, not Flux.** Flux returns raw points and
  Go runs the step-hold math. Unit tests then pin every metric with
  fixtures. A day of the busiest sensor is about 1,400 points, so 8 days
  of 11 sensors is under 60,000 rows. Cost: a larger query response than
  pre-aggregated Flux.
- **D2. Daily energy totals come from the Tesla counters.**
  `bellmore_solar_generated`, `bellmore_home_usage`,
  `bellmore_grid_imported`, and `bellmore_grid_exported` reset at local
  midnight (checked 2026-11-02), so a day's total is its last reading.
  Only car energy needs integration (`wall_connector_power`, kW). Cost: a
  counter that stops resetting would show running totals.
- **D3. No solar forecast (Evan, 2026-11-02: "I wouldn't really trust
  forecasts. Let's drop solar forecasts.").** This removes the spec's
  proposed "tomorrow's forecast" metric. The service reads no
  `energy_production_tomorrow*` sensor.
- **D4. The anchors moved.** Exact step-hold on the real data gives
  net-zero hours of **11.8** (2026-10-26), **10.8** (2026-10-28), and
  **10.5** (2026-10-31). The spec's 11.5, 10.6, and 10.8 came from
  1-minute Flux windows. The gap-naive count is about 2 hours on each day,
  so the bug the spec names is real. Task 8 checks the live service
  against these values. Cost: none; exact step-hold is the definition.
- **D5. The 7-hour chart is ported as it is on the wall today:** 7 hours,
  30-minute windows, and the same merge-variable names. Arrays stay
  JSON-encoded strings, so the template's script block does not change.
  The window mean is now time-weighted (step-hold), not a mean of the
  points. Today's solar kWh comes from the counter, not an integral.
  Cost: the curve is smoother on days with sparse data.
- **D6. The poller joins the `monitoring` network** (external), as
  `grafana.yml` and `blackbox.yml` do, and reaches `influxdb2:8086` by
  name. Nothing new is published. Cost: none.
- **D7. When InfluxDB fails,** the service serves its cached copy for up
  to 15 minutes. After that it returns 503 and never a page of zeros.
  Cost: a stale screen during an outage. The screen's "updated" time shows
  this.
- **D8. Coordinates for the sun markers go in the host config file**
  (`latitude`, `longitude`), never in the repo. With none set, the chart
  draws no markers, as the Python service does.

## Review Focus

1. **A sensor that does not change all day.** The grid sits at 0 kW from
   10:00 to 16:00 with no rows. Expected: those 6 hours count as net zero.
   Pinned in Task 1 (`TestHeldLongGap`) and Task 3.
2. **A daylight saving day.** 2026-11-01 is 25 hours long and 2026-03-08 is
   23. Expected: day windows run local midnight to local midnight, and
   hours of real time are summed, so net zero can read 24.5. Pinned in
   Task 3 (`TestWeekDSTFallBack`).
3. **No reading before the window.** A sensor added mid-week, or the car
   charger before its history starts. Expected: `null` for the days with
   no known value, never 0. Pinned in Task 1 and Task 3.
4. **The backup reserve changes mid-day.** The reserve is 40% until 12:00
   and 20% after, and the battery reaches 35% at 09:00 and 20% at 18:00.
   Expected: the reserve time reads 9:00 AM, because 35 is at or below the
   40 in force then. Pinned in Task 1 (`TestFirstAtOrBelowMovingFloor`).
5. **InfluxDB is down or slow.** Expected: the cached copy for up to 15
   minutes, then 503. Never zeros, never the token in a log. Pinned in
   Task 2 and Task 5.

---

### Task 1: Step-hold primitives (`internal/series`)

**Files:**
- Create: `internal/series/series.go`, `internal/series/series_test.go`
- Modify: `internal/date/date.go` (add `Start`), `internal/date/date_test.go`

**Interfaces:**
- Produces:
  - `type Point struct { T time.Time; V float64 }`
  - `type Span struct { V float64; Start, End time.Time }`
  - `func Held(pts []Point, start, end time.Time) []Span`. `pts` is sorted
    by time. The last point at or before `start` seeds the first span. With
    no seed, the spans begin at the first point inside the window. Points
    at or after `end` are ignored.
  - `func Hours(spans []Span, keep func(v float64) bool) float64`
  - `func KWh(spans []Span) float64` (kW times hours)
  - `func Mean(spans []Span) (float64, bool)` (time-weighted)
  - `func PeakFirst(spans []Span) (v float64, at time.Time, ok bool)`
    (the highest value, and the first time it was reached)
  - `func FirstAtOrBelow(level, floor []Span) (time.Time, bool)` (the
    first instant where the level is at or below the floor in force at
    that instant)
  - `func LastIn(pts []Point, start, end time.Time) (Point, bool)`
  - `func (d date.Date) Start(loc *time.Location) time.Time` (local
    midnight)

- [ ] **Step 1: Write the failing tests.** Use synthetic points on a fixed
  day in `America/New_York`. Every test asserts an absolute number.
  - `TestHeldLongGap`: a seed of 1.2 kW at 23:50 the day before, 0 at
    10:00, 0.8 at 16:00. `Hours(abs < 0.05)` over the day = **6.0**.
  - `TestHeldNoSeed`: the first point at 06:00 and no seed. The spans start
    at 06:00, and `Hours(all)` = **18.0**.
  - `TestKWhStep`: 7.2 kW from 01:00 to 02:30, then 0. `KWh` = **10.8**,
    not the trapezoid value.
  - `TestMeanTimeWeighted`: 0 for 20 minutes, then 3 for 10 minutes, in a
    30-minute window. `Mean` = **1.0**, where the mean of the two points
    is 1.5.
  - `TestPeakFirst`: 98 at 11:00, 100 at 13:00, 99 at 14:00, 100 at 15:00.
    The result is 100 at **13:00**.
  - `TestFirstAtOrBelowMovingFloor`: the case in Review Focus 4. The
    result is **09:00**. Add the "never reached" case (`ok == false`).
  - `TestStartDST`: `Date{2026, 11, 1}.Start(ny)` to
    `Date{2026, 11, 2}.Start(ny)` spans **25 h**.
- [ ] **Step 2: Run them and see them fail.**
  `go test ./internal/series/ ./internal/date/` fails: undefined names.
- [ ] **Step 3: Implement.** Plain loops over sorted slices, with no
  interpolation anywhere.
- [ ] **Step 4: Run the tests and see them pass.**
  `go test -race ./internal/series/ ./internal/date/` passes, and
  `go vet ./...` and staticcheck are clean.
- [ ] **Step 5: Commit, open the PR, merge when green.**

### Task 2: InfluxDB reader (`internal/influx`)

**Files:**
- Create: `internal/influx/influx.go`, `internal/influx/influx_test.go`,
  `internal/influx/testdata/points.csv` (synthetic)

**Interfaces:**
- Consumes: `series.Point`.
- Produces:
  - `func New(baseURL, org, bucket, token string, timeout time.Duration) *Client`
  - `func (c *Client) Points(ctx context.Context, entities []string, start, end time.Time, seedBack time.Duration) (map[string][]series.Point, error)`.
    It runs **one** Flux query: the union of
    `range(start: start - seedBack, stop: start) |> last()` and
    `range(start: start, stop: end)`, filtered to `_field == "value"` and
    the given `entity_id`s, sorted by time per entity.
  - `func ValidEntity(id string) bool` (`^[a-z0-9_]+$`)

- [ ] **Step 1: Write the failing tests** against an `httptest.Server`:
  - `TestPointsParse`: the server returns `testdata/points.csv` (CSV with
    `Accept: application/csv`, the header row, no annotations). The result
    has the right count and values per entity, sorted, with the seed
    point first.
  - `TestPointsRequest`: the request is `POST /api/v2/query?org=bellmore`
    with `Authorization: Token <token>` and a Flux body that holds each
    entity ID and both ranges.
  - `TestPointsRejectsBadEntity`: `entities = ["x\") or true //"]` returns
    an error before any request is sent.
  - `TestErrorHidesToken`: the server returns 401 and its body echoes the
    token. The error text does not contain the token.
  - `TestTimeout`: the server sleeps past the timeout. `Points` returns an
    error within the timeout plus 1 s.
- [ ] **Step 2: Run them and see them fail.**
- [ ] **Step 3: Implement** with `net/http` and `encoding/csv`. Read at
  most 32 MB of response. Map columns by header name (`_time`, `_value`,
  `entity_id`); never by position.
- [ ] **Step 4: Run the tests and see them pass** (`-race`).
- [ ] **Step 5: Commit, open the PR, merge when green.**

### Task 3: Solar metrics (`internal/solar`)

**Files:**
- Create: `internal/solar/entities.go`, `internal/solar/week.go`,
  `internal/solar/today.go`, `internal/solar/*_test.go`,
  `internal/solar/testdata/` (synthetic point sets as Go literals or JSON)

**Interfaces:**
- Consumes: `series.*`, `date.Date`.
- Produces:
  - `type Entities struct { SolarKW, GridKW, LoadKW, BatteryKW, CarKW, ChargePct, ReservePct, SolarKWh, HomeKWh, GridInKWh, GridOutKWh string }`
    and `var DefaultEntities`, with the sensor names in spec 4.2 and the
    Python config (`bellmore_solar_power`, `bellmore_grid_power`,
    `bellmore_load_power`, `bellmore_battery_power`,
    `wall_connector_power`, `bellmore_percentage_charged`,
    `bellmore_backup_reserve`, `bellmore_solar_generated`,
    `bellmore_home_usage`, `bellmore_grid_imported`,
    `bellmore_grid_exported`).
  - `func (e Entities) All() []string`
  - `type Day struct` with JSON tags: `label` (`Mon 10/26`), `solar_kwh`,
    `home_kwh`, `grid_in_kwh`, `grid_out_kwh`, `self_powered_pct`,
    `net_zero_h`, `peak_pct`, `peak_time`, `reserve_time`, `car_kwh`. Each
    number is a `*float64` and each time a `*string`, so a missing value
    encodes as `null`.
  - `func Week(today date.Date, now time.Time, loc *time.Location, pts map[string][]series.Point, e Entities, netZeroKW float64) []Day`.
    It returns 8 days: the 7 before today, then today up to `now`.
  - `func Chart(pts []series.Point, start, end time.Time, step time.Duration) [][2]float64`
    (`[unix ms, time-weighted mean rounded to 0.1]` per window, labeled
    with the window start; empty windows are left out)

Metric rules (each with its own test):
- Counter totals (solar, home, grid in, grid out): the last reading inside
  the day (D2).
- `self_powered_pct` = `100 × (1 − grid_in / home)`, clamped to 0–100;
  `null` if home is 0 or missing.
- `net_zero_h` = `Hours(abs(v) < netZeroKW)` over the grid spans.
- `car_kwh` = `KWh` over the car spans.
- `peak_pct` and `peak_time` = `PeakFirst` over the charge spans.
- `reserve_time` = `FirstAtOrBelow(charge, reserve)`; `null` means "not
  reached". The reserve seed looks back 400 days (it changes rarely).

- [ ] **Step 1: Write the failing tests.**
  - `TestWeekGapDay`: grid readings of 0.0 at 23:00 the day before, 1.5 at
    07:00, 0.0 at 08:30, −2.0 at 17:00, and 0.0 at 19:00, with no other
    rows. Net zero = 7 + 8.5 + 5 = **20.5** h. The gap-naive count is 0.
  - `TestWeekCounters`: counter readings that reset at midnight give the
    day's last reading.
  - `TestWeekDSTFallBack`: grid at 0 all of 2026-11-01 (seed only). Net
    zero = **25.0**.
  - `TestWeekNoSeed`: car points start on day 3. Days 1–2 have
    `car_kwh == nil`.
  - `TestWeekToday`: today stops at `now`, not at midnight.
  - `TestChartWindows`: the windows and the time-weighted means, using
    the Task 1 numbers.
  - `TestDayJSONNulls`: a `Day` with missing values marshals to `null`.
- [ ] **Step 2: Run them and see them fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run the tests and see them pass.**
- [ ] **Step 5: Commit, open the PR, merge when green.**

### Task 4: Sun markers (`internal/sun`)

**Files:**
- Create: `internal/sun/sun.go`, `internal/sun/sun_test.go`

**Interfaces:**
- Produces: `type Event struct { Label string; At time.Time }` and
  `func Events(d date.Date, lat, lon float64, loc *time.Location) []Event`.
  It returns `Rise`, `Noon`, and `Set` (the labels in
  `app/utils/solar.py`), or nil at the poles when the sun does not rise.

- [ ] **Step 1: Write the failing test.** Port the NOAA math in
  `app/utils/solar.py`. Expected values come from running that Python
  function for a **public** point (New York City Hall, 40.7128,
  −74.0060) on 2026-06-21, 2026-11-01, and 2026-12-21. Paste its output
  into the test, and match each time within 1 minute.
- [ ] **Step 2: Run it and see it fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run it and see it pass.**
- [ ] **Step 5: Commit, open the PR, merge when green.**

### Task 5: Config, handlers, and wiring

**Files:**
- Modify: `internal/config/config.go` (+ test), `internal/server/server.go`
  (+ test), `cmd/trmnl-poller/main.go` (+ test),
  `config/poller.example.yml`
- Create: `internal/server/solar.go`

**Interfaces:**
- Consumes: Tasks 1–4.
- Produces:
  - Config file keys: `influx.org` (default `bellmore`), `influx.bucket`
    (default `home_assistant/autogen`), `solar.entities` (default
    `solar.DefaultEntities`), `solar.net_zero_kw` (default `0.05`),
    `solar.chart_hours` (default 7), `solar.chart_step_minutes`
    (default 30), `latitude` and `longitude` (optional, both or neither).
    Env: `INFLUX_URL`, `INFLUX_TOKEN`. Startup refuses an entity ID that
    fails `influx.ValidEntity`, a `net_zero_kw` of 0 or less, and
    `INFLUX_URL` without `INFLUX_TOKEN`. With no `INFLUX_URL`, the solar
    paths return 404, the way an unset feed is an empty source.
  - `GET /v1/solar/today` returns `current_timestamp`
    (`Monday, November 2, 1:05 PM`), `display_timezone`,
    `str_daily_energy`, `peak_solar_kw`, `peak_solar_time`,
    `js_solar_events` (a JSON string of `[{label, timestamp_ms}]`), and
    `str_bellmore_solar_power`, `str_bellmore_grid_power`,
    `str_bellmore_load_power` (JSON strings of `[[ms, kW], ...]`), named
    from the configured entity IDs. These are the names
    `ui/solar_line_chart.erb` reads.
  - `GET /v1/solar/week` returns `days` (`[]solar.Day`),
    `week_solar_kwh`, `updated` (`1:05 PM`), and
    the bar arrays `str_categories`, `str_solar`, `str_load`, `str_grid`
    (JSON strings; grid = in − out, from the counters).
  - Each solar path caches its InfluxDB result for 60 s and serves a stale
    copy for up to 15 min when a refresh fails (D7). The key check stays
    in front of both paths.

- [ ] **Step 1: Write the failing tests.**
  - Config: the defaults, each refusal above, and that the token is
    absent from every config error.
  - Server: `TestSolarTodayShape` and `TestSolarWeekShape` against a fake
    reader (an interface over `Points`, so no HTTP is needed).
    `TestSolarStale`: the reader fails after one success, so the second
    response is the cached one. `TestSolarDown`: no success, then the
    reader fails, so the response is 503. `TestSolarNeedsKey`: no key
    gives 404.
  - Main: `buildDeps` with `INFLUX_URL` set wires the client.
- [ ] **Step 2: Run them and see them fail.**
- [ ] **Step 3: Implement.** Reuse the countdown handler's pattern: compute
  per request, `slog` one line per request, and never log the token or a
  response body.
- [ ] **Step 4: Run the tests and see them pass** (`go test -race ./...`).
- [ ] **Step 5: Commit, open the PR, merge when green.** The image ships
  on merge. The paths stay dark until Task 7 sets the env on lunix.

### Task 6: Templates, fixtures, and plugins

**Files:**
- Create: `templates/solar-today/{full.liquid,settings.yml,fixture.json}`,
  `templates/solar-week/{full.liquid,settings.yml,fixture.json}`
- Modify: `templates/plugins.yml`,
  `internal/server/fixture_test.go` (extend `TestFixtureMatchesServer` to
  both new templates)

- [ ] **Step 1: `solar-today/full.liquid`** is `ui/solar_line_chart.erb`
  with only the comment line that names the old plugin URL changed.
  `settings.yml` copies the countdown's, with
  `polling_url: https://trmnl-poller.evanhoffman.com/v1/solar/today`,
  `refresh_interval: 15`, `name: Solar Today`. 15 is the floor without
  TRMNL+: the phase 1 spike notes show 5 stored as 15.
- [ ] **Step 2: `solar-week/full.liquid`** is a new layout for 800×480 in
  4 grays: the existing 8-day bar chart on top, then a table with one row
  per day. Columns: day, solar, home, grid in/out, self-powered %,
  net-zero h, peak % @ time, reserve time, car kWh. The title bar shows
  the week's solar kWh and `updated`. A `null`
  renders as `—`. `refresh_interval: 60`, `name: Solar Week`.
- [ ] **Step 3: Fixtures.** Write synthetic `fixture.json` files that
  include at least one `null` of each kind and a DST day. Extend
  `TestFixtureMatchesServer`, so a fixture key the server does not send
  fails the test.
- [ ] **Step 4: Render both fixtures** with `trmnl render-markup` (as in
  the CI `render` job). Check that nothing is cut off or overlaps and that
  the table fits with no scrolling. Send both images to Evan, then merge
  without waiting (as phase 1's R10).
- [ ] **Step 5: Create the two plugins** with
  `trmnl create-plugin-setting` (the TRMNL key comes from 1Password into a
  mode-600 temp file in the session scratchpad, deleted at the end of the
  phase). Write their IDs in `templates/plugins.yml`. CI pushes the
  templates and settings on merge.
- [ ] **Step 6: Commit, open the PR, merge when green.** Check that the CI
  `templates` job pushed both plugins.

### Task 7: lunix deploy

**Files (lunix repo, straight to master per phase 1 R22):**
- Modify: `docker-compose/trmnl-poller.yml` (join the external
  `monitoring` network beside the default one; add `INFLUX_URL`),
  `docker-compose/secrets/trmnl-poller.env.tpl` (add
  `INFLUX_TOKEN={{ op://Code Secrets/trmnl-pws-influx-readonly-token/password }}`),
  `COMPOSE.md` (one line on the network)
- Host only: `/tank/data/configs/etc/trmnl-poller/config.yml` (add
  `latitude` and `longitude`, copied from the Python service's live
  `config/config.yml`)

- [ ] **Step 1: Add `INFLUX_TOKEN` to the secrets file** on lunix from the
  scratchpad token file over `scp`, keeping mode 0600. Never print it.
- [ ] **Step 2: Apply.** Run `docker compose -f trmnl-poller.yml up -d`
  (never `--remove-orphans`), then wait for the container to report
  healthy.
- [ ] **Step 3: Check from lunix with the key.** Read the key from the
  secrets file inside a single remote command, and never echo it.
  `curl -s -H "X-Poller-Key: …" http://127.0.0.1:8790/v1/solar/week | jq`
  shows 8 days and no error. `/v1/solar/today` shows three series.
- [ ] **Step 4: Commit and push the lunix changes.**

### Task 8: Real-data check, playlist, and Evan at the wall

- [ ] **Step 1: Check the numbers against an independent calculation.**
  Compute net-zero hours, car kWh, and peak % with its time for each
  complete day in the week window. Use a script in the session scratchpad
  that reads raw points with the read-only token and applies step-hold.
  Expected: each `/v1/solar/week` value matches within 0.1 h, 0.1 kWh, or
  1%. Where the window still holds 2026-10-26, 2026-10-28, or 2026-10-31,
  those days read 11.8, 10.8, and 10.5 hours (D4). Record the table in
  the PR or issue, never in the repo.
- [ ] **Step 2: Check the counters against the Python screen.** Solar kWh
  per day should match the old summary within 5%. The old one integrates
  power; the new one reads the counter.
- [ ] **Step 3: Add both plugins to device 3884's playlist** after the
  existing solar items. Leave the old webhook items (27170, 46019) in
  place.
- [ ] **Step 4: Wait for TRMNL's first poll of each path** (a `Ruby` user
  agent in the nginx log, status 200). TRMNL does not poll while the
  device sleeps, 01:00–05:30.
- [ ] **Step 5: Hand over to Evan.** Tell him what to look for on each
  screen and which old playlist items to remove once he is happy. Cutover
  (phase 5) stops the Python service later; this phase does not stop it.
- [ ] **Step 6: Clean up.** Delete the scratchpad credential files, and
  file an issue for every finding the reviews deferred.
