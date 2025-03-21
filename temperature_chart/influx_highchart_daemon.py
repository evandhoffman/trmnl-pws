#!/usr/bin/env python3
"""
InfluxDB to Webhook Temperature Data Sender
Queries temperature data directly from InfluxDB and submits to a webhook
Runs continuously with environment variable configuration
"""

import os
import sys
import time
import json
import logging
import urllib3
import requests
import statistics
import pytz
from datetime import datetime, timedelta, timezone
from typing import List, Dict, Any, Optional, Tuple

# Disable SSL warnings
urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s [%(levelname)s] %(message)s',
    datefmt='%Y-%m-%dT%H:%M:%S%z'
)
logger = logging.getLogger("influxdb-to-webhook")

class InfluxDBClient:
    """Simple client for querying InfluxDB directly"""
    
    def __init__(self, url: str, token: str, org: str, verify_ssl: bool = False):
        """
        Initialize InfluxDB client
        
        Args:
            url: InfluxDB server URL
            token: Authentication token
            org: Organization name
            verify_ssl: Whether to verify SSL certificates
        """
        self.url = url.rstrip('/')
        self.token = token
        self.org = org
        self.verify_ssl = verify_ssl
        
    def query(self, flux_query: str) -> List[Dict[str, Any]]:
        """
        Execute a Flux query and parse results
        
        Args:
            flux_query: Flux query string
            
        Returns:
            List of records as dictionaries
        """
        query_url = f"{self.url}/api/v2/query?org={self.org}"
        
        headers = {
            "Authorization": f"Token {self.token}",
            "Content-Type": "application/vnd.flux",
            "Accept": "application/csv"
        }
        
        logger.info(f"Querying InfluxDB: {self.url}")
        logger.debug(f"Flux Query: {flux_query}")
        
        response = requests.post(
            query_url,
            data=flux_query,
            headers=headers,
            verify=self.verify_ssl
        )
        
        if response.status_code != 200:
            logger.error(f"Query failed: {response.status_code} - {response.text}")
            raise Exception(f"InfluxDB query failed: {response.status_code}")
            
        # Parse CSV response
        return self._parse_csv(response.text)
        
    def _parse_csv(self, csv_data: str) -> List[Dict[str, Any]]:
        """
        Parse InfluxDB CSV response to list of records
        
        Args:
            csv_data: CSV data from InfluxDB
            
        Returns:
            List of records as dictionaries
        """
        lines = csv_data.strip().split('\n')
        
        # Find data section (after annotations)
        data_start = 0
        for i, line in enumerate(lines):
            if not line.startswith('#'):
                data_start = i
                break
                
        if data_start >= len(lines):
            logger.warning("No data rows found in response")
            return []
            
        # Get headers
        headers = [h.strip() for h in lines[data_start].split(',')]
        
        # Parse data rows
        records = []
        for i in range(data_start + 1, len(lines)):
            line = lines[i].strip()
            if not line:
                continue
                
            values = line.split(',')
            if len(values) != len(headers):
                logger.warning(f"Row {i} has {len(values)} values but expected {len(headers)}")
                continue
                
            record = {}
            for j, header in enumerate(headers):
                value = values[j]
                # Convert numeric values
                if value.replace('.', '', 1).isdigit():
                    record[header] = float(value)
                elif value.lower() in ('true', 'false'):
                    record[header] = value.lower() == 'true'
                elif value == '':
                    record[header] = None
                else:
                    record[header] = value
                    
            records.append(record)
            
        logger.info(f"Parsed {len(records)} records from InfluxDB response")
        return records

def get_temperature_data(
    client: InfluxDBClient, 
    start_time: str, 
    end_time: str, 
    bucket: str, 
    measurement: str
) -> List[Dict[str, Any]]:
    """
    Get temperature data from InfluxDB
    
    Args:
        client: InfluxDB client
        start_time: Start time (Flux format)
        end_time: End time (Flux format)
        bucket: InfluxDB bucket
        measurement: Measurement name
        
    Returns:
        List of temperature records
    """
    # Build Flux query
    flux_query = f"""
    from(bucket: "{bucket}")
        |> range(start: {start_time}, stop: {end_time})
        |> filter(fn: (r) => r["_measurement"] == "{measurement}")
        |> filter(fn: (r) => r["_field"] == "value")
        |> filter(fn: (r) => r["domain"] == "sensor")
        |> filter(fn: (r) => r["entity_id"] ==  "evan_s_pws_temp")
        |> aggregateWindow(
                every: 10m,
                fn: (tables=<-, column) => tables
                    |> mean(),
            )
    """
    
    return client.query(flux_query)

