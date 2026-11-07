"""Rebuild prototypes/android/flows.html from the Mermaid blocks in docs/archive/redesign/flows.md.

The page embeds the rendered SVGs, so it opens offline without a CDN. Rendering needs Google Chrome and the
mermaid npm package (installed once into a cache directory):

    python3 prototypes/android/tools/build_flows.py

Exits non-zero if any diagram fails to parse.
"""
import html
import json
import os
import re
import subprocess
import sys
import tempfile

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
SRC = os.path.join(ROOT, "docs", "archive", "redesign", "flows.md")
OUT = os.path.join(ROOT, "prototypes", "android", "flows.html")
CHROME = os.environ.get("CHROME", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
CACHE = os.environ.get("MERMAID_CACHE", os.path.join(tempfile.gettempdir(), "persona-mermaid"))
MERMAID = os.path.join(CACHE, "node_modules", "mermaid", "dist", "mermaid.min.js")

THEME = {
    "primaryColor": "#EEF2FF", "primaryBorderColor": "#5B6EE8", "primaryTextColor": "#1C1C1E",
    "secondaryColor": "#F4F5F7", "tertiaryColor": "#FFFFFF", "lineColor": "#6B7280", "textColor": "#1C1C1E",
    "fontSize": "15px", "noteBkgColor": "#FFF8E6", "noteTextColor": "#1C1C1E", "noteBorderColor": "#E2B65A",
    "actorBkg": "#EEF2FF", "actorBorder": "#5B6EE8", "actorTextColor": "#1C1C1E", "signalColor": "#374151",
    "signalTextColor": "#1C1C1E", "labelBoxBkgColor": "#F4F5F7", "labelBoxBorderColor": "#9CA3AF",
    "labelTextColor": "#1C1C1E", "loopTextColor": "#1C1C1E", "edgeLabelBackground": "#FFFFFF",
    "clusterBkg": "#F7F7FA", "clusterBorder": "#C7CAD1",
}


def ensure_mermaid():
    if os.path.exists(MERMAID):
        return
    os.makedirs(CACHE, exist_ok=True)
    subprocess.run(["npm", "install", "--prefix", CACHE, "mermaid@11", "--no-audit", "--no-fund"], check=True, timeout=300)


def render(blocks):
    """Render every block in headless Chrome; returns [{ok, svg | error}]."""
    config = {
        "startOnLoad": False, "theme": "base", "securityLevel": "strict", "themeVariables": THEME,
        "fontFamily": '-apple-system, "PingFang SC", "Helvetica Neue", sans-serif',
        "flowchart": {"htmlLabels": True, "curve": "basis", "wrappingWidth": 320, "padding": 12},
        "sequence": {"mirrorActors": False, "messageFontSize": 15, "actorFontSize": 15, "noteFontSize": 14},
    }
    page = f"""<!doctype html><meta charset="utf-8">
<script src="file://{MERMAID}"></script>
<pre id="out">pending</pre>
<script>
mermaid.initialize({json.dumps(config)});
(async () => {{
  const blocks = {json.dumps(blocks, ensure_ascii=False)};
  const res = [];
  for (let i = 0; i < blocks.length; i++) {{
    try {{ res.push({{ok: true, svg: (await mermaid.render("m" + i, blocks[i])).svg}}); }}
    catch (e) {{ res.push({{ok: false, error: String(e && e.message || e)}}); }}
  }}
  document.getElementById("out").textContent = JSON.stringify(res);
}})();
</script>"""
    with tempfile.TemporaryDirectory() as tmp:
        path = os.path.join(tmp, "render.html")
        with open(path, "w", encoding="utf-8") as f:
            f.write(page)
        dom = subprocess.run([CHROME, "--headless=new", "--disable-gpu", "--allow-file-access-from-files",
                              "--virtual-time-budget=20000", "--dump-dom", "file://" + path],
                             capture_output=True, text=True, timeout=180).stdout
    m = re.search(r'<pre id="out">(.*?)</pre>', dom, re.S)
    if not m or m.group(1) == "pending":
        sys.exit("render did not finish")
    return json.loads(html.unescape(m.group(1)))


def inline(t):
    t = html.escape(t, quote=False)
    t = re.sub(r"\[([^\]]+)\]\([^)]+\)", r"\1", t)
    t = re.sub(r"`([^`]+)`", r"<code>\1</code>", t)
    return re.sub(r"\*\*([^*]+)\*\*", r"<b>\1</b>", t)


def page(md, svgs):
    sections = re.split(r"\n## ", md)
    intro = [line[2:] for line in sections[0].splitlines() if line.startswith("> ")]
    items = []
    for sec, svg in zip(sections[1:], svgs):
        title, body = sec.split("\n", 1)
        items.append(("f" + title.split(".", 1)[0], title, body.split("```mermaid", 1)[0].strip(), svg))
    toc = "".join(f'<a href="#{a}">{inline(t)}</a>' for a, t, _, _ in items)
    cards = "".join(f'<section id="{a}"><h2>{inline(t)}</h2><p class="cap">{inline(c)}</p><div class="fig">{s}</div></section>'
                    for a, t, c, s in items)
    head = "".join(f'<p class="cap">{inline(line)}</p>' for line in intro)
    return f"""<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Persona Android：主要功能流程图</title>
<style>
:root{{color-scheme:light dark;--bg:#f6f6f8;--fg:#1c1c1e;--sub:#6b6b70;--card:#fff;--line:#e3e3e8;--tint:#3a6df0}}
@media (prefers-color-scheme:dark){{:root{{--bg:#111214;--fg:#ececf0;--sub:#9a9aa2;--card:#1c1d20;--line:#2c2d31;--tint:#8aa8ff}}}}
*{{box-sizing:border-box}}
body{{margin:0;background:var(--bg);color:var(--fg);font:15px/1.6 -apple-system,"PingFang SC","Helvetica Neue",sans-serif}}
header{{padding:28px 24px 8px;max-width:1100px;margin:auto}}
h1{{font-size:24px;margin:0 0 6px}}
header .cap{{margin:4px 0}}
nav{{display:flex;flex-wrap:wrap;gap:6px 8px;max-width:1100px;margin:12px auto 0;padding:0 24px}}
nav a{{white-space:nowrap;color:var(--tint);text-decoration:none;border:1px solid var(--line);border-radius:999px;padding:3px 10px;font-size:13px;background:var(--card)}}
main{{max-width:1100px;margin:auto;padding:8px 24px 64px}}
section{{background:var(--card);border:1px solid var(--line);border-radius:16px;padding:18px 20px;margin:18px 0}}
h2{{font-size:18px;margin:0 0 4px}}
.cap{{color:var(--sub);margin:0 0 12px}}
code{{font:13px ui-monospace,SFMono-Regular,Menlo,monospace;background:rgba(127,127,127,.14);padding:1px 5px;border-radius:5px}}
.fig{{background:#fff;color:#1c1c1e;border-radius:12px;padding:12px;overflow-x:auto;text-align:center}}
.fig svg{{max-width:100%;height:auto}}
</style></head><body>
<header><h1>Persona Android：主要功能流程图</h1>{head}</header>
<nav>{toc}</nav>
<main>{cards}</main>
</body></html>"""


def main():
    md = open(SRC, encoding="utf-8").read()
    blocks = re.findall(r"```mermaid\n(.*?)```", md, re.S)
    ensure_mermaid()
    res = render(blocks)
    bad = [(i, r["error"]) for i, r in enumerate(res) if not r["ok"]]
    for i, err in bad:
        print(f"diagram {i + 1}: {err[:300]}", file=sys.stderr)
    if bad:
        sys.exit(1)
    with open(OUT, "w", encoding="utf-8") as f:
        f.write(page(md, [r["svg"] for r in res]))
    print(f"wrote {os.path.relpath(OUT, ROOT)}: {len(res)} diagrams")


if __name__ == "__main__":
    main()
