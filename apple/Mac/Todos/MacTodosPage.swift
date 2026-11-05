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
        .pageToolbar {
            MacToolbarMiddle() // keeps these buttons off the page's title at the narrowest window (#830, #853)
            ToolbarItem(placement: .primaryAction) {
                Button {
                    creatingTitle = ""
                    creatingNotes = ""
                    showCreate = true
                } label: {
                    Label("新建待办", systemImage: Theme.Icon.add)
                }
            }
        }
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
                        title: query.isEmpty ? "没有待办" : "没有找到待办",
                        message: query.isEmpty ? "记下一件要做的事，交给 Agent 执行并跟踪进度。" : "换个搜索词试试",
                        action: query.isEmpty ? ("新建待办", { showCreate = true }) : nil
                    )
                }
            }
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
            return "启动失败：\(err)"
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
        } else {
            SplitPlaceholder(symbol: Theme.Icon.todo, title: "选一个待办", message: "左边列表里选一个待办，详情会在这里打开。")
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
