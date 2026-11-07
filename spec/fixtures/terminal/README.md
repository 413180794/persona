# 终端核心行为夹具

本目录供 Android 的纯 Kotlin `TerminalCore`／JUnit 使用，以后 XCTest 读取同一批 JSON。判定依据是当前 iOS 的实现和单测；这里记录现有行为，包括尚未修正的边界情况。参考版本为 `e491af26d2b9bb0b65b237cf4dd246968dd85fc6`。

共 13 个 JSON 文件、193 个用例。网格、历史行、折行和键盘纯逻辑均已与 iOS 源码对照；没有仅凭推测填写 `expected` 的用例。所有新编数据均为示例文本，录制帧只选取通过白名单核查的示例输出。

## 文件与来源

| 文件 | 内容 |
| --- | --- |
| `grid.json` | VT 子集、宽字符覆盖、光标、跨帧解析、重置 |
| `styles.json` | SGR、颜色、OSC 8 链接 |
| `widths.json` | 字素宽度，包括组合字符和 emoji |
| `scrollback.json` | ANSI 历史行解析 |
| `scroll_detection.json` | 相邻两屏的上滚检测 |
| `wrap.json` | 显示折行范围、缩进、填充和光标 |
| `recorded.json` | 真实录制中的示例输出片段 |
| `keys.json` | 键名与修饰键到终端字节 |
| `key_names.json` | 编码器的别名、大小写和未知输入 |
| `paste.json` | 文本与括号粘贴 |
| `typed.json` | 字符与修饰键到键事件或文本事件 |
| `modifiers.json` | 单次、锁定、按住和释放 |
| `batching.json` | 旧 `/keys` 接口的输入分批 |

以下表格使用这些源码简称；每个 JSON 用例的 `source` 另存完整仓库相对路径和函数／测试名。

| 简称 | 来源 |
| --- | --- |
| G | [TerminalGrid.swift](../../../apple/Persona/Views/Session/Terminal/TerminalGrid.swift) |
| W | [TerminalWrap.swift](../../../apple/Persona/Views/Session/Terminal/TerminalWrap.swift) |
| I | [TerminalInput.swift](../../../apple/Persona/Views/Session/Terminal/TerminalInput.swift) |
| M | [TerminalKeys.swift](../../../apple/Persona/Views/Chat/TerminalKeys.swift) |
| GT | [TerminalGridTests.swift](../../../apple/PersonaTests/TerminalGridTests.swift) |
| WT | [TerminalWrapTests.swift](../../../apple/PersonaTests/TerminalWrapTests.swift) |
| TT | [TerminalTests.swift](../../../apple/PersonaTests/TerminalTests.swift) |

设计依据为 [终端文档](../../../docs/integrations/terminal.md) 的第七至九节、第十一节「手机上的滚动历史」及第十二节更正，[Android 方案 5.11](../../../docs/archive/redesign/android.md)，以及[设计总纲 D1、D4、4.1](../../../docs/archive/redesign/README.md)。

## 网格规则

