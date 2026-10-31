import SwiftUI

/// A new conversation in the shell (docs/ios.md「新外壳」), like ChatGPT's empty chat: nothing but the composer, and over
/// it what needs the user right now — agents waiting on them and the inbox's 需要你 — one quiet line each. Nothing to
/// do, nothing shown (the user's choice: no suggestions in their place). While the conversation spins up, the first
/// message and a typing line so the send feels instant.
struct ShellHome: View {
    @Environment(AppModel.self) private var model
    var pending: PendingStart?
    var error: String?
    @Binding var launcher: String?
    @Binding var mode: String
    @Binding var effort: String
    var openInbox: (InboxItem) -> Void
    var send: (String, [ImagePayload]) -> Void

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.xl) {
                if let pending {
                    HStack {
                        Spacer(minLength: Theme.Size.avatarXL)
                        VStack(alignment: .trailing, spacing: Theme.Space.sm) {
                            if pending.imageCount > 0 {
                                Label("\(pending.imageCount) 张图片", systemImage: Theme.Icon.photo).font(Theme.Font.label).foregroundStyle(.secondary)
                            }
                            Text(pending.text)
                                .font(Theme.Font.body)
                                .foregroundStyle(Color.primary)
                                .padding(.horizontal, Theme.Space.l)
                                .padding(.vertical, Theme.Space.m)
                                .background(Theme.Shell.userBubble, in: .rect(cornerRadius: Theme.Radius.bubble, style: .continuous))
                        }
                    }
                    .padding(.top, Theme.Space.m)
                    TypingIndicator(text: "\(model.persona.name) 正在准备…")
                }
            }
            .padding(.horizontal, Theme.Space.l)
            .frame(maxWidth: ChatView.readableWidth)
            .frame(maxWidth: .infinity)
        }
        .scrollDismissesKeyboard(.interactively)
        .contentMargins(.top, AssistantTopBar.height, for: .scrollContent)
        .background(Theme.pageBackground.ignoresSafeArea())
        .safeAreaInset(edge: .bottom, spacing: 0) {
            VStack(alignment: .leading, spacing: Theme.Space.m) {
                if pending == nil {
                    if let error {
                        Label(error, systemImage: Theme.Icon.error)
                            .font(Theme.Font.caption)
                            .foregroundStyle(Theme.danger)
                            .padding(.horizontal, Theme.Space.l)
                    }
                    ShellNeedsYou(openInbox: openInbox)
                }
                StarterComposer(disabled: pending != nil, shell: StarterComposer.ShellModes(launcher: $launcher, mode: $mode, effort: $effort),
                                send: send)
            }
            .padding(.horizontal, Theme.Space.m)
            .padding(.bottom, Theme.Space.s)
            .frame(maxWidth: ChatView.readableWidth)
            .frame(maxWidth: .infinity)
        }
        .navigationTitle(model.persona.name)
        .task(id: launcher) { if pending == nil { await model.warmAssistant(launcher: launcher) } }
    }
}

/// What needs the user, over the empty composer: the agents waiting on them (an answer, an approval, a plan), then the
/// inbox's 需要你, newest first — three at most, then where the rest are. A tap opens it where it lives.
struct ShellNeedsYou: View {
    @Environment(AppModel.self) private var model
    var openInbox: (InboxItem) -> Void

    private static let shown = 3

    private enum Line: Identifiable {
        case agent(Interaction)
        case inbox(InboxItem)

        var id: String {
            switch self {
            case .agent(let ix): "ix-" + ix.id
            case .inbox(let it): "in-" + it.id
            }
        }
    }

    private var lines: [Line] {
        let agents = model.pendingInteractions.filter { !model.isMuted($0.sessionKey) }.map(Line.agent)
        let inbox = InboxStore.shared.items
            .filter { !$0.isDone && $0.triage == .action }
            .sorted { ($0.updatedAt ?? .distantPast) > ($1.updatedAt ?? .distantPast) }
            .map(Line.inbox)
        return agents + inbox
    }

