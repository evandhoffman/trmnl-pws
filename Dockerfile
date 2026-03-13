# Builder stage: install dependencies into a virtual environment
FROM cgr.dev/chainguard/python:latest-dev AS builder

WORKDIR /app

COPY requirements.txt .
RUN python -m venv /app/venv && \
    /app/venv/bin/pip install --no-cache-dir -r requirements.txt

# Runtime stage: minimal Chainguard image with no dev tools
FROM cgr.dev/chainguard/python:latest

WORKDIR /app

# Default path for persisted state lock file
ENV STATE_LOCK_PATH=/tmp/last_trmnl_update.lock
ENV PATH="/app/venv/bin:$PATH"

# Copy installed venv from builder (owned by root, world-readable — fine for any runtime UID)
COPY --from=builder /app/venv /app/venv

# Copy application code
COPY app/ ./app/

CMD ["python", "-m", "app.main"]
