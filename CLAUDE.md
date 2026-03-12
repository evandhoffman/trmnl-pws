# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

A unified weather and energy monitoring system that pulls data from InfluxDB (Home Assistant) and pushes formatted payloads to TRMNL e-ink displays via webhooks. Runs as a single Docker container with a plugin-based architecture (consolidated from 4 separate microservices in v2.0.0).

## Commands

```bash
# Run locally (requires config/config.yml and config/secrets.yml)
python -m app.main

# Docker
docker-compose up -d
docker-compose logs -f
docker-compose down
```

No test suite — testing is done by running locally and inspecting logs.

## Architecture

**Data flow:** InfluxDB → plugins → state check (rate limiting) → TRMNL webhook API

### Plugin System

All plugins live in `app/plugins/` and inherit from `BasePlugin` (`app/plugins/__init__.py`). Required methods:
- `collect_data() -> Dict[str, Any]` — queries InfluxDB, returns `{"merge_variables": {...}}`
- `get_webhook_id() -> str` — returns webhook UUID from secrets config

To add a plugin: create the class, add config section to `config.example.yml`/`config.yml`, add webhook ID to `secrets.example.yml`/`secrets.yml`, initialize in `main.py`.

### State & Rate Limiting (`app/state.py`)

Persists per-webhook `{timestamp, failure_count}` to `/tmp/last_trmnl_update.lock` (JSON). Implements exponential backoff: `poll_interval × 2^failure_count`, capped at 3600s. Resets on success.

The orchestrator (`main.py`) loads/saves state once per iteration and sleeps only until the next plugin is ready — not a fixed interval.

### TRMNL API Constraints

- Standard tier: 12 req/hr, 2KB payload limit
- TRMNL+ tier: 30 req/hr, 5KB payload limit
- Set `trmnl_plus_subscriber: true` in config to adjust limits
- HTTP 429 → increments failure_count, triggers backoff

### Flux Query Timezone

**All Flux queries must import and set timezone location** or date boundaries will be calculated in UTC:
```flux
import "timezone"
option location = timezone.location(name: "America/New_York")
```

Config has two separate timezone fields:
- `timezone` — for display formatting
- `influx_query_timezone` — for Flux query date boundaries

### Configuration

- `config/config.yml` — settings (gitignored; use `config.example.yml` as template)
- `config/secrets.yml` — InfluxDB token and webhook IDs (gitignored; use `secrets.example.yml`)

### Docker

- Runs as user `3444:3444`
- State file mounted from host for persistence across restarts
- Uses `tini` for proper signal handling
- `network_mode: host` so it can reach InfluxDB on the local network

## Debugging

Set `log_level: DEBUG` in `config.yml` to see Flux query text, InfluxDB record counts, payload sizes, and state file operations.

Common issues:
- **No InfluxDB results** — check `entity_id` filters in Flux queries match actual entity names in Home Assistant
- **Backoff stuck** — check `/tmp/last_trmnl_update.lock` is writable by user 3444
- **Wrong date boundaries** — verify `influx_query_timezone` is set correctly in config and all Flux queries include the timezone import
