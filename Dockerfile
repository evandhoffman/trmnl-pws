FROM python:3.11-slim

WORKDIR /app

# Default path for persisted state lock file
ENV STATE_LOCK_PATH=/tmp/last_trmnl_update.lock

# Install tini for proper signal handling
RUN apt-get update && apt-get install -y tini && rm -rf /var/lib/apt/lists/*

# Copy and install dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy application code
COPY app/ ./app/

# Use tini as entrypoint for proper signal handling
ENTRYPOINT ["/usr/bin/tini", "--"]

# Run the application
CMD ["python", "-m", "app.main"]
