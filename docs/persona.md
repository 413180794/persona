# 形象（persona）

一个形象 = Jarvis 的**名字**、**头像**、Jarvis 顶栏里**会动的状态圆点**、一套**配色**。设置 › 形象里选，选了以后这几样一起换：
顶栏的脸和名字、侧边栏的卡片、资料页、输入框的「问 xxx…」，以及整个 App 的主色（按钮、链接、自己的气泡、选中态）。

- **默认形象 Jarvis**：App 自己画的面罩（`JarvisAvatar`），颜色跟随界面风格，不带配色。仓库里只有它。
- **其他形象**：放在 hub 机器的 `~/.jarvis/personas/<id>/`，手机选中时下载并缓存。
- 体验模式只有默认形象（`AppModel.persona`）。

## 为什么形象放 hub、不进仓库和 App 包

形象常常是网上找来的第三方角色（图上还带着别的平台的水印），而 TestFlight 是公开链接、仓库会给朋友看。所以：

- **不进仓库、不进 App**：这类角色的原图和生成出来的资源都不进仓库，也不打进 App 包。
- **仓库里放的**：协议、加载代码、生成脚本，以及原创的东西：默认形象和脚本现画的演示形象 `demo`（不存图片）。
- **生成时的工作目录**：要放在仓库外（或不提交的 `.lane/`）。
- **发版检查照常**：`check_bundle_privacy.py` 和隐私钩子都照常跑。
- **以后要改**：如果要把某个形象正式放进 App，先确认版权，再改这一节。

## 包格式（format 1）

```
~/.jarvis/personas/doudou/
  persona.json
  avatar/64.png  avatar/128.png  avatar/256.png  avatar/512.png
  dot/idle.png  dot/thinking.png  dot/working.png  dot/needs_you.png  dot/offline.png   # APNG
  preview.png    # 生成脚本给人看的总览，App 不用（hub 也照样能下）
```

```json
{
  "format": 1,
  "id": "doudou",
  "name": "豆豆",
  "subtitle": "圆滚滚的黄色小家伙",
  "avatar": { "sizes": { "64": "avatar/64.png", "128": "avatar/128.png", "256": "avatar/256.png", "512": "avatar/512.png" }, "safe": 1.0 },
  "dot": {
    "size": 144,
    "states": {
      "idle":      { "file": "dot/idle.png", "fps": 8, "frames": 24 },
      "thinking":  { "file": "dot/thinking.png", "fps": 8, "frames": 16 },
      "working":   { "file": "dot/working.png", "fps": 10, "frames": 12 },
      "needs_you": { "file": "dot/needs_you.png", "fps": 6, "frames": 12 },
      "offline":   { "file": "dot/offline.png", "fps": 4, "frames": 12 }
    }
  },
  "colors": {
    "light": { "primary": "#F2C230", "secondary": "#7FAE5A", "accent": "#94600A", "bubble": "#8A5800", "bubble_text": "#FFFFFF" },
    "dark":  { "primary": "#F2C230", "secondary": "#7FAE5A", "accent": "#B47A0C", "bubble": "#8F5C00", "bubble_text": "#FFFFFF" }
  }
}
```

| 字段 | 说明 |
| --- | --- |
| `format` | 1。hub 和 App 都不认别的值（以后不兼容地改格式就加一，旧 App 自然看不到新包） |
| `id` | `a-z0-9-`，最多 32 个字符，必须和目录同名；`jarvis` 留给默认形象 |
| `name` / `subtitle` | 顶栏、侧边栏、资料页、输入框里用 `name`；`subtitle` 只在设置里 |
| `avatar.sizes` | 边长（像素）→ 文件，正方形 PNG。App 按「显示点数 × 屏幕倍数」挑最小的够大的那张，裁成圆形显示。`safe` 是重要内容所在的圆占内切圆的比例（1 = 整个内切圆都安全；脚本出的图本身就是圆的） |
| `dot.size` | 帧的边长（像素）。顶栏显示 46 pt，144 px 正好是 48 pt @3x |
| `dot.states` | 五个状态各一个 APNG（只有一帧时就是普通 PNG）；必须有 `idle`，缺的状态 App 用 `idle` 的。`fps` 在文件里没写每帧时长时才用；`frames` 只是给人看的 |
| `colors` | 每个外观一套 `#RRGGBB`，见下面「色系」。整块可以省略（只换脸不换颜色） |

