import os
import re

TEMPLATE_PATH = "/render/config.template.yml"
OUTPUT_PATH = "/config/config.yml"

detect_cameras = {
    name.strip().lower()
    for name in os.environ.get("DETECT_CAMERAS", "").split(",")
    if name.strip()
}

with open(TEMPLATE_PATH) as f:
    content = f.read()


def replace(match: re.Match) -> str:
    camera_name = match.group(2).lower()
    value = "true" if camera_name in detect_cameras else "false"
    return f"{match.group(1)}{value}"


# Hanya ganti token yang benar-benar di posisi "enabled: __DETECT_<NAMA>__" di baris
# BUKAN komentar (^ tanpa # di depannya) -- menghindari token yang kebetulan disebut
# di komentar/prosa (mis. contoh cara pakai) ikut ke-substitusi.
rendered, count = re.subn(
    r"^(\s*enabled:\s*)__DETECT_([A-Za-z0-9]+)__", replace, content, flags=re.MULTILINE
)

with open(OUTPUT_PATH, "w") as f:
    f.write(rendered)

print(f"[render_config] DETECT_CAMERAS={sorted(detect_cameras)}")
print(f"[render_config] {count} token(s) diganti, ditulis ke {OUTPUT_PATH}")