| 规则 | 来源 | 覆盖用例 |
| --- | --- | --- |
| 网格最小为一行一列，重置清空内容、样式和解析状态并恢复可见光标。 | G.init／reset | `minimum_grid_size`、`visibility_and_reset` |
| 写入使光标前进到下一格，允许 `col == cols`，越界字符直接丢弃且不自动折行。 | G.printChar | `right_edge_does_not_wrap` |
| CUP／HVP、相对移动及绝对行列定位采用一基参数、零基结果并限制在屏内。 | G.csi／moveTo | `cursor_motion_and_clamping` |
| CR 回列首、LF 保留列、BS 向左一格、TAB 到下一八列制表位且不越过最后一列。 | G.control | `control_characters` |
| 屏内 LF 下移一行，底行 LF 上移整屏并补空行。 | G.lineFeed | `lf_bottom_scroll` |
| ED／EL 按指定范围擦除，整屏或整行擦除不移动光标。 | G.eraseDisplay／eraseLine；GT.erasesAndOverwrites | `erase_display_modes`、`erase_line_modes`、`erase_and_overwrite` |
| ECH 使用当前背景色并按需创建空格，其他样式不随擦除保留。 | G.blank | `erase_and_overwrite` |
| ICH／DCH 插入或删除已存格子，插入后超过列宽的部分被截断。 | G.insertChars／deleteChars | `insert_delete_characters` |
| IL／DL／SU／SD 移动整行并用空行填补，数量最多为屏幕行数。 | G.csi | `insert_delete_and_scroll_rows` |
| 宽字符由左半宽度二和右半宽度零组成，同位置重画不能清掉新写的左半。 | G.putRightHalf；WT.overwritingAWideCharacterInPlaceKeepsIt；CLAUDE.md 宽字符经验 | `wide_redraw_in_place` |
| 覆盖宽字符任一半时清空旧字符另一半，新宽字符右半另用专门逻辑写入。 | G.put／putRightHalf | `wide_left_half_replaced`、`wide_overlaps_next_wide`、`erase_and_overwrite` |
| 擦除逐格替换，擦除右半格时仍保留左半宽度二的格子。 | G.blank | `erase_wide_half_is_literal` |
| Swift Character 字素决定宽度，中文和常见 emoji 占两格，框线和文本符号占一格。 | G.width；GT.widths | `width_cjk`、`width_fullwidth`、`width_emoji`、`width_rule`、`width_text_symbol`、`grapheme_widths_in_grid` |
| 孤立组合音标宽度为零并追加到前一格，列首的孤立组合音标被忽略。 | G.width／printChar | `width_combining`、`combining_across_frames` |
| 肤色、变体和 ZWJ 序列按整个字素处理，ASCII 起头的键帽序列仍返回一格。 | G.width | `width_skin_tone`、`width_variation`、`width_zwj`、`width_flag`、`width_combined_latin`、`width_ascii_keycap` |
| CSI 和 OSC 解析状态跨 feed 保留，但 UTF-8 解码每次 feed 独立进行。 | G.Parser.step／feed(Data) | `split_csi_and_osc`、`utf8_replacement_per_frame` |
| 未实现的 CSI、字符集、标题和同步输出序列被忽略，备用屏模式不另建网格。 | G.Parser.step／csi；GT.unknownSequencesAreSkipped | `unknown_sequences`、`alternate_screen_is_ignored` |
| DECTCEM 控制光标显隐，其他私有模式不改变它。 | G.csi | `erase_and_overwrite`、`visibility_and_reset` |
| SGR 支持基本色、亮色、索引色、真彩色及各样式的设置和关闭。 | G.sgr | `sgr_color_modes`、`sgr_flags_and_resets` |
| SGR 冒号参数按分号展开，颜色值钳制到字节范围。 | G.Parser.step／sgr | `sgr_colon_and_clamp` |
| OSC 8 链接由 BEL 或 ESC 终止，SGR 复位保留链接，空 URI 关闭链接。 | G.Parser.step／osc／sgr | `osc_bel_and_escape_termination`、`sgr_link_survives_reset` |
| 录制的彩色文本、中文框线、emoji 和链接按 herdr 给出的格位还原。 | GT.mirrorsWhatHerdrShows／keepsColorsAndStyles／wideCharactersTakeTwoCells／readsLinks | `recorded_sample_output` |

## 历史与滚动规则

