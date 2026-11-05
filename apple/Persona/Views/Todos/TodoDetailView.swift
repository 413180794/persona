import BeautifulUI
import SwiftUI

struct TodoDetailView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.tabIsActive) private var inFront
    @Environment(\.dismiss) private var dismiss
    var todoID: String

    @State private var store = TodoStore.shared
    @State private var text = TodoEditorText()
    @FocusState private var titleFocused: Bool
    @State private var isEditingNotes = false
    @State private var editingNotesText = ""
    @State private var showHandoff = false
    @State private var showDeleteConfirm = false
    @State private var showReleased = false
    /// The PR its work is in, opened in the app's link sheet (docs/ios.md, 链接在 App 内打开).
    @State private var link: LinkTarget?

    private var currentTodo: TodoItem? {
        store.todo(id: todoID)
    }

    var body: some View {
        Group {
            if let todo = currentTodo {
                content(todo)
            } else {
                VStack(spacing: Theme.Space.l) {
                    EmptyStateView(symbol: Theme.Icon.todo, title: "待办不存在或已删除", message: "")
                }
                .themedPage()
            }
        }
        .detailTitle(currentTodo?.title ?? "待办详情")
        .navigationBarTitleDisplayMode(.inline)
        .pageToolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Button(role: .destructive) {
                        showDeleteConfirm = true
                    } label: {
                        Label("删除待办", systemImage: Theme.Icon.delete)
                    }
                } label: {
                    Image(systemName: Theme.Icon.more)
                }
            }
        }
        .sheet(isPresented: $showHandoff) {
            if let todo = currentTodo {
                TodoHandoffSheet(todo: todo)
            }
        }
        #if os(macOS)
        .macLinkPanel($link)
        #else
        .sheet(item: $link) { target in
            LinkSheet(target: target, key: model.assistantMain ?? model.assistantCurrent)
        }
        #endif
        .alert("删除待办", isPresented: $showDeleteConfirm) {
            Button("删除", role: .destructive) {
                Task { await deleteTodo() }
            }
            Button("取消", role: .cancel) {}
        } message: {
            Text("确定要删除这个待办吗？关联的 Agent 会话不会被删除。")
        }
        .onChange(of: currentTodo) { _, todo in
            // Another todo (the Mac list picked it), or an edit made elsewhere: take the stored text, but never over a
            // field the user is typing in.
            if let todo { text.reseed(from: todo, editingTitle: titleFocused, editingNotes: isEditingNotes) }
        }
        .onAppear {
            #if DEBUG
            if UserDefaults.standard.bool(forKey: "showTodoHandoff") {
                showHandoff = true
            }
            #endif
            if let todo = currentTodo {
                text = TodoEditorText(todo)
            }
        }
    }

    /// Deletes the todo and leaves its page; a failed delete keeps the page and shows the error.
    private func deleteTodo() async {
        do {
            try await store.delete(id: todoID)
            dismiss()
        } catch {
            report(error)
        }
    }

    /// Shows a failed write's error, from a plain method rather than inside the async block.
    private func report(_ failure: any Error) {
        AppModel.shared.show(failure)
    }

    private func content(_ todo: TodoItem) -> some View {
        let sessions = model.allSessions(inFront: inFront)
        let lookup = TodosLogic.makeSessionLookup(sessions)
        let status = TodosLogic.todoStatus(todo, lookup: lookup)
        let activeAgents = todo.agents.filter { $0.releasedAt == nil }
        let releasedAgents = todo.agents.filter { $0.releasedAt != nil }

        return List {
            Section {
                VStack(alignment: .leading, spacing: Theme.Space.s) {
                    TextField("待办标题", text: $text.title)
                        .font(Theme.Font.title)
                        .focused($titleFocused)
                        .onSubmit {
                            let t = text.title.trimmingCharacters(in: .whitespacesAndNewlines)
                            if !t.isEmpty && t != todo.title {
                                Task {
                                    do { _ = try await store.update(id: todo.id, title: t) } catch { model.show(error) }
                                }
                            }
                        }

                    HStack(spacing: Theme.Space.sm) {
                        Chip(text: todo.statusLabel(status), look: .tone(status.todoTone))
                        Text("·")
                            .foregroundStyle(.secondary)
                        Text("\(activeAgents.count) 个 Agent")
                            .font(Theme.Font.caption)
                            .foregroundStyle(.secondary)
                        if let d = todo.createdAt {
                            Text("·")
                                .foregroundStyle(.secondary)
                            Text("\(Fmt.relative(d))创建")
                                .font(Theme.Font.caption)
                                .foregroundStyle(.secondary)
                        }
                    }

                    if todo.autoDone {
                        // The hub closed it (docs/todos.md「自动完成」): say why, and how to undo it.
                        Text(todo.autoDoneReason.map { "Agent 停下后判断已经做完，自动标成了完成：\($0)。没做完就点「重新打开」。" }
                             ?? "Agent 停下后判断已经做完，自动标成了完成。没做完就点「重新打开」。")
                            .font(Theme.Font.caption)
                            .foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                .padding(.vertical, Theme.Space.xs)
            }

            stageSection(todo)

            Section {
                VStack(alignment: .leading, spacing: Theme.Space.s) {
                    HStack {
                        Text("说明")
                            .font(Theme.Font.caption)
                            .foregroundStyle(.secondary)
                        Spacer()
                        Button(isEditingNotes ? "完成" : "编辑") {
                            if isEditingNotes {
                                let n = editingNotesText.trimmingCharacters(in: .whitespacesAndNewlines)
                                Task {
                                    do {
                                        _ = try await store.update(id: todo.id, notes: n)
                                        text.notes = n
                                        isEditingNotes = false
                                    } catch {
                                        // Still editing: what was typed stays in the field, and the error says why it did not save.
                                        model.show(error)
                                    }
                                }
                            } else {
                                editingNotesText = text.notes
                                isEditingNotes = true
                            }
                        }
                        .font(Theme.Font.captionStrong)
                        .lineLimit(1)
                        .foregroundStyle(Theme.accent)
                    }

                    if isEditingNotes {
                        TextField("待办说明", text: $editingNotesText, axis: .vertical)
                            .lineLimit(3...8)
                            .textFieldStyle(.plain)
                            .padding(Theme.Space.s)
                            .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
                    } else if !text.notes.isEmpty {
                        Text(text.notes)
                            .font(Theme.Font.body)
                            .foregroundStyle(Color.primary)
                    } else {
                        Text("暂无说明")
                            .font(Theme.Font.body)
                            .foregroundStyle(.tertiary)
                    }
                }
                .padding(.vertical, Theme.Space.xs)
            }

            TodoSpacesSection(todo: todo, sessions: sessions, machineName: machineName, hubGroup: model.hub?.group,
                              machineOnline: { model.machine(model.hubRef($0), inFront: inFront)?.online ?? false })

            Section {
                if activeAgents.isEmpty {
                    Text("暂无运行中的 Agent，点击下方「交给 Agent」指派。")
                        .font(Theme.Font.secondary)
                        .foregroundStyle(.secondary)
                        .padding(.vertical, Theme.Space.xs)
                } else {
                    ForEach(activeAgents, id: \.sessionID) { a in
                        agentRow(a, todo: todo, isReleased: false, lookup: lookup)
                    }
                }
            } header: {
                Text("Agents · \(activeAgents.count)")
                    .font(Theme.Font.sectionHeader)
                    .foregroundStyle(.secondary)
            }

            if !releasedAgents.isEmpty {
                Section {
                    DisclosureGroup(isExpanded: $showReleased) {
                        ForEach(releasedAgents, id: \.sessionID) { a in
                            agentRow(a, todo: todo, isReleased: true, lookup: lookup)
                        }
                    } label: {
                        Text("参与过 · \(releasedAgents.count)")
                            .font(Theme.Font.sectionHeader)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
        .themedPage()
        .safeAreaInset(edge: .bottom) {
            bottomBar(todo)
        }
    }

    /// How far the hub says the work got (`stage`, #2776) and the PR its work is in (`pr_url`, #2768), when it has
    /// either; a hub that sends neither draws nothing here.
    @ViewBuilder private func stageSection(_ todo: TodoItem) -> some View {
        let stage = todo.todoStage
        let pr = todo.prLink
        if stage != nil || pr != nil {
            Section {
                VStack(alignment: .leading, spacing: Theme.Space.m) {
                    if let stage {
                        TodoStageBar(stage: stage)
                    }
                    if let pr {
                        Button { link = LinkTarget(url: pr) } label: {
                            Label(todo.prLabel ?? "查看 PR", systemImage: Theme.Icon.pullRequest)
                                .font(Theme.Font.captionStrong)
                                .lineLimit(1)
                        }
                        .themedButton()
                    }
                }
                .padding(.vertical, Theme.Space.xs)
            }
        }
    }

    /// The machine's name as the other pages show it (`model.machine(...)?.title`, the same as the Agents rows): the hub's
    /// own machines, since a todo belongs to the hub (`AppModel.hubRef`).
    private func machineName(_ id: String) -> String {
        model.machine(model.hubRef(id), inFront: inFront)?.title ?? id
    }

    private func agentRow(_ a: TodoItem.Agent, todo: TodoItem, isReleased: Bool, lookup: @escaping TodosLogic.SessionLookup) -> some View {
        let key = SessionKey(group: model.hub?.group ?? "", machineID: a.machineID, sessionID: a.sessionID)
        let s = lookup(a.machineID, a.sessionID)
        let isFailed = a.startError.map { !$0.isEmpty } ?? false
        let title = a.title ?? s?.displayTitle ?? "Agent 会话"
        let subtitle: String = {
            if isFailed, let err = a.startError {
                return "启动失败：\(err)"
            }
            if isReleased {
                return "已结束"
            }
            if let s, let last = s.lastText, !last.isEmpty {
                return Fmt.plain(last)
            }
            return a.cwd ?? ""
        }()

        return Button {
            dismiss()
            openTodoAgentChat(key, in: model)
        } label: {
            HStack(alignment: .top, spacing: Theme.Space.m) {
                if isFailed {
                    Image(systemName: Theme.Icon.errorFilled)
                        .foregroundStyle(Theme.danger)
                        .font(Theme.Font.body)
                        .padding(.top, Theme.Space.xs)
                } else {
                    StatusDot(state: isReleased ? .idle : (s?.state ?? .offline), size: Theme.Size.dotL)
                        .padding(.top, Theme.Space.xs)
                }

                VStack(alignment: .leading, spacing: Theme.Space.xs) {
                    HStack {
                        Text(title)
                            .font(Theme.Font.bodyStrong)
                            .lineLimit(1)
                        Spacer()
                        if let d = a.linkedAt {
                            Text(Fmt.relative(d))
                                .font(Theme.Font.label)
                                .foregroundStyle(.secondary)
                        }
                    }

                    let place = [machineName(a.machineID), s?.spaceName, s?.slotLabel].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
                    if !place.isEmpty {
                        Text(place)
                            .font(Theme.Font.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }

                    if let source = TodosLogic.agentSource(a, lookup: lookup) {
                        Text(source)
                            .font(Theme.Font.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }

                    Text(subtitle)
                        .font(Theme.Font.secondary)
                        .foregroundStyle(isFailed || s?.state == .needsYou ? Theme.danger : .secondary)
                        .lineLimit(2)
                }
                Image(systemName: Theme.Icon.disclosure)
                    .font(Theme.Font.label)
                    .foregroundStyle(.tertiary)
                    .padding(.top, Theme.Space.xs)
            }
            .padding(.vertical, Theme.Space.xs)
        }
        .buttonStyle(.plain)
        .swipeActions(edge: .trailing) {
            Button(role: .destructive) {
                Task {
                    do {
                        try await store.unlinkAgent(todoID: todo.id, machineID: a.machineID, sessionID: a.sessionID)
                    } catch {
                        model.show(error)
                    }
                }
            } label: {
                Label(isFailed ? "移除" : "解除关联", systemImage: isFailed ? Theme.Icon.delete : Theme.Icon.link)
            }
        }
    }

    private func bottomBar(_ todo: TodoItem) -> some View {
        HStack(spacing: Theme.Space.m) {
            Button {
                showHandoff = true
            } label: {
                HStack(spacing: Theme.Space.xs) {
                    Image(systemName: Theme.Icon.handToAI)
                    Text("交给 Agent").lineLimit(1).minimumScaleFactor(0.8)
                }
                .font(Theme.Font.heading)
                .scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.wellS)
            }
            .themedButton(prominent: true)
            .controlSize(.large)

            Button {
                store.toggleDone(todo)
            } label: {
                HStack(spacing: Theme.Space.xs) {
                    Image(systemName: todo.isDone ? Theme.Icon.undo : Theme.Icon.done)
                    Text(todo.isDone ? "重新打开" : "完成").lineLimit(1).minimumScaleFactor(0.8)
                }
                .font(Theme.Font.heading)
                .scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.wellS)
            }
            .themedButton(prominent: false)
            .controlSize(.large)
        }
        .padding(.horizontal, Theme.Space.page)
        .padding(.vertical, Theme.Space.s)
        .background(Theme.pageBackground)
    }
}

/// Opens an Agent's chat from a todo. On Mac the todos section gives way first, as it does for every other way into a
/// chat (`MacShellState.willOpen`); the hand-off sheet and the todo's Agent rows share this.
@MainActor func openTodoAgentChat(_ key: SessionKey, in model: AppModel) {
    #if os(macOS)
    MacShellState.shared.willOpen()
    #endif
    model.open(key)
}
