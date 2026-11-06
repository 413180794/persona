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
                        Label("\(a.displayName ?? a.provider): \(e)", systemImage: Theme.Icon.error)
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
                                    prefs.isPinned(s.id) ? Label("Unpin [Personal Agent List View c92179]", systemImage: Theme.Icon.unpin) : Label("Pin [Personal Agent List View 173f88]", systemImage: Theme.Icon.pin)
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
                    Text("No personal agent sessions").foregroundStyle(.secondary)
                } else {
                    ForEach(hints, id: \.self) { hint in
                        Label(hint, systemImage: Theme.Icon.info).foregroundStyle(.secondary)
                    }
                }
            } else if loaded && groups.isEmpty && error == nil {
                Text("No matching sessions").foregroundStyle(.secondary)
            }
        }
        .navigationTitle("Personal Agents")
        .pageSearchable(text: $query, prompt: String(localized: "Search Chats [Personal Agent List View feb955]"))
        .pageToolbar {
            ToolbarItem(placement: .primaryAction) { viewMenu }
        }
        .alert("New Group", isPresented: $naming) {
            TextField(String(localized: "Group name"), text: $newSection)
            Button("Cancel", role: .cancel) { newSection = "" }
            Button("Create") {
                let n = newSection
                update { $0.addSection(n) }
                newSection = ""
            }
        } message: { Text("Long-press a session to move it into this group.") }
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
            } label: { Label("Group By [Personal Agent List View 72148c]", systemImage: Theme.Icon.groupBy) }
                .pickerStyle(.menu)
            Picker(selection: Binding(get: { prefs.view.sortBy }, set: { v in update { $0.view.sort = v.rawValue } })) {
                ForEach(AgentSortBy.allCases, id: \.self) { Text($0.label).tag($0) }
            } label: { Label("Sort By", systemImage: Theme.Icon.upDown) }
                .pickerStyle(.menu)
            Menu {
                Picker("Display", selection: Binding(get: { prefs.view.showMode }, set: { v in update { $0.view.show = v.rawValue } })) {
                    ForEach(AgentShow.allCases, id: \.self) { Text($0.label).tag($0) }
                }
                .pickerStyle(.inline)
                if allProviders.count > 1 {
                    Section("Source") {
                        ForEach(allProviders, id: \.self) { p in
                            Toggle(PersonalAgentArrange.providerLabel(p), isOn: Binding(
                                get: { prefs.view.providerFilter.isEmpty || prefs.view.providerFilter.contains(p) },
                                set: { _ in let all = allProviders; update { $0.toggleProvider(p, all: all) } }))
                        }
                    }
                }
            } label: { Label("Display", systemImage: Theme.Icon.view) }
            Divider()
            Button { naming = true } label: { Label("New Group", systemImage: Theme.Icon.newGroup) }
            if !prefs.sections.isEmpty {
                Menu {
                    ForEach(prefs.sections) { sec in
                        Button(role: .destructive) { update { $0.removeSection(sec.id) } } label: { Text(sec.name) }
                    }
                } label: { Label("Delete Group", systemImage: Theme.Icon.deleteGroup) }
            }
            Divider()
            Button { update { $0.resetView() } } label: { Label("Reset to Default", systemImage: Theme.Icon.resetDefaults) }
                .disabled(prefs.view.isDefault)
            Button {
                query = ""
                update { $0.view.show = nil; $0.view.providers = nil }
            } label: { Label("View All", systemImage: Theme.Icon.listView) }
        } label: {
            Image(systemName: prefs.view.isDefault ? Theme.Icon.filter : Theme.Icon.filterActive)
        }
        .accessibilityLabel("Filter and sort")
    }

    @ViewBuilder private func rowMenu(_ s: PersonalAgentSession) -> some View {
        Button { update { $0.togglePin(s.id) } } label: {
            prefs.isPinned(s.id) ? Label("Unpin [Personal Agent List View c92179]", systemImage: Theme.Icon.unpin) : Label("Pin [Personal Agent List View 173f88]", systemImage: Theme.Icon.pin)
        }
        if !prefs.sections.isEmpty {
            Menu {
                ForEach(prefs.sections) { sec in
                    Button { update { $0.assign(s.id, to: prefs.assign[s.id] == sec.id ? nil : sec.id) } } label: {
                        if prefs.assign[s.id] == sec.id { Label(sec.name, systemImage: Theme.Icon.done) } else { Text(sec.name) }
                    }
                }
                if prefs.assign[s.id] != nil {
                    Button("Remove from Group") { update { $0.assign(s.id, to: nil) } }
                }
            } label: { Label("Move to Group", systemImage: Theme.Icon.folder) }
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
            self.error = String(localized: "Can't connect to personal agents: \(error.localizedDescription)")
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
            if pinned { Image(systemName: Theme.Icon.pinned).imageScale(.small).foregroundStyle(.orange).accessibilityLabel("Pinned [Personal Agent List View fb47db]") }
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
