# TRMNL PWS

TRMNL PWS is a unified Python service that reads Home Assistant data from InfluxDB, renders plugin-specific payloads, and pushes them to TRMNL webhooks on a shared schedule.

## Features

- Weather dashboard with temperature, feels-like, humidity, wind, dew point, pressure, UV, and rain data
- Solar power chart with solar, load, grid, and battery series
- Solar summary chart with recent daily energy totals
- Temperature chart for indoor and outdoor history
- Centralized per-webhook rate limiting with persisted exponential backoff
- Shared scheduler and config model for running multiple TRMNL plugins in one process

## Project Layout

- `app/main.py`: scheduler, plugin initialization, and main processing loop
- `app/plugins/`: plugin collectors for weather, solar power, solar summary, and temperature charts
- `app/influx_client.py`: InfluxDB client construction
- `app/state.py`: webhook state, retry tracking, and backoff logic
- `app/webhook.py`: TRMNL payload validation and webhook POSTing
- `config/config.example.yml`: application config template
- `config/secrets.example.yml`: secrets and webhook ID template
- `ui/`: ERB templates rendered by TRMNL
- `tests/`: pytest coverage for formatting, conversions, state, and webhook behavior

## Configuration

Copy the example files and fill in your local values:

```bash
cp config/config.example.yml config/config.yml
cp config/secrets.example.yml config/secrets.yml
```

`config/config.yml` defines:

- `general`: log level, display timezone, Flux query timezone, poll interval, and TRMNL+ tier flag
- `influxdb`: URL, org, bucket, and SSL verification settings
- `plugins`: enablement, entity IDs, aggregation windows, and display names per plugin

`config/secrets.yml` defines:

- `influxdb.token`
- `webhooks.*` IDs for each enabled TRMNL plugin

## Local Development

Install runtime dependencies:

```bash
pip install -r requirements.txt
```

Install test dependencies:

```bash
pip install -r requirements-dev.txt
```

Run the service:

```bash
python -m app.main
```

Run the test suite:

```bash
pytest
```

Set `general.log_level: DEBUG` when you need to inspect Flux queries, payload sizes, record counts, or webhook state decisions.

## Docker

Run with Docker Compose:

```bash
docker-compose up -d
docker-compose logs -f
docker-compose down
```

The container uses a two-stage Chainguard Python image, runs as `3444:3444`, mounts config read-only, and persists webhook state at `/tmp/last_trmnl_update.lock`.

## Operational Notes

- Flux queries should set `timezone.location(...)` and use `general.influx_query_timezone` for date boundaries.
- `general.timezone` controls display formatting.
- Standard TRMNL accounts are limited to 12 requests/hour and 2 KB payloads.
- Set `general.trmnl_plus_subscriber: true` for TRMNL+ limits of 30 requests/hour and 5 KB payloads.

## trmnl-poller

`trmnl-poller` is a small Go service in `cmd/trmnl-poller/`. TRMNL's servers poll it
for the countdown screen: the next trip, the next school event, and the next holiday.
It answers `GET https://trmnl-poller.evanhoffman.com/v1/countdown` with JSON, and only
when the request has the secret header `X-Poller-Key`. Any other request gets a 404.

It runs as a container (`ghcr.io/evandhoffman/trmnl-poller`) on a home server behind
a reverse proxy. The proxy accepts only TRMNL's server addresses. The TripIt and
school calendar feed URLs, and the key, come from the environment. The repo holds none
of them. Settings that are safe to read aloud live in an optional YAML file. See
`config/poller.example.yml`.

The screen template is in `templates/countdown/`. The `templates` job of the `Poller CI` workflow pushes it
to TRMNL with `cmd/push-templates`, which puts the key in the polling header.

### Key rules

The key has at least 32 characters. Use letters, digits, `-` and `_` only. TRMNL also
reads a `name=value&name=value` form of the header, so `&`, `=`, `:` and `#` could split
the key. The service and `push-templates` both refuse any other character.

### Deploy timing

A watcher on the server pulls the `latest` image once a day at 04:00. A server change
goes live the next morning. A template change goes live when it merges to `main`. So a
change to the JSON shape must be additive, and the server change must land before the
template change.

### Rotate the key

Do these steps back to back. Between step 2 and step 4 every poll returns 404.

1. Generate the new key with letters and digits only.
2. Put the new key in the server's secret file and recreate the container.
3. Update the GitHub secret `TRMNL_POLLER_KEY`.
4. Re-run the `templates` job of the latest `Poller CI` run on `main`, or merge a
   template change. TRMNL then gets the new header.
