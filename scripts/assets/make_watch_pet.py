#!/usr/bin/env python3
"""Builds the Apple Watch's Milk Frog (docs/apple/persona.md "The Milk Frog on the watch") into the built-in nwverse package.

  python3 scripts/assets/make_watch_pet.py SOURCE [--out DIR]

SOURCE holds the finished art, which stays out of the repo: frames/<clip>/000.png onward, and face-<state>.png. Each
clip becomes one animated PNG the watch plays, each face a small picture for the row and the watch face. pet.json
names them with their hashes, so a watch fetches only what changed. DIR defaults to the package's pet/. Needs ffmpeg.
"""
import argparse
import hashlib
import json
import pathlib
import subprocess

OUT = pathlib.Path("server/internal/hub/personas") / "nwverse" / "pet"
SIZE = 288  # pixels: the page draws it 144 points wide at 2x
FACE = 128
# fps; still is the frame that shows the state alone (the always-on display, while the wrist is down).
CLIPS = {
    "needs_you": (10, 0), "working": (10, 2), "done": (12, 20), "idle": (10, 0),
    "sleep": (10, 15), "offline": (10, 10), "tap": (10, 0),
}
STATES = ["needs_you", "working", "done", "idle", "sleep", "offline"]


def ffmpeg(*args):
    subprocess.run(["ffmpeg", "-v", "error", "-y", *args], check=True)


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("source", type=pathlib.Path)
    parser.add_argument("--out", type=pathlib.Path, default=OUT)
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)

    clips = {}
    for name, (fps, still) in CLIPS.items():
        out = args.out / f"{name}.png"
        frames = sorted((args.source / "frames" / name).glob("*.png"))
        ffmpeg("-framerate", str(fps), "-i", str(args.source / "frames" / name / "%03d.png"),
               "-vf", f"scale={SIZE}:{SIZE}:flags=lanczos", "-plays", "0", "-f", "apng", "-pred", "mixed", str(out))
        clips[name] = {"file": f"pet/{out.name}", "sha256": sha256(out), "fps": fps, "frames": len(frames), "still": still}

    faces = {}
    for name in STATES:
        out = args.out / f"face-{name}.png"
        ffmpeg("-i", str(args.source / f"face-{name}.png"), "-vf", f"scale={FACE}:{FACE}:flags=lanczos", str(out))
        faces[name] = {"file": f"pet/{out.name}", "sha256": sha256(out)}

    manifest = {"format": 1, "size": SIZE, "clips": clips, "faces": faces}
    (args.out / "pet.json").write_text(json.dumps(manifest, indent=2) + "\n")


if __name__ == "__main__":
    main()
