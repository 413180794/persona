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
        static let share = "square.and.arrow.up"
        static let save = "square.and.arrow.down"
        static let retry = "arrow.clockwise"
        static let undo = "arrow.uturn.backward"
        static let interrupt = "stop.fill"
        static let stopLoop = "stop.circle"
        static let run = "play.fill"
        static let add = "plus"
        static let send = "arrow.up"
        static let sendMail = "paperplane.fill"
        static let reply = "arrowshape.turn.up.left"
        /// Leave the app for another one (飞书).
        static let openExternal = "arrow.up.forward.app"
        /// Translate, and what was translated.
        static let translate = "translate"
        /// Open the thing the row points at (a sub-Agent, a card's link).
        static let open = "arrow.up.right"
        /// Open a file or a document in its viewer.
        static let openDocument = "arrow.up.forward.square"
        /// What follows from something before it: a follow-up, the next row of a table.
        static let followOn = "arrow.turn.down.right"
        static let enlarge = "arrow.up.left.and.arrow.down.right"
        static let upDown = "arrow.up.arrow.down"
        static let select = "selection.pin.in.out"
        static let insert = "text.cursor"
        static let view = "eye"
        static let options = "slider.horizontal.3"
        static let settings = "gearshape"
        static let enter = "return"
        static let link = "link"
        static let id = "number"
        static let pin = "pin"
        static let unpin = "pin.slash"
        static let mute = "bell.slash"
        static let unmute = "bell"
        static let collapseAll = "rectangle.compress.vertical"
        static let newConversation = "plus.bubble"
        static let keyboard = "keyboard"

        // MARK: Navigation

        static let disclosure = "chevron.right"
        static let back = "chevron.left"
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
        static let folder = "folder"
        static let folderFilled = "folder.fill"
        static let photo = "photo"
        static let photos = "photo.on.rectangle"
        static let list = "list.bullet.rectangle"
        static let text = "text.alignleft"
        static let machines = "macbook.and.iphone"
        /// A computer whose kind is not known.
        static let machine = "desktopcomputer"
        static let laptop = "laptopcomputer"
        static let server = "server.rack"
        static let phone = "iphone"
        static let watch = "applewatch"
        static let handoff = "iphone.and.arrow.forward"
        static let person = "person.crop.circle"
        /// The user's own messages.
        static let user = "person.fill"
        static let inbox = "tray.full"
        static let mail = "envelope"
        static let mailFilled = "envelope.fill"
        /// Send someone a message.
        static let message = "bubble.left.fill"
        static let credentials = "key"
        static let memory = "brain"
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
        /// Google's G, only on the 使用 Google 账号登录 button.
        static let google = "brand-google"
        static let gmail = "brand-gmail"

        /// A Linux distribution's mark; macOS uses the system's apple.logo.
        static func os(_ f: OSFamily) -> String? {
            switch f {
            case .macos, .other: nil
            default: "os-\(f.rawValue)"
            }
        }
    }
}
