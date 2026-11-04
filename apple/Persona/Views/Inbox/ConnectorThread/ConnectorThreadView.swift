import SwiftUI

// A chat app's conversation opened from the inbox (Telegram's), laid out like the Feishu chat page: others on the
// left with their avatar and (in a group) name on the first of a run, mine on the right; a time between messages
// after a pause or on a new day; pictures, stickers, video thumbnails, voice and files; a reply's quote leading back
// to what it answers. Older pages load as the top comes into view. With a reply action, a box at the bottom: the hub
// asks the user on the phone before anything is sent (connector_gate).

struct ConnectorThreadView: View {
    var model: ConnectorThreadModel
    @State private var position = ScrollPosition(idType: String.self)
    @State private var flash: QuoteFlashMark?
    @State private var replyTo: ThreadMessage?
    @State private var text = ""
    @State private var selection: TextSelection?
    @State private var olderFailed = false
    @State private var notice: String?
    @FocusState private var focused: Bool

    private var isGroup: Bool { model.chat?.isGroup ?? false }

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                top
                ForEach(Array(model.messages.enumerated()), id: \.element.id) { i, m in
                    let prev = i > 0 ? model.messages[i - 1] : nil
                    if ThreadLayout.separates(m, after: prev) { LarkTimeSeparator(date: m.at) }
                    let grouped = ThreadLayout.continues(m, after: prev)
                    ConnectorThreadRow(connector: model.connector, message: m, grouped: grouped, isGroup: isGroup,
                                       canReply: model.chat?.reply != nil, reply: { answer($0) }, jump: { await jump(to: $0) })
                        .padding(.top, grouped ? LarkChatLayout.groupedGap : Theme.Space.s)
                        .overlay {
                            if flash?.line == m.id { QuoteFlash().id(flash?.id) }
                        }
                        .id(m.id)
                }
            }
            .scrollTargetLayout()
            .padding(.horizontal, Theme.Space.page)
            .padding(.bottom, Theme.Space.m)
        }
        .scrollPosition($position)
        .defaultScrollAnchor(.bottom)
        .scrollDismissesKeyboard(.interactively)
        .overlay {
            if !model.loaded {
                ProgressView()
            } else if model.messages.isEmpty && model.failure == nil {
                EmptyStateView(symbol: Theme.Icon.messagesApp, title: "没有消息", message: "这个会话里还没有能显示的消息。")
            }
        }
        .safeAreaInset(edge: .top) {
            if let line = notice ?? model.failure {
                Label(line, systemImage: Theme.Icon.error)
                    .font(Theme.Font.label)
                    .foregroundStyle(.secondary)
                    .lineLimit(3)
                    .padding(.horizontal, Theme.Space.m)
                    .padding(.vertical, Theme.Space.s)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .surface(in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
                    .padding(.horizontal, Theme.Space.page)
                    .padding(.top, Theme.Space.xs)
                    .onTapGesture { notice = nil }
                    .accessibilityIdentifier("thread.notice")
            }
        }
        .safeAreaInset(edge: .bottom) {
            if model.chat?.reply != nil { composer }
        }
        .task {
            if !model.loaded { await model.load() }
        }
    }

    /// Above the oldest message: the next page loading as it comes into view, or (it failed) a button to try again.
    @ViewBuilder private var top: some View {
        if model.hasOlder {
            Group {
                if olderFailed {
                    Button { Task { await older() } } label: {
                        Label("加载更早的消息", systemImage: Theme.Icon.retry).font(Theme.Font.label).lineLimit(1)
                    }
                    .buttonStyle(.borderless)
                } else {
                    ProgressView().onAppear { Task { await older() } }
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, Theme.Space.m)
            .accessibilityIdentifier("thread.older")
        } else if model.loaded, !model.messages.isEmpty {
            Text("没有更早的消息了").font(Theme.Font.caption).foregroundStyle(.tertiary)
                .frame(maxWidth: .infinity).padding(.vertical, Theme.Space.m)
        }
    }

    /// The page before, keeping the message that was at the top where it was.
    private func older() async {
        let first = model.messages.first?.id
        let ok = await model.loadOlder()
        olderFailed = !ok && model.hasOlder
        if ok, let first {
            try? await Task.sleep(for: .milliseconds(30))
            position.scrollTo(id: first, anchor: .top)
        }
    }

    /// Back to what a quote answers: the message shown, highlighted; read back a few pages for one not loaded yet.
    private func jump(to id: String) async -> Bool {
        guard await model.reach(id) else {
            notice = "原消息太早了，往上翻几页再找"
            return false
        }
        withAnimation(Theme.Motion.standard) { position.scrollTo(id: id, anchor: .center) }
        try? await Task.sleep(for: .milliseconds(250))
        flash = QuoteFlashMark(line: id)
        return true
    }

    private func answer(_ m: ThreadMessage) {
        replyTo = m
        focused = true
    }

    private var composer: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            HStack(alignment: .bottom, spacing: Theme.Space.s) {
                VStack(alignment: .leading, spacing: 0) {
                    if let r = replyTo {
                        QuoteBarRow(heading: "回复 \(r.isMine ? "自己" : r.sender.name)", summary: r.summary, lines: 1, tint: LarkColor.blue,
                                    removeLabel: "取消回复") { replyTo = nil }
                            .padding(.leading, Theme.Space.l)
                            .padding(.trailing, Theme.Space.xs)
                            .padding(.top, Theme.Space.s)
                    }
                    TextField("回复 \(model.chat?.name ?? "")", text: $text, selection: $selection, axis: .vertical)
                        .lineLimit(1...5)
                        .focused($focused)
                        .disabled(model.sending)
                        .accessibilityIdentifier("thread.box")
                        .padding(.horizontal, Theme.Space.l)
                        .padding(.vertical, Theme.Space.s)
                }
                .surface(interactive: true, in: .rect(cornerRadius: Theme.Radius.bubble, style: .continuous))
                if model.sending {
                    ProgressView().scaledFrame(width: Theme.Size.wellL, height: Theme.Size.wellL)
                } else if LarkComposer.canSend(text) {
                    Button(action: send) {
                        Image(systemName: Theme.Icon.send).font(Theme.Font.heading)
                            .scaledFrame(width: Theme.Size.wellL, height: Theme.Size.wellL)
                    }
                    .themedButton(prominent: true)
                    .clipShape(.circle)
                    .accessibilityLabel("发送")
                    .accessibilityIdentifier("thread.send")
                    .transition(.scale.combined(with: .opacity))
                }
            }
            if model.sending {
                Text("在手机上弹出的确认卡片里点「允许」后发出").font(Theme.Font.caption).foregroundStyle(.secondary)
                    .padding(.horizontal, Theme.Space.l)
            }
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.bottom, Theme.Space.s)
        .animation(Theme.Motion.standard, value: replyTo)
        .animation(Theme.Motion.standard, value: model.sending)
    }

    private func send() {
        let words = text.trimmingCharacters(in: .whitespacesAndNewlines)
        let quote = replyTo
        Task {
            if await model.reply(words, to: quote) {
                text = ""
                replyTo = nil
                Haptics.success()
                if let last = model.messages.last?.id { withAnimation(Theme.Motion.standard) { position.scrollTo(id: last, anchor: .bottom) } }
            } else {
                Haptics.attention()
            }
        }
    }
}

