import PhotosUI
import SwiftUI

enum ChatStyle {
    /// 开发: every step visible, tab bar hidden.
    case dev
    /// 对话 (assistant): thoughts hidden, tool runs folded into one line, lives at the root of its tab.
    case assistant
}

struct ChatView: View {
    @Environment(AppModel.self) private var model
    /// The session on screen; the sibling strip can switch it to another agent in the same space.
    @State private var key: SessionKey
    var style: ChatStyle = .dev
    @State private var position = ScrollPosition(edge: .bottom)
    @State private var atBottom = true
    @State private var showTerminal = false
    @State private var file: FileRef?

    init(key: SessionKey, style: ChatStyle = .dev) {
        _key = State(initialValue: key)
        self.style = style
    }

    private var store: ChatStore { model.chat(key) }
    private var session: Session? { model.session(key) }
    private var status: SessionStatus { session?.status ?? .unknown }

    var body: some View {
        let store = store
        let rows = store.rows
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 14) {
                if store.hasMore {
                    Button {
                        Task { await model.loadOlder(key) }
                    } label: {
                        HStack(spacing: 6) {
                            if store.isLoadingOlder { ProgressView().controlSize(.mini) }
                            Text("加载更早的消息")
                        }
                        .font(.caption.weight(.medium))
                        .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.plain)
                    .foregroundStyle(.secondary)
                    .padding(.vertical, 8)
                }
                if !store.loadedOnce && rows.isEmpty {
                    ChatSkeleton()
                } else if rows.isEmpty {
                    EmptyStateView(symbol: "text.bubble", title: "还没有消息", message: "发一句话开始吧。")
                }
                ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                    rowView(row, isLatest: index == rows.count - 1)
                        .id(row.id)
                }
                if let err = store.error {
                    Label(err, systemImage: "exclamationmark.triangle").font(.caption).foregroundStyle(Theme.danger)
                }
            }
            .padding(.horizontal, 16)
            .padding(.top, 8)
            .padding(.bottom, 12)
        }
        .scrollPosition($position)
        .defaultScrollAnchor(.bottom)
        .scrollDismissesKeyboard(.interactively)
        .onScrollGeometryChange(for: Bool.self) { geo in
            geo.contentOffset.y + geo.containerSize.height >= geo.contentSize.height - 120
        } action: { _, near in
            atBottom = near
        }
        .onChange(of: rows.last?.id) { _, _ in
            if atBottom { withAnimation(.snappy) { position.scrollTo(edge: .bottom) } }
        }
        .overlay(alignment: .bottomTrailing) {
            if !atBottom {
                Button {
                    withAnimation(.snappy) { position.scrollTo(edge: .bottom) }
                } label: {
                    Image(systemName: "arrow.down").font(.callout.weight(.semibold)).frame(width: 40, height: 40)
                }
                .buttonStyle(.glass)
                .clipShape(.circle)
                .padding(16)
                .transition(.scale.combined(with: .opacity))
            }
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            VStack(spacing: 10) {
                ForEach(model.interactions(for: key)) { ix in InteractionPanel(interaction: ix) }
                if status.isActive && model.interactions(for: key).isEmpty {
                    LiveActivityBar(store: store, status: status, openTerminal: { showTerminal = true }) {
                        Task { await model.interrupt(key) }
                    }
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                }
                Composer(key: key, status: status, placeholder: style == .assistant ? "问 Jarvis…" : nil)
            }
            .padding(.horizontal, 12)
            .padding(.bottom, 8)
            .animation(.snappy, value: status)
            .animation(.snappy, value: model.interactions(for: key).map(\.id))
        }
        .background(Color(.systemBackground))
        .navigationTitle(title)
        .navigationSubtitle(subtitle)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Menu {
                    Button { showTerminal = true } label: { Label("终端", systemImage: "terminal") }
                    if status.isActive {
                        Button { Task { await model.interrupt(key) } } label: { Label("中断", systemImage: "stop.circle") }
                    }
                    Divider()
                    if let s = session {
                        Button { UIPasteboard.general.string = "claude --resume \(s.sessionID)" } label: {
                            Label("拷贝恢复命令", systemImage: "doc.on.doc")
                        }
                        Button { UIPasteboard.general.string = s.cwd } label: { Label("拷贝目录", systemImage: "folder") }
                        if s.startedByJarvis && s.status.isLive {
                            Button(role: .destructive) { Task { await model.close(key) } } label: {
                                Label("退出 Claude 并关闭标签页", systemImage: "xmark.circle")
                            }
                        }
                    }
                } label: {
                    Image(systemName: "ellipsis")
                }
            }
        }
        .safeAreaInset(edge: .top, spacing: 0) {
            if style == .dev { SiblingStrip(current: key) { next in switchTo(next) } }
        }
        .toolbar(style == .dev ? .hidden : .automatic, for: .tabBar)
        .sheet(isPresented: $showTerminal) { TerminalSheet(key: key) }
        .sheet(item: $file) { ref in FileViewer(ref: ref) }
        .environment(\.openFile) { path in
            file = FileRef(machineID: key.machineID, path: path, cwd: session?.cwd)
        }
        .onAppear {
            model.subscribe(key)
            model.visibleSession = key
            model.markSeen(key)
        }
        .onDisappear {
            model.unsubscribe(key)
            if model.visibleSession == key { model.visibleSession = nil }
        }
        .onChange(of: session?.state) { _, st in
            if st == .unread && model.visibleSession == key { model.markSeen(key) }
        }
    }

    private func switchTo(_ next: SessionKey) {
        guard next != key else { return }
        Haptics.tap()
        model.unsubscribe(key)
        key = next
        position = ScrollPosition(edge: .bottom)
        model.subscribe(next)
        model.visibleSession = next
        model.markSeen(next)
    }

    private var title: String {
        let t = session?.title ?? ""
        if style == .assistant { return t.isEmpty || t == "Claude Code" ? "Jarvis" : t }
        return t.isEmpty ? "对话" : t
    }

    private var subtitle: String {
        guard let s = session else { return "" }
        if style == .assistant {
            if !model.interactions(for: key).isEmpty { return "等你确认" }
            return s.status.isActive ? "正在处理…" : (Fmt.model(s.model) ?? "Jarvis")
        }
        let machine = model.machine(s.machineID)?.name ?? s.machineID
        let place = [s.spaceName, s.slotLabel].compactMap { $0 }.joined(separator: " › ")
        return [machine, place.isEmpty ? nil : place, s.state.label].compactMap { $0 }.joined(separator: " · ")
    }

    @ViewBuilder private func rowView(_ row: ChatRow, isLatest: Bool) -> some View {
        switch row {
        case .user(let i): UserBubble(item: i)
        case .assistant(let i): AssistantText(item: i)
        case .thinking(let i):
            if style == .dev { ThinkingRow(item: i) }
        case .tools(let steps):
            if style == .assistant { AssistantToolsRow(steps: steps) } else { ToolGroupView(steps: steps, isLatest: isLatest) }
        case .notice(let i): NoticeRow(item: i)
        case .image(let i):
            if let p = i.image {
                HStack { if i.role == "user" { Spacer(minLength: 60) }; Base64Image(payload: p).frame(maxWidth: 240) }
            }
        }
    }
}

