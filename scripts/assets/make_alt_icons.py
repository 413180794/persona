#!/usr/bin/env python3
"""The home screen icons of the built-in 形象 (设置 › 外观 › 形象, docs/persona.md「App 图标」), and the previews the app
draws them with in its own pages.

The 形象 decides the icon (AppIconChoice): 团子, the default, is the app's own `AppIcon` (make_icon.py), with a set
for each fixed look (`AppIcon-PaperLight` / `…Dark`, for 设置 › 外观 › 深浅色); 面罩, 汇流, 像素, 晶面 and 软糖 are their faces drawn by
the app's own PersonaFace (rendered on this Mac by persona_face/main.swift, so the icon is the face the app shows)
on a square ground. Each set has any / dark / tinted (iOS 18 home screen looks); the previews are image sets
(any + dark) of every icon, the default one included, read from its own icon set. Rerun after changing a drawing:
  python3 scripts/assets/make_alt_icons.py
"""
import json
import os
import subprocess
import tempfile

from PIL import Image, ImageDraw, ImageFilter

from make_icon import tinted_from

S = 1024
PREVIEW = 384
ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
ASSETS = os.path.join(ROOT, "apple/Persona/Resources/Assets.xcassets")
PREVIEWS = os.path.join(ASSETS, "AppIcons")
FACE_TOOL = os.path.join(os.path.dirname(os.path.abspath(__file__)), "persona_face")
FACE_SOURCE = os.path.join(ROOT, "apple/Shared/Design/PersonaFace.swift")
# The face's share of the icon's side: the disc sits inside the home screen's squircle with room around it.
FACE_SHARE = 0.80

# 面罩 in the default style's accent (system blue), 汇流 in teal, 像素 in indigo, 晶面 in purple, 软糖 in orange: the
# icon cannot follow 主题色, so each has its own.
# Ground: the light icon's pale wash of the accent, the dark one's near-black of it.
FACES = {
    "AppIcon-Visor": ("visor", (0, 122, 255), (232, 241, 255), (9, 16, 30)),
    "AppIcon-Circuit": ("circuit", (42, 160, 168), (228, 242, 240), (6, 18, 19)),
    "AppIcon-Pixel": ("pixel", (88, 86, 214), (236, 236, 252), (13, 12, 30)),
    "AppIcon-Facet": ("facet", (175, 82, 222), (245, 235, 251), (22, 10, 30)),
    "AppIcon-Blob": ("blob", (255, 149, 0), (255, 243, 226), (30, 18, 4)),
}


def build_renderer(workdir):
    """Compiles persona_face/main.swift with the app's PersonaFace.swift into a Mac binary."""
    binary = os.path.join(workdir, "render")
    subprocess.run(["xcrun", "swiftc", "-O", "-o", binary, os.path.join(FACE_TOOL, "shim.swift"),
                    os.path.join(FACE_TOOL, "main.swift"), FACE_SOURCE], check=True)
    return binary


def face(binary, workdir, design, accent, scheme):
    side = round(S * FACE_SHARE)
    out = os.path.join(workdir, f"{design}-{scheme}.png")
    subprocess.run([binary, design, "%02X%02X%02X" % accent, scheme, str(side), out], check=True)
    return Image.open(out).convert("RGBA")


def on_ground(img, ground):
    """The face centred on a square of `ground`, with a soft shadow under the disc."""
    out = Image.new("RGBA", (S, S), ground + (255,))
    side = img.size[0]
    at = ((S - side) // 2, (S - side) // 2)
    shadow = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    d = ImageDraw.Draw(shadow)
    d.ellipse((at[0], at[1] + 18, at[0] + side, at[1] + side + 18), fill=(0, 0, 0, 70))
    out.alpha_composite(shadow.filter(ImageFilter.GaussianBlur(28)))
    out.alpha_composite(img, at)
    return out.convert("RGB")


def face_icon(binary, workdir, design, accent, light_ground, dark_ground):
    light = on_ground(face(binary, workdir, design, accent, "light"), light_ground)
    dark = on_ground(face(binary, workdir, design, accent, "dark"), dark_ground)
    return light, dark, tinted_from(dark)


def kept(name):
    """A set's art as it is (the default icon's, drawn by make_icon.py): light, dark and the tinted one derived."""
    folder = os.path.join(ASSETS, f"{name}.appiconset")
    light = Image.open(os.path.join(folder, "icon.png")).convert("RGB")
    dark = Image.open(os.path.join(folder, "icon-dark.png")).convert("RGB")
    return light, dark, tinted_from(dark)


# The icons that follow the app's 深浅色 (AppIconChoice.fixedLooks): set-name prefix → the set the pictures come from.
FIXED_LOOKS = {"AppIcon-Paper": "AppIcon"}

# Preview image set: the icon set it shows. The default's is read from the app icon (scripts/assets/make_icon.py).
PREVIEW_OF = {"app-icon-default": "AppIcon", "app-icon-visor": "AppIcon-Visor", "app-icon-circuit": "AppIcon-Circuit",
              "app-icon-pixel": "AppIcon-Pixel", "app-icon-facet": "AppIcon-Facet", "app-icon-blob": "AppIcon-Blob"}


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
    with tempfile.TemporaryDirectory() as workdir:
        binary = build_renderer(workdir)
        for name, (design, accent, light_ground, dark_ground) in FACES.items():
            write_icon_set(name, *face_icon(binary, workdir, design, accent, light_ground, dark_ground))
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
    print("wrote", ", ".join(list(FACES) + [p + l for p in FIXED_LOOKS for l in ("Light", "Dark")]), "and",
          len(PREVIEW_OF), "previews")


if __name__ == "__main__":
    main()
