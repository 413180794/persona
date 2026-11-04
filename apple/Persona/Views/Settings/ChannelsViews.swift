import BeautifulUI
import SwiftUI

// 设置 › 插件 › 渠道 (docs/channels.md「界面」): the list and one channel's page; the 渠道 row on 设置 › 插件; the line on a
// connector's page that says it is a channel; the 「来自 …」 under a message and the mark beside a conversation's title.

/// A channel's mark: its brand's (飞书), else a symbol of its kind in the accent color.
struct ChannelMark: View {
    var channel: HubChannel
    var side: CGFloat

    var body: some View {
        if let brand = ChannelText.brand(channel.id) {
            BrandMark(brand, side: side)
        } else {
            Image(systemName: ChannelText.symbol(channel)).resizable().scaledToFit().foregroundStyle(Theme.accent)
                .scaledFrame(width: side, height: side)
                .accessibilityHidden(true)
        }
    }
}

/// The dot beside a channel's state.
private struct ChannelStateDot: View {
    var channel: HubChannel?

    var body: some View {
        Circle().fill(ChannelText.tone(channel)).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
    }
}

/// 设置 › 插件's first row: 渠道, how many are connected (red when one went wrong). Only with a hub that has channels.
struct ChannelsEntryRow: View {
    @Environment(AppModel.self) private var model
    @State private var channels: [HubChannel]?

    var body: some View {
        if model.hubFeatures.contains("channels") {
            Section {
                Group {
                    NavigationLink(value: SettingsPage.channels) {
                        if AppStyle.current.usesBeautifulUI {
                            SettingsRow(title: "渠道", symbol: Theme.Icon.channels, tint: Theme.accent, value: ChannelText.summary(channels), look: .oneLine)
                        } else {
                            LabeledContent {
                                Text(ChannelText.summary(channels)).lineLimit(1)
                                    .foregroundStyle(ChannelText.hasProblem(channels) ? Theme.danger : Color.secondary)
                            } label: {
                                Label { Text("渠道").lineLimit(1) } icon: { Image(systemName: Theme.Icon.channels) }
                            }
                        }
                    }
                    .accessibilityIdentifier("plugins-channels")
                }
                .bareListRow()
            } footer: {
                Text("在哪儿能和 Persona 对话：Persona 自己的各端、微信、飞书机器人……")
            }
            .task(id: model.settingsPath.count) { // again when a channel's page closes
                if let l = try? await model.api?.channels() {
                    channels = l
                    ChannelDirectory.shared.update(l)
                }
            }
        }
    }
}

/// 设置 › 插件 › 渠道: every way of talking to Persona, in three sections (the apps, the installed, a market's).
struct ChannelsView: View {
    @State private var page = ChannelsModel()

    var body: some View {
        SettingsForm {
            if let list = page.channels, list.isEmpty {
                Section {
                    EmptyStateView(symbol: Theme.Icon.channels, title: "没有渠道", message: "这个 hub 还没列出任何渠道。")
                        .listRowBackground(Color.clear)
                }
            }
            ForEach(page.sections) { section in
                Section {
                    Group {
                        ForEach(section.channels) { c in
                            NavigationLink(value: Self.destination(c)) { ChannelRowLabel(channel: c) }
                                .accessibilityIdentifier("channel-\(c.id)")
                        }
                    }
                    .bareListRow()
                } header: {
                    SettingsHeading(section.title)
                } footer: {
                    Text(section.footer)
                }
            }
            if let error = page.error {
                Section { Text(error).font(Theme.Font.label).foregroundStyle(Theme.danger).textSelection(.enabled) }
            }
        }
        .themedPage()
        .overlay { if page.channels == nil && page.loading { ProgressView() } }
        .detailTitle("渠道")
        .navigationBarTitleDisplayMode(.inline)
        .task { await page.load() }
        .refreshable { await page.load() }
    }

    /// A market's opens its page in the plugin market; the rest their own.
    static func destination(_ c: HubChannel) -> SettingsPage {
        if c.isMarket {
            if let market = c.market, let plugin = c.plugin { return .marketPlugin(market: market, id: plugin) }
            return .pluginMarket
        }
        return .channel(c.id)
    }
}