| 规则 | 来源 | 覆盖用例 |
| --- | --- | --- |
| 历史页使用可增长网格，CRLF 归一为 LF，换行回列首且保留内部空行。 | G.rows(fromANSI:)；GT.scrollbackPagesAreRowsOfColoredText | `scrollback_colored_rows`、`scrollback_lf_resets_column` |
| 空输入返回零行，结尾换行只去掉最后一个空行。 | G.rows(fromANSI:) | `scrollback_empty`、`scrollback_plain`、`scrollback_final_empty_line` |
| 两屏都至少四行且首行不同才搜索正向位移。 | G.scrolled | `scroll_small_screen`、`scroll_first_row_equal`、`scroll_growth_in_place` |
| 新屏顶部必须连续等于旧屏第 k 行起至少 `max(3, (old.count-k)/3)` 行，整数除法向下取整。 | G.scrolled | `scroll_up_two`、`scroll_needs_three_matches`、`scroll_threshold_floor`、`scroll_threshold_four` |
| 连续匹配中至少两行不能全是空格或横分隔线。 | G.scrolled／W.TermCell.isFiller；WT.onlyAMoveUpIsAScroll | `scroll_filler_not_evidence`、`scroll_one_telling_line` |
| 匹配比较完整格子及样式，去尾空白后的纯文本相同还不够。 | G.scrolled／rowKey | `scroll_style_change`、`scroll_sparse_row_difference` |
| 清屏和下滚不算上滚，多个位移满足条件时返回最小位移。 | G.scrolled；WT.onlyAMoveUpIsAScroll | `scroll_clear_is_not_scroll`、`scroll_down_is_not_up`、`scroll_smallest_matching_shift` |

`scrolled` 的返回值是检测到的行数，零表示未检测到；它不直接等于历史追加命令。调用方是否接管、是否支持滚轮、何时清空历史由 `TerminalFeed` 决定，本目录不把这些状态机混入网格快照。

## 显示折行规则

| 规则 | 来源 | 覆盖用例 |
| --- | --- | --- |
| 关闭折行时每个源行对应一条显示行，空行仍保留。 | W.lines | `wrap_disabled`、`wrap_empty_rows` |
| 折行只在后半行寻找合适断点，长词找不到断点时按列截断。 | W.breakPoint／canBreak；终端文档第八节 | `wrap_words`、`wrap_long_word` |
| 能回退时不在收尾标点前或半角开括号后断行。 | W.canBreak | `wrap_cjk_punctuation`、`wrap_opening_punctuation` |
| 当前实现检查前一格的文字，无法识别全角开括号的右半格。 | W.canBreak | `wrap_fullwidth_opening_current` |
| 边界落在宽字符右半格且可回退时，整个宽字符移至下一行。 | W.lines | `wrap_wide_edge`、`wrap_wide_boundary` |
| 列表标记及其空格、前导空格和竖框线决定续行缩进，缩进最多半行。 | W.hangingIndent | `wrap_marker_indent`、`wrap_numbered_indent`、`wrap_border_indent`、`wrap_indent_capped` |
| 列表标记后没有空格时不增加标记缩进，断点的默认空白不带到续行开头。 | W.hangingIndent／lines | `wrap_marker_requires_space`、`wrap_break_spaces` |
| 剩余全是横线或任意背景空格时停止折行，光标在剩余区时继续。 | W.TermCell.isFiller／lines | `wrap_rule_padding`、`wrap_background_padding`、`wrap_cursor_keeps_padding` |
| 光标可扩展显示范围，带下划线等有效样式的尾空格不按默认空白裁掉。 | G.TermCell.isBlank／W.lines | `wrap_cursor_past_text`、`wrap_styled_blank_not_trimmed` |

夹具只断言 `DisplayLine` 的行号、起止格位和缩进；竖框线重画、颜色渲染、字体和选区属于视图验收。显示宽度必须为正；极窄视口与异常游标越过已存内容很远的组合不在本批输入域中。

## 键盘规则

