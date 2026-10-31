#!/usr/bin/env python3
"""Makes a persona package (docs/persona.md): Jarvis's name, face, the animated dot in the Jarvis tab's top bar and
the colors, from a few pictures of a character.

  python3 scripts/persona/make_persona.py new --id doudou --name 豆豆 a.jpg b.jpg c.jpg   # generate, then build
  python3 scripts/persona/make_persona.py generate --work W a.jpg b.jpg [--only think,sleep] [--force]
  python3 scripts/persona/make_persona.py build --work W --id doudou --name 豆豆 [--out DIR]
  python3 scripts/persona/make_persona.py demo --out DIR      # an original placeholder (the mock hub, UI tests)
  python3 scripts/persona/make_persona.py check DIR           # the hub's and the app's rules, no hub needed
  python3 scripts/persona/make_persona.py install DIR [--hub HOST]   # to the hub's ~/.jarvis/personas/<id>/

generate draws the poses with Codex's built-in image_gen (`codex exec`, Codex's own login: no API key), the pictures
as references: transparent PNGs in <work>/poses/. Codex must reach chatgpt.com; --proxy http://host:port if it can't.
build needs no network: it lays the poses into round frames, animates them (breathing and a blink, a thought bubble,
a spinner arc, a pulsing ring, floating z's), writes the dot's APNGs, the avatar sizes, persona.json with colors taken
from the character (held to the app's contrast floors) and preview.png to look at. Pillow and numpy are set up in
~/.cache/jarvis/persona-venv on first use.
"""
import argparse
import colorsys
import io
import json
import math
import os
import re
import shlex
import shutil
import subprocess
import sys
import tarfile

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
VENV = os.path.expanduser("~/.cache/jarvis/persona-venv")

FORMAT = 1
ID_PATTERN = re.compile(r"^[a-z0-9][a-z0-9-]{0,31}$")
BUILTIN = "jarvis"
STATES = ["idle", "thinking", "working", "needs_you", "offline"]
AVATAR_SIZES = [64, 128, 256, 512]
# The dot's frames: 48 pt at 3x (the top bar shows it at 46 pt).
DOT = 144
MAX_FILE = 4 << 20
MAX_TOTAL = 16 << 20
# The app's readability floors (StyleSpecTests): white words on the accent, words on the user's bubble, the accent as
# a link on the page (white in light, black in dark).
FLOOR_ON_ACCENT = 3.5
FLOOR_BUBBLE = 4.5
FLOOR_LINK = 3.0
# 需要你 (StatusPalette.needsYou, light): the ring of the dot that calls you.
NEEDS_YOU = (0xE5, 0x48, 0x4D)

# The poses generate draws, in order; an edit starts from the pose it names so the two stay the same picture.
POSES = [
    {"name": "avatar", "say": "calm and friendly, looking at the viewer, a slight three-quarter view, arms relaxed"},
    {"name": "blink", "edit": "avatar", "say": "the same picture with both eyes closed in a relaxed blink; nothing else changes"},
    {"name": "think", "say": "thinking: the head tilted a little, eyes looking up to one side, one hand touching the chin"},
    {"name": "work_a", "say": "working hard: focused eyes, both arms up in a martial-arts stance, the left fist forward"},
    {"name": "work_b", "edit": "work_a", "say": "the next beat of the same move: the right fist forward, the left pulled back; nothing else changes"},
    {"name": "wave_a", "say": "calling the viewer: looking straight at the viewer, one hand raised beside the head, waving"},
    {"name": "wave_b", "edit": "wave_a", "say": "the next beat of the wave: the raised hand tilted the other way; nothing else changes"},
    {"name": "sleep", "say": "asleep: eyes closed, the head drooping a little, peaceful"},
]
POSE_NAMES = [p["name"] for p in POSES]
# Each pose's own placement comes from its base pose; edits keep their base's (they are the same picture).
BASE = {p["name"]: p.get("edit", p["name"]) for p in POSES}


def ensure_deps():
    """Pillow and numpy, from a venv of our own when the system Python has neither (re-runs this script in it)."""
    try:
        import numpy  # noqa: F401
        import PIL  # noqa: F401
        return
    except ImportError:
        pass
    if os.environ.get("JARVIS_PERSONA_VENV"):
        sys.exit("Pillow / numpy still missing in %s" % VENV)
    py = os.path.join(VENV, "bin", "python")
    if not os.path.exists(py):
        print("setting up %s (Pillow, numpy)…" % VENV, file=sys.stderr)
        subprocess.check_call([sys.executable, "-m", "venv", VENV])
        subprocess.check_call([py, "-m", "pip", "install", "-q", "pillow", "numpy"])
    os.environ["JARVIS_PERSONA_VENV"] = "1"
    os.execv(py, [py, os.path.abspath(__file__)] + sys.argv[1:])


# MARK: colors

def hex_rgb(s):
    s = s.strip().lstrip("#")
    if not re.fullmatch(r"[0-9a-fA-F]{6}", s):
        raise ValueError("not a #RRGGBB color: %r" % s)
    return tuple(int(s[i:i + 2], 16) for i in (0, 2, 4))


