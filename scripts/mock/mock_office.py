"""Made-up Office documents for the mock hub's file viewer (docs/apple/ios.md "File viewer"): the smallest valid docx, xlsx,
pptx and rtf that QuickLook and the table viewer open, built with the standard library only. All text is invented."""
import datetime
import io
import zipfile
from xml.sax.saxutils import escape

CT = "http://schemas.openxmlformats.org/package/2006/content-types"
REL = "http://schemas.openxmlformats.org/package/2006/relationships"
ODR = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
W = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
S = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
P = "http://schemas.openxmlformats.org/presentationml/2006/main"
A = "http://schemas.openxmlformats.org/drawingml/2006/main"
XML = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n'


def _zip(parts):
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as z:
        for name, body in parts:
            z.writestr(name, XML + body)
    return buf.getvalue()


def _types(defaults, overrides):
    d = "".join('<Default Extension="%s" ContentType="%s"/>' % x for x in defaults)
    o = "".join('<Override PartName="%s" ContentType="%s"/>' % x for x in overrides)
    return '<Types xmlns="%s">%s%s</Types>' % (CT, d, o)


def _rels(items):
    return '<Relationships xmlns="%s">%s</Relationships>' % (REL, "".join(
        '<Relationship Id="%s" Type="%s/%s" Target="%s"/>' % (i, ODR, t, target) for i, t, target in items))


BASE = [("rels", "application/vnd.openxmlformats-package.relationships+xml"), ("xml", "application/xml")]


def docx(title, paragraphs):
    """A Word document: a title, then paragraphs; a paragraph starting with "- " is a bullet line."""
    def para(text, size=None, bold=False):
        rpr = ("<w:b/>" if bold else "") + ('<w:sz w:val="%d"/>' % size if size else "")
        return '<w:p><w:r>%s<w:t xml:space="preserve">%s</w:t></w:r></w:p>' % ("<w:rPr>%s</w:rPr>" % rpr if rpr else "", escape(text))
    body = para(title, size=40, bold=True) + "".join(
        para("• " + p[2:]) if p.startswith("- ") else para(p, bold=p.endswith("：")) for p in paragraphs)
    doc = '<w:document xmlns:w="%s"><w:body>%s<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>' \
          '<w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr></w:body></w:document>' % (W, body)
    return _zip([
        ("[Content_Types].xml", _types(BASE, [("/word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml")])),
        ("_rels/.rels", _rels([("rId1", "officeDocument", "word/document.xml")])),
        ("word/document.xml", doc),
    ])


def _col(i):
    s = ""
    i += 1
    while i:
        i, r = divmod(i - 1, 26)
        s = chr(65 + r) + s
    return s


