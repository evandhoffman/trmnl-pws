"""Helpers for calculating local solar events for chart annotations."""

from __future__ import annotations

import math
from dataclasses import dataclass
from datetime import date, datetime, time, timedelta, timezone
from typing import Any

import pytz

from app.utils.formatting import timestamp_to_milliseconds


ZENITH_DEGREES = -0.83
JULIAN_UNIX_EPOCH = 2440587.5
J2000 = 2451545.0
SOLAR_TRANSIT_LONGITUDE_OFFSET = 102.9372


@dataclass(frozen=True)
class SolarEvent:
    kind: str
    label: str
    short_label: str
    timestamp: datetime

    def to_payload(self) -> dict[str, Any]:
        return {
            "kind": self.kind,
            "label": self.label,
            "short_label": self.short_label,
            "timestamp_ms": timestamp_to_milliseconds(self.timestamp),
            "time_pretty": self.timestamp.strftime("%-I:%M %p"),
        }


def _julian_day_to_datetime(julian_day: float) -> datetime:
    unix_seconds = (julian_day - JULIAN_UNIX_EPOCH) * 86400
    return datetime.fromtimestamp(unix_seconds, tz=timezone.utc)


def _solar_events_for_date(
    local_date: date, latitude: float, longitude: float, tz_name: str
) -> dict[str, datetime]:
    latitude_rad = math.radians(latitude)
    tz = pytz.timezone(tz_name)
    west_longitude = -longitude
    local_midnight = tz.localize(datetime.combine(local_date, time.min))
    utc_midnight = local_midnight.astimezone(timezone.utc)
    julian_day = utc_midnight.timestamp() / 86400 + JULIAN_UNIX_EPOCH

    solar_cycle = round(julian_day - J2000 - 0.0009 - (west_longitude / 360))
    solar_noon_guess = J2000 + 0.0009 + (west_longitude / 360) + solar_cycle

    mean_anomaly = math.radians(
        (357.5291 + 0.98560028 * (solar_noon_guess - J2000)) % 360
    )
    equation_of_center = (
        1.9148 * math.sin(mean_anomaly)
        + 0.0200 * math.sin(2 * mean_anomaly)
        + 0.0003 * math.sin(3 * mean_anomaly)
    )
    ecliptic_longitude = math.radians(
        (math.degrees(mean_anomaly) + equation_of_center + 180 + SOLAR_TRANSIT_LONGITUDE_OFFSET)
        % 360
    )

    solar_transit = (
        solar_noon_guess
        + 0.0053 * math.sin(mean_anomaly)
        - 0.0069 * math.sin(2 * ecliptic_longitude)
    )
    declination = math.asin(math.sin(ecliptic_longitude) * math.sin(math.radians(23.44)))

    hour_angle_cos = (
        math.sin(math.radians(ZENITH_DEGREES))
        - math.sin(latitude_rad) * math.sin(declination)
    ) / (math.cos(latitude_rad) * math.cos(declination))
    hour_angle_cos = min(1.0, max(-1.0, hour_angle_cos))
    hour_angle = math.degrees(math.acos(hour_angle_cos))

    sunrise = _julian_day_to_datetime(solar_transit - hour_angle / 360).astimezone(tz)
    solar_noon = _julian_day_to_datetime(solar_transit).astimezone(tz)
    sunset = _julian_day_to_datetime(solar_transit + hour_angle / 360).astimezone(tz)

    return {
        "sunrise": sunrise,
        "solar_noon": solar_noon,
        "sunset": sunset,
    }


def get_solar_events_between(
    start: datetime, end: datetime, latitude: float, longitude: float, tz_name: str
) -> list[dict[str, Any]]:
    """Return solar event payloads that fall within a chart's visible time range."""
    if start.tzinfo is None:
        start = start.replace(tzinfo=timezone.utc)
    if end.tzinfo is None:
        end = end.replace(tzinfo=timezone.utc)

    if end < start:
        start, end = end, start

    start_ms = timestamp_to_milliseconds(start.astimezone(timezone.utc))
    end_ms = timestamp_to_milliseconds(end.astimezone(timezone.utc))
    tz = pytz.timezone(tz_name)
    current_date = start.astimezone(tz).date()
    end_date = end.astimezone(tz).date()
    events: list[SolarEvent] = []

    while current_date <= end_date:
        day_events = _solar_events_for_date(current_date, latitude, longitude, tz_name)
        events.extend(
            [
                SolarEvent(
                    kind="sunrise",
                    label=f"Sunrise {day_events['sunrise'].strftime('%-I:%M %p')}",
                    short_label="Rise",
                    timestamp=day_events["sunrise"],
                ),
                SolarEvent(
                    kind="solar_noon",
                    label=f"Solar Noon {day_events['solar_noon'].strftime('%-I:%M %p')}",
                    short_label="Noon",
                    timestamp=day_events["solar_noon"],
                ),
                SolarEvent(
                    kind="sunset",
                    label=f"Sunset {day_events['sunset'].strftime('%-I:%M %p')}",
                    short_label="Set",
                    timestamp=day_events["sunset"],
                ),
            ]
        )
        current_date += timedelta(days=1)

    payloads = [event.to_payload() for event in events]
    return [
        payload
        for payload in payloads
        if start_ms <= payload["timestamp_ms"] <= end_ms
    ]
