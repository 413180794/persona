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
  （没锁屏，重启模拟器也一样），换一台从没装过 App 的模拟器第一次就过了，卡住的具体原因没再追。用 `simctl list devices` 挑一台没跑过 Persona 的。
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
- mock hub 要按真 hub 的样子答，尤其是「还没准备好」的那几种：Telegram 关着时真 hub 列不出它的能力（程序没跑过，能力是程序自己报的），
  mock 却一直列着 `login`，UI 测试和截图全绿，真 hub 上的页面却没有开关、写着「在 hub.json 设置」，打不开（2026-11-03，#622）。
  写 mock 前先读一遍真 hub 那条路径在各个状态下回什么，测试从用户一开始看到的状态（关着）走起。
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
  （Persona 顶栏的玻璃圆钮「历史对话」「新对话」）点在圆外，什么都不发生，看上去像按钮坏了（2026-10-31 iPhone 17 Pro 上 `testButtonsAndPickingAConversation`
  在 master 上就挂）。手指和 VoiceOver 都点在中间，真机不受影响。这类按钮用 `tapCenter()`（`JarvisUITests/TapCenter.swift`）。
  点完要断言页面真的变了，别只找两个页面都有的东西（新对话页和对话页都有「问 Persona…」，点空了也过，错报到下一步）。
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
  `xcrun simctl keychain <udid> reset`。Mac 的验收副本同理（钥匙串按 bundle id 分）：要体验模式用 `-openWelcome YES -demo.on YES`，先清掉这个副本的配对。

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
  回归：`python3 -B -m unittest discover -s scripts/release -p 'test*privacy*.py' && python3 -B -m unittest discover -s scripts/test -p 'test_privacy*.py'`，另跑四个平台的 `NO_DEPLOY=1 ./scripts/release/publish.sh`。
- 手表 Release 二进制可能是 arm64 + arm64_32 的通用 Mach-O；签名里的证书邮箱 / 个人名不是业务数据，隐私扫描只排除每个切片中经过结构和边界验证的嵌入签名 blob。
  `LC_CODE_SIGNATURE` 的偏移相对于切片，不能按第一个切片的签名位置截断整个文件；也不能因为是通用文件就把证书扫描误报后删掉私词。
  两个架构的签名前后、签名占位区剩余字节及文件尾照常扫描；畸形签名、越界 / 重叠切片不排除。回归覆盖薄 / 通用 32、64 位及大小端，另用真实手表归档验证。
- `privacy_check: clean` 不代表私有值字典完整：没有本机配置、私词表时，已登记的匹配项可能只剩本机路径。npm 锁文件的下载主机要用独立的公开来源规则拦截，不能只靠私词表；包名是公开的，不代表 `resolved` 下载地址也是公开的。
- 检查暂存内容必须读取 Git 索引，不能只读工作树：工作树里已经修好、索引里仍是旧地址时，提交钩子必须拒绝；反过来索引安全、工作树后来改坏时，不应误拦已暂存的安全内容。
- 锁文件先按完整 JSON 检查，再走普通文本扫描；不要被 NUL/二进制判断跳过，重复键和 JSON 转义也不能绕过检查。错误只输出位置和类型，不能让被拦截的地址进入日志。
- hooks 文件存在不等于启用：`core.hooksPath` 是克隆本地配置，启用后用隔离临时仓库验证真实 `git commit`，不拿主工作区做拒绝测试。网页构建前也检查，避免未启用钩子的克隆直接发布。
- 测试递归失败要明确控制或模拟递归限制，不要假定固定嵌套层数在所有 Python 版本都会报错；Python 回归测试单独运行，`npm test` 不会自动覆盖它。

## 网页分组与折叠

