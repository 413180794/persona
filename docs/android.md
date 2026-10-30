# Android App

`android/`：Kotlin + Jetpack Compose 的 Android 客户端，连同一个 hub，接口和 iOS 一样（`docs/protocol.md`），服务端不用改。
minSdk 26（Android 8），targetSdk 34。界面中文。

一句话描述：在 Android 手机上看各台电脑 herdr 里的 Agent、发消息、回答提问 / 审批计划 / 授权、按终端对话框的选项，
不需要任何推送账号。

## 现在有什么

| 页面 | 内容 |
| --- | --- |
| 配对 | 扫 `jarvisd hub pair` 的二维码（`jarvis://pair?url=…&token=…`，也能从链接打开），或手动填地址和 token；配对后立刻 `POST /api/v1/auth/tokens`（`kind: phone`）换这台手机自己的 token，旧 hub 没有这个接口时继续用配对 token |
| Agents | 顶部切换「按项目 / 按状态 / 按机器」（记住选择），排序规则和 iOS `ChatsView` 一样，写在 `data/Grouping.kt` 里并有单测。上面固定是「需要你处理」卡片和置顶的 Agent（只出现一次），下面是分组，最后是最近 7 天的 offline 会话；不含助手的对话；下拉刷新。<br>按项目：置顶的空间 → 其他忙的空间 → 屏蔽但在跑的空间（弱化）→「空闲的空间」（默认折叠）；空间里忙的、置顶的、待唤醒的在上，空闲的收成一行，点开展开。<br>按状态：需要你 → 完成未看 → 工作中 → 待唤醒 → 已屏蔽 → 已看（「已看」默认折叠）。<br>按机器：按 hub 配置的顺序，标题带在线状态和状态计数。<br>每一段都能点标题折叠，折叠状态存在手机上 |
| 空间操作 | 空间标题右边的 ⋯：置顶空间、屏蔽这个空间（其中的 Agent 可以单独开启通知）、重命名空间（改 herdr workspace 名，hub 把置顶 / 屏蔽一起改到新名字上） |
| 对话 | 历史分页（`turns=2`，滚到顶自动往前翻）、实时 items（WebSocket `subscribe`，按「items 的连续性」补洞）、工具调用和结果合成一行（点开看命令 / diff / 输出，截断的点「看全文」）、图片按需取（`lazy_images`）、Markdown；发消息带 `client_msg_id`，被拒（`dialog_open` / `input_draft`）时弹窗说明，单行草稿可以「清空并发送」；中断（Esc）；Claude 给的下一条建议点一下填进输入框 |
| 交互卡片 | 提问（单选直接点、多选 / 自定义文本再提交）、计划（批准 / 批准并全部放行 / 打回带反馈）、授权（允许 / 总是允许 / 拒绝）、终端对话框（按 hub 解析出的 `option` 回答，`prompt_changed` 时提示）、密码框；都能「交给电脑处理」（`dismiss`） |
| 终端 | 对话页右上角：每秒读一次 `/screen`，下面一排按键（Esc、↑↓、回车、Tab、1/2/3、Ctrl-C） |
| Agent 操作 | 长按一行：置顶 / 取消置顶（置顶的只在顶部「置顶」里出现一次）、屏蔽 / 开启通知（规则和 hub 一样：Agent 自己 → `allowed` → 它的空间；屏蔽的灰点加 🔕、不弹通知）、重命名（herdr 标签页名和 Claude 会话名一起改）、关闭 Agent（先说明会退出哪台电脑上的什么、关掉哪个标签页）。置顶 / 屏蔽存在 hub（`/api/v1/pins`、`/api/v1/mutes`），点了立刻生效，hub 拒绝时退回原样 |
| 新开会话 | 列表右下角 +：选电脑、目录（家目录里列「最近用过」，点一下选中、再点进入）、启动器、权限模式（和 iOS 同一组，「默认」交给启动器）、可选的第一条消息。hub 说没启动起来但新窗格里其实起来了（停在对话框上，或者 node 不认识这个版本的输入框底栏）时，照样打开新会话 |
| 设置 | hub 地址和连接状态、各台机器（在线、版本、`node_mismatch` 提示、重新连接）、后台通知设置入口、断开 hub |

还没做（iOS 有）：「对话」tab 的助手、收件箱 / 邮件 / 飞书、`phone_*` 手机数据工具、实时终端流（`terminal_subscribe`）、文件和图片上传、
空间页、快捷筛选和搜索、手表。协议都在，后续按 iOS 的做法补。

hub 拒绝这台手机的 token（401：在别的设备上撤销了、换了 hub）时不再无限重连，直接回到配对页并说明原因；已配对时打开配对链接也会回到配对页重新配。

## 通知（不用推送服务）

国内卖的安卓手机大多没有 Google 服务（FCM 收不到），各家厂商推送又要开发者账号和实名认证，所以第一版不接推送：

- App 开一个前台服务（`HubService`，类型 `specialUse`）一直连着 hub 的 WebSocket，通知栏常驻一条「已连接 hub / N 件事需要你」（最低优先级，不响）。
  网络变了马上重连，断线指数退避到 30 秒；开机后自动启动（`BootReceiver`）。
- 收到 `interaction` 就弹「需要你处理」通知（高优先级），能直接在通知上点：授权「允许 / 拒绝」、计划「批准 / 打回（可输入反馈）」、
  单选提问的前 3 个选项、终端对话框的前 3 个选项。`interaction_resolved` 时收回通知。
