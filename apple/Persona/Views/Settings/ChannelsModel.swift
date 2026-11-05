import Observation
import PersonaKit
import SwiftUI

// 设置 › 渠道 (docs/channels.md): every way of talking to Persona — the apps, the Feishu bot, WeChat, /agent in
// Feishu, voice, the chat app bots plugins contribute — as GET /api/v1/channels lists them; one channel's page (its
// switch, binding a step at a time, where its messages go, what it does, a test message); and the source of a message
// that came from one (its client_msg_id is `<channel>-<message id>`). Only with a hub whose features have `channels`.

extension APIClient {
    func channel(_ id: String) async throws -> HubChannelDetail { try await get("/api/v1/channels/\(id)") }

    /// Changes only what is given: the switch, where messages go, features by id.
    func updateChannel(_ id: String, enabled: Bool? = nil, target: ChannelTarget? = nil, features: [String: Bool]? = nil) async throws -> HubChannelDetail {
        struct Body: Encodable {
            var enabled: Bool?
            var target: ChannelTarget?
            var features: [String: Bool]?
        }
        let body = Body(enabled: enabled, target: target, features: features)
        return try JSON.decoder.decode(HubChannelDetail.self, from: try await request("PUT", "/api/v1/channels/\(id)", body: body))
    }

    /// One step of binding (the connectors' sign-in steps, docs/connectors.md): "" starts over.
    func bindChannel(_ id: String, step: String, values: [String: String]) async throws -> ConnectorPrompt {
        struct Body: Encodable { var step: String; var values: [String: String] }
        return try await post("/api/v1/channels/\(id)/bind", body: Body(step: step, values: values), timeout: 90)
    }

    func unbindChannel(_ id: String) async throws -> HubChannelDetail {
        try JSON.decoder.decode(HubChannelDetail.self, from: try await request("DELETE", "/api/v1/channels/\(id)/binding"))
    }

    /// A line to the owner only, to see the channel reaches them.
    func testChannel(_ id: String) async throws { _ = try await post("/api/v1/channels/\(id)/test", timeout: 40) }
}

/// What channels are called and say in the settings and the conversation.
enum ChannelText {
    static func state(_ c: HubChannel) -> String {
        switch c.state {
        case "connected": "已连接"
        case "error": "出错了"
        case "disabled": "已停用"
        case "not_installed": "没安装"
        default: "未连接"
        }
    }

    /// The dot beside the state: green connected, red broken, gray otherwise.
    static func tone(_ c: HubChannel?) -> Color {
        switch c?.state {
        case "connected": Theme.positive
        case "error": Theme.danger
        default: .secondary
        }
    }

    /// The row's second line: the error, the apps' ends, the account, else what it is.
    static func subtitle(_ c: HubChannel) -> String {
        if c.state == "error", let e = c.error, !e.isEmpty { return e }
        if c.isApp, !c.surfaces.isEmpty { return c.surfaces.joined(separator: " · ") }
        if let name = c.account?.name, !name.isEmpty, !c.isMarket { return name }
        return c.description ?? ""
    }

    struct Section: Identifiable, Equatable {
        var title: String
        var footer: String
        var channels: [HubChannel]
        var id: String { title }
    }

    /// The list's three sections (the empty ones left out): the apps, the installed ones, those a market has.
    static func sections(_ list: [HubChannel]) -> [Section] {
        [Section(title: "Persona", footer: "在 Persona 的哪一端说都进同一段对话，提问和批准在卡片上原地作答。",
                 channels: list.filter(\.isApp)),
         Section(title: "已接入", footer: "这些地方发给机器人的消息会交给 Persona，回答和 Agent 的提问发回原处。只认绑定的那一个人。",
                 channels: list.filter { !$0.isApp && !$0.isMarket }),
         Section(title: "可以安装", footer: "扩展市场里提供渠道的扩展：装好后在这里绑定。", channels: list.filter(\.isMarket))]
            .filter { !$0.channels.isEmpty }
    }

