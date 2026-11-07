#!/usr/bin/env python3
"""Makes a persona package (docs/apple/persona.md): Persona's name, face, the animated dot in the Persona tab's top bar and
the colors, from a few pictures of a character.

  python3 scripts/assets/make_persona.py new --id doudou --name Doudou a.jpg b.jpg c.jpg   # generate, then build
  python3 scripts/assets/make_persona.py generate --work W a.jpg b.jpg [--only think,sleep] [--force]
  python3 scripts/assets/make_persona.py build --work W --id doudou --name Doudou [--out DIR]
  python3 scripts/assets/make_persona.py demo --out DIR      # an original placeholder (the mock hub, UI tests)
  python3 scripts/assets/make_persona.py check DIR           # the hub's and the app's rules, no hub needed
  python3 scripts/assets/make_persona.py install DIR [--hub HOST]   # to the hub's ~/.persona/personas/<id>/
  python3 scripts/assets/make_persona.py tiers generate --spec S --work W   # a tiered look: every tier's poses at once
  python3 scripts/assets/make_persona.py tiers build --spec S --work W --out DIR   # the package, and W/review.html

generate draws the poses with Codex's built-in image_gen (`codex exec`, Codex's own login: no API key), the pictures
as references: transparent PNGs in <work>/poses/. Codex must reach chatgpt.com; --proxy http://host:port if it can't.
build needs no network: it lays the poses into round frames, animates them (breathing and a blink, a thought bubble,
a spinner arc, a pulsing ring, floating z's), writes the dot's APNGs, the avatar sizes, persona.json with colors taken
from the character (held to the app's contrast floors) and preview.png to look at. Pillow and numpy are set up in
~/.cache/persona/persona-venv on first use.
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
VENV = os.path.expanduser("~/.cache/persona/persona-venv")

FORMAT = 1
ID_PATTERN = re.compile(r"^[a-z0-9][a-z0-9-]{0,31}$")
BUILTIN = "persona"
STATES = ["idle", "thinking", "working", "needs_you", "offline"]
AVATAR_SIZES = [64, 128, 256, 512]
# The dot's frames: 48 pt at 3x (the top bar shows it at 46 pt).
DOT = 144
MAX_FILE = 4 << 20
MAX_TOTAL = 16 << 20
# A package built into personad rides along with every hub update: the whole of it at most this.
BUILTIN_BUDGET = 5 << 20
# about: the second line of the look's card in Settings › Appearance › Persona (like "colors follow the theme color"), Chinese written without spaces.
ABOUT_MAX = 10
# The app's readability floors (StyleSpecTests): white words on the accent, words on the user's bubble, the accent as
# a link on the page (white in light, black in dark).
FLOOR_ON_ACCENT = 3.5
FLOOR_BUBBLE = 4.5
FLOOR_LINK = 3.0
# "Needs you" (StatusPalette.needsYou, light): the ring of the dot that calls you.
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
    if os.environ.get("PERSONA_PERSONA_VENV"):
        sys.exit("Pillow / numpy still missing in %s" % VENV)
    py = os.path.join(VENV, "bin", "python")
    if not os.path.exists(py):
        print("setting up %s (Pillow, numpy)…" % VENV, file=sys.stderr)
        subprocess.check_call([sys.executable, "-m", "venv", VENV])
        subprocess.check_call([py, "-m", "pip", "install", "-q", "pillow", "numpy"])
    os.environ["PERSONA_PERSONA_VENV"] = "1"
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

# Where the character sits in every pose unless a persona says otherwise (a tier's egg is drawn whole).
CAMERA = ("head and upper body, centered, the top of the head about 10% below the top edge, the body cut off by the "
          "bottom edge, the face large and clear")


def pose_prompt(todo, describe, have, style=None, camera=None):
    lines = [
        "You are drawing the poses of a mascot for an app. The attached pictures show the character.",
        "Keep the character exactly as in them%s. Take nothing else from the pictures: no watermark, no text, no logo, "
        "no background, no frame." % (" (%s)" % describe if describe else ""),
    ]
    if style:
        lines.append(style)
    lines += [
        "",
        "Use your built-in image_gen tool, one call per image below, in this order. Right after each call, copy the PNG "
        "it produced into the current directory under the name given (overwrite it if it exists). Write no other files.",
        "",
        "Every image: square; the character alone on a fully transparent background (a real alpha channel, no "
        "checkerboard, no floor, no shadow under it); soft studio light as in the pictures; the same camera and scale for "
        "all of them: %s. It is shown as small as 40 points, so keep shapes simple and readable, with no small props."
        % (camera or CAMERA),
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
    # --poses: what a persona's own poses say instead (an egg has no arms to box with).
    say = json.loads(args.poses) if getattr(args, "poses", None) else {}
    todo = [dict(p, say=say.get(p["name"], p["say"])) for p in POSES
            if (p["name"] in only if only else (args.force or p["name"] not in have))]
    if not todo:
        print("every pose is there (--force or --only to draw again)")
        return
    refs = [os.path.abspath(r) for r in args.refs]
    for r in refs:
        if not os.path.exists(r):
            sys.exit("no such picture: %s" % r)
    names = {p["name"] for p in todo}
    prompt = pose_prompt(todo, args.describe, have - names, getattr(args, "style", None), getattr(args, "camera", None))
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


def weight_center(img, axis=0):
    """Where the character's weight is (alpha-weighted), across (axis 0) or down (1): a tail or a raised arm counts for
    what it is."""
    import numpy as np
    a = np.asarray(img.getchannel("A")).astype(float)
    total = a.sum()
    size = img.width if axis == 0 else img.height
    return float((a.sum(axis) * np.arange(size)).sum() / total) if total else size / 2


def rows_of(img):
    """The first and last rows with something opaque in them."""
    import numpy as np
    rows = np.where((np.asarray(img.getchannel("A")) > 16).any(1))[0]
    return (int(rows[0]), int(rows[-1])) if len(rows) else (0, img.height)


def placements(poses, W, framing, center="head", vertical="top"):
    """One camera for every pose: the avatar's head a quarter down is half the circle wide; each base pose's head is
    put at the same place (its crown near the top, centered); edits take their base's. framing.json nudges a pose by hand:
    {"think": {"scale": 1.05, "dx": 0.02, "dy": -0.01}} (fractions of the side); "*" nudges every pose without its own.
    center="weight": across, each pose with its edits is centered on the character's weight (their mean), not on its
    head: a tadpole's tail or an egg leaning over stays in the middle of the circle.
    vertical: "top" (the crown near the top, for head-and-shoulders); for a character the circle holds whole,
    "ground" (every pose standing on one ground line, the avatar's height centered: an egg's shell stays put when the
    one inside sinks into it) or "weight" (each pose's weight at the middle: a tadpole's head holds still as its tail
    swings)."""
    _, _, width = head_of(poses["avatar"])
    k = 0.5 * W / width
    out = {}
    fa = framing.get("avatar", framing.get("*", {}))
    ka = k * fa.get("scale", 1.0)
    a_top, a_bottom = rows_of(poses["avatar"])
    ground = W * (0.5 + fa.get("dy", 0)) + (a_bottom - a_top) * ka / 2
    for name, img in poses.items():
        base = BASE.get(name, name)
        if base in out:
            out[name] = out[base]
            continue
        top, cx, _ = head_of(poses[base] if base in poses else img)
        if center == "weight":
            group = [poses[n] for n in poses if BASE.get(n, n) == base]
            cx = sum(weight_center(g) for g in group) / len(group)
        f = framing.get(base, framing.get("*", {}))
        kk = k * f.get("scale", 1.0)
        if vertical == "ground":
            y = ground - rows_of(poses[base] if base in poses else img)[1] * kk
        elif vertical == "weight":
            group = [poses[n] for n in poses if BASE.get(n, n) == base]
            y = W * (0.5 + f.get("dy", 0)) - sum(weight_center(g, 1) for g in group) / len(group) * kk
        else:
            y = W * (0.12 + f.get("dy", 0)) - top * kk
        out[base] = Placement(kk, W * (0.5 + f.get("dx", 0)) - cx * kk, y)
        out[name] = out[base]
    return out


def affine(img, W, place, scale=1.0, angle=0.0, dx=0.0, dy=0.0, pivot=0.5):
    """The pose laid into a W square at its placement, then scaled about its bottom middle, turned (degrees, clockwise)
    about the middle across and `pivot` of the way down (0.5 the middle, near 1 its bottom: rocking) and moved (pixels)."""
    from PIL import Image
    import numpy as np

    def m(a, b, c, d, e, f):
        return np.array([[a, b, c], [d, e, f], [0, 0, 1]], float)
    k = place.k
    M = m(k, 0, place.x, 0, k, place.y)
    M = m(scale, 0, W / 2 * (1 - scale), 0, scale, W * (1 - scale)) @ M
    t = math.radians(angle)
    c, s = math.cos(t), math.sin(t)
    px, py = W / 2, W * pivot
    M = m(c, -s, px - c * px + s * py, s, c, py - s * px - c * py) @ M
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


# MARK: motions (how a tier moves)

def _wave(i, n, k=1.0, ph=0.0):
    return math.sin(2 * math.pi * (k * i / n + ph))


def _arch(i, period):
    """0 → 1 → 0 over each `period` frames: a hop's height."""
    return math.sin(math.pi * (i % period) / period)


