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
    @State private var debugImages: [ImagePayload]?
    @State private var showInfo = false

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
                // Finished turns read like the assistant: thoughts hidden, tool runs folded into one line.
                // Only the turn still running (after the last user message) shows every step.
                let liveFrom = status.isActive ? (rows.lastIndex { if case .user = $0 { true } else { false } } ?? -1) : rows.count
                ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                    rowView(row, isLatest: index == rows.count - 1, live: index > liveFrom)
                        .id(row.id)
                }
                if style == .assistant {
                    let live = store.liveText
                    let lastText: String? = { if case .assistant(let i) = rows.last { return i.text } else { return nil } }()
                    if !live.isEmpty && !(lastText.map { $0.hasPrefix(live) || live == $0 } ?? false) {
                        StreamingText(text: live).id("live")
                    } else if status == .working && live.isEmpty {
                        TypingIndicator(text: "Jarvis 正在思考…").id("typing")
                    }
                }
                // Bottom sentinel: while it is on screen we are at the end and the "jump to bottom" arrow hides
                // (geometry math missed the composer / queue insets and kept the arrow up at the very end).
                Color.clear
                    .frame(height: 1)
                    .onScrollVisibilityChange(threshold: 0.01) { visible in atBottom = visible }
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

        .onChange(of: rows.last?.id) { _, _ in
            if atBottom { withAnimation(.snappy) { position.scrollTo(edge: .bottom) } }
        }
        .onChange(of: store.liveText.count / 40) { _, _ in
            if atBottom { position.scrollTo(edge: .bottom) }
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
                if style == .dev && status.isActive && model.interactions(for: key).isEmpty {
                    LiveActivityBar(store: store, status: status, openTerminal: { showTerminal = true }) {
                        Task { await model.interrupt(key) }
                    }
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                }
                if let queued = session?.queued, !queued.isEmpty, style == .dev {
                    QueueStrip(key: key, queued: queued)
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
        .navigationBarTitleDisplayMode(.inline)
        .overlay(alignment: .top) {
            if showInfo {
                DevInfoCard(key: key, session: session)
                    .padding(.horizontal, 12)
                    .padding(.top, 6)
                    .transition(.move(edge: .top).combined(with: .opacity))
            }
        }
        .toolbar {
            // A custom title: tapping it toggles the developer info card (instead of the system scroll-to-top).
            ToolbarItem(placement: .principal) {
                Button {
                    Haptics.tap()
                    withAnimation(.snappy) { showInfo.toggle() }
                } label: {
                    VStack(spacing: 1) {
                        Text(title).font(.headline).lineLimit(1)
                        HStack(spacing: 3) {
                            Text(subtitle).lineLimit(1)
                            Image(systemName: "chevron.down")
                                .font(.system(size: 8, weight: .bold))
                                .rotationEffect(.degrees(showInfo ? 180 : 0))
                        }
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: 260)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("\(title)，\(showInfo ? "收起" : "查看")会话信息")
            }
            if style == .assistant {
                ToolbarItem(placement: .topBarTrailing) { AssistantModelMenu(key: key) }
            }
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
        .fullScreenCover(isPresented: $showTerminal) { TerminalSheet(key: key) }
        .sheet(item: $file) { ref in FileViewer(ref: ref) }
        #if DEBUG
        // Simulator automation: `-openFirstImage YES` opens the viewer on the first image once it loads.
        .onChange(of: store.rows.count) { _, _ in
            guard UserDefaults.standard.bool(forKey: "openFirstImage"), debugImages == nil else { return }
            let imgs = store.items.compactMap(\.image)
            if !imgs.isEmpty { debugImages = imgs }
        }
        .fullScreenCover(isPresented: Binding(get: { debugImages != nil }, set: { if !$0 { debugImages = [] } })) {
            if let imgs = debugImages, !imgs.isEmpty { ImageViewer(images: imgs, index: 0) }
        }
        #endif
        .environment(\.openFile) { path in
            file = FileRef(machineID: key.machineID, path: path, cwd: session?.cwd)
        }
        .onAppear {
            model.subscribe(key)
            model.visibleSession = key
            model.markSeen(key)
            if style == .dev { AppModel.rememberChat(key) }
            #if DEBUG
            if UserDefaults.standard.bool(forKey: "openTerminal") { showTerminal = true }
            if UserDefaults.standard.bool(forKey: "showInfo") { showInfo = true }
            #endif
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
        if style == .dev { AppModel.rememberChat(next) }
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
            let launcher = model.assistantLaunchers[key] ?? s.launcher
            return s.status.isActive ? "正在处理…" : ([launcher, Fmt.model(s.model)].compactMap { $0 }.joined(separator: " · "))
        }
        let machine = model.machine(s.machineID)?.name ?? s.machineID
        let place = [s.spaceName, s.slotLabel].compactMap { $0 }.joined(separator: " › ")
        return [machine, place.isEmpty ? nil : place, s.state.label].compactMap { $0 }.joined(separator: " · ")
    }

    @ViewBuilder private func rowView(_ row: ChatRow, isLatest: Bool, live: Bool) -> some View {
        switch row {
        case .user(let i): UserBubble(item: i)
        case .assistant(let i): AssistantText(item: i)
        case .thinking(let i):
            if style == .dev && live { ThinkingRow(item: i) }
        case .tools(let steps):
            if style == .dev && live { ToolGroupView(steps: steps, isLatest: isLatest) } else { AssistantToolsRow(steps: steps) }
        case .notice(let i): NoticeRow(item: i)
        case .recap(let i): RecapCard(item: i)
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
    private var text: String {
        get { model.drafts[key] ?? "" }
        nonmutating set { model.drafts[key] = newValue }
    }
    private var textBinding: Binding<String> { Binding(get: { text }, set: { text = $0 }) }
    @State private var picks: [PhotosPickerItem] = []
    @State private var showPhotos = false
    @State private var showCamera = false
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
                    // A PhotosPicker inside a Menu never presents (the menu dismisses first): present it from here.
                    Button { showPhotos = true } label: { Label("照片", systemImage: "photo.on.rectangle") }
                    if UIImagePickerController.isSourceTypeAvailable(.camera) {
                        Button { showCamera = true } label: { Label("拍照", systemImage: "camera") }
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

                TextField(status.isActive ? "补充说明（会排队发送）" : (placeholder ?? "给 Claude 发消息"), text: textBinding, axis: .vertical)
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
        .photosPicker(isPresented: $showPhotos, selection: $picks, maxSelectionCount: 4, matching: .images)
        .fullScreenCover(isPresented: $showCamera) {
            CameraPicker { img in add(img) }.ignoresSafeArea()
        }
        .onChange(of: picks) { _, items in
            guard !items.isEmpty else { return }
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
        var failed = 0
        for item in items {
            // iCloud-only photos are downloaded here; HEIC and others are re-encoded to JPEG below.
            guard let data = try? await item.loadTransferable(type: Data.self), let img = UIImage(data: data) else {
                failed += 1
                continue
            }
            add(img)
        }
        picks = []
        if failed > 0 {
            model.toast = Toast("有 \(failed) 张照片读不出来（可能还在 iCloud 里没下载完）", style: .error)
        }
    }

    private func add(_ img: UIImage) {
        guard images.count < 4 else {
            model.toast = Toast("一次最多 4 张", style: .error)
            return
        }
        let scaled = img.scaledDown(maxSide: 1600)
        if let jpeg = scaled.jpegData(compressionQuality: 0.75) {
            images.append((scaled, ImagePayload(mime: "image/jpeg", data: jpeg.base64EncodedString())))
            Haptics.tap()
        }
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

/// Full-screen live terminal of the herdr pane, for anything the chat cannot express.
struct TerminalSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let key: SessionKey
    @State private var text = ""
    @State private var input = ""

    var body: some View {
        NavigationStack {
            VStack(spacing: 10) {
                TerminalText(text: text.isEmpty ? model.chat(key).screen : text, maxLines: 400)
                TerminalKeyBar { keys in Task { await model.keys(key, keys); await poll() } }
                HStack(spacing: 8) {
                    TextField("发送文字到终端", text: $input)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .padding(10)
                        .background(Color.primary.opacity(0.06), in: .rect(cornerRadius: 12, style: .continuous))
                        .onSubmit(sendInput)
                    Button(action: sendInput) {
                        Image(systemName: "return").frame(width: 24, height: 24)
                    }
                    .buttonStyle(.glassProminent)
                    .disabled(input.isEmpty)
                    .accessibilityLabel("发送并回车")
                }
            }
            .padding(.horizontal, 12)
            .padding(.bottom, 8)
            .navigationTitle(model.session(key)?.slotLabel.map { "终端 · \($0)" } ?? "终端")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() } } }
            .task {
                while !Task.isCancelled {
                    await poll()
                    try? await Task.sleep(for: .seconds(1))
                }
            }
        }
    }

    private func sendInput() {
        let t = input
        guard !t.isEmpty else { return }
        input = ""
        Task { await model.keys(key, ["enter"], text: t); await poll() }
    }

    private func poll() async {
        if let s = try? await model.api?.screen(key, lines: 200) { text = s.text }
    }
}

/// Input typed while Claude works and not yet taken, shown above the composer instead of in the chat.
/// Editing pulls it back (like ↑ in the terminal); the other queued messages are re-queued in order.
struct QueueStrip: View {
    @Environment(AppModel.self) private var model
    let key: SessionKey
    var queued: [String]
    @State private var busy = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: "tray.and.arrow.down")
                Text("排队中 \(queued.count) 条 · Claude 忙完会依次处理").lineLimit(1)
                Spacer(minLength: 0)
                if busy { ProgressView().controlSize(.mini) }
            }
            .font(.caption.weight(.semibold))
            .foregroundStyle(.secondary)
            ForEach(Array(queued.enumerated()), id: \.offset) { i, q in
                Menu {
                    Button { act(i, delete: false) } label: { Label("撤回编辑", systemImage: "pencil") }
                    Button { UIPasteboard.general.string = q } label: { Label("拷贝", systemImage: "doc.on.doc") }
                    Button(role: .destructive) { act(i, delete: true) } label: { Label("删除", systemImage: "trash") }
                } label: {
                    HStack(alignment: .top, spacing: 8) {
                        Text("\(i + 1)")
                            .font(.caption2.weight(.bold).monospacedDigit())
                            .foregroundStyle(Theme.accent)
                            .frame(width: 18, height: 18)
                            .background(Theme.accent.opacity(0.14), in: .circle)
                        Text(q).font(.subheadline).foregroundStyle(Color.primary).lineLimit(2).multilineTextAlignment(.leading)
                        Spacer(minLength: 0)
                        Image(systemName: "ellipsis").font(.caption).foregroundStyle(.secondary).padding(.top, 3)
                    }
                    .padding(.horizontal, 10)
                    .padding(.vertical, 7)
                    .background(Color.primary.opacity(0.05), in: .rect(cornerRadius: 12, style: .continuous))
                }
                .disabled(busy)
            }
        }
        .padding(10)
        .glassEffect(.regular, in: .rect(cornerRadius: 18, style: .continuous))
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }

    private func act(_ i: Int, delete: Bool) {
        busy = true
        Task {
            await model.editQueued(key, index: i, delete: delete)
            busy = false
        }
    }
}

