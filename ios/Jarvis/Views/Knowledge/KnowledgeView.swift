import MarkdownUI
import SwiftUI

/// What Jarvis knows (the hub's ~/.jarvis/data, docs/knowledge.md): projects first, then chats, people, docs, meetings and
/// events, most important and most relevant first. Not a browser of Feishu: every row is Jarvis' own digest, and the
/// way out is handing it to Jarvis or an Agent.
struct KnowledgeView: View {
    @Environment(AppModel.self) private var model
    @State private var kind: KBKind = .projects
    @State private var tag: String?
    @State private var list: KBList?
    @State private var error: String?
    @State private var query = ""
    @State private var hits: [KBHit] = []

    var body: some View {
        List {
            Section {
                Picker("类型", selection: $kind) {
                    ForEach(KBKind.allCases) { k in Text(k.title).tag(k) }
                }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 4, trailing: 0))
                if let tags = list?.tags, !tags.isEmpty {
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 6) {
                            tagChip(nil, "全部")
                            ForEach(tags, id: \.self) { t in tagChip(t, t) }
                        }
                    }
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets(top: 0, leading: 0, bottom: 4, trailing: 0))
                }
            }
            if !query.isEmpty {
                Section("搜索") {
                    if hits.isEmpty { Text("没有找到").foregroundStyle(.secondary) }
                    ForEach(hits, id: \.self) { h in
                        VStack(alignment: .leading, spacing: 3) {
                            Text(h.line).font(.subheadline).lineLimit(3)
                            Text(h.path).font(.caption2.monospaced()).foregroundStyle(.tertiary).lineLimit(1).truncationMode(.middle)
                        }
                    }
                }
            } else if let list {
                if list.items.isEmpty {
                    Section {
                        EmptyStateView(symbol: kind.symbol, title: "还没有\(kind.title)",
                                       message: "hub 会把飞书和邮件里的会话归档、压缩，自动整理出项目、人、文档和会议，并按重要级和相关性排好。")
                    }
                    .listRowBackground(Color.clear)
                }
                let active = list.items.filter { !$0.projectDone }
                let done = list.items.filter(\.projectDone)
                Section {
                    ForEach(active) { e in row(e) }
                }
                if !done.isEmpty {
                    Section("已完成") { ForEach(done) { e in row(e) } }
                }
            } else if let error {
                Section { Label(error, systemImage: "exclamationmark.triangle").foregroundStyle(Theme.danger).font(.footnote) }
            } else {
                Section { ProgressView().frame(maxWidth: .infinity) }.listRowBackground(Color.clear)
            }
        }
        .listStyle(.insetGrouped)
        .themedPage()
        .navigationTitle("知识库")
        .navigationBarTitleDisplayMode(.inline)
        .searchable(text: $query, prompt: "搜索项目、会话、聊天记录")
        .task(id: query) {
            guard !query.isEmpty, let api = model.api else { hits = []; return }
            try? await Task.sleep(for: .milliseconds(300)) // typing
            hits = (try? await api.kbSearch(query)) ?? []
        }
        .task(id: "\(kind.rawValue)|\(tag ?? "")") { await load() }
        .refreshable { await load() }
        .navigationDestination(for: KBEntry.self) { e in KnowledgeDetailView(entry: e) }
    }

    private func load() async {
        guard let api = model.api else { return }
        do {
            list = try await api.kbList(kind, tag: tag)
            error = nil
        } catch {
            self.error = error.shown
        }
    }

    private func tagChip(_ value: String?, _ label: String) -> some View {
        let on = tag == value
        return Button {
            Haptics.tap()
            tag = value
        } label: {
            Text(label)
                .font(.caption.weight(.medium))
                .lineLimit(1)
                .padding(.horizontal, 10)
                .padding(.vertical, 5)
                .foregroundStyle(on ? Color.white : Color.primary)
                .background(on ? Theme.accent : Color.primary.opacity(0.06), in: .capsule)
        }
        .buttonStyle(.plain)
    }

    private func row(_ e: KBEntry) -> some View {
        NavigationLink(value: e) { KBRow(entry: e) }
    }
}