/// A channel's row: its mark, name and account (or what it is), its state on the right.
private struct ChannelRowLabel: View {
    var channel: HubChannel

    var body: some View {
        let subtitle = ChannelText.subtitle(channel)
        if AppStyle.current.usesBeautifulUI {
            BUIListRow(channel.name, detail: subtitle.isEmpty ? nil : subtitle, value: ChannelText.state(channel), detailLines: 1) {
                if let brand = ChannelText.brand(channel.id) {
                    SettingsBrandMark(asset: brand)
                } else {
                    BUIIconTile(ChannelText.symbol(channel), tint: Theme.accent)
                }
            } trailing: {
                ChannelStateDot(channel: channel)
            }
        } else {
            HStack(spacing: Theme.Space.m) {
                ChannelMark(channel: channel, side: Theme.Size.icon)
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(channel.name).lineLimit(1)
                    if !subtitle.isEmpty {
                        Text(subtitle).font(Theme.Font.label)
                            .foregroundStyle(channel.state == "error" ? Theme.danger : Color.secondary).lineLimit(1)
                    }
                }
                Spacer(minLength: Theme.Space.s)
                HStack(spacing: Theme.Space.sm) {
                    ChannelStateDot(channel: channel)
                    Text(ChannelText.state(channel)).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1)
                }
            }
            .padding(.vertical, Theme.Space.xxs)
        }
    }
}

/// 设置 › 插件 › 渠道 › one: its state, switch, binding, where its messages go, its features, what waits on it, what it
/// can do, a test message, and its own page if it has one. A read-only one (the apps, /agent) is only looked at.
struct ChannelDetailView: View {
    @Environment(AppModel.self) private var model
    @State private var page: ChannelModel
    @State private var binding: ConnectorSignInModel
    @State private var confirmUnbind = false

    init(id: String) {
        let page = ChannelModel(id: id)
        _page = State(initialValue: page)
        _binding = State(initialValue: ConnectorSignInModel(id: id, deps: ChannelModel.bindingDeps(.live, report: { AppModel.shared.show($0) })))
    }

