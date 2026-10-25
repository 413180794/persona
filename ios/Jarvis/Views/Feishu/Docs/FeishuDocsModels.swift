import Foundation

// Wire types of lark-connector's document, drive, wiki and docs-notification routes (src/shared/contracts/documents.ts,
// drive-space.ts, wiki.ts, docs-notifications.ts, document-*.ts), plus the pure logic the screens share.
// Times stay strings: some lists send "" (discover), which JSON.decoder would turn into 1970.

// MARK: - Lists

nonisolated struct FeishuDocsIcon: Codable, Hashable, Sendable {
    var type: String
    var value: String
}

/// One row of 最近 / 我的空间 / 共享给我 / 收藏 / 快速访问 / 推荐 (`DocumentListItem` + `DriveSpaceItem` + `DriveDiscoverItem`).
nonisolated struct FeishuDocsItem: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var token: String?
    var kind: String?
    var title: String
    var url: String
    var type: Int
    var icon: FeishuDocsIcon?
    var owner: String?
    var ownerAvatarUrl: String?
    var createdAt: String?
    var updatedAt: String?
    var editedAt: String?
    var activityAt: String?
    var mine: Bool?
    var starred: Bool?
    var pinned: Bool?
    var shortcut: Bool?
    var external: Bool?
    var sharedFolder: Bool?
    var size: Double?
    var `extension`: String?
    var supported: Bool?
    // 推荐 (discover) only.
    var summary: String?
    var reason: String?
    var views: Int?
    var likes: Int?
    var comments: Int?

    var docKind: FeishuDocsKind { FeishuDocsKind(type: type, url: url, title: title, kind: kind) }
    var isFolder: Bool { docKind == .folder }
    /// The folder token a drive listing takes (`folder=`).
    var folderToken: String { token ?? id }
    var updated: Date? { updatedAt.flatMap(JSON.parseDate) }
    var activity: Date? { activityAt.flatMap(JSON.parseDate) }
    var edited: Date? { editedAt.flatMap(JSON.parseDate) }
}

nonisolated struct FeishuDocsPage: Decodable, Sendable {
    var items: [FeishuDocsItem]
    var nextCursor: String?
    var path: [Crumb]?

    nonisolated struct Crumb: Decodable, Hashable, Sendable {
        var token: String
        var name: String
    }
}

/// What a document is, for its icon, color and how the reader treats it. Types are Lark explorer object types.
nonisolated enum FeishuDocsKind: String, Sendable {
    case folder, doc, sheet, bitable, mindnote, file, slides, wiki, page, whiteboard, other

    init(type: Int, url: String, title: String = "", kind: String? = nil) {
        if kind == "folder" { self = .folder; return }
        switch type {
        case 0: self = .folder
        case 2, 22: self = .doc
        case 3: self = .sheet
        case 8: self = .bitable
        case 11: self = .mindnote
        case 12: self = .file
        case 16: self = .wiki
        case 30: self = .slides
        default: self = Self.from(url: url)
        }
    }

    /// From the link alone (`/docx/`, `/sheets/`, `/base/`…); a wiki link does not say what it holds.
    static func from(url: String) -> FeishuDocsKind {
        let parts = URL(string: url)?.pathComponents.dropFirst() ?? []
        switch parts.first ?? "" {
        case "docx", "docs", "doc": return .doc
        case "sheets", "sheet": return .sheet
        case "base", "bitable": return .bitable
        case "mindnotes", "mindnote": return .mindnote
        case "file": return .file
        case "slides": return .slides
        case "wiki": return .wiki
        case "page": return .page
        case "board", "whiteboard": return .whiteboard
        case "drive": return parts.dropFirst().first == "folder" ? .folder : .other
        default: return .other
        }
    }

    var label: String {
        switch self {
        case .folder: "文件夹"
        case .doc: "文档"
        case .sheet: "表格"
        case .bitable: "多维表格"
        case .mindnote: "思维笔记"
        case .file: "文件"
        case .slides: "幻灯片"
        case .wiki: "知识库"
        case .page: "页面"
        case .whiteboard: "画板"
        case .other: "文档"
        }
    }

    var symbol: String {
        switch self {
        case .folder: "folder.fill"
        case .doc: "doc.text.fill"
        case .sheet: "tablecells.fill"
        case .bitable: "square.grid.3x2.fill"
        case .mindnote: "point.3.connected.trianglepath.dotted"
        case .file: "doc.fill"
        case .slides: "play.rectangle.fill"
        case .wiki: "books.vertical.fill"
        case .page: "rectangle.on.rectangle"
        case .whiteboard: "scribble.variable"
        case .other: "doc.fill"
        }
    }

    /// Feishu's own colors for each kind (LarkColor names).
    var hue: String {
        switch self {
        case .folder: "yellow"
        case .doc, .other: "blue"
        case .sheet: "green"
        case .bitable: "purple"
        case .mindnote: "wathet"
        case .file: "grey"
        case .slides: "orange"
        case .wiki: "indigo"
        case .page: "turquoise"
        case .whiteboard: "violet"
        }
    }
}

