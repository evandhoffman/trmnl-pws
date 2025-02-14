import asyncio
import logging
from aioambient.api import API
import os
from dotenv import load_dotenv

# Load environment variables
load_dotenv()

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

async def fetch_weather_data():
    # Get API keys from environment
    api_key = os.getenv('AMBIENT_API_KEY')
    app_key = os.getenv('AMBIENT_APPLICATION_KEY')
    
    if not api_key or not app_key:
        logger.error("Missing API keys - please check your .env file")
        return
    
    try:
        # Initialize the API client
        client = API(app_key, api_key)
        
        # Get all devices
        devices = await client.get_devices()
        logger.info(f"Found {len(devices)} devices")
        
        # Fetch data for each device
        for device in devices:
            logger.info(f"Fetching data for device: {device['macAddress']}")
            data = await client.get_device_details(device['macAddress'], limit=1)
            if data:
                logger.info("Latest weather data:")
                for key, value in data[0].items():
                    logger.info(f"  {key}: {value}")
                logger.info(f"Json: {data}")
            else:
                logger.warning(f"No data available for device {device['macAddress']}")
            
    except Exception as e:
        logger.error(f"Error fetching weather data: {str(e)}")
        raise

if __name__ == "__main__":
    asyncio.run(fetch_weather_data())
