# 经验：测试与验证

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- 截图验证别靠 computer-use 点模拟器（Mac 锁屏时拿不到窗口，没锁时会抢用户的鼠标）：用 DEBUG 启动参数（`docs/ios.md`）直达页面再截图。
- `xcodebuild test` 会启动不带 `-pairURL` 的 App 申请通知权限，之后这台模拟器的截图都会被授权弹窗挡住，跑完测试要重启模拟器再截图。
- 模拟器里上一次启动遗留的系统授权弹窗，卸载重装 App 也不消失，要 `simctl shutdown` 重启模拟器（手动 `simctl launch` 不带 `-pairURL` 就会弹通知授权）。
- `xcrun simctl launch` 对已经在运行的 App 只是拉到前台、新启动参数不生效：截图前用 `--terminate-running-process`。
- UI 测试一律 `app.launchPinned()` 启动（`UITestStyle`）：App 记着上一个测试留下的风格，BeautifulUI 风格的界面又不一样，不说风格的测试会碰上别人留下的样子
  （LongSessionTests 在 BeautifulStyleTests 之后跑就挂过）。默认液态玻璃，`TEST_RUNNER_JARVIS_UI_STYLE=beautiful` 整套换成 BeautifulUI 风格跑；
  只测某一种风格的测试自己传 `-ui.style`。它经 `launchJarvis()` 启动，没选布局的测试照旧跑在四个 tab 的旧布局上。`UITestStyleTests` 查源码（`launch()`、`launchJarvis()` 都算漏）。
- JarvisTests 别跑在配对过真 hub 的模拟器上（`jarvis-sim` / `sim.sh` 配对的那台）：iOS 的测试宿主不跳过钥匙串（只有 Mac 跳过，`Shared/Keychain.swift`），
  一启动就拿配对连 hub、以模拟器身份注册手机工具、弹通知授权。2026-11-02 在这样一台上连着三次「test runner hung before establishing connection」
  （没锁屏，重启模拟器也一样），换一台从没装过 App 的模拟器第一次就过了，卡住的具体原因没再追。用 `simctl list devices` 挑一台没跑过 Jarvis 的。
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
- `-pairURL` 配的 hub 和这台模拟器上次配的不同时，App 从头开始、停在助手页：`-app.tab`、`-dev.openChat`、`-tab dev` 都不起作用（2026-11-01）。
  老测试一直连同一个 mock 地址，碰不到；换了端口、换了 hub 就碰到。要可靠：先只带 `-pairURL` 启动一次、退出，再带页面参数启动（`ScrollUnderPushTests`）。
- 「推送时不打断」要在手指还在、惯性还在的时候推。停下后再推，测不出滚动中整屏跳：`LiveUpdateTests` 的「滚到中间行不动」一直是绿的，
  真实 hub 上 Agents 列表滚动中跳到 900 pt 以上。逐帧量用 `PerfScroll`（`docs/testing.md`「滚动中推送」）。
- 要在真实数据上量又不能改用户的状态时，在 hub 前面挡一个只读代理：GET 照转；别的方法回 `{}` 不发出去；WebSocket 只往上转 subscribe、unsubscribe、pong
  （不转 `register_phone`，模拟器就不会接 `phone_*` 调用）。同一份数据开着推送、扣住推送各跑一遍（A/B），差出来的才是推送造成的。
  代理、录下来的推送、截图只放 `/tmp`，不进仓库（里面是真实的会话）。
- 查「这个 body 为什么又跑了」：body 里临时加 `let _ = Self._logChanges()`，再
  `xcrun simctl spawn <udid> log show --info --debug --predicate 'eventMessage CONTAINS "AgentsList:"'`，
  看到的是「哪个属性变了」（`\AppModel.allSessions changed`、`_xxx changed` 是 `@AppStorage`）。
  2026-11-01 靠它找到了 UserDefaults 写入、`onChange` 里读模型这两处漏。查完删掉。
- 逐帧量滚动时，异常按「前 0.6 秒内到的推送」归因只是时间上的巧合：mock 每轮几条推送一起到，几乎每个异常都能对上一条。
  确认是谁造成的，要逐类关掉推送重跑（mock 加临时开关），再看 App 里的时间线（`PerfScroll.mark`）。
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
  不能因为找不到 `app.tabBars` 就认定配对页没有退出。外屏只有约 678pt 高，Form 最后一行可能还没被懒加载；先滚到「确认并继续」再点。
- Agents 的搜索默认收起后，列表身份用固定的导航菜单或列表内容判断，不能用搜索框是否存在判断是否还在对话里；快速筛选是首行，滚走后也不能拿它判断页面身份。
  需要输入时先下拉，已经展开时不重复下拉触发刷新；空档回归同时检查首行与导航栏的距离和选中胶囊的真实像素。

