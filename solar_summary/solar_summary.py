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
import pytz
import argparse
import yaml
from datetime import datetime, timezone, timedelta, time as dt_time
from typing import List, Dict, Any

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
        self.url = url.rstrip('/')
        self.token = token
        self.org = org
        self.verify_ssl = verify_ssl

    def query(self, flux_query: str) -> List[Dict[str, Any]]:
        query_url = f"{self.url}/api/v2/query?org={self.org}"
        headers = {
            "Authorization": f"Token {self.token}",
            "Content-Type": "application/vnd.flux",
            "Accept": "application/csv"
        }
        logger.info(f"Querying InfluxDB: {self.url}")
        response = requests.post(
            query_url,
            data=flux_query,
            headers=headers,
            verify=self.verify_ssl
        )
        if response.status_code != 200:
            logger.error(f"Query failed: {response.status_code} - {response.text}")
            raise Exception(f"InfluxDB query failed: {response.status_code}")
        return self._parse_csv(response.text)

    def _parse_csv(self, csv_data: str) -> List[Dict[str, Any]]:
        lines = csv_data.strip().split('\n')
        data_start = 0
        for i, line in enumerate(lines):
            if not line.startswith('#'):
                data_start = i
                break
        if data_start >= len(lines):
            return []
        headers = [h.strip() for h in lines[data_start].split(',')]
        records = []
        for line in lines[data_start + 1:]:
            if not line.strip():
                continue
            values = line.split(',')
            if len(values) != len(headers):
                continue
            rec = {}
            for j, header in enumerate(headers):
                val = values[j]
                if val.replace('.', '', 1).isdigit():
                    rec[header] = float(val)
                elif val.lower() in ('true', 'false'):
                    rec[header] = val.lower() == 'true'
                elif val == '':
                    rec[header] = None
                else:
                    rec[header] = val
            records.append(rec)
        logger.info(f"Parsed {len(records)} records from InfluxDB response")
        return records


def execute_query(
    client: InfluxDBClient,
    query_template: str,
) -> List[Dict[str, Any]]:
    """
    Execute a Flux query and clean results.
    """
    logger.debug(f"Executing flux query: {query_template}")
    results = client.query(query_template)
    cleaned = []
    for rec in results:
        if "_value" in rec and "_time" in rec:
            cr = {"_time": rec["_time"], "_value": rec["_value"]}
            if "entity_id" in rec:
                cr["entity_id"] = rec["entity_id"].strip()
            cleaned.append(cr)
    logger.info(f"Cleaned {len(cleaned)} records from InfluxDB response")
    return cleaned


def process_solar_data(
    daily_records: List[Dict[str, Any]],
    config: Dict[str, Any]
) -> Dict[str, Any]:
    """
    Build merge_variables dynamically for N days of data.
    - str_categories: ["Mon, 21 Apr", ..., "Sun, 27 Apr (10:04 AM)"]
    - str_grid, str_load, str_solar arrays of floats
    """
    tz = pytz.timezone(config.get('general', {}).get('timezone', 'America/New_York'))
    now_ny = datetime.now(timezone.utc).astimezone(tz)
    midnight = now_ny.replace(hour=0, minute=0, second=0, microsecond=0)
    today_date = midnight.date()

    # Determine unique mapping dates from records
    dates = set()
    for rec in daily_records:
        ts = rec['_time']
        if isinstance(ts, str) and ts.endswith('Z'):
            ts = ts.replace('Z', '+00:00')
        dt = datetime.fromisoformat(ts).astimezone(tz)
        # midnight timestamp → previous day
        if dt.time() == dt_time(0, 0):
            map_date = (dt - timedelta(days=1)).date()
        else:
            map_date = dt.date()
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
        slots.append({'date': label, 'grid': 0.0, 'load': 0.0, 'solar': 0.0, 'date_obj': d})

    # Assign values
    for rec in daily_records:
        ts = rec['_time']
        if isinstance(ts, str) and ts.endswith('Z'):
            ts = ts.replace('Z', '+00:00')
        dt = datetime.fromisoformat(ts).astimezone(tz)
        if dt.time() == dt_time(0, 0):
            map_date = (dt - timedelta(days=1)).date()
        else:
            map_date = dt.date()
        for slot in slots:
            if slot['date_obj'] == map_date:
                val = round(float(rec['_value']), 2)
                ent = rec.get('entity_id', '')
                if 'grid' in ent:
                    slot['grid'] = val
                elif 'load' in ent:
                    slot['load'] = val
                elif 'solar' in ent:
                    slot['solar'] = val
                break

    # Build merge_variables
    merge = {
        'str_categories': json.dumps([s['date'] for s in slots]),
        'str_grid':       json.dumps([s['grid'] for s in slots]),
        'str_load':       json.dumps([s['load'] for s in slots]),
        'str_solar':      json.dumps([s['solar'] for s in slots]),
    }
    logger.info(f"Payload to webhook: {merge}")
    return merge