def rgb_hex(c):
    return "#%02X%02X%02X" % tuple(max(0, min(255, round(v))) for v in c)


def luminance(c):
    def lin(v):
        v /= 255
        return v / 12.92 if v <= 0.03928 else ((v + 0.055) / 1.055) ** 2.4
    r, g, b = c
    return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)


def contrast(a, b):
    la, lb = luminance(a), luminance(b)
    return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)


def shade(c, floor, against=(255, 255, 255)):
    """The color's hue, vivid, at the lightest lightness that keeps `against` (white words) at `floor`:1 on it."""
    h, l, s = colorsys.rgb_to_hls(*(v / 255 for v in c))
    s = max(s, 0.65)
    best = None
    for i in range(200):
        L = l * (1 - i / 200)
        rgb = tuple(v * 255 for v in colorsys.hls_to_rgb(h, L, s))
        if contrast(against, rgb) >= floor + 0.05:
            best = rgb
            break
    return best or (0, 0, 0)


def mix(a, b, t):
    return tuple(a[i] * (1 - t) + b[i] * t for i in range(3))


def extract_colors(img):
    """The character's own colors: primary, the hue most of it is (the body); secondary, the next one at least 40°
    away (eyes, a scarf). From pixels that are opaque, colorful and not dark."""
    import numpy as np
    a = np.asarray(img.convert("RGBA")).reshape(-1, 4).astype(float)
    px = a[a[:, 3] > 200][:, :3] / 255
    mx, mn = px.max(1), px.min(1)
    v, d = mx, mx - mn
    s = np.where(mx > 0, d / np.maximum(mx, 1e-6), 0)
    keep = (s > 0.3) & (v > 0.35)
    px, mx, d = px[keep], mx[keep], d[keep]
    if len(px) < 50:
        return (120, 120, 120), (90, 90, 90)
    r, g, b = px[:, 0], px[:, 1], px[:, 2]
    h = np.where(mx == r, ((g - b) / d) % 6, np.where(mx == g, (b - r) / d + 2, (r - g) / d + 4)) * 60
    bins = (h // 10).astype(int) % 36
    counts = np.bincount(bins, minlength=36)
    p = int(counts.argmax())

    def median(bin_):
        near = [(bin_ + k) % 36 for k in (-1, 0, 1)]
        return tuple(float(x) * 255 for x in np.median(px[np.isin(bins, near)], axis=0))

    far = [i for i in range(36) if min((i - p) % 36, (p - i) % 36) >= 4 and counts[i] >= 0.004 * len(px)]
    q = max(far, key=lambda i: counts[i]) if far else (p + 18) % 36
    return median(p), (median(q) if far else mix(median(p), (60, 120, 200), 0.7))


def palette(primary, secondary, accent_light=None, accent_dark=None):
    """persona.json's colors: the character's primary for decoration, accents and the user's bubble in its hue at the
    app's floors (white words on them). An accent given by hand is used as is (check holds it to the floors)."""
    al = accent_light or shade(primary, 4.5)
    ad = accent_dark or shade(primary, FLOOR_ON_ACCENT + 0.1)
    return {
        "light": {"primary": rgb_hex(primary), "secondary": rgb_hex(secondary), "accent": rgb_hex(al),
                  "bubble": rgb_hex(shade(al, FLOOR_BUBBLE + 0.2)), "bubble_text": "#FFFFFF"},
        "dark": {"primary": rgb_hex(primary), "secondary": rgb_hex(secondary), "accent": rgb_hex(ad),
                 "bubble": rgb_hex(shade(ad, FLOOR_BUBBLE + 0.1)), "bubble_text": "#FFFFFF"},
    }


# MARK: generate

def pose_prompt(todo, describe, have):
    lines = [
        "You are drawing the poses of a mascot for an app. The attached pictures show the character.",
        "Keep the character exactly as in them%s. Take nothing else from the pictures: no watermark, no text, no logo, "
        "no background, no frame." % (" (%s)" % describe if describe else ""),
        "",
        "Use your built-in image_gen tool, one call per image below, in this order. Right after each call, copy the PNG "
        "it produced into the current directory under the name given (overwrite it if it exists). Write no other files.",
        "",
        "Every image: square; the character alone on a fully transparent background (a real alpha channel, no "
        "checkerboard, no floor, no shadow under it); soft studio light as in the pictures; the same camera and scale for "
        "all of them: head and upper body, centered, the top of the head about 10% below the top edge, the body cut off "
        "by the bottom edge, the face large and clear. It is shown as small as 40 points, so keep shapes simple and "
        "readable, with no small props.",
        "",
    ]
    for i, p in enumerate(todo, 1):
        if p.get("edit"):
            base = p["edit"] + ".png"
            how = "edit %s (the image you made for it%s): " % (
                base, "" if p["edit"] not in have else "; it is a file here from an earlier run, load it with view_image first")
            lines.append("%d. %s.png: %s%s." % (i, p["name"], how, p["say"]))
        else:
            lines.append("%d. %s.png: %s." % (i, p["name"], p["say"]))
    lines += ["", "When all are written, reply with the file names, one a line."]
    return "\n".join(lines)


def check_pose(path):
    from PIL import Image
    im = Image.open(path)
    if im.mode != "RGBA":
        return "not RGBA (%s): no transparent background" % im.mode
    import numpy as np
    a = np.asarray(im.getchannel("A"))
    clear = (a < 10).mean()
    if clear < 0.08:
        return "only %.0f%% transparent: the background was drawn in" % (clear * 100)
    return None


def generate(args):
    ensure_deps()
    work = os.path.abspath(args.work)
    poses = os.path.join(work, "poses")
    os.makedirs(poses, exist_ok=True)
    only = set(args.only.split(",")) if args.only else None
    unknown = (only or set()) - set(POSE_NAMES)
    if unknown:
        sys.exit("unknown poses: %s (known: %s)" % (", ".join(sorted(unknown)), ", ".join(POSE_NAMES)))
    have = {n for n in POSE_NAMES if os.path.exists(os.path.join(poses, n + ".png"))}
    todo = [p for p in POSES if (p["name"] in only if only else (args.force or p["name"] not in have))]
    if not todo:
        print("every pose is there (--force or --only to draw again)")
        return
    refs = [os.path.abspath(r) for r in args.refs]
    for r in refs:
        if not os.path.exists(r):
            sys.exit("no such picture: %s" % r)
    names = {p["name"] for p in todo}
    prompt = pose_prompt(todo, args.describe, have - names)
    cmd = [args.codex, "exec", "--json", "--ephemeral", "--skip-git-repo-check", "-C", poses, "-s", "workspace-write",
           "-c", "notify=[]", "-c", "model_reasoning_effort=low"]
    if args.model:
        cmd += ["-m", args.model]
    for r in refs:
        cmd += ["-i", r]
    env = dict(os.environ)
    if args.proxy:
        env.update(HTTPS_PROXY=args.proxy, HTTP_PROXY=args.proxy, NO_PROXY="localhost,127.0.0.1")
    print("codex draws %d pose(s): %s" % (len(todo), ", ".join(p["name"] for p in todo)), flush=True)
    log = open(os.path.join(work, "generate.log"), "a")
    log.write("\n=== %s\n%s\n" % (" ".join(shlex.quote(c) for c in cmd), prompt))
    proc = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=log, text=True, env=env)
    proc.stdin.write(prompt)
    proc.stdin.close()
    for line in proc.stdout:
        log.write(line)
        try:
            e = json.loads(line)
        except ValueError:
            continue
        item = e.get("item") or {}
        if e.get("type") == "item.completed" and item.get("type") == "agent_message":
            print("  codex: " + item.get("text", "").strip().replace("\n", " / ")[:300], flush=True)
        elif e.get("type") == "turn.failed":
            print("  codex failed: %s" % json.dumps(e.get("error"))[:300], flush=True)
    proc.wait()
    log.close()
    bad = []
    for p in todo:
        f = os.path.join(poses, p["name"] + ".png")
        why = "missing" if not os.path.exists(f) else check_pose(f)
        if why:
            bad.append("%s: %s" % (p["name"], why))
    if bad:
        sys.exit("poses to draw again (--only %s):\n  %s" % (",".join(b.split(":")[0] for b in bad), "\n  ".join(bad)))
    print("poses in %s" % poses)


