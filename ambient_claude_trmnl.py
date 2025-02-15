import asyncio
import logging
from aioambient.api import API
import httpx
import os
from dotenv import load_dotenv
from datetime import datetime
import pytz

# Load environment variables
load_dotenv()

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

# Constants
WEBHOOK_URL = "https://usetrmnl.com/api/custom_plugins/e2037c24-42ad-4726-b810-9ef9ddb24e81"
TIMEZONE = os.getenv('TIMEZONE', 'America/New_York')  # Default to Eastern Time if not specified

def format_date(epoch_ms: int) -> str:
    """Convert epoch timestamp to pretty date format in local timezone"""
    # Convert milliseconds to seconds if necessary
    epoch_sec = epoch_ms / 1000 if epoch_ms > 1000000000000 else epoch_ms
    
    # Create datetime object in UTC
    utc_dt = datetime.fromtimestamp(epoch_sec, pytz.UTC)
    
    # Convert to local timezone
    local_tz = pytz.timezone(TIMEZONE)
    local_dt = utc_dt.astimezone(local_tz)
    
    # Format date: "Sat 15 Feb, 11:29 AM"
    return local_dt.strftime("%a %d %b, %I:%M %p")

async def post_to_webhook(data: dict):
    """Post data to the webhook endpoint"""
    headers = {
        "Content-Type": "application/json"
    }
    
    # Add pretty date format if dateutc exists
    if 'dateutc' in data:
        data['date_pretty'] = format_date(int(data['dateutc']))
    
    # Format the data to match the expected structure
    payload = {
        "merge_variables": data
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