import json
import pytest
from datetime import datetime, timezone, timedelta
from pathlib import Path
from unittest.mock import patch

import app.state as state_module
from app.state import (
    calculate_backoff,
    ensure_webhook_initialized,
    get_failure_count,
    load_state,
    record_update,
    save_state,
    should_update,
)

WEBHOOK_ID = "test-webhook-1234-5678-abcd"


@pytest.fixture(autouse=True)
def tmp_state_file(tmp_path, monkeypatch):
    """Redirect STATE_FILE to a temp path for every test."""
    tmp_file = tmp_path / "test_state.lock"
    monkeypatch.setattr(state_module, "STATE_FILE", tmp_file)
    return tmp_file


class TestCalculateBackoff:
    def test_zero_failures_returns_base(self):
        assert calculate_backoff(0, 300) == 300

    def test_one_failure_doubles(self):
        assert calculate_backoff(1, 300) == 600

    def test_two_failures_quadruples(self):
        assert calculate_backoff(2, 300) == 1200

    def test_capped_at_one_hour(self):
        assert calculate_backoff(100, 300) == 3600

    def test_cap_with_small_base(self):
        assert calculate_backoff(20, 1) == 3600


class TestGetFailureCount:
    def test_new_format_returns_count(self):
        s = {WEBHOOK_ID: {"timestamp": "2024-01-01T00:00:00+00:00", "failure_count": 3}}
        assert get_failure_count(s, WEBHOOK_ID) == 3

    def test_missing_webhook_returns_zero(self):
        assert get_failure_count({}, WEBHOOK_ID) == 0

    def test_legacy_string_format_returns_zero(self):
        s = {WEBHOOK_ID: "2024-01-01T00:00:00+00:00"}
        assert get_failure_count(s, WEBHOOK_ID) == 0

    def test_missing_failure_count_key_returns_zero(self):
        s = {WEBHOOK_ID: {"timestamp": "2024-01-01T00:00:00+00:00"}}
        assert get_failure_count(s, WEBHOOK_ID) == 0


class TestEnsureWebhookInitialized:
    def test_initializes_missing_webhook(self):
        s = {}
        modified = ensure_webhook_initialized(s, WEBHOOK_ID)
        assert modified is True
        assert WEBHOOK_ID in s
        assert s[WEBHOOK_ID]["failure_count"] == 0
        assert "timestamp" in s[WEBHOOK_ID]

    def test_does_not_overwrite_existing(self):
        ts = "2024-01-01T00:00:00+00:00"
        s = {WEBHOOK_ID: {"timestamp": ts, "failure_count": 2}}
        modified = ensure_webhook_initialized(s, WEBHOOK_ID)
        assert modified is False
        assert s[WEBHOOK_ID]["failure_count"] == 2
        assert s[WEBHOOK_ID]["timestamp"] == ts


class TestRecordUpdate:
    def test_success_resets_failure_count(self):
        s = {WEBHOOK_ID: {"timestamp": "2024-01-01T00:00:00+00:00", "failure_count": 3}}
        record_update(s, WEBHOOK_ID, success=True)
        assert s[WEBHOOK_ID]["failure_count"] == 0

    def test_failure_increments_failure_count(self):
        s = {WEBHOOK_ID: {"timestamp": "2024-01-01T00:00:00+00:00", "failure_count": 1}}
        record_update(s, WEBHOOK_ID, success=False)
        assert s[WEBHOOK_ID]["failure_count"] == 2

    def test_first_failure_sets_count_to_one(self):
        s = {WEBHOOK_ID: {"timestamp": "2024-01-01T00:00:00+00:00", "failure_count": 0}}
        record_update(s, WEBHOOK_ID, success=False)
        assert s[WEBHOOK_ID]["failure_count"] == 1

    def test_migrates_legacy_string_format(self):
        s = {WEBHOOK_ID: "2024-01-01T00:00:00+00:00"}
        record_update(s, WEBHOOK_ID, success=False)
        assert isinstance(s[WEBHOOK_ID], dict)
        assert s[WEBHOOK_ID]["failure_count"] == 1

    def test_updates_timestamp(self):
        old_ts = "2020-01-01T00:00:00+00:00"
        s = {WEBHOOK_ID: {"timestamp": old_ts, "failure_count": 0}}
        record_update(s, WEBHOOK_ID, success=True)
        assert s[WEBHOOK_ID]["timestamp"] != old_ts


class TestShouldUpdate:
    def _state_with_age(self, seconds_ago, failure_count=0):
        """Build a state dict where last update was `seconds_ago` seconds in the past."""
        ts = (datetime.now(timezone.utc) - timedelta(seconds=seconds_ago)).isoformat()
        return {WEBHOOK_ID: {"timestamp": ts, "failure_count": failure_count}}

    def test_never_updated_returns_true(self):
        assert should_update({}, WEBHOOK_ID, poll_interval=300) is True

    def test_recent_update_returns_false(self):
        s = self._state_with_age(seconds_ago=60)
        assert should_update(s, WEBHOOK_ID, poll_interval=300) is False

    def test_old_enough_returns_true(self):
        s = self._state_with_age(seconds_ago=400)
        assert should_update(s, WEBHOOK_ID, poll_interval=300) is True

    def test_backoff_prevents_update_after_failure(self):
        # 1 failure → backoff = 600s; only 400s have elapsed → should not update
        s = self._state_with_age(seconds_ago=400, failure_count=1)
        assert should_update(s, WEBHOOK_ID, poll_interval=300) is False

    def test_update_allowed_after_backoff_expires(self):
        # 1 failure → backoff = 600s; 700s elapsed → should update
        s = self._state_with_age(seconds_ago=700, failure_count=1)
        assert should_update(s, WEBHOOK_ID, poll_interval=300) is True


class TestLoadSaveState:
    def test_save_and_load_roundtrip(self, tmp_state_file):
        s = {WEBHOOK_ID: {"timestamp": "2024-01-01T00:00:00+00:00", "failure_count": 2}}
        save_state(s)
        loaded = load_state()
        assert loaded == s

    def test_load_missing_file_returns_empty(self, tmp_state_file):
        assert not tmp_state_file.exists()
        assert load_state() == {}

    def test_load_corrupt_file_returns_empty(self, tmp_state_file):
        tmp_state_file.write_text("not valid json")
        assert load_state() == {}