/// A file's icon from its extension (pdf, zip, image…), as Feishu tells uploaded files apart.
nonisolated enum FeishuDocsFileKind {
    static func symbol(ext: String?, title: String) -> (symbol: String, hue: String) {
        let e = (ext?.isEmpty == false ? ext! : (title.split(separator: ".").last.map(String.init) ?? "")).lowercased()
        switch e {
        case "pdf": return ("doc.richtext.fill", "red")
        case "zip", "rar", "7z", "tar", "gz", "bz2": return ("doc.zipper", "orange")
        case "png", "jpg", "jpeg", "gif", "webp", "svg", "heic": return ("photo.fill", "turquoise")
        case "mp3", "wav", "m4a", "flac", "ogg": return ("waveform", "violet")
        case "mp4", "mov", "webm", "mkv", "avi": return ("film.fill", "carmine")
        case "xls", "xlsx", "csv", "ods": return ("tablecells.fill", "green")
        case "ppt", "pptx", "odp", "key": return ("play.rectangle.fill", "orange")
        case "doc", "docx", "odt", "rtf", "pages": return ("doc.text.fill", "blue")
        case "md", "txt", "json", "yaml", "yml", "js", "ts", "py", "go", "sh", "swift", "html", "css":
            return ("chevron.left.forwardslash.chevron.right", "indigo")
        default: return ("doc.fill", "grey")
        }
    }
}

// MARK: - Grouping

/// Time buckets the lists are sectioned into (Feishu's recent list reads top-down by day like this).
nonisolated enum FeishuDocsBucket: Int, CaseIterable, Sendable {
    case today, yesterday, week, month, earlier

    var title: String {
        switch self {
        case .today: "今天"
        case .yesterday: "昨天"
        case .week: "最近 7 天"
        case .month: "最近 30 天"
        case .earlier: "更早"
        }
    }

    static func of(_ date: Date?, now: Date = .now, calendar: Calendar = .current) -> FeishuDocsBucket {
        guard let date else { return .earlier }
        if calendar.isDate(date, inSameDayAs: now) { return .today }
        let startToday = calendar.startOfDay(for: now)
        guard let startYesterday = calendar.date(byAdding: .day, value: -1, to: startToday) else { return .earlier }
        if date >= startYesterday && date < startToday { return .yesterday }
        if date > now { return .today }
        let days = now.timeIntervalSince(date) / 86400
        if days < 7 { return .week }
        if days < 30 { return .month }
        return .earlier
    }

    /// Splits items (already in display order) into non-empty buckets, keeping their order inside each bucket.
    static func group<T>(_ items: [T], now: Date = .now, calendar: Calendar = .current,
                         date: (T) -> Date?) -> [(bucket: FeishuDocsBucket, items: [T])] {
        var out: [FeishuDocsBucket: [T]] = [:]
        for item in items { out[of(date(item), now: now, calendar: calendar), default: []].append(item) }
        return allCases.compactMap { b in out[b].map { (b, $0) } }
    }
}

/// How a drive listing is sorted (lark-connector `sort=`), and the client-side type filter of the lists.
nonisolated enum FeishuDocsDriveSort: String, CaseIterable, Sendable {
    case edited, created, name, owner

    var label: String {
        switch self {
        case .edited: "最近编辑"
        case .created: "创建时间"
        case .name: "名称"
        case .owner: "所有者"
        }
    }
}