def xlsx(sheets):
    """A workbook: [(sheet name, rows)], each row a list of str / int / float / bool / date; the first row is the header."""
    strings, index = [], {}

    def cell(r, c, v):
        ref = "%s%d" % (_col(c), r + 1)
        if isinstance(v, bool):
            return '<c r="%s" t="b"><v>%d</v></c>' % (ref, v)
        if isinstance(v, datetime.date):
            return '<c r="%s" s="2"><v>%d</v></c>' % (ref, (v - datetime.date(1899, 12, 30)).days)
        if isinstance(v, (int, float)):
            return '<c r="%s"><v>%s</v></c>' % (ref, v)
        v = str(v)
        if v not in index:
            index[v] = len(strings)
            strings.append(v)
        return '<c r="%s" t="s"%s><v>%d</v></c>' % (ref, ' s="1"' if r == 0 else "", index[v])

    parts, overrides, wb_sheets, wb_rels = [], [], [], []
    for n, (name, rows) in enumerate(sheets, 1):
        data = "".join('<row r="%d">%s</row>' % (r + 1, "".join(cell(r, c, v) for c, v in enumerate(row))) for r, row in enumerate(rows))
        parts.append(("xl/worksheets/sheet%d.xml" % n, '<worksheet xmlns="%s"><sheetData>%s</sheetData></worksheet>' % (S, data)))
        overrides.append(("/xl/worksheets/sheet%d.xml" % n, "application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"))
        wb_sheets.append('<sheet name="%s" sheetId="%d" r:id="rId%d"/>' % (escape(name), n, n))
        wb_rels.append(("rId%d" % n, "worksheet", "worksheets/sheet%d.xml" % n))
    k = len(sheets)
    wb_rels += [("rId%d" % (k + 1), "sharedStrings", "sharedStrings.xml"), ("rId%d" % (k + 2), "styles", "styles.xml")]
    sst = '<sst xmlns="%s" count="%d" uniqueCount="%d">%s</sst>' % (S, len(strings), len(strings), "".join(
        '<si><t xml:space="preserve">%s</t></si>' % escape(x) for x in strings))
    styles = ('<styleSheet xmlns="%s"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font>'
              '<font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill>'
              '<fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border/></borders>'
              '<cellStyleXfs count="1"><xf/></cellStyleXfs><cellXfs count="3"><xf/><xf fontId="1" applyFont="1"/><xf numFmtId="14" applyNumberFormat="1"/></cellXfs></styleSheet>' % S)
    return _zip([
        ("[Content_Types].xml", _types(BASE, [("/xl/workbook.xml", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"),
                                             ("/xl/sharedStrings.xml", "application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"),
                                             ("/xl/styles.xml", "application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml")] + overrides)),
        ("_rels/.rels", _rels([("rId1", "officeDocument", "xl/workbook.xml")])),
        ("xl/workbook.xml", '<workbook xmlns="%s" xmlns:r="%s"><sheets>%s</sheets></workbook>' % (S, ODR, "".join(wb_sheets))),
        ("xl/_rels/workbook.xml.rels", _rels(wb_rels)),
        ("xl/sharedStrings.xml", sst),
        ("xl/styles.xml", styles),
    ] + parts)


THEME = ('<a:theme xmlns:a="%s" name="Mock"><a:themeElements><a:clrScheme name="Mock">'
         '<a:dk1><a:srgbClr val="1C1C1E"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:dk2><a:srgbClr val="1F3A5F"/></a:dk2>'
         '<a:lt2><a:srgbClr val="EEF2F7"/></a:lt2><a:accent1><a:srgbClr val="0A84FF"/></a:accent1><a:accent2><a:srgbClr val="30D158"/></a:accent2>'
         '<a:accent3><a:srgbClr val="FF9F0A"/></a:accent3><a:accent4><a:srgbClr val="FF453A"/></a:accent4><a:accent5><a:srgbClr val="BF5AF2"/></a:accent5>'
         '<a:accent6><a:srgbClr val="64D2FF"/></a:accent6><a:hlink><a:srgbClr val="0A84FF"/></a:hlink><a:folHlink><a:srgbClr val="5E5CE6"/></a:folHlink>'
         '</a:clrScheme><a:fontScheme name="Mock"><a:majorFont><a:latin typeface="Helvetica"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont>'
         '<a:minorFont><a:latin typeface="Helvetica"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont></a:fontScheme>'
         '<a:fmtScheme name="Mock"><a:fillStyleLst>%s</a:fillStyleLst><a:lnStyleLst>%s</a:lnStyleLst>'
         '<a:effectStyleLst>%s</a:effectStyleLst><a:bgFillStyleLst>%s</a:bgFillStyleLst></a:fmtScheme></a:themeElements></a:theme>') % (
    A, '<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>' * 3, '<a:ln w="9525"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln>' * 3,
    '<a:effectStyle><a:effectLst/></a:effectStyle>' * 3, '<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>' * 3)


def _shape(i, name, x, y, cx, cy, paragraphs, size, bold=False, color=None):
    fill = '<a:solidFill><a:srgbClr val="%s"/></a:solidFill>' % color if color else ""
    runs = "".join('<a:p><a:r><a:rPr lang="zh-CN" sz="%d"%s>%s</a:rPr><a:t>%s</a:t></a:r></a:p>' % (
        size, ' b="1"' if bold else "", fill, escape(t)) for t in paragraphs)
    return ('<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/>'
            '<a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr wrap="square"/>'
            '<a:lstStyle/>%s</p:txBody></p:sp>') % (i, name, x, y, cx, cy, runs)


def _tree(shapes):
    return ('<p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>%s</p:spTree></p:cSld>'
            % shapes)


def pptx(slides):
    """A deck: [(title, [lines])], 16:9, a title and a text box per slide."""
    ns = 'xmlns:a="%s" xmlns:r="%s" xmlns:p="%s"' % (A, ODR, P)
    cx, cy = 12192000, 6858000
    parts, overrides, ids, rels = [], [], [], []
    for n, (title, lines) in enumerate(slides, 1):
        shapes = _shape(2, "Title", 685800, 457200, cx - 1371600, 1143000, [title], 4000, bold=True, color="1F3A5F") + \
                 _shape(3, "Body", 685800, 1828800, cx - 1371600, cy - 2743200, lines, 2400)
        parts += [("ppt/slides/slide%d.xml" % n, '<p:sld %s>%s<p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>' % (ns, _tree(shapes))),
                  ("ppt/slides/_rels/slide%d.xml.rels" % n, _rels([("rId1", "slideLayout", "../slideLayouts/slideLayout1.xml")]))]
        overrides.append(("/ppt/slides/slide%d.xml" % n, "application/vnd.openxmlformats-officedocument.presentationml.slide+xml"))
        ids.append('<p:sldId id="%d" r:id="rId%d"/>' % (255 + n, n + 1))
        rels.append(("rId%d" % (n + 1), "slide", "slides/slide%d.xml" % n))
    k = len(slides)
    rels = [("rId1", "slideMaster", "slideMasters/slideMaster1.xml")] + rels + [("rId%d" % (k + 2), "theme", "theme/theme1.xml")]
    pres = ('<p:presentation %s><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst>%s</p:sldIdLst>'
            '<p:sldSz cx="%d" cy="%d"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>') % (ns, "".join(ids), cx, cy)
    master = ('<p:sldMaster %s>%s<p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" '
              'accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>'
              '<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst></p:sldMaster>') % (ns, _tree(""))
    layout = '<p:sldLayout %s>%s<p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>' % (ns, _tree(""))
    return _zip([
        ("[Content_Types].xml", _types(BASE, [
            ("/ppt/presentation.xml", "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"),
            ("/ppt/slideMasters/slideMaster1.xml", "application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"),
            ("/ppt/slideLayouts/slideLayout1.xml", "application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"),
            ("/ppt/theme/theme1.xml", "application/vnd.openxmlformats-officedocument.theme+xml")] + overrides)),
        ("_rels/.rels", _rels([("rId1", "officeDocument", "ppt/presentation.xml")])),
        ("ppt/presentation.xml", pres),
        ("ppt/_rels/presentation.xml.rels", _rels(rels)),
        ("ppt/slideMasters/slideMaster1.xml", master),
        ("ppt/slideMasters/_rels/slideMaster1.xml.rels", _rels([("rId1", "slideLayout", "../slideLayouts/slideLayout1.xml"),
                                                                ("rId2", "theme", "../theme/theme1.xml")])),
        ("ppt/slideLayouts/slideLayout1.xml", layout),
        ("ppt/slideLayouts/_rels/slideLayout1.xml.rels", _rels([("rId1", "slideMaster", "../slideMasters/slideMaster1.xml")])),
        ("ppt/theme/theme1.xml", THEME),
    ] + parts)


def rtf(title, paragraphs):
    """Rich text: a bold title, then paragraphs (non-ASCII as \\u escapes)."""
    def enc(t):
        return "".join(c if ord(c) < 128 and c not in "\\{}" else ("\\" + c if c in "\\{}" else "\\u%d?" % (ord(c) if ord(c) < 32768 else ord(c) - 65536)) for c in t)
    body = "\\pard\\sa200\\b\\fs36 %s\\b0\\fs24\\par\n" % enc(title) + "".join("\\pard\\sa120 %s\\par\n" % enc(p) for p in paragraphs)
    return ("{\\rtf1\\ansi\\ansicpg1252\\deff0{\\fonttbl{\\f0 Helvetica;}}\n" + body + "}\n").encode()


def csv_text(rows=240):
    """A made-up observations table, wide enough to scroll sideways, with a quoted field and a comma in one."""
    cities = ["上海", "Tokyo", "Lisbon", "Reykjavík", "São Paulo", "Nairobi"]
    out = ["时间,城市,温度 °C,体感 °C,湿度 %,降雨 mm,风速 km/h,风向,气压 hPa,能见度 km,紫外线,备注"]
    for i in range(rows):
        note = '"阵雨, 傍晚转晴"' if i % 17 == 0 else ('"有""雷暴""预警"' if i % 29 == 0 else "")
        out.append("2026-11-%02d %02d:00,%s,%.1f,%.1f,%d,%.1f,%d,%s,%d,%d,%d,%s" % (
            1 + i // 24 % 28, i % 24, cities[i % len(cities)], 8 + i * 7 % 23 * 0.9, 6 + i * 5 % 25 * 0.9, 40 + i * 13 % 55,
            i * 3 % 11 * 0.4, 3 + i * 11 % 30, "NESW"[i % 4], 1003 + i % 19, 5 + i % 20, i % 9, note))
    return "\n".join(out) + "\n"


def _png(w, h, rgb):
    """A small solid PNG with a darker band (a made-up chart), standard library only."""
    import struct, zlib
    rows = b"".join(b"\x00" + b"".join(bytes(rgb if y > h // 3 and x % 8 < 5 and y > h - (x * 7 % h) else (245, 247, 250)) for x in range(w)) for y in range(h))
    def chunk(t, d):
        return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xFFFFFFFF)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(rows)) + chunk(b"IEND", b"")


def notebook():
    """A made-up analysis notebook: Markdown with a formula, code, stdout / stderr, an HTML table with its text
    version, a chart, and an error with a colored traceback."""
    import base64, json
    def md(src):
        return {"cell_type": "markdown", "metadata": {}, "source": src.splitlines(True)}
    def code(n, src, outputs):
        return {"cell_type": "code", "execution_count": n, "metadata": {}, "source": src.splitlines(True), "outputs": outputs}
    table_html = ("<table><thead><tr><th>城市</th><th>降雨概率</th></tr></thead><tbody><tr><td>上海</td><td>0.62</td></tr>"
                  "<tr><td>Tokyo</td><td>0.35</td></tr></tbody></table>")
    cells = [
        md("# 降雨概率分析\n\n示例数据，模型是逻辑回归：$p = \\sigma(w^\\top x + b)$。"),
        code(1, "import pandas as pd\ndf = pd.read_csv(\"data/observations.csv\")\nprint(len(df), \"行\")",
             [{"output_type": "stream", "name": "stdout", "text": ["240 行\n"]},
              {"output_type": "stream", "name": "stderr", "text": ["UserWarning: 列「备注」有空值\n"]}]),
        code(2, "df.groupby(\"城市\")[\"降雨 mm\"].mean().round(2).head(2)",
             [{"output_type": "execute_result", "execution_count": 2, "metadata": {},
               "data": {"text/html": table_html, "text/plain": "城市\n上海     0.62\nTokyo    0.35\nName: 降雨 mm, dtype: float64"}}]),
        code(3, "df.plot(x=\"时间\", y=\"温度 °C\")",
             [{"output_type": "display_data", "metadata": {},
               "data": {"image/png": base64.b64encode(_png(160, 72, (10, 132, 255))).decode(), "text/plain": "<Figure size 640x288>"}}]),
        code(4, "df[\"风速\"].max()",
             [{"output_type": "error", "ename": "KeyError", "evalue": "'风速'",
               "traceback": ["\u001b[0;31m---------------------------------------------------------------------------\u001b[0m",
                             "\u001b[0;31mKeyError\u001b[0m                                  Traceback (most recent call last)",
                             "Cell \u001b[0;32mIn[4], line 1\u001b[0m\n\u001b[0;32m----> 1\u001b[0m df[\u001b[38;5;124m\"风速\"\u001b[39m]\u001b[38;5;241m.\u001b[39mmax()",
                             "\u001b[0;31mKeyError\u001b[0m: '风速'"]}]),
        md("## 结论\n\n- 上海的降雨概率最高\n- 下一步：加上气压特征"),
    ]
    return json.dumps({"cells": cells, "metadata": {"kernelspec": {"display_name": "Python 3", "language": "python", "name": "python3"},
                                                    "language_info": {"name": "python"}}, "nbformat": 4, "nbformat_minor": 5},
                      ensure_ascii=False, indent=1)


def tone_wav(seconds=6, rate=22050):
    """A made-up voice memo: six seconds of a soft little melody, 16-bit mono WAV."""
    import math, struct, wave
    notes = [523.25, 659.25, 783.99, 659.25, 587.33, 698.46, 880.0, 523.25]
    buf = io.BytesIO()
    with wave.open(buf, "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(rate)
        frames = bytearray()
        for i in range(seconds * rate):
            t = i / rate
            f = notes[int(t / (seconds / len(notes))) % len(notes)]
            env = min(1.0, (t % (seconds / len(notes))) * 20) * 0.25
            frames += struct.pack("<h", int(32767 * env * math.sin(2 * math.pi * f * t)))
        w.writeframes(bytes(frames))
    return buf.getvalue()


# The weather app's sample documents (docs/ is where the tree keeps them).
SAMPLES = {
    "季度报告.docx": lambda: docx("天气 App 第四季度报告", [
        "这份报告是编造的，用来检查文件页的 Word 预览。", "本季度完成：", "- 主屏幕小组件上线，覆盖小号和中号两种尺寸",
        "- 逐小时预报改为 6 小时降雨条", "- 后台刷新的耗电下降了三成", "下一步：",
        "- 锁屏小组件", "- 雷阵雨提醒的推送", "附注：所有数据来自示例城市，不对应任何真实地点的观测。"]),
    "逐小时预报.xlsx": lambda: xlsx([
        ("逐小时预报", [["时间", "城市", "温度 °C", "降雨概率 %", "风速 km/h"]] +
         [["%02d:00" % h, ["上海", "Tokyo", "Lisbon"][h % 3], round(12 + (h * 7 % 13) * 0.8, 1), h * 9 % 100, 5 + h * 3 % 22] for h in range(24)]),
        ("城市", [["城市", "时区", "纬度", "经度", "更新日期", "有雷达"], ["上海", "UTC+8", 31.2, 121.5, datetime.date(2026, 10, 31), True],
                ["Tokyo", "UTC+9", 35.7, 139.7, datetime.date(2026, 11, 1), True], ["Lisbon", "UTC+0", 38.7, -9.1, datetime.date(2026, 11, 2), False]]),
    ]),
    "路线图.pptx": lambda: pptx([
        ("天气 App 路线图", ["2026 年第四季度 · 示例数据", "小组件 · 推送 · 锁屏"]),
        ("这一季做完的", ["主屏幕小组件（小号、中号）", "6 小时降雨条", "后台刷新省电三成"]),
        ("下一季", ["锁屏小组件", "雷阵雨提醒", "Apple Watch 表盘"]),
    ]),
    "城市观测.csv": lambda: csv_text(),
    "分析.ipynb": lambda: notebook(),
    # The page's files beside it, read in through the hub by the preview (#1297).
    "报告.css": lambda: "h1 { color: #0a84ff; border-bottom: 2px solid #0a84ff; padding-bottom: 4px; }\n",
    "雨.png": lambda: _png(160, 72, (48, 176, 199)),
    "站会录音.wav": lambda: tone_wav(),
    # A page with a script and a remote image: neither may run or load until the preview allows them.
    "报告.html": lambda: ('<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>本周降雨</title>'
                         '<meta name="viewport" content="width=device-width, initial-scale=1">'
                         '<style>body{font:16px -apple-system,sans-serif;margin:20px;color:#1c1c1e}h1{font-size:22px}'
                         'table{border-collapse:collapse}td,th{border:1px solid #ccc;padding:6px 10px}th{background:#eef2f7}'
                         '@media (prefers-color-scheme: dark){body{color:#eee;background:#111}th{background:#223}}</style></head>'
                         '<body><h1>本周降雨</h1><p id="state">脚本没有运行</p>'
                         '<table><tr><th>城市</th><th>降雨 mm</th></tr><tr><td>上海</td><td>12.4</td></tr><tr><td>Tokyo</td><td>3.1</td></tr></table>'
                         '<link rel="stylesheet" href="报告.css">'
                         '<p><img src="雨.png" alt="同目录的图片" width="160" height="72"></p>'
                         '<p><img src="https://example.com/chart.png" alt="远程图片（不会加载）" width="160" height="80"></p>'
                         '<p><a href="https://example.com/rain">详细数据</a></p>'
                         '<script>document.getElementById("state").textContent = "脚本运行了";</script></body></html>'),
    "发布说明.rtf": lambda: rtf("天气 App 4.2 发布说明", [
        "新增：主屏幕小组件，显示当前温度和接下来 6 小时的降雨。", "改进：后台刷新更省电。", "修复：深色模式下标题的对比度。"]),
}