| 规则 | 来源 | 覆盖用例 |
| --- | --- | --- |
| 修饰键名按 ctrl、alt、shift 排列，键栏 esc 始终不带修饰。 | M.apply；TT.combinesInHerdrOrder | `modifier_order`、`modifier_escape`、`key_ctrl_alt_shift_esc` |
| 箭头及 Home／End 使用 CSI，修饰参数为 `1 + shift + 2*alt + 4*ctrl`。 | I.encode；WT.keysAreWhatATerminalSends | `key_up`、`key_down`、`key_home`、`key_end`、`key_ctrl_alt_shift_up` |
| Insert／Delete／PageUp／PageDown 和 F5–F12 使用波浪号序列，F1–F4 无修饰时使用 SS3。 | I.encode | `key_insert`、`key_delete`、`key_pageup`、`key_pagedown`、`key_f1` 至 `key_f12`、`key_ctrl_alt_shift_f1`、`key_ctrl_alt_shift_delete` |
| Enter 发送 CR，Shift 或 Alt 加 Enter 发送单个 ESC 加 CR。 | I.encode | `key_enter`、`key_shift_enter`、`key_alt_enter`、`key_ctrl_enter`、`key_ctrl_alt_shift_enter` |
| Tab 发送 HT，Shift 加 Tab 固定发送 CSI Z，Alt 与 Shift 同时按下不额外加 ESC。 | I.encode | `key_tab`、`key_shift_tab`、`key_alt_tab`、`key_alt_shift_tab` |
| Backspace 默认发送 DEL，Ctrl 改为 BS，Ctrl 加 Space 发送 NUL。 | I.encode | `key_backspace`、`key_ctrl_backspace`、`key_ctrl_alt_backspace`、`key_space`、`key_ctrl_space` |
| Ctrl 字母和指定标点／数字映射为控制字节，Alt 通常加 ESC 前缀，但 Ctrl 加左方括号／3 直接返回一个 ESC。 | I.encode | `key_ctrl_c`、`key_ctrl_a`、`key_ctrl_at`、`key_ctrl_2` 至 `key_ctrl_8`、`key_ctrl_backslash`、`key_ctrl_slash`、`key_ctrl_question`、`key_ctrl_alt_bracket_open` |
| Shift 单字符使用大写转换而不模拟键盘符号布局，加号键支持尾部两个加号的名称写法。 | I.encode | `key_shift_a`、`key_shift_1`、`key_ctrl_plus`、`key_alt_plus` |
| 编码器忽略名称大小写并接受 control／option／meta／return／escape 别名，未知基键返回 null，未知修饰词被忽略。 | I.encode | `encode_case_alias`、`encode_meta_alias`、`encode_return_alias`、`encode_escape_alias`、`encode_unknown`、`encode_empty`、`encode_trailing_separator`、`encode_unknown_modifier` |
| 多行文本加括号粘贴边界，CRLF 和 LF 改为 CR，其他换行字符保持原值。 | I.text；WT.aPasteWithLinesIsOnePiece | `paste_plain`、`paste_multiline`、`paste_cr`、`paste_unicode_separator`、`paste_tab`、`paste_empty` |
| Ctrl／Alt 下的 ASCII 字符转换为键名，非 ASCII 和换行仍作为文本，单独 Shift 将文本大写。 | M.typed；TT.typedCharacters | `typed_ctrl_upper`、`typed_ctrl_space`、`typed_ctrl_chinese`、`typed_ctrl_newline`、`typed_shift_lower`、`typed_shift_nonascii` |
| 单击使修饰键单次生效，快速第二次点击锁定，慢速第二次点击关闭。 | M.tap／pressed；TT.TerminalModifierTests | `modifier_once`、`modifier_lock`、`modifier_slow_second_tap` |
| 按住期间发送键后释放会关闭，未发送就释放会变成单次生效。 | M.hold／release／pressed | `modifier_hold_used`、`modifier_hold_unused`、`modifier_mixed_states` |
| 多个按住键共用一个使用标志，后一次 hold 会重置这个标志。 | M.hold／release | `modifier_shared_hold_usage` |
| 请求内先文本后键，键后的文本另起请求，未发送文字的退格在本地按字素撤销。 | I.KeyBatcher；WT.KeyBatcherTests | `batch_text_then_keys`、`batch_text_after_key`、`batch_pending_backspace`、`batch_backspace_after_empty`、`batch_grapheme_backspace`、`batch_empty_text` |

