import Foundation
import Observation

/// One tool invocation with its result, as shown in a chat.
nonisolated struct ToolCall: Identifiable, Hashable, Sendable {
    var use: Item
    var result: Item?
    var images: [ImagePayload] = []

    var id: String { use.id }
    var name: String { use.toolName ?? "tool" }
    var input: JSONValue { use.toolInput ?? .object([:]) }
    var isRunning: Bool { result == nil }
    var isError: Bool { result?.isError ?? false }
}

nonisolated enum ChatRow: Identifiable, Hashable, Sendable {
    case user(Item)
    case assistant(Item)
    case thinking(Item)
    case tools([ToolStep])
    case notice(Item)
    case image(Item)
    case recap(Item)

    var id: String {
        switch self {
        case .user(let i), .assistant(let i), .thinking(let i), .notice(let i), .image(let i), .recap(let i): i.id
        case .tools(let steps): "tools-" + (steps.first?.id ?? "")
        }
    }
}

nonisolated extension ChatRow {
    var firstTS: Date? {
        switch self {
        case .user(let i), .assistant(let i), .thinking(let i), .notice(let i), .image(let i), .recap(let i): i.ts
        case .tools(let steps): steps.lazy.compactMap(\.firstTS).first
        }
    }
    var lastTS: Date? {
        switch self {
        case .user(let i), .assistant(let i), .thinking(let i), .notice(let i), .image(let i), .recap(let i): i.ts
        case .tools(let steps): steps.reversed().lazy.compactMap(\.lastTS).first
        }
    }
}

/// A step inside a run of tool activity: a call, or a thought between calls.
nonisolated enum ToolStep: Identifiable, Hashable, Sendable {
    case call(ToolCall)
    case thought(Item)

    var id: String {
        switch self {
        case .call(let c): c.id
        case .thought(let i): i.id
        }
    }
    var firstTS: Date? {
        switch self {
        case .call(let c): c.use.ts
        case .thought(let i): i.ts
        }
    }
    var lastTS: Date? {
        switch self {
        case .call(let c): c.result?.ts ?? c.use.ts
        case .thought(let i): i.ts
        }
    }
}

/// One exchange: what the user said (including messages queued while the agent worked), everything the agent did,
/// and its final answer. Finished turns render as messages → one folded "process" row → the answer.
nonisolated struct Turn: Identifiable, Hashable, Sendable {
    var id: String
    var users: [ChatRow] = []
    var process: [ChatRow] = []
    var answer: ChatRow?
    var trailing: [ChatRow] = [] // notices that belong to the turn (e.g. "interrupted")
    /// Background tasks that woke the agent during this exchange (their notices).
    var wakes: [Item] = []
    /// Everything from the first wake on, for the "后台任务完成" drawer.
    var afterWake: [ChatRow] = []
    /// What came after the answer (a follow-up once a background task reported back): only in the drawer.
    var tail: [ChatRow] = []
    /// Everything the agent did in arrival order, answer included.
    var steps: [ChatRow] = []
    /// Messages and steps in arrival order (how the running turn is shown: input sent mid-turn stays where it was sent).
    var ordered: [ChatRow] = []

    /// From the first message to the last thing the agent did ("已工作 5 分 12 秒").
    var startedAt: Date? { (users + steps).lazy.compactMap(\.firstTS).first }
    var endedAt: Date? { (steps + trailing).reversed().lazy.compactMap(\.lastTS).first }

    var toolCount: Int {
        process.reduce(0) { n, r in
            if case .tools(let steps) = r { return n + steps.reduce(0) { if case .call = $1 { $0 + 1 } else { $0 } } }
            return n
        }
    }
    var thoughtCount: Int {
        process.reduce(0) { n, r in
            switch r {
            case .thinking: return n + 1
            case .tools(let steps): return n + steps.reduce(0) { if case .thought = $1 { $0 + 1 } else { $0 } }
            default: return n
            }
        }
    }
    var interimCount: Int { process.filter { if case .assistant = $0 { true } else { false } }.count }

    /// Background tasks split an exchange into runs (before the first wake, after each one), each ending in the
    /// agent's words. The answer is the longest of those: a full summary followed by "build 42 is up", or "waiting
    /// for the build" followed by the full summary. What comes before it folds into one work line (wakes included);
    /// what comes after it shows only in the drawer.
    mutating func pickAnswerAcrossWakes() {
        let all = ordered
        var candidates: [Int] = []
        var last: Int?
        for (i, r) in all.enumerated() {
            switch r {
            case .notice(let n) where n.wake == true:
                if let l = last { candidates.append(l) }
                last = nil
            case .assistant: last = i
            default: break
            }
        }
        if let l = last { candidates.append(l) }
        func length(_ i: Int) -> Int { if case .assistant(let a) = all[i] { a.text?.count ?? 0 } else { 0 } }
        var pick: Int?
        for c in candidates where pick == nil || length(c) >= length(pick!) { pick = c } // ties: the later one
        if let first = all.firstIndex(where: { if case .notice(let n) = $0 { n.wake == true } else { false } }) {
            afterWake = Array(all[first...]).filter { if case .user = $0 { false } else { true } }
        }
        guard let pick else { return }
        tail = all[(pick + 1)...].filter { if case .user = $0 { false } else { true } }
        ordered = Array(all[...pick]) + all[(pick + 1)...].filter { if case .user = $0 { true } else { false } }
        process = ordered.filter { if case .user = $0 { false } else { true } }
    }
}

