#!/usr/bin/env python3
"""TestFlight setup for Jarvis (idempotent). Jarvis is internal-only: one internal group, one tester.

  python3 scripts/asc_testflight.py                    # group "Internal" + testers, then status
  python3 scripts/asc_testflight.py status             # the last few builds and their processing state
  python3 scripts/asc_testflight.py wait [minutes] [build]  # poll until that build (default: newest) is VALID, then attach it
  python3 scripts/asc_testflight.py info "<What to Test>"   # write "What to Test" on the newest build, attach it

Internal testers must be users of the team in App Store Connect (tester@example.com is).
The App ID comes from scripts/app_id.txt (see docs/release.md).
"""
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from asc import APIError, app_id, get_all, request  # noqa: E402

APP_ID = app_id()
GROUP = "Internal"
TESTERS = ["tester@example.com"]
FEEDBACK_EMAIL = "person@example.com"

BETA_INFO = {
    "en-US": "Jarvis is a personal assistant that drives Claude Code sessions on your own computers from the iPhone and the "
             "Apple Watch: watch every step stream in, answer questions, approve plans and tool calls. It can also read and "
             "write your contacts, calendar, reminders, photos, health data and mail when you ask.",
    "zh-Hans": "Jarvis 是只给自己用的助手：在 iPhone 和 Apple Watch 上驱动自己电脑里的 Claude Code 会话——实时看每一步、"
               "回答提问、审批计划和工具调用；也能在你需要时读写通讯录、日历、提醒事项、照片、健康数据和邮件。",
}


def default_whats_new() -> str:
    return ("请试试：连上 hub 后新开一个会话，看 Claude 的每一步实时出现；遇到提问直接在手机上回答，遇到计划就审批；"
            "在「手机数据」里打开要允许的数据源，然后让 Claude 查一下通讯录或这周的日程。有问题请在 TestFlight 里反馈。")


def group_id() -> str:
    for g in get_all(f"/v1/apps/{APP_ID}/betaGroups"):
        if g["attributes"]["name"] == GROUP:
            return g["id"]
    print("create group", GROUP)
    body = {"data": {"type": "betaGroups",
                     "attributes": {"name": GROUP, "isInternalGroup": True, "hasAccessToAllBuilds": True},
                     "relationships": {"app": {"data": {"type": "apps", "id": APP_ID}}}}}
    return request("POST", "/v1/betaGroups", body)["data"]["id"]


def add_tester(gid: str, email: str):
    members = get_all(f"/v1/betaGroups/{gid}/betaTesters")
    if any(m["attributes"].get("email", "").lower() == email.lower() for m in members):
        print("already in group:", email)
        return
    # One email has a tester record per app, so linking another app's record answer with 409
    # "Tester(s) cannot be assigned to this group because they belong to another app". Creating the
    # tester together with the group relationship works and reuses the person.
    body = {"data": {"type": "betaTesters", "attributes": {"email": email, "firstName": "chyroc"},
                     "relationships": {"betaGroups": {"data": [{"type": "betaGroups", "id": gid}]}}}}
    try:
        request("POST", "/v1/betaTesters", body)
        print("invited", email)
    except APIError as e:
        if e.status == 409:
            print(f"{email}: already a tester under another app (409); add it to this app's {GROUP} group by hand "
                  "in App Store Connect › Users and Access › TestFlight")
            return
        raise


def builds(limit: int = 5):
    return request("GET", "/v1/builds", params={"filter[app]": APP_ID, "sort": "-uploadedDate", "limit": limit})["data"]


def status():
    found = builds()
    if not found:
        print("no builds yet (./scripts/release.sh)")
        return
    for b in found:
        a = b["attributes"]
        print(a["version"], a["processingState"], a.get("uploadedDate", "")[:16],
              "expired" if a.get("expired") else "", a.get("expirationDate", ""), b["id"])


def newest_valid():
    found = request("GET", "/v1/builds", params={"filter[app]": APP_ID, "filter[processingState]": "VALID",
                                                "sort": "-uploadedDate", "limit": 1})["data"]
    return found[0] if found else None


