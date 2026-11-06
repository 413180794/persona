#!/usr/bin/env python3
"""The README's pictures in docs/assets/readme/: banner, feature icons, badges, and the moving hero.

  python3 scripts/assets/make_readme_assets.py svg                  # banner, architecture, icons, badges
  python3 scripts/assets/make_readme_assets.py shoot <dir>          # screenshots + demo recordings from the simulator
  python3 scripts/assets/make_readme_assets.py hero <dir>           # hero-light.webp / hero-dark.webp from <dir>

`svg` reads the version floors from apple/project.yml, android/gradle/libs.versions.toml and server/go.mod, so the
badges follow the code, and puts the app icon's own picture (AppIcon.appiconset) in the banner; it needs Pillow. `shoot` drives a DEBUG build in demo mode (made-up data, no hub) that is already installed on
the simulator given by JARVISCTL_UDID, over the Runtime CLI (docs/runtime-cli.md): it relaunches the app with
`-demo.on YES`, takes every scene in light and dark, and records the demo once per appearance. `hero` needs ffmpeg
and Pillow; it puts the demo in the middle phone and four still screens beside it.
"""
import base64
import io
import math
import os
import re
import signal
import subprocess
import sys
import tempfile
import time

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
OUT = os.path.join(ROOT, "docs/assets/readme")
APP_ICON = os.path.join(ROOT, "apple/Persona/Resources/Assets.xcassets/AppIcon.appiconset")

# The app icon's colors (scripts/assets/make_icon.py) and the site's (site/public/home.css).
CORAL, PEACH, EMBER = "#ee7c54", "#ffc4a0", "#b03e22"
THEMES = {
    "light": dict(bg1="#fbf7f1", bg2="#f1e7da", glow="#f6b48f", glow_op=0.55, fg="#1c1814", muted="#6f665c",
                  card="#fffdf9", card_op=0.92, stroke="#e6d9c8", dot_op=0.07, dots="#b03e22",
                  spark=("#f6a57e", "#d4683f", "#a83e1f"), icon="icon.png", lift="#3c1e0a", lift_op=0.16),
    "dark": dict(bg1="#2a1913", bg2="#0c0b0a", glow="#ee7c54", glow_op=0.30, fg="#f3ede4", muted="#a89c8f",
                 card="#1d1915", card_op=0.88, stroke="#3b312a", dot_op=0.06, dots=PEACH,
                 spark=(PEACH, CORAL, EMBER), icon="icon-dark.png", lift="#000000", lift_op=0.5),
}
SERIF = "Georgia, 'Times New Roman', serif"
SERIF_CJK = "'Songti SC', 'Noto Serif SC', 'Source Han Serif SC', STSong, SimSun, serif"
SANS = "-apple-system, BlinkMacSystemFont, 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', 'Noto Sans SC', sans-serif"


def write(name, text):
    os.makedirs(OUT, exist_ok=True)
    with open(os.path.join(OUT, name), "w", encoding="utf-8") as f:
        f.write(text)
    print(os.path.relpath(os.path.join(OUT, name), ROOT))


def spark_path(cx, cy, r, n=96):
    """The icon's four-point spark: |x|^p + |y|^p = r^p with p < 1, as an SVG path."""
    exp = 2 / 0.62
    pts = []
    for i in range(n):
        a = 2 * math.pi * i / n
        c, s = math.cos(a), math.sin(a)
        pts.append((cx + math.copysign(abs(c) ** exp, c) * r, cy + math.copysign(abs(s) ** exp, s) * r))
    return "M" + " L".join(f"{x:.1f} {y:.1f}" for x, y in pts) + " Z"


def app_icon(theme, px=320):
    """The app icon's picture for the theme as a JPEG data URI: an SVG shown with <img> loads no other files."""
    from PIL import Image
    img = Image.open(os.path.join(APP_ICON, THEMES[theme]["icon"])).convert("RGB").resize((px, px), Image.LANCZOS)
    buf = io.BytesIO()
    img.save(buf, "JPEG", quality=85, optimize=True, progressive=True)
    return "data:image/jpeg;base64," + base64.b64encode(buf.getvalue()).decode()


def text_width(s, size):
    """Rough advance width: CJK and full-width marks one em, the rest a little over half."""
    w = 0.0
    for ch in s:
        if ord(ch) > 0x2E80:
            w += 1.0
        elif ch in " ·.,:":
            w += 0.32
        elif ch.isupper():
            w += 0.66
        else:
            w += 0.52
    return w * size


# ---------------------------------------------------------------- banner

# README.md is English and README.zh-CN.md Chinese: the banner and the architecture picture are drawn once per
# language. English keeps the plain file names (banner-light.svg), Chinese adds ".zh-CN" (banner-light.zh-CN.svg).
LANGS = {"en": "", "zh-CN": ".zh-CN"}

