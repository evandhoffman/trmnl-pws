"""Shared TRMNL webhook poster"""

import json
import logging
import requests
from typing import Dict, Any

logger = logging.getLogger(__name__)

TRMNL_BASE_URL = "https://usetrmnl.com/api/custom_plugins"


def post_to_webhook(
    webhook_id: str, 
    merge_variables: Dict[str, Any],
    trmnl_plus: bool = False
) -> bool:
    """
    Post data to a TRMNL webhook endpoint
    
    Args:
        webhook_id: The webhook UUID
        merge_variables: Data to send to the webhook
        trmnl_plus: Whether user has TRMNL+ subscription (affects payload size limit)
        
    Returns:
        True if successful, False otherwise
        
    Raises:
        PayloadTooLargeError: If payload exceeds size limits
    """
    url = f"{TRMNL_BASE_URL}/{webhook_id}"
    payload = {"merge_variables": merge_variables}
    
    # Convert to JSON to check size
    json_payload = json.dumps(payload, default=str)
    payload_size = len(json_payload.encode('utf-8'))
    
    # Check payload size limits
    max_size = 5120 if trmnl_plus else 2048  # 5KB for TRMNL+, 2KB for standard
    if payload_size > max_size:
        logger.error(
            f"Payload size ({payload_size} bytes) exceeds limit "
            f"({max_size} bytes for {'TRMNL+' if trmnl_plus else 'standard tier'})"
        )
        return False
    
    logger.debug(f"Posting {payload_size} bytes to webhook {webhook_id[:8]}...")
    
    try:
        response = requests.post(
            url,
            json=payload,
            headers={"Content-Type": "application/json"},
            timeout=30
        )
        
        if response.status_code == 429:
            logger.error("Rate limit exceeded (429). Consider increasing poll_interval.")
            return False
        
        response.raise_for_status()
        logger.info(
            f"Successfully posted data to webhook {webhook_id[:8]}... "
            f"({payload_size} bytes, status {response.status_code})"
        )
        return True
        
    except requests.exceptions.HTTPError as e:
        logger.error(f"HTTP error posting to webhook: {e} - {response.text if response else 'No response'}")
        return False
    except requests.exceptions.RequestException as e:
        logger.error(f"Error posting to webhook: {e}")
        return False
