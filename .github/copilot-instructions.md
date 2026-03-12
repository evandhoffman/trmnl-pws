# TRMNL Personal Weather Station - AI Assistant Guide

## Project Overview
A unified weather and energy monitoring system that collects data from InfluxDB (Home Assistant) and pushes formatted data to TRMNL e-ink displays via webhooks. Runs as a single consolidated Docker container with a plugin architecture.

## Architecture

### Unified Service Pattern (Consolidated)
The project has been consolidated from 4 separate microservices into a **single unified application** with a plugin architecture:

**Directory Structure:**
```
/app
  ├── __init__.py              # Package marker (v2.0.0)
  ├── config.py                # YAML config loading with validation
  ├── influx_client.py         # Official InfluxDB client wrapper
  ├── webhook.py               # TRMNL webhook poster with payload validation
  ├── state.py                 # Rate limit state tracking & exponential backoff
  ├── main.py                  # Scheduler loop with orchestration
  ├── plugins/
  │   ├── __init__.py          # BasePlugin abstract class
  │   ├── weather.py           # InfluxDB weather data (LatestValue queries)
  │   ├── solar_power.py       # 7-hour power chart with aggregation
  │   ├── solar_summary.py     # 7-day energy summary via integral
  │   └── temperature_chart.py # 12-hour temperature chart
  └── utils/
      ├── formatting.py        # Timestamp formatting utilities
      └── conversions.py       # Unit conversions & compass directions
/config
  ├── config.yml              # Live config (gitignored)
  ├── config.example.yml      # Template config
  ├── secrets.yml             # Live secrets (gitignored)
  └── secrets.example.yml     # Template secrets
```

**Single Entry Point:**
- `docker-compose.yml` - Runs unified application with volume mounts
- `Dockerfile` - Python 3.11-slim with tini for signal handling
- `requirements.txt` - Single dependency list

### Data Flow
```
InfluxDB (Home Assistant) → 4 plugins → State tracking → Rate limit backoff → TRMNL webhooks
  (weather, solar_power, solar_summary, temperature_chart)
```

## Configuration Patterns

### YAML Configuration (Unified)
Single configuration file approach:
- `config.yml` (gitignored) - Live settings with actual credentials
- `secrets.yml` (gitignored) - Webhook IDs and InfluxDB tokens
- Both have `.example.yml` templates in repository

**Key Settings:**
```yaml
general:
  log_level: INFO
  timezone: America/New_York                    # Display timezone
  influx_query_timezone: America/New_York      # Query timezone for date boundaries
  poll_interval: 300                           # 5 min (12 req/hr at standard tier)
  trmnl_plus_subscriber: false

influxdb:
  url: http://192.168.1.32:8086
  org: bellmore
  bucket: home_assistant/autogen
  verify_ssl: false

plugins:
  weather:
    enabled: true
    webhook_id_key: WEATHER_WEBHOOK_ID
    # ... entity mappings ...
```

## Key Development Patterns

### Official InfluxDB Client
Uses official `influxdb-client` library (not custom HTTP):
- Automatic retries and connection pooling
- Query API with Flux support
- Better error handling

### Timezone Handling in Flux Queries
**All Flux queries must include timezone location** for correct date boundaries:
```flux
import "timezone"
option location = timezone.location(name: "America/New_York")
```

Without this, queries use UTC which can show tomorrow's data when it's actually today.

### Plugin Architecture
All plugins inherit from `BasePlugin`:
```python
class MyPlugin(BasePlugin):
    def collect_data(self) -> Dict[str, Any]:
        """Return merge_variables dict"""
    def get_webhook_id(self) -> str:
        """Return webhook ID from secrets"""
```

Helper methods:
- `get_bucket()` - InfluxDB bucket from config
- `get_timezone()` - Display timezone
- `get_influx_query_timezone()` - Query timezone for Flux

### State Tracking & Rate Limit Backoff
State file: `/tmp/last_trmnl_update.lock` (or configurable via `STATE_LOCK_PATH`)

Stores per-webhook:
- Last attempt timestamp (UTC ISO format)
- Failure count for exponential backoff

**Exponential Backoff Algorithm:**
- 1st failure: wait 600s (2 × 300s poll_interval)
- 2nd failure: wait 1200s (4 × 300s)
- 3rd failure: wait 2400s (8 × 300s)
- 4th+ failure: capped at 3600s (1 hour max)

Resets on success. Prevents hammering TRMNL API during rate limits.

### Orchestration Pattern (main.py)
State management at orchestration level, not in plugins:
1. Load state once per iteration
2. Calculate min_wait_seconds across all plugins
3. Sleep only until next plugin is ready (not always full poll_interval)
4. Save state once per iteration if modified

This enables smart waiting - if backoff is active, wakes up early to retry.

### TRMNL Webhook Format
All plugins return dict with `merge_variables`:
```python
return {
    "merge_variables": {
        "temperature": 72.5,
        "humidity": 65,
        # ... other fields ...
    }
}
```

Webhook posting includes:
- Payload size validation (2KB standard, 5KB TRMNL+)
- Rate limit detection (HTTP 429)
- Status returns: 'success', 'rate_limited', 'failed'

## Docker Deployment

### Single Container Pattern
```bash
docker-compose up -d
```

Runs as user 3444:3444 (configurable in docker-compose.yml)

### State Persistence
Lock file mounted on host to survive container restarts:
```yaml
volumes:
  - ./last_trmnl_update.lock:/tmp/last_trmnl_update.lock
```

## Testing & Debugging

### Local Testing
```bash
python -m app.main
```

Uses config.yml + secrets.yml from `/config` directory

### Debug Logging
Set `log_level: DEBUG` in config to see:
- Flux query details
- InfluxDB record counts
- Timestamp calculations
- State file operations

### Common Issues
1. **Timezone mismatch**: Ensure `influx_query_timezone` matches `timezone`
2. **No query results**: Check Flux `entity_id` filters match actual InfluxDB data
3. **Solar summary empty**: Requires power data (kW measurement) not energy (kWh)
4. **Rate limit backoff stuck**: Check `/tmp/last_trmnl_update.lock` permissions for user 3444

## Adding New Plugins

1. Create `app/plugins/new_plugin.py` inheriting from `BasePlugin`
2. Implement `collect_data()` and `get_webhook_id()`
3. Add config section to `config.example.yml` and `config.yml`
4. Add webhook ID key to `secrets.example.yml` and `secrets.yml`
5. Initialize in `main.py` plugin list
6. Respect rate limit backoff via state management

## External Dependencies
- **TRMNL**: Create private plugin at [usetrmnl.com](https://docs.usetrmnl.com/go/plugin-marketplace/plugin-creation)
- **InfluxDB**: Home Assistant data store at `home_assistant/autogen` bucket
- **Docker**: For containerized deployment
