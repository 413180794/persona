#!/usr/bin/env python3
"""Reads the personal build / deploy config (docs/config.md) so no script hard-codes one person's setup.

The config lives outside the repository so clean worktrees (releases) see it too:
  $JARVIS_CONFIG, else ~/.config/jarvis/config.json   (template: config.example.json)

  python3 scripts/jarvis_config.py env              # shell exports: eval "$(python3 scripts/jarvis_config.py env)"
  python3 scripts/jarvis_config.py xcconfig         # writes apple/Config.xcconfig (bundle id, team) for XcodeGen / Xcode
  python3 scripts/jarvis_config.py export-options <out.plist> [upload|export]
  python3 scripts/jarvis_config.py get <dotted.key>  # one value, e.g. hub.url
"""
import json
import os
import shlex
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def path() -> str:
    return os.path.expanduser(os.environ.get("JARVIS_CONFIG", "~/.config/jarvis/config.json"))


def load() -> dict:
    p = path()
    if not os.path.exists(p):
        sys.exit(f"missing {p}\nCopy config.example.json there and fill it in (docs/config.md).")
    with open(p) as f:
        return json.load(f)


def get(cfg: dict, key: str, default=None):
    cur = cfg
    for part in key.split("."):
        if not isinstance(cur, dict) or part not in cur:
            return default
        cur = cur[part]
    return cur


def machine(cfg: dict, mid: str) -> dict:
    for m in cfg.get("machines", []):
        if m.get("id") == mid:
            return m
    sys.exit(f"machine {mid!r} is not in {path()}")


def env(cfg: dict) -> dict:
    store = cfg.get("app_store", {})
    hub = cfg.get("hub", {})
    hub_ssh = machine(cfg, hub["machine"]).get("ssh", hub["machine"]) if hub.get("machine") else ""
    out = {
        "JARVIS_BUNDLE_ID": get(cfg, "app.bundle_id", ""),
        "JARVIS_TEAM_ID": get(cfg, "app.team_id", ""),
        "ASC_KEY_ID": store.get("key_id", ""),
        "ASC_ISSUER_ID": store.get("issuer_id", ""),
        "ASC_KEY_PATH": os.path.expanduser(store.get("key_path", "")),
        "ASC_APP_ID": str(store.get("app_id", "")),
        "JARVIS_HUB_MACHINE": hub.get("machine", ""),
        "JARVIS_HUB_SSH": hub_ssh,
        "JARVIS_HUB_URL": hub.get("url", ""),
        "JARVIS_MACHINES": " ".join(m["id"] for m in cfg.get("machines", [])),
        "JARVIS_SIM": cfg.get("simulator", ""),
        # Where scripts/publish.sh puts jarvisd and install.sh (the site/ worker's domain), and the wrangler to run.
        "JARVIS_PUBLISH_URL": get(cfg, "publish.site_url", "").rstrip("/"),
        "JARVIS_WRANGLER": os.path.expanduser(get(cfg, "publish.wrangler", "")),
    }
    # Which Xcode builds (and releases) the app; a DEVELOPER_DIR already in the environment wins. iPhone Duo needs
    # Xcode 27 or later: built with an older one, the app does not reach under the status bar and the camera.
    if cfg.get("xcode") and not os.environ.get("DEVELOPER_DIR"):
        out["DEVELOPER_DIR"] = os.path.join(os.path.expanduser(cfg["xcode"]), "Contents", "Developer")
    return out


def xcconfig(cfg: dict):
    # Generated, git-ignored: the committed project only says $(JARVIS_BUNDLE_ID).
    out = os.path.join(ROOT, "apple", "Config.xcconfig")
    with open(out, "w") as f:
        f.write("// Generated from %s by scripts/jarvis_config.py; do not edit or commit.\n" % path())
        f.write("JARVIS_BUNDLE_ID = %s\n" % get(cfg, "app.bundle_id", ""))
        f.write("DEVELOPMENT_TEAM = %s\n" % get(cfg, "app.team_id", ""))
        # The push relay the app registers with (docs/push-relay.md); "//" starts a comment in xcconfig, hence $().
        f.write("JARVIS_PUSH_RELAY = %s\n" % get(cfg, "app.push_relay", "").rstrip("/").replace("//", "/$()/"))


def export_options(cfg: dict, out: str, destination: str = "upload"):
    bid = get(cfg, "app.bundle_id", "")
    plist = f"""<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>method</key>
	<string>app-store-connect</string>
	<key>destination</key>
	<string>{destination}</string>
	<key>signingStyle</key>
	<string>manual</string>
	<key>signingCertificate</key>
	<string>Apple Distribution</string>
	<key>provisioningProfiles</key>
	<dict>
		<key>{bid}</key>
		<string>Jarvis App Store</string>
		<key>{bid}.watchkitapp</key>
		<string>Jarvis Watch App Store</string>
		<key>{bid}.liveactivity</key>
		<string>Jarvis Live Activity App Store</string>
		<key>{bid}.notify</key>
		<string>Jarvis Notify App Store</string>
	</dict>
	<key>teamID</key>
	<string>{get(cfg, "app.team_id", "")}</string>
	<key>uploadSymbols</key>
	<true/>
	<key>manageAppVersionAndBuildNumber</key>
	<false/>
</dict>
</plist>
"""
    with open(out, "w") as f:
        f.write(plist)


def main():
    args = sys.argv[1:]
    cfg = load()
    if args[:1] == ["env"]:
        for k, v in env(cfg).items():
            print(f"export {k}={shlex.quote(v)}")
    elif args[:1] == ["xcconfig"]:
        xcconfig(cfg)
    elif args[:1] == ["export-options"] and len(args) >= 2:
        export_options(cfg, args[1], args[2] if len(args) > 2 else "upload")
    elif args[:1] == ["get"] and len(args) == 2:
        v = get(cfg, args[1])
        if v is None:
            sys.exit(f"{args[1]} is not set in {path()}")
        print(v if isinstance(v, str) else json.dumps(v))
    elif args[:1] == ["machine"] and len(args) == 2:
        print(json.dumps(machine(cfg, args[1])))
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main()