def process_temperature_data(records: List[Dict[str, Any]]) -> Dict[str, Any]:
    """
    Process temperature data and calculate statistics
    
    Args:
        records: Temperature records from InfluxDB
        
    Returns:
        Dictionary with temperature data and statistics formatted for Highcharts
    """
    # Extract relevant fields and organize by sensor
    sensors_data = {}
    all_temps = []
    
    for record in records:
        if "_value" not in record or "entity_id" not in record or "_time" not in record:
            continue
            
        entity_id = record["entity_id"]
        temp = record["_value"]
        timestamp = record["_time"]
        
        # Convert timestamp if it's a string
        if isinstance(timestamp, str):
            try:
                # Simple approach: strip microseconds completely
                # Remove the decimal part and the Z, then add timezone
                if '.' in timestamp and 'Z' in timestamp:
                    timestamp = timestamp.split('.')[0] + '+00:00'
                elif 'Z' in timestamp:
                    timestamp = timestamp.replace('Z', '+00:00')
                
                timestamp = datetime.fromisoformat(timestamp)
            except ValueError:
                logger.debug(f"Could not parse timestamp: {timestamp}")
                continue
                
        # Skip invalid temperatures
        if temp is None or temp < -50 or temp > 150:
            continue
            
        all_temps.append(temp)
        
        # Convert timestamp to milliseconds for Highcharts
        timestamp_ms = int(timestamp.timestamp() * 1000)
        
        # Add to sensor data
        if entity_id not in sensors_data:
            sensors_data[entity_id] = []
            
        # Add data point in Highcharts format [timestamp_ms, value]
        sensors_data[entity_id].append([timestamp_ms, temp])
        
    # Sort data by timestamp for each sensor
    for entity_id in sensors_data:
        sensors_data[entity_id].sort(key=lambda x: x[0])
        
    # Create display names mapping
    display_names = {
        "evan_s_pws_inside_temp": "Inside Temperature",
        "evan_s_pws_temp": "Outside Temperature",
        "evan_s_pws_dew_point": "Dew Point",
        "evan_s_pws_temp_1": "Sensor 1 Temperature",
        "evan_s_pws_temp_2": "Sensor 2 Temperature",
        "evan_s_pws_temp_3": "Sensor 3 Temperature",
        "evan_s_pws_temp_5": "Sensor 5 Temperature"
    }
    
    # Calculate statistics
    stats = {}
    if all_temps:
        stats = {
            "min": min(all_temps),
            "max": max(all_temps),
            "avg": statistics.mean(all_temps) if all_temps else None,
            "count": len(all_temps)
        }
        
    # Get most recent reading for each sensor
    most_recent = {}
    for entity_id, data_points in sensors_data.items():
        if data_points:
            # Get the last data point (they're sorted)
            last_point = data_points[-1]
            most_recent[entity_id] = {
                "temperature": last_point[1],
                "timestamp": datetime.fromtimestamp(last_point[0] / 1000),
                "entity_id": entity_id,
                "display_name": display_names.get(entity_id, entity_id.replace("evan_s_pws_", "").replace("_", " ").title())
            }

    utc_now = datetime.now(timezone.utc)
    mytz = pytz.timezone(os.environ['TIME_ZONE'])
    local_now = utc_now.astimezone(mytz)
    formatted_timestamp = local_now.strftime("%A, %B %-d, %-I:%M %p")

    # Format data for webhook
    # Create a result with top-level keys for each sensor's data
    result = {
        "current_timestamp": formatted_timestamp,
        "statistics": stats,
        "most_recent": most_recent,
        "highcharts_options": {
            "title": {
                "text": "Temperature Data"
            },
            "xAxis": {
                "type": "datetime",
                "title": {
                    "text": "Time"
                }
            },
            "yAxis": {
                "title": {
                    "text": "Temperature (°F)"
                }
            },
            "tooltip": {
                "valueSuffix": "°F",
                "xDateFormat": "%Y-%m-%d %H:%M:%S",
                "shared": True,
                "crosshairs": True
            }
        },
        "display_names": display_names  # Mapping of entity_id to display names
    }
    
    # Add each sensor's data as a top-level key
    for entity_id, data in sensors_data.items():
        # Regular array data
        key_name = f"temp_data_{entity_id}"
        result[key_name] = data
        
        # JavaScript-compatible string representation
        js_key_name = f"js_{entity_id}"
        # Convert the array to a JavaScript-compatible string
        js_data_str = str(data).replace("'", "")  # Remove single quotes to make it JS-compatible
        result[js_key_name] = js_data_str
    
    return result

