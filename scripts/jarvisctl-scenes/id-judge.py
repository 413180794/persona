#!/usr/bin/env python3
"""id-judge.py <out.tsv> <diff dir> --old <dir> <dir> [<dir>…] --cli <dir> <dir> [<dir>…]

Do the shots taken through the runtime CLI (id-cli.py) show what the launch-per-shot shots (id-old.sh) show? Same
rules as tools/shotdiff.py: a pixel differs when a channel moves by more than 2; what changes by itself between two
shots of the same method is noise, grown by 6 px. Here the noise is every pair within each method (more shots cover
more phases of what keeps moving: a working agent's dot, the metric curves). Per shot: the pixels that differ between
the first old and the first CLI shot, each method's noise, and what is left: unexplained. A diff image (old | cli |
red unexplained) goes to <diff dir> for each shot with unexplained pixels. Needs numpy and Pillow (as shotdiff.py).
"""
import itertools
import os
import sys

import numpy as np
from PIL import Image

args = sys.argv[1:]
out, diffs = args[0], args[1]
old = args[args.index("--old") + 1:args.index("--cli")]
cli = args[args.index("--cli") + 1:]
if len(old) < 2 or len(cli) < 2:
    sys.exit("need at least two runs of each method (the noise)")
os.makedirs(diffs, exist_ok=True)


def load(p):
    return np.asarray(Image.open(p).convert("RGB"), dtype=np.int16)


def differ(a, b):
    return (np.abs(a - b) > 2).any(axis=2)


def grow(mask, r=6):
    m = mask.copy()
    for _ in range(r):
        g = m.copy()
        g[1:, :] |= m[:-1, :]
        g[:-1, :] |= m[1:, :]
        g[:, 1:] |= m[:, :-1]
        g[:, :-1] |= m[:, 1:]
        m = g
    return m


def noise(shots):
    n = np.zeros(shots[0].shape[:2], dtype=bool)
    for a, b in itertools.combinations(shots, 2):
        n |= differ(a, b)
    return n


names = sorted(f for f in os.listdir(old[0]) if f.endswith(".png") and not f.startswith("warm")
               and all(os.path.exists(os.path.join(d, f)) for d in old + cli))
rows = ["shot\told_vs_cli\told_noise\tcli_noise\tunexplained\tbox"]
bad = 0
for n in names:
    os_ = [load(os.path.join(d, n)) for d in old]
    cs = [load(os.path.join(d, n)) for d in cli]
    d = differ(os_[0], cs[0])
    no, nc = noise(os_), noise(cs)
    u = d & ~grow(no | nc)
    box = ""
    if u.any():
        bad += 1
        ys, xs = np.nonzero(u)
        box = f"x {xs.min()}-{xs.max()} y {ys.min()}-{ys.max()}"
        red = cs[0].copy()
        red[u] = [255, 0, 0]
        Image.fromarray(np.concatenate([os_[0], cs[0], red], axis=1).astype(np.uint8)).save(os.path.join(diffs, n))
    rows.append(f"{n}\t{int(d.sum())}\t{int(no.sum())}\t{int(nc.sum())}\t{int(u.sum())}\t{box}")
with open(out, "w") as f:
    f.write("\n".join(rows) + "\n")
print(f"{len(names)} shots, {bad} with unexplained pixels → {out}")
