# TRMNL Personal Weather Station - Consolidation Plan

## Overview

Consolidate the four separate microservices (`ambient_trmnl_webhook`, `solar`, `solar_summary`, `temperature_chart`) into a single unified Docker container with shared configuration and a common codebase.

## Current State

### Existing Services

| Service | Data Source | Webhook Purpose | Config Style |
|---------|-------------|-----------------|--------------|
| `ambient_trmnl_webhook/` | AmbientWeather API | Live weather display | `.env` |
| `solar/` | InfluxDB | 24h solar power chart | `config.yml` |
| `solar_summary/` | InfluxDB | 7-day solar energy bars | `config.yml` |
| `temperature_chart/` | InfluxDB | 24h temperature chart | `.env` |

### Issues with Current Architecture
- **Duplicated code**: `InfluxDBClient` class copied in 3 files
- **Inconsistent config**: Mix of `.env` and YAML files
- **Multiple containers**: 4 separate Docker images to build/maintain
- **Separate requirements**: Overlapping dependencies across 4 `requirements.txt` files

---

## TRMNL API Constraints

Reference: https://docs.usetrmnl.com/go/private-plugins/webhooks

### Rate Limits

| Tier | Requests per Hour |
|------|-------------------|
| Standard | 12 |
| TRMNL+ | 30 |

Requests exceeding the rate limit receive a `429` response. Enabling "Debug Logs" on the plugin settings page temporarily increases the rate limit during development.

**Implication for this project**: With a 5-minute (300 second) poll interval, we send **12 requests/hour per plugin**. This is exactly at the standard tier limit. Consider:
- Slightly increasing `poll_interval` to 301+ seconds for safety margin
- Or upgrading to TRMNL+ for more headroom

**Configuration**: The app includes a `trmnl_plus_subscriber: true/false` setting to toggle appropriate rate limit and payload size warnings at runtime.

### Payload Size Limits

| Tier | Max Payload |
|------|-------------|
| Standard | 2 KB |
| TRMNL+ | 5 KB |

**Implication**: Chart data (24h of readings at 10-min intervals = 144 points) must be carefully sized. JSON arrays of `[timestamp, value]` pairs can grow quickly.

### Merge Strategies

TRMNL supports three strategies for updating plugin data:

1. **Replace (default)**: Full replacement of `merge_variables`
2. **`deep_merge`**: Merges new values into existing nested structures
3. **`stream`**: Appends to arrays with optional `stream_limit` to cap array size

```bash
# Example: stream strategy with limit
curl "https://usetrmnl.com/api/custom_plugins/{UUID}" \
  -H "Content-Type: application/json" \
  -d '{"merge_variables": {"temps": [42]}, "merge_strategy": "stream", "stream_limit": 144}' \
  -X POST
```

**Potential optimization**: For chart plugins, we could use `stream` strategy to append only new data points instead of sending the full 24h dataset each time. This would reduce payload size and bandwidth.

### API Endpoint

```
POST https://usetrmnl.com/api/custom_plugins/{WEBHOOK_UUID}
Content-Type: application/json

{"merge_variables": {...}}
```

---

## Target Architecture

### Directory Structure

```
trmnl-pws/
├── app/
│   ├── __init__.py
│   ├── main.py                 # Entry point - scheduler loop
│   ├── config.py               # Configuration loader
│   ├── influx_client.py        # Shared InfluxDB client (official library)
│   ├── webhook.py              # Shared TRMNL webhook poster
│   ├── plugins/
│   │   ├── __init__.py
│   │   ├── weather.py          # Weather data (from InfluxDB)
│   │   ├── solar_power.py      # Solar power chart (24h)
│   │   ├── solar_summary.py    # Solar energy summary (7-day)
│   │   └── temperature_chart.py # Temperature chart (24h)
│   └── utils/
│       ├── __init__.py
│       ├── formatting.py       # Date/time formatting helpers
│       └── conversions.py      # Unit conversions (wind direction, etc.)
├── config/
│   ├── config.example.yml      # Committed template
│   └── secrets.example.yml     # Committed template for secrets
├── Dockerfile
├── docker-compose.yml
├── requirements.txt
├── README.md
├── docs/
│   └── consolidation_plan.md
└── ui/                         # Existing ERB templates (unchanged)
    ├── weather-full.erb
    ├── weather-half.erb
    ├── weather-chart.erb
    ├── solar.erb
    └── solar_summary.erb
```

