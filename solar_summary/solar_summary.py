#!/usr/bin/env python3
"""
InfluxDB to Webhook Solar Power Data Sender
Queries solar power data directly from InfluxDB and submits to a webhook
Runs continuously with configuration from a config file
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
import argparse
import yaml
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

def execute_query(
    client: InfluxDBClient,
    query_template: str,
) -> List[Dict[str, Any]]:
    """
    Execute a Flux query with time range variables
    
    Args:
        client: InfluxDB client
        query_template: Query template with {start_time}, {end_time}, and {bucket} placeholders
        start_time: Start time (Flux format)
        end_time: End time (Flux format)
        bucket: InfluxDB bucket
        
    Returns:
        List of query result records
    """
    # Replace placeholders in the query template
    flux_query = query_template
    
    # Log the query before execution
    logger.debug(f"Executing flux query: {flux_query}")
    
    # Get the raw results
    results = client.query(flux_query)
    
    # Clean and validate results to ensure we only have the expected fields
    cleaned_records = []
    for record in results:
        # Ensure we have the minimum required fields
        if "_value" in record and "_time" in record:
            # Create a clean record with only the necessary fields
            clean_record = {
                "_time": record["_time"],
                "_value": record["_value"]
            }
            
            # Add entity_id if it exists
            if "entity_id" in record:
                clean_record["entity_id"] = record["entity_id"]
                
            cleaned_records.append(clean_record)
    
    logger.info(f"Cleaned {len(cleaned_records)} records from InfluxDB response")
    return cleaned_records

def process_solar_data(daily_records: List[Dict[str, Any]], config) -> Dict[str, Any]:
    """
    Process solar power data without statistical calculations
    
    Args:
        current_records: Current solar power records from InfluxDB
        daily_records: Daily energy records from InfluxDB
        config: Configuration object
        
    Returns:
        Dictionary with solar data formatted for Highcharts
    """
    # Get display preferences from config
    chart_config = config.get('charts', {})
    display_names = {}
    
    # Parse display names from config
    display_names_config = chart_config.get('display_names', {})
    for entity_id, display_name in display_names_config.items():
        display_names[entity_id] = display_name
    
    # Extract relevant fields and organize by sensor
    sensors_data = {}
    
    # Process daily energy data (kwh)
    daily_energy_data = {}
    for record in daily_records:
        if "_value" not in record or "_time" not in record:
            continue
            
        energy = round(float(record["_value"]),2)
        timestamp = record["_time"]
        entity_id = record["entity_id"]

        logger.info(f"Processing daily record: {entity_id} at {timestamp} with energy {energy}")
        
        # Convert timestamp if it's a string
        if isinstance(timestamp, str):
            try:
                if '.' in timestamp and 'Z' in timestamp:
                    timestamp = timestamp.split('.')[0] + '+00:00'
                elif 'Z' in timestamp:
                    timestamp = timestamp.replace('Z', '+00:00')
                
                timestamp = datetime.fromisoformat(timestamp)
            except ValueError:
                logger.debug(f"Could not parse timestamp: {timestamp}")
                continue
                
        # Convert timestamp to milliseconds for Highcharts
        timestamp_ms = int(timestamp.timestamp() * 1000)
        
        # Add only timestamp and value for Highcharts
        #daily_energy_data.append([timestamp_ms, energy])
        daily_energy_data[timestamp_ms][entity_id] = energy
    
    # Sort daily energy data by timestamp
    #daily_energy_data.sort(key=lambda x: x[0])
    
    # Get most recent reading for each sensor
    most_recent = {}
    for entity_id, data_points in sensors_data.items():
        if data_points:
            # Get the last data point (they're sorted)
            last_point = data_points[-1]
            most_recent[entity_id] = {
                "power": last_point[1],
                "timestamp": datetime.fromtimestamp(last_point[0] / 1000),
                "entity_id": entity_id,
                "display_name": display_names.get(entity_id, entity_id.replace("_", " ").title())
            }

    # Get timezone from config
    timezone_str = config.get('general', {}).get('timezone', 'UTC')
    utc_now = datetime.now(timezone.utc)
    mytz = pytz.timezone(timezone_str)
    local_now = utc_now.astimezone(mytz)
    formatted_timestamp = local_now.strftime("%A, %B %-d, %-I:%M %p")

    # Format data for webhook
    result = {
        "current_timestamp": formatted_timestamp,
    }
    
    # Add each sensor's power data as stringified arrays only
    for entity_id, data in sensors_data.items():
        # JavaScript-compatible string representation with str_ prefix
        str_key_name = f"str_{entity_id}"
        # Convert the array to a JavaScript-compatible string
        str_data_str = str(data).replace("'", "")  # Remove single quotes to make it JS-compatible
        result[str_key_name] = str_data_str
    
    # Add daily energy data as stringified array only
    result["str_daily_energy"] = str(daily_energy_data).replace("'", "")
    
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

    payload = {
        "merge_variables": data
    }
    
    # Convert datetime objects to ISO format for JSON serialization
    json_data = json.dumps(payload, default=lambda x: x.isoformat() if isinstance(x, datetime) else str(x))
    
    logger.info(f"Sending data to webhook: {url[:40]}...")
    payload_size = len(json_data)
    logger.debug(f"JSON: {json.dumps(payload, indent=2)}")
   
    max_payload_size_bytes = 4000

    if payload_size > max_payload_size_bytes:
        logger.error(f"*** Payload size: {payload_size} bytes, max is {max_payload_size_bytes} bytes ***")
        return False
    else:
        logger.info(f"Payload size: {payload_size} bytes")

    try:

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

def calculate_daily_range(days_back: int) -> Tuple[str, str]:
    """
    Calculate time range for daily energy data
    
    Args:
        days_back: Number of days to look back from now
        
    Returns:
        Tuple of (start, end) formatted for Flux
    """
    # End time is always now
    flux_end = "now()"
    
    # Start time is a relative duration
    flux_start = f"-{days_back}d"
            
    return flux_start, flux_end

def load_config(config_file: str) -> Dict[str, Any]:
    """
    Load configuration from a YAML file
    
    Args:
        config_file: Path to the configuration file
        
    Returns:
        Dictionary with loaded configuration
    """
    if not os.path.exists(config_file):
        logger.error(f"Configuration file not found: {config_file}")
        raise FileNotFoundError(f"Configuration file not found: {config_file}")
        
    logger.info(f"Loading configuration from: {config_file}")
    
    with open(config_file, 'r') as file:
        config = yaml.safe_load(file)
    
    # Validate required configuration
    required_sections = ['general', 'influxdb', 'webhook', 'queries']
    for section in required_sections:
        if section not in config:
            logger.error(f"Missing required configuration section: {section}")
            raise ValueError(f"Missing required configuration section: {section}")
    
    return config

def main():
    # Parse command line arguments
    parser = argparse.ArgumentParser(description='InfluxDB to Webhook Solar Data Sender')
    parser.add_argument('-c', '--config', dest='config_file', default='config.yaml',
                        help='Path to configuration file (default: config.yaml)')
    args = parser.parse_args()
    
    try:
        # Load configuration
        config = load_config(args.config_file)
        
        # Set log level
        general_config = config.get('general', {})
        log_level = general_config.get('log_level', 'INFO').upper()
        logger.setLevel(getattr(logging, log_level))
        
        # Get InfluxDB configuration
        influx_config = config.get('influxdb', {})
        influx_url = influx_config.get('url')
        influx_org = influx_config.get('org')
        influx_token = influx_config.get('token')
        bucket = influx_config.get('bucket')
        verify_ssl = influx_config.get('verify_ssl', False)
        
        # Get webhook configuration
        webhook_config = config.get('webhook', {})
        webhook_url = webhook_config.get('url')
        
        # Get query time ranges
        hours_back = general_config.get('hours_back', 24)
        days_back = general_config.get('days_back', 30)
        
        # Get query templates
        queries_config = config.get('queries', {})
        energy_query = queries_config.get('energy_query')
        
        # Get polling interval
        poll_interval = general_config.get('poll_interval', 300)
        
    except (FileNotFoundError, ValueError, yaml.YAMLError) as e:
        logger.error(f"Configuration error: {str(e)}")
        return 1

    # Set up InfluxDB client
    client = InfluxDBClient(
        url=influx_url,
        token=influx_token,
        org=influx_org,
        verify_ssl=verify_ssl
    )

    # Main processing loop
    while True:
        try:
            # Calculate time range for current power data
            power_start_time, power_end_time = calculate_time_range(hours_back)
            logger.info(f"Querying InfluxDB")
            
            
            # Execute energy query
            daily_records = execute_query(
                client=client,
                query_template=energy_query,
            )
            logger.info(" Successfully retrieved daily energy data")
            
            if not daily_records:
                logger.warning("No solar data found")
            else:
                # Process data
                processed_data = process_solar_data(daily_records, config)
                
                # Send to webhook
                success = send_to_webhook(webhook_url, processed_data)
                
                if not success:
                    logger.error("Failed to send data to webhook")
            
            # Wait for next iteration
            logger.info(f"Waiting {poll_interval} seconds before next data collection...")
            time.sleep(poll_interval)
            
        except Exception as e:
            logger.error(f"Error in main processing loop: {str(e)}", exc_info=True)
            
            # Wait before retrying to avoid rapid error loops
            retry_interval = general_config.get('retry_interval', 60)
            logger.info(f"Waiting {retry_interval} seconds before retrying...")
            time.sleep(retry_interval)

if __name__ == "__main__":
    sys.exit(main())
