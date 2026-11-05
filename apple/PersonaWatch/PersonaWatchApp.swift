import SwiftUI
import WatchKit

@main
struct PersonaWatchApp: App {
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

/// Watch state: seeded by the phone over WatchConnectivity, refreshed from the hubs directly while open. With several
/// groups of hubs on the phone (docs/multi-hub.md「手表」) the watch gets each group's credential and shows them merged;
/// what it does goes to the group of what it acts on.
@Observable
final class WatchModel {
    /// Each group's credential ("" = the default group); one group from a phone before several hubs.
    var configs: [String: ServerConfig] = [:]
    /// What the groups are called (labels, when there are several).
    var names: [String: String] = [:]
    /// The phone's groups it could not get the watch a credential for yet (#1185); the watch says so and offers to
    /// sync again instead of using the phone's own.
    var missing: [String] = []
    /// The default (or first) group's: the watch is paired while there is one.
    var config: ServerConfig? { configs[""] ?? groups.first.flatMap { configs[$0] } }
    /// The groups, the default one first.
    var groups: [String] { configs.keys.sorted { ($0.isEmpty ? 0 : 1, $0) < ($1.isEmpty ? 0 : 1, $1) } }
    var sessions: [Session] = []
    var interactions: [Interaction] = []
    /// The inbox's 需要你 (Feishu messages and mail the AI says need the user), newest first.
    var inbox: [InboxItem] = []
    var machines: [Machine] = []
    var error: String?
    var lastUpdate: Date?

    /// The default (or first) group's client: the inbox (each hub has its own; the watch shows the first group's).
    var api: APIClient? { groups.first.flatMap { api($0) } }
    /// A group's client.
    func api(_ group: String) -> APIClient? { configs[group].map { APIClient(config: $0, group: group) } }

    init() {
        if let data = UserDefaults.standard.data(forKey: "configs"), let all = try? JSONDecoder().decode(WatchConfigs.self, from: data) {
            (configs, names, missing) = (all.configs, all.names, all.missing ?? [])
        } else if let data = UserDefaults.standard.data(forKey: "config"), let c = try? JSONDecoder().decode(ServerConfig.self, from: data) {
            configs = ["": c]
        }
        #if DEBUG
        if let s = UserDefaults.standard.string(forKey: "pairURL"), let c = ServerConfig(pairURL: s) { configs = ["": c] }
        // `-showWatchMissing 公司,家里`: those groups have no credential for the watch (#1185); `""`: the default group,
        // none at all without -pairURL.
        if let s = UserDefaults.standard.string(forKey: "showWatchMissing") {
            missing = s.split(separator: ",", omittingEmptySubsequences: false).map(String.init)
            missing.forEach { names[$0] = $0 }
        }
        #endif
        WatchSync.shared.onConfig = { [weak self] c in
            self?.configs = ["": c]
            self?.names = [:]
            self?.missing = []
            UserDefaults.standard.set(try? JSONEncoder().encode(c), forKey: "config")
            UserDefaults.standard.removeObject(forKey: "configs")
            Task { await self?.refresh() }
        }
        WatchSync.shared.onConfigs = { [weak self] all in
            self?.configs = all.configs
            self?.names = all.names
            self?.missing = all.missing ?? []
            UserDefaults.standard.set(try? JSONEncoder().encode(all), forKey: "configs")
            Task { await self?.refresh() }
        }
        WatchSync.shared.onSummary = { [weak self] s in
            guard let self else { return }
            self.apply(sessions: s.sessions, interactions: s.interactions, machines: s.machines)
            if let inbox = s.inbox { self.applyInbox(inbox) }
        }
        WatchRecordingTransfer.shared.start()
        WatchSync.shared.activate()
        Task { await poll() }
    }