nonisolated enum FeishuDocsFilter: String, CaseIterable, Sendable {
    case all, doc, sheet, bitable, mindnote, file, folder

    var label: String {
        switch self {
        case .all: "全部类型"
        case .doc: "文档"
        case .sheet: "表格"
        case .bitable: "多维表格"
        case .mindnote: "思维笔记"
        case .file: "文件"
        case .folder: "文件夹"
        }
    }

    func matches(_ item: FeishuDocsItem) -> Bool {
        switch self {
        case .all: true
        case .doc: [.doc, .wiki, .page, .other].contains(item.docKind)
        case .sheet: item.docKind == .sheet
        case .bitable: item.docKind == .bitable
        case .mindnote: item.docKind == .mindnote
        case .file: item.docKind == .file
        case .folder: item.docKind == .folder
        }
    }
}

/// Folders first (as Feishu lists a folder), the rest in the server's order.
nonisolated func feishuDocsFoldersFirst(_ items: [FeishuDocsItem]) -> [FeishuDocsItem] {
    items.filter(\.isFolder) + items.filter { !$0.isFolder }
}

/// Appends a page to a list, dropping ids already shown (cursor pages can overlap).
nonisolated func feishuDocsAppendUnique<T: Identifiable>(_ list: [T], _ page: [T]) -> [T] where T.ID: Hashable {
    var seen = Set(list.map(\.id))
    var out = list
    for item in page where seen.insert(item.id).inserted { out.append(item) }
    return out
}

// MARK: - Notifications

nonisolated struct FeishuDocsNotice: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var title: String
    var url: String?
    var type: Int
    var icon: FeishuDocsIcon?
    var kind: String
    var summary: String
    var atMe: Bool
    var unreadCount: Int
    var updatedAt: String
    var muted: Bool?

    var updated: Date? { JSON.parseDate(updatedAt) }
    var docKind: FeishuDocsKind { FeishuDocsKind(type: type, url: url ?? "") }
    var noticeKind: FeishuDocsNoticeKind { FeishuDocsNoticeKind(rawValue: kind) ?? .other }
}

nonisolated struct FeishuDocsNoticePage: Decodable, Sendable {
    var items: [FeishuDocsNotice]
    var nextCursor: String?
}

nonisolated enum FeishuDocsNoticeKind: String, Sendable {
    case mention, comment, reply, solve, share, reopen, reaction, other

    var label: String {
        switch self {
        case .mention: "提到了你"
        case .comment: "评论"
        case .reply: "回复"
        case .solve: "解决了评论"
        case .share: "分享"
        case .reopen: "重新打开评论"
        case .reaction: "表情回复"
        case .other: "通知"
        }
    }

    var symbol: String {
        switch self {
        case .mention: "at"
        case .comment: "text.bubble.fill"
        case .reply: "arrowshape.turn.up.left.fill"
        case .solve: "checkmark.circle.fill"
        case .share: "square.and.arrow.up.fill"
        case .reopen: "arrow.uturn.backward.circle.fill"
        case .reaction: "face.smiling.inverse"
        case .other: "bell.fill"
        }
    }

    var hue: String {
        switch self {
        case .mention: "red"
        case .comment, .reply: "blue"
        case .solve: "green"
        case .share: "turquoise"
        case .reopen: "orange"
        case .reaction: "yellow"
        case .other: "grey"
        }
    }
}

/// The filter chips of 文档通知.
nonisolated enum FeishuDocsNoticeFilter: String, CaseIterable, Sendable {
    case all, unread, mention, comment, share

    var label: String {
        switch self {
        case .all: "全部"
        case .unread: "未读"
        case .mention: "@我"
        case .comment: "评论"
        case .share: "分享"
        }
    }

    func matches(_ n: FeishuDocsNotice) -> Bool {
        switch self {
        case .all: true
        case .unread: n.unreadCount > 0
        case .mention: n.atMe || n.noticeKind == .mention
        case .comment: [.comment, .reply, .solve, .reopen, .reaction].contains(n.noticeKind)
        case .share: n.noticeKind == .share
        }
    }
}

// MARK: - Wiki

nonisolated struct FeishuDocsWikiSpace: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var name: String
    var description: String?
    var icon: FeishuDocsIcon?
    var kind: String?
    var starred: Bool?
    var homeToken: String?
    var mine: Bool?
}

nonisolated struct FeishuDocsWikiSpacePage: Decodable, Sendable {
    var spaces: [FeishuDocsWikiSpace]
    var nextCursor: String?
}

