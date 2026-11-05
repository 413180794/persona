import SwiftUI

// The Agents list's menus depend on nothing that changes with each push from the hub: the list is drawn again on
// every one (sessions, machines), and a menu drawn again with it is handed to UIKit again — the open ⋯ menu jumped back
// to its top under the finger choosing a machine (2026-10-31), a row's long-press menu was rebuilt for every row on
// every push. The ⋯ menu sits in the toolbar of `ChatsView`, which does not follow the list (on the Mac in the shell's,
// `MacShell`: open, it stayed put through `/mock/churn` pushes and a machine could be picked, 2026-11-02); a row's is
// equal while what it offers is. What the actions need at tap time (the sections listed now, the session as it is now) comes from
// an object the views do not observe. `LiveUpdateTests` covers it.

/// A machine as the ⋯ menu shows it: not its live state (last seen, online), which changes with every push.
nonisolated struct MachineChoice: Equatable, Identifiable {
    /// `MachineRef.path`: the machine id, "group|id" for another group of hubs (what the filter remembers).
    var id: String
    /// With several groups, with the group's name (`Machine.title`).
    var name: String
    var symbol: String

    @MainActor init(_ m: Machine) {
        id = m.ref.path
        name = m.title
        symbol = m.symbol
    }
}

/// The menu at the top left of Agents: grouping, 卡片 / 细列表, folding, the machine filter, 机器. Shows the chosen
/// machine's name in the corner; "全部" keeps the plain filter icon.
struct AgentsListMenu: View {
    @Environment(AppModel.self) private var model
    var actions: Actions

    /// Kept current by the list (`update`), read when an item is tapped.
    @MainActor final class Actions {
        private(set) var sections: [String] = []
        private var foldAll: ([String]) -> Void = { _ in }
        private var unfoldAll: () -> Void = {}
        /// Not `openMonitor` / `openMachines`: a stored property's name is in the Release binary, and those are DEBUG
        /// launch arguments the release check looks for (TestFlight 50 stopped on one).
        private var onMachines: () -> Void = {}

        /// `sections`: the fold ids of what the list shows now.
        func update(sections: [String], foldAll: @escaping ([String]) -> Void, unfoldAll: @escaping () -> Void,
                    onMachines: @escaping () -> Void) {
            self.sections = sections
            self.foldAll = foldAll
            self.unfoldAll = unfoldAll
            self.onMachines = onMachines
        }

        func collapse() { foldAll(sections) }
        func expand() { unfoldAll() }
        func machinesPage() { onMachines() }
    }

    @StoredValue("dev.machineFilter") private var machineFilter = ""
    @StoredValue(HubFilter.key) private var hubFilter = ""
    @StoredValue("dev.grouping") private var grouping: DevGrouping = .standard
    /// The grouping the list shows: a stored 按待办 is 按项目 on a hub without todos (`DevGrouping.resolved`).
    private var shownGrouping: DevGrouping { DevGrouping.resolved(grouping, supportsTodos: TodoStore.shared.isSupported) }
    private var groupingChoice: Binding<DevGrouping> { Binding(get: { shownGrouping }, set: { grouping = $0 }) }
    @StoredValue("dev.compact") private var compact = false

    /// The shell (docs/ios.md「新外壳」) shows these inside its 工作 switch's menu: the items, without a button of their own.
    var inline = false

    var body: some View {
        // Only the machines' names and icons: not their live state, which changes with every push.
        let machines = model.machineChoices
        let chosen = machines.first { $0.id == machineFilter }
        let hub = model.isMultiHub ? HubFilter.group(hubFilter).map { model.groupName($0) } : nil
        if inline {
            items(machines)
        } else {
            Menu { items(machines) } label: { label(chosen, hub: hub) }
                .accessibilityLabel((hub.map { "当前 hub：\($0)，" } ?? "") + (chosen.map { "当前机器：\($0.name)，" } ?? "")
                                    + "分组：" + shownGrouping.title)
        }
    }