/// Rows grouped for display: turns, plus standalone rows (recaps, compaction notices) between them.
nonisolated enum ChatBlock: Identifiable, Hashable, Sendable {
    case turn(Turn)
    case row(ChatRow)

    var id: String {
        switch self {
        case .turn(let t): "turn-" + t.id
        case .row(let r): r.id
        }
    }

    static func build(_ rows: [ChatRow]) -> [ChatBlock] {
        var blocks: [ChatBlock] = []
        var current: Turn?
        // The last exchange as it was before closing, and where it sits: a background task that reports back after
        // a recap card (Claude Code writes one when a finished session sits idle) still belongs to that exchange.
        var lastRaw: Turn?
        var lastIndex = 0
        var reopenedAt: Int?

        func close() {
            guard var t = current else { return }
            lastRaw = t
            if !t.wakes.isEmpty { t.pickAnswerAcrossWakes() }
            // The answer is the last thing the assistant said; everything before it is process.
            // (Steps after the final words, rare trailing tool calls, stay in the process.)
            t.steps = t.process
            if let i = t.process.lastIndex(where: { if case .assistant = $0 { true } else { false } }) {
                t.answer = t.process.remove(at: i)
            }
            if let i = reopenedAt {
                blocks.insert(.turn(t), at: i) // back in its place, the recap card still after it
                lastIndex = i
            } else {
                lastIndex = blocks.count
                blocks.append(.turn(t))
            }
            reopenedAt = nil
            current = nil
        }

        for row in rows {
            switch row {
            case .user(let item):
                if item.queued == true, current != nil {
                    current!.users.append(row) // queued input joins the turn it was sent during
                    current!.ordered.append(row)
                } else {
                    close()
                    current = Turn(id: row.id, users: [row], ordered: [row])
                }
            case .recap:
                close()
                blocks.append(.row(row))
            case .notice(let item):
                // Inside a turn: "interrupted" closes it visibly; compaction / API errors are part of the process
                // (auto-compaction happens mid-turn and must not split it).
                if item.wake == true, current == nil, let raw = lastRaw, blocks.indices.contains(lastIndex),
                   case .turn(let closed) = blocks[lastIndex], closed.id == raw.id {
                    blocks.remove(at: lastIndex) // reopen the exchange the recap closed
                    reopenedAt = lastIndex
                    current = raw
                }
                if item.wake == true, current != nil {
                    // A background task woke the idle agent: still the same exchange (the answer is picked at close).
                    current!.wakes.append(item)
                    current!.process.append(row)
                    current!.ordered.append(row)
                } else if current == nil {
                    // e.g. a background command finishing after a recap: part of the (next) work, never its own line.
                    current = Turn(id: row.id)
                    current!.process.append(row)
                    current!.ordered.append(row)
                } else if (item.text ?? "").contains("中断") {
                    current!.trailing.append(row)
                } else {
                    current!.process.append(row)
                    current!.ordered.append(row)
                }
            default:
                if current == nil { current = Turn(id: row.id) }
                current!.process.append(row)
                current!.ordered.append(row)
            }
        }
        close()
        return blocks
    }
}