CHIPS = {  # the site's hero (site/public/index.html): demo data, not counts from anyone's hub
    "en": [
        ("#e2553a", "Needs you", "photo-sync · review plan", True),
        ("#e0a33a", "Working", "weather · 5 tools", False),
        (None, "Persona", "3 things need you today", False),
        ("#3f9b62", "Inbox", "AI twin drafted 2 replies", False),
    ],
    "zh-CN": [
        ("#e2553a", "需要你", "photo-sync · 计划等你审批", True),
        ("#e0a33a", "工作中", "天气 › widget · 工具 5", False),
        (None, "Persona", "今天有 3 件事需要你", False),
        ("#3f9b62", "收件箱", "AI 分身起草了 2 条回复", False),
    ],
}

TAGLINE = {  # (text, font family, size, letter spacing): English is longer, so smaller and without spacing
    "en": ("Every computer you own, in your pocket.", SERIF, 25, 0),
    "zh-CN": ("你的每台电脑，都在口袋里。", SERIF_CJK, 32, 2),
}


def banner(theme, lang="en"):
    t = THEMES[theme]
    tagline, tag_font, tag_size, tag_spacing = TAGLINE[lang]
    W, H = 1280, 320
    cx, cy, S = 176, 160, 200  # the app icon, a side S, corners as on the home screen
    x0, y0, r = cx - S // 2, cy - S // 2, S * 0.2237
    chips, y = [], 40
    for i, (dot, title, rest, pulse) in enumerate(CHIPS[lang]):
        x = 812 + (30, 64, 0, 44)[i]
        tw, rw = text_width(title, 20), text_width(rest, 19)
        w = 22 + 16 + 12 + tw + 14 + rw + 24
        h = 50
        if dot:
            mark = f'<circle cx="{x + 30}" cy="{y + h / 2}" r="7" fill="{dot}"/>'
            if pulse:
                mark += (f'<circle class="pulse" cx="{x + 30}" cy="{y + h / 2}" r="7" fill="none" stroke="{dot}" '
                         f'stroke-width="2"/>')
        else:
            mark = (f'<g transform="translate({x + 16} {y + 11}) scale({28 / S})" clip-path="url(#iconMask)">'
                    f'<use href="#icon"/></g>')
        chips.append(
            f'<g class="chip c{i}"><rect x="{x}" y="{y}" width="{w:.0f}" height="{h}" rx="{h / 2}" fill="{t["card"]}" '
            f'fill-opacity="{t["card_op"]}" stroke="{t["stroke"]}"/>{mark}'
            f'<text x="{x + 50}" y="{y + 32}" font-family="{SANS}" font-size="20" font-weight="600" fill="{t["fg"]}">{title}</text>'
            f'<text x="{x + 50 + tw + 14:.0f}" y="{y + 32}" font-family="{SANS}" font-size="19" fill="{t["muted"]}">{rest}</text></g>')
        y += 64
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" lang="{lang}">
<title>Persona</title>
<style>
  .pulse {{ transform-box: fill-box; transform-origin: center; animation: pulse 2.4s ease-out infinite; }}
  .chip {{ animation: float 7s ease-in-out infinite; }}
  .c1 {{ animation-delay: -1.8s; }} .c2 {{ animation-delay: -3.5s; }} .c3 {{ animation-delay: -5.2s; }}
  .halo {{ transform-origin: {cx}px {cy}px; animation: breathe 6s ease-in-out infinite; }}
  @keyframes pulse {{ from {{ transform: scale(1); opacity: .9; }} to {{ transform: scale(2.6); opacity: 0; }} }}
  @keyframes float {{ 0%, 100% {{ transform: translateY(0); }} 50% {{ transform: translateY(-4px); }} }}
  @keyframes breathe {{ 0%, 100% {{ opacity: .75; }} 50% {{ opacity: 1; }} }}
  @media (prefers-reduced-motion: reduce) {{ .pulse, .chip, .halo {{ animation: none; }} }}
</style>
<defs>
  <linearGradient id="bg" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="{t["bg1"]}"/><stop offset="1" stop-color="{t["bg2"]}"/></linearGradient>
  <radialGradient id="glow" cx="{cx}" cy="{cy}" r="260" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="{t["glow"]}" stop-opacity="{t["glow_op"]}"/><stop offset="1" stop-color="{t["glow"]}" stop-opacity="0"/></radialGradient>
  <pattern id="dots" width="22" height="22" patternUnits="userSpaceOnUse"><circle cx="2" cy="2" r="1.3" fill="{t["dots"]}" fill-opacity="{t["dot_op"]}"/></pattern>
  <filter id="lift" x="-30%" y="-30%" width="160%" height="170%"><feDropShadow dx="0" dy="10" stdDeviation="14" flood-color="{t["lift"]}" flood-opacity="{t["lift_op"]}"/></filter>
  <image id="icon" width="{S}" height="{S}" href="{app_icon(theme)}"/>
  <clipPath id="iconMask"><rect width="{S}" height="{S}" rx="{r:.1f}"/></clipPath>
  <clipPath id="frame"><rect width="{W}" height="{H}" rx="24"/></clipPath>
