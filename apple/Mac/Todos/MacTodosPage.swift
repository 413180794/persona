import BeautifulUI
import SwiftUI

struct MacTodosPage: View {
    @Environment(AppModel.self) private var model
    @State private var store = TodoStore.shared
    @State private var selectedTodoID: String?
    @State private var query = ""
    @State private var showCreate = false
    @State private var creatingTitle = ""
    @State private var creatingNotes = ""
    @State private var expandedDone = false

    @AppStorage("mac.todos.listWidth") private var listWidth: Double = Theme.Size.macTodosListIdeal
    @State private var dragging: Double?
    @Environment(\.tabIsActive) private var inFront

    private var allTodos: [TodoItem] {
        let lookup = TodosLogic.makeSessionLookup(AppModel.shared.allSessions(inFront: inFront))
        return TodosLogic.sortTodos(store.todos, lookup: lookup)
    }

    private var openList: [TodoItem] {
        allTodos.filter { !$0.isDone && matchesQuery($0) }
    }

    private var doneList: [TodoItem] {
        allTodos.filter { $0.isDone && matchesQuery($0) }
    }

    private func matchesQuery(_ todo: TodoItem) -> Bool {
        if query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return true }
        let q = query.localizedLowercase
        if todo.title.localizedLowercase.contains(q) { return true }
        if let notes = todo.notes, notes.localizedLowercase.contains(q) { return true }
        return false
    }

    /// Whether the done group is expanded for the current query: a non-empty query with done matches reveals the
    /// collapsed group; an empty query (same trim rule as `matchesQuery`) or a query without done matches leaves the
    /// current state untouched, so clearing the search never collapses a group the user opened (#3474).
    /// Pure (no AppKit/UIKit types), so nonisolated despite the target's default MainActor isolation: the unit tests
    /// call it from a synchronous nonisolated context (cf. `nonisolated enum MacPaste`).
    nonisolated enum SearchExpansion {
        static func doneExpanded(query: String, hasDoneMatches: Bool, current: Bool) -> Bool {
            if query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return current }
            return hasDoneMatches ? true : current
        }
    }

    var body: some View {
        HStack(spacing: 0) {
            listColumn
                .frame(width: min(max(dragging ?? listWidth, Theme.Size.macTodosListMin), Theme.Size.macTodosListMax))
            MacPanelResizeHandle { delta in
                dragging = min(max((dragging ?? listWidth) + delta, Theme.Size.macTodosListMin), Theme.Size.macTodosListMax)
            } ended: {
                if let dragging { listWidth = dragging }
                dragging = nil
            }
            detailColumn
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .background(Theme.pageBackground)
        .sheet(isPresented: $showCreate) {
            createSheet
        }
        .task {
            await store.refresh()
            #if DEBUG
            // `-openMacTodo <text>`: select the first todo whose title has the text (detail screenshots).
            if let match = UserDefaults.standard.string(forKey: "openMacTodo"),
               let picked = allTodos.first(where: { $0.title.contains(match) }) {
                selectedTodoID = picked.id
            }
            #endif
            if selectedTodoID == nil {
                selectedTodoID = openList.first?.id ?? doneList.first?.id
            }
        }
        .onChange(of: store.todos) { _, todos in
            // The selected todo can vanish from any path: the row menu, the detail page's delete (its dismiss
            // runs on the page's own stack root and does nothing here), or a push delete from another device.
            // Without this the id keeps pointing at the gone todo and the detail shows its tombstone forever.
            selectedTodoID = MacTodosSelection.resolve(
                selectedID: selectedTodoID,
                existing: todos,
                open: openList,
                done: doneList
            )
        }
    }

    private var listColumn: some View {
        VStack(spacing: 0) {
            searchField
            List(selection: $selectedTodoID) {
                if !(openList.isEmpty && doneList.isEmpty) {
                    if !openList.isEmpty {
                        Section("没完成 · \(openList.count)") {
                            ForEach(openList) { todo in
                                row(todo)
                            }
                        }
                    }

                    if !doneList.isEmpty {
                        Section {
                            DisclosureGroup(isExpanded: $expandedDone) {
                                ForEach(doneList) { todo in
                                    row(todo)
                                }
                            } label: {
                                Text("已完成 · \(doneList.count)")
                                    .font(Theme.Font.sectionHeader)
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                }
            }
            .listStyle(.sidebar)
            .scrollContentBackground(.hidden)
            // Over the list, not a row of it: a sidebar list's row is one line, and the message was cut to it.
            .overlay(alignment: .top) {
                if openList.isEmpty && doneList.isEmpty {
                    EmptyStateView(
                        symbol: Theme.Icon.todo,
                        title: query.isEmpty ? String(localized: "没有待办") : String(localized: "没有找到待办"),
                        message: query.isEmpty ? String(localized: "记下一件要做的事，交给 Agent 执行并跟踪进度。") : String(localized: "换个搜索词试试"),
                        action: query.isEmpty ? (String(localized: "新建待办"), { showCreate = true }) : nil
                    )
                }
            }
            // A done-only search must not leave the matching group hidden behind the collapsed disclosure (#3474).
            .onChange(of: query) { _, _ in
                expandedDone = SearchExpansion.doneExpanded(query: query, hasDoneMatches: !doneList.isEmpty, current: expandedDone)
            }
        }
        // In the list column's own title row, at its trailing edge, as the Agents list's buttons (#1223, #2318): as a
        // window toolbar item its glyph came out gray, as if disabled; the open todo's menu keeps the window's edge.
        .overlay(alignment: .topTrailing) {
            MacTitleRowButton(String(localized: "新建待办"), symbol: Theme.Icon.add) {
                creatingTitle = ""
                creatingNotes = ""
                showCreate = true
            }
            .padding(.trailing, Theme.Space.l)
            .frame(height: Theme.Size.macToolbarHeight)
            .ignoresSafeArea(.container, edges: .top)
        }
    }

    private func row(_ todo: TodoItem) -> some View {
        let lookup = TodosLogic.makeSessionLookup(AppModel.shared.allSessions(inFront: inFront))
        let status = TodosLogic.todoStatus(todo, lookup: lookup)
        let urgent = TodosLogic.mostUrgentAgent(todo, lookup: lookup)
        let activeAgents = todo.agents.filter { $0.releasedAt == nil }

        return Button {
            selectedTodoID = todo.id
        } label: {
            HStack(alignment: .top, spacing: Theme.Space.s) {
                Button {
                    store.toggleDone(todo)
                } label: {
                    Image(systemName: todo.isDone ? Theme.Icon.successFilled : Theme.Icon.unchecked)
                        .font(Theme.Font.secondary)
                        .foregroundStyle(todo.isDone ? Theme.positive : .secondary)
                }
                .buttonStyle(.plain)

                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    HStack {
                        Text(todo.title)
                            .font(Theme.Font.bodyStrong)
                            .foregroundStyle(todo.isDone ? Color.secondary : Color.primary)
                            .strikethrough(todo.isDone)
                            .lineLimit(1)
                        Spacer()
                        if let d = todo.updatedAt ?? todo.createdAt {
                            Text(Fmt.relative(d))
                                .font(Theme.Font.caption)
                                .foregroundStyle(.secondary)
                        }
                    }

                    HStack(spacing: Theme.Space.xs) {
                        StatusDot(state: urgent?.status ?? (status == .done ? .idle : .unknown), size: Theme.Size.dotS)
                        Text(todo.statusLabel(status))
                            .font(Theme.Font.labelStrong)
                            .foregroundStyle(status.todoTone.text)
                        if !activeAgents.isEmpty {
                            Text("· \(activeAgents.count) 个 Agent")
                                .font(Theme.Font.caption)
                                .foregroundStyle(.secondary)
                        }
                    }

                    if let reason = todo.autoDoneReason {
                        Text(reason)
                            .font(Theme.Font.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    } else if let summary = mostUrgentSummary(urgent: urgent) {
                        Text(summary)
                            .font(Theme.Font.caption)
                            .foregroundStyle(urgent?.status == .needsYou ? Theme.danger : .secondary)
                            .lineLimit(1)
                    }
                }
            }
            .padding(.vertical, Theme.Space.xxs)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .tag(todo.id)
        .contextMenu {
            Button(todo.isDone ? "重新打开" : "完成") {
                store.toggleDone(todo)
            }
            Divider()
            Button(role: .destructive) {
                Task {
                    do { try await store.delete(id: todo.id) } catch { AppModel.shared.show(error) }
                }
            } label: {
                Label("删除", systemImage: Theme.Icon.delete)
            }
        }
    }

    private func mostUrgentSummary(urgent: TodoMostUrgent?) -> String? {
        guard let urgent else { return nil }
        if urgent.isFailed, let err = urgent.agent.startError, !err.isEmpty {
            return String(localized: "启动失败：\(err)")
        }
        if let s = urgent.session {
            if let last = s.lastText, !last.isEmpty {
                return Fmt.plain(last)
            }
            return s.displayTitle
        }
        if let t = urgent.agent.title, !t.isEmpty {
            return t
        }
        return nil
    }

    private var searchField: some View {
        HStack(spacing: Theme.Space.xs) {
            Image(systemName: Theme.Icon.search)
                .font(Theme.Font.caption)
                .foregroundStyle(.secondary)
            TextField("搜索待办", text: $query)
                .textFieldStyle(.plain)
            if !query.isEmpty {
                Button { query = "" } label: {
                    Image(systemName: Theme.Icon.clear).foregroundStyle(.tertiary)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.horizontal, Theme.Space.s)
        .padding(.vertical, Theme.Space.xs)
        .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
        .padding(.horizontal, Theme.Space.s)
        .padding(.top, Theme.Space.s)
    }

    @ViewBuilder private var detailColumn: some View {
        if let id = selectedTodoID {
            NavigationStack {
                TodoDetailView(todoID: id)
                    .id(id) // another todo gets a fresh page, so its title and notes are read from the store again
            }
        } else if openList.isEmpty && doneList.isEmpty {
            // One empty state a page (docs/mac-design.md): the list's says there is nothing, the detail stays plain.
            Color.clear
        } else {
            SplitPlaceholder(symbol: Theme.Icon.todo, title: String(localized: "选一个待办"), message: String(localized: "左边列表里选一个待办，详情会在这里打开。"))
        }
    }

    private var createSheet: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("标题", text: $creatingTitle)
                    TextField("说明（交给 Agent 时会一起发过去）", text: $creatingNotes, axis: .vertical)
                        .lineLimit(3...8)
                }
            }
            .navigationTitle("新建待办")
            .sheetCancel(unsaved: !(creatingTitle + creatingNotes).trimmingCharacters(in: .whitespacesAndNewlines).isEmpty) {
                showCreate = false
            }
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("添加") {
                        let t = creatingTitle.trimmingCharacters(in: .whitespacesAndNewlines)
                        let n = creatingNotes.trimmingCharacters(in: .whitespacesAndNewlines)
                        // The sheet closes only once the todo is saved: a failed save keeps the typed title and notes.
                        Task {
                            do {
                                let created = try await store.create(title: t, notes: n.isEmpty ? nil : n)
                                showCreate = false
                                selectedTodoID = created.id
                            } catch {
                                AppModel.shared.show(error)
                            }
                        }
                    }
                    .lineLimit(1)
                    .disabled(creatingTitle.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
            .macSheet()
        }
    }
}

/// Pure rule for the Mac todos page's selection after the store's list changes (#3471).
nonisolated enum MacTodosSelection {
    /// The id the detail column should show: the selected id while its todo is still in `existing` (the full
    /// store list, so a search that hides it does not move the selection); otherwise the first open todo, then
    /// the first done todo (the lists as currently filtered and sorted), then nil when nothing remains.
    static func resolve(selectedID: String?, existing: [TodoItem], open: [TodoItem], done: [TodoItem]) -> String? {
        if let selectedID, existing.contains(where: { $0.id == selectedID }) {
            return selectedID
        }
        return open.first?.id ?? done.first?.id
    }
}
