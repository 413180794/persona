# 在 Mac 上构建、安装 Persona Link

Persona Link 是 Mac 菜单栏里的一个小 App：把这台 Mac 接到你的 Persona hub 上，hub 就能经它用上这台电脑（看、驱动这台上的 Claude Code / Codex 等）。
它里面带着连接器 jarvis-link 和 personad，第一次打开会顺手装好本机常驻的 node 服务。原理、两种连接模式和协议见 [link.md](link.md)，这里只讲怎么装起来。

还没有发布好的安装包，目前都从源码构建，几分钟就好。

## 准备

| 要什么 | 说明 |
| --- | --- |
| macOS 15 或更新 | App 的最低系统版本 |
| Xcode（完整版，不是只有命令行工具） | 用 `xcodebuild` 编 Swift package；装好后 `sudo xcode-select -s /Applications/Xcode.app` 并打开一次 Xcode 同意许可 |
| Go | 编 jarvis-link 和 personad。版本按 `server/go.mod`；本机的 Go 旧一点也行，没设 `GOTOOLCHAIN=local` 时 Go 会自己下载需要的版本 |
| 一台在跑的 hub | 并且开着 TLS 端口（hub.json 的 `public.tls_listen`）；你能打开它的管理界面，或者能登录 hub 机器跑命令 |

不需要 Apple 开发者账号：App 用本机的 ad-hoc 签名，只在构建它的这台 Mac 上运行。

## 构建并安装

```bash
git clone <仓库地址> persona && cd persona
./scripts/app/mac-link.sh install
```

这一条会：编 App（Apple 芯片 / Intel 跟随本机）、编 jarvis-link 和 personad 放进 App、签名、装到 `/Applications/Jarvis Link.app`（为了兼容旧版本，文件名还叫 Jarvis Link，打开后显示的是 Persona Link），然后打开它。
已经在跑的旧版本会先被退出、替换掉。

- 只编不装：`./scripts/app/mac-link.sh build`，产物在 `apple/PersonaLink/build/Jarvis Link.app`。
- 编一个 Apple 芯片和 Intel 都能跑的：`UNIVERSAL=1 ./scripts/app/mac-link.sh build`。
- bundle id 默认是占位的 `com.example.jarvis.link`，自己用不用改；想换就在 `~/.config/persona/config.json` 里写 `mac.link_bundle_id`
  （或设环境变量 `PERSONA_LINK_BUNDLE_ID`）。换了 bundle id 等于换了一个 App，「登录时启动」要重新勾。

打开后菜单栏右上角出现一个图标，没有 Dock 图标。第一次打开时：

- 它会把 App 里的 personad 复制到 `~/.persona/bin/`（已经有了就不动），并装一个 LaunchAgent `com.jarvis.node` 让本机的 node 常驻；
- 第一次连内网地址时，系统会问「允许 Persona Link 查找本地网络上的设备」，选允许。点了拒绝的话，到 系统设置 › 隐私与安全性 › 本地网络 里打开。

## 配对

推荐「我去连 hub（主动）」：这台 Mac 去连 hub，不用开端口，换网络也会自己重连。

1. 在 hub 上给这台电脑生成配对链接，两种做法任选：
   - hub 管理界面 › 电脑 › 添加电脑 › **电脑来连**：填这台的 id（如 `macbook`）和名字，生成链接；
   - 或者在 hub 机器上：`personad hub pair --machine macbook`（hub 是 Docker 部署的话在容器里跑这条，见 [docker.md](docker.md)）。
2. 点菜单栏图标 › **配对…**，把链接粘贴进去，点配对。
3. 好了的样子：图标变成已连接，菜单里写着「本机 node 在线」「这台是 hub 上的「macbook」」；手机 App 的机器列表里这台在线。
4. 想开机就连上：菜单里勾「登录时启动」。

链接 10 分钟内有效、只能用一次，过期了重新生成。它在用掉之前等于配对权限，别贴到群里。

这台 hub 连得到、但它连不到 hub 的电脑（比如 hub 在别人的网络里，只能经代理进来），用「让 hub 来连（被动）」：配对窗口里选这一项，
点「生成配对链接」，把链接粘贴到 hub 管理界面 › 电脑 › 添加电脑 › **hub 来连**。防火墙只对 hub 放行它监听的端口（默认 18663）。细节见 [link.md](link.md)「被动模式」。