`quick` 是触摸层已经判断好的布尔输入；本目录不把手势时间配置当成实际输入延迟。拼音组字的 marked text 应只在候选提交后发送，`insertText` 和 `unmarkText` 两条提交路径都要支持（CLAUDE.md、I.UITextInput）。这些 UIKit／Android InputConnection 回调，以及 `hasText`、听写、键栏触摸事件仍需平台测试，本批没有声称验证它们。`dict[k, default:]` 的经验针对 `CGMutablePath` 字典持有方式，属于框线渲染检查，不应翻译成 Kotlin 网格规则。

## JSON 结构

所有文件是 UTF-8、两空格缩进，顶层为 `schema_version: 1`、`kind` 和 `cases` 数组。每个用例包含全目录唯一的英文 snake_case `name`、中文 `description`、非空出处数组 `source`、`input` 和 `expected`。

直接搬自单测的输入与断言标 `from_test`；为实现补充的边界或改造后的录制片段标 `derived: true`，两者互斥。单测用例的完整输出快照也由真实实现补齐，不仅检查原单测中的几项断言。

### 网格与历史行

`kind: grid` 的输入是 `cols`、`rows` 和 `frames`。每帧恰好有 `vt`（JSON 转义字符串，按 Swift Character 喂入）或 `base64`（解码成字节后调用 `feed(Data)`）之一，可选 `reset: {cols, rows}` 在喂入前重置网格。按顺序处理所有帧，帧之间保留网格和解析器状态。这里没有传输层的 epoch／seq 判定；full 帧在适配时显式转成 reset。

`expected.steps` 与 `frames` 一一对应，每步含：

| 字段 | 含义 |
| --- | --- |
| `cols`、`rows` | 规范化后的尺寸 |
| `text` | 每源行一个字符串，补到 `cols` 格，普通空格和尾空格不可裁掉 |
| `row_lengths` | 每行实际存储的格子数，隐式尾空白与显式空格需区分 |
| `wide_cells` | 所有宽度不等于一的格子，按行列排序为 `[row, col, width]`；缺省宽度为一 |
| `style_runs` | 所有非默认样式的连续区间，包含 `row`、`from`、`to`、`style`，区间左闭右开 |
| `cursor` | 零基 `row`、`col` 和 `visible`；隐藏时仍比较坐标 |
| `scrolled` | `TerminalGrid.scrolled(from: old, to: new)` 的整数结果 |

每个宽度零的右半格用 **U+FFF0** 占位，绝不作为终端输入。左半格保留原始字素，不能用 UTF-16 长度当作占格数；例如 `中\ufff0a` 表示三格。`wide_cells` 是权威宽度，不能假定宽度二的格子总跟着宽度零的格子，见逐格擦除用例。输入不含原生 U+FFF0；比较时不做 Unicode 归一化。

`old` 是本帧喂入前的网格；如果有 reset，先 reset 再取 old，因此重置帧不把旧尺寸屏幕算入滚动检测。快照保留全部 `rows`，不套用 `TerminalFeed` 的尾部空行裁剪。

`style` 只列出非默认字段：颜色为 `{"indexed": n}` 或 `{"rgb": [r,g,b]}`；省略前景／背景表示默认色；布尔样式省略表示 false；省略 `link` 表示无链接。`style_runs` 之外的格子为默认样式，禁止忽略颜色、链接或空格上的样式。

`kind: ansi_rows` 输入为 `text`，直接调用 `rows(fromANSI:)`；输出同样使用 `text`、`row_lengths`、`wide_cells`、`style_runs`，但不补齐固定列宽，也没有光标和滚动字段。`kind: width` 输入单个字素 `character`，输出整数 `width`。

### 折行

`kind: wrap` 的 `input` 包含 `ansi`、正整数 `per_line`、布尔 `wrap` 和可空的 `cursor: {row, col}`。先用 `rows(fromANSI:)` 得到源行，再执行 `TerminalWrap.lines`；不能先用固定宽网格截断文本。

`expected.lines` 每项为 `row`、`from`、`to`、`indent`，均以格为单位，范围左闭右开。缩进不包含在源行范围内；不额外拼接文本或裁掉断点空格。`cursor` 为 null 表示不参与显示范围计算。

### 输入与键编码

