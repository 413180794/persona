#!/usr/bin/env python3
"""Validate and publish a signed APK without deploying Hub services."""
import argparse
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
import tarfile
import tempfile

from android_apk_atomic import digest_file, validate_release

SCRIPT_DIR = Path(__file__).resolve().parent


def command_argv(value):
    try:
        command = json.loads(value)
    except json.JSONDecodeError as error:
        raise argparse.ArgumentTypeError("SSH command must be a JSON argv array") from error
    if not isinstance(command, list) or not command or any(not isinstance(arg, str) or not arg for arg in command):
        raise argparse.ArgumentTypeError("SSH command must be a nonempty JSON argv array")
    return command


def tool(name):
    found = shutil.which(name)
    if found:
        return found
    sdk = os.environ.get("ANDROID_HOME") or os.environ.get("ANDROID_SDK_ROOT")
    if sdk:
        candidates = sorted(Path(sdk).glob(f"build-tools/*/{name}"), reverse=True)
        for candidate in candidates:
            if os.access(candidate, os.X_OK):
                return str(candidate)
    raise ValueError(f"required Android SDK tool is missing: {name}")


def run(*command):
    return subprocess.check_output(command, text=True).strip()


def parse_signer_certificate(verification):
    """Read the single verified signer from legacy and SDK 37 apksigner output."""
    lines = verification.splitlines()
    counts = [line for line in lines if line.startswith("Number of signers:")]
    if counts != ["Number of signers: 1"]:
        raise ValueError("expected exactly one verified APK signer")
    certificates = [line for line in lines if "certificate SHA-256 digest:" in line]
    if len(certificates) != 1:
        raise ValueError("expected exactly one signer certificate SHA-256 line")
    match = re.fullmatch(
        r"(?:Signer #1|V[1-4](?:\.\d+)? Signer:) certificate SHA-256 digest: ([0-9a-fA-F]{64})",
        certificates[0],
    )
    if not match:
        raise ValueError("invalid signer certificate SHA-256 digest or output format")
    return match[1].lower()


def inspect_apk(apk, args):
    badging = run(tool("aapt2"), "dump", "badging", str(apk))
    package = re.search(r"^package: name='([^']+)' versionCode='([^']+)' versionName='([^']*)'", badging, re.M)
    sdk = re.search(r"^(?:sdkVersion|minSdkVersion):'(\d+)'", badging, re.M)
    if not package or not sdk:
        raise ValueError("cannot read APK package/version/min-sdk metadata")
    release = dict(package_name=package[1], version_code=int(package[2]),
                   version_name=package[3], min_sdk=int(sdk[1]))
    for key in ("version_code", "version_name", "min_sdk"):
        expected = getattr(args, key)
        if expected is not None and expected != release[key]:
            raise ValueError(f"--{key.replace('_', '-')} differs from actual APK")
    if args.expected_package and args.expected_package != release["package_name"]:
        raise ValueError("APK package differs from expected package")
    signing = run(tool("apksigner"), "verify", "--verbose", "--print-certs", str(apk))
    certificate = parse_signer_certificate(signing)
    if args.expected_cert_sha256 and certificate != args.expected_cert_sha256.lower().replace(":", ""):
        raise ValueError("APK signing certificate differs from expected certificate")
    release.update(certificate_sha256=certificate, sha256=digest_file(apk),
                   size=apk.stat().st_size, notes=args.notes)
    if args.source_sha:
        if not re.fullmatch(r"[0-9a-fA-F]{40}|[0-9a-fA-F]{64}", args.source_sha):
            raise ValueError("source_sha must be a full Git commit SHA")
        release["source_sha"] = args.source_sha.lower()
    validate_release(release)
    return release


def destination(args):
    target = args.hub_ssh or os.environ.get("PERSONA_HUB_SSH")
    if not target:
        sys.path.insert(0, str(SCRIPT_DIR.parent))
        import persona_config
        target = persona_config.env(persona_config.load()).get("PERSONA_HUB_SSH")
    if not target or target.startswith("-"):
        raise ValueError("configure hub.machine SSH address or pass --hub-ssh")
    if args.remote_dir:
        path = Path(args.remote_dir)
        if not path.is_absolute() or tuple(path.parts[-2:]) != ("apps", "android"):
            raise ValueError("--remote-dir must be an absolute path ending in /apps/android")
        resolver = "release_dir=" + shlex.quote(str(path))
    else:
        resolver = run(sys.executable, str(SCRIPT_DIR.parent / "persona_identity.py"),
                       "--service", "hub", "--prefix", "-")
        resolver += '\nrelease_dir="$PERSONA_HOME/apps/android"'
    return target, resolver


def publish(args):
    if not args.apk.is_file():
        raise ValueError("APK file does not exist")
    with tempfile.TemporaryDirectory(prefix="persona-apk-") as temporary:
        staging = Path(temporary)
        apk = staging / "app.apk"
        shutil.copyfile(args.apk, apk)
        release = inspect_apk(apk, args)
        apk.rename(staging / (release["sha256"] + ".apk"))
        (staging / "latest.json").write_text(json.dumps(release, ensure_ascii=False, indent=2) + "\n")
        shutil.copyfile(SCRIPT_DIR / "android_apk_atomic.py", staging / "publish.py")
        target, resolver = destination(args)
        # The stage name is local-generated and shell-quoted. SSH retains its normal
        # known-host verification and authentication; no service command is run.
        command = "set -eu\numask 077\n" + resolver + '\nmkdir -p "$release_dir"\n'
        command += 'stage=$(mktemp -d "$release_dir/.upload-XXXXXXXX")\n'
        command += 'trap \'rm -rf "$stage"\' EXIT HUP INT TERM\n'
        command += 'tar -xzf - -C "$stage"\n'
        command += 'python3 "$stage/publish.py" --directory "$release_dir" --stage "$stage"\n'
        with tempfile.TemporaryFile() as archive:
            with tarfile.open(fileobj=archive, mode="w:gz") as bundle:
                for path in staging.iterdir():
                    bundle.add(path, arcname=path.name)
            archive.seek(0)
            subprocess.run(args.ssh_command + ["--", target, command], stdin=archive, check=True)
        print(json.dumps(release, ensure_ascii=False))


def parser():
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("apk", type=Path)
    result.add_argument("--version-code", type=int)
    result.add_argument("--version-name")
    result.add_argument("--min-sdk", type=int)
    result.add_argument("--notes", default="")
    result.add_argument("--expected-cert-sha256", default=os.environ.get("PERSONA_ANDROID_CERT_SHA256"))
    result.add_argument("--expected-package", default=os.environ.get("PERSONA_ANDROID_PACKAGE"))
    result.add_argument("--source-sha")
    result.add_argument("--hub-ssh")
    result.add_argument("--ssh-command", type=command_argv, default=["ssh"], help="JSON argv array for SSH transport")
    result.add_argument("--remote-dir", help="absolute Hub apps/android directory")
    return result


def main():
    arguments = parser()
    try:
        publish(arguments.parse_args())
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        arguments.exit(1, f"APK publication failed: {error}\n")


if __name__ == "__main__":
    main()
