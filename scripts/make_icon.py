#!/usr/bin/env python3
"""App icon: a coral four-point spark inside a glowing reactor ring on a warm night background.

Drawn at 4x and downsampled. Writes the iPhone icon (any / dark / tinted), the watch icon and docs/icon-preview.png:
  python3 scripts/make_icon.py
"""
import json
import math
import os

from PIL import Image, ImageChops, ImageDraw, ImageFilter

S = 1024
K = 4
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
IOS = os.path.join(ROOT, "apple/Jarvis/Resources/Assets.xcassets/AppIcon.appiconset")
WATCH = os.path.join(ROOT, "apple/JarvisWatch/Assets.xcassets/AppIcon.appiconset")

CORAL = (238, 124, 84)
PEACH = (255, 196, 160)
EMBER = (176, 62, 34)
NIGHT_TOP = (40, 24, 18)
NIGHT_BOTTOM = (10, 8, 8)


def gradient(top, bottom, size):
    img = Image.new("RGB", (size, size))
    d = ImageDraw.Draw(img)
    for y in range(size):
        t = y / (size - 1)
        d.line([(0, y), (size, y)], fill=tuple(int(a + (b - a) * t) for a, b in zip(top, bottom)))
    return img


def spark(size, cx, cy, r, waist):
    """Four-point star: concave diamond with smooth curved sides."""
    pts = []
    n = 720
    for i in range(n):
        a = 2 * math.pi * i / n
        # Superellipse-like astroid: |x|^p + |y|^p = r^p with p < 1 gives concave sides.
        p = waist
        c, s = math.cos(a), math.sin(a)
        x = math.copysign(abs(c) ** (2 / p), c) * r
        y = math.copysign(abs(s) ** (2 / p), s) * r
        pts.append((cx + x, cy + y))
    m = Image.new("L", (size, size), 0)
    ImageDraw.Draw(m).polygon(pts, fill=255)
    return m


def ring(size, cx, cy, r, width):
    m = Image.new("L", (size, size), 0)
    d = ImageDraw.Draw(m)
    d.ellipse([cx - r, cy - r, cx + r, cy + r], outline=255, width=width)
    return m


def render(variant):
    big = S * K
    c = big / 2
    if variant == "tinted":
        base = Image.new("RGB", (big, big), (0, 0, 0))
    else:
        base = gradient(NIGHT_TOP, NIGHT_BOTTOM, big)
    img = base.convert("RGBA")

    # Soft warm glow behind everything.
    glow = Image.new("L", (big, big), 0)
    ImageDraw.Draw(glow).ellipse([c - big * 0.36, c - big * 0.36, c + big * 0.36, c + big * 0.36], fill=150)
    glow = glow.filter(ImageFilter.GaussianBlur(big * 0.09))
    glow_col = (255, 255, 255) if variant == "tinted" else CORAL
    img = Image.composite(Image.new("RGBA", (big, big), glow_col + (255,)), img, glow.point(lambda v: int(v * 0.55)))

    # Reactor ring with a bloom.
    r_mask = ring(big, c, c, big * 0.335, int(big * 0.022))
    bloom = r_mask.filter(ImageFilter.GaussianBlur(big * 0.018))
    ring_col = (255, 255, 255) if variant == "tinted" else PEACH
    img = Image.composite(Image.new("RGBA", (big, big), ring_col + (255,)), img, bloom.point(lambda v: int(v * 0.7)))
    img = Image.composite(Image.new("RGBA", (big, big), ring_col + (255,)), img, r_mask.point(lambda v: int(v * 0.85)))

    # Tick marks on the ring (12 of them, the cardinal ones longer).
    ticks = Image.new("L", (big, big), 0)
    td = ImageDraw.Draw(ticks)
    for i in range(12):
        a = 2 * math.pi * i / 12
        inner = big * (0.375 if i % 3 else 0.37)
        outer = big * 0.395
        td.line([(c + inner * math.sin(a), c - inner * math.cos(a)), (c + outer * math.sin(a), c - outer * math.cos(a))],
                fill=255, width=int(big * (0.012 if i % 3 == 0 else 0.007)))
    img = Image.composite(Image.new("RGBA", (big, big), ring_col + (255,)), img, ticks.point(lambda v: int(v * 0.55)))

    # The spark: vertical gradient peach → coral → ember.
    s_mask = spark(big, c, c, big * 0.25, 0.62)
    s_grad = gradient(PEACH, EMBER, big) if variant != "tinted" else Image.new("RGB", (big, big), (255, 255, 255))
    s_bloom = s_mask.filter(ImageFilter.GaussianBlur(big * 0.03))
    img = Image.composite(Image.new("RGBA", (big, big), (CORAL if variant != "tinted" else (255, 255, 255)) + (255,)), img,
                          s_bloom.point(lambda v: int(v * 0.6)))
    img = Image.composite(s_grad.convert("RGBA"), img, s_mask)

    # Small companion spark, upper right.
    s2 = spark(big, c + big * 0.165, c - big * 0.165, big * 0.045, 0.62)
    img = Image.composite(Image.new("RGBA", (big, big), (PEACH if variant != "tinted" else (255, 255, 255)) + (255,)), img, s2)

    out = img.resize((S, S), Image.LANCZOS).convert("RGB")
    if variant == "tinted":
        out = out.convert("L").convert("RGB")
    return out


def write_set(folder, images, contents):
    os.makedirs(folder, exist_ok=True)
    for name, im in images.items():
        im.save(os.path.join(folder, name))
    with open(os.path.join(folder, "Contents.json"), "w") as f:
        json.dump(contents, f, indent=2)


def main():
    light = render("any")
    tinted = render("tinted")
    write_set(IOS, {"icon.png": light, "icon-dark.png": light, "icon-tinted.png": tinted}, {
        "images": [
            {"filename": "icon.png", "idiom": "universal", "platform": "ios", "size": "1024x1024"},
            {"appearances": [{"appearance": "luminosity", "value": "dark"}], "filename": "icon-dark.png",
             "idiom": "universal", "platform": "ios", "size": "1024x1024"},
            {"appearances": [{"appearance": "luminosity", "value": "tinted"}], "filename": "icon-tinted.png",
             "idiom": "universal", "platform": "ios", "size": "1024x1024"},
        ],
        "info": {"author": "xcode", "version": 1},
    })
    write_set(WATCH, {"icon.png": light}, {
        "images": [{"filename": "icon.png", "idiom": "universal", "platform": "watchos", "size": "1024x1024"}],
        "info": {"author": "xcode", "version": 1},
    })
    os.makedirs(os.path.join(ROOT, "docs"), exist_ok=True)
    preview = Image.new("RGB", (S * 2 + 60, S + 40), (245, 243, 238))
    mask = Image.new("L", (S, S), 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, S, S], radius=int(S * 0.225), fill=255)
    preview.paste(light, (20, 20), mask)
    preview.paste(tinted, (S + 40, 20), mask)
    preview.resize((preview.width // 2, preview.height // 2), Image.LANCZOS).save(os.path.join(ROOT, "docs/icon-preview.png"))
    print("icons written")


if __name__ == "__main__":
    main()
