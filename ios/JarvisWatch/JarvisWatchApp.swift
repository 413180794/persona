import SwiftUI
import WatchKit

@main
struct JarvisWatchApp: App {
    @State private var model = WatchModel()
    @Environment(\.scenePhase) private var phase

    var body: some Scene {
        WindowGroup {
            WatchRootView()
                .environment(model)
                .tint(Theme.accent)
        }
        .onChange(of: phase) { _, p in if p == .active { Task { await model.refresh() } } }
    }
}

/// Watch state: seeded by the phone over WatchConnectivity, refreshed from the hub directly while open.
@Observable
final class WatchModel {
    var config: ServerConfig?
    var sessions: [Session] = []
    var interactions: [Interaction] = []
    /// The inbox's 需要你 (Feishu messages and mail the AI says need the user), newest first.
    var inbox: [InboxItem] = []
    var machines: [Machine] = []
    var error: String?
    var lastUpdate: Date?

    var api: APIClient? { config.map { APIClient(config: $0) } }

    init() {
        if let data = UserDefaults.standard.data(forKey: "config"), let c = try? JSONDecoder().decode(ServerConfig.self, from: data) {
            config = c
        }
        #if DEBUG
        if let s = UserDefaults.standard.string(forKey: "pairURL"), let c = ServerConfig(pairURL: s) { config = c }
        #endif
        WatchSync.shared.onConfig = { [weak self] c in
            self?.config = c
            UserDefaults.standard.set(try? JSONEncoder().encode(c), forKey: "config")
            Task { await self?.refresh() }
        }
        WatchSync.shared.onSummary = { [weak self] s in
            guard let self else { return }
            self.apply(sessions: s.sessions, interactions: s.interactions, machines: s.machines)
            if let inbox = s.inbox { self.applyInbox(inbox) }
        }
        WatchSync.shared.activate()
        Task { await poll() }
    }

    private func apply(sessions: [Session], interactions: [Interaction], machines: [Machine]) {
        let hadNew = interactions.contains { ix in !self.interactions.contains { $0.id == ix.id } }
        func rank(_ s: Session) -> Int { s.state.priority }
        self.sessions = sessions.filter { $0.state.isLive && !$0.assistant || $0.state == .needsYou || $0.state == .working }
            .sorted { rank($0) != rank($1) ? rank($0) < rank($1) : ($0.updatedAt ?? .distantPast) > ($1.updatedAt ?? .distantPast) }
        self.interactions = interactions
        if !machines.isEmpty { self.machines = machines }
        lastUpdate = .now
        if hadNew { WKInterfaceDevice.current().play(.notification) }
    }

    private func applyInbox(_ items: [InboxItem]) {
        let open = items.filter { !$0.isDone && $0.triage == .action }
        let hadNew = open.contains { it in !inbox.contains { $0.id == it.id } } && !inbox.isEmpty
        inbox = open
        if hadNew { WKInterfaceDevice.current().play(.notification) }
    }

    func refresh() async {
        guard let api else { return }
        do {
            async let s = api.sessions()
            async let i = api.interactions()
            async let m = api.machines()
            async let x = api.inbox()
            let (ss, ii, mm) = try await (s, i, m)
            apply(sessions: ss, interactions: ii, machines: mm)
            if let xx = try? await x { applyInbox(xx) }
            error = nil
        } catch {
            self.error = error.shown
        }
    }

    private func poll() async {
        while true {
            try? await Task.sleep(for: .seconds(8))
            if WKApplication.shared().applicationState == .active { await refresh() }
        }
    }

    func resolve(_ ix: Interaction, _ r: Resolution) async {
        guard let api else { return }
        do {
            try await api.resolve(ix.id, r)
            interactions.removeAll { $0.id == ix.id }
            WKInterfaceDevice.current().play(.success)
        } catch {
            self.error = error.shown
            WKInterfaceDevice.current().play(.failure)
        }
    }

    func send(_ key: SessionKey, _ text: String) async {
        guard let api else { return }
        do {
            try await api.send(key, text: text)
            WKInterfaceDevice.current().play(.success)
        } catch {
            self.error = error.shown
            WKInterfaceDevice.current().play(.failure)
        }
    }

    func setDone(_ item: InboxItem) async {
        guard let api else { return }
        do {
            try await api.setInboxDone(item.id, true)
            inbox.removeAll { $0.id == item.id }
            WKInterfaceDevice.current().play(.success)
        } catch {
            self.error = error.shown
            WKInterfaceDevice.current().play(.failure)
        }
    }

