import Foundation

extension Theme {
    /// SF Symbols by meaning: one meaning, one symbol. Views write `Image(systemName: Theme.Icon.copy)`, not the name.
    /// A symbol that two files use belongs here (`IconVocabularyTests`); one used in a single place may stay a literal
    /// until a second place needs it. A filled form is its own entry where both forms are in use.
    nonisolated enum Icon {
        // MARK: State

        /// Something failed or needs fixing: tool errors, API errors, a failed send, a failed run.
        static let error = "exclamationmark.triangle"
        static let errorFilled = "exclamationmark.triangle.fill"
        /// The highest alert level, above `error` (a machine's critical alert).
        static let critical = "exclamationmark.octagon.fill"
        static let success = "checkmark.circle"
        static let successFilled = "checkmark.circle.fill"
        static let done = "checkmark"
        /// Many items filed at once (收件箱 › 全部标为已处理).
        static let doneAll = "checkmark.rectangle.stack"
        /// Many items set aside at once without handling them (收件箱 › 全部忽略).
        static let dismissAll = "rectangle.stack.badge.minus"
        static let info = "info.circle"
        static let infoFilled = "info.circle.fill"
        static let offline = "wifi.exclamationmark"
        static let needsYou = "exclamationmark.bubble.fill"
        /// A status dot (online, unread).
        static let dot = "circle.fill"
        static let time = "clock"
        /// Agents grouped or ordered by their state (the Agents list's 按状态 / 状态优先).
        static let byState = "circle.grid.2x1"
        static let muted = "bell.slash.fill"
        static let notifications = "bell.badge"
        /// An Agent at work (the fleet's working count).
        static let working = "gearshape.fill"
        /// Calls moved to the background.
        static let backgroundTask = "gearshape.2"
        static let backgroundTaskFilled = "gearshape.2.fill"
        /// A multi-agent workflow running in the background (Claude Code's Workflow tool).
        static let workflow = "flowchart"
        static let pinned = "pin.fill"
        static let unchecked = "circle"
        static let uncheckedBox = "square"
        static let todo = "checklist"
        /// Recent, or waiting to come back (reconnecting, last used).
        static let recent = "clock.arrow.circlepath"
        static let inProgress = "arrow.triangle.2.circlepath"
        /// A goal the agent works toward until it holds (Claude Code's /goal, a Codex thread goal).
        static let goal = "target"
        static let recurring = "repeat"
        static let schedule = "alarm"
        /// The session triage: a stopped agent read to say whether it is done or what it waits for (设置 › 会话分诊).
        static let triage = "text.badge.checkmark"
        /// Persona woke a waiting agent up (a message it typed in for the model, `Injected`).
        static let wakeUp = "bell.and.waves.left.and.right"
        /// A task that waits for the user's answer (its notice in the main conversation, `Injected`).
        static let waitsForYou = "hand.raised.fill"
        /// Asks before acting (the manual permission mode), or needs someone to act.
        static let askFirst = "hand.raised"
        /// bypassPermissions: nothing is asked.
        static let allowEverything = "exclamationmark.shield"
        static let permissions = "lock.shield"
        /// The two little arrows after a value that opens a menu of choices.
        static let choices = "chevron.up.chevron.down"
        static let effort = "gauge.with.dots.needle.50percent"
        static let missingFile = "doc.questionmark"

        // MARK: Actions

        static let copy = "doc.on.doc"
        static let paste = "doc.on.clipboard"
        static let close = "xmark"
        /// Clear a field, dismiss a sheet, remove an attachment.
        static let clear = "xmark.circle.fill"
        /// End something that keeps running: close an Agent, forget the server.
        static let end = "xmark.circle"
        static let more = "ellipsis"
        static let moreMenu = "ellipsis.circle"
        static let delete = "trash"
        static let edit = "pencil"
        /// Draw on a picture (the image viewer's 标注, into the composer).
        static let markup = "pencil.tip.crop.circle"
        static let compose = "square.and.pencil"
        /// Speech to text in the composer (a tap starts, a second one ends).
        static let dictate = "mic"
        static let dictating = "mic.fill"
        /// 长录音作为上下文，与输入框听写区分。
        static let recording = "waveform.circle"
        static let pause = "pause.fill"
        /// Back and ahead 15 seconds in something that plays (an audio file).
        static let skipBack = "gobackward.15"
        static let skipForward = "goforward.15"
        static let share = "square.and.arrow.up"
        static let save = "square.and.arrow.down"
        static let down = "arrow.down"
        static let gitBranch = "arrow.triangle.branch"
        /// The spaces a host nests under another (Orca's child worktrees,「N 个子项」, #3075).
        static let childSpaces = "square.3.layers.3d.down.right"
        /// 分支到新聊天: a new conversation from a reply (#2137).
        static let forkChat = "arrow.branch"
        /// 回退到这里: back to before one of your messages in a Claude session (#2915).
        static let rewindChat = "clock.arrow.circlepath"
        static let gitUpstream = "cloud"
        static let gitNoUpstream = "icloud.slash"
        /// Git 拉取 / 推送 / 获取 in the node menu (docs/terminal.md「节点菜单」).
        static let gitPull = "arrow.down.to.line.compact"
        static let gitPush = "arrow.up.to.line.compact"
        static let gitFetch = "icloud.and.arrow.down"
        /// Split a terminal pane to the right / below (the node menu's 拆分窗格).
        static let splitRight = "rectangle.split.2x1"
        static let splitDown = "rectangle.split.1x2"
        /// Close a whole terminal tab, every pane in it.
        static let closeTab = "xmark.rectangle"
        /// 新建 worktree…: another checkout of a workspace's repository (docs/terminal.md「新建 worktree…」).
        static let newWorktree = "plus.rectangle.on.folder"
        static let retry = "arrow.clockwise"
        /// Put a computer's own Persona files right (机器详情 › 这台电脑能做什么 › 修复).
        static let repair = "wrench.adjustable"
        /// Loads the in-app page again (the link sheet's header).
        static let reload = "arrow.clockwise"
        static let undo = "arrow.uturn.backward"
        static let interrupt = "stop.fill"
        static let stopLoop = "stop.circle"
        static let run = "play.fill"
        static let add = "plus"
        static let send = "arrow.up"
        /// The round send button beside a composer.
        static let submit = "arrow.up.circle.fill"
        static let sendMail = "paperplane.fill"
        static let reply = "arrowshape.turn.up.left"
        /// 表情回复: pick an emoji to react with (飞书).
        static let react = "face.smiling"
        /// Leave the app for another one (飞书).
        static let openExternal = "arrow.up.forward.app"
        /// Translate, and what was translated.
        static let translate = "translate"
        /// 设置 › 外观 › 形象.
        static let persona = "person.crop.artframe"
        /// 设置 › 外观 › 界面风格.
        static let style = "paintpalette"
        /// 设置 › 外观 › 主题色.
        static let themeColor = "swatchpalette"
        /// 设置 › 外观 › 布局 (the shell or the old tabs).
        static let layout = "rectangle.topthird.inset.filled"
        /// 设置 › 外观 › 深浅色, and its 跟随系统.
        static let colorScheme = "circle.lefthalf.filled"
        /// 深浅色 › 浅色.
        static let lightMode = "sun.max"
        /// 深浅色 › 深色.
        static let darkMode = "moon"
        /// Open the thing the row points at (a sub-Agent, a card's link).
        static let open = "arrow.up.right"
        /// Open a file or a document in its viewer.
        static let openDocument = "arrow.up.forward.square"
        /// What follows from something before it: a follow-up, the next row of a table.
        static let followOn = "arrow.turn.down.right"
        static let enlarge = "arrow.up.left.and.arrow.down.right"
        /// The text size setting.
        static let textSize = "textformat.size"
        static let upDown = "arrow.up.arrow.down"
        static let select = "selection.pin.in.out"
        static let insert = "text.cursor"
        static let view = "eye"
        /// Hide from a list (it stays where it is, out of sight).
        static let hide = "eye.slash"
        /// Try something out for real (测试可用性: a launcher answers one question on each machine).
        static let tryOut = "bolt.horizontal.circle"
        static let options = "slider.horizontal.3"
        /// A list filter menu: off, and on (something filtered).
        static let filter = "line.3.horizontal.decrease.circle"
        static let filterActive = "line.3.horizontal.decrease.circle.fill"
        /// A plain list layout.
        static let listView = "list.bullet"
        static let settings = "gearshape"
        static let enter = "return"
        static let link = "link"
        static let id = "number"
        static let pin = "pin"
        static let unpin = "pin.slash"
        /// Archive a conversation (out of the list, kept under 已归档) and bring it back.
        static let archive = "archivebox"
        static let unarchive = "tray.and.arrow.up"
        static let mute = "bell.slash"
        static let unmute = "bell"
        static let collapseAll = "rectangle.compress.vertical"
        static let newConversation = "plus.bubble"
        /// Compact a conversation's context on demand (压缩上下文, #1727).
        static let compactContext = "arrow.down.and.line.horizontal.and.arrow.up"
        static let keyboard = "keyboard"
        /// Puts the keyboard away (the chat's composer, beside the mode chips).
        static let dismissKeyboard = "keyboard.chevron.compact.down"

        // MARK: Navigation

        static let disclosure = "chevron.right"
        static let back = "chevron.left"
        /// Forward in a page's history (the link panel's web view).
        static let forward = "chevron.forward"
        static let expand = "chevron.down"
        static let collapse = "chevron.up"

        // MARK: Things

        /// Persona itself (the brand's four-point star).
        static let jarvis = "sparkle"
        /// AI does it: summaries, drafts, AI edits, hand to Persona.
        static let ai = "sparkles"
        /// The Agents tab and what stands for it.
        static let agents = "chevron.left.forwardslash.chevron.right"
        /// Source code (a code file, a doc's code block, viewing a file's source).
        static let code = "curlybraces"
        static let terminal = "terminal"
        static let terminalFilled = "terminal.fill"
        /// The model or launcher an Agent runs with.
        static let model = "cube"
        static let cpu = "cpu"
        static let context = "gauge.with.dots.needle.33percent"
        /// A piece of context a message carries (a page excerpt, a part of a Feishu /agent request's context).
        static let quote = "text.quote"
        /// The context a message carries, folded into one row.
        static let contexts = "rectangle.stack"
        static let space = "square.stack.3d.up"
        static let spaceFilled = "square.stack.3d.up.fill"
        static let conversation = "text.bubble"
        static let file = "doc"
        static let document = "doc.text"
        /// A spreadsheet (Feishu 表格).
        static let sheet = "tablecells"
        /// A slide deck (PowerPoint, Keynote).
        static let slides = "rectangle.on.rectangle.angled"
        /// A database table (Feishu 多维表格).
        static let base = "square.grid.3x2"
        /// A 公司圈 (Feishu Moments) post in the inbox: the company's public feed.
        static let moments = "rectangle.3.group.bubble"
        static let folder = "folder"
        /// Home: 回到主对话 (the main conversation), and a repository's main checkout in the Agents list's 按仓库 (as Orca
        /// and the web mark it).
        static let home = "house"
        static let folderFilled = "folder.fill"
        static let photo = "photo"
        static let photos = "photo.on.rectangle"
        static let camera = "camera"
        static let list = "list.bullet.rectangle"
        static let text = "text.alignleft"
        static let machines = "macbook.and.iphone"
        /// 个人 Agent: the Lark gateway's personal agents page (the Agents list's toolbar).
        static let personalAgents = "person.crop.circle.badge.checkmark"
        /// A computer whose kind is not known.
        static let machine = "desktopcomputer"
        static let laptop = "laptopcomputer"
        /// A Mac's live screen (VNC, docs/computer-use.md「实时画面」).
        static let screen = "display"
        /// The Mac's pointer drawn over its screen in the app's VNC (#1343), when the Mac sends none of its own.
        static let pointer = "cursorarrow"
        /// VNC in a small window over everything (picture in picture).
        static let pictureInPicture = "pip.enter"
        /// The small window's own buttons, named on its card (VNCPiPCard): back to the app, and ▶ (connect again).
        static let pictureInPictureExit = "pip.exit"
        static let pictureInPicturePlay = "play.fill"
        /// 触控板模式: the screen moves the pointer like a trackpad.
        static let trackpad = "rectangle.and.hand.point.up.left"
        /// 指针居中: the pointer back to the middle.
        static let pointerCenter = "scope"
        /// The Mac's clipboard and the phone's, both ways.
        static let clipboard = "clipboard"
        /// How to use it (帮助).
        static let help = "questionmark.circle"
        /// 我完成了: the Mac handed back to the agent.
        static let handBack = "hand.thumbsup"
        /// Signing in with a code entered on a web page instead of a computer's login (GitHub's device flow).
        static let deviceCode = "key"
        /// A website whose sign-in (cookies) a browser keeps (a Mac's 浏览器登录, #1304).
        static let siteLogin = "lock"
        /// This Mac out of the hub's reach (the Persona page's device chip on the Mac).
        static let laptopOffline = "laptopcomputer.slash"
        static let server = "server.rack"
        /// A group of hubs that know each other (设置 › hub, the group filter and labels of the merged lists;
        /// docs/multi-hub.md).
        static let hubGroup = "network"
        /// A hub reached through an SSH tunnel (设置 › hub › SSH 隧道, docs/network.md「经 SSH 隧道连 hub」).
        static let sshTunnel = "network.badge.shield.half.filled"
        /// This device's own SSH key (its public half, to copy onto the server).
        static let sshKey = "key.horizontal"
        /// The SSH server's key fingerprint, confirmed and pinned.
        static let hostKeyTrusted = "touchid"
        /// The hub has a newer version, or is updating itself (docs/hub-update.md).
        static let hubUpdate = "arrow.down.to.line.circle"
        /// The assistant's skills (~/.jarvis/skills).
        static let skills = "puzzlepiece.extension"
        /// A plugin of the hub's (设置 › 扩展, docs/extensions.md).
        static let plugins = "powerplug"
        /// 扩展市场: the markets' catalog of plugins to install.
        static let pluginMarket = "storefront"
        /// A plugin installed from a market / upgraded to its newer version.
        static let install = "arrow.down.app"
        static let upgrade = "arrow.up.square"
        /// A page a plugin draws (its declarative UI).
        static let pluginPage = "chart.bar.doc.horizontal"
        /// A plugin's main-navigation entry by the `icon` it asks for (docs/extensions.md「UI 插槽」): one meaning each,
        /// anything else is `plugins`.
        static let pluginChart = "chart.bar"
        static let pluginGauge = "gauge.with.needle"
        static let pluginList = "list.bullet"
        static let pluginBell = "bell"
        static let pluginDoc = "doc.text"
        static let pluginCalendar = "calendar"
        static let pluginMoney = "creditcard"
        static let pluginStar = "star"
        static let phone = "iphone"
        static let watch = "applewatch"
        static let handoff = "iphone.and.arrow.forward"
        /// The default web browser (open a link outside the app).
        static let safari = "safari"
        /// A web page a machine serves on one of its ports (an agent's dev server, docs/ports.md).
        static let servedPage = "globe"
        /// Something opened in a window of its own (Mac).
        static let newWindow = "macwindow.badge.plus"
        /// The Mac code viewer's find bar: the previous / next match.
        static let findPrevious = collapse
        static let findNext = expand
        /// Picked text or a page going along with the next message (作为上下文).
        static let addContext = "text.badge.plus"
        static let person = "person.crop.circle"
        /// The user's own messages.
        static let user = "person.fill"
        static let inbox = "tray.full"
        /// The conversations Persona opened by itself (收件箱's AI 分身, scheduled tasks, 飞书 /agent…): the sidebar's
        /// 自动对话 (#3266).
        static let autoConversations = "sparkles.rectangle.stack"
        static let mail = "envelope"
        static let mailFilled = "envelope.fill"
        /// Feishu's chats as a source (the inbox's 飞书 filter, the knowledge base's 会话).
        static let feishuChats = "bubble.left.and.bubble.right"
        /// The mail accounts (管理邮箱).
        static let mailAccounts = "envelope.badge"
        /// A chat app's bot the user talks to Persona through (设置 › 连接器 › 飞书机器人).
        static let chatBot = "message.badge.waveform"
        /// 设置 › 渠道: the ways of talking to Persona (docs/channels.md).
        static let channels = "ellipsis.bubble"
        /// A voice channel (talking to Persona out loud), and a chat's way into a voice conversation (语音对话).
        static let voiceChannel = "waveform.and.mic"
        /// The voice conversation's bar (docs/voice.md「界面」). 静音: turns the call's microphone off; muted, the button
        /// shows `voiceUnmute` (取消静音), the same mic filled like a toggle that is on, as iOS 电话's 静音. The muted
        /// orb wears `voiceMute`.
        static let voiceMute = "mic.slash"
        static let voiceUnmute = "mic.slash.fill"
        /// 结束: hangs the voice conversation up (in `Theme.danger`).
        static let voiceEnd = "phone.down.fill"
        /// 字幕: what was said in the voice conversation, in full.
        static let voiceCaptions = "captions.bubble"
        /// 音色: the voice Persona speaks in.
        static let voiceTimbre = "person.wave.2"
        /// Pairing a device or a bot with a one-time code.
        static let pairing = "link.badge.plus"
        /// 设置 › 连接器: the data sources Persona reads and acts in (this device, mail, 飞书).
        static let connectors = "point.3.connected.trianglepath.dotted"
        /// This device as a connector (its contacts, calendar, photos…): iPhone, iPad or Mac.
        #if os(macOS)
        static let thisDevice = "laptopcomputer"
        #else
        static let thisDevice = "iphone.gen3"
        #endif
        /// AI 草拟回复: a reply drafted in the user's voice.
        static let draftReply = "pencil.and.scribble"
        /// 交给 AI: an inbox item handed to Persona or an agent.
        static let handToAI = "paperplane"
        /// Mentions of the user (@我).
        static let mention = "at"
        /// The day's 今天 card at the top of the assistant's conversation (docs/unified-entry.md).
        static let today = "sun.max.fill"
        /// Something done on the phone from the conversation: an event added to its calendar, a reminder.
        static let addedEvent = "calendar.badge.plus"
        static let addedReminder = "checklist.checked"
        /// Kinds of tasks that start without asking first (这类以后直接开).
        static let taskTrust = "bolt.shield"
        /// A plan the assistant handed the user (计划卡).
        static let taskPlan = "list.clipboard"
        /// Saying something straight to a task (说一句, 「对「xx」说」).
        static let routeToTask = "arrowshape.turn.up.right"
        /// A pull request a task opened.
        static let pullRequest = "arrow.triangle.pull"
        /// GitHub's thread page: a merged pull request, an open issue, a review asking for changes, a check skipped
        /// or cancelled, a label.
        static let merged = "arrow.triangle.merge"
        static let issueOpen = "smallcircle.filled.circle"
        static let changesRequested = "plusminus.circle"
        static let skipped = "minus.circle"
        static let label = "tag"
        /// A group chat (群聊).
        static let groupChat = "rectangle.3.group.bubble"
        /// 信息 (the Messages app's data on a Mac) as a connector, and its conversations.
        static let messagesApp = "message.fill"
        /// An attachment (a mail's, a message's): a file; a video, a voice message.
        static let attachment = "paperclip"
        static let video = "video"
        static let voiceMessage = "waveform"
        /// A video's thumbnail: it plays elsewhere (the chat app).
        static let playMedia = "play.circle.fill"
        /// Send someone a message.
        static let message = "bubble.left.fill"
        static let credentials = "key"
        /// Cannot be changed from here: a session the phone cannot send to, a launcher that fixes its own model.
        static let locked = "lock"
        /// Feishu [removed] words: encoded for the eye, not encrypted.
        static let braveClimb = "shield"
        static let memory = "brain"
        /// Search (a list's, the sidebar's).
        static let search = "magnifyingglass"
        /// The knowledge base (~/.jarvis/data).
        static let knowledge = "books.vertical"
        /// 健康: the health space (docs/health.md), its entry and its conversations' mark.
        static let health = "heart"
        static let healthFilled = "heart.fill"
        /// What stays private (健康's note on where its conversations and memory are kept).
        static let privacy = "checkmark.shield"
        /// A tool the app has no symbol for.
        static let unknownTool = "wrench.and.screwdriver"
    }

