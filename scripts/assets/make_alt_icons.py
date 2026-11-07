#!/usr/bin/env python3
"""Generate the default icon's fixed light/dark sets and its in-app preview.

Both supported personas use AppIcon (drawn by make_icon.py). Rerun after changing the default icon:
  python3 scripts/assets/make_alt_icons.py
"""
import json
import os

from PIL import Image

from make_icon import tinted_from

PREVIEW = 384
ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
ASSETS = os.path.join(ROOT, "apple/Persona/Resources/Assets.xcassets")
PREVIEWS = os.path.join(ASSETS, "AppIcons")
def kept(name):
    """A set's art as it is (the default icon's, drawn by make_icon.py): light, dark and the tinted one derived."""
    folder = os.path.join(ASSETS, f"{name}.appiconset")
    light = Image.open(os.path.join(folder, "icon.png")).convert("RGB")
    dark = Image.open(os.path.join(folder, "icon-dark.png")).convert("RGB")
    return light, dark, tinted_from(dark)


# The icons that follow the app's light / dark mode (AppIconChoice.iconName(for:)): set-name prefix → the set the pictures come from.
FIXED_LOOKS = {"AppIcon-Paper": "AppIcon"}

# Preview image set: the icon set it shows. The default's is read from the app icon (scripts/assets/make_icon.py).
PREVIEW_OF = {"app-icon-default": "AppIcon"}


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
            # Settings' header showed an empty tile there; both views that draw it are resizable.
            {"filename": "preview.png", "idiom": "universal"},
            {"appearances": [{"appearance": "luminosity", "value": "dark"}], "filename": "preview-dark.png",
             "idiom": "universal"},
        ],
        "info": {"author": "xcode", "version": 1},
    })


def main():
    # Settings › Appearance › Light / Dark set to Light / Dark (#710): for the icons that follow it, a set with only that look whatever the
    # home screen is (tinted as usual).
    for prefix, source in FIXED_LOOKS.items():
        light, dark, tinted = kept(source)
        write_icon_set(prefix + "Light", light, light, tinted)
        write_icon_set(prefix + "Dark", dark, dark, tinted)
    os.makedirs(PREVIEWS, exist_ok=True)
    write_json(os.path.join(PREVIEWS, "Contents.json"), {"info": {"author": "xcode", "version": 1}})
    for name, icon_set in PREVIEW_OF.items():
        write_preview(name, icon_set)
    print("wrote", ", ".join([p + l for p in FIXED_LOOKS for l in ("Light", "Dark")]), "and",
          len(PREVIEW_OF), "previews")


if __name__ == "__main__":
    main()
