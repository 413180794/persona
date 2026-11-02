#!/usr/bin/env python3
"""Minimal App Store Connect API client with no third-party dependencies.

The ES256 JWT is signed with the system `openssl`, so this runs on a stock
macOS Python. Credentials come from the config (docs/config.md), overridable with ASC_KEY_ID, ASC_ISSUER_ID,
ASC_KEY_PATH in the environment.

The App Store Connect app id (not the bundle id) is read from the config
(app_store.app_id in the config) — set once the App record exists in App Store Connect.

Usage as a CLI for quick reads:
  python3 scripts/release/asc.py GET /v1/apps
"""
import base64
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import jarvis_config  # noqa: E402

API = "https://api.appstoreconnect.apple.com"
CONFIG = jarvis_config.load()
STORE = CONFIG.get("app_store", {})
BUNDLE_ID = jarvis_config.get(CONFIG, "app.bundle_id", "")
KEY_ID = os.environ.get("ASC_KEY_ID") or STORE.get("key_id", "")
ISSUER = os.environ.get("ASC_ISSUER_ID") or STORE.get("issuer_id", "")
KEY_PATH = os.path.expanduser(os.environ.get("ASC_KEY_PATH") or STORE.get("key_path") or f"~/.appstoreconnect/AuthKey_{KEY_ID}.p8")


def app_id() -> str:
    """App Store Connect numeric app id, from app_store.app_id in the config (docs/config.md)."""
    value = str(STORE.get("app_id", "")).strip()
    if not value.isdigit():
        sys.exit(
            f"app_store.app_id in {jarvis_config.path()} must be the numeric Apple ID of the App record, got {value!r}.\n"
            f"Create the record on the web first (App Store Connect › Apps › + › New App, bundle id {BUNDLE_ID}). "
            "See docs/release.md."
        )
    return value


_token = (0.0, "")


def _b64(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode()


def _der_to_raw(der: bytes) -> bytes:
    """Convert an ASN.1 DER ECDSA signature to the raw r||s form JWT expects."""
    def read_int(buf, i):
        assert buf[i] == 0x02
        n = buf[i + 1]
        return int.from_bytes(buf[i + 2:i + 2 + n], "big"), i + 2 + n
    i = 2 if der[1] < 0x80 else 3
    r, i = read_int(der, i)
    s, _ = read_int(der, i)
    return r.to_bytes(32, "big") + s.to_bytes(32, "big")


def token() -> str:
    global _token
    if _token[0] > time.time() + 60:
        return _token[1]
    if not os.path.exists(KEY_PATH):
        sys.exit(f"App Store Connect key not found: {KEY_PATH} (set ASC_KEY_PATH)")
    now = int(time.time())
    header = _b64(json.dumps({"alg": "ES256", "kid": KEY_ID, "typ": "JWT"}).encode())
    payload = _b64(json.dumps({"iss": ISSUER, "iat": now, "exp": now + 1200, "aud": "appstoreconnect-v1"}).encode())
    signing_input = f"{header}.{payload}".encode()
    der = subprocess.run(["openssl", "dgst", "-sha256", "-sign", KEY_PATH], input=signing_input,
                         capture_output=True, check=True).stdout
    _token = (now + 1200, f"{header}.{payload}.{_b64(_der_to_raw(der))}")
    return _token[1]


class APIError(Exception):
    def __init__(self, status, body):
        super().__init__(f"HTTP {status}: {body}")
        self.status, self.body = status, body


def request(method: str, path: str, body=None, params=None):
    url = path if path.startswith("http") else API + path
    if params:
        url += ("&" if "?" in url else "?") + urllib.parse.urlencode(params, safe=",[]")
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method, headers={
        "Authorization": f"Bearer {token()}", "Content-Type": "application/json"})
    for attempt in range(5):
        try:
            with urllib.request.urlopen(req, timeout=120) as resp:
                raw = resp.read()
                return json.loads(raw) if raw else None
        except urllib.error.HTTPError as e:
            if e.code == 429 or e.code >= 500:
                time.sleep(2 ** attempt)
                continue
            raise APIError(e.code, e.read().decode()) from None
    raise APIError(429, "retries exhausted")


def get_all(path: str, params=None):
    """GET every page of a collection and return the concatenated data."""
    out, url = [], path
    params = dict(params or {}, limit=200)
    while url:
        page = request("GET", url, params=params)
        out += page["data"]
        url, params = page.get("links", {}).get("next"), None
    return out


def create(type_: str, attributes: dict, relationships: dict, path: str):
    rels = {k: {"data": {"type": t, "id": i}} for k, (t, i) in relationships.items()}
    return request("POST", path, {"data": {"type": type_, "attributes": attributes, "relationships": rels}})["data"]


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    method, path = sys.argv[1], sys.argv[2]
    print(json.dumps(request(method, path), indent=1, ensure_ascii=False))