- 对齐 iOS 状态分组时，不能直接按原始 `state` 分桶：idle / unknown 但带 loop、后台工作或运行中子 Agent 的是「待唤醒」；屏蔽读取 `/mutes`，项目屏蔽按机器 / 项目名称匹配，Agent 的单独允许项可覆盖项目屏蔽，但不能覆盖 Agent 自己的屏蔽。
- CSS 的 `grid-template-rows: 0fr` 和 opacity 只把折叠内容藏起来，不会让链接退出键盘焦点和无障碍树；侧边栏折叠容器加 `inert`。组标题内的状态圆点已有同义文字，要 `aria-hidden`，否则按钮会被读成两遍「工作中」。
- 网页截图要先等菜单项真正渲染，不要在点开菜单的同一帧直接截图（可能只拍到外壳）；深浅色验证把主题偏好也存进 localStorage，单独改根节点 class 在 reload 后会回到旧主题。

## WebSocket 握手测试

- Node 22 的内置 `WebSocket` 在握手被 HTTP 401 / 400 拒绝时可能只触发 `error`，不触发 `close`，`readyState` 仍是 CONNECTING。
  拒绝测试不要等 `close`，更不能把任意网络错误当成鉴权正确；用 HTTP upgrade 请求直接断言拒绝状态（错误 / 缺失 token 为 401、
  缺失 / 错误子协议为 400、错误路径为 404），意外收到 101 或连接错误均失败，设置总超时并回收 socket。成功握手仍用真实 WebSocket 验证。

## 脚本目录迁移

- 脚本移到子目录时，文字引用替换不够：同时检查 `__file__` / `$0` 推算根目录、Python 同级 import、跨目录子进程、Go 的路径构造和 SSH 传脚本的调用。
  公共配置和隐私扫描保留顶层；隐私扫描用脚本所属仓库定位根目录，不能因为调用者当前目录不同扫到另一份 checkout。
- `unittest discover -s scripts` 不会自动发现没有包标记的子目录；按 `test/`、`release/`、`deploy/`、`mock/` 分别发现并核对数量。
  兼容 shell 入口只 `exec` 转发，用带空格的目录、空参数、标准输入、环境变量和非零退出码测试，不通过真正发布或重启服务来验证转发。

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
- **Xcode、SDK、模拟器运行时分开检查**：Xcode 可以搭配兼容的旧版运行时；某份运行时的设备限制不等于 Xcode 或真机的限制。
  `xcrun simctl list runtimes -j` 的 `supportedDeviceTypes` 给出实际设备列表；运行时 `Contents/Resources/profile.plist` 的
  `restrictedToDeviceTypes` 可能限定设备，按设备配置的 `productClass` 核对，不能只凭系统版本猜。创建失败时先查支持范围，再换组合。
- **运行时显示 available，创建却报 `runtime path not found`**：先核对 `simctl runtime list -j` 和运行时实际路径，不直接删镜像或改系统 plist。
  已缓存的镜像可用 `xcodebuild -downloadPlatform iOS -buildVersion <系统版本> -architectureVariant arm64 -exportPath <新建空目录>` 导出，
  再用 `xcodebuild -importPlatform <导出的 .exportedBundle>` 重新登记。一次排查中下载命令提示已缓存、导出完成，但退出码为 70；
  不能把它算作成功，需检查导出内容并确认导入退出码为 0。重新导入后，原来无法创建的设备成功创建、启动并运行 App。
  验收按「创建独立设备 → boot / bootstatus → 安装 App → 启动 → 截图」走完；若需运行测试，再确认 Xcode 能启动测试宿主。
  Swift Testing 可能先打印 XCTest 的 `Executed 0 tests`，再实际运行 Swift 测试；以末尾 `Test run with N tests ... passed` 的数量为准，不把前一行当成空跑。
- **`List` 里的 `LabeledContent` 在无障碍树里是一个元素**（2026-11-02）：标签和值合成一个 label（「登录, 来自 MacBook 的 gh 登录」），
  `app.staticTexts["来自 MacBook 的 gh 登录"]` 找不到，测试报失败、截图上却明明有这行字。按值找要用
  `descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", 值))`；同一页里单独的 `Text`（账号、状态）照常是 staticText。
