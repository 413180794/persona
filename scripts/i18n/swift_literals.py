"""A small Swift lexer: yields the string literals of a source file (with interpolations) and where they start.

Used by the localization checks (scripts/i18n/apple_l10n.py); good enough for Swift as this repository writes it:
line / block comments (nested), "…", multi-line \"\"\"…\"\"\", raw #"…"# strings and \\( … ) interpolations
that themselves contain strings.
"""

import re
from dataclasses import dataclass

CJK = re.compile(r"[぀-ヿ㐀-䶿一-鿿＀-￯]")


@dataclass
class Literal:
    line: int  # 1-based line of the opening quote
    col: int  # 1-based column (characters) of the opening quote
    text: str  # the literal's body as written, interpolations kept as \(…)
    end_line: int

    @property
    def has_cjk(self) -> bool:
        # Only the literal's own text, not what is interpolated into it.
        return bool(CJK.search(strip_interpolations(self.text)))


def strip_interpolations(text: str) -> str:
    out, i, depth = [], 0, 0
    while i < len(text):
        if depth == 0 and text.startswith("\\(", i):
            depth, i = 1, i + 2
            continue
        if depth:
            if text[i] == "(":
                depth += 1
            elif text[i] == ")":
                depth -= 1
            i += 1
            continue
        out.append(text[i])
        i += 1
    return "".join(out)


def literals(src: str):
    """Yields every string literal (outermost and nested in interpolations) in the source."""
    found: list[Literal] = []
    _scan(src, 0, len(src), found, top=True)
    found.sort(key=lambda l: (l.line, l.col))
    return found


def _pos(src: str, i: int, cache={}):
    key = id(src)
    starts = cache.get(key)
    if starts is None or starts[0] is not src:
        idx = [0]
        for m in re.finditer("\n", src):
            idx.append(m.end())
        starts = (src, idx)
        cache.clear()
        cache[key] = starts
    idx = starts[1]
    lo, hi = 0, len(idx) - 1
    while lo < hi:
        mid = (lo + hi + 1) // 2
        if idx[mid] <= i:
            lo = mid
        else:
            hi = mid - 1
    return lo + 1, i - idx[lo] + 1


def _scan(src, i, end, found, top=False, stop_paren=False):
    """Scans code from i; returns the index after the closing ')' when stop_paren (interpolation), else end."""
    depth = 0
    while i < end:
        c = src[i]
        if src.startswith("//", i):
            j = src.find("\n", i)
            i = end if j < 0 else j
            continue
        if src.startswith("/*", i):
            d, i = 1, i + 2
            while i < end and d:
                if src.startswith("/*", i):
                    d, i = d + 1, i + 2
                elif src.startswith("*/", i):
                    d, i = d - 1, i + 2
                else:
                    i += 1
            continue
        if c == "#" or c == '"':
            j = i
            while j < end and src[j] == "#":
                j += 1
            hashes = j - i
            if j < end and src[j] == '"':
                i = _string(src, i, j, hashes, end, found)
                continue
            if hashes:
                i = j
                continue
        if stop_paren:
            if c == "(":
                depth += 1
            elif c == ")":
                if depth == 0:
                    return i + 1
                depth -= 1
        i += 1
    return i


def _string(src, start, q, hashes, end, found):
    multi = src.startswith('"""', q)
    open_len = 3 if multi else 1
    close = ('"""' if multi else '"') + "#" * hashes
    esc = "\\" + "#" * hashes
    i = q + open_len
    body_start = i
    while i < end:
        if src.startswith(esc, i):
            k = i + len(esc)
            if k < end and src[k] == "(":
                i = _scan(src, k + 1, end, found, stop_paren=True)
                continue
            i = k + 1
            continue
        if src.startswith(close, i):
            line, col = _pos(src, q)
            end_line, _ = _pos(src, i)
            found.append(Literal(line, col - hashes if hashes else col, src[body_start:i], end_line))
            return i + len(close)
        if not multi and src[i] == "\n":
            break
        i += 1
    return i
