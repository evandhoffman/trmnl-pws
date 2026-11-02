# TRMNL overhaul: design

Date: 2026-11-01. Status: approved by Evan, 2026-11-01.

This repo is **public**. This document, the code, and the example config hold
no LAN addresses, no secrets, no feed URLs, and no personal names beyond what
the screen labels need. Real values live in an untracked config file and in
1Password.

## 1. Why

The TRMNL on the wall goes stale. The countdown screen today reads
"-21 days" until a fall break that already passed, because a person typed
that date in once and nobody updated it. Every hand-typed value on the wall
can drift the same way.

Evan wants two things:

1. **Screens that maintain themselves.** No screen depends on a value that
   someone typed in once.
2. **Changes that reach the wall with no hands.** A merge to `main` updates
   the templates on TRMNL and the service on lunix. Nobody pastes markup into
   the TRMNL web editor, and nobody SSHs in to rebuild.

He also wants more data on the solar screens, and he expects to tweak every
screen once the new version is live.

## 2. Goals, non-goals, anti-goals

**Goals**

- A new countdown screen that always shows real upcoming events (section 4.1).
- More solar data: Powerwall peak charge and its time, hours at net zero on
  the grid, car charging energy, and more (section 4.2).
- The weather and temperature screens keep working, with the known
  indoor-temperature bug fixed (section 4.3).
- TRMNL **polls** a new service, `trmnl-poller`, written in **Go**, at
  `trmnl-poller.evanhoffman.com` (section 5).
- The endpoint is locked down (section 6).
- CI pushes templates to TRMNL and ships the container image (section 7).

**Non-goals**

- A general TRMNL framework, a plugin marketplace entry, or multi-user
  support. One house, one device.
- A TRMNL+ subscription. Evan does not have one, and the design must not need
  one.
- Changes to the weather or temperature screens beyond the bug fix. Evan will
  ask for tweaks after he sees the new instance.

**Anti-goals** (failure even if it works)

- A countdown that shows a negative number, an empty column, or a past event.
- Any value on the wall that a person must remember to update, including a
  yearly hand-typed school calendar.
- A public endpoint that leaks travel plans to anyone but TRMNL. The trip
  column says when the house is empty.
- A metric that is silently wrong. The gap bug in section 5.3 makes a naive
  query report 1 hour where the truth is 10.

## 3. Current state

- `trmnl-pws` is a Python service. It reads Home Assistant data from
  InfluxDB and pushes four screens to TRMNL webhooks every 5 minutes:
  weather, solar power (24 h chart), solar summary (7-day bars), and
  temperature (12 h chart). Templates are Liquid with Highcharts, in `ui/`
  (the `.erb` extension is historical; the content is Liquid).
- It runs on **lunix** as a Docker Compose container built from a checkout
  at `~/git/trmnl-pws`. Production ran the branch `add-chart-annotations`,
  14 commits ahead of `main`; that branch merged into `main` on 2026-11-01,
  so `main` now matches production. Deploys are by hand.
- It holds rate-limit and backoff state, and trims payloads, for the webhook
  limits (12 posts/hour, 5 KB per payload on the standard plan; the repo's
  docs still say 2 KB, which is out of date).
- The temperature chart logs "49 outdoor and 0 indoor temperature readings"
  on every run. The indoor series is likely blank on the screen.
- **The TRMNL account** (surveyed with the CLI, 2026-11-01): one device, a
  TRMNL OG+ on firmware 1.8.18, 4-level gray palette, refresh about every
  5 minutes, asleep 01:00–05:30. Nine playlist items, all always on:

  | Plugin setting (ID) | Kind | Fate |
  | --- | --- | --- |
  | Weather Dashboard (18870) | private, webhook, `ui/pws_dashboard.erb` | replaced by a polling plugin |
  | Daily Temperature Chart (25545) | private, webhook | replaced |
  | Solar Panel Stats (27170) | private, webhook, 24 h chart | replaced |
  | Solar daily stats (46019) | private, webhook, 7-day bars | replaced |
  | Thanksgiving 2026 (25354) | built-in "Days Left Until", typed date 2026-11-26 | retired by the new countdown |
  | Bing fall break (291262) | built-in "Days Left Until", typed date 2026-10-10 (the stale one) | retired by the new countdown |
  | Hourly Weather Forecast | installed recipe (polling), not Evan's | untouched |
  | Personal Calendar | built-in Google Calendar | untouched |
  | My Activity | STRMNL (Strava) | untouched |

  The live template of plugin 46019 matches `ui/solar_bar_chart.erb`
  exactly. Evan has no polling plugin of his own yet.
