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


def load_state():
    """
    Load state from the lock file.
    
    Returns:
        dict: Dictionary mapping webhook_id -> timestamp (ISO format)
    """
    if not STATE_FILE.exists():
        logger.info(f"No state file found at {STATE_FILE}, starting fresh")
        return {}
    
    try:
        with open(STATE_FILE, 'r') as f:
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
        with open(STATE_FILE, 'w') as f:
            json.dump(state, f, indent=2)
        logger.debug(f"Saved state for {len(state)} webhook(s) to {STATE_FILE}")
    except IOError as e:
        logger.error(f"Failed to save state file: {e}")


def ensure_webhook_initialized(webhook_id):
    """
    Ensure a webhook has a timestamp in the state file.
    If missing, initialize it with the current time.
    
    Args:
        webhook_id: The webhook ID to check/initialize
    """
    state = load_state()
    
    if webhook_id not in state:
        now = datetime.utcnow()
        timestamp_str = now.isoformat()
        state[webhook_id] = timestamp_str
        save_state(state)
        logger.info(f"Initialized webhook {webhook_id[:8]}... with timestamp {timestamp_str} UTC")
    else:
        logger.debug(f"Webhook {webhook_id[:8]}... already has timestamp in state")


def get_last_update_time(webhook_id):
    """
    Get the last update time for a specific webhook.
    
    Args:
        webhook_id: The webhook ID to check
        
    Returns:
        datetime or None: Last update time, or None if never updated
    """
    state = load_state()
    timestamp_str = state.get(webhook_id)
    
    if timestamp_str:
        try:
            dt = datetime.fromisoformat(timestamp_str)
            logger.info(f"Webhook {webhook_id[:8]}... last updated at {timestamp_str} UTC")
            return dt
        except ValueError as e:
            logger.warning(f"Invalid timestamp for {webhook_id}: {e}")
            return None
    else:
        logger.info(f"Webhook {webhook_id[:8]}... has no previous update timestamp")
    
    return None


def record_update(webhook_id):
    """
    Record that a webhook was just updated.
    
    Args:
        webhook_id: The webhook ID that was updated
    """
    state = load_state()
    now = datetime.utcnow()
    timestamp_str = now.isoformat()
    state[webhook_id] = timestamp_str
    save_state(state)
    logger.info(f"Recorded update for webhook {webhook_id[:8]}... at {timestamp_str}")


def seconds_since_last_update(webhook_id):
    """
    Calculate how many seconds have elapsed since the last update.
    
    Args:
        webhook_id: The webhook ID to check
        
    Returns:
        float or None: Seconds elapsed, or None if never updated
    """
    last_update = get_last_update_time(webhook_id)
    
    if last_update is None:
        return None
    
    elapsed = (datetime.utcnow() - last_update).total_seconds()
    return elapsed


def should_update(webhook_id, poll_interval):
    """
    Check if enough time has elapsed to allow an update.
    
    Args:
        webhook_id: The webhook ID to check
        poll_interval: Minimum seconds between updates
        
    Returns:
        bool: True if update should proceed, False if too soon
    """
    elapsed = seconds_since_last_update(webhook_id)
    
    if elapsed is None:
        # Never updated before
        return True
    
    return elapsed >= poll_interval