/// One message: an event centred; others' with the avatar (first of a run) and, in a group, the name; mine on the
/// right without either. A picture, sticker or video alone has no bubble.
struct ConnectorThreadRow: View {
    var connector: String
    var message: ThreadMessage
    var grouped: Bool
    var isGroup: Bool
    var canReply: Bool
    var reply: (ThreadMessage) -> Void
    var jump: (String) async -> Bool
    @Environment(\.openURL) private var openURL

    private var m: ThreadMessage { message }

    var body: some View {
        if m.isEvent {
            Text(m.text ?? "").font(Theme.Font.label).foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .frame(maxWidth: .infinity)
                .padding(.horizontal, Theme.Space.xl)
                .padding(.vertical, Theme.Space.xs)
        } else {
            let mine = m.isMine
            HStack(alignment: .top, spacing: Theme.Space.s) {
                Group {
                    if !mine && !grouped {
                        ThreadAvatar(connector: connector, name: m.sender.name, ref: m.sender.avatar)
                    } else {
                        Color.clear
                    }
                }
                .scaledFrame(width: Theme.Size.wellM, height: !mine && !grouped ? Theme.Size.wellM : Theme.Size.line)
                VStack(alignment: mine ? .trailing : .leading, spacing: Theme.Space.xs) {
                    if isGroup, !mine, !grouped {
                        Text(m.sender.name).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                    }
                    content.contextMenu { menu }
                    if m.edited == true {
                        Text("已编辑").font(Theme.Font.caption).foregroundStyle(.tertiary)
                    }
                }
                .frame(maxWidth: .infinity, alignment: mine ? .trailing : .leading)
                if !mine { Color.clear.scaledFrame(width: Theme.Size.wellM, height: Theme.Size.line) }
            }
            .accessibilityElement(children: .combine)
        }
    }

    private var media: ThreadMedia? { m.media?.first }
    private var words: String { m.text?.trimmingCharacters(in: .whitespacesAndNewlines) ?? "" }