nonisolated struct FeishuDocsWikiNode: Codable, Identifiable, Hashable, Sendable {
    var token: String
    var spaceId: String
    var parentToken: String?
    var title: String
    var objType: Int
    var url: String
    var hasChildren: Bool
    var icon: FeishuDocsIcon?
    var supported: Bool?
    var target: Target?

    nonisolated struct Target: Codable, Hashable, Sendable {
        var spaceId: String
        var token: String
    }

    var id: String { token }
    var docKind: FeishuDocsKind { FeishuDocsKind(type: objType, url: url, title: title) }
    /// Where this node's children are listed: a shortcut's live with its target.
    var childrenSource: (space: String, node: String) { target.map { ($0.spaceId, $0.token) } ?? (spaceId, token) }
}

nonisolated struct FeishuDocsWikiChildrenPage: Decodable, Sendable {
    var nodes: [FeishuDocsWikiNode]
    var nextCursor: String?
}

nonisolated struct FeishuDocsWikiTree: Decodable, Sendable {
    var space: FeishuDocsWikiSpace
    var wikiBaseUrl: String?
    var roots: [FeishuDocsWikiNode]
    var children: [String: [FeishuDocsWikiNode]]
    var path: [String]?
    var restricted: Bool?
}

nonisolated struct FeishuDocsWikiUpdate: Codable, Identifiable, Hashable, Sendable {
    var token: String
    var title: String
    var objType: Int
    var url: String
    var icon: FeishuDocsIcon?
    var supported: Bool?
    var editor: Editor
    var updatedAt: Double

    nonisolated struct Editor: Codable, Hashable, Sendable {
        var id: String
        var name: String
        var app: Bool?
    }

    var id: String { token }
    var updated: Date? { updatedAt > 0 ? Date(timeIntervalSince1970: updatedAt / 1000) : nil }
    var docKind: FeishuDocsKind { FeishuDocsKind(type: objType, url: url, title: title) }
}

nonisolated struct FeishuDocsWikiUpdatesPage: Decodable, Sendable {
    var updates: [FeishuDocsWikiUpdate]
    var nextCursor: String?
}

/// One visible row of a knowledge base's tree: the node and how deep it sits.
nonisolated struct FeishuDocsWikiRow: Identifiable, Hashable, Sendable {
    var node: FeishuDocsWikiNode
    var depth: Int
    var expanded: Bool
    var id: String { node.token }
}

/// The rows a tree shows: roots, then each expanded node's loaded children beneath it, depth first.
/// A node whose children have not arrived yet still shows as expanded (its row draws a spinner).
nonisolated func feishuDocsWikiRows(roots: [FeishuDocsWikiNode], children: [String: [FeishuDocsWikiNode]], expanded: Set<String>) -> [FeishuDocsWikiRow] {
    var out: [FeishuDocsWikiRow] = []
    var seen = Set<String>()
    func walk(_ nodes: [FeishuDocsWikiNode], depth: Int) {
        for n in nodes where seen.insert(n.token).inserted {
            let open = n.hasChildren && expanded.contains(n.token)
            out.append(FeishuDocsWikiRow(node: n, depth: depth, expanded: open))
            if open, let kids = children[n.token] { walk(kids, depth: depth + 1) }
        }
    }
    walk(roots, depth: 0)
    return out
}

// MARK: - Reader

nonisolated struct FeishuDocsAuthority: Codable, Hashable, Sendable {
    var copy: Bool
    var print: Bool
    var export: Bool
}

/// `documents/read?format=markdown`.
nonisolated struct FeishuDocsMarkdownResponse: Decodable, Sendable {
    var type: String
    var token: String
    var title: String
    var url: String
    var permission: String?
    var authority: FeishuDocsAuthority?
    var markdown: String
    var partial: Bool?
}

/// One node of the ProseMirror-style tree `documents/read?format=json` returns.
nonisolated struct FeishuDocsNode: Codable, Hashable, Sendable {
    var type: String
    var attrs: [String: JSONValue]?
    var content: [FeishuDocsNode]?
    var marks: [FeishuDocsMark]?
    var text: String?

    func attr(_ key: String) -> String? { attrs?[key]?.string }
    func number(_ key: String) -> Double? { attrs?[key]?.double }
    func flag(_ key: String) -> Bool { attrs?[key]?.bool ?? (attrs?[key]?.string == "true") }
    var children: [FeishuDocsNode] { content ?? [] }
    var blockID: String? { attr("id") }

    /// All text below this node, mentions by their label.
    var plainText: String {
        if let text { return text }
        if type == "mention" { return attr("label") ?? "" }
        if type == "hardBreak" { return "\n" }
        if type == "equation" { return attr("latex") ?? "" }
        return children.map(\.plainText).joined()
    }
}

