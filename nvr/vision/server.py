"""nvr-vision: person + face analysis for NVR Lite, meant for a stronger machine than the
NVR host (here the Raspberry Pi). Plan: nvr/docs/11-rencana-pengembangan.md, Tahap E.

nvrd calls POST /watch while there is motion in a camera's zones. The service then
reads that camera's sub-stream from go2rtc (RTSP, over the LAN), runs person detection
(YOLOX-nano) a few times per second and places each person in a zone:

  inside  - the person's feet (bottom centre of the box) are in an "inside" zone
  outside - the feet, or at least a quarter of the box, are in an "outside" zone
            (people right behind the gate often have their feet hidden by it)

Once someone is in a zone, the main stream is read too (kept open 15 s after the last
person; people are then detected on its frames, so boxes and faces line up), analysis
runs at 5 fps while someone is inside, and faces are found (YuNet) in
the upper part of each inside person's box at full resolution and turned into 128-d
embeddings (SFace). Results go back to nvrd (POST VISION_CALLBACK) about once per
processed frame that has people in zones. Nothing is stored here.

POST /watch   {"camera_id": 1, "sub": "bardi_sub", "main": "bardi",
               "zones": [{"kind": "inside", "points": [[x, y], ...]}], "seconds": 20}
              -> {"ok": true, "until": <unix>}       (starts or extends a session)
GET  /healthz -> {"sessions": [...], "person_ms": .., "face_ms": .., "reports": ..}

Both need "Authorization: Bearer <VISION_TOKEN>" (healthz too, it names cameras).
"""
import base64
import hmac
import json
import os
import threading
import time
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

os.environ.setdefault("OPENCV_FFMPEG_CAPTURE_OPTIONS", "rtsp_transport;tcp|stimeout;8000000")
import cv2  # noqa: E402
import numpy as np  # noqa: E402
import onnxruntime as ort  # noqa: E402

LISTEN = os.environ.get("VISION_LISTEN", "0.0.0.0:8093")
TOKEN = os.environ.get("VISION_TOKEN", "")
RTSP = os.environ.get("VISION_RTSP", "").rstrip("/")  # rtsp://user:pass@nvr-host:8556
CALLBACK = os.environ.get("VISION_CALLBACK", "")  # http://nvr-host:8091/api/v1/vision/report
THREADS = int(os.environ.get("VISION_THREADS", "2"))
SAMPLE_FPS = float(os.environ.get("VISION_FPS", "3"))
# Zones already limit where people count, so a lower score is safe; at night (spike of
# 2026-11-20) people behind the gate scored 0.25-0.45 with YOLOX-nano.
PERSON_MIN = float(os.environ.get("VISION_PERSON_MIN", "0.3"))
FACE_MIN = float(os.environ.get("VISION_FACE_MIN", "0.6"))  # spike S11: real faces scored 0.61–0.91
MODELS = os.environ.get("VISION_MODELS", "/models")
MAX_SECONDS = 60
HD_LINGER = 15  # seconds the main stream stays open after the last person in a zone
SUB_WARM = int(os.environ.get("VISION_SUB_WARM", "300"))  # seconds the sub-stream stays open
FAST_FPS = float(os.environ.get("VISION_FAST_FPS", "5"))  # analysis rate while someone is inside
MAX_BODY = 64 << 10

cv2.setNumThreads(1)

# --- person detection: YOLOX-nano, as in nvr/detect ----------------------------------
SIZE = 416
opts = ort.SessionOptions()
opts.intra_op_num_threads = THREADS
opts.inter_op_num_threads = 1
opts.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
yolox = ort.InferenceSession(os.path.join(MODELS, "yolox_nano.onnx"), opts, providers=["CPUExecutionProvider"])
yolox_in = yolox.get_inputs()[0].name
_g, _s = [], []
for s in (8, 16, 32):
    n = SIZE // s
    yv, xv = np.meshgrid(np.arange(n), np.arange(n), indexing="ij")
    _g.append(np.stack((xv, yv), 2).reshape(-1, 2))
    _s.append(np.full((n * n, 1), s))
