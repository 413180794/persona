import SwiftUI

/// Shared by the iOS and Mac Agents list; the tag is a sibling of the fold button, never nested inside it.
struct TodoSpaceTag: View {
    @Environment(AppModel.self) private var model
    let group: SpaceGroup
    var open: (String) -> Void

    var body: some View {
        if let todo = linkedTodo {
            Button { open(todo.id) } label: {
                Image(systemName: Theme.Icon.todo).font(Theme.Font.caption).foregroundStyle(.secondary)
                    .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("待办：\(todo.title)")
            .help("待办：\(todo.title)")
            .accessibilityIdentifier("todo.space." + group.id)
        }
    }

    private var linkedTodo: TodoItem? {
        let store = TodoStore.shared
        let appModel: AppModel = model
        guard let space = group.todoSpace, store.spacesSupported, TodosLogic.isOnTodosHub(sessionGroup: group.group, hubGroup: appModel.hub?.group) else { return nil }
        return TodosLogic.findTodoForSpace(store.todos, machineID: group.machineID, space: space)
    }
}

struct SpaceTodoMenu: View {
    @Environment(AppModel.self) private var model
    let group: SpaceGroup
    var link: () -> Void

    var body: some View {
        let store = TodoStore.shared
        let appModel: AppModel = model
        if let space = group.todoSpace, store.spacesSupported, TodosLogic.isOnTodosHub(sessionGroup: group.group, hubGroup: appModel.hub?.group) {
            let todo = TodosLogic.findTodoForSpace(store.todos, machineID: group.machineID, space: space)
            Button(action: link) {
                Label(todo == nil ? "挂到待办…" : "改挂到其他待办…", systemImage: Theme.Icon.todo).lineLimit(1)
            }
            .disabled(store.changingSpaces)
            if let todo {
                Button(role: .destructive) {
                    Task {
                        do {
                            try await store.setSpace(todoID: todo.id,
                                                     space: TodoItem.Space(machineID: group.machineID, space: space), linked: false)
                        } catch { AppModel.shared.show(error) }
                    }
                } label: {
                    Label("移出待办", systemImage: Theme.Icon.link).lineLimit(1)
                }
                .disabled(store.changingSpaces)
            }
        }
    }
}

/// The same detail view is embedded in the Mac todo page.
struct TodoSpacesSection: View {
    let todo: TodoItem
    let sessions: [Session]
    var machineName: (String) -> String
    /// The todos' hub group and whether a computer of it is connected (`TodosLogic.hasLiveAgent`).
    var hubGroup: String?
    var machineOnline: (String) -> Bool

    var body: some View {
        if TodoStore.shared.spacesSupported, let spaces = todo.spaces, !spaces.isEmpty {
            Section {
                ForEach(spaces, id: \.self) { project in
                    HStack(spacing: Theme.Space.m) {
                        VStack(alignment: .leading, spacing: Theme.Space.xs) {
                            Text("\(project.space) · \(machineName(project.machineID))")
                                .font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
                            if !TodosLogic.hasLiveAgent(in: project, sessions: sessions, hubGroup: hubGroup, machineOnline: machineOnline) {
                                Text("当前没有 Agent").font(Theme.Font.caption).foregroundStyle(.secondary)
                            }
                        }
                        Spacer()
                        Button(role: .destructive) {
                            Task {
                                do { try await TodoStore.shared.setSpace(todoID: todo.id, space: project, linked: false) }
                                catch { AppModel.shared.show(error) }
                            }
                        } label: {
                            Text("移出").foregroundStyle(Theme.danger)
                        }
                        .font(Theme.Font.captionStrong)
                        .lineLimit(1)
                        .buttonStyle(.borderless)
                        .accessibilityLabel("移出 \(project.space)")
                        .disabled(TodoStore.shared.changingSpaces)
                    }
                    .padding(.vertical, Theme.Space.xs)
                }
            } header: {
                Text("项目 · \(spaces.count)").font(Theme.Font.sectionHeader).foregroundStyle(.secondary)
            }
        }
    }
}
