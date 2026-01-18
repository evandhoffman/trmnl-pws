"""Base plugin interface for TRMNL data collectors"""

import logging
from abc import ABC, abstractmethod
from typing import Dict, Any
from influxdb_client import InfluxDBClient

logger = logging.getLogger(__name__)


class BasePlugin(ABC):
    """
    Base class for all TRMNL data collector plugins
    
    Each plugin is responsible for:
    1. Querying data from InfluxDB
    2. Formatting data for TRMNL's merge_variables structure
    3. Providing its webhook ID
    """
    
    def __init__(self, config: Dict[str, Any], secrets: Dict[str, Any], influx_client: InfluxDBClient):
        """
        Initialize the plugin
        
        Args:
            config: Application configuration dictionary
            secrets: Secrets configuration dictionary
            influx_client: InfluxDB client instance
        """
        self.config = config
        self.secrets = secrets
        self.influx_client = influx_client
        self.general_config = config.get('general', {})
        self.influx_config = config.get('influxdb', {})
        
        # Each plugin should set this in __init__
        self.plugin_name = self.__class__.__name__
        
        logger.info(f"Initialized plugin: {self.plugin_name}")
    
    @abstractmethod
    def collect_data(self) -> Dict[str, Any]:
        """
        Collect data from InfluxDB and return formatted merge_variables
        
        Returns:
            Dictionary of merge_variables for TRMNL webhook
            
        Raises:
            Exception: If data collection fails
        """
        pass
    
    @abstractmethod
    def get_webhook_id(self) -> str:
        """
        Get the webhook ID for this plugin
        
        Returns:
            Webhook UUID string
        """
        pass
    
    def get_timezone(self) -> str:
        """
        Get the configured timezone
        
        Returns:
            Timezone string (e.g., 'America/New_York')
        """
        return self.general_config.get('timezone', 'America/New_York')
    
    def get_bucket(self) -> str:
        """
        Get the InfluxDB bucket name
        
        Returns:
            Bucket name string
        """
        return self.influx_config.get('bucket', 'home_assistant/autogen')
