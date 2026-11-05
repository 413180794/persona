import SwiftUI

// The Mac inbox's state (docs/inbox.md, docs/mac.md「收件箱」): one per app, shared by the sidebar's filters, the list
// column and the detail column, which the window's three columns hold separately (`MacInboxFilters`, `MacInboxList`,
// `MacInboxDetail`). The filters are the iPhone's (same `inbox.source` / `inbox.kind` defaults), picked from the
// sidebar instead of a row of chips.

/// A row in the list: an inbox item, or an agent waiting on the user (heads 需要你, as on the iPhone).
enum MacInboxSelection: Hashable {
    case item(String)
    case agent(String)
}

/// One of the sidebar's inbox entries: 全部, 飞书, each mail account (sources), then @我 / 私聊 / 群聊 / 邮件 / Agent (kinds).
enum MacInboxFilter: Hashable, Identifiable {
    case all
    case feishu
    case account(MailAccount)
    case kind(String)

    var id: String {
        switch self {
        case .all: "all"
        case .feishu: "feishu"
        case .account(let a): "account." + a.id
        case .kind(let k): "kind." + k
        }
    }

    /// The kinds, in the iPhone's chip order.
    static let kinds: [(key: String, title: String)] = [("mention", "@我"), ("dm", "私聊"), ("group", "群聊"), ("mail", "邮件"), ("agent", "Agent")]

    var title: String {
        switch self {
        case .all: "全部"
        case .feishu: "飞书"
        case .account(let a): InboxView.short(a.email)
        case .kind(let k): Self.kinds.first { $0.key == k }?.title ?? k
        }
    }

    var icon: String {
        switch self {
        case .all: Theme.Icon.inbox
        case .feishu: Theme.Icon.feishuChats
        case .account: Theme.Icon.mail
        case .kind(let k):
            switch k {
            case "mention": Theme.Icon.mention
            case "dm": Theme.Icon.person
            case "group": Theme.Icon.groupChat
            case "mail": Theme.Icon.mailFilled
            default: Theme.Icon.agents
            }
        }
    }

    /// The `inbox.source` / `inbox.kind` pair this entry stands for.
    var source: String {
        switch self {
        case .feishu: "feishu"
        case .account(let a): a.id
        default: ""
        }
    }

    var kind: String {
        if case .kind(let k) = self { return k }
        return ""
    }
}

@MainActor @Observable
final class MacInboxState {
    static let shared = MacInboxState()

    /// A document is open beside the selected item (the link panel): the page gives it the filters' and the actions'
    /// columns (#2113, `MacInboxPage`, `macInboxActions`).
    var reading = false {
        didSet { if !reading { listWhileReading = false } }
    }
    /// The list brought back beside a document being read (its button in the item's actions row); folded again when
    /// the document closes.
    var listWhileReading = false

    /// "" all, "feishu", or a mail account id (the iPhone's `inbox.source`).
    var source: String = UserDefaults.standard.string(forKey: "inbox.source") ?? "" {
        didSet { UserDefaults.standard.set(source, forKey: "inbox.source") }
    }
    /// "" all, agent, dm, mention, group, mail (the iPhone's `inbox.kind`).
    var kind: String = UserDefaults.standard.string(forKey: "inbox.kind") ?? "" {
        didSet { UserDefaults.standard.set(kind, forKey: "inbox.kind") }
    }
    var query = ""
    var selection: Set<MacInboxSelection> = []
    /// Sections shown open: tiers (action, fyi, noise) and "done". 已折叠 and 已处理 start closed, as on the iPhone.
    var expanded: Set<String> = ["action", "fyi"]
    var accounts: [MailAccount] = StateCache.load("mail-accounts", as: [MailAccount].self) ?? []
    /// The sheets the list opens: 写邮件, 管理邮箱, 修改 on the AI 分身's draft, 交给 AI.
    var composing: ComposeDraft?
    var showAccounts = false
    var editingDraft: InboxItem?
    var drafting: InboxItem?
    var handingOff: InboxItem?

    private var store: InboxStore { .shared }

    // MARK: Filters

    var filter: MacInboxFilter {
        get {
            if !kind.isEmpty { return .kind(kind) }
            if source == "feishu" { return .feishu }
            if let a = accounts.first(where: { $0.id == source }) { return .account(a) }
            return .all
        }
        set {
            source = newValue.source
            kind = newValue.kind
        }
    }

    /// The sidebar's entries: sources, then kinds.
    var sources: [MacInboxFilter] { [.all, .feishu] + accounts.map { .account($0) } }
    var kinds: [MacInboxFilter] { MacInboxFilter.kinds.map { .kind($0.key) } }

    func matches(_ it: InboxItem, source: String? = nil, kind: String? = nil, query: String? = nil) -> Bool {
        let source = source ?? self.source, kind = kind ?? self.kind, query = query ?? self.query
        if !source.isEmpty && it.account != source { return false }
        if !kind.isEmpty && !(it.kind == kind || kind == "group" && it.kind == "bot" || kind == "mention" && it.kind == "doc") { return false }
        if query.isEmpty { return true }
        return [it.title, it.sender ?? "", it.preview, it.summary ?? ""].contains { $0.localizedCaseInsensitiveContains(query) }
    }