### Files to be Removed (after migration)
```
ambient_trmnl_webhook/    # Entire directory
solar/                    # Entire directory  
solar_summary/            # Entire directory
temperature_chart/        # Entire directory
```

---

## Configuration Design

### `config/config.yml` (committed)

```yaml
general:
  log_level: INFO
  timezone: America/New_York
  poll_interval: 300              # 5 minutes - all plugins use the same interval
  trmnl_plus_subscriber: false    # Set to true for higher rate limits (30/hr) and payload size (5KB)

influxdb:
  url: http://127.0.0.1:8086          # InfluxDB URL (adjust for your network)
  org: bellmore
  bucket: home_assistant/autogen
  verify_ssl: false

plugins:
  weather:
    enabled: true
    webhook_id_key: WEATHER_WEBHOOK_ID    # Reference to secrets.yml
    entities:
      outdoor_temp: evan_s_pws_temperature
      indoor_temp: evan_s_pws_inside_temperature
      humidity: evan_s_pws_humidity
      indoor_humidity: evan_s_pws_humidity_indoor
      wind_speed: evan_s_pws_wind_speed
      wind_gust: evan_s_pws_wind_gust
      wind_direction: evan_s_pws_wind_direction
      dew_point: evan_s_pws_dew_point
      feels_like: evan_s_pws_feels_like
      pressure: evan_s_pws_relative_pressure
      daily_rain: evan_s_pws_daily_rain
      uv_index: evan_s_pws_uv_index
      solar_radiation: evan_s_pws_irradiance

  solar_power:
    enabled: true
    webhook_id_key: SOLAR_POWER_WEBHOOK_ID
    hours_back: 7                          # From configs/solar.yml
    entities:
      solar_power: bellmore_solar_power      # kW - current solar generation
      grid_power: bellmore_grid_power        # kW - grid import/export
      load_power: bellmore_load_power        # kW - home consumption
      battery_power: bellmore_battery_power  # kW - battery charge/discharge
    display_names:
      bellmore_solar_power: Solar
      bellmore_grid_power: Grid
      bellmore_load_power: Home
      bellmore_battery_power: Battery

  solar_summary:
    enabled: true
    webhook_id_key: SOLAR_SUMMARY_WEBHOOK_ID
    days_back: 7
    entities:
      solar_energy: bellmore_solar_generated   # kWh - daily solar generation
      grid_energy: bellmore_grid_imported      # kWh - daily grid import
      load_energy: bellmore_home_usage         # kWh - daily home consumption

  temperature_chart:
    enabled: true
    webhook_id_key: TEMPERATURE_CHART_WEBHOOK_ID
    hours_back: 12                         # From configs/temperature_chart.env
    entities:
      outdoor_temp: evan_s_pws_temperature
```

### `config/secrets.yml` (gitignored)

```yaml
influxdb:
  token: "0JdC_iPdTctUgbXQk0TzUSvYRYzmexTAq57aHsboQgvXhMz6HOhIjRkJBuPjcMtIZlxAmNFYTAirYLkYFHgBlg=="

# AmbientWeather API credentials (not needed if using InfluxDB for weather)
ambient_weather:
  api_key: "4dd75f516e124d4a9cbead38e2826a3013c42afd3b0b4c5f85b9194b530df163"
  application_key: "d7574e9ccac1497a8eea82cf6e8b0751de5715553b1844eb83d68d60262ab4a5"

webhooks:
  WEATHER_WEBHOOK_ID: "e2037c24-42ad-4726-b810-9ef9ddb24e81"
  SOLAR_POWER_WEBHOOK_ID: "a659844b-e66d-4b6d-a6c5-5a0aba64bb66"
  SOLAR_SUMMARY_WEBHOOK_ID: "f28f9910-e301-45ad-a755-cbc5b787ef59"
  TEMPERATURE_CHART_WEBHOOK_ID: "e8ba516a-a56c-4e12-8fd0-4b632c6aaf00"
```

### `config/secrets.example.yml` (committed)

```yaml
influxdb:
  token: "your-influxdb-token-here"

# AmbientWeather API credentials (not needed if using InfluxDB for weather data)
ambient_weather:
  api_key: "your-ambient-api-key"
  application_key: "your-ambient-application-key"

webhooks:
  WEATHER_WEBHOOK_ID: "your-weather-webhook-id"
  SOLAR_POWER_WEBHOOK_ID: "your-solar-power-webhook-id"
  SOLAR_SUMMARY_WEBHOOK_ID: "your-solar-summary-webhook-id"
  TEMPERATURE_CHART_WEBHOOK_ID: "your-temperature-chart-webhook-id"
```

