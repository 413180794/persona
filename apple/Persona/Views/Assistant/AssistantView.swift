import BeautifulUI
import PhotosUI
import SwiftUI

/// The Persona tab: one personal assistant (a Claude session in ~/.jarvis/assistant on the hub's machine) that can reach the
/// user's computers, iPhone data, mail, company data and the knowledge base. One conversation is the main one (where the
/// user usually talks to it, and where other pages hand things to it); the others are side conversations.
struct AssistantView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.horizontalSizeClass) private var widthClass
    /// The ChatGPT-style shell (docs/ios.md「新外壳」): it owns the drawer, and pushes its pages here.
    @Environment(\.shell) private var shell
    #if os(iOS)
    /// The iPad's tabs without 收件箱 and 设置 (`RootTabs.padSidebar`): the sidebar column lists them as the shell's does.
    @Environment(\.padRouter) private var padRouter
    #endif
    @State private var pending: PendingStart?
    /// Pages pushed by value: the shell's 收件箱 and 机器 (and what they push themselves).
    @State private var path = NavigationPath()
    /// The last shell request shown (`ShellState.request`'s serial).
    @State private var handledRequest = 0
    @State private var startError: String?
    @State private var starterLauncher: String?
    @State private var showProfile = false
    @State private var showKnowledge = false
    @State private var showRoutines = false
    @State private var showTodos = false
    /// The sidebar (历史对话, or a drag to the right anywhere on the page): docs/ios.md「Persona 顶栏和侧边栏」.
    @State private var drawer = AssistantDrawer()
    /// Regular width (iPhone Duo open): the sidebar is a column beside the conversation; 历史对话 shows or hides it.
    @State private var columns = SplitColumns.shown
    #if DEBUG
    @State private var showAvatars = false
    #endif
    /// Mode / effort for new conversations ("" = the launcher's own), remembered across launches.
    @StoredValue("assistant.newMode") private var starterMode = ""
    @StoredValue("assistant.newEffort") private var starterEffort = ""

    private var current: SessionKey? { model.assistantCurrent }

    /// The sidebar column has the shell's places (收件箱, 待办, 机器) and 设置 (the iPad, `padRouter`).
    private var padPlaces: Bool {
        #if os(iOS)
        padRouter != nil && widthClass == .regular
        #else
        false
        #endif
    }

    /// What the shell or the iPad's tabs asked this page to show.
    private var request: (page: ShellPage, serial: Int)? {
        #if os(iOS)
        if shell == nil, let padRouter { return padRouter.request }
        #endif
        return shell?.request
    }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("AssistantView")
        #endif
        if shell != nil {
            stack // the shell holds the drawer around both of its pages
        } else if widthClass == .regular {
            // The split keeps both columns clear of the fold when the device is partly folded.
            #if os(macOS)
            // The history list beside the conversation, flush with the window; 历史对话 folds it away. Not a
            // NavigationSplitView: the Mac's would not fold the list's column (#856, `MacListBeside`).
            MacListBeside(listShown: columns != .detailOnly, widthKey: "mac.assistant.listWidth") {
                sidebar(column: true)
                    // The list's name and search are the title bar's row, beside the traffic lights: the row stood
                    // empty above them (#830). Nothing of the window's toolbar sits over this column.
                    .ignoresSafeArea(.container, edges: .top)
            } page: {
                stack
            }
            #else
            NavigationSplitView(columnVisibility: $columns) {
                // The system sizes the column on iPhone Duo (and moves the divide onto the fold); iPad, as the other tabs.
                sidebar(column: true)
                    .toolbarVisibility(.hidden, for: .navigationBar)
                    .modifier(SidebarWidth())
            } detail: {
                stack
            }
            .modifier(SidebarTopFill(shown: columns != .detailOnly))
            .navigationSplitViewStyle(.balanced)
            #endif
        } else {
            // The sidebar takes the whole height, like ChatGPT's: the tab bar fades away as it comes out
            // (`AssistantDrawer.tabBar`).
            AssistantDrawerLayout(drawer: drawer, enabled: atRoot, sidebar: { sidebar(column: false) }, page: stack)
        }
    }

    /// The sidebar, as the drawer (compact) or as a column of the split (regular, the tab bar beside it: no 设置; on
    /// the iPad, whose tabs have neither, the shell's places and 设置 as on the iPhone, `padPlaces`).
    private func sidebar(column: Bool) -> some View {
        let places = column && padPlaces
        return AssistantSidebar(current: pending == nil ? current : nil,
                                open: { key in
                                    model.assistantCurrent = key
                                    pending = nil
                                    drawer.close()
                                    // The conversation, not the page pushed over it (the same one open: no change to see).
                                    if places { backToRoot() }
                                },
                                newConversation: { newConversation(); drawer.close(); if places { backToRoot() } },
                                openProfile: { drawer.close(); showProfile = true },
                                openRoutines: { drawer.close(); showRoutines = true },
                                openKnowledge: { drawer.close(); showKnowledge = true },
                                openSettings: places ? { model.shellSettings = true }
                                    : column ? nil : { drawer.close(); model.selectedTab = .settings },
                                drawer: column ? nil : drawer,
                                openInbox: places ? { show(.inbox) } : nil,
                                openTodos: places ? { show(.todos) } : { drawer.close(); showTodos = true },
                                onMachines: places ? { show(.machines) } : nil)
    }

    /// 历史对话: the drawer on a phone, the sidebar column beside the conversation on a wide display.
    private func toggleSidebar() {
        if let shell {
            shell.drawer.open()
        } else if widthClass == .regular {
            withAnimation(Theme.Motion.standard) { columns = columns == .detailOnly ? SplitColumns.shown : .detailOnly }
        } else {
            drawer.open()
        }
    }

    /// Nothing pushed on the page: the sidebar's drag only works here (pushed pages have their own back swipe).
    private var atRoot: Bool {
        var pushed = showProfile || showKnowledge || showRoutines || showTodos || !path.isEmpty
        #if DEBUG
        pushed = pushed || showAvatars
        #endif
        return !pushed
    }

    /// The drawer the page slides with: the shell's, or its own.
    private var activeDrawer: AssistantDrawer { shell?.drawer ?? drawer }

    private var stack: some View {
        NavigationStack(path: $path) {
            Group {
                if let key = current, pending == nil {
                    ChatView(key: key, style: .assistant)
                        .id(key)
                        // The iPad draws the shell's composer too (`ChatComposerKind.padCard`): the assistant's, as on the iPhone.
                        .environment(\.shellChat, shell != nil || ChatComposerKind.padCard)
                } else if shell != nil {
                    ShellHome(pending: pending, error: startError, launcher: $starterLauncher, mode: $starterMode,
                              effort: $starterEffort, openInbox: { path.append($0) }) { text, images in
                        Task { await start(text, images) }
                    }
                } else {
                    AssistantHome(pending: pending, error: startError, launcher: $starterLauncher, mode: $starterMode,
                                  effort: $starterEffort) { text, images in
                        Task { await start(text, images) }
                    }
                }
            }
            // No system bar: the top bar floats over the conversation with a fog under it (it scrolls beneath).
            .toolbarVisibility(.hidden, for: .navigationBar)
            #if os(iOS) || os(visionOS)
            .background { RootBarHidden(atRoot: atRoot) }
            #endif
            .overlay(alignment: .top) {
                AssistantTopBar(key: pending == nil ? current : nil,
                                sidebarIsColumn: widthClass == .regular,
                                openSidebar: toggleSidebar,
                                newConversation: newConversation,
                                openKnowledge: { showKnowledge = true },
                                openProfile: { showProfile = true })
                #if os(macOS)
                // At the window's edge only with the history list folded away too (#830).
                .transformEnvironment(\.macTitleRowLeading) { if columns != .detailOnly { $0 = 0 } }
                #endif
            }
            #if os(macOS)
            // The bar is the title bar's row, as the list beside it (#830); pushed pages keep their own.
            .ignoresSafeArea(.container, edges: .top)
            #endif
            .navigationDestination(isPresented: $showProfile) { AssistantProfileView() }
            .navigationDestination(isPresented: $showKnowledge) { KnowledgeView() }
            .navigationDestination(isPresented: $showRoutines) { AssistantRoutinesView() }
            .navigationDestination(isPresented: $showTodos) { TodosView() }
            .modifier(ShellDestinations(path: $path, enabled: shell != nil || padPlaces))
            .modifier(ClassicTodoDetailDestination(enabled: shell == nil && !padPlaces))
            // On appear too (a cold launch onto 收件箱 asks before this page exists), but each request once: the root
            // appears again after every pop, and replaying the last request pushed the inbox right back.
            .onChange(of: request?.serial, initial: true) { _, serial in
                guard let serial, serial > handledRequest else { return }
                handledRequest = serial
                show(request?.page)
            }
            .onChange(of: atRoot, initial: true) { _, root in shell?.chatAtRoot = root }
            // Opening a conversation from the profile (a run, a follow-up) shows it: back to the root. So does 问 Persona
            // from the knowledge base, even when its conversation is the one already open.
            .onChange(of: model.assistantCurrent) { backToRoot() }
            .onChange(of: model.assistantReveal) { backToRoot(); activeDrawer.close() }
            #if DEBUG
            // `-openAssistantPage profile|avatars|routines|runs|…` (with `-tab assistant`): the profile and its pages;
            // `-openAssistantSidebar YES` (or a fraction): the sidebar is out; `-showDrawerAutoDrag <rounds>`: scripted drags, measured
            // (`DrawerProbe`, with `-showDrawerProbe YES`).
            .navigationDestination(isPresented: $showAvatars) { PersonaGallery() }
            .onAppear {
                switch UserDefaults.standard.string(forKey: "openAssistantPage") {
                case "avatars": showAvatars = true
                case "knowledge": showKnowledge = true
                case "todos": showTodos = true
                case .some(let p) where p != "assistant": showProfile = true
                default: break
                }
                // `-openAssistantSidebar 0.5`: held part way out, as a drag leaves it (screenshots of its corners).
                let sidebarAt = UserDefaults.standard.double(forKey: "openAssistantSidebar")
                if sidebarAt > 0 { activeDrawer.progress = min(sidebarAt, 1) }
                else if UserDefaults.standard.bool(forKey: "openAssistantSidebar") { activeDrawer.progress = 1 }
                #if os(iOS)
                let rounds = UserDefaults.standard.integer(forKey: "showDrawerAutoDrag")
                if rounds > 0 { Task { await DrawerAutoDrag(drawer: activeDrawer).run(rounds: rounds) } }
                #endif
                // `-openShellPage inbox|machines|routines|knowledge|profile` (the new shell): that page pushed.
                switch UserDefaults.standard.string(forKey: "openShellPage") {
                case "inbox": show(.inbox)
                case "todos": show(.todos)
                case "machines": show(.machines)
                case "routines": show(.routines)
                case "knowledge": show(.knowledge)
                case "profile": show(.profile)
                default: break
                }
            }
            #endif
            .task { await model.refreshAssistant() }
            // 新建主对话… from the top bar's menu or the sidebar's (#1762).
            .sheet(isPresented: Binding(get: { model.newMainAsked }, set: { model.newMainAsked = $0 })) { NewMainSheet() }
            #if DEBUG
            .task { // `-showNewMain YES`: the question up, for screenshots
                if UserDefaults.standard.bool(forKey: "showNewMain") { try? await Task.sleep(for: .seconds(3)); model.newMainAsked = true }
            }
            #endif
        }
        #if os(macOS)
        // Its pushed pages (知识库, 定时任务, 待办…) go back from the title row, not the system's button (#2318).
        .environment(\.macSectionStack, true)
        #endif
    }

    /// A page the shell asked for (the drawer, the home page): from the conversation's root, so back comes to it.
    private func show(_ page: ShellPage?) {
        guard let page else { return }
        backToRoot()
        switch page {
        case .newConversation: newConversation()
        case .profile: showProfile = true
        case .routines: showRoutines = true
        case .knowledge: showKnowledge = true
        case .inbox: path.append(ShellRoute.inbox)
        case .todos: path.append(ShellRoute.todos)
        case .machines: path.append(ShellRoute.machines)
        }
    }

    private func backToRoot() {
        if !path.isEmpty { path = NavigationPath() }
        showProfile = false
        showRoutines = false
        showKnowledge = false
        showTodos = false
        #if os(iOS)
        // A task pushed beside the sidebar (regular width, `detailPresentation`) is a page of this stack too: going
        // back to the root, or to another conversation, takes it away (else the next chat would push it again). A
        // phone's task sheet stays as it was.
        if widthClass == .regular {
            TasksStore.shared.detailTask = nil
            TasksStore.shared.detailAgent = nil
        }
        #endif
    }

    private func newConversation() {
        withAnimation(Theme.Motion.standard) {
            model.assistantCurrent = nil
            pending = nil
        }
    }

    private func start(_ text: String, _ images: [ImagePayload]) async {
        guard let api = model.api else { return }
        startError = nil
        withAnimation(Theme.Motion.standard) { pending = PendingStart(text: text, imageCount: images.count) }
        do {
            let s = try await api.startConversation(prompt: text, images: images, launcher: starterLauncher,
                                                    permissionMode: starterMode.isEmpty ? nil : starterMode,
                                                    effort: starterEffort.isEmpty ? nil : starterEffort,
                                                    main: model.assistantMain == nil) // the first one becomes the main conversation
            if model.assistantMain == nil { await model.refreshAssistant() }
            Haptics.success()
            model.began(s, text: text, images: images) // the message stays on screen until claude has written it
            withAnimation(Theme.Motion.standard) {
                model.assistantCurrent = s.key
                pending = nil
            }
        } catch {
            withAnimation(Theme.Motion.standard) {
                pending = nil
                startError = error.shown
            }
            Haptics.error()
        }
    }
}

