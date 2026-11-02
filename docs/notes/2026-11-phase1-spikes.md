# Phase 1 spike notes (2026-11-01)

Throwaway findings for Task 1 of the phase 1 plan. Later tasks (iCal
parse, trip filter, template, template push in CI) build on these values.
All real trip data is replaced by placeholders. Secrets appear only as
`op://` references.

## Step 1: TripIt feed shape

### Command

```sh
TRIPIT_ICAL_URL="op://Code Secrets/trmnl-pws-tripit-ical-url/password" \
  op run -- sh -c 'curl -s -m 20 "$TRIPIT_ICAL_URL" -o "$SCRATCH/tripit.ics"'
```

`$SCRATCH` is a directory outside the repo. The call returned HTTP 200 and
6695 bytes. Wall time was 52 s, but most of that is `op run` (each `op run`
took about 50 s on this laptop). The HTTP fetch itself is fast.

### File-level facts

- Line endings are CRLF on every line. Long lines are folded (RFC 5545:
  CRLF plus one leading space). 38 of 163 physical lines are continuations.
  The parser must unfold before it reads a property.
- Header: `PRODID:-//John Papaioannou/NONSGML Bennu 0.1//EN`, `VERSION:2.0`,
  `X-WR-CALNAME:<Name> (TripIt)`, `X-WR-CALDESC:TripIt Calendar`,
  `X-PUBLISHED-TTL:PT15M`.
- No `VTIMEZONE`, no `TZID` parameter, no `RRULE`, no `RDATE`, no `EXDATE`.
- Text values escape commas as `\,` and newlines as `\n` (RFC 5545 TEXT).
- Every VEVENT has `DTSTAMP`, `UID`, `DTSTART`, `DTEND`, `SUMMARY`,
  `LOCATION` (sometimes empty), `DESCRIPTION`, `TRANSP:TRANSPARENT`.
  `GEO` is present on some events (8 of 11).

### Event counts (feed on 2026-11-01)

| Kind | Count |
|------|-------|
| VEVENT total | 11 |
| Trip-level | 3 |
| Lodging (check-in / check-out) | 6 |
| Car rental (pick up / drop off) | 2 |
| Flight | 0 |

The feed had no flights on this date. The flight shape is not observed.
The rule below does not depend on it.

### Trip-level event

```text
UID:<uuid>@tripit.com                       (no "item-" prefix)
DTSTART;VALUE=DATE:<YYYYMMDD>              first day of the trip
DTEND;VALUE=DATE:<YYYYMMDD>                day after the last day (exclusive)
SUMMARY:<trip name>
LOCATION:<City>\, <ST>\, <Country>         or <City>\, <ST>
GEO:<lat>;<lon>                            absent when no destination
DESCRIPTION:<Name> is in <City>\, <ST>\, <Country> from <Mon> <D> to <D>\, <YYYY>\n...
```

- All-day: `VALUE=DATE`. `DTEND` is exclusive. Last day = `DTEND` minus one
  calendar day. Check: in each trip the last check-out or drop-off falls on
  `DTEND - 1`, and the DESCRIPTION "to <D>" date also equals `DTEND - 1`.
- SUMMARY is the trip name the user typed or TripIt chose. Seen shapes:
  - `<City>\, <ST>\, <Country>\, <Month> <YYYY>` (TripIt default)
  - `<Free-text trip name>` (user renamed it, e.g. `<Conference> <YYYY>`)
  - `Your trip\, <Month> <YYYY>` (no destination set)
- So **take the destination from LOCATION, not SUMMARY.** LOCATION seen as
  `<City>\, <ST>\, <Country>` and `<City>\, <ST>` (a US city with a
  two-letter code). The city is the text before the first unescaped comma.
- When the trip has no destination, LOCATION is the literal sentinel
  `No destination specified` and there is no GEO. One of three trips had it.

### Item events (lodging, car; flights expected to match)

```text
UID:item-<uuid>@tripit.com                 ("item-" prefix)
DTSTART:<YYYYMMDD>T<HHMMSS>Z               lodging: UTC
DTSTART:<YYYYMMDD>T<HHMMSS>                car: floating local time, no Z
SUMMARY:Check-in: <Hotel>  | Check-out: <Hotel>
SUMMARY:Pick Up Rental Car: <Company> | Drop Off Rental Car: <Company>
LOCATION:<street address>  (empty for car events)
DESCRIPTION:...\n[Lodging] Arrive <Hotel>...   or  [Car Rental] Pick Up ...
```

- Always timed (never `VALUE=DATE`). Each is a one-hour block.
- The kind tag is in DESCRIPTION: `[Lodging]`, `[Car Rental]`. Flights are
  expected to use a similar tag, not observed.
- Item events carry no link to their trip except the trip id inside the
  DESCRIPTION URL. Do not use it: the URL is account data.

### The rule (for Task 6)

**An event is a trip when its `DTSTART` has `VALUE=DATE` and its UID does
not start with `item-`; its first day is `DTSTART`, its last day is `DTEND`
minus one day, and its city is the text of `LOCATION` before the first
unescaped comma, unless `LOCATION` is `No destination specified`.**