/// One ranked entity: importance badge, name, its one-line state, tags and how much it concerns the user.
struct KBRow: View {
    var entry: KBEntry

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            ImportanceBadge(importance: entry.importance)
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 6) {
                    Text(entry.name).font(.body.weight(.semibold)).lineLimit(1)
                    if let k = entry.projectKind { Chip(text: k, look: k == "长期" ? .tint(Theme.Hue.blue) : .neutral) }
                }
                if let line = entry.oneLine, !line.isEmpty {
                    Text(line).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                }
                HStack(spacing: 6) {
                    RelevanceMeter(value: entry.relevance)
                    ForEach((entry.tags ?? []).prefix(3), id: \.self) { t in
                        Text("#" + t).font(.caption2).foregroundStyle(.secondary).lineLimit(1)
                    }
                    Spacer(minLength: 0)
                    if let d = entry.updated, d.realDate != nil {
                        Text(Fmt.relative(d)).font(.caption2).foregroundStyle(.tertiary)
                    }
                }
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }
}

struct ImportanceBadge: View {
    var importance: String?

    var body: some View {
        let p = importance ?? ""
        Text(p.isEmpty ? "–" : p)
            .font(.caption.weight(.bold).monospaced())
            .foregroundStyle(p.isEmpty ? Color.secondary : Color.white)
            .frame(width: 30, height: 22)
            .background(Self.tint(p), in: .rect(cornerRadius: 6, style: .continuous))
            .accessibilityLabel(p.isEmpty ? "未分级" : "重要级 \(p)")
    }

    static func tint(_ p: String) -> Color {
        switch p {
        case "P0": Theme.needsYou
        case "P1": Theme.busy
        case "P2": Theme.Hue.blue
        case "P3": Color.secondary.opacity(0.6)
        default: Color.primary.opacity(0.08)
        }
    }
}

/// Relevance to the user (0–100) as a short bar and the number.
struct RelevanceMeter: View {
    var value: Int

    var body: some View {
        HStack(spacing: 4) {
            Capsule().fill(Color.primary.opacity(0.08)).frame(width: 36, height: 4)
                .overlay(alignment: .leading) {
                    Capsule().fill(value >= 70 ? Theme.needsYou : value >= 40 ? Theme.busy : Color.secondary)
                        .frame(width: 36 * CGFloat(min(max(value, 0), 100)) / 100, height: 4)
                }
            Text("\(value)").font(.caption2.monospacedDigit()).foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("与我相关 \(value)")
    }
}

struct KnowledgeDetailView: View {
    @Environment(AppModel.self) private var model
    var entry: KBEntry
    @State private var detail: KBDetailResponse?
    @State private var error: String?
    @State private var editing = false
    @State private var newAgent = false
    @State private var showRecent = false