    /// 设置's 渠道 row: how many are connected, and how many went wrong.
    static func summary(_ list: [HubChannel]?) -> String {
        guard let list else { return "" }
        let on = list.filter { !$0.isApp && $0.state == "connected" }.count
        let bad = list.filter { $0.state == "error" }.count
        return bad > 0 ? "\(on) 个已连接 · \(bad) 个出错" : "\(on) 个已连接"
    }

    static func hasProblem(_ list: [HubChannel]?) -> Bool { list?.contains { $0.state == "error" } ?? false }

    static func target(_ t: ChannelTarget?) -> String {
        switch t?.kind {
        case "new": "新对话（下一条消息开）"
        case "conversation": t?.title?.isEmpty == false ? t!.title! : "一段对话"
        case "agent": t?.title?.isEmpty == false ? "Agent · " + t!.title! : "一个 Agent"
        default: "主对话"
        }
    }

    /// What the channel can do, in a few words a line.
    static func capabilities(_ c: HubChannel.Capabilities) -> [String] {
        var out: [String] = []
        let takes = ["文字", c.imagesIn == true ? "图片" : nil, c.filesIn == true ? "文件" : nil, c.voiceIn == true ? "语音" : nil]
        out.append("收" + takes.compactMap { $0 }.joined(separator: "、"))
        out.append(c.format == "plain" ? "回答是纯文字" : "回答带格式（Markdown）")
        if let n = c.maxText, n > 0 { out.append("一条最多 \(n) 字，长的拆开发") }
        if c.asks == "text" { out.append("提问和批准回复序号或文字") } else if c.asks == "buttons" { out.append("提问和批准点按钮") }
        if c.typing == true { out.append("处理时显示「正在输入」") }
        if c.progress == true { out.append("处理时显示进度") }
        return out
    }

    /// The bind section's heading and first button.
    static func bindTitle(_ c: HubChannel) -> String {
        switch c.bind {
        case "qr": "扫码绑定"
        case "code": "配对"
        default: "绑定"
        }
    }

    /// The settings page a channel's `settings_url` opens; nil for one this app does not know.
    static func page(_ url: String?) -> SettingsPage? {
        guard let url, url.hasPrefix("settings/") else { return nil }
        let rest = url.dropFirst("settings/".count)
        switch rest {
        case "connectors/feishu-bot": return .feishuBot
        case "connectors/feishu": return .feishu
        case "model-config": return .modelConfiguration
        default:
            // settings/extensions/<id>, or settings/plugins/<id> as hubs from before the rename write it (#1844).
            for prefix in ["extensions/", "plugins/"] where rest.hasPrefix(prefix) {
                let id = String(rest.dropFirst(prefix.count))
                if !id.isEmpty, !id.contains("/") { return .plugin(id) }
            }
            if rest.hasPrefix("connectors/"), case let id = String(rest.dropFirst("connectors/".count)), !id.isEmpty, !id.contains("/") {
                return .connector(id)
            }
            return nil
        }
    }

    /// The symbol of a channel without a brand's mark, by its kind.
    static func symbol(_ c: HubChannel) -> String {
        switch c.kind {
        case "app": Theme.Icon.ai
        case "voice": Theme.Icon.voiceChannel
        case "mention": Theme.Icon.mention
        default: Theme.Icon.chatBot
        }
    }

    /// A channel's brand mark (`Theme.Brand`): Feishu's for the Feishu bot and /agent; nil for the rest (WeChat's mark
    /// and its terms are undecided, as on the web).
    static func brand(_ id: String) -> String? {
        switch id {
        case "feishu", "lark_agent": Theme.Brand.lark
        case "telegram": Theme.Brand.telegram
        default: nil
        }
    }
}

/// The hub's channels as the conversations need them (names and marks for 「来自 …」), fetched once per hub and again
/// when the settings change one. Empty on a hub without `channels`.
@Observable final class ChannelDirectory {
    static let shared = ChannelDirectory()

    private(set) var list: [HubChannel] = []
    @ObservationIgnored private var key: String?
    @ObservationIgnored private var inflight: Task<Void, Never>?