**检查规则**

- **hub 端**（`server/internal/hub/persona.go`）：对不上的包不出现在列表里，原因写进日志。会检查：
  - 文件名只能是包里的普通相对路径，不能用 `..`，不能有点开头的部分，不跟软链；
  - 单个文件 ≤ 4 MB，整个包 ≤ 16 MB；
  - 必须有 `idle`；
  - 状态名只能是这五个。
- **App 端**（`PersonaManifest.problem`）：再查一遍同样的规则。
- **脚本**：`make_persona.py check` 把这些规则和对比度底线一起查。

## 状态（顶栏的圆点）

| 状态 | `dot.states` 键 | 什么时候（`AssistantTopBar.dotState`） | 生成脚本的动画 |
| --- | --- | --- | --- |
| 空闲 | `idle` | 默认 | 呼吸（轻微缩放、上下），每圈末尾眨一次眼 |
| 思考中 | `thinking` | 对话在跑，还没写字也没调工具：最后一条是用户消息或 thinking | 托腮抬眼、轻轻摇摆，右上角的思考气泡里三个点轮流亮 |
| 工作中 | `working` | 对话在跑，已经在写或在调工具 | 两拍动作交替加上下颠，主色的弧线沿边转圈 |
| 等你处理 | `needs_you` | 这条对话有待处理的交互（提问、批准） | 两拍挥手，外圈一圈「需要你」的红色在呼吸 |
| 离线 | `offline` | hub 连不上，或者 Jarvis 所在的机器离线 | 睡着、整体褪成灰色，右上角飘 z |

动画上的约定：

- **第 0 帧单独就要能看出是哪个状态**。系统开了「减弱动态效果」或低电量模式时，只显示第 0 帧，不动。
- 帧率和帧数要克制：4–10 fps，12–24 帧一圈。
- 设计取舍（2026-10-31）：状态色（需要你的红、工作中的弧线）是画进帧里的，App 不再往上叠东西。这样一个形象的五个状态完全由包自己决定，接新形象不用改代码。

## 为什么是 APNG + UIImageView

| 方案 | 取舍 |
| --- | --- |
| Lottie | 要矢量动画和一个第三方库，AI 生成的是位图，转不过去 |
| GIF | 没有半透明，只有 256 色，圆形边缘发毛 |
| 视频（HEVC 带透明） | 播放器重，小尺寸多实例不划算，也难生成 |
| SwiftUI 每帧重画 | 圆点常驻顶栏，每帧跑布局和绘制费电 |
| 精灵图（一张大图） | 可以，但不是标准格式，打开文件看不出动画 |
| **APNG**（采用） | ImageIO 原生解码，带半透明，浏览器 / 访达里直接能看动画，Pillow 能写 |

播放用 `UIImageView.animationImages`（`PersonaFramesView`）：

- 帧切换由 Core Animation 在渲染服务里做，App 每帧不跑任何代码。
- 视图离开窗口就停。
- 只解码当前用到的状态，最多缓存 6 个状态（`PersonaStore.frames`）。

一个状态 144 px × 24 帧，解码后约 2 MB；整个包约 1–2 MB。

默认形象不走位图，用 `JarvisAvatar` 画同样的五个状态：

- 空闲每 3.6 秒眨一次眼：用 periodic 的 TimelineView 加一个短的 phase 动画，两次眨眼之间不重画。
- 思考、工作、等你处理三个状态以 15 fps 动。
- 离线是灰色、不动。

## 色系 → 设计 token

`colors` 不直接拿来用，而是叠在当前界面风格的 spec 上（`StyleSpec.tinted`）。所以各处照常读 token，形象一换，所有读 token 的地方跟着换：

