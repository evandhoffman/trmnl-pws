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

## How the TRMNL CLI calls were run

Each `op run` needs 1Password to be unlocked, and took about 50 s on this
laptop. To pay that cost once, one `op run` wraps a whole shell script of
CLI calls:

```sh
TRMNL_API_KEY="op://Code Secrets/trmnl-pws-trmnl-api-key/password" \
  op run -- sh batch.sh
```

Each CLI call inside the script runs with a hard kill after 45 s:

```sh
trmnl "$@" --rsh-timeout 30s & p=$!
( exec </dev/null >/dev/null 2>&1; sleep 45; kill -9 $p && touch HUNG.$p ) &
wait $p
```

Caveat: the watchdog subshell must close its stdout and stderr (`exec
>/dev/null 2>&1`). If it keeps them, `op run` waits for the leftover
`sleep 45` to close the pipe, and every batch takes at least 45 s more.

`trmnl auth inspect` showed `bearer_auth: configured`, 157 of 157 secured
operations callable.

## Step 2: TRMNL import format

**Status: partly done.** 1Password locked after the first batch
(`error initializing client: authorization timeout`, 6 tries from 00:03 to
00:14 EST on 2026-11-02). The import, the read-back, the
`refresh_interval` tests and the delete did not run.

### Done: create the scratch plugin

```sh
trmnl create-plugin-setting 'name: scratch-import-test, plugin_id: 37' --rsh-timeout 30s
```

Request body: `{"name": "scratch-import-test", "plugin_id": 37}`. Took
0.11 s. Response (200):

```json
{"data":{"id":<id>,"name":"scratch-import-test","description":null,
 "plugin_id":37,"refresh_interval":null,"health_notification_enabled":false,
 "icon_url":"https://trmnl.com/images/plugins/private_plugin.svg",
 "icon_content_type":null,"strategy":"polling","read_only?":false,
 "sync":{"source_kinds":["custom"],"upload_allowed":true,"reason":null,
 "action_id":null}}}
```

- A new private plugin starts with `strategy: polling` and
  `refresh_interval: null`.
- `list-plugin-settings` took 0.14 s and did not hang.

### Not yet run: import and read-back

Planned request for `trmnl import-plugin-setting-files <id>`, JSON on stdin:

```json
{"files": {
  "settings.yml": "---\nstrategy: polling\npolling_url: https://example.com/trmnl/dummy.json\npolling_verb: get\npolling_headers: x-poller-key=dummy-not-a-secret&accept=application/json\nrefresh_interval: 60\nname: scratch-import-test\n",
  "full.liquid": "<div class=\"title\">{{ title }}</div>"
}}
```

Per the API spec: `settings.yml` is required; a markup file left out keeps
its current content. The response is `{"data":{"settings_yaml":"..."}}`.
The `polling_headers` syntax `key=value&key=value` is the candidate. It is
not yet confirmed to round-trip.

### From the API spec only (not yet confirmed by a call)

- `update-plugin-setting` takes `refresh_interval` from the enum
  `1440, 720, 480, 360, 240, 120, 60, 30, 15, 10, 5` (minutes). The spec
  says a value faster than the plan allows is raised to the nearest allowed
  value. Whether the import path applies the same enum is open; planned
  tests are 5, 15, 45, 1440 and 2000.
- `get-plugin-setting-files` on another author's recipe omits
  `polling_headers` and `polling_body`. For our own private plugin they are
  expected to come back.

## Step 3: `render-markup`

**Status: not run** (same 1Password block).

From the API spec (`POST /api/markup`, operation `renderMarkup`):

- Body: `{"markup": <string or array of strings>, "variables": <object>}`.
  Both are required.
- Response 200: `{"data": <string or array of strings>}`, which is the
  rendered Liquid output as text (HTML), not a PNG and not a URL. An array
  in gives an array out.
- 401 or 403 has no body.

Planned call:

```sh
trmnl render-markup --rsh-timeout 30s < render.json
```

with `render.json`:

```json
{"markup": "<div class=\"title\">{{ title }}</div><div>{{ trips[0].city }} in {{ trips[0].days }} days</div>",
 "variables": {"title": "Countdown", "trips": [{"city": "Springfield", "days": 12}]}}
```

## Open caveats

- Steps 2 and 3 need a rerun with 1Password unlocked.
- The scratch plugin `scratch-import-test` still exists on the account and
  needs `trmnl delete-plugin-setting <id>`. It is on no playlist.
- No flight event was in the feed, so the flight shape is unobserved.
