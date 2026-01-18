"""Solar summary plugin - queries InfluxDB for daily solar energy data"""

import json
import logging
from datetime import datetime, timezone, timedelta, time as dt_time
from typing import Dict, Any, List
from app.plugins import BasePlugin
from app.utils.conversions import round_value
import pytz

logger = logging.getLogger(__name__)


class SolarSummaryPlugin(BasePlugin):
    """Plugin for collecting and formatting solar energy summary data"""

    def __init__(self, config: Dict[str, Any], secrets: Dict[str, Any], influx_client):
        super().__init__(config, secrets, influx_client)
        self.plugin_config = config["plugins"]["solar_summary"]
        self.plugin_name = "SolarSummary"

    def collect_data(self) -> Dict[str, Any]:
        """
        Query InfluxDB for daily solar energy data and format for charts

        Returns:
            Dictionary with merge_variables for TRMNL
        """
        days_back = self.plugin_config.get("days_back", 7)
        entities = self.plugin_config.get("entities", {})
        bucket = self.get_bucket()
        tz = pytz.timezone(self.get_timezone())

        # Build Flux query using the proven working pattern
        entity_list = list(entities.values())
        # Build entity filter matching the working query
        entity_conditions = " or ".join([f'r.entity_id == "{e}"' for e in entity_list])

        logger.debug(f"Solar summary entities: {entity_list}")

        flux_query = f"""
import "date"
import "timezone"
import "strings"

option location = timezone.location(name: "{self.get_timezone()}")

from(bucket: "{bucket}")
    |> range(
        start: date.truncate(t: -{days_back}d, unit: 1d, location: location),
        stop: now()
    )
    |> filter(fn: (r) =>
        r._measurement == "kW" and
        ({entity_conditions}) and
        r.domain == "sensor" and
        r._field == "value"
    )
    |> aggregateWindow(every: 1d, fn: integral, createEmpty: false)
    |> map(fn: (r) => ({{
        r with
            _value: r._value / 3600.0,
            entity_id: strings.replaceAll(v: r.entity_id, t: "bellmore_", u: "home_")
    }}))
        """

        logger.debug(f"Executing Flux query for solar summary")
        logger.debug(f"Query: {flux_query}")

        # Execute query
        query_api = self.influx_client.query_api()
        tables = query_api.query(flux_query)

        # Process results
        daily_records = []
        for table in tables:
            for record in table.records:
                entity_id = record.values.get("entity_id")
                timestamp = record.get_time()
                value = record.get_value()

                logger.debug(
                    f"Record: entity_id={entity_id}, timestamp={timestamp}, value={value}"
                )

                if entity_id and value is not None:
                    daily_records.append(
                        {"_time": timestamp, "_value": value, "entity_id": entity_id}
                    )

        logger.info(f"Collected {len(daily_records)} daily energy records")
        if daily_records:
            logger.debug(f"First record: {daily_records[0]}")
        else:
            logger.warning(f"No records found. Entities: {entity_list}")

        # Process data for display
        now_ny = datetime.now(timezone.utc).astimezone(tz)
        midnight = now_ny.replace(hour=0, minute=0, second=0, microsecond=0)
        today_date = midnight.date()

        # Determine unique mapping dates from records
        dates = set()
        for rec in daily_records:
            ts = rec["_time"]
            # midnight timestamp → previous day
            if ts.time() == dt_time(0, 0):
                map_date = (ts - timedelta(days=1)).date()
            else:
                map_date = ts.date()
            dates.add(map_date)

        # Always include today
        dates.add(today_date)

        # Sort ascending
        sorted_dates = sorted(dates)

        # Prepare slots
        slots = []
        for d in sorted_dates:
            if d == today_date:
                label = d.strftime("%a %-m/%-d") + f" ({now_ny.strftime('%-I:%M %p')})"
            else:
                label = d.strftime("%a %-m/%-d")
            slots.append(
                {"date": label, "grid": 0.0, "load": 0.0, "solar": 0.0, "date_obj": d}
            )

        # Assign values to slots
        for rec in daily_records:
            ts = rec["_time"]
            if ts.time() == dt_time(0, 0):
                map_date = (ts - timedelta(days=1)).date()
            else:
                map_date = ts.date()

            for slot in slots:
                if slot["date_obj"] == map_date:
                    val = round_value(rec["_value"], 2)
                    ent = rec.get("entity_id", "")

                    # Map to the correct slot based on entity type
                    if "grid" in ent:
                        slot["grid"] = val
                    elif "load" in ent or "usage" in ent:
                        slot["load"] = val
                    elif "solar" in ent or "generated" in ent:
                        slot["solar"] = val
                    break

        # Build merge_variables with JSON stringified arrays
        merge = {
            "str_categories": json.dumps([s["date"] for s in slots]),
            "str_grid": json.dumps([s["grid"] for s in slots]),
            "str_load": json.dumps([s["load"] for s in slots]),
            "str_solar": json.dumps([s["solar"] for s in slots]),
        }

        logger.info(f"Formatted solar summary for {len(slots)} days")
        return merge

    def get_webhook_id(self) -> str:
        """Get the solar summary webhook ID"""
        webhook_key = self.plugin_config.get(
            "webhook_id_key", "SOLAR_SUMMARY_WEBHOOK_ID"
        )
        return self.secrets["webhooks"][webhook_key]