struct PendingStart: Equatable {
    var text: String
    var imageCount: Int
}

/// The first message of a new conversation while it spins up, laid out as the chat will lay it out the moment it takes
/// over: the day, the message in the chat's own bubble, then a typing line, at the bottom over the composer, in the
/// chat's margins. It used to sit at the top of the new-conversation page and jump to the bottom when the chat came in
/// (#1911). `bottom`: the space the chat leaves between its newest row and the composer.
struct FirstMessagePending: View {
    var pending: PendingStart
    var name: String
    var shell: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.l) { // the chat's rows' spacing
            DayLine(date: .now)
            VStack(alignment: .trailing, spacing: Theme.Space.sm) {
                if pending.imageCount > 0 {
                    Label("\(pending.imageCount) 张图片", systemImage: Theme.Icon.photo).font(Theme.Font.label).foregroundStyle(.secondary)
                }
                HStack {
                    Spacer(minLength: 48)
                    UserBubbleBody(quotes: [], text: pending.text)
                }
            }
            .frame(maxWidth: .infinity, alignment: .trailing)
            TypingIndicator(text: "\(name) 正在准备…")
        }
        .padding(.horizontal, Theme.Space.l) // the chat's list margins
        .frame(maxWidth: ChatView.readableWidth)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottom)
    }
}