</defs>
<g clip-path="url(#frame)">
  <rect width="{W}" height="{H}" fill="url(#bg)"/>
  <rect width="{W}" height="{H}" fill="url(#dots)"/>
  <rect class="halo" width="{W}" height="{H}" fill="url(#glow)"/>
  <rect x="{x0}" y="{y0}" width="{S}" height="{S}" rx="{r:.1f}" fill="{t["bg1"]}" filter="url(#lift)"/>
  <g transform="translate({x0} {y0})" clip-path="url(#iconMask)"><use href="#icon"/></g>
  <rect x="{x0 + .5}" y="{y0 + .5}" width="{S - 1}" height="{S - 1}" rx="{r - .5:.1f}" fill="none" stroke="{t["stroke"]}"/>
  <text x="330" y="176" font-family="{SERIF}" font-size="104" font-weight="700" letter-spacing="-1" fill="{t["fg"]}">Persona</text>
  <text x="334" y="232" font-family="{tag_font}" font-size="{tag_size}" letter-spacing="{tag_spacing}" fill="{t["muted"]}">{tagline}</text>
  {"".join(chips)}
  <rect x=".5" y=".5" width="{W - 1}" height="{H - 1}" rx="24" fill="none" stroke="{t["stroke"]}"/>
</g>
</svg>
'''


# ---------------------------------------------------------------- feature icons

GLYPHS = {
    "agents": '<rect x="15" y="18" width="34" height="28" rx="5"/><path d="M22 28l5 4-5 4"/><path d="M31 37h10"/>',
    "approve": '<path d="M17 22a6 6 0 0 1 6-6h18a6 6 0 0 1 6 6v13a6 6 0 0 1-6 6H29l-8 6v-6h0a4 4 0 0 1-4-4z"/>'
               '<path d="M25 29l4.5 4.5L39 24"/>',
    "assistant": None,  # the spark, filled
    "inbox": '<path d="M16 35l5.5-15a3 3 0 0 1 2.8-2h15.4a3 3 0 0 1 2.8 2L48 35v9a3 3 0 0 1-3 3H19a3 3 0 0 1-3-3z"/>'
             '<path d="M16 35h9l3 5h8l3-5h9"/>',
    "island": '<rect x="21" y="12" width="22" height="40" rx="6"/><rect x="26" y="16" width="12" height="5" rx="2.5" '
              'fill="#fff" stroke="none"/><path d="M27 31h10M27 37h6"/>',
    "hub": '<path d="M32 13l15 5.5v11c0 9.5-6.4 16.8-15 20.5-8.6-3.7-15-11-15-20.5v-11z"/><path d="M25 31.5l5 5 9-10"/>',
}


def icon(name):
    glyph = GLYPHS[name]
    if glyph is None:
        glyph = (f'<path d="{spark_path(31, 33, 15, 64)}" fill="#fff" stroke="none"/>'
                 f'<path d="{spark_path(43.5, 20.5, 3.6, 32)}" fill="#fff" stroke="none"/>')
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 64 64">
<defs>
  <linearGradient id="g" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#f4956a"/><stop offset="1" stop-color="#c24a28"/></linearGradient>
  <linearGradient id="hl" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#fff" stop-opacity=".35"/><stop offset=".5" stop-color="#fff" stop-opacity="0"/></linearGradient>
</defs>
<rect width="64" height="64" rx="16" fill="url(#g)"/>
<rect x=".75" y=".75" width="62.5" height="62.5" rx="15.25" fill="url(#hl)" stroke="#fff" stroke-opacity=".18" stroke-width="1.5"/>
<g fill="none" stroke="#fff" stroke-width="3.2" stroke-linecap="round" stroke-linejoin="round">{glyph}</g>
</svg>
'''


# ---------------------------------------------------------------- badges