    /// Other products' own marks, in the asset catalog's Brands folder (sources and terms in THIRD_PARTY_NOTICES.md).
    /// They are drawn as their owners publish them: not recolored, not redrawn as symbols. The OS marks are one-color
    /// templates and take the distribution's color where they are drawn.
    nonisolated enum Brand {
        static let claude = "brand-claude"
        static let openAI = "brand-openai"
        static let lark = "brand-lark"
        /// GitHub's mark (primer/octicons mark-github), a template image: it takes the foreground color.
        static let github = "brand-github"
        /// Telegram's logo (telegram.org/img/t_logo.svg), as it is.
        static let telegram = "brand-telegram"
        /// Google's G, only on the 使用 Google 账号登录 button.
        static let google = "brand-google"
        static let gmail = "brand-gmail"
        /// Microsoft's four squares, only on the 使用 Microsoft 账号登录 button and Outlook accounts.
        static let microsoft = "brand-microsoft"
        /// iPhone's own apps, for the data Persona reads from them (设置 › 连接器 › 这台 iPhone): their App Store artwork.
        static let iosContacts = "ios-app-contacts"
        static let iosCalendar = "ios-app-calendar"
        static let iosReminders = "ios-app-reminders"
        static let iosHealth = "ios-app-health"
        static let iosPhotos = "ios-app-photos"
        static let iosMaps = "ios-app-maps"

        /// How much of its square box a mark's artwork fills, so marks of different shapes look the same size side by
        /// side (`BrandMark`). The assets have no padding of their own (brand-lark's viewBox is cropped to the bird), and
        /// they fit the box: a round or wide mark fills it (1), a full square looks bigger than a circle of the same
        /// width, so it is drawn smaller, as on Material's keyline grid (18 of 24 for a square, 20 for a circle).
        static func optical(_ asset: String) -> CGFloat {
            asset == microsoft ? 0.88 : 1
        }

        /// Marks whose artwork is a circle: they fill a round badge (`BrandBadge`); any other shape is drawn at
        /// `inCircle` of it, so its corners stay inside the circle (a 5:4 mark's corners reach the edge at 0.78).
        static let round: Set<String> = [telegram, github, google]
        static let inCircle: CGFloat = 0.72

        /// A mark on a tile (`BrandTile`): the share of the tile's side it gets, about what a symbol has on its tile.
        static let onTile: CGFloat = 0.62

        /// iPhone apps' icons: drawn whole in an app icon's squircle with its edge, not fitted as a mark.
        static func isAppIcon(_ asset: String) -> Bool { asset.hasPrefix("ios-app-") }

        /// A Linux distribution's mark; macOS uses the system's apple.logo.
        static func os(_ f: OSFamily) -> String? {
            switch f {
            case .macos, .other: nil
            default: "os-\(f.rawValue)"
            }
        }
    }
}
