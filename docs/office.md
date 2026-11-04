# 生成 Office 文件（`personad office`，#1300）

助手和 Agent 生成 Word、PowerPoint、Excel 文件用的命令，是 #1289 的 M9。文件生成在那台机器上；回复里写出它的绝对路径，App 就把它显示成文件，点开是 M1–M3 的预览。
让助手和 Agent 用上它的 skill 是 #1300 的下一步。

## 为什么做进 personad

跑助手的 hub 机器上不一定有 Node、pip / venv、LibreOffice（实测：Ubuntu 上缺 `ensurepip`，Claude Code 是原生二进制）。
依赖 python-docx / python-pptx（要编译的 lxml）或 Node 的 docx / pptxgenjs 的做法在那里跑不起来，各台电脑的环境也不一定齐。
每台机器都有 `personad`，所以生成器做在它里面（`server/internal/office`）。Go 没有许可证合适又成熟的 docx / pptx 库，所以自己写最小的合法 Office Open XML；
Markdown 用 goldmark（MIT）解析。参考了 Anthropic 官方 docx / xlsx / pptx skill 的思路（写完自查、表格用公式、版式统一），只借思路，没有搬内容。

## 命令

| 命令 | 做什么 |
| --- | --- |
| `personad office docx <file.md> [-o out.docx] [--title T]` | Word 文档，A4 |
| `personad office pptx <file.md> [-o out.pptx] [--title T]` | 16:9 的演示文稿 |
| `personad office xlsx <file.json\|file.csv> [-o out.xlsx]` | Excel 工作簿 |
| `personad office check <file.docx\|.xlsx\|.pptx>` | 读回结构，自己的和别人的都行 |

- 不写 `-o` 就放在输入旁边、换成对应的扩展名。写完打印一段 JSON：`file`（绝对路径）、`bytes`、`check`（同 check 的结果）。
- 退出码：0 成功，1 失败（stderr 一行原因，比如图片找不到），2 用法不对。

## 写什么

**docx（Markdown）**：
- 支持：`#` 到 `###` 标题（更深的按三级算）、段落、**粗体** / *斜体* / `代码` / ~~删除线~~ / 链接、有序和无序列表（可嵌套，有序列表从自己的第一个数开始）、GitHub 表格（第一行是表头，列对齐照 `:---:`）、代码块、引用、任务列表的勾选框、`---`（分页）。
- 图片 `![说明](相对 Markdown 文件的路径)`：只认 PNG / JPEG / GIF，最宽到页面宽度。网上的图不去下载，只留说明文字。

**pptx（Markdown）**：
- 分页：有 `---` 就按它分页，没有就每个 `#` / `##` 标题开一页。每页第一个标题是这页的标题。
- 封面：第一页只有 `#` 标题和段落时做成封面，段落是副标题。
- 正文：列表是要点（编号列表带编号），其他段落是不带圆点的行。
- 图和表：每页一张图或一个表；有文字时图放右边、表放下面。
- 字号：按这页的文字量自动变小（24 → 20 → 18 → 14 磅）。很多查看器不会像 PowerPoint 编辑时那样自动缩小字。

**xlsx（JSON 或 CSV）**：

```json
{"sheets":[{"name":"销售","header":["月份","收入","日期"],
  "rows":[["1月",1200.5,"2026-01-31"],["合计","=SUM(B2:B2)",null]],
  "widths":[10,14,12],"formats":["","#,##0.00","yyyy-mm-dd"],"freeze":true}]}
```

- 单元格：文字、数字、`true` / `false`、`null`（空），或者以 `=` 开头的公式。
- 表头加粗、浅色底、冻结（`freeze: false` 关掉）。列宽默认按内容算，中文算两个字宽。
- `formats` 是按列的 Excel 数字格式；格式像日期时（有 `yy`、`dd` 或 `mmm`），`"2026-01-31"` 这样的文字会变成真的日期。
- 工作表名会去掉 Excel 不认的字符、截到 31 个字、重名的加序号。
- CSV：第一行是表头，像数字的就是数字，但 `007` 这样带前导零的编号保持文字。

## 公式的值

工作簿里的公式在写的时候就算好，像 Excel 保存时那样把值写进文件（`formula.go`）。很多查看器（QuickLook、App 的预览、很多网页查看器）不会算公式，只显示文件里存的值，没有值就是空格子。
Excel 和 Numbers 打开时照样全部重算（`fullCalcOnLoad`）。

- 能算的：`+ - * / ^ & %`、比较、单元格和区域（别的工作表也行：`Sheet!A1`、`'带 空格'!A1:B2`），以及 SUM AVERAGE MIN MAX COUNT COUNTA ROUND ABS IF IFERROR AND OR NOT。
  `ROUND` 照 Excel 的 15 位有效数字，`2.345` 舍到 `2.35`。
- 遇到别的函数（VLOOKUP 等），这个公式就不写值，留给 Excel 算，在不算公式的查看器里会是空的。
- 循环引用写成 `#REF!`。

## 怎么验证的

- `server/internal/office` 的单元测试：每个部件 XML 合法，内容、编号、图、表、公式值都在，check 能读回。
- 合成的样例在 macOS 上交叉检查（2026-11-04）：
  - QuickLook 缩略图：文档、每种幻灯片、工作簿的公式值都显示对了。
  - `textutil` 读 docx。
  - python-docx / openpyxl / python-pptx 打开不报错，读到标题、表格、图、冻结、数字格式、日期和公式的缓存值。
- QuickLook 的几处限制已经绕开：
  - 表格的边框和宽度写在表格自己上，不只写在样式里。
  - 每个有序列表一个编号定义，不用 `startOverride`。
  - 分页用下一段的「段前分页」，不用分页符：分页符在 QuickLook 里显示成一个方块。
- QuickLook 仍会把表格列宽缩到内容那么宽；Word 里是整页宽。
