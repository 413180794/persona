import SwiftUI

/// Message positions are decimal strings that can outgrow Int64 in theory; compare them as numbers.
nonisolated enum LarkPosition {
    static func less(_ a: String, _ b: String) -> Bool {
        let x = a.trimmingCharacters(in: .whitespaces), y = b.trimmingCharacters(in: .whitespaces)
        let nx = x.hasPrefix("-"), ny = y.hasPrefix("-")
        if nx != ny { return nx }
        let ax = nx ? String(x.dropFirst()) : x, ay = ny ? String(y.dropFirst()) : y
        let r = ax.count != ay.count ? ax.count < ay.count : ax < ay
        return nx ? !r && ax != ay : r
    }

    static func next(_ p: String) -> String {
        if let n = Int64(p) { return String(n + 1) }
        return p
    }
}

/// One Feishu chat, opened like Feishu: at the oldest unread message under a 以下为新消息 line (or at the latest
/// when all is read), newer pages as you scroll down, read state reported only for what was on screen,
/// 回到最新 / 有人@你 jumps.
struct LarkChatView: View {
    @Environment(AppModel.self) private var model
    var conversation: LarkConversation
    @State private var pager = LarkPager()
    @State private var loadingOlder = false
    @State private var loadingNewer = false
    @State private var error: String?
    @State private var text = ""
    @State private var sending = false
    @State private var position = ScrollPosition(edge: .bottom)
    @State private var fileRequest: (url: String, name: String)?
    /// Read position when the chat was opened: the divider sits above the first message after it. Frozen for the visit.
    @State private var anchor: String?
    /// Highest position already reported as read.
    @State private var reportTask: Task<Void, Never>?
    @State private var atBottom = true
    @State private var mention: String?
    @State private var person: LarkPersonRef?
    @State private var debugPick: LarkMessage?
    @State private var debugPost: String?

