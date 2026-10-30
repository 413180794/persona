#!/usr/bin/env python3
"""TestFlight setup for Jarvis (idempotent): an internal group, and an external one people join by invitation only.

  python3 scripts/asc_testflight.py                    # group "Internal" + testers, then status
  python3 scripts/asc_testflight.py status             # the last few builds and their processing state
  python3 scripts/asc_testflight.py wait [minutes] [build]  # poll until that build (default: newest) is VALID, then attach it
  python3 scripts/asc_testflight.py info "<What to Test>"   # write "What to Test" on the newest build, attach it
  python3 scripts/asc_testflight.py external           # group "Friends": newest build, Beta App Review, invitations

"wait" and "info" also hand the build to the external group when the config has app_store.external, so every new
TestFlight build reaches the invited testers too (after Beta App Review); a failure there never fails the release.

Internal testers (app_store.testers in the config) must be users of the team in App Store Connect.
External testers (app_store.external.testers) can be anyone: they get an email invitation, and the group has no
public link, so nobody else can join. Their first build of a version goes through Beta App Review, which needs the
contact in app_store.external.review (docs/release.md). The App ID comes from app_store.app_id in the config (docs/config.md).
"""
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from asc import STORE, APIError, app_id, get_all, request  # noqa: E402

APP_ID = app_id()
GROUP = "Internal"
TESTERS = STORE.get("testers", [])  # [{"email", "first_name"}]
FEEDBACK_EMAIL = STORE.get("feedback_email", "")
EXTERNAL = STORE.get("external", {})  # {"group", "testers", "review", "privacy_url"}
EXTERNAL_GROUP = EXTERNAL.get("group", "Friends")