struct ChatSkeleton: View {
    @State private var on = false
    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            ForEach(0..<5, id: \.self) { i in
                RoundedRectangle(cornerRadius: 8)
                    .fill(Color.primary.opacity(on ? 0.08 : 0.04))
                    .frame(width: [220, 300, 180, 260, 140][i], height: 14)
                    .frame(maxWidth: .infinity, alignment: i == 0 ? .trailing : .leading)
            }
        }
        .onAppear { withAnimation(.easeInOut(duration: 0.9).repeatForever()) { on = true } }
    }
}

/// Message input: text, photos, quick slash commands; turns into a stop button while Claude works.
struct Composer: View {
    @Environment(AppModel.self) private var model
    let key: SessionKey
    var status: SessionStatus
    var placeholder: String?
    @State private var text = ""
    @State private var picks: [PhotosPickerItem] = []
    @State private var images: [(UIImage, ImagePayload)] = []
    @State private var sending = false
    @FocusState private var focused: Bool

    private var canSend: Bool { !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !images.isEmpty }

    var body: some View {
        VStack(spacing: 8) {
            if !images.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack {
                        ForEach(Array(images.enumerated()), id: \.offset) { idx, pair in
                            Image(uiImage: pair.0).resizable().scaledToFill().frame(width: 56, height: 56)
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
                Menu {
                    PhotosPicker(selection: $picks, maxSelectionCount: 4, matching: .images) {
                        Label("照片", systemImage: "photo")
                    }
                    Section("快捷指令") {
                        ForEach(SlashCommand.all) { cmd in
                            Button { Task { await sendText(cmd.text) } } label: { Label(cmd.title, systemImage: cmd.symbol) }
                        }
                    }
                } label: {
                    Image(systemName: "plus")
                        .font(.body.weight(.semibold))
                        .frame(width: 36, height: 36)
                }
                .buttonStyle(.glass)
                .clipShape(.circle)

                TextField(status.isActive ? "补充说明（会排队发送）" : (placeholder ?? "给 Claude 发消息"), text: $text, axis: .vertical)
                    .lineLimit(1...6)
                    .focused($focused)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 9)
                    .glassEffect(.regular.interactive(), in: .rect(cornerRadius: 20, style: .continuous))

                if canSend || !status.isActive {
                    Button {
                        Task { await sendText(text) }
                    } label: {
                        Group {
                            if sending { ProgressView().tint(.white) } else { Image(systemName: "arrow.up").font(.body.weight(.bold)) }
                        }
                        .frame(width: 36, height: 36)
                    }
                    .buttonStyle(.glassProminent)
                    .clipShape(.circle)
                    .disabled(!canSend || sending)
                    .keyboardShortcut(.return, modifiers: .command)
                } else {
                    Button {
                        Task { await model.interrupt(key) }
                    } label: {
                        Image(systemName: "stop.fill").font(.body.weight(.bold)).frame(width: 36, height: 36)
                    }
                    .buttonStyle(.glassProminent)
                    .tint(Theme.danger)
                    .clipShape(.circle)
                    .accessibilityLabel("中断")
                }
            }
        }
        .onChange(of: picks) { _, items in
            Task { await load(items) }
        }
    }

