import asyncio
import logging
from aioambient.api import API
import httpx
import os
import signal
from dotenv import load_dotenv
from datetime import datetime, timezone
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
WEBHOOK_ID = os.getenv('WEBHOOK_ID')
WEBHOOK_URL = f"https://usetrmnl.com/api/custom_plugins/{WEBHOOK_ID}"
TIMEZONE = os.getenv('TIMEZONE', 'America/New_York')
INTERVAL_SECONDS = int(os.getenv('INTERVAL_SECONDS', '300'))  # Default to 5 minutes

if not WEBHOOK_ID:
    raise ValueError("Missing WEBHOOK_ID environment variable")

def format_last_rain(last_rain_str: str) -> str:
    """Calculate relative time since last rain"""
    last_rain = datetime.fromisoformat(last_rain_str.replace('Z', '+00:00'))
    now = datetime.now(timezone.utc)
    diff = now - last_rain
    minutes = diff.total_seconds() / 60
    
    if minutes < 16:
        return "just now"
    elif minutes < 60:
        return f"{int(minutes)} minutes ago"
    elif minutes < 2880:
        hours = int(minutes / 60)
        return f"{hours} hours ago"
    elif minutes < 43200:
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
    
    # Add formatted date for current reading
    if 'dateutc' in webhook_data:
        epoch_ms = int(webhook_data['dateutc'])
        # Convert milliseconds to seconds if necessary
        epoch_sec = epoch_ms / 1000 if epoch_ms > 1000000000000 else epoch_ms
        # Create datetime in UTC
        utc_dt = datetime.fromtimestamp(epoch_sec, timezone.utc)
        # Convert to local timezone
        local_tz = pytz.timezone(TIMEZONE)
        local_dt = utc_dt.astimezone(local_tz)
        # Format: "Sun 16 Feb, 03:15 PM"
        webhook_data['date_pretty'] = local_dt.strftime("%a %d %b, %I:%M %p")
    
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

async def main():
    # Setup signal handlers
    loop = asyncio.get_running_loop()
    loop.add_signal_handler(signal.SIGINT, lambda: asyncio.create_task(shutdown()))
    loop.add_signal_handler(signal.SIGTERM, lambda: asyncio.create_task(shutdown()))

    logger.info(f"Starting weather collection service. Interval: {INTERVAL_SECONDS} seconds")
    
    try:
        while True:
            try:
                await fetch_and_post_weather_data()
            except Exception as e:
                logger.error(f"Error in main loop: {str(e)}")
                
            logger.info(f"Sleeping for {INTERVAL_SECONDS} seconds...")
            await asyncio.sleep(INTERVAL_SECONDS)
    except asyncio.CancelledError:
        logger.info("Main loop cancelled, shutting down...")

async def shutdown(signal=None):
    if signal:
        logger.info(f"Received exit signal {signal.name}...")
    
    tasks = [t for t in asyncio.all_tasks() if t is not asyncio.current_task()]
    [task.cancel() for task in tasks]
    
    logger.info(f"Cancelling {len(tasks)} outstanding tasks")
    await asyncio.gather(*tasks, return_exceptions=True)
    logger.info("Shutdown complete.")

if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        logger.info("Received KeyboardInterrupt, exiting...")
    except Exception as e:
        logger.error(f"Unexpected error: {str(e)}")
        raise