    var body: some View {
        SettingsForm {
            header
            if let c = page.channel {
                if c.isMarket {
                    market(c)
                } else if c.editable && !c.isApp {
                    enabled(c)
                }
                if c.bindsHere { bind(c) }
                if c.editable, c.kind == "im", c.target != nil { target(c) }
                if c.editable, !c.features.isEmpty { features(c) }
                if let d = page.detail { activity(d) }
                if c.isApp, !c.surfaces.isEmpty { surfaces(c) }
                if !c.isMarket { capabilities(c) }
                if c.editable, c.isBound, c.enabled { test(c) }
                more(c)
            }
        }
        .themedPage()
        .detailTitle(page.channel?.name ?? "渠道")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: model.config) { await page.load() }
        .refreshable { await page.load() }
        .confirmationDialog("解除和\(page.channel?.name ?? "它")的绑定？", isPresented: $confirmUnbind, titleVisibility: .visible) {
            Button("解除绑定", role: .destructive) { Task { await page.unbind() } }
        } message: {
            Text("解除后它不再把消息交给 Persona，在等的回答和提问一起清掉。再用要重新绑定。")
        }
    }

    // MARK: sections

    @ViewBuilder private var header: some View {
        Section {
            HStack(spacing: Theme.Space.m) {
                if let c = page.channel {
                    ChannelMark(channel: c, side: Theme.Size.wellM)
                } else {
                    Image(systemName: Theme.Icon.channels).resizable().scaledToFit().foregroundStyle(Theme.accent)
                        .scaledFrame(width: Theme.Size.wellM, height: Theme.Size.wellM)
                }
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(page.channel?.name ?? page.id).font(Theme.Font.heading).lineLimit(1)
                    HStack(spacing: Theme.Space.sm) {
                        ChannelStateDot(channel: page.channel)
                        Text(stateText).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(2)
                    }
                }
                Spacer(minLength: Theme.Space.s)
                if page.loading && page.detail == nil { ProgressView() }
            }
            .padding(.vertical, Theme.Space.xs)
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("channel-header")
            if let c = page.channel {
                if let text = c.description, !text.isEmpty {
                    Text(text).font(Theme.Font.secondary).foregroundStyle(.secondary)
                }
                if let at = c.lastEventAt, at.timeIntervalSince1970 > 0 {
                    LabeledContent("最近一条消息") {
                        TimelineView(.periodic(from: .now, by: 30)) { ctx in
                            Text(Fmt.relative(at, now: ctx.date)).lineLimit(1)
                        }
                    }
                }
                if let e = c.error, !e.isEmpty, c.state != "connected" {
                    // Not bound yet is a sentence, not something broken: red only for an error.
                    Text(e).font(Theme.Font.label).foregroundStyle(c.state == "error" ? Theme.danger : Color.secondary)
                        .textSelection(.enabled)
                        .accessibilityIdentifier("channel-error")
                }
            }
            if let e = page.error, !e.isEmpty {
                Text(e).font(Theme.Font.label).foregroundStyle(Theme.danger).textSelection(.enabled)
            }
        } footer: {
            if let c = page.channel, c.readOnly, !c.isApp {
                Text("这个渠道在别处设置（hub.json 或它自己的页面），这里只看状态。")
            }
        }
    }

    private var stateText: String {
        guard let c = page.channel else { return page.loading ? "正在读取…" : "读不到" }
        var parts = [ChannelText.state(c)]
        if let a = c.account?.name, !a.isEmpty, !c.isMarket { parts.append(a) }
        return parts.joined(separator: " · ")
    }

    private func market(_ c: HubChannel) -> some View {
        Section {
            NavigationLink(value: ChannelsView.destination(c)) {
                Label { Text("去插件市场安装").lineLimit(1) } icon: { Image(systemName: Theme.Icon.pluginMarket) }
            }
            .accessibilityIdentifier("channel-install")
        } footer: {
            Text("装好以后回到这里绑定。")
        }
    }

    private func enabled(_ c: HubChannel) -> some View {
        Section {
            Toggle(isOn: Binding(get: { c.enabled }, set: { on in Task { await page.setEnabled(on) } })) {
                Text("启用").lineLimit(1)
            }
            .disabled(page.busy)
            .accessibilityIdentifier("channel-enabled")
        } footer: {
            Text(c.enabled ? "关掉后它不再把消息交给 Persona；绑定和设置留着。" : "打开后才能绑定和收消息。")
        }
    }

    @ViewBuilder private func bind(_ c: HubChannel) -> some View {
        if c.isBound {
            Section {
                LabeledContent("绑定的账号") { Text(c.account?.name ?? "已绑定").lineLimit(1) }
                Button(role: .destructive) { confirmUnbind = true } label: { Text("解除绑定").lineLimit(1) }
                    .disabled(page.busy)
                    .accessibilityIdentifier("channel-unbind")
            } header: {
                SettingsHeading("绑定")
            } footer: {
                Text("只有绑定的这个人发给机器人的消息会交给 Persona；别人发来只回一句说明，群里一律不处理。")
            }
        } else if c.enabled {
            ConnectorSignInSteps(name: c.name, model: binding, reload: { await page.load(quietly: true) },
                                 footer: bindFooter(c), heading: "绑定", start: ChannelText.bindTitle(c))
        }
    }

    private func bindFooter(_ c: HubChannel) -> String {
        switch c.bind {
        case "qr": "用\(c.name)扫码，扫码的人就是这个渠道的主人；登录状态加密存在 hub 上，解除绑定时删掉。"
        case "code": "把配对码发给机器人，发码的人就是这个渠道的主人。"
        default: "凭据加密存在 hub 上，只交给这个渠道；之后可能还要配对一次，配对的人就是这个渠道的主人。"
        }
    }

    // 消息发到: the main conversation, a new one (opened by the next message), or one of the assistant's.

    private func target(_ c: HubChannel) -> some View {
        Section {
            Picker(selection: Binding(get: { Self.tag(c.target) }, set: { tag in
                guard let t = targetFor(tag), tag != Self.tag(c.target) else { return }
                Task { await page.setTarget(t) }
            })) {
                Text("主对话").tag("main")
                Text("新对话（下一条消息开）").tag("new")
                ForEach(choices(c)) { s in
                    Text(s.title).lineLimit(1).tag(s.tag)
                }
                if c.target?.kind == "agent" { Text(ChannelText.target(c.target)).lineLimit(1).tag("agent") }
            } label: {
                Text("消息发到").lineLimit(1)
            }
            .disabled(page.busy)
            .accessibilityIdentifier("channel-target")
        } footer: {
            Text(c.target?.kind == "agent" ? "现在发给在飞书里用 /agents 选的 Agent；在这里改回 Persona 的对话也行。"
                 : "在\(c.name)里也能用 /new 开新对话、/main 回主对话。")
        }
    }

    private struct Choice: Identifiable {
        var tag: String
        var title: String
        var id: String { tag }
    }

    /// The assistant's recent conversations (the main one is 主对话), with the current one if it is not among them.
    private func choices(_ c: HubChannel) -> [Choice] {
        let main = model.assistantMain
        var out = model.assistantConversations.filter { $0.key != main }.prefix(15).map { s in
            Choice(tag: "c:" + s.machineID + "/" + s.sessionID, title: s.title.isEmpty ? "新对话" : s.title)
        }
        if let t = c.target, t.kind == "conversation", case let tag = Self.tag(t), !out.contains(where: { $0.tag == tag }) {
            out.insert(Choice(tag: tag, title: ChannelText.target(t)), at: 0)
        }
        return Array(out)
    }

    private static func tag(_ t: ChannelTarget?) -> String {
        switch t?.kind {
        case "new": "new"
        case "conversation": "c:" + (t?.machineID ?? "") + "/" + (t?.sessionID ?? "")
        case "agent": "agent"
        default: "main"
        }
    }

    private func targetFor(_ tag: String) -> ChannelTarget? {
        switch tag {
        case "main": return .main
        case "new": return .new
        default:
            guard tag.hasPrefix("c:"), let slash = tag.firstIndex(of: "/") else { return nil }
            let machine = String(tag[tag.index(tag.startIndex, offsetBy: 2)..<slash])
            let session = String(tag[tag.index(after: slash)...])
            return ChannelTarget(kind: "conversation", machineID: machine, sessionID: session)
        }
    }

    private func features(_ c: HubChannel) -> some View {
        Section {
            ForEach(c.features) { f in
                Toggle(isOn: Binding(get: { f.on }, set: { on in Task { await page.setFeature(f.id, on: on) } })) {
                    VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                        Text(f.title).lineLimit(1)
                        if let d = f.description, !d.isEmpty {
                            Text(d).font(Theme.Font.label).foregroundStyle(.secondary)
                        }
                    }
                }
                .disabled(page.busy)
                .accessibilityIdentifier("channel-feature-\(f.id)")
            }
        } header: {
            SettingsHeading("功能")
        }
    }

    /// What waits on it, and the conversations its messages went to lately (tapped: opened).
    @ViewBuilder private func activity(_ d: HubChannelDetail) -> some View {
        if d.waiting.answers > 0 || d.waiting.asks > 0 || !d.conversations.isEmpty {
            Section {
                if d.waiting.answers > 0 {
                    LabeledContent("在等的回答") { Text("\(d.waiting.answers) 个").monospacedDigit() }
                }
                if d.waiting.asks > 0 {
                    LabeledContent("发到这里的提问") { Text("\(d.waiting.asks) 个").monospacedDigit() }
                }
                ForEach(d.conversations) { conv in
                    Button {
                        model.openAssistantConversation(SessionKey(group: model.hub?.group ?? "", machineID: conv.machineID, sessionID: conv.sessionID))
                    } label: {
                        LabeledContent {
                            if let at = conv.at { Text(Fmt.relative(at)).lineLimit(1) }
                        } label: {
                            Label { Text(conv.title?.isEmpty == false ? conv.title! : "一段对话").lineLimit(1) } icon: {
                                Image(systemName: Theme.Icon.conversation)
                            }
                        }
                    }
                    .foregroundStyle(Color.primary)
                }
            } header: {
                SettingsHeading("最近")
            }
        }
    }

    private func surfaces(_ c: HubChannel) -> some View {
        Section {
            Text(c.surfaces.joined(separator: "、")).font(Theme.Font.body)
        } header: {
            SettingsHeading("这些地方都能用")
        }
    }

    private func capabilities(_ c: HubChannel) -> some View {
        Section {
            ForEach(ChannelText.capabilities(c.capabilities), id: \.self) { line in
                Text(line).font(Theme.Font.body)
            }
        } header: {
            SettingsHeading("能做什么")
        }
    }

    private func test(_ c: HubChannel) -> some View {
        Section {
            Button {
                Task { await page.test() }
            } label: {
                Text(page.busy ? "正在发…" : "发一条测试消息").lineLimit(1)
            }
            .disabled(page.busy)
            .accessibilityIdentifier("channel-test")
        } footer: {
            Text(page.testResult ?? "只发给绑定的你自己，看看\(c.name)能不能收到。")
        }
    }

    @ViewBuilder private func more(_ c: HubChannel) -> some View {
        if let dest = ChannelText.page(page.detail?.settingsURL) {
            Section {
                NavigationLink(value: dest) {
                    Label { Text(moreTitle(dest)).lineLimit(1) } icon: { Image(systemName: Theme.Icon.options) }
                }
                .accessibilityIdentifier("channel-more")
            }
        }
    }

    private func moreTitle(_ dest: SettingsPage) -> String {
        switch dest {
        case .modelConfiguration: "模型配置"
        case .plugin: "插件设置"
        default: "更多设置"
        }
    }
}

