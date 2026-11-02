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
        static let info = "info.circle"
        static let offline = "wifi.exclamationmark"
        static let needsYou = "exclamationmark.bubble.fill"
        /// A status dot (online, unread).
        static let dot = "circle.fill"
        static let time = "clock"
        static let muted = "bell.slash.fill"
        static let notifications = "bell.badge"
        /// An Agent at work (the fleet's working count).
        static let working = "gearshape.fill"
        /// Calls moved to the background.
        static let backgroundTask = "gearshape.2"
        static let backgroundTaskFilled = "gearshape.2.fill"
        static let pinned = "pin.fill"
        static let unchecked = "circle"
        static let uncheckedBox = "square"
        static let todo = "checklist"
        /// Recent, or waiting to come back (reconnecting, last used).
        static let recent = "clock.arrow.circlepath"
        static let inProgress = "arrow.triangle.2.circlepath"
        static let recurring = "repeat"
        static let schedule = "alarm"
        /// Asks before acting (the manual permission mode), or needs someone to act.
        static let askFirst = "hand.raised"
        /// bypassPermissions: nothing is asked.
        static let allowEverything = "exclamationmark.shield"
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
        static let compose = "square.and.pencil"
        /// Speech to text in the composer (a tap starts, a second one ends).
        static let dictate = "mic"
        static let dictating = "mic.fill"
        /// 长录音作为上下文，与输入框听写区分。
        static let recording = "waveform.circle"
        static let pause = "pause.fill"
        static let share = "square.and.arrow.up"
        static let save = "square.and.arrow.down"
        static let retry = "arrow.clockwise"
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
        /// 设置 › 外观 › App 图标 (the home screen icon).
        static let appIcon = "app.badge"
        /// 设置 › 外观 › 布局 (the shell or the old tabs).
        static let layout = "rectangle.topthird.inset.filled"
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
        static let keyboard = "keyboard"

        // MARK: Navigation

        static let disclosure = "chevron.right"
        static let back = "chevron.left"
        /// Forward in a page's history (the link panel's web view).
        static let forward = "chevron.forward"
        static let expand = "chevron.down"
        static let collapse = "chevron.up"

        // MARK: Things

        /// Jarvis itself (the brand's four-point star).
        static let jarvis = "sparkle"
        /// AI does it: summaries, drafts, AI edits, hand to Jarvis.
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
        static let space = "square.stack.3d.up"
        static let spaceFilled = "square.stack.3d.up.fill"
        static let conversation = "text.bubble"
        static let file = "doc"
        static let document = "doc.text"
        /// A spreadsheet (Feishu 表格).
        static let sheet = "tablecells"
        /// A database table (Feishu 多维表格).
        static let base = "square.grid.3x2"
        /// A removed (Feishu Moments) post in the inbox: the company's public feed.
        static let moments = "rectangle.3.group.bubble"
        static let folder = "folder"
        static let folderFilled = "folder.fill"
        static let photo = "photo"
        static let photos = "photo.on.rectangle"
        static let camera = "camera"
        static let list = "list.bullet.rectangle"
        static let text = "text.alignleft"
        static let machines = "macbook.and.iphone"
        /// A computer whose kind is not known.
        static let machine = "desktopcomputer"
        static let laptop = "laptopcomputer"
        /// This Mac out of the hub's reach (the Jarvis page's device chip on the Mac).
        static let laptopOffline = "laptopcomputer.slash"
        static let server = "server.rack"
        /// The hub has a newer version, or is updating itself (docs/hub-update.md).
        static let hubUpdate = "arrow.down.to.line.circle"
        /// The assistant's skills (~/.jarvis/skills).
        static let skills = "puzzlepiece.extension"
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
        static let mail = "envelope"
        static let mailFilled = "envelope.fill"
        /// Feishu's chats as a source (the inbox's 飞书 filter, the knowledge base's 会话).
        static let feishuChats = "bubble.left.and.bubble.right"
        /// The mail accounts (管理邮箱).
        static let mailAccounts = "envelope.badge"
        /// 设置 › 连接器: the data sources Jarvis reads and acts in (this device, mail, 飞书).
        static let connectors = "point.3.connected.trianglepath.dotted"
        /// This device as a connector (its contacts, calendar, photos…): iPhone, iPad or Mac.
        #if os(macOS)
        static let thisDevice = "laptopcomputer"
        #else
        static let thisDevice = "iphone.gen3"
        #endif
        /// AI 草拟回复: a reply drafted in the user's voice.
        static let draftReply = "pencil.and.scribble"
        /// 交给 AI: an inbox item handed to Jarvis or an agent.
        static let handToAI = "paperplane"
        /// Mentions of the user (@我).
        static let mention = "at"
        /// A group chat (群聊).
        static let groupChat = "rectangle.3.group.bubble"
        /// 信息 (the Messages app's data on a Mac) as a connector, and its conversations.
        static let messagesApp = "message.fill"
        /// An attachment (a mail's, a message's): a file; a video, a voice message.
        static let attachment = "paperclip"
        static let video = "video"
        static let voiceMessage = "waveform"
        /// Send someone a message.
        static let message = "bubble.left.fill"
        static let credentials = "key"
        static let memory = "brain"
        /// Search (a list's, the sidebar's).
        static let search = "magnifyingglass"
        /// The knowledge base (~/.jarvis/data).
        static let knowledge = "books.vertical"
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
        /// Google's G, only on the 使用 Google 账号登录 button.
        static let google = "brand-google"
        static let gmail = "brand-gmail"
        /// Microsoft's four squares, only on the 使用 Microsoft 账号登录 button and Outlook accounts.
        static let microsoft = "brand-microsoft"
        /// iPhone's own apps, for the data Jarvis reads from them (设置 › 连接器 › 这台 iPhone): their App Store artwork.
        static let iosContacts = "ios-app-contacts"
        static let iosCalendar = "ios-app-calendar"
        static let iosReminders = "ios-app-reminders"
        static let iosHealth = "ios-app-health"
        static let iosPhotos = "ios-app-photos"
        static let iosMaps = "ios-app-maps"

        /// A Linux distribution's mark; macOS uses the system's apple.logo.
        static func os(_ f: OSFamily) -> String? {
            switch f {
            case .macos, .other: nil
            default: "os-\(f.rawValue)"
            }
        }
    }
}
