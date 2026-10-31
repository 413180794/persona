import AppKit
import SwiftUI

/// What the window's commands open over the page (⌘K, ⌘N): set from the menu, the menu bar or ⌘K itself.
@Observable final class MacShellState {
    static let shared = MacShellState()
    var quickOpen = false
    var newAgent = false
    /// 知识库 or 机器 while picked (they are not iPhone tabs); nil shows the tab's section.
    var section: MacSection? = MacSection.remembered() {
        didSet { MacSection.remember(section) }
    }
    /// A conversation the menus asked to open in its own window (MacWindowOpener does it).
    var chatWindow: SessionKey?
    /// What ⌘K opens with typed in (DEBUG `-openQuickOpen <query>`).
    var seed = ""
    /// What the Agent menu asks of the chat showing `key` (a new value each time): toggle the inspector
    /// (显示会话信息 ⌥⌘I), or bring up its own 重命名 / 清空上下文 / 关闭 Agent dialogs.
    var chatRequest: ChatRequest?
    /// The Agents list's 全部折叠 / 全部展开 (its own ⋯ menu's), for 显示 › 全部折叠 / 全部展开.
    var agentsList: AgentsListMenu.Actions?
    /// The key window's number, and the chat each window shows (ChatView registers itself): what the Agent menu acts on.
    var keyWindow: Int?
    var chatsByWindow: [Int: SessionKey] = [:]
    /// `openWindow(id: "main")`, from a view that is always there (the menu bar item's label).
    @ObservationIgnored var openMain: (() -> Void)?
    @ObservationIgnored private var keyObservers: [Any] = []

    struct ChatRequest: Equatable {
        enum Kind { case inspector, rename, clear, close }
        let key: SessionKey
        let kind: Kind
        /// The window it is for: only the chat in that window answers (the same chat can be open in two).
        var window: Int?
        let id = UUID()
    }
}

/// ⌘K: a floating panel near the window's top, like Xcode's Open Quickly / Spotlight. The field keeps the focus;
/// ↑↓ move the selection, ⏎ opens it, Esc or a click outside closes.
struct MacQuickOpenPanel: View {
    @Environment(AppModel.self) private var model
    @State private var query = ""
    @State private var selected = 0
    @FocusState private var focused: Bool

    var body: some View {
        let groups = MacQuickOpen.sections(query, sessions: model.allSessions, machines: model.machines, inbox: InboxStore.shared.items)
        let hits = groups.flatMap(\.hits)
        VStack(spacing: 0) {
            HStack(spacing: Theme.Space.s) {
                Image(systemName: Theme.Icon.search)
                    .foregroundStyle(.secondary)
                    .accessibilityHidden(true)
                TextField("快速打开：Agent、Jarvis 对话、收件箱…", text: $query)
                    .textFieldStyle(.plain)
                    .font(Theme.Font.title)
                    .focused($focused)
                    .onSubmit { if hits.indices.contains(selected) { open(hits[selected]) } }
                    .onKeyPress(.downArrow) { selected = MacQuickOpen.move(selected, by: 1, count: hits.count); return .handled }
                    .onKeyPress(.upArrow) { selected = MacQuickOpen.move(selected, by: -1, count: hits.count); return .handled }
                    .onKeyPress(.escape) { close(); return .handled }
                    .accessibilityLabel("快速打开")
            }
            .padding(Theme.Space.m)
            if !hits.isEmpty {
                Divider()
                ScrollViewReader { proxy in
                    ScrollView {
                        LazyVStack(alignment: .leading, spacing: 0) {
                            ForEach(groups) { g in
                                Text(g.title)
                                    .font(Theme.Font.caption.weight(.semibold))
                                    .foregroundStyle(.secondary)
                                    .padding(.horizontal, Theme.Space.m)
                                    .padding(.top, Theme.Space.s)
                                    .padding(.bottom, Theme.Space.xs)
                                ForEach(g.hits) { hit in
                                    let i = hits.firstIndex(of: hit) ?? 0
                                    QuickOpenRow(hit: hit, selected: i == selected)
                                        .id(hit.id)
                                        .contentShape(Rectangle())
                                        .onTapGesture { open(hit) }
                                        .onHover { if $0 { selected = i } }
                                }
                            }
                        }
                        .padding(.bottom, Theme.Space.s)
                    }
                    .frame(maxHeight: Theme.Size.macQuickOpenListMax)
                    .fixedSize(horizontal: false, vertical: true)
                    .onChange(of: selected) { _, i in
                        if hits.indices.contains(i) { proxy.scrollTo(hits[i].id) }
                    }
                }
            } else if !query.isEmpty {
                Divider()
                Text("没有找到「\(query)」")
                    .foregroundStyle(.secondary)
                    .padding(Theme.Space.l)
            }
        }
        .frame(width: Theme.Size.macQuickOpenWidth)
        .background(Theme.Fill.macPanel, in: RoundedRectangle(cornerRadius: Theme.Radius.panel))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.panel).strokeBorder(.separator))
        .shadow(radius: Theme.Space.xl, y: Theme.Space.s)
        .onAppear {
            query = MacShellState.shared.seed
            MacShellState.shared.seed = ""
            focused = true
        }
        .onChange(of: query) { selected = 0 }
    }

    private func open(_ hit: MacQuickOpen.Hit) {
        close()
        switch hit.target {
        case .agent(let k), .conversation(let k): MacShellState.shared.open(k, model)
        case .inbox(let id):
            MacShellState.shared.willOpen()
            model.selectedTab = .mail
            MacInboxState.shared.selection = [.item(id)]
        case .section(let s): MacShellState.shared.go(s, model)
        case .newAgent: MacShellState.shared.newAgent = true
        case .newConversation:
            MacShellState.shared.willOpen()
            model.selectedTab = .assistant
            model.assistantCurrent = nil
        }
    }

    private func close() { MacShellState.shared.quickOpen = false }
}

