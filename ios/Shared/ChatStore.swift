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

nonisolated extension ChatRow {
    /// A message from the user, or a picture sent with one.
    var isUserInput: Bool {
        switch self {
        case .user: true
        case .image(let i): i.role == "user" && i.toolUseID == nil
        default: false
        }
    }
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

/// A step inside a run of tool activity: a call, a thought between calls, or a run of them the page left out
/// (`Item.fold`, fetched when the work line is opened).
nonisolated enum ToolStep: Identifiable, Hashable, Sendable {
    case call(ToolCall)
    case thought(Item)
    case folded(Item)

    var id: String {
        switch self {
        case .call(let c): c.id
        case .thought(let i), .folded(let i): i.id
        }
    }
    var firstTS: Date? {
        switch self {
        case .call(let c): c.use.ts
        case .thought(let i), .folded(let i): i.ts
        }
    }
    var lastTS: Date? {
        switch self {
        case .call(let c): c.result?.ts ?? c.use.ts
        case .thought(let i): i.ts
        case .folded(let i): i.fold?.until ?? i.ts
        }
    }
    /// Tool calls and thoughts in it (a fold counts what it stands for).
    var tools: Int {
        switch self {
        case .call: 1
        case .thought: 0
        case .folded(let i): i.fold?.tools ?? 0
        }
    }
    var thoughts: Int {
        switch self {
        case .call: 0
        case .thought: 1
        case .folded(let i): i.fold?.thoughts ?? 0
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
            if case .tools(let steps) = r { return n + steps.reduce(0) { $0 + $1.tools } }
            return n
        }
    }
    var thoughtCount: Int {
        process.reduce(0) { n, r in
            switch r {
            case .thinking: return n + 1
            case .tools(let steps): return n + steps.reduce(0) { $0 + $1.thoughts }
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
        // What the user sent stays with the exchange (their pictures too: they sit above the message they came with).
        if let first = all.firstIndex(where: { if case .notice(let n) = $0 { n.wake == true } else { false } }) {
            afterWake = Array(all[first...]).filter { !$0.isUserInput }
        }
        guard let pick else { return }
        tail = all[(pick + 1)...].filter { !$0.isUserInput }
        ordered = Array(all[...pick]) + all[(pick + 1)...].filter(\.isUserInput)
        process = ordered.filter { if case .user = $0 { false } else { true } }
    }
}

/// Background tasks and the calls that started them: a notice names its call by tool_use_id, or only by the task id
/// the call's result announced ("running in background with ID: …", "Monitor started (task …"). Built with the
/// rows; a call outside the loaded items is simply not found.
nonisolated struct TaskIndex: Equatable, Sendable {
    /// A call's notices, oldest first, by the call's tool_use_id.
    var notes: [String: [TaskNote]] = [:]
    /// The call a notice came from (notice item id → tool_use_id).
    var calls: [String: String] = [:]
    /// Calls whose result says they went to the background (they may not have reported back yet).
    var started: Set<String> = []
    /// What each of those calls was: its description, and when it started.
    var descriptions: [String: String] = [:]
    var startedAt: [String: Date] = [:]

    nonisolated(unsafe) private static let launched = /(?:running in background with ID|agentId|Monitor started \(task|moved to the background \(ID):?\s*([A-Za-z0-9_-]+)/

    static func build(_ items: [Item]) -> TaskIndex {
        var x = TaskIndex()
        var byTask: [String: String] = [:]
        var uses: [String: Item] = [:]
        for it in items {
            switch it.kind {
            case .toolUse:
                if let id = it.toolUseID { uses[id] = it }
            case .toolResult:
                guard let id = it.toolUseID, let text = it.text, text.contains("background") || text.contains("Monitor started") || text.contains("agentId"),
                      let m = text.firstMatch(of: launched) else { continue }
                byTask[String(m.1)] = id
                x.started.insert(id)
                if let use = uses[id] {
                    x.descriptions[id] = use.toolInput?["description"]?.string
                    x.startedAt[id] = use.ts
                }
            case .notice:
                guard let n = it.task, let call = n.toolUseID ?? n.taskID.flatMap({ byTask[$0] }) else { continue }
                x.notes[call, default: []].append(n)
                x.calls[it.id] = call
                if x.descriptions[call] == nil, let use = uses[call] {
                    x.descriptions[call] = use.toolInput?["description"]?.string
                    x.startedAt[call] = use.ts
                }
            default: break
            }
        }
        return x
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
                } else if item.task == nil, (item.text ?? "").contains("中断") {
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

/// Where a batch of items sits in the transcript (the hub's `history` / `from_seq` / `to_seq`; nil from older hubs).
nonisolated struct ItemSpan: Sendable, Equatable {
    /// The version of the transcript file; another one means it was replaced and its seqs started over.
    var history: String?
    /// The seq the batch continues from (catch-up, live pushes, after_seq pages); nil for pages from the end.
    var from: Int?
    /// The last line the batch accounts for: lines without items count, so it is often past its last item.
    var to: Int?
}

nonisolated extension Item {
    /// The block within its transcript line (ids end in "#<n>").
    var part: Int { Self.part(of: id) }

    static func part(of id: String) -> Int {
        guard let hash = id.lastIndex(of: "#") else { return 0 }
        return Int(id[id.index(after: hash)...]) ?? 0
    }

    /// Work a fold may stand for: tool calls, their results and images, thoughts, folds.
    var isWork: Bool {
        switch kind {
        case .thinking, .toolUse, .toolResult, .fold: true
        case .image: toolUseID != nil
        default: false
        }
    }

    /// This fold's range holds `other` (by line, then block).
    func folds(_ other: Item) -> Bool {
        guard let f = fold else { return false }
        return (other.seq, other.part) >= (seq, part) && (other.seq, other.part) <= (f.throughSeq, Self.part(of: f.throughID))
    }
}

/// Transcript items and live state of one session, shared by every view showing it.
@Observable
final class ChatStore {
    let key: SessionKey
    private(set) var items: [Item] = []
    private(set) var rows: [ChatRow] = []
    private(set) var blocks: [ChatBlock] = []
    /// Every image in the conversation (the viewer pages through them), rebuilt with `blocks`: a view that derived it
    /// from `items` in its body redrew on every item.
    private(set) var images: [ImagePayload] = []
    /// Background tasks and their calls (`TaskIndex`), rebuilt with `blocks`.
    private(set) var tasks = TaskIndex()
    /// Bumped when the items start over (another history, or the latest page replacing what was here): a frozen view
    /// of the old items (`ChatView.Frozen`) means nothing any more.
    private(set) var resets = 0
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
    /// The version of the transcript the items come from (`ItemSpan.history`). Another one means the file was replaced
    /// and its seqs started over: everything here is stale.
    @ObservationIgnored private(set) var history: String?
    /// Everything from the first item through this seq is here, without holes (lines without items count). Catch-ups
    /// ask for what comes after it; 0 = no baseline yet (nothing loaded from the end of the transcript).
    @ObservationIgnored private(set) var covered = 0

    init(key: SessionKey) {
        self.key = key
        // Paint the last exchanges from the disk cache right away; the server's backlog merges in after.
        if let cached = ChatCache.load(key), !cached.items.isEmpty {
            items = cached.items
            ids = Set(cached.items.map(\.id))
            hasMore = cached.hasMore
            history = cached.history
            covered = cached.covered ?? (cached.items.last?.seq ?? 0)
            rows = ChatStore.buildRows(items)
            blocks = ChatBlock.build(rows)
            tasks = TaskIndex.build(items)
            version += 1
        }
    }

    var lastSeq: Int { items.last?.seq ?? 0 }

    /// What a batch left to do: nothing, fetch what comes after `covered` (a hole, or a catch-up the hub capped), or
    /// start over from the latest page (the transcript was replaced under a continuing batch).
    enum Gap: Equatable { case none, fill, reload }

    /// Takes a batch of items with its place in the transcript. Batches that continue from a seq must start inside
    /// what is here (`span.from <= covered`) — one that does not reveals a hole, which `fill` fetches; its items are
    /// kept meanwhile. A page counted back from the end that does not reach what is here replaces it (an old disk
    /// cache, several exchanges behind). Without a span (an older hub) batches are trusted, as before.
    @discardableResult
    func apply(_ new: [Item], span: ItemSpan, backlog: Bool = false, hasMore: Bool? = nil, moreAfter: Bool = false) -> Gap {
        if let h = span.history, let mine = history, h != mine {
            reset()
            history = h
            if let from = span.from, from > 0 { return .reload } // the rest of a file we never saw from its start
        }
        if let h = span.history { history = h }
        guard let to = span.to else {
            merge(new, backlog: backlog, hasMore: hasMore)
            covered = max(covered, lastSeq)
            return moreAfter ? .reload : .none
        }
        if let from = span.from {
            merge(new, backlog: backlog, hasMore: hasMore)
            // No baseline yet: the backlog from the end sets one. A batch from line 0 (a new session watched from its
            // first line) is its own baseline: without it, holes after it went unnoticed. If the backlog already came
            // and set none (the transcript did not exist yet) and the first lines' push was lost, only the latest
            // page tells where we are.
            if covered == 0 && from > 0 { return loadedOnce ? .reload : .none }
            guard from <= covered else { return .fill }
            covered = max(covered, to)
            return moreAfter ? .fill : .none
        }
        // A page from the end.
        if covered > 0, let first = new.first?.seq, first > covered + 1 {
            replace(with: new, hasMore: hasMore ?? true, span: span)
            if backlog { loadedOnce = true }
            return .none
        }
        merge(new, backlog: backlog, hasMore: hasMore)
        covered = max(covered, to)
        return .none
    }

    private func reset() {
        resets += 1
        items = []
        ids = []
        covered = 0
        hasMore = false
        loadedOnce = false
        scheduleRebuild()
    }

    func merge(_ new: [Item], backlog: Bool = false, hasMore: Bool? = nil) {
        if backlog || !new.isEmpty { error = nil } // data arrived: a stale "machine is offline" no longer applies
        var changed = dropFolded(by: new)
        var inOrder = true
        for it in Self.withImageRefs(new, key) where !ids.contains(it.id) {
            if let last = items.last, (it.seq, it.part) < (last.seq, last.part) { inOrder = false }
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
        if !inOrder { items = Self.ordered(items) }
        scheduleRebuild()
        scheduleSave()
    }

    /// A fold stands for the work in its range: what is here of that work (steps, or the same fold over a shorter
    /// range from an older page) gives way to it.
    private func dropFolded(by new: [Item]) -> Bool {
        let folds = new.filter { f in f.kind == .fold && !items.contains { $0.id == f.id && $0.fold == f.fold } }
        guard !folds.isEmpty else { return false }
        let before = items.count
        items.removeAll { it in it.isWork && folds.contains { $0.folds(it) } }
        guard items.count != before else { return false }
        ids = Set(items.map(\.id))
        settled = nil
        return true
    }

    /// The work a fold stood for, fetched (`AppModel.unfold`): it takes the fold's place.
    func unfold(_ fold: Item, into work: [Item]) {
        guard let i = items.firstIndex(where: { $0.id == fold.id }) else { return }
        items.remove(at: i)
        ids.remove(fold.id)
        settled = nil
        merge(work.filter { $0.kind != .fold })
        scheduleRebuild()
    }

    @ObservationIgnored private var rebuildScheduled = false
    @ObservationIgnored private var rebuildRevision = 0
    @ObservationIgnored private var settled: SettledRows?

    /// The rows and blocks of everything before the last exchange, reused while items only grow at the end (a live
    /// session): rebuilding all of them on every event took 30 ms at 1000 exchanges.
    nonisolated struct SettledRows: Sendable {
        var count: Int
        var firstID: String
        var lastID: String
        var rows: [ChatRow]
        var blocks: [ChatBlock]
    }

    /// `buildRows` + `ChatBlock.build`, redoing only what comes after `settled` when that still holds. An exchange
    /// starts at a prompt and nothing before it changes when items are appended — unless a tool result (or a tool's
    /// image) arrives for a call before it; then everything is built again.
    nonisolated static func rebuild(_ items: [Item], settled: SettledRows?) -> (rows: [ChatRow], blocks: [ChatBlock], settled: SettledRows?) {
        func full() -> (rows: [ChatRow], blocks: [ChatBlock], settled: SettledRows?) {
            let rows = buildRows(items)
            return (rows, ChatBlock.build(rows), nil)
        }
        guard let cut = items.lastIndex(where: { $0.role == "user" && $0.kind == .text && $0.queued != true }), cut > 0,
              standsAlone(items[cut...]) else { return full() }
        var base = settled
        // Still the same items up to it: an older page or a sorted-in item moves them.
        if let b = base, !(b.count <= cut && items.first?.id == b.firstID && items[b.count - 1].id == b.lastID && standsAlone(items[b.count...])) {
            base = nil
        }
        var rows = base?.rows ?? []
        var blocks = base?.blocks ?? []
        let start = base?.count ?? 0
        if start < cut {
            let more = buildRows(Array(items[start..<cut]))
            rows += more
            blocks += ChatBlock.build(more)
        }
        let next = SettledRows(count: cut, firstID: items[0].id, lastID: items[cut - 1].id, rows: rows, blocks: blocks)
        let tail = buildRows(Array(items[cut...]))
        return (rows + tail, blocks + ChatBlock.build(tail), next)
    }

    /// Every tool result and tool image in the slice belongs to a call in it.
    nonisolated private static func standsAlone(_ items: ArraySlice<Item>) -> Bool {
        var calls = Set<String>()
        for it in items {
            guard let tid = it.toolUseID else { continue }
            if it.kind == .toolUse { calls.insert(tid) } else if !calls.contains(tid) { return false }
        }
        return true
    }

    /// Coalesces events into one background worker; only a current snapshot is published on the main actor.
    private func scheduleRebuild() {
        rebuildRevision += 1
        guard !rebuildScheduled else { return }
        rebuildScheduled = true
        Task { [weak self] in
            guard let self else { return }
            while true {
                let revision = self.rebuildRevision
                let items = self.items, settled = self.settled
                let (built, images, tasks) = await Task.detached(priority: .userInitiated) {
                    (ChatStore.rebuild(items, settled: settled), items.compactMap(\.image), TaskIndex.build(items))
                }.value
                guard revision == self.rebuildRevision else { continue }
                self.rows = built.rows
                self.blocks = built.blocks
                self.settled = built.settled
                if images.map(\.identity) != self.images.map(\.identity) { self.images = images }
                if tasks != self.tasks { self.tasks = tasks }
                self.version += 1
                self.rebuildScheduled = false
                return
            }
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
            let history = self.history, covered = self.covered
            Task.detached(priority: .utility) { ChatCache.save(key, items: tail, hasMore: more, history: history, covered: covered) }
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
    func replace(with latest: [Item], hasMore: Bool, span: ItemSpan = ItemSpan()) {
        // Pushes that came in while the page was on its way, past what it holds, stay (the next push finds the hole
        // between, if any). Not after another history: those seqs belong to the old file.
        let keep = span.history == nil || span.history == history ? items.filter { $0.seq > (span.to ?? .max) } : []
        resets += 1
        items = []
        ids = []
        self.hasMore = hasMore
        if let h = span.history { history = h }
        merge(latest)
        merge(keep)
        covered = span.to ?? lastSeq
        scheduleRebuild() // also when the page is empty
    }

    /// Items in transcript order: by line, then by block within the line (ids end in "#<block>"; "#10" after "#2").
    nonisolated static func ordered(_ items: [Item]) -> [Item] {
        items.sorted { ($0.seq, $0.part) < ($1.seq, $1.part) }
    }

    func prepend(_ older: [Item], hasMore: Bool) {
        self.hasMore = hasMore
        merge(older)
    }

    /// Live preview: the lines Claude is currently printing, cut above the input box.
    var livePreview: [String] { ScreenParser.activity(screen) }
    var spinnerLine: String? { ScreenParser.spinner(screen) }

    nonisolated static func buildRows(_ items: [Item]) -> [ChatRow] {
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
            case .fold:
                flushThoughts(asRows: run.isEmpty)
                run.append(.folded(it))
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

    /// A choice the terminal is waiting on (AskUserQuestion without the hook, a permission prompt…), read off the
    /// screen: its header chip, the question, numbered options with their descriptions.
    struct Dialog: Equatable, Sendable {
        enum Kind: Equatable, Sendable { case plain, typeSomething, chat }
        struct Option: Equatable, Sendable {
            var key: String
            var label: String
            var detail: String?
            var kind: Kind = .plain
        }
        /// A step of a multi-question AskUserQuestion: the TUI's tab bar "←  ☐ 审核策略  ✔ 推送代码  ✔ Submit  →".
        struct Step: Equatable, Sendable {
            var label: String
            var done: Bool
        }
        var header: String?
        var question: String
        /// Lines above the question that belong to it (the command a permission prompt is about).
        var detail: String?
        var options: [Option]
        /// The questions of a multi-question dialog, from its tab bar (taken out of `detail`, where it read as text).
        var steps: [Step] = []

        init(header: String?, question: String, detail: String?, options: [Option]) {
            self.header = header
            self.question = question
            self.options = options
            let (steps, rest) = ScreenParser.steps(in: detail)
            self.steps = steps
            self.detail = rest
        }
    }

    /// The tab bar of a multi-question dialog out of the lines above the question: its steps (Submit dropped), and the
    /// lines left (nil when none).
    static func steps(in detail: String?) -> ([Dialog.Step], String?) {
        guard let detail, !detail.isEmpty else { return ([], nil) }
        let marks: Set<Character> = ["☐", "□", "☒", "✔", "◻", "■", "✓"]
        var steps: [Dialog.Step] = []
        var rest: [String] = []
        for line in detail.components(separatedBy: "\n") {
            let count = line.filter { marks.contains($0) }.count
            guard count >= 2 || (count >= 1 && (line.contains("←") || line.contains("→"))) else { rest.append(line); continue }
            var label = "", mark: Character?
            func flush() {
                let t = label.trimmingCharacters(in: .whitespaces)
                if let m = mark, !t.isEmpty, t.lowercased() != "submit" { steps.append(.init(label: t, done: m == "☒" || m == "✔" || m == "■" || m == "✓")) }
                label = ""
            }
            for c in line where c != "←" && c != "→" {
                if marks.contains(c) { flush(); mark = c } else { label.append(c) }
            }
            flush()
        }
        return (steps, rest.isEmpty ? nil : rest.joined(separator: "\n"))
    }

    /// The dialog at the bottom of the screen, or nil when there is none (plain options or none at all).
    static func dialog(_ screen: String) -> Dialog? {
        let lines = screen.components(separatedBy: "\n")
        // Numbered rows: (line index, number, label, indent of the label).
        var rows: [(i: Int, n: Int, label: String, indent: Int)] = []
        for (i, raw) in lines.enumerated() {
            var t = Substring(raw)
            let lead = t.prefix { $0 == " " }.count
            t = t.dropFirst(lead)
            var indent = lead
            if t.hasPrefix("❯") || t.hasPrefix(">") {
                t = t.dropFirst()
                let sp = t.prefix { $0 == " " }.count
                t = t.dropFirst(sp)
                indent += 1 + sp
            }
            guard let dot = t.firstIndex(of: "."), let n = Int(t[..<dot]), (1...9).contains(n) else { continue }
            let after = t[t.index(after: dot)...]
            guard after.hasPrefix(" ") else { continue }
            let label = after.trimmingCharacters(in: .whitespaces)
            guard !label.isEmpty else { continue }
            rows.append((i, n, label, indent + t.distance(from: t.startIndex, to: dot) + 2))
        }
        // The last run 1, 2, 3… is the live dialog.
        guard let start = rows.lastIndex(where: { $0.n == 1 }) else { return nil }
        var block = [rows[start]]
        for r in rows[(start + 1)...] where r.n == block.last!.n + 1 { block.append(r) }
        guard block.count >= 2 else { return nil }

        var options: [Dialog.Option] = []
        for (k, r) in block.enumerated() {
            let end = k + 1 < block.count ? block[k + 1].i : min(lines.count, r.i + 6)
            var detail: [String] = []
            for line in lines[(r.i + 1)..<end] {
                let lead = line.prefix { $0 == " " }.count
                let t = line.trimmingCharacters(in: .whitespaces)
                if t.isEmpty || isRule(t) || lead < r.indent - 1 { if !detail.isEmpty { break } else { continue } }
                detail.append(t)
            }
            let lower = r.label.lowercased()
            let kind: Dialog.Kind = lower.hasPrefix("type something") ? .typeSomething : lower.hasPrefix("chat about this") ? .chat : .plain
            options.append(Dialog.Option(key: String(r.n), label: r.label, detail: detail.isEmpty ? nil : joinWrapped(detail), kind: kind))
        }

        // Above the options, up to the rule that opens the dialog.
        let first = block[0].i
        var top = 0
        for i in stride(from: first - 1, through: 0, by: -1) where isRule(lines[i]) { top = i + 1; break }
        var header: String?
        var quoted: [String] = []
        var plain: [String] = []
        for line in lines[top..<first] {
            let t = line.trimmingCharacters(in: .whitespaces)
            if t.isEmpty { continue }
            if let c = t.first, "☐□☒✔◻■".contains(c) {
                header = t.dropFirst().trimmingCharacters(in: .whitespaces)
            } else if t.hasPrefix("│") || t.hasPrefix("|") {
                quoted.append(t.dropFirst().trimmingCharacters(in: .whitespaces))
            } else {
                plain.append(t)
            }
        }
        let question: String
        var detail: String?
        if !quoted.isEmpty {
            question = joinWrapped(quoted)
            if !plain.isEmpty { detail = plain.joined(separator: "\n") }
        } else if let last = plain.last {
            question = last
            if plain.count > 1 { detail = plain.dropLast().joined(separator: "\n") }
        } else {
            question = header ?? ""
            if header != nil { header = nil }
        }
        return Dialog(header: header, question: question, detail: detail, options: options)
    }

    /// Rows the terminal wrapped, back into one text: no space where a row ended inside CJK text.
    static func joinWrapped(_ rows: [String]) -> String {
        var out = ""
        for r in rows {
            if let last = out.last, let next = r.first, !(last.isCJK || next.isCJK) { out += " " }
            out += r
        }
        return out
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
    private struct Entry: Codable {
        var items: [Item]
        var hasMore: Bool
        var history: String?
        var covered: Int?
    }
    private static let maxBytes = 6 << 20

    private static func url(_ key: SessionKey) -> URL? {
        guard let base = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask).first else { return nil }
        let safe = "\(key.machineID)_\(key.sessionID)".map { $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" ? $0 : "_" }
        // v2: items carry `wake`; older copies would keep the merged turns until they scrolled out. v3: notices carry
        // `task` (a page's copy of an item never replaces the cached one: old notices stayed centered blocks).
        return base.appending(path: "chats-v3").appending(path: String(safe) + ".json")
    }

    static func load(_ key: SessionKey) -> (items: [Item], hasMore: Bool, history: String?, covered: Int?)? {
        guard let url = url(key), let data = try? Data(contentsOf: url),
              let e = try? JSON.decoder.decode(Entry.self, from: data) else { return nil }
        return (e.items, e.hasMore, e.history, e.covered)
    }

    static func remove(_ key: SessionKey) {
        guard let url = url(key) else { return }
        try? FileManager.default.removeItem(at: url)
    }

    static func save(_ key: SessionKey, items: [Item], hasMore: Bool, history: String? = nil, covered: Int? = nil) {
        guard let url = url(key), !items.isEmpty else { return }
        var entry = Entry(items: items, hasMore: hasMore, history: history, covered: covered)
        guard var data = try? JSON.encoder.encode(entry) else { return }
        if data.count > maxBytes { // screenshots make turns huge: keep the words, drop the pictures
            entry.items = items.filter { $0.kind != .image }
            guard let slim = try? JSON.encoder.encode(entry) else { return }
            data = slim
        }
        try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try? data.write(to: url, options: .atomic)
    }

    /// Items from the start of the `turns`-th last user prompt on (never half a transcript line: what a page before
    /// its first seq would bring back leaves out the rest of that line).
    static func tail(_ items: [Item], turns: Int) -> [Item] {
        var seen = 0
        for i in items.indices.reversed() where items[i].role == "user" && items[i].kind == .text && items[i].queued != true {
            seen += 1
            if seen == turns {
                var start = i
                while start > 0 && items[start - 1].seq == items[start].seq { start -= 1 }
                return Array(items[start...])
            }
        }
        return items
    }
}

/// A message on its way: shown at once, the request runs in the background.
nonisolated struct PendingMessage: Identifiable, Hashable, Codable, Sendable {
    /// waiting: no network or no hub; it goes by itself once the app is connected and caught up (AppModel.flushOutbox).
    enum State: Hashable, Codable, Sendable { case sending, sent, failed, waiting }
    var id = UUID()
    var text: String
    var images: [ImagePayload]
    var state: State = .sending

    /// Comparable form: what the transcript keeps of it — the phone's image note, pasted wrappers and Claude's
    /// "[Image #21]" placeholders removed, whitespace ignored.
    private static let normalized = TextMemo<String>(limit: 512)

    static func normalize(_ s: String) -> String {
        normalized(s) { s in
            var t = s
            for pattern in [#"<attached_images>[\s\S]*?</attached_images>"#, #"<[\\/]*pasted_content[^>]*>"#, #"\[Image #\d+\]"#, #"\s+"#] {
                t = t.replacingOccurrences(of: pattern, with: "", options: .regularExpression)
            }
            return t
        }
    }

    /// The transcript's copy of a message sent from here (equal once normalized, or one containing the other:
    /// Claude adds placeholders and the phone adds notes). Containing only counts for longer texts: a pending "好"
    /// was taken for any message with 好 in it.
    static func same(_ a: String, _ b: String) -> Bool {
        let x = normalize(a), y = normalize(b)
        guard !x.isEmpty, !y.isEmpty else { return false }
        return x == y || (min(x.count, y.count) >= 8 && (x.contains(y) || y.contains(x)))
    }
}

nonisolated private extension Character {
    var isCJK: Bool {
        guard let v = unicodeScalars.first?.value else { return false }
        return (0x2E80...0x9FFF).contains(v) || (0xF900...0xFAFF).contains(v) || (0xFF00...0xFFEF).contains(v) || (0x3000...0x303F).contains(v)
    }
}