    @ViewBuilder private var content: some View {
        if let media, ["sticker", "image", "video"].contains(media.kind), media.picture != nil, words.isEmpty, m.replyTo == nil {
            ThreadMediaView(connector: connector, media: media) // a picture alone: no bubble around it
        } else {
            VStack(alignment: .leading, spacing: Theme.Space.sm) {
                if let q = m.replyTo {
                    QuoteCard(heading: q.from ?? "消息", summary: q.text.flatMap { $0.isEmpty ? nil : $0 } ?? "（原消息）", tint: LarkColor.blue,
                              ink: .primary) {
                        Task {
                            if await jump(q.id) { Haptics.tap() } else { Haptics.attention() }
                        }
                    }
                }
                if let media { ThreadMediaView(connector: connector, media: media) }
                if !words.isEmpty {
                    Text(words).font(Theme.Font.body).lineHeight(.body).foregroundStyle(.primary).textSelection(.enabled)
                }
            }
            .padding(Theme.Space.m)
            .background(m.isMine ? LarkColor.myBubble : LarkColor.otherBubble, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
        }
    }

    @ViewBuilder private var menu: some View {
        if canReply {
            Button { reply(m) } label: { Label("回复", systemImage: Theme.Icon.reply) }
        }
        if !m.summary.isEmpty {
            Button {
                UIPasteboard.general.string = m.text?.isEmpty == false ? m.text : m.summary
                Haptics.success()
            } label: { Label("拷贝", systemImage: Theme.Icon.copy) }
        }
        if let s = m.url, let u = URL(string: s) {
            Button { openURL(u) } label: { Label("在 Telegram 里打开", systemImage: Theme.Icon.openExternal) }
        }
    }
}

/// A message's media: a picture (tap: full screen), a sticker, a video's thumbnail with its length, a voice message's
/// length, a file's name and size. What cannot be fetched shows a placeholder of its kind.
struct ThreadMediaView: View {
    var connector: String
    var media: ThreadMedia
    @State private var viewing = false

    var body: some View {
        switch media.kind {
        case "image":
            picture
        case "sticker":
            if let ref = media.picture {
                LarkRemoteImage(url: ConnectorMedia.url(connector, ref: ref), maxPixel: 400, contentMode: .fit)
                    .scaledFrame(width: Theme.Size.box120, height: Theme.Size.box120)
                    .accessibilityLabel("贴纸 \(media.emoji ?? "")")
            } else {
                placeholder(Theme.Icon.photo, "贴纸 \(media.emoji ?? "")")
            }
        case "video":
            ZStack(alignment: .bottomLeading) {
                if let ref = media.thumb {
                    let size = LarkImagePart.displaySize(width: Double(media.width ?? 0), height: Double(media.height ?? 0))
                    LarkRemoteImage(url: ConnectorMedia.url(connector, ref: ref), maxPixel: 900)
                        .frame(width: size.width, height: size.height)
                        .clipShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
                        .overlay {
                            Image(systemName: Theme.Icon.playMedia).font(Theme.Font.display).foregroundStyle(.white.opacity(0.9))
                        }
                } else {
                    placeholder(Theme.Icon.video, "视频")
                }
                Text(ThreadMediaText.duration(media.duration ?? 0))
                    .font(Theme.Font.caption).monospacedDigit().foregroundStyle(.white)
                    .padding(.horizontal, Theme.Space.sm).padding(.vertical, Theme.Space.xxs)
                    .background(.black.opacity(0.5), in: .capsule)
                    .padding(Theme.Space.s)
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("视频 \(ThreadMediaText.duration(media.duration ?? 0))")
        case "voice":
            Label {
                Text("语音 \(ThreadMediaText.duration(media.duration ?? 0))").font(Theme.Font.secondaryMedium).monospacedDigit()
            } icon: {
                Image(systemName: Theme.Icon.voiceMessage)
            }
            .foregroundStyle(.primary)
        default:
            let name = media.name ?? "文件"
            HStack(spacing: Theme.Space.m) {
                VStack(alignment: .leading, spacing: Theme.Space.xs) {
                    Text(name).font(Theme.Font.secondaryMedium).foregroundStyle(.primary).lineLimit(2).multilineTextAlignment(.leading)
                    if let size = media.size, size > 0 {
                        Text(size.formatted(.byteCount(style: .file))).font(Theme.Font.label).foregroundStyle(.secondary)
                    }
                }
                Spacer(minLength: Theme.Space.s)
                LarkFileCard.badge((name as NSString).pathExtension.uppercased())
            }
            .scaledFrame(maxWidth: Theme.Size.box240)
            .accessibilityElement(children: .combine)
        }
    }

    @ViewBuilder private var picture: some View {
        if let ref = media.picture {
            let url = ConnectorMedia.url(connector, ref: ref)
            let size = LarkImagePart.displaySize(width: Double(media.width ?? 0), height: Double(media.height ?? 0))
            LarkRemoteImage(url: url, maxPixel: 900)
                .frame(width: size.width, height: size.height)
                .clipShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous).strokeBorder(Color.primary.opacity(0.06)))
                .contentShape(.rect)
                .onTapGesture { viewing = true }
                .fullScreenCover(isPresented: $viewing) { LarkImageGallery(urls: [url], index: 0) }
                .accessibilityLabel("图片")
        } else {
            placeholder(Theme.Icon.photo, "图片")
        }
    }

    private func placeholder(_ symbol: String, _ label: String) -> some View {
        Label(label, systemImage: symbol).font(Theme.Font.secondary).foregroundStyle(.secondary)
            .padding(Theme.Space.m)
            .background(Color.primary.opacity(0.05), in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
    }
}
