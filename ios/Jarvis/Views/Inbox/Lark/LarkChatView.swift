import SwiftUI

/// One Feishu chat: latest messages rendered like Feishu (rich text, pictures, cards…), older on demand,
/// reply, and hand it to Jarvis.
struct LarkChatView: View {
    @Environment(AppModel.self) private var model
    var conversation: LarkConversation
    @State private var messages: [LarkMessage] = []
    @State private var hasOlder = false
    @State private var loadingOlder = false
    @State private var error: String?
    @State private var text = ""
    @State private var sending = false
    @State private var position = ScrollPosition(edge: .bottom)
    @State private var fileRequest: (url: String, name: String)?

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                if hasOlder {
                    Button { Task { await loadOlder() } } label: {
                        Group { if loadingOlder { ProgressView().controlSize(.small) } else { Text("加载更早的消息").lineLimit(1) } }
                            .font(.caption.weight(.medium))
                            .frame(maxWidth: .infinity, minHeight: 32)
                    }
                    .buttonStyle(.plain)
                    .foregroundStyle(LarkColor.blue)
                }
                if let error { Label(error, systemImage: "exclamationmark.triangle").font(.caption).foregroundStyle(Theme.danger).padding(.bottom, 8) }
                ForEach(Array(messages.enumerated()), id: \.element.id) { i, m in
                    let prev = i > 0 ? messages[i - 1] : nil
                    if Self.startsDay(m, after: prev) {
                        Text(Self.dayLabel(m.createdAt)).font(.caption2.weight(.medium)).foregroundStyle(.tertiary)
                            .frame(maxWidth: .infinity).padding(.vertical, 10)
                    }
                    LarkMessageRow(message: m, grouped: Self.continues(m, after: prev))
                        .padding(.top, Self.continues(m, after: prev) ? 3 : 12)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 12)
        }
        .scrollPosition($position)
        .defaultScrollAnchor(Self.debugTop ? .top : .bottom)
        .environment(\.larkOpenFile, { url, name in fileRequest = (url, name) })
        .environment(\.larkRefresh, { Task { await refreshLatest() } })
        .modifier(LarkFileOpener(request: $fileRequest))
        .safeAreaInset(edge: .bottom) { composer }
        .navigationTitle(conversation.name)
        .navigationSubtitle(conversation.isTopic == true ? "飞书话题群" : "飞书")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar(.hidden, for: .tabBar)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button { Task { await askJarvis() } } label: { Label("让 Jarvis 总结", systemImage: "sparkles") }
            }
        }
        .task {
            LarkMedia.shared.loadEmojiCatalog()
            await load()
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(15))
                await refreshLatest()
            }
        }
    }

    private var composer: some View {
        HStack(alignment: .bottom, spacing: 8) {
            TextField("回复 \(conversation.name)", text: $text, axis: .vertical)
                .lineLimit(1...5)
                .padding(.horizontal, 14)
                .padding(.vertical, 9)
                .glassEffect(.regular.interactive(), in: .rect(cornerRadius: 20, style: .continuous))
            Button { Task { await send() } } label: {
                Group { if sending { ProgressView().tint(.white) } else { Image(systemName: "arrow.up").font(.body.weight(.bold)) } }
                    .frame(width: 36, height: 36)
            }
            .buttonStyle(.glassProminent)
            .clipShape(.circle)
            .disabled(sending || text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            .accessibilityLabel("发送")
        }
        .padding(.horizontal, 12)
        .padding(.bottom, 8)
    }

    #if DEBUG
    /// Simulator automation: `-larkTop YES` starts at the oldest loaded message (screenshots of long chats).
    static var debugTop: Bool { UserDefaults.standard.bool(forKey: "larkTop") }
    #else
    static let debugTop = false
    #endif

    /// Same sender within five minutes: no second avatar / name.
    static func continues(_ m: LarkMessage, after prev: LarkMessage?) -> Bool {
        guard let prev, !m.isSystem, !prev.isSystem, prev.senderName == m.senderName, prev.isMine == m.isMine,
              let a = prev.createdAt, let b = m.createdAt else { return false }
        return b.timeIntervalSince(a) < 300
    }

    static func startsDay(_ m: LarkMessage, after prev: LarkMessage?) -> Bool {
        guard let d = m.createdAt else { return false }
        guard let p = prev?.createdAt else { return true }
        return !Calendar.current.isDate(d, inSameDayAs: p) || d.timeIntervalSince(p) > 3 * 3600
    }

    static func dayLabel(_ d: Date?) -> String {
        guard let d else { return "" }
        let cal = Calendar.current
        let time = d.formatted(date: .omitted, time: .shortened)
        if cal.isDateInToday(d) { return time }
        if cal.isDateInYesterday(d) { return "昨天 " + time }
        if cal.isDate(d, equalTo: .now, toGranularity: .year) { return d.formatted(.dateTime.month().day()) + " " + time }
        return d.formatted(.dateTime.year().month().day()) + " " + time
    }

    private func load() async {
        guard let api = model.api else { return }
        do {
            let r = try await api.larkMessages(conversation.id, topic: conversation.isTopic == true)
            messages = r.messages
            hasOlder = r.hasOlder
            error = nil
            if let last = messages.last { try? await api.larkRead(conversation.id, position: last.position) }
            #if DEBUG
            // Simulator automation: `-larkFocus <messageId>` scrolls to that message (screenshots of one kind of card).
            if let focus = UserDefaults.standard.string(forKey: "larkFocus") {
                try? await Task.sleep(for: .milliseconds(600))
                position.scrollTo(id: focus, anchor: .top)
            }
            #endif
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func refreshLatest() async {
        guard let api = model.api, let r = try? await api.larkMessages(conversation.id, topic: conversation.isTopic == true) else { return }
        // Cards update in place (alert status, votes…): replace the ones we already show.
        let byID = Dictionary(r.messages.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
        let updated = messages.map { byID[$0.id] ?? $0 }
        let fresh = r.messages.filter { m in !messages.contains { $0.id == m.id } }
        if updated != messages { messages = updated }
        guard !fresh.isEmpty else { return }
        withAnimation(.snappy) { messages += fresh }
        position.scrollTo(edge: .bottom)
        if let last = messages.last { try? await api.larkRead(conversation.id, position: last.position) }
    }

    private func loadOlder() async {
        guard let api = model.api, let first = messages.first, !loadingOlder else { return }
        loadingOlder = true
        defer { loadingOlder = false }
        guard let r = try? await api.larkMessages(conversation.id, before: first.position, topic: conversation.isTopic == true) else { return }
        messages = r.messages.filter { m in !messages.contains { $0.id == m.id } } + messages
        hasOlder = r.hasOlder
    }

    private func send() async {
        guard let api = model.api else { return }
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        sending = true
        defer { sending = false }
        do {
            try await api.larkSend(conversation.id, text: t)
            text = ""
            Haptics.success()
            try? await Task.sleep(for: .milliseconds(800))
            await refreshLatest()
        } catch {
            model.show(error)
        }
    }

    private func askJarvis() async {
        guard let api = model.api else { return }
        let prompt = "读取我飞书会话「\(conversation.name)」（lark-connector feedId \(conversation.id)）最近的消息，总结要点，列出需要我回复或处理的事，并给出建议的回复。"
        do {
            let s = try await api.startConversation(prompt: prompt)
            model.assistantCurrent = s.key
            model.selectedTab = .assistant
        } catch {
            model.show(error)
        }
    }
}

/// One message: avatar and name (others), bubble or bare content, reply quote, reactions, thread count.
struct LarkMessageRow: View {
    var message: LarkMessage
    var grouped: Bool
    /// Inside a thread sheet the reply count is not a link to itself.
    var inThread = false
    @State private var showThread = false

    var body: some View {
        let m = message
        if m.isSystem {
            Group {
                if let p = m.parts?.first, p.metaBool("system") { LarkSystemText(part: p) } else { Text(m.text).font(.caption).foregroundStyle(.secondary) }
            }
            .multilineTextAlignment(.center)
            .frame(maxWidth: .infinity)
            .padding(.horizontal, 24)
            .padding(.vertical, 4)
        } else {
            HStack(alignment: .top, spacing: 8) {
                if m.isMine { Spacer(minLength: 44) } else {
                    Group {
                        if grouped { Color.clear } else { LarkAvatarImage(name: m.senderName, url: m.senderAvatar, size: 34) }
                    }
                    .frame(width: 34, height: grouped ? 1 : 34)
                }
                VStack(alignment: m.isMine ? .trailing : .leading, spacing: 4) {
                    if !grouped && !m.isMine {
                        HStack(spacing: 4) {
                            Text(m.senderName).font(.caption.weight(.medium)).foregroundStyle(.secondary).lineLimit(1)
                            if m.senderType == "bot" {
                                Text("机器人").font(.system(size: 9, weight: .semibold)).foregroundStyle(LarkColor.blue)
                                    .padding(.horizontal, 4).padding(.vertical, 1).background(LarkColor.blue.opacity(0.12), in: .rect(cornerRadius: 3))
                            }
                        }
                    }
                    content
                    footer
                }
                .frame(maxWidth: .infinity, alignment: m.isMine ? .trailing : .leading)
            }
        }
    }

    @ViewBuilder private var content: some View {
        let m = message
        let parts = m.parts ?? []
        if m.recalled {
            Text(m.isMine ? "你撤回了一条消息" : "\(m.senderName) 撤回了一条消息").font(.footnote).foregroundStyle(.tertiary).italic()
                .padding(.horizontal, 12).padding(.vertical, 8)
                .background(Color.primary.opacity(0.04), in: .rect(cornerRadius: 14, style: .continuous))
        } else if parts.isEmpty {
            bubble { Text(m.text.isEmpty ? "[\(m.type.lowercased())]" : m.text).font(.system(size: 16)) }
        } else if LarkPartsView.isBare(parts) {
            if let reply = m.replyTo { LarkReplyQuote(reply: reply).padding(.horizontal, 4) }
            LarkPartsView(parts: parts)
                .environment(\.larkContext, LarkRenderContext(messageID: m.id, images: LarkPartsView.imageURLs(parts)))
                .frame(maxWidth: parts.contains { $0.kind == "card" } ? .infinity : nil, alignment: m.isMine ? .trailing : .leading)
                .contextMenu { copyButton }
        } else {
            bubble {
                VStack(alignment: .leading, spacing: 6) {
                    if let reply = m.replyTo { LarkReplyQuote(reply: reply) }
                    LarkPartsView(parts: parts)
                        .environment(\.larkContext, LarkRenderContext(messageID: m.id, images: LarkPartsView.imageURLs(parts)))
                }
            }
        }
    }

    private func bubble<C: View>(@ViewBuilder _ c: () -> C) -> some View {
        c()
            .padding(.horizontal, 12)
            .padding(.vertical, 9)
            .background(message.isMine ? LarkColor.myBubble : LarkColor.otherBubble, in: .rect(cornerRadius: 14, style: .continuous))
            .contextMenu { copyButton }
    }

    private var copyButton: some View {
        Button {
            UIPasteboard.general.string = message.text
            Haptics.success()
        } label: { Label("拷贝", systemImage: "doc.on.doc") }
    }

    @ViewBuilder private var footer: some View {
        let m = message
        let reactions = m.reactions ?? []
        if !reactions.isEmpty {
            LarkFlowLayout(spacing: 6, lineSpacing: 6) {
                ForEach(reactions, id: \.key) { r in LarkReactionChip(reaction: r) }
            }
            .frame(maxWidth: 280, alignment: m.isMine ? .trailing : .leading)
        }
        let replies = max(m.threadReplyCount ?? 0, m.replyCount)
        if replies > 0 || m.edited == true || !LarkChatView.continues(m, after: nil) {
            HStack(spacing: 8) {
                if replies > 0 && !inThread {
                    Button { showThread = true } label: {
                        Label("\(replies) 条回复", systemImage: "arrowshape.turn.up.left").foregroundStyle(LarkColor.blue)
                    }
                    .buttonStyle(.plain)
                }
                if m.edited == true { Text("已编辑") }
                if let d = m.createdAt { Text(d.formatted(date: .omitted, time: .shortened)) }
            }
            .font(.caption2)
            .foregroundStyle(.tertiary)
            .lineLimit(1)
            .sheet(isPresented: $showThread) { LarkThreadView(root: m) }
        }
    }
}

/// A thread: its first message, then every reply.
struct LarkThreadView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var root: LarkMessage
    @State private var replies: [LarkMessage] = []
    @State private var loading = true
    @State private var error: String?
    @State private var fileRequest: (url: String, name: String)?

    var body: some View {
        NavigationStack {
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    LarkMessageRow(message: root, grouped: false, inThread: true)
                    HStack {
                        Text(loading ? "加载回复…" : "\(replies.count) 条回复").font(.caption.weight(.medium)).foregroundStyle(.secondary)
                        Rectangle().fill(Color.primary.opacity(0.08)).frame(height: 1)
                    }
                    .padding(.vertical, 12)
                    if let error { Label(error, systemImage: "exclamationmark.triangle").font(.caption).foregroundStyle(Theme.danger) }
                    ForEach(Array(replies.enumerated()), id: \.element.id) { i, m in
                        let prev = i > 0 ? replies[i - 1] : nil
                        LarkMessageRow(message: m, grouped: LarkChatView.continues(m, after: prev), inThread: true)
                            .padding(.top, LarkChatView.continues(m, after: prev) ? 3 : 12)
                    }
                }
                .padding(12)
            }
            .environment(\.larkOpenFile, { url, name in fileRequest = (url, name) })
            .modifier(LarkFileOpener(request: $fileRequest))
            .navigationTitle("话题")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .topBarTrailing) { Button("完成") { dismiss() } } }
            .task {
                defer { loading = false }
                guard let api = model.api else { return }
                do { replies = try await api.larkThread(root.id).messages.filter { $0.id != root.id } } catch { self.error = error.localizedDescription }
            }
        }
    }
}

