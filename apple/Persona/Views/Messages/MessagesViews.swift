import SwiftUI

// 设置 › 连接器 › 信息: one Mac's Messages, read through the hub (docs/plan-connectors.md「五」). The user reads every
// conversation here; 交给 Jarvis (on by default) also gives the real ones to the knowledge base, the inbox and the
// assistant, and can be turned off, and what the hub kept deleted. Nothing is marked read, nothing is sent.

struct MessagesChatsRoute: Hashable {}
struct MessagesThreadRoute: Hashable { var id: String; var name: String }

/// The connector's page: which Mac, whether it may read, and how to let it.
struct MessagesConnectorView: View {
    @Environment(AppModel.self) private var model
    @State private var status: MessagesStatus?
    @State private var loading = true
    @State private var failure: String?
    @State private var saving = false
    @State private var confirmDelete = false
    @State private var deleted: String?

    var body: some View {
        List {
            Section {
                HStack(spacing: Theme.Space.m) {
                    Image(systemName: Theme.Icon.messagesApp).resizable().scaledToFit().foregroundStyle(Theme.positive)
                        .scaledFrame(width: Theme.Size.wellM, height: Theme.Size.wellM)
                    VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                        Text(status.map { "\($0.machine) 上的信息" } ?? "信息").font(Theme.Font.heading).lineLimit(1)
                        HStack(spacing: Theme.Space.sm) {
                            Circle().fill(tint).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
                            Text(stateText).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(2)
                        }
                    }
                    Spacer(minLength: Theme.Space.s)
                    if loading { ProgressView() }
                }
                .padding(.vertical, Theme.Space.xs)
                if status?.status == "ok" {
                    NavigationLink(value: MessagesChatsRoute()) {
                        Label("会话", systemImage: Theme.Icon.feishuChats)
                    }
                    .accessibilityIdentifier("messages-chats")
                }
                if let e = (status?.status == "ok" ? nil : status?.error) ?? failure, !e.isEmpty {
                    Text(e).font(Theme.Font.label).foregroundStyle(Theme.danger).textSelection(.enabled)
                }
            }
            if status?.status == "no_permission", let guide = status?.guide, !guide.isEmpty {
                Section {
                    ForEach(Array(guide.enumerated()), id: \.offset) { i, step in
                        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.m) {
                            Text("\(i + 1)").font(Theme.Font.labelStrong).monospacedDigit().foregroundStyle(Theme.accent)
                            Text(step).font(Theme.Font.body).textSelection(.enabled)
                        }
                    }
                    Button { Task { await load() } } label: {
                        Label("重新检查", systemImage: Theme.Icon.retry).lineLimit(1)
                    }
                    .disabled(loading)
                } header: {
                    SettingsHeading("在 \(status?.machine ?? "那台 Mac") 上打开权限")
                }
            }
            if let ai = status?.ai {
                Section {
                    Toggle(isOn: Binding(get: { ai }, set: { on in Task { await setAI(on) } })) {
                        Label("交给 Jarvis（知识库、收件箱、助手）", systemImage: Theme.Icon.ai).lineLimit(2)
                    }
                    .disabled(saving)
                    .accessibilityIdentifier("messages-ai")
                    Button(role: .destructive) { confirmDelete = true } label: {
                        Label("删除 hub 上已存的信息", systemImage: Theme.Icon.delete).lineLimit(1)
                    }
                    .disabled(ai || saving)
                    .accessibilityIdentifier("messages-delete")
                    if let deleted {
                        Text(deleted).font(Theme.Font.label).foregroundStyle(.secondary)
                    }
                } footer: {
                    Text(ai ? "通讯录里的人和你回复过的会话会进知识库和收件箱（和飞书一样分拣），助手能查、能总结；验证码会打码。关掉立刻停止；要删 hub 上已存的，先关掉。"
                            : "现在不交给 Jarvis：不同步、不进收件箱，助手读不到。hub 上之前存下的还在，可以删除。")
                }
            }
            Section {
            } footer: {
                Text(status?.ai == nil ? "消息只在这里给你自己看：不存到 hub、不交给 AI，看了也不会标成已读。Jarvis 只读，不会替你发消息。"
                                       : "这里能看全部会话（直接读那台 Mac，看了也不会标成已读）。Jarvis 只读，不会替你发消息。")
            }
        }
        .confirmationDialog("删除 hub 上已存的信息？", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("删除", role: .destructive) { Task { await deleteData() } }
        } message: {
            Text("知识库里的信息会话和人、收件箱里的信息条目都会删掉；那台 Mac 上的消息不受影响。")
        }
        .themedPage()
        .navigationTitle("信息")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
    }

    private var tint: Color {
        switch status?.status {
        case nil: .secondary
        case "ok": Theme.positive
        case "not_configured": .secondary
        default: Theme.danger
        }
    }

    private var stateText: String {
        guard let status else { return loading ? "正在检查…" : "读不到状态" }
        switch status.status {
        case "ok": return "可以读"
        case "no_permission": return "没有完全磁盘访问权限"
        case "not_configured": return "那台电脑上没有「信息」"
        default: return "读不了"
        }
    }

    private func load() async {
        loading = true
        defer { loading = false }
        do {
            status = try await model.api?.messagesStatus()
            failure = nil
        } catch {
            failure = error.shown.map { "读不到信息的状态：\($0)" }
        }
    }

    private func setAI(_ on: Bool) async {
        saving = true
        defer { saving = false }
        do {
            if let s = try await model.api?.setMessagesAI(on) { status = s }
            deleted = nil
            failure = nil
        } catch {
            failure = error.shown.map { "没改成：\($0)" }
        }
    }

    private func deleteData() async {
        saving = true
        defer { saving = false }
        do {
            if let d = try await model.api?.deleteMessagesData() {
                deleted = "已删除 \(d.chats) 个会话、\(d.people) 个人、\(d.inbox) 件收件箱条目"
            }
            failure = nil
        } catch {
            failure = error.shown.map { "没删成：\($0)" }
        }
    }
}

