# trmnl-poller Phase 1 (foundation and countdown) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Put the new three-column countdown on the wall, served by a new
locked-down Go service that TRMNL polls, with merges reaching the wall
without hands.

**Architecture:** A Go HTTP service (`trmnl-poller`) answers
`GET /v1/countdown` with JSON built from three sources: the TripIt iCal
feed, the university's public Google Calendar, and NYSE holidays computed
in code. It runs on lunix behind nginx at `trmnl-poller.evanhoffman.com`,
locked to TRMNL's IPs and a secret header. CI tests it, ships the image to
GHCR for Watchtower, and pushes the Liquid template and plugin settings to
TRMNL with the `trmnl` CLI.

**Tech Stack:** Go 1.27 (standard library `net/http`, `log/slog`),
`github.com/arran4/golang-ical`, `gopkg.in/yaml.v3`, staticcheck (as a Go
tool), Chainguard images, GitHub Actions (GitHub-hosted), the `trmnl` CLI
v0.1.1, nginx and systemd on lunix, Python + uv for the lunix allowlist
script.

**Spec:** `docs/superpowers/specs/2026-11-01-trmnl-overhaul-design.md`.
Read it first. This plan covers its build-order phases 1 and 2 (spikes and
countdown). Solar, weather and temperature, and cutover get their own
plans.

## Global Constraints

- Go module `github.com/evandhoffman/trmnl-pws`, `go 1.27` in `go.mod`. Go
  code lives at the repo root (`cmd/`, `internal/`) beside the Python
  `app/`, which stays untouched in this phase.
- **The repo is public.** Never commit a LAN address, the public IP, a
  secret, the TripIt URL, the school calendar ID, or real trip data.
  Fixtures are synthetic, except the school excerpt (public data).
- Every change lands on `main` through a small PR, squash-merged. No branch
  lives longer than its PR. Each task below is one PR unless it says
  otherwise.
- Time zone for every date decision: `America/New_York`. Day counts use
  calendar dates, never 24-hour blocks.
- Secret header name: `X-Poller-Key`. The value is at least 32 characters.
- Service port inside and outside the container: `8790`. Public hostname:
  `trmnl-poller.evanhoffman.com`.
- Never log a secret, the TripIt URL, the school calendar URL, or a full
  `settings.yml`. Logs use `log/slog` with JSON output to stdout.
- CI runs on `ubuntu-latest` (this repo is under `evandhoffman`). Every job
  sets `timeout-minutes`. Every artifact sets `retention-days: 3` or less.
  `docker/build-push-action` steps set `DOCKER_BUILD_RECORD_RETENTION_DAYS: 3`.
- CI previews use fixture data only. Never call `get-playlist-item-preview`
  in CI.
- The `trmnl` CLI is pinned to v0.1.1 in CI, and every CLI call has a hard
  timeout (the files endpoint hung for 4 of 5 plugins on 2026-11-01).
- 1Password vault "Code Secrets", items prefixed `trmnl-pws-`.
- lunix changes follow `~/git/lunix/AGENTS.md`: `nginx -t` before every
  reload, reload with
  `docker exec nginx nginx -t && docker exec nginx nginx -s reload`, never
  `--remove-orphans`, `allow`/`deny` only inside `location /`.

## Review Focus

1. **A feed is down** (TripIt 5xx, timeout, or never fetched since start).
   Expected: the countdown still returns three columns from the other
   sources; holidays always fill. Pinned in Task 8 and Task 9.
2. **Late evening, Eastern time** (for example 23:30 EST is 04:30 UTC the
   next day). Expected: day counts follow the local date. Thanksgiving
   reads 1 day at 23:30 the night before, and "Today" only after local
   midnight. Pinned in Task 7.
3. **A timed iCal event near midnight UTC** (TripIt events carry times).
   Expected: the event's date is its local `America/New_York` date. Pinned
   in Task 5.
4. **Two events on the same date from different sources.** Expected: both
   show, ordered trip, school, holiday. Pinned in Task 7.
5. **Only holidays available** (no trip booked, school titles renamed).
   Expected: three holiday columns, and one warning log line saying the
   school source found nothing in 120 days. Pinned in Task 7 and Task 8.

---

### Task 0: Prerequisites from Evan (manual, no PR)

These need Touch ID or a browser. The implementer asks Evan for them and
waits; nothing later can deploy without them.

- [ ] **Step 1: Evan creates the 1Password items**

```bash
op item create --vault "Code Secrets" --category password \
  --title trmnl-pws-poller-key --generate-password='letters,digits,32' \
  'env[text]=TRMNL_POLLER_KEY' 'purpose[text]=X-Poller-Key header for trmnl-poller'
op item create --vault "Code Secrets" --category password \
  --title trmnl-pws-tripit-ical-url 'env[text]=TRIPIT_ICAL_URL'   # paste the URL in the 1Password app, not the terminal
```

The school calendar URL is public; it goes in the item
`trmnl-pws-school-ical-url` the same way, so the calendar ID stays out of
the repo.

- [ ] **Step 2: Evan creates a TRMNL Account API key** (trmnl.com → Account
  → API keys) with capabilities `read` and `content`, and stores it as
  `trmnl-pws-trmnl-api-key`.

