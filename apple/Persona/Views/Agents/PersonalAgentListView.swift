import SwiftUI

/// Opens the personal agents page from the Agents tab.
struct PersonalAgentsRoute: Hashable {}

/// The Lark gateway's personal agents (grok_bot, muse, …) with a view menu like the Lark gateway's (Group By / Sort By / Display /
/// New Group / Reset to Default / View All), search, pins (swipe or long-press) and custom sections. Prefs are shared with
/// the web through the hub. Re-pulls on the hub's `agents` events.
struct PersonalAgentListView: View {
    @Environment(AppModel.self) private var model
    @State private var sessions: [PersonalAgentSession] = []
    @State private var sources: [PersonalAgentSourceStatus] = []
    @State private var error: String?
    @State private var loaded = false
    @State private var query = ""
    @State private var naming = false
    @State private var newSection = ""
    private var store: PersonalAgentPrefsStore { .shared }

    private var prefs: PersonalAgentPrefs { store.prefs }
    private var groups: [PersonalAgentGroup] { PersonalAgentArrange.arrange(sessions, prefs: prefs, query: query) }
    private var allProviders: [String] {
        var seen: [String] = []
        for s in sessions where !seen.contains(s.provider) { seen.append(s.provider) }
        return seen
    }

    private func update(_ change: (inout PersonalAgentPrefs) -> Void) { store.update(model.api, change) }

    var body: some View {
        List {
            if let error {
                Section { Label(error, systemImage: Theme.Icon.error).foregroundStyle(Theme.danger) }
            }
            ForEach(sources.flatMap { $0.accounts ?? [] }, id: \.accountId) { a in
                if let e = a.lastError, !e.isEmpty {
                    Section {
                        Label("\(a.displayName ?? a.provider)：\(e)", systemImage: Theme.Icon.error)
                            .font(Theme.Font.caption).foregroundStyle(Theme.cautionText)
                    }
                }
            }
            ForEach(groups) { g in
                Section {
                    ForEach(g.sessions) { s in
                        NavigationLink(value: s) { PersonalAgentRow(session: s, pinned: prefs.isPinned(s.id)) }
                            .swipeActions(edge: .leading) {
                                Button { update { $0.togglePin(s.id) } } label: {
                                    prefs.isPinned(s.id) ? Label("取消置顶", systemImage: Theme.Icon.unpin) : Label("置顶", systemImage: Theme.Icon.pin)
                                }
                                .tint(.orange)
                            }
                            .contextMenu { rowMenu(s) }
                    }
                } header: {
                    if !g.label.isEmpty {
                        HStack(spacing: Theme.Space.xs) {
                            if g.id == "pinned" { Image(systemName: Theme.Icon.pinned).imageScale(.small) }
                            Text(g.label)
                            Spacer()
                            if g.unread > 0 { Text("\(g.unread)").monospacedDigit() }
                        }
                    }
                }
            }
            if loaded && sessions.isEmpty && error == nil {
                let hints = sources.compactMap(\.setupHint)
                if hints.isEmpty {
                    Text("没有个人 Agent 会话").foregroundStyle(.secondary)
                } else {
                    ForEach(hints, id: \.self) { hint in
                        Label(hint, systemImage: Theme.Icon.info).foregroundStyle(.secondary)
                    }
                }
            } else if loaded && groups.isEmpty && error == nil {
                Text("没有匹配的会话").foregroundStyle(.secondary)
            }
        }
        .navigationTitle("个人 Agent")
        .pageSearchable(text: $query, prompt: String(localized: "搜索会话"))
        .pageToolbar {
            ToolbarItem(placement: .primaryAction) { viewMenu }
        }
        .alert("新建分组", isPresented: $naming) {
            TextField("分组名称", text: $newSection)
            Button("取消", role: .cancel) { newSection = "" }
            Button("创建") {
                let n = newSection
                update { $0.addSection(n) }
                newSection = ""
            }
        } message: { Text("长按会话可以移到这个分组") }
        .overlay { if !loaded && error == nil { ProgressView() } }
        .refreshable { await load() }
        .task { await load() }
        .task {
            for await _ in PersonalAgentSync.shared.changes() { await load() }
        }
    }

