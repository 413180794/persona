#!/usr/bin/env python3
"""Prepares App Store signing through the App Store Connect API (idempotent).

- registers the iPhone and Apple Watch bundle ids,
- enables PUSH_NOTIFICATIONS and HEALTHKIT on the iPhone App ID,
- creates (or rebuilds) the "Jarvis App Store" / "Jarvis Watch App Store" provisioning profiles
  with the team's distribution certificate and installs them where Xcode looks.

The API key's role (App Manager) can't use cloud-managed signing, so release builds sign manually with
the "Apple Distribution" identity in the login keychain (see docs/release.md).

  python3 scripts/asc_signing.py                 # App Store profiles
  python3 scripts/asc_signing.py --dev           # also development profiles for registered devices
  python3 scripts/asc_signing.py --check         # only report what is there, change nothing
"""
import base64
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from asc import BUNDLE_ID, APIError, create, get_all, request  # noqa: E402

APP = BUNDLE_ID
WATCH = APP + ".watchkitapp"
# bundle identifier -> (App ID name, profile name, capabilities)
TARGETS = {
    APP: ("Jarvis", "Jarvis App Store", ["PUSH_NOTIFICATIONS", "HEALTHKIT"]),
    WATCH: ("Jarvis Watch", "Jarvis Watch App Store", []),
}
PROFILE_DIR = os.path.expanduser("~/Library/MobileDevice/Provisioning Profiles")


def bundle_id(identifier: str, name: str, check_only: bool) -> str:
    found = [b for b in get_all("/v1/bundleIds", {"filter[identifier]": identifier})
             if b["attributes"]["identifier"] == identifier]
    if found:
        print("have bundle id", identifier, found[0]["id"])
        return found[0]["id"]
    if check_only:
        sys.exit(f"bundle id {identifier} is not registered yet; run without --check")
    print("register", identifier)
    return create("bundleIds", {"identifier": identifier, "name": name, "platform": "IOS"}, {}, "/v1/bundleIds")["id"]


def ensure_capability(bid: str, capability: str, check_only: bool):
    # This relationship doesn't page (no `limit`).
    have = {c["attributes"]["capabilityType"] for c in request("GET", f"/v1/bundleIds/{bid}/bundleIdCapabilities")["data"]}
    if capability in have:
        print("  have capability", capability)
        return
    if check_only:
        print("  missing capability", capability)
        return
    print("  enable", capability)
    # PUSH_NOTIFICATIONS and HEALTHKIT take no settings; the API rejects any key we invent
    # ("'PUSH_NOTIFICATIONS' is not a valid value for settings/0/key").
    try:
        create("bundleIdCapabilities", {"capabilityType": capability, "settings": []},
               {"bundleId": ("bundleIds", bid)}, "/v1/bundleIdCapabilities")
    except APIError as e:
        # Entity.attribute.invalid when the capability is not offered back for this App ID.
        sys.exit(f"could not enable {capability}: {e}")


def certificate(kind: str) -> str:
    wanted = ("DISTRIBUTION", "IOS_DISTRIBUTION") if kind == "dist" else ("DEVELOPMENT", "IOS_DEVELOPMENT")
    certs = [c for c in get_all("/v1/certificates") if c["attributes"]["certificateType"] in wanted]
    if not certs:
        sys.exit(f"No {'distribution' if kind == 'dist' else 'development'} certificate in the team: create one first "
                 "(see docs/release.md).")
    return max(certs, key=lambda c: c["attributes"]["expirationDate"])["id"]


def profile(bid: str, name: str, cert: str, kind: str = "IOS_APP_STORE", check_only: bool = False):
    # Includes INVALID ones: changing the App ID's capabilities invalidates profiles, and a stale profile with the
    # same name blocks creating the new one. Recreating is cheap and keeps it in sync with the App ID.
    existing = get_all("/v1/profiles", {"filter[name]": name})
    current = [p for p in existing if p["attributes"]["profileState"] == "ACTIVE"]
    if check_only:
        print("  profile", name, "ACTIVE" if current else "missing or invalid")
        return
    for p in existing:
        request("DELETE", f"/v1/profiles/{p['id']}")
    print("create profile", name)
    body = {"data": {"type": "profiles",
                     "attributes": {"name": name, "profileType": kind},
                     "relationships": {"bundleId": {"data": {"type": "bundleIds", "id": bid}},
                                       "certificates": {"data": [{"type": "certificates", "id": cert}]}}}}
    if kind in ("IOS_APP_DEVELOPMENT", "MAC_CATALYST_APP_DEVELOPMENT"):
        platform = "MAC_OS" if kind.startswith("MAC") else "IOS"
        devices = [d for d in get_all("/v1/devices")
                   if d["attributes"]["status"] == "ENABLED" and d["attributes"]["platform"] == platform]
        body["data"]["relationships"]["devices"] = {"data": [{"type": "devices", "id": d["id"]} for d in devices]}
    created = request("POST", "/v1/profiles", body)["data"]
    os.makedirs(PROFILE_DIR, exist_ok=True)
    path = os.path.join(PROFILE_DIR, f"{created['attributes']['uuid']}.mobileprovision")
    with open(path, "wb") as f:
        f.write(base64.b64decode(created["attributes"]["profileContent"]))
    print("  installed", path)


def main():
    check_only = "--check" in sys.argv
    cert = certificate("dist")
    print("distribution certificate", cert)
    ids = {}
    for identifier, (name, profile_name, capabilities) in TARGETS.items():
        print(identifier)
        bid = bundle_id(identifier, name, check_only)
        ids[identifier] = bid
        for capability in capabilities:
            ensure_capability(bid, capability, check_only)
        profile(bid, profile_name, cert, check_only=check_only)
    if "--dev" in sys.argv:
        dev = certificate("dev")
        for identifier, (name, profile_name, _) in TARGETS.items():
            profile(ids[identifier], profile_name.replace("App Store", "Development"), dev,
                    "IOS_APP_DEVELOPMENT", check_only)
    if not check_only:
        print("\nDone. Release builds sign with these profiles (release.sh generates the export options from the config).")


if __name__ == "__main__":
    try:
        main()
    except APIError as e:
        sys.exit(str(e))