- 模拟器的钥匙串里留着以前配对过的 hub（比如测 mock 时的 `127.0.0.1:18831`）时，`-demo.on YES` 不生效：`Keychain.serverConfig` 优先，
  App 画的是快照、请求都发去那个死掉的地址，看上去「点了没反应」，toast 是「连不上 hub，刚才的操作没有生效」。跑体验模式的测试前
  `xcrun simctl keychain <udid> reset`。

## 隐私检查与依赖下载源

- 2026-11-02 App 配好 `app.push_relay` 后，发布方的公开中继 URL 必须进 Info.plist，但它也会被配置里的个人值扫描命中。
  App bundle 只识别配置中完整、无凭据 / 查询参数的 HTTPS `/relay` URL；同一域名出现在其他位置、附加路径 / 参数、邮箱或凭据仍拒绝，
  公开 Go 二进制也不放行。不要从私词表删掉整个域名，回归覆盖这些相邻和扩展 URL。

- 2026-11-02 隐私扫描修好后，公开 jarvisd 仍未部署：引入网络依赖后原始二进制约 35–37 MiB，超过 Workers 静态资源每个文件 25 MiB 的限制；不是 cf 鉴权过期。
  发布 `.gz` 下载包（约 12–14 MiB），先扫描未压缩二进制，安装端先校验压缩包再解压；上传前检查所有资源尺寸。`NO_DEPLOY=1` 也必须过尺寸检查，不能只编译 / 扫描就算部署验证。
  失败不能一概提示重新登录；TestFlight 上传成功、服务端发布失败要分开报告，并继续完成已上传构建的内部 / 外部分发。
- 2026-11-02 公开 jarvisd 的扫描误报：SSH 的全局请求名被当成邮箱，Tailscale 内嵌网页的官方联系链接、RFC1918 完整网段和云元数据常量被当成个人数据。
  Go 会把只读字符串拼在一起，CIDR 后甚至紧接另一个 CIDR，不能按可打印字符串的边界判断。先按真实编译输入追到依赖源码，不把原始命中输出到日志。
  `check_bundle_privacy.py --whole-short` 只对带 `public` 构建标记、未 replace 且 checksum 与 `server/go.sum` 一致的依赖，按精确协议 / HTML / 网段上下文分类；
  `publish.sh` 先 `go mod verify`。配置和私词表仍在原始文本上优先匹配，依赖缺失、校验和不符、上下文变化都拒绝；不能放行整个邮箱域名或整个私网段。
  回归：`python3 -B -m unittest discover -s scripts -p 'test*privacy*.py'`，另跑四个平台的 `NO_DEPLOY=1 ./scripts/publish.sh`。
- `privacy_check: clean` 不代表私有值字典完整：没有本机配置、私词表时，已登记的匹配项可能只剩本机路径。npm 锁文件的下载主机要用独立的公开来源规则拦截，不能只靠私词表；包名是公开的，不代表 `resolved` 下载地址也是公开的。
- 检查暂存内容必须读取 Git 索引，不能只读工作树：工作树里已经修好、索引里仍是旧地址时，提交钩子必须拒绝；反过来索引安全、工作树后来改坏时，不应误拦已暂存的安全内容。
- 锁文件先按完整 JSON 检查，再走普通文本扫描；不要被 NUL/二进制判断跳过，重复键和 JSON 转义也不能绕过检查。错误只输出位置和类型，不能让被拦截的地址进入日志。
- hooks 文件存在不等于启用：`core.hooksPath` 是克隆本地配置，启用后用隔离临时仓库验证真实 `git commit`，不拿主工作区做拒绝测试。网页构建前也检查，避免未启用钩子的克隆直接发布。
- 测试递归失败要明确控制或模拟递归限制，不要假定固定嵌套层数在所有 Python 版本都会报错；Python 回归测试单独运行，`npm test` 不会自动覆盖它。

## 网页分组与折叠

- 对齐 iOS 状态分组时，不能直接按原始 `state` 分桶：idle / unknown 但带 loop、后台工作或运行中子 Agent 的是「待唤醒」；屏蔽读取 `/mutes`，项目屏蔽按机器 / 项目名称匹配，Agent 的单独允许项可覆盖项目屏蔽，但不能覆盖 Agent 自己的屏蔽。
- CSS 的 `grid-template-rows: 0fr` 和 opacity 只把折叠内容藏起来，不会让链接退出键盘焦点和无障碍树；侧边栏折叠容器加 `inert`。组标题内的状态圆点已有同义文字，要 `aria-hidden`，否则按钮会被读成两遍「工作中」。
- 网页截图要先等菜单项真正渲染，不要在点开菜单的同一帧直接截图（可能只拍到外壳）；深浅色验证把主题偏好也存进 localStorage，单独改根节点 class 在 reload 后会回到旧主题。

