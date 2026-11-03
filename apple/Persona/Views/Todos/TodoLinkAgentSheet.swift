import BeautifulUI
import SwiftUI

struct TodoLinkAgentSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var session: Session

    @State private var store = TodoStore.shared
    @State private var selectedTodoID: String = ""

    private var openTodos: [TodoItem] {
        let appModel: AppModel = model
        let group = appModel.hub?.group ?? ""
        let sessions = TodosLogic.linkedSessions(of: store.todos, hubGroup: group, session: appModel.session)
        let lookup = TodosLogic.makeSessionLookup(sessions)
        return TodosLogic.sortTodos(store.todos.filter { !$0.isDone }, lookup: lookup)
    }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text("选择要将此 Agent 关联到的待办：")
                        .font(Theme.Font.secondary)
                        .foregroundStyle(.secondary)
                }
                .listRowBackground(Color.clear)

                Section {
                    ForEach(openTodos) { todo in
                        Button {
                            selectedTodoID = todo.id
                        } label: {
                            HStack {
                                Text(todo.title)
                                    .font(Theme.Font.body)
                                    .foregroundStyle(Color.primary)
                                Spacer()
                                if selectedTodoID == todo.id {
                                    Image(systemName: Theme.Icon.done)
                                        .foregroundStyle(Theme.accent)
                                }
                            }
                        }
                    }
                }
            }
            .themedPage()
            .navigationTitle("关联到待办")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("取消") { dismiss() }
                        .lineLimit(1)
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("关联") {
                        let tid = selectedTodoID
                        Task {
                            do {
                                try await store.linkAgent(todoID: tid, machineID: session.machineID, sessionID: session.sessionID)
                                dismiss()
                            } catch {
                                AppModel.shared.show(error)
                            }
                        }
                    }
                    .lineLimit(1)
                    .disabled(selectedTodoID.isEmpty)
                }
            }
        }
        .presentationDetents([.medium, .large])
    }
}
