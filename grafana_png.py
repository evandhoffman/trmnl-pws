#!/usr/bin/env python3
"""
Grafana to E-ink Display Image Converter
Fetches Grafana dashboard panels as PNG and converts them for 1-bit e-ink displays
"""

import os
import time
import argparse
import requests
import urllib3
import logging
from PIL import Image, ImageOps, ImageEnhance
from io import BytesIO
from datetime import datetime

# Disable SSL warnings
urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s [%(levelname)s] %(message)s',
    datefmt='%Y-%m-%dT%H:%M:%S%z'
)
logger = logging.getLogger("grafana-eink")

def get_grafana_image(base_url, dashboard_uid, panel_id, api_key=None, 
                     width=800, height=480, from_time='now-24h', to_time='now'):
    """
    Fetches an image from Grafana's render API
    
    Parameters:
    base_url (str): Base Grafana URL (e.g., 'https://home.evanhoffman.com/grafana')
    dashboard_uid (str): Dashboard UID or name
    panel_id (int): ID of the panel to render
    api_key (str, optional): Grafana API key for authentication
    width (int): Width of the rendered image
    height (int): Height of the rendered image
    from_time (str): Start time (Grafana time format)
    to_time (str): End time (Grafana time format)
    
    Returns:
    PIL.Image: The fetched image
    """
    # Construct the render URL
    render_url = f"{base_url}/render/d-solo/{dashboard_uid}?panelId={panel_id}&width={width}&height={height}&from={from_time}&to={to_time}&theme=light"
    
    headers = {}
    if api_key:
        headers['Authorization'] = f"Bearer {api_key}"
    
    logger.info(f"Fetching image from Grafana: {render_url}")
    response = requests.get(render_url, headers=headers, verify=False)
    
    if response.status_code != 200:
        logger.error(f"Failed to fetch image: {response.status_code} - {response.text}")
        raise Exception(f"Failed to fetch image: {response.status_code} - {response.text}")
    
    logger.info(f"Successfully fetched image: {len(response.content)} bytes")
    
    # Create a PIL Image from the response content
    return Image.open(BytesIO(response.content))

def process_for_eink(image, threshold=128, dither=True):
    """
    Process image for e-ink display
    
    Parameters:
    image (PIL.Image): The input image
    threshold (int): Threshold for converting to black and white (0-255)
    dither (bool): Whether to apply dithering
    
    Returns:
    PIL.Image: Processed 1-bit image
    """
    # Convert to grayscale
    gray_image = ImageOps.grayscale(image)
    
    # Enhance contrast to make lines more distinct
    enhancer = ImageEnhance.Contrast(gray_image)
    gray_image = enhancer.enhance(2.0)  # Increase contrast
    
    # Convert to 1-bit with optional dithering
    if dither:
        # Floyd-Steinberg dithering often works well for charts
        bw_image = gray_image.convert('1', dither=Image.FLOYDSTEINBERG)
    else:
        # Simple threshold
        bw_image = gray_image.point(lambda x: 255 if x > threshold else 0, '1')
    
    return bw_image

def main():
    parser = argparse.ArgumentParser(description='Fetch and process Grafana charts for e-ink displays')
    parser.add_argument('--url', required=True, help='Base Grafana URL')
    parser.add_argument('--dashboard', required=True, help='Dashboard UID or name')
    parser.add_argument('--panel', required=True, type=int, help='Panel ID')
    parser.add_argument('--api-key', help='Grafana API key (if authentication is required)')
    parser.add_argument('--width', type=int, default=800, help='Width of rendered image')
    parser.add_argument('--height', type=int, default=480, help='Height of rendered image')
    parser.add_argument('--from-time', default='now-24h', help='Start time in Grafana format')
    parser.add_argument('--to-time', default='now', help='End time in Grafana format')
    parser.add_argument('--output', default='eink_display.png', help='Output filename')
    parser.add_argument('--no-dither', action='store_true', help='Disable dithering')
    parser.add_argument('--threshold', type=int, default=128, help='Threshold for black/white conversion (0-255)')
    parser.add_argument('--verify-ssl', action='store_true', help='Verify SSL certificates (default: disabled)')
    parser.add_argument('--log-level', choices=['DEBUG', 'INFO', 'WARNING', 'ERROR', 'CRITICAL'], 
                      default='INFO', help='Set logging level')
    
    args = parser.parse_args()
    
    # Set logging level from command line argument
    logging.getLogger().setLevel(getattr(logging, args.log_level))
    
    # Remove trailing slash from URL if present
    base_url = args.url.rstrip('/')
    
    logger.info(f"Starting Grafana to E-ink converter")
    logger.info(f"Target Grafana instance: {base_url}")
    logger.info(f"Dashboard: {args.dashboard}, Panel: {args.panel}")
    logger.info(f"Time range: {args.from_time} to {args.to_time}")
    
    try:
        # Override the global SSL verification setting if requested
        if args.verify_ssl:
            # Re-enable warnings if verification is enabled
            urllib3.disable_warnings()
            
        # Fetch the image from Grafana
        original_image = get_grafana_image(
            base_url=base_url,
            dashboard_uid=args.dashboard,
            panel_id=args.panel,
            api_key=args.api_key,
            width=args.width,
            height=args.height,
            from_time=args.from_time,
            to_time=args.to_time
        )
        
        # Process the image for e-ink display
        eink_image = process_for_eink(
            image=original_image,
            threshold=args.threshold,
            dither=not args.no_dither
        )
        
        # Save the processed image
        eink_image.save(args.output)
        logger.info(f"Processed image saved to {args.output}")
        
        # Optionally save original for comparison
        original_output = f"original_{args.output}"
        original_image.save(original_output)
        logger.info(f"Original image saved to {original_output} for comparison")
        
    except Exception as e:
        logger.error(f"Error: {e}", exc_info=True)
        return 1
    
    logger.info("Script completed successfully")
    return 0

if __name__ == "__main__":
    exit(main())
