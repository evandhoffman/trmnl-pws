"""Temperature chart plugin - queries InfluxDB for temperature data"""

import json
import logging
from datetime import datetime, timezone
from typing import Dict, Any, List
from app.plugins import BasePlugin
from app.utils.formatting import timestamp_to_milliseconds
from app.utils.formatting import format_timestamp_for_display
from app.utils.conversions import round_value
from app.utils.solar import get_solar_events_between
import pytz

logger = logging.getLogger(__name__)


class TemperatureChartPlugin(BasePlugin):
    """Plugin for collecting and formatting temperature chart data"""

    def __init__(self, config: Dict[str, Any], secrets: Dict[str, Any], influx_client):
        super().__init__(config, secrets, influx_client)
        self.plugin_config = config["plugins"]["temperature_chart"]
        self.plugin_name = "TemperatureChart"

    def _build_series_summary(self, points: List[List[float]]) -> Dict[str, str]:
        if not points:
            return {
                "temp_low_value": "--",
                "temp_low_time": "--",
                "temp_high_value": "--",
                "temp_high_time": "--",
                "temp_last_value": "--",
                "temp_last_time": "--",
            }

        local_tz_name = self.get_timezone()
        low_point = min(points, key=lambda point: point[1])
        high_point = max(points, key=lambda point: point[1])
        last_point = points[-1]

        def format_point(point: List[float]) -> tuple[str, str]:
            timestamp = datetime.fromtimestamp(point[0] / 1000, tz=timezone.utc)
            return (
                f"{point[1]:.1f}°F",
                format_timestamp_for_display(timestamp, local_tz_name, "%-I:%M %p"),
            )

        low_value, low_time = format_point(low_point)
        high_value, high_time = format_point(high_point)
        last_value, last_time = format_point(last_point)

        return {
            "temp_low_value": low_value,
            "temp_low_time": low_time,
            "temp_high_value": high_value,
            "temp_high_time": high_time,
            "temp_last_value": last_value,
            "temp_last_time": last_time,
        }

    def _build_solar_event_payload(
        self, outdoor_points: List[List[float]], indoor_points: List[List[float]]
    ) -> str:
        coordinates = self.get_coordinates()
        if not coordinates:
            logger.info("Temperature chart solar annotations disabled: no coordinates configured")
            return "[]"

        combined_points = outdoor_points + indoor_points
        if not combined_points:
            logger.info("Temperature chart solar annotations skipped: no chart points available")
            return "[]"

        latitude, longitude = coordinates
        start = datetime.fromtimestamp(min(point[0] for point in combined_points) / 1000, tz=timezone.utc)
        end = datetime.fromtimestamp(max(point[0] for point in combined_points) / 1000, tz=timezone.utc)
        events = get_solar_events_between(start, end, latitude, longitude, self.get_timezone())
        logger.info(
            "Temperature chart solar annotations: %s",
            ", ".join(f"{event['kind']}={event['time_pretty']}" for event in events) or "none in range",
        )
        return json.dumps(events)

    def collect_data(self) -> Dict[str, Any]:
        """
        Query InfluxDB for temperature data and format for Highcharts

        Returns:
            Dictionary with merge_variables for TRMNL
        """
        hours_back = self.plugin_config.get("hours_back", 12)
        aggregation_minutes = self.plugin_config.get("aggregation_interval_minutes", 30)
        entities = self.plugin_config.get("entities", {})

        # Build Flux query
        start_time = f"-{hours_back}h"
        end_time = "now()"
        bucket = self.get_bucket()
        query_tz = self.get_influx_query_timezone()

        # Get temperature entities
        outdoor_temp_entity = entities.get("outdoor_temp", "evan_s_pws_temperature")
        indoor_temp_entity = entities.get("indoor_temp")
        entity_filters = [f'r["entity_id"] == "{outdoor_temp_entity}"']
        if indoor_temp_entity:
            entity_filters.append(f'r["entity_id"] == "{indoor_temp_entity}"')
        entity_filter = " or ".join(entity_filters)

        flux_query = f"""
import "timezone"

option location = timezone.location(name: "{query_tz}")

from(bucket: "{bucket}")
    |> range(start: {start_time}, stop: {end_time})
    |> filter(fn: (r) => r["_measurement"] == "°F")
    |> filter(fn: (r) => r["_field"] == "value")
    |> filter(fn: (r) => r["domain"] == "sensor")
    |> filter(fn: (r) => {entity_filter})
    |> aggregateWindow(every: {aggregation_minutes}m, fn: mean, createEmpty: false)
        """

        logger.debug(f"Executing Flux query: {flux_query}")

        # Execute query
        query_api = self.influx_client.query_api()
        tables = query_api.query(flux_query)

        # Process results into Highcharts format
        outdoor_temp_data = []
        indoor_temp_data = []
        for table in tables:
            for record in table.records:
                entity_id = record.values.get("entity_id")
                timestamp = record.get_time()
                value = record.get_value()

                if value is not None and -50 < value < 150:  # Sanity check
                    timestamp_ms = timestamp_to_milliseconds(timestamp)
                    if entity_id == indoor_temp_entity:
                        indoor_temp_data.append([timestamp_ms, round_value(value, 1)])
                    else:
                        outdoor_temp_data.append([timestamp_ms, round_value(value, 1)])

        # Sort by timestamp
        outdoor_temp_data.sort(key=lambda x: x[0])
        indoor_temp_data.sort(key=lambda x: x[0])

        logger.info(
            f"Collected {len(outdoor_temp_data)} outdoor and {len(indoor_temp_data)} indoor temperature readings"
        )

        # Format current timestamp
        local_tz = pytz.timezone(self.get_timezone())
        local_now = datetime.now(timezone.utc).astimezone(local_tz)
        formatted_timestamp = local_now.strftime("%A, %B %-d, %-I:%M %p")

        # Format data for webhook (JavaScript-compatible string for Highcharts)
        js_data_str = json.dumps(outdoor_temp_data)
        js_indoor_data_str = json.dumps(indoor_temp_data)
        summary = self._build_series_summary(outdoor_temp_data)

        return {
            "current_timestamp": formatted_timestamp,
            "display_timezone": self.get_timezone(),
            "js_temperature_data": js_data_str,
            "js_indoor_temperature_data": js_indoor_data_str,
            "js_solar_events": self._build_solar_event_payload(
                outdoor_temp_data, indoor_temp_data
            ),
            **summary,
        }

    def get_webhook_id(self) -> str:
        """Get the temperature chart webhook ID"""
        webhook_key = self.plugin_config.get(
            "webhook_id_key", "TEMPERATURE_CHART_WEBHOOK_ID"
        )
        return self.secrets["webhooks"][webhook_key]
