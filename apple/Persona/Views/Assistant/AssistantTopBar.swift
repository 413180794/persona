import SwiftUI

/// The Persona tab's top bar (docs/ios.md「Persona 顶栏和侧边栏」), floating over the conversation with a fog under it
/// instead of a solid bar: 历史对话 (the sidebar) on the left, Persona's face and name in the middle, 新对话 on the right.
/// The face opens a menu: 知识库, 详细信息 (the profile), the copy items (the working directory, a block of facts to
/// paste to another agent, the session id), the launcher, the session info card, 中断. While the hub is out of reach
/// the name pill shows the spinner and 等待网络 / 连接中… / 更新中….
struct AssistantTopBar: View {
    @Environment(AppModel.self) private var model
    /// In the shell (docs/ios.md「新外壳」) the middle is the 助手 | Agents switch; its 助手 opens this bar's menu.
    @Environment(\.shell) private var shell
    /// The conversation on screen; nil on the new-conversation page.
    var key: SessionKey?
    /// The sidebar is a column beside the conversation (a wide display): the button shows or hides it.
    var sidebarIsColumn = false
    var openSidebar: () -> Void
    var newConversation: () -> Void
    var openKnowledge: () -> Void
    var openProfile: () -> Void
    @State private var showInfo = false
    @State private var showLink = false
    @State private var launchers: [Launcher] = []
    /// A copy's confirmation, in the name pill for a moment (a toast would sit right on the face).
    @State private var hint: String?

    /// The bar's height below the status bar: what the conversation leaves free at its top so nothing hides under it.
    static let height: CGFloat = 70
    /// How far below the bar the fog fades out.
    static let fade: CGFloat = 30