---

## Entity Mapping (Weather Plugin)

Based on InfluxDB data export, mapping from current AmbientWeather fields to InfluxDB entities:

| Webhook Field | InfluxDB Entity | Unit |
|---------------|-----------------|------|
| `tempf` | `evan_s_pws_temperature` | °F |
| `tempinf` | `evan_s_pws_inside_temperature` | °F |
| `humidity` | `evan_s_pws_humidity` | % |
| `humidityin` | `evan_s_pws_humidity_indoor` | % |
| `windspeedmph` | `evan_s_pws_wind_speed` | mph |
| `windgustmph` | `evan_s_pws_wind_gust` | mph |
| `winddir` | `evan_s_pws_wind_direction` | ° |
| `dewPoint` | `evan_s_pws_dew_point` | °F |
| `feelsLike` | `evan_s_pws_feels_like` | °F |
| `baromrelin` | `evan_s_pws_relative_pressure` | inHg |
| `dailyrainin` | `evan_s_pws_daily_rain` | in |
| `uv` | `evan_s_pws_uv_index` | Index |
| `solarradiation` | `evan_s_pws_irradiance` | W/m² |

### Flux Query for Entity Discovery

Use this query to list all available entities and their current values:

```flux
from(bucket: "home_assistant/autogen")
  |> range(start: -3d)
  |> filter(fn: (r) => r["entity_id"] =~ /^evan_s_pws|^bellmore/)
  // Filter for the value field specifically to ensure _value exists
  |> filter(fn: (r) => r["_field"] == "value")
  |> last()
  // Cast _value to string to harmonize mixed types (e.g., float vs string)
  |> map(fn: (r) => ({ 
      r with 
      _value: string(v: r._value) 
    }))
  |> keep(columns: ["entity_id", "_measurement", "_value", "_time"])
  |> group()
```

### Note on `lastRain`
The current weather plugin shows "last rain X hours/days ago". This requires either:
1. Querying InfluxDB for the last time `evan_s_pws_daily_rain` increased
2. Storing the last rain timestamp in memory/file

**Recommendation**: Query InfluxDB for last non-zero `evan_s_pws_precipitation_intensity` value.

---

## Implementation Details

### Shared InfluxDB Client (`app/influx_client.py`)

Use the official `influxdb-client` package instead of the custom HTTP client:

```python
from influxdb_client import InfluxDBClient

def create_client(config: dict, secrets: dict) -> InfluxDBClient:
    return InfluxDBClient(
        url=config['influxdb']['url'],
        token=secrets['influxdb']['token'],
        org=config['influxdb']['org'],
        verify_ssl=config['influxdb'].get('verify_ssl', False)
    )
```

### Shared Webhook Poster (`app/webhook.py`)

```python
import requests

TRMNL_BASE_URL = "https://usetrmnl.com/api/custom_plugins"

def post_to_webhook(webhook_id: str, merge_variables: dict) -> bool:
    url = f"{TRMNL_BASE_URL}/{webhook_id}"
    payload = {"merge_variables": merge_variables}
    response = requests.post(url, json=payload)
    return response.ok
```

### Plugin Interface (`app/plugins/__init__.py`)

Each plugin implements a common interface:

```python
from abc import ABC, abstractmethod
from typing import Dict, Any

class BasePlugin(ABC):
    def __init__(self, config: dict, secrets: dict, influx_client):
        self.config = config
        self.secrets = secrets
        self.influx_client = influx_client
    
    @abstractmethod
    def collect_data(self) -> Dict[str, Any]:
        """Query InfluxDB and return merge_variables dict"""
        pass
    
    @abstractmethod
    def get_webhook_id(self) -> str:
        """Return the webhook ID for this plugin"""
        pass
```

### Main Scheduler (`app/main.py`)