def versions():
    """The floors the code builds for: {label: value}."""
    def grab(path, pattern):
        with open(os.path.join(ROOT, path), encoding="utf-8") as f:
            m = re.search(pattern, f.read(), re.M)
        if not m:
            raise SystemExit(f"{path}: {pattern} not found")
        return m.group(1)

    ios = grab("apple/project.yml", r'^\s+iOS:\s*"?([\d.]+)')
    watch = grab("apple/project.yml", r'^\s+watchOS:\s*"?([\d.]+)')
    mac = grab("apple/project.yml", r'^\s+macOS:\s*"?([\d.]+)')
    sdk = int(grab("android/gradle/libs.versions.toml", r'^minSdk\s*=\s*"(\d+)"'))
    go = grab("server/go.mod", r"^go\s+([\d.]+)")
    android = {31: "12", 32: "12L", 33: "13", 34: "14", 35: "15", 36: "16"}.get(sdk, f"API {sdk}")
    short = lambda v: v[:-2] if v.endswith(".0") else v  # noqa: E731
    return [("iOS", short(ios) + "+"), ("watchOS", short(watch) + "+"), ("macOS", short(mac) + "+"),
            ("Android", android + "+"), ("hub", "Go " + ".".join(go.split(".")[:2])),
            ("agents", "Claude Code · Codex")]


VERDANA = {  # advance widths at 11px, shields.io's metric for Verdana
    " ": 3.9, ".": 3.9, "+": 9.2, "·": 3.9, "0": 7.0, "1": 7.0, "2": 7.0, "3": 7.0, "4": 7.0, "5": 7.0, "6": 7.0,
    "7": 7.0, "8": 7.0, "9": 7.0, "C": 7.7, "G": 8.5, "O": 8.7, "S": 7.6, "A": 7.5, "a": 6.7, "b": 6.9, "c": 5.8,
    "d": 6.9, "e": 6.6, "g": 6.9, "h": 7.0, "i": 3.0, "l": 3.0, "n": 7.0, "o": 6.7, "r": 4.7, "s": 5.8, "t": 4.3,
    "u": 7.0, "w": 9.0, "x": 6.6, "I": 4.6, "P": 7.4, "E": 7.0, "D": 8.5, "L": 6.1,
}


def vw(s):
    return sum(VERDANA.get(ch, 7.0) for ch in s)