    @ViewBuilder private func items(_ machines: [MachineChoice]) -> some View {
            #if DEBUG || PERF_SIGNPOST
            let _ = PerfTrace.body("ListMenu.content")
            #endif
            Picker("分组", selection: groupingChoice) {
                ForEach(DevGrouping.offered(supportsTodos: TodoStore.shared.isSupported)) { g in Label(g.title, systemImage: g.symbol).tag(g) }
            }
            Picker("显示", selection: $compact) {
                Label("卡片", systemImage: "rectangle.grid.1x2").tag(false)
                Label("细列表", systemImage: Theme.Icon.listView).tag(true)
            }
            Section {
                Button {
                    withAnimation(Theme.Motion.standard) { actions.collapse() }
                } label: { Label("全部折叠", systemImage: Theme.Icon.collapseAll) }
                Button {
                    withAnimation(Theme.Motion.standard) { actions.expand() }
                } label: { Label("全部展开", systemImage: "rectangle.expand.vertical") }
            }
            if model.isMultiHub {
                // Several groups of hubs (docs/multi-hub.md「按组筛选」): one group's agents only.
                Picker("hub", selection: Binding(get: { hubFilter }, set: { hubFilter = $0; machineFilter = "" })) {
                    Label("全部 hub", systemImage: Theme.Icon.hubGroup).tag("")
                    ForEach(model.groupIDs, id: \.self) { g in
                        Label(model.groupName(g), systemImage: Theme.Icon.hubGroup).tag(HubFilter.value(g))
                    }
                }
            }
            Picker("机器", selection: $machineFilter) {
                Label("全部机器", systemImage: "square.grid.2x2").tag("")
                ForEach(machines.filter { m in HubFilter.matches(hubFilter, MachineRef(path: m.id).group) }) { m in
                    Label(m.name, systemImage: m.symbol).tag(m.id)
                }
            }
            Section {
                Button { actions.machinesPage() } label: {
                    Label("机器", systemImage: Theme.Icon.machines)
                }
            }
    }

    @ViewBuilder private func label(_ chosen: MachineChoice?, hub: String?) -> some View {
            if let m = chosen {
                HStack(spacing: Theme.Space.sm) {
                    Image(systemName: m.symbol)
                    Text(m.name).lineLimit(1)
                    Image(systemName: Theme.Icon.expand).font(Theme.Font.labelHeavy)
                }
                .font(Theme.Font.secondaryStrong)
                .fixedSize()
            } else if let hub {
                // One group of hubs shown (several groups): its name in the corner, like a machine's.
                HStack(spacing: Theme.Space.sm) {
                    Image(systemName: Theme.Icon.hubGroup)
                    Text(hub).lineLimit(1)
                    Image(systemName: Theme.Icon.expand).font(Theme.Font.labelHeavy)
                }
                .font(Theme.Font.secondaryStrong)
                .fixedSize()
            } else {
                Image(systemName: shownGrouping == .standard ? "line.3.horizontal.decrease" : shownGrouping.symbol)
            }
    }
}

/// A row's long-press menu in Agents. Equal while the facts it shows are: the row's session changes with every push
/// while its agent works (a new last line), and the menu held open on it was rebuilt each time.
struct AgentRowMenu: View, Equatable {
    @Environment(AppModel.self) private var model
    nonisolated let key: SessionKey
    nonisolated let pinned: Bool
    nonisolated let muted: Bool
    nonisolated let looping: Bool
    nonisolated let live: Bool
    nonisolated var terminal = true
    nonisolated var attachable = false
    /// 删除对话 is offered (`Session.canDelete`).
    nonisolated var deletable = false
    nonisolated let actions: Actions

    @MainActor final class Actions {
        private var renameHandler: (Session) -> Void = { _ in }
        private var closeHandler: (Session) -> Void = { _ in }
        private var linkTodoHandler: (Session) -> Void = { _ in }
        private var deleteHandler: (Session) -> Void = { _ in }

        func update(rename: @escaping (Session) -> Void, close: @escaping (Session) -> Void, linkTodo: @escaping (Session) -> Void = { _ in },
                    delete: @escaping (Session) -> Void = { _ in }) {
            renameHandler = rename
            closeHandler = close
            linkTodoHandler = linkTodo
            deleteHandler = delete
        }

        func rename(_ s: Session) { renameHandler(s) }
        func close(_ s: Session) { closeHandler(s) }
        func linkTodo(_ s: Session) { linkTodoHandler(s) }
        func delete(_ s: Session) { deleteHandler(s) }
    }