    /// Loads the list for this hub once (a hub without `channels`: nothing, the marks never show).
    func ensure(_ api: APIClient?, features: Set<String>) {
        guard let api, features.contains("channels") else {
            if !list.isEmpty { list = [] }
            key = nil
            return
        }
        let k = api.config.url.absoluteString + "|" + api.group
        guard key != k, inflight == nil else { return }
        inflight = Task { @MainActor in
            defer { inflight = nil }
            guard let l = try? await api.channels() else { return }
            list = l
            key = k
        }
    }

    /// The settings changed a channel (or one came or went): taken again next time.
    func forget() { key = nil }

    func update(_ l: [HubChannel]) { list = l }
}

/// 设置 › 渠道: the list.
@Observable final class ChannelsModel {
    struct Deps {
        var api: () -> APIClient?
        static var live: Deps { .init(api: { AppModel.shared.api }) }
    }

    private(set) var channels: [HubChannel]?
    private(set) var loading = false
    private(set) var error: String?
    @ObservationIgnored private let deps: Deps

    init(deps: Deps = .live) { self.deps = deps }

    var sections: [ChannelText.Section] { ChannelText.sections(channels ?? []) }

    func load() async {
        guard !loading else { return }
        loading = true
        defer { loading = false }
        do {
            guard let api = deps.api() else { throw APIError(status: 0, message: "还没连接 hub") }
            let l = try await api.channels()
            channels = l
            ChannelDirectory.shared.update(l)
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}

/// 设置 › 渠道 › one: its switch, where its messages go, its features, unbinding and a test message. Binding a
/// step at a time is `ConnectorSignInModel` on the channel's bind (`ChannelModel.binding`).
@Observable final class ChannelModel {
    struct Deps {
        var api: () -> APIClient?
        static var live: Deps { .init(api: { AppModel.shared.api }) }
    }

    let id: String
    private(set) var detail: HubChannelDetail?
    private(set) var loading = false
    private(set) var busy = false
    private(set) var error: String?
    /// What the test message did, for people.
    private(set) var testResult: String?
    @ObservationIgnored private let deps: Deps

    init(id: String, deps: Deps = .live) {
        self.id = id
        self.deps = deps
    }

    var channel: HubChannel? { detail?.channel }

    /// The sign-in steps' deps for this channel's binding: the same steps as a connector's, on POST …/bind.
    static func bindingDeps(_ deps: Deps, report: @escaping (Error) -> Void) -> ConnectorDeps {
        ConnectorDeps(api: deps.api, report: report, login: { api, id, step, values in
            try await api.bindChannel(id, step: step, values: values)
        })
    }

    func load(quietly: Bool = false) async {
        guard !loading else { return }
        if !quietly { loading = true }
        defer { loading = false }
        await run { api in try await api.channel(self.id) }
    }

    func setEnabled(_ on: Bool) async {
        await change { api in try await api.updateChannel(self.id, enabled: on) }
    }

    func setTarget(_ t: ChannelTarget) async {
        await change { api in try await api.updateChannel(self.id, target: t) }
    }

    func setFeature(_ fid: String, on: Bool) async {
        await change { api in try await api.updateChannel(self.id, features: [fid: on]) }
    }

    func unbind() async {
        await change { api in try await api.unbindChannel(self.id) }
    }

    func test() async {
        guard let api = deps.api() else { return }
        busy = true
        defer { busy = false }
        do {
            try await api.testChannel(id)
            testResult = "发出去了：去\(channel?.name ?? "那边")看看收到没有。"
        } catch {
            testResult = "没发出去：" + (error.shown ?? error.localizedDescription)
        }
    }

    private func change(_ call: @escaping (APIClient) async throws -> HubChannelDetail) async {
        busy = true
        defer { busy = false }
        if await run(call) { ChannelDirectory.shared.forget() }
    }

    @discardableResult private func run(_ call: @escaping (APIClient) async throws -> HubChannelDetail) async -> Bool {
        do {
            guard let api = deps.api() else { throw APIError(status: 0, message: "还没连接 hub") }
            detail = try await call(api)
            error = nil
            return true
        } catch {
            self.error = error.shown ?? error.localizedDescription
            return false
        }
    }
}