Either test alone separates the 3 trips from the 8 items in this feed. The
code should require both, so a change to one does not let items through.

## Evidence labels

Steps 2 and 3 label each finding:

- **[observed]**: seen in a real call on 2026-11-02 against a scratch
  private plugin.
- **[spec]**: read from https://trmnl.com/api-docs/openapi.json or
  `trmnl <cmd> --help` only.

Step 1 is all observed.

## How the TRMNL CLI calls were run

The CLI reads `TRMNL_API_KEY` from the environment. Two ways to supply it
without a shell variable or a command-line value:

```sh
# 1Password (needs an unlocked app; each op run took about 50 s here)
TRMNL_API_KEY="op://Code Secrets/trmnl-pws-trmnl-api-key/password" \
  op run -- sh batch.sh

# a 0600 env file, loaded inside one command only
sh -c 'set -a; . "$CREDS"/trmnl.env; set +a; exec sh batch.sh'
```

Each CLI call inside `batch.sh` runs with a hard kill after 45 s:

```sh
if [ -n "$IN" ]; then trmnl "$@" --rsh-timeout 30s < "$IN" & p=$!
else trmnl "$@" --rsh-timeout 30s & p=$!; fi
( exec </dev/null >/dev/null 2>&1; sleep 45; kill -9 $p && touch HUNG.$p ) &
wait $p
```

Caveats [observed]:

- A background job in `sh` reads stdin from `/dev/null`. A JSON body on
  the pipe is lost (`Request body is required`). Redirect the body file
  explicitly onto the backgrounded command, as above.
- The watchdog subshell must close stdout and stderr. If it keeps them,
  `op run` waits for the leftover `sleep 45`.
- No call hung. Every call took 0.04 s to 0.23 s, measured around the
  `trmnl` process. This includes `get-plugin-setting-files` (13 calls,
  0.11 s to 0.23 s), which hung for other plugins on 2026-11-01.
- `trmnl auth inspect`: `bearer_auth: configured`, 157 of 157 secured
  operations callable. The key still lacks the `delete` capability (see
  below), so "callable" does not mean "permitted".

## Step 2: TRMNL import format

### Create [observed]

```sh
trmnl create-plugin-setting 'name: scratch-import-test, plugin_id: 37' --rsh-timeout 30s
```

Body: `{"name": "scratch-import-test", "plugin_id": 37}`. 0.11 s. The
response `data` has `id`, `plugin_id: 37`, `strategy: "polling"`,
`refresh_interval: null`, `read_only?: false`,
`sync.source_kinds: ["custom"]`.

### Import [observed]

```sh
trmnl import-plugin-setting-files <id> -o json --rsh-timeout 30s < body.json
```

`body.json`:

```json
{"files": {
  "settings.yml": "---\nstrategy: polling\npolling_url: https://example.com/trmnl/dummy.json\npolling_verb: get\npolling_headers: |-\n  X-Poller-Key: <value>\nrefresh_interval: 60\nname: scratch-import-test\n",
  "full.liquid": "<div class=\"title\">{{ title }}</div>"
}}
```

- Response: `{"data":{"settings_yaml":"<the stored settings.yml>"}}`.
- First import into a new plugin [observed]: the import above (6 keys in `settings.yml`)
  was accepted. In the read-back, every key it did not send had the value
  the plugin had at creation: `no_screen_padding: 'no'`,
  `dark_mode: 'no'`, `oauth_enabled: 'false'`, `oauth_pkce_enabled: 'no'`,
  and empty values for the rest. Those values match the read-back taken
  before any import. So "default" here means "the value the plugin
  already had". The test does not show the server resetting a key.
- Reimport [observed]: the only absent keys tested were `polling_headers`
  and `full.liquid`. Both kept their stored values: `polling_headers`
  (see "Import without headers" below), and `full.liquid` (unchanged after
  more than 15 settings-only imports). Other absent keys were not tested
  on a reimport.
- **Guidance for the push tool:** always send `polling_url` (it is
  committed; the hostname is public) and a non-empty `polling_headers`.
  The tool then never depends on how the server treats an absent key.

### Read-back [observed]

`get-plugin-setting-files <id>` returns
`{"data":{"files":{"settings.yml": ..., "full.liquid": ...}}}`. The
stored `settings.yml` has these keys, in this order:

```yaml
---
strategy: polling
no_screen_padding: 'no'
dark_mode: 'no'
static_data:
polling_verb: get
framework_version:
serverless_language:
refresh_interval_floor:
polling_url: https://example.com/trmnl/dummy.json
polling_headers:
polling_body:
id: <id>
oauth_enabled: 'false'
oauth_pkce_enabled: 'no'
name: scratch-import-test
description:
refresh_interval: 60
```

- `polling_url` comes back as written.
- **`polling_headers` always comes back empty, even when headers are
  stored.** The read-back cannot confirm the headers.
- `get-plugin-setting-details <id>` returns `settings` with sensitive
  values removed (no URL, no headers), `form_fields`, `available_fields`,
  `framework.version` (`3.4.4` here) and `sizes`. It does not show the
  headers either.