/// The line at the top of a connector's page that is a channel too (the Feishu bot, WeChat): where it is set up as one.
struct ChannelLinkSection: View {
    @Environment(AppModel.self) private var model
    var channelID: String

    var body: some View {
        if model.hubFeatures.contains("channels") {
            Section {
                NavigationLink(value: SettingsPage.channel(channelID)) {
                    Label {
                        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                            Text("这是一个对话渠道").lineLimit(1)
                            Text("在 设置 › 插件 › 渠道 里设置").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                        }
                    } icon: {
                        Image(systemName: Theme.Icon.channels).foregroundStyle(Theme.accent)
                    }
                }
                .accessibilityIdentifier("connector-channel-link")
            }
        }
    }
}

/// 「来自 微信」 under a message that came in from a channel (its client_msg_id is `<channel>-…`, docs/channels.md
/// 「来源标记」); nothing for the app's own, or on a hub without channels. The names come from `ChannelDirectory`, which
/// the root view keeps loaded (`.loadsChannels()`): this view is empty until then, and an empty view runs no task.
struct ChannelSourceLine: View {
    var clientMsgID: String?

    var body: some View {
        if let c = ChannelSource.of(clientMsgID, in: ChannelDirectory.shared.list) {
            HStack(spacing: Theme.Space.xs) {
                ChannelMark(channel: c, side: Theme.Size.markS)
                Text("来自 \(c.name)").font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
            }
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("message-channel")
        }
    }
}

extension View {
    /// Keeps `ChannelDirectory` loaded for the hub on screen: once it says it has channels, again for another hub.
    func loadsChannels() -> some View { modifier(ChannelDirectoryLoader()) }
}

private struct ChannelDirectoryLoader: ViewModifier {
    @Environment(AppModel.self) private var model

    func body(content: Content) -> some View {
        let has = model.hubFeatures.contains("channels")
        content.task(id: "\(has)|\(model.api?.config.url.absoluteString ?? "")|\(model.api?.group ?? "")") {
            ChannelDirectory.shared.ensure(model.api, features: model.hubFeatures)
        }
    }
}

/// The latest channel a conversation had messages from (`Session.channels`), small beside its title in the sidebar.
struct ConversationChannelMark: View {
    var ids: [String]?

    var body: some View {
        if let id = ids?.first {
            let c = ChannelDirectory.shared.list.first { $0.id == id } ?? HubChannel(id: id, name: id)
            ChannelMark(channel: c, side: Theme.Size.markS)
                .foregroundStyle(.secondary)
                .accessibilityLabel("来自 \(c.name)")
        }
    }
}