def motion_breathe(state, i, n):
    """The default: breathing, a sway while thinking, a bob while working; the frog's own."""
    if state == "idle":
        return dict(scale=1 + 0.012 * _wave(i, n), dy=0.008 * _wave(i, n))
    if state == "thinking":
        return dict(angle=2.2 * _wave(i, n))
    if state == "working":
        return dict(dy=-0.012 * abs(math.sin(math.pi * i / 4)))
    if state == "offline":
        return dict(scale=1 + 0.008 * _wave(i, n))
    return {}


def motion_rock(state, i, n):
    """An egg rocking on its bottom: gently, harder while it works at hatching, hopping when it calls."""
    if state == "idle":
        return dict(angle=3 * _wave(i, n), pivot=0.97)
    if state == "thinking":
        return dict(angle=5 + 1.5 * _wave(i, n), pivot=0.97)
    if state == "working":
        return dict(angle=-7 if i % 4 < 2 else 7, dy=-0.012 * (i % 2), pivot=0.97)
    if state == "needs_you":
        return dict(dy=-0.04 * _arch(i, 6), pivot=0.97)
    return dict(angle=4, scale=1 + 0.006 * _wave(i, n), pivot=0.97)


def motion_peek(state, i, n):
    """Out of its shell and back: popping up, bouncing while it works, springing up to call."""
    if state == "idle":
        return dict(dy=-0.018 * max(0.0, _wave(i, n)), scale=1 + 0.01 * _wave(i, n))
    if state == "thinking":
        return dict(angle=2.5 * _wave(i, n), pivot=0.95)
    if state == "working":
        return dict(dy=-0.03 * _arch(i, 4), scale=0.97 if i % 4 == 0 else 1.0)
    if state == "needs_you":
        return dict(dy=-0.05 * _arch(i, 6))
    return dict(scale=1 + 0.006 * _wave(i, n))


def motion_swim(state, i, n):
    """A tadpole in water: drifting side to side, swimming hard while it works, bobbing to call."""
    if state == "idle":
        return dict(dx=0.025 * _wave(i, n), angle=3 * _wave(i, n, ph=0.25), dy=0.01 * _wave(i, n, 2))
    if state == "thinking":
        return dict(dx=0.015 * _wave(i, n), angle=2 * _wave(i, n, ph=0.25))
    if state == "working":
        return dict(dx=0.035 * _wave(i, n), angle=7 * _wave(i, n, ph=0.25))
    if state == "needs_you":
        return dict(dy=-0.03 * _arch(i, 6))
    return dict(dy=0.015 * _wave(i, n), angle=2)


def motion_hop(state, i, n):
    """A froglet: breathing with a hop now and then, hopping on the spot while it works, a big jump to call."""
    if state == "idle":
        hop = -0.045 * math.sin(math.pi * (i - 6) / 6) if 6 <= i < 12 else 0.0
        return dict(scale=1 + 0.01 * _wave(i, n) if not hop else 1.0, dy=hop)
    if state == "thinking":
        return dict(angle=2 * _wave(i, n))
    if state == "working":
        return dict(dy=-0.04 * _arch(i, 4), scale=0.97 if i % 4 == 0 else 1.0)
    if state == "needs_you":
        return dict(dy=-0.07 * _arch(i, 6), scale=0.96 if i % 6 == 0 else 1.0)
    return dict(scale=1 + 0.008 * _wave(i, n))


def motion_shake(state, i, n):
    """The laughing frog: shaking with laughter, lunging into its punches, a snore you can see."""
    if state == "idle":
        return dict(angle=2.5 * _wave(i, n, 3), dy=-0.01 * abs(_wave(i, n, 3)), scale=1 + 0.015 * abs(_wave(i, n, 3)))
    if state == "thinking":
        return dict(angle=1.5 * _wave(i, n))
    if state == "working":
        return dict(scale=1.05 if i % 2 == 0 else 1.0, angle=1.5 if i % 4 < 2 else -1.5)
    if state == "needs_you":
        return dict(angle=3 * _wave(i, n, 2), dy=-0.02 * abs(_wave(i, n, 2)))
    return dict(scale=1 + 0.015 * _wave(i, n))


def motion_float(state, i, n):
    """The cosmic frog hovers: floating, powering up while it works, drifting up to call, sunk low asleep."""
    if state == "idle":
        return dict(dy=-0.01 - 0.025 * _wave(i, n), angle=1.5 * _wave(i, n, ph=0.25))
    if state == "thinking":
        return dict(dy=-0.01 - 0.02 * _wave(i, n), angle=1 * _wave(i, n, ph=0.25))
    if state == "working":
        return dict(scale=1 + 0.025 * _wave(i, n, 2), dy=-0.015 * _wave(i, n))
    if state == "needs_you":
        return dict(dy=-0.015 - 0.03 * _wave(i, n))
    return dict(dy=0.01, scale=1 + 0.006 * _wave(i, n))