GRID, STRIDE = np.concatenate(_g).astype(np.float32), np.concatenate(_s).astype(np.float32)


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
        area = lambda b: (b[..., 2] - b[..., 0]) * (b[..., 3] - b[..., 1])  # noqa: E731
        o = inter / (area(boxes[i]) + area(boxes[order[1:]]) - inter + 1e-9)
        order = order[1:][o < iou]
    return keep


def people(frame):
    """Person boxes [x, y, w, h] relative to the frame, with scores."""
    h, w = frame.shape[:2]
    r = min(SIZE / w, SIZE / h)
    canvas = np.full((SIZE, SIZE, 3), 114, np.uint8)
    canvas[: int(h * r), : int(w * r)] = cv2.resize(frame, (int(w * r), int(h * r)))
    out = yolox.run(None, {yolox_in: canvas.transpose(2, 0, 1)[None].astype(np.float32)})[0][0]
    xy = (out[:, :2] + GRID) * STRIDE / r
    wh = np.exp(out[:, 2:4]) * STRIDE / r
    score = out[:, 5] * out[:, 4]  # COCO class 0 = person
    keep = score >= PERSON_MIN
    if not keep.any():
        return []
    xy, wh, score = xy[keep], wh[keep], score[keep]
    boxes = np.concatenate([xy - wh / 2, xy + wh / 2], 1)
    res = []
    for i in nms(boxes, score):
        x1, y1, x2, y2 = np.clip(boxes[i], 0, [w, h, w, h])
        res.append(([float(x1 / w), float(y1 / h), float((x2 - x1) / w), float((y2 - y1) / h)], float(score[i])))
    return res


# --- faces: YuNet + SFace (OpenCV Zoo) -------------------------------------------------
yunet = cv2.FaceDetectorYN.create(os.path.join(MODELS, "face_detection_yunet_2023mar.onnx"), "", (320, 320), FACE_MIN, 0.3, 20)
sface = cv2.FaceRecognizerSF.create(os.path.join(MODELS, "face_recognition_sface_2021dec.onnx"), "")
infer = threading.Lock()  # one model run at a time; the Pi is shared


def faces_in_rect(frame, x1, y1, x2, y2, max_w=1280):
    """Faces in a pixel rectangle of a full-res frame. Large rectangles are scaled down
    to max_w for detection; alignment and the embedding use the full-res pixels."""
    H, W = frame.shape[:2]
    x1, y1, x2, y2 = max(0, int(x1)), max(0, int(y1)), min(W, int(x2)), min(H, int(y2))
    crop = frame[y1:y2, x1:x2]
    if crop.shape[0] < 24 or crop.shape[1] < 24:
        return []
    k = min(1.0, max_w / crop.shape[1])
    det = cv2.resize(crop, (int(crop.shape[1] * k), int(crop.shape[0] * k))) if k < 1 else crop
    yunet.setInputSize((det.shape[1], det.shape[0]))
    _, found = yunet.detect(det)
    out = []
    for f in [] if found is None else found:
        f = f.copy()
        f[:14] /= k  # box + 5 landmarks back to full-res crop coordinates
        fw, fh = float(f[2]), float(f[3])
        if fh < 40:  # too small to recognise (see spike S11)
            continue
        emb = sface.feature(sface.alignCrop(crop, f)).flatten()
        emb = emb / (np.linalg.norm(emb) + 1e-9)
        # A viewable crop: the face with some margin, at most 256 px.
        mx, my = fw * 0.35, fh * 0.35
        fx1, fy1 = int(max(0, f[0] - mx)), int(max(0, f[1] - my))
        fx2, fy2 = int(min(crop.shape[1], f[0] + fw + mx)), int(min(crop.shape[0], f[1] + fh + my))
        face = crop[fy1:fy2, fx1:fx2]
        if max(face.shape[:2]) > 256:
            r = 256 / max(face.shape[:2])
            face = cv2.resize(face, (int(face.shape[1] * r), int(face.shape[0] * r)))
        jpeg = cv2.imencode(".jpg", face, [cv2.IMWRITE_JPEG_QUALITY, 88])[1].tobytes()
        out.append({
            "box": [round(float((x1 + f[0]) / W), 4), round(float((y1 + f[1]) / H), 4), round(fw / W, 4), round(fh / H, 4)],
            "score": round(float(f[-1]), 3),
            "size": int(fh),
            "embedding": [round(float(v), 5) for v in emb],
            "jpeg": base64.b64encode(jpeg).decode(),
        })
    return out