    private func apply(sessions: [Session], interactions: [Interaction], machines: [Machine]) {
        // Agents of quiet tasks and their questions are the main conversation's to tell (#1731), by the hub's word.
        let (sessions, interactions) = QuietTasks.leavingOut(sessions: sessions, interactions: interactions, quiet: \.quiet)
        let hadNew = interactions.contains { ix in !self.interactions.contains { $0.uid == ix.uid } }
        func rank(_ s: Session) -> Int { s.state.priority }
        self.sessions = sessions.filter { $0.state.isLive && !$0.assistant || $0.state == .needsYou || $0.state == .working }
            .sorted { rank($0) != rank($1) ? rank($0) < rank($1) : $0.sortDate > $1.sortDate }
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

    /// Every group's agents, requests and machines (each group's hub answers for its own), merged.
    func refresh() async {
        guard let first = api else { return }
        var ss: [Session] = [], ii: [Interaction] = [], mm: [Machine] = []
        var failure: Error?
        let multi = configs.count > 1
        for g in groups {
            guard let api = api(g) else { continue }
            do {
                async let s = api.sessions()
                async let i = api.interactions()
                async let m = api.machines()
                let (a, b, c) = try await (s, i, m)
                ss += a
                ii += b
                mm += multi ? c.map { var m = $0; m.groupLabel = names[g] ?? g; return m } : c
            } catch {
                failure = error
            }
        }
        if failure == nil || !ss.isEmpty || !mm.isEmpty { apply(sessions: ss, interactions: ii, machines: mm) }
        if let xx = try? await first.inbox() { applyInbox(xx) }
        error = failure.flatMap { ss.isEmpty && mm.isEmpty ? $0.watchShown : nil }
    }

    private func poll() async {
        while true {
            try? await Task.sleep(for: .seconds(8))
            if WKApplication.shared().applicationState == .active { await refresh() }
        }
    }

    func resolve(_ ix: Interaction, _ r: Resolution) async {
        // To the hub of the interaction's own group.
        guard sessions.first(where: { $0.key == ix.sessionKey })?.canApprove == true, let api = api(ix.group) else { return }
        do {
            _ = try await api.resolve(ix.id, r)
            interactions.removeAll { $0.uid == ix.uid }
            WKInterfaceDevice.current().play(.success)
        } catch let e as APIError where e.status == 410 {
            // Already ended on server: remove interaction cleanly without error
            interactions.removeAll { $0.uid == ix.uid }
            WKInterfaceDevice.current().play(.directionDown)
        } catch {
            self.error = error.watchShown
            WKInterfaceDevice.current().play(.failure)
        }
    }

    func send(_ key: SessionKey, _ text: String) async {
        guard sessions.first(where: { $0.key == key })?.canSend == true, let api = api(key.group) else { return }
        do {
            try await api.send(key, text: text)
            WKInterfaceDevice.current().play(.success)
        } catch {
            self.error = error.watchShown
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
            self.error = error.watchShown
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
            self.error = error.watchShown
            WKInterfaceDevice.current().play(.failure)
            return false
        }
    }

    func interrupt(_ key: SessionKey) async {
        guard sessions.first(where: { $0.key == key })?.canInterrupt == true else { return }
        try? await api(key.group)?.interrupt(key)
        WKInterfaceDevice.current().play(.stop)
    }

    func machine(_ ref: MachineRef) -> Machine? { machines.first { $0.ref == ref } }
}

struct WatchRootView: View {
    @Environment(WatchModel.self) private var model
    @State private var openedItem: InboxItem?
    @State private var recordingOpen = false
    #if DEBUG
    @State private var openedInteraction: Interaction?
    @State private var openedSession: Session?
    /// `-openWatchItem running`: the list without 需要你, so the running sessions are on screen (screenshots).
    private static let runningFirst = UserDefaults.standard.string(forKey: "openWatchItem") == "running"
    /// `-openWatchItem inbox`: 需要你 without the agents' requests, so the inbox rows are on screen (screenshots).
    private static let inboxFirst = UserDefaults.standard.string(forKey: "openWatchItem") == "inbox"
    #else
    private static let runningFirst = false
    private static let inboxFirst = false
    #endif

