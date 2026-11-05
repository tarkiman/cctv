"""nvr-detect: a tiny HTTP wrapper around YOLOX-nano (ONNX Runtime, CPU).

POST /detect   body: JPEG bytes
               -> {"ms": 412.3, "width": 640, "height": 360,
                   "detections": [{"label": "person", "score": 0.62, "box": [x, y, w, h]}]}
               box is relative to the image (0..1). Only LABELS are reported, after NMS.
GET  /healthz  -> {"model": "yolox_nano.onnx", "threads": 1, "inferences": 12, "avg_ms": 560.1}

Inference is serialised (one at a time): the caller (nvrd) only sends frames while there is
motion, and a queue would just add latency on a small CPU.
"""
import io
import json
import os
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import numpy as np
import onnxruntime as ort
from PIL import Image

SIZE = 416
# COCO class ids of interest. Animals and vehicles are reported so the caller can reject a
# "person" box that is better explained by something else.
LABELS = {0: "person", 1: "bicycle", 2: "car", 3: "motorcycle", 15: "cat", 16: "dog"}
MIN_SCORE = float(os.environ.get("DETECT_MIN_SCORE", "0.25"))
MAX_BODY = 4 << 20

opts = ort.SessionOptions()
opts.intra_op_num_threads = int(os.environ.get("DETECT_THREADS", "1"))
opts.inter_op_num_threads = 1
opts.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
MODEL = os.environ.get("DETECT_MODEL", "/models/yolox_nano.onnx")
session = ort.InferenceSession(MODEL, opts, providers=["CPUExecutionProvider"])
input_name = session.get_inputs()[0].name
lock = threading.Lock()
stats = {"inferences": 0, "total_ms": 0.0}

# Anchor-free grid for a 416 input (strides 8, 16, 32).
_grids, _strides = [], []
for s in (8, 16, 32):
    n = SIZE // s
    yv, xv = np.meshgrid(np.arange(n), np.arange(n), indexing="ij")
    _grids.append(np.stack((xv, yv), 2).reshape(-1, 2))
    _strides.append(np.full((n * n, 1), s))
GRID, STRIDE = np.concatenate(_grids).astype(np.float32), np.concatenate(_strides).astype(np.float32)


def preprocess(img):
    w, h = img.size
    r = min(SIZE / w, SIZE / h)
    canvas = np.full((SIZE, SIZE, 3), 114, np.uint8)
    resized = np.asarray(img.resize((int(w * r), int(h * r)), Image.BILINEAR))
    canvas[: resized.shape[0], : resized.shape[1]] = resized
    return canvas[:, :, ::-1].transpose(2, 0, 1)[None].astype(np.float32), r  # BGR, NCHW, 0..255


def nms(boxes, scores, iou=0.45):
    order = scores.argsort()[::-1]
    keep = []
    while order.size:
        i = order[0]
        keep.append(i)
        xx1 = np.maximum(boxes[i, 0], boxes[order[1:], 0])
        yy1 = np.maximum(boxes[i, 1], boxes[order[1:], 1])
        xx2 = np.minimum(boxes[i, 2], boxes[order[1:], 2])
        yy2 = np.minimum(boxes[i, 3], boxes[order[1:], 3])
        inter = np.clip(xx2 - xx1, 0, None) * np.clip(yy2 - yy1, 0, None)
        area = lambda b: (b[..., 2] - b[..., 0]) * (b[..., 3] - b[..., 1])
        o = inter / (area(boxes[i]) + area(boxes[order[1:]]) - inter + 1e-9)
        order = order[1:][o < iou]
    return keep


def detect(jpeg):
    img = Image.open(io.BytesIO(jpeg)).convert("RGB")
    w, h = img.size
    x, r = preprocess(img)
    t = time.perf_counter()
    with lock:
        out = session.run(None, {input_name: x})[0][0]
    ms = (time.perf_counter() - t) * 1000
    stats["inferences"] += 1
    stats["total_ms"] += ms

    xy = (out[:, :2] + GRID) * STRIDE / r
    wh = np.exp(out[:, 2:4]) * STRIDE / r
    ids = np.array(list(LABELS))
    cls_scores = out[:, 5:][:, ids] * out[:, 4:5]  # objectness × class
    best = cls_scores.argmax(1)
    score = cls_scores[np.arange(len(best)), best]
    keep = score >= MIN_SCORE
    if not keep.any():
        return ms, w, h, []
    xy, wh, best, score = xy[keep], wh[keep], best[keep], score[keep]
    boxes = np.concatenate([xy - wh / 2, xy + wh / 2], 1)
    dets = []
    for i in nms(boxes, score):
        x1, y1, x2, y2 = np.clip(boxes[i], 0, [w, h, w, h])
        dets.append({
            "label": LABELS[int(ids[best[i]])],
            "score": round(float(score[i]), 3),
            "box": [round(float(x1 / w), 4), round(float(y1 / h), 4), round(float((x2 - x1) / w), 4), round(float((y2 - y1) / h), 4)],
        })
    return ms, w, h, dets


class Handler(BaseHTTPRequestHandler):
    def _json(self, code, body):
        data = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path != "/healthz":
            return self._json(404, {"error": "not found"})
        n = stats["inferences"]
        self._json(200, {"model": os.path.basename(MODEL), "threads": opts.intra_op_num_threads, "inferences": n,
                         "avg_ms": round(stats["total_ms"] / n, 1) if n else None})

    def do_POST(self):
        if self.path != "/detect":
            return self._json(404, {"error": "not found"})
        length = int(self.headers.get("Content-Length") or 0)
        if not 0 < length <= MAX_BODY:
            return self._json(400, {"error": "body must be a JPEG up to 4 MB"})
        try:
            ms, w, h, dets = detect(self.rfile.read(length))
        except Exception as e:  # bad image
            return self._json(400, {"error": str(e)})
        self._json(200, {"ms": round(ms, 1), "width": w, "height": h, "detections": dets})

    def log_message(self, *args):  # quiet; nvrd logs what matters
        pass


if __name__ == "__main__":
    host, port = os.environ.get("DETECT_LISTEN", "127.0.0.1:8092").rsplit(":", 1)
    print(json.dumps({"msg": "nvr-detect listening", "listen": f"{host}:{port}", "model": MODEL, "threads": opts.intra_op_num_threads}))
    ThreadingHTTPServer((host, int(port)), Handler).serve_forever()