| kind | input | expected |
| --- | --- | --- |
| `key` | `key` 与 `modifiers` 数组，修饰词为 ctrl／alt／shift | `name` 为 M.apply 的结果，`bytes` 为编码字符串，`utf8` 为字节整数数组 |
| `encode` | 完整 `name` | `bytes` 和 `utf8`；不支持时两者都为 null |
| `paste` | 原始 `text` | I.text 的 `bytes` 和 `utf8` |
| `typed` | 一个字素 `character` 与 `modifiers` | `key` 和 `text`，恰好一个非 null；此阶段尚未编码成字节 |
| `modifiers` | `probe_key` 和顺序执行的 `actions` | 每步的 ctrl／alt／shift 状态、`active` 和 `applied` |
| `batch` | 每项恰含 `text` 或 `key` 的 `events` | 顺序调用 take 得到的 `requests: [{keys, text}]` 以及最终 `empty` |

`modifiers.actions` 支持 `tap`（带 `modifier`、`quick`）、`hold`、`release`（带 `modifier`）和 `pressed`。初始状态全为 `off`，另三种状态为 `once`、`held`、`locked`。`applied` 只是查询 `apply(probe_key)`，不会消耗修饰键，只有显式 `pressed` 才执行消费。

`bytes` 和 `utf8` 表达同一份输出，必须同时一致；例如 NUL 是 `"\u0000"`／`[0]`，null 表示不支持，空字符串／空数组表示合法空输出。拼接键名后再编码，不能对方向键一律加 ESC 前缀。

## 录制数据与隐私

`recorded_sample_output` 取自 `apple/PersonaTests/Fixtures/terminal-observe.jsonl` 的零基帧 0、2、3，保留原始字节及尺寸。第 1 帧包含命令路径，整帧跳过，所以这一用例是派生片段，首行只剩 `$`，不声称与原录制整屏相同。

核查时先 base64 解码，再去掉 CSI 和 OSC 8 的控制部分，逐段检查可打印内容；仅放行示例构建文案、框线、中文对齐示例、emoji、重复字母以及 `https://example.com/docs`。没有路径、主机名、账号或真实对话。测试输入未复制其他录制中的会话内容。普通隐私扫描不会自动解码 base64，新增录制必须重复这一步，不能只看扫描通过。

## 对照与复跑

本次使用 Apple Swift 6.4，在 `/tmp/spec-term-reference/` 编译临时驱动程序。网格和折行直接引用 G、W 原文件；I、M 因同文件含 UIKit／SwiftUI 视图，在临时目录原样抽取纯逻辑声明，只把导入改为 Foundation。没有改动任何 iOS 文件，也没有另写一套 VT 或键编码算法。

临时程序 `main.swift` 不提交；本机保留源文件和 `check` 可执行文件。它读取所有 JSON，逐用例调用真实实现，将结果按上述结构序列化后与 `expected` 比较，任一不符立即失败；折行还检查 `TerminalLineLayout` 首次计算和重复计算等于全量结果。复跑命令在仓库根目录执行，编译和执行都有超时：

```sh
python3 - <<'PY'
from pathlib import Path
import subprocess

scratch = Path('/tmp/spec-term-reference')
assert (scratch / 'main.swift').is_file(), '临时驱动程序已清理，请按下表重建'
input_source = Path('apple/Persona/Views/Session/Terminal/TerminalInput.swift').read_text()
modifier_source = Path('apple/Persona/Views/Chat/TerminalKeys.swift').read_text()
(scratch / 'InputCore.swift').write_text('import Foundation\n' + input_source[
    input_source.index('nonisolated enum TerminalInputEvent'):
    input_source.index('/// A position in the composition')])
(scratch / 'Modifiers.swift').write_text('import Foundation\n' + modifier_source[
    modifier_source.index('enum ModifierState'):
    modifier_source.index('/// Terminal keys in one row')])
subprocess.run([
    'swiftc', 'apple/Persona/Views/Session/Terminal/TerminalGrid.swift',
    'apple/Persona/Views/Session/Terminal/TerminalWrap.swift',
    str(scratch / 'InputCore.swift'), str(scratch / 'Modifiers.swift'),
    str(scratch / 'main.swift'), '-o', str(scratch / 'check')
], check=True, timeout=120)
files = sorted(Path('spec/fixtures/terminal').glob('*.json'))
subprocess.run([str(scratch / 'check'), *map(str, files)], check=True, timeout=60)
PY
```