# What a tier's spec names as its "motion": each a function of the state, the frame and the frame count, giving the
# frame's scale, angle, dx, dy (fractions of the side) and pivot. Frame 0 stays close to rest: it is the still one.
MOTIONS = {"breathe": motion_breathe, "rock": motion_rock, "peek": motion_peek, "swim": motion_swim,
           "hop": motion_hop, "shake": motion_shake, "float": motion_float}


class Frames:
    """Draws the round frames of one persona at W = side × ss (supersampled, scaled down at the end). A theme (a tier's
    backdrop, THEMES) paints the disc, what moves around the character and the rim; without one it is a pale disc of
    the secondary color under a white rim."""

    def __init__(self, poses, colors, side, ss=3, framing=None, theme=None, motion=None, center="head", vertical="top"):
        self.side, self.W = side, side * ss
        self.poses = poses
        self.place = placements(poses, self.W, framing or {}, center, vertical)
        self.motion = MOTIONS[motion or "breathe"]
        light = colors["light"]
        self.primary = hex_rgb(light["primary"])
        self.secondary = hex_rgb(light["secondary"])
        self.accent = hex_rgb(light["accent"])
        self.theme = theme
        if theme:
            self.disc, self.mask = theme.disc(self.W)
            self.disc_off, _ = disc_layer(self.W, *theme.off)
            self.arc = theme.arc or self.accent
            self.z = theme.z
        else:
            top = mix(self.secondary, (255, 255, 255), 0.84)
            self.disc, self.mask = disc_layer(self.W, top, mix(self.secondary, (255, 255, 255), 0.66))
            gray = (232, 232, 236)
            self.disc_off, _ = disc_layer(self.W, gray, (208, 208, 214))
            self.arc = self.accent
            self.z = (72, 72, 84)

    def pose(self, name):
        return self.poses.get(name) or self.poses["avatar"]

    def move(self, state, i, n):
        """The frame's transform in this persona's motion."""
        return self.motion(state, i, n)

    def frame(self, name, *, scale=1.0, angle=0.0, dx=0.0, dy=0.0, pivot=0.5, off=False, draw=None, t=0.0):
        """One frame; t (0–1) is where the loop is, for what the theme moves."""
        from PIL import Image, ImageDraw, ImageEnhance
        W = self.W
        ch = affine(self.pose(name), W, self.place[name if name in self.poses else "avatar"], scale, angle, dx * W, dy * W,
                    pivot)
        if off:
            alpha = ch.getchannel("A")
            ch = ImageEnhance.Color(ch.convert("RGB")).enhance(0.2)
            ch = ImageEnhance.Brightness(ch).enhance(0.92).convert("RGBA")
            ch.putalpha(alpha)
        out = (self.disc_off if off else self.disc).copy()
        if self.theme and not off:
            self.theme.paint(out, self.mask, W, t, "behind")
        clipped = Image.new("RGBA", (W, W), (0, 0, 0, 0))
        clipped.paste(ch, (0, 0), Image.composite(ch.getchannel("A"), Image.new("L", (W, W), 0), self.mask))
        out = Image.alpha_composite(out, clipped)
        if self.theme and not off:
            self.theme.paint(out, self.mask, W, t, "front")
        if self.theme:
            self.theme.rim(out, W, t, off)
        else:
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
        return [self.frame("blink" if i in (19, 20) and "blink" in self.poses else "avatar", t=i / n,
                           **self.move("idle", i, n))
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
        return [self.frame("think", draw=bubble(i), t=i / n, **self.move("thinking", i, n)) for i in range(n)], 8

    def working(self):
        """The two beats of the move with a bob, and an arc going round the rim (12 frames, 10 fps)."""
        n = 12

        def arc(i):
            def draw(img, d):
                W = self.W
                w = W * 0.075
                box = (w / 2, w / 2, W - w / 2, W - w / 2)
                d.ellipse(box, outline=self.arc + (60,), width=round(w))
                start = i * 360 / n - 90
                if self.theme and self.theme.comet:
                    # A comet going round the rim: a bright head, a tail fading out behind it.
                    for k in range(10):
                        a0 = start + 110 - (k + 1) * 11
                        d.arc(box, a0, a0 + 12, fill=self.arc + (round(255 * (1 - k / 10) ** 1.5),),
                              width=max(1, round(w * (1 - k / 14))))
                    hx = W / 2 + (W / 2 - w / 2) * math.cos(math.radians(start + 110))
                    hy = W / 2 + (W / 2 - w / 2) * math.sin(math.radians(start + 110))
                    r = w * 0.75
                    d.ellipse((hx - r, hy - r, hx + r, hy + r), fill=(255, 255, 255, 255))
                    return
                d.arc(box, start, start + 110, fill=self.arc + (255,), width=round(w))
            return draw
        return [self.frame("work_a" if i % 4 < 2 else "work_b", draw=arc(i), t=i / n, **self.move("working", i, n))
                for i in range(n)], 10

    def needs_you(self):
        """The wave's two beats inside a ring that pulses in "Needs you"'s red (12 frames, 6 fps)."""
        n = 12

        def ring(i):
            def draw(img, d):
                W = self.W
                w = W * 0.07
                alpha = round(255 * (0.7 + 0.3 * math.cos(2 * math.pi * i / n)))
                d.ellipse((w / 2, w / 2, W - w / 2, W - w / 2), outline=NEEDS_YOU + (alpha,), width=round(w))
            return draw
        return [self.frame("wave_a" if (i // 3) % 2 == 0 else "wave_b", draw=ring(i), t=i / n, **self.move("needs_you", i, n))
                for i in range(n)], 6

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
                    zig(d, x, y, size, self.z + (a,), max(2, round(W * 0.028)))
            return draw
        return [self.frame("sleep", off=True, draw=zs(i), t=i / n, **self.move("offline", i, n))
                for i in range(n)], 4


# MARK: themes (a tier's backdrop)

class Theme:
    """A tier's backdrop: the disc (a vertical gradient, or a radial one: center → edge), what moves behind and in
    front of the character (functions of the layer, its side and the loop's phase t), the rim, the working arc's
    color and the offline disc. What moves is clipped to the disc and loops with t, so every state loops whole."""

    def __init__(self, top, bottom, radial=False, behind=None, front=None, rim=None, arc=None, dark=False, comet=False):
        self.top, self.bottom, self.radial, self.comet = top, bottom, radial, comet
        self.behind, self.front, self.rim_fn, self.arc = behind, front, rim, arc
        self.off = ((58, 58, 64), (30, 30, 34)) if dark else ((232, 232, 236), (208, 208, 214))
        self.z = (205, 205, 215) if dark else (72, 72, 84)

    def disc(self, W):
        if not self.radial:
            return disc_layer(W, self.top, self.bottom)
        from PIL import Image, ImageDraw
        import numpy as np
        y, x = np.mgrid[0:W, 0:W] / (W - 1)
        r = np.clip(np.hypot(x - 0.5, y - 0.42) / 0.62, 0, 1)[..., None]
        rgb = np.array(self.top) * (1 - r) + np.array(self.bottom) * r
        grad = Image.fromarray(rgb.astype("uint8"), "RGB").convert("RGBA")
        mask = Image.new("L", (W, W), 0)
        ImageDraw.Draw(mask).ellipse((0, 0, W - 1, W - 1), fill=255)
        out = Image.new("RGBA", (W, W), (0, 0, 0, 0))
        out.paste(grad, (0, 0), mask)
        return out, mask

    def paint(self, img, mask, W, t, where):
        fn = self.behind if where == "behind" else self.front
        if not fn:
            return
        from PIL import Image, ImageChops
        layer = Image.new("RGBA", (W, W), (0, 0, 0, 0))
        fn(layer, W, t)
        layer.putalpha(ImageChops.multiply(layer.getchannel("A"), mask))
        img.alpha_composite(layer)

    def rim(self, img, W, t, off):
        if self.rim_fn and not off:
            self.rim_fn(img, W, t)
            return
        from PIL import ImageDraw
        lw = max(1, round(W * 0.012))
        ImageDraw.Draw(img).ellipse((lw / 2, lw / 2, W - 1 - lw / 2, W - 1 - lw / 2), outline=(255, 255, 255, 110), width=lw)


def _loop(t, k, n):
    """Particle k of n's own place in the loop (0–1)."""
    return (t + k / n) % 1


def _fade(u):
    """In over the first fifth, out over the last."""
    return max(0.0, min(1.0, u / 0.2, (1 - u) / 0.2))


def _blurred(layer, radius):
    from PIL import ImageFilter
    return layer.filter(ImageFilter.GaussianBlur(radius))


def _heart(d, cx, cy, r, fill):
    d.ellipse((cx - r, cy - r * 0.9, cx + r * 0.05, cy + r * 0.15), fill=fill)
    d.ellipse((cx - r * 0.05, cy - r * 0.9, cx + r, cy + r * 0.15), fill=fill)
    d.polygon([(cx - r * 0.97, cy - r * 0.25), (cx + r * 0.97, cy - r * 0.25), (cx, cy + r * 1.05)], fill=fill)


def _sparkle(d, cx, cy, r, fill):
    k = 0.28
    d.polygon([(cx, cy - r), (cx + r * k, cy - r * k), (cx + r, cy), (cx + r * k, cy + r * k), (cx, cy + r),
               (cx - r * k, cy + r * k), (cx - r, cy), (cx - r * k, cy - r * k)], fill=fill)


def theme_hearts(layer, W, t):
    """Little pink hearts floating up beside the egg."""
    from PIL import ImageDraw
    d = ImageDraw.Draw(layer)
    for k, (x, r) in enumerate([(0.16, 0.05), (0.84, 0.042), (0.74, 0.034), (0.27, 0.03)]):
        u = _loop(t, k, 4)
        a = round(220 * _fade(u))
        _heart(d, W * (x + 0.02 * math.sin(2 * math.pi * (u + k / 4))), W * (0.86 - 0.62 * u), W * r, (255, 128, 160, a))


def theme_bubbles(layer, W, t):
    """Bubbles rising through pale water."""
    from PIL import ImageDraw
    d = ImageDraw.Draw(layer)
    for k, (x, r) in enumerate([(0.14, 0.045), (0.86, 0.035), (0.24, 0.025), (0.78, 0.05), (0.9, 0.022)]):
        u = _loop(t, k, 5)
        a = _fade(u)
        cx, cy, rr = W * (x + 0.015 * math.sin(2 * math.pi * (2 * u + k / 5))), W * (0.92 - 0.8 * u), W * r
        d.ellipse((cx - rr, cy - rr, cx + rr, cy + rr), fill=(255, 255, 255, round(60 * a)),
                  outline=(255, 255, 255, round(220 * a)), width=max(1, round(W * 0.008)))
        d.ellipse((cx - rr * 0.5, cy - rr * 0.55, cx - rr * 0.15, cy - rr * 0.2), fill=(255, 255, 255, round(230 * a)))


def theme_meadow(layer, W, t):
    """A meadow at the bottom: grass, and small flowers nodding in turn."""
    from PIL import ImageDraw
    d = ImageDraw.Draw(layer)
    blades = []
    for i in range(15):
        x = i / 14
        h = 0.1 + 0.05 * ((i * 7) % 3)
        sway = 0.012 * math.sin(2 * math.pi * (t + i / 15))
        blades += [(W * (x - 0.04), W), (W * (x + sway), W * (1 - h)), (W * (x + 0.04), W)]
    for i in range(0, len(blades), 3):
        d.polygon(blades[i:i + 3], fill=(108, 186, 128, 200))
    for k, (x, y) in enumerate([(0.12, 0.8), (0.88, 0.78), (0.2, 0.9), (0.8, 0.9)]):
        bob = 0.012 * math.sin(2 * math.pi * (t + k / 4))
        cx, cy, r = W * x, W * (y + bob), W * 0.028
        for j in range(5):
            ang = 2 * math.pi * j / 5 + 2 * math.pi * t / 5
            px, py = cx + r * math.cos(ang), cy + r * math.sin(ang)
            d.ellipse((px - r * 0.7, py - r * 0.7, px + r * 0.7, py + r * 0.7), fill=(255, 255, 255, 235))
        d.ellipse((cx - r * 0.55, cy - r * 0.55, cx + r * 0.55, cy + r * 0.55), fill=(250, 200, 60, 255))


def _rays(layer, W, t, n, colors, center=(0.5, 0.45), spin=1.0):
    """n wedges round a point, turning one wedge a loop (so the loop has no jump)."""
    from PIL import ImageDraw
    d = ImageDraw.Draw(layer)
    cx, cy = W * center[0], W * center[1]
    R = W * 1.2
    for i in range(n):
        a0 = 2 * math.pi * (i + spin * t) / n
        a1 = a0 + math.pi / n
        d.polygon([(cx, cy), (cx + R * math.cos(a0), cy + R * math.sin(a0)), (cx + R * math.cos(a1), cy + R * math.sin(a1))],
                  fill=colors[i % len(colors)])


def theme_sunny(layer, W, t):
    """Soft sun rays turning slowly behind the frog."""
    from PIL import Image
    rays = Image.new("RGBA", layer.size, (0, 0, 0, 0))
    _rays(rays, W, t, 12, [(255, 255, 255, 70)])
    layer.alpha_composite(_blurred(rays, W * 0.01))


def theme_blaze_behind(layer, W, t):
    """A hot glow and orange-gold rays turning behind the laughing frog, embers rising."""
    from PIL import Image, ImageDraw
    glow = Image.new("RGBA", layer.size, (0, 0, 0, 0))
    ImageDraw.Draw(glow).ellipse((W * 0.12, W * 0.12, W * 0.88, W * 0.88), fill=(255, 120, 20, 150))
    layer.alpha_composite(_blurred(glow, W * 0.12))
    rays = Image.new("RGBA", layer.size, (0, 0, 0, 0))
    _rays(rays, W, t, 16, [(255, 150, 30, 90), (255, 210, 80, 60)])
    layer.alpha_composite(_blurred(rays, W * 0.006))
    d = ImageDraw.Draw(layer)
    for k, x in enumerate([0.12, 0.88, 0.2, 0.8, 0.5, 0.33, 0.67]):
        u = _loop(t, k, 7)
        r = W * (0.012 + 0.006 * (k % 2))
        cx, cy = W * (x + 0.02 * math.sin(2 * math.pi * (u + k / 7))), W * (0.95 - 0.75 * u)
        d.ellipse((cx - r, cy - r, cx + r, cy + r), fill=(255, 190 - 40 * (k % 2), 60, round(230 * _fade(u))))


def theme_blaze_rim(img, W, t):
    """A ring of fire light that flickers."""
    from PIL import Image, ImageDraw
    lw = W * 0.03
    flick = 0.75 + 0.25 * math.sin(2 * math.pi * 3 * t)
    ring = Image.new("RGBA", img.size, (0, 0, 0, 0))
    ImageDraw.Draw(ring).ellipse((lw, lw, W - 1 - lw, W - 1 - lw), outline=(255, 140, 20, round(255 * flick)), width=round(lw))
    img.alpha_composite(_blurred(ring, W * 0.012))
    ImageDraw.Draw(img).ellipse((lw * 0.6, lw * 0.6, W - 1 - lw * 0.6, W - 1 - lw * 0.6), outline=(255, 214, 120, 230),
                                width=max(1, round(W * 0.01)))


# Stars of the cosmic disc: (x, y, size, phase); the bright ones are gold sparkles.
STARS = [(0.12, 0.3, 1, 0.0), (0.22, 0.14, 2, 0.3), (0.34, 0.08, 1, 0.6), (0.7, 0.1, 1, 0.2), (0.84, 0.22, 2, 0.8),
         (0.9, 0.42, 1, 0.5), (0.08, 0.56, 1, 0.7), (0.16, 0.74, 2, 0.1), (0.88, 0.66, 1, 0.4), (0.8, 0.84, 2, 0.9),
         (0.28, 0.9, 1, 0.35), (0.62, 0.94, 1, 0.65), (0.5, 0.05, 1, 0.15), (0.04, 0.4, 1, 0.85), (0.95, 0.55, 1, 0.25)]


def theme_cosmic_behind(layer, W, t):
    """A nebula and twinkling stars; two gold sparkles flare in turn."""
    from PIL import Image, ImageDraw
    neb = Image.new("RGBA", layer.size, (0, 0, 0, 0))
    d = ImageDraw.Draw(neb)
    d.ellipse((W * -0.1, W * 0.45, W * 0.6, W * 1.05), fill=(140, 90, 255, 120))
    d.ellipse((W * 0.45, W * -0.05, W * 1.1, W * 0.55), fill=(60, 140, 255, 90))
    d.ellipse((W * 0.25, W * 0.2, W * 0.75, W * 0.7), fill=(190, 120, 255, 70))
    layer.alpha_composite(_blurred(neb, W * 0.09))
    d = ImageDraw.Draw(layer)
    for x, y, size, ph in STARS:
        tw = 0.45 + 0.55 * (0.5 + 0.5 * math.sin(2 * math.pi * (t + ph)))
        r = W * (0.006 if size == 1 else 0.01)
        d.ellipse((W * x - r, W * y - r, W * x + r, W * y + r), fill=(255, 255, 255, round(255 * tw)))
    for k, (x, y, r) in enumerate([(0.85, 0.3, 0.06), (0.14, 0.4, 0.045), (0.82, 0.74, 0.04)]):
        u = _loop(t, k, 3)
        s = math.sin(math.pi * u) ** 2
        _sparkle(d, W * x, W * y, W * r * (0.5 + 0.5 * s), (250, 190, 40, round(110 + 145 * s)))


def theme_cosmic_rim(img, W, t):
    """A neon violet ring with a glow that breathes."""
    from PIL import Image, ImageDraw
    lw = W * 0.03
    glow = 0.7 + 0.3 * math.sin(2 * math.pi * t)
    ring = Image.new("RGBA", img.size, (0, 0, 0, 0))
    ImageDraw.Draw(ring).ellipse((lw, lw, W - 1 - lw, W - 1 - lw), outline=(160, 120, 255, round(255 * glow)), width=round(lw))
    img.alpha_composite(_blurred(ring, W * 0.014))
    ImageDraw.Draw(img).ellipse((lw * 0.6, lw * 0.6, W - 1 - lw * 0.6, W - 1 - lw * 0.6), outline=(214, 200, 255, 235),
                                width=max(1, round(W * 0.01)))


# The tiers' backdrops (scripts/assets/personas/<id>.json names one per tier): the low ones pale and soft, the high
# ones dark, glowing and busy.
THEMES = {
    "shell": Theme((247, 243, 236), (226, 218, 204)),
    "hearts": Theme((255, 248, 228), (251, 228, 184), behind=theme_hearts),
    "bubbles": Theme((232, 247, 253), (176, 222, 243), behind=theme_bubbles),
    "meadow": Theme((236, 249, 239), (196, 235, 207), front=theme_meadow),
    "sunny": Theme((255, 246, 214), (212, 236, 190), behind=theme_sunny),
    "blaze": Theme((70, 36, 14), (18, 9, 5), radial=True, behind=theme_blaze_behind, rim=theme_blaze_rim,
                   arc=(255, 186, 60), dark=True, comet=True),
    "cosmic": Theme((48, 30, 110), (8, 6, 26), radial=True, behind=theme_cosmic_behind, rim=theme_cosmic_rim,
                    arc=(178, 156, 255), dark=True, comet=True),
}


def shadow(img, box, blur):
    from PIL import Image, ImageDraw, ImageFilter
    layer = Image.new("RGBA", img.size, (0, 0, 0, 0))
    ImageDraw.Draw(layer).rounded_rectangle(box, radius=(box[3] - box[1]) / 2, fill=(0, 0, 0, 60))
    img.alpha_composite(layer.filter(ImageFilter.GaussianBlur(blur)))


def zig(d, x, y, size, color, width):
    """A "z" from three strokes (no font: the same on every machine)."""
    pts = [(x, y), (x + size, y), (x, y + size), (x + size, y + size)]
    d.line(pts, fill=color, width=width, joint="curve")


def save_apng(frames, fps, path, palette=False):
    """palette: 256 colors shared by every frame (with alpha), about a third the bytes; for the packages built into
    personad, where every hub downloads them with each update."""
    if palette:
        frames = shared_palette(frames)
    frames[0].save(path, save_all=True, append_images=frames[1:], duration=round(1000 / fps), loop=0,
                   disposal=0, blend=0, optimize=palette)


def shared_palette(frames):
    """The frames in one 256-color palette (alpha kept): quantized as one strip, so no frame flickers to its own."""
    from PIL import Image
    w, h = frames[0].size
    strip = Image.new("RGBA", (w, h * len(frames)))
    for i, f in enumerate(frames):
        strip.paste(f, (0, i * h))
    q = strip.quantize(colors=256, method=Image.Quantize.FASTOCTREE, dither=Image.Dither.FLOYDSTEINBERG)
    return [q.crop((0, i * h, w, (i + 1) * h)) for i in range(len(frames))]


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
    if os.path.exists(out):
        shutil.rmtree(out)
    manifest, big, sheets = write_look(out, "", pid, name, subtitle, poses, colors, framing)
    with open(os.path.join(out, "persona.json"), "w") as f:
        json.dump(manifest, f, ensure_ascii=False, indent=2)
        f.write("\n")
    preview(out, big, sheets, colors).save(os.path.join(out, "preview.png"), optimize=True)


def write_look(root, prefix, pid, name, subtitle, poses, colors, framing, theme=None, small=False, motion=None,
               center="head", vertical="top"):
    """The avatar sizes and the dot's APNGs under root/prefix; returns the manifest (its files named from root), the
    big avatar and each state's frames. small: the packages built into personad (shared palettes, no 512 avatar)."""
    from PIL import Image
    os.makedirs(os.path.join(root, prefix, "avatar"), exist_ok=True)
    os.makedirs(os.path.join(root, prefix, "dot"), exist_ok=True)
    # The avatar: drawn once at 1024 and scaled down, so every size is the same picture.
    big = Frames(poses, colors, 1024, ss=1, framing=framing, theme=theme, center=center, vertical=vertical).frame("avatar")
    sizes = {}
    for sz in AVATAR_SIZES[:-1] if small else AVATAR_SIZES:
        f = prefix + "avatar/%d.png" % sz
        big.resize((sz, sz), Image.Resampling.LANCZOS).save(os.path.join(root, f), optimize=True)
        sizes[str(sz)] = f
    fr = Frames(poses, colors, DOT, framing=framing, theme=theme, motion=motion, center=center, vertical=vertical)
    states, sheets = {}, {}
    for st in STATES:
        frames, fps = getattr(fr, st)()
        f = prefix + "dot/%s.png" % st
        save_apng(frames, fps, os.path.join(root, f), palette=small)
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
    return manifest, big, sheets


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
    """The rules the hub and the app hold a package to (docs/apple/persona.md); exits with what breaks them. A tiered one:
    its own persona.json (the default tier's, for hubs that do not know tiers) and every tier's."""
    ensure_deps()
    errs = []
    try:
        m = json.load(open(os.path.join(d, "persona.json")))
    except (OSError, ValueError) as e:
        sys.exit("persona.json: %s" % e)
    pid = m.get("id", "")
    if os.path.basename(os.path.normpath(d)) != pid:
        errs.append("the directory is %r, the id %r" % (os.path.basename(os.path.normpath(d)), pid))
    check_manifest(d, m, pid, errs, "")
    tiers = m.get("tiers")
    if tiers is not None:
        levels = tiers.get("levels") or []
        keys = [t.get("key") for t in levels]
        if tiers.get("default") not in keys:
            errs.append("tiers: the default %r is not one of %s" % (tiers.get("default"), keys))
        src = tiers.get("source") or {}
        if src.get("kind") != "nwverse-board" or not str(src.get("url", "")).startswith("https://"):
            errs.append("tiers: a source the hub does not read: %r" % src)
        for t in levels:
            label = "tier %s: " % t.get("key")
            try:
                tm = json.load(open(os.path.join(d, t.get("manifest", ""))))
            except (OSError, ValueError) as e:
                errs.append(label + "manifest: %s" % e)
                continue
            if "tiers" in tm:
                errs.append(label + "has tiers of its own")
            check_manifest(d, tm, pid, errs, label)
    total = sum(os.path.getsize(os.path.join(r, f)) for r, _, fs in os.walk(d) for f in fs)
    if total > MAX_TOTAL:
        errs.append("%d bytes in all, over %d" % (total, MAX_TOTAL))
    if errs:
        sys.exit("%s:\n  %s" % (d, "\n  ".join(errs)))
    print("%s: ok (%d KB)" % (d, total // 1024))


def check_manifest(d, m, pid, errs, label):
    """One persona.json of the package in d: its id, name, files (named from d), dot and colors."""
    from PIL import Image
    if m.get("format") != FORMAT:
        errs.append(label + "format %r, want %d" % (m.get("format"), FORMAT))
    if m.get("id") != pid or not ID_PATTERN.match(pid) or pid == BUILTIN:
        errs.append(label + "id %r" % m.get("id"))
    if not str(m.get("name", "")).strip():
        errs.append(label + "no name")

    def file_ok(f, what):
        p = os.path.normpath(os.path.join(d, f))
        if f.startswith("/") or ".." in f.split("/") or any(x.startswith(".") for x in f.split("/")):
            errs.append(label + "%s: %r is not a plain path in the package" % (what, f))
            return None
        if not os.path.isfile(p):
            errs.append(label + "%s: no %s" % (what, f))
            return None
        if os.path.getsize(p) > MAX_FILE:
            errs.append(label + "%s: %s is over %d bytes" % (what, f, MAX_FILE))
        return p

    sizes = (m.get("avatar") or {}).get("sizes") or {}
    if not sizes:
        errs.append(label + "no avatar sizes")
    for sz, f in sizes.items():
        p = file_ok(f, "avatar %s" % sz)
        if p:
            im = Image.open(p)
            if im.size != (int(sz), int(sz)):
                errs.append(label + "avatar %s is %dx%d" % (sz, *im.size))
    states = (m.get("dot") or {}).get("states") or {}
    if "idle" not in states:
        errs.append(label + "no idle dot")
    for st, spec in states.items():
        if st not in STATES:
            errs.append(label + "unknown state %r" % st)
            continue
        p = file_ok(spec.get("file", ""), "dot %s" % st)
        if p:
            im = Image.open(p)
            if im.size[0] != im.size[1]:
                errs.append(label + "dot %s is not square" % st)
            if getattr(im, "n_frames", 1) > 120:
                errs.append(label + "dot %s has %d frames (120 at most)" % (st, im.n_frames))
    about = m.get("about")
    if about is not None and (not isinstance(about, str) or len(about.replace(" ", "")) > ABOUT_MAX):
        errs.append(label + "about %r: a few words, %d at most" % (about, ABOUT_MAX))
    colors = m.get("colors") or {}
    for key, page in (("light", (255, 255, 255)), ("dark", (0, 0, 0))):
        c = colors.get(key) or {}
        try:
            if "accent" in c:
                a = hex_rgb(c["accent"])
                if contrast((255, 255, 255), a) < FLOOR_ON_ACCENT:
                    errs.append(label + "%s accent %s: white on it %.2f:1, under %.1f" % (key, c["accent"], contrast((255, 255, 255), a), FLOOR_ON_ACCENT))
                if contrast(a, page) < FLOOR_LINK:
                    errs.append(label + "%s accent %s on the page %.2f:1, under %.1f" % (key, c["accent"], contrast(a, page), FLOOR_LINK))
            if "bubble" in c:
                b, tx = hex_rgb(c["bubble"]), hex_rgb(c.get("bubble_text", "#FFFFFF"))
                if contrast(b, tx) < FLOOR_BUBBLE:
                    errs.append(label + "%s bubble %s: its words %.2f:1, under %.1f" % (key, c["bubble"], contrast(b, tx), FLOOR_BUBBLE))
            for k in ("primary", "secondary"):
                if k in c:
                    hex_rgb(c[k])
        except ValueError as e:
            errs.append(label + "%s: %s" % (key, e))


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


# MARK: tiers

def load_spec(path):
    """A tiered persona's spec (scripts/assets/personas/<id>.json): the source of the level, each tier's character,
    poses, theme and colors."""
    spec = json.load(open(path))
    keys = [t["key"] for t in spec["tiers"]]
    if spec.get("default") not in keys:
        sys.exit("%s: the default tier %r is not one of %s" % (path, spec.get("default"), ", ".join(keys)))
    return spec


def chosen_tiers(spec, only):
    want = set(only.split(",")) if only else None
    unknown = (want or set()) - {t["key"] for t in spec["tiers"]}
    if unknown:
        sys.exit("unknown tiers: %s" % ", ".join(sorted(unknown)))
    return [t for t in spec["tiers"] if want is None or t["key"] in want]


def tiers_generate(args):
    """Draws every tier's poses at once, one codex run each (in <work>/<tier>/, the tier's log beside its poses)."""
    spec = load_spec(args.spec)
    work = os.path.abspath(args.work)
    procs = []
    for t in chosen_tiers(spec, args.tiers):
        d = os.path.join(work, t["key"])
        os.makedirs(d, exist_ok=True)
        # A tier may draw from pictures of its own (a photo of the character instead of the board's flat icon),
        # with the words on how to draw from them that go with those.
        refs = [os.path.join(work, r.format(key=t["key"])) for r in t.get("refs") or spec["refs"]]
        cmd = [sys.executable, os.path.abspath(__file__), "generate", "--work", d, "--codex", args.codex,
               "--describe", "%s; this one is %s" % (t.get("identity") or spec["identity"], t["describe"]),
               "--style", t.get("style") or spec["style"], "--poses", json.dumps(t.get("poses") or {})]
        for flag, v in (("--camera", t.get("camera")), ("--model", args.model), ("--proxy", args.proxy),
                        ("--only", args.only)):
            if v:
                cmd += [flag, v]
        if args.force:
            cmd.append("--force")
        out = open(os.path.join(d, "generate.out"), "a")
        procs.append((t["key"], subprocess.Popen(cmd + refs, stdout=out, stderr=subprocess.STDOUT), out))
        print("%s: drawing (log in %s/generate.out)" % (t["key"], d), flush=True)
    failed = []
    for key, proc, out in procs:
        proc.wait()
        out.close()
        print("%s: %s" % (key, "done" if proc.returncode == 0 else "failed (%d)" % proc.returncode), flush=True)
        if proc.returncode:
            failed.append(key)
    if failed:
        sys.exit("draw again: --tiers %s (each tier's generate.out says which poses)" % ",".join(failed))


def tiers_build(args):
    """The tiered package from every tier's poses (<work>/<tier>/poses, framing.json beside them): each tier's look
    under tiers/<tier>/ with its own persona.json, the package's persona.json the default tier's plus the tiers; and
    <work>/review.html, every tier's face and states on light and dark, to look over before it ships."""
    ensure_deps()
    spec = load_spec(args.spec)
    work = os.path.abspath(args.work)
    pid = spec["id"]
    out = os.path.abspath(args.out)
    if os.path.basename(out) != pid:
        sys.exit("--out must end in the package's id: .../%s" % pid)
    part = os.path.join(os.path.dirname(out), "." + pid + ".part")
    shutil.rmtree(part, ignore_errors=True)
    os.makedirs(part)
    levels, looks = [], []
    for t in spec["tiers"]:
        key = t["key"]
        d = os.path.join(work, key)
        poses = load_poses(d)
        fpath = os.path.join(d, "framing.json")
        framing = dict(t.get("framing") or {}, **(json.load(open(fpath)) if os.path.exists(fpath) else {}))
        colors = palette(hex_rgb(t["primary"]), hex_rgb(t["secondary"]),
                         hex_rgb(t["accent_light"]) if t.get("accent_light") else None,
                         hex_rgb(t["accent_dark"]) if t.get("accent_dark") else None)
        prefix = "tiers/%s/" % key
        m, big, _ = write_look(part, prefix, pid, spec["name"], t["name"], poses, colors, framing,
                               theme=THEMES[t["theme"]], small=True, motion=t.get("motion"),
                               center=spec.get("center", "head"), vertical=t.get("vertical", "top"))
        m["about"] = spec["about"]
        with open(os.path.join(part, prefix, "persona.json"), "w") as f:
            json.dump(m, f, ensure_ascii=False, indent=2)
            f.write("\n")
        levels.append({"key": key, "name": t["name"], "when": t["when"], "manifest": prefix + "persona.json"})
        looks.append((t, m, big))
        print("%s: built" % key, flush=True)
    root = dict(next(m for t, m, _ in looks if t["key"] == spec["default"]))
    root["tiers"] = {"source": spec["source"], "default": spec["default"], "levels": levels}
    with open(os.path.join(part, "persona.json"), "w") as f:
        json.dump(root, f, ensure_ascii=False, indent=2)
        f.write("\n")
    if os.path.exists(out):
        shutil.rmtree(out)
    os.rename(part, out)
    check_package(out)
    total = sum(os.path.getsize(os.path.join(r, f)) for r, _, fs in os.walk(out) for f in fs)
    review = os.path.join(work, "review.html")
    write_review(review, spec, out, looks, total)
    print("review: %s" % review)
    if total > BUILTIN_BUDGET:
        sys.exit("%d KB, over the %d KB a package built into personad may take" % (total // 1024, BUILTIN_BUDGET // 1024))


STATE_NAMES = {"idle": "空闲", "thinking": "思考中", "working": "工作中", "needs_you": "等你处理", "offline": "离线"}
MOTION_NAMES = {"breathe": "呼吸", "rock": "摇晃", "peek": "探头", "swim": "游动", "hop": "蹦跳", "shake": "笑得发抖",
                "float": "悬浮"}


def write_review(path, spec, pkg, looks, total):
    """One HTML file with every picture inside (data: URLs): send it, open it anywhere, the dots move."""
    import base64
    import html

    pics = {}

    def data(f):
        """A placeholder the page's script fills from one copy of the file."""
        if f not in pics:
            pics[f] = "data:image/png;base64," + base64.b64encode(open(os.path.join(pkg, f), "rb").read()).decode()
        return "#" + f

    def size(files):
        return sum(os.path.getsize(os.path.join(pkg, f)) for f in files) // 1024

    strip = "".join('<figure><img class="dot" data-f="%s"><figcaption>%s</figcaption></figure>'
                    % (data(m["dot"]["states"]["idle"]["file"]), html.escape(t["name"])) for t, m, _ in looks)
    cards = []
    for t, m, _ in looks:
        files = list(m["avatar"]["sizes"].values()) + [s["file"] for s in m["dot"]["states"].values()]
        panes = []
        for mode in ("light", "dark"):
            c = m["colors"][mode]
            sw = "".join('<i style="background:%s" title="%s %s"></i>' % (c[k], k, c[k])
                         for k in ("primary", "secondary", "accent", "bubble"))
            dots = "".join('<figure><img class="dot" data-f="%s"><figcaption>%s</figcaption></figure>'
                           % (data(m["dot"]["states"][st]["file"]), STATE_NAMES[st]) for st in STATES)
            panes.append('<div class="pane %s"><div class="dots">%s</div><div class="sw">%s</div></div>' % (mode, dots, sw))
        cards.append(
            '<section><header><img class="face" data-f="%s"><div><h2>%s</h2><p>%s</p><p class="k">动作 %s · 背景 %s · %d KB</p>'
            '</div></header>%s</section>'
            % (data(m["avatar"]["sizes"]["256"]), html.escape(t["name"]), html.escape(t.get("condition", "")),
               MOTION_NAMES.get(t.get("motion") or "breathe", t.get("motion")), t["theme"], size(files), "".join(panes)))
    page = """<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width">
<title>%(name)s形象 · 审批</title>
<style>
body{margin:0;font:15px/1.5 -apple-system,BlinkMacSystemFont,"PingFang SC",sans-serif;background:#f2f2f7;color:#1c1c1e}
main{max-width:1080px;margin:0 auto;padding:32px 20px 60px}
h1{font-size:28px;margin:0 0 4px}.lead{color:#6e6e73;margin:0 0 24px}
.strip{display:flex;gap:18px;flex-wrap:wrap;background:#fff;border-radius:18px;padding:18px 20px;margin-bottom:28px}
figure{margin:0;text-align:center}figcaption{font-size:12px;color:#8e8e93;margin-top:4px}
.dot{width:92px;height:92px;display:block}
section{background:#fff;border-radius:22px;padding:20px;margin-bottom:22px;box-shadow:0 1px 2px rgba(0,0,0,.04)}
header{display:flex;gap:20px;align-items:center;margin-bottom:16px}
.face{width:128px;height:128px;border-radius:50%%}
h2{margin:0;font-size:22px}header p{margin:2px 0 0}.k{color:#8e8e93;font-size:13px}
.pane{display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:12px;border-radius:16px;padding:16px;margin-top:10px}
.pane.light{background:#f2f2f7}.pane.dark{background:#1c1c1e}.pane.dark figcaption{color:#98989d}
.dots{display:flex;gap:16px;flex-wrap:wrap}
.sw{display:flex;gap:6px}.sw i{width:26px;height:26px;border-radius:8px;display:block;box-shadow:inset 0 0 0 1px rgba(127,127,127,.25)}
</style>
<main>
<h1>%(name)s形象 · 7 档</h1>
<p class="lead">等级取自 NWVERSE 贡献看板；卡片第二行写「%(about)s」。下面每档是头像和顶栏圆点的五个状态（会动），浅色、深色各一行，右边是这一档的配色。整包 %(kb)d KB。</p>
<div class="strip">%(strip)s</div>
%(cards)s
</main>
<script>const P=%(pics)s;document.querySelectorAll("img[data-f]").forEach(i=>i.src=P[i.dataset.f.slice(1)]);</script>
</html>
""" % {"name": html.escape(spec["name"]), "about": html.escape(spec["about"]), "kb": total // 1024, "strip": strip,
       "cards": "".join(cards), "pics": json.dumps(pics)}
    with open(path, "w") as f:
        f.write(page)


# MARK: install

def install(args):
    d = os.path.abspath(args.dir)
    check_package(d)
    pid = os.path.basename(os.path.normpath(d))
    host = args.hub
    if not host:
        cfg = subprocess.run([sys.executable, os.path.join(ROOT, "scripts", "persona_config.py"), "env"],
                             capture_output=True, text=True, check=True).stdout
        m = re.search(r"PERSONA_HUB_SSH=(\S+)", cfg)
        host = shlex.split(m.group(1))[0] if m else ""
    if not host:
        sys.exit("no hub: --hub HOST, or hub.machine in ~/.config/persona/config.json (docs/operations/config.md)")
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as t:
        for name in sorted(os.listdir(d)):
            t.add(os.path.join(d, name), arcname=name)
    # Next to the old one under a dot name (the hub skips those), then swapped in: the phone never lists half a package.
    script = ("set -e; d=~/.persona/personas; mkdir -p $d; rm -rf $d/.{id}.new $d/.{id}.old; mkdir $d/.{id}.new; "
              "tar -xzf - -C $d/.{id}.new; [ -d $d/{id} ] && mv $d/{id} $d/.{id}.old; mv $d/.{id}.new $d/{id}; "
              "rm -rf $d/.{id}.old").format(id=pid)
    subprocess.run(["ssh", host, script], input=buf.getvalue(), check=True)
    print("installed %s on %s:~/.persona/personas/%s (phones pick it up in 设置 › 形象)" % (pid, host, pid))


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
        p.add_argument("--style", help="how to draw it (a 3D toy look from flat pictures)")
        p.add_argument("--camera", help="where the character sits in every pose, instead of head and upper body")
        p.add_argument("--poses", help='JSON {"think": "what this pose shows instead"}')

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
    p = sub.add_parser("tiers", help="a persona whose look follows a level (scripts/assets/personas/<id>.json)")
    tsub = p.add_subparsers(dest="action", required=True)
    t = tsub.add_parser("generate", help="draw every tier's poses with Codex, the tiers at once")
    t.add_argument("--spec", required=True)
    t.add_argument("--work", required=True, help="<work>/refs/ holds the pictures the spec names; poses go in <work>/<tier>/")
    t.add_argument("--tiers", help="only these tiers (comma separated)")
    t.add_argument("--only", help="only these poses: " + ",".join(POSE_NAMES))
    t.add_argument("--force", action="store_true")
    t.add_argument("--model")
    t.add_argument("--proxy")
    t.add_argument("--codex", default="codex")
    t.set_defaults(fn=tiers_generate)
    t = tsub.add_parser("build", help="the tiered package from every tier's poses, and <work>/review.html")
    t.add_argument("--spec", required=True)
    t.add_argument("--work", required=True)
    t.add_argument("--out", required=True, help="the package directory, e.g. server/internal/hub/personas/<id>")
    t.set_defaults(fn=tiers_build)
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