    var body: some View {
        let all = lines
        if !all.isEmpty {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(all.prefix(Self.shown)) { line in row(line) }
                if all.count > Self.shown {
                    Button { model.selectedTab = .mail } label: {
                        HStack(spacing: Theme.Space.m) {
                            Image(systemName: Theme.Icon.inbox)
                                .font(Theme.Font.heading)
                                .foregroundStyle(.secondary)
                                .scaledFrame(width: Theme.Size.icon)
                            Text("还有 \(all.count - Self.shown) 件在收件箱").font(Theme.Font.body).foregroundStyle(.secondary).lineLimit(1)
                            Spacer(minLength: 0)
                        }
                        .modifier(LineFrame())
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("shell-needs-more")
                }
            }
            .transition(.opacity)
            .accessibilityElement(children: .contain) // its own id; the rows keep theirs
            .accessibilityIdentifier("shell-needs-you")
        }
    }

    @ViewBuilder private func row(_ line: Line) -> some View {
        switch line {
        case .agent(let ix):
            Button { model.open(ix.sessionKey) } label: {
                HStack(spacing: Theme.Space.m) {
                    Image(systemName: ix.kind.symbol)
                        .font(Theme.Font.heading)
                        .foregroundStyle(Theme.needsYou)
                        .scaledFrame(width: Theme.Size.icon)
                    text(who: agentName(ix), what: ix.summary ?? ix.questions?.first?.question ?? ix.title)
                }
                .modifier(LineFrame())
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("shell-needs-agent")
        case .inbox(let it):
            Button { openInbox(it) } label: {
                HStack(spacing: Theme.Space.m) {
                    mark(it)
                    text(who: it.title, what: it.summary ?? it.preview)
                }
                .modifier(LineFrame())
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("shell-needs-inbox")
        }
    }

    /// ChatGPT's rows: one line of large gray text.
    private func text(who: String, what: String) -> some View {
        // All in the secondary gray like ChatGPT's rows; the name a step darker to find it at a glance.
        (Text(who).foregroundStyle(Color.primary.opacity(0.75)) + Text("  " + what.replacingOccurrences(of: "\n", with: " ")).foregroundStyle(.secondary))
            .font(Theme.Font.body)
            .lineLimit(1)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder private func mark(_ it: InboxItem) -> some View {
        if it.source == "lark" && it.kind != "doc" && it.kind != "moment" {
            Image(Theme.Brand.lark).resizable().scaledToFit()
                .scaledFrame(width: Theme.Size.iconS, height: Theme.Size.iconS)
                .scaledFrame(width: Theme.Size.icon)
        } else {
            Image(systemName: Self.symbol(it))
                .font(Theme.Font.heading)
                .foregroundStyle(.secondary)
                .scaledFrame(width: Theme.Size.icon)
        }
    }

    private static func symbol(_ it: InboxItem) -> String {
        switch it.kind {
        case "doc": Theme.Icon.document
        case "moment": Theme.Icon.moments
        default: Theme.Icon.mail
        }
    }

    private func agentName(_ ix: Interaction) -> String {
        if let t = ix.sessionTitle, !t.isEmpty { return t }
        return ix.cwd.map { URL(fileURLWithPath: $0).lastPathComponent } ?? "Agent"
    }

    private struct LineFrame: ViewModifier {
        func body(content: Content) -> some View {
            content
                .padding(.horizontal, Theme.Space.l)
                .scaledFrame(minHeight: Theme.Size.box56)
                .contentShape(.rect)
        }
    }
}

/// The pages the shell pushes onto the 助手 page by value (docs/ios.md「新外壳」), and what those push in turn: the
/// inbox (its items and Feishu pages), the machines (a machine, its agents). One stack, values only: a value pushed on
/// top of a view-based push lands under it (docs/lessons/ios.md).
struct ShellDestinations: ViewModifier {
    @Binding var path: NavigationPath
    var enabled: Bool
    @StateObject private var accounts = InboxAccountState()

    func body(content: Content) -> some View {
        if enabled {
            content
                .navigationDestination(for: ShellRoute.self) { route in
                    switch route {
                    case .inbox: InboxPage(path: $path, accountState: accounts, embedded: true)
                    case .machines: MachinesView()
                    }
                }
                .modifier(InboxDestinations(accounts: accounts.accounts))
                .navigationDestination(for: MachineDetailRoute.self) { r in MachineDetailView(machineID: r.id) }
                .navigationDestination(for: SessionKey.self) { key in ChatView(key: key).id(key) }
                .environment(\.feishuOpen) { path.append($0) }
        } else {
            content
        }
    }
}

extension EnvironmentValues {
    /// The Jarvis conversation is in the shell (docs/ios.md「新外壳」): neutral bubbles, the shell's composer, modes in it.
    @Entry var shellChat = false
}