private struct QuickOpenRow: View {
    var hit: MacQuickOpen.Hit
    var selected: Bool

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            Group {
                if let state = hit.state, state != .idle {
                    StatusDot(state: state)
                } else {
                    Image(systemName: hit.icon).foregroundStyle(selected ? .white : .secondary)
                }
            }
            .frame(width: Theme.Size.iconXS)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(hit.title).lineLimit(1)
                if !hit.subtitle.isEmpty {
                    Text(hit.subtitle)
                        .font(Theme.Font.caption)
                        .foregroundStyle(selected ? .white : .secondary)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 0)
        }
        .foregroundStyle(selected ? .white : .primary)
        .padding(.horizontal, Theme.Space.s)
        .padding(.vertical, Theme.Space.xs)
        .background(selected ? Color.accentColor : .clear, in: RoundedRectangle(cornerRadius: Theme.Radius.badge))
        .padding(.horizontal, Theme.Space.xs)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
    }
}

/// ⌘K over the whole window: a faint dim that closes it on a click, the panel near the top.
struct MacQuickOpenLayer: View {
    var body: some View {
        if MacShellState.shared.quickOpen {
            ZStack(alignment: .top) {
                Theme.Fill.macScrim
                    .ignoresSafeArea()
                    .onTapGesture { MacShellState.shared.quickOpen = false }
                    .accessibilityHidden(true)
                MacQuickOpenPanel()
                    .padding(.top, Theme.Size.macQuickOpenTop)
            }
        }
    }
}

extension MacShellState {
    /// Shows a section: an iPhone tab moves the tab, 知识库 / 机器 are picked on the Mac side.
    func go(_ s: MacSection, _ model: AppModel) {
        let p = MacSection.pick(s)
        section = p.override
        if let tab = p.tab { model.selectedTab = tab }
    }
}

extension MacShellState {
    /// The chat the menus act on: the key window's; with no key window (the app in the background, the menu bar's
    /// menu open), the last one shown. A key window without a chat (知识库, a sheet) has none.
    nonisolated static func focusedChat(keyWindow: Int?, chats: [Int: SessionKey], fallback: SessionKey?) -> SessionKey? {
        guard let keyWindow else { return fallback }
        return chats[keyWindow]
    }

    func focusedChat(_ model: AppModel) -> SessionKey? {
        Self.focusedChat(keyWindow: keyWindow, chats: chatsByWindow, fallback: model.visibleSession)
    }

    /// Follows the key window (the menus are rebuilt when it changes). Started once at launch.
    func watchKeyWindow() {
        guard keyObservers.isEmpty else { return }
        let update: (Notification) -> Void = { _ in
            MainActor.assumeIsolated { MacShellState.shared.keyWindow = NSApp.keyWindow?.windowNumber }
        }
        for name in [NSWindow.didBecomeKeyNotification, NSWindow.didResignKeyNotification] {
            keyObservers.append(NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main, using: update))
        }
    }

    /// The main window to the front, opened again if it was closed (the Dock menu, the menu bar list).
    func showMain() {
        NSApp.activate()
        if let w = NSApp.windows.first(where: { $0.identifier?.rawValue.hasPrefix("main") == true }) {
            w.makeKeyAndOrderFront(nil)
        } else {
            openMain?()
        }
    }
}

extension MacShellState {
    /// Something is about to open a chat or a tab's page: 知识库 / 机器 make way. Called directly because when the tab
    /// and the open chat are already the ones asked for, nothing changes for MacShell's onChange to see.
    func willOpen() { section = nil }

    /// `AppModel.open` for the Mac: the section comes to the front too.
    func open(_ key: SessionKey, _ model: AppModel, latestAnswer: Bool = false) {
        willOpen()
        model.open(key, latestAnswer: latestAnswer)
    }
}