| persona.json | token | 用在哪里 |
| --- | --- | --- |
| `accent` | `StyleSpec.accent` → `Theme.accent` | 能点的东西：按钮、链接、选中态、Tab 的 tint |
|  | BeautifulUI 的 `accent` / `accentTint` |  |
|  | 飞书页面的链接和「我的气泡」 | 「飞书」风格自己保留飞书蓝 |
| `bubble` / `bubble_text` | `StyleSpec.userBubble` / `userBubbleText` | 自己发的消息 |
| `primary` | `StyleSpec.personaPrimary` → `Theme.persona` | 只用来装饰：头像的光晕、设置里卡片的底色。没有时等于 accent |
| `secondary` | 无 | 只在生成时用：圆点背景的淡色 |

**对比度底线**：和每个界面风格一样（`StyleSpecTests`）。包里的颜色没达到时，App 把它往黑（或往白）调到达标为止（`PersonaPalette.meeting`），所以包没法让按钮上的字看不清。

- 主色上的白字 ≥ 3.5:1；
- 主色当链接时，对页面背景 ≥ 3:1（浅色对白、深色对黑）；
- 气泡上的字 ≥ 4.5:1。

生成脚本取色的方法：

- 主色取角色身上面积最大的色相（`primary`），副色取离它至少 40° 的下一个色相（`secondary`）。
- `accent` 和 `bubble` 用主色的色相，压暗到刚好过底线。
- 黄色这种白字读不了的颜色，会变成蜂蜜色 / 琥珀色。
- 想要别的颜色就用 `--accent-light` / `--accent-dark` 手动给。

**换形象等于换一次风格**：RootView 按 `AppModel.look`（风格 + 形象配色）重建整棵视图树。打开的 Tab 和对话会记住，但推出来的页面会被关掉。所以选形象的界面放在设置列表里（`PersonaPicker`，和界面风格一样），而不是推出去的单独页面。

## 手机上（`PersonaStore`）

- **缓存目录**：`Application Support/Personas/<id>/<rev>/`。下载时先写到旁边的 `.<rev>.part`，全部下完再整个挪过去，所以不会用到半个包。`index.json` 是上次的列表。
- **启动**：直接从磁盘显示选中的形象，不等 hub。
- **刷新列表**：每次连上 hub 和打开设置时各刷新一次。rev 变了就下载新的，再删掉没人用的旧 rev。
- **hub 上没有了**：用默认形象，但保留用户的选择，包回来了就恢复。
- **旧 hub**（没有这个接口，404）：设置里提示「这个 hub 还不能放形象：更新 hub 后再看」。

## 接入一个新形象

1. **准备 1–3 张角色的图**：全身、头部特写、一个有特点的动作。放在仓库外，或者 `.lane/` 里。
2. **生成姿势**：用 Codex 自己的登录，不需要 API key。

   ```sh
   python3 scripts/persona/make_persona.py generate --work ~/personas/doudou \
     --describe "a round yellow 3D character with green-ringed eyes" a.jpg b.jpg c.jpg
   ```

   会画出 8 张透明背景的 PNG 到 `<work>/poses/`：
   - `avatar`、`blink`（眨眼，由 avatar 改出来）；
   - `think`；
   - `work_a` / `work_b`（同一个动作的两拍）；
   - `wave_a` / `wave_b`（挥手的两拍）；
   - `sleep`。

3. **看一遍 `poses/`**：不满意的用 `--only think,sleep` 重画。
4. **打包**（不联网）：

   ```sh
   python3 scripts/persona/make_persona.py build --work ~/personas/doudou --id doudou --name 豆豆 --subtitle "圆滚滚的黄色小家伙"
   ```

   - 包在 `<work>/package/doudou/`，先看 `preview.png`：五个状态的帧、浅色和深色下的颜色。
   - 位置不对时，在 `<work>/framing.json` 里按姿势微调，例如 `{"think": {"scale": 1.05, "dx": 0.02, "dy": -0.01}}`（单位是边长的比例）。
   - 颜色不对就用 `--primary`、`--accent-light`、`--accent-dark`。