/// Transcript items and live state of one session, shared by every view showing it.
@Observable
final class ChatStore {
    let key: SessionKey
    private(set) var items: [Item] = []
    private(set) var rows: [ChatRow] = []
    private(set) var blocks: [ChatBlock] = []
    /// Bumped whenever `blocks` is rebuilt: views key derived data (the chat's lines) on it instead of diffing blocks.
    @ObservationIgnored private(set) var version = 0
    private(set) var hasMore = false
    private(set) var loadedOnce = false
    var screen: String = ""
    /// Text of the assistant message being generated (headless sessions stream it token by token).
    var liveText: String = ""
    var screenStatus: SessionStatus = .unknown
    var screenUpdatedAt: Date?
    var error: String?
    var isLoadingOlder = false
    /// Messages sent from this phone that the transcript has not shown yet (drawn at the end right away).
    var pending: [PendingMessage] = []

    @ObservationIgnored private var ids = Set<String>()

    init(key: SessionKey) {
        self.key = key
        // Paint the last exchanges from the disk cache right away; the server's backlog merges in after.
        if let cached = ChatCache.load(key), !cached.items.isEmpty {
            items = cached.items
            ids = Set(cached.items.map(\.id))
            hasMore = cached.hasMore
            rows = ChatStore.buildRows(items)
            blocks = ChatBlock.build(rows)
            version += 1
        }
    }

    var lastSeq: Int { items.last?.seq ?? 0 }

    func merge(_ new: [Item], backlog: Bool = false, hasMore: Bool? = nil) {
        if backlog || !new.isEmpty { error = nil } // data arrived: a stale "machine is offline" no longer applies
        var changed = false
        var inOrder = true
        for it in Self.withImageRefs(new, key) where !ids.contains(it.id) {
            if let last = items.last, (it.seq, it.id) < (last.seq, last.id) { inOrder = false }
            ids.insert(it.id)
            items.append(it)
            changed = true
        }
        // "Nothing older" only counts from a page that reaches our oldest item: a catch-up batch (newer items only)
        // saying so had the list stop at its oldest loaded message with no way further up.
        let reachesStart = new.map(\.seq).min().map { $0 <= (items.first?.seq ?? .max) } ?? false
        if let hasMore, backlog, !loadedOnce || items.count <= new.count, hasMore || reachesStart { self.hasMore = hasMore }
        if backlog { loadedOnce = true }
        guard changed else { return }
        if !pending.isEmpty { // the real message arrived: drop the local copy
            let typed = new.filter { $0.role == "user" && $0.kind == .text }.map { $0.text ?? "" }
            pending.removeAll { p in typed.contains { PendingMessage.same($0, p.text) } }
        }
        // Streamed items arrive in order: only older pages / reconnect backlogs need the sort.
        if !inOrder { items.sort { $0.seq == $1.seq ? $0.id < $1.id : $0.seq < $1.seq } }
        scheduleRebuild()
        scheduleSave()
    }

    @ObservationIgnored private var rebuildScheduled = false

    /// Rows and blocks are rebuilt from all items; a burst of events (a tool call, its result, the next thought)
    /// is rebuilt and redrawn once, on the next turn of the main actor, instead of once per event.
    private func scheduleRebuild() {
        guard !rebuildScheduled else { return }
        rebuildScheduled = true
        Task { [weak self] in
            guard let self else { return }
            self.rebuildScheduled = false
            self.rows = ChatStore.buildRows(self.items)
            self.blocks = ChatBlock.build(self.rows)
            self.version += 1
        }
    }

    @ObservationIgnored private var saveTask: Task<Void, Never>?

