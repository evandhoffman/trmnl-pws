from datetime import datetime, timezone

import app.utils.solar as solar_module
from app.utils.solar import get_solar_events_between


class TestGetSolarEventsBetween:
    def test_returns_sunrise_noon_and_sunset_for_full_local_day(self):
        start = datetime(2024, 6, 15, 4, 0, tzinfo=timezone.utc)
        end = datetime(2024, 6, 16, 4, 0, tzinfo=timezone.utc)

        events = get_solar_events_between(
            start,
            end,
            latitude=40.7128,
            longitude=-74.0060,
            tz_name="America/New_York",
        )

        assert [event["kind"] for event in events] == [
            "sunrise",
            "solar_noon",
            "sunset",
        ]
        assert [event["short_label"] for event in events] == ["Rise", "Noon", "Set"]

    def test_filters_events_outside_requested_window(self):
        events = get_solar_events_between(
            datetime(2024, 6, 15, 0, 0, tzinfo=timezone.utc),
            datetime(2024, 6, 15, 1, 0, tzinfo=timezone.utc),
            latitude=40.7128,
            longitude=-74.0060,
            tz_name="America/New_York",
        )

        assert events == []

    def test_preserves_event_order_across_local_date_boundaries(self):
        start = datetime(2024, 12, 20, 23, 0, tzinfo=timezone.utc)
        end = datetime(2024, 12, 21, 23, 0, tzinfo=timezone.utc)

        events = get_solar_events_between(
            start,
            end,
            latitude=40.7128,
            longitude=-74.0060,
            tz_name="America/New_York",
        )

        timestamps = [event["timestamp_ms"] for event in events]
        assert timestamps == sorted(timestamps)

    def test_uses_pytz_timezone_lookup(self, monkeypatch):
        seen = []
        original_timezone = solar_module.pytz.timezone

        def tracking_timezone(name):
            seen.append(name)
            return original_timezone(name)

        monkeypatch.setattr(solar_module.pytz, "timezone", tracking_timezone)

        get_solar_events_between(
            datetime(2024, 6, 15, 4, 0, tzinfo=timezone.utc),
            datetime(2024, 6, 16, 4, 0, tzinfo=timezone.utc),
            latitude=40.7128,
            longitude=-74.0060,
            tz_name="America/New_York",
        )

        assert "America/New_York" in seen
