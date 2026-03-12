# TRMNL PWS

Unified weather and energy monitoring for TRMNL displays. The service reads Home Assistant data from InfluxDB, formats plugin-specific payloads, and pushes them to TRMNL webhooks from a single Python process.

## What It Does

- Weather dashboard with outdoor temperature, feels-like, humidity, wind, dew point, pressure trend, UV, and rain info
- Solar power line chart with load, grid, daily generation, and peak solar summary
- Solar summary bar chart with daily energy totals and weekly solar generation total
- Temperature chart with indoor and outdoor series
- Centralized webhook rate limiting with persistent exponential backoff

## Project Layout

- `app/main.py`: scheduler and plugin orchestration
- `app/plugins/`: weather, solar power, solar summary, and temperature plugins
- `app/state.py`: per-webhook state and backoff tracking
- `ui/`: ERB templates rendered by TRMNL
- `config/config.example.yml` and `config/secrets.example.yml`: local config templates

## Local Setup

1. Install dependencies:

```bash
pip install -r requirements.txt
```

2. Create local config files:

```bash
cp config/config.example.yml config/config.yml
cp config/secrets.example.yml config/secrets.yml
```

3. Fill in your InfluxDB settings, webhook IDs, and entity mappings.

## Running

Run locally:

```bash
python -m app.main
```

Run with Docker:

```bash
docker-compose up -d
docker-compose logs -f
docker-compose down
```

The container runs as `3444:3444`, uses `tini`, mounts config files read-only, and persists the webhook state file at `/tmp/last_trmnl_update.lock`.

## Notes

- All Flux queries should set `timezone.location(...)` using `general.influx_query_timezone`.
- `general.timezone` controls display formatting.
- There is no automated test suite yet; validate changes by running locally and reviewing logs.
