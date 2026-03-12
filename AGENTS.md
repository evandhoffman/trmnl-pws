# Repository Guidelines

## Project Structure & Module Organization
This repo is a unified Python service that reads InfluxDB data and pushes TRMNL webhook payloads. `app/main.py` runs the scheduler, `app/plugins/` contains collectors, `app/state.py` handles per-webhook rate-limit state, and `app/webhook.py` posts validated payloads. Shared helpers live in `app/utils/`. Keep live config in `config/config.yml` and `config/secrets.yml` from the `.example.yml` templates; UI templates live in `ui/`, and supporting notes live in `docs/`.

## Build, Test, and Development Commands
Install dependencies with `pip install -r requirements.txt`.

Run locally:
```bash
python -m app.main
```

Run in Docker:
```bash
docker-compose up -d
docker-compose logs -f
docker-compose down
```
The container runs as `3444:3444`, uses `tini`, mounts config read-only, and persists `/tmp/last_trmnl_update.lock`.

## Coding Style & Naming Conventions
Use 4-space indentation, `snake_case` for functions, variables, and config keys, and `PascalCase` for classes. Plugins should inherit from `BasePlugin` and implement `collect_data()` and `get_webhook_id()`. Keep plugin names aligned with config sections, for example `solar_summary` -> `SolarSummaryPlugin`. Prefer concise logging with `logger.info()`, `logger.warning()`, and actionable error text.

## Architecture & Data Rules
Data flow is `InfluxDB -> plugin -> state check/backoff -> TRMNL webhook`. All Flux queries must set a timezone location:
```flux
import "timezone"
option location = timezone.location(name: "America/New_York")
```
Use `general.timezone` for display formatting and `general.influx_query_timezone` for Flux date boundaries. Respect TRMNL limits: standard tier is 12 requests/hour and 2 KB payloads; TRMNL+ raises that to 30 requests/hour and 5 KB via `trmnl_plus_subscriber: true`.

## Testing & Debugging Guidelines
There is no automated test suite today. Validate changes by running the service locally or through Docker and inspecting logs. For plugin changes, verify the query returns data, the payload stays under TRMNL limits, and state/backoff behavior still works. Set `log_level: DEBUG` to inspect Flux queries, record counts, payload sizes, and state operations. Common failures are mismatched `entity_id` filters, bad timezone handling, and unwritable state files.

## Commit & Pull Request Guidelines
Recent history uses short imperative subjects such as `fix PR review issues`, `renamed`, and `add CLAUDE.md for Claude Code guidance`. Keep commits focused and small. Pull requests should describe the runtime impact, note config or webhook changes, link the issue when relevant, and include screenshots or payload examples for `ui/` or display-output changes.

## Configuration & Extension Notes
Never commit real secrets. When adding a plugin, update `app/plugins/`, add config and secret keys to both example and local YAML files, and register the plugin in `app/main.py`. Preserve the existing state-management flow so retries and backoff remain centralized.
