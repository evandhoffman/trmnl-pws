FROM python:3.11-slim

WORKDIR /app

# Copy requirements and script
COPY requirements.txt .
COPY ambient_claude_trmnl.py .

# Install dependencies
RUN pip install --no-cache-dir -r requirements.txt

# Run the script
CMD ["python", "ambient_claude_trmnl.py"]