    func reply(_ item: InboxItem, _ text: String) async -> Bool {
        guard let api else { return false }
        do {
            try await api.inboxReply(item.id, text: text)
            inbox.removeAll { $0.id == item.id }
            WKInterfaceDevice.current().play(.success)
            return true
        } catch {
            self.error = error.shown
            WKInterfaceDevice.current().play(.failure)
            return false
        }
    }

    func interrupt(_ key: SessionKey) async {
        try? await api?.interrupt(key)
        WKInterfaceDevice.current().play(.stop)
    }

    func machine(_ id: String) -> Machine? { machines.first { $0.id == id } }
}

struct WatchRootView: View {
    @Environment(WatchModel.self) private var model
    @State private var openedItem: InboxItem?

    var body: some View {
        NavigationStack {
            if model.config == nil {
                WatchPairView()
            } else {
                List {
                    if !model.interactions.isEmpty || !model.inbox.isEmpty {
                        Section("需要你") {
                            ForEach(model.interactions) { ix in
                                NavigationLink { WatchInteractionView(interaction: ix) } label: {
                                    VStack(alignment: .leading, spacing: 2) {
                                        Label(ix.kind.watchTitle, systemImage: ix.kind.watchSymbol)
                                            .font(.caption2.weight(.semibold))
                                            .foregroundStyle(Theme.attention)
                                        Text(ix.watchSummary).font(.footnote).lineLimit(3)
                                        Text(ix.sessionTitle ?? "").font(.caption2).foregroundStyle(.secondary).lineLimit(1)
                                    }
                                }
                            }
                            ForEach(model.inbox) { it in
                                NavigationLink { WatchInboxItemView(item: it) } label: { WatchInboxRow(item: it) }
                            }
                        }
                    }
                    Section("运行中") {
                        if model.sessions.isEmpty {
                            Text("没有运行中的 Claude").font(.footnote).foregroundStyle(.secondary)
                        }
                        ForEach(model.sessions) { s in
                            NavigationLink { WatchSessionView(session: s) } label: {
                                HStack(spacing: 8) {
                                    Circle().fill(s.state.color).frame(width: 8, height: 8)
                                    VStack(alignment: .leading, spacing: 1) {
                                        Text(s.displayTitle).font(.footnote.weight(.semibold)).lineLimit(2)
                                        Text([s.spaceName, s.slotLabel].compactMap { $0 }.joined(separator: " › ").isEmpty
                                             ? (model.machine(s.machineID)?.name ?? s.machineID)
                                             : [s.spaceName, s.slotLabel].compactMap { $0 }.joined(separator: " › "))
                                            .font(.caption2).foregroundStyle(.secondary).lineLimit(1)
                                    }
                                }
                            }
                        }
                    }
                    if let e = model.error {
                        Text(e).font(.caption2).foregroundStyle(Theme.danger)
                    }
                }
                .navigationTitle("Jarvis")
                .refreshable { await model.refresh() }
                .navigationDestination(item: $openedItem) { WatchInboxItemView(item: $0) }
                #if DEBUG
                // `-openInboxFirst YES`: open the first inbox item (screenshots).
                .task {
                    guard UserDefaults.standard.bool(forKey: "openInboxFirst") else { return }
                    for _ in 0..<20 where model.inbox.isEmpty { try? await Task.sleep(for: .milliseconds(500)) }
                    openedItem = model.inbox.first
                }
                #endif
            }
        }
    }
}

extension InteractionKind {
    var watchTitle: String {
        switch self {
        case .question: "提问"
        case .plan: "计划审批"
        case .permission: "授权"
        case .screen: "终端"
        }
    }

    var watchSymbol: String {
        switch self {
        case .question: "questionmark.bubble"
        case .plan: "list.bullet.clipboard"
        case .permission: "hand.raised"
        case .screen: "terminal"
        }
    }
}

extension Interaction {
    var watchSummary: String {
        switch kind {
        case .question: questions?.first?.question ?? ""
        case .plan: (plan ?? "").split(separator: "\n").first.map { $0.trimmingCharacters(in: CharacterSet(charactersIn: "# ")) } ?? "执行计划"
        case .permission: [toolName, summary].compactMap { $0 }.joined(separator: "：")
        case .screen: "终端里有对话框"
        }
    }
}

