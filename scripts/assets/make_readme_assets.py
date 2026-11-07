#!/usr/bin/env python3
"""The README's pictures in docs/assets/readme/: banner, feature icons, badges, and the moving hero.

  python3 scripts/assets/make_readme_assets.py svg                  # banner, architecture, icons, badges
  python3 scripts/assets/make_readme_assets.py shoot <dir>          # screenshots + demo recordings from the simulator
  python3 scripts/assets/make_readme_assets.py hero <dir>           # hero-light.webp / hero-dark.webp from <dir>

`svg` reads the version floors from apple/project.yml, android/gradle/libs.versions.toml and server/go.mod, so the
badges follow the code, and puts the app icon's own picture (AppIcon.appiconset) in the banner; it needs Pillow. `shoot` drives a DEBUG build in demo mode (made-up data, no hub) that is already installed on
the simulator given by PERSONACTL_UDID, over the Runtime CLI (docs/runtime-cli.md): it relaunches the app with
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

CHIPS = {  # Illustrative tasks, not activity from a real hub.
    "en": [
        (None, "SuperAgent", "One conversation", False),
        ("#e0a33a", "Delegate", "Ship the fix", False),
        ("#3f9b62", "Connect", "Calendar · Inbox", False),
        ("#e2553a", "Your call", "Approve a plan", True),
    ],
    "zh-CN": [
        (None, "SuperAgent", "一个入口", False),
        ("#e0a33a", "交给 AI", "把问题解决", False),
        ("#3f9b62", "连接", "日历 · 收件箱", False),
        ("#e2553a", "你来拍板", "批准执行计划", True),
    ],
}

TAGLINE = {  # (text, font family, size, letter spacing)
    "en": ("One place. Every task.", SERIF, 32, 0),
    "zh-CN": ("一个入口，解决所有问题。", SERIF_CJK, 32, 1),
}
SUPERAGENT = {"en": "Your SuperAgent", "zh-CN": "你的 SuperAgent"}


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
  <text x="334" y="92" font-family="{SANS}" font-size="18" font-weight="600" letter-spacing="2" fill="{t["muted"]}">{SUPERAGENT[lang]}</text>
  <text x="330" y="184" font-family="{SERIF}" font-size="104" font-weight="700" letter-spacing="-1" fill="{t["fg"]}">Persona</text>
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
    "handoff": '<rect x="12" y="16" width="29" height="23" rx="4"/><path d="M20 45h13M26 39v6"/>'
               '<rect x="38" y="26" width="14" height="26" rx="4"/><path d="M43 31h4M43 47h4"/>',
    "extensions": '<path d="M18 17h10v-3a5 5 0 0 1 10 0v3h10v11h-3a5 5 0 0 0 0 10h3v10H37v-3a5 5 0 0 0-10 0v3H18V37h-3a5 5 0 0 1 0-10h3z"/>',
    "phone-data": '<rect x="18" y="12" width="28" height="40" rx="6"/><path d="M27 17h10"/>'
                  '<path d="M24 30h5l3-6 4 13 3-7h3M29 46h6"/>',
    "connectors": '<circle cx="32" cy="32" r="7"/><circle cx="16" cy="16" r="4"/>'
                  '<circle cx="48" cy="16" r="4"/><circle cx="16" cy="48" r="4"/><circle cx="48" cy="48" r="4"/>'
                  '<path d="M19 19l8 8M37 27l8-8M19 45l8-8M37 37l8 8"/>',
    "clients": '<rect x="12" y="14" width="40" height="28" rx="4"/><path d="M24 50h16M32 42v8"/>',
    "hub": '<path d="M32 13l15 5.5v11c0 9.5-6.4 16.8-15 20.5-8.6-3.7-15-11-15-20.5v-11z"/><path d="M25 31.5l5 5 9-10"/>',
}


def icon(name):
    glyph = GLYPHS[name]
    if glyph is None:
        glyph = (f'<path d="{spark_path(31, 33, 15, 64)}" fill="{EMBER}" stroke="none"/>'
                 f'<path d="{spark_path(43.5, 20.5, 3.6, 32)}" fill="{EMBER}" stroke="none"/>')
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 64 64">
<rect x=".75" y=".75" width="62.5" height="62.5" rx="15.25" fill="{PEACH}" stroke="{CORAL}" stroke-opacity=".45" stroke-width="1.5"/>
<g fill="none" stroke="{EMBER}" stroke-width="3.2" stroke-linecap="round" stroke-linejoin="round">{glyph}</g>
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
    android = {30: "11", 31: "12", 32: "12L", 33: "13", 34: "14", 35: "15", 36: "16"}.get(sdk, f"API {sdk}")
    short = lambda v: v[:-2] if v.endswith(".0") else v  # noqa: E731
    return [("iOS", short(ios) + "+"), ("watchOS", short(watch) + "+"), ("macOS", short(mac) + "+"),
            ("Android", android + "+"), ("hub", "Go " + ".".join(go.split(".")[:2]))]


AGENT_BADGES = ("Claude Code", "Codex", "DSH", "pi", "opencode")
ENGINE_BADGES = ("herdr", "Herdr App", "Orca", "Paseo")
RUNTIME_BADGES = AGENT_BADGES + ENGINE_BADGES
CLIENTS_BADGE = "iPhone · Apple Watch · Mac · Vision Pro · Android · Web"
SUPPORT_BADGES = {
    "Clients": CLIENTS_BADGE,
    "Agents": " · ".join(AGENT_BADGES),
    "Engines": " · ".join(ENGINE_BADGES),
}


VERDANA = {  # advance widths at 11px, shields.io's metric for Verdana
    " ": 3.9, ".": 3.9, "+": 9.2, "·": 3.9, "0": 7.0, "1": 7.0, "2": 7.0, "3": 7.0, "4": 7.0, "5": 7.0, "6": 7.0,
    "7": 7.0, "8": 7.0, "9": 7.0, "C": 7.7, "G": 8.5, "O": 8.7, "S": 7.6, "A": 7.5, "a": 6.7, "b": 6.9, "c": 5.8,
    "d": 6.9, "e": 6.6, "g": 6.9, "h": 7.0, "i": 3.0, "l": 3.0, "n": 7.0, "o": 6.7, "r": 4.7, "s": 5.8, "t": 4.3,
    "u": 7.0, "w": 9.0, "x": 6.6, "I": 4.6, "P": 7.4, "E": 7.0, "D": 8.5, "L": 6.1,
}


def vw(s):
    return sum(VERDANA.get(ch, 7.0) for ch in s)


def badge(label, value=""):
    lw, rw = vw(label) + 16, vw(value) + 16 if value else 0
    w, h = lw + rw, 22
    accessible = f"{label}: {value}" if value else label
    value_text = (f'<text x="{lw + rw / 2:.1f}" y="15" textLength="{vw(value):.0f}" font-weight="bold">{value}</text>'
                  if value else "")
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="{w:.0f}" height="{h}" viewBox="0 0 {w:.0f} {h}" role="img" aria-label="{accessible}">
<title>{accessible}</title>
<clipPath id="r"><rect width="{w:.0f}" height="{h}" rx="5"/></clipPath>
<g clip-path="url(#r)"><rect width="{lw:.0f}" height="{h}" fill="#3a322d"/><rect x="{lw:.0f}" width="{rw:.0f}" height="{h}" fill="#d4683f"/></g>
<g fill="#fff" font-family="Verdana, 'DejaVu Sans', Geneva, sans-serif" font-size="11" text-anchor="middle">
<text x="{lw / 2:.1f}" y="15" textLength="{vw(label):.0f}">{label}</text>
{value_text}
</g>
</svg>
'''


def support_badge(label):
    """Three equally sized support rows, with the category labels aligned."""
    value = SUPPORT_BADGES[label]
    advance = vw(value) * 12 / 11
    left = 76
    width = math.ceil(max(vw(s) * 12 / 11 for s in SUPPORT_BADGES.values()) + left + 24)
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="26" viewBox="0 0 {width} 26" role="img" aria-label="{label}: {value}">
<title>{label}: {value}</title>
<clipPath id="r"><rect x=".5" y=".5" width="{width - 1}" height="25" rx="5"/></clipPath>
<g clip-path="url(#r)"><rect width="{width}" height="26" fill="#fffdf9"/><rect width="{left}" height="26" fill="{PEACH}"/></g>
<rect x=".5" y=".5" width="{width - 1}" height="25" rx="5" fill="none" stroke="#e6d9c8"/>
<g font-family="Verdana, 'DejaVu Sans', Geneva, sans-serif" font-size="12">
  <text x="{left / 2}" y="17" text-anchor="middle" font-weight="bold" fill="{EMBER}">{label}</text>
  <text x="{left + 12}" y="17" textLength="{advance:.1f}" fill="#49362a">{value}</text>
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
        title="Your devices, context and Agents come together in Persona",
        devices=(("iPhone · Apple Watch", "Ask, answer and keep up"), ("Mac · Web", "Pick up where you left off"),
                 ("Android · Vision Pro", "The same work, another screen")),
        machine="Your context. Your decisions.", parts=("Assistant", "Inbox", "Knowledge", "Tasks"),
        duties=("Bring the context together", "Decide what happens next", "Follow the work through"),
        connectors="Your context", sources=("Messages", "Mail", "Calendar", "Photos"),
        computers="Your computers", agents="Where your Agents work", ask="Ask · decide", work="Delegate"),
    "zh-CN": dict(
        title="设备、上下文和 Agent，在 Persona 汇合",
        devices=(("iPhone · Apple Watch", "提问、回答，随时掌握进度"), ("Mac · Web", "回到桌前，接着同一件事"),
                 ("Android · Vision Pro", "换一块屏幕，工作继续")),
        machine="你的上下文，你来决定", parts=("助手", "收件箱", "知识库", "任务"),
        duties=("串起相关信息", "决定下一步怎么做", "跟进每件事的进展"),
        connectors="与你有关的信息", sources=("消息", "邮件", "日历", "照片"),
        computers="你的电脑", agents="Agent 在这里动手", ask="提问 · 决策", work="安排任务"),
}


def architecture(theme, lang="en"):
    """A product overview, not a deployment diagram; deliberately quiet in either appearance."""
    a = ARCH[lang]
    W, H = 1280, 540
    ink, muted, line = "#1e293b", "#64748b", "#94a3b8"
    out = []

    def text(x, y, s, size=16, weight=400, color=ink, anchor="start"):
        return (f'<text x="{x}" y="{y}" font-family="{SANS}" font-size="{size}" font-weight="{weight}" '
                f'fill="{color}" text-anchor="{anchor}">{s}</text>')

    def card(x, y, w, h, fill="#f8fafc", stroke="#cbd5e1", dashed=False):
        dash = ' stroke-dasharray="6 3"' if dashed else ""
        return f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="16" fill="{fill}" stroke="{stroke}"{dash}/>'

    def tile(x, y, glyph, color):
        return (f'<g transform="translate({x} {y})" fill="none" stroke="{color}" stroke-width="2.2" '
                f'stroke-linecap="round" stroke-linejoin="round" color="{color}">{TILE_GLYPHS[glyph]}</g>')

    def chips(x, y, items, color=muted, size=14):
        rendered = []
        for s in items:
            w = text_width(s, size) + 20
            rendered.append(card(x, y, round(w), 30, "#fff", "#dbe4e8"))
            rendered.append(text(round(x + w / 2), y + 20, s, size, 500, color, "middle"))
            x += w + 8
        return "".join(rendered)

    # Context and decisions enter Persona; work continues with the user's Agents.
    for d in ("M330 112 C400 112 400 168 470 168", "M330 224 L470 224",
              "M330 336 C400 336 400 280 470 280", "M810 224 L960 224", "M640 438 L640 380"):
        out.append(f'<path d="{d}" fill="none" stroke="{line}" stroke-width="2" marker-end="url(#arrow)"/>')

    for (y, glyph), (title, subtitle) in zip(((68, "phone"), (180, "desktop"), (292, "android")), a["devices"]):
        out += [card(30, y, 300, 88, "#eff6ff", "#bfdbfe", True), tile(72, y + 43, glyph, "#3b82f6"),
                text(110, y + 38, title, weight=600), text(110, y + 63, subtitle, 14, color=muted)]

    out += [card(470, 68, 340, 312, "#fff4eb", PEACH),
            f'<path d="{spark_path(512, 120, 18, 64)}" fill="{CORAL}"/>',
            text(546, 130, "Persona", 32, 700), text(490, 158, a["machine"], 14, color=muted)]
    for i, s in enumerate(a["parts"]):
        x, y = 490 + (i % 2) * 154, 182 + (i // 2) * 58
        out += [card(x, y, 146, 46, "#fff", PEACH), text(x + 73, y + 29, s, 16, 600, EMBER, "middle")]
    out += [text(640, 314 + 23 * i, s, 14, color=muted, anchor="middle") for i, s in enumerate(a["duties"])]

    out += [card(960, 86, 290, 276, "#ecfdf5", "#a7f3d0", True), tile(995, 124, "laptop", "#059669"),
            text(1025, 130, a["computers"], weight=600), text(980, 164, a["agents"], 14, color=muted),
            chips(980, 192, ("Claude Code", "Codex"), "#065f46"),
            chips(980, 240, ("DSH", "pi", "opencode"), "#065f46")]
    out += [card(470, 438, 340, 72), text(490, 465, a["connectors"], weight=600),
            chips(490, 476, a["sources"], size=13)]

    for x, label in ((400, a["ask"]), (885, a["work"])):
        w = text_width(label, 13) + 20
        out += [card(round(x - w / 2), 211, round(w), 26, "#fff", "#cbd5e1"),
                text(x, 229, label, 13, 500, muted, "middle")]

    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" lang="{lang}">
<title>{a["title"]}</title>
<defs><marker id="arrow" markerWidth="8" markerHeight="6" refX="8" refY="3" orient="auto"><path d="M0,0 L8,3 L0,6" fill="none" stroke="{line}" stroke-width="1.5"/></marker></defs>
<rect x=".5" y=".5" width="{W - 1}" height="{H - 1}" rx="20" fill="#fff" stroke="#e2e8f0"/>
{"".join(out)}
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
    for label in RUNTIME_BADGES:
        write(f"badge-{label.lower().replace(' ', '-')}.svg", badge(label))
    for label in SUPPORT_BADGES:
        write(f"badge-{label.lower()}.svg", support_badge(label))


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
    from personactl import Ctl
    udid = os.environ.get("PERSONACTL_UDID")
    if not udid:
        raise SystemExit("set PERSONACTL_UDID to the simulator with the DEBUG build")
    return Ctl(udid=udid, bundle=os.environ.get("PERSONA_BUNDLE_ID"), port=int(os.environ.get("PERSONACTL_PORT", 18950)))


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
