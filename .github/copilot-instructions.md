# TRMNL Personal Weather Station - AI Assistant Guide

## Project Overview
A multi-service weather and energy monitoring system that collects data from AmbientWeather stations and InfluxDB, then pushes formatted data to TRMNL e-ink displays via webhooks. Each service runs independently as a Dockerized daemon.

## Architecture

### Service Independence Pattern
Each subdirectory is a **standalone microservice** with its own:
- Python script (data collector/processor)
- `Dockerfile` + `docker-compose.yml` 
- `requirements.txt`
- Configuration (either `.env` or `config.yml`)

**Services:**
- `ambient_trmnl_webhook/` - Polls AmbientWeather API for live weather data
- `solar/` - Queries InfluxDB for solar power data (24h + 30d aggregates)
- `solar_summary/` - Daily solar energy summary calculations
- `temperature_chart/` - InfluxDB temperature data for Highcharts visualization

### Data Flow
```
AmbientWeather API → ambient_trmnl_webhook → TRMNL webhook
InfluxDB (Home Assistant) → solar/temperature services → TRMNL webhook
```

Each service formats data into TRMNL's `merge_variables` structure for ERB templates in `ui/`.

## Configuration Patterns

### Two Config Approaches
1. **Environment variables** (`.env` files): Used by `ambient_trmnl_webhook`, `temperature_chart`
   - Never committed (add to `.gitignore`)
   - Required vars: `WEBHOOK_ID`, API keys, `TIMEZONE`

2. **YAML config files**: Used by `solar/`, `solar_summary/`
   - `config.example.yml` is committed template
   - `config.yml` contains real credentials (gitignored)
   - Structured with sections: `general`, `influxdb`, `webhook`, `queries`

### InfluxDB Query Pattern
Services use **Flux queries** with placeholder substitution:
```python
flux_query = query_template.format(
    start_time=start_time,
    end_time=end_time,
    bucket=bucket
)
```
See [solar_data.py](solar/solar_data.py#L155-L165) for the `execute_query()` pattern.

## Key Development Patterns

### Custom InfluxDBClient Class
All InfluxDB services implement a lightweight client (not the official library):
- Direct HTTP POST to `/api/v2/query`
- CSV response parsing (splits on commas, handles headers)
- SSL verification disabled (`verify_ssl: false`)

Example: [solar/solar_data.py](solar/solar_data.py#L32-L98)

### Timezone Handling
**Always use pytz for timezone conversions**, especially for TRMNL display formatting:
```python
local_tz = pytz.timezone(TIMEZONE)  # From config
local_dt = utc_dt.astimezone(local_tz)
```

### TRMNL Webhook Format
All services POST to `https://usetrmnl.com/api/custom_plugins/{WEBHOOK_ID}`:
```json
{
  "merge_variables": {
    "tempf": 72.5,
    "date_pretty": "Sat 18 Jan, 02:15 PM",
    ...
  }
}
```

## UI Templates (ERB)
Located in `ui/`, these are **TRMNL-specific ERB templates**:
- Use double curly braces: `{{tempf}}`, `{{winddir_pretty}}`
- Styled for e-ink displays (black/white, specific grid layouts)
- `weather-chart.erb` includes Highcharts with tabular-nums font variant
- Custom helpers: `| round` filter, JavaScript data embedding via `{{js_evan_s_pws_temp}}`

## Docker Deployment

### Standard Pattern Per Service
```bash
cd <service-directory>
docker-compose up -d
```

### Dockerfile Conventions
- Base: `python:3.11-slim`
- Uses `tini` for proper signal handling (graceful shutdown)
- Single script execution: `CMD ["python", "<script>.py"]`

### Continuous Operation
Services run in infinite loops with configurable poll intervals:
- `poll_interval: 300` (5 minutes) - normal operation
- `retry_interval: 60` (1 minute) - on errors
- All services run as Docker containers on home lab infrastructure

## Testing & Debugging

### Local Testing
```bash
# Install deps
pip install -r requirements.txt

# Run directly (uses .env or config.yml)
python <script>.py
```

**Note**: Services are tested against live TRMNL webhooks - no local/mock webhook environment exists.

### Common Issues
1. **SSL verification errors**: Set `verify_ssl: false` in config
2. **Empty InfluxDB results**: Check Flux query syntax, entity_id filters
3. **Timezone display issues**: Verify `TIMEZONE` setting matches display location

## Adding New Services

1. Create subdirectory with structure:
   ```
   new_service/
     ├── new_service.py
     ├── Dockerfile
     ├── docker-compose.yml
     ├── requirements.txt
     └── config.example.yml (or use .env)
   ```

2. Follow `InfluxDBClient` pattern if querying InfluxDB
3. Format webhook payload with `merge_variables` key
4. Add corresponding ERB template to `ui/` directory

## External Dependencies
- **AmbientWeather**: Requires `AMBIENT_API_KEY` + `AMBIENT_APPLICATION_KEY` from [ambientweather.com](https://ambientweather.com/faqs/question/view/id/1834/)
- **TRMNL**: Create private plugin at [usetrmnl.com](https://docs.usetrmnl.com/go/plugin-marketplace/plugin-creation)
- **InfluxDB**: Home Assistant data store at `home_assistant/autogen` bucket
