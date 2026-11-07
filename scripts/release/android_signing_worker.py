#!/usr/bin/env python3
"""Poll one APK signing request; private signing material stays on this machine."""
import argparse
import fcntl
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import tempfile
import uuid

from android_apk_atomic import digest_file
from publish_android_apk import parse_signer_certificate, tool

HEX_SHA256 = r"[0-9a-f]{64}"


def execute(command, *, environment=None):
    # Never surface output/errors from password commands or signing programs.
    try:
        result = subprocess.run(command, env=environment, capture_output=True, text=True, timeout=180)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise ValueError("signing worker subprocess could not complete") from error
    if result.returncode:
        raise ValueError("signing worker subprocess failed")
    return result.stdout


def validate_config(config):
    for key in ("ssh_target", "remote_queue_dir", "keystore", "alias", "expected_cert_sha256",
                "expected_package", "lock_file"):
        if not isinstance(config.get(key), str) or not config[key]:
            raise ValueError(f"missing configuration field: {key}")
    if not re.fullmatch(r"[A-Za-z0-9_.@-]+", config["ssh_target"]) or config["ssh_target"].startswith("-"):
        raise ValueError("invalid SSH target")
    queue = config["remote_queue_dir"]
    if not re.fullmatch(r"/[A-Za-z0-9_./-]+", queue) or ".." in Path(queue).parts:
        raise ValueError("queue must be an absolute path without shell metacharacters or spaces")
    if not re.fullmatch(HEX_SHA256, config["expected_cert_sha256"]):
        raise ValueError("expected_cert_sha256 must be lowercase SHA-256 hex")
    for key in ("password_command", "key_password_command"):
        command = config.get(key)
        if key == "key_password_command" and command is None:
            continue
        if not isinstance(command, list) or not command or any(not isinstance(arg, str) for arg in command):
            raise ValueError(f"{key} must be a nonempty argv array")


def validate_request(request, config):
    if not re.fullmatch(r"[0-9a-f]{40}", request.get("source_sha", "")):
        raise ValueError("request source_sha must be a full lowercase Git SHA")
    if not re.fullmatch(HEX_SHA256, request.get("unsigned_sha256", "")):
        raise ValueError("invalid unsigned_sha256")
    if request.get("package") != config["expected_package"]:
        raise ValueError("request package differs from configured package")
    version = request.get("version_code")
    if type(version) is not int or not 0 < version <= 2100000000:
        raise ValueError("invalid request version_code")


def sdk_tool(config, name):
    if config.get("sdk_dir"):
        candidates = sorted(Path(config["sdk_dir"]).expanduser().glob(f"build-tools/*/{name}"), reverse=True)
        for candidate in candidates:
            if os.access(candidate, os.X_OK):
                return str(candidate)
        raise ValueError(f"SDK tool missing: {name}")
    return tool(name)


def metadata(apk, config, request):
    badging = execute([sdk_tool(config, "aapt2"), "dump", "badging", str(apk)])
    package = re.search(r"^package: name='([^']+)' versionCode='(\d+)'", badging, re.M)
    if not package or package[1] != request["package"] or int(package[2]) != request["version_code"]:
        raise ValueError("APK package/version differs from signing request")


def sign(apk, output, config, request):
    if digest_file(apk) != request["unsigned_sha256"]:
        raise ValueError("unsigned APK hash differs from signing request")
    metadata(apk, config, request)
    aligned = output.parent / "aligned.apk"
    execute([sdk_tool(config, "zipalign"), "-P", "16", "-f", "4", str(apk), str(aligned)])
    password = execute(config["password_command"]).rstrip("\r\n")
    key_password = (execute(config["key_password_command"]).rstrip("\r\n")
                    if config.get("key_password_command") else password)
    if not password or not key_password or "\n" in password or "\n" in key_password:
        raise ValueError("password command returned empty or multiline data")
    environment = dict(os.environ, PERSONA_APK_STORE_PASSWORD=password, PERSONA_APK_KEY_PASSWORD=key_password)
    try:
        execute([sdk_tool(config, "apksigner"), "sign", "--ks", str(Path(config["keystore"]).expanduser()),
                 "--ks-key-alias", config["alias"], "--ks-pass", "env:PERSONA_APK_STORE_PASSWORD",
                 "--key-pass", "env:PERSONA_APK_KEY_PASSWORD", "--out", str(output), str(aligned)],
                environment=environment)
    finally:
        environment.pop("PERSONA_APK_STORE_PASSWORD")
        environment.pop("PERSONA_APK_KEY_PASSWORD")
        password = key_password = None
    verification = execute([sdk_tool(config, "apksigner"), "verify", "--verbose", "--print-certs", str(output)])
    certificate = parse_signer_certificate(verification)
    if certificate != config["expected_cert_sha256"]:
        raise ValueError("signed APK certificate differs from configured certificate")
    execute([sdk_tool(config, "zipalign"), "-c", "-P", "16", "4", str(output)])
    metadata(output, config, request)
    digest = digest_file(output)
    return dict(request, signed_sha256=digest, sha256=digest, certificate_sha256=certificate,
                size=output.stat().st_size)


