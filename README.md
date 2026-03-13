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
