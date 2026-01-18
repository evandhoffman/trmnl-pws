"""Utility functions for date/time formatting"""

import pytz
from datetime import datetime, timezone
from typing import Optional


def format_timestamp_for_display(
    timestamp: datetime,
    tz_name: str = "America/New_York",
    format_str: str = "%a %d %b, %I:%M %p"
) -> str:
    """
    Format a timestamp for display in local timezone
    
    Args:
        timestamp: datetime object (assumed UTC if no timezone)
        tz_name: Timezone name (e.g., 'America/New_York')
        format_str: strftime format string
        
    Returns:
        Formatted timestamp string (e.g., "Sat 18 Jan, 02:15 PM")
    """
    # Ensure timestamp is timezone-aware
    if timestamp.tzinfo is None:
        timestamp = timestamp.replace(tzinfo=timezone.utc)
    
    # Convert to local timezone
    local_tz = pytz.timezone(tz_name)
    local_dt = timestamp.astimezone(local_tz)
    
    return local_dt.strftime(format_str)


def format_relative_time(timestamp: datetime, now: Optional[datetime] = None) -> str:
    """
    Format a timestamp as relative time (e.g., "5 minutes ago", "2 hours ago")
    
    Args:
        timestamp: datetime object to format
        now: Current time (defaults to datetime.now(timezone.utc))
        
    Returns:
        Relative time string
    """
    if now is None:
        now = datetime.now(timezone.utc)
    
    # Ensure both are timezone-aware
    if timestamp.tzinfo is None:
        timestamp = timestamp.replace(tzinfo=timezone.utc)
    if now.tzinfo is None:
        now = now.replace(tzinfo=timezone.utc)
    
    diff = now - timestamp
    minutes = diff.total_seconds() / 60
    
    if minutes < 1:
        return "just now"
    elif minutes < 60:
        return f"{int(minutes)} minute{'s' if int(minutes) != 1 else ''} ago"
    elif minutes < 1440:  # < 24 hours
        hours = int(minutes / 60)
        return f"{hours} hour{'s' if hours != 1 else ''} ago"
    elif minutes < 10080:  # < 7 days
        days = int(minutes / 1440)
        return f"{days} day{'s' if days != 1 else ''} ago"
    else:
        weeks = int(minutes / 10080)
        return f"{weeks} week{'s' if weeks != 1 else ''} ago"


def timestamp_to_milliseconds(timestamp: datetime) -> int:
    """
    Convert a datetime to milliseconds since epoch (for Highcharts)
    
    Args:
        timestamp: datetime object
        
    Returns:
        Milliseconds since epoch as integer
    """
    return int(timestamp.timestamp() * 1000)
