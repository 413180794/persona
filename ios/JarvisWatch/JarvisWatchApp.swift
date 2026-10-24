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

    func refresh() async {
        guard let api else { return }
        do {
            async let s = api.sessions()
            async let i = api.interactions()
            async let m = api.machines()
            let (ss, ii, mm) = try await (s, i, m)
            apply(sessions: ss, interactions: ii, machines: mm)
            error = nil
        } catch {
            self.error = error.localizedDescription
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
            self.error = error.localizedDescription
            WKInterfaceDevice.current().play(.failure)
        }
    }

    func send(_ key: SessionKey, _ text: String) async {
        guard let api else { return }
        do {
            try await api.send(key, text: text)
            WKInterfaceDevice.current().play(.success)
        } catch {
            self.error = error.localizedDescription
            WKInterfaceDevice.current().play(.failure)
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

    var body: some View {
        NavigationStack {
            if model.config == nil {
                VStack(spacing: 8) {
                    Image(systemName: "iphone.and.arrow.forward").font(.title2).foregroundStyle(Theme.accent)
                    Text("在 iPhone 上打开 Jarvis 完成配对").font(.footnote).multilineTextAlignment(.center)
                }
                .padding()
            } else {
                List {
                    if !model.interactions.isEmpty {
                        Section("等你处理") {
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
                    ForEach(ScreenParser.options(interaction.screen ?? ""), id: \.key) { opt in
                        Button("\(opt.key). \(opt.label)") { act(Resolution(keys: [opt.key])) }.font(.caption2)
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

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 6) {
                    Circle().fill(session.state.color).frame(width: 7, height: 7)
                    Text(session.state.label).font(.caption2.weight(.semibold)).foregroundStyle(session.state.color)
                }
                ForEach(items.suffix(6)) { it in
                    Text(it.text ?? "")
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
            await load()
            try? await model.api?.post("/api/v1/sessions/\(session.machineID)/\(session.sessionID)/seen")
        }
    }

    private func load() async {
        guard let api = model.api, let res = try? await api.items(session.key, beforeSeq: nil, limit: 40) else { return }
        items = res.items.filter { $0.kind == .text }
    }
}
