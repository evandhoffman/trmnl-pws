"""Weather plugin - queries InfluxDB for weather data"""

import logging
from datetime import datetime, timezone
from typing import Dict, Any, Optional
from app.plugins import BasePlugin
from app.utils.formatting import format_timestamp_for_display, format_relative_time
from app.utils.conversions import format_wind_description, round_value

logger = logging.getLogger(__name__)


class WeatherPlugin(BasePlugin):
    """Plugin for collecting and formatting weather data from InfluxDB"""
    
    def __init__(self, config: Dict[str, Any], secrets: Dict[str, Any], influx_client):
        super().__init__(config, secrets, influx_client)
        self.plugin_config = config['plugins']['weather']
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
        
        flux_query = f"""
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
    
    def _query_last_rain(self) -> Optional[datetime]:
        """
        Query the last time it rained (precipitation_intensity > 0)
        
        Returns:
            Timestamp of last rain or None
        """
        bucket = self.get_bucket()
        
        flux_query = f"""
from(bucket: "{bucket}")
    |> range(start: -30d)
    |> filter(fn: (r) => r["entity_id"] == "evan_s_pws_precipitation_intensity")
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
    
    def collect_data(self) -> Dict[str, Any]:
        """
        Query InfluxDB for weather data and format for TRMNL
        
        Returns:
            Dictionary with merge_variables for TRMNL
        """
        entities = self.plugin_config.get('entities', {})
        
        # Query latest values for each entity
        result = {}
        
        # Temperature data (°F)
        temp_entities = {
            'tempf': ('outdoor_temp', '°F'),
            'tempinf': ('indoor_temp', '°F'),
            'dewPoint': ('dew_point', '°F'),
            'feelsLike': ('feels_like', '°F')
        }
        
        for key, (config_key, measurement) in temp_entities.items():
            entity_id = entities.get(config_key)
            if entity_id:
                data = self._query_latest_value(entity_id, measurement)
                if data:
                    result[key] = round_value(data[0], 1)
        
        # Humidity data (%)
        humidity_entities = {
            'humidity': ('humidity', '%'),
            'humidityin': ('indoor_humidity', '%')
        }
        
        for key, (config_key, measurement) in humidity_entities.items():
            entity_id = entities.get(config_key)
            if entity_id:
                data = self._query_latest_value(entity_id, measurement)
                if data:
                    result[key] = round_value(data[0], 0)
        
        # Wind data
        wind_speed_entity = entities.get('wind_speed')
        wind_gust_entity = entities.get('wind_gust')
        wind_dir_entity = entities.get('wind_direction')
        
        wind_speed = None
        wind_dir = None
        
        if wind_speed_entity:
            data = self._query_latest_value(wind_speed_entity, 'mph')
            if data:
                wind_speed = round_value(data[0], 1)
                result['windspeedmph'] = wind_speed
        
        if wind_gust_entity:
            data = self._query_latest_value(wind_gust_entity, 'mph')
            if data:
                result['windgustmph'] = round_value(data[0], 1)
        
        if wind_dir_entity:
            data = self._query_latest_value(wind_dir_entity, '°')
            if data:
                wind_dir = round_value(data[0], 0)
                result['winddir'] = wind_dir
        
        # Create formatted wind description
        if wind_speed is not None and wind_dir is not None:
            result['winddir_pretty'] = format_wind_description(wind_speed, wind_dir)
        
        # Pressure (inHg)
        pressure_entity = entities.get('pressure')
        if pressure_entity:
            data = self._query_latest_value(pressure_entity, 'inHg')
            if data:
                result['baromrelin'] = round_value(data[0], 2)
        
        # Rain (in)
        rain_entity = entities.get('daily_rain')
        if rain_entity:
            data = self._query_latest_value(rain_entity, 'in')
            if data:
                result['dailyrainin'] = round_value(data[0], 3)
        
        # UV index
        uv_entity = entities.get('uv_index')
        if uv_entity:
            data = self._query_latest_value(uv_entity, 'Index')
            if data:
                result['uv'] = round_value(data[0], 0)
        
        # Solar radiation (W/m²)
        solar_rad_entity = entities.get('solar_radiation')
        if solar_rad_entity:
            data = self._query_latest_value(solar_rad_entity, 'W/m²')
            if data:
                result['solarradiation'] = round_value(data[0], 1)
        
        # Query last rain time
        last_rain_time = self._query_last_rain()
        if last_rain_time:
            result['last_rain_date_pretty'] = format_relative_time(last_rain_time)
        
        # Add formatted current timestamp
        result['date_pretty'] = format_timestamp_for_display(
            datetime.now(timezone.utc),
            self.get_timezone(),
            "%a %d %b, %I:%M %p"
        )
        
        logger.info(f"Collected weather data with {len(result)} fields")
        return result
    
    def get_webhook_id(self) -> str:
        """Get the weather webhook ID"""
        webhook_key = self.plugin_config.get('webhook_id_key', 'WEATHER_WEBHOOK_ID')
        return self.secrets['webhooks'][webhook_key]
