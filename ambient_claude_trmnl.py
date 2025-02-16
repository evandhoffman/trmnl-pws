import asyncio
import logging
from aioambient.api import API
import httpx
import os
from dotenv import load_dotenv
from datetime import datetime, timezone

# Load environment variables
load_dotenv()

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

# Constants
WEBHOOK_ID = os.getenv('WEBHOOK_ID')
WEBHOOK_URL = f"https://usetrmnl.com/api/custom_plugins/{WEBHOOK_ID}"
TIMEZONE = os.getenv('TIMEZONE', 'America/New_York')

if not WEBHOOK_ID:
    raise ValueError("Missing WEBHOOK_ID environment variable")

def format_last_rain(last_rain_str: str) -> str:
    """Calculate relative time since last rain"""
    last_rain = datetime.fromisoformat(last_rain_str.replace('Z', '+00:00'))
    now = datetime.now(timezone.utc)
    diff = now - last_rain
    minutes = diff.total_seconds() / 60
    
    if minutes < 15:
        return "just now"
    elif minutes < 60:
        return f"{int(minutes)} minutes ago"
    elif minutes < 1440:  # 24 hours
        hours = int(minutes / 60)
        return f"{hours} hours ago"
    elif minutes < 20160:  # 14 days
        days = int(minutes / 1440)
        return f"{days} days ago"
    else:
        weeks = int(minutes / 10080)  # 7 days
        return f"{weeks} weeks ago"

def get_cardinal_direction(degrees: float) -> str:
    """Convert degrees to cardinal direction"""
    directions = [
        "N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE",
        "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"
    ]
    index = round(((degrees % 360) / 22.5)) % 16
    return directions[index]

async def post_to_webhook(data: dict):
    """Post data to the webhook endpoint"""
    headers = {
        "Content-Type": "application/json"
    }
    
    # Create a copy of the data to avoid modifying the original
    webhook_data = data.copy()
    
    # Add relative time for last rain
    if 'lastRain' in webhook_data:
        webhook_data['last_rain_date_pretty'] = format_last_rain(webhook_data['lastRain'])
    
    # Add cardinal wind direction
    if 'winddir' in webhook_data and 'windspeedmph' in webhook_data:
        cardinal = get_cardinal_direction(float(webhook_data['winddir']))
        webhook_data['winddir_pretty'] = f"{webhook_data['windspeedmph']} mph from the {cardinal}"
    
    # Format the data to match the expected structure
    payload = {
        "merge_variables": webhook_data
    }
    
    async with httpx.AsyncClient() as client:
        try:
            response = await client.post(WEBHOOK_URL, json=payload, headers=headers)
            response.raise_for_status()
            logger.info(f"Successfully posted data to webhook. Status: {response.status_code}")
            logger.debug(f"Webhook response: {response.text}")
        except httpx.HTTPError as e:
            logger.error(f"Error posting to webhook: {str(e)}")
            raise

async def fetch_and_post_weather_data():
    # Get API keys from environment
    api_key = os.getenv('AMBIENT_API_KEY')
    app_key = os.getenv('AMBIENT_APPLICATION_KEY')
    
    if not api_key or not app_key:
        logger.error("Missing API keys - please check your .env file")
        return
    
    try:
        # Initialize the API client
        client = API(app_key, api_key)  # application_key comes first
        
        # Get all devices
        devices = await client.get_devices()
        logger.info(f"Found {len(devices)} devices")
        
        # Fetch and post data for each device
        for device in devices:
            logger.info(f"Fetching data for device: {device['macAddress']}")
            data = await client.get_device_details(device['macAddress'], limit=1)
            
            if data and len(data) > 0:
                latest_data = data[0]
                logger.info("Latest weather data:")
                for key, value in latest_data.items():
                    logger.info(f"  {key}: {value}")
                
                # Post to webhook
                logger.info("Posting data to webhook...")
                await post_to_webhook(latest_data)
            else:
                logger.warning(f"No data available for device {device['macAddress']}")
            
    except Exception as e:
        logger.error(f"Error in fetch and post operation: {str(e)}")
        raise

if __name__ == "__main__":
    asyncio.run(fetch_and_post_weather_data())