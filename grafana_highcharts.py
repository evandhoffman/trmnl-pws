#!/usr/bin/env python3
"""
Direct InfluxDB to Highcharts Converter
Queries InfluxDB directly and converts data to Highcharts format
"""

import os
import json
import argparse
import logging
import requests
import urllib3
from datetime import datetime, timedelta
import sys

# Disable SSL warnings
urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s [%(levelname)s] %(message)s',
    datefmt='%Y-%m-%dT%H:%M:%S%z'
)
logger = logging.getLogger("influx-to-highcharts")

def query_influxdb(influx_url, token, org, query, hours_back):
    """
    Query InfluxDB directly using the Flux query language
    """
    # Calculate time range based on hours_back
    stop_time = "now()"
    start_time = f"-{hours_back}h"  # Use relative time syntax for Flux
    
    # Replace any time range variables in the query
    if "v.timeRangeStart" in query:
        query = query.replace("v.timeRangeStart", start_time)
    else:
        # If we don't have variables but have a range() function, try to make a direct replacement
        import re
        range_pattern = r'range\s*\(\s*start\s*:[^,\)]*'
        if re.search(range_pattern, query):
            query = re.sub(range_pattern, f'range(start: {start_time}', query)
    
    if "v.timeRangeStop" in query:
        query = query.replace("v.timeRangeStop", stop_time)
    
    # Handle window period - use a reasonable default based on the time range
    if "v.windowPeriod" in query:
        # For larger time ranges, use larger window periods
        if hours_back > 168:  # > 1 week
            window = "1h"
        elif hours_back > 48:  # > 2 days
            window = "15m"
        else:
            window = "5m"
        
        query = query.replace("v.windowPeriod", window)
    
    logger.info(f"Time range: {start_time} to {stop_time}")
    logger.debug(f"Using query: {query}")
    
    # Debug the final query
    logger.debug(f"Final Flux query: {query}")
    
    # Prepare request headers
    headers = {
        "Authorization": f"Token {token}",
        "Content-Type": "application/json",
        "Accept": "application/csv"
    }
    
    # Build query URL
    query_url = f"{influx_url}/api/v2/query"
    
    # Add org parameter to URL if provided (this is more reliable than in the payload)
    if org:
        query_url = f"{query_url}?org={org}"
    
    # Prepare request payload
    payload = {
        "query": query,
        "type": "flux"
    }
    
    # Make the query request
    logger.info(f"Querying InfluxDB: {influx_url}")
    logger.debug(f"Query URL with params: {query_url}")
    
    response = requests.post(
        query_url,
        json=payload,
        headers=headers,
        verify=False
    )
    
    # Enhanced error logging
    if response.status_code != 200:
        logger.error(f"Failed to query data: {response.status_code} - {response.text}")
        logger.debug(f"Response headers: {response.headers}")
        
        # Special case for token auth issues
        if response.status_code in (401, 403):
            logger.error("Authentication failed. Your InfluxDB token may be invalid or may not have sufficient permissions.")
        
        raise Exception(f"Failed to query data: {response.status_code}")
    
    # Parse CSV response
    lines = response.text.strip().split('\n')
    
    # Extract header
    header = lines[0].split(',')
    
    # Find column indices for time and value
    time_idx = None
    value_idx = None
    for i, col in enumerate(header):
        if col.strip() == '_time':
            time_idx = i
        elif col.strip() == '_value':
            value_idx = i
    
    if time_idx is None or value_idx is None:
        logger.error("Could not find _time or _value columns in response")
        logger.debug(f"Headers: {header}")
        raise Exception("Could not find required columns in InfluxDB response")
    
    # Parse data rows
    data_points = []
    for line in lines[1:]:
        if line.startswith('#') or not line.strip():
            continue
        
        fields = line.split(',')
        if len(fields) > max(time_idx, value_idx):
            try:
                time_str = fields[time_idx].strip()
                value_str = fields[value_idx].strip()
                
                # Parse ISO timestamp to milliseconds
                dt = datetime.fromisoformat(time_str.replace('Z', '+00:00'))
                timestamp_ms = int(dt.timestamp() * 1000)
                
                # Parse value to float
                value = float(value_str)
                
                # Add to data points
                data_points.append([timestamp_ms, value])
            except (ValueError, TypeError) as e:
                logger.warning(f"Could not parse row: {line}, error: {e}")
    
    # Sort by timestamp
    data_points.sort(key=lambda x: x[0])
    
    return data_points

def convert_to_highcharts(data_points, title="Temperature Data"):
    """
    Convert raw data points to Highcharts format
    """
    highcharts_data = {
        "title": {"text": title},
        "series": [{
            "name": "Temperature (°F)",
            "data": data_points
        }],
        "chart": {"type": "line", "zoomType": "x"},
        "xAxis": {
            "type": "datetime",
            "title": {"text": "Time"}
        },
        "yAxis": {
            "title": {"text": "Temperature (°F)"}
        },
        "tooltip": {
            "shared": True,
            "crosshairs": True,
            "xDateFormat": "%Y-%m-%d %H:%M:%S",
            "valueSuffix": " °F"
        }
    }
    
    return highcharts_data

