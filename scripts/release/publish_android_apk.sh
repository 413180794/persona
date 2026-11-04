#!/bin/bash
# Publish an already signed APK using the hub SSH target from the build config.
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
if [ "$#" -eq 0 ]; then
  echo "用法：$0 <apk> [--version-code <整数>] [--version-name <版本>] [--min-sdk <整数>] [--notes <说明>]" >&2
  exit 2
fi
CONFIG_ENV=$(python3 "$SCRIPT_DIR/../persona_config.py" env) || {
  echo "无法读取发布配置，请先配置 hub.machine 和对应机器的 SSH 地址。" >&2
  exit 1
}
eval "$CONFIG_ENV"
if [ -z "${JARVIS_HUB_SSH:-}" ]; then
  echo "未配置 hub SSH 地址，请先配置 hub.machine 和对应机器的 SSH 地址。" >&2
  exit 1
fi

STAGING=$(mktemp -d "${TMPDIR:-/tmp}/jarvis-apk.XXXXXXXX")
trap 'rm -rf "$STAGING"' EXIT
python3 - "$STAGING" "$@" <<'PY'
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

staging = Path(sys.argv[1])
parser = argparse.ArgumentParser(description="发布已签名的 Android APK")
parser.add_argument("apk", type=Path)
parser.add_argument("--version-code", type=int)
parser.add_argument("--version-name")
parser.add_argument("--min-sdk", type=int)
parser.add_argument("--notes", default="")
args = parser.parse_args(sys.argv[2:])
if not args.apk.is_file():
    parser.error("APK 文件不存在")
apk = staging / "app.apk"
shutil.copyfile(args.apk, apk)

def tool(name):
    found = shutil.which(name)
    if found:
        return found
    sdk = os.environ.get("ANDROID_HOME") or os.environ.get("ANDROID_SDK_ROOT")
    if sdk:
        pattern = "build-tools/*/aapt2" if name == "aapt2" else "cmdline-tools/*/bin/apkanalyzer"
        candidates = sorted(Path(sdk).glob(pattern), reverse=True)
        return next((str(p) for p in candidates if os.access(p, os.X_OK)), None)

def run(*command):
    return subprocess.check_output(command, text=True, stderr=subprocess.DEVNULL).strip()

detected = {}
if args.version_code is None or args.version_name is None or args.min_sdk is None:
    aapt = tool("aapt2")
    if aapt:
        try:
            badging = run(aapt, "dump", "badging", str(apk))
            for key, pattern in {
                "version_code": r"\bversionCode='([^']+)'",
                "version_name": r"\bversionName='([^']*)'",
                "min_sdk": r"^sdkVersion:'([^']+)'",
            }.items():
                match = re.search(pattern, badging, re.MULTILINE)
                if match:
                    detected[key] = match.group(1)
        except subprocess.CalledProcessError:
            pass
    analyzer = tool("apkanalyzer")
    if analyzer:
        for key, command in {"version_code": "version-code", "version_name": "version-name", "min_sdk": "min-sdk"}.items():
            if key not in detected and getattr(args, key) is None:
                try:
                    detected[key] = run(analyzer, "manifest", command, str(apk))
                except subprocess.CalledProcessError:
                    pass

release = {"notes": args.notes}
for key in ("version_code", "version_name", "min_sdk"):
    value = getattr(args, key)
    if value is None:
        value = detected.get(key)
    if value is None or value == "":
        parser.error("无法读取 APK 元数据，请安装 aapt2 / apkanalyzer 或传入 --" + key.replace("_", "-"))
    if key != "version_name":
        try:
            value = int(value)
        except ValueError:
            parser.error(key + " 必须是整数")
        if value <= 0 or (key == "version_code" and value > 2100000000):
            parser.error(key + " 超出有效范围")
    release[key] = value
digest = hashlib.sha256()
with apk.open("rb") as source:
    for chunk in iter(lambda: source.read(1024 * 1024), b""):
        digest.update(chunk)
release.update(sha256=digest.hexdigest(), size=apk.stat().st_size)
apk.rename(staging / (release["sha256"] + ".apk"))
(staging / "latest.json").write_text(json.dumps(release, ensure_ascii=False, indent=2) + "\n")
PY

# Stage each upload separately, then expose the complete release by renaming its manifest.
REMOTE_STAGE=".upload-$(basename "$STAGING")"
RESOLVER=$(python3 "$SCRIPT_DIR/../persona_identity.py" --service hub --prepare --prefix -)
# No macOS extended attributes in the archive (see upload_web in scripts/deploy/deploy.sh).
COPYFILE_DISABLE=1 tar --no-xattrs -C "$STAGING" -czf - . | ssh -- "$JARVIS_HUB_SSH" "$RESOLVER
set -e; stage=\"\$PERSONA_HOME/apps/android/$REMOTE_STAGE\"; umask 077; mkdir -p \"\$stage\"; tar -xzf - -C \"\$stage\"; cd \"\$PERSONA_HOME/apps/android\"; mv \"\$stage\"/*.apk .; mv \"\$stage/latest.json\" latest.json; rmdir \"\$stage\""
echo "Android 安装包已发布。"
