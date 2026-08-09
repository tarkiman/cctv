import datetime
import os
import pathlib
import shutil
import subprocess

STORAGE_ROOT = pathlib.Path(os.environ.get("TIMELAPSE_STORAGE", "/storage/timelapse"))
CAMERAS = [c.strip() for c in os.environ.get("TIMELAPSE_CAMERAS", "").split(",") if c.strip()]
FRAMERATE = os.environ.get("TIMELAPSE_FRAMERATE", "24")
KEEP_RAW = os.environ.get("TIMELAPSE_KEEP_RAW", "false").lower() == "true"


def compile_day(camera: str, date_str: str) -> None:
    src_dir = STORAGE_ROOT / camera / date_str
    if not src_dir.is_dir() or not any(src_dir.glob("*.jpg")):
        return

    out_dir = STORAGE_ROOT / camera / "videos"
    out_dir.mkdir(parents=True, exist_ok=True)
    out_path = out_dir / f"{date_str}.mp4"

    cmd = [
        "ffmpeg", "-y",
        "-framerate", FRAMERATE,
        "-pattern_type", "glob",
        "-i", str(src_dir / "*.jpg"),
        "-c:v", "libx264",
        "-pix_fmt", "yuv420p",
        str(out_path),
    ]
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode != 0:
        print(f"[timelapse] gagal compile {camera} {date_str}: {result.stderr}", flush=True)
        return

    print(f"[timelapse] compiled {out_path}", flush=True)
    if not KEEP_RAW:
        shutil.rmtree(src_dir)


def main() -> None:
    yesterday = (datetime.date.today() - datetime.timedelta(days=1)).isoformat()
    for camera in CAMERAS:
        compile_day(camera, yesterday)


if __name__ == "__main__":
    main()
