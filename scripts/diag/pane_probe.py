#!/usr/bin/env python3
"""Runs on one machine (stdin to `python3 -`): which session each herdr agent pane really runs, and whether a Codex
turn is open. Prints one JSON list. Used by live_panes_check.py; stdlib only, reads nothing but herdr and the
agents' own files.

Truth per pane:
  codex   the thread named in the terminal title ("<name> | <dir>") in ~/.codex/session_index.jsonl, its rollout
          the most recently written of that name; no name in the title -> unverifiable
  claude  herdr's agent_session id, else <config dir>/sessions/<pid>.json of the pane's claude process
"""
import glob, json, os, re, shutil, subprocess, sys, time

HOME = os.path.expanduser("~")


def herdr_bin():
    for p in (shutil.which("herdr"), HOME + "/.local/bin/herdr", "/opt/homebrew/bin/herdr", "/usr/local/bin/herdr"):
        if p and os.path.exists(p):
            return p
    sys.exit("herdr not found")


H = herdr_bin()


def herdr(*args):
    out = subprocess.run([H, *args], capture_output=True, text=True, timeout=20).stdout
    return json.loads(out)


def codex_titles(home):
    titles = {}
    try:
        with open(os.path.join(home, "session_index.jsonl")) as f:
            for line in f:
                try:
                    e = json.loads(line)
                except ValueError:
                    continue
                if e.get("id") and (e.get("thread_name") or "").strip():
                    titles[e["id"]] = e["thread_name"].strip()
    except OSError:
        pass
    return titles


def rollout(home, sid):
    m = glob.glob(os.path.join(home, "sessions", "*", "*", "*", "rollout-*-" + sid + ".jsonl"))
    return max(m, key=os.path.getmtime) if m else None


def turn_state(path):
    """The last turn event in the rollout's tail: 'open' after task_started, 'closed' after complete / aborted."""
    size = os.path.getsize(path)
    with open(path, "rb") as f:
        f.seek(max(0, size - (1 << 20)))
        tail = f.read()
    last = None
    for m in re.finditer(rb'"type":"(task_started|task_complete|turn_aborted)"', tail):
        last = m.group(1)
    return {None: None, b"task_started": "open"}.get(last, "closed")


def claude_dirs(pid):
    dirs = [HOME + "/.claude"]
    env = ""
    try:
        env = open(f"/proc/{pid}/environ", "rb").read().decode(errors="replace").replace("\0", " ")
    except OSError:
        env = subprocess.run(["ps", "eww", "-p", str(pid)], capture_output=True, text=True).stdout
    m = re.search(r"CLAUDE_CONFIG_DIR=(\S+)", env)
    if m:
        dirs.insert(0, m.group(1))
    return dirs


def pane_pids(pane):
    try:
        d = herdr("pane", "process-info", "--pane", pane)
        return [p["pid"] for p in d["result"]["process_info"]["foreground_processes"]]
    except Exception:
        return []


def main():
    agents = herdr("agent", "list")["result"]["agents"]
    chome = HOME + "/.codex"
    titles = codex_titles(chome)
    out = []
    for a in agents:
        r = {"pane": a["pane_id"], "agent": a["agent"], "herdr_status": a.get("agent_status"),
             "herdr_id": (a.get("agent_session") or {}).get("value")}
        title = a.get("terminal_title_stripped") or ""
        if a["agent"] == "codex":
            i = title.rfind(" | ")
            name = title[:i].strip() if i > 0 else ""
            cut = name[:-3] if name.endswith("...") else name[:-1] if name.endswith("…") else None
            ids = [s for s, t in titles.items() if name and (t == name or (cut and t.startswith(cut)))]
            paths = [(rollout(chome, s), s) for s in ids]
            paths = [(os.path.getmtime(p), s, p) for p, s in paths if p]
            if paths:
                _, sid, p = max(paths)
                r.update(truth=sid, how="title" if len(paths) == 1 else "title-newest", turn=turn_state(p),
                         age=round(time.time() - os.path.getmtime(p)))
            else:
                r.update(truth=None, how="no-name" if not name else "name-unknown")
        elif a["agent"] == "claude":
            if r["herdr_id"]:
                r.update(truth=r["herdr_id"], how="herdr")
            else:
                for pid in pane_pids(a["pane_id"]):
                    for d in claude_dirs(pid):
                        try:
                            r.update(truth=json.load(open(f"{d}/sessions/{pid}.json"))["sessionId"], how="pid")
                            break
                        except (OSError, ValueError, KeyError):
                            pass
                    if r.get("truth"):
                        break
                r.setdefault("truth", None)
                r.setdefault("how", "unknown")
        else:
            continue
        out.append(r)
    print(json.dumps(out))


main()