/// The old layout's new conversation: `AssistantWelcome` (greeting, what Persona can reach, one-tap suggestions); while
/// a conversation spins up it shows the first message and a typing indicator so the send feels instant.
struct AssistantHome: View {
    @Environment(AppModel.self) private var model
    var pending: PendingStart?
    var error: String?
    @Binding var launcher: String?
    @Binding var mode: String
    @Binding var effort: String
    var send: (String, [ImagePayload]) -> Void

    /// How far the page fades out above the chips and the field.
    private static let fade = Theme.Space.xl

    var body: some View {
        ScrollView {
            if let pending {
                FirstMessagePending(pending: pending, name: model.persona.name, shell: false)
                    // The page's height inside its margins, the message at its bottom (a long one runs up out of it, as the
                    // chat shows its newest end).
                    .containerRelativeFrame(.vertical, alignment: .bottom)
            } else {
                VStack(alignment: .leading, spacing: Theme.Space.xl) {
                    AssistantWelcome(error: error, send: send)
                }
                .padding(.horizontal, Theme.Space.xl)
                .padding(.bottom, Theme.Space.xl)
                // The conversation's measure on a wide display (iPhone Duo open).
                .frame(maxWidth: ChatView.readableWidth)
                .frame(maxWidth: .infinity)
            }
        }
        .scrollDismissesKeyboard(.interactively)
        .dismissesKeyboardOnTap()
        // The floating top bar (AssistantTopBar) is over the page: leave its room, let the page scroll under it. Before
        // the composer is added: margins reach every scroll view below them, and the chips' row above the field stood
        // the top bar's height tall, over the suggestion cards.
        .contentMargins(.top, AssistantTopBar.height, for: .scrollContent)
        // Room to scroll the last suggestions clear of the fade over the chips (as ShellHome).
        .contentMargins(.bottom, pending == nil ? Self.fade : ChatView.newestGap, for: .scrollContent)
        .themedGroupedPage()
        .safeAreaInset(edge: .bottom, spacing: 0) {
            // The chips' row a step off the field (2026-11-04: with Theme.Space.s it sat right on it, and the
            // suggestion cards were cut off hard just above it).
            VStack(alignment: .leading, spacing: Theme.Space.m) {
                if ChatComposerKind.padCard {
                    // The iPad: the iPhone's composer, the launcher, mode and effort in its model menu (#2779).
                    StarterComposer(disabled: pending != nil, shell: StarterComposer.ShellModes(launcher: $launcher, mode: $mode, effort: $effort),
                                    send: send)
                } else {
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: Theme.Space.s) {
                            // The BeautifulUI style picks the launcher in the prompt bar's model menu.
                            if !AppStyle.current.usesBeautifulUI { StarterModelChip(launcher: $launcher) }
                            NewSessionModePicker(mode: $mode, effort: $effort, modes: SessionModes.assistantModes)
                        }
                        .padding(.horizontal, Theme.Space.xs)
                    }
                    if AppStyle.current.usesBeautifulUI {
                        BeautifulStarterComposer(disabled: pending != nil, launcher: $launcher, send: send)
                    } else {
                        StarterComposer(disabled: pending != nil, send: send)
                    }
                }
            }
            .padding(.horizontal, Theme.Space.m)
            .padding(.bottom, Theme.Space.s)
            .frame(maxWidth: ChatView.readableWidth)
            .frame(maxWidth: .infinity)
            // Above the chips the page fades out instead of ending in a hard edge across the cards.
            .background(alignment: .top) {
                LinearGradient(colors: [Theme.groupedPage.opacity(0), Theme.groupedPage], startPoint: .top, endPoint: .bottom)
                    .frame(height: Self.fade)
                    .offset(y: -Self.fade)
                    .allowsHitTesting(false)
                    .accessibilityHidden(true)
            }
            // At large text sizes the page reaches down here: back the chips and the field with the page, or the
            // machine chips scrolled under them read through.
            .background(Theme.groupedPage.ignoresSafeArea(edges: .bottom))
        }
        .navigationTitle("Persona")
        .task(id: launcher) { if pending == nil { await model.warmAssistant(launcher: launcher) } }
    }
}

