#!/usr/bin/env python3
"""Collects the Android screenshot tests' images by name (docs/testing.md「安卓截图」).

Paparazzi writes each snapshot under a hash in its report; this copies them to <out>/<snapshot name>.png so they can be
looked at (and compared before / after a change):

    python3 scripts/app/android_shots.py android/app/build/reports/paparazzi/debug android/app/build/shots
"""
import glob, json, os, shutil, sys

report, out = sys.argv[1], sys.argv[2]
shutil.rmtree(out, ignore_errors=True)
os.makedirs(out)
# Oldest run first: a snapshot taken again in a later run (a class re-run on its own) replaces the earlier one.
for run in sorted(glob.glob(os.path.join(report, "runs", "*.js")), key=os.path.getmtime):
    text = open(run, encoding="utf-8").read()
    for shot in json.loads(text[text.index("= ") + 2:].rstrip().rstrip(";")):
        shutil.copy(os.path.join(report, shot["file"]), os.path.join(out, shot["name"] + ".png"))
names = sorted(os.listdir(out))
print("%d shots in %s: %s" % (len(names), out, " ".join(n[:-4] for n in names)))