    /// Writes the last two exchanges to the disk cache, a moment after the latest change.
    private func scheduleSave() {
        saveTask?.cancel()
        let key = key
        saveTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(2))
            guard !Task.isCancelled, let self else { return }
            let tail = ChatCache.tail(self.items, turns: 2)
            let more = self.hasMore || tail.count < self.items.count
            Task.detached(priority: .utility) { ChatCache.save(key, items: tail, hasMore: more) }
        }
    }

    /// Lazy images learn where to fetch their bytes from.
    nonisolated static func withImageRefs(_ items: [Item], _ key: SessionKey) -> [Item] {
        guard items.contains(where: { $0.image?.lazy == true }) else { return items }
        return items.map { it in
            var it = it
            if it.image?.lazy == true { it.image?.ref = "\(key.machineID)/\(key.sessionID)/\(it.id)" }
            return it
        }
    }

    /// Starts over from the latest page: what came in between was too much to fetch, and a hole would never fill.
    func replace(with latest: [Item], hasMore: Bool) {
        items = []
        ids = []
        self.hasMore = hasMore
        merge(latest)
    }

    func prepend(_ older: [Item], hasMore: Bool) {
        self.hasMore = hasMore
        merge(older)
    }

    /// Live preview: the lines Claude is currently printing, cut above the input box.
    var livePreview: [String] { ScreenParser.activity(screen) }
    var spinnerLine: String? { ScreenParser.spinner(screen) }

    static func buildRows(_ items: [Item]) -> [ChatRow] {
        var results: [String: Item] = [:]
        var images: [String: [ImagePayload]] = [:]
        for it in items {
            guard let tid = it.toolUseID else { continue }
            if it.kind == .toolResult { results[tid] = it }
            if it.kind == .image, let img = it.image { images[tid, default: []].append(img) }
        }
        var rows: [ChatRow] = []
        var run: [ToolStep] = []
        var pendingThoughts: [Item] = []

        func flushThoughts(asRows: Bool) {
            if asRows { rows.append(contentsOf: pendingThoughts.map { .thinking($0) }) }
            else { run.append(contentsOf: pendingThoughts.map { .thought($0) }) }
            pendingThoughts.removeAll()
        }
        func flushRun() {
            if !run.isEmpty { rows.append(.tools(run)); run.removeAll() }
        }

        for it in items {
            switch it.kind {
            case .toolUse:
                flushThoughts(asRows: run.isEmpty)
                var call = ToolCall(use: it, result: it.toolUseID.flatMap { results[$0] })
                call.images = it.toolUseID.flatMap { images[$0] } ?? []
                run.append(.call(call))
            case .thinking:
                pendingThoughts.append(it)
            case .toolResult:
                continue
            case .image where it.toolUseID != nil:
                continue
            default:
                if run.isEmpty { flushThoughts(asRows: true) } else { flushThoughts(asRows: false); flushRun() }
                switch it.kind {
                case .text: rows.append(it.role == "user" ? .user(it) : .assistant(it))
                case .notice: rows.append(.notice(it))
                case .recap: rows.append(.recap(it))
                case .image: rows.append(.image(it))
                default: break
                }
            }
        }
        if !run.isEmpty {
            flushThoughts(asRows: false)
            flushRun()
        } else {
            flushThoughts(asRows: true)
        }
        return rows
    }
}

/// Extracts useful bits from Claude Code's terminal screen.
nonisolated enum ScreenParser {
    private static let spinnerGlyphs: Set<Character> = ["✻", "✶", "✳", "✢", "✽", "·", "*", "⏺", "◐", "◓", "◑", "◒"]

    /// "Cogitating… (12s · ↓ 3.2k tokens)" from the status line above the input box.
    static func spinner(_ screen: String) -> String? {
        for line in screen.split(separator: "\n", omittingEmptySubsequences: true).reversed() {
            let t = line.trimmingCharacters(in: .whitespaces)
            guard let first = t.first, spinnerGlyphs.contains(first), t.contains("…") else { continue }
            var s = String(t.dropFirst()).trimmingCharacters(in: .whitespaces)
            s = s.replacingOccurrences(of: " · esc to interrupt", with: "").replacingOccurrences(of: "esc to interrupt", with: "")
            return s.replacingOccurrences(of: "()", with: "").trimmingCharacters(in: .whitespaces)
        }
        return nil
    }

    /// The last few content lines above the prompt box (what is streaming right now).
    static func activity(_ screen: String, max: Int = 8) -> [String] {
        var lines = screen.components(separatedBy: "\n")
        // Cut at the first horizontal rule from the bottom that starts the input box.
        if let idx = lines.lastIndex(where: { isRule($0) }), let top = lines[..<idx].lastIndex(where: { isRule($0) }) {
            lines = Array(lines[..<top])
        } else if let idx = lines.lastIndex(where: { isRule($0) }) {
            lines = Array(lines[..<idx])
        }
        let content = lines
            .map { $0.replacingOccurrences(of: "\u{00a0}", with: " ") }
            .filter { line in
                let t = line.trimmingCharacters(in: .whitespaces)
                return !t.isEmpty && spinner(t) == nil
            }
        return Array(content.suffix(max))
    }

    static func isRule(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        return t.count > 20 && t.allSatisfy { $0 == "─" || $0 == "━" || $0 == "-" }
    }

    /// Numbered options on a TUI dialog ("❯ 1. Yes", "2. No") → (number, label).
    static func options(_ screen: String) -> [(key: String, label: String, selected: Bool)] {
        var out: [(String, String, Bool)] = []
        for raw in screen.components(separatedBy: "\n") {
            var t = raw.trimmingCharacters(in: .whitespaces)
            var selected = false
            if t.hasPrefix("❯") { selected = true; t = String(t.dropFirst()).trimmingCharacters(in: .whitespaces) }
            guard let dot = t.firstIndex(of: "."), let n = Int(t[..<dot]), n >= 1, n <= 9 else { continue }
            let label = t[t.index(after: dot)...].trimmingCharacters(in: .whitespaces)
            guard !label.isEmpty else { continue }
            out.append((String(n), label, selected))
        }
        // Keep only the trailing contiguous 1…N block (the active dialog).
        if let lastOne = out.lastIndex(where: { $0.0 == "1" }) { out = Array(out[lastOne...]) }
        return out
    }
}