BETA_INFO = {
    "en-US": "Jarvis drives the Claude Code and Codex sessions on your own computers from the iPhone and the Apple Watch: "
             "watch every step stream in, answer questions, approve plans and tool calls. Its assistant can also read your "
             "mail and calendar and, when you ask, your contacts, reminders, photos and health data. It connects to a hub "
             "you run yourself (see the README of the project).",
    "zh-Hans": "Jarvis 让你在 iPhone 和 Apple Watch 上驱动自己各台电脑里的 Claude Code 和 Codex：实时看每一步、回答提问、"
               "审批计划和工具调用；助手还能读你的邮件和日历，在你需要时读写通讯录、提醒事项、照片和健康数据。"
               "需要连接你自己部署的 hub（见项目 README）。",
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


def add_tester(gid: str, email: str, first_name: str, last_name: str = ""):
    members = get_all(f"/v1/betaGroups/{gid}/betaTesters")
    if any(m["attributes"].get("email", "").lower() == email.lower() for m in members):
        print("already in group:", email)
        return
    # One email has a tester record per app, so linking another app's record answer with 409
    # "Tester(s) cannot be assigned to this group because they belong to another app". Creating the
    # tester together with the group relationship works and reuses the person.
    attrs = {"email": email, "firstName": first_name}
    if last_name:
        attrs["lastName"] = last_name
    body = {"data": {"type": "betaTesters", "attributes": attrs,
                     "relationships": {"betaGroups": {"data": [{"type": "betaGroups", "id": gid}]}}}}
    try:
        request("POST", "/v1/betaTesters", body)
        print("invited", email)
    except APIError as e:
        if e.status == 409:
            print(f"{email}: already a tester under another app (409); add it to this app's group by hand "
                  "in App Store Connect › TestFlight")
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
        if EXTERNAL.get("privacy_url"):
            attrs["privacyPolicyUrl"] = EXTERNAL["privacy_url"]  # external testing requires one
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
                share_external(build)
                return
            if state in ("INVALID", "FAILED"):
                sys.exit(f"build {a['version']} finished as {state}: check App Store Connect for the reason")
        if time.time() > deadline:
            sys.exit(f"still {last or 'not listed'} after {minutes} min (builds appear 10–30 min after upload)")
        time.sleep(30)


def external_group_id() -> str:
    """The external group: invitation only (no public link), so only the emails added here can install."""
    for g in get_all(f"/v1/apps/{APP_ID}/betaGroups"):
        if g["attributes"]["name"] == EXTERNAL_GROUP:
            if g["attributes"].get("publicLinkEnabled"):
                request("PATCH", f"/v1/betaGroups/{g['id']}",
                        {"data": {"type": "betaGroups", "id": g["id"], "attributes": {"publicLinkEnabled": False}}})
                print("turned off the public link of", EXTERNAL_GROUP)
            return g["id"]
    print("create group", EXTERNAL_GROUP)
    body = {"data": {"type": "betaGroups",
                     "attributes": {"name": EXTERNAL_GROUP, "isInternalGroup": False, "publicLinkEnabled": False,
                                    "feedbackEnabled": True},
                     "relationships": {"app": {"data": {"type": "apps", "id": APP_ID}}}}}
    return request("POST", "/v1/betaGroups", body)["data"]["id"]


def write_review_details() -> bool:
    """Beta App Review's contact and notes; False when the config has no contact yet (review can't be submitted)."""
    r = EXTERNAL.get("review", {})
    missing = [k for k in ("first_name", "last_name", "phone", "email") if not r.get(k)]
    if missing:
        print("app_store.external.review is missing", ", ".join(missing), "- not submitting for Beta App Review")
        return False
    attrs = {"contactFirstName": r["first_name"], "contactLastName": r["last_name"], "contactPhone": r["phone"],
             "contactEmail": r["email"], "notes": r.get("notes", ""), "demoAccountRequired": bool(r.get("demo_account"))}
    if r.get("demo_account"):
        attrs["demoAccountName"] = r["demo_account"].get("name", "")
        attrs["demoAccountPassword"] = r["demo_account"].get("password", "")
    detail = request("GET", f"/v1/apps/{APP_ID}/betaAppReviewDetail")["data"]
    request("PATCH", f"/v1/betaAppReviewDetails/{detail['id']}",
            {"data": {"type": "betaAppReviewDetails", "id": detail["id"], "attributes": attrs}})
    print("beta app review contact written")
    return True


def submit_for_review(build):
    """Asks for Beta App Review; a build already submitted (or approved) answers 409/422, which is fine."""
    state = request("GET", f"/v1/builds/{build['id']}/buildBetaDetail")["data"]["attributes"].get("externalBuildState")
    if state in ("WAITING_FOR_BETA_REVIEW", "IN_BETA_REVIEW", "BETA_APPROVED", "IN_BETA_TESTING"):
        print("build", build["attributes"]["version"], "beta review:", state)
        return
    request("POST", "/v1/betaAppReviewSubmissions",
            {"data": {"type": "betaAppReviewSubmissions",
                      "relationships": {"build": {"data": {"type": "builds", "id": build["id"]}}}}})
    print("build", build["attributes"]["version"], "submitted for Beta App Review")


def share(build):
    """Put the build in the external group and ask for Beta App Review; invite the testers once it is submitted.

    Without a review contact the build only sits in the group and nobody is added: testers could not install
    it before the review anyway."""
    gid = external_group_id()
    linked = {b["id"] for b in get_all(f"/v1/betaGroups/{gid}/builds")}
    if build["id"] not in linked:
        request("POST", f"/v1/betaGroups/{gid}/relationships/builds", {"data": [{"type": "builds", "id": build["id"]}]})
        print("build", build["attributes"]["version"], "is now in", EXTERNAL_GROUP)
    if not write_review_details():
        print("testers are invited once a build is submitted")
        return
    submit_for_review(build)
    testers = EXTERNAL.get("testers", [])
    if not testers:
        print("no app_store.external.testers in the config: nobody invited yet")
    for t in testers:
        add_tester(gid, t["email"], t.get("first_name") or t["email"].split("@")[0], t.get("last_name", ""))


def share_external(build):
    """share() after a release, when the config has an external group; its errors don't fail the release."""
    if not EXTERNAL:
        return
    try:
        share(build)
    except APIError as e:
        print(f"external group {EXTERNAL_GROUP}: {e} (retry: python3 scripts/asc_testflight.py external)")


def external(build=None):
    build = build or newest_valid()
    if not build:
        sys.exit("no VALID build yet (python3 scripts/asc_testflight.py wait)")
    write_info(build, default_whats_new())
    share(build)


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
        share_external(build)
    elif args[:1] == ["external"]:
        external()
    elif args[:1] in ([], ["setup"]):
        gid = group_id()
        for t in TESTERS:
            add_tester(gid, t["email"], t.get("first_name") or t["email"].split("@")[0])
        status()


if __name__ == "__main__":
    try:
        main()
    except APIError as e:
        sys.exit(str(e))
