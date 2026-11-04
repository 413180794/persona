"""Frame-by-frame motion in a decoded recording (tools/vidrows). Usage:
  streamrows.py <rows.bin> [--from S] [--to S] [--band 0.20,0.60] [--chrome ROW] [--out frames.tsv]
Times are seconds into the video. Per frame: the content band's shift from the frame before (rows of the half-height
profile, + = down), and with --chrome (the half-row where the bottom chrome starts) the gap between the lowest ink row
above it and it. Prints a summary and RESULT."""
import array, struct, sys

def frames(path):
    with open(path, "rb") as f:
        while True:
            h = f.read(12)
            if len(h) < 12: return
            t, rows = struct.unpack("<di", h)
            mean = array.array("f"); mean.frombytes(f.read(4 * rows))
            ink = array.array("f"); ink.frombytes(f.read(4 * rows))
            yield t, mean, ink

def shift(a, b, lo, hi, rng=160):
    best, bs = None, 0
    for s in range(-rng, rng + 1):
        if lo + s < 0 or hi + s > len(b): continue
        d = 0.0
        for y in range(lo, hi, 2): d += abs(a[y] - b[y + s])
        if best is None or d < best: best, bs = d, s
    same = sum(abs(a[y] - b[y]) for y in range(lo, hi, 2))
    return bs, best, same

args = sys.argv[1:]
path = args[0]
opt = {args[i]: args[i + 1] for i in range(1, len(args) - 1, 2)}
t0, t1 = float(opt.get("--from", 0)), float(opt.get("--to", 1e9))
blo, bhi = map(float, opt.get("--band", "0.20,0.60").split(","))
chrome = int(opt["--chrome"]) if "--chrome" in opt else None
rows_out = []
prev = None
for t, mean, ink in frames(path):
    if t < t0 or t > t1: prev = (t, mean, ink); continue
    n = len(mean)
    lo, hi = int(n * blo), int(n * bhi)
    gap = None
    if chrome:
        y = chrome - 1
        while y > n * 0.15 and ink[y] < 0.01: y -= 1
        gap = chrome - 1 - y
    if prev is not None:
        s, best, same = shift(prev[1], mean, lo, hi)
        moved = same > 0.5 and s != 0 and best < same * 0.5
        rows_out.append((t, s if moved else 0, same, gap, best))
    prev = (t, mean, ink)

moves = [r for r in rows_out if r[1] != 0]
changed = [r for r in rows_out if r[2] > 0.5]
print("frames %d, changed %d, moved %d" % (len(rows_out), len(changed), len(moves)))
# Direction reversals: the content went one way, then back, within 1 s.
rev = 0
for i in range(1, len(moves)):
    if moves[i][1] * moves[i - 1][1] < 0 and moves[i][0] - moves[i - 1][0] < 1: rev += 1
print("reversals within 1 s: %d" % rev)
# A big change no small shift explains: the layout jumped (a block re-laid out, rows gone or doubled for a frame).
jumps = [r for r in rows_out if r[2] > 15 and (r[1] == 0 or r[4] > r[2] * 0.3)]
print("unexplained changes: %d %s" % (len(jumps), ["%.3f(%.0f)" % (r[0], r[2]) for r in jumps[:12]]))
for r in moves[:40]: print("  t %.3f shift %+d rows (x2 px)%s" % (r[0], r[1], "" if r[3] is None else " gap %d" % r[3]))
if chrome:
    gaps = [r[3] for r in rows_out]
    steps = [gaps[i] - gaps[i - 1] for i in range(1, len(gaps)) if gaps[i] != gaps[i - 1]]
    print("end gap: min %d max %d, changes %d: %s" % (min(gaps), max(gaps), len(steps), steps[:40]))
if "--out" in opt:
    with open(opt["--out"], "w") as f:
        for r in rows_out: f.write("%.4f\t%d\t%.2f\t%s\n" % (r[0], r[1], r[2], "" if r[3] is None else r[3]))
print("RESULT moved %d reversals %d jumps %d" % (len(moves), rev, len(jumps)))

# One-frame flickers: a frame unlike the frames on both sides of it, which are alike (the reply gone for a frame).
def diff(a, b, lo, hi): return sum(abs(a[y] - b[y]) for y in range(lo, hi, 2))
win = [(t, m) for t, m, k in frames(path) if t0 <= t <= t1]
flick = []
for i in range(1, len(win) - 1):
    n = len(win[i][1]); lo, hi = int(n * blo), int(n * bhi)
    a, b, c = win[i - 1][1], win[i][1], win[i + 1][1]
    ab, ac = diff(a, b, lo, hi), diff(a, c, lo, hi)
    if ab > 5 and ac < ab * 0.2: flick.append((win[i][0], ab, ac))
print("one-frame flickers: %d %s" % (len(flick), ["%.3f" % f[0] for f in flick[:10]]))
print("RESULT flickers %d" % len(flick))