- **小一点的 iPhone 上，列表里的行可能在浮动标签栏底下**（2026-11-02，#488）：`row.swipeLeft()` 从行的中间开始滑，
  落在 Liquid Glass 的标签栏上就成了拖动标签栏的选中项，切到最左边的「Persona」，测试只看到「拉不出关闭按钮」。行这时照样是
  `exists` / `hittable`。同一个测试在 iPhone 18 Pro Max 上过、在 iPhone 18 Pro 上 100% 挂，一度被当成 #382 的回归（#382 之前的提交在 Pro 上一样挂）。
  对行做滑动、点按前先 `app.bringAboveTabBar(row)`（`JarvisUITests/AgentsSearch.swift`）；查「只在某个车道挂」的 UI 测试，先比一下各自模拟器的机型。
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
  BeautifulUI 的新外壳里要知道对话是否开着，用 `probe.chat`（page `agents`），不靠「返回」按钮。
- 看对话内容的 UI 测试一律带 `-openChatsFresh YES`（清掉磁盘上的对话缓存）：mock 每次重置，对话都从同一个 seq 重新开始，前面测试 churn 出来的步骤
  还在 App 的缓存里，占着这次 `/mock/say` 拿到的 seq，新回复「已经有了」就不显示，屏幕上是上一次的「工具 21 · 回复 5」。
  2026-11-02 `ChatFollowTests.testJarvisChatFollows` 在跑过 `ScrollUnderPushTests` / `ScreenTruthTests` 的模拟器上挂，看着像新外壳的 Persona 对话不显示回复，其实是测试环境；真实 hub 的 seq 是 transcript 里的位置，只增不重用。
- 测试按「滑一下停在哪」挑行的，别处改了行高或字号（#245 改了新外壳的字号档）就会滑到另一处，结果跟着变（2026-11-02，#339）：
  `testScrolledListHoldsStillThroughUpdates` 以前碰巧停在不换名次的空间，没测到读者那一屏被换掉；二分落在一个只改字号的提交上，真正的问题是 App 没保住读者那一行。
  二分找到的提交只说明「从这里开始暴露」，先看它改的东西能不能解释现象。找回一行用标题，别用整条无障碍标签（里面有「3 分钟前」，跨分钟就对不上）。
- 数 body 的测试（`RenderBudgetTests`）要等页面停下来再开始量（2026-11-03，#475）：启动后对话还在加载历史、排版，第一个量的推送（`rev`）
  就把这些气泡算到了自己头上，看着像「屏幕上什么都没变的推送重画了 6 个气泡」，换个顺序或多量一次就没了。现在先等计数 1 秒不变（`settled()`）。

## 要 macOS 自带命令的 Go 测试（2026-11-02）

- 信息连接器读 `chat.db` 用的是 macOS 的 `/usr/bin/sqlite3`（jarvisd 不带 cgo），hub 机器（Linux）上没有 `sqlite3`，那几个测试在 hub 上会跳过。
  开发 Mac 上没有 Go：在 hub 上 `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go test -c -o x.test ./internal/connector/imessage` 交叉编译测试二进制，
  拷回 Mac，**在包目录里**跑（`testdata` 是相对路径）。同样的办法用于 `node` 包里要 macOS 的测试。
- `sqlite3 -json` 没有行时什么都不打印（不是 `[]`），解析前先判空。

## Mac 版真窗口验收（2026-11-02，#403）

- 在没签名的模拟器构建上跑 JarvisTests（`CODE_SIGNING_ALLOWED=NO`）时，`NotificationServiceTests.opensWithTheKeyInTheSharedGroup` 一定失败：没签名就没有钥匙串组，
  `PushCrypto.ensureKey()` 返回 nil。master 上同样失败，看到它别当成自己改出来的。
- 验 显示 › 刷新（⌘R）要让 App 在前台、主窗口是 key window：没有 key window 时只按分区刷新 hub 数据（`MacViewCommands.refreshTarget`），
  用 cua 的 `invoke_menu` 在后台调，会看到收件箱 / 知识库「没刷」（假 hub 日志里只有 sessions、machines 那一组），其实是测法不对。
- cua 在这个 App 上：工具栏里的菜单按钮、sheet 窗口在后台用辅助功能点不了（AXPress 报 -25202，sheet 的树读不到），要 `bring_to_front` 后用桌面坐标点；
  `delivery_mode: "foreground"` 点完会切回原来的前台 App，刚弹出的菜单、浮层跟着关掉。sheet 用前台 Esc 关；消息操作条要真的右键（`rightMouseDown`），AXShowMenu 不行。
  触控板左右滑（List 的 swipeActions）cua 模拟不了：它只能发滚轮事件，不带手势阶段。
