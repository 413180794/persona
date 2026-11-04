#!/usr/bin/env python3
"""The app icons a user can switch to (设置 › 外观 › App 图标, docs/ios.md「App 图标」), and the previews the page shows.

Each alternate icon is an app icon set with any / dark / tinted (iOS 18 home screen looks); the previews are image sets
(any + dark) of every choice, the default one included, read from its own icon set. Rerun after changing a drawing:
  python3 scripts/assets/make_alt_icons.py
"""
import json
import os
import random

from PIL import Image, ImageDraw, ImageFilter, ImageFont

from make_icon import tinted_from

S = 1024
PREVIEW = 384
ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
ASSETS = os.path.join(ROOT, "apple/Persona/Resources/Assets.xcassets")
PREVIEWS = os.path.join(ASSETS, "AppIcons")
DIDOT = "/System/Library/Fonts/Supplemental/Didot.ttc"
DIDOT_BOLD = 2

# p. — paper, ink, the red full stop.
PAPER = (239, 232, 220)
INK = (17, 17, 17)
STOP = (215, 55, 43)
NIGHT_PAPER = (22, 20, 18)
NIGHT_INK = (238, 231, 219)
NIGHT_STOP = (229, 72, 59)

# 气场 — a field of colour, no object.
AURA_GROUND = (13, 11, 20)
AURA_BLOBS = [((360, 380), 300, (255, 107, 61)), ((690, 430), 280, (122, 92, 255)),
              ((520, 720), 290, (33, 199, 168)), ((520, 520), 150, (255, 225, 198))]


def grain(img, strength, seed, light=False):
    """Paper / film grain: uniform noise as a faint ink (or light) wash, the same every run (no churn on a rerun)."""
    noise = Image.frombytes("L", (S, S), random.Random(seed).randbytes(S * S))
    alpha = noise.point(lambda v: int(v * strength))
    tone = Image.new("RGB", (S, S), (255, 255, 255) if light else (0, 0, 0))
    out = img.convert("RGB").copy()
    out.paste(tone, (0, 0), alpha)
    return out


def didot(ground, ink, stop, textured=True):
    img = Image.new("RGB", (S, S), ground)
    if textured:
        img = grain(img, 0.09, 7)
    d = ImageDraw.Draw(img)
    font = ImageFont.truetype(DIDOT, 860, index=DIDOT_BOLD)
    d.text((470, 690), "p", font=font, fill=ink, anchor="ms")
    d.ellipse((766 - 44, 662 - 44, 766 + 44, 662 + 44), fill=stop)
    return img


def aura(ground, blobs, textured=True):
    img = Image.new("RGB", (S, S), ground)
    layer = Image.new("RGB", (S, S), ground)
    d = ImageDraw.Draw(layer)
    for (cx, cy), r, col in blobs:
        d.ellipse((cx - r, cy - r, cx + r, cy + r), fill=col)
    img = layer.filter(ImageFilter.GaussianBlur(110))
    return grain(img, 0.10, 5, light=True) if textured else img


def gray(img):
    return img.convert("L").convert("RGB")


ICONS = {
    # Asset name: (any, dark, tinted). Tinted is greyscale on black; the system colours it.
    "AppIcon-Didot": lambda: (
        didot(PAPER, INK, STOP),
        didot(NIGHT_PAPER, NIGHT_INK, NIGHT_STOP),
        didot((0, 0, 0), (255, 255, 255), (150, 150, 150), textured=False),
    ),
    "AppIcon-Aura": lambda: (
        aura(AURA_GROUND, AURA_BLOBS),
        aura(AURA_GROUND, AURA_BLOBS),
        gray(aura((0, 0, 0), AURA_BLOBS, textured=False)),
    ),
}

# Drawn elsewhere and kept as PNGs: the icon set's icon.png (light) and icon-dark.png are the art (1024, RGB, no
# alpha); this script only derives the tinted one from the dark one (make_icon.tinted_from, as for the app icon) and
# rewrites the set. 蜜桃 is the default icon's hand-drawn helper (#709) on peach: picked from generated art, so
# replacing it is replacing those two PNGs and rerunning.
KEPT = ["AppIcon-Peach"]
# The icons that follow the app's 深浅色 (AppIconChoice.fixedLooks): set-name prefix → the set the pictures come from.
FIXED_LOOKS = {"AppIcon-Paper": "AppIcon", "AppIcon-Peach": "AppIcon-Peach"}


