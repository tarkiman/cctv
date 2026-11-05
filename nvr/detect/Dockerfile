# nvr-detect: person detection for NVR Lite (fase 3). Receives a JPEG over HTTP and
# returns boxes for a few COCO classes. Runs only when the "detect" compose profile is on.
FROM python:3.11-slim
RUN pip install --no-cache-dir onnxruntime==1.20.1 numpy==2.1.3 pillow==11.0.0
# YOLOX-nano (Apache-2.0, Megvii), 416×416, pinned by checksum.
ADD --checksum=sha256:c789161ed43c8269fcd4e67c67eeeb4e80c622da2eb296a20bc6007bd18a0b7d \
    https://github.com/Megvii-BaseDetection/YOLOX/releases/download/0.1.1rc0/yolox_nano.onnx /models/yolox_nano.onnx
# ADD from a URL creates a root-only (600) file; the service runs as uid 1001.
RUN chmod 755 /models && chmod 644 /models/yolox_nano.onnx
COPY server.py /app/server.py
RUN useradd -u 1001 -M detect
USER detect
ENV DETECT_LISTEN=127.0.0.1:8092 DETECT_THREADS=1 DETECT_MODEL=/models/yolox_nano.onnx
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s \
    CMD python -c "import os,urllib.request;urllib.request.urlopen('http://'+os.environ['DETECT_LISTEN']+'/healthz',timeout=3)"
ENTRYPOINT ["python", "-u", "/app/server.py"]
