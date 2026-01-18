"""
State management for tracking last webhook update times.
"""

import json
import logging
import os
from datetime import datetime
from pathlib import Path

logger = logging.getLogger(__name__)

STATE_FILE = Path(os.environ.get("STATE_LOCK_PATH", "/tmp/last_trmnl_update.lock"))

# Maximum backoff time (1 hour)
MAX_BACKOFF_SECONDS = 3600


def load_state():
    """
    Load state from the lock file.

    Returns:
        dict: Dictionary mapping webhook_id -> {timestamp, failure_count}
    """
    if not STATE_FILE.exists():
        logger.info(f"No state file found at {STATE_FILE}, starting fresh")
        return {}

    try:
        with open(STATE_FILE, "r") as f:
            state = json.load(f)
        logger.info(f"Loaded state for {len(state)} webhook(s) from {STATE_FILE}")
        return state
    except (json.JSONDecodeError, IOError) as e:
        logger.warning(f"Failed to load state file: {e}, starting fresh")
        return {}


def save_state(state):
    """
    Save state to the lock file.

    Args:
        state: Dictionary mapping webhook_id -> timestamp (ISO format)
    """
    try:
        STATE_FILE.parent.mkdir(parents=True, exist_ok=True)
        with open(STATE_FILE, "w") as f:
            json.dump(state, f, indent=2)
        logger.debug(f"Saved state for {len(state)} webhook(s) to {STATE_FILE}")
    except IOError as e:
        logger.error(f"Failed to save state file: {e}")


def ensure_webhook_initialized(state, webhook_id):
    """
    Ensure a webhook has a timestamp in the state.
    If missing, initialize it with the current time.

    Args:
        state: Current state dict (will be modified in place)
        webhook_id: The webhook ID to check/initialize
        
    Returns:
        bool: True if state was modified, False otherwise
    """
    if webhook_id not in state:
        now = datetime.utcnow()
        timestamp_str = now.isoformat()
        state[webhook_id] = {"timestamp": timestamp_str, "failure_count": 0}
        logger.info(
            f"Initialized webhook {webhook_id[:8]}... with timestamp {timestamp_str} UTC"
        )
        return True
    else:
        logger.debug(f"Webhook {webhook_id[:8]}... already has timestamp in state")
        return False


def get_last_update_time(state, webhook_id):
    """
    Get the last update time for a specific webhook.

    Args:
        state: Current state dict
        webhook_id: The webhook ID to check

    Returns:
        datetime or None: Last update time, or None if never updated
    """
    webhook_state = state.get(webhook_id)

    if webhook_state:
        # Handle both old format (string) and new format (dict)
        if isinstance(webhook_state, str):
            timestamp_str = webhook_state
        else:
            timestamp_str = webhook_state.get("timestamp")

        if timestamp_str:
            try:
                dt = datetime.fromisoformat(timestamp_str)
                logger.info(
                    f"Webhook {webhook_id[:8]}... last updated at {timestamp_str} UTC"
                )
                return dt
            except ValueError as e:
                logger.warning(f"Invalid timestamp for {webhook_id}: {e}")
                return None
    else:
        logger.info(f"Webhook {webhook_id[:8]}... has no previous update timestamp")

    return None


def record_update(state, webhook_id, success=True):
    """
    Record that a webhook was just updated.

    Args:
        state: Current state dict (will be modified in place)
        webhook_id: The webhook ID that was updated
        success: Whether the update was successful (resets failure count)
    """
    now = datetime.utcnow()
    timestamp_str = now.isoformat()

    # Get current failure count
    webhook_state = state.get(webhook_id, {})
    if isinstance(webhook_state, str):
        # Migrate old format
        failure_count = 0
    else:
        failure_count = webhook_state.get("failure_count", 0)

    if success:
        # Reset failure count on success
        failure_count = 0
    else:
        # Increment failure count on failure
        failure_count += 1

    state[webhook_id] = {"timestamp": timestamp_str, "failure_count": failure_count}

    if success:
        logger.info(
            f"Recorded successful update for webhook {webhook_id[:8]}... at {timestamp_str} UTC"
        )
    else:
        backoff = calculate_backoff(failure_count, 300)  # Using default poll_interval
        logger.info(
            f"Recorded failed update for webhook {webhook_id[:8]}... at {timestamp_str} UTC "
            f"(failure #{failure_count}, next retry in {backoff}s)"
        )


def seconds_since_last_update(state, webhook_id):
    """
    Calculate how many seconds have elapsed since the last update.

    Args:
        state: Current state dict
        webhook_id: The webhook ID to check

    Returns:
        float or None: Seconds elapsed, or None if never updated
    """
    last_update = get_last_update_time(state, webhook_id)

    if last_update is None:
        return None

    elapsed = (datetime.utcnow() - last_update).total_seconds()
    return elapsed


def calculate_backoff(failure_count, base_interval):
    """
    Calculate exponential backoff time.

    Args:
        failure_count: Number of consecutive failures
        base_interval: Base polling interval in seconds

    Returns:
        int: Backoff time in seconds (capped at MAX_BACKOFF_SECONDS)
    """
    if failure_count == 0:
        return base_interval

    # Exponential backoff: base_interval * 2^failure_count
    backoff = base_interval * (2**failure_count)

    # Cap at 1 hour
    return min(backoff, MAX_BACKOFF_SECONDS)


def get_failure_count(state, webhook_id):
    """
    Get the failure count for a webhook.

    Args:
        state: Current state dict
        webhook_id: The webhook ID to check

    Returns:
        int: Number of consecutive failures
    """
    webhook_state = state.get(webhook_id, {})

    if isinstance(webhook_state, str):
        # Old format, no failure count
        return 0

    return webhook_state.get("failure_count", 0)


def should_update(state, webhook_id, poll_interval):
    """
    Check if enough time has elapsed to allow an update.
    Uses exponential backoff for failed webhooks.

    Args:
        state: Current state dict
        webhook_id: The webhook ID to check
        poll_interval: Base polling interval in seconds

    Returns:
        bool: True if update should proceed, False if too soon
    """
    elapsed = seconds_since_last_update(state, webhook_id)

    if elapsed is None:
        # Never updated before
        return True

    # Get failure count and calculate backoff
    failure_count = get_failure_count(state, webhook_id)
    required_interval = calculate_backoff(failure_count, poll_interval)

    return elapsed >= required_interval
