import os
import re
import sys
from functools import wraps

import requests
from flask import Flask, Response, render_template, request

CONFIG_TEMPLATE_PATH = "/app/config.template.yml"
STATE_PATH = "/state/detect_cameras.txt"
DOCKER_PROXY_URL = "http://docker-socket-proxy:2375"
FRIGATE_CONTAINER = "cctv-frigate"
FRIGATE_API_URL = "http://frigate:5000"

PANEL_USER = os.environ.get("PANEL_USER", "admin")
PANEL_PASS = os.environ.get("PANEL_PASS", "")

if not PANEL_PASS:
    print(
        "[panel] PANEL_PASS belum diisi di .env -- panel menolak start tanpa password.",
        file=sys.stderr,
    )
    sys.exit(1)

app = Flask(__name__)


def check_auth(username: str, password: str) -> bool:
    return username == PANEL_USER and password == PANEL_PASS


def requires_auth(f):
    @wraps(f)
    def decorated(*args, **kwargs):
        auth = request.authorization
        if not auth or not check_auth(auth.username, auth.password):
            return Response(
                "Login diperlukan",
                401,
                {"WWW-Authenticate": 'Basic realm="CCTV Panel"'},
            )
        return f(*args, **kwargs)

    return decorated


def discover_toggleable_cameras() -> list[str]:
    with open(CONFIG_TEMPLATE_PATH) as f:
        content = f.read()
    names = re.findall(
        r"^\s*enabled:\s*__DETECT_([A-Za-z0-9]+)__", content, flags=re.MULTILINE
    )
    return sorted({n.lower() for n in names})


def read_current_detect_cameras() -> set[str]:
    if os.path.exists(STATE_PATH):
        with open(STATE_PATH) as f:
            raw = f.read()
    else:
        raw = os.environ.get("DETECT_CAMERAS", "")
    return {c.strip().lower() for c in raw.split(",") if c.strip()}


def write_state(selected: set[str]) -> None:
    os.makedirs(os.path.dirname(STATE_PATH), exist_ok=True)
    with open(STATE_PATH, "w") as f:
        f.write(",".join(sorted(selected)))


def restart_frigate() -> None:
    resp = requests.post(
        f"{DOCKER_PROXY_URL}/containers/{FRIGATE_CONTAINER}/restart", timeout=30
    )
    resp.raise_for_status()


def frigate_live_status() -> dict:
    try:
        resp = requests.get(f"{FRIGATE_API_URL}/api/stats", timeout=5)
        resp.raise_for_status()
        cameras = resp.json().get("cameras", {})
        return {
            name: v.get("detection_enabled", False) for name, v in cameras.items()
        }
    except requests.RequestException:
        return {}


@app.route("/", methods=["GET", "POST"])
@requires_auth
def index():
    cameras = discover_toggleable_cameras()
    message = None
    error = None

    if request.method == "POST":
        selected = {c for c in cameras if request.form.get(c) == "on"}
        write_state(selected)
        try:
            restart_frigate()
            message = "Tersimpan. Frigate sedang restart, tunggu beberapa detik lalu refresh untuk lihat status terbaru."
        except requests.RequestException as e:
            error = f"Setting tersimpan, tapi restart otomatis gagal ({e}). Restart manual: docker compose up -d --force-recreate frigate"

    current = read_current_detect_cameras()
    live_status = frigate_live_status()

    return render_template(
        "index.html",
        cameras=cameras,
        current=current,
        live_status=live_status,
        message=message,
        error=error,
    )


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8000)
