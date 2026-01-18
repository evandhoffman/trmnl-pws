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
  poll_interval: 300        # 5 minutes - all plugins use the same interval

influxdb:
  url: https://homeassistant.local:8086
  org: home_assistant
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
    hours_back: 24
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
    hours_back: 24
    entities:
      outdoor_temp: evan_s_pws_temperature
```

### `config/secrets.yml` (gitignored)

```yaml
influxdb:
  token: "your-influxdb-token-here"

webhooks:
  WEATHER_WEBHOOK_ID: "abc123..."
  SOLAR_POWER_WEBHOOK_ID: "def456..."
  SOLAR_SUMMARY_WEBHOOK_ID: "ghi789..."
  TEMPERATURE_CHART_WEBHOOK_ID: "jkl012..."
```

### `config/secrets.example.yml` (committed)

```yaml
influxdb:
  token: "your-influxdb-token-here"

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