- Watchtower runs on lunix and updates every container daily at 04:00
  Eastern.
- Evan's device has the **Developer edition**, so the Account API, API keys,
  and polling plugins are available. The `trmnl` CLI (v0.1.1) is installed
  on Evan's Mac.

## 4. The screens

### 4.1 Countdown (new)

Layout **C**, chosen by Evan from mockups: **three columns side by side**.
Each column shows the number of days left (large), the event name, and a
small source label.

**Sources** (Evan's choice):

1. **Trips**, from Evan's **TripIt iCal feed**.
2. **School dates**, from his son's university academic calendar: breaks,
   residence halls closing and opening, start and end of semester,
   commencement. The university publishes it as a **public Google
   Calendar** (Evan found it, 2026-11-01). Its ID goes in config, not in
   this public repo. The feed is
   `https://calendar.google.com/calendar/ical/<calendar-id>/public/basic.ics`.
   Checked 2026-11-01: HTTP 200, 361 KB, 1,151 all-day events from 2000 to
   2031, no `RRULE` lines. The screen label (for example "Binghamton") comes
   from config.
3. **Major holidays**, computed in code. No feed, no upkeep.

**Rules** (Evan confirmed each):

- Each source supplies its next upcoming event.
- If a source has nothing upcoming (for example, no trip booked), its slot
  goes to the next unused event from the other sources. The screen never
  shows an empty column.
- Columns sort by date, soonest on the left. The source label shows where
  each event came from.
- The screen never shows a negative count or a past event. When an event
  passes, its column moves to the next event on its own.
- Holidays are the floor: there is always a next holiday, so the screen can
  always fill three columns.

**Calls I made** (Evan can change them):

- On the day itself, the column reads "Today". During a multi-day trip it
  reads "Day 2 of 5". A multi-day school break counts down to its first day.
- **Holidays (Evan decided):** every day the NYSE closes for a full day:
  New Year's Day, Martin Luther King Jr. Day, Washington's Birthday
  (Presidents' Day), Good Friday, Memorial Day, Juneteenth, Independence
  Day, Labor Day, Thanksgiving, and Christmas. Compute them by rule; Good
  Friday needs the Easter date. *My calls:* count down to the holiday's own
  date (December 25), not the weekday the market observes it; keep
  Halloween as a family extra; leave out Columbus Day and Veterans Day,
  which are Federal Reserve bank holidays but NYSE trading days. The list
  lives in config, so Evan can add or remove one without a code change.
  Test each rule against the NYSE's published calendar for 2026 and 2027.
- School events filter by patterns in config, so deadlines and the
  winter-session events stay off the screen. Default patterns, from the
  real feed's titles: `Residence halls close` (when he comes home; it is
  the best countdown target), `No classes (... break)`, `Classes begin`,
  `Last day of classes`, `Commencement`. Exclude anything containing
  `Winter session`. Matching ignores case ("Residence Halls close" appears
  too). When two kept events fall on the same day, show one: prefer
  "Residence halls close" and name it after the break, for example
  "Thanksgiving break".
- The trip column shows **only the destination city and the dates**. Never
  flight numbers, hotels, or confirmation codes. TripIt's feed holds a
  trip-level event plus per-flight and per-hotel events; use the trip-level
  event.
- Day counts use calendar dates in `America/New_York`, not 24-hour blocks.
  Test this on DST change days (2026-11-01 was one).

### 4.2 Solar (more data)

The existing two solar screens stay: the 24-hour power chart (solar, load,
grid, battery) and the 7-day summary. The new metrics go on the 7-day
summary, which becomes a per-day table or a chart plus a table. The layout is
left to the builder, then to Evan's tweaks.

**Metrics Evan asked for:**

| Metric | Source sensor | Notes |
| --- | --- | --- |
| Peak Powerwall charge % per day | `bellmore_percentage_charged` | Samples about every 10 min, history from 2024-11-08. In winter the % is the news (median peak 43–60% in Nov 2025–Feb 2026); in summer it hits 100% and the time is the news. |
| Time the peak was first reached | same | First time the day's max was reached, local time. |
| Hours per day at **net zero on the grid** | `bellmore_grid_power` (kW) | Evan's definition: grid neither supplying nor taking power. Cutoff: abs(grid) < 0.05 kW (my call, configurable). Last week: 9.5–11.5 h/day. Not outage islanding. |
| Car charging energy per day | `wall_connector_power` (kW) | Tesla Wall Connector; peaks ~7.7 kW; history from 2025-10-15. No energy sensor exists, so integrate power over time. Show kWh with one decimal (my call). Last two weeks: 0.7–10.3 kWh/day. |

**Metrics I proposed and Evan has not confirmed** (build them; Evan cuts
after he sees the screen):

- **Time the battery reached its reserve** each day, or "not reached".
  Evan: the Powerwall keeps a reserve of 30–40%, so the low % itself
  carries no news, but the time it got there might. Read the reserve from
  `bellmore_backup_reserve`; it can change, so compare each reading to the
  reserve in force at that moment.
- Self-powered %: the share of the day's home load (car included) that solar
  and the battery covered.
- Grid import and export kWh per day (`bellmore_grid_imported`,
  `bellmore_grid_exported`).
- ~~Tomorrow's solar forecast.~~ Dropped by Evan, 2026-11-02: "I wouldn't
  really trust forecasts. Let's drop solar forecasts."

Other sensors available: `bellmore_solar_generated`, `bellmore_home_usage`,
`bellmore_battery_charged`, `bellmore_battery_discharged`,
`bellmore_backup_reserve`, `bellmore_grid_status`.

### 4.3 Weather and temperature (port)

Port both as they are **on the wall today**, which is `main` after the
2026-11-01 merge of the production branch. That includes the sunrise,
solar-noon, and sunset markers. The live compose setup on
lunix uses `network_mode: host` and reaches InfluxDB on the host's port; the
new container may use the Docker network instead, which is the builder's
call. Fix the indoor-temperature series (0 readings today:
the cause is either the gap bug in section 5.3 or a renamed sensor). Evan
will ask for tweaks after the cutover.

## 5. The service: `trmnl-poller`

### 5.1 Shape

- **Go**, latest stable release. Evan opened the door to Go; I recommend
  it for a public endpoint: one static binary on a minimal image, no shell,
  no interpreter. The standard library's `net/http` is the server.
- A **read-only HTTP API**: one GET path per screen, for example
  `/v1/countdown`, `/v1/solar/today`, `/v1/solar/week`, `/v1/weather`,
  `/v1/temperature`. Each returns the JSON the template needs, and nothing
  more.
- TRMNL **polls** each path on the plugin's refresh schedule. The service
  pushes nothing. The webhook client, the rate-limit state, the backoff,
  and the payload trimming all retire.
- The service computes each response on request, with short in-memory
  caches: InfluxDB results for about a minute, feeds for about an hour. No
  persisted state, so the filesystem can be read-only.
- `/healthz` (for Compose and monitoring) and `/version` (reports the git
  SHA). Both carry no data.

### 5.2 Data sources

| Source | Access |
| --- | --- |
| InfluxDB 2 (`home_assistant/autogen`, org `bellmore`) on lunix | Over the Docker network on lunix. Use a **new read-only token** scoped to that one bucket, not the current token. |
| TripIt iCal feed | HTTPS GET. The URL holds a private token, so it is a secret. |
| University academic calendar | HTTPS GET of the public Google Calendar ICS (section 4.1). Public, not a secret. |
| Holidays | Computed in code. |

All Flux queries set `option location = timezone.location(name:
"America/New_York")`, as the current code does.

### 5.3 The gap rule (test first)

Home Assistant writes a value **only when it changes**. While the grid sits
at 0 kW, InfluxDB gets no new rows. A query that ignores the gaps gets the
wrong answer:

| Day | Net-zero hours, gaps ignored | Net-zero hours, last value carried forward |
| --- | --- | --- |
| 2026-10-26 | 0.8 | 11.5 |
| 2026-10-28 | 1.3 | 10.6 |
| 2026-10-31 | 0.6 | 10.8 |

Every duration and energy metric must carry the last value forward through
gaps. In Flux that means `aggregateWindow(..., createEmpty: true)` and then
`fill(usePrevious: true)`; Go code that integrates does the same. Write the
failing tests first, with fixtures that have long gaps. Use the values above
as anchors: a known input must give a known absolute number.

The same rule likely explains the indoor-temperature bug, and may affect the
current solar charts.

## 6. Security

Evan's requirement: "very locked down." The trip column reveals when the
house is empty.

- **TLS** through the existing nginx and wildcard certificate on lunix, at
  `trmnl-poller.evanhoffman.com`. **Reachability is already in place**
  (checked 2026-11-01): the wildcard `*.evanhoffman.com` DNS record at
  Cloudflare resolves through a dynamic-DNS name to the home's public IP,
  and nginx answers on 443 there (`api.stationcast.app` is served the same
  way). The new hostname resolves today, so no DNS change is needed. Follow
  the lunix repo's "Adding an nginx virtualhost" steps in its `AGENTS.md`,
  with TRMNL's IPs in place of the LAN-only `allow` line, inside
  `location /`. Do not write the public IP into this repo.
- **IP allowlist.** nginx allows only TRMNL's server IPs
  (`trmnl list-server-ips`), in an include file the vhost reads. A systemd
  timer on lunix (the repo's pattern for scheduled jobs) refreshes it daily.
  *My call:* it applies the new list itself, because the list comes from
  TRMNL's own API over TLS, the same party the endpoint serves. It writes
  the file, runs `nginx -t`, reloads only on a pass, and logs any change to
  the journal. Loki already collects the journal, so a Grafana alert on that
  log line covers notice. A failed fetch keeps the old list.
- **Secret header** on every request, set in each polling plugin's headers.
  A request without it gets a 404, so the endpoint does not reveal itself.
  Check it in the Go service with a constant-time compare, not only in nginx.
- **GET only.** No write paths, no raw query endpoint, no debug pages, no
  directory listing.
- **Least data.** Each path returns only what its screen shows. Trips:
  city and dates only.
- **Container:** non-root, read-only root filesystem, no shell in the
  runtime image, no published port on the LAN beyond what nginx needs.
  nginx rate-limits the vhost; fail2ban (already on lunix) watches it.
- **Least privilege upstream:** the read-only InfluxDB token above.

## 7. Delivery: how changes reach the wall

**Templates.** Each screen's Liquid template lives in the repo. On a merge
to `main`, CI pushes each template to its TRMNL plugin with the `trmnl` CLI
(`import-plugin-setting-files`), authenticated by a TRMNL Account API key in
a GitHub Actions secret. Template tweaks reach the wall within minutes. CI
can also render each template (`render-markup`) and attach the image to the
run, so a change can be checked without walking to the device.

*My call:* CI previews use **fixture data only**, never live data. The repo
is public, so its run logs and artifacts are public. A live countdown
preview would publish travel dates. Never call
`get-playlist-item-preview` in CI for that reason. Keep fixtures beside the
templates, and use the same fixtures in the Go tests.

**Plugin setup.** A private plugin's files are a `settings.yml` plus one
`.liquid` file per markup size (`full.liquid`, and so on). `settings.yml`
holds `strategy`, `polling_url`, `polling_headers`, `polling_verb`, and
`refresh_interval` (confirmed with `get-plugin-setting-files` on 2026-11-01).
So:

- The builder creates each new polling plugin once with the CLI
  (`create-plugin-setting`), and its ID goes in this repo's CI config. CI
  never creates or deletes plugins.
- On every merge, CI pushes the full file set with
  `import-plugin-setting-files`: the templates **and** `settings.yml`. The
  committed `settings.yml` has a placeholder for the header; CI fills in the
  secret from a GitHub Actions secret at push time and never writes it to a
  log or artifact. Rotating the secret is then a secret update plus a merge.
- The polling URL carries no key. TRMNL's own form warns that a key in the
  URL is readable by any AI agent connected to the account.

**Service.** On a merge to `main`, CI builds the image, runs it once as a
smoke test (baseline `conventions/ci.md`: build the exact production target
and run it), and pushes it to `ghcr.io/evandhoffman/trmnl-poller`, tagged
with the SHA and `latest`. The repo is public, so the package is public and
Watchtower needs no registry login. Watchtower on lunix pulls it at its
daily 04:00 run.

*My call:* daily is acceptable for code changes, because most tweaks are
template changes and those go out at once. If that proves too slow, the
builder can add a label-scoped Watchtower schedule for this one container.

**lunix side.** A Compose file for `trmnl-poller` and an nginx vhost go in
the `lunix` repo, next to the other compose files, like every other service
there. No DNS change is needed (section 6).

**Evan decided:** the builder applies the lunix changes itself, over SSH:
the Compose stack, the nginx site, the allowlist timer, and the nginx
reload. Reload with lunix's documented command,
`docker exec nginx nginx -t && docker exec nginx nginx -s reload`, never
without a passing `nginx -t`. nginx fronts every lunix service, including
StationCast's public API, so a failed reload is an outage for all of them.
Follow the lunix `AGENTS.md` rules (for example, never `--remove-orphans`
from a single stack file).

**Baseline.** Follow `evanwtf/baseline`. It has no Go convention yet; copy
StationCast's `backend-go/` (layout, `Dockerfile`, `ci-backend-go.yml`) as
the reference. This repo is under `evandhoffman`, so CI uses GitHub-hosted
runners, not the self-hosted ones. Set `timeout-minutes` on every job and
`retention-days` (3 at most) on every artifact. Baseline's release question
Q4 (tags versus release on merge) is open. This repo has no releases today;
*my call* is image-on-merge with the SHA as the version, and no `v*` tags,
until Evan decides Q4.

**Secrets** (1Password, vault "Code Secrets", prefix `trmnl-pws-`). Evan
creates each item himself, so no raw secret enters a transcript:

- `trmnl-pws-tripit-ical-url`
- `trmnl-pws-poller-header-secret` (generate with `--generate-password`)
- `trmnl-pws-trmnl-api-key`
- `trmnl-pws-influx-readonly-token`

## 8. Build order

Evan wants to see a new screen early and tweak from there.

**Evan decided: no long-running branches.** Every change lands on `main`
through a small, short-lived pull request (the repo requires PRs on
`main`). Each phase below is several such PRs, not one branch. Squash-merge.
Until a new screen is ready, its code ships dark: the Python service keeps
driving the wall, and the Go service runs alongside it.

1. **Spikes** (throwaway, answer the risks in section 9): the exact file
   format `import-plugin-setting-files` expects, round-tripped
   with `get-plugin-setting-files` on a scratch plugin; whether polling has a
   size limit; TripIt feed shape.
2. **Countdown end to end.** Go service skeleton, the countdown path, the
   endpoint on lunix with the full lockdown, a new polling plugin on TRMNL,
   and the CI that pushes the template and ships the image. The old
   countdown plugin leaves the playlist when the new one is right.
3. **Solar.** The gap-rule tests first, then the metrics, then the screens.
4. **Weather and temperature.** Port, fix the indoor series.
5. **Cutover.** Each new polling plugin replaces its webhook plugin in the
   playlist once Evan has seen it right on the wall. Then the Python
   container stops, and a later change deletes the Python code, `ui/*.erb`,
   and the webhook config.

Each phase ends with Evan looking at the real device, not only a test run.

## 9. Risks

- **R1. Closed.** The school calendar feed exists (section 4.1). The
  remaining risk is that the university renames event titles, which would
  silently drop events. Log a warning when the school source yields no
  upcoming event within 120 days.
- **R2. The CLI is new (v0.1.1).** Its file import format and its behavior
  in CI may change. Pin the CLI version in CI and keep a plain HTTP fallback
  against the Account API (`https://trmnl.com/api-docs/openapi.json`). On
  2026-11-01, `get-plugin-setting-files` returned in under a second for one
  plugin and hung with no response for four others, while `list-devices`
  worked. Every CI call needs a hard timeout and a clear failure.
- **R3. Polling limits are undocumented.** The docs state no response size
  cap and no refresh rate choices for plain polling. Measure both in the
  spike.
- **R4. TRMNL server IPs change.** Covered by the refresh job in section 6.
- **R6. Reachability was only checked from inside the LAN** (through the
  router's hairpin). The first real poll from TRMNL proves it end to end;
  until then, treat it as likely, not certain.
- **R5. Recurring events.** Go's iCal libraries handle `RRULE` less
  maturely than Python's. Test recurrence and all-day events early.

## 10. Who decided what

**Evan decided:** self-maintaining screens; merge-to-wall delivery; the
three countdown sources and the school's Google Calendar feed; holidays as
the default; layout C; the backfill
rule; columns that never empty; the four solar metrics in section 4.2;
net-zero meaning grid ≈ 0, not outages; polling over webhooks; a
locked-down endpoint at `trmnl-poller.evanhoffman.com`; Go is acceptable;
no TRMNL+; Developer edition is available; weather and temperature
unchanged for now; the builder applies the lunix changes and reloads nginx
through Docker; NYSE full-closure days as the holidays; the time the
battery reached reserve instead of the overnight low; no long-running
branches; the production branch merges into `main`.

**I decided (marked "my call" above):** Go over Python; the "Today" and
"Day 2 of 5" wording; the holiday list; school keyword filtering;
city-and-dates only for trips; the ±0.05 kW cutoff; kWh with one decimal;
the unconfirmed solar metrics; counting down to a holiday's own date;
Halloween as an extra; no Columbus or Veterans Day; daily Watchtower; image-on-merge with no
tags; a new read-only InfluxDB token; the secret names; an allowlist job
that applies TRMNL's list itself; fixture-only CI previews; CI pushes
`settings.yml` with the secret injected.

**Left to the builder:** the solar summary layout; Go module layout and
library choices; JSON shapes per path; cache durations; how CI renders a
preview; host networking or the Docker network; the Flux-versus-Go split for
each metric.
