import pytest
from app.utils.conversions import degrees_to_cardinal, format_wind_description, round_value


class TestDegreesToCardinal:
    def test_north(self):
        assert degrees_to_cardinal(0) == "N"
        assert degrees_to_cardinal(360) == "N"

    def test_cardinal_directions(self):
        assert degrees_to_cardinal(90) == "E"
        assert degrees_to_cardinal(180) == "S"
        assert degrees_to_cardinal(270) == "W"

    def test_intercardinal_directions(self):
        assert degrees_to_cardinal(45) == "NE"
        assert degrees_to_cardinal(135) == "SE"
        assert degrees_to_cardinal(225) == "SW"
        assert degrees_to_cardinal(315) == "NW"

    def test_secondary_intercardinal(self):
        assert degrees_to_cardinal(22.5) == "NNE"
        assert degrees_to_cardinal(67.5) == "ENE"
        assert degrees_to_cardinal(157.5) == "SSE"
        assert degrees_to_cardinal(337.5) == "NNW"

    def test_normalizes_over_360(self):
        assert degrees_to_cardinal(360 + 90) == "E"
        assert degrees_to_cardinal(720) == "N"

    def test_boundary_rounds_to_nearest(self):
        # 11.24 degrees rounds to N (index 0), 11.26 rounds to NNE (index 1)
        assert degrees_to_cardinal(11.0) == "N"
        assert degrees_to_cardinal(12.0) == "NNE"


class TestFormatWindDescription:
    def test_basic_format(self):
        result = format_wind_description(5.2, 315)
        assert result == "5.2 mph from the NW"

    def test_north_wind(self):
        result = format_wind_description(10, 0)
        assert result == "10 mph from the N"

    def test_zero_speed(self):
        result = format_wind_description(0, 180)
        assert result == "0 mph from the S"


class TestRoundValue:
    def test_none_returns_zero(self):
        assert round_value(None) == 0.0

    def test_rounds_to_one_decimal_by_default(self):
        assert round_value(3.567) == 3.6

    def test_rounds_to_specified_decimals(self):
        assert round_value(3.567, 2) == 3.57
        assert round_value(3.567, 0) == 4.0

    def test_integer_input(self):
        assert round_value(5) == 5.0

    def test_returns_float(self):
        assert isinstance(round_value(3), float)