/// What a new conversation opens on, in both layouts (2026-11-02 the user wanted it back in the shell, which had shown
/// only the composer): a greeting, what Persona can reach, one-tap things to ask.
struct AssistantWelcome: View {
    @Environment(AppModel.self) private var model
    var error: String? = nil
    var send: (String, [ImagePayload]) -> Void

    private let suggestions: [(symbol: String, title: String, prompt: String)] = [
        ("calendar", "今天的安排", "看一下我今天的安排：iPhone 日历和飞书日历都查，合并后按时间列出来。"),
        (Theme.Icon.mailAccounts, "要处理的邮件", "我的邮箱里最近有哪些需要我处理或回复的邮件？挑重要的说。"),
        ("macbook.and.iphone", "电脑在忙什么", "我各台电脑上的 Agent 现在都在忙什么？哪些在等我、哪些跑完了还没看？"),
        ("bubble.left.and.text.bubble.right", "飞书消息", "总结一下我今天飞书上收到的重要消息，哪些需要我回复。"),
        ("person.2.wave.2", "会前准备", "看看我今天接下来的会，挑最重要的一个帮我准备：议程、相关文档、上次的纪要和没完成的待办。"),
        ("heart.text.square", "这周的健康", "根据我 iPhone 上的健康数据，总结一下这周的步数、睡眠和运动情况。"),
    ]

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xl) {
            header
            if let error {
                Label(error, systemImage: Theme.Icon.error)
                    .font(Theme.Font.caption)
                    .foregroundStyle(Theme.danger)
            }
            if AppStyle.current.usesBeautifulUI {
                // BeautifulUI's list card: one row per thing to ask, its mark, what it is and how it is put.
                BUIListSection("可以问我") {
                    ForEach(suggestions, id: \.title) { s in
                        Button {
                            Haptics.tap()
                            send(s.prompt, [])
                        } label: {
                            BUIListRow(s.title, detail: s.prompt, accessory: .disclosure, detailLines: 1) {
                                BUIIconTile(s.symbol, tint: Theme.accent)
                            }
                        }
                        .buttonStyle(.plain)
                        .modifier(WelcomeFold.Mark(first: s.title == suggestions.first?.title))
                    }
                }
            } else {
                LazyVGrid(columns: [GridItem(.flexible(), spacing: 12), GridItem(.flexible(), spacing: 12)], spacing: 12) {
                    ForEach(suggestions, id: \.title) { s in
                        Button {
                            Haptics.tap()
                            send(s.prompt, [])
                        } label: {
                            VStack(alignment: .leading, spacing: Theme.Space.m) {
                                Image(systemName: s.symbol)
                                    .font(Theme.Font.title)
                                    .foregroundStyle(Theme.accent)
                                Text(s.title).font(Theme.Font.heading).foregroundStyle(Color.primary)
                            }
                            .scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.box72, alignment: .topLeading)
                            .card(padding: 14)
                        }
                        .buttonStyle(.plain)
                        // Two cards a row: the row ends where the taller of the first two does.
                        .modifier(WelcomeFold.Mark(first: suggestions.prefix(2).contains { $0.title == s.title }))
                    }
                }
            }
        }
        .coordinateSpace(.named(WelcomeFold.space))
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("assistant-welcome")
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: Theme.Space.l) {
            Text(greeting).font(Theme.Font.display).padding(.top, Theme.Space.xl)
            Text(model.assistantMain == nil
                 ? "这是你的主对话：我会记住这里聊过的事。我能看你的电脑、iPhone、邮箱、飞书和整理好的项目，也能替你盯着事、到点提醒。"
                 : "开一段新的旁支对话。我能看你的电脑、iPhone、邮箱、飞书和整理好的项目，也能替你盯着事、到点提醒。")
                .font(Theme.Font.secondary).lineHeight(.secondary)
                .foregroundStyle(.secondary)
            // Wraps: in one HStack, a few more machines (or larger text) widened the whole page past the screen.
            LarkFlowLayout(spacing: 8, lineSpacing: 8) {
                ForEach(model.machines.filter(\.online)) { m in
                    Chip(text: m.name, symbol: m.symbol)
                }
                #if os(macOS)
                // What this chip shows is this device's own link to the hub: on the Mac, this Mac.
                Chip(text: ConnectorText.device, symbol: model.live.looksConnected ? Theme.Icon.laptop : Theme.Icon.laptopOffline)
                #else
                Chip(text: "iPhone", symbol: model.live.looksConnected ? Theme.Icon.phone : "iphone.slash")
                #endif
            }
        }
    }

    private var greeting: String {
        switch Calendar.current.component(.hour, from: .now) {
        case 5..<11: "早上好"
        case 11..<13: "中午好"
        case 13..<18: "下午好"
        default: "晚上好"
        }
    }
}