def send_to_webhook(url: str, data: Dict[str, Any]) -> bool:
    payload = {"merge_variables": data}
    json_data = json.dumps(payload, default=lambda x: x.isoformat() if isinstance(x, datetime) else str(x))
    logger.info(f"Sending data to webhook: {url[:40]}...")
    logger.info(f"Payload: {json_data}")
    if len(json_data) > 4000:
        logger.error(f"Payload too large: {len(json_data)} bytes")
        return False
    try:
        resp = requests.post(
            url,
            data=json_data,
            headers={"Content-Type": "application/json"}
        )
        if 200 <= resp.status_code < 300:
            logger.info(f"Successfully sent data: {resp.status_code}")
            return True
        else:
            logger.error(f"Webhook error: {resp.status_code} - {resp.text}")
            return False
    except Exception as e:
        logger.error(f"Error sending webhook: {e}")
        return False


def load_config(config_file: str) -> Dict[str, Any]:
    if not os.path.exists(config_file):
        raise FileNotFoundError(f"Configuration file not found: {config_file}")
    with open(config_file, 'r') as f:
        cfg = yaml.safe_load(f)
    for section in ['general', 'influxdb', 'webhook', 'queries']:
        if section not in cfg:
            raise ValueError(f"Missing configuration section: {section}")
    return cfg


def main():
    parser = argparse.ArgumentParser(description='InfluxDB to Webhook Solar Data Sender')
    parser.add_argument('-c', '--config', dest='config_file', default='config.yaml',
                        help='Path to configuration file (default: config.yaml)')
    args = parser.parse_args()

    try:
        config = load_config(args.config_file)
        log_level = config.get('general', {}).get('log_level', 'INFO').upper()
        logger.setLevel(getattr(logging, log_level))

        influx_cfg = config['influxdb']
        client = InfluxDBClient(
            url=influx_cfg['url'],
            token=influx_cfg['token'],
            org=influx_cfg['org'],
            verify_ssl=influx_cfg.get('verify_ssl', False)
        )

        webhook_url = config['webhook']['url']
        energy_query = config['queries']['energy_query']
        poll_interval = config['general'].get('poll_interval', 300)
        retry_interval = config['general'].get('retry_interval', 60)
    except Exception as e:
        logger.error(f"Configuration error: {e}")
        sys.exit(1)

    while True:
        try:
            daily_records = execute_query(client, energy_query)
            if daily_records:
                data = process_solar_data(daily_records, config)
                if not send_to_webhook(webhook_url, data):
                    logger.error("Failed to send data to webhook")
            else:
                logger.warning("No solar data found")
            time.sleep(poll_interval)
        except Exception:
            logger.exception("Error in main loop; retrying after delay")
            time.sleep(retry_interval)

if __name__ == "__main__":
    sys.exit(main())

