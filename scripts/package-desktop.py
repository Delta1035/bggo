"""Collect MyGo packages; include runtime resources in the Windows portable ZIP."""
import json
from pathlib import Path
import shutil
import sys
import zipfile

platform = sys.argv[1]
if platform not in ("linux", "windows"):
    raise SystemExit("Expected linux or windows")
version = json.loads(Path("mygo.json").read_text(encoding="utf-8"))["version"]
stage = Path("build") / f"{platform}-amd64"
out = Path("release-assets")
out.mkdir(exist_ok=True)
patterns = ("*.deb", "*.tar.gz", "install.sh") if platform == "linux" else ("* Setup *.exe",)
packages = []
for pattern in patterns:
    matches = list(stage.glob(pattern))
    if len(matches) != 1:
        raise SystemExit(f"Expected exactly one {stage / pattern}, found {len(matches)}")
    packages.extend(matches)
for package in packages:
    # GitHub replaces spaces in release asset names with dots. Normalize before hashing.
    shutil.copy2(package, out / package.name.replace(" ", "."))
for update_file in stage.glob("update-*.json"):
    shutil.copy2(update_file, out / update_file.name)
for delta_file in stage.glob("*.delta"):
    shutil.copy2(delta_file, out / delta_file.name)
if platform == "windows":
    if not (stage / "BGGo.exe").is_file():
        raise SystemExit("Missing Windows executable")
    with zipfile.ZipFile(out / f"bggo-{version}-windows-amd64-portable.zip", "w", zipfile.ZIP_DEFLATED) as archive:
        for path in sorted(stage.rglob("*")):
            if path.is_file() and path not in packages:
                archive.write(path, path.relative_to(stage))