/// Where the welcome's first row of things to ask ends, from the welcome's top (so scrolling does not move it): the
/// shell keeps what needs the user over the composer below it (`ShellHome`).
struct WelcomeFold: PreferenceKey {
    static let space = "assistant-welcome"
    static let defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }

    /// On every thing to ask; the ones in the first row say where they end.
    struct Mark: ViewModifier {
        var first: Bool

        func body(content: Content) -> some View {
            content.background {
                GeometryReader { g in
                    Color.clear.preference(key: WelcomeFold.self, value: first ? g.frame(in: .named(WelcomeFold.space)).maxY : 0)
                }
            }
        }
    }
}

struct TypingIndicator: View {
    var text: String
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var phase = 0.0

    var body: some View {
        if AppStyle.current.usesBeautifulUI { shimmer } else { dots }
    }

    /// The BeautifulUI style: its thinking header while it works, the sparkle and the words with the shimmer across them.
    private var shimmer: some View {
        HStack(spacing: Theme.Space.sm) {
            BUIIcon(.sparkle, size: 13)
                .foregroundStyle(Theme.ai)
            Text(text)
                .font(Theme.Font.secondary)
                .buiShimmer()
        }
        .accessibilityElement(children: .combine)
    }

    private var dots: some View {
        HStack(spacing: Theme.Space.m) {
            HStack(spacing: Theme.Space.xs) {
                ForEach(0..<3) { i in
                    Circle()
                        .fill(Theme.accent)
                        .scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
                        .opacity(0.35 + 0.65 * max(0, sin(phase + Double(i) * 0.9)))
                }
            }
            Text(text).font(Theme.Font.secondary).foregroundStyle(.secondary).contentTransition(.opacity)
        }
        .onAppear {
            guard !reduceMotion else { return }
            withAnimation(.linear(duration: 1.2).repeatForever(autoreverses: false)) { phase = .pi * 2 }
        }
    }
}

/// Composer for a conversation that does not exist yet: the chat's input line (`ComposerRow`) with its own text, sent
/// through `send` (it starts the conversation).
struct StarterComposer: View {
    @Environment(AppModel.self) private var model
    var disabled: Bool
    /// The shell's composer (docs/ios.md「新外壳」): the launcher, mode and effort are in its gauge menu.
    var shell: ShellModes? = nil
    var send: (String, [ImagePayload]) -> Void

    struct ShellModes {
        @Binding var launcher: String?
        @Binding var mode: String
        @Binding var effort: String
    }

    @State private var launchers: [Launcher] = []
    @State private var text = ""
    @State private var picks: [PhotosPickerItem] = []
    @State private var images: [(UIImage, ImagePayload)] = []
    @State private var showPhotos = false
    @State private var showFiles = false
    @State private var showRecording = false
    @State private var recordingContexts: [ContextRef] = []
    @State private var uploads = FileUploads()
    @State private var videos = VideoAttachments()
    @State private var selection: TextSelection?
    @FocusState private var focused: Bool

    private var canSend: Bool {
        !disabled && !videos.busy
            && (!text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !images.isEmpty || !videos.ready.isEmpty || !recordingContexts.isEmpty)
    }

    private func submit() {
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        let files = videos.ready
        // Nothing to send: a second ⏎ on the Mac before the field redraws (#785) would start a conversation with the
        // recording placeholder.
        guard !videos.busy, !t.isEmpty || !images.isEmpty || !files.isEmpty || !recordingContexts.isEmpty else { return }
        let body = AttachedFile.message(t.isEmpty ? (images.isEmpty ? (files.isEmpty ? "参考录音内容。" : "请看视频") : "请看图片") : t, files: files)
        // The keyboard goes down with the send: the conversation that takes over has a field of its own, and the keyboard
        // coming back up and going down again on the way moved the first message up and down with it (#1911).
        focused = false
        send(recordingContexts.isEmpty ? body : recordingContexts.map(\.xml).joined(separator: "\n") + "\n\n" + body, images.map(\.1))
        text = ""
        images = []
        videos.sent()
        recordingContexts = []
    }

    /// The assistant's machine (the hub's unless set otherwise): files and videos go to its temp directory.
    private var machine: MachineRef { model.hubRef(model.assistantConfig?.machineID ?? model.hubMachineID) }

    /// A PhotosPicker inside a Menu never presents (the menu dismisses first): the buttons present it from here.
    @ViewBuilder private var attachments: some View {
        Button { showPhotos = true } label: { Label("照片", systemImage: Theme.Icon.photos) }
        Button { showFiles = true } label: { Label("文件", systemImage: Theme.Icon.file) }
        Button { showRecording = true } label: { Label("录音上下文", systemImage: Theme.Icon.recording).lineLimit(1) }
    }