    var body: some View {
        List {
            Section {
                let e = shown
                VStack(alignment: .leading, spacing: 10) {
                    HStack(spacing: 8) {
                        ImportanceBadge(importance: e.importance)
                        if let k = e.projectKind { Chip(text: k + "项目", look: k == "长期" ? .tint(Theme.Hue.blue) : .neutral) }
                        RelevanceMeter(value: e.relevance)
                        Spacer()
                    }
                    Text(e.name).font(.title2.weight(.bold))
                    if let line = e.oneLine, !line.isEmpty { Text(line).foregroundStyle(.secondary) }
                    if let tags = e.tags, !tags.isEmpty {
                        Text(tags.map { "#" + $0 }.joined(separator: "  ")).font(.caption).foregroundStyle(.secondary)
                    }
                }
                .padding(.vertical, 4)
                HStack(spacing: 10) {
                    Button { Task { await ask() } } label: {
                        Label("问 Jarvis", systemImage: "sparkles").lineLimit(1).frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.borderedProminent)
                    if entry.kind == KBKind.projects.rawValue {
                        Button { newAgent = true } label: {
                            Label("开 Agent", systemImage: "plus.bubble").lineLimit(1).frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.bordered)
                    }
                }
                .controlSize(.large)
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
            }
            if let d = detail?.detail {
                if let agents = detail?.agents, !agents.isEmpty {
                    Section("在做这个项目的 Agent") {
                        ForEach(agents) { s in
                            Button { model.open(s.key) } label: {
                                SessionRow(session: s, machine: model.machine(s.machineID))
                            }
                            .buttonStyle(.plain)
                        }
                    }
                }
                if let sum = d.summary, !sum.isEmpty {
                    Section("档案") {
                        Markdown(Self.dropTitle(sum)).markdownTheme(.jarvis).textSelection(.enabled)
                            .padding(.vertical, 4)
                    }
                }
                if let log = d.log, !log.isEmpty {
                    Section("时间线") {
                        ForEach(Array(log.reversed().enumerated()), id: \.offset) { _, l in
                            Text(String(l.dropFirst(2))).font(.subheadline)
                        }
                    }
                }
                links(d)
                if let recent = d.recent, !recent.isEmpty {
                    Section {
                        DisclosureGroup("最近 7 天的消息", isExpanded: $showRecent) {
                            Text(recent).font(.footnote.monospaced()).textSelection(.enabled)
                        }
                    }
                }
                Section {
                    Text(d.path).font(.caption2.monospaced()).foregroundStyle(.tertiary).textSelection(.enabled)
                } footer: {
                    Text("Jarvis、AI 分身和各台电脑上的 Agent 都能直接读这个目录。")
                }
            } else if let error {
                Section { Label(error, systemImage: "exclamationmark.triangle").foregroundStyle(Theme.danger).font(.footnote) }
            } else {
                Section { ProgressView().frame(maxWidth: .infinity) }.listRowBackground(Color.clear)
            }
        }
        .listStyle(.insetGrouped)
        .themedPage()
        .navigationTitle(KBKind(rawValue: entry.kind)?.title ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button("打标") { editing = true }.lineLimit(1)
            }
        }
        .sheet(isPresented: $editing) {
            KBTagEditor(entry: shown) { await load() }
        }
        .sheet(isPresented: $newAgent) {
            NewChatView(initialPrompt: agentPrompt) { s in
                newAgent = false
                model.open(s.key)
            }
        }
        .task { await load() }
        .refreshable { await load() }
    }

    @ViewBuilder private func links(_ d: KBDetail) -> some View {
        let links = d.links ?? []
        let dirs = d.meta?["dirs"]?.array?.compactMap(\.string) ?? []
        if !links.isEmpty || !dirs.isEmpty {
            Section("关联") {
                ForEach(links) { e in
                    NavigationLink(value: e) {
                        Label {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(e.name).font(.subheadline).lineLimit(1)
                                if let l = e.oneLine, !l.isEmpty { Text(l).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
                            }
                        } icon: {
                            Image(systemName: KBKind(rawValue: e.kind)?.symbol ?? "circle")
                        }
                    }
                }
                ForEach(dirs, id: \.self) { dir in
                    Label(dir, systemImage: "folder").font(.subheadline.monospaced()).lineLimit(1).truncationMode(.head)
                }
            }
        }
    }

    static func dropTitle(_ md: String) -> String {
        guard md.hasPrefix("# ") else { return md }
        return md.split(separator: "\n", maxSplits: 1, omittingEmptySubsequences: false).dropFirst().joined(separator: "\n")
    }

    /// The entry as last loaded (a linked row starts with only its name).
    private var shown: KBEntry {
        guard let m = detail?.detail.meta else { return entry }
        var e = entry
        if let n = m["name"]?.string ?? m["title"]?.string, !n.isEmpty { e.name = n }
        if let i = m["importance"]?.string { e.importance = i }
        if let r = m["relevance"]?.int { e.relevance = r }
        if let t = m["tags"]?.array { e.tags = t.compactMap(\.string) }
        if let l = m["one_line"]?.string ?? m["note"]?.string, !l.isEmpty { e.oneLine = l }
        if e.kind == KBKind.projects.rawValue, let k = m["kind"]?.string { e.extra = k + " · " + (m["status"]?.string ?? "active") }
        return e
    }

    private var context: ContextRef {
        let kindTitle = KBKind(rawValue: entry.kind)?.title ?? ""
        var text = detail?.detail.summary ?? entry.oneLine ?? ""
        if let log = detail?.detail.log, !log.isEmpty { text += "\n\n时间线（最近）：\n" + log.suffix(10).joined(separator: "\n") }
        if let path = detail?.detail.path { text += "\n\n知识库目录：\(path)" }
        return ContextRef(kind: "context", title: "\(kindTitle) · \(shown.name)", url: "", text: text)
    }

