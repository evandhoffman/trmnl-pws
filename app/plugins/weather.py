"""Weather plugin - queries InfluxDB for weather data"""

import logging
from datetime import datetime, timezone
from typing import Dict, Any, Optional, Iterable, List
from app.plugins import BasePlugin
from app.utils.formatting import format_timestamp_for_display, format_relative_time
from app.utils.conversions import (
    format_wind_description,
    format_compact_wind,
    round_value,
)

logger = logging.getLogger(__name__)


class WeatherPlugin(BasePlugin):
    """Plugin for collecting and formatting weather data from InfluxDB"""

    def __init__(self, config: Dict[str, Any], secrets: Dict[str, Any], influx_client):
        super().__init__(config, secrets, influx_client)
        self.plugin_config = config["plugins"]["weather"]
        self.plugin_name = "Weather"

    def _query_latest_value(self, entity_id: str, measurement: str) -> Optional[tuple]:
        """
        Query the latest value for a specific entity

        Args:
            entity_id: The entity ID to query
            measurement: The measurement type (e.g., '°F', '%', 'mph')

        Returns:
            Tuple of (value, timestamp) or None if not found
        """
        bucket = self.get_bucket()
        query_tz = self.get_influx_query_timezone()

        flux_query = f"""
import "timezone"

option location = timezone.location(name: "{query_tz}")

from(bucket: "{bucket}")
    |> range(start: -24h)
    |> filter(fn: (r) => r["entity_id"] == "{entity_id}")
    |> filter(fn: (r) => r["_measurement"] == "{measurement}")
    |> filter(fn: (r) => r["_field"] == "value")
    |> last()
        """

        query_api = self.influx_client.query_api()
        tables = query_api.query(flux_query)

        for table in tables:
            for record in table.records:
                value = record.get_value()
                timestamp = record.get_time()
                return (value, timestamp)

        return None

    def _query_latest_values(
        self, entity_measurements: Iterable[tuple[str, str]]
    ) -> Dict[str, tuple]:
        """Query the latest values for multiple entity/measurement pairs."""
        pairs = list(entity_measurements)
        if not pairs:
            return {}

        bucket = self.get_bucket()
        query_tz = self.get_influx_query_timezone()
        filters = " or ".join(
            [
                f'(r["entity_id"] == "{entity_id}" and r["_measurement"] == "{measurement}")'
                for entity_id, measurement in pairs
            ]
        )

        flux_query = f"""
import "timezone"

option location = timezone.location(name: "{query_tz}")

from(bucket: "{bucket}")
    |> range(start: -24h)
    |> filter(fn: (r) => r["_field"] == "value")
    |> filter(fn: (r) => r["domain"] == "sensor")
    |> filter(fn: (r) => {filters})
    |> last()
        """

        latest_values = {}
        tables = self.influx_client.query_api().query(flux_query)
        for table in tables:
            for record in table.records:
                entity_id = record.values.get("entity_id")
                measurement = record.values.get("_measurement")
                if entity_id and measurement:
                    latest_values[(entity_id, measurement)] = (
                        record.get_value(),
                        record.get_time(),
                    )

        return latest_values

    def _query_last_rain(self, entity_id: str) -> Optional[datetime]:
        """
        Query the last time it rained (precipitation_intensity > 0)

        Returns:
            Timestamp of last rain or None
        """
        bucket = self.get_bucket()
        query_tz = self.get_influx_query_timezone()

        flux_query = f"""
import "timezone"

option location = timezone.location(name: "{query_tz}")

from(bucket: "{bucket}")
    |> range(start: -30d)
    |> filter(fn: (r) => r["entity_id"] == "{entity_id}")
    |> filter(fn: (r) => r["_field"] == "value")
    |> filter(fn: (r) => r["_value"] > 0.0)
    |> last()
        """

        query_api = self.influx_client.query_api()
        tables = query_api.query(flux_query)

        for table in tables:
            for record in table.records:
                return record.get_time()

        return None

    def _query_latest_value_before(
        self, entity_id: str, measurement: str, hours_ago: int
    ) -> Optional[tuple]:
        """Query the latest value available before a cutoff time."""
        bucket = self.get_bucket()
        query_tz = self.get_influx_query_timezone()

        flux_query = f"""
import "timezone"

option location = timezone.location(name: "{query_tz}")

from(bucket: "{bucket}")
    |> range(start: -24h, stop: -{hours_ago}h)
    |> filter(fn: (r) => r["entity_id"] == "{entity_id}")
    |> filter(fn: (r) => r["_measurement"] == "{measurement}")
    |> filter(fn: (r) => r["_field"] == "value")
    |> last()
        """

        tables = self.influx_client.query_api().query(flux_query)
        for table in tables:
            for record in table.records:
                return (record.get_value(), record.get_time())

        return None

    def _get_pressure_trend(
        self, current_pressure: float, prior_pressure: Optional[float]
    ) -> str:
        """Classify pressure movement over roughly three hours."""
        if prior_pressure is None:
            return "→"

        delta = current_pressure - prior_pressure
        if delta > 0.06:
            return "↑"
        if delta < -0.06:
            return "↓"
        return "→"

    def _get_pressure_trend_label(self, trend_symbol: str) -> str:
        """Convert pressure trend arrows into compact labels."""
        if trend_symbol == "↑":
            return "Rising"
        if trend_symbol == "↓":
            return "Falling"
        return "Steady"

    def _query_temperature_history(
        self,
        entity_id: str,
        measurement: str,
        hours_back: int = 3,
        aggregation_minutes: int = 15,
    ) -> List[float]:
        """Query recent temperature history for sparkline rendering."""
        bucket = self.get_bucket()
        query_tz = self.get_influx_query_timezone()

        flux_query = f"""
import "timezone"

option location = timezone.location(name: "{query_tz}")

from(bucket: "{bucket}")
    |> range(start: -{hours_back}h)
    |> filter(fn: (r) => r["entity_id"] == "{entity_id}")
    |> filter(fn: (r) => r["_measurement"] == "{measurement}")
    |> filter(fn: (r) => r["_field"] == "value")
    |> aggregateWindow(every: {aggregation_minutes}m, fn: mean, createEmpty: false)
        """

        values = []
        tables = self.influx_client.query_api().query(flux_query)
        for table in tables:
            for record in table.records:
                value = record.get_value()
                if value is not None and -50 < value < 150:
                    values.append((record.get_time(), round_value(value, 1)))

        values.sort(key=lambda row: row[0])
        return [row[1] for row in values]

    def _build_sparkline_points(
        self, values: List[float], width: int = 312, height: int = 108, padding: int = 8
    ) -> str:
        """Convert a numeric series into compact SVG polyline points."""
        if len(values) < 2:
            return ""

        min_value = min(values)
        max_value = max(values)
        value_span = max(max_value - min_value, 0.1)
        x_span = max(width - (padding * 2), 1)
        y_span = max(height - (padding * 2), 1)

        points = []
        for index, value in enumerate(values):
            x = padding + (x_span * index / (len(values) - 1))
            normalized = (value - min_value) / value_span
            y = height - padding - (normalized * y_span)
            points.append(f"{x:.1f},{y:.1f}")

        return " ".join(points)

    def collect_data(self) -> Dict[str, Any]:
        """
        Query InfluxDB for weather data and format for TRMNL

        Returns:
            Dictionary with merge_variables for TRMNL
        """
        entities = self.plugin_config.get("entities", {})
        result = {}
        latest_pairs = []

        # Temperature data (°F)
        temp_entities = {
            "tempf": ("outdoor_temp", "°F"),
            "tempinf": ("indoor_temp", "°F"),
            "dewPoint": ("dew_point", "°F"),
            "feelsLike": ("feels_like", "°F"),
        }

        for key, (config_key, measurement) in temp_entities.items():
            entity_id = entities.get(config_key)
            if entity_id:
                latest_pairs.append((entity_id, measurement))

        # Humidity data (%)
        humidity_entities = {
            "humidity": ("humidity", "%"),
            "humidityin": ("indoor_humidity", "%"),
        }

        for _, (config_key, measurement) in humidity_entities.items():
            entity_id = entities.get(config_key)
            if entity_id:
                latest_pairs.append((entity_id, measurement))

        # Wind data
        wind_speed_entity = entities.get("wind_speed")
        wind_gust_entity = entities.get("wind_gust")
        wind_dir_entity = entities.get("wind_direction")
        if wind_speed_entity:
            latest_pairs.append((wind_speed_entity, "mph"))
        if wind_gust_entity:
            latest_pairs.append((wind_gust_entity, "mph"))
        if wind_dir_entity:
            latest_pairs.append((wind_dir_entity, "°"))

        # Pressure/rain/UV/solar data
        pressure_entity = entities.get("pressure")
        rain_entity = entities.get("daily_rain")
        uv_entity = entities.get("uv_index")
        solar_rad_entity = entities.get("solar_radiation")
        if pressure_entity:
            latest_pairs.append((pressure_entity, "inHg"))
        if rain_entity:
            latest_pairs.append((rain_entity, "in"))
        if uv_entity:
            latest_pairs.append((uv_entity, "Index"))
        if solar_rad_entity:
            latest_pairs.append((solar_rad_entity, "W/m²"))

        latest_values = self._query_latest_values(latest_pairs)

        for key, (config_key, measurement) in temp_entities.items():
            entity_id = entities.get(config_key)
            if entity_id:
                data = latest_values.get((entity_id, measurement))
                if data:
                    result[key] = round_value(data[0], 1)

        for key, (config_key, measurement) in humidity_entities.items():
            entity_id = entities.get(config_key)
            if entity_id:
                data = latest_values.get((entity_id, measurement))
                if data:
                    result[key] = round_value(data[0], 0)

        wind_speed = None
        wind_dir = None

        if wind_speed_entity:
            data = latest_values.get((wind_speed_entity, "mph"))
            if data:
                wind_speed = round_value(data[0], 1)
                result["windspeedmph"] = wind_speed

        if wind_gust_entity:
            data = latest_values.get((wind_gust_entity, "mph"))
            if data:
                result["windgustmph"] = round_value(data[0], 1)

        if wind_dir_entity:
            data = latest_values.get((wind_dir_entity, "°"))
            if data:
                wind_dir = round_value(data[0], 0)
                result["winddir"] = wind_dir

        # Create formatted wind description
        if wind_speed is not None and wind_dir is not None:
            result["winddir_pretty"] = format_wind_description(wind_speed, wind_dir)
            result["wind_compact"] = format_compact_wind(wind_speed, wind_dir)
        elif wind_speed is not None:
            result["wind_compact"] = f"{round_value(wind_speed, 0):.0f} mph"

        if "windgustmph" in result:
            result["wind_gust_pretty"] = f"Gust {round_value(result['windgustmph'], 0):.0f} mph"
        else:
            result["wind_gust_pretty"] = "Gust --"

        # Pressure (inHg)
        if pressure_entity:
            data = latest_values.get((pressure_entity, "inHg"))
            if data:
                current_pressure = round_value(data[0], 2)
                result["baromrelin"] = current_pressure
                prior_pressure = self._query_latest_value_before(
                    pressure_entity, "inHg", 3
                )
                result["pressure_trend"] = self._get_pressure_trend(
                    current_pressure,
                    round_value(prior_pressure[0], 2) if prior_pressure else None,
                )
                result["pressure_trend_label"] = self._get_pressure_trend_label(
                    result["pressure_trend"]
                )

        # Rain (in)
        if rain_entity:
            data = latest_values.get((rain_entity, "in"))
            if data:
                result["dailyrainin"] = round_value(data[0], 3)

        # UV index
        if uv_entity:
            data = latest_values.get((uv_entity, "Index"))
            if data:
                result["uv"] = round_value(data[0], 0)

        # Solar radiation (W/m²)
        if solar_rad_entity:
            data = latest_values.get((solar_rad_entity, "W/m²"))
            if data:
                result["solarradiation"] = round_value(data[0], 1)

        # Query last rain time
        precip_entity = entities.get("precipitation_intensity")
        last_rain_time = self._query_last_rain(precip_entity) if precip_entity else None
        if last_rain_time:
            result["last_rain_date_pretty"] = format_relative_time(last_rain_time)
        else:
            result["last_rain_date_pretty"] = "No recent rain"

        outdoor_temp_entity = entities.get("outdoor_temp")
        if outdoor_temp_entity:
            sparkline_values = self._query_temperature_history(outdoor_temp_entity, "°F")
            result["temp_sparkline_points"] = self._build_sparkline_points(
                sparkline_values
            )
        else:
            result["temp_sparkline_points"] = ""

        result.setdefault("tempf", 0)
        result.setdefault("tempinf", 0)
        result.setdefault("humidityin", 0)
        result.setdefault("feelsLike", 0)
        result.setdefault("humidity", 0)
        result.setdefault("dewPoint", 0)
        result.setdefault("uv", 0)
        result.setdefault("solarradiation", 0)
        result.setdefault("dailyrainin", 0)
        result.setdefault("wind_compact", "--")
        result.setdefault("baromrelin", "--")
        result.setdefault("pressure_trend_label", "Steady")

        # Add formatted current timestamp
        result["date_pretty"] = format_timestamp_for_display(
            datetime.now(timezone.utc), self.get_timezone(), "%a %d %b, %I:%M %p"
        )

        logger.info(f"Collected weather data with {len(result)} fields")
        return result

    def get_webhook_id(self) -> str:
        """Get the weather webhook ID"""
        webhook_key = self.plugin_config.get("webhook_id_key", "WEATHER_WEBHOOK_ID")
        return self.secrets["webhooks"][webhook_key]