def faces_in(frame, box):
    """Faces in the upper part of a person box (relative coords) of a full-res frame."""
    H, W = frame.shape[:2]
    x, y, w, h = box
    return faces_in_rect(frame, (x - w * 0.15) * W, y * H, (x + w * 1.15) * W, (y + h * 0.5) * H)


def inside_region(zones):
    """Where faces of people inside the fence can be: the inside zones' bounding box,
    extended upwards by a third of the picture (heads are above the feet). None when
    the camera has no inside zone."""
    pts = [p for z in zones if z["kind"] == "inside" for p in z["points"]]
    if not pts:
        return None
    xs, ys = [p[0] for p in pts], [p[1] for p in pts]
    return max(0.0, min(xs)), max(0.0, min(ys) - 0.33), min(1.0, max(xs)), min(1.0, max(ys))


# --- zones ----------------------------------------------------------------------------
def contains(points, px, py):
    inside, n = False, len(points)
    j = n - 1
    for i in range(n):
        (xi, yi), (xj, yj) = points[i], points[j]
        if (yi > py) != (yj > py) and px < (xj - xi) * (py - yi) / (yj - yi) + xi:
            inside = not inside
        j = i
    return inside


def zone_of(zones, box):
    x, y, w, h = box
    fx, fy = x + w / 2, y + h  # feet
    for z in zones:
        if z["kind"] == "inside" and contains(z["points"], fx, fy):
            return "inside"
    for z in zones:
        if z["kind"] != "outside":
            continue
        if contains(z["points"], fx, fy):
            return "outside"
        hits = sum(contains(z["points"], x + w * (i + 0.5) / 5, y + h * (j + 0.5) / 5) for i in range(5) for j in range(5))
        if hits >= 6:  # ≥ about a quarter of the box
            return "outside"
    return None


# --- streams --------------------------------------------------------------------------
class Reader(threading.Thread):
    """Reads an RTSP stream continuously and keeps only the latest frame, so analysis
    never lags behind live. Readers are shared and kept open for a while after their
    last use (see reader()): opening a stream costs seconds (until its next keyframe),
    which is exactly when someone walks up to the camera."""

    def __init__(self, name, linger):
        super().__init__(daemon=True)
        self.name, self.frame, self.at, self.stop = name, None, 0.0, threading.Event()
        self.error = ""
        self.used, self.linger = time.time(), linger

    def run(self):
        while not self.stop.is_set():
            cap = cv2.VideoCapture(f"{RTSP}/{self.name}", cv2.CAP_FFMPEG)
            if not cap.isOpened():
                self.error = "cannot open stream"
                self.stop.wait(3)
                continue
            while not self.stop.is_set():
                ok, frame = cap.read()
                if not ok:
                    self.error = "stream ended"
                    break
                self.frame, self.at, self.error = frame, time.time(), ""
            cap.release()

    def latest(self, max_age=2.0):
        return self.frame if time.time() - self.at <= max_age else None


readers, readers_lock = {}, threading.Lock()