def ssh(config, command):
    return execute(["ssh", "-o", "BatchMode=yes", "--", config["ssh_target"], command])


def scp(config, source, destination):
    execute(["scp", "-q", "-o", "BatchMode=yes", "--", source, destination])


def reusable_receipt(config, request, ready):
    try:
        receipt = json.loads(ready)
    except json.JSONDecodeError:
        # A partial or corrupt ready marker is recoverable by signing again.
        return False
    if not isinstance(receipt, dict):
        return False
    if any(receipt.get(key) != request[key] for key in ("source_sha", "unsigned_sha256", "package", "version_code")):
        return False
    if receipt.get("certificate_sha256") != config["expected_cert_sha256"]:
        return False
    expected = receipt.get("signed_sha256")
    if not isinstance(expected, str) or not re.fullmatch(HEX_SHA256, expected):
        return False
    apk = config["remote_queue_dir"].rstrip("/") + "/signed.apk"
    code = ("import hashlib,pathlib\n"
            f"apk=pathlib.Path({apk!r})\n"
            "if not apk.is_file():\n print('missing')\nelse:\n"
            " digest=hashlib.sha256()\n"
            " with apk.open('rb') as stream:\n"
            "  for chunk in iter(lambda: stream.read(1024*1024), b''): digest.update(chunk)\n"
            " print(digest.hexdigest())\n")
    return ssh(config, "python3 -c " + shlex.quote(code)).strip() == expected


def poll(config):
    queue = config["remote_queue_dir"].rstrip("/")
    remote = config["ssh_target"] + ":" + queue
    raw = ssh(config, f"if test -f {shlex.quote(queue + '/request.json')}; then cat {shlex.quote(queue + '/request.json')}; fi")
    if not raw.strip():
        return "idle"
    request = json.loads(raw)
    validate_request(request, config)
    ready = ssh(config, f"if test -f {shlex.quote(queue + '/receipt.json')}; then cat {shlex.quote(queue + '/receipt.json')}; fi")
    if ready.strip() and reusable_receipt(config, request, ready):
        return "already signed"
    with tempfile.TemporaryDirectory(prefix="persona-android-sign-") as temporary:
        directory = Path(temporary)
        unsigned, signed = directory / "unsigned.apk", directory / "signed.apk"
        scp(config, remote + "/unsigned.apk", str(unsigned))
        receipt = sign(unsigned, signed, config, request)
        receipt_file = directory / "receipt.json"
        receipt_file.write_text(json.dumps(receipt, sort_keys=True) + "\n")
        suffix = ".pending-" + uuid.uuid4().hex
        pending_apk, pending_receipt = queue + "/signed.apk" + suffix, queue + "/receipt.json" + suffix
        try:
            scp(config, str(signed), config["ssh_target"] + ":" + pending_apk)
            scp(config, str(receipt_file), config["ssh_target"] + ":" + pending_receipt)
            # The build worker holds its queue lock until receipt consumption. Check
            # identity again before exposing results; receipt is the ready marker.
            code = ("import hashlib,json,os,pathlib\n"
                    f"q=pathlib.Path({queue!r})\n"
                    f"expected={request!r}\n"
                    "current=json.loads((q/'request.json').read_text())\n"
                    "if current != expected: raise ValueError('request changed')\n"
                    f"apk=pathlib.Path({pending_apk!r}); receipt=pathlib.Path({pending_receipt!r})\n"
                    "data=json.loads(receipt.read_text())\n"
                    "if hashlib.sha256(apk.read_bytes()).hexdigest()!=data['signed_sha256']: raise ValueError('upload hash mismatch')\n"
                    "os.replace(apk,q/'signed.apk')\nos.replace(receipt,q/'receipt.json')\n")
            ssh(config, "python3 -c " + shlex.quote(code))
        finally:
            ssh(config, "rm -f -- " + shlex.quote(pending_apk) + " " + shlex.quote(pending_receipt))
    return "signed " + request["source_sha"]


def run(config):
    validate_config(config)
    lock_path = Path(config["lock_file"]).expanduser()
    lock_path.parent.mkdir(parents=True, exist_ok=True)
    with lock_path.open("a") as lock:
        try:
            fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return "another signing poll is running"
        return poll(config)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True, type=Path)
    args = parser.parse_args()
    try:
        config = json.loads(args.config.expanduser().read_text())
        print(run(config))
    except (OSError, ValueError, KeyError, TypeError) as error:
        # Do not print untrusted config/request contents or password subprocess errors.
        parser.exit(1, f"Signing poll failed ({type(error).__name__}); inspect queue/config and tool availability.\n")


if __name__ == "__main__":
    main()