def send_to_webhook(url: str, data: Dict[str, Any]) -> bool:
    """
    Send data to webhook
    
    Args:
        url: Webhook URL
        data: Data to send
        
    Returns:
        True if successful, False otherwise
    """
    try:
        # Wrap data in merge_variables as requested
        payload = {
            "merge_variables": data
        }
        
        # Convert datetime objects to ISO format for JSON serialization
        json_data = json.dumps(payload, default=lambda x: x.isoformat() if isinstance(x, datetime) else str(x))
        
        logger.info(f"Sending data to webhook: {url}")
        logger.debug(f"Payload size: {len(json_data)} bytes")
        
        response = requests.post(
            url,
            data=json_data,
            headers={"Content-Type": "application/json"}
        )
        
        if response.status_code >= 200 and response.status_code < 300:
            logger.info(f"Successfully sent data to webhook: {response.status_code}")
            return True
        else:
            logger.error(f"Failed to send data to webhook: {response.status_code} - {response.text}")
            return False
            
    except Exception as e:
        logger.error(f"Error sending data to webhook: {str(e)}")
        return False

def calculate_time_range(hours_back: int) -> Tuple[str, str]:
    """
    Calculate time range for Flux query based on hours back from now
    
    Args:
        hours_back: Number of hours to look back from now
        
    Returns:
        Tuple of (start, end) formatted for Flux
    """
    # End time is always now
    flux_end = "now()"
    
    # Start time is a relative duration
    flux_start = f"-{hours_back}h"
            
    return flux_start, flux_end

def main():
    # Read configuration from environment variables
    try:
        webhook_url = os.environ['WEBHOOK_URL']
        influx_url = os.environ['INFLUXDB_URL']
        influx_org = os.environ['INFLUXDB_ORG']
        influx_token = os.environ['INFLUXDB_TOKEN']
        bucket = os.environ['INFLUXDB_BUCKET']
        hours_back = int(os.environ.get('HOURS_BACK', 24))
        
        # Optional: log level from env var
        log_level = os.environ.get('LOG_LEVEL', 'INFO').upper()
        logger.setLevel(getattr(logging, log_level))
        
    except KeyError as e:
        logger.error(f"Missing required environment variable: {e}")
        return 1
    except ValueError:
        logger.error("Invalid HOURS_BACK value. Must be an integer.")
        return 1

    # Set up InfluxDB client
    client = InfluxDBClient(
        url=influx_url,
        token=influx_token,
        org=influx_org,
        verify_ssl=False  # Make configurable via env var if needed
    )

    # Main processing loop
    while True:
        try:
            # Calculate time range based on hours back
            start_time, end_time = calculate_time_range(hours_back)
            logger.info(f"Querying temperature data for the past {hours_back} hours ({start_time} to {end_time})")
            
            # Get temperature data
            records = get_temperature_data(
                client=client,
                start_time=start_time,
                end_time=end_time,
                bucket=bucket,
                measurement='°F'  # Hardcoded or could be from env var
            )
            
            if not records:
                logger.warning("No temperature data found")
            else:
                # Process data
                processed_data = process_temperature_data(records)
                
                # Send to webhook
                success = send_to_webhook(webhook_url, processed_data)
                
                if not success:
                    logger.error("Failed to send data to webhook")
            
            # Wait for next iteration
            sleep_seconds = 300
            logger.info(f"Waiting {sleep_seconds} seconds before next data collection...")
            time.sleep(sleep_seconds)
            
        except Exception as e:
            logger.error(f"Error in main processing loop: {str(e)}", exc_info=True)
            
            # Wait before retrying to avoid rapid error loops
            logger.info("Waiting 60 seconds before retrying...")
            time.sleep(60)

if __name__ == "__main__":
    sys.exit(main())