    private func sendText(_ raw: String) async {
        let t = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty || !images.isEmpty else { return }
        sending = true
        let payloads = images.map(\.1)
        let ok = await model.send(key, text: t.isEmpty ? "请看图片" : t, images: payloads)
        sending = false
        if ok {
            if raw == text { text = "" }
            images = []
        }
    }

    private func load(_ items: [PhotosPickerItem]) async {
        for item in items {
            guard let data = try? await item.loadTransferable(type: Data.self), let img = UIImage(data: data) else { continue }
            let scaled = img.scaledDown(maxSide: 1600)
            if let jpeg = scaled.jpegData(compressionQuality: 0.75) {
                images.append((scaled, ImagePayload(mime: "image/jpeg", data: jpeg.base64EncodedString())))
            }
        }
        picks = []
    }
}

struct SlashCommand: Identifiable {
    var title: String
    var text: String
    var symbol: String
    var id: String { text }

    static let all: [SlashCommand] = [
        SlashCommand(title: "继续", text: "继续", symbol: "forward"),
        SlashCommand(title: "压缩上下文 /compact", text: "/compact", symbol: "rectangle.compress.vertical"),
        SlashCommand(title: "查看上下文 /context", text: "/context", symbol: "chart.pie"),
        SlashCommand(title: "提交并推送", text: "把当前改动按规范提交并推送", symbol: "arrow.up.doc"),
        SlashCommand(title: "汇报进展", text: "简要汇报一下目前的进展、还剩什么", symbol: "list.bullet.rectangle"),
    ]
}

extension UIImage {
    func scaledDown(maxSide: CGFloat) -> UIImage {
        let side = max(size.width, size.height)
        guard side > maxSide else { return self }
        let scale = maxSide / side
        let target = CGSize(width: size.width * scale, height: size.height * scale)
        return UIGraphicsImageRenderer(size: target).image { _ in draw(in: CGRect(origin: .zero, size: target)) }
    }
}

/// Live terminal mirror of the herdr pane with a key pad, for anything the chat view cannot express.
struct TerminalSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let key: SessionKey
    @State private var text = ""
    @State private var input = ""

    var body: some View {
        NavigationStack {
            VStack(spacing: 10) {
                TerminalText(text: text.isEmpty ? model.chat(key).screen : text, maxLines: 200)
                KeyPad { keys in Task { await model.keys(key, keys); await poll() } }
                HStack {
                    TextField("发送文字（不回车）", text: $input)
                        .padding(10)
                        .background(Color.primary.opacity(0.06), in: .rect(cornerRadius: 12, style: .continuous))
                    Button {
                        let t = input
                        input = ""
                        Task { await model.keys(key, [], text: t); await poll() }
                    } label: { Image(systemName: "paperplane.fill") }
                        .buttonStyle(.glassProminent)
                        .disabled(input.isEmpty)
                }
            }
            .padding()
            .navigationTitle("终端")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() } } }
            .task {
                while !Task.isCancelled {
                    await poll()
                    try? await Task.sleep(for: .seconds(1))
                }
            }
        }
        .presentationDetents([.medium, .large])
    }

    private func poll() async {
        if let s = try? await model.api?.screen(key, lines: 120) { text = s.text }
    }
}