- [ ] **Step 3: Load the GitHub Actions secrets** (the implementer may run
  this once Evan has unlocked 1Password; values never print):

```bash
op read "op://Code Secrets/trmnl-pws-trmnl-api-key/password" | gh secret set TRMNL_API_KEY -R evandhoffman/trmnl-pws
op read "op://Code Secrets/trmnl-pws-poller-key/password"    | gh secret set TRMNL_POLLER_KEY -R evandhoffman/trmnl-pws
```

Verify: `gh secret list -R evandhoffman/trmnl-pws` lists both names.

---

### Task 1: Spikes (throwaway; one PR with the notes only)

**Files:**
- Create: `docs/notes/2026-11-phase1-spikes.md`

- [ ] **Step 1: TripIt feed shape.** Fetch the feed with
  `curl -s "$(op read 'op://Code Secrets/trmnl-pws-tripit-ical-url/password')"`
  into the session scratchpad (never the repo). Record, with real values
  replaced by placeholders: how a trip-level event differs from flight,
  hotel, and car events (UID pattern, SUMMARY pattern, `VALUE=DATE` or
  timed); where the destination city appears; whether any `RRULE` exists.
  Write the rule as one sentence the Task 6 code can implement.
- [ ] **Step 2: TRMNL import format.** With the CLI signed in with
  `content`, create a scratch private plugin
  (`trmnl create-plugin-setting`, name `scratch-import-test`), push a
  `settings.yml` with `strategy: polling`, a dummy `polling_url`, a
  `polling_headers` line, `refresh_interval: 60`, and a one-line
  `full.liquid` with `trmnl import-plugin-setting-files <id>`. Read it back
  with `get-plugin-setting-files` and `get-plugin-setting-details`. Record
  the exact request body shape, the `polling_headers` syntax that
  round-trips, which `refresh_interval` values the API accepts, and how long
  each call took. Delete the scratch plugin.
- [ ] **Step 3: `render-markup`.** Render the scratch template with a JSON
  merge-variable payload; record the command, the input shape, and what it
  returns (PNG, HTML, or a URL).
- [ ] **Step 4: Commit the notes and open the PR**

```bash
git switch -c spikes-phase1 origin/main
git add docs/notes/2026-11-phase1-spikes.md
git commit -m "docs: phase 1 spike notes"
gh pr create --fill && gh pr merge --squash --auto
```

---

### Task 2: Go skeleton, version, and the CI check job

**Files:**
- Create: `go.mod`, `cmd/trmnl-poller/main.go`, `cmd/trmnl-poller/main_test.go`,
  `staticcheck.conf`, `.github/workflows/poller.yml`
- Modify: `.gitignore` (add `/trmnl-poller` binary)

**Interfaces:**
- Produces: `func run(args []string, stdout io.Writer) int` in
  `cmd/trmnl-poller`; `var version = "dev"` set by
  `-ldflags "-X main.version=<sha>"`.

- [ ] **Step 1: Write the failing test**

```go
func TestVersionFlag(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"--version"}, &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := out.String(); got != "trmnl-poller dev\n" {
		t.Fatalf("got %q", got)
	}
}
```

- [ ] **Step 2: Run it and see it fail:** `go test ./cmd/...` → FAIL
  (`run` undefined).
- [ ] **Step 3: Implement `run`** with subcommands `serve` (default, filled
  in by Task 10), `healthcheck` (Task 10), and the `--version` flag.
  `go mod init github.com/evandhoffman/trmnl-pws`;
  `go get -tool honnef.co/go/tools/cmd/staticcheck@latest`.
- [ ] **Step 4: Run it and see it pass:** `go test ./...` → `ok`.
- [ ] **Step 5: Add the CI workflow.** `poller.yml`, job `check`,
  `runs-on: ubuntu-latest`, `timeout-minutes: 10`, triggers `push` to `main`
  and `pull_request`, `paths:` `cmd/**`, `internal/**`, `templates/**`,
  `go.mod`, `go.sum`, `poller.Dockerfile`, `.github/workflows/poller.yml`.
  Steps: `actions/checkout`, `actions/setup-go` with
  `go-version-file: go.mod`, then one step that collects status across
  `test -z "$(gofmt -l .)"`, `go mod tidy -diff`, `go vet ./...`, and
  `go tool staticcheck ./...`, then `go test -race ./...`.
- [ ] **Step 6: Commit, PR, merge** once the `check` job is green.

---

### Task 3: Calendar dates (`internal/date`)

**Files:**
- Create: `internal/date/date.go`, `internal/date/date_test.go`

**Interfaces:**
- Produces: `type Date struct{ Year int; Month time.Month; Day int }`;
  `func Of(t time.Time, loc *time.Location) Date`;
  `func (d Date) DaysUntil(o Date) int`; `func (d Date) AddDays(n int) Date`;
  `func (d Date) Before(o Date) bool`; `func (d Date) Weekday() time.Weekday`;
  `func (d Date) Label() string` (for example `"Sat Nov 14"`);
  `func (d Date) String() string` (`"2026-11-14"`).

- [ ] **Step 1: Write the failing tests**