def kept(name):
    folder = os.path.join(ASSETS, f"{name}.appiconset")
    light = Image.open(os.path.join(folder, "icon.png")).convert("RGB")
    dark = Image.open(os.path.join(folder, "icon-dark.png")).convert("RGB")
    return light, dark, tinted_from(dark)


# Preview image set: the icon set it shows. The default's is read from the app icon (scripts/assets/make_icon.py).
PREVIEW_OF = {"app-icon-default": "AppIcon", "app-icon-peach": "AppIcon-Peach", "app-icon-didot": "AppIcon-Didot",
              "app-icon-aura": "AppIcon-Aura"}


def write_json(path, data):
    with open(path, "w") as f:
        json.dump(data, f, indent=2)
        f.write("\n")


def write_icon_set(name, any_img, dark, tinted):
    folder = os.path.join(ASSETS, f"{name}.appiconset")
    os.makedirs(folder, exist_ok=True)
    for file, img in (("icon.png", any_img), ("icon-dark.png", dark), ("icon-tinted.png", tinted)):
        img.save(os.path.join(folder, file), optimize=True)
    write_json(os.path.join(folder, "Contents.json"), {
        "images": [
            {"filename": "icon.png", "idiom": "universal", "platform": "ios", "size": "1024x1024"},
            {"appearances": [{"appearance": "luminosity", "value": "dark"}], "filename": "icon-dark.png",
             "idiom": "universal", "platform": "ios", "size": "1024x1024"},
            {"appearances": [{"appearance": "luminosity", "value": "tinted"}], "filename": "icon-tinted.png",
             "idiom": "universal", "platform": "ios", "size": "1024x1024"},
        ],
        "info": {"author": "xcode", "version": 1},
    })


def write_preview(name, icon_set):
    src = os.path.join(ASSETS, f"{icon_set}.appiconset")
    folder = os.path.join(PREVIEWS, f"{name}.imageset")
    os.makedirs(folder, exist_ok=True)
    for file, out in (("icon.png", "preview.png"), ("icon-dark.png", "preview-dark.png")):
        img = Image.open(os.path.join(src, file)).convert("RGB").resize((PREVIEW, PREVIEW), Image.LANCZOS)
        img.save(os.path.join(folder, out), optimize=True)
    write_json(os.path.join(folder, "Contents.json"), {
        "images": [
            # No scale: one picture for every scale and platform. Marked 3x, the Mac (1x / 2x only) found no image and
            # 设置's header showed an empty tile there; both views that draw it are resizable.
            {"filename": "preview.png", "idiom": "universal"},
            {"appearances": [{"appearance": "luminosity", "value": "dark"}], "filename": "preview-dark.png",
             "idiom": "universal"},
        ],
        "info": {"author": "xcode", "version": 1},
    })


def main():
    for name, draw in ICONS.items():
        write_icon_set(name, *draw())
    for name in KEPT:
        write_icon_set(name, *kept(name))
    # 设置 › 外观 › 深浅色 set to 浅色 / 深色 (#710): for the icons that follow it, a set with only that look whatever the
    # home screen is (tinted as usual).
    for prefix, source in FIXED_LOOKS.items():
        light, dark, tinted = kept(source)
        write_icon_set(prefix + "Light", light, light, tinted)
        write_icon_set(prefix + "Dark", dark, dark, tinted)
    os.makedirs(PREVIEWS, exist_ok=True)
    write_json(os.path.join(PREVIEWS, "Contents.json"), {"info": {"author": "xcode", "version": 1}})
    for name, icon_set in PREVIEW_OF.items():
        write_preview(name, icon_set)
    print("wrote", ", ".join(list(ICONS) + KEPT), "and", len(PREVIEW_OF), "previews")


if __name__ == "__main__":
    main()
