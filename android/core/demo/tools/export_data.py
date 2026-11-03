#!/usr/bin/env python3
"""Package the mock hub's shared iOS export without changing any iOS files."""

import base64
import json
from pathlib import Path
import re

MODULE = Path(__file__).resolve().parents[1]
ROOT = MODULE.parents[2]
source = ROOT / "apple/Persona/Resources/Demo.json"
data = json.loads(source.read_text())

# Derive replacements from identity fields instead of duplicating fictional names in source.
names = set()
for messages in data["lark"].values():
    names.update(message["sender_name"] for message in messages if not message["is_mine"])
for message in data["mail"].values():
    if message["from"].get("name", "").isascii():
        names.add(message["from"]["name"])
names.update(person["name"] for person in data["kb"]["people"])
replacements = {name: "示例联系人" + str(index + 1) for index, name in enumerate(sorted(names))}
for person in data["kb"]["people"]:
    replacements[person["id"]] = "person-" + str(len(replacements))


def text(value):
    for name in sorted(replacements, key=len, reverse=True):
        value = value.replace(name, replacements[name])
    return re.sub(r"[\w.+-]+@[\w.-]+\.[A-Za-z]+", "contact@example.com", value)


def sanitize(value, key=""):
    if isinstance(value, dict):
        return {text(k): sanitize(v, k) for k, v in value.items()}
    if isinstance(value, list):
        return [sanitize(v, key) for v in value]
    if isinstance(value, str):
        if key in {"hostname", "ssh_host", "imap_host", "smtp_host"}:
            return "example.com"
        return text(value)
    return value


data = sanitize(data)
for frame in data["terminal_frames"]:
    frame["data"] = base64.b64encode(text(base64.b64decode(frame["data"]).decode()).encode()).decode()
target = MODULE / "src/main/assets/demo/Demo.json"
target.parent.mkdir(parents=True, exist_ok=True)
target.write_text(json.dumps(data, ensure_ascii=False, separators=(",", ":"), sort_keys=True) + "\n")
print("已更新 Android 体验数据")