```go
func TestDaysUntilAcrossDSTEnd(t *testing.T) {   // 2026-11-01 is a DST change day
	if got := (Date{2026, 11, 1}).DaysUntil(Date{2026, 11, 26}); got != 25 { t.Fatal(got) }
}
func TestDaysUntilAcrossDSTStart(t *testing.T) {
	if got := (Date{2027, 3, 13}).DaysUntil(Date{2027, 3, 15}); got != 2 { t.Fatal(got) }
}
func TestDaysUntilLeapDay(t *testing.T) {
	if got := (Date{2028, 2, 28}).DaysUntil(Date{2028, 3, 1}); got != 2 { t.Fatal(got) }
}
func TestOfUsesLocalDate(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	utc := time.Date(2026, 11, 26, 4, 30, 0, 0, time.UTC) // 23:30 EST on Nov 25
	if got := Of(utc, ny); got != (Date{2026, 11, 25}) { t.Fatal(got) }
}
func TestLabel(t *testing.T) {
	if got := (Date{2026, 11, 14}).Label(); got != "Sat Nov 14" { t.Fatal(got) }
}
```

- [ ] **Step 2: Run and see them fail:** `go test ./internal/date/` → FAIL.
- [ ] **Step 3: Implement.** `DaysUntil` converts both dates to midnight
  **UTC** and divides the difference by 24 h; UTC has no DST, so the count
  is exact.
- [ ] **Step 4: Run and see them pass.**
- [ ] **Step 5: Commit** (may share a PR with Task 4).

---

### Task 4: NYSE holidays (`internal/holidays`)

**Files:**
- Create: `internal/holidays/holidays.go`, `internal/holidays/holidays_test.go`

**Interfaces:**
- Consumes: `date.Date`.
- Produces: `type Holiday struct{ Key, Name string; Date date.Date }`;
  `func Easter(year int) date.Date`;
  `func ForYear(year int, keys []string) []Holiday` (sorted by date);
  `func Upcoming(from date.Date, keys []string, n int) []Holiday`
  (the next `n` on or after `from`, crossing years);
  `var DefaultKeys = []string{"new_years", "mlk", "presidents", "good_friday", "memorial", "juneteenth", "independence", "labor", "thanksgiving", "christmas", "halloween"}`.
  Unknown keys make `ForYear` panic at startup; config validation (Task
  10) calls `Validate(keys []string) error` first.

Display names: "New Year's Day", "MLK Day", "Presidents' Day",
"Good Friday", "Memorial Day", "Juneteenth", "Independence Day",
"Labor Day", "Thanksgiving", "Christmas", "Halloween". Dates are the
holiday's own date, never the NYSE observed weekday (spec 4.1).

- [ ] **Step 1: Write the failing tests** with these exact dates (checked
  against weekday arithmetic and the 2026 and 2027 Easter dates):

```go
var want2026 = map[string]date.Date{
	"new_years": {2026, 1, 1}, "mlk": {2026, 1, 19}, "presidents": {2026, 2, 16},
	"good_friday": {2026, 4, 3}, "memorial": {2026, 5, 25}, "juneteenth": {2026, 6, 19},
	"independence": {2026, 7, 4}, "labor": {2026, 9, 7}, "thanksgiving": {2026, 11, 26},
	"christmas": {2026, 12, 25}, "halloween": {2026, 10, 31},
}
var want2027 = map[string]date.Date{
	"new_years": {2027, 1, 1}, "mlk": {2027, 1, 18}, "presidents": {2027, 2, 15},
	"good_friday": {2027, 3, 26}, "memorial": {2027, 5, 31}, "juneteenth": {2027, 6, 19},
	"independence": {2027, 7, 4}, "labor": {2027, 9, 6}, "thanksgiving": {2027, 11, 25},
	"christmas": {2027, 12, 25}, "halloween": {2027, 10, 31},
}
func TestEaster(t *testing.T) { /* 2026 → Apr 5, 2027 → Mar 28, 2038 → Apr 25 */ }
func TestUpcomingCrossesYear(t *testing.T) {
	got := Upcoming(date.Date{2026, 12, 26}, DefaultKeys, 2)
	// want New Year's Day 2027-01-01, then MLK Day 2027-01-18
}
func TestUpcomingIncludesToday(t *testing.T) {
	got := Upcoming(date.Date{2026, 11, 26}, DefaultKeys, 1) // Thanksgiving itself
}
```

- [ ] **Step 2: Run and see them fail.**
- [ ] **Step 3: Implement.** Easter uses the anonymous Gregorian
  (Meeus/Jones/Butcher) algorithm. Nth-weekday and last-weekday helpers
  cover MLK (3rd Mon Jan), Presidents' (3rd Mon Feb), Memorial (last Mon
  May), Labor (1st Mon Sep), Thanksgiving (4th Thu Nov).
- [ ] **Step 4: Run and see them pass.**
- [ ] **Step 5: Commit, PR (with Task 3), merge.**

---

### Task 5: iCal parsing (`internal/ical`)

**Files:**
- Create: `internal/ical/ical.go`, `internal/ical/ical_test.go`,
  `internal/ical/testdata/school.ics` (a 10–20 event excerpt of the real
  public feed covering 2026-11-25 to 2027-03-15, including the
  "Winter session" and "Passover" events), `internal/ical/testdata/tripit.ics`
  (synthetic, shaped per the Task 1 notes: one trip-level event, one
  flight, one hotel; fake cities)