# MARK: build

class Placement:
    """Where a pose sits in a square of side W: scale k and the offset of its top-left corner."""

    def __init__(self, k, x, y):
        self.k, self.x, self.y = k, x, y


def head_of(img):
    """The pose's head: the top of what is opaque, the middle of its top rows (crown, eyes, snout), and its width a
    quarter down."""
    import numpy as np
    a = np.asarray(img.getchannel("A")) > 16
    rows = np.where(a.any(1))[0]
    if len(rows) == 0:
        raise SystemExit("an empty pose")
    top, bottom = int(rows[0]), int(rows[-1])
    h = bottom - top
    centers = []
    for y in range(top, top + max(1, int(h * 0.3))):
        xs = np.where(a[y])[0]
        if len(xs):
            centers.append((xs[0] + xs[-1]) / 2)
    y = min(bottom, top + int(h * 0.25))
    xs = np.where(a[y])[0]
    width = (xs[-1] - xs[0]) if len(xs) else img.width / 2
    return top, float(np.mean(centers)), float(width)


def placements(poses, W, framing):
    """One camera for every pose: the avatar's head a quarter down is half the circle wide; each base pose's head is
    put at the same place (its crown near the top, centered); edits take their base's. framing.json nudges a pose by hand:
    {"think": {"scale": 1.05, "dx": 0.02, "dy": -0.01}} (fractions of the side)."""
    _, _, width = head_of(poses["avatar"])
    k = 0.5 * W / width
    out = {}
    for name, img in poses.items():
        base = BASE.get(name, name)
        if base in out:
            out[name] = out[base]
            continue
        top, cx, _ = head_of(poses[base] if base in poses else img)
        f = framing.get(base, {})
        kk = k * f.get("scale", 1.0)
        out[base] = Placement(kk, W * (0.5 + f.get("dx", 0)) - cx * kk, W * (0.12 + f.get("dy", 0)) - top * kk)
        out[name] = out[base]
    return out


