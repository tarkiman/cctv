import datetime
import os
import pathlib
import time

import requests

FRIGATE_URL = os.environ.get("FRIGATE_URL", "http://frigate:5000")
INTERVAL_SECONDS = int(os.environ.get("TIMELAPSE_INTERVAL_SECONDS", "60"))
STORAGE_ROOT = pathlib.Path(os.environ.get("TIMELAPSE_STORAGE", "/storage/timelapse"))
CAMERAS = [c.strip() for c in os.environ.get("TIMELAPSE_CAMERAS", "").split(",") if c.strip()]


def capture_once() -> None:
    now = datetime.datetime.now()
    date_dir = now.strftime("%Y-%m-%d")
    time_name = now.strftime("%H%M%S")
    for camera in CAMERAS:
        url = f"{FRIGATE_URL}/api/{camera}/latest.jpg"
        try:
            resp = requests.get(url, timeout=10)
            resp.raise_for_status()
        except requests.RequestException as exc:
            print(f"[timelapse] gagal ambil snapshot {camera}: {exc}", flush=True)
            continue
        out_dir = STORAGE_ROOT / camera / date_dir
        out_dir.mkdir(parents=True, exist_ok=True)
        (out_dir / f"{time_name}.jpg").write_bytes(resp.content)
        print(f"[timelapse] {camera} -> {out_dir / f'{time_name}.jpg'}", flush=True)


def main() -> None:
    if not CAMERAS:
        raise SystemExit(
            "TIMELAPSE_CAMERAS kosong - isi nama kamera (dipisah koma) sesuai key "
            "kamera di frigate/config.yml, mis. TIMELAPSE_CAMERAS=teras,garasi"
        )
    while True:
        capture_once()
        time.sleep(INTERVAL_SECONDS)


if __name__ == "__main__":
    main()