## 新加的 UI 测试

- DEBUG 启动参数触发的动作（发一条消息、打开某页）不要写在 `onAppear` 里每次都跑（2026-11-02）：带 `-openSession` 启动时对话页 0.2 秒内出现、消失、再出现三次，
  `-openSendMessage` 就写了三条消息（各自新的 id），`ConnectionTests` 看到 hub 收到三次，像是断网重发的 bug。会产生副作用的触发一次启动只做一次（`ChatView.debugSent`）。
  判断是不是真重复：看 mock `/mock/sent` 里的 `client_msg_id`，同一条消息的重发 id 相同，服务端和 mock 都按它去重。
- 新建的测试文件要先 `xcodegen` 再跑，否则 `-only-testing:` 选中的是空集，xcodebuild 照样 `** TEST SUCCEEDED **`（`Executed 0 tests`）。
  看结果要 grep `Executed N tests`，N 不能是 0（2026-11-01，端口转发的 UI 测试第一次「通过」其实一个都没跑）。
- 启动一律 `app.launchPinned()`（钉住风格），不是 `launchJarvis()`：`UITestStyleTests` 扫源码拦这个。
- 在 zsh 里写 `for v in "dark shell" …; do set -- $v` 不会按空格拆开（zsh 默认不分词），`$1` 是整串、`$2` 为空：外观、布局参数全错，
  测试还是绿的。这种循环放进 `bash -c '…'` 跑。
- 新端把模型选择移到独立接口后，旧功能的保存请求要省略模型字段，防止旧草稿覆盖新选择；但旧 hub 的 PUT 可能把省略解释为清空，必须先确认统一配置能力，旧 hub 继续携带原字段。用 FakeHub 分别覆盖新旧协议，而不是只测新接口。
- SDK 和模拟器列表里有对应条目，不等于 Xcode 能完成构建：平台安装状态或运行时版本不匹配时，scheme 报目的地不可用，actool 也会报没有兼容运行时。用独立临时工程、单独编译依赖可做 Swift 类型检查，但绕过资源编译不能当作正常 App 构建或截图验收；不要改正式工程来掩盖环境问题。
- 模拟器和 Mac 共用回环：App 在模拟器里监听 `127.0.0.1:<端口>` 时，Mac 上已有程序占着这个号就会冲突（端口转发会退到随机端口，正好测到这条路径）。
- **测试里执行的脚本会在开发机上真的跑**（2026-11-01）：`TestDeployScriptInstallsTheBinary` 执行 hub 的部署脚本，脚本最后一句
  `pkill -f '[j]arvisd node --attach'` 把跑测试的那台机器上真在服务的常驻 node 杀了：装了常驻 node 的 Mac 上每跑一次 hub 测试就掉线一次
  （launchd 约 10 秒拉起、hub 经 tailcat 重连几十秒、DSH 插件再重连，合计约一分半，期间打开会话报 transcript not found）。
  以前没人在装了常驻 node 的机器上跑测试，一直没暴露。脚本里会碰进程或服务的命令（`pkill`、`launchctl`、`systemctl`…）在测试里一律用
  PATH 最前面的假命令替掉，只记参数、再断言参数（`hub/deploy_test.go` 的 `fakePkill`）；验证时先记下真进程的 pid，跑完核对没变。
- 新外壳里「从左边缘往右拖」不等于「返回」：Agents 页没开对话时它翻到助手页，在助手页上它拉出抽屉。随机操作的测试拿它当返回，
  最后停在抽屉上，Agents 列表看不见、按设计扣着更新，像是「收敛失败」（2026-11-02，`ScreenTruthTests` seed 42）。回列表用界面上的控件：
  对话的返回按钮、抽屉的关闭、助手页往左拖（`ScreenTruthTests.backToList`）；判断「在列表上」别只看筛选条，它随列表滚走，看有没有点得到的行。
  对话页曾经有两个「返回」：看得见的那层，和看不见的那层（折起后全开的那层滑进状态栏那一条，`isHittable` 也是真，点了不返回），取第一个点得到的会一直点错。
  2026-11-02 起看不见的那层不在树上，任何时候只有一个「返回」，就是屏幕上那个（`ChatHeaderTests`）；折起的细条本身一直在状态栏下面，两种风格都是，
  「标题栏在状态栏那一条」其实是那个看不见的「返回」的 frame。