def badge(label, value):
    lw, rw = vw(label) + 16, vw(value) + 16
    w, h = lw + rw, 22
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="{w:.0f}" height="{h}" viewBox="0 0 {w:.0f} {h}" role="img" aria-label="{label}: {value}">
<title>{label}: {value}</title>
<clipPath id="r"><rect width="{w:.0f}" height="{h}" rx="5"/></clipPath>
<g clip-path="url(#r)"><rect width="{lw:.0f}" height="{h}" fill="#3a322d"/><rect x="{lw:.0f}" width="{rw:.0f}" height="{h}" fill="#d4683f"/></g>
<g fill="#fff" font-family="Verdana, 'DejaVu Sans', Geneva, sans-serif" font-size="11" text-anchor="middle">
<text x="{lw / 2:.1f}" y="15" textLength="{vw(label):.0f}">{label}</text>
<text x="{lw + rw / 2:.1f}" y="15" textLength="{vw(value):.0f}" font-weight="bold">{value}</text>
</g>
</svg>
'''


# ---------------------------------------------------------------- architecture

TILE_GLYPHS = {  # drawn around (0, 0), about 26 wide, stroked in the accent
    "phone": '<rect x="-8" y="-13" width="16" height="26" rx="4"/><path d="M-3 -9.5h6"/>',
    "desktop": '<rect x="-12" y="-10" width="24" height="16" rx="2.5"/><path d="M-4 11h8M0 6v5"/>',
    "android": '<path d="M-11 7a11 11 0 0 1 22 0z"/><path d="M-6.5 -5.5l-3 -4.5M6.5 -5.5l3 -4.5"/>'
               '<circle cx="-4" cy="1.5" r="1.1" fill="currentColor"/><circle cx="4" cy="1.5" r="1.1" fill="currentColor"/>',
    "laptop": '<rect x="-10" y="-9" width="20" height="13" rx="2"/><path d="M-13.5 8h27"/>',
    "server": '<rect x="-11" y="-11" width="22" height="9" rx="2"/><rect x="-11" y="2" width="22" height="9" rx="2"/>'
              '<circle cx="-6" cy="-6.5" r="1.1" fill="currentColor"/><circle cx="-6" cy="6.5" r="1.1" fill="currentColor"/>',
}


ARCH = {
    "en": dict(
        title="How Persona fits together: your devices connect to the hub, the hub to your computers and connectors",
        devices=(("iPhone · Apple Watch", "App, Live Activity, watch"), ("Mac · Web", "Web app hosted by the hub"),
                 ("Android", "No push service needed")),
        machine="An always-on machine", parts=("Assistant", "Inbox", "Knowledge", "Postgres"),
        duties=("Relays chats · tracks requests", "Runs the assistant · gets messages",
                "Reaches you over APNs when closed"),
        connectors="Connectors", sources=("Lark", "Mail", "GitHub", "Telegram"),
        ssh="SSH or Persona Link", apns="APNs push"),
    "zh-CN": dict(
        title="Persona 的结构：你的设备连 hub，hub 连你的各台电脑和连接器",
        devices=(("iPhone · Apple Watch", "App、灵动岛和手表"), ("Mac · 网页", "网页版由 hub 托管"),
                 ("Android", "同一套协议，不靠推送服务")),
        machine="一台常开的机器", parts=("助手", "收件箱", "知识库", "Postgres"),
        duties=("转发对话 · 管理要你回答的事", "运行助手 · 收消息和邮件", "App 关着时经 APNs 提醒你"),
        connectors="连接器", sources=("飞书", "邮件", "GitHub", "Telegram"),
        ssh="SSH 或 Persona Link", apns="APNs 推送"),
}


def architecture(theme, lang="en"):
    t = THEMES[theme]
    a = ARCH[lang]
    accent = "#d4683f" if theme == "light" else "#ef7c4f"
    line = "#dccbb7" if theme == "light" else "#4a3d33"
    shadow = "#3c1e0a" if theme == "light" else "#000000"
    shadow_op = 0.10 if theme == "light" else 0.45
    W, H = 1280, 620
    out = []

    def tile(x, y, glyph):
        return (f'<rect x="{x}" y="{y}" width="44" height="44" rx="12" fill="{accent}" fill-opacity=".14"/>'
                f'<g transform="translate({x + 22} {y + 22})" fill="none" stroke="{accent}" stroke-width="2.2" '
                f'stroke-linecap="round" stroke-linejoin="round" color="{accent}">{TILE_GLYPHS[glyph]}</g>')

    def card(x, y, w, h, stroke=None, width=1, opacity=1):
        return (f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="18" fill="{t["card"]}" stroke="{stroke or t["stroke"]}" '
                f'stroke-width="{width}" stroke-opacity="{opacity}" filter="url(#shadow)"/>')

    def text(x, y, s, size, weight=400, color=None, anchor="start"):
        return (f'<text x="{x}" y="{y}" font-family="{SANS}" font-size="{size}" font-weight="{weight}" '
                f'fill="{color or t["fg"]}" text-anchor="{anchor}">{s}</text>')

    def chip(x, y, s, size=15, strong=False):
        w = text_width(s, size) + 18
        fill = f'fill="{accent}" fill-opacity=".14"' if strong else f'fill="{t["bg2"]}" fill-opacity="{".5" if theme == "light" else ".9"}"'
        return (w, f'<rect x="{x:.0f}" y="{y}" width="{w:.0f}" height="{size + 13}" rx="{(size + 13) / 2}" {fill} '
                   f'stroke="{accent if strong else t["stroke"]}" stroke-opacity="{".5" if strong else "1"}"/>'
                   + text(f"{x + w / 2:.0f}", y + size + 2, s, size, 500, accent if strong else t["fg"], "middle"))

    def chips(x, y, items, size=15, strong=False, gap=8):
        parts = []
        for s in items:
            w, svg = chip(x, y, s, size, strong)
            parts.append(svg)
            x += w + gap
        return "".join(parts)

    def label(cx, cy, s, size=14):
        w = text_width(s, size) + 18
        return (f'<rect x="{cx - w / 2:.0f}" y="{cy - 13}" width="{w:.0f}" height="26" rx="13" fill="{t["card"]}" '
                f'stroke="{accent}" stroke-opacity=".55"/>' + text(cx, cy + 5, s, size, 500, accent, "middle"))

    # Wires first (under the cards): a quiet base line, and accent dots running along it.
    wires = [
        "M330 158 C400 158 400 220 470 220", "M330 274 L470 274", "M330 390 C400 390 400 328 470 328",  # devices -> hub
        "M810 220 C885 220 885 189 960 189", "M810 328 C885 328 885 359 960 359",                       # hub -> computers
        "M640 500 L640 438",                                                                             # connectors -> hub
    ]
    for d in wires:
        out.append(f'<path d="{d}" fill="none" stroke="{line}" stroke-width="2"/>')
        out.append(f'<path class="flow" d="{d}" fill="none" stroke="{accent}" stroke-width="3" stroke-linecap="round" '
                   f'stroke-dasharray="1 15"/>')
    apns = "M640 110 C640 40 180 40 180 110"
    out.append(f'<path d="{apns}" fill="none" stroke="{accent}" stroke-opacity=".55" stroke-width="2" stroke-dasharray="6 7"/>')
    out.append(f'<path class="flow slow" d="{apns}" fill="none" stroke="{accent}" stroke-width="3" stroke-linecap="round" '
               f'stroke-dasharray="1 15"/>')

    # Your devices.
    for (y, glyph), (title, sub) in zip(((110, "phone"), (226, "desktop"), (342, "android")), a["devices"]):
        out += [card(30, y, 300, 96), tile(50, y + 26, glyph), text(110, y + 43, title, 20, 600),
                text(110, y + 70, sub, 16, 400, t["muted"])]

    # The hub.
    out.append(f'<rect x="462" y="102" width="356" height="344" rx="24" fill="{accent}" fill-opacity=".18" filter="url(#glow)"/>')
    out.append(card(470, 110, 340, 328, stroke=accent, width=1.5, opacity=0.7))
    out.append(f'<path d="{spark_path(512, 152, 18, 64)}" fill="url(#spark)"/>')
    out += [text(546, 150, "personad hub", 24, 700), text(546, 176, a["machine"], 16, 400, t["muted"])]
    for i, s in enumerate(a["parts"]):
        cx, cy = 490 + (i % 2) * 154, 214 + (i // 2) * 58
        out.append(f'<rect x="{cx}" y="{cy}" width="146" height="46" rx="14" fill="{accent}" fill-opacity=".10" '
                   f'stroke="{accent}" stroke-opacity=".35"/>' + text(cx + 73, cy + 29, s, 18, 600, t["fg"], "middle"))
    out += [text(640, 362 + 26 * i, s, 15, 400, t["muted"], "middle") for i, s in enumerate(a["duties"])]

    # Your computers.
    for y, glyph, title, items in ((130, "laptop", "Mac", ("Claude Code", "iMessage")),
                                   (300, "server", "Linux", ("Codex",))):
        out += [card(960, y, 290, 118), tile(980, y + 20, glyph), text(1040, y + 40, title, 20, 600),
                text(1040, y + 64, "personad node · herdr", 15, 400, t["muted"]), chips(1040, y + 78, items)]

    # Connectors.
    out += [card(470, 500, 340, 96), text(490, 534, a["connectors"], 18, 600),
            chips(490, 550, a["sources"], 15, True)]

    # Wire labels on top.
    out += [label(400, 274, "HTTPS / WSS"), label(885, 274, a["ssh"], 13), label(410, 57, a["apns"])]

    s1, s2, s3 = t["spark"]
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" lang="{lang}">
<title>{a["title"]}</title>
<style>
  .flow {{ animation: flow 1.6s linear infinite; }}
  .slow {{ animation-duration: 3.2s; }}
  @keyframes flow {{ to {{ stroke-dashoffset: -32; }} }}
  @media (prefers-reduced-motion: reduce) {{ .flow {{ animation: none; }} }}
</style>
<defs>
  <linearGradient id="bg" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="{t["bg1"]}"/><stop offset="1" stop-color="{t["bg2"]}"/></linearGradient>
  <radialGradient id="halo" cx="640" cy="274" r="420" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="{t["glow"]}" stop-opacity="{t["glow_op"] * 0.7:.2f}"/><stop offset="1" stop-color="{t["glow"]}" stop-opacity="0"/></radialGradient>
  <linearGradient id="spark" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="{s1}"/><stop offset=".55" stop-color="{s2}"/><stop offset="1" stop-color="{s3}"/></linearGradient>
  <pattern id="dots" width="22" height="22" patternUnits="userSpaceOnUse"><circle cx="2" cy="2" r="1.3" fill="{t["dots"]}" fill-opacity="{t["dot_op"]}"/></pattern>
  <filter id="shadow" x="-10%" y="-10%" width="120%" height="130%"><feDropShadow dx="0" dy="6" stdDeviation="8" flood-color="{shadow}" flood-opacity="{shadow_op}"/></filter>
  <filter id="glow" x="-20%" y="-20%" width="140%" height="140%"><feGaussianBlur stdDeviation="14"/></filter>
  <clipPath id="frame"><rect width="{W}" height="{H}" rx="24"/></clipPath>
</defs>
<g clip-path="url(#frame)">
  <rect width="{W}" height="{H}" fill="url(#bg)"/>
  <rect width="{W}" height="{H}" fill="url(#dots)"/>
  <rect width="{W}" height="{H}" fill="url(#halo)"/>
  {"".join(out)}
  <rect x=".5" y=".5" width="{W - 1}" height="{H - 1}" rx="24" fill="none" stroke="{t["stroke"]}"/>
</g>
</svg>
'''


