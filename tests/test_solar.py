from datetime import date, datetime

import app.utils.solar as solar_module
from app.utils.solar import get_solar_events_for_date


class TestGetSolarEventsBetween:
    USER_LATITUDE = 40.74276375822451
    USER_LONGITUDE = -73.98457100144142

    def test_returns_sunrise_noon_and_sunset_for_full_local_day(self):
        events = get_solar_events_for_date(
            date(2024, 6, 15),
            latitude=self.USER_LATITUDE,
            longitude=self.USER_LONGITUDE,
            tz_name="America/New_York",
        )

        assert [event["label"] for event in events] == ["Rise", "Noon", "Set"]

    def test_returns_same_payload_for_repeated_requests_on_same_date(self):
        first = get_solar_events_for_date(
            date(2024, 6, 15),
            latitude=self.USER_LATITUDE,
            longitude=self.USER_LONGITUDE,
            tz_name="America/New_York",
        )
        second = get_solar_events_for_date(
            date(2024, 6, 15),
            latitude=self.USER_LATITUDE,
            longitude=self.USER_LONGITUDE,
            tz_name="America/New_York",
        )

        assert first == second

    def test_uses_pytz_timezone_lookup(self, monkeypatch):
        seen = []
        original_timezone = solar_module.pytz.timezone

        def tracking_timezone(name):
            seen.append(name)
            return original_timezone(name)

        monkeypatch.setattr(solar_module.pytz, "timezone", tracking_timezone)
        solar_module.SOLAR_EVENT_CACHE.clear()

        get_solar_events_for_date(
            date(2024, 6, 15),
            latitude=self.USER_LATITUDE,
            longitude=self.USER_LONGITUDE,
            tz_name="America/New_York",
        )

        assert "America/New_York" in seen

    def test_returns_non_empty_event_payload_for_user_coordinates(self):
        events = get_solar_events_for_date(
            date(2024, 6, 15),
            latitude=self.USER_LATITUDE,
            longitude=self.USER_LONGITUDE,
            tz_name="America/New_York",
        )

        assert events
        assert any(event["label"] == "Noon" for event in events)
        assert all(event["timestamp_ms"] > 0 for event in events)

    def test_event_timestamps_land_on_requested_local_date(self):
        events = get_solar_events_for_date(
            date(2026, 3, 13),
            latitude=40.66148862046403,
            longitude=-73.52188909967647,
            tz_name="America/New_York",
        )

        assert {
            datetime.fromtimestamp(event["timestamp_ms"] / 1000, solar_module.pytz.timezone("America/New_York")).date()
            for event in events
        } == {date(2026, 3, 13)}
