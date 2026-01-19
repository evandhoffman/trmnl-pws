"""Main entry point for TRMNL PWS application"""

import sys
import time
import signal
import logging
import pytz
from datetime import datetime, timedelta
from typing import List
from app.config import load_config, load_secrets
from app.influx_client import create_client
from app.webhook import post_to_webhook
from app.state import (
    load_state,
    save_state,
    should_update,
    record_update,
    seconds_since_last_update,
    ensure_webhook_initialized,
    get_failure_count,
    calculate_backoff,
)
from app.plugins import BasePlugin
from app.plugins.weather import WeatherPlugin
from app.plugins.solar_power import SolarPowerPlugin
from app.plugins.solar_summary import SolarSummaryPlugin
from app.plugins.temperature_chart import TemperatureChartPlugin

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)-8s %(message)s",
    datefmt="%Y-%m-%dT%H:%M:%S",
)
logger = logging.getLogger(__name__)

# Global flag for graceful shutdown
shutdown_requested = False


def signal_handler(signum, frame):
    """Handle shutdown signals"""
    global shutdown_requested
    logger.info(f"Received signal {signum}, initiating graceful shutdown...")
    shutdown_requested = True


def initialize_plugins(config: dict, secrets: dict, influx_client) -> List[BasePlugin]:
    """
    Initialize enabled plugins

    Args:
        config: Application configuration
        secrets: Secrets configuration
        influx_client: InfluxDB client instance

    Returns:
        List of initialized plugin instances
    """
    plugins = []
    plugin_configs = config.get("plugins", {})

    # Weather plugin
    if plugin_configs.get("weather", {}).get("enabled", False):
        try:
            plugins.append(WeatherPlugin(config, secrets, influx_client))
            logger.info("Initialized Weather plugin")
        except Exception as e:
            logger.error(f"Failed to initialize Weather plugin: {e}")

    # Solar power plugin
    if plugin_configs.get("solar_power", {}).get("enabled", False):
        try:
            plugins.append(SolarPowerPlugin(config, secrets, influx_client))
            logger.info("Initialized Solar Power plugin")
        except Exception as e:
            logger.error(f"Failed to initialize Solar Power plugin: {e}")

    # Solar summary plugin
    if plugin_configs.get("solar_summary", {}).get("enabled", False):
        try:
            plugins.append(SolarSummaryPlugin(config, secrets, influx_client))
            logger.info("Initialized Solar Summary plugin")
        except Exception as e:
            logger.error(f"Failed to initialize Solar Summary plugin: {e}")

    # Temperature chart plugin
    if plugin_configs.get("temperature_chart", {}).get("enabled", False):
        try:
            plugins.append(TemperatureChartPlugin(config, secrets, influx_client))
            logger.info("Initialized Temperature Chart plugin")
        except Exception as e:
            logger.error(f"Failed to initialize Temperature Chart plugin: {e}")

    return plugins


