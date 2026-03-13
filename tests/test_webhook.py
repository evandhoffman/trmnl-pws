import pytest
from unittest.mock import MagicMock, patch
from app.webhook import post_to_webhook

WEBHOOK_ID = "test-webhook-1234-5678-abcd"


def make_response(status_code=200, raise_for_status=None):
    resp = MagicMock()
    resp.status_code = status_code
    if raise_for_status:
        resp.raise_for_status.side_effect = raise_for_status
    else:
        resp.raise_for_status.return_value = None
    return resp


class TestPostToWebhook:
    def test_success(self):
        with patch("app.webhook.requests.post", return_value=make_response(200)) as mock_post:
            result = post_to_webhook(WEBHOOK_ID, {"key": "value"})
        assert result == "success"
        mock_post.assert_called_once()

    def test_rate_limited_returns_rate_limited(self):
        with patch("app.webhook.requests.post", return_value=make_response(429)):
            result = post_to_webhook(WEBHOOK_ID, {"key": "value"})
        assert result == "rate_limited"

    def test_http_error_returns_failed(self):
        import requests as req_lib
        resp = make_response(500, raise_for_status=req_lib.exceptions.HTTPError("500"))
        with patch("app.webhook.requests.post", return_value=resp):
            result = post_to_webhook(WEBHOOK_ID, {"key": "value"})
        assert result == "failed"

    def test_request_exception_returns_failed(self):
        import requests as req_lib
        with patch("app.webhook.requests.post", side_effect=req_lib.exceptions.ConnectionError("no connection")):
            result = post_to_webhook(WEBHOOK_ID, {"key": "value"})
        assert result == "failed"

    def test_oversized_payload_standard_tier_returns_failed(self):
        # Just over 2 KB for standard tier
        big_data = {"data": "x" * 2100}
        result = post_to_webhook(WEBHOOK_ID, big_data, trmnl_plus=False)
        assert result == "failed"

    def test_oversized_for_standard_but_ok_for_plus(self):
        # ~2.5 KB — over standard (2 KB) but under TRMNL+ (5 KB)
        medium_data = {"data": "x" * 2400}
        with patch("app.webhook.requests.post", return_value=make_response(200)):
            result = post_to_webhook(WEBHOOK_ID, medium_data, trmnl_plus=True)
        assert result == "success"

    def test_oversized_payload_plus_tier_returns_failed(self):
        # Just over 5 KB for TRMNL+
        big_data = {"data": "x" * 5200}
        result = post_to_webhook(WEBHOOK_ID, big_data, trmnl_plus=True)
        assert result == "failed"

    def test_posts_to_correct_url(self):
        with patch("app.webhook.requests.post", return_value=make_response(200)) as mock_post:
            post_to_webhook(WEBHOOK_ID, {"k": "v"})
        url = mock_post.call_args[0][0]
        assert WEBHOOK_ID in url
        assert url.startswith("https://usetrmnl.com/api/custom_plugins/")