- 一轮做完（工作中 → 完成未看）且 App 不在前台时弹「做完了」。对话页只在 App 真的在前台时才上报已看（`/seen`），
  否则放在后台的对话页会把「做完了」吞掉。
- 规则对照 hub 给 iPhone 的推送（`docs/server.md`「推送策略」），但时机在手机上算：需要你立即弹；做完了不等 `done_delay_seconds`。
- 代价是耗电和被系统清理。小米 / 红米（MIUI、HyperOS）等要在系统设置里把这个 App 的省电策略设成「无限制」并打开「自启动」，
  设置页有两个入口按钮（电池优化白名单、MIUI 的自启动管理页，找不到时退回应用详情页）。以后要接厂商推送时，hub 侧加一个和 APNs 并列的发送器即可。

## 构建和安装

```sh
./scripts/android.sh          # 编译 debug 包、装到连着的手机 / 模拟器并打开
./scripts/android.sh apk      # 只出 APK：android/app/build/outputs/apk/debug/app-debug.apk
./scripts/android.sh test     # 单元测试（模型解码、行合并、Markdown）
```

- 需要 Android SDK（`$ANDROID_HOME`，默认 `~/Library/Android/sdk`）和 JDK 17；没有 JDK 17 时脚本用 Android Studio 自带的。
- Application ID 走配置：`~/.config/jarvis/config.json` 的 `android.application_id`，没写就用 `app.bundle_id`，再没有就是 `com.example.jarvis`。
- hub 默认是 HTTP（内网 / VPN），`network_security.xml` 放开了明文流量；不要把 hub 暴露到公网。
- 装到真机：手机开 USB 调试连上电脑跑 `./scripts/android.sh`，或者把 APK 发到手机上直接装。

## 代码

| 文件 | 内容 |
| --- | --- |
| `data/Models.kt` | 线上类型（字段全可选，未知字段忽略，Go 的零时间当空） |
| `data/ApiClient.kt` | REST；错误统一成 `HubException(status, message, code, fields)` |
| `data/Hub.kt` | WebSocket、重连、machines / sessions / interactions 的状态和事件流 |
| `data/ChatStore.kt` | 一个会话的 items：`covered`、`history`、补洞（`after_seq`）、往前翻页 |
| `service/HubService.kt`、`Notifier.kt`、`ActionReceiver.kt` | 前台服务、通知、通知上的按钮 |
| `ui/` | Compose 页面：`AgentsScreen`、`ChatScreen`、`InteractionCard`、`PairScreen`、`SettingsScreen`、`Markdown` |

## 验证记录

模拟器（Android 14）连真实 hub，一台 Linux 跑 hub、一台 Mac 经 SSH 接入：

- 扫码链接配对、换设备 token；两台机器在线，列表显示真实会话
- 手机发消息到 Mac 上 herdr 里的 claude；claude 的 AskUserQuestion 变成卡片（hook 拦下的走 `question`，TUI 里的走 `screen`），在手机上选择后 claude 收到
- 计划模式下 ExitPlanMode 的对话框在手机上批准，claude 继续执行改了文件
- App 在后台：提问通知上直接点选项，claude 收到；一轮做完弹「做完了」
- hub 重启后几秒内自动重连；长会话往前翻到两天前的内容；深色模式截图检查

真机（Redmi K60 Pro，Android 16，澎湃 OS 3，没有 Google 服务），手机经 USB（`adb reverse tcp:18660 tcp:18660`）连 hub：

- 配对、列表、对话页正常；claude 的提问在手机上回答，电脑上的 claude 收到
- 「需要你处理」的通知系统里有（channel `input`，重要级别最高），但默认不在通知栏显示：澎湃 OS 给新装的 App 关掉了悬浮 / 锁屏通知，
  要在「设置 › 通知与控制中心 › 通知管理 › Jarvis」里打开。锁屏后长时间的后台连接还没在真机上测

## 经验

- `adb shell input swipe` 从屏幕最上方往下滑会拉出系统的快捷设置，测试翻历史时从屏幕中间开始滑。
- herdr server 继承启动它的 shell 的环境：在带代理变量的 shell 里起 herdr，里面的 claude 也会走那个代理。联调时用干净的登录 shell 起 herdr。
- Claude Code 设置里放开了的工具（allow 规则、bypass 模式）不会触发 PermissionRequest，测授权卡片要在没放开的会话里。
- 新开会话时 node 靠输入框底栏的字样判断 claude 起来了（`server/internal/node/control.go` 的 `inputBoxVisible`）。有的 Claude Code 版本在手动确认模式下底栏是
  `⏸ manual mode on · ← for agents`，node 认不出，等满 1 分钟报「did not become ready」，其实会话已经能用：App 按「启动前没有、启动后在这个目录里的窗格」找到它并打开。
- 小米 / 红米：`adb install` 要先在开发者选项里打开「USB 安装」（要登录小米账号），手机上还会弹「通过 USB 安装」的确认框，几秒内不点就自动拒绝
  （`INSTALL_FAILED_USER_RESTRICTED`）。`adb shell input` 代点屏幕要再打开「USB 调试（安全设置）」，否则报 `INJECT_EVENTS`。
- 手机没法直接访问 hub 时（hub 在内网、手机在流量上），联调可以走 USB：电脑上把本机 18660 转到 hub，再 `adb reverse tcp:18660 tcp:18660`，
  手机配对时用 `http://127.0.0.1:18660`。
