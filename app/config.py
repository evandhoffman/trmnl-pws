"""Configuration loader for TRMNL PWS application"""

import os
import yaml
import logging
from typing import Dict, Any

logger = logging.getLogger(__name__)


def load_config(config_path: str = 'config/config.yml') -> Dict[str, Any]:
    """
    Load configuration from YAML file
    
    Args:
        config_path: Path to config.yml file
        
    Returns:
        Dictionary containing configuration
        
    Raises:
        FileNotFoundError: If config file doesn't exist
        ValueError: If config is invalid
    """
    if not os.path.exists(config_path):
        raise FileNotFoundError(f"Configuration file not found: {config_path}")
    
    with open(config_path, 'r') as f:
        config = yaml.safe_load(f)
    
    # Validate required sections
    required_sections = ['general', 'influxdb', 'plugins']
    for section in required_sections:
        if section not in config:
            raise ValueError(f"Missing required configuration section: {section}")
    
    logger.info(f"Loaded configuration from {config_path}")
    return config


def load_secrets(secrets_path: str = 'config/secrets.yml') -> Dict[str, Any]:
    """
    Load secrets from YAML file
    
    Args:
        secrets_path: Path to secrets.yml file
        
    Returns:
        Dictionary containing secrets
        
    Raises:
        FileNotFoundError: If secrets file doesn't exist
        ValueError: If secrets are invalid
    """
    if not os.path.exists(secrets_path):
        raise FileNotFoundError(f"Secrets file not found: {secrets_path}")
    
    with open(secrets_path, 'r') as f:
        secrets = yaml.safe_load(f)
    
    # Validate required sections
    required_sections = ['influxdb', 'webhooks']
    for section in required_sections:
        if section not in secrets:
            raise ValueError(f"Missing required secrets section: {section}")
    
    # Validate InfluxDB token
    if not secrets['influxdb'].get('token'):
        raise ValueError("Missing InfluxDB token in secrets")
    
    logger.info(f"Loaded secrets from {secrets_path}")
    return secrets