    private var isTopic: Bool { conversation.isTopic == true }
    /// Opens at a message (first unread / a flagged message) rather than at the bottom.
    private var opensAtMessage: Bool { conversation.focus != nil || (!isTopic && conversation.unread > 0) }

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                if pager.hasOlder {
                    Button { Task { await loadOlder() } } label: {
                        Group { if loadingOlder { ProgressView().controlSize(.small) } else { Text("加载更早的消息").lineLimit(1) } }
                            .font(Theme.Font.labelMedium)
                            .scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.well)
                    }
                    .buttonStyle(.plain)
                    .foregroundStyle(LarkColor.blue)
                }
                if let error { Label(error, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger).padding(.bottom, Theme.Space.s) }
                ForEach(Array(pager.messages.enumerated()), id: \.element.id) { i, m in
                    let prev = i > 0 ? pager.messages[i - 1] : nil
                    VStack(alignment: .leading, spacing: 0) {
                        if isFirstUnread(m, after: prev) { LarkNewMessagesDivider() }
                        if Self.startsDay(m, after: prev) {
                            Text(Self.dayLabel(m.createdAt)).font(Theme.Font.labelMedium).foregroundStyle(.secondary)
                                .frame(maxWidth: .infinity).padding(.vertical, Theme.Space.m)
                        }
                        LarkMessageRow(message: m, grouped: Self.continues(m, after: prev), isGroup: conversation.isGroup, feedID: conversation.id)
                            .padding(.top, Self.continues(m, after: prev) ? 3 : 12)
                    }
                    .onAppear { markSeen(m) }
                }
                if pager.hasNewer {
                    ProgressView().controlSize(.small).scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.tapTarget)
                        .onAppear { Task { await loadNewer() } }
                }
            }
            .padding(.horizontal, Theme.Space.m)
            .padding(.vertical, Theme.Space.m)
        }
        .scrollPosition($position)
        .defaultScrollAnchor(Self.debugTop || opensAtMessage ? .top : .bottom)
        .onScrollGeometryChange(for: Bool.self) { g in
            g.contentOffset.y + g.containerSize.height >= g.contentSize.height - g.contentInsets.bottom - 80
        } action: { _, v in atBottom = v }
        .overlay(alignment: .bottomTrailing) { jumpToLatestButton }
        .overlay(alignment: .topTrailing) { mentionButton }
        .environment(\.larkOpenFile, { url, name in fileRequest = (url, name) })
        .environment(\.larkRefresh, { Task { await refreshLatest() } })
        .environment(\.larkOpenProfile, { person = $0 })
        .environment(\.larkReact, { m, key in react(m, key) })
        .modifier(LarkFileOpener(request: $fileRequest))
        .sheet(item: $person) { LarkProfileView(person: $0) }
        .sheet(item: $debugPick) { m in LarkReactionPicker(message: m) { react(m, $0) } }
        .sheet(isPresented: Binding(get: { debugPost != nil }, set: { if !$0 { debugPost = nil } })) {
            if let id = debugPost { LarkMomentsPostView(postID: id) }
        }
        .safeAreaInset(edge: .bottom) { composer }
        .themedPage()
        .navigationTitle(conversation.name)
        .navigationSubtitle(isTopic ? "飞书话题群" : "飞书")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar(.hidden, for: .tabBar)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button { Task { await askJarvis() } } label: { Label("让 Jarvis 总结", systemImage: Theme.Icon.ai) }
            }
        }
        .task {
            LarkMedia.shared.loadEmojiCatalog()
            // The last messages seen here paint at once (offline too); the network only brings them up to date.
            // Background sync (LarkSync) keeps this cache current, so it is usually already the latest. An unread chat
            // opens at its first unread message: the cache serves when it reaches back that far.
            if pager.messages.isEmpty, conversation.focus == nil,
               let cached = StateCache.load("lark-chat-\(conversation.id)", as: [LarkMessage].self), !cached.isEmpty,
               !opensAtMessage || cached.first.map({ !LarkPosition.less(LarkPosition.next(conversation.readPosition ?? "0"), $0.position) }) == true {
                pager.messages = cached
                pager.hasOlder = true
                pager.olderCursor = cached.first?.position
                pager.newerCursor = cached.last?.position
                let read = conversation.readPosition ?? "0"
                if opensAtMessage, let first = cached.first(where: { LarkPosition.less(read, $0.position) }) { await scroll(to: first.id) }
            }
            await load()
            while !Task.isCancelled { // a fallback: changes arrive as pushes (below)
                try? await Task.sleep(for: .seconds(60))
                await refreshLatest()
            }
        }
        .task {
            for await _ in LarkSync.shared.changes(of: conversation.id) { await refreshLatest() }
        }
        .onDisappear { flushRead() }
    }

    private var composer: some View {
        HStack(alignment: .bottom, spacing: Theme.Space.s) {
            TextField("回复 \(conversation.name)", text: $text, axis: .vertical)
                .lineLimit(1...5)
                .padding(.horizontal, Theme.Space.l)
                .padding(.vertical, Theme.Space.s)
                .surface(interactive: true, in: .rect(cornerRadius: Theme.Radius.bubble, style: .continuous))
            Button { Task { await send() } } label: {
                Group { if sending { ProgressView().tint(.white) } else { Image(systemName: Theme.Icon.send).font(Theme.Font.heading) } }
                    .scaledFrame(width: Theme.Size.wellL, height: Theme.Size.wellL)
            }
            .themedButton(prominent: true)
            .clipShape(.circle)
            .disabled(sending || text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            .accessibilityLabel("发送")
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.bottom, Theme.Space.s)
    }

    // MARK: Jumps

    /// Unread messages from others that have not been on screen yet.
    private var unseenNew: Int {
        guard let anchor else { return 0 }
        let seenUnread = pager.messages.filter { !$0.isMine && !$0.isSystem && pager.seen.contains($0.position) && LarkPosition.less(anchor, $0.position) }.count
        return max(0, conversation.unread - seenUnread)
    }

    @ViewBuilder private var jumpToLatestButton: some View {
        if pager.hasNewer || !atBottom {
            let n = unseenNew
            Button { Task { await jumpToLatest() } } label: {
                HStack(spacing: Theme.Space.sm) {
                    Image(systemName: Theme.Icon.expand).font(.system(size: 13, weight: .bold))
                    if n > 0 { Text("\(n > 99 ? "99+" : "\(n)") 条新消息").font(Theme.Font.captionMedium).lineLimit(1) }
                }
                .foregroundStyle(LarkColor.blue)
                .padding(.horizontal, n > 0 ? 12 : 0)
                .scaledFrame(minWidth: Theme.Size.wellXL, minHeight: Theme.Size.wellXL)
                .surface(interactive: true, in: .capsule)
            }
            .buttonStyle(.plain)
            .padding(.trailing, Theme.Space.l)
            .padding(.bottom, Theme.Space.s)
            .accessibilityLabel(n > 0 ? "\(n) 条新消息，回到最新" : "回到最新")
            .transition(.scale.combined(with: .opacity))
        }
    }

    @ViewBuilder private var mentionButton: some View {
        if let mention, !pager.seen.contains(mention) {
            Button { Task { await jump(to: mention) } } label: {
                Label("有人@你", systemImage: "at").font(Theme.Font.captionMedium).lineLimit(1)
                    .foregroundStyle(LarkColor.hue("red"))
                    .padding(.horizontal, Theme.Space.m).scaledFrame(minHeight: Theme.Size.wellM)
                    .surface(interactive: true, in: .capsule)
            }
            .buttonStyle(.plain)
            .padding(.trailing, Theme.Space.l)
            .padding(.top, Theme.Space.s)
        }
    }

    private func isFirstUnread(_ m: LarkMessage, after prev: LarkMessage?) -> Bool {
        guard let anchor, LarkPosition.less(anchor, m.position) else { return false }
        guard let prev else { return true }
        return !LarkPosition.less(anchor, prev.position)
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

    // MARK: Loading

    private func apply(_ r: APIClient.LarkMessages) {
        pager.replace(with: r)
        if !r.hasNewer.map({ $0 }).isTrue { StateCache.save("lark-chat-\(conversation.id)", Array(r.messages.suffix(80))) }
    }

    private func load() async {
        guard let api = model.api else { return }
        do {
            if let focus = conversation.focus {
                apply(try await api.larkMessages(conversation.id, around: focus))
                pager.readCursor = pager.messages.last?.position
                if let m = pager.messages.first(where: { $0.position == focus }) { await scroll(to: m.id) }
            } else if opensAtMessage {
                // Feishu opens an unread chat at its read position: the page around the first unread message.
                let read = conversation.readPosition ?? "0"
                anchor = read
                pager.readCursor = read
                if let p = conversation.mentionPosition, LarkPosition.less(read, p) { mention = p }
                apply(try await api.larkMessages(conversation.id, around: LarkPosition.next(read)))
                if let first = pager.messages.first(where: { LarkPosition.less(read, $0.position) }) {
                    await scroll(to: first.id)
                } else {
                    position.scrollTo(edge: .bottom)
                }
            } else {
                apply(try await api.larkMessages(conversation.id, topic: isTopic))
                pager.readCursor = pager.messages.last?.position
            }
            error = nil
            #if DEBUG
            // Simulator automation: `-larkFocus <messageId>` scrolls to that message (screenshots of one kind of card).
            if let focus = UserDefaults.standard.string(forKey: "larkFocus") { await scroll(to: focus) }
            // `-larkProfile <userId>` opens that person's card, `-larkPicker YES` the reaction picker on the last message.
            if let uid = UserDefaults.standard.string(forKey: "larkProfile") { person = LarkPersonRef(id: uid, name: "", feedID: conversation.id) }
            if UserDefaults.standard.bool(forKey: "larkPicker"), let last = pager.messages.last(where: { !$0.isSystem }) { debugPick = last }
            // `-larkMoments <postId>` opens that removed post.
            if let post = UserDefaults.standard.string(forKey: "larkMoments") { debugPost = post }
            #endif
        } catch {
            self.error = error.shown
        }
    }

    /// Scrolls once the rows exist (the lazy stack lays them out after the data lands).
    private func scroll(to id: String) async {
        try? await Task.sleep(for: .milliseconds(80))
        position.scrollTo(id: id, anchor: .top)
        try? await Task.sleep(for: .milliseconds(250))
        position.scrollTo(id: id, anchor: .top)
    }

    /// Polls the tail while it is loaded: new messages append, cards that changed (alert status…) update in place.
    private func refreshLatest() async {
        guard !pager.hasNewer, let api = model.api, let r = try? await api.larkMessages(conversation.id, topic: isTopic) else { return }
        let fresh = pager.refresh(with: r)
        guard !fresh.isEmpty else { return }
        let follow = atBottom
        withAnimation(Theme.Motion.standard) { pager.add(fresh) }
        if follow { position.scrollTo(edge: .bottom) }
    }

    private func loadOlder() async {
        guard let api = model.api, let cursor = pager.olderRequest, !loadingOlder else { return }
        loadingOlder = true
        defer { loadingOlder = false }
        guard let r = try? await api.larkMessages(conversation.id, before: cursor, topic: isTopic) else { return }
        pager.prepend(r)
    }

    private func loadNewer() async {
        guard let api = model.api, let cursor = pager.newerRequest, !loadingNewer, pager.hasNewer else { return }
        loadingNewer = true
        defer { loadingNewer = false }
        guard let r = try? await api.larkMessages(conversation.id, after: cursor) else { return }
        pager.append(r, from: cursor)
    }

    /// 回到最新: the latest page; the new-messages line goes away.
    private func jumpToLatest() async {
        if pager.hasNewer, let api = model.api, let r = try? await api.larkMessages(conversation.id, topic: isTopic) {
            apply(r)
            anchor = nil
        }
        withAnimation(Theme.Motion.standard) { position.scrollTo(edge: .bottom) }
    }

    /// 有人@你: to the mention, loading the page around it if needed.
    private func jump(to target: String) async {
        if let m = pager.messages.first(where: { $0.position == target }) {
            withAnimation(Theme.Motion.standard) { position.scrollTo(id: m.id, anchor: .top) }
            return
        }
        guard let api = model.api, let r = try? await api.larkMessages(conversation.id, around: target) else { return }
        apply(r)
        if let m = pager.messages.first(where: { $0.position == target }) { await scroll(to: m.id) }
    }

    // MARK: Reactions

    /// Toggles my reaction at once, then tells Feishu; the next poll brings the server's version (names…).
    private func react(_ m: LarkMessage, _ key: String) {
        guard let i = pager.messages.firstIndex(where: { $0.id == m.id }) else { return }
        let me = pager.messages.last(where: \.isMine)?.senderName ?? "我"
        let (next, remove) = pager.messages[i].togglingReaction(key, me: me)
        withAnimation(Theme.Motion.standard) { pager.messages[i] = next }
        Haptics.tap()
        Task {
            do {
                try await model.api?.larkReact(m.id, key: key, remove: remove)
            } catch {
                model.show(error)
                if let j = pager.messages.firstIndex(where: { $0.id == m.id }) { pager.messages[j] = m }
            }
        }
    }

    // MARK: Read reporting

    /// A message reached the screen: once rows stop arriving, report everything up to the newest one seen.
    private func markSeen(_ m: LarkMessage) {
        guard pager.see(m) else { return }
        guard !isTopic else { return }
        reportTask?.cancel()
        reportTask = Task {
            try? await Task.sleep(for: .milliseconds(400))
            guard !Task.isCancelled else { return }
            flushRead()
        }
    }

    private func flushRead() {
        guard !isTopic, let api = model.api, let report = pager.readReport() else { return }
        let feed = conversation.id
        Task { try? await api.larkRead(feed, report) }
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
            if pager.hasNewer { await jumpToLatest() } else { await refreshLatest(); position.scrollTo(edge: .bottom) }
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

/// Feishu's 以下为新消息 line above the first unread message.
struct LarkNewMessagesDivider: View {
    var body: some View {
        HStack(spacing: Theme.Space.m) {
            Rectangle().fill(LarkColor.blue.opacity(0.35)).frame(height: Theme.Size.line)
            Text("以下为新消息").font(Theme.Font.labelMedium).foregroundStyle(LarkColor.blue).lineLimit(1).fixedSize()
            Rectangle().fill(LarkColor.blue.opacity(0.35)).frame(height: Theme.Size.line)
        }
        .padding(.vertical, Theme.Space.m)
        .accessibilityAddTraits(.isHeader)
    }
}

/// One message, laid out like lark-connector (and Feishu desktop): everyone's messages on the left with avatar and,
/// in group chats, the sender's name on the first of a run; mine in blue. Avatar and name open the profile card.
struct LarkMessageRow: View {
    var message: LarkMessage
    var grouped: Bool
    var isGroup = true
    var feedID: String? = nil
    /// Inside a thread sheet the reply count is not a link to itself.
    var inThread = false
    @Environment(\.larkOpenProfile) private var openProfile
    @Environment(\.larkReact) private var react
    @State private var showThread = false
    @State private var picking = false

    var body: some View {
        let m = message
        if m.isSystem {
            Group {
                if let p = m.parts?.first, p.metaBool("system") { LarkSystemText(part: p) } else { Text(m.text).font(Theme.Font.label).foregroundStyle(.secondary) }
            }
            .multilineTextAlignment(.center)
            .frame(maxWidth: .infinity)
            .padding(.horizontal, Theme.Space.xl)
            .padding(.vertical, Theme.Space.xs)
        } else {
            HStack(alignment: .top, spacing: Theme.Space.s) {
                Group {
                    if grouped { Color.clear } else {
                        LarkAvatarImage(name: m.senderName, url: m.senderAvatar, size: 34)
                            .onTapGesture { showProfile() }
                            .accessibilityAddTraits(.isButton)
                            .accessibilityLabel("\(m.senderName) 的资料")
                    }
                }
                .scaledFrame(width: Theme.Size.wellM, height: grouped ? Theme.Size.line : Theme.Size.wellM)
                VStack(alignment: .leading, spacing: Theme.Space.xs) {
                    if !grouped && isGroup {
                        HStack(spacing: Theme.Space.xs) {
                            Text(m.senderName).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                                .onTapGesture { showProfile() }
                            if m.senderType == "bot" {
                                Text("机器人").font(Theme.Font.labelStrong).foregroundStyle(LarkColor.blue)
                                    .padding(.horizontal, Theme.Space.xs).padding(.vertical, 1).background(LarkColor.blue.opacity(0.12), in: .rect(cornerRadius: 3))
                            }
                        }
                    }
                    content
                    footer
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .sheet(isPresented: $picking) {
                LarkReactionPicker(message: m) { react?(m, $0) }
            }
        }
    }

    private func showProfile() {
        guard let id = message.senderID, !id.isEmpty else { return }
        openProfile?(LarkPersonRef(id: id, name: message.senderName, avatar: message.senderAvatar, feedID: feedID))
    }

    @ViewBuilder private var content: some View {
        let m = message
        let parts = m.parts ?? []
        if m.recalled {
            Text(m.isMine ? "你撤回了一条消息" : "\(m.senderName) 撤回了一条消息").font(Theme.Font.caption).foregroundStyle(.tertiary).italic()
                .padding(.horizontal, Theme.Space.m).padding(.vertical, Theme.Space.s)
                .background(Color.primary.opacity(0.04), in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
        } else if parts.isEmpty {
            bubble { Text(m.text.isEmpty ? "[\(m.type.lowercased())]" : m.text).font(Theme.Font.body) }
        } else if LarkPartsView.isBare(parts) {
            if let reply = m.replyTo { LarkReplyQuote(reply: reply).padding(.horizontal, Theme.Space.xs) }
            LarkPartsView(parts: parts)
                .environment(\.larkContext, LarkRenderContext(messageID: m.id, images: LarkPartsView.imageURLs(parts)))
                .frame(maxWidth: parts.contains { $0.kind == "card" || $0.kind == "link" } ? .infinity : nil, alignment: .leading)
                .contextMenu { menu }
        } else {
            bubble {
                VStack(alignment: .leading, spacing: Theme.Space.sm) {
                    if let reply = m.replyTo { LarkReplyQuote(reply: reply) }
                    LarkPartsView(parts: parts)
                        .environment(\.larkContext, LarkRenderContext(messageID: m.id, images: LarkPartsView.imageURLs(parts)))
                }
            }
        }
    }

    private func bubble<C: View>(@ViewBuilder _ c: () -> C) -> some View {
        c()
            .padding(.horizontal, Theme.Space.m)
            .padding(.vertical, Theme.Space.m)
            .background(message.isMine ? LarkColor.myBubble : LarkColor.otherBubble, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
            .contextMenu { menu }
    }

    /// Long press: a row of quick reactions, more emoji, copy.
    @ViewBuilder private var menu: some View {
        if react != nil && !message.recalled {
            ControlGroup {
                ForEach(Array(LarkMedia.shared.quickReactions.prefix(4)), id: \.self) { key in
                    Button { react?(message, key) } label: {
                        if let img = LarkMedia.shared.emojiImage(key, points: 28) { Image(uiImage: img) } else { Text(LarkMedia.labelFallback(key, text: nil)) }
                    }
                }
            }
            .controlGroupStyle(.compactMenu)
            Button { picking = true } label: { Label("表情回复", systemImage: "face.smiling") }
        }
        Button {
            UIPasteboard.general.string = message.text
            Haptics.success()
        } label: { Label("拷贝", systemImage: Theme.Icon.copy) }
        if let id = message.senderID, !id.isEmpty, openProfile != nil {
            Button { showProfile() } label: { Label("查看 \(message.senderName) 的资料", systemImage: Theme.Icon.person) }
        }
    }

    @ViewBuilder private var footer: some View {
        let m = message
        let reactions = m.reactions ?? []
        if !reactions.isEmpty {
            LarkFlowLayout(spacing: 6, lineSpacing: 6) {
                ForEach(reactions, id: \.key) { r in
                    Button { react?(m, r.key) } label: { LarkReactionChip(reaction: r) }
                        .buttonStyle(.plain)
                        .disabled(react == nil)
                }
                if react != nil {
                    Button { picking = true } label: {
                        Image(systemName: "face.smiling").font(.system(size: 14)).foregroundStyle(.secondary)
                            .scaledFrame(width: Theme.Size.well, height: Theme.Size.iconL).background(Color.primary.opacity(0.05), in: .capsule)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("表情回复")
                }
            }
            .scaledFrame(maxWidth: Theme.Size.box300, alignment: .leading)
        }
        let replies = max(m.threadReplyCount ?? 0, m.replyCount)
        if replies > 0 || m.edited == true || !LarkChatView.continues(m, after: nil) {
            HStack(spacing: Theme.Space.s) {
                if replies > 0 && !inThread {
                    Button { showThread = true } label: {
                        Label("\(replies) 条回复", systemImage: Theme.Icon.reply).foregroundStyle(LarkColor.blue)
                    }
                    .buttonStyle(.plain)
                }
                if m.edited == true { Text("已编辑") }
                if let d = m.createdAt { Text(d.formatted(date: .omitted, time: .shortened)) }
            }
            .font(Theme.Font.label)
            .foregroundStyle(.secondary)
            .lineLimit(1)
            .sheet(isPresented: $showThread) { LarkThreadView(root: m, isGroup: isGroup, feedID: feedID) }
        }
    }
}

extension LarkMessage {
    /// The message with my reaction `key` toggled (lark-connector's optimistic update: remove if mine, else add).
    func togglingReaction(_ key: String, me: String) -> (LarkMessage, remove: Bool) {
        var m = self
        var list = m.reactions ?? []
        if let i = list.firstIndex(where: { $0.key.lowercased() == key.lowercased() }) {
            var r = list[i]
            if r.mine == true {
                r.count -= 1
                r.mine = false
                r.users?.removeAll { $0 == me }
                if r.count <= 0 { list.remove(at: i) } else { list[i] = r }
                m.reactions = list
                return (m, true)
            }
            r.count += 1
            r.mine = true
            r.users = (r.users ?? []) + [me]
            list[i] = r
        } else {
            list.append(LarkReaction(key: key, count: 1, mine: true, users: [me]))
        }
        m.reactions = list
        return (m, false)
    }
}

/// A thread: its first message, then every reply.
struct LarkThreadView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var root: LarkMessage
    var isGroup = true
    var feedID: String? = nil
    @State private var person: LarkPersonRef?
    @State private var replies: [LarkMessage] = []
    @State private var loading = true
    @State private var error: String?
    @State private var fileRequest: (url: String, name: String)?

    var body: some View {
        NavigationStack {
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    LarkMessageRow(message: root, grouped: false, isGroup: isGroup, feedID: feedID, inThread: true)
                    HStack {
                        Text(loading ? "加载回复…" : "\(replies.count) 条回复").font(Theme.Font.labelMedium).foregroundStyle(.secondary)
                        Rectangle().fill(Color.primary.opacity(0.08)).frame(height: Theme.Size.line)
                    }
                    .padding(.vertical, Theme.Space.m)
                    if let error { Label(error, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger) }
                    ForEach(Array(replies.enumerated()), id: \.element.id) { i, m in
                        let prev = i > 0 ? replies[i - 1] : nil
                        LarkMessageRow(message: m, grouped: LarkChatView.continues(m, after: prev), isGroup: isGroup, feedID: feedID, inThread: true)
                            .padding(.top, LarkChatView.continues(m, after: prev) ? 3 : 12)
                    }
                }
                .padding(Theme.Space.m)
            }
            .environment(\.larkOpenFile, { url, name in fileRequest = (url, name) })
            .environment(\.larkOpenProfile, { person = $0 })
            .environment(\.larkReact, { m, key in
                guard let i = replies.firstIndex(where: { $0.id == m.id }) else { return }
                let (next, remove) = m.togglingReaction(key, me: "我")
                replies[i] = next
                Task { do { try await model.api?.larkReact(m.id, key: key, remove: remove) } catch { model.show(error) } }
            })
            .modifier(LarkFileOpener(request: $fileRequest))
            .sheet(item: $person) { LarkProfileView(person: $0) }
            .navigationTitle("话题")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .topBarTrailing) { Button("完成") { dismiss() } } }
            .task {
                defer { loading = false }
                guard let api = model.api else { return }
                do { replies = try await api.larkThread(root.id).messages.filter { $0.id != root.id } } catch { self.error = error.shown }
            }
        }
    }
}

/// The message a reply quotes: a bar, the author, one or two lines of it.
struct LarkReplyQuote: View {
    var reply: LarkReplyTo

    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            Capsule().fill(Color.primary.opacity(0.2)).frame(width: Theme.Size.ruleWide)
            Text(reply.recalled == true ? "\(reply.senderName)：消息已撤回" : "\(reply.senderName)：\(Fmt.plain(reply.text))")
                .font(Theme.Font.caption)
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
        HStack(spacing: Theme.Space.xs) {
            if let url = media.emoji(reaction.key)?.url {
                LarkRemoteImage(url: url, maxPixel: 64, contentMode: .fit).scaledFrame(width: Theme.Size.markL, height: Theme.Size.markL)
            } else {
                Text(LarkMedia.labelFallback(reaction.key, text: nil)).font(Theme.Font.label)
            }
            Text(users.isEmpty || users.count > 3 ? "\(reaction.count)" : users.joined(separator: "、"))
                .font(Theme.Font.label)
                .foregroundStyle(reaction.mine == true ? LarkColor.blue : .secondary)
                .lineLimit(1)
        }
        .padding(.horizontal, Theme.Space.s)
        .padding(.vertical, Theme.Space.xs)
        .background(reaction.mine == true ? LarkColor.blue.opacity(0.12) : Color.primary.opacity(0.05), in: .capsule)
        .accessibilityLabel("\(media.emoji(reaction.key)?.label ?? reaction.key) \(reaction.count)")
    }
}

private extension Optional where Wrapped == Bool {
    var isTrue: Bool { self == true }
}