/// The message a reply quotes: a bar, the author, one or two lines of it.
struct LarkReplyQuote: View {
    var reply: LarkReplyTo

    var body: some View {
        HStack(spacing: 6) {
            Capsule().fill(Color.primary.opacity(0.2)).frame(width: 2.5)
            Text(reply.recalled == true ? "\(reply.senderName)：消息已撤回" : "\(reply.senderName)：\(Fmt.plain(reply.text))")
                .font(.system(size: 13))
                .foregroundStyle(.secondary)
                .lineLimit(2)
        }
        .fixedSize(horizontal: false, vertical: true)
    }
}

/// A reaction: the emoji picture, then who (a few names) or how many.
struct LarkReactionChip: View {
    var reaction: LarkReaction

    var body: some View {
        let media = LarkMedia.shared
        let users = reaction.users ?? []
        HStack(spacing: 4) {
            if let url = media.emoji(reaction.key)?.url {
                LarkRemoteImage(url: url, maxPixel: 64, contentMode: .fit).frame(width: 17, height: 17)
            } else {
                Text(LarkMedia.labelFallback(reaction.key, text: nil)).font(.caption2)
            }
            Text(users.isEmpty || users.count > 3 ? "\(reaction.count)" : users.joined(separator: "、"))
                .font(.caption)
                .foregroundStyle(reaction.mine == true ? LarkColor.blue : .secondary)
                .lineLimit(1)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(reaction.mine == true ? LarkColor.blue.opacity(0.12) : Color.primary.opacity(0.05), in: .capsule)
        .accessibilityLabel("\(media.emoji(reaction.key)?.label ?? reaction.key) \(reaction.count)")
    }
}
