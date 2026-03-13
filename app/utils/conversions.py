"""Utility functions for unit conversions"""

from typing import Union


def degrees_to_cardinal(degrees: float) -> str:
    """
    Convert wind direction in degrees to cardinal direction

    Args:
        degrees: Wind direction in degrees (0-360)

    Returns:
        Cardinal direction string (e.g., "N", "NNE", "NE", etc.)
    """
    directions = [
        "N",
        "NNE",
        "NE",
        "ENE",
        "E",
        "ESE",
        "SE",
        "SSE",
        "S",
        "SSW",
        "SW",
        "WSW",
        "W",
        "WNW",
        "NW",
        "NNW",
    ]

    # Normalize degrees to 0-360
    degrees = degrees % 360

    # Calculate index (16 directions, so 360/16 = 22.5 degrees each)
    index = round(degrees / 22.5) % 16

    return directions[index]


def format_wind_description(speed_mph: float, direction_degrees: float) -> str:
    """
    Format wind speed and direction for display

    Args:
        speed_mph: Wind speed in mph
        direction_degrees: Wind direction in degrees

    Returns:
        Formatted string (e.g., "5.2 mph from the NW")
    """
    cardinal = degrees_to_cardinal(direction_degrees)
    return f"{speed_mph} mph from the {cardinal}"


def degrees_to_arrow(degrees: float) -> str:
    """
    Convert wind direction in degrees to a simple arrow glyph.

    Args:
        degrees: Wind direction in degrees (0-360)

    Returns:
        Arrow glyph representing the nearest cardinal/intercardinal direction
    """
    arrows = ["↑", "↗", "→", "↘", "↓", "↙", "←", "↖"]
    degrees = degrees % 360
    index = round(degrees / 45) % 8
    return arrows[index]


def format_compact_wind(speed_mph: float, direction_degrees: float) -> str:
    """
    Format wind for compact dashboard tiles.

    Args:
        speed_mph: Wind speed in mph
        direction_degrees: Wind direction in degrees

    Returns:
        Compact formatted string (e.g., "↗ NE 5 mph")
    """
    cardinal = degrees_to_cardinal(direction_degrees)
    arrow = degrees_to_arrow(direction_degrees)
    return f"{arrow} {cardinal} {round_value(speed_mph, 0):.0f} mph"


def round_value(value: Union[float, int], decimals: int = 1) -> float:
    """
    Round a numeric value to specified decimal places

    Args:
        value: Numeric value to round
        decimals: Number of decimal places

    Returns:
        Rounded value
    """
    if value is None:
        return 0.0
    return round(float(value), decimals)