struct WatchInteractionView: View {
    @Environment(WatchModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var interaction: Interaction
    @State private var picks: [String: Set<String>] = [:]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 8) {
                switch interaction.kind {
                case .question:
                    ForEach(interaction.questions ?? []) { q in
                        Text(q.question).font(.footnote.weight(.semibold))
                        ForEach(q.options, id: \.label) { opt in
                            Button {
                                if q.multiSelect {
                                    var set = picks[q.question, default: []]
                                    if set.contains(opt.label) { set.remove(opt.label) } else { set.insert(opt.label) }
                                    picks[q.question] = set
                                } else {
                                    picks[q.question] = [opt.label]
                                    if (interaction.questions ?? []).count == 1 { submit() }
                                }
                            } label: {
                                HStack {
                                    Text(opt.label).font(.footnote)
                                    Spacer()
                                    if picks[q.question, default: []].contains(opt.label) { Image(systemName: "checkmark") }
                                }
                            }
                        }
                    }
                    if (interaction.questions ?? []).count > 1 || (interaction.questions ?? []).contains(where: \.multiSelect) {
                        Button("提交") { submit() }.buttonStyle(.borderedProminent)
                    }
                case .plan:
                    Text(interaction.plan ?? "").font(.caption2)
                    Button("批准并执行") { act(Resolution(approve: true, mode: "bypassPermissions")) }.buttonStyle(.borderedProminent)
                    Button("打回") { act(Resolution(approve: false, feedback: "请重新考虑这个计划")) }
                case .permission:
                    Text(interaction.summary ?? "").font(.caption2.monospaced())
                    Button("允许") { act(Resolution(allow: true)) }.buttonStyle(.borderedProminent)
                    Button("拒绝") { act(Resolution(allow: false)) }
                case .screen:
                    Text(interaction.screen ?? "").font(.system(size: 9, design: .monospaced))
                    if let d = interaction.dialog { // the hub's reading: answered by option, checked on the computer first
                        ForEach(d.options.filter { $0.kind == nil }, id: \.key) { opt in
                            Button("\(opt.key). \(opt.label)") { act(Resolution(option: opt.key)) }.font(.caption2)
                        }
                    } else {
                        ForEach(ScreenParser.options(interaction.screen ?? ""), id: \.key) { opt in
                            Button("\(opt.key). \(opt.label)") { act(Resolution(keys: [opt.key])) }.font(.caption2)
                        }
                    }
                    HStack {
                        Button("Esc") { act(Resolution(keys: ["esc"])) }
                        Button("⏎") { act(Resolution(keys: ["enter"])) }
                    }
                }
                Button("交给电脑处理") { act(.dismissed) }.font(.caption2).foregroundStyle(.secondary)
            }
        }
        .navigationTitle(interaction.kind.watchTitle)
    }

    private func submit() {
        var answers: [String: String] = [:]
        for q in interaction.questions ?? [] {
            let set = picks[q.question, default: []]
            answers[q.question] = q.options.map(\.label).filter(set.contains).joined(separator: ", ")
        }
        act(Resolution(answers: answers))
    }

    private func act(_ r: Resolution) {
        Task {
            await model.resolve(interaction, r)
            dismiss()
        }
    }
}

struct WatchSessionView: View {
    @Environment(WatchModel.self) private var model
    var session: Session
    @State private var reply = ""
    @State private var items: [Item] = []

    /// The session as the latest refresh has it (the one this page was opened with goes stale): its state follows, and
    /// a new rev (the transcript grew) reloads the messages.
    private var current: Session { model.sessions.first { $0.key == session.key } ?? session }

    var body: some View {
        let session = current
        ScrollView {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 6) {
                    Circle().fill(session.state.color).frame(width: 7, height: 7)
                    Text(session.state.label).font(.caption2.weight(.semibold)).foregroundStyle(session.state.color)
                }
                ForEach(items.suffix(6)) { it in
                    Text(WatchText.shown(it.text ?? ""))
                        .font(.caption2)
                        .lineLimit(8)
                        .padding(6)
                        .frame(maxWidth: .infinity, alignment: it.role == "user" ? .trailing : .leading)
                        .background(it.role == "user" ? Theme.accent.opacity(0.25) : Color.white.opacity(0.08), in: .rect(cornerRadius: 8))
                }
                TextField("回复（可听写）", text: $reply)
                    .onSubmit {
                        let t = reply
                        reply = ""
                        guard !t.isEmpty else { return }
                        Task { await model.send(session.key, t); await load() }
                    }
                HStack {
                    Button("继续") { Task { await model.send(session.key, "继续") } }
                    if session.status.isActive {
                        Button { Task { await model.interrupt(session.key) } } label: { Image(systemName: "stop.fill") }
                            .tint(Theme.danger)
                    }
                }
            }
        }
        .navigationTitle(session.displayTitle)
        .task {
            try? await model.api?.post("/api/v1/sessions/\(session.machineID)/\(session.sessionID)/seen")
        }
        .task(id: session.rev) { await load() }
        // The model refreshes every 8 s while the app is active; a session without a rev (older hub) still reloads.
        .task(id: session.rev == nil ? model.lastUpdate : nil) { if session.rev == nil { await load() } }
    }

    private func load() async {
        guard let api = model.api, let res = try? await api.items(session.key, beforeSeq: nil, turns: 2) else { return }
        items = res.items.filter { $0.kind == .text && !WatchText.shown($0.text ?? "").isEmpty }
    }
}