/// The Mac's conversations, newest first.
struct MessagesChatListView: View {
    @State private var page = MessagesChatsModel()
    @State private var query = ""

    var body: some View {
        let chats = page.filtered(query)
        List {
            if case .noPermission(let why) = page.failure, page.chats.isEmpty {
                Section {
                    Label(why, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger)
                }
            } else if let f = page.failure {
                Label(f.text, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger)
            }
            ForEach(chats) { chat in
                NavigationLink(value: MessagesThreadRoute(id: chat.id, name: chat.name)) {
                    MessagesChatRow(chat: chat)
                }
                .accessibilityIdentifier("messages-chat-\(chat.id)")
                .onAppear {
                    if chat.id == page.chats.last?.id { Task { await page.loadMore() } }
                }
            }
            if page.loading && page.loaded {
                ProgressView().frame(maxWidth: .infinity)
            }
        }
        .themedPage()
        .overlay {
            if !page.loaded {
                ProgressView()
            } else if chats.isEmpty && page.failure == nil {
                EmptyStateView(symbol: Theme.Icon.messagesApp, title: query.isEmpty ? "没有会话" : "没有找到",
                               message: query.isEmpty ? "那台 Mac 上的「信息」还没有会话。" : "换个名字或号码试试。")
            }
        }
        .navigationTitle("会话")
        .navigationBarTitleDisplayMode(.inline)
        .searchable(text: $query, prompt: "搜索名字或号码")
        .task { if !page.loaded { await page.load() } }
        .refreshable { await page.load() }
    }
}

/// One conversation in the list: who, the newest message, when, how many not read.
struct MessagesChatRow: View {
    var chat: MessagesChat

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Space.m) {
            avatar
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                HStack(alignment: .firstTextBaseline, spacing: Theme.Space.sm) {
                    Text(chat.name).font(Theme.Font.bodyStrong).lineLimit(1)
                    Spacer(minLength: Theme.Space.xs)
                    Text(LarkTime.short(chat.last)).font(Theme.Font.caption).foregroundStyle(.secondary).fixedSize()
                }
                HStack(alignment: .top, spacing: Theme.Space.sm) {
                    Text(preview).font(Theme.Font.secondary).lineHeight(.secondary).foregroundStyle(.secondary).lineLimit(2)
                    Spacer(minLength: 0)
                    if let n = chat.unread, n > 0 {
                        CountBadge(count: n, look: .solid(Theme.accent))
                    }
                }
            }
        }
        .padding(.vertical, Theme.Space.xs)
        .contentShape(.rect)
    }

    private var preview: String {
        let text = chat.preview ?? ""
        return chat.fromMe == true && !text.isEmpty ? "你：" + text : text
    }

    @ViewBuilder private var avatar: some View {
        if chat.isGroup {
            Circle().fill(Theme.monogramFill)
                .scaledFrame(width: Theme.Size.avatar, height: Theme.Size.avatar)
                .overlay(Image(systemName: Theme.Icon.groupChat).font(Theme.Font.body).foregroundStyle(.secondary))
        } else {
            Monogram(name: chat.name, size: Theme.Size.avatar, fill: Theme.monogramFill)
        }
    }
}