def generate_html(highcharts_data, output_file):
    """
    Generate HTML file with Highcharts visualization
    """
    html_template = f"""<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>{highcharts_data.get('title', {}).get('text', 'InfluxDB Data Export')}</title>
    <script src="https://code.highcharts.com/highcharts.js"></script>
    <script src="https://code.highcharts.com/modules/exporting.js"></script>
    <script src="https://code.highcharts.com/modules/export-data.js"></script>
    <style>
        #container {{
            min-width: 310px;
            max-width: 1200px;
            height: 500px;
            margin: 0 auto;
        }}
    </style>
</head>
<body>
    <div id="container"></div>
    <script>
        Highcharts.chart('container', {json.dumps(highcharts_data, indent=4)});
    </script>
</body>
</html>
    """
    
    with open(output_file, 'w') as f:
        f.write(html_template)
    
    logger.info(f"HTML file with Highcharts visualization generated: {output_file}")

def main():
    parser = argparse.ArgumentParser(description='Query InfluxDB directly and convert to Highcharts format')
    parser.add_argument('--url', required=True, help='InfluxDB base URL (e.g., https://influxdb.example.com:8086)')
    parser.add_argument('--token', required=True, help='InfluxDB API token')
    parser.add_argument('--org', help='InfluxDB organization name or ID')
    parser.add_argument('--query', help='Flux query to execute')
    parser.add_argument('--query-file', help='File containing Flux query')
    parser.add_argument('--hours-back', type=int, default=24, help='Hours to look back from now')
    parser.add_argument('--title', default='Temperature Data', help='Chart title')
    parser.add_argument('--output', default='influxdb_data.json', help='Output JSON file for data')
    parser.add_argument('--html-output', default='influxdb_highcharts.html', help='Output HTML file with Highcharts visualization')
    parser.add_argument('--verify-ssl', action='store_true', help='Verify SSL certificates (default: disabled)')
    parser.add_argument('--log-level', choices=['DEBUG', 'INFO', 'WARNING', 'ERROR', 'CRITICAL'], 
                      default='INFO', help='Set logging level')
    
    args = parser.parse_args()
    
    # Set logging level from command line argument
    logging.getLogger().setLevel(getattr(logging, args.log_level))
    
    # Remove trailing slash from URL if present
    influx_url = args.url.rstrip('/')
    
    # Get query from either command line or file
    query = args.query
    if not query and args.query_file:
        try:
            with open(args.query_file, 'r') as f:
                query = f.read()
        except Exception as e:
            logger.error(f"Could not read query file: {e}")
            return 1
    
    if not query:
        # Default query for weather station temperature data
        query = """from(bucket: "home_assistant/autogen")
  |> range(start: v.timeRangeStart, stop: v.timeRangeStop)
  |> filter(fn: (r) => r["_measurement"] == "°F")
  |> filter(fn: (r) => r["_field"] == "value")
  |> filter(fn: (r) => r["domain"] == "sensor")
  |> filter(fn: (r) => r["entity_id"] == "evan_s_pws_temp" )
  |> filter(fn: (r) => r._value < 130)
  |> aggregateWindow(every: v.windowPeriod, fn: mean, createEmpty: false)
  |> yield(name: "mean")"""
    
    logger.info(f"Starting InfluxDB to Highcharts converter")
    logger.info(f"Target InfluxDB instance: {influx_url}")
    if args.org:
        logger.info(f"Organization: {args.org}")
    logger.info(f"Time range: {args.hours_back} hours back from now")
    
    try:
        # Query InfluxDB directly
        logger.info(f"Querying InfluxDB")
        data_points = query_influxdb(
            influx_url,
            args.token,
            args.org,
            query,
            args.hours_back
        )
        
        # Convert to Highcharts format
        logger.info(f"Converting data to Highcharts format")
        highcharts_data = convert_to_highcharts(data_points, args.title)
        
        # Log summary
        logger.info(f"Found {len(data_points)} data points")
        
        # Save data to JSON file
        with open(args.output, 'w') as f:
            json.dump(highcharts_data, f, indent=2)
        
        logger.info(f"Highcharts-compatible data saved to {args.output}")
        
        # Generate HTML file with Highcharts visualization
        if args.html_output:
            logger.info(f"Generating HTML visualization")
            generate_html(highcharts_data, args.html_output)
        
    except Exception as e:
        logger.error(f"Error: {e}", exc_info=True)
        return 1
    
    logger.info("Script completed successfully")
    return 0

if __name__ == "__main__":
    sys.exit(main())
