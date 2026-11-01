import SwiftUI

// The Agents list's menus depend on nothing that changes with each push from the hub: the list is drawn again on
// every one (sessions, machines), and a menu drawn again with it is handed to UIKit again — the open ⋯ menu jumped back
// to its top under the finger choosing a machine (2026-10-31), a row's long-press menu was rebuilt for every row on
// every push. The ⋯ menu sits in the toolbar of `ChatsView`, which does not follow the list; a row's is equal while
// what it offers is. What the actions need at tap time (the sections listed now, the session as it is now) comes from
// an object the views do not observe. `LiveUpdateTests` covers it.

/// A machine as the ⋯ menu shows it: not its live state (last seen, online), which changes with every push.
nonisolated struct MachineChoice: Equatable, Identifiable {
    var id: String
    var name: String
    var symbol: String

    @MainActor init(_ m: Machine) {
        id = m.id
        name = m.name
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

    @AppStorage("dev.machineFilter") private var machineFilter = ""
    @AppStorage("dev.grouping") private var grouping: DevGrouping = .space
    @AppStorage("dev.compact") private var compact = false

    /// The shell (docs/ios.md「新外壳」) shows these inside its 工作 switch's menu: the items, without a button of their own.
    var inline = false

    var body: some View {
        // Only the machines' names and icons: not their live state, which changes with every push.
        let machines = model.machineChoices
        let chosen = machines.first { $0.id == machineFilter }
        if inline {
            items(machines)
        } else {
            Menu { items(machines) } label: { label(chosen) }
                .accessibilityLabel((chosen.map { "当前机器：\($0.name)，" } ?? "") + "分组：" + grouping.title)
        }
    }

    @ViewBuilder private func items(_ machines: [MachineChoice]) -> some View {
            #if DEBUG || PERF_SIGNPOST
            let _ = PerfTrace.body("ListMenu.content")
            #endif
            Picker("分组", selection: $grouping) {
                ForEach(DevGrouping.allCases) { g in Label(g.title, systemImage: g.symbol).tag(g) }
            }
            Picker("显示", selection: $compact) {
                Label("卡片", systemImage: "rectangle.grid.1x2").tag(false)
                Label("细列表", systemImage: "list.bullet").tag(true)
            }
            Section {
                Button {
                    withAnimation(Theme.Motion.standard) { actions.collapse() }
                } label: { Label("全部折叠", systemImage: Theme.Icon.collapseAll) }
                Button {
                    withAnimation(Theme.Motion.standard) { actions.expand() }
                } label: { Label("全部展开", systemImage: "rectangle.expand.vertical") }
            }
            Picker("机器", selection: $machineFilter) {
                Label("全部机器", systemImage: "square.grid.2x2").tag("")
                ForEach(machines) { m in
                    Label(m.name, systemImage: m.symbol).tag(m.id)
                }
            }
            Section {
                Button { actions.machinesPage() } label: {
                    Label("机器", systemImage: Theme.Icon.machines)
                }
            }
    }

    @ViewBuilder private func label(_ chosen: MachineChoice?) -> some View {
            if let m = chosen {
                HStack(spacing: Theme.Space.sm) {
                    Image(systemName: m.symbol)
                    Text(m.name).lineLimit(1)
                    Image(systemName: Theme.Icon.expand).font(Theme.Font.labelHeavy)
                }
                .font(Theme.Font.secondaryStrong)
                .fixedSize()
            } else {
                Image(systemName: grouping == .space ? "line.3.horizontal.decrease" : grouping.symbol)
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
    nonisolated let actions: Actions

    @MainActor final class Actions {
        private var renameHandler: (Session) -> Void = { _ in }
        private var closeHandler: (Session) -> Void = { _ in }

        func update(rename: @escaping (Session) -> Void, close: @escaping (Session) -> Void) {
            renameHandler = rename
            closeHandler = close
        }

        func rename(_ s: Session) { renameHandler(s) }
        func close(_ s: Session) { closeHandler(s) }
    }

    nonisolated static func == (a: Self, b: Self) -> Bool {
        a.key == b.key && a.pinned == b.pinned && a.muted == b.muted && a.looping == b.looping && a.live == b.live
            && a.terminal == b.terminal && a.attachable == b.attachable && a.actions === b.actions
    }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("RowMenu.content")
        #endif
        // The session as it is when an item is tapped.
        let now = { model.session(key) }
        #if os(macOS)
        MacOpenInWindowButton(key: key)
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
        if looping {
            Button(role: .destructive) { model.stopLoop(key) } label: { Label("停止循环", systemImage: Theme.Icon.stopLoop) }
        }
        Button {
            guard let s = now() else { return }
            Task { await model.askJarvis(AgentHandoff.prompt(s, machine: model.machine(s.machineID)?.name)) }
        } label: {
            Label("交给 Jarvis", systemImage: Theme.Icon.ai)
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

    init(group: SpaceGroup, rename: @escaping () -> Void, open: @escaping () -> Void) {
        id = group.id + "|" + group.spaceID
        unread = group.sessions.filter { $0.state == .unread }.map(\.key)
        self.group = group
        self.rename = rename
        self.open = open
    }

    nonisolated static func == (a: Self, b: Self) -> Bool { a.id == b.id && a.unread == b.unread }

    var body: some View {
        SpaceMenuItems(group: group, rename: rename, open: open)
    }
}