```python
import time
import logging
from app.config import load_config, load_secrets
from app.influx_client import create_client
from app.webhook import post_to_webhook
from app.plugins.weather import WeatherPlugin
from app.plugins.solar_power import SolarPowerPlugin
from app.plugins.solar_summary import SolarSummaryPlugin
from app.plugins.temperature_chart import TemperatureChartPlugin

def main():
    config = load_config('config/config.yml')
    secrets = load_secrets('config/secrets.yml')
    client = create_client(config, secrets)
    
    plugins = []
    if config['plugins']['weather']['enabled']:
        plugins.append(WeatherPlugin(config, secrets, client))
    if config['plugins']['solar_power']['enabled']:
        plugins.append(SolarPowerPlugin(config, secrets, client))
    # ... etc
    
    poll_interval = config['general']['poll_interval']
    
    while True:
        for plugin in plugins:
            try:
                data = plugin.collect_data()
                webhook_id = plugin.get_webhook_id()
                post_to_webhook(webhook_id, data)
            except Exception as e:
                logging.error(f"Plugin {plugin.__class__.__name__} failed: {e}")
        
        time.sleep(poll_interval)
```

---

## Docker Configuration

### `Dockerfile`

```dockerfile
FROM python:3.11-slim

WORKDIR /app

# Install tini for proper signal handling
RUN apt-get update && apt-get install -y tini && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY app/ ./app/

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["python", "-m", "app.main"]
```

### `docker-compose.yml`

```yaml
version: '3.8'

services:
  trmnl-pws:
    build: .
    container_name: trmnl-pws
    restart: unless-stopped
    volumes:
      - ./config/config.yml:/app/config/config.yml:ro
      - ./config/secrets.yml:/app/config/secrets.yml:ro
    environment:
      - TZ=America/New_York
```

### `requirements.txt`

```
influxdb-client>=1.36.0
requests>=2.28.0
pyyaml>=6.0
pytz>=2023.3
urllib3>=1.26.0
```

---

## Migration Steps

### Phase 1: Setup New Structure
1. Create `app/` directory structure
2. Create shared modules (`config.py`, `influx_client.py`, `webhook.py`)
3. Create plugin base class and utility modules
4. Create `config/config.example.yml` and `config/secrets.example.yml`
5. Create new root-level `Dockerfile`, `docker-compose.yml`, `requirements.txt`

### Phase 2: Migrate Plugins
1. Port `temperature_chart` → `app/plugins/temperature_chart.py`
2. Port `solar` → `app/plugins/solar_power.py`
3. Port `solar_summary` → `app/plugins/solar_summary.py`
4. Create new `app/plugins/weather.py` (InfluxDB-based, replacing AmbientWeather API)

### Phase 3: Testing
1. Create `config/config.yml` and `config/secrets.yml` with real values
2. Run locally: `python -m app.main`
3. Verify all 4 webhooks receive correct data
4. Build and test Docker container

### Phase 4: Cleanup
1. `git rm -r ambient_trmnl_webhook/`
2. `git rm -r solar/`
3. `git rm -r solar_summary/`
4. `git rm -r temperature_chart/`
5. Update root `README.md`
6. Update `.gitignore` to include `config/secrets.yml`

---

## Git Commands Summary

```bash
# Create new structure
mkdir -p app/plugins app/utils config

# Move/create files (Phase 1-2)
# ... implementation ...

# Cleanup old directories (Phase 4)
git rm -r ambient_trmnl_webhook/
git rm -r solar/
git rm -r solar_summary/
git rm -r temperature_chart/

# Update gitignore
echo "config/secrets.yml" >> .gitignore

# Commit
git add -A
git commit -m "Consolidate services into single unified container"
```

---

## Benefits of Consolidation

1. **Single container**: One image to build, deploy, and monitor
2. **Shared code**: No more duplicated InfluxDB client or config loading
3. **Consistent config**: All YAML, clear separation of config vs secrets
4. **Official library**: Using `influxdb-client` instead of custom HTTP code
5. **Unified logging**: Single log stream for all plugins
6. **Easier maintenance**: One `requirements.txt`, one `Dockerfile`
7. **Plugin architecture**: Easy to add new data sources in the future

---

## Open Questions / Future Enhancements

1. **Health checks**: Add HTTP endpoint for Docker health checks?
2. **Metrics**: Add Prometheus metrics for monitoring?
3. **Retry logic**: Implement exponential backoff for failed webhook posts?
4. **Caching**: Cache InfluxDB queries to reduce load?
5. **Individual intervals**: Allow per-plugin poll intervals if needed later?
6. **Stream strategy**: Use TRMNL's `stream` merge strategy to append only new data points for chart plugins, reducing payload size?
7. **Rate limit safety**: Increase `poll_interval` slightly above 300s to stay safely under the 12/hour limit?