### `polling_headers` syntax [observed]

The read-back cannot show headers, so the test pointed `polling_url` at
`https://httpbin.org/headers` (it echoes the request headers), then ran
`start-refresh <id>` and polled `get-refresh <id> <job_id>`. The
`variables` field shows what TRMNL sent. Each syntax below reached the
server:

| `polling_headers` in settings.yml | Headers sent |
|---|---|
| `x-poller-key=<v>&accept=application/json` | `X-Poller-Key`, `Accept: application/json` |
| `\|-` block, lines `X-Poller-Key: <v>` and `X-Second: two` | both |
| `\|-` block, lines `x-poller-key=<v>` and `x-second=two` | both |
| `'X-Poller-Key: <v>'` (one quoted line) | `X-Poller-Key` |

- Use the block form with one `Name: Value` per line. The form help text
  documents it ("One per line as `Name: Value` (or `name=value`)").
- In `variables`, TRMNL showed the echoed value as `[set]` for the line
  forms. It showed the raw value for the `&` form. Do not rely on this
  masking.

### Import without headers [observed] (important for Task 7)

Each case first stored headers, then imported a `settings.yml` with
`polling_headers` in the given state, then refreshed against httpbin:

| `polling_headers` in the import | Headers after |
|---|---|
| key absent | kept |
| `polling_headers:` (null) | **cleared** |
| `polling_headers: ""` | **cleared** |

The read-back `settings.yml` has `polling_headers:` (null). **So a
download-then-reimport of the file as-is wipes the secret header.** The CI
push tool must either remove the `polling_headers` key from the file it
imports, or write the real header value into it.

### `refresh_interval` values

Through `import-plugin-setting-files` [observed]: no value is rejected.
The server coerces it.

| Sent | Stored |
|---|---|
| 0 | 15 |
| 5 | 15 |
| 15 | 15 |
| 45 | 60 |
| 60 | 60 |
| 1440 | 1440 |
| 2000 | 1440 |

Through `update-plugin-setting <id>` (body `{"refresh_interval": N}`):

- 45 [observed]: rejected, exit code 4,
  `{"error": "refresh_interval must be one of 1440, 720, 480, 360, 240, 120, 60, 30, 15, 10, 5"}`.
- 15 [observed]: accepted, stored 15.
- 5 [observed]: accepted, stored 15. The spec says a value faster than
  the plan allows is raised to the nearest allowed value; this account's
  floor is 15.

Use 15, 60 or 1440 to get exactly what you send on this plan [observed].
30 is in the enum but untested [spec].

### Delete [observed]

```sh
trmnl delete-plugin-setting <id> --rsh-timeout 30s
```

Failed, exit code 4:
`{"error": "This API key lacks the delete capability. Create a key with it under Account, API keys."}`.
The account key has read and content capabilities only. The scratch
plugin still exists (see Open caveats).

## Step 3: `render-markup` [observed]

```sh
trmnl render-markup -o json --rsh-timeout 30s < render.json
trmnl render-markup 'markup: "Hello, {{ name }}!", variables.name: World'
```

Body: `{"markup": <string or array of strings>, "variables": <object>}`.
It returns rendered text (HTML), not a PNG and not a URL. Each call took
0.09 s to 0.14 s.

| Input | `data` returned |
|---|---|
| `"<div class=\"title\">{{ title }}</div><div>{{ trips[0].city }} in {{ trips[0].days }} days</div>"` with `{"title":"Countdown","trips":[{"city":"Springfield","days":12}]}` | `"<div class=\"title\">Countdown</div><div>Springfield in 12 days</div>"` |
| array `["<div>{{ title }}</div>", "<span>{{ title }}</span>"]` | array `["<div>Two</div>", "<span>Two</span>"]` |
| `{{ title \| upcase }} {{ missing }} {% for t in trips %}[...]{% endfor %}` | `"COUNTDOWN  [Springfield:12][Shelbyville:40]"` |
| `{% if x %}unclosed` | `"Liquid error: Liquid syntax error (line 1): 'if' tag was never closed"` |
| `{{ 'now' \| date: '%Y-%m-%d' }} {{ trmnl.user.name }}` with `{}` | `"2026-11-02 "` |

- **A Liquid syntax error still returns HTTP 200 and exit code 0.** The
  error is in the text: `data` starts with `Liquid error:`. A CI check must
  test the text, not the exit code.
- An undefined variable renders as an empty string, with no error.
- Filters and loops work. No `trmnl.*` context is present: the plugin's
  merge variables are not added.
- The output is the bare template. It has no framework CSS or layout
  wrapper, so it is not a picture of the screen.

## Open caveats

- **Scratch plugin `scratch-import-test` is not deleted**, because the key
  lacks `delete`. It is on no playlist. It was left inert:
  `polling_url: https://example.com/trmnl/dummy.json`, no headers,
  `refresh_interval: 1440`. Delete it from the web UI or with a key that
  has `delete`.
- No flight event was in the feed, so the flight shape is unobserved.
- Whether `'now' | date` uses UTC or the account time zone is unknown.
  The test ran at 00:xx in New York, which is the same date in UTC.