/// One conversation's messages, oldest at the top; older pages load from the top.
struct MessagesThreadView: View {
    @State private var page: MessagesThreadModel
    private let title: String

    init(id: String, name: String) {
        _page = State(initialValue: MessagesThreadModel(chatID: id))
        title = name
    }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: Theme.Space.xs) {
                    if page.more {
                        Button {
                            let first = page.messages.first?.id
                            Task {
                                await page.loadOlder()
                                if let first { proxy.scrollTo(first, anchor: .top) }
                            }
                        } label: {
                            Label("更早的消息", systemImage: Theme.Icon.recent).font(Theme.Font.label).lineLimit(1)
                        }
                        .buttonStyle(.borderless)
                        .disabled(page.loading)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, Theme.Space.s)
                    }
                    if let f = page.failure {
                        Label(f.text, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger)
                            .frame(maxWidth: .infinity)
                    }
                    let lines = MessagesLine.lines(page.messages, group: page.chat?.isGroup ?? false,
                                                   services: (page.chat?.services.count ?? 0) > 1)
                    ForEach(lines) { line in
                        MessagesLineView(line: line).id(line.message.id)
                    }
                }
                .padding(.horizontal, Theme.Space.page)
                .padding(.vertical, Theme.Space.m)
            }
            .defaultScrollAnchor(.bottom)
        }
        .themedPage()
        .overlay {
            if !page.loaded {
                ProgressView()
            } else if page.messages.isEmpty && page.failure == nil {
                EmptyStateView(symbol: Theme.Icon.messagesApp, title: "没有消息", message: "这个会话里还没有能显示的消息。")
            }
        }
        .navigationTitle(page.chat?.name ?? title)
        .navigationBarTitleDisplayMode(.inline)
        .task { if !page.loaded { await page.load() } }
    }
}

/// A message with what goes above it: the time when there was a pause, who wrote it in a group, the service when it
/// changes (a person's iMessage and SMS chats are one conversation).
struct MessagesLine: Identifiable {
    var message: MessagesMessage
    var time: String?
    var sender: String?
    var service: String?

    var id: String { message.id }

    /// A gap this long (or a new day) shows the time again.
    static let pause: TimeInterval = 3600

    static func lines(_ messages: [MessagesMessage], group: Bool, services: Bool, now: Date = .now) -> [MessagesLine] {
        var out: [MessagesLine] = []
        var previous: MessagesMessage?
        var service: String?
        for m in messages {
            var line = MessagesLine(message: m)
            if let p = previous, m.at.timeIntervalSince(p.at) < pause, Calendar.current.isDate(m.at, inSameDayAs: p.at) {
                if group, !m.isMine, !m.isEvent, let who = m.sender?.shown, p.isMine || p.isEvent || p.sender?.handle != m.sender?.handle {
                    line.sender = who
                }
            } else {
                line.time = MessagesTime.separator(m.at, now: now)
                if group, !m.isMine, !m.isEvent { line.sender = m.sender?.shown }
            }
            if services, !m.isEvent, let s = m.service, !s.isEmpty, s != service {
                line.service = s == "SMS" ? "短信" : s
                service = s
            }
            out.append(line)
            previous = m
        }
        return out
    }
}

/// The time between messages: 今天 08:00, 昨天 21:30, 星期三 09:12, 10月21日 18:00, 2025年10月21日 18:00.
nonisolated enum MessagesTime {
    static func separator(_ d: Date, now: Date = .now) -> String {
        let cal = Calendar.current
        let time = d.formatted(.dateTime.hour(.twoDigits(amPM: .omitted)).minute(.twoDigits))
        if cal.isDateInToday(d) { return "今天 " + time }
        if cal.isDateInYesterday(d) { return "昨天 " + time }
        if now.timeIntervalSince(d) < 6 * 86400, d < now { return d.formatted(.dateTime.weekday(.wide)) + " " + time }
        if cal.isDate(d, equalTo: now, toGranularity: .year) { return d.formatted(.dateTime.month().day()) + " " + time }
        return d.formatted(.dateTime.year().month().day()) + " " + time
    }
}