def affine(img, W, place, scale=1.0, angle=0.0, dx=0.0, dy=0.0):
    """The pose laid into a W square at its placement, then scaled about its bottom middle, turned about the middle
    (degrees, clockwise) and moved (pixels)."""
    from PIL import Image
    import numpy as np

    def m(a, b, c, d, e, f):
        return np.array([[a, b, c], [d, e, f], [0, 0, 1]], float)
    k = place.k
    M = m(k, 0, place.x, 0, k, place.y)
    M = m(scale, 0, W / 2 * (1 - scale), 0, scale, W * (1 - scale)) @ M
    t = math.radians(angle)
    c, s = math.cos(t), math.sin(t)
    M = m(c, -s, W / 2 - c * W / 2 + s * W / 2, s, c, W / 2 - s * W / 2 - c * W / 2) @ M
    M = m(1, 0, dx, 0, 1, dy) @ M
    inv = np.linalg.inv(M)
    return img.transform((W, W), Image.Transform.AFFINE, tuple(inv[:2].reshape(-1)), resample=Image.Resampling.BICUBIC)


def disc_layer(W, top, bottom):
    from PIL import Image, ImageDraw
    grad = Image.new("RGBA", (1, W))
    for y in range(W):
        grad.putpixel((0, y), tuple(round(v) for v in mix(top, bottom, y / (W - 1))) + (255,))
    grad = grad.resize((W, W))
    mask = Image.new("L", (W, W), 0)
    ImageDraw.Draw(mask).ellipse((0, 0, W - 1, W - 1), fill=255)
    out = Image.new("RGBA", (W, W), (0, 0, 0, 0))
    out.paste(grad, (0, 0), mask)
    return out, mask