    var body: some View {
        NavigationStack {
            if model.config == nil {
                WatchPairView()
            } else {
                List {
                    NavigationLink { WatchRecordingView() } label: {
                        Label("录音上下文", systemImage: Theme.Icon.recording).font(Theme.Font.secondaryMedium).lineLimit(1)
                    }.watchCard()
                    // A group the phone could not get the watch a credential for: none of its agents is below (#1185).
                    if !model.missing.isEmpty {
                        Section {
                            Text("「\(model.missing.map { model.names[$0] ?? $0 }.joined(separator: "、"))」还没有手表的凭据")
                                .font(.footnote).foregroundStyle(.secondary).lineLimit(3)
                                .watchCard()
                            WatchSyncButton().watchCard()
                        }
                    }
                    if !model.interactions.isEmpty || !model.inbox.isEmpty, !Self.runningFirst {
                        Section("需要你") {
                            ForEach(Self.inboxFirst ? [] : model.interactions, id: \.uid) { ix in
                                NavigationLink { WatchInteractionView(interaction: ix) } label: {
                                    VStack(alignment: .leading, spacing: Theme.Watch.lineSpacing) {
                                        Label(ix.kind.watchTitle, systemImage: ix.kind.watchSymbol)
                                            .font(.footnote.weight(.semibold))
                                            .foregroundStyle(Theme.needsYou)
                                        Text(ix.watchSummary).font(.headline).lineLimit(3)
                                        Text(ix.taskTitle.map { "任务 · " + $0 } ?? ix.sessionTitle ?? "")
                                            .font(.footnote).foregroundStyle(.secondary).lineLimit(1)
                                    }
                                }
                                .watchCard()
                            }
                            ForEach(model.inbox) { it in
                                NavigationLink { WatchInboxItemView(item: it) } label: { WatchInboxRow(item: it) }
                                    .watchCard()
                            }
                        }
                    }
                    if let e = model.error {
                        // Offline: say so first; what is below (if anything) is from the last refresh that worked.
                        Section {
                            Label {
                                VStack(alignment: .leading, spacing: Theme.Watch.lineSpacing) {
                                    Text("连不上 hub").font(.headline)
                                    if let t = model.lastUpdate {
                                        Text("下面是 \(t.formatted(.relative(presentation: .named))) 的数据").font(.footnote).foregroundStyle(.secondary)
                                    }
                                    Text(e).font(.footnote).foregroundStyle(.secondary).lineLimit(3)
                                }
                            } icon: {
                                Image(systemName: Theme.Icon.offline).foregroundStyle(Theme.danger)
                            }
                            .watchCard()
                            Button { Task { await model.refresh() } } label: { Text("重试").lineLimit(1) }
                                .watchCard()
                        }
                    }
                    if model.lastUpdate != nil || model.error == nil {
                    Section("运行中") {
                        if model.lastUpdate == nil {
                            HStack(spacing: Theme.Space.s) {
                                ProgressView().frame(width: 22, height: 22)
                                Text("加载中").font(.footnote).foregroundStyle(.secondary)
                            }
                            .watchCard()
                        } else if model.sessions.isEmpty {
                            Text("没有运行中的 Claude").font(.footnote).foregroundStyle(.secondary)
                                .watchCard()
                        }
                        ForEach(model.sessions) { s in
                            NavigationLink { WatchSessionView(session: s) } label: {
                                HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
                                    // On the title's first line (titles wrap to two), not centered on the row.
                                    Circle().fill(s.state.color).frame(width: 8, height: 8)
                                        .alignmentGuide(.firstTextBaseline) { $0[.bottom] + 2 }
                                    VStack(alignment: .leading, spacing: Theme.Watch.lineSpacing) {
                                        Text(s.displayTitle).font(.headline).lineLimit(2)
                                        Text([s.spaceName, s.slotLabel].compactMap { $0 }.joined(separator: " › ").isEmpty
                                             ? (model.machine(s.machineRef)?.title ?? s.machineID)
                                             : [s.spaceName, s.slotLabel].compactMap { $0 }.joined(separator: " › "))
                                            .font(.footnote).foregroundStyle(.secondary).lineLimit(1)
                                    }
                                }
                            }
                            .watchCard()
                        }
                    }
                    }
                }
                .navigationTitle("Persona")
                .refreshable { await model.refresh() }
                .navigationDestination(item: $openedItem) { WatchInboxItemView(item: $0) }
                #if DEBUG
                .navigationDestination(item: $openedInteraction) { WatchInteractionView(interaction: $0) }
                .navigationDestination(item: $openedSession) { WatchSessionView(session: $0) }
                // `-openInboxFirst YES`: open the first inbox item (screenshots).
                .task {
                    guard UserDefaults.standard.bool(forKey: "openInboxFirst") else { return }
                    for _ in 0..<20 where model.inbox.isEmpty { try? await Task.sleep(for: .milliseconds(500)) }
                    openedItem = model.inbox.first
                }
                // `-openWatchItem question|plan|permission|screen|session`: open the first request of that kind, or the
                // first running session (screenshots). `running` and `inbox` are handled by `runningFirst` / `inboxFirst`.
                .task {
                    guard let want = UserDefaults.standard.string(forKey: "openWatchItem") else { return }
                    for _ in 0..<20 where model.lastUpdate == nil { try? await Task.sleep(for: .milliseconds(500)) }
                    if want == "session" {
                        openedSession = model.sessions.first
                    } else if want != "running", want != "inbox" {
                        openedInteraction = model.interactions.first { $0.kind.rawValue == want }
                    }
                }
                #endif
            }
        }
        .sheet(isPresented: $recordingOpen) { NavigationStack { WatchRecordingView() } }
        #if DEBUG
        .onAppear { recordingOpen = UserDefaults.standard.bool(forKey: "openRecording") }
        #endif
    }
}

extension View {
    /// A row of the watch's lists as a card (`Theme.Watch`): its own rounded background with a gap to the next card,
    /// padding inside, at least 44 pt tall, so a 1-line and a 3-line row have the same margins.
    func watchCard() -> some View {
        let p = Theme.Watch.cardPadding, half = max(0, Theme.Watch.cardGap - Theme.Watch.listRowSpacing) / 2
        return frame(maxWidth: .infinity, minHeight: Theme.Watch.minRowHeight - p.top - p.bottom, alignment: .leading)
            .listRowInsets(EdgeInsets(top: p.top + half, leading: p.leading, bottom: p.bottom + half, trailing: p.trailing))
            .listRowBackground(
                RoundedRectangle(cornerRadius: Theme.Watch.cardRadius, style: .continuous)
                    .fill(Theme.Watch.cardFill)
                    .padding(.vertical, half)
            )
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
            VStack(alignment: .leading, spacing: Theme.Watch.blockSpacing) {
                switch interaction.kind {
                case .question:
                    ForEach(interaction.questions ?? []) { q in
                        Text(q.question).font(.headline)
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
                                // One line, like every button: "(Recommended)" becomes the 推荐 tag the phone shows.
                                let (text, recommended) = QuestionOption.display(opt.label)
                                HStack(spacing: Theme.Space.sm) {
                                    Text(text).font(.footnote).lineLimit(1).minimumScaleFactor(0.8)
                                    if recommended {
                                        Text("推荐").font(.caption2.weight(.semibold)).foregroundStyle(Theme.ai)
                                            .padding(.horizontal, 5).padding(.vertical, 1)
                                            .background(Theme.ai.opacity(0.2), in: .capsule)
                                            .fixedSize()
                                    }
                                    Spacer(minLength: 0)
                                    if picks[q.question, default: []].contains(opt.label) { Image(systemName: Theme.Icon.done) }
                                }
                            }
                        }
                    }
                    if (interaction.questions ?? []).count > 1 || (interaction.questions ?? []).contains(where: \.multiSelect) {
                        Button("提交") { submit() }.buttonStyle(.borderedProminent)
                    }
                case .plan:
                    Text(WatchText.shown(interaction.plan ?? "")).font(.footnote)
                    Button("批准并执行") { act(Resolution(approve: true, mode: "bypassPermissions")) }.buttonStyle(.borderedProminent)
                    Button("打回") { act(Resolution(approve: false, feedback: "请重新考虑这个计划")) }
                case .permission:
                    Text(interaction.summary ?? "").font(.caption2.monospaced())
                    if interaction.requiresInput {
                        Text("请在 iPhone 上填写后允许").font(Theme.Font.caption)
                    } else {
                        Button { act(Resolution(allow: true)) } label: { Text("允许").lineLimit(1) }
                            .buttonStyle(.borderedProminent)
                    }
                    Button { act(Resolution(allow: false)) } label: { Text("拒绝").lineLimit(1) }
                case .screen:
                    Text(interaction.screen ?? "").font(.caption2.monospaced())
                    if let d = interaction.dialog, d.multi == true { // check boxes: ticked here, sent together
                        let ticked = picks[d.rev] ?? Set(d.options.filter { $0.kind == nil && $0.checked == true }.map(\.key))
                        ForEach(d.options.filter { $0.kind == nil }, id: \.key) { opt in
                            Button {
                                var set = ticked
                                if set.contains(opt.key) { set.remove(opt.key) } else { set.insert(opt.key) }
                                picks[d.rev] = set
                            } label: {
                                HStack(spacing: Theme.Space.sm) {
                                    Text("\(opt.key). \(opt.label)").lineLimit(1).minimumScaleFactor(0.8)
                                    Spacer(minLength: 0)
                                    if ticked.contains(opt.key) { Image(systemName: Theme.Icon.done) }
                                }
                            }
                            .font(.caption2)
                        }
                        Button(d.submit == "Next" ? "下一题" : "提交") {
                            act(Resolution(options: d.options.map(\.key).filter(ticked.contains)))
                        }
                        .buttonStyle(.borderedProminent)
                        .disabled(ticked.isEmpty)
                    } else if let d = interaction.dialog { // the hub's reading: answered by option, checked on the computer first
                        ForEach(d.options.filter { $0.kind == nil }, id: \.key) { opt in
                            Button { act(Resolution(option: opt.key)) } label: {
                                Text("\(opt.key). \(opt.label)").lineLimit(1).minimumScaleFactor(0.8)
                            }
                            .font(.caption2)
                        }
                    } else {
                        ForEach(ScreenParser.options(interaction.screen ?? ""), id: \.key) { opt in
                            Button { act(Resolution(keys: [opt.key])) } label: {
                                Text("\(opt.key). \(opt.label)").lineLimit(1).minimumScaleFactor(0.8)
                            }
                            .font(.caption2)
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
        .disabled(model.sessions.first(where: { $0.key == interaction.sessionKey })?.canApprove != true)
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
        guard model.sessions.first(where: { $0.key == interaction.sessionKey })?.canApprove == true else { return }
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
    /// The hub's channels, to say which a message came in from (asked once; nothing from a hub without them).
    @State private var channels: [HubChannel]?

    /// The session as the latest refresh has it (the one this page was opened with goes stale): its state follows, and
    /// a new rev (the transcript grew) reloads the messages.
    private var current: Session { model.sessions.first { $0.key == session.key } ?? session }

    var body: some View {
        let session = current
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Watch.blockSpacing) {
                HStack(spacing: Theme.Space.sm) {
                    Circle().fill(session.state.color).frame(width: 7, height: 7)
                    Text(session.state.label).font(.caption2.weight(.semibold)).foregroundStyle(session.state.tone.text)
                }
                ForEach(items.suffix(6)) { it in
                    // A bubble as wide as its words, the user's on the right, set off from the other edge.
                    Text(WatchText.shown(it.text ?? ""))
                        .font(.footnote)
                        .lineLimit(8)
                        .padding(Theme.Watch.bubblePadding)
                        .background(it.role == "user" ? Theme.accent.opacity(0.25) : Color.white.opacity(0.08), in: .rect(cornerRadius: Theme.Radius.control))
                        .padding(it.role == "user" ? .leading : .trailing, 14)
                        .frame(maxWidth: .infinity, alignment: it.role == "user" ? .trailing : .leading)
                    // Came in from a channel (WeChat, the Feishu bot…), as under the phone's bubble.
                    if it.role == "user", let c = ChannelSource.of(it.clientMsgID, in: channels ?? []) {
                        Text("来自 \(c.name)").font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                            .frame(maxWidth: .infinity, alignment: .trailing)
                    }
                }
                if let source = session.sourceSubtitle {
                    Text(source).font(Theme.Font.caption).foregroundStyle(.secondary)
                }
                TextField("回复（可听写）", text: $reply)
                    .disabled(!session.canSend)
                    .onSubmit {
                        guard session.canSend else { return }
                        let t = reply
                        reply = ""
                        guard !t.isEmpty else { return }
                        Task { await model.send(session.key, t); await load() }
                    }
                HStack {
                    Button("继续") { Task { await model.send(session.key, "继续") } }.lineLimit(1).disabled(!session.canSend)
                    if session.status.isActive && session.canInterrupt {
                        Button { Task { await model.interrupt(session.key) } } label: { Image(systemName: Theme.Icon.interrupt) }
                            .tint(Theme.danger)
                    }
                }
            }
        }
        .navigationTitle(session.displayTitle)
        .task {
            try? await model.api(session.group)?.post("/api/v1/sessions/\(session.machineID)/\(session.sessionID)/seen")
        }
        .task(id: session.rev) { await load() }
        // The model refreshes every 8 s while the app is active; a session without a rev (older hub) still reloads.
        .task(id: session.rev == nil ? model.lastUpdate : nil) { if session.rev == nil { await load() } }
    }

    private func load() async {
        guard let api = model.api(session.group), let res = try? await api.items(session.key, beforeSeq: nil, turns: 2) else { return }
        items = res.items.filter { $0.kind == .text && !WatchText.shown($0.text ?? "").isEmpty }
        if channels == nil, items.contains(where: { $0.role == "user" && ChannelSource.possible($0.clientMsgID) }) {
            channels = (try? await api.channels()) ?? []
        }
    }
}

/// Shown until the watch has the phone's pairing: pull it from the phone (when nearby) instead of only waiting. A phone
/// that is paired but could not get the watch a credential of its own says so (#1185): the watch never gets the
/// phone's.
struct WatchPairView: View {
    @Environment(WatchModel.self) private var model