def reader(name, linger):
    """The shared reader of a stream, started if needed and marked as used now."""
    with readers_lock:
        r = readers.get(name)
        if r is None or r.stop.is_set() or not r.is_alive():
            r = Reader(name, linger)
            readers[name] = r
            r.start()
        r.used = time.time()
        r.linger = linger
        return r


def reap():
    while True:
        time.sleep(5)
        with readers_lock:
            for name, r in list(readers.items()):
                if time.time() - r.used > r.linger:
                    r.stop.set()
                    del readers[name]


class Session(threading.Thread):
    def __init__(self, req):
        super().__init__(daemon=True)
        self.camera_id = req["camera_id"]
        self.update(req)
        # Keep the main stream open until then. Cameras with an inside zone open it right
        # away: by the time a person is detected, the best face moments may be over.
        self.hd_until = time.time() + HD_LINGER if inside_region(self.zones) else 0.0
        self.last_inside = 0.0

    def update(self, req):
        self.zones = [{"kind": z["kind"], "points": [tuple(p) for p in z["points"]]} for z in req.get("zones", [])]
        self.until = max(getattr(self, "until", 0), time.time() + min(MAX_SECONDS, float(req.get("seconds", 20))))
        self.sub_name, self.main_name = req["sub"], req["main"]

    def hd(self):
        return reader(self.main_name, HD_LINGER) if time.time() < self.hd_until else None

    def run(self):
        try:
            while time.time() < self.until:
                t0 = time.time()
                # The sub-stream stays warm for SUB_WARM after the session.
                frame = reader(self.sub_name, SUB_WARM).latest()
                if frame is not None:
                    try:
                        self.analyse(frame)
                    except Exception as e:  # keep the session alive; report what broke
                        stats["last_error"] = f"analyse: {e!r}"[:200]
                        print(f"camera {self.camera_id}: analyse failed: {e!r}", flush=True)
                fast = time.time() - self.last_inside < 3
                time.sleep(max(0.0, 1 / (FAST_FPS if fast else SAMPLE_FPS) - (time.time() - t0)))
        finally:
            with sessions_lock:
                if sessions.get(self.camera_id) is self:
                    del sessions[self.camera_id]

    def analyse(self, sub_frame):
        # While the HD stream is open, detect people on the HD frame itself (scaled
        # down): sub and main stream decode with different delays, and boxes from one
        # don't line up with faces in the other when the person moves.
        main = self.hd()
        hd = main.latest() if main else None
        frame = cv2.resize(hd, (640, 360)) if hd is not None else sub_frame
        t = time.perf_counter()
        with infer:
            found = people(frame)
        stats["person_ms"].append((time.perf_counter() - t) * 1000)
        persons = []
        for box, score in found:
            z = zone_of(self.zones, box)
            if z:
                persons.append({"zone": z, "box": [round(v, 4) for v in box], "score": round(score, 3)})
        if persons:
            # Someone in any zone: open (or keep) the HD stream, so it is ready by the
            # time a person coming from the street is inside and facing the camera.
            self.hd_until = time.time() + HD_LINGER
            if main is None:
                self.hd()
        faces = []
        now = time.time()
        if any(p["zone"] == "inside" for p in persons):
            self.last_inside = now
        region = inside_region(self.zones)
        if hd is not None and region is not None:
            # Search the whole area where faces of people inside can be, not only the
            # person boxes: at night detection flickers and walking blurs the body.
            H, W = hd.shape[:2]
            t = time.perf_counter()
            with infer:
                found = faces_in_rect(hd, region[0] * W, region[1] * H, region[2] * W, region[3] * H)
            stats["face_ms"].append((time.perf_counter() - t) * 1000)
            for f in found:
                fx, fy, fw, fh = f["box"]
                cx, cy = fx + fw / 2, fy + fh / 2
                owner = [p["zone"] for p in persons if p["box"][0] <= cx <= p["box"][0] + p["box"][2] and
                         p["box"][1] <= cy <= p["box"][1] + p["box"][3]]
                # From this camera's angle a head outside the gate can look like it is
                # above the inside zone, so the face's own position decides nothing: it
                # counts when it belongs to someone inside, or (no box: detection
                # flickers) when someone was inside a moment ago, never when it belongs
                # to someone outside.
                if "inside" in owner or (not owner and now - self.last_inside < 10):
                    f["zone"] = "inside"
                    faces.append(f)
            if faces:  # a face inside counts as someone inside
                self.last_inside = now
                self.hd_until = now + HD_LINGER
        if persons or faces:
            report({"camera_id": self.camera_id, "at": round(time.time(), 3), "persons": persons, "faces": faces})


