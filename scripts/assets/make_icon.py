#!/usr/bin/env python3
"""App icon: 纸色, the hand-drawn helper waving on warm paper (#709, picked by the user in 2026-11). The art was
generated and chosen, not drawn here: the iPhone set's icon.png (light) and icon-dark.png (dark) are it, 1024, RGB,
no alpha. This derives the rest from them: the tinted look, the watch icon (the light one; the system masks it round),
the Mac sizes and docs/icon-preview.png:
  python3 scripts/assets/make_icon.py
  python3 scripts/assets/make_icon.py mac     # only the Mac sizes

The Mac icon is the same picture on macOS's icon grid: the system does not mask a Mac icon, so it carries its own
rounded square (824 of 1024, centered) and shadow, at every size the Mac App Store asks for (16 to 512, 1x and 2x).
Replacing the icon is overwriting the two PNGs and rerunning this (and scripts/assets/make_alt_icons.py for the
settings preview).
"""
import json
import os
import sys

from PIL import Image, ImageDraw, ImageFilter, ImageOps

S = 1024
ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
IOS = os.path.join(ROOT, "apple/Persona/Resources/Assets.xcassets/AppIcon.appiconset")
WATCH = os.path.join(ROOT, "apple/PersonaWatch/Assets.xcassets/AppIcon.appiconset")
MAC_SIZES = (16, 32, 128, 256, 512)


def tinted_from(dark):
    """The tinted look from the dark one: greyscale, levels stretched so the whole ground is black (the system colours
    it by brightness): the brightest pixel of a band along the edges, where only the ground is, becomes black."""
    g = ImageOps.grayscale(dark.convert("RGB"))
    band = S // 25
    edges = [g.crop(box) for box in ((0, 0, S, band), (0, S - band, S, S), (0, 0, band, S), (S - band, 0, S, S))]
    lo = min(200, max(max(e.getdata()) for e in edges) + 4)
    return g.point(lambda v: max(0, min(255, (v - lo) * 255 // (255 - lo)))).convert("RGB")


def mac_icon(picture):
    """macOS's grid at 1024: an 824 rounded square (corner 185) 100 in from the left and right, lifted a little
    above center (the shadow sits under it), with the picture filling it."""
    body, radius, top = 824, 185, 88
    big = 4  # drawn at 4x for smooth corners, then downsampled
    canvas = Image.new("RGBA", (S * big, S * big), (0, 0, 0, 0))
    mask = Image.new("L", (body * big, body * big), 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, body * big - 1, body * big - 1], radius=radius * big, fill=255)
    left = (S - body) // 2
    shadow = Image.new("RGBA", canvas.size, (0, 0, 0, 0))
    shadow.paste((0, 0, 0, 90), (left * big, (top + 12) * big), mask)
    canvas = Image.alpha_composite(canvas, shadow.filter(ImageFilter.GaussianBlur(14 * big)))
    art = picture.convert("RGBA").resize((body * big, body * big), Image.LANCZOS)
    canvas.paste(art, (left * big, top * big), mask)
    return canvas.resize((S, S), Image.LANCZOS)


def write_mac(picture):
    """The Mac sizes into the iPhone icon's set (the Mac app builds the same asset catalog; iOS ignores idiom mac)."""
    icon = mac_icon(picture)
    path = os.path.join(IOS, "Contents.json")
    with open(path) as f:
        contents = json.load(f)
    contents["images"] = [i for i in contents["images"] if i.get("idiom") != "mac"]
    for size in MAC_SIZES:
        for scale in (1, 2):
            name = f"mac-{size}{'@2x' if scale == 2 else ''}.png"
            icon.resize((size * scale, size * scale), Image.LANCZOS).save(os.path.join(IOS, name))
            contents["images"].append({"filename": name, "idiom": "mac", "scale": f"{scale}x", "size": f"{size}x{size}"})
    with open(path, "w") as f:
        json.dump(contents, f, indent=2)
        f.write("\n")


def write_set(folder, images, contents):
    os.makedirs(folder, exist_ok=True)
    for name, im in images.items():
        im.save(os.path.join(folder, name))
    with open(os.path.join(folder, "Contents.json"), "w") as f:
        json.dump(contents, f, indent=2)


def main():
    light = Image.open(os.path.join(IOS, "icon.png")).convert("RGB")
    dark = Image.open(os.path.join(IOS, "icon-dark.png")).convert("RGB")
    tinted = tinted_from(dark)
    write_set(IOS, {"icon.png": light, "icon-dark.png": dark, "icon-tinted.png": tinted}, {
        "images": [
            {"filename": "icon.png", "idiom": "universal", "platform": "ios", "size": "1024x1024"},
            {"appearances": [{"appearance": "luminosity", "value": "dark"}], "filename": "icon-dark.png",
             "idiom": "universal", "platform": "ios", "size": "1024x1024"},
            {"appearances": [{"appearance": "luminosity", "value": "tinted"}], "filename": "icon-tinted.png",
             "idiom": "universal", "platform": "ios", "size": "1024x1024"},
        ],
        "info": {"author": "xcode", "version": 1},
    })
    write_mac(light)
    write_set(WATCH, {"icon.png": light}, {
        "images": [{"filename": "icon.png", "idiom": "universal", "platform": "watchos", "size": "1024x1024"}],
        "info": {"author": "xcode", "version": 1},
    })
    os.makedirs(os.path.join(ROOT, "docs"), exist_ok=True)
    preview = Image.new("RGB", (S * 3 + 80, S + 40), (245, 243, 238))
    mask = Image.new("L", (S, S), 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, S, S], radius=int(S * 0.225), fill=255)
    for i, im in enumerate((light, dark, tinted)):
        preview.paste(im, (20 + i * (S + 20), 20), mask)
    preview.resize((preview.width // 2, preview.height // 2), Image.LANCZOS).save(os.path.join(ROOT, "docs/icon-preview.png"))
    print("icons written")


if __name__ == "__main__":
    if sys.argv[1:] == ["mac"]:
        write_mac(Image.open(os.path.join(IOS, "icon.png")))
        print("mac icons written")
    else:
        main()
