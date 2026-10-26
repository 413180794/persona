import PhotosUI
import SwiftUI

/// The 对话 tab: one personal assistant (a Claude session in ~/.jarvis/assistant on example-machine) that can reach the
/// user's computers, iPhone data, mail and company data. Conversations are separate sessions; the newest is resumed.
struct AssistantView: View {
    @Environment(AppModel.self) private var model
    @State private var pending: PendingStart?
    @State private var showHistory = false
    @State private var startError: String?
    @State private var starterLauncher: String?

    private var current: SessionKey? { model.assistantCurrent }

    var body: some View {
        NavigationStack {
            Group {
                if let key = current, pending == nil {
                    ChatView(key: key, style: .assistant)
                        .id(key)
                } else {
                    AssistantHome(pending: pending, error: startError, launcher: $starterLauncher) { text, images in
                        Task { await start(text, images) }
                    }
                }
            }
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button { showHistory = true } label: { Image(systemName: "clock.arrow.circlepath") }
                        .accessibilityLabel("历史对话")
                }
                ToolbarItem(placement: .topBarTrailing) {
                    if current != nil {
                        Button {
                            withAnimation(.snappy) { model.assistantCurrent = nil }
                            Haptics.tap()
                        } label: { Image(systemName: "square.and.pencil") }
                            .accessibilityLabel("新对话")
                    }
                }
            }
            .sheet(isPresented: $showHistory) {
                AssistantHistory { key in
                    model.assistantCurrent = key
                    showHistory = false
                }
            }
            .task { await model.refreshAssistant() }
        }
    }

    private func start(_ text: String, _ images: [ImagePayload]) async {
        guard let api = model.api else { return }
        startError = nil
        withAnimation(.snappy) { pending = PendingStart(text: text, imageCount: images.count) }
        do {
            let s = try await api.startConversation(prompt: text, images: images, launcher: starterLauncher)
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
    var send: (String, [ImagePayload]) -> Void

    private let suggestions: [(symbol: String, title: String, prompt: String)] = [
        ("calendar", "今天的安排", "看一下我今天的安排：iPhone 日历和飞书日历都查，合并后按时间列出来。"),
        ("envelope.badge", "要处理的邮件", "我的邮箱里最近有哪些需要我处理或回复的邮件？挑重要的说。"),
        ("macbook.and.iphone", "电脑在忙什么", "我的各台电脑上，herdr 里的 Claude 现在都在忙什么？有没有在等我处理的？"),
        ("bubble.left.and.text.bubble.right", "飞书消息", "总结一下我今天飞书上收到的重要消息，哪些需要我回复。"),
        ("chevron.left.forwardslash.chevron.right", "没提交的代码", "检查一下公司 Mac 上 ~/code 里有哪些仓库有没提交或没推送的改动。"),
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
                            Text(pending.text)
                                .foregroundStyle(.white)
                                .padding(.horizontal, 14)
                                .padding(.vertical, 10)
                                .background(Theme.userBubble.gradient, in: .rect(cornerRadius: 20, style: .continuous))
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
                                .card(padding: 14, radius: 18)
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
        .background(Color(.systemGroupedBackground))
        .safeAreaInset(edge: .bottom, spacing: 0) {
            VStack(alignment: .leading, spacing: 8) {
                StarterModelChip(launcher: $launcher).padding(.leading, 4)
                StarterComposer(disabled: pending != nil, send: send)
            }
            .padding(.horizontal, 12)
            .padding(.bottom, 8)
        }
        .navigationTitle("Jarvis")
        .navigationBarTitleDisplayMode(.inline)
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 14) {
            AppGlyph(size: 56).padding(.top, 28)
            Text(greeting).font(.system(size: 32, weight: .bold, design: .rounded))
            Text("我能看你的电脑、iPhone、邮箱和飞书。问我任何事。")
                .font(.body)
                .foregroundStyle(.secondary)
            HStack(spacing: 8) {
                ForEach(model.machines.filter(\.online)) { m in
                    Chip(text: m.name, symbol: m.symbol, tint: Theme.idle)
                }
                Chip(text: "iPhone", symbol: "iphone", tint: model.live.state == .connected ? Theme.idle : .secondary)
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
            withAnimation(.linear(duration: 1.2).repeatForever(autoreverses: false)) { phase = .pi * 2 }
        }
    }
}

/// Composer for a conversation that does not exist yet.
struct StarterComposer: View {
    var disabled: Bool
    var send: (String, [ImagePayload]) -> Void
    @State private var text = ""
    @State private var picks: [PhotosPickerItem] = []
    @State private var images: [(UIImage, ImagePayload)] = []

    private var canSend: Bool { !disabled && (!text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !images.isEmpty) }

    var body: some View {
        VStack(spacing: 8) {
            if !images.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack {
                        ForEach(Array(images.enumerated()), id: \.offset) { idx, pair in
                            Image(uiImage: pair.0).resizable().scaledToFill().frame(width: 52, height: 52)
                                .clipShape(.rect(cornerRadius: 12, style: .continuous))
                                .overlay(alignment: .topTrailing) {
                                    Button { images.remove(at: idx) } label: {
                                        Image(systemName: "xmark.circle.fill").symbolRenderingMode(.palette)
                                            .foregroundStyle(.white, .black.opacity(0.6))
                                    }
                                    .offset(x: 5, y: -5)
                                }
                        }
                    }
                    .padding(.top, 6)
                }
            }
            HStack(alignment: .bottom, spacing: 8) {
                PhotosPicker(selection: $picks, maxSelectionCount: 4, matching: .images) {
                    Image(systemName: "photo").font(.body.weight(.semibold)).frame(width: 36, height: 36)
                }
                .themedButton()
                .clipShape(.circle)
                TextField("问 Jarvis…", text: $text, axis: .vertical)
                    .lineLimit(1...6)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 9)
                    .surface(interactive: true, in: .rect(cornerRadius: 20, style: .continuous))
                Button {
                    let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
                    send(t.isEmpty ? "请看图片" : t, images.map(\.1))
                    text = ""
                    images = []
                } label: {
                    Image(systemName: "arrow.up").font(.body.weight(.bold)).frame(width: 36, height: 36)
                }
                .themedButton(prominent: true)
                .clipShape(.circle)
                .disabled(!canSend)
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
                .background(Color.primary.opacity(0.04), in: .capsule)
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

/// Switch the model (launcher) of an assistant conversation: the next message restarts claude with --resume.
struct AssistantModelMenu: View {
    @Environment(AppModel.self) private var model
    let key: SessionKey
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
                        if l.name == current { Label(title(l), systemImage: "checkmark") } else { Text(title(l)) }
                    }
                }
            }
        } label: {
            HStack(spacing: 3) {
                Image(systemName: "cpu")
                Text(current).font(.caption.monospaced()).lineLimit(1)
            }
        }
        .task { launchers = (try? await model.launchers(model.assistantConfig?.machineID ?? key.machineID)) ?? [] }
    }

    private func title(_ l: Launcher) -> String {
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
            Chip(text: current, symbol: "cpu", tint: Theme.accent)
        }
        .task { launchers = (try? await model.launchers(model.assistantConfig?.machineID ?? "example-machine")) ?? [] }
    }
}