def report(body):
    if not CALLBACK:
        return
    data = json.dumps(body).encode()
    req = urllib.request.Request(CALLBACK, data, {"Content-Type": "application/json", "Authorization": "Bearer " + TOKEN,
                                                 "X-Requested-With": "nvr"})
    try:
        urllib.request.urlopen(req, timeout=5).read()
        stats["reports"] += 1
    except Exception as e:  # nvrd restarting: drop this report
        stats["report_errors"] += 1
        stats["last_error"] = str(e)[:200]


sessions, sessions_lock = {}, threading.Lock()
stats = {"person_ms": [], "face_ms": [], "reports": 0, "report_errors": 0, "last_error": ""}


def avg(v):
    v = v[-200:]
    return round(sum(v) / len(v), 1) if v else None


class Handler(BaseHTTPRequestHandler):
    def _json(self, code, body):
        data = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def _authorized(self):
        got = self.headers.get("Authorization", "")
        return bool(TOKEN) and hmac.compare_digest(got.encode(), ("Bearer " + TOKEN).encode())

    def do_GET(self):
        if self.path != "/healthz":
            return self._json(404, {"error": "not found"})
        if not self._authorized():
            return self._json(401, {"error": "unauthorized"})
        with sessions_lock:
            active = [{"camera_id": s.camera_id, "seconds_left": round(s.until - time.time()), "hd": time.time() < s.hd_until}
                      for s in sessions.values()]
        with readers_lock:
            open_streams = {n: {"age": round(time.time() - r.at, 1) if r.at else None, "error": r.error} for n, r in readers.items()}
        self._json(200, {"sessions": active, "streams": open_streams, "person_ms": avg(stats["person_ms"]), "face_ms": avg(stats["face_ms"]),
                         "reports": stats["reports"], "report_errors": stats["report_errors"], "last_error": stats["last_error"]})

    def do_POST(self):
        if self.path != "/watch":
            return self._json(404, {"error": "not found"})
        if not self._authorized():
            return self._json(401, {"error": "unauthorized"})
        length = int(self.headers.get("Content-Length") or 0)
        if not 0 < length <= MAX_BODY:
            return self._json(400, {"error": "body too large"})
        try:
            req = json.loads(self.rfile.read(length))
            cid = int(req["camera_id"])
            for k in ("sub", "main"):
                if not isinstance(req.get(k), str) or not req[k] or "/" in req[k]:
                    raise ValueError(k)
        except (ValueError, KeyError, TypeError) as e:
            return self._json(400, {"error": f"bad request: {e}"})
        with sessions_lock:
            s = sessions.get(cid)
            if s and s.is_alive():
                s.update(req)
            else:
                s = Session(req)
                sessions[cid] = s
                s.start()
        self._json(200, {"ok": True, "until": round(s.until)})

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    if not TOKEN or not RTSP:
        raise SystemExit("VISION_TOKEN and VISION_RTSP are required")
    threading.Thread(target=reap, daemon=True).start()
    host, port = LISTEN.rsplit(":", 1)
    print(f"nvr-vision listening on {LISTEN}, {THREADS} threads, {SAMPLE_FPS} fps", flush=True)
    ThreadingHTTPServer((host, int(port)), Handler).serve_forever()
