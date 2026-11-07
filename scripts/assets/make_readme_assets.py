#!/usr/bin/env python3
"""The README's pictures in docs/assets/readme/: banner, badges, and the moving hero.

  python3 scripts/assets/make_readme_assets.py svg                  # banners (both languages) and badges
  python3 scripts/assets/make_readme_assets.py shoot <dir>          # screenshots + demo recordings from the simulator
  python3 scripts/assets/make_readme_assets.py hero <dir> [zh-CN]   # hero-light.webp / hero-dark.webp from <dir>

`svg` puts the app icon's own picture (AppIcon.appiconset) in the banner; it needs Pillow. `shoot` drives a DEBUG
build in demo mode (made-up data, no hub) that is already installed on the simulator given by PERSONACTL_UDID, over
the Runtime CLI (docs/runtime-cli.md): it relaunches the app with `-demo.on YES`, takes every scene in light and
dark, and records the demo once per appearance (the demo's taps match the Chinese demo data's labels).
`hero` needs ffmpeg and Pillow; it puts the demo in the middle phone and four still screens beside it, and writes
hero-<theme>.webp for English shots or hero-<theme>.zh-CN.webp when given `zh-CN`.
"""
import base64
import io
import os
import signal
import subprocess
import sys
import tempfile
import time

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
OUT = os.path.join(ROOT, "docs/assets/readme")
APP_ICON = os.path.join(ROOT, "apple/Persona/Resources/Assets.xcassets/AppIcon.appiconset")

# Quiet, near-neutral grounds with a faint warm glow from the app icon (scripts/assets/make_icon.py).
THEMES = {
    "light": dict(bg1="#fcfaf7", bg2="#f3eee7", glow="#f6c4a5", glow_op=0.32, fg="#1c1814", muted="#6f665c",
                  stroke="#e8e0d5", icon="icon.png", lift="#3c1e0a", lift_op=0.14),
    "dark": dict(bg1="#1c1715", bg2="#111010", glow="#ee7c54", glow_op=0.14, fg="#f3ede4", muted="#a89c8f",
                 stroke="#2f2925", icon="icon-dark.png", lift="#000000", lift_op=0.5),
}
SERIF = "Georgia, 'Times New Roman', serif"
SERIF_CJK = "'Songti SC', 'Noto Serif SC', 'Source Han Serif SC', STSong, SimSun, serif"


def write(name, text):
    os.makedirs(OUT, exist_ok=True)
    with open(os.path.join(OUT, name), "w", encoding="utf-8") as f:
        f.write(text)
    print(os.path.relpath(os.path.join(OUT, name), ROOT))


def app_icon(theme, px=320):
    """The app icon's picture for the theme as a JPEG data URI: an SVG shown with <img> loads no other files."""
    from PIL import Image
    img = Image.open(os.path.join(APP_ICON, THEMES[theme]["icon"])).convert("RGB").resize((px, px), Image.LANCZOS)
    buf = io.BytesIO()
    img.save(buf, "JPEG", quality=85, optimize=True, progressive=True)
    return "data:image/jpeg;base64," + base64.b64encode(buf.getvalue()).decode()


# ---------------------------------------------------------------- banner

# README.md is English and README.zh-CN.md Chinese: the banner and the hero are drawn once per language.
# English keeps the plain file names (banner-light.svg), Chinese adds ".zh-CN" (banner-light.zh-CN.svg).
LANGS = {"en": "", "zh-CN": ".zh-CN"}

TAGLINE = {  # (text, font family, size, letter spacing)
    "en": ("Your superagent. One place for every task.", SERIF, 30, 0),
    "zh-CN": ("你的 superagent，一个入口解决所有问题。", SERIF_CJK, 32, 1),
}


def banner(theme, lang="en"):
    """A quiet strip: the app icon and the wordmark side by side, the tagline under them, centered as one group."""
    t = THEMES[theme]
    tagline, tag_font, tag_size, tag_spacing = TAGLINE[lang]
    W, H, S = 1280, 300, 132
    r = S * 0.2237  # the icon's corner, as on the home screen
    word_w = 330  # "Persona" at 96 px in Georgia bold, measured
    gap = 34
    x0 = (W - (S + gap + word_w)) / 2
    y0 = 52
    tx = W / 2
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" lang="{lang}">
<title>Persona — {tagline}</title>
<defs>
  <linearGradient id="bg" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="{t["bg1"]}"/><stop offset="1" stop-color="{t["bg2"]}"/></linearGradient>
  <radialGradient id="glow" cx="{W / 2}" cy="{y0 + S / 2}" r="420" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="{t["glow"]}" stop-opacity="{t["glow_op"]}"/><stop offset="1" stop-color="{t["glow"]}" stop-opacity="0"/></radialGradient>
  <filter id="lift" x="-30%" y="-30%" width="160%" height="170%"><feDropShadow dx="0" dy="8" stdDeviation="12" flood-color="{t["lift"]}" flood-opacity="{t["lift_op"]}"/></filter>
  <clipPath id="iconMask"><rect x="{x0:.1f}" y="{y0}" width="{S}" height="{S}" rx="{r:.1f}"/></clipPath>
  <clipPath id="frame"><rect width="{W}" height="{H}" rx="20"/></clipPath>