5. **装到 hub**：`python3 scripts/persona/make_persona.py install ~/personas/doudou/package/doudou`。会先解到旁边，再整个换上去；hub 机器默认读 `~/.config/jarvis/config.json`。
6. **手机上选**：设置 › 形象。

一步做完生成和打包：`make_persona.py new --id doudou --name 豆豆 a.jpg b.jpg c.jpg`。不用 AI 也行：把自己画的 8 张透明 PNG 按上面的名字放进 `<work>/poses/`，直接 `build`。只有 `avatar` 是必须的，缺的姿势用它代替。

脚本第一次运行会把 Pillow、numpy 装进 `~/.cache/jarvis/persona-venv`。

## 用 Codex 生图（2026-10-31 实测，codex-cli 0.153）

- **生图工具**：`codex exec` 里的内置 `image_gen` 工具（feature `image_generation`，stable）。
  - 用的是 Codex 自己的 ChatGPT 登录，不用 `OPENAI_API_KEY`，也不碰别的网关的 key。
  - 生成的图在 `$CODEX_HOME/generated_images/<thread>/`。提示词里让它每张图生成后立刻拷进工作目录，并按名字存。
- **透明背景**：提示词里要「fully transparent background」就能得到真的 RGBA，1254×1254，约一半像素透明，没有水印。脚本会检查透明比例。
- **一致性**：
  - 参考图都用 `-i` 传进去。`-i` 吃多个值，所以提示词要从 stdin 给。
  - 眨眼、动作的第二拍、挥手的第二拍让它在第一张的基础上改（edit），不是重新画，这样两帧对得上。
  - 8 张在一个 `exec` 里画完，前面的图都在上下文里。
- **模型**：ChatGPT 登录只能用一部分模型，用户配置里的模型会报「not supported when using Codex with a ChatGPT account」。`--model` 换一个能用的（当时 `gpt-6-astra` 可以）。
- **网络**：
  - 要能连上 chatgpt.com。
  - 对话请求认 `socks5h://` 代理，但 `image_gen` 和刷新模型列表用的 HTTP 客户端不认，报「connection failed: error sending request」。
  - 要给 HTTP CONNECT 代理：`--proxy http://host:port`。
- **耗时**：一张约 2–3 分钟，8 张二十分钟左右；几个形象可以同时跑（2026-10-31 四个并行，32 张 6 分钟画完）。
  - 慢得多（一张十分钟以上、几个并行就全部停住）时，先量隧道的吞吐：`curl -x <代理> -o /dev/null -w '%{speed_download}' <大文件>`。
    每张图约 1.6 MB，每次请求还要带上参考图和上下文；出口只有十几 KB/s 时（2026-10-31 hub 机器的外网一度只有 11 KB/s）就会这样。
    换一台外网快、连得上 chatgpt.com 的机器开 `ssh -D`，CONNECT 代理不用改。

## 测试

| 测试 | 内容 |
| --- | --- |
| `server/internal/hub/persona_test.go` | 只列出完整的包；rev 跟着内容变；文件不出包目录；没有令牌拿不到 |
| `apple/JarvisTests/PersonaTests.swift` | 解析 persona.json；hub 会拒绝的包 App 也拒绝；颜色被拉到底线；叠在风格上（飞书风格保留飞书蓝）；下载整包、按 rev 换新删旧；下载失败什么都不变；APNG 解帧 |
| `apple/JarvisUITests/PersonaPickerTests.swift` | 起 `MOCK_PERSONAS=demo python3 scripts/mock_hub.py`：设置里选 hub 上的形象，顶栏换成它的名字；选回 Jarvis 又变回来 |

截图和录屏用的 DEBUG 启动参数：

- `-openPersona <id>`：启动时选这个形象（包要已经在手机上，或者随列表下下来）。
- `-showPersonaState idle|thinking|working|needs_you|offline`：顶栏固定在一个状态。

启动参数的名字照 CLAUDE.md 的约定用 `open…` / `show…` 开头，并且加进了 `release.sh` 的列表。
- `-tab assistant -openAssistantPage avatars`：所有形象的头像和五个状态，浅色、深色各一份。