也可以不用界面，在终端里配对：

```bash
echo '<配对链接>' | "/Applications/Jarvis Link.app/Contents/Helpers/jarvis-link" pair -
```

## 菜单里的东西

| 写着 | 意思 |
| --- | --- |
| 内网 / 公网 IPv6 / 域名 · 地址 | 现在走的是哪条路连到 hub |
| hub「…」· 互联组共 N 台 hub | 连着哪台 hub（旧 hub 不发名字时没有这行） |
| 本机 node 在线 | 一切正常 |
| 链路是通的，但 hub 连不上本机的 node | 常驻 node 没在跑，见下面「常见问题」 |
| 未连接 · N 秒后重试 | 每个候选地址为什么连不上，一行一个；它会自己重试，也可以点「立即重连」 |
| hub 不认这台的凭据 | 这台在 hub 上被删了或凭据撤销了：重新配对 |

图标的几种样子见 [link.md](link.md)「菜单栏 App」。

## 升级

拉最新代码再装一次就行，配对和设置都在 `~/.persona/link.json`，不会丢：

```bash
git pull && ./scripts/app/mac-link.sh install
```

升级 App 不会替换常驻 node 用的那份 personad。支持更新协议的 node 可以直接在 Persona 的机器页点「重新部署」，沿用现有连接更新，不必重装菜单栏 App。

旧 node 首次需要在本机升级一次。先查看真实服务的二进制路径和状态：

```bash
"/Applications/Jarvis Link.app/Contents/Helpers/jarvis-link" node status --json
```

按原安装 / 部署渠道替换 `bin` 指向的服务二进制并重启 `com.jarvis.node`，保留已有运行根、启动参数和配对。不要先卸载再查询路径，也不要把升级 App 当成常驻 node 已升级。首次迁移和失败恢复的完整说明见 [node-update.md](node-update.md)。

## 卸载

1. 不想要常驻 node 了，先停掉、删掉它的服务（要在删 App 之前跑）：`"/Applications/Jarvis Link.app/Contents/Helpers/jarvis-link" node uninstall`。
2. 菜单 › 退出 Persona Link；系统设置 › 通用 › 登录项 里有它的话去掉。
3. 删 App：`rm -rf "/Applications/Jarvis Link.app"`。
4. 配对凭据在 `~/.persona/link.json`，日志在 `~/.persona/link.log`，personad 在 `~/.persona/bin/`，不要了就删。hub 那边在管理界面 › 电脑 里把这台删掉。

## 常见问题

**构建报 `xcodebuild: error: SDK … cannot be located` 或找不到开发者目录**：系统指向的是命令行工具，不是 Xcode。
`sudo xcode-select -s /Applications/Xcode.app`，或者临时 `export DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer`。

**构建报 Go 版本太旧、下载 toolchain 失败**：装一个不低于 `server/go.mod` 里写的 Go，或者让 `go` 能访问网络自动下载。

**打开时系统说「无法验证开发者」**：从别处拷来的 App 才会这样（ad-hoc 签名只在构建的那台机器上被信任）。在要用的那台 Mac 上自己构建；
实在要拷，右键 › 打开，或 `xattr -dr com.apple.quarantine "/Applications/Jarvis Link.app"`。

**一直「未连接」，内网那行写着不可达**：多半是本地网络权限被拒了，见上面「第一次打开时」；在外面时内网连不上是正常的，会走公网 IPv6 或域名。

**「链路是通的，但 hub 连不上本机的 node」**：`"/Applications/Jarvis Link.app/Contents/Helpers/jarvis-link" node status` 看常驻 node 的服务装没装、
launchd 有没有加载它；日志在 `~/.persona/node-attach.log`。没装就在菜单里点「安装本机 node 服务」。

**菜单栏面板是直角、带一圈细边框的老样子**：App 是用旧脚本编的。拉最新代码重新 `install`（原因见 [lessons/macos.md](lessons/macos.md)「Persona Link 的面板是老样子」）。

**看日志**：`tail -f ~/.persona/link.log`。用过旧版（Jarvis）的机器上，这些文件可能还在 `~/.jarvis/` 下（[config.md](config.md)「命名与兼容」）。
