"""Solar power plugin - queries InfluxDB for solar power data"""

import json
import logging
from datetime import datetime, timezone
from typing import Dict, Any, List
from app.plugins import BasePlugin
from app.utils.formatting import timestamp_to_milliseconds
from app.utils.conversions import round_value
import pytz

logger = logging.getLogger(__name__)


class SolarPowerPlugin(BasePlugin):
    """Plugin for collecting and formatting solar power chart data"""

    def __init__(self, config: Dict[str, Any], secrets: Dict[str, Any], influx_client):
        super().__init__(config, secrets, influx_client)
        self.plugin_config = config["plugins"]["solar_power"]
        self.plugin_name = "SolarPower"

    def collect_data(self) -> Dict[str, Any]:
        """
        Query InfluxDB for solar power data and format for Highcharts

        Returns:
            Dictionary with merge_variables for TRMNL
        """
        hours_back = self.plugin_config.get("hours_back", 7)
        aggregation_minutes = self.plugin_config.get("aggregation_interval_minutes", 30)
        entities = self.plugin_config.get("entities", {})
        bucket = self.get_bucket()
        query_tz = self.get_influx_query_timezone()

        # Build entity filter for Flux query
        entity_list = list(entities.values())
        entity_filter = " or ".join([f'r["entity_id"] == "{e}"' for e in entity_list])

        # Build Flux query for power data
        start_time = f"-{hours_back}h"
        end_time = "now()"

        flux_query = f"""
import "timezone"

option location = timezone.location(name: "{query_tz}")

from(bucket: "{bucket}")
    |> range(start: {start_time}, stop: {end_time})
    |> filter(fn: (r) => {entity_filter})
    |> filter(fn: (r) => r["_field"] == "value")
    |> filter(fn: (r) => r["_measurement"] == "kW")
    |> filter(fn: (r) => r["domain"] == "sensor")
    |> aggregateWindow(every: {aggregation_minutes}m, fn: mean, createEmpty: false)
        """

        logger.debug(f"Executing Flux query: {flux_query}")

        # Execute query
        query_api = self.influx_client.query_api()
        tables = query_api.query(flux_query)

        # Process results into Highcharts format
        sensors_data = {}
        for table in tables:
            for record in table.records:
                entity_id = record.values.get("entity_id")
                timestamp = record.get_time()
                value = record.get_value()

                if entity_id and value is not None:
                    if entity_id not in sensors_data:
                        sensors_data[entity_id] = []

                    timestamp_ms = timestamp_to_milliseconds(timestamp)
                    sensors_data[entity_id].append(
                        [timestamp_ms, round_value(value, 1)]
                    )

        # Sort data by timestamp for each sensor
        for entity_id in sensors_data:
            sensors_data[entity_id].sort(key=lambda x: x[0])
            logger.info(
                f"Sensor {entity_id} contains {len(sensors_data[entity_id])} readings"
            )

        # Format current timestamp
        local_tz = pytz.timezone(self.get_timezone())
        local_now = datetime.now(timezone.utc).astimezone(local_tz)
        formatted_timestamp = local_now.strftime("%A, %B %-d, %-I:%M %p")

        # Build result with stringified arrays for each entity
        result = {"current_timestamp": formatted_timestamp}

        for entity_id, data in sensors_data.items():
            # JavaScript-compatible string representation
            str_key_name = f"str_{entity_id}"
            result[str_key_name] = json.dumps(data)

        logger.info(f"Collected solar power data for {len(sensors_data)} sensors")
        return result

    def get_webhook_id(self) -> str:
        """Get the solar power webhook ID"""
        webhook_key = self.plugin_config.get("webhook_id_key", "SOLAR_POWER_WEBHOOK_ID")
        return self.secrets["webhooks"][webhook_key]