    var body: some View {
        VStack(spacing: Theme.Space.s) {
            if !uploads.items.isEmpty { UploadsBar(uploads: uploads) }
            if !videos.items.isEmpty { VideoAttachmentsBar(videos: videos) }
            if !images.isEmpty { ComposerImages(images: $images) }
            if let shell {
                ShellComposerBar(placeholder: "问 \(model.persona.name)", text: $text, selection: $selection, focused: $focused,
                                 canSend: canSend, hasAttachments: !images.isEmpty || !videos.items.isEmpty || !recordingContexts.isEmpty,
                                 modesTitle: StarterModesMenu.title(modes: shell, launchers: launchers, model: model),
                                 quotes: $recordingContexts, send: submit) {
                    attachments
                } modes: {
                    StarterModesMenu(modes: shell, launchers: launchers)
                }
                .task { launchers = (try? await model.launchers(model.hubRef(model.assistantConfig?.machineID ?? model.hubMachineID)))?.filter(\.runsAssistant) ?? [] }
            } else {
                ComposerRow(placeholder: "问 \(model.persona.name)…", text: $text, selection: $selection, focused: $focused, canSend: canSend,
                            send: submit, quotes: $recordingContexts) {
                    attachments
                }
            }
        }
        .recordingContextSheet(isPresented: $showRecording) { recordingContexts.append($0) }
        .mediaPicker(isPresented: $showPhotos, selection: $picks)
        .fileUploadPicker(isPresented: $showFiles) { urls in
            // The assistant runs on its machine (the hub's unless set otherwise): files go to that one's temp directory.
            uploads.start(urls, machine: model.hubRef(model.assistantConfig?.machineID ?? model.hubMachineID), model: model) { path in
                text = PathInsert.edit(path, text: text, selection: $selection, focused: $focused)
            }
        }
        #if os(macOS)
        // Finder drags and ⌘V of pictures / copied files, as in an Agent's composer (docs/mac.md「细节」).
        .macComposerFiles(images: images.count, focused: focused, attach: { MacDrop.append($0, to: &images) }, upload: { urls in
            uploads.start(urls, machine: model.hubRef(model.assistantConfig?.machineID ?? model.hubMachineID), model: model) { path in
                text = PathInsert.edit(path, text: text, selection: $selection, focused: $focused)
            }
        })
        #endif
        .onChange(of: picks) { _, items in
            Task {
                guard !items.isEmpty else { return }
                let movies = items.filter(PickedMovie.isVideo) // sent as files (VideoAttach.swift)
                if !movies.isEmpty { videos.start(movies, machine: machine, model: model) }
                for item in items where !PickedMovie.isVideo(item) {
                    guard let data = try? await item.loadTransferable(type: Data.self), let img = UIImage(data: data) else {
                        AppModel.shared.toast = Toast("有照片读不出来（可能还在 iCloud 里没下载完）", style: .error)
                        continue
                    }
                    let scaled = img.scaledDown(maxSide: 1600)
                    if let jpeg = scaled.jpegData(compressionQuality: 0.75) {
                        images.append((scaled, ImagePayload(mime: "image/jpeg", data: jpeg.base64EncodedString())))
                    }
                }
                picks = []
            }
        }
    }
}

/// The BeautifulUI style's composer for a conversation that does not exist yet: the prompt bar (`PromptComposer`) with its own text,
/// sent through `send` (it starts the conversation). Its model menu is the launcher the conversation starts with.
struct BeautifulStarterComposer: View {
    @Environment(AppModel.self) private var model
    var disabled: Bool
    @Binding var launcher: String?
    var send: (String, [ImagePayload]) -> Void
    @State private var text = ""
    @State private var picks: [PhotosPickerItem] = []
    @State private var images: [(UIImage, ImagePayload)] = []
    @State private var quotes: [ContextRef] = []
    @State private var launchers: [Launcher] = []
    @State private var showPhotos = false
    @State private var showFiles = false
    @State private var showRecording = false
    @State private var uploads = FileUploads()
    @State private var videos = VideoAttachments()
    @State private var selection: TextSelection?
    @FocusState private var focused: Bool

    private var launcherMachine: String { model.assistantConfig?.machineID ?? model.hubMachineID }
    private var currentLauncher: String { launcher ?? model.assistantConfig?.launcher ?? "ccc" }
    /// Where files and videos go: the assistant's machine.
    private var machine: MachineRef { model.hubRef(launcherMachine) }

    /// The machine's launchers; until they load, the current one alone, so the button shows what a new
    /// conversation starts with.
    private var models: [PromptModel] {
        let list = launchers.map { PromptModel(id: $0.name, name: $0.name, tag: SessionModes.gptModelName($0.model) ?? Fmt.model($0.model) ?? "") }
        return list.isEmpty ? [PromptModel(id: currentLauncher, name: currentLauncher, tag: "")] : list
    }