- cua 的坐标会按最近一次整屏截图的缩放换算：同一个会话里拍过一张缩小的整屏图（`max_image_dimension` 1280，屏幕 1920 宽），
  之后按原始坐标点就被放大了 1.5 倍，点到了墙纸。点墙纸会触发 macOS 的「显示桌面」，把所有窗口推到屏幕边上，再点一次墙纸才回来。
  整屏截图一律用原始分辨率（`max_image_dimension: 0`）；用坐标点之前，先确认它落在自己的窗口或弹出的菜单里。
- 「只在 mock 下坏」先拿体验模式对照，再去看 hub 的代码到底怎么走（#511）：两个假 hub 都可能不像 hub，而且可能一个比 hub 好、一个比 hub 坏。
  新建 Persona 对话：mock 原来根本不写第一句、不推会话列表（App 显示「还没有消息」、新对话不选中）；DemoHub 立刻写好第一句（一切正常）；
  hub 是先推会话列表，node 对还没写 transcript 的会话回空的 items，等 claude 写下第一行（watch 每 0.5 秒看一次）才推。所以真 hub 上也会闪一下「还没有消息」，
  这才是 App 该修的（第一句按「已发送」留在屏幕上）。当初写「体验模式也复现」，其实是副本钥匙串里留着 mock 的配对、`-demo.on YES` 没生效（见上面「钥匙串里留着以前配对过的 hub」）。
- cua 的 `tree_format: "elements"` 不列只有值、没有标签的静态文字（新对话页的乐观气泡、「Persona 正在准备…」都在里面），拿它判断「没显示」会误判；
  判断某段文字在不在，用 `get_window_state` 带 `query`（树里有 `AXStaticText = "…"`）或截图。
- cua 的 `type_text` 默认在后台用辅助功能插入文字（`AXSelectedText`）：主窗口工具栏的搜索框能筛选，sheet 里 `.searchable` 的框（新建对话 › 启动器）
  字显示出来了、列表却不筛，看着像 App 的 bug；用 `delivery_mode: "foreground"`（真按键）打同样的字就筛了（2026-11-03）。sheet 里打字一律前台投递，
  验收单里写明，否则执行者会报「不通过」。
- cua 里 sheet 自己的 window_id 读不到树（`ax_window_unresolved`），也不能对它定点投递；用主窗口的 window_id 读（sheet 在树里是 `AXSheet`），
  拿到框的 token 再 `type_text` 前台投递。前台投递完 cua 会把前台还给原来的 App，之后别往桌面（`scope: desktop`）发键，会落到别的 App 上。

## 逐像素比对（「只有一组时界面不变」，docs/testing.md「多 hub」）

- 同一个包连拍两次也不会完全一样：液态玻璃按钮、输入栏、卡片的边缘高光每次启动随机（同一次启动内不变），「正在工作」前的系统菊花一直在转，
  Reduce Motion 管不住它们。先用改动前的包连拍三四次当噪声，比较时排除落在噪声里的像素，剩下的差异逐张放大看（2026-11-02，#256；做法写在 testing.md「多 hub」）。
- 要比得稳：固定状态栏时间、冻结 mock 的时钟、每次卸载重装 App、mock 每次重启（相对时间一致），等画面停下再截（不要固定睡几秒：这台机器负载高时 7 秒还可能是白屏）。

- `scripts/test/test-server.sh` 只同步了 `server/`，#630 带进来的 `spec/fixtures` 测试（`TestWireSamples`、续传、resolution）读 `../../../spec` 和 `apple/` 的 Swift 源码，
  在 hub 上全量跑必挂（`open ../../../spec/fixtures/wire: no such file`），看着像自己的改动坏了。现在脚本连 `spec/` 和 `apple/**/*.swift` 一起同步（#642）；
  再有测试读 `server/` 以外的文件，同样要加进脚本（2026-11-03）。