    private var session: Session? { key.flatMap { model.session($0) } }
    private var machineID: String { model.assistantMachineID(key) }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("AssistantTopBar")
        #endif
        bar
            // 会话信息 is a sheet, like the Agents chat's (SessionInfoSheet).
            .sheet(isPresented: Binding(get: { showInfo && key != nil }, set: { showInfo = $0 })) {
                if let key {
                    #if os(macOS)
                    // A window sheet: SessionInfoSheet has no stack or 完成 of its own on the Mac (it is the chat's inspector there).
                    NavigationStack {
                        SessionInfoSheet(key: key, session: session)
                            .navigationTitle("会话信息")
                            .toolbar {
                                ToolbarItem(placement: .confirmationAction) { Button("完成") { showInfo = false }.lineLimit(1) }
                            }
                    }
                    .macSheet()
                    #else
                    SessionInfoSheet(key: key, session: session)
                        .presentationDetents([.medium, .large])
                        .presentationDragIndicator(.visible)
                    #endif
                }
            }
            .onChange(of: key) { showInfo = false }
            #if DEBUG
            .task { // `-showInfo YES`: 会话信息 open, for screenshots (the Agents chat reads the same flag)
                if UserDefaults.standard.bool(forKey: "showInfo") { try? await Task.sleep(for: .seconds(2)); showInfo = true }
            }
            #endif
        .task(id: model.hubRef(machineID)) { launchers = ((try? await model.launchers(model.hubRef(machineID))) ?? []).filter(\.runsAssistant) }
    }

    @ViewBuilder private var bar: some View {
        if shell != nil { shellBar } else { classicBar }
    }

    /// ChatGPT's top bar: the persona's face (the drawer; a mark while the inbox has something for the user), the
    /// switch, 新对话.
    private var shellBar: some View {
        @Bindable var model = model
        let link = model.connection.shown
        return HStack(alignment: .top) {
            ShellPersonaButton(id: "assistant-sidebar-button", action: openSidebar)
            Spacer(minLength: Theme.Space.s)
            ShellSwitch(tab: $model.selectedTab) { menuItems(link) }
            Spacer(minLength: Theme.Space.s)
            ShellCircleButton(symbol: Theme.Icon.compose, label: "新对话", id: "assistant-new", action: newConversation)
                .disabled(key == nil)
                .opacity(key == nil ? 0.4 : 1)
        }
        .shellTopBarScaled()
        .padding(.horizontal, Theme.Space.l)
        .padding(.top, Theme.Space.xxs)
        .frame(height: Self.height, alignment: .top)
        .background(alignment: .top) { BarFog(solid: Self.height - 6, fade: Self.fade) }
        .connectionDetails($showLink)
        .modifier(ShellBarAtRest()) // ShellMovingBar stands in while the strip slides
    }

    private var classicBar: some View {
        HStack(alignment: .top) {
            GlassEffectContainer {
                circle(sidebarIsColumn ? "sidebar.left" : "line.3.horizontal", label: "历史对话", id: "assistant-sidebar-button") {
                    Haptics.tap()
                    openSidebar()
                }
            }
            Spacer(minLength: 8)
            face
            Spacer(minLength: 8)
            GlassEffectContainer {
                circle("square.and.pencil", label: "新对话", id: "assistant-new") {
                    Haptics.tap()
                    newConversation()
                }
                .disabled(key == nil)
            }
        }
        .padding(.horizontal, Theme.Space.l)
        .padding(.top, Theme.Space.xxs)
        .frame(height: Self.height, alignment: .top)
        .background(alignment: .top) {
            // Solid down to the bar's bottom edge, fading out below it (fading under the pill let the messages show
            // through the name).
            BarFog(solid: Self.height - 6, fade: Self.fade)
        }
    }

    private func circle(_ symbol: String, label: String, id: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(Theme.Font.heading)
                .dynamicTypeSize(...DynamicTypeSize.xxLarge)
                .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .surface(interactive: true, in: .circle)
        .accessibilityLabel(label)
        .accessibilityIdentifier(id)
    }

    /// The face over the name pill; both are the menu's button. A copy's confirmation covers the pill for a moment
    /// (beside the menu, not on it: anything attached to a Menu becomes part of its button and its label).
    private var face: some View {
        ZStack(alignment: .bottom) {
            menu
            if let hint {
                HStack(spacing: Theme.Space.xs) {
                    Image(systemName: Theme.Icon.successFilled).foregroundStyle(Theme.positive)
                    Text(hint).lineLimit(1)
                }
                .font(Theme.Font.labelStrong)
                .padding(.horizontal, Theme.Space.m)
                .scaledFrame(height: Theme.Size.icon)
                .fixedSize()
                .background(.regularMaterial, in: .capsule)
                .overlay(Capsule().strokeBorder(Color.primary.opacity(0.08), lineWidth: 0.5))
                .allowsHitTesting(false)
                .transition(.opacity)
            }
        }
        .animation(Theme.Motion.standard, value: hint)
        .task(id: hint) { await clearHint() }
    }

    /// A multi-line closure in the modifier chain slows the release build's type-check (docs/lessons/ios.md): a method instead.
    private func clearHint() async {
        guard hint != nil else { return }
        try? await Task.sleep(for: .seconds(3))
        hint = nil
    }

    private var menu: some View {
        let link = model.connection.shown
        return Menu { menuItems(link) } label: {
            VStack(spacing: -7) {
                TopBarDot(key: key, machineID: machineID)
                    .shadow(color: Theme.persona.opacity(0.3), radius: 8, y: 3)
                ZStack {
                    if link != .online {
                        ConnectionLabel(phase: link, compact: true).transition(.opacity)
                    } else {
                        Text(name).font(Theme.Font.labelStrong).lineLimit(1).transition(.opacity)
                    }
                }
                .padding(.horizontal, Theme.Space.m)
                .scaledFrame(height: Theme.Size.icon)
                .scaledFrame(maxWidth: Theme.Size.box200)
                .fixedSize() // as wide as the name, at most 200
                // Not the glass `surface`: inside a Menu's label the glass spreads over the whole label (face included).
                .background(.regularMaterial, in: .capsule)
                .overlay(Capsule().strokeBorder(Color.primary.opacity(0.08), lineWidth: 0.5))
                .animation(Theme.Motion.standard, value: link)
                .opacity(hint == nil ? 1 : 0)
            }
            .contentShape(.rect)
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .accessibilityLabel(link != .online ? "\(name)，\(link.title)" : name)
        .accessibilityHint("知识库、详细信息、拷贝目录和对话信息")
        .accessibilityIdentifier("jarvis-avatar")
        .connectionDetails($showLink)
    }

    /// The pill's name: the persona's (Persona's) on the main conversation and the new-conversation page, a side
    /// conversation's title.
    private var name: String {
        let persona = model.persona.name
        guard let key, key != model.assistantMain else { return persona }
        let t = session?.title ?? ""
        return t.isEmpty || t == "Claude Code" ? persona : t
    }

    /// The conversation's launcher (the next message uses it) or, on the new-conversation page, the default one.
    private var launcher: String {
        // The main conversation follows 设置 › 助手 (the hub sends it there), side ones keep their own.
        if let key, key == model.assistantMain, let l = model.assistantLaunchers[key] ?? model.assistantConfig?.launcher { return l }
        if let key { return model.assistantLaunchers[key] ?? session?.launcher ?? model.assistantConfig?.launcher ?? "claude" }
        return model.assistantConfig?.launcher ?? "claude"
    }

    private var cwd: String {
        if let c = session?.cwd, !c.isEmpty { return c }
        if let c = model.assistantConfig?.cwd, !c.isEmpty { return c }
        return "~/.persona/assistant"
    }

    @ViewBuilder private func menuItems(_ link: ConnectionStatus.Phase) -> some View {
        if model.isMultiHub, let current = model.hub?.group {
            // Several groups of hubs: each has its own Persona (docs/multi-hub.md「hub 级页面」); pick whose.
            Section("hub") {
                Picker("hub", selection: Binding(get: { current }, set: { model.selectHub($0) })) {
                    ForEach(model.groupIDs, id: \.self) { g in
                        Label(model.groupName(g), systemImage: Theme.Icon.hubGroup).tag(g)
                    }
                }
            }
        }
        Section(menuTitle) {
            if link != .online {
                Button { showLink = true } label: { Label("连接详情", systemImage: Theme.Icon.offline) }
            }
            Button(action: openKnowledge) { Label("知识库", systemImage: Theme.Icon.knowledge) }
            Button(action: openProfile) { Label("详细信息", systemImage: Theme.Icon.person) }
            if let main = model.assistantMain, key != main {
                Button {
                    withAnimation(Theme.Motion.standard) { model.assistantCurrent = main }
                    Haptics.tap()
                } label: { Label("回到主对话", systemImage: "house") }
            }
        }
        Section {
            Button { copy(cwd, "已拷贝工作目录") } label: { Label("拷贝目录", systemImage: Theme.Icon.folder) }
            Button { copy(infoText, "已拷贝对话信息") } label: { Label("拷贝信息", systemImage: Theme.Icon.copy) }
            if let key {
                Button { copy(key.sessionID, "已拷贝会话 ID") } label: { Label("拷贝会话 ID", systemImage: Theme.Icon.id) }
            }
        }
        if let key {
            Section {
                Menu {
                    Section("下一条消息起使用") {
                        ForEach(launchers) { l in
                            Button {
                                model.assistantLaunchers[key] = l.name
                                Haptics.tap()
                                model.toast = Toast("已切换到 \(l.name)，下一条消息生效", style: .success)
                            } label: {
                                let title = [l.name, Fmt.model(l.model)].compactMap { $0 }.joined(separator: " · ")
                                if l.name == launcher { Label(title, systemImage: Theme.Icon.done) } else { Text(title) }
                            }
                        }
                    }
                } label: { Label("启动器：\(launcher)", systemImage: Theme.Icon.model) }
                Button { showInfo = true } label: {
                    Label("会话信息", systemImage: Theme.Icon.info)
                }
                if session?.status.isActive == true {
                    Button { Task { await model.interrupt(key) } } label: { Label("中断", systemImage: Theme.Icon.interrupt) }
                }
            }
        }
    }

    /// The menu's heading: which conversation, and what it runs on.
    private var menuTitle: String {
        let title = baseMenuTitle
        guard model.isMultiHub, let g = model.hub?.group else { return title }
        return title + " · " + model.groupName(g)
    }

    private var baseMenuTitle: String {
        guard let key else { return "新对话 · \(launcher)" }

        let title = name == model.persona.name && key == model.assistantMain ? "主对话" : name
        return [title, launcher, Fmt.model(session?.model)].compactMap { $0 }.joined(separator: " · ")
    }

    private var infoText: String {
        AssistantCopyText.info(title: session.map { $0.title.isEmpty || $0.title == "Claude Code" ? nil : $0.title } ?? nil,
                               isMain: key != nil && key == model.assistantMain, sessionID: key?.sessionID,
                               machine: model.machine(model.hubRef(machineID))?.name, machineID: machineID, cwd: cwd,
                               launcher: launcher, model: Fmt.model(session?.model))
    }

    private func copy(_ text: String, _ done: String) {
        UIPasteboard.general.string = text
        Haptics.success()
        hint = done
        AccessibilityNotification.Announcement(done).post()
    }
}