/// Shown until the watch has the phone's pairing: pull it from the phone (when nearby) instead of only waiting.
struct WatchPairView: View {
    @State private var syncing = false
    @State private var failed = false

    var body: some View {
        ScrollView {
            VStack(spacing: 10) {
                Image(systemName: "iphone.and.arrow.forward").font(.title2).foregroundStyle(Theme.accent)
                Text("还没有配对").font(.headline)
                Text("iPhone 上的 Jarvis 配对好之后，点下面从 iPhone 同步。").font(.footnote).foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                Button {
                    syncing = true
                    failed = false
                    WatchSync.shared.requestConfig { ok in
                        syncing = false
                        failed = !ok
                        WKInterfaceDevice.current().play(ok ? .success : .failure)
                    }
                } label: {
                    HStack {
                        if syncing { ProgressView() }
                        Text("从 iPhone 同步").lineLimit(1)
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(syncing)
                if failed {
                    Text("连不上 iPhone：打开 iPhone 上的 Jarvis，或在 设置 › 同步到 Apple Watch。").font(.caption2).foregroundStyle(Theme.attention)
                        .multilineTextAlignment(.center)
                }
            }
            .padding()
        }
    }
}

// MARK: - 收件箱

/// One inbox item in 需要你: where it came from, the title, the AI's summary.
struct WatchInboxRow: View {
    var item: InboxItem

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Label(item.isLark ? (item.kindLabel ?? "飞书") : "邮件", systemImage: item.isLark ? "bubble.left.fill" : "envelope.fill")
                .font(.caption2.weight(.semibold))
                .foregroundStyle(item.kind == "mention" ? Theme.danger : Theme.accent)
            Text(item.title).font(.footnote.weight(.semibold)).lineLimit(1)
            Text(item.summary?.isEmpty == false ? item.summary! : item.preview).font(.caption2).foregroundStyle(.secondary).lineLimit(3)
        }
    }
}

/// An inbox item on the wrist: the summary and the latest message; AI 回复 drafts a reply to read and send,
/// 已处理 files it.
struct WatchInboxItemView: View {
    @Environment(WatchModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var item: InboxItem
    @State private var draft: String?
    @State private var drafting = false
    @State private var sending = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 8) {
                if let s = item.summary, !s.isEmpty {
                    Text(s).font(.footnote.weight(.semibold))
                }
                Text([item.sender, item.preview].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: "："))
                    .font(.caption2).foregroundStyle(.secondary)
                if let draft {
                    Text(draft)
                        .font(.caption2)
                        .padding(6)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(Theme.accent.opacity(0.25), in: .rect(cornerRadius: 8))
                    Button {
                        sending = true
                        Task {
                            if await model.reply(item, draft) { dismiss() }
                            sending = false
                        }
                    } label: {
                        if sending { ProgressView() } else { Text("发送").lineLimit(1) }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(sending)
                } else {
                    Button {
                        drafting = true
                        Task {
                            draft = try? await model.api?.inboxDraft(item.id)
                            drafting = false
                            if draft == nil { WKInterfaceDevice.current().play(.failure) }
                        }
                    } label: {
                        if drafting { ProgressView() } else { Label("AI 回复", systemImage: "sparkles").lineLimit(1) }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(drafting)
                }
                Button {
                    Task {
                        await model.setDone(item)
                        dismiss()
                    }
                } label: { Label("已处理", systemImage: "checkmark").lineLimit(1) }
            }
        }
        .navigationTitle(item.title)
        .onAppear { if draft == nil { draft = item.agentDraft } } // the AI 分身's, ready to send
    }
}