def main():
    """Main application entry point"""
    global shutdown_requested

    # Setup signal handlers
    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)

    logger.info("Starting TRMNL PWS application")

    try:
        # Load configuration
        config = load_config("config/config.yml")
        secrets = load_secrets("config/secrets.yml")

        # Set log level from config
        log_level = config.get("general", {}).get("log_level", "INFO").upper()
        logging.getLogger().setLevel(getattr(logging, log_level))
        logger.info(f"Log level set to {log_level}")

        # Create InfluxDB client
        influx_client = create_client(config, secrets)
        logger.info("InfluxDB client created successfully")

        # Initialize plugins
        plugins = initialize_plugins(config, secrets, influx_client)

        if not plugins:
            logger.error("No plugins enabled! Check your configuration.")
            return 1

        logger.info(f"Initialized {len(plugins)} plugin(s)")

        # Initialize webhook timestamps for all plugins on startup
        logger.info("Initializing webhook state...")
        state = load_state()
        state_modified = False
        for plugin in plugins:
            webhook_id = plugin.get_webhook_id()
            if ensure_webhook_initialized(state, webhook_id):
                state_modified = True

        if state_modified:
            save_state(state)

        # Get configuration
        poll_interval = config.get("general", {}).get("poll_interval", 300)
        trmnl_plus = config.get("general", {}).get("trmnl_plus_subscriber", False)

        logger.info(f"Poll interval: {poll_interval} seconds")
        logger.info(f"TRMNL+ subscriber: {trmnl_plus}")

        # Main processing loop
        iteration = 0
        while not shutdown_requested:
            iteration += 1
            logger.info(f"=== Starting iteration {iteration} ===")

            # Load state once per iteration
            state = load_state()
            iteration_state_modified = False
            min_wait_seconds = poll_interval  # Default to full poll_interval

            for plugin in plugins:
                if shutdown_requested:
                    break

                try:
                    # Get webhook ID first to check state
                    webhook_id = plugin.get_webhook_id()

                    # Check if enough time has elapsed since last update
                    if not should_update(state, webhook_id, poll_interval):
                        elapsed = seconds_since_last_update(state, webhook_id)
                        failure_count = get_failure_count(state, webhook_id)
                        required_interval = calculate_backoff(
                            failure_count, poll_interval
                        )
                        remaining = required_interval - elapsed

                        # Track minimum wait time for next iteration
                        min_wait_seconds = min(min_wait_seconds, remaining)

                        # Calculate expected update time
                        timezone = config.get("general", {}).get(
                            "timezone", "America/New_York"
                        )
                        tz = pytz.timezone(timezone)
                        next_update_time = datetime.now(tz) + timedelta(
                            seconds=remaining
                        )
                        next_update_str = next_update_time.strftime("%H:%M")

                        backoff_msg = (
                            f" (backoff x{2**failure_count})"
                            if failure_count > 0
                            else ""
                        )
                        logger.info(
                            f"⏸ {plugin.plugin_name} skipped - "
                            f"last update was {elapsed:.0f}s ago, "
                            f"next update in {remaining:.0f}s at {next_update_str}{backoff_msg}"
                        )
                        continue

                    logger.info(f"Processing plugin: {plugin.plugin_name}")

                    # Collect data from plugin
                    data = plugin.collect_data()

                    # Post to webhook
                    status = post_to_webhook(webhook_id, data, trmnl_plus)

                    # Always record the attempt timestamp
                    # For rate_limited errors, this enables exponential backoff
                    if status == "success":
                        record_update(state, webhook_id, success=True)
                        iteration_state_modified = True
                        logger.info(f"✓ {plugin.plugin_name} completed successfully")
                    elif status == "rate_limited":
                        record_update(state, webhook_id, success=False)
                        iteration_state_modified = True
                        logger.warning(
                            f"✗ {plugin.plugin_name} rate limited (will use backoff)"
                        )
                    else:
                        record_update(state, webhook_id, success=False)
                        iteration_state_modified = True
                        logger.warning(f"✗ {plugin.plugin_name} failed to post webhook")

                except Exception as e:
                    logger.error(
                        f"✗ {plugin.plugin_name} failed with error: {e}", exc_info=True
                    )

            # Save state once at end of iteration if modified
            if iteration_state_modified:
                save_state(state)

            if shutdown_requested:
                break

            # Wait for next iteration (use minimum wait time if backoff is active)
            logger.info(
                f"Waiting {min_wait_seconds:.0f} seconds before next iteration..."
            )

            # Sleep in smaller increments to allow faster shutdown
            sleep_remaining = min_wait_seconds
            while sleep_remaining > 0 and not shutdown_requested:
                sleep_time = min(sleep_remaining, 5)
                time.sleep(sleep_time)
                sleep_remaining -= sleep_time

        logger.info("Shutting down gracefully...")
        influx_client.close()
        logger.info("InfluxDB client closed")
        return 0

    except FileNotFoundError as e:
        logger.error(f"Configuration file not found: {e}")
        return 1
    except ValueError as e:
        logger.error(f"Configuration error: {e}")
        return 1
    except Exception as e:
        logger.error(f"Unexpected error: {e}", exc_info=True)
        return 1


if __name__ == "__main__":
    sys.exit(main())