/// What 拷贝信息 puts on the clipboard: enough for another agent (or the user at a terminal) to find this conversation.
nonisolated enum AssistantCopyText {
    static func info(title: String?, isMain: Bool, sessionID: String?, machine: String?, machineID: String, cwd: String,
                     launcher: String, model: String?) -> String {
        var lines: [String] = []
        let name = [isMain ? "主对话" : nil, title.map { "「\($0)」" }].compactMap { $0 }.joined(separator: " ")
        lines.append(sessionID == nil ? "Persona 助手（hub 上的 Claude）" : "Persona 助手的对话" + (name.isEmpty ? "" : " " + name))
        if let sessionID { lines.append("会话 ID：\(sessionID)") }
        lines.append("所在机器：" + (machine.map { $0 == machineID ? $0 : "\($0)（\(machineID)）" } ?? machineID))
        lines.append("工作目录：\(cwd)")
        lines.append("启动器：\(launcher)" + (model.map { " · 模型：\($0)" } ?? ""))
        if let sessionID { lines.append("在那台机器上接着聊：cd \(cwd) && claude --resume \(sessionID)") }
        return lines.joined(separator: "\n")
    }
}

/// The face in the bar, in its own view: what it shows reads the machines, the session and its last item, all of which
/// every hub push replaces; read in the bar itself, each push rebuilt the bar and its menu (an open menu with it).
private struct TopBarDot: View {
    @Environment(AppModel.self) private var model
    let key: SessionKey?
    let machineID: String