    /// Agents (and the assistant) waiting for an answer, an approval or a plan review.
    func waitingAgents(_ model: AppModel, source: String? = nil, kind: String? = nil) -> [Interaction] {
        let source = source ?? self.source, kind = kind ?? self.kind
        guard source.isEmpty, kind.isEmpty || kind == "agent" else { return [] }
        return model.pendingInteractions.filter { ix in
            !model.isMuted(ix.sessionKey) && (query.isEmpty || [ix.sessionTitle ?? "", ix.summary ?? "", ix.title].contains { $0.localizedCaseInsensitiveContains(query) })
        }
    }

    /// What still waits in an entry (the sidebar's count): open items 需要你, plus the waiting agents.
    func count(_ f: MacInboxFilter, model: AppModel) -> Int {
        let items = store.items.filter { !$0.isDone && $0.triage == .action && matches($0, source: f.source, kind: f.kind, query: "") }
        let agents = f.source.isEmpty && (f.kind.isEmpty || f.kind == "agent")
            ? model.pendingInteractions.filter { !model.isMuted($0.sessionKey) }.count : 0
        return (f.kind == "agent" ? 0 : items.count) + agents
    }

    // MARK: Sections

    struct Sections {
        var tiers: [(tier: InboxItem.Tier, items: [InboxItem], agents: [Interaction])]
        var done: [InboxItem]
        var isEmpty: Bool { tiers.isEmpty && done.isEmpty }
    }

    func sections(_ model: AppModel) -> Sections {
        let partition = Dictionary(grouping: store.items.filter { matches($0) }, by: \.isDone)
        let byTier = Dictionary(grouping: partition[false] ?? [], by: \.triage)
        let agents = waitingAgents(model)
        let tiers = InboxItem.Tier.allCases.compactMap { tier -> (InboxItem.Tier, [InboxItem], [Interaction])? in
            let items = kind == "agent" ? [] : byTier[tier] ?? []
            let a = tier == .action ? agents : []
            return items.isEmpty && a.isEmpty ? nil : (tier, items, a)
        }
        let done = (partition[true] ?? []).sorted { ($0.doneAt ?? .distantPast) > ($1.doneAt ?? .distantPast) }
        return Sections(tiers: tiers, done: done)
    }

    /// 全部标为已处理: the open items the list shows (its filter and search); the agents waiting are not items.
    var openInView: [InboxItem] { kind == "agent" ? [] : store.items.filter { !$0.isDone && matches($0) } }

    /// What 全部标为已处理 covers and how many, for its help tag.
    var markAllScope: String {
        InboxBulk.scopeLine(filters: filter == .all ? [] : [filter.title], query: query, count: openInView.count)
    }

    /// The rows in the order the list shows them (closed sections left out): for moving the selection on.
    func visibleOrder(_ model: AppModel) -> [MacInboxSelection] {
        let s = sections(model)
        var out: [MacInboxSelection] = []
        for t in s.tiers where expanded.contains(t.tier.rawValue) {
            out += t.agents.map { .agent($0.id) } + t.items.map { .item($0.id) }
        }
        if expanded.contains("done") { out += s.done.map { .item($0.id) } }
        return out
    }

    func toggle(_ section: String) {
        if expanded.contains(section) { expanded.remove(section) } else { expanded.insert(section) }
    }

    // MARK: Actions

    func item(_ id: String) -> InboxItem? { store.items.first { $0.id == id } }

    /// The selected items (agent rows are not items).
    var selectedItems: [InboxItem] {
        selection.compactMap { if case .item(let id) = $0 { return item(id) } else { return nil } }
    }

    /// 已处理 (or 移回 when all are done already) on these rows, then the selection moves to the row after them, as
    /// Mail does when a message is deleted.
    func toggleDone(_ ids: Set<MacInboxSelection>, model: AppModel) {
        let items = ids.compactMap { s -> InboxItem? in if case .item(let id) = s { return item(id) } else { return nil } }
        guard !items.isEmpty else { return }
        let done = !items.allSatisfy(\.isDone)
        let order = visibleOrder(model)
        let after = order.lastIndex { ids.contains($0) }.map { order.index(after: $0) } ?? order.endIndex
        let next = order[after...].first { !ids.contains($0) } ?? order[..<(order.lastIndex { ids.contains($0) } ?? 0)].last { !ids.contains($0) }
        withAnimation(Theme.Motion.standard) { store.setDone(items, done) }
        if done, ids == selection { selection = next.map { [$0] } ?? [] }
    }

    /// The agent's chat (the assistant's in Persona), where the question / plan / approval waits.
    func open(_ ix: Interaction, model: AppModel) {
        if model.session(ix.sessionKey)?.assistant == true {
            model.selectHub(ix.group)
            model.assistantCurrent = ix.sessionKey
            model.selectedTab = .assistant
        } else {
            model.openChat = ix.sessionKey
            model.selectedTab = .chats
        }
    }

    func loadAccounts(_ model: AppModel) async {
        guard let api = model.api, let a = try? await api.mailAccounts() else { return }
        accounts = a
        StateCache.save("mail-accounts", a)
        if !source.isEmpty, source != "feishu", !a.contains(where: { $0.id == source }) { source = "" }
    }
}