/// System camera for "拍照" in the composer.
struct CameraPicker: UIViewControllerRepresentable {
    var onImage: (UIImage) -> Void
    @Environment(\.dismiss) private var dismiss

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeUIViewController(context: Context) -> UIImagePickerController {
        let vc = UIImagePickerController()
        vc.sourceType = .camera
        vc.delegate = context.coordinator
        return vc
    }

    func updateUIViewController(_ vc: UIImagePickerController, context: Context) {}

    final class Coordinator: NSObject, UIImagePickerControllerDelegate, UINavigationControllerDelegate {
        let parent: CameraPicker
        init(_ parent: CameraPicker) { self.parent = parent }

        func imagePickerController(_ picker: UIImagePickerController, didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]) {
            if let img = info[.originalImage] as? UIImage { parent.onImage(img) }
            parent.dismiss()
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) { parent.dismiss() }
    }
}

/// Developer facts about a session, shown under the title: model, context in use, git branch, Claude Code version…
struct DevInfoCard: View {
    @Environment(AppModel.self) private var model
    let key: SessionKey
    var session: Session?
    @State private var info: APIClient.SessionInfo?
    @State private var error: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            if let info {
                Grid(alignment: .leadingFirstTextBaseline, horizontalSpacing: 12, verticalSpacing: 8) {
                    row("cpu", "模型", [Fmt.model(info.model) ?? info.model, info.launcher].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                    row("gauge.with.dots.needle.33percent", "上下文", Self.tokens(info.contextTokens) + " tokens" + (info.compactions > 0 ? " · 压缩过 \(info.compactions) 次" : ""))
                    row("arrow.triangle.branch", "分支", info.gitBranch ?? "—")
                    row("folder", "目录", Fmt.path(info.cwd ?? session?.cwd ?? "", home: model.machine(key.machineID)?.home))
                    row("macbook", "位置", [model.machine(key.machineID)?.name, session?.spaceName, session?.slotLabel].compactMap { $0 }.joined(separator: " › "))
                    row("text.bubble", "对话", "\(info.turns) 轮 · 共生成 \(Self.tokens(info.outputTokens)) tokens")
                    row("lock.open", "权限", info.permissionMode ?? "—")
                    row("shippingbox", "Claude Code", info.version ?? "—")
                    if let started = info.startedAt, started.isMeaningful {
                        row("clock", "开始", started.formatted(date: .abbreviated, time: .shortened))
                    }
                }
                HStack(spacing: 8) {
                    Button {
                        UIPasteboard.general.string = info.sessionID
                        Haptics.success()
                        model.toast = Toast("已拷贝 session id", style: .success)
                    } label: { Label("拷贝 ID", systemImage: "number").lineLimit(1) }
                    Button {
                        UIPasteboard.general.string = "cd \(info.cwd ?? "") && claude --resume \(info.sessionID)"
                        Haptics.success()
                        model.toast = Toast("已拷贝恢复命令", style: .success)
                    } label: { Label("拷贝恢复命令", systemImage: "terminal").lineLimit(1) }
                }
                .buttonStyle(.glass)
                .controlSize(.small)
            } else if let error {
                Label(error, systemImage: "exclamationmark.triangle").font(.footnote).foregroundStyle(Theme.danger)
            } else {
                ProgressView().frame(maxWidth: .infinity)
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .glassEffect(.regular, in: .rect(cornerRadius: 20, style: .continuous))
        .task(id: key) {
            do { info = try await model.api?.sessionInfo(key) } catch { self.error = error.localizedDescription }
        }
    }

    private func row(_ symbol: String, _ label: String, _ value: String) -> some View {
        GridRow {
            Label(label, systemImage: symbol)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .gridColumnAlignment(.leading)
            Text(value.isEmpty ? "—" : value)
                .font(.caption.monospaced())
                .lineLimit(2)
                .truncationMode(.middle)
                .textSelection(.enabled)
        }
    }

    static func tokens(_ n: Int) -> String {
        switch n {
        case 1_000_000...: String(format: "%.2fM", Double(n) / 1_000_000)
        case 1_000...: String(format: "%.1fk", Double(n) / 1_000)
        default: "\(n)"
        }
    }
}
