import AppKit
import SwiftUI

/// The menu bar item: the Mac's stand-in for the Live Activity / Dynamic Island (docs/mac.md「和 iOS 的对应」).
/// The icon carries how many agents need you; the menu lists them (answer, allow or approve right there), what is
/// working, and what finished and is still unread. It stays when the main window is closed.
enum MacMenuBar {
    static let unreadLimit = 5

    struct Sections {
        var needsYou: [Session] = []
        var working: [Session] = []
        var unread: [Session] = []
        var isEmpty: Bool { needsYou.isEmpty && working.isEmpty && unread.isEmpty }
    }

    /// Waiting the longest first; finished ones newest first and only the last few. Muted agents are left out of
    /// what needs you (as the badges do) but still show while they work.
    static func sections(_ sessions: [Session], muted: (Session) -> Bool) -> Sections {
        let indexed = Array(sessions.enumerated())
        func pick(_ state: AgentState, newestFirst: Bool) -> [Session] {
            indexed.filter { $0.element.state == state }
                .sorted { a, b in
                    let x = a.element.stateSince ?? .distantPast, y = b.element.stateSince ?? .distantPast
                    if x != y { return newestFirst ? x > y : x < y }
                    return a.offset < b.offset
                }
                .map(\.element)
        }
        return Sections(needsYou: pick(.needsYou, newestFirst: false).filter { !muted($0) },
                        working: pick(.working, newestFirst: false),
                        unread: Array(pick(.unread, newestFirst: true).prefix(unreadLimit)))
    }

    /// The number beside the icon.
    static func count(needsYou: Int) -> String? { needsYou > 0 ? "\(needsYou)" : nil }
}

/// The icon in the menu bar: the Jarvis mark and how many need you.
struct MacMenuBarLabel: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("MacMenuBarLabel")
        #endif
        let n = MacMenuBar.sections(model.allSessions, muted: { model.isMuted($0) }).needsYou.count
        HStack(spacing: Theme.Space.xxs) {
            Image(systemName: Theme.Icon.ai)
            if let c = MacMenuBar.count(needsYou: n) { Text(c).monospacedDigit() }
        }
        .accessibilityLabel(n > 0 ? "Jarvis，\(n) 个需要你" : "Jarvis")
        // Always there (unlike the main window): what reopens the main window for the Dock menu and notifications.
        .onAppear { MacShellState.shared.openMain = { openWindow(id: "main") } }
    }
}

/// The menu itself (`MenuBarExtra`, menu style): native menu items, submenus for the quick answers.
struct MacMenuBarMenu: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        let s = MacMenuBar.sections(model.allSessions, muted: { model.isMuted($0) })
        if !model.isPaired {
            Text("还没有连接 hub")
        } else if s.isEmpty {
            Text("没有需要你处理的")
        }
        if !s.needsYou.isEmpty {
            Section("需要你") {
                ForEach(s.needsYou) { session in needsYouItem(session) }
            }
        }
        if !s.working.isEmpty {
            Section("正在工作") {
                ForEach(s.working) { session in Button(label(session)) { open(session.key) } }
            }
        }
        if !s.unread.isEmpty {
            Section("做完了，还没看") {
                ForEach(s.unread) { session in Button(label(session)) { open(session.key) } }
            }
        }
        Divider()
        Button("打开 Jarvis") { show() }
        Button("新建 Agent 对话…") {
            show()
            MacShellState.shared.newAgent = true
        }
        .disabled(!model.isPaired)
        Divider()
        Button("退出 Jarvis") { NSApp.terminate(nil) }
            .keyboardShortcut("q")
    }

    private func label(_ s: Session) -> String {
        "\(s.displayTitle) · \(model.machines.first { $0.id == s.machineID }?.name ?? s.machineID)"
    }

    /// An agent that needs you: a submenu with the answers the phone's cards offer in one tap, and 打开对话.
    @ViewBuilder private func needsYouItem(_ s: Session) -> some View {
        if let ix = model.pendingInteractions.first(where: { $0.sessionKey == s.key }), let quick = MacMenuBar.quickAnswers(ix), !quick.isEmpty {
            Menu(label(s)) {
                Text(MacMenuBar.summary(ix))
                ForEach(Array(quick.enumerated()), id: \.offset) { _, q in
                    Button(q.title) { Task { await model.resolve(ix, q.resolution) } }
                }
                Divider()
                Button("打开对话") { open(s.key) }
            }
        } else {
            Button(label(s)) { open(s.key) }
        }
    }

    private func open(_ key: SessionKey) {
        show()
        MacShellState.shared.open(key, model)
    }

    private func show() {
        MacShellState.shared.openMain = { openWindow(id: "main") }
        MacShellState.shared.showMain()
    }
}

extension MacMenuBar {
    struct QuickAnswer {
        var title: String
        var resolution: Resolution
    }

    /// The same one-tap answers as the list's cards (InteractionPreviewCard.quickActions): a single short question's
    /// options, 允许 / 拒绝, a plan's 批准 (edits go through, commands still ask: you haven't read it in full here), and a
    /// terminal dialog's short options when the hub read it. Anything else needs the chat.
    static func quickAnswers(_ ix: Interaction) -> [QuickAnswer]? {
        switch ix.kind {
        case .question:
            guard let qs = ix.questions, qs.count == 1, let q = qs.first, !q.multiSelect, q.options.count <= 4 else { return nil }
            return q.options.map { QuickAnswer(title: $0.label, resolution: Resolution(answers: [q.question: $0.label])) }
        case .permission:
            return [QuickAnswer(title: "允许", resolution: Resolution(allow: true)),
                    QuickAnswer(title: "拒绝", resolution: Resolution(allow: false))]
        case .plan:
            return [QuickAnswer(title: "批准", resolution: Resolution(approve: true, mode: "acceptEdits"))]
        case .screen:
            guard ix.dialog != nil, let d = ix.screenDialog else { return nil }
            let plain = d.options.filter { $0.kind == .plain && $0.label.count <= 24 }
            guard (1...4).contains(plain.count) else { return nil }
            return plain.map { QuickAnswer(title: $0.label, resolution: Resolution(option: $0.key)) }
        }
    }

    /// One line on what it asks.
    static func summary(_ ix: Interaction) -> String {
        let s: String = switch ix.kind {
        case .question: ix.questions?.first?.question ?? "Claude 在问你"
        case .plan: PlanText.title(ix.plan ?? "")
        case .permission: [ix.toolName, ix.summary].compactMap { $0 }.joined(separator: "：")
        case .screen: ix.screenDialog?.question ?? "终端在问你"
        }
        return String(s.prefix(80))
    }
}
