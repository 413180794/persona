import PhotosUI
import SwiftUI

/// The Jarvis tab: one personal assistant (a Claude session in ~/.jarvis/assistant on the hub's machine) that can reach the
/// user's computers, iPhone data, mail, company data and the knowledge base. One conversation is the main one (where the
/// user usually talks to it, and where other pages hand things to it); the others are side conversations.
struct AssistantView: View {
    @Environment(AppModel.self) private var model
    @State private var pending: PendingStart?
    @State private var showHistory = false
    @State private var startError: String?
    @State private var starterLauncher: String?
    @State private var showProfile = false
    #if DEBUG
    @State private var showAvatars = false
    #endif
    /// Mode / effort for new conversations ("" = the launcher's own), remembered across launches.
    @AppStorage("assistant.newMode") private var starterMode = ""
    @AppStorage("assistant.newEffort") private var starterEffort = ""

    private var current: SessionKey? { model.assistantCurrent }

    var body: some View {
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
            .toolbar {
                // Plain icons in the text color: the accent is for the page's main action (send), not for the bar.
                ToolbarItemGroup(placement: .topBarLeading) {
                    Button { showHistory = true } label: { Image(systemName: "clock.arrow.circlepath") }
                        .tint(.primary)
                        .accessibilityLabel("历史对话")
                    NavigationLink { KnowledgeView() } label: { Image(systemName: "books.vertical") }
                        .tint(.primary)
                        .accessibilityLabel("知识库")
                }
                ToolbarItemGroup(placement: .topBarTrailing) {
                    if let main = model.assistantMain, current != main {
                        Button {
                            withAnimation(.snappy) { model.assistantCurrent = main }
                            Haptics.tap()
                        } label: { Image(systemName: "house") }
                            .tint(.primary)
                            .accessibilityLabel("回到主对话")
                    }
                    if current != nil {
                        Button {
                            withAnimation(.snappy) { model.assistantCurrent = nil }
                            Haptics.tap()
                        } label: { Image(systemName: "square.and.pencil") }
                            .tint(.primary)
                            .accessibilityLabel("新对话")
                    }
                    Button { showProfile = true } label: { JarvisAvatar(size: 30, working: working) }
                        .accessibilityLabel("Jarvis 的资料")
                        .accessibilityIdentifier("jarvis-profile")
                }
            }
            .navigationDestination(isPresented: $showProfile) { AssistantProfileView() }
            // Opening a conversation from the profile (a run, a follow-up) shows it: back to the root.
            .onChange(of: model.assistantCurrent) { showProfile = false }
            #if DEBUG
            // `-openAssistantPage profile|avatars|routines|runs|…` (with `-tab assistant`): the profile and its pages.
            .navigationDestination(isPresented: $showAvatars) { JarvisAvatarGallery() }
            .onAppear {
                switch UserDefaults.standard.string(forKey: "openAssistantPage") {
                case "avatars": showAvatars = true
                case .some(let p) where p != "assistant": showProfile = true
                default: break
                }
            }
            #endif
            .sheet(isPresented: $showHistory) {
                AssistantHistory { key in
                    model.assistantCurrent = key
                    showHistory = false
                }
            }
            .task { await model.refreshAssistant() }
        }
    }

    /// The conversation on screen is running a turn: the avatar looks around.
    private var working: Bool {
        guard let key = current else { return false }
        return model.session(key)?.state == .working
    }

    private func start(_ text: String, _ images: [ImagePayload]) async {
        guard let api = model.api else { return }
        startError = nil
        withAnimation(.snappy) { pending = PendingStart(text: text, imageCount: images.count) }
        do {
            let s = try await api.startConversation(prompt: text, images: images, launcher: starterLauncher,
                                                    permissionMode: starterMode.isEmpty ? nil : starterMode,
                                                    effort: starterEffort.isEmpty ? nil : starterEffort,
                                                    main: model.assistantMain == nil) // the first one becomes the main conversation
            if model.assistantMain == nil { await model.refreshAssistant() }
            Haptics.success()
            withAnimation(.snappy) {
                model.assistantCurrent = s.key
                pending = nil
            }
        } catch {
            withAnimation(.snappy) {
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
            VStack(alignment: .leading, spacing: 24) {
                if let pending {
                    HStack {
                        Spacer(minLength: 48)
                        VStack(alignment: .trailing, spacing: 6) {
                            if pending.imageCount > 0 {
                                Label("\(pending.imageCount) 张图片", systemImage: "photo").font(.caption).foregroundStyle(.secondary)
                            }
                            // The same bubble as a sent message (white on the light-blue bubble was 1.2:1).
                            Text(pending.text)
                                .foregroundStyle(Theme.userBubbleText)
                                .padding(.horizontal, 14)
                                .padding(.vertical, 10)
                                .background(Theme.userBubbleFill, in: .rect(cornerRadius: Theme.Radius.bubble, style: .continuous))
                        }
                    }
                    .padding(.top, 12)
                    TypingIndicator(text: "Jarvis 正在准备…")
                } else {
                    header
                    if let error {
                        Label(error, systemImage: "exclamationmark.triangle")
                            .font(.footnote)
                            .foregroundStyle(Theme.danger)
                    }
                    LazyVGrid(columns: [GridItem(.flexible(), spacing: 12), GridItem(.flexible(), spacing: 12)], spacing: 12) {
                        ForEach(suggestions, id: \.title) { s in
                            Button {
                                Haptics.tap()
                                send(s.prompt, [])
                            } label: {
                                VStack(alignment: .leading, spacing: 10) {
                                    Image(systemName: s.symbol)
                                        .font(.title3)
                                        .foregroundStyle(Theme.accent)
                                    Text(s.title).font(.subheadline.weight(.semibold)).foregroundStyle(Color.primary)
                                }
                                .frame(maxWidth: .infinity, minHeight: 72, alignment: .topLeading)
                                .card(padding: 14)
                            }
                            .buttonStyle(.plain)
                        }
                    }
                }
            }
            .padding(.horizontal, 20)
            .padding(.bottom, 20)
        }
        .scrollDismissesKeyboard(.interactively)
        .themedGroupedPage()
        .safeAreaInset(edge: .bottom, spacing: 0) {
            VStack(alignment: .leading, spacing: 8) {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        StarterModelChip(launcher: $launcher)
                        NewSessionModePicker(mode: $mode, effort: $effort, modes: SessionModes.switchableModes)
                    }
                    .padding(.horizontal, 4)
                }
                StarterComposer(disabled: pending != nil, send: send)
            }
            .padding(.horizontal, 12)
            .padding(.bottom, 8)
        }
        .navigationTitle("Jarvis")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { ToolbarItem(placement: .principal) { ConnectionTitle(title: "Jarvis") } }
        .task(id: launcher) { if pending == nil { await model.warmAssistant(launcher: launcher) } }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 14) {
            JarvisAvatar(size: 56).padding(.top, 28)
            Text(greeting).font(Theme.Font.greeting)
            Text(model.assistantMain == nil
                 ? "这是你的主对话：我会记住这里聊过的事。我能看你的电脑、iPhone、邮箱、飞书和整理好的项目，也能替你盯着事、到点提醒。"
                 : "开一段新的旁支对话。我能看你的电脑、iPhone、邮箱、飞书和整理好的项目，也能替你盯着事、到点提醒。")
                .font(.body)
                .foregroundStyle(.secondary)
            HStack(spacing: 8) {
                ForEach(model.machines.filter(\.online)) { m in
                    Chip(text: m.name, symbol: m.symbol)
                }
                Chip(text: "iPhone", symbol: model.live.state == .connected ? "iphone" : "iphone.slash")
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
        HStack(spacing: 10) {
            HStack(spacing: 4) {
                ForEach(0..<3) { i in
                    Circle()
                        .fill(Theme.accent)
                        .frame(width: 7, height: 7)
                        .opacity(0.35 + 0.65 * max(0, sin(phase + Double(i) * 0.9)))
                }
            }
            Text(text).font(.subheadline).foregroundStyle(.secondary).contentTransition(.opacity)
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
        VStack(spacing: 8) {
            if !uploads.items.isEmpty { UploadsBar(uploads: uploads) }
            if !images.isEmpty { ComposerImages(images: $images) }
            ComposerRow(placeholder: "问 Jarvis…", text: $text, selection: $selection, focused: $focused, canSend: canSend,
                        send: {
                            let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
                            send(t.isEmpty ? "请看图片" : t, images.map(\.1))
                            text = ""
                            images = []
                        }) {
                // A PhotosPicker inside a Menu never presents (the menu dismisses first): present it from here.
                Button { showPhotos = true } label: { Label("照片", systemImage: "photo.on.rectangle") }
                Button { showFiles = true } label: { Label("文件", systemImage: "doc") }
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

/// Past assistant conversations, newest first.
struct AssistantHistory: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var open: (SessionKey) -> Void

    var body: some View {
        let list = model.assistantConversations
        NavigationStack {
            List {
                if list.isEmpty {
                    Text("还没有对话").foregroundStyle(.secondary)
                }
                ForEach(list) { s in
                    Button { open(s.key) } label: {
                        VStack(alignment: .leading, spacing: 4) {
                            HStack {
                                if s.key == model.assistantMain { Chip(text: "主对话", symbol: "house.fill") }
                                Text(s.title.isEmpty || s.title == "Claude Code" ? "新对话" : s.title)
                                    .font(.body.weight(.semibold))
                                    .foregroundStyle(Color.primary)
                                    .lineLimit(1)
                                Spacer()
                                if s.state == .working || s.state == .needsYou || s.state == .unread {
                                    StatusLabel(state: s.state)
                                } else {
                                    Text(Fmt.relative(s.updatedAt)).font(.caption).foregroundStyle(.secondary)
                                }
                            }
                            if let t = s.lastText, !t.isEmpty {
                                Text(Fmt.plain(t)).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                            }
                        }
                        .padding(.vertical, 2)
                    }
                    .swipeActions {
                        if s.status.isLive {
                            Button(role: .destructive) { Task { await model.close(s.key) } } label: { Label("结束", systemImage: "xmark") }
                        }
                    }
                }
            }
            .themedPage()
            .navigationTitle("历史对话")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() } } }
        }
        .presentationDetents([.medium, .large])
    }
}

/// Tool activity in the assistant: one quiet line ("工具 4 · 运行 3 · 读取 1"), expandable to the steps.
struct AssistantToolsRow: View {
    var steps: [ToolStep]
    @State private var open = false

    private var calls: [ToolCall] { steps.compactMap { if case .call(let c) = $0 { c } else { nil } } }

    var body: some View {
        let calls = calls
        let running = calls.last.map(\.isRunning) ?? false
        VStack(alignment: .leading, spacing: 8) {
            Button {
                withAnimation(.snappy) { open.toggle() }
            } label: {
                HStack(spacing: 6) {
                    if running {
                        ProgressView().controlSize(.mini)
                        if let last = calls.last {
                            Text("\(ToolPresentation.verb(last.name)) \(ToolPresentation.subject(last))").lineLimit(1)
                        }
                    } else {
                        Image(systemName: "wrench.and.screwdriver")
                        Text("工具 \(calls.count)")
                        Text(sources(calls)).foregroundStyle(.tertiary).lineLimit(1)
                    }
                    Spacer(minLength: 0)
                    Image(systemName: "chevron.right").font(.caption2.weight(.semibold)).rotationEffect(.degrees(open ? 90 : 0))
                }
                .font(.caption)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 12)
                .padding(.vertical, 7)
                .background(Theme.Fill.well, in: .capsule)
            }
            .buttonStyle(.plain)
            if open {
                ForEach(calls) { c in ToolCallRow(call: c, autoExpand: false) }
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

/// The assistant chat's title: the conversation's name over "launcher · model ⌄", and the menu behind it — which
/// launcher the next message uses (it used to be a monospaced "claude" chip beside ⋯, repeating the subtitle and
/// squeezing the title to four characters) and the session info card (what tapping the title did before).
struct AssistantTitleMenu: View {
    @Environment(AppModel.self) private var model
    let key: SessionKey
    var title: String
    var subtitle: String
    var infoShown: Bool
    var toggleInfo: () -> Void
    @State private var launchers: [Launcher] = []

    var body: some View {
        let current = model.assistantLaunchers[key] ?? model.session(key)?.launcher ?? model.assistantConfig?.launcher ?? "ccc"
        Menu {
            Section("下一条消息起使用") {
                ForEach(launchers) { l in
                    Button {
                        model.assistantLaunchers[key] = l.name
                        Haptics.tap()
                        model.toast = Toast("已切换到 \(l.name)，下一条消息生效", style: .success)
                    } label: {
                        if l.name == current { Label(name(l), systemImage: "checkmark") } else { Text(name(l)) }
                    }
                }
            }
            Button(action: toggleInfo) {
                Label(infoShown ? "收起会话信息" : "会话信息", systemImage: "info.circle")
            }
        } label: {
            VStack(spacing: 1) {
                Text(title).font(.headline).foregroundStyle(.primary).lineLimit(1)
                HStack(spacing: 3) {
                    Text(subtitle).lineLimit(1)
                    Image(systemName: "chevron.down").font(.caption2.weight(.bold)).imageScale(.small)
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            }
            .frame(maxWidth: 260)
            .contentShape(.rect)
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .accessibilityLabel("\(title)，\(subtitle)")
        .accessibilityHint("切换启动器，或查看会话信息")
        .task { launchers = (try? await model.launchers(model.assistantConfig?.machineID ?? key.machineID)) ?? [] }
    }

    private func name(_ l: Launcher) -> String {
        [l.name, Fmt.model(l.model)].compactMap { $0 }.joined(separator: " · ")
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
                    if l.name == current { Label(l.name, systemImage: "checkmark") } else { Text("\(l.name)  \(Fmt.model(l.model) ?? "")") }
                }
            }
        } label: {
            ModeChip(symbol: "cpu", text: current, tint: .secondary) // neutral, like the mode and effort chips beside it
        }
        .task { launchers = (try? await model.launchers(model.assistantConfig?.machineID ?? model.hubMachineID)) ?? [] }
    }
}
