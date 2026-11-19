# nvr-vision: person + face analysis for NVR Lite (Tahap E), run on a stronger machine
# than the NVR host (the Raspberry Pi). See server.py and docs/08-deployment.md.
FROM python:3.11-slim
RUN pip install --no-cache-dir onnxruntime==1.20.1 numpy==2.1.3 opencv-python-headless==4.10.0.84
# Models pinned by checksum: YOLOX-nano (Apache-2.0, Megvii), YuNet (MIT) and SFace
# (Apache-2.0) from the OpenCV Model Zoo.
ADD --checksum=sha256:c789161ed43c8269fcd4e67c67eeeb4e80c622da2eb296a20bc6007bd18a0b7d \
    https://github.com/Megvii-BaseDetection/YOLOX/releases/download/0.1.1rc0/yolox_nano.onnx /models/yolox_nano.onnx
ADD --checksum=sha256:8f2383e4dd3cfbb4553ea8718107fc0423210dc964f9f4280604804ed2552fa4 \
    https://github.com/opencv/opencv_zoo/raw/main/models/face_detection_yunet/face_detection_yunet_2023mar.onnx /models/face_detection_yunet_2023mar.onnx
ADD --checksum=sha256:0ba9fbfa01b5270c96627c4ef784da859931e02f04419c829e83484087c34e79 \
    https://github.com/opencv/opencv_zoo/raw/main/models/face_recognition_sface/face_recognition_sface_2021dec.onnx /models/face_recognition_sface_2021dec.onnx
RUN chmod 755 /models && chmod 644 /models/*.onnx
COPY server.py /app/server.py
RUN useradd -u 1001 -M vision
USER vision
ENV VISION_LISTEN=0.0.0.0:8093 VISION_THREADS=2 VISION_MODELS=/models
HEALTHCHECK --interval=60s --timeout=5s --start-period=30s \
    CMD python -c "import os,urllib.request as u;u.urlopen(u.Request('http://127.0.0.1:'+os.environ['VISION_LISTEN'].rsplit(':',1)[1]+'/healthz',headers={'Authorization':'Bearer '+os.environ['VISION_TOKEN']}),timeout=3)"
ENTRYPOINT ["python", "-u", "/app/server.py"]