</defs>
<g clip-path="url(#frame)">
  <rect width="{W}" height="{H}" fill="url(#bg)"/>
  <rect width="{W}" height="{H}" fill="url(#glow)"/>
  <rect x="{x0:.1f}" y="{y0}" width="{S}" height="{S}" rx="{r:.1f}" fill="{t["bg1"]}" filter="url(#lift)"/>
  <image x="{x0:.1f}" y="{y0}" width="{S}" height="{S}" href="{app_icon(theme)}" clip-path="url(#iconMask)"/>
  <rect x="{x0 + .5:.1f}" y="{y0 + .5}" width="{S - 1}" height="{S - 1}" rx="{r - .5:.1f}" fill="none" stroke="{t["stroke"]}"/>
  <text x="{x0 + S + gap:.1f}" y="{y0 + S / 2 + 33:.0f}" font-family="{SERIF}" font-size="96" font-weight="700" letter-spacing="-1" fill="{t["fg"]}">Persona</text>
  <text x="{tx:.0f}" y="{y0 + S + 66}" font-family="{tag_font}" font-size="{tag_size}" letter-spacing="{tag_spacing}" fill="{t["muted"]}" text-anchor="middle">{tagline}</text>
  <rect x=".5" y=".5" width="{W - 1}" height="{H - 1}" rx="20" fill="none" stroke="{t["stroke"]}"/>
</g>
</svg>
'''


# ---------------------------------------------------------------- badges

# What Persona drives, in the order the README lists them: (file name, kind, product).
BADGES = [
    ("claude-code", "agent", "Claude Code"),
    ("codex", "agent", "Codex"),
    ("herdr", "engine", "herdr"),
    ("orca", "engine", "Orca"),
]

VERDANA = {  # advance widths at 11px, shields.io's metric for Verdana
    " ": 3.9, ".": 3.9, "+": 9.2, "·": 3.9, "0": 7.0, "1": 7.0, "2": 7.0, "3": 7.0, "4": 7.0, "5": 7.0, "6": 7.0,
    "7": 7.0, "8": 7.0, "9": 7.0, "C": 7.7, "G": 8.5, "O": 8.7, "S": 7.6, "A": 7.5, "a": 6.7, "b": 6.9, "c": 5.8,
    "d": 6.9, "e": 6.6, "g": 6.9, "h": 7.0, "i": 3.0, "l": 3.0, "n": 7.0, "o": 6.7, "r": 4.7, "s": 5.8, "t": 4.3,
    "u": 7.0, "w": 9.0, "x": 6.6, "I": 4.6, "P": 7.4, "E": 7.0, "D": 8.5, "L": 6.1,
}


def vw(s, bold=False):
    return sum(VERDANA.get(ch, 7.0) for ch in s) * (1.1 if bold else 1)


def badge(kind, product):
    """One dark pill for both GitHub themes: the kind in a muted tone, the product in white; no colored segment."""
    kw, pw = vw(kind), vw(product, bold=True)
    w, h = 12 + kw + 8 + pw + 12, 24
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="{w:.0f}" height="{h}" viewBox="0 0 {w:.0f} {h}" role="img" aria-label="{kind}: {product}">
<title>{kind}: {product}</title>
<rect x=".5" y=".5" width="{w - 1:.0f}" height="{h - 1}" rx="6" fill="#26211e" stroke="#4a413a"/>
<g font-family="Verdana, 'DejaVu Sans', Geneva, sans-serif" font-size="11">
<text x="12" y="16" fill="#b3a79b" textLength="{kw:.0f}">{kind}</text>
<text x="{12 + kw + 8:.0f}" y="16" fill="#ffffff" font-weight="bold" textLength="{pw:.0f}">{product}</text>
</g>
</svg>
'''


def cmd_svg():
    for theme in THEMES:
        for lang, suffix in LANGS.items():
            write(f"banner-{theme}{suffix}.svg", banner(theme, lang))
    for name, kind, product in BADGES:
        write(f"badge-{name}.svg", badge(kind, product))

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


def hero(shots, theme, frames_dir, suffix=""):
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
    path = os.path.join(OUT, f"hero-{theme}{suffix}.webp")
    frames[0].save(path, save_all=True, append_images=frames[1:], duration=round(1000 / FPS), loop=0,
                   quality=72, method=6, minimize_size=True)
    print(f"{os.path.relpath(path, ROOT)}: {len(frames)} frames, {os.path.getsize(path) / 1e6:.2f} MB")


def cmd_hero(shots, lang="en"):
    if lang not in LANGS:
        raise SystemExit(f"hero: language must be one of {', '.join(LANGS)}")
    with tempfile.TemporaryDirectory() as tmp:
        for theme in THEMES:
            hero(shots, theme, tmp, LANGS[lang])


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
        cmd_hero(sys.argv[2], *sys.argv[3:4])


if __name__ == "__main__":
    main()