    private var agentPrompt: String {
        context.xml + "\n\n"
    }

    private func ask() async {
        await model.askJarvis(context.xml + "\n\n说说「\(shown.name)」现在怎么样了：最新进展、在等我的事、接下来该做什么，能替我处理的先处理。")
    }

    private func load() async {
        guard let api = model.api else { return }
        do {
            detail = try await api.kbDetail(entry.kind, id: entry.id)
            error = nil
        } catch {
            self.error = error.shown
        }
    }
}

/// Importance, relevance, tags (and a project's kind / status) set by hand; the hub keeps them over the automatic ones.
struct KBTagEditor: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var entry: KBEntry
    var saved: () async -> Void
    @State private var importance = ""
    @State private var relevance = 50.0
    @State private var tags = ""
    @State private var projectKind = "short"
    @State private var status = "active"
    @State private var saving = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                Section("重要级") {
                    Picker("重要级", selection: $importance) {
                        ForEach(["P0", "P1", "P2", "P3"], id: \.self) { Text($0).tag($0) }
                    }
                    .pickerStyle(.segmented)
                }
                Section {
                    Slider(value: $relevance, in: 0...100, step: 5)
                } header: { Text("与我相关 \(Int(relevance))") }
                Section {
                    TextField("用空格分隔", text: $tags)
                } header: { Text("标签") }
                if entry.kind == KBKind.projects.rawValue {
                    Section("项目") {
                        Picker("类型", selection: $projectKind) {
                            Text("短期（需求）").tag("short")
                            Text("长期（使命）").tag("long")
                        }
                        Picker("状态", selection: $status) {
                            Text("进行中").tag("active")
                            Text("暂停").tag("paused")
                            Text("已完成").tag("done")
                        }
                    }
                }
                if let error {
                    Section { Label(error, systemImage: "exclamationmark.triangle").foregroundStyle(Theme.danger).font(.footnote) }
                }
            }
            .navigationTitle(entry.name)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("保存") { Task { await save() } }.disabled(saving)
                }
            }
            .onAppear {
                importance = entry.importance?.isEmpty == false ? entry.importance! : "P2"
                relevance = Double(entry.relevance)
                tags = (entry.tags ?? []).joined(separator: " ")
                if let extra = entry.extra, entry.kind == KBKind.projects.rawValue {
                    let parts = extra.components(separatedBy: " · ")
                    projectKind = parts.first ?? "short"
                    status = parts.count > 1 ? parts[1] : "active"
                }
            }
        }
        .presentationDetents([.medium, .large])
    }

    private func save() async {
        guard let api = model.api else { return }
        saving = true
        defer { saving = false }
        let isProject = entry.kind == KBKind.projects.rawValue
        let u = KBUpdate(importance: importance, relevance: Int(relevance),
                         tags: tags.split(whereSeparator: \.isWhitespace).map(String.init),
                         status: isProject ? status : nil, projectKind: isProject ? projectKind : nil)
        do {
            try await api.kbUpdate(entry.kind, id: entry.id, u)
            Haptics.success()
            await saved()
            dismiss()
        } catch {
            self.error = error.shown
            Haptics.error()
        }
    }
}

/// An Agent handed to Jarvis: who it is and where it runs, so Jarvis can read it, follow up, and file its result
/// under the right project (the knowledge base's project dirs).
enum AgentHandoff {
    static func prompt(_ s: Session, machine: String?) -> String {
        let title = s.title.isEmpty ? (s.cwd as NSString).lastPathComponent : s.title
        var text = "机器：\(machine ?? s.machineID)（\(s.machineID)）\n会话：\(s.sessionID)\n目录：\(s.cwd)\n状态：\(s.state.rawValue)"
        if let last = s.lastText, !last.isEmpty { text += "\n最后说的：\(String(last.prefix(800)))" }
        let ref = ContextRef(kind: "context", title: "Agent · \(title)", url: "", text: text)
        return ref.xml + "\n\n看一下这个 Agent（agent_read）：它在做什么、做到哪了、有没有在等我。它属于哪个项目就把进展记进项目（kb_update，没登记目录的顺便登记 dirs）；"
            + "需要我拍板的列出来，能替我推进的直接推进。"
    }
}