    private var viewMenu: some View {
        Menu {
            Picker(selection: Binding(get: { prefs.view.groupBy }, set: { g in update { $0.view.group = g.rawValue } })) {
                ForEach(AgentGroupBy.allCases, id: \.self) { Text($0.label).tag($0) }
            } label: { Label("分组方式", systemImage: "square.stack") }
                .pickerStyle(.menu)
            Picker(selection: Binding(get: { prefs.view.sortBy }, set: { v in update { $0.view.sort = v.rawValue } })) {
                ForEach(AgentSortBy.allCases, id: \.self) { Text($0.label).tag($0) }
            } label: { Label("排序方式", systemImage: Theme.Icon.upDown) }
                .pickerStyle(.menu)
            Menu {
                Picker("显示", selection: Binding(get: { prefs.view.showMode }, set: { v in update { $0.view.show = v.rawValue } })) {
                    ForEach(AgentShow.allCases, id: \.self) { Text($0.label).tag($0) }
                }
                .pickerStyle(.inline)
                if allProviders.count > 1 {
                    Section("来源") {
                        ForEach(allProviders, id: \.self) { p in
                            Toggle(PersonalAgentArrange.providerLabel(p), isOn: Binding(
                                get: { prefs.view.providerFilter.isEmpty || prefs.view.providerFilter.contains(p) },
                                set: { _ in let all = allProviders; update { $0.toggleProvider(p, all: all) } }))
                        }
                    }
                }
            } label: { Label("显示", systemImage: Theme.Icon.view) }
            Divider()
            Button { naming = true } label: { Label("新建分组", systemImage: "folder.badge.plus") }
            if !prefs.sections.isEmpty {
                Menu {
                    ForEach(prefs.sections) { sec in
                        Button(role: .destructive) { update { $0.removeSection(sec.id) } } label: { Text(sec.name) }
                    }
                } label: { Label("删除分组", systemImage: "folder.badge.minus") }
            }
            Divider()
            Button { update { $0.resetView() } } label: { Label("重置为默认", systemImage: "arrow.counterclockwise") }
                .disabled(prefs.view.isDefault)
            Button {
                query = ""
                update { $0.view.show = nil; $0.view.providers = nil }
            } label: { Label("查看全部", systemImage: Theme.Icon.listView) }
        } label: {
            Image(systemName: prefs.view.isDefault ? Theme.Icon.filter : Theme.Icon.filterActive)
        }
        .accessibilityLabel("筛选与排序")
    }

    @ViewBuilder private func rowMenu(_ s: PersonalAgentSession) -> some View {
        Button { update { $0.togglePin(s.id) } } label: {
            prefs.isPinned(s.id) ? Label("取消置顶", systemImage: Theme.Icon.unpin) : Label("置顶", systemImage: Theme.Icon.pin)
        }
        if !prefs.sections.isEmpty {
            Menu {
                ForEach(prefs.sections) { sec in
                    Button { update { $0.assign(s.id, to: prefs.assign[s.id] == sec.id ? nil : sec.id) } } label: {
                        if prefs.assign[s.id] == sec.id { Label(sec.name, systemImage: Theme.Icon.done) } else { Text(sec.name) }
                    }
                }
                if prefs.assign[s.id] != nil {
                    Button("移出分组") { update { $0.assign(s.id, to: nil) } }
                }
            } label: { Label("移到分组", systemImage: Theme.Icon.folder) }
        }
    }

    private func load() async {
        guard let api = model.api else { return }
        async let p: Void = store.load(api)
        do {
            async let list = api.personalAgentSessions()
            async let st = api.personalAgentSources()
            sessions = try await list
            PersonalAgentIndex.update(sessions)
            sources = (try? await st) ?? sources
            error = nil
        } catch {
            // Say it plainly instead of showing stale rows as if they were current (§6 step 1).
            self.error = String(localized: "无法连接个人 Agent：\(error.localizedDescription)")
        }
        _ = await p
        loaded = true
    }
}

private struct PersonalAgentRow: View {
    let session: PersonalAgentSession
    var pinned = false

    var body: some View {
        HStack(spacing: Theme.Space.m) {
            if pinned { Image(systemName: Theme.Icon.pinned).imageScale(.small).foregroundStyle(.orange).accessibilityLabel("已置顶") }
            PersonalAgentAvatar(session: session, size: 32)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(session.title.isEmpty ? session.sessionKey : session.title).lineLimit(1)
                Text(session.rankTime, format: .relative(presentation: .named))
                    .font(Theme.Font.label).foregroundStyle(.secondary)
            }
            .padding(.leading, session.parentSessionKey == nil ? 0 : Theme.Space.l)
            Spacer(minLength: Theme.Space.s)
            if !session.canSend {
                Image(systemName: Theme.Icon.locked).foregroundStyle(.secondary)
            }
            if session.unreadCount > 0 {
                Text("\(session.unreadCount)")
                    .font(Theme.Font.labelStrong).foregroundStyle(.white)
                    .padding(.horizontal, Theme.Space.sm).padding(.vertical, Theme.Space.xxs)
                    .background(Capsule().fill(Theme.danger))
            }
        }
    }
}
