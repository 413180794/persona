import SwiftUI

struct ChatsView: View {
    @Environment(AppModel.self) private var model
    @State private var path: [SessionKey] = []
    @State private var query = ""
    @State private var machineFilter: String?
    @State private var showNew = false
    @State private var showAllRecent = false
    @State private var showAllIdle = false

    private var filtered: [Session] {
        model.allSessions.filter { s in
            !s.assistant && (machineFilter == nil || s.machineID == machineFilter) &&
            (query.isEmpty || s.title.localizedCaseInsensitiveContains(query) || s.cwd.localizedCaseInsensitiveContains(query)
             || (s.lastText ?? "").localizedCaseInsensitiveContains(query))
        }
    }

    private func byTime(_ a: Session, _ b: Session) -> Bool { (a.updatedAt ?? .distantPast) > (b.updatedAt ?? .distantPast) }

    /// Working or waiting for the user.
    private var active: [Session] {
        filtered.filter { $0.status.isActive || $0.pendingInteractions > 0 }.sorted { a, b in
            let pa = priority(a), pb = priority(b)
            return pa != pb ? pa < pb : byTime(a, b)
        }
    }

    /// Finished a turn the user has not looked at yet.
    private var finished: [Session] {
        filtered.filter { $0.status == .done && $0.pendingInteractions == 0 }.sorted(by: byTime)
    }

    private var idle: [Session] {
        filtered.filter { ($0.status == .idle || $0.status == .unknown) && $0.pendingInteractions == 0 }.sorted(by: byTime)
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

                if !active.isEmpty {
                    Section {
                        ForEach(active) { s in row(s) }
                    } header: {
                        SectionHeader(title: "进行中", count: active.count, tint: Theme.working)
                    }
                }

                if !finished.isEmpty {
                    Section {
                        ForEach(finished) { s in row(s) }
                    } header: {
                        SectionHeader(title: "刚完成", count: finished.count, tint: Theme.done)
                    }
                }

                if !idle.isEmpty {
                    Section {
                        ForEach(showAllIdle ? idle : Array(idle.prefix(8))) { s in row(s) }
                        if idle.count > 8 && !showAllIdle {
                            Button("显示全部 \(idle.count) 个") { withAnimation { showAllIdle = true } }
                                .font(.subheadline.weight(.medium))
                        }
                    } header: {
                        SectionHeader(title: "空闲", count: idle.count, tint: Theme.idle)
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
                        EmptyStateView(symbol: "chevron.left.forwardslash.chevron.right", title: "开始第一个开发会话",
                                       message: "在任意一台电脑的 herdr 里启动 Claude，手机上实时看、随时接手。",
                                       action: ("新建对话", { showNew = true }))
                    }
                    .listRowBackground(Color.clear)
                }
            }
            .listStyle(.insetGrouped)
            .navigationTitle("开发")
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
            .onChange(of: model.openChat, initial: true) { _, key in
                guard let key else { return }
                path = [key]
                model.openChat = nil
            }
            .onAppear {
                if model.debugShowNewChat { model.debugShowNewChat = false; showNew = true }
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
                }
                if let last = session.lastText, !last.isEmpty {
                    Text(Fmt.plain(last))
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
            }
        }
        .padding(.vertical, 4)
    }
}
