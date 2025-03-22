FROM python:3.11-slim

WORKDIR /app

# Copy requirements and script
COPY requirements.txt .

# Install dependencies
RUN pip install --upgrade pip &&  pip install --no-cache-dir -r requirements.txt

# Use tini for proper signal handling
RUN apt-get update && \
    apt-get install -y tini && \
    rm -rf /var/lib/apt/lists/*

# Use tini as entrypoint
ENTRYPOINT ["/usr/bin/tini", "--"]

COPY influx_highchart_daemon.py .

# Run the script
CMD ["python", "influx_highchart_daemon.py"]