---

## After-Action Report

### What Went Well

1. **Plugin Architecture**: The `BasePlugin` abstract class pattern proved effective. It enforced a consistent interface across all four plugins while allowing each to implement their own data collection logic independently. Easy to test and extend.

2. **Unified Configuration**: Moving from a mix of `.env` and `config.yml` files to a consistent YAML-based approach (`config.yml` + `secrets.yml`) significantly improved clarity and maintainability. Clear separation between configuration (committed) and secrets (gitignored).

3. **Official InfluxDB Client**: Adopting the official `influxdb-client` library eliminated custom HTTP code and provided automatic retries, connection pooling, and better error handling out of the box.

4. **Docker Containerization**: Single Dockerfile with tini for proper signal handling simplified deployment. No more managing 4 separate container images.

5. **Shared Utilities**: Consolidating formatting, conversion, and InfluxDB client code eliminated duplication and reduced maintenance surface area.

### What Needed Iteration

1. **Payload Size Optimization**: Initial implementation of `solar_power` plugin exceeded the 2KB TRMNL standard tier limit (3182 bytes). Solution: Added configurable `aggregation_interval_minutes` setting (default 30 min), reducing the solar_power payload by 67% to 1153 bytes while maintaining visual quality.

2. **State Management Architecture**: Initial approach placed state tracking at the plugin level. This caused multiple state file loads per iteration and made backoff logic fragmented. Solution: Elevated state management to the orchestration layer (`main.py`), loading state once per iteration and passing it to functions that need it.

3. **Rate Limit Handling**: Initially, failed webhook posts would retry immediately on the next poll interval, potentially hammering the TRMNL API during rate limit windows. Solution: Implemented exponential backoff algorithm with caching:
   - Per-webhook failure tracking in state file (`/tmp/last_trmnl_update.lock`)
   - Exponential backoff: `2^failure_count × base_interval`, capped at 3600s (1 hour)
   - State persists across container restarts via volume mount

4. **Timezone Issues in Flux Queries**: Queries used UTC by default, causing incorrect date boundaries (showing tomorrow's data). Solution: Added `influx_query_timezone` configuration setting and updated all Flux queries to include `import "timezone"` with `option location = timezone.location(name: "...")` to ensure date boundaries align with the actual timezone.

5. **Solar Summary Data Retrieval**: Initially configured to query `bellmore_solar_generated` (kWh energy entity) which didn't exist or had no data. Solution: Changed to query `bellmore_solar_power` (kW power entity) and use Flux `integral()` aggregation to calculate daily energy, making the plugin functional.

6. **Smart Polling Inefficiency**: Initially, `main.py` always slept the full `poll_interval` even when backoff was active, wasting time before retry windows expired. Solution: Calculate `min_wait_seconds` across all plugins based on their backoff state, sleep only until the next plugin is ready, enabling early wakeup when backoff windows close.

7. **State File I/O Overhead**: Early implementation loaded and saved state multiple times per iteration. Solution: Implemented caching pattern - load state once at the start of each iteration, pass the state object to all decision functions, save only once at the end if modified.

### Key Learnings

- **InfluxDB Timezone Handling**: Always specify timezone explicitly in Flux queries, even if you think UTC will work. Query timezone should be independent of display timezone.
- **Payload Size Matters**: Monitor webhook payload sizes against TRMNL limits. Aggregation intervals are effective levers for reducing size without losing signal.
- **Stateful Rate Limiting**: When implementing backoff, store state durably (file/database) and make it accessible to orchestration logic, not individual plugins.
- **Plugin Simplicity**: Plugins should focus solely on data collection. Cross-cutting concerns like rate limiting, retries, and state management belong in the orchestration layer.

### Testing Approach That Worked

- **Manual log inspection**: Running the unified app locally with `log_level: DEBUG` revealed payload sizes, query execution, timezone calculations, and state transitions.
- **Incremental deployment**: Tested each plugin independently before integrating all four.
- **State file inspection**: Examining `/tmp/last_trmnl_update.lock` during development provided visibility into backoff calculations and timestamps.
- **Real webhook posts**: All verification was done against live TRMNL webhooks with actual data, catching edge cases that would be missed in unit tests.

### Deployment Status

✅ **Production Ready**: All four plugins functioning correctly with proper data collection, rate limit protection, and timezone handling. State tracking prevents violations across container restarts. Smart polling minimizes unnecessary waiting.
