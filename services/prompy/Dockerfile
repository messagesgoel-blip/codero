FROM python:3.12-slim

LABEL maintainer="codero"
LABEL org.opencontainers.image.description="prompy — codero SLO observability sidecar"

WORKDIR /app

# Install dependencies first for layer caching.
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy service source.
COPY . .

# Non-root user for security.
RUN adduser --disabled-password --gecos "" prompy
USER prompy

EXPOSE 9210

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
  CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:9210/metrics')" || exit 1

ENTRYPOINT ["python", "-m", "prompy.main"]