def attach(build, gid: str):
    """Put the build in the internal group so testers get it (internal groups are auto-approved).

    Groups with hasAccessToAllBuilds already see every build and reject explicit assignment."""
    g = request("GET", f"/v1/betaGroups/{gid}")
    if g["data"]["attributes"].get("hasAccessToAllBuilds"):
        print("build", build["attributes"]["version"], "is available to", GROUP, "(all builds)")
        return
    request("POST", f"/v1/betaGroups/{gid}/relationships/builds",
            {"data": [{"type": "builds", "id": build["id"]}]})
    print("build", build["attributes"]["version"], "is now in", GROUP)


def write_info(build, whats_new: str):
    """Test information at the app level and "What to Test" on one build."""
    locales = {l["attributes"]["locale"]: l for l in get_all(f"/v1/apps/{APP_ID}/betaAppLocalizations")}
    for locale, description in BETA_INFO.items():
        attrs = {"description": description, "feedbackEmail": FEEDBACK_EMAIL}
        if locale in locales:
            request("PATCH", f"/v1/betaAppLocalizations/{locales[locale]['id']}",
                    {"data": {"type": "betaAppLocalizations", "id": locales[locale]["id"], "attributes": attrs}})
        else:
            request("POST", "/v1/betaAppLocalizations",
                    {"data": {"type": "betaAppLocalizations",
                              "attributes": dict(attrs, locale=locale),
                              "relationships": {"app": {"data": {"type": "apps", "id": APP_ID}}}}})
    have = {l["attributes"]["locale"]: l for l in request("GET", f"/v1/builds/{build['id']}/betaBuildLocalizations")["data"]}
    for locale in BETA_INFO:
        if locale in have:
            request("PATCH", f"/v1/betaBuildLocalizations/{have[locale]['id']}",
                    {"data": {"type": "betaBuildLocalizations", "id": have[locale]["id"],
                              "attributes": {"whatsNew": whats_new}}})
        else:
            request("POST", "/v1/betaBuildLocalizations",
                    {"data": {"type": "betaBuildLocalizations",
                              "attributes": {"locale": locale, "whatsNew": whats_new},
                              "relationships": {"build": {"data": {"type": "builds", "id": build["id"]}}}}})
    print("what to test written on build", build["attributes"]["version"])


def build_by_version(version: str):
    found = request("GET", "/v1/builds", params={"filter[app]": APP_ID, "filter[version]": version, "limit": 1})["data"]
    return found[0] if found else None


def wait(minutes: int, version: str | None = None):
    """Poll until the build finishes processing, then attach it to the internal group.

    Pass the build number release.sh printed: right after an upload the new build is not listed yet, and "the newest
    build" is still the previous one (already VALID), which got attached instead of the new one."""
    deadline = time.time() + minutes * 60
    last = None
    while True:
        build = build_by_version(version) if version else (builds(1) or [None])[0]
        if build:
            a = build["attributes"]
            state = a["processingState"]
            if state != last:
                print(time.strftime("%H:%M:%S"), "build", a["version"], state)
                last = state
            if state == "VALID":
                write_info(build, default_whats_new())
                attach(build, group_id())
                return
            if state in ("INVALID", "FAILED"):
                sys.exit(f"build {a['version']} finished as {state}: check App Store Connect for the reason")
        if time.time() > deadline:
            sys.exit(f"still {last or 'not listed'} after {minutes} min (builds appear 10–30 min after upload)")
        time.sleep(30)


def main():
    args = sys.argv[1:]
    if args[:1] == ["status"]:
        status()
    elif args[:1] == ["wait"]:
        wait(int(args[1]) if len(args) > 1 else 45, args[2] if len(args) > 2 else None)
    elif args[:1] == ["info"]:
        build = newest_valid()
        if not build:
            sys.exit("no VALID build yet (python3 scripts/asc_testflight.py wait)")
        write_info(build, args[1] if len(args) > 1 else default_whats_new())
        attach(build, group_id())
    elif args[:1] in ([], ["setup"]):
        gid = group_id()
        for email in TESTERS:
            add_tester(gid, email)
        status()


if __name__ == "__main__":
    try:
        main()
    except APIError as e:
        sys.exit(str(e))