/// On-disk copy of each conversation's last exchanges (Caches/chats), so opening or switching to a chat never
/// shows a blank page while the hub catches up.
nonisolated enum ChatCache {
    private struct Entry: Codable { var items: [Item]; var hasMore: Bool }
    private static let maxBytes = 6 << 20

    private static func url(_ key: SessionKey) -> URL? {
        guard let base = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask).first else { return nil }
        let safe = "\(key.machineID)_\(key.sessionID)".map { $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" ? $0 : "_" }
        // v2: items carry `wake`; older copies would keep the merged turns until they scrolled out.
        return base.appending(path: "chats-v2").appending(path: String(safe) + ".json")
    }

    static func load(_ key: SessionKey) -> (items: [Item], hasMore: Bool)? {
        guard let url = url(key), let data = try? Data(contentsOf: url),
              let e = try? JSON.decoder.decode(Entry.self, from: data) else { return nil }
        return (e.items, e.hasMore)
    }

    static func save(_ key: SessionKey, items: [Item], hasMore: Bool) {
        guard let url = url(key), !items.isEmpty else { return }
        var entry = Entry(items: items, hasMore: hasMore)
        guard var data = try? JSON.encoder.encode(entry) else { return }
        if data.count > maxBytes { // screenshots make turns huge: keep the words, drop the pictures
            entry.items = items.filter { $0.kind != .image }
            guard let slim = try? JSON.encoder.encode(entry) else { return }
            data = slim
        }
        try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try? data.write(to: url, options: .atomic)
    }

    /// Items from the start of the `turns`-th last user prompt on.
    static func tail(_ items: [Item], turns: Int) -> [Item] {
        var seen = 0
        for i in items.indices.reversed() where items[i].role == "user" && items[i].kind == .text && items[i].queued != true {
            seen += 1
            if seen == turns { return Array(items[i...]) }
        }
        return items
    }
}

/// A message on its way: shown at once, the request runs in the background.
nonisolated struct PendingMessage: Identifiable, Hashable, Sendable {
    enum State: Hashable, Sendable { case sending, sent, failed }
    var id = UUID()
    var text: String
    var images: [ImagePayload]
    var state: State = .sending

    /// Comparable form: what the transcript keeps of it — the phone's image note, pasted wrappers and Claude's
    /// "[Image #21]" placeholders removed, whitespace ignored.
    static func normalize(_ s: String) -> String {
        var t = s
        for pattern in [#"<attached_images>[\s\S]*?</attached_images>"#, #"<[\\/]*pasted_content[^>]*>"#, #"\[Image #\d+\]"#, #"\s+"#] {
            t = t.replacingOccurrences(of: pattern, with: "", options: .regularExpression)
        }
        return t
    }

    /// The transcript's copy of a message sent from here (equal once normalized, or one containing the other:
    /// Claude adds placeholders and the phone adds notes).
    static func same(_ a: String, _ b: String) -> Bool {
        let x = normalize(a), y = normalize(b)
        guard !x.isEmpty, !y.isEmpty else { return false }
        return x == y || x.contains(y) || y.contains(x)
    }
}