nonisolated struct FeishuDocsMark: Codable, Hashable, Sendable {
    var type: String
    var attrs: [String: JSONValue]?
}

/// `documents/read?format=json`: the tree plus what the reader needs around it. `base` / `spreadsheet` are only
/// checked for presence (the Base viewer is out of scope; sheets read as Markdown when export is allowed).
nonisolated struct FeishuDocsView: Decodable, Sendable {
    var title: String
    var url: String
    var token: String
    var content: FeishuDocsNode
    var outline: [FeishuDocsOutlineItem]
    var owner: FeishuDocsPerson?
    var createdAt: String?
    var updatedAt: String?
    var permission: String?
    var authority: FeishuDocsAuthority?
    var reader: String?
    var object: Object?
    var partial: Bool?
    var isBase: Bool
    var isSpreadsheet: Bool

    nonisolated struct Object: Decodable, Hashable, Sendable {
        var kind: String
        var token: String
        var `extension`: String?
        var image: String?
        var empty: Bool?
    }

    enum CodingKeys: String, CodingKey {
        case title, url, token, content, outline, owner, createdAt, updatedAt, permission, authority, reader, object, partial, base, spreadsheet
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        url = try c.decodeIfPresent(String.self, forKey: .url) ?? ""
        token = try c.decodeIfPresent(String.self, forKey: .token) ?? ""
        content = try c.decodeIfPresent(FeishuDocsNode.self, forKey: .content) ?? FeishuDocsNode(type: "doc")
        outline = try c.decodeIfPresent([FeishuDocsOutlineItem].self, forKey: .outline) ?? []
        owner = try c.decodeIfPresent(FeishuDocsPerson.self, forKey: .owner)
        createdAt = try c.decodeIfPresent(String.self, forKey: .createdAt)
        updatedAt = try c.decodeIfPresent(String.self, forKey: .updatedAt)
        permission = try c.decodeIfPresent(String.self, forKey: .permission)
        authority = try c.decodeIfPresent(FeishuDocsAuthority.self, forKey: .authority)
        reader = try c.decodeIfPresent(String.self, forKey: .reader)
        object = try c.decodeIfPresent(Object.self, forKey: .object)
        partial = try c.decodeIfPresent(Bool.self, forKey: .partial)
        isBase = c.contains(.base)
        isSpreadsheet = c.contains(.spreadsheet)
    }
}

nonisolated struct FeishuDocsOutlineItem: Codable, Hashable, Identifiable, Sendable {
    var id: String
    var title: String
    var level: Int
}

nonisolated struct FeishuDocsPerson: Codable, Hashable, Sendable {
    var id: String?
    var name: String
    var avatarUrl: String?
}

/// `documents/header`: where the document sits and who edited it last.
nonisolated struct FeishuDocsHeader: Decodable, Sendable {
    var token: String?
    var type: Int?
    var inMySpace: Bool?
    var path: [FeishuDocsPage.Crumb]?
    var icon: FeishuDocsIcon?
    var editedAt: String?
    var editorName: String?
}

/// `documents/reader/info`: owner, counts.
nonisolated struct FeishuDocsReaderInfo: Decodable, Sendable {
    var owner: FeishuDocsPerson?
    var creator: FeishuDocsPerson?
    var createdAt: String?
    var updatedAt: String?
    var views: Int?
    var visitors: Int?
    var viewsToday: Int?
    var likes: Int?
    var comments: Int?
}

/// `documents/ai-summary`: the cached summary (never generated here).
nonisolated struct FeishuDocsAISummary: Decodable, Sendable {
    var state: String
    var summary: String
}

/// `documents/comments`.
nonisolated struct FeishuDocsComments: Decodable, Sendable {
    var threads: [Thread]

    nonisolated struct Thread: Decodable, Identifiable, Hashable, Sendable {
        var id: String
        var whole: Bool
        var resolved: Bool
        var quote: String
        var anchorBlockId: String?
        var createdAt: String?
        var replies: [Reply]
        var visibility: String?
    }

    nonisolated struct Reply: Decodable, Identifiable, Hashable, Sendable {
        var id: String
        var author: FeishuDocsPerson
        var content: [Segment]
        var imageCount: Int?
        var createdAt: String?
        var edited: Bool?
        var reactions: [Reaction]?

        var text: String { content.map(\.text).joined() }
        var created: Date? { createdAt.flatMap(JSON.parseDate) }
    }

    nonisolated struct Segment: Decodable, Hashable, Sendable {
        var kind: String
        var text: String
        var href: String?
    }

    nonisolated struct Reaction: Decodable, Hashable, Sendable {
        var key: String
        var count: Int
    }
}

