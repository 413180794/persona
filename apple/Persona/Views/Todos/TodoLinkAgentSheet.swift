import BeautifulUI
import SwiftUI

/// The same picker links an individual Agent or an entire project, including a newly created todo.
struct TodoLinkAgentSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let target: TodoLinkTarget
    @State private var store = TodoStore.shared
    @State private var draft = TodoLinkDraft()

    init(session: Session) {
        target = .agent(machineID: session.machineID, sessionID: session.sessionID, currentTodoID: session.todo?.id)
    }

    init(space: TodoItem.Space) { target = .space(space) }

    private var currentTodoID: String? { target.currentTodoID(in: store.todos) }
    private var title: String {
        if target.isSpace { return currentTodoID == nil ? "挂到待办" : "改挂到其他待办" }
        return currentTodoID == nil ? "关联到待办" : "改关联到其他待办"
    }

    private var openTodos: [TodoItem] {
        let appModel: AppModel = model
        let group = appModel.hub?.group ?? ""
        let sessions = TodosLogic.linkedSessions(of: store.todos, hubGroup: group, session: appModel.session)
        return TodosLogic.sortTodos(store.openTodos, lookup: TodosLogic.makeSessionLookup(sessions))
    }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    if case .space(let project) = target {
                        Text("选择一个未完成的待办，把项目「\(project.space)」整组挂上：")
                    } else {
                        Text("选择要将此 Agent 关联到的待办：")
                    }
                }
                .font(Theme.Font.secondary)
                .foregroundStyle(.secondary)
                .listRowBackground(Color.clear)

                Section {
                    if openTodos.isEmpty {
                        Text("暂无未完成的待办").font(Theme.Font.secondary).foregroundStyle(.secondary)
                    }
                    ForEach(openTodos) { todo in
                        Button { draft.selectedTodoID = todo.id } label: {
                            HStack {
                                Text(todo.title).font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
                                Spacer()
                                if draft.selectedTodoID == todo.id {
                                    Image(systemName: Theme.Icon.done).foregroundStyle(Theme.accent)
                                }
                            }
                        }
                    }
                }

                Section {
                    if draft.creating {
                        TextField("输入新待办标题…", text: $draft.newTitle).font(Theme.Font.body)
                        Button("创建并挂上") { createAndLink() }
                            .lineLimit(1)
                            .disabled(draft.newTitle.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    } else {
                        Button { draft.creating = true; draft.newTitle = "" } label: {
                            Label("新建待办…", systemImage: Theme.Icon.add).lineLimit(1)
                        }
                    }
                    if let currentTodoID {
                        Button(target.isSpace ? "移出待办" : "解除关联", role: .destructive) {
                            submit { try await store.unlink(target, from: currentTodoID) }
                        }
                        .lineLimit(1)
                    }
                }
            }
            .accessibilityIdentifier("todo.link.list")
            .disabled(draft.busy)
            .themedPage()
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("取消") { dismiss() }.lineLimit(1).disabled(draft.busy)
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(target.isSpace ? "挂上" : "关联") {
                        submit { try await store.link(target, to: draft.selectedTodoID) }
                    }
                    .lineLimit(1)
                    .disabled(draft.selectedTodoID.isEmpty || draft.busy)
                }
            }
        }
        .onAppear { draft.reset(currentTodoID: currentTodoID) }
        .onChange(of: target.id) { _, _ in draft.reset(currentTodoID: currentTodoID) }
        .presentationDetents([.medium, .large])
    }

    private func submit(_ operation: @escaping @MainActor () async throws -> Void) {
        draft.busy = true
        Task {
            defer { draft.busy = false }
            do { try await operation(); dismiss() }
            catch { AppModel.shared.show(error) }
        }
    }

    private func createAndLink() {
        let title = draft.newTitle.trimmingCharacters(in: .whitespacesAndNewlines)
        submit {
            if let message = try await store.createAndLink(title: title, target: target) {
                AppModel.shared.toast = Toast(message, style: .error)
            }
        }
    }
}