    var body: some View {
        VStack(spacing: Theme.Space.s) {
            if !uploads.items.isEmpty { UploadsBar(uploads: uploads) }
            if !videos.items.isEmpty { VideoAttachmentsBar(videos: videos) }
            if !images.isEmpty { ComposerImages(images: $images) }
            PromptComposer(placeholder: "问 \(model.persona.name)…", text: $text, selection: $selection, focused: $focused,
                           hasAttachments: !images.isEmpty || !videos.items.isEmpty || !quotes.isEmpty, disabled: disabled,
                           attach: PromptItems.attachments(camera: false),
                           mentions: { PromptItems.sessions(model.sessionsNow, except: nil) },
                           models: models,
                           modelID: currentLauncher,
                           pickModel: { id in
                               launcher = id
                               Haptics.tap()
                           },
                           onAttach: { source in
                               switch source.id {
                               case "photo": showPhotos = true
                               case "recording": showRecording = true
                               default: showFiles = true
                               }
                           },
                           onMention: { source in
                               guard let ref = PromptItems.quote(source, in: model.sessionsNow) else { return false }
                               quotes.append(ref)
                               return true
                           },
                           quotes: $quotes,
                           send: {
                               let typed = text
                               let t = typed.trimmingCharacters(in: .whitespacesAndNewlines)
                               if videos.busy { // nothing goes; the bar clears the box right after this, so it is put back
                                   model.toast = Toast("视频还在上传，传完再发送")
                                   Task { text = typed }
                                   return
                               }
                               let files = videos.ready
                               let body = AttachedFile.message(t.isEmpty ? (images.isEmpty ? (files.isEmpty ? "参考上面的内容。" : "请看视频") : "请看图片") : t,
                                                               files: files)
                               // Quoted sessions ride along as <jarvis-context> tags before what was typed, as in a chat.
                               send(quotes.isEmpty ? body : quotes.map(\.xml).joined(separator: "\n") + "\n\n" + body, images.map(\.1))
                               text = ""
                               images = []
                               videos.sent()
                               quotes = []
                           })
        }
        // Again once the hub is paired: on a fresh launch the first try runs before there is one.
        .task(id: launcherMachine) { launchers = (try? await model.launchers(model.hubRef(launcherMachine))) ?? [] }
        .recordingContextSheet(isPresented: $showRecording) { quotes.append($0) }
        .mediaPicker(isPresented: $showPhotos, selection: $picks)
        .fileUploadPicker(isPresented: $showFiles) { urls in
            // The assistant runs on its machine (the hub's unless set otherwise): files go to that one's temp directory.
            uploads.start(urls, machine: model.hubRef(model.assistantConfig?.machineID ?? model.hubMachineID), model: model) { path in
                text = PathInsert.edit(path, text: text, selection: $selection, focused: $focused)
            }
        }
        #if os(macOS)
        // Finder drags and ⌘V of pictures / copied files, as in an Agent's composer (docs/mac.md「细节」).
        .macComposerFiles(images: images.count, focused: focused, attach: { MacDrop.append($0, to: &images) }, upload: { urls in
            uploads.start(urls, machine: model.hubRef(model.assistantConfig?.machineID ?? model.hubMachineID), model: model) { path in
                text = PathInsert.edit(path, text: text, selection: $selection, focused: $focused)
            }
        })
        #endif
        .onChange(of: picks) { _, items in
            Task {
                guard !items.isEmpty else { return }
                let movies = items.filter(PickedMovie.isVideo) // sent as files (VideoAttach.swift)
                if !movies.isEmpty { videos.start(movies, machine: machine, model: model) }
                for item in items where !PickedMovie.isVideo(item) {
                    guard let data = try? await item.loadTransferable(type: Data.self), let img = UIImage(data: data) else {
                        AppModel.shared.toast = Toast("有照片读不出来（可能还在 iCloud 里没下载完）", style: .error)
                        continue
                    }
                    let scaled = img.scaledDown(maxSide: 1600)
                    if let jpeg = scaled.jpegData(compressionQuality: 0.75) {
                        images.append((scaled, ImagePayload(mime: "image/jpeg", data: jpeg.base64EncodedString())))
                    }
                }
                picks = []
            }
        }
    }
}

/// Tool activity in the assistant: one quiet line ("工具 4 · 运行 3 · 读取 1"), expandable to the steps.
struct AssistantToolsRow: View {
    var steps: [ToolStep]
    @State private var open = false

    private var calls: [ToolCall] { steps.compactMap { if case .call(let c) = $0 { c } else { nil } } }

    var body: some View {
        let calls = calls
        let count = steps.reduce(0) { $0 + $1.tools } // a fold counts the calls it stands for
        let running = calls.last.map(\.isRunning) ?? false
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            if AppStyle.current.usesBeautifulUI {
                // BeautifulUI's chip run, as in an agent's chat: one chip per kind of tool (a spinner while one runs).
                Button {
                    withAnimation(Theme.Motion.standard) { open.toggle() }
                } label: {
                    ToolChipGroup(ToolRunSummary.chips(calls), isOpen: open, isRunning: running)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("\(count) 次工具调用：\(sources(calls))")
            } else {
                Button {
                    withAnimation(Theme.Motion.standard) { open.toggle() }
                } label: {
                    HStack(spacing: Theme.Space.sm) {
                        if running {
                            ProgressView().controlSize(.mini)
                            if let last = calls.last {
                                Text("\(ToolPresentation.verb(last.name)) \(ToolPresentation.subject(last))").lineLimit(1)
                            }
                        } else {
                            Image(systemName: Theme.Icon.unknownTool)
                            Text("工具 \(count)")
                            Text(sources(calls)).foregroundStyle(.tertiary).lineLimit(1)
                        }
                        Spacer(minLength: 0)
                        Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong).rotationEffect(.degrees(open ? 90 : 0))
                    }
                    .font(Theme.Font.label)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, Theme.Space.m)
                    .padding(.vertical, Theme.Space.s)
                    .background(Theme.Fill.well, in: .capsule)
                }
                .buttonStyle(.plain)
            }
            if open {
                ForEach(steps) { step in
                    switch step {
                    case .call(let c): ToolCallRow(call: c, autoExpand: false)
                    case .folded(let f): FoldedWorkRow(fold: f)
                    case .thought: EmptyView()
                    }
                }
            }
        }
    }

    /// Which kinds of data were touched, in plain words.
    private func sources(_ calls: [ToolCall]) -> String {
        var seen: [String] = []
        for c in calls {
            let n = ToolPresentation.baseName(c.name)
            let label: String
            if n.hasPrefix("phone_") { label = "iPhone" }
            else if n.hasPrefix("mail_") { label = "邮件" }
            else if n == "Bash" { label = (c.input["command"]?.string ?? "").contains("ssh ") ? "电脑" : "命令" }
            else if n == "Skill" { label = c.input["skill"]?.string ?? "技能" }
            else if n.hasPrefix("Web") { label = "网页" }
            else { label = ToolPresentation.verb(c.name) }
            if !seen.contains(label) { seen.append(label) }
        }
        return seen.prefix(4).joined(separator: "、")
    }
}