**Interfaces:**
- Consumes: `date.Date`.
- Produces: `type Event struct{ UID, Summary, Location, Description string; Start, End date.Date; AllDay bool }`
  where `End` is the **last day the event covers** (inclusive; an all-day
  `DTEND` is exclusive in iCal, so subtract one day);
  `func Parse(r io.Reader, loc *time.Location) ([]Event, error)`.

- [ ] **Step 1: Write the failing tests**

```go
func TestParseSchoolAllDay(t *testing.T) {
	// "No classes (Thanksgiving break)" DTSTART 20261125 DTEND 20261130
	// want Start {2026,11,25}, End {2026,11,29}, AllDay true
}
func TestParseTimedEventUsesLocalDate(t *testing.T) {
	// inline VEVENT: DTSTART:20261115T033000Z (22:30 EST Nov 14)
	// want Start {2026,11,14}
}
func TestParseTZIDEvent(t *testing.T) {
	// DTSTART;TZID=America/Los_Angeles:20261114T220000 → 01:00 EST Nov 15
	// want Start {2026,11,15}
}
func TestParseUnescapesText(t *testing.T) {
	// SUMMARY:Residence halls close at 10 a.m.\, east → "…10 a.m., east"
}
func TestParseRejectsGarbage(t *testing.T) { /* "<html>…" → non-nil error */ }
```

- [ ] **Step 2: Run and see them fail.**
- [ ] **Step 3: Implement `Parse`** on `github.com/arran4/golang-ical`.
  Skip events with no `DTSTART`. If an event has an `RRULE`, log a warning
  with its UID and use only its first occurrence (neither feed had one on
  2026-11-01; spec risk R5).
- [ ] **Step 4: Run and see them pass.**
- [ ] **Step 5: Commit, PR, merge.**

---

### Task 6: Countdown sources (`internal/countdown`)

**Files:**
- Create: `internal/countdown/event.go`, `internal/countdown/school.go`,
  `internal/countdown/trips.go`, `internal/countdown/sources_test.go`

**Interfaces:**
- Consumes: `ical.Event`, `ical.Parse`, `holidays.Holiday`, `date.Date`.
- Produces:
  - `type Source int` with `Trip`, `School`, `Holiday` (this order is the
    tie-break order) and `func (s Source) Label(schoolLabel string) string`
    → `"Trip"`, the configured school label (for example `"Binghamton"`),
    `"Holiday"`.
  - `type Event struct{ Source Source; Name string; Start, End date.Date }`
    (`End` inclusive).
  - `type SchoolRules struct{ Include, Exclude []string }` (regular
    expressions, matched case-insensitively) and
    `var DefaultSchoolRules SchoolRules` with Include
    `^Residence halls close`, `^No classes \(.*break\)`, `^Classes begin`,
    `^Last day of classes`, `^Commencement`; Exclude `winter session`.
  - `func SchoolEvents(evs []ical.Event, r SchoolRules) []Event`
  - `func TripEvents(evs []ical.Event) []Event`
  - `func HolidayEvents(hs []holidays.Holiday) []Event`

School naming and merging (spec 4.1):
- `No classes (Thanksgiving break)` → name `Thanksgiving break`, start on
  its first day.
- `Residence halls close…` on the same date as a kept break → one event
  with the break's name. Alone → name `Halls close`.
- `Commencement ceremonies` → `Commencement`. Other kept titles keep their
  text.

Trips: apply the Task 1 rule to keep only trip-level events. `Name` is the
destination city alone (no state, country, month, airline, hotel, or
confirmation text).

- [ ] **Step 1: Write the failing tests**

```go
func TestSchoolEventsFromFixture(t *testing.T) {
	// testdata/school.ics → in order:
	// {School,"Thanksgiving break",2026-11-25..}, {School,"Last day of classes",2026-12-08},
	// {School,"Halls close",2026-12-17}, {School,"Classes begin",2027-01-19},
	// {School,"Spring break",2027-03-06}
	// and nothing containing "Winter session", "Passover", "Final examinations", "open"
}
func TestTripEventsKeepOnlyTrips(t *testing.T) {
	// testdata/tripit.ics → exactly one Event, Name is the fake city only,
	// Start/End are the trip's first and last days
}
func TestTripNameHasNoBookingDetails(t *testing.T) {
	// a trip-level event whose DESCRIPTION holds a confirmation code and hotel
	// → Name contains neither
}
```

- [ ] **Step 2–4:** fail, implement, pass.
- [ ] **Step 5: Commit, PR, merge.**

---

### Task 7: Column selection (`internal/countdown`)

**Files:**
- Create: `internal/countdown/columns.go`, `internal/countdown/columns_test.go`

**Interfaces:**
- Produces:

```go
type State string // "upcoming", "today", "during"

type Column struct {
	Source    string `json:"source"`     // Source.Label(...)
	Name      string `json:"name"`
	Days      int    `json:"days"`       // 0 for today and during
	State     State  `json:"state"`
	DayOf     int    `json:"day_of,omitempty"`    // during only
	DayTotal  int    `json:"day_total,omitempty"` // during only
	DateLabel string `json:"date"`       // Date.Label() of Start
}

func Columns(today date.Date, trips, school, hols []Event, schoolLabel string, n int) []Column
```