/// Open threads first (newest activity first), resolved ones after.
nonisolated func feishuDocsSortedThreads(_ threads: [FeishuDocsComments.Thread]) -> [FeishuDocsComments.Thread] {
    func last(_ t: FeishuDocsComments.Thread) -> Date {
        t.replies.compactMap(\.created).max() ?? t.createdAt.flatMap(JSON.parseDate) ?? .distantPast
    }
    return threads.sorted { a, b in
        if a.resolved != b.resolved { return !a.resolved }
        return last(a) > last(b)
    }
}

// MARK: - Markdown preparation

/// The Markdown export cut into sections that render (and scroll to) on their own.
nonisolated struct FeishuDocsMarkdownBody: Sendable, Equatable {
    nonisolated struct Chunk: Identifiable, Sendable, Equatable {
        var id: String
        var text: String
    }

    var chunks: [Chunk]
    var outline: [FeishuDocsOutlineItem]
    var images: [String]
}

nonisolated enum FeishuDocsMarkdownExport {
    /// Turns lark-connector's export into what the reader shows:
    /// - drops the leading `# Title` and `[Source](<url>)` lines (the reader draws its own title),
    /// - points media at the hub (`http://<lark-connector>/api/documents/…` → `/api/v1/lark/raw/documents/…`),
    /// - splits at headings outside code fences (and at blank lines in long runs), so one long document is many lazy rows,
    /// - collects the headings (the outline) and every image (the gallery).
    static func prepare(_ markdown: String, title: String? = nil, maxLines: Int = 60) -> FeishuDocsMarkdownBody {
        var lines = rewriteMedia(markdown).components(separatedBy: "\n")
        // Leading title and source link.
        while let first = lines.first, first.trimmingCharacters(in: .whitespaces).isEmpty { lines.removeFirst() }
        if let first = lines.first, first.hasPrefix("# "), title == nil || unescape(String(first.dropFirst(2))).trimmingCharacters(in: .whitespaces) == title?.trimmingCharacters(in: .whitespaces) {
            lines.removeFirst()
        }
        while let first = lines.first, first.trimmingCharacters(in: .whitespaces).isEmpty { lines.removeFirst() }
        if let first = lines.first, first.hasPrefix("[Source](") { lines.removeFirst() }

        var chunks: [FeishuDocsMarkdownBody.Chunk] = []
        var outline: [FeishuDocsOutlineItem] = []
        var current: [String] = []
        var currentID = "md-0"
        var fence: String?
        func flush() {
            let text = current.joined(separator: "\n").trimmingCharacters(in: .newlines)
            if !text.trimmingCharacters(in: .whitespaces).isEmpty { chunks.append(.init(id: currentID, text: text)) }
            current = []
        }
        for (i, line) in lines.enumerated() {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if let f = fence {
                current.append(line)
                if trimmed.hasPrefix(f) { fence = nil }
                continue
            }
            if trimmed.hasPrefix("```") || trimmed.hasPrefix("~~~") {
                fence = String(trimmed.prefix(3))
                current.append(line)
                continue
            }
            if let level = headingLevel(line) {
                flush()
                currentID = "md-\(i)"
                let text = unescape(String(line.drop(while: { $0 == "#" }))).trimmingCharacters(in: .whitespaces)
                if !text.isEmpty { outline.append(.init(id: currentID, title: stripInline(text), level: level)) }
                current.append(line)
                continue
            }
            // Long runs without headings: break at a blank line followed by a new top-level block.
            if current.count >= maxLines, trimmed.isEmpty, i + 1 < lines.count, startsTopLevelBlock(lines[i + 1]) {
                flush()
                currentID = "md-\(i + 1)"
                continue
            }
            current.append(line)
        }
        flush()
        return FeishuDocsMarkdownBody(chunks: chunks, outline: outline, images: images(in: chunks.map(\.text).joined(separator: "\n")))
    }

    /// `# ` … `###### ` at the start of a line.
    static func headingLevel(_ line: String) -> Int? {
        let hashes = line.prefix(while: { $0 == "#" }).count
        guard (1...6).contains(hashes), line.dropFirst(hashes).first == " " else { return nil }
        return hashes
    }

    /// A line that starts a block at column 0 that is not a continuation of a list, table or quote above it.
    static func startsTopLevelBlock(_ line: String) -> Bool {
        guard let c = line.first, c != " ", c != "\t", c != "|", c != ">" else { return false }
        return !(line.hasPrefix("- ") || line.hasPrefix("* ") || line.hasPrefix("+ ") || line.range(of: #"^\d+\. "#, options: .regularExpression) != nil)
    }

    /// lark-connector writes absolute links to its own `/api/documents/…` media; the phone reaches them through the hub.
    static func rewriteMedia(_ s: String) -> String {
        s.replacingOccurrences(of: #"https?://[A-Za-z0-9.\-]+(:\d+)?/api/(?!v1/)"#, with: "/api/v1/lark/raw/", options: .regularExpression)
    }

    /// Image targets in order: `![alt](<url>)` or `![alt](url)`.
    static func images(in s: String) -> [String] {
        guard let re = try? NSRegularExpression(pattern: #"!\[[^\]]*\]\(<?([^)>\s]+)>?\)"#) else { return [] }
        return re.matches(in: s, range: NSRange(s.startIndex..., in: s)).compactMap { m in
            Range(m.range(at: 1), in: s).map { String(s[$0]) }
        }
    }

    /// Removes Markdown backslash escapes (`\-`, `\+`, `\.`…) for plain display.
    static func unescape(_ s: String) -> String {
        s.replacingOccurrences(of: #"\\([\\`*_{}\[\]()#+\-.!|<>~])"#, with: "$1", options: .regularExpression)
    }

    /// Drops `**`, `*`, `` ` `` and link syntax from a heading for the outline.
    static func stripInline(_ s: String) -> String {
        var t = s.replacingOccurrences(of: #"\[([^\]]*)\]\([^)]*\)"#, with: "$1", options: .regularExpression)
        for mark in ["**", "__", "`", "~~"] { t = t.replacingOccurrences(of: mark, with: "") }
        return t.trimmingCharacters(in: .whitespaces)
    }
}

// MARK: - Node tree helpers

nonisolated enum FeishuDocsTree {
    /// Block types the reader can scroll to as rows (top-level children of `doc`).
    static func blocks(_ doc: FeishuDocsNode) -> [FeishuDocsNode] { doc.children }

    /// The number each item of an ordered list shows. Every docx list item arrives as its own one-item `orderedList`;
    /// consecutive ones keep counting unless one says `restart` (then it starts at its `start`).
    static func orderedNumbers(_ siblings: [FeishuDocsNode]) -> [Int?] {
        var out: [Int?] = []
        var next: Int?
        for n in siblings {
            guard n.type == "orderedList" else { out.append(nil); next = nil; continue }
            let start = Int(n.number("start") ?? 1)
            let value = (n.flag("restart") || next == nil) ? start : next!
            out.append(value)
            next = value + max(1, n.children.filter { $0.type == "listItem" }.count)
        }
        return out
    }

    /// Every image source in document order (the gallery pages through them).
    static func images(_ node: FeishuDocsNode) -> [String] {
        var out: [String] = []
        func walk(_ n: FeishuDocsNode) {
            if n.type == "image", let src = n.attr("src"), !src.isEmpty { out.append(src) }
            n.children.forEach(walk)
        }
        walk(node)
        return out
    }

    /// The headings as an outline, for trees whose `outline` came back empty.
    static func outline(_ doc: FeishuDocsNode) -> [FeishuDocsOutlineItem] {
        doc.children.compactMap { n in
            guard n.type == "heading", let id = n.blockID else { return nil }
            let title = n.plainText.trimmingCharacters(in: .whitespacesAndNewlines)
            return title.isEmpty ? nil : FeishuDocsOutlineItem(id: id, title: title, level: Int(n.number("level") ?? 1))
        }
    }

    /// The top-level block an outline entry lives in (headings nested in callouts or columns scroll to their container).
    static func rowID(for blockID: String, in doc: FeishuDocsNode) -> String? {
        func contains(_ n: FeishuDocsNode) -> Bool { n.blockID == blockID || n.children.contains(where: contains) }
        for (i, n) in doc.children.enumerated() where contains(n) { return rowKey(n, index: i) }
        return nil
    }

    static func rowKey(_ n: FeishuDocsNode, index: Int) -> String { n.blockID ?? "row-\(index)" }
}

/// A table laid out on a grid: each row's slots, with spanned-over positions left empty so columns line up.
nonisolated struct FeishuDocsTableLayout: Equatable, Sendable {
    nonisolated struct Slot: Equatable, Sendable {
        /// Index of the cell in its row's content, nil for a position covered by a cell above or to the left.
        var cell: Int?
        var column: Int
        var colspan: Int
        var header: Bool
    }

    var rows: [[Slot]]
    var columns: Int
    var widths: [Double]

    init(_ table: FeishuDocsNode, defaultWidth: Double = 140, minWidth: Double = 72, maxWidth: Double = 320) {
        let tableRows = table.children.filter { $0.type == "tableRow" }
        var covered: [Int: Int] = [:]   // column -> rows still covered by a rowspan from above
        var out: [[Slot]] = []
        var widths: [Int: Double] = [:]
        var columns = 0
        for row in tableRows {
            var slots: [Slot] = []
            var col = 0
            func skipCovered() {
                while let left = covered[col], left > 0 {
                    slots.append(Slot(cell: nil, column: col, colspan: 1, header: false))
                    col += 1
                }
            }
            for (i, cell) in row.children.enumerated() where cell.type == "tableCell" || cell.type == "tableHeader" {
                skipCovered()
                let span = max(1, Int(cell.number("colspan") ?? 1))
                let rowspan = max(1, Int(cell.number("rowspan") ?? 1))
                slots.append(Slot(cell: i, column: col, colspan: span, header: cell.type == "tableHeader"))
                if let ws = cell.attrs?["colwidth"]?.array?.compactMap(\.double), ws.count == span {
                    for (k, w) in ws.enumerated() where widths[col + k] == nil { widths[col + k] = w }
                }
                for k in 0..<span where rowspan > 1 { covered[col + k] = rowspan }
                col += span
            }
            skipCovered()
            columns = max(columns, col)
            // A row's rowspans count down once the row is done (the ones set in this row cover the next rowspan-1 rows).
            for (c, left) in covered { covered[c] = left - 1 }
            out.append(slots)
        }
        // Pad short rows so every row spans every column.
        for r in out.indices {
            let used = out[r].reduce(0) { $0 + $1.colspan }
            for c in used..<max(used, columns) { out[r].append(Slot(cell: nil, column: c, colspan: 1, header: false)) }
        }
        self.rows = out
        self.columns = columns
        self.widths = (0..<columns).map { min(maxWidth, max(minWidth, widths[$0] ?? defaultWidth)) }
    }

    func width(of slot: Slot) -> Double {
        (slot.column..<min(columns, slot.column + slot.colspan)).reduce(0) { $0 + widths[$1] }
    }
}

// MARK: - Errors

/// A failed lark-connector read: the HTTP status and its `error.code` (DOCUMENT_EXPORT_FORBIDDEN, DOCUMENT_FORMAT_UNSUPPORTED…).
nonisolated struct FeishuDocsFailure: LocalizedError, Sendable {
    var status: Int
    var code: String?

    var errorDescription: String? {
        switch code {
        case "DOCUMENT_EXPORT_FORBIDDEN": "作者禁止导出这篇文档"
        case "DOCUMENT_FORMAT_UNSUPPORTED": "这种文档暂不支持在 Jarvis 中阅读"
        case "DOCUMENT_NOT_FOUND": "文档不存在或已被删除"
        case "DOCUMENT_FORBIDDEN", "DOCUMENT_PERMISSION_DENIED": "没有这篇文档的访问权限"
        case "DOCUMENT_TIMEOUT": "飞书响应超时，请稍后重试"
        case "DOCUMENT_INVALID_URL": "无法识别的文档链接"
        default:
            switch status {
            case 401: "飞书登录已失效"
            case 403: "没有访问权限"
            case 404: "内容不存在或已被删除"
            case 502, 503, 504: "飞书服务暂时不可用，请稍后重试"
            default: code ?? HTTPURLResponse.localizedString(forStatusCode: status)
            }
        }
    }

    /// `{"error":{"code":…}}` (lark-connector) or `{"error":"…"}` (hub).
    static func code(from data: Data) -> String? {
        guard let v = try? JSON.decoder.decode(JSONValue.self, from: data), let e = v["error"] else { return nil }
        return e["code"]?.string ?? e.string
    }
}
