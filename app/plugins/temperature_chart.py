"""Temperature chart plugin - queries InfluxDB for temperature data"""

import json
import logging
from datetime import datetime, timedelta, timezone
from typing import Dict, Any, List
from app.plugins import BasePlugin
from app.utils.formatting import timestamp_to_milliseconds
from app.utils.conversions import round_value
import pytz

logger = logging.getLogger(__name__)


class TemperatureChartPlugin(BasePlugin):
    """Plugin for collecting and formatting temperature chart data"""

    def __init__(self, config: Dict[str, Any], secrets: Dict[str, Any], influx_client):
        super().__init__(config, secrets, influx_client)
        self.plugin_config = config["plugins"]["temperature_chart"]
        self.plugin_name = "TemperatureChart"

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

        # Get outdoor temperature entity
        outdoor_temp_entity = entities.get("outdoor_temp", "evan_s_pws_temperature")

        flux_query = f"""
import "timezone"

option location = timezone.location(name: "{query_tz}")

from(bucket: "{bucket}")
    |> range(start: {start_time}, stop: {end_time})
    |> filter(fn: (r) => r["_measurement"] == "°F")
    |> filter(fn: (r) => r["_field"] == "value")
    |> filter(fn: (r) => r["domain"] == "sensor")
    |> filter(fn: (r) => r["entity_id"] == "{outdoor_temp_entity}")
    |> aggregateWindow(every: {aggregation_minutes}m, fn: mean, createEmpty: false)
        """

        logger.debug(f"Executing Flux query: {flux_query}")

        # Execute query
        query_api = self.influx_client.query_api()
        tables = query_api.query(flux_query)

        # Process results into Highcharts format
        temp_data = []
        for table in tables:
            for record in table.records:
                timestamp = record.get_time()
                value = record.get_value()

                if value is not None and -50 < value < 150:  # Sanity check
                    timestamp_ms = timestamp_to_milliseconds(timestamp)
                    temp_data.append([timestamp_ms, round_value(value, 1)])

        # Sort by timestamp
        temp_data.sort(key=lambda x: x[0])

        logger.info(f"Collected {len(temp_data)} temperature readings")

        # Format current timestamp
        local_tz = pytz.timezone(self.get_timezone())
        local_now = datetime.now(timezone.utc).astimezone(local_tz)
        formatted_timestamp = local_now.strftime("%A, %B %-d, %-I:%M %p")

        # Format data for webhook (JavaScript-compatible string for Highcharts)
        js_data_str = json.dumps(temp_data)

        return {
            "current_timestamp": formatted_timestamp,
            f"js_{outdoor_temp_entity}": js_data_str,
        }

    def get_webhook_id(self) -> str:
        """Get the temperature chart webhook ID"""
        webhook_key = self.plugin_config.get(
            "webhook_id_key", "TEMPERATURE_CHART_WEBHOOK_ID"
        )
        return self.secrets["webhooks"][webhook_key]