    var body: some View {
        ScrollView {
            VStack(spacing: 10) {
                Image(systemName: Theme.Icon.handoff).font(.title2).foregroundStyle(Theme.accent)
                if model.missing.isEmpty {
                    Text("还没有配对").font(.headline)
                    Text("iPhone 上的 Persona 配对好之后，点下面从 iPhone 同步。").font(.footnote).foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                } else {
                    Text("还没有手表的凭据").font(.headline)
                    Text("iPhone 联网后，点下面重新同步。")
                        .font(.footnote).foregroundStyle(.secondary).multilineTextAlignment(.center)
                }
                WatchSyncButton()
                // In the scroll view: beside it, it squeezed the page until the sync button was out of sight.
                NavigationLink { WatchRecordingView() } label: {
                    Label("录音上下文", systemImage: Theme.Icon.recording).lineLimit(1)
                }
                .padding(.top, Theme.Space.s)
            }
            .padding()
        }
    }
}

/// 从 iPhone 同步: asks the phone for the watch's credentials (it tries the hub again for the ones it has not got).
struct WatchSyncButton: View {
    @Environment(WatchModel.self) private var model
    @State private var syncing = false
    @State private var failed = false

    var body: some View {
        VStack(spacing: 10) {
            Button {
                syncing = true
                failed = false
                WatchSync.shared.requestConfig { ok in
                    syncing = false
                    failed = !ok
                    WKInterfaceDevice.current().play(ok && model.missing.isEmpty ? .success : .failure)
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
                Text("连不上 iPhone：打开 iPhone 上的 Persona，或在 设置 › 同步到 Apple Watch。").font(.caption2).foregroundStyle(Theme.cautionText)
                    .multilineTextAlignment(.center)
            }
        }
    }
}

// MARK: - 收件箱

/// One inbox item in 需要你: where it came from, the title, the AI's summary.
struct WatchInboxRow: View {
    var item: InboxItem

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Watch.lineSpacing) {
            Label {
                Text(item.isLark ? (item.kindLabel ?? "飞书") : "邮件")
            } icon: {
                if item.isLark { Image(Theme.Brand.lark).resizable().scaledToFit().frame(width: 14, height: 14) }
                else { Image(systemName: Theme.Icon.mailFilled) }
            }
                .font(.footnote.weight(.semibold))
                .foregroundStyle(item.kind == "mention" ? Theme.danger : Theme.accent)
            Text(item.title).font(.headline).lineLimit(2)
            Text(item.summary?.isEmpty == false ? item.summary! : item.preview).font(.footnote).foregroundStyle(.secondary).lineLimit(3)
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
            VStack(alignment: .leading, spacing: Theme.Watch.blockSpacing) {
                if let s = item.summary, !s.isEmpty {
                    Text(s).font(.headline)
                }
                Text([item.sender, item.preview].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: "："))
                    .font(.footnote).foregroundStyle(.secondary)
                if let draft {
                    Text(draft)
                        .font(.footnote)
                        .padding(Theme.Watch.bubblePadding)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(Theme.accent.opacity(0.25), in: .rect(cornerRadius: Theme.Radius.control))
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
                        if drafting { ProgressView() } else { Label("AI 回复", systemImage: Theme.Icon.ai).lineLimit(1) }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(drafting)
                }
                Button {
                    Task {
                        await model.setDone(item)
                        dismiss()
                    }
                } label: { Label("已处理", systemImage: Theme.Icon.done).lineLimit(1) }
            }
        }
        .navigationTitle(item.title)
        .onAppear { if draft == nil { draft = item.agentDraft } } // the AI 分身's, ready to send
    }
}

private extension Error {
    /// The watch has no connection title (the phone's 等待网络 / 连接中…): no network is still worth a line here.
    var watchShown: String? { isOffline ? localizedDescription : shown }
}