struct MessagesLineView: View {
    var line: MessagesLine

    private var m: MessagesMessage { line.message }

    var body: some View {
        VStack(alignment: m.isMine ? .trailing : .leading, spacing: Theme.Space.xxs) {
            if let time = line.time {
                caption([time, line.service].compactMap { $0 }.joined(separator: " · ")).padding(.top, Theme.Space.m)
            } else if let service = line.service {
                caption(service).padding(.top, Theme.Space.s)
            }
            if m.isEvent || m.unsent == true {
                caption(eventText).padding(.vertical, Theme.Space.xs)
            } else {
                if let who = line.sender {
                    Text(who).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                        .padding(.horizontal, Theme.Space.m).padding(.top, Theme.Space.xs)
                }
                if let quote = m.replyTo, !quote.isEmpty {
                    HStack(spacing: Theme.Space.sm) {
                        Capsule().fill(.tertiary).frame(width: Theme.Size.rule)
                        Text(quote).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(2)
                    }
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, Theme.Space.m)
                }
                HStack {
                    if m.isMine { Spacer(minLength: Theme.Size.avatarXL) }
                    bubble
                    if !m.isMine { Spacer(minLength: Theme.Size.avatarXL) }
                }
                if m.edited == true {
                    Text("已编辑").font(Theme.Font.caption).foregroundStyle(.tertiary).padding(.horizontal, Theme.Space.m)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: m.isEvent || m.unsent == true ? .center : m.isMine ? .trailing : .leading)
    }

    private func caption(_ s: String) -> some View {
        Text(s).font(Theme.Font.caption).foregroundStyle(.secondary).multilineTextAlignment(.center)
            .frame(maxWidth: .infinity)
    }

    private var eventText: String {
        guard m.unsent == true, !m.isEvent else { return m.text ?? "" }
        return (m.isMine ? "你" : m.sender?.shown ?? "对方") + "撤回了一条消息"
    }

    private var bubble: some View {
        VStack(alignment: .leading, spacing: Theme.Space.sm) {
            ForEach(Array((m.attachments ?? []).enumerated()), id: \.offset) { _, a in
                MessagesAttachmentRow(attachment: a)
            }
            if m.undecoded == true {
                Text("这条消息的格式读不出来").font(Theme.Font.body).italic().foregroundStyle(.secondary)
            } else if let text = m.text, !text.isEmpty {
                Text(text).font(Theme.Font.body).lineHeight(.body).textSelection(.enabled)
            }
        }
        .foregroundStyle(m.isMine ? AnyShapeStyle(Theme.userBubbleText) : AnyShapeStyle(.primary))
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.s)
        .background(m.isMine ? Theme.userBubbleFill : AnyShapeStyle(Theme.Fill.chip),
                    in: .rect(cornerRadius: Theme.Radius.bubble, style: .continuous))
    }
}

/// An attachment as the Mac describes it: its file stays there.
struct MessagesAttachmentRow: View {
    var attachment: MessagesAttachment

    var body: some View {
        Label {
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(title).font(Theme.Font.labelStrong).lineLimit(1)
                if let size = attachment.size, size > 0 {
                    Text(size.formatted(.byteCount(style: .file))).font(Theme.Font.caption).opacity(0.8)
                }
            }
        } icon: {
            Image(systemName: symbol)
        }
    }

    private var title: String {
        switch attachment.kind {
        case "image": "图片" + (attachment.name.map { " · " + $0 } ?? "")
        case "video": "视频" + (attachment.name.map { " · " + $0 } ?? "")
        case "audio": "语音"
        default: attachment.name ?? "文件"
        }
    }

    private var symbol: String {
        switch attachment.kind {
        case "image": Theme.Icon.photo
        case "video": Theme.Icon.video
        case "audio": Theme.Icon.voiceMessage
        default: Theme.Icon.attachment
        }
    }
}
