import BeautifulUI
import PhotosUI
import SwiftUI

/// The Jarvis tab: one personal assistant (a Claude session in ~/.jarvis/assistant on the hub's machine) that can reach the
/// user's computers, iPhone data, mail, company data and the knowledge base. One conversation is the main one (where the
/// user usually talks to it, and where other pages hand things to it); the others are side conversations.
struct AssistantView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.horizontalSizeClass) private var widthClass
    @State private var pending: PendingStart?
    @State private var startError: String?
    @State private var starterLauncher: String?
    @State private var showProfile = false
    @State private var showKnowledge = false
    @State private var showRoutines = false
    /// The sidebar (历史对话, or a drag to the right anywhere on the page): docs/ios.md「Jarvis 顶栏和侧边栏」.
    @State private var drawer = AssistantDrawer()
    /// Regular width (iPhone Duo open): the sidebar is a column beside the conversation; 历史对话 shows or hides it.
    @State private var columns = NavigationSplitViewVisibility.all
    #if DEBUG
    @State private var showAvatars = false
    #endif
    /// Mode / effort for new conversations ("" = the launcher's own), remembered across launches.
    @AppStorage("assistant.newMode") private var starterMode = ""
    @AppStorage("assistant.newEffort") private var starterEffort = ""

    private var current: SessionKey? { model.assistantCurrent }

    var body: some View {
        #if PERF_SIGNPOST
        let _ = PerfTrace.event("AssistantView.body")
        #endif
        if widthClass == .regular {
            // The split keeps both columns clear of the fold when the device is partly folded.
            NavigationSplitView(columnVisibility: $columns) {
                // No width of our own: the system sizes the column, and moves the divide onto the fold.
                sidebar(column: true)
                    .toolbarVisibility(.hidden, for: .navigationBar)
            } detail: {
                stack
            }
            .navigationSplitViewStyle(.balanced)
        } else {
            AssistantDrawerLayout(drawer: drawer, enabled: atRoot, sidebar: { sidebar(column: false) }, page: stack)
                // The sidebar takes the whole height, like ChatGPT's: the tab bar would sit over its bottom row and
                // repeat its 设置 right under the sidebar's own.
                .toolbarVisibility(drawer.isOpen ? .hidden : .automatic, for: .tabBar)
        }
    }

    /// The sidebar, as the drawer (compact) or as a column of the split (regular, the tab bar beside it: no 设置).
    private func sidebar(column: Bool) -> some View {
        AssistantSidebar(current: pending == nil ? current : nil,
                         open: { key in
                             model.assistantCurrent = key
                             pending = nil
                             drawer.close()
                         },
                         newConversation: { newConversation(); drawer.close() },
                         openProfile: { drawer.close(); showProfile = true },
                         openRoutines: { drawer.close(); showRoutines = true },
                         openKnowledge: { drawer.close(); showKnowledge = true },
                         openSettings: column ? nil : { drawer.close(); model.selectedTab = .settings })
    }

    /// 历史对话: the drawer on a phone, the sidebar column beside the conversation on a wide display.
    private func toggleSidebar() {
        if widthClass == .regular {
            withAnimation(Theme.Motion.standard) { columns = columns == .detailOnly ? .all : .detailOnly }
        } else {
            drawer.open()
        }
    }

    /// Nothing pushed on the page: the sidebar's drag only works here (pushed pages have their own back swipe).
    private var atRoot: Bool {
        var pushed = showProfile || showKnowledge || showRoutines
        #if DEBUG
        pushed = pushed || showAvatars
        #endif
        return !pushed
    }

    private var stack: some View {
        NavigationStack {
            Group {
                if let key = current, pending == nil {
                    ChatView(key: key, style: .assistant)
                        .id(key)
                } else {
                    AssistantHome(pending: pending, error: startError, launcher: $starterLauncher, mode: $starterMode,
                                  effort: $starterEffort) { text, images in
                        Task { await start(text, images) }
                    }
                }
            }
            // No system bar: the top bar floats over the conversation with a fog under it (it scrolls beneath).
            .toolbarVisibility(.hidden, for: .navigationBar)
            .overlay(alignment: .top) {
                AssistantTopBar(key: pending == nil ? current : nil,
                                sidebarIsColumn: widthClass == .regular,
                                openSidebar: toggleSidebar,
                                newConversation: newConversation,
                                openKnowledge: { showKnowledge = true },
                                openProfile: { showProfile = true })
            }
            .navigationDestination(isPresented: $showProfile) { AssistantProfileView() }
            .navigationDestination(isPresented: $showKnowledge) { KnowledgeView() }
            .navigationDestination(isPresented: $showRoutines) { AssistantRoutinesView() }
            // Opening a conversation from the profile (a run, a follow-up) shows it: back to the root. So does 问 Jarvis
            // from the knowledge base, even when its conversation is the one already open.
            .onChange(of: model.assistantCurrent) { backToRoot() }
            .onChange(of: model.assistantReveal) { backToRoot(); drawer.close() }
            #if DEBUG
            // `-openAssistantPage profile|avatars|routines|runs|…` (with `-tab assistant`): the profile and its pages;
            // `-openAssistantSidebar YES`: the sidebar is out.
            .navigationDestination(isPresented: $showAvatars) { PersonaGallery() }
            .onAppear {
                switch UserDefaults.standard.string(forKey: "openAssistantPage") {
                case "avatars": showAvatars = true
                case "knowledge": showKnowledge = true
                case .some(let p) where p != "assistant": showProfile = true
                default: break
                }
                if UserDefaults.standard.bool(forKey: "openAssistantSidebar") { drawer.progress = 1 }
            }
            #endif
            .task { await model.refreshAssistant() }
        }
    }

    private func backToRoot() {
        showProfile = false
        showRoutines = false
        showKnowledge = false
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

/// Empty state: greeting, what Jarvis can reach, one-tap suggestions; while a conversation spins up it shows the
/// first message and a typing indicator so the send feels instant.
struct AssistantHome: View {
    @Environment(AppModel.self) private var model
    var pending: PendingStart?
    var error: String?
    @Binding var launcher: String?
    @Binding var mode: String
    @Binding var effort: String
    var send: (String, [ImagePayload]) -> Void

    private let suggestions: [(symbol: String, title: String, prompt: String)] = [
        ("calendar", "今天的安排", "看一下我今天的安排：iPhone 日历和飞书日历都查，合并后按时间列出来。"),
        ("envelope.badge", "要处理的邮件", "我的邮箱里最近有哪些需要我处理或回复的邮件？挑重要的说。"),
        ("macbook.and.iphone", "电脑在忙什么", "我各台电脑上的 Agent 现在都在忙什么？哪些在等我、哪些跑完了还没看？"),
        ("bubble.left.and.text.bubble.right", "飞书消息", "总结一下我今天飞书上收到的重要消息，哪些需要我回复。"),
        ("person.2.wave.2", "会前准备", "看看我今天接下来的会，挑最重要的一个帮我准备：议程、相关文档、上次的纪要和没完成的待办。"),
        ("heart.text.square", "这周的健康", "根据我 iPhone 上的健康数据，总结一下这周的步数、睡眠和运动情况。"),
    ]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.xl) {
                if let pending {
                    HStack {
                        Spacer(minLength: 48)
                        VStack(alignment: .trailing, spacing: Theme.Space.sm) {
                            if pending.imageCount > 0 {
                                Label("\(pending.imageCount) 张图片", systemImage: Theme.Icon.photo).font(Theme.Font.label).foregroundStyle(.secondary)
                            }
                            // The same bubble as a sent message (white on the light-blue bubble was 1.2:1).
                            Text(pending.text)
                                .foregroundStyle(Theme.userBubbleText)
                                .padding(.horizontal, Theme.Space.l)
                                .padding(.vertical, Theme.Space.m)
                                .background(Theme.userBubbleFill, in: .rect(cornerRadius: Theme.Radius.bubble, style: .continuous))
                        }
                    }
                    .padding(.top, Theme.Space.m)
                    TypingIndicator(text: "Jarvis 正在准备…")
                } else {
                    header
                    if let error {
                        Label(error, systemImage: Theme.Icon.error)
                            .font(Theme.Font.caption)
                            .foregroundStyle(Theme.danger)
                    }
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
                        }
                    }
                }
            }
            .padding(.horizontal, Theme.Space.xl)
            .padding(.bottom, Theme.Space.xl)
            // The conversation's measure on a wide display (iPhone Duo open).
            .frame(maxWidth: ChatView.readableWidth)
            .frame(maxWidth: .infinity)
        }
        .scrollDismissesKeyboard(.interactively)
        // The floating top bar (AssistantTopBar) is over the page: leave its room, let the page scroll under it. Before
        // the composer is added: margins reach every scroll view below them, and the chips' row above the field stood
        // the top bar's height tall, over the suggestion cards.
        .contentMargins(.top, AssistantTopBar.height, for: .scrollContent)
        .themedGroupedPage()
        .safeAreaInset(edge: .bottom, spacing: 0) {
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: Theme.Space.s) {
                        // The BeautifulUI style picks the launcher in the prompt bar's model menu.
                        if !AppStyle.current.usesBeautifulUI { StarterModelChip(launcher: $launcher) }
                        NewSessionModePicker(mode: $mode, effort: $effort, modes: SessionModes.switchableModes)
                    }
                    .padding(.horizontal, Theme.Space.xs)
                }
                if AppStyle.current.usesBeautifulUI {
                    BeautifulStarterComposer(disabled: pending != nil, launcher: $launcher, send: send)
                } else {
                    StarterComposer(disabled: pending != nil, send: send)
                }
            }
            .padding(.horizontal, Theme.Space.m)
            .padding(.bottom, Theme.Space.s)
            .frame(maxWidth: ChatView.readableWidth)
            .frame(maxWidth: .infinity)
            // At large text sizes the page reaches down here: back the chips and the field with the page, or the
            // machine chips scrolled under them read through.
            .background(Theme.groupedPage.ignoresSafeArea(edges: .bottom))
        }
        .navigationTitle("Jarvis")
        .task(id: launcher) { if pending == nil { await model.warmAssistant(launcher: launcher) } }
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
                Chip(text: "iPhone", symbol: model.live.state == .connected ? Theme.Icon.phone : "iphone.slash")
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

