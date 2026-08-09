FROM python:3.12-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg cron \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY capture.py compile_timelapse.py entrypoint.sh ./
RUN pip install --no-cache-dir requests && chmod +x entrypoint.sh

ENTRYPOINT ["./entrypoint.sh"]
