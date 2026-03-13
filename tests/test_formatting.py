import pytest
from datetime import datetime, timezone, timedelta
from app.utils.formatting import (
    format_relative_time,
    format_timestamp_for_display,
    timestamp_to_milliseconds,
)

FIXED_NOW = datetime(2024, 6, 15, 12, 0, 0, tzinfo=timezone.utc)


class TestFormatRelativeTime:
    def test_just_now(self):
        ts = FIXED_NOW - timedelta(seconds=30)
        assert format_relative_time(ts, now=FIXED_NOW) == "just now"

    def test_one_minute(self):
        ts = FIXED_NOW - timedelta(minutes=1)
        assert format_relative_time(ts, now=FIXED_NOW) == "1 minute ago"

    def test_plural_minutes(self):
        ts = FIXED_NOW - timedelta(minutes=5)
        assert format_relative_time(ts, now=FIXED_NOW) == "5 minutes ago"

    def test_one_hour(self):
        ts = FIXED_NOW - timedelta(hours=1)
        assert format_relative_time(ts, now=FIXED_NOW) == "1 hour ago"

    def test_plural_hours(self):
        ts = FIXED_NOW - timedelta(hours=3)
        assert format_relative_time(ts, now=FIXED_NOW) == "3 hours ago"

    def test_one_day(self):
        ts = FIXED_NOW - timedelta(days=1)
        assert format_relative_time(ts, now=FIXED_NOW) == "1 day ago"

    def test_plural_days(self):
        ts = FIXED_NOW - timedelta(days=3)
        assert format_relative_time(ts, now=FIXED_NOW) == "3 days ago"

    def test_one_week(self):
        ts = FIXED_NOW - timedelta(weeks=1)
        assert format_relative_time(ts, now=FIXED_NOW) == "7 days ago"

    def test_plural_weeks(self):
        ts = FIXED_NOW - timedelta(weeks=3)
        assert format_relative_time(ts, now=FIXED_NOW) == "21 days ago"

    def test_naive_timestamp_treated_as_utc(self):
        naive = datetime(2024, 6, 15, 11, 59, 0)  # no tzinfo
        result = format_relative_time(naive, now=FIXED_NOW)
        assert result == "1 minute ago"


class TestFormatTimestampForDisplay:
    def test_utc_to_eastern(self):
        # 2024-01-18 19:15 UTC = 2:15 PM EST
        ts = datetime(2024, 1, 18, 19, 15, 0, tzinfo=timezone.utc)
        result = format_timestamp_for_display(ts, "America/New_York")
        assert result == "Thu 18 Jan, 02:15 PM"

    def test_naive_datetime_treated_as_utc(self):
        naive = datetime(2024, 1, 18, 19, 15, 0)
        result = format_timestamp_for_display(naive, "America/New_York")
        assert result == "Thu 18 Jan, 02:15 PM"

    def test_custom_format(self):
        ts = datetime(2024, 6, 15, 12, 0, 0, tzinfo=timezone.utc)
        result = format_timestamp_for_display(ts, "UTC", "%Y-%m-%d")
        assert result == "2024-06-15"


class TestTimestampToMilliseconds:
    def test_unix_epoch(self):
        epoch = datetime(1970, 1, 1, 0, 0, 0, tzinfo=timezone.utc)
        assert timestamp_to_milliseconds(epoch) == 0

    def test_known_timestamp(self):
        ts = datetime(2024, 1, 1, 0, 0, 0, tzinfo=timezone.utc)
        ms = timestamp_to_milliseconds(ts)
        assert ms == 1704067200000

    def test_returns_int(self):
        ts = datetime(2024, 6, 15, 12, 0, 0, tzinfo=timezone.utc)
        assert isinstance(timestamp_to_milliseconds(ts), int)