struct TypingIndicator: View {
    var text: String
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var phase = 0.0

    var body: some View {
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
    var send: (String, [ImagePayload]) -> Void
    @State private var text = ""
    @State private var picks: [PhotosPickerItem] = []
    @State private var images: [(UIImage, ImagePayload)] = []
    @State private var showPhotos = false
    @State private var showFiles = false
    @State private var uploads = FileUploads()
    @State private var selection: TextSelection?
    @FocusState private var focused: Bool

    private var canSend: Bool { !disabled && (!text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !images.isEmpty) }

    var body: some View {
        VStack(spacing: Theme.Space.s) {
            if !uploads.items.isEmpty { UploadsBar(uploads: uploads) }
            if !images.isEmpty { ComposerImages(images: $images) }
            ComposerRow(placeholder: "问 \(model.persona.name)…", text: $text, selection: $selection, focused: $focused, canSend: canSend,
                        send: {
                            let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
                            send(t.isEmpty ? "请看图片" : t, images.map(\.1))
                            text = ""
                            images = []
                        }) {
                // A PhotosPicker inside a Menu never presents (the menu dismisses first): present it from here.
                Button { showPhotos = true } label: { Label("照片", systemImage: Theme.Icon.photos) }
                Button { showFiles = true } label: { Label("文件", systemImage: Theme.Icon.file) }
            }
        }
        .photosPicker(isPresented: $showPhotos, selection: $picks, maxSelectionCount: 4, matching: .images)
        .fileUploadPicker(isPresented: $showFiles) { urls in
            // The assistant runs on its machine (the hub's unless set otherwise): files go to that one's temp directory.
            uploads.start(urls, machine: model.assistantConfig?.machineID ?? model.hubMachineID, model: model) { path in
                text = PathInsert.edit(path, text: text, selection: $selection, focused: $focused)
            }
        }
        .onChange(of: picks) { _, items in
            Task {
                guard !items.isEmpty else { return }
                for item in items {
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
    @State private var uploads = FileUploads()
    @State private var selection: TextSelection?
    @FocusState private var focused: Bool

    private var launcherMachine: String { model.assistantConfig?.machineID ?? model.hubMachineID }
    private var currentLauncher: String { launcher ?? model.assistantConfig?.launcher ?? "ccc" }

    /// The machine's launchers; until they load, the current one alone, so the button shows what a new
    /// conversation starts with.
    private var models: [PromptModel] {
        let list = launchers.map { PromptModel(id: $0.name, name: $0.name, tag: Fmt.model($0.model) ?? "") }
        return list.isEmpty ? [PromptModel(id: currentLauncher, name: currentLauncher, tag: "")] : list
    }

    var body: some View {
        VStack(spacing: Theme.Space.s) {
            if !quotes.isEmpty { ContextChips(refs: $quotes) }
            if !uploads.items.isEmpty { UploadsBar(uploads: uploads) }
            if !images.isEmpty { ComposerImages(images: $images) }
            PromptComposer(placeholder: "问 \(model.persona.name)…", text: $text, selection: $selection, focused: $focused,
                           hasAttachments: !images.isEmpty || !quotes.isEmpty, disabled: disabled,
                           attach: PromptItems.attachments(camera: false),
                           mentions: { PromptItems.sessions(model.allSessions, except: nil) },
                           models: models,
                           modelID: currentLauncher,
                           pickModel: { id in
                               launcher = id
                               Haptics.tap()
                           },
                           onAttach: { source in if source.id == "photo" { showPhotos = true } else { showFiles = true } },
                           onMention: { source in
                               guard let ref = PromptItems.quote(source, in: model.allSessions) else { return false }
                               quotes.append(ref)
                               return true
                           },
                           send: {
                               let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
                               let body = t.isEmpty ? (images.isEmpty ? "参考上面的内容。" : "请看图片") : t
                               // Quoted sessions ride along as <jarvis-context> tags before what was typed, as in a chat.
                               send(quotes.isEmpty ? body : quotes.map(\.xml).joined(separator: "\n") + "\n\n" + body, images.map(\.1))
                               text = ""
                               images = []
                               quotes = []
                           })
        }
        // Again once the hub is paired: on a fresh launch the first try runs before there is one.
        .task(id: launcherMachine) { launchers = (try? await model.launchers(launcherMachine)) ?? [] }
        .photosPicker(isPresented: $showPhotos, selection: $picks, maxSelectionCount: 4, matching: .images)
        .fileUploadPicker(isPresented: $showFiles) { urls in
            // The assistant runs on its machine (the hub's unless set otherwise): files go to that one's temp directory.
            uploads.start(urls, machine: model.assistantConfig?.machineID ?? model.hubMachineID, model: model) { path in
                text = PathInsert.edit(path, text: text, selection: $selection, focused: $focused)
            }
        }
        .onChange(of: picks) { _, items in
            Task {
                guard !items.isEmpty else { return }
                for item in items {
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
                    if l.name == current { Label(l.name, systemImage: Theme.Icon.done) } else { Text("\(l.name)  \(Fmt.model(l.model) ?? "")") }
                }
            }
        } label: {
            ModeChip(symbol: Theme.Icon.model, text: current, tint: .secondary) // neutral, like the mode and effort chips beside it
        }
        .task { launchers = (try? await model.launchers(model.assistantConfig?.machineID ?? model.hubMachineID)) ?? [] }
    }
}