class Frames:
    """Draws the round frames of one persona at W = side × ss (supersampled, scaled down at the end)."""

    def __init__(self, poses, colors, side, ss=3, framing=None):
        self.side, self.W = side, side * ss
        self.poses = poses
        self.place = placements(poses, self.W, framing or {})
        light = colors["light"]
        self.primary = hex_rgb(light["primary"])
        self.secondary = hex_rgb(light["secondary"])
        self.accent = hex_rgb(light["accent"])
        top = mix(self.secondary, (255, 255, 255), 0.84)
        self.disc, self.mask = disc_layer(self.W, top, mix(self.secondary, (255, 255, 255), 0.66))
        gray = (232, 232, 236)
        self.disc_off, _ = disc_layer(self.W, gray, (208, 208, 214))

    def pose(self, name):
        return self.poses.get(name) or self.poses["avatar"]

    def frame(self, name, *, scale=1.0, angle=0.0, dx=0.0, dy=0.0, off=False, draw=None):
        from PIL import Image, ImageDraw, ImageEnhance
        W = self.W
        ch = affine(self.pose(name), W, self.place[name if name in self.poses else "avatar"], scale, angle, dx * W, dy * W)
        if off:
            alpha = ch.getchannel("A")
            ch = ImageEnhance.Color(ch.convert("RGB")).enhance(0.2)
            ch = ImageEnhance.Brightness(ch).enhance(0.92).convert("RGBA")
            ch.putalpha(alpha)
        out = (self.disc_off if off else self.disc).copy()
        clipped = Image.new("RGBA", (W, W), (0, 0, 0, 0))
        clipped.paste(ch, (0, 0), Image.composite(ch.getchannel("A"), Image.new("L", (W, W), 0), self.mask))
        out = Image.alpha_composite(out, clipped)
        d = ImageDraw.Draw(out)
        lw = max(1, round(W * 0.012))
        d.ellipse((lw / 2, lw / 2, W - 1 - lw / 2, W - 1 - lw / 2), outline=(255, 255, 255, 110), width=lw)
        if draw:
            draw(out, ImageDraw.Draw(out))
        return out.resize((self.side, self.side), Image.Resampling.LANCZOS)

    # The states. Frame 0 of each is the one shown still (Reduce Motion, Low Power Mode): it must say the state alone.

    def idle(self):
        """Breathing, and a blink near the end of the loop (24 frames, 8 fps)."""
        n = 24
        return [self.frame("blink" if i in (19, 20) and "blink" in self.poses else "avatar",
                           scale=1 + 0.012 * math.sin(2 * math.pi * i / n), dy=0.008 * math.sin(2 * math.pi * i / n))
                for i in range(n)], 8

    def thinking(self):
        """A slow sway under a thought bubble whose dots light up in turn (16 frames, 8 fps)."""
        n = 16

        def bubble(i):
            def draw(img, d):
                W = self.W
                cx, cy, r = W * 0.76, W * 0.19, W * 0.15
                shadow(img, (cx - r, cy - r * 0.72 + W * 0.015, cx + r, cy + r * 0.72 + W * 0.015), W * 0.02)
                d.rounded_rectangle((cx - r, cy - r * 0.72, cx + r, cy + r * 0.72), radius=r * 0.72, fill=(255, 255, 255, 250))
                d.ellipse((cx - r * 0.85, cy + r * 0.62, cx - r * 0.55, cy + r * 0.92), fill=(255, 255, 255, 250))
                lit = (i // 4) % 4
                for k in range(3):
                    x = cx + (k - 1) * r * 0.55
                    rr = r * (0.17 if k == lit else 0.14)
                    col = self.accent + (255,) if k == lit else (150, 150, 158, 255)
                    d.ellipse((x - rr, cy - rr, x + rr, cy + rr), fill=col)
            return draw
        return [self.frame("think", angle=2.2 * math.sin(2 * math.pi * i / n), draw=bubble(i)) for i in range(n)], 8

    def working(self):
        """The two beats of the move with a bob, and an arc going round the rim (12 frames, 10 fps)."""
        n = 12

        def arc(i):
            def draw(img, d):
                W = self.W
                w = W * 0.075
                box = (w / 2, w / 2, W - w / 2, W - w / 2)
                d.ellipse(box, outline=self.accent + (60,), width=round(w))
                start = i * 360 / n - 90
                d.arc(box, start, start + 110, fill=self.accent + (255,), width=round(w))
            return draw
        return [self.frame("work_a" if i % 4 < 2 else "work_b", dy=-0.012 * abs(math.sin(math.pi * i / 4)), draw=arc(i))
                for i in range(n)], 10

    def needs_you(self):
        """The wave's two beats inside a ring that pulses in 需要你's red (12 frames, 6 fps)."""
        n = 12

        def ring(i):
            def draw(img, d):
                W = self.W
                w = W * 0.07
                alpha = round(255 * (0.7 + 0.3 * math.cos(2 * math.pi * i / n)))
                d.ellipse((w / 2, w / 2, W - w / 2, W - w / 2), outline=NEEDS_YOU + (alpha,), width=round(w))
            return draw
        return [self.frame("wave_a" if (i // 3) % 2 == 0 else "wave_b", draw=ring(i)) for i in range(n)], 6

    def offline(self):
        """Asleep in gray, z's floating up out of the circle's top right (12 frames, 4 fps)."""
        n = 12

        def zs(i):
            def draw(img, d):
                W = self.W
                for k in range(3):
                    t = ((i / n) + k / 3) % 1
                    size = W * (0.08 + 0.06 * t)
                    x, y = W * (0.64 + 0.12 * t), W * (0.36 - 0.22 * t)
                    a = round(255 * min(1, 2.5 * (1 - t)))
                    zig(d, x, y, size, (72, 72, 84, a), max(2, round(W * 0.028)))
            return draw
        return [self.frame("sleep", off=True, scale=1 + 0.008 * math.sin(2 * math.pi * i / n), draw=zs(i)) for i in range(n)], 4


def shadow(img, box, blur):
    from PIL import Image, ImageDraw, ImageFilter
    layer = Image.new("RGBA", img.size, (0, 0, 0, 0))
    ImageDraw.Draw(layer).rounded_rectangle(box, radius=(box[3] - box[1]) / 2, fill=(0, 0, 0, 60))
    img.alpha_composite(layer.filter(ImageFilter.GaussianBlur(blur)))


def zig(d, x, y, size, color, width):
    """A "z" from three strokes (no font: the same on every machine)."""
    pts = [(x, y), (x + size, y), (x, y + size), (x + size, y + size)]
    d.line(pts, fill=color, width=width, joint="curve")


def save_apng(frames, fps, path):
    frames[0].save(path, save_all=True, append_images=frames[1:], duration=round(1000 / fps), loop=0,
                   disposal=0, blend=0, optimize=False)


def load_poses(work):
    from PIL import Image
    d = os.path.join(work, "poses")
    poses = {}
    for n in POSE_NAMES:
        f = os.path.join(d, n + ".png")
        if os.path.exists(f):
            poses[n] = Image.open(f).convert("RGBA")
    if "avatar" not in poses:
        sys.exit("no %s/avatar.png: generate first" % d)
    return poses


def build(args):
    ensure_deps()
    from PIL import Image
    work = os.path.abspath(args.work)
    if not ID_PATTERN.match(args.id) or args.id == BUILTIN:
        sys.exit("--id: a-z, 0-9 and -, up to 32, not %r" % BUILTIN)
    out = os.path.abspath(args.out or os.path.join(work, "package", args.id))
    poses = load_poses(work)
    missing = [n for n in POSE_NAMES if n not in poses]
    if missing:
        print("no %s: the avatar stands in for them" % ", ".join(missing))
    framing = {}
    fpath = os.path.join(work, "framing.json")
    if os.path.exists(fpath):
        framing = json.load(open(fpath))
    primary, secondary = extract_colors(poses["avatar"])
    if args.primary:
        primary = hex_rgb(args.primary)
    if args.secondary:
        secondary = hex_rgb(args.secondary)
    colors = palette(primary, secondary, hex_rgb(args.accent_light) if args.accent_light else None,
                     hex_rgb(args.accent_dark) if args.accent_dark else None)
    write_package(out, args.id, args.name, args.subtitle, poses, colors, framing)
    check_package(out)
    print("package in %s (look at preview.png)" % out)


def write_package(out, pid, name, subtitle, poses, colors, framing):
    from PIL import Image
    if os.path.exists(out):
        shutil.rmtree(out)
    os.makedirs(os.path.join(out, "avatar"))
    os.makedirs(os.path.join(out, "dot"))
    # The avatar: drawn once at 1024 and scaled down, so every size is the same picture.
    big = Frames(poses, colors, 1024, ss=1, framing=framing).frame("avatar")
    sizes = {}
    for s in AVATAR_SIZES:
        f = "avatar/%d.png" % s
        big.resize((s, s), Image.Resampling.LANCZOS).save(os.path.join(out, f), optimize=True)
        sizes[str(s)] = f
    fr = Frames(poses, colors, DOT, framing=framing)
    states, sheets = {}, {}
    for st in STATES:
        frames, fps = getattr(fr, st)()
        f = "dot/%s.png" % st
        save_apng(frames, fps, os.path.join(out, f))
        states[st] = {"file": f, "fps": fps, "frames": len(frames)}
        sheets[st] = frames
    manifest = {
        "format": FORMAT,
        "id": pid,
        "name": name,
        "avatar": {"sizes": sizes, "safe": 1.0},
        "dot": {"size": DOT, "states": states},
        "colors": colors,
    }
    if subtitle:
        manifest["subtitle"] = subtitle
    with open(os.path.join(out, "persona.json"), "w") as f:
        json.dump(manifest, f, ensure_ascii=False, indent=2)
        f.write("\n")
    preview(out, big, sheets, colors).save(os.path.join(out, "preview.png"), optimize=True)


def preview(out, avatar, sheets, colors):
    """One picture to look at: the avatar, each state's first frames, and the colors, on light and on dark."""
    from PIL import Image, ImageDraw
    cell = DOT + 12
    cols = 8
    w = 24 + 256 + 24 + cols * cell + 24
    h_half = 24 + len(STATES) * cell + 24
    img = Image.new("RGBA", (w, h_half * 2), (0, 0, 0, 0))
    for half, (bg, key) in enumerate([((246, 246, 248), "light"), ((18, 18, 20), "dark")]):
        y0 = half * h_half
        ImageDraw.Draw(img).rectangle((0, y0, w, y0 + h_half), fill=bg + (255,))
        img.alpha_composite(avatar.resize((256, 256), Image.Resampling.LANCZOS), (24, y0 + 24))
        d = ImageDraw.Draw(img)
        for i, k in enumerate(["primary", "secondary", "accent", "bubble"]):
            c = hex_rgb(colors[key][k])
            d.rounded_rectangle((24 + i * 64, y0 + 300, 24 + i * 64 + 56, y0 + 356), radius=10, fill=c + (255,))
        for r, st in enumerate(STATES):
            frames = sheets[st]
            step = max(1, len(frames) // cols)
            for c, f in enumerate(frames[::step][:cols]):
                img.alpha_composite(f, (24 + 256 + 24 + c * cell, y0 + 24 + r * cell))
    return img


# MARK: check

def check_package(d):
    """The rules the hub and the app hold a package to (docs/persona.md); exits with what breaks them."""
    ensure_deps()
    from PIL import Image
    errs = []
    try:
        m = json.load(open(os.path.join(d, "persona.json")))
    except (OSError, ValueError) as e:
        sys.exit("persona.json: %s" % e)
    pid = m.get("id", "")
    if m.get("format") != FORMAT:
        errs.append("format %r, want %d" % (m.get("format"), FORMAT))
    if not ID_PATTERN.match(pid) or pid == BUILTIN:
        errs.append("id %r" % pid)
    if os.path.basename(os.path.normpath(d)) != pid:
        errs.append("the directory is %r, the id %r" % (os.path.basename(os.path.normpath(d)), pid))
    if not str(m.get("name", "")).strip():
        errs.append("no name")

    def file_ok(f, what):
        p = os.path.normpath(os.path.join(d, f))
        if f.startswith("/") or ".." in f.split("/") or any(x.startswith(".") for x in f.split("/")):
            errs.append("%s: %r is not a plain path in the package" % (what, f))
            return None
        if not os.path.isfile(p):
            errs.append("%s: no %s" % (what, f))
            return None
        if os.path.getsize(p) > MAX_FILE:
            errs.append("%s: %s is over %d bytes" % (what, f, MAX_FILE))
        return p

    sizes = (m.get("avatar") or {}).get("sizes") or {}
    if not sizes:
        errs.append("no avatar sizes")
    for s, f in sizes.items():
        p = file_ok(f, "avatar %s" % s)
        if p:
            im = Image.open(p)
            if im.size != (int(s), int(s)):
                errs.append("avatar %s is %dx%d" % (s, *im.size))
    states = (m.get("dot") or {}).get("states") or {}
    if "idle" not in states:
        errs.append("no idle dot")
    for st, spec in states.items():
        if st not in STATES:
            errs.append("unknown state %r" % st)
            continue
        p = file_ok(spec.get("file", ""), "dot %s" % st)
        if p:
            im = Image.open(p)
            if im.size[0] != im.size[1]:
                errs.append("dot %s is not square" % st)
            if getattr(im, "n_frames", 1) > 120:
                errs.append("dot %s has %d frames (120 at most)" % (st, im.n_frames))
    colors = m.get("colors") or {}
    for key, page in (("light", (255, 255, 255)), ("dark", (0, 0, 0))):
        c = colors.get(key) or {}
        try:
            if "accent" in c:
                a = hex_rgb(c["accent"])
                if contrast((255, 255, 255), a) < FLOOR_ON_ACCENT:
                    errs.append("%s accent %s: white on it %.2f:1, under %.1f" % (key, c["accent"], contrast((255, 255, 255), a), FLOOR_ON_ACCENT))
                if contrast(a, page) < FLOOR_LINK:
                    errs.append("%s accent %s on the page %.2f:1, under %.1f" % (key, c["accent"], contrast(a, page), FLOOR_LINK))
            if "bubble" in c:
                b, t = hex_rgb(c["bubble"]), hex_rgb(c.get("bubble_text", "#FFFFFF"))
                if contrast(b, t) < FLOOR_BUBBLE:
                    errs.append("%s bubble %s: its words %.2f:1, under %.1f" % (key, c["bubble"], contrast(b, t), FLOOR_BUBBLE))
            for k in ("primary", "secondary"):
                if k in c:
                    hex_rgb(c[k])
        except ValueError as e:
            errs.append("%s: %s" % (key, e))
    total = sum(os.path.getsize(os.path.join(r, f)) for r, _, fs in os.walk(d) for f in fs)
    if total > MAX_TOTAL:
        errs.append("%d bytes in all, over %d" % (total, MAX_TOTAL))
    if errs:
        sys.exit("%s:\n  %s" % (d, "\n  ".join(errs)))
    print("%s: ok (%d KB)" % (d, total // 1024))


# MARK: demo

def demo_poses(size=1024):
    """An original placeholder character drawn from shapes: a mint drop with round eyes. For the mock hub and UI
    tests, and to try the pipeline without generating anything."""
    from PIL import Image, ImageDraw
    body, belly, dark = (64, 178, 160), (196, 236, 222), (34, 44, 52)
    W = size * 2

    def draw(eyes="open", look=(0, 0), hand=None, tilt=0.0):
        img = Image.new("RGBA", (W, W), (0, 0, 0, 0))
        d = ImageDraw.Draw(img)
        d.ellipse((W * 0.18, W * 0.12, W * 0.82, W * 1.2), fill=body + (255,))
        d.ellipse((W * 0.3, W * 0.62, W * 0.7, W * 1.15), fill=belly + (255,))
        for side in (-1, 1):
            ex, ey = W * (0.5 + side * 0.13), W * 0.4
            if eyes == "open":
                d.ellipse((ex - W * 0.085, ey - W * 0.095, ex + W * 0.085, ey + W * 0.095), fill=(255, 255, 255, 255))
                px, py = ex + look[0] * W * 0.035, ey + look[1] * W * 0.04
                d.ellipse((px - W * 0.045, py - W * 0.05, px + W * 0.045, py + W * 0.05), fill=dark + (255,))
            else:
                d.arc((ex - W * 0.07, ey - W * 0.05, ex + W * 0.07, ey + W * 0.06), 20, 160, fill=dark + (255,), width=round(W * 0.018))
        d.arc((W * 0.44, W * 0.5, W * 0.56, W * 0.56), 20, 160, fill=dark + (255,), width=round(W * 0.012))
        if hand:
            hx, hy = W * hand[0], W * hand[1]
            d.ellipse((hx - W * 0.07, hy - W * 0.07, hx + W * 0.07, hy + W * 0.07), fill=(36, 128, 114, 255))
        if tilt:
            img = img.rotate(tilt, resample=Image.Resampling.BICUBIC, center=(W / 2, W * 0.7))
        return img.resize((size, size), Image.Resampling.LANCZOS)

    return {
        "avatar": draw(),
        "blink": draw(eyes="closed"),
        "think": draw(look=(0.8, -0.8), hand=(0.62, 0.58), tilt=-5),
        "work_a": draw(look=(0, 0.4), hand=(0.24, 0.62), tilt=3),
        "work_b": draw(look=(0, 0.4), hand=(0.76, 0.62), tilt=-3),
        "wave_a": draw(hand=(0.84, 0.36), tilt=-2),
        "wave_b": draw(hand=(0.9, 0.3), tilt=2),
        "sleep": draw(eyes="closed", tilt=6),
    }


def demo(args):
    ensure_deps()
    out_root = os.path.abspath(args.out)
    work = os.path.join(out_root, ".demo-work")
    os.makedirs(os.path.join(work, "poses"), exist_ok=True)
    for n, img in demo_poses().items():
        img.save(os.path.join(work, "poses", n + ".png"))
    poses = load_poses(work)
    colors = palette(*extract_colors(poses["avatar"]))
    out = os.path.join(out_root, args.id)
    write_package(out, args.id, args.name, "演示用的原创形象", poses, colors, {})
    shutil.rmtree(work)
    check_package(out)


# MARK: install

def install(args):
    d = os.path.abspath(args.dir)
    check_package(d)
    pid = os.path.basename(os.path.normpath(d))
    host = args.hub
    if not host:
        cfg = subprocess.run([sys.executable, os.path.join(ROOT, "scripts", "jarvis_config.py"), "env"],
                             capture_output=True, text=True, check=True).stdout
        m = re.search(r"JARVIS_HUB_SSH=(\S+)", cfg)
        host = shlex.split(m.group(1))[0] if m else ""
    if not host:
        sys.exit("no hub: --hub HOST, or hub.machine in ~/.config/jarvis/config.json (docs/config.md)")
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as t:
        for name in sorted(os.listdir(d)):
            t.add(os.path.join(d, name), arcname=name)
    # Next to the old one under a dot name (the hub skips those), then swapped in: the phone never lists half a package.
    script = ("set -e; d=~/.jarvis/personas; mkdir -p $d; rm -rf $d/.{id}.new $d/.{id}.old; mkdir $d/.{id}.new; "
              "tar -xzf - -C $d/.{id}.new; [ -d $d/{id} ] && mv $d/{id} $d/.{id}.old; mv $d/.{id}.new $d/{id}; "
              "rm -rf $d/.{id}.old").format(id=pid)
    subprocess.run(["ssh", host, script], input=buf.getvalue(), check=True)
    print("installed %s on %s:~/.jarvis/personas/%s (phones pick it up in 设置 › 形象)" % (pid, host, pid))


def new(args):
    generate(args)
    build(args)


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)

    def gen_args(p):
        p.add_argument("refs", nargs="+", help="pictures of the character")
        p.add_argument("--work", help="where the poses and the package go (default ./persona-<id>)")
        p.add_argument("--only", help="draw these poses again: " + ",".join(POSE_NAMES))
        p.add_argument("--force", action="store_true", help="draw every pose again")
        p.add_argument("--describe", help="the character in a few words (helps keep it the same)")
        p.add_argument("--model", help="codex's model (one your Codex login may use)")
        p.add_argument("--proxy", help="an HTTP proxy for codex, when chatgpt.com is out of reach")
        p.add_argument("--codex", default="codex")

    def build_args(p):
        p.add_argument("--id", required=True)
        p.add_argument("--name", required=True)
        p.add_argument("--subtitle")
        p.add_argument("--out", help="the package directory (default <work>/package/<id>)")
        p.add_argument("--primary", help="#RRGGBB instead of the character's own")
        p.add_argument("--secondary", help="#RRGGBB: the second color (the dot's backdrop is a pale one of it)")
        p.add_argument("--accent-light", help="#RRGGBB: what can be tapped, light appearance")
        p.add_argument("--accent-dark", help="#RRGGBB: dark appearance")

    p = sub.add_parser("generate", help="draw the poses with Codex")
    gen_args(p)
    p.set_defaults(fn=generate)
    p = sub.add_parser("build", help="make the package from the poses (no network)")
    p.add_argument("--work", required=True)
    build_args(p)
    p.set_defaults(fn=build)
    p = sub.add_parser("new", help="generate, then build")
    gen_args(p)
    build_args(p)
    p.set_defaults(fn=new)
    p = sub.add_parser("demo", help="an original placeholder package")
    p.add_argument("--out", required=True, help="the directory the package goes in (as <out>/<id>)")
    p.add_argument("--id", default="demo")
    p.add_argument("--name", default="小薄荷")
    p.set_defaults(fn=demo)
    p = sub.add_parser("check", help="check a package")
    p.add_argument("dir")
    p.set_defaults(fn=lambda a: check_package(a.dir))
    p = sub.add_parser("install", help="copy a package to the hub")
    p.add_argument("dir")
    p.add_argument("--hub", help="ssh host of the hub's machine (default: the config's)")
    p.set_defaults(fn=install)
    args = ap.parse_args()
    if getattr(args, "work", None) is None and hasattr(args, "refs"):
        args.work = "persona-" + (getattr(args, "id", None) or "new")
    args.fn(args)


if __name__ == "__main__":
    main()