    nonisolated static func == (a: Self, b: Self) -> Bool {
        a.key == b.key && a.pinned == b.pinned && a.muted == b.muted && a.looping == b.looping && a.live == b.live
            && a.terminal == b.terminal && a.attachable == b.attachable && a.deletable == b.deletable
            && a.actions === b.actions
    }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("RowMenu.content")
        #endif
        // The session as it is when an item is tapped (all of it: 交给 Persona sends its last line).
        let now = { model.latest(key) }
        #if os(macOS)
        MacOpenInWindowButton(key: key)
        Divider()
        #elseif os(visionOS)
        VisionOpenInWindowButton(key: key)
        Divider()
        #endif
        Button { model.togglePin(key) } label: {
            Label(pinned ? "取消置顶" : "置顶", systemImage: pinned ? Theme.Icon.unpin : Theme.Icon.pin)
        }
        if terminal {
            Button { if let s = now() { actions.rename(s) } } label: { Label("重命名…", systemImage: Theme.Icon.edit) }
        }
        Button { model.toggleMute(key) } label: {
            Label(muted ? "开启通知" : "屏蔽通知", systemImage: muted ? Theme.Icon.unmute : Theme.Icon.mute)
        }
        // TodoItem actions only for an Agent on the selected hub: the todos and their links live there.
        if TodoStore.shared.isSupported, let current = now(),
            TodosLogic.isOnTodosHub(sessionGroup: current.key.group, hubGroup: model.hub?.group) {
            if let todo = current.todo {
                Button {
                    guard let s = now() else { return }
                    Task {
                        do {
                            try await TodoStore.shared.unlinkAgent(todoID: todo.id, machineID: s.machineID, sessionID: s.sessionID)
                        } catch {
                            AppModel.shared.show(error)
                        }
                    }
                } label: {
                    Label("解除关联", systemImage: Theme.Icon.link)
                }
            } else {
                Button {
                    if let s = now() { actions.linkTodo(s) }
                } label: {
                    Label("关联到待办…", systemImage: Theme.Icon.todo)
                }
            }
        }
        if looping {
            Button(role: .destructive) { model.stopLoop(key) } label: { Label("停止循环", systemImage: Theme.Icon.stopLoop) }
        }
        Button {
            guard let s = now() else { return }
            Task { await model.askJarvis(AgentHandoff.prompt(s, machine: model.machine(s.machineRef)?.name)) }
        } label: {
            Label("交给 Persona", systemImage: Theme.Icon.ai)
        }
        Button { UIPasteboard.general.string = now()?.cwd } label: { Label("拷贝目录", systemImage: Theme.Icon.folder) }
        if attachable {
            Button { Task { await model.attach(key) } } label: { Label("实时接入", systemImage: Theme.Icon.link).lineLimit(1) }
        }
        if terminal {
            Button {
                guard let s = now(), s.canTerminal else { return }
                UIPasteboard.general.string = "\(s.resumeCommand) \(s.sourceThreadID ?? s.sessionID)"
            } label: { Label("拷贝恢复命令", systemImage: Theme.Icon.terminal).lineLimit(1) }
        }
        if live && terminal {
            Button(role: .destructive) { if let s = now() { actions.close(s) } } label: { Label("关闭 Agent…", systemImage: Theme.Icon.end) }
        }
        if deletable {
            Button(role: .destructive) {
                if let s = now(), s.canDelete { actions.delete(s) }
            } label: { Label("删除对话…", systemImage: Theme.Icon.delete).lineLimit(1) }
        }
    }
}

/// The ⋯ on a space's header in Agents: `SpaceMenuItems`, equal while what it offers is (the space, its unread agents,
/// whether it can be renamed); the group's other fields change with every push.
struct SpaceHeaderMenu: View, Equatable {
    nonisolated let id: String
    nonisolated let unread: [SessionKey]
    var group: SpaceGroup
    var rename: () -> Void
    var open: () -> Void
    var linkTodo: (() -> Void)?

    init(group: SpaceGroup, rename: @escaping () -> Void, open: @escaping () -> Void, linkTodo: (() -> Void)? = nil) {
        id = group.id + "|" + group.spaceID + "|" + group.name
        unread = group.sessions.filter { $0.state == .unread }.map(\.key)
        self.group = group
        self.rename = rename
        self.open = open
        self.linkTodo = linkTodo
    }

    nonisolated static func == (a: Self, b: Self) -> Bool { a.id == b.id && a.unread == b.unread }

    var body: some View {
        SpaceMenuItems(group: group, rename: rename, open: open, linkTodo: linkTodo)
    }
}
