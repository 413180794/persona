# 经验：测试与验证

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- 截图验证别靠 computer-use 点模拟器（Mac 锁屏时拿不到窗口，没锁时会抢用户的鼠标）：用 DEBUG 启动参数（`docs/ios.md`）直达页面再截图。
- `xcodebuild test` 会启动不带 `-pairURL` 的 App 申请通知权限，之后这台模拟器的截图都会被授权弹窗挡住，跑完测试要重启模拟器再截图。
- 模拟器里上一次启动遗留的系统授权弹窗，卸载重装 App 也不消失，要 `simctl shutdown` 重启模拟器（手动 `simctl launch` 不带 `-pairURL` 就会弹通知授权）。
- `xcrun simctl launch` 对已经在运行的 App 只是拉到前台、新启动参数不生效：截图前用 `--terminate-running-process`。
- UI 测试锁了模拟器屏幕却没解锁（模拟器上按 Home 不解锁，要上滑），下一次 `xcodebuild test` 报「test runner hung before establishing connection」。
  模拟器系统语言是英文时，系统授权弹窗的按钮叫「Allow」，UI 测试两种都要认。
- 模拟器会被 XCUITest 的 typeText 切成「硬件键盘」模式：之后软键盘不再弹出（键盘按钮显示已弹起，但屏幕上没有键盘），依赖键盘高度的测试全挂。
  这台机器没有 Simulator.app，`defaults write com.apple.iphonesimulator …ConnectHardwareKeyboard` 改不回来：`simctl create` 一台新的跑。
- XCUITest 在键盘弹起的动画中点键栏会点空（键栏正跟着键盘移动）：先等键盘出现、停一下再点。
- 按时间排的列表要有确定的次序（同一时间再按 id）：两个空间最后更新时间相同时，每次渲染顺序都可能对调，UI 测试点到的是另一行。
- XCUITest 的启动参数（`-key value`）会盖过 UserDefaults / `@AppStorage` 的读取，App 自己写进去的值读不出来：只能用来固定测试不会改的值
  （新对话上次的机器），会在界面上改的（Agents 的分组、筛选）要在测试开头用界面恢复。`-dev.openChat ""` 让 App 不打开上次的对话。
  反过来用 `-dev.openChat <会话>` 打开对话时要连 `-app.tab chats` 一起给：冷启动只在存着的 tab 是 Agents 时恢复对话，`-tab dev` 切 tab 时已经晚了，
  上一个测试停在别的 tab 时对话就打不开（2026-10-31 `LiveUpdateTests` 的看图用例在别的车道「第一步就找不到过程卡」）。只想打开某个对话用 `-openSession`。
- 看不到截图（Read 打开 PNG 什么都没有、模型说「工具没有输出」）不是目录的问题（2026-10-31 试过 `$TMPDIR`、`/tmp`、仓库里的图都一样）：
  是模型网关把工具结果里的图片丢了，只留文字。先用一张已知内容的图分别放在用户消息和 tool_result 里直接请求网关，对比两边能不能读出来；
  界面改动必须真的看到截图再交付，看不到就先修这条链路。具体网关和修法见 `CLAUDE.local.md`。
- 手动跑 `xcodebuild test` / `build-for-testing` 前先 `eval "$(python3 scripts/jarvis_config.py env)"`：配置里的 `xcode` 只经它变成 `DEVELOPER_DIR`。
  直接跑用的是系统默认的 Xcode，SDK 旧一档时报 `toolbarVerticalEdge` 之类「no exact matches in call to initializer」（2026-10-31 被当成别人的代码坏了，白查一下午）。
- 量界面的跳动 / 闪动要看画面：`simctl io recordVideo` 录下来、ffmpeg 拆帧，逐帧跟踪一条像素；只闪一帧的跳只有这样看得到。
  App 里记的位置日志可能和屏幕上的不一致（2026-10-31 日志说表头在原位，画面上整页偏了 10pt），以画面为准。