/// Model chip on the assistant home page: which launcher the next new conversation starts with.
struct StarterModelChip: View {
    @Environment(AppModel.self) private var model
    @Binding var launcher: String?
    @State private var launchers: [Launcher] = []

    var body: some View {
        let current = launcher ?? model.assistantConfig?.launcher ?? "ccc"
        Menu {
            ForEach(launchers) { l in
                Button {
                    launcher = l.name
                    Haptics.tap()
                } label: {
                    if l.name == current { Label(l.name, systemImage: Theme.Icon.done) } else { Text("\(l.name)  \(SessionModes.gptModelName(l.model) ?? Fmt.model(l.model) ?? "")") }
                }
            }
        } label: {
            ModeChip(symbol: Theme.Icon.model, text: current, tint: .secondary) // neutral, like the mode and effort chips beside it
        }
        .labelOnlyMenu()
        .task { launchers = (try? await model.launchers(model.hubRef(model.assistantConfig?.machineID ?? model.hubMachineID))) ?? [] }
    }
}

/// The shell composer's model menu for a conversation not started yet (#1091): the launcher it starts with (that is the
/// model), the effort and, a level down, the permission mode ("" keeps the launcher's own).
struct StarterModesMenu: View {
    @Environment(AppModel.self) private var model
    var modes: StarterComposer.ShellModes
    var launchers: [Launcher]

    /// The button's title: the launcher's model (its name when it names none) and the effort picked.
    @MainActor static func title(modes: StarterComposer.ShellModes, launchers: [Launcher], model: AppModel) -> SessionModes.ComposerTitle? {
        let name = modes.launcher ?? model.assistantConfig?.launcher
        let launcher = launchers.first { $0.name == name }
        return SessionModes.composerTitle(model: SessionModes.modelName(launcher?.model) ?? launcher?.name ?? name,
                                          effort: SessionModes.effort(modes.effort)?.title)
    }

    var body: some View {
        if modes.mode == "bypassPermissions" { // said first, as in a running conversation's menu (SessionModesMenu)
            Section { BypassWarning() }
        }
        if !launchers.isEmpty {
            Section("模型") {
                Picker("启动器", selection: Binding(get: { modes.launcher ?? model.assistantConfig?.launcher ?? "" }, set: { modes.launcher = $0 })) {
                    ForEach(launchers) { l in
                        Text(SessionModes.modelName(l.model).map { "\(l.name) · \($0)" } ?? l.name).tag(l.name)
                    }
                }
                .pickerStyle(.inline)
            }
        }
        Section("思考强度") {
            Picker("思考强度", selection: modes.$effort) {
                Text("跟随启动器").tag("")
                ForEach(SessionModes.effortLevels) { o in Label { Text(o.title); Text(o.detail) } icon: { Image(systemName: o.symbol) }.tag(o.id) }
            }
            .pickerStyle(.inline)
        }
        Picker(selection: modes.$mode) {
            Text("跟随启动器").tag("")
            ForEach(SessionModes.assistantModes) { o in Label { Text(o.title); Text(o.detail) } icon: { Image(systemName: o.symbol) }.tag(o.id) }
        } label: {
            Label("权限模式：\(SessionModes.mode(modes.mode)?.title ?? "跟随启动器")", systemImage: SessionModes.mode(modes.mode)?.symbol ?? Theme.Icon.askFirst)
        }
        .pickerStyle(.menu)
    }
}

#if os(iOS) || os(visionOS)
/// Hides the stack's navigation bar again once the page is back at its root. Swiped back from a pushed page with its
/// search still open (知识库), UIKit left the bar showing over the root, with the root's title and ~106 pt tall: the top
/// bar was pushed down under it, and a tap where the switch belongs hit the empty bar (2026-11-01). SwiftUI still has
/// the bar hidden, so it never hides it again.
private struct RootBarHidden: UIViewRepresentable {
    var atRoot: Bool

    func makeUIView(context: Context) -> UIView {
        let v = UIView()
        v.isUserInteractionEnabled = false
        return v
    }

    func updateUIView(_ v: UIView, context: Context) {
        guard atRoot else { return }
        // Now, and once the pop and the search's own dismissal have finished.
        for delay in [0.0, 0.4, 0.9] {
            DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak v] in
                var r: UIResponder? = v
                while let next = r?.next, !(next is UIViewController) { r = next }
                guard let nav = (r?.next as? UIViewController)?.navigationController,
                      nav.viewControllers.count == 1, !nav.isNavigationBarHidden else { return }
                nav.setNavigationBarHidden(true, animated: false)
            }
        }
    }
}
#endif

/// The classic layout has no ShellDestinations, so nothing registers the todo detail route in this stack: a todo row
/// pushed from the todo list (TodosView) would open nothing. The shell registers it itself (ShellHome.swift).
private struct ClassicTodoDetailDestination: ViewModifier {
    var enabled: Bool

    func body(content: Content) -> some View {
        if enabled {
            content.navigationDestination(for: TodoDetailRoute.self) { r in TodoDetailView(todoID: r.id) }
        } else {
            content
        }
    }
}