def cmd_svg():
    for theme in THEMES:
        for lang, suffix in LANGS.items():
            write(f"banner-{theme}{suffix}.svg", banner(theme, lang))
            write(f"architecture-{theme}{suffix}.svg", architecture(theme, lang))
    for name in GLYPHS:
        write(f"icon-{name}.svg", icon(name))
    for label, value in versions():
        write(f"badge-{label.lower()}.svg", badge(label, value))


# ---------------------------------------------------------------- shoot (simulator, demo mode)

DEMO_MAIN = "server/b2e4d6f8-0000-4000-8000-000000000001"  # apple/Persona/Resources/Demo.json
STILLS = {
    "plan": dict(scene="chat", session="studio/a1f3c2d4-0000-4000-8000-000000000005"),     # ExitPlanMode
    "working": dict(scene="chat", session="macbook/a1f3c2d4-0000-4000-8000-000000000001"),  # working, streaming
    "inbox": dict(scene="inbox"),
    "machines": dict(scene="machines"),
}


def ctl():
    sys.path.insert(0, os.path.join(ROOT, "scripts/app"))
    from jarvisctl import Ctl
    udid = os.environ.get("JARVISCTL_UDID")
    if not udid:
        raise SystemExit("set JARVISCTL_UDID to the simulator with the DEBUG build")
    return Ctl(udid=udid, bundle=os.environ.get("PERSONA_BUNDLE_ID"), port=int(os.environ.get("JARVISCTL_PORT", 18950)))


