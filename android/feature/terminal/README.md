# Android 终端模块

已实现只看、手机尺寸接管、冲突确认、VT 网格绘制、宽字符、框线与块字符、帧续传及缺帧恢复、中文组字、修饰键、逐行滚轮、格子点击和 ANSI 读屏降级。终端使用随包附带的 JetBrains Mono，许可见 OFL.txt。终端配色固定深色，页面不透明（任何风格都不用玻璃），按钮保持单行，键栏可横滑。

首帧前始终保留网格视图，按实际布局计算列×行后才请求接管。尺寸变化的防抖参数沿用终端设计。接管中、只看由标题下的一行说明，不在终端上方另放提示卡：提示卡出现、消失会改终端高度，接管时每次都让窗格重排。键栏固定占位。按下位置作为整次滑动及惯性滚动的格子锚点，每行独立发送一个滚轮事件。

只看和读屏降级时照样能按键：键栏、键盘、粘贴走 `POST …/keys`（同一时间一个请求，在途时打的字攒进下一个，先文本后键，`KeyBatcher`），发完立刻再读一次屏幕；滚轮和点击只在接管时有。接管时输入经现有 WebSocket 按序发送，断线后不补发输入。进入后台或离开页面时取消任务并退订，交还电脑尺寸。旧 hub 或 node 不支持流时使用现有读屏 HTTP 接口。

## 主线旧 App 移植过来的（2026-11-02）

- 键栏：⇧Tab 是一个键（Claude Code 切模式）；所有键是设计系统的 `JKeyCap`，Ctrl / Alt / ⇧ 按 iOS 的样子分一次（着色加描边）、按住（实底）、锁住（实底加下划线）。
- 接管 / 只读记在手机上（`TerminalPreferences`，SharedPreferences `persona-terminal`，不跟配对走）；「接管终端？」弹框里选的只读只管这一次。
- 接管时滚出屏幕顶的行攒在手机上（`core:terminal` 的 `ScrollbackGrid`，最多 3000 行），实时画面贴着视图顶部；node 不支持 `terminal_scroll` 时手指滑动就在这些行里本地翻。键盘收起、窗格变高时程序重画出来的行从攒下的里去掉，不会出现两遍。只看时视图跟着最后一行，用户往上翻了就停在那里，翻回底部再跟；镜像屏幕最后一行文字下面的空行不画。
- 字形：框线（细、粗、双线、圆角、虚线、半线）逐格画，块字符填充，⏺ ⏸ ⏵ ⏹ ⏴ ● ■ ▶ ◀ 按形状画（字体里没有，系统会拿 emoji 画成两格宽的彩色图），✔ ✳ 等加 U+FE0E 当文字画，OSC 8 链接画半透明下划线（点了仍是终端单击）。
- 选择文本：顶栏的拷贝按钮打开整页纯文本（攒下的行 + 屏幕），等宽不折行，横竖都能滑，长按选一段，「全部拷贝」。
- 显示：顶栏右边的设置按钮打开「显示」面板：字号 7–20、步长 0.5、默认 15（dp，不跟系统字体大小，列数稳定），存在手机上；自动换行开关也在这里。

## 当前 App 接线

App 已依赖 `:feature:terminal`，`AppServices` 使用唯一活动连接创建 `HubTerminalTransport` 并提供随配对切换的代理（含 `keys`）；`Root` 已注册 `terminalEntries`，`ShellChat`、默认聊天入口及通知终端兜底均可导航到 `TerminalRoute`。

为完成入口和协议转发，最小修改了终端目录以外的 `ChatEntries.kt`、`ChatScreen.kt`、`ServerEvent.kt` 和 `OkHttpHubConnection.kt`；其余协议、HTTP 实现、路由均为新增文件。settings 只追加本模块的 include。

## 验证与范围

`TerminalCriticalPathTest`、`TerminalPortTest` 覆盖手机网格订阅、宽字符、重连续传、缺帧重取、接管冲突、只看时走 `/keys` 和分批、失败丢弃、记住只读 / 接管、攒行和放手清空、镜像空行、字号步进、中文组字、修饰键和多行粘贴；`core:terminal` 的 `ScrollbackGridTest` 用 iOS 的录制帧（`apple/PersonaTests/Fixtures/terminal-control-scroll.jsonl`）验证攒行，`BoxDrawingTest` 对照 iOS 的笔画表。

模拟器上用假 hub（`MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1`，攒行加 `MOCK_TERMINAL_SCROLL=1`，镜像真实帧加 `MOCK_TERMINAL_FIXTURE=apple/PersonaTests/Fixtures/terminal-observe-permission.jsonl`）看过三套风格 × 深浅色。真机输入法（拼音）未验证；性能未实测。