Rules (spec 4.1, Evan's):
1. Candidates per source: trips whose `End >= today`; school and holiday
   events whose `Start >= today`.
2. Take each non-empty source's earliest candidate.
3. Fill the remaining slots, up to `n`, with the earliest unused
   candidates from all sources.
4. Sort by `Start`, ties by source order trip, school, holiday.
5. State: `today` when `Start == today`; `during` when a trip has
   `Start < today <= End` (DayOf = days since Start + 1, DayTotal = span);
   else `upcoming`. A school break counts down to its first day only.

- [ ] **Step 1: Write the failing tests**

```go
func TestMockupScenario(t *testing.T) {
	// today 2026-11-01; trip "Denver" 2026-11-14..18; school fixture; holidays from Upcoming
	// want [Trip Denver 13 upcoming "Sat Nov 14"], [Binghamton "Thanksgiving break" 24],
	//      [Holiday "Thanksgiving" 25]
}
func TestNoTripBackfills(t *testing.T) {
	// today 2026-11-01, no trips → Thanksgiving break 24, Thanksgiving 25,
	// then the earlier of "Last day of classes" (Dec 8) and Christmas → Last day of classes 37
}
func TestOnlyHolidays(t *testing.T) {
	// today 2026-11-01, no trips, no school → Thanksgiving 25, Christmas 54, New Year's Day 61
}
func TestTodayState(t *testing.T) {
	// today 2026-11-26 → holiday column {Name:"Thanksgiving", Days:0, State:"today"}
}
func TestEveningBeforeIsNotToday(t *testing.T) {
	// today = date.Of(2026-11-26T04:30Z, NY) = Nov 25 → Thanksgiving Days 1, upcoming
}
func TestDuringTrip(t *testing.T) {
	// trip 2026-11-14..18, today 2026-11-15 → {State:"during", DayOf:2, DayTotal:5, Days:0}
}
func TestPastEventsNeverShow(t *testing.T) {
	// school event 2026-10-10 "Fall break", today 2026-10-11 → absent; no Days < 0 anywhere
}
func TestSameDayTieOrder(t *testing.T) {
	// trip starting 2026-11-26 and Thanksgiving 2026-11-26 → trip column before holiday column
}
func TestAlwaysThreeColumns(t *testing.T) {
	// property: for 400 consecutive days from 2026-11-01 with no trips and no school,
	// len(Columns(...)) == 3 and every Days >= 0
}
```

- [ ] **Step 2–4:** fail, implement, pass.
- [ ] **Step 5: Commit, PR, merge.**

---

### Task 8: Feed fetching with cache (`internal/feeds`)

**Files:**
- Create: `internal/feeds/feeds.go`, `internal/feeds/feeds_test.go`

**Interfaces:**
- Produces: `type Feed struct{ /* name, url, ttl, client, clock, last good */ }`;
  `func New(name, url string, ttl time.Duration, now func() time.Time) *Feed`;
  `func (f *Feed) Events(ctx context.Context, loc *time.Location) ([]ical.Event, error)`.
  TTL is 1 hour for both feeds. HTTP timeout 10 s; body capped at 5 MiB.
  On a failed fetch it returns the last good events and logs a warning
  with the feed `name` (never the URL). With no last good result it
  returns the error.

- [ ] **Step 1: Write the failing tests** (httptest server, fake clock)

```go
func TestServesCacheWithinTTL(t *testing.T)        // 2 calls inside 1h → 1 upstream hit
func TestRefetchesAfterTTL(t *testing.T)           // advance clock 61m → 2 upstream hits
func TestKeepsLastGoodOnFailure(t *testing.T)      // OK, then 500 after TTL → old events, nil error
func TestErrorWhenNeverFetched(t *testing.T)       // first call 500 → error
func TestLogsNeverContainURL(t *testing.T)         // capture slog output; URL with "secret-token" absent
func TestRejectsOversizeBody(t *testing.T)         // 6 MiB body → error
```

- [ ] **Step 2–4:** fail, implement, pass.
- [ ] **Step 5:** Add the school-silence warning in the countdown builder
  (Task 9): when `SchoolEvents` yields no event within 120 days of today,
  log `"school feed has no upcoming events"` once per fetch. Test it with
  captured logs.
- [ ] **Step 6: Commit, PR, merge.**

---

### Task 9: HTTP server (`internal/server`)

**Files:**
- Create: `internal/server/server.go`, `internal/server/server_test.go`

**Interfaces:**
- Consumes: `feeds.Feed`, `countdown.*`, `holidays.Upcoming`.
- Produces:
  `type Deps struct{ Key string; Loc *time.Location; Now func() time.Time; Trips, School *feeds.Feed; SchoolLabel string; SchoolRules countdown.SchoolRules; HolidayKeys []string; Version string }`;
  `func New(d Deps) http.Handler`.
  Response for `/v1/countdown`:
  `{"columns":[Column,Column,Column],"updated":"7:05 AM"}`.

Behavior:
- Every path requires `X-Poller-Key` equal to `Key`
  (`crypto/subtle.ConstantTimeCompare`). The one exception is
  `GET /healthz` from a loopback address, for the container healthcheck.
- A missing or wrong key, a non-GET method, or an unknown path all get the
  same response: status 404, body `404 page not found\n`.
- `/healthz` → `ok`. `/version` → the version string.
- A feed error makes that source empty; the response is still 200 with
  three columns (holidays fill).
- One access log line per request: method, path, status, duration,
  `X-Real-IP`. Never the key.

- [ ] **Step 1: Write the failing tests**

```go
func TestNoKeyIs404(t *testing.T)
func TestWrongKeyIs404SameBody(t *testing.T)     // body identical to unknown-path 404
func TestPostIs404(t *testing.T)
func TestCountdownOK(t *testing.T)               // fixed clock 2026-11-01 12:00 EST; feeds via httptest;
                                                 // 200, 3 columns, first is the fixture trip
func TestCountdownWithTripFeedDown(t *testing.T) // trip server 500, never fetched → 200, 3 columns, no "Trip"
func TestHealthzLoopbackWithoutKey(t *testing.T) // RemoteAddr 127.0.0.1 → 200 "ok"
func TestHealthzRemoteNeedsKey(t *testing.T)     // RemoteAddr 203.0.113.9 → 404
func TestAccessLogHasNoKey(t *testing.T)
```

- [ ] **Step 2–4:** fail, implement, pass.
- [ ] **Step 5: Commit, PR, merge.**

---

### Task 10: Config and wiring (`internal/config`, `cmd/trmnl-poller`)

**Files:**
- Create: `internal/config/config.go`, `internal/config/config_test.go`,
  `config/poller.example.yml`
- Modify: `cmd/trmnl-poller/main.go`, `cmd/trmnl-poller/main_test.go`

**Interfaces:**
- Produces: `type Config struct{ Listen, Timezone, SchoolLabel string; SchoolRules countdown.SchoolRules; HolidayKeys []string; Key, TripURL, SchoolURL string }`;
  `func Load(path string, getenv func(string) string) (Config, error)`.
  The YAML file is optional (`--config`, default
  `/etc/trmnl-poller/config.yml`); missing file → defaults
  (`Listen ":8790"`, `Timezone "America/New_York"`, `SchoolLabel "School"`,
  `DefaultSchoolRules`, `holidays.DefaultKeys`). Secrets and URLs come
  only from env: `TRMNL_POLLER_KEY`, `TRIPIT_ICAL_URL`, `SCHOOL_ICAL_URL`.

- [ ] **Step 1: Write the failing tests**

```go
func TestLoadDefaultsWithoutFile(t *testing.T)
func TestRefusesMissingKey(t *testing.T)       // error mentions TRMNL_POLLER_KEY
func TestRefusesShortKey(t *testing.T)         // 31 chars → error
func TestRefusesUnknownHoliday(t *testing.T)   // "festivus" → error
func TestRefusesBadRegex(t *testing.T)
func TestEmptyFeedURLDisablesSource(t *testing.T) // TRIPIT_ICAL_URL unset → Trips nil, no error
```

- [ ] **Step 2–4:** fail, implement, pass.
- [ ] **Step 5: Wire `serve`** (load config, build feeds, `server.New`,
  `http.Server` with read/write timeouts of 10 s, graceful shutdown on
  SIGTERM within 10 s) and `healthcheck` (GET `http://127.0.0.1:8790/healthz`,
  exit 0 on 200, else 1).
- [ ] **Step 6: Run locally and look:**

```bash
TRMNL_POLLER_KEY=$(openssl rand -hex 16) SCHOOL_ICAL_URL="$(op read 'op://Code Secrets/trmnl-pws-school-ical-url/password')" \
  go run ./cmd/trmnl-poller serve &
curl -s -H "X-Poller-Key: $TRMNL_POLLER_KEY" localhost:8790/v1/countdown | jq .
```

Expected: three columns; on 2026-11-01 without TripIt: Thanksgiving break
(24), Thanksgiving (25), Last day of classes (37).

- [ ] **Step 7: Commit, PR, merge.**

---

### Task 11: Container image and publishing

**Files:**
- Create: `poller.Dockerfile`
- Modify: `.github/workflows/poller.yml` (add job `image`)

- [ ] **Step 1: Write `poller.Dockerfile`.** Builder
  `cgr.dev/chainguard/go:latest`, `CGO_ENABLED=0 go build -trimpath
  -ldflags "-s -w -X main.version=${VERSION}"`; runtime
  `cgr.dev/chainguard/static:latest`, `USER nonroot`, `EXPOSE 8790`,
  `ENTRYPOINT ["/usr/local/bin/trmnl-poller"]`, `CMD ["serve"]`,
  `HEALTHCHECK CMD ["/usr/local/bin/trmnl-poller","healthcheck"]`. Label
  `org.opencontainers.image.source=https://github.com/evandhoffman/trmnl-pws`.
- [ ] **Step 2: Add the `image` job** (`needs: check`,
  `timeout-minutes: 15`, `permissions: packages: write` only on push to
  `main`). Build the exact target; smoke-run it:

```bash
docker run --rm trmnl-poller:ci --version                          # prints "trmnl-poller <sha>"
docker run -d --name p -e TRMNL_POLLER_KEY=$(openssl rand -hex 16) trmnl-poller:ci
sleep 3 && docker exec p /usr/local/bin/trmnl-poller healthcheck   # exit 0
```

On push to `main` only: push `ghcr.io/evandhoffman/trmnl-poller:<sha>`
and `:latest`.
- [ ] **Step 3: Verify the package is public:**
  `docker logout ghcr.io; docker pull ghcr.io/evandhoffman/trmnl-poller:latest`
  succeeds. If it fails with "unauthorized", ask Evan to set the package
  visibility to public on GitHub (Package settings → Change visibility).
- [ ] **Step 4: Commit, PR, merge;** confirm the `image` job pushed on
  `main`.

---

### Task 12: Countdown template

**Files:**
- Create: `templates/countdown/full.liquid`, `templates/countdown/settings.yml`,
  `templates/countdown/fixture.json`, `templates/plugins.yml`
- Create: `internal/countdown/fixture_test.go`

**Interfaces:**
- `templates/plugins.yml`: `countdown: <plugin setting id>` (filled in Task 14).
- `settings.yml`: `strategy: polling`,
  `polling_url: https://trmnl-poller.evanhoffman.com/v1/countdown`,
  `polling_verb: get`, `polling_headers` with the value placeholder
  `__POLLER_KEY__` in the syntax Task 1 recorded, `refresh_interval: 60`,
  `name: Countdown`.

- [ ] **Step 1: Write the failing test.** `TestFixtureMatchesServer`:
  build the JSON for the mockup scenario (Task 7) with a fixed clock, and
  compare it byte for byte with `templates/countdown/fixture.json`. This
  keeps the template's sample data honest.
- [ ] **Step 2: Run and see it fail** (no fixture yet).
- [ ] **Step 3: Write `fixture.json`** from the server's output, and
  `full.liquid` for layout C: three equal bordered columns in the TRMNL
  framework (`view view--full`, `columns`, `column`), each with the source
  label (small caps), the day count (largest type), `days`, and the event
  name. `state == "today"` shows `Today` in place of the count;
  `state == "during"` shows `Day {{day_of}} of {{day_total}}`. A title bar
  reads `Countdown` and `updated {{updated}}`.
- [ ] **Step 4: Run and see the test pass;** render the template with the
  fixture via `render-markup` (Task 1 notes) and send the image to Evan
  with SendUserFile. Iterate on his comments before merging.
- [ ] **Step 5: Commit, PR, merge.**

---

### Task 13: Template push (`cmd/push-templates`) and its CI job

**Files:**
- Create: `cmd/push-templates/main.go`, `cmd/push-templates/main_test.go`
- Modify: `.github/workflows/poller.yml` (add job `templates`)

**Interfaces:**
- Produces: `func build(dir string, key string) (map[string]string, error)`
  (file name → contents, with `__POLLER_KEY__` replaced) and
  `func push(ctx context.Context, id int, files map[string]string, runner Runner) error`,
  where `Runner` runs `trmnl import-plugin-setting-files <id>` with the
  Task 1 body shape on stdin and a 60 s timeout.

- [ ] **Step 1: Write the failing tests**

```go
func TestBuildSubstitutesKey(t *testing.T)
func TestBuildRefusesEmptyKey(t *testing.T)
func TestBuildRefusesMissingPlaceholder(t *testing.T)  // settings.yml without __POLLER_KEY__ → error
func TestBuildRefusesLeftoverPlaceholder(t *testing.T) // a second placeholder spelling → error
func TestPushErrorNeverContainsKey(t *testing.T)       // runner fails, echoing stdin → error text redacted
func TestPushTimesOut(t *testing.T)                    // runner blocks → error within the timeout
```

- [ ] **Step 2–4:** fail, implement, pass.
- [ ] **Step 5: Add the `templates` job:** on push to `main` with changes
  under `templates/**` or `cmd/push-templates/**`, `timeout-minutes: 10`,
  install `go install github.com/usetrmnl/cli/cmd/trmnl@v0.1.1`, env
  `TRMNL_API_KEY` and `TRMNL_POLLER_KEY` from secrets, run
  `go run ./cmd/push-templates` for every entry in `templates/plugins.yml`.
  On pull requests: render each template with its fixture and upload the
  images (`retention-days: 1`), skipping when the secret is unavailable.
- [ ] **Step 6: Commit, PR, merge.**

---

### Task 14: Deploy on lunix and go live (lunix repo changes plus TRMNL setup)

Work in `~/git/lunix` by its own rules (`AGENTS.md`); its changes are a
separate PR in that repo if its `main` requires one.

**Files (lunix repo):**
- Create: `docker-compose/trmnl-poller.yml`,
  `docker-compose/secrets/trmnl-poller.env.tpl`,
  `etc/nginx/sites-available/trmnl-poller.evanhoffman.com`,
  `etc/nginx/snippets/trmnl-allow.conf` (generated; committed as a seed),
  `trmnl_allowlist/` (uv project: `pyproject.toml`, `src/trmnl_allowlist/__init__.py`,
  `tests/test_render.py`), `systemd/trmnl-allowlist.service`,
  `systemd/trmnl-allowlist.timer`

**Interfaces (allowlist):**
`render(ips: dict[str, list[str]]) -> str` returns `allow <ip>;` lines
for every IPv4 and IPv6 address, then `deny all;`. `main()` fetches
`https://trmnl.com/api/ips` (10 s timeout), and writes the snippet only if
it differs, atomically. Then it runs `docker exec nginx nginx -t`. On a
pass it reloads; on a fail it restores the previous file. It logs one
line per run with the count and whether the list changed.

- [ ] **Step 1: Write the failing allowlist tests** (ruff, `mypy --strict`,
  and pytest per Evan's Python defaults):

```python
def test_render_lists_every_ip_then_denies() -> None: ...
def test_render_refuses_empty_list() -> None: ...          # raises ValueError
def test_render_refuses_non_ip_entry() -> None: ...        # "1.2.3.4; allow all" → ValueError
def test_unchanged_list_writes_nothing(tmp_path) -> None: ...
def test_failed_nginx_test_restores_old_file(tmp_path) -> None: ...  # fake runner returns 1
```

- [ ] **Step 2–4:** fail, implement, pass.
- [ ] **Step 5: Write the stack and site.**
  - Compose: image `ghcr.io/evandhoffman/trmnl-poller:latest`,
    `restart: unless-stopped`, `read_only: true`, `user: "65532:65532"`,
    `cap_drop: [ALL]`, `security_opt: [no-new-privileges:true]`,
    `env_file: secrets/trmnl-poller.env`, `ports: ["8790:8790"]`,
    `stop_grace_period: 15s`.
  - `.env.tpl`: `op://` references for `TRMNL_POLLER_KEY`,
    `TRIPIT_ICAL_URL`, `SCHOOL_ICAL_URL`.
  - Site: copy `sonarr.evanhoffman.com`'s structure; inside `location /`
    use `include snippets/trmnl-allow.conf;` in place of the LAN `allow`
    line; add `limit_req` (define the zone in an http-level `conf.d` file
    if none exists) at 30 requests/minute, burst 10; `proxy_pass` to the
    host's LAN address on 8790; set `X-Real-IP`. The port-80 server
    redirects to HTTPS as the others do.
- [ ] **Step 6: Deploy.** Evan runs
  `./scripts/render-secrets.sh trmnl-poller --push` (Touch ID). Then:

```bash
ssh lunix 'cd ~/git/lunix && git pull --ff-only && cd docker-compose && docker compose -f trmnl-poller.yml up -d'
uv run configsync push            # nginx site and snippet, per configsync/README.md
ssh lunix 'docker exec nginx nginx -t && docker exec nginx nginx -s reload'
ssh lunix 'sudo cp ~/git/lunix/systemd/trmnl-allowlist.* /etc/systemd/system/ && sudo systemctl daemon-reload && sudo systemctl enable --now trmnl-allowlist.timer'
```

- [ ] **Step 7: Verify the lockdown from the LAN** (all must hold):

```bash
curl -s -o /dev/null -w '%{http_code}\n' https://trmnl-poller.evanhoffman.com/v1/countdown   # 403 (LAN IP not allowed)
ssh lunix 'curl -s -o /dev/null -w "%{http_code}\n" localhost:8790/v1/countdown'               # 404 (no key)
ssh lunix 'docker inspect trmnl-poller --format "{{.State.Health.Status}}"'                     # healthy
ssh lunix 'systemctl list-timers trmnl-allowlist.timer --no-pager'                               # next run listed
```

- [ ] **Step 8: Create the TRMNL plugin and go live.**
  `trmnl create-plugin-setting` (private plugin, name `Countdown`); put its
  ID in `templates/plugins.yml` in a trmnl-pws PR; on merge the
  `templates` job pushes the files. Add it to the playlist with
  `trmnl add-device-playlist-item`. Check `get-plugin-setting-details`
  shows `state: healthy` and the lunix access log shows a 200 from a TRMNL
  IP. That first real poll closes spec risk R6.
- [ ] **Step 9: Show Evan.** Fetch the item's preview with
  `get-playlist-item-preview` (locally, never CI) and send it with
  SendUserFile. **Only after Evan confirms it on the wall**, remove the two
  old countdown items from the playlist (playlist items 523649 "Thanksgiving
  2026" and 583077 "Bing fall break"; plugin settings 25354 and 291262).
  Leave the plugin settings themselves for the cutover plan to delete.

---

## Self-review notes

- Spec coverage for this phase: section 4.1 (Tasks 3–7, 12), 5.1 shape and
  `/healthz` `/version` (Tasks 9–11), 5.2 sources (Tasks 5, 8), 6 security
  (Tasks 9, 10, 14), 7 delivery (Tasks 11, 13, 14), 8 build order steps 1–2
  (Tasks 1–14), 9 risks R2 (timeouts, Task 13), R3 (Task 1), R4 (Task 14),
  R5 (Task 5), R6 (Task 14). Sections 4.2, 4.3, 5.3, and the read-only
  InfluxDB token belong to the solar and weather plans.
- Not in this plan: the fail2ban jail from spec section 6. The allowlist
  already rejects every non-TRMNL address with a 403, so fail2ban adds
  little here; the cutover plan revisits it.