def fresh(c, appearance):
    """Demo mode from the shipped data again, chat caches dropped (an earlier run's answer would show otherwise)."""
    c.launch(extra=["-demo.on", "YES", "-openWelcome", "YES", "-openChatsFresh", "YES"])
    subprocess.run(["xcrun", "simctl", "ui", c.udid, "appearance", appearance], check=True)


def still(c, args, path):
    c.call("navigate", coldChats=True, **args)
    time.sleep(4.5)  # the demo mode's "not really sent" toast fades after about 4 s
    if args["scene"] in ("chat", "assistant"):
        c.call("wait-loaded", timeout=10)
    c.call("settle", pixels=800, timeout=8)
    subprocess.run(["xcrun", "simctl", "io", c.udid, "screenshot", path], check=True, capture_output=True)
    print(path)


def demo(c, path):
    """The assistant's day, over to Agents, open the Claude that asks, answer it, back to the list."""
    c.call("navigate", scene="assistant", session=DEMO_MAIN, coldChats=True)
    time.sleep(5)
    c.call("wait-loaded", timeout=10)
    c.call("settle", pixels=800, timeout=8)
    rec = subprocess.Popen(["xcrun", "simctl", "io", c.udid, "recordVideo", "--codec=h264", "--force", path],
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    time.sleep(3.2)
    c.tap(id="shell-agents")
    time.sleep(2.2)
    c.tap(labelContains="补齐网络层的单元测试、", trait="button")
    time.sleep(2.6)
    c.tap(labelContains="Swift Testing", trait="button", visibleOnly=True)
    time.sleep(3.4)
    c.tap(label="返回", trait="button")
    time.sleep(2.0)
    rec.send_signal(signal.SIGINT)
    rec.wait(timeout=30)
    print(path)


def cmd_shoot(out):
    os.makedirs(out, exist_ok=True)
    c = ctl()
    for appearance in ("light", "dark"):
        fresh(c, appearance)
        for name, args in STILLS.items():
            still(c, args, os.path.join(out, f"{name}-{appearance}.png"))
        fresh(c, appearance)
        demo(c, os.path.join(out, f"demo-{appearance}.mp4"))


# ---------------------------------------------------------------- hero (animated WebP)

HERO_W, HERO_H, FPS = 1800, 1000, 12
# (screen, screen width, center x, top y); drawn back to front, the demo in the middle on top.
SIDES = [("inbox", 262, 205, 262), ("machines", 262, 1595, 262), ("plan", 322, 525, 168), ("working", 322, 1275, 168)]
MID_W, MID_TOP = 392, 52
BEZEL = 12
SCREEN_RADIUS = 55 * 3 / 1206  # the display's corner (55 pt on a 402 pt wide phone), per pixel of screen width


def hero(shots, theme, frames_dir):
    from PIL import Image, ImageDraw, ImageFilter

    t = THEMES[theme]
    hexrgb = lambda h: tuple(int(h[i:i + 2], 16) for i in (1, 3, 5))  # noqa: E731

    def rounded(size, r):
        m = Image.new("L", size, 0)
        ImageDraw.Draw(m).rounded_rectangle([0, 0, size[0] - 1, size[1] - 1], radius=r, fill=255)
        return m

    def screen(img, w):
        img = img.convert("RGB").resize((w, round(w * img.height / img.width)), Image.LANCZOS)
        return img, rounded(img.size, round(w * SCREEN_RADIUS))

    # Panel: the banner's gradient, a glow behind the middle phone, rounded corners.
    a, b = hexrgb(t["bg1"]), hexrgb(t["bg2"])
    small = Image.new("RGB", (HERO_W // 20, HERO_H // 20))
    px = small.load()
    for y in range(small.height):
        for x in range(small.width):
            k = (x / small.width + y / small.height) / 2
            px[x, y] = tuple(round(a[i] + (b[i] - a[i]) * k) for i in range(3))
    panel = small.resize((HERO_W, HERO_H), Image.BILINEAR)
    glow = Image.new("L", (HERO_W, HERO_H), 0)
    ImageDraw.Draw(glow).ellipse([HERO_W / 2 - 520, 120, HERO_W / 2 + 520, 980], fill=255)
    glow = glow.filter(ImageFilter.GaussianBlur(140)).point(lambda v: int(v * t["glow_op"]))
    panel = Image.composite(Image.new("RGB", panel.size, hexrgb(t["glow"])), panel, glow).convert("RGBA")

    bezel_col = (24, 24, 26, 255) if theme == "light" else (52, 50, 54, 255)
    rim_col = (90, 88, 92, 255) if theme == "light" else (98, 94, 100, 255)
    shadow_a = 70 if theme == "light" else 150

    def device(canvas, w, cx, top, content=None, dim=0.0):
        h = round(w * 2622 / 1206)
        r = round(w * SCREEN_RADIUS)
        dw, dh, dr = w + 2 * BEZEL, h + 2 * BEZEL, r + BEZEL
        x0 = round(cx - dw / 2)
        sh = Image.new("RGBA", canvas.size, (0, 0, 0, 0))
        ImageDraw.Draw(sh).rounded_rectangle([x0, top + 18, x0 + dw, top + dh + 18], radius=dr, fill=(30, 12, 4, shadow_a))
        canvas.alpha_composite(sh.filter(ImageFilter.GaussianBlur(26)))
        body = Image.new("RGBA", (dw, dh), (0, 0, 0, 0))
        d = ImageDraw.Draw(body)
        d.rounded_rectangle([0, 0, dw - 1, dh - 1], radius=dr, fill=rim_col)
        d.rounded_rectangle([2, 2, dw - 3, dh - 3], radius=dr - 2, fill=bezel_col)
        canvas.alpha_composite(body, (x0, top))
        if content is not None:
            img, mask = screen(content, w)
            if dim:
                img = Image.blend(img, Image.new("RGB", img.size, hexrgb(t["bg2"])), dim)
            canvas.paste(img, (x0 + BEZEL, top + BEZEL), mask)
        return (x0 + BEZEL, top + BEZEL)

    base = panel.copy()
    for name, w, cx, top in SIDES:
        device(base, w, cx, top, Image.open(os.path.join(shots, f"{name}-{theme}.png")), dim=0.12 if w < 300 else 0.04)
    at = device(base, MID_W, HERO_W / 2, MID_TOP)
    corner = rounded((HERO_W, HERO_H), 40)

    # The demo, frame by frame, in the middle screen; a short crossfade from the end back to the start.
    for f in os.listdir(frames_dir):
        os.remove(os.path.join(frames_dir, f))
    subprocess.run(["ffmpeg", "-loglevel", "error", "-i", os.path.join(shots, f"demo-{theme}.mp4"),
                    "-vf", f"fps={FPS},scale={MID_W}:-1:flags=lanczos", os.path.join(frames_dir, "%04d.png")], check=True)
    names = sorted(os.listdir(frames_dir))
    clips = [Image.open(os.path.join(frames_dir, n)).convert("RGB") for n in names]
    fade = FPS // 2
    for i in range(fade):
        clips[-fade + i] = Image.blend(clips[-fade + i], clips[0], (i + 1) / (fade + 1))
    mask = rounded(clips[0].size, round(MID_W * SCREEN_RADIUS))
    frames = []
    for clip in clips:
        f = base.copy()
        f.paste(clip, at, mask)
        out = Image.new("RGBA", f.size, (0, 0, 0, 0))
        out.paste(f, (0, 0), corner)
        frames.append(out)
    path = os.path.join(OUT, f"hero-{theme}.webp")
    frames[0].save(path, save_all=True, append_images=frames[1:], duration=round(1000 / FPS), loop=0,
                   quality=72, method=6, minimize_size=True)
    print(f"{os.path.relpath(path, ROOT)}: {len(frames)} frames, {os.path.getsize(path) / 1e6:.2f} MB")


def cmd_hero(shots):
    with tempfile.TemporaryDirectory() as tmp:
        for theme in THEMES:
            hero(shots, theme, tmp)


def main():
    if len(sys.argv) < 2 or sys.argv[1] not in ("svg", "shoot", "hero"):
        raise SystemExit(__doc__)
    cmd = sys.argv[1]
    if cmd == "svg":
        cmd_svg()
    elif len(sys.argv) < 3:
        raise SystemExit(f"{cmd} needs a directory")
    elif cmd == "shoot":
        cmd_shoot(sys.argv[2])
    else:
        cmd_hero(sys.argv[2])


if __name__ == "__main__":
    main()