    var body: some View {
        PersonaDot(size: Theme.Size.avatarL, state: model.personaState(key, machineID: machineID))
    }
}

extension AppModel {
    /// The machine Persona's conversation `key` runs on; on the new-conversation page (nil) the assistant's.
    func assistantMachineID(_ key: SessionKey?) -> String {
        key?.machineID ?? assistantConfig?.machineID ?? hubMachineID
    }

    /// What the persona's face shows for Persona's conversation `key` (docs/persona.md「状态」): 离线 when the hub or
    /// Persona's machine is out of reach, 等你处理 when the conversation waits on the user, 思考中 / 工作中 while a turn
    /// runs (thinking until it writes or calls a tool), else 空闲. Read only by the face's own view (`TopBarDot`,
    /// `ShellPersonaButton`): every hub push changes what it reads.
    func personaState(_ key: SessionKey?, machineID: String) -> PersonaState {
        #if DEBUG
        // `-showPersonaState idle|thinking|working|needs_you|offline`: hold the face in one state (screenshots).
        if let s = UserDefaults.standard.string(forKey: "showPersonaState").flatMap(PersonaState.init(rawValue:)) { return s }
        #endif
        // 更新中… is the hub answering again: not 离线.
        let link = connection.shown
        if link == .waitingForNetwork || link == .connecting || machine(hubRef(machineID))?.online == false { return .offline }
        guard let key, let session = session(key) else { return .idle }
        if session.state == .needsYou || !interactions(for: key).isEmpty { return .needsYou }
        guard session.state == .working else { return .idle }
        let last = chats[key]?.items.last
        return last == nil || last?.role == "user" || last?.kind == .thinking ? .thinking : .working
    }
}

/// `TopFog` behind a floating bar, as its background: from the top of the screen (under the status bar) down to `solid`
/// below the safe area's top, then fading out over `fade`, and across the whole width. On iPhone Duo's outer screen
/// the status bar stands at the side: a fog only as wide as the page stopped beside it, a hard edge (#283).
struct BarFog: View {
    var solid: CGFloat
    var fade: CGFloat

    var body: some View {
        GeometryReader { g in
            let inset = g.safeAreaInsets
            let solidHeight = inset.top + solid
            TopFog(solidHeight: solidHeight, fadeHeight: fade)
                .frame(width: g.size.width + inset.leading + inset.trailing, height: solidHeight + fade)
                .offset(x: -inset.leading, y: -inset.top)
        }
    }
}

/// The fog under a floating bar: the page's own color, blurred, solid for `solidHeight` and then fading out over
/// `fadeHeight` (the content scrolls under it and shows through only below the bar).
struct TopFog: View {
    var solidHeight: CGFloat
    var fadeHeight: CGFloat

    var body: some View {
        let solid = solidHeight / max(solidHeight + fadeHeight, 1)
        ZStack {
            Rectangle().fill(.regularMaterial)
            Theme.pageBackground.opacity(0.82)
        }
        .mask(LinearGradient(stops: [.init(color: .black, location: 0), .init(color: .black, location: solid),
                                     .init(color: .clear, location: 1)], startPoint: .top, endPoint: .bottom))
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }
}
