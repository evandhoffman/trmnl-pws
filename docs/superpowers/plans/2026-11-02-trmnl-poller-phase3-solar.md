# trmnl-poller Phase 3 (solar) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Put two new solar screens on the wall from `trmnl-poller`. Solar
Today is a rolling 24-hour power chart with a battery strip. Solar Week is
an 8-day bar chart with a metrics table. Every duration, energy, and mean
carries the last value forward through gaps.

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

**Approved design:** Evan approved both screens on 2026-11-02 from
mockups of 26 Fridays of real data ("ok, yes this looks good. Let's go
with this."). The two prototype templates are in
`docs/superpowers/plans/2026-11-02-phase3-solar/`: `solar-today.liquid`
and `solar-week.liquid`. Task 6 starts from them. Keep the look; change
only what this plan names.

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
  no known value is `null` in JSON and an em dash on the screen.
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
- **Display values are strings formatted in Go (D9):** kWh, kW, and hours
  with one decimal (`"11.0"`), percent as a whole number (`"54"`), table
  times compact (`"1:05p"`), header times long (`"12:50 PM"`).

## Decisions

Each line names what it costs if it is wrong.

- **D1. Go computes the metrics, not Flux.** Flux returns raw points and
  Go runs the step-hold math, so unit tests pin every metric. A day of the
  busiest sensor is about 1,400 points; 8 days of 11 sensors is under
  60,000 rows. Cost: a larger query response.
- **D2. Daily energy totals come from the Tesla counters.**
  `bellmore_solar_generated`, `bellmore_home_usage`,
  `bellmore_grid_imported`, and `bellmore_grid_exported` reset at local
  midnight (checked 2026-11-02), so a day's total is its last reading.
  Only car energy needs integration (`wall_connector_power`, kW). Cost: a
  counter that stops resetting would show running totals.
- **D3. No solar forecast** (Evan, 2026-11-02: "I wouldn't really trust
  forecasts. Let's drop solar forecasts."). The service reads no
  `energy_production_tomorrow*` sensor.
- **D4. The anchors moved.** Exact step-hold on the real data gives
  net-zero hours of **11.8** (2026-10-26), **10.8** (2026-10-28), and
  **10.5** (2026-10-31). The spec's 11.5, 10.6, and 10.8 came from
  1-minute Flux windows. The gap-naive count is about 2 hours on each day.
  Cost: none; exact step-hold is the definition.
- **D5. Solar Today replaces the 7-hour chart** (Evan approved the
  mockups). Window: the **24 hours ending now** (Evan saw midnight-to-
  midnight leave the evening blank, and the battery events he cares about
  fall in the evening). Buckets: 5 minutes, time-weighted. Marks: solar as
  a patterned area, home as a dashed line, grid as a heavy dotted line
  (below zero is export), a legend above the plot, thin lines for sunrise,
  noon, and sunset (both days, where they fall in the window), and a solid
  line at midnight labeled with the day. Under it, a battery-charge strip
  (0–100%, its own axis, never a second y-axis on the power chart) with the
  day's reserve dashed and the battery events (D10) as labeled lines.
  Header: generated today (counter), peak kW today with its time, solar
  now, home now, battery now. Payload: about 12 KB, which polling allows
  (the webhook's 2 KB limit is gone). Cost: confirm on the first real poll
  that TRMNL takes 12 KB; fall back to 10-minute buckets if not.
- **D6. The poller joins the `monitoring` network** (external), as
  `grafana.yml` and `blackbox.yml` do, and reaches `influxdb2:8086` by
  name. Nothing new is published. Cost: none.
- **D7. When InfluxDB fails,** the service serves its cached copy for up
  to 15 minutes. After that it returns 503 and never a page of zeros.
  Cost: a stale screen during an outage; the "updated" time shows it.
- **D8. Coordinates for the sun markers go in the host config file**
  (`latitude`, `longitude`), never in the repo. With none set, the chart
  draws no sun markers.
- **D9. Display strings, not numbers.** The mockups showed `11.0` printed
  as `11`, because a JSON number loses its trailing zero on the way
  through the renderer. Go formats every displayed value. Cost: the
  template cannot do arithmetic on them (it does not need to).
- **D10. Battery events (Evan, 2026-11-02: "the important data are just
  time it reached 100/max, time we started drawing from it, time it reached
  floor"; "we can use max() of the reserve value as that day's value").**
  For one local day, over the step-held charge %:
  - **full** = the first time the charge reached the day's maximum;
  - **drain** = the end of the last span at that maximum, that is, when
    the battery left its max for good (`null` if it is still at max at the
    end of the window). A short dip and recovery does not count;
  - **reserve** = the first time at or after `drain` that the charge is at
    or below the day's **highest** `bellmore_backup_reserve` value (the
    reserve seed looks back 400 days); `null` means "not reached".
  The week table shows all three per day. The Today strip shows each
  event that falls inside its 24 hours, from yesterday and from today.
- **D11. Tick labels and day labels come from the server.** The Today
  x-axis runs in minutes from the window start. Go computes each tick
  position and its wall-clock label (`"6p"`, `"12a"`) and the midnight
  marker, so a daylight saving day labels correctly. The prototype's
  JavaScript tick math is replaced by `str_ticks`.

## Review Focus

1. **A sensor that does not change all day.** The grid sits at 0 kW from
   10:00 to 16:00 with no rows. Expected: those 6 hours count as net zero.
   Pinned in Task 1 (`TestHeldLongGap`) and Task 3.
2. **A daylight saving day.** 2026-11-01 is 25 hours long and 2026-03-08 is
   23. Expected: day windows run local midnight to local midnight, real
   hours are summed (net zero can read 25.0), and the Today ticks keep
   their wall-clock labels. Pinned in Task 3 (`TestWeekDSTFallBack`,
   `TestTodayTicksDST`).
3. **No reading before the window.** A sensor added mid-week, or the car
   charger before its history starts. Expected: `null` for the days with
   no known value, never 0. Pinned in Task 1 and Task 3.
4. **The battery dips and recovers.** Charge reaches 100 at 13:00, dips to
   98 at 14:00, is back at 100 at 14:30, and leaves 100 for good at 17:10.
   Expected: full 1:00p, drain 5:10p. A battery still at max at `now` has
   drain `null`. Pinned in Task 3 (`TestBatteryEvents`).
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
  - `func Max(spans []Span) (float64, bool)`
  - `func FirstAt(spans []Span, keep func(v float64) bool, from time.Time) (time.Time, bool)`
    (the first instant at or after `from` where `keep` holds)
  - `func LastIn(pts []Point, start, end time.Time) (Point, bool)`
  - `func (d date.Date) Start(loc *time.Location) time.Time` (local
    midnight)

- [ ] **Step 1: Write the failing tests.** Use synthetic points in
  `America/New_York`. Every test asserts an absolute number.
  - `TestHeldLongGap`: a seed of 1.2 kW at 23:50 the day before, 0 at
    10:00, 0.8 at 16:00. `Hours(abs < 0.05)` over the day = **6.0**.
  - `TestHeldNoSeed`: the first point at 06:00 and no seed. The spans start
    at 06:00, and `Hours(all)` = **18.0**.
  - `TestKWhStep`: 7.2 kW from 01:00 to 02:30, then 0. `KWh` = **10.8**,
    not the trapezoid value.
  - `TestMeanTimeWeighted`: 0 for 20 minutes, then 3 for 10 minutes, in a
    30-minute window. `Mean` = **1.0**, where the mean of the two points
    is 1.5.
  - `TestFirstAt`: a span that starts before `from` and matches returns
    `from`, not the span start. No match returns `ok == false`.
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
  `entity_id`), never by position.
- [ ] **Step 4: Run the tests and see them pass** (`-race`).
- [ ] **Step 5: Commit, open the PR, merge when green.**

### Task 3: Solar metrics and payloads (`internal/solar`)

**Files:**
- Create: `internal/solar/entities.go`, `internal/solar/battery.go`,
  `internal/solar/week.go`, `internal/solar/today.go`,
  `internal/solar/format.go`, `internal/solar/*_test.go`

**Interfaces:**
- Consumes: `series.*`, `date.Date`, `sun.Event` (Task 4; if Task 4 is not
  merged yet, take `[]sun.Event` as a parameter type defined in Task 4 and
  merge Task 4 first).
- Produces:
  - `type Entities struct { SolarKW, GridKW, LoadKW, CarKW, ChargePct, ReservePct, SolarKWh, HomeKWh, GridInKWh, GridOutKWh string }`
    and `var DefaultEntities` (`bellmore_solar_power`,
    `bellmore_grid_power`, `bellmore_load_power`, `wall_connector_power`,
    `bellmore_percentage_charged`, `bellmore_backup_reserve`,
    `bellmore_solar_generated`, `bellmore_home_usage`,
    `bellmore_grid_imported`, `bellmore_grid_exported`), and
    `func (e Entities) All() []string`.
  - `type BatteryEvents struct { Max float64; Full, Drain, Reserve *time.Time }`
    and `func Battery(charge, reserve []series.Span, end time.Time) BatteryEvents`
    (the D10 rules; `end` is the day's end or `now`).
  - `type Day struct` with JSON tags, every field a `*string`: `label`
    (`Mon 10/26`, today adds ` (now)`), `solar_kwh`, `home_kwh`,
    `grid_in_kwh`, `grid_out_kwh`, `self_powered_pct`, `net_zero_h`,
    `peak_pct`, `full_time`, `drain_time`, `reserve_time`, `car_kwh`.
  - `type Week struct` with `days []Day`, `week_solar_kwh`, `updated`
    (`4:00 PM`), and the bar arrays `str_categories`, `str_solar`,
    `str_load`, `str_grid` (JSON-encoded strings of numbers; grid = in −
    out). `func BuildWeek(today date.Date, now time.Time, loc *time.Location, pts map[string][]series.Point, e Entities, netZeroKW float64) Week`
    returns 8 days: the 7 before today, then today up to `now`.
  - `type Today struct` with the header fields `generated_kwh`,
    `peak_kw`, `peak_time`, `now_solar_kw`, `now_home_kw`, `battery_pct`,
    `reserve_pct` (number or `null`; the chart draws it), `date_label`
    (`Friday, October 30`), `updated`; and the chart fields `str_solar`,
    `str_home`, `str_grid`, `str_battery` (JSON strings of
    `[[minute, value], ...]`, minute = minutes since the window start),
    `str_ticks` (`[[minute, "6p"], ...]`, every 3 wall-clock hours),
    `midnight_min`, `midnight_label` (`Fri 10/30`), `str_sun`
    (`[{label, min, time}]`), `str_batt_events`
    (`[{label: "full"|"draining"|"reserve", min, time}]`).
    `func BuildToday(now time.Time, loc *time.Location, pts map[string][]series.Point, e Entities, sun []sun.Event) Today`.

Metric rules (each with its own test):
- Counter totals (solar, home, grid in, grid out): the last reading inside
  the day (D2).
- `self_powered_pct` = `100 × (1 − grid_in / home)`, clamped to 0–100;
  `null` if home is 0 or missing.
- `net_zero_h` = `Hours(abs(v) < netZeroKW)` over the grid spans.
- `car_kwh` = `KWh` over the car spans, floored at 0.
- `peak_pct`, `full_time`, `drain_time`, `reserve_time` = `Battery(...)`.
- Today's chart buckets: 5 minutes from `now − 24h`, each the `Mean` of
  its spans; a bucket with no spans is left out. Header `peak_kw` is the
  largest solar bucket since local midnight.

- [ ] **Step 1: Write the failing tests.**
  - `TestWeekGapDay`: grid readings of 0.0 at 23:00 the day before, 1.5 at
    07:00, 0.0 at 08:30, −2.0 at 17:00, and 0.0 at 19:00, with no other
    rows. Net zero = 7 + 8.5 + 5 = **20.5** h. The gap-naive count is 0.
  - `TestWeekCounters`: counters that reset at midnight give the day's
    last reading, as `"47.8"`.
  - `TestWeekDSTFallBack`: grid at 0 all of 2026-11-01 (seed only). Net
    zero = **"25.0"**.
  - `TestWeekNoSeed`: car points start on day 3. Days 1–2 have
    `car_kwh == nil`.
  - `TestWeekToday`: today stops at `now`, not at midnight.
  - `TestBatteryEvents`: Review Focus 4, plus: still at max at `now` gives
    drain `nil`; reserve values 40 then 20 in one day use **40**; charge
    reaching 40 at 21:50 gives reserve **9:50p**; never reaching it gives
    `nil`.
  - `TestTodayWindow`: `now` = 16:00 gives buckets from 16:00 the day
    before, minute 0 to minute 1435; `midnight_min` = **480**.
  - `TestTodayTicksDST`: `now` = 2026-11-01 16:00. The ticks keep
    wall-clock labels across the repeated 1 AM hour, and `midnight_min`
    = **480**.
  - `TestTodayEventsTwoDays`: yesterday's drain and reserve and today's
    full all appear, each with its minute.
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
  `app/utils/solar.py`), or nil when the sun does not rise. The Today
  handler calls it for yesterday and today and keeps the events inside the
  window.

- [ ] **Step 1: Write the failing test.** Port the NOAA math in
  `app/utils/solar.py`. Expected values come from running that Python
  function for a **public** point (New York City Hall, 40.7128,
  −74.0060) on 2026-06-21, 2026-11-01, and 2026-12-21. Paste its output
  into the test, and match each time within 1 minute.
- [ ] **Step 2: Run it and see it fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run it and see it pass.**
- [ ] **Step 5: Commit, open the PR, merge when green.** Merge this before
  Task 3 if Task 3 needs the type.

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
    `latitude` and `longitude` (optional, both or neither). Env:
    `INFLUX_URL`, `INFLUX_TOKEN`. Startup refuses an entity ID that fails
    `influx.ValidEntity`, a `net_zero_kw` of 0 or less, and `INFLUX_URL`
    without `INFLUX_TOKEN`. With no `INFLUX_URL`, the solar paths return
    404, the way an unset feed is an empty source.
  - `GET /v1/solar/today` returns `solar.Today` as JSON.
  - `GET /v1/solar/week` returns `solar.Week` as JSON.
  - Each solar path caches its InfluxDB result for 60 s and serves a stale
    copy for up to 15 min when a refresh fails (D7). The key check stays in
    front of both paths.

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
- Modify: `templates/plugins.yml`, `internal/server/fixture_test.go`
  (extend `TestFixtureMatchesServer` to both new templates)
- Delete: `docs/superpowers/plans/2026-11-02-phase3-solar/` (the
  prototypes move into `templates/`)

- [ ] **Step 1: `solar-today/full.liquid`** starts from the prototype
  `solar-today.liquid`. Change only: read `str_ticks` for the x-axis
  ticks and labels (D11) instead of computing them, and read the D9
  strings. `settings.yml` copies the countdown's, with
  `polling_url: https://trmnl-poller.evanhoffman.com/v1/solar/today`,
  `refresh_interval: 15`, `name: Solar Today`. 15 is the floor without
  TRMNL+ (the phase 1 spike notes show 5 stored as 15).
- [ ] **Step 2: `solar-week/full.liquid`** is the prototype
  `solar-week.liquid`, with the `Full` column reading `full_time`.
  `refresh_interval: 60`, `name: Solar Week`.
- [ ] **Step 3: Fixtures.** Write synthetic `fixture.json` files that
  include at least one `null` of each kind, a DST day, and battery events
  from two days. Extend `TestFixtureMatchesServer`, so a fixture key the
  server does not send fails the test.
- [ ] **Step 4: Render both fixtures** with `trmnl render-markup` (as in
  the CI `render` job). Check that nothing is cut off or overlaps and that
  the table fits with no scrolling. Send both images to Evan, then merge
  without waiting (as phase 1's R10). Note: `code.highcharts.com` answers
  403 to a headless browser, so a local screenshot must load Highcharts
  from `cdn.jsdelivr.net/npm/highcharts@11.4.8/` instead. TRMNL's own
  renderer is not affected; leave the template's URLs as they are.
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
  `/v1/solar/week` shows 8 days and no error; `/v1/solar/today` shows four
  series and the size in bytes.
- [ ] **Step 4: Commit and push the lunix changes.**

### Task 8: Real-data check, playlist, and Evan at the wall

- [ ] **Step 1: Check the numbers against an independent calculation.**
  For each complete day in the week window, compute net-zero hours, car
  kWh, peak %, and the full, drain, and reserve times with a step-hold
  script in the session scratchpad that reads raw points with the
  read-only token. Expected: each `/v1/solar/week` value matches within
  0.1 h, 0.1 kWh, 1%, or 5 minutes. Where the window still holds
  2026-10-26, 2026-10-28, or 2026-10-31, those days read 11.8, 10.8, and
  10.5 hours (D4). Record the table in the PR or an issue, never in the
  repo.
- [ ] **Step 2: Check the counters against the Python screen.** Solar kWh
  per day should match the old summary within 5%. The old one integrates
  power; the new one reads the counter.
- [ ] **Step 3: Add both plugins to device 3884's playlist** after the
  existing solar items. Leave the old webhook items (27170, 46019) in
  place.
- [ ] **Step 4: Wait for TRMNL's first poll of each path** (a `Ruby` user
  agent in the nginx log, status 200). TRMNL does not poll while the
  device sleeps, 01:00–05:30. Confirm the Today payload size TRMNL took
  (D5).
- [ ] **Step 5: Hand over to Evan.** Tell him what to look for on each
  screen and which old playlist items to remove once he is happy. Cutover
  (phase 5) stops the Python service later; this phase does not stop it.
- [ ] **Step 6: Clean up.** Delete the scratchpad credential files, and
  file an issue for every finding the reviews deferred.
