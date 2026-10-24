import SwiftUI

struct ChatsView: View {
    @Environment(AppModel.self) private var model
    @State private var path: [SessionKey] = []
    @State private var query = ""
    @State private var machineFilter: String?
    @State private var showNew = false
    @State private var showAllRecent = false

    private var filtered: [Session] {
        model.allSessions.filter { s in
            (machineFilter == nil || s.machineID == machineFilter) &&
            (query.isEmpty || s.title.localizedCaseInsensitiveContains(query) || s.cwd.localizedCaseInsensitiveContains(query)
             || (s.lastText ?? "").localizedCaseInsensitiveContains(query))
        }
    }

    private var live: [Session] {
        filtered.filter { $0.status.isLive }.sorted { a, b in
            let pa = priority(a), pb = priority(b)
            return pa != pb ? pa < pb : (a.updatedAt ?? .distantPast) > (b.updatedAt ?? .distantPast)
        }
    }

    private var recent: [Session] {
        filtered.filter { !$0.status.isLive }.sorted { ($0.updatedAt ?? .distantPast) > ($1.updatedAt ?? .distantPast) }
    }

    private func priority(_ s: Session) -> Int {
        if s.pendingInteractions > 0 || s.status == .blocked { return 0 }
        switch s.status {
        case .working: return 1
        case .done: return 2
        default: return 3
        }
    }

    var body: some View {
        NavigationStack(path: $path) {
            List {
                Section { ConnectionBanner() }
                    .listRowInsets(EdgeInsets(top: 0, leading: 16, bottom: 0, trailing: 16))
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)

                if !model.pendingInteractions.isEmpty && query.isEmpty {
                    Section {
                        ForEach(model.pendingInteractions) { ix in
                            InteractionPreviewCard(interaction: ix) { path.append(ix.sessionKey) }
                                .listRowInsets(EdgeInsets(top: 6, leading: 16, bottom: 6, trailing: 16))
                                .listRowBackground(Color.clear)
                                .listRowSeparator(.hidden)
                        }
                    } header: {
                        SectionHeader(title: "需要你处理", count: model.pendingInteractions.count, tint: Theme.attention)
                    }
                }

                if !live.isEmpty {
                    Section {
                        ForEach(live) { s in row(s) }
                    } header: {
                        SectionHeader(title: "运行中", count: live.count, tint: Theme.working)
                    }
                }

                if !recent.isEmpty {
                    Section {
                        ForEach(showAllRecent ? recent : Array(recent.prefix(12))) { s in row(s) }
                        if recent.count > 12 && !showAllRecent {
                            Button("显示全部 \(recent.count) 个") { withAnimation { showAllRecent = true } }
                                .font(.subheadline.weight(.medium))
                        }
                    } header: {
                        SectionHeader(title: "最近", count: nil, tint: .secondary)
                    }
                }

                if model.allSessions.isEmpty {
                    Section {
                        EmptyStateView(symbol: "sparkles", title: "开始第一段对话",
                                       message: "在任意一台电脑的 herdr 里启动 Claude，手机上实时看、随时接手。",
                                       action: ("新建对话", { showNew = true }))
                    }
                    .listRowBackground(Color.clear)
                }
            }
            .listStyle(.insetGrouped)
            .navigationTitle("Jarvis")
            .searchable(text: $query, prompt: "搜索对话、目录、内容")
            .refreshable { await model.refresh() }
            .toolbar {
                ToolbarItem(placement: .topBarLeading) { machineMenu }
                ToolbarItem(placement: .topBarTrailing) {
                    Button { showNew = true } label: { Image(systemName: "square.and.pencil") }
                        .accessibilityLabel("新建对话")
                }
            }
            .navigationDestination(for: SessionKey.self) { key in ChatView(key: key) }
            .sheet(isPresented: $showNew) {
                NewChatView { session in
                    showNew = false
                    path.append(session.key)
                }
            }
            .onChange(of: model.openChat) { _, key in
                guard let key else { return }
                path = [key]
                model.openChat = nil
            }
        }
    }

    private func row(_ s: Session) -> some View {
        NavigationLink(value: s.key) {
            SessionRow(session: s, machine: model.machine(s.machineID), showMachine: machineFilter == nil)
        }
        .swipeActions(edge: .trailing) {
            if s.startedByJarvis && s.status.isLive {
                Button(role: .destructive) { Task { await model.close(s.key) } } label: { Label("关闭", systemImage: "xmark") }
            }
        }
        .swipeActions(edge: .leading) {
            if s.status.isActive {
                Button { Task { await model.interrupt(s.key) } } label: { Label("中断", systemImage: "stop.fill") }
                    .tint(Theme.attention)
            }
        }
        .contextMenu {
            Button { UIPasteboard.general.string = s.cwd } label: { Label("拷贝目录", systemImage: "folder") }
            Button { UIPasteboard.general.string = "claude --resume \(s.sessionID)" } label: { Label("拷贝恢复命令", systemImage: "terminal") }
        }
    }

    private var machineMenu: some View {
        Menu {
            Picker("机器", selection: $machineFilter) {
                Label("全部机器", systemImage: "square.grid.2x2").tag(String?.none)
                ForEach(model.machines) { m in
                    Label(m.name, systemImage: m.symbol).tag(Optional(m.id))
                }
            }
        } label: {
            Image(systemName: machineFilter == nil ? "line.3.horizontal.decrease" : "line.3.horizontal.decrease.circle.fill")
        }
        .accessibilityLabel("按机器筛选")
    }
}

struct SectionHeader: View {
    var title: String
    var count: Int?
    var tint: Color

    var body: some View {
        HStack(spacing: 6) {
            Text(title)
            if let count {
                Text("\(count)")
                    .font(.caption2.weight(.bold).monospacedDigit())
                    .foregroundStyle(tint)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 1)
                    .background(tint.opacity(0.14), in: .capsule)
            }
        }
    }
}

struct SessionRow: View {
    var session: Session
    var machine: Machine?
    var showMachine = true

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            SessionAvatar(machine: machine, status: session.pendingInteractions > 0 ? .blocked : session.status)
            VStack(alignment: .leading, spacing: 4) {
                HStack(alignment: .firstTextBaseline) {
                    Text(session.title.isEmpty ? session.folderName : session.title)
                        .font(.body.weight(.semibold))
                        .lineLimit(1)
                    Spacer(minLength: 6)
                    TimelineView(.periodic(from: .now, by: 30)) { ctx in
                        Text(Fmt.relative(session.updatedAt, now: ctx.date))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                HStack(spacing: 6) {
                    if session.status.isLive || session.pendingInteractions > 0 {
                        StatusLabel(status: session.status, pending: session.pendingInteractions)
                    }
                    Text(session.folderName)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                    if showMachine, let machine {
                        Text("· " + machine.name).font(.caption).foregroundStyle(.tertiary).lineLimit(1)
                    }
                    Spacer(minLength: 0)
                    if let model = Fmt.model(session.model) { Chip(text: model) }
                }
                if let last = session.lastText, !last.isEmpty {
                    Text(last)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
            }
        }
        .padding(.vertical, 4)
    }
}