结果为 `PASS 193 cases`。另对原有 mixed／permission 录制的 130 帧做只读回放，最终文本与配套 `.txt` 逐行一致，并验证显示宽度 23／40／80、折行开／关时的增量布局等于全量布局；这些录制的内容没有复制进本目录。复跑这项检查可用同样的 G、W 源文件编译 `/tmp/spec-term-reference/recordings/main.swift`，执行其生成的 `check`，同样设置编译和执行超时。

临时目录会被系统清理；其他机器需按以下调用映射重建一次性 JSON 驱动程序，序列化规则见上一节，不需要访问线上服务。

| kind | 实现入口 |
| --- | --- |
| grid | 初始化 `TerminalGrid`，逐帧可选 reset、保存 `lines`、feed、读取状态并调用 `scrolled` |
| ansi_rows／width | `TerminalGrid.rows(fromANSI:)`／`TerminalGrid.width` |
| wrap | `TerminalWrap.lines`，并与 `TerminalLineLayout.lines` 比较 |
| key | 将列表中的修饰键设为 `.once`，`TerminalModifiers.apply` 后调用 `TerminalKeyBytes.encode` |
| encode／paste | `TerminalKeyBytes.encode`／`TerminalKeyBytes.text` |
| typed | 将列表中的修饰键设为 `.once`，调用 `TerminalModifiers.typed` |
| modifiers | 从默认 `TerminalModifiers` 开始，逐动作执行对应方法后读取状态 |
| batch | 向 `KeyBatcher.push` 逐项送入事件，再 take 到 nil |

这批对照不需要 Go、数据库、模拟器或 Android 构建。它没有验证平台输入法、绘制效果，也没有性能测试结果。

## 已知差异与接入边界

- 文档说不在开头标点后断行，但 `canBreak` 检查宽字符右半格的空文本时，无法识别全角开括号；`wrap_fullwidth_opening_current` 保留现状，Android 不应悄悄修正期望值。
- 网格是 herdr 重画结果的 VT 子集；越界丢弃、忽略备用屏、逐格擦除后可能残留半个宽字符、跨帧 UTF-8 不重组，都按当前 iOS 实现记录。
- 多键同时按住共用 `usedWhileHeld`，以及 Ctrl+Alt+左方括号只输出一个 ESC，均已用边界夹具固定，不能按通常的终端经验改写。
- 第十一节的历史描述要结合第十二节更正阅读：接管时滑动优先转发 herdr 滚轮；本目录的上滚检测只给出纯函数结果，不规定 UI 何时追加或展示历史。
- `TerminalCore` 总体设计还包含帧序、重连和接管状态机，本车道只交付网格、折行、滚动检测与输入纯逻辑；传输状态机和平台输入法由后续终端移植车道接入。

## 新增用例

先在对应主题文件添加唯一名称、具体中文说明和可定位的来源；直接复制单测才写 `from_test`，修改过输入或补边界则写 `derived: true`。一次用例只改变一组相关行为，需要验证跨帧状态时将所有相关帧放在同一用例。

手工写出要验证的关键断言，再通过现有 iOS 实现补齐整份 `expected`；核对零基格位、右半格、显式尾空格、颜色和光标。不要用新写的 Kotlin 实现生成自己的基准答案。新增规则同步补到上面的规则表，录制数据先核查解码后的可打印内容。

提交前复跑对照、检查 JSON 为 UTF-8 两空格缩进且名称唯一，并执行 `python3 scripts/privacy_check.py --staged` 及带 `--message` 的提交信息扫描；只暂存本目录的具体文件。