- XCUITest 的 `element.tap()` 在 iOS 27.0 模拟器上点在 frame 左上角往里约 4pt（27.1 上点在圆里）：圆形 `contentShape(.circle)` 的按钮
  （Jarvis 顶栏的玻璃圆钮「历史对话」「新对话」）点在圆外，什么都不发生，看上去像按钮坏了（2026-10-31 iPhone 17 Pro 上 `testButtonsAndPickingAConversation`
  在 master 上就挂）。手指和 VoiceOver 都点在中间，真机不受影响。这类按钮用 `tapCenter()`（`JarvisUITests/TapCenter.swift`）。
  点完要断言页面真的变了，别只找两个页面都有的东西（新对话页和对话页都有「问 Jarvis…」，点空了也过，错报到下一步）。
- 量滚动帧率别用 XCUITest 的拖动：它每次手势前要在 App 主线程上抓一次无障碍快照，自己就造成 40–100 ms 的长帧（每 3 秒一次，像是卡顿）。
  用 App 内的匀速滚动（`AutoScroller`，`-showFileAutoScroll`）加帧计数（`FrameMeter`），从一个空闲的会话打开（在跑的会话在后面每秒重画）。
- 带输入框的 alert：输入时键盘弹起，alert 跟着往上挪，紧接着点「保存」会点空（alert 还在、什么都没发生，看起来像保存没生效，2026-11-01 `AssistantTitlesTests` 差点因此去改 App）。
  打完字停一下再点，点完确认 alert 没了；alert 里的输入框光标落在哪是随机的，清空前先点框的右端把光标放到末尾。
- 隐私扫描要在真实构建 / 部署配置齐备后跑：缺配置时检查项不完整，显示 `clean` 不代表完整检查通过；新增配置后重新扫描。
  测试夹具的机器标识也可能与配置碰巧同名，改用中性占位值，不加豁免。
- `xcrun simctl push` 不运行通知服务扩展（2026-11-01，Xcode 27.1 / iOS 27.0）：CoreSimulatorBridge 把推送当本地通知直接加进通知中心，
  扩展进程不起，带 `mutable-content` 的推送原样显示（中继的推送就显示成「有新消息」）。扩展的逻辑放进共享代码、在 App 进程里测
  （`PushCrypto.service`，`NotificationServiceTests`），扩展本身只能真机验。同一个 App 的几条通知在通知中心叠成一组，
  UI 测试找文字前要先点开那组（label 带「已分组」）。
- Duo 外屏会把四个 tab 画成侧边按钮，无障碍树里没有 `TabBar` 父节点：配对完成要断言配对页消失、四个根导航按钮出现，再查 hub 保存的引擎。
  不能因为找不到 `app.tabBars` 就认定配对页没有退出。外屏只有约 678pt 高，Form 最后一行可能还没被懒加载；先滚到「暂时跳过」再点。
- Agents 的搜索默认收起后，列表身份用固定的导航菜单或列表内容判断，不能用搜索框是否存在判断是否还在对话里；快速筛选是首行，滚走后也不能拿它判断页面身份。
  需要输入时先下拉，已经展开时不重复下拉触发刷新；空档回归同时检查首行与导航栏的距离和选中胶囊的真实像素。

## 隐私检查与依赖下载源

- `privacy_check: clean` 不代表私有值字典完整：没有本机配置、私词表时，已登记的匹配项可能只剩本机路径。npm 锁文件的下载主机要用独立的公开来源规则拦截，不能只靠私词表；包名是公开的，不代表 `resolved` 下载地址也是公开的。
- 检查暂存内容必须读取 Git 索引，不能只读工作树：工作树里已经修好、索引里仍是旧地址时，提交钩子必须拒绝；反过来索引安全、工作树后来改坏时，不应误拦已暂存的安全内容。
- 锁文件先按完整 JSON 检查，再走普通文本扫描；不要被 NUL/二进制判断跳过，重复键和 JSON 转义也不能绕过检查。错误只输出位置和类型，不能让被拦截的地址进入日志。
- hooks 文件存在不等于启用：`core.hooksPath` 是克隆本地配置，启用后用隔离临时仓库验证真实 `git commit`，不拿主工作区做拒绝测试。网页构建前也检查，避免未启用钩子的克隆直接发布。
- 测试递归失败要明确控制或模拟递归限制，不要假定固定嵌套层数在所有 Python 版本都会报错；Python 回归测试单独运行，`npm test` 不会自动覆盖它。
