"""Shared InfluxDB client using official influxdb-client library"""

import logging
from typing import Dict, Any
from influxdb_client import InfluxDBClient
from influxdb_client.client.query_api import QueryApi

logger = logging.getLogger(__name__)


def create_client(config: Dict[str, Any], secrets: Dict[str, Any]) -> InfluxDBClient:
    """
    Create an InfluxDB client using the official library

    Args:
        config: Application configuration dict
        secrets: Secrets configuration dict

    Returns:
        InfluxDBClient instance
    """
    influx_config = config["influxdb"]

    client = InfluxDBClient(
        url=influx_config["url"],
        token=secrets["influxdb"]["token"],
        org=influx_config["org"],
        verify_ssl=influx_config.get("verify_ssl", False),
    )

    logger.info(f"Created InfluxDB client for {influx_config['url']}")
    return client


def get_query_api(client: InfluxDBClient) -> QueryApi:
    """
    Get the Query API from an InfluxDB client

    Args:
        client: InfluxDB client instance

    Returns:
        QueryApi instance for executing Flux queries
    """
    return client.query_api()
