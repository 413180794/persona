import Security
import SwiftUI

// The Dynamic Island is an iPhone's: a Mac has no island to diagnose.
#if os(iOS)

/// 灵动岛诊断 (hidden: touch and hold the first preview in 设置 › 通知 › 灵动岛): the steps between 设置 › 形象 and the
/// island, for a phone that draws the wrong persona — what the app saved for the widget extension, what the shared
/// keychain group holds, what the extension last read (docs/live-activity.md「诊断」). 「复制全部」 is what to send.
struct IslandDiagnosticsView: View {
    @Environment(AppModel.self) private var model
    @State private var sections: [IslandDiagnostics.Section] = []

    var body: some View {
        List {
            ForEach(sections) { section in
                Section(section.title) {
                    ForEach(section.rows) { row in
                        if row.wide {
                            Text(row.value).font(Theme.Font.body).textSelection(.enabled)
                        } else {
                            LabeledContent {
                                Text(row.value).multilineTextAlignment(.trailing).textSelection(.enabled)
                            } label: {
                                Text(row.label).lineLimit(1)
                            }
                            .font(Theme.Font.secondary)
                        }
                    }
                }
            }
            Section {
                Button {
                    LiveActivityController.shared.look(model.fleetLook, force: true) { model.fleetFaces }
                    reload()
                } label: {
                    Label("重新写入并重画", systemImage: Theme.Icon.retry).lineLimit(1)
                }
                Button {
                    UIPasteboard.general.string = IslandDiagnostics.text(sections)
                    model.toast = Toast("已复制", style: .success)
                } label: {
                    Label("复制全部", systemImage: Theme.Icon.copy).lineLimit(1)
                }
            } footer: {
                Text("「扩展最近读到」在灵动岛每次重画后更新（同样的内容最多十分钟写一次）。看不到它时，扩展可能读不了钥匙串，这时只有系统日志里有它的记录（控制台，子系统 jarvis，类别 island）。")
            }
        }
        .themedPage()
        .navigationTitle("灵动岛诊断")
        .navigationBarTitleDisplayMode(.inline)
        .task { reload() }
        .refreshable { reload() }
    }

    private func reload() {
        let (items, listed) = FleetKeychain.items()
        var input = IslandDiagnostics.Input(look: model.fleetLook, group: FleetKeychain.group, writes: FleetKeychain.lastWrites,
                                            items: items, listed: listed, seen: FleetLookReport.seen)
        input.activity = LiveActivityController.shared.runningLook
        sections = IslandDiagnostics.sections(input, now: .now)
        Task {
            guard let api = model.api else { return }
            do { input.hub = .some(try await api.fleetLook(device: PushRegistrar.deviceID)) } catch { return }
            sections = IslandDiagnostics.sections(input, now: .now)
        }
    }
}

/// The page's content, worked out apart from the keychain so it can be tested.
nonisolated enum IslandDiagnostics {
    struct Row: Identifiable, Hashable {
        var label: String
        var value: String
        /// A sentence on its own (the verdict), not a value beside a label.
        var wide = false
        var id: String { label }
    }

    struct Section: Identifiable, Hashable {
        var title: String
        var rows: [Row]
        var id: String { title }
    }

    /// What the page is made from.
    struct Input {
        var look: FleetLook
        var group: String?
        var writes: [String: (at: Date, status: OSStatus, bytes: Int?)] = [:]
        var items: [FleetKeychain.Item] = []
        var listed: OSStatus = errSecItemNotFound
        var seen: FleetLookReport?
        /// The running activity's content: nil without an activity, .some(nil) when its content carries no look.
        var activity: FleetLook??
        /// What the hub keeps for this device: nil until asked, .some(nil) when it has none (an older hub, or not told).
        var hub: FleetLook??
    }

    static func sections(_ i: Input, now: Date) -> [Section] {
        let accounts = [FleetLookStore.lookAccount, FleetLookStore.facesAccount]
        let current = FleetLookReport.describe(i.look)
        func shown(_ l: FleetLook??, none: String, missing: String) -> String {
            switch l { case .none: none; case .some(.none): missing; case .some(.some(let l)): FleetLookReport.describe(l) }
        }
        var out = [Section(title: "结论", rows: [Row(label: "看起来", value: verdict(i), wide: true)])]
        out.append(Section(title: "形象", rows: [
            Row(label: "设置里", value: current),
            Row(label: "颜色", value: String(format: "%06X / %06X", i.look.light, i.look.dark)),
            Row(label: "hub 记下的", value: shown(i.hub, none: "还没问到", missing: "没有（hub 太旧，或还没告诉它）")),
            Row(label: "实时活动的内容里", value: shown(i.activity, none: "没有正在显示的实时活动", missing: "没有")),
        ]))
        out.append(Section(title: "App 这次启动写进钥匙串", rows: [Row(label: "钥匙串组", value: i.group ?? "无：安装包的 Info.plist 里没有组名")]
            + accounts.map { a in
                guard let w = i.writes[a] else { return Row(label: a, value: "还没写过（和已存的一样时不写）") }
                return Row(label: a, value: [describe(w.status), w.bytes.map { "\($0) 字节" }, time(w.at, now)].compactMap { $0 }.joined(separator: " · "))
            }))
        let listedRows = i.items.isEmpty
            ? [Row(label: "条目", value: i.listed == errSecSuccess || i.listed == errSecItemNotFound ? "没有" : describe(i.listed))]
            : i.items.map { Row(label: "\($0.account) · \($0.group)", value: "\($0.bytes) 字节" + ($0.modified.map { " · " + time($0, now) } ?? "")) }
        out.append(Section(title: "钥匙串里", rows: listedRows))
        if let seen = i.seen {
            out.append(Section(title: "扩展最近一次", rows: [
                Row(label: "时间", value: time(seen.at, now)),
                Row(label: "画的形象", value: seen.content ?? "内容里没有形象"),
                Row(label: "钥匙串里的形象", value: [seen.lookStatus.map(describe), seen.design, seen.package].compactMap { $0 }.joined(separator: " · ")),
                Row(label: "形象包的图", value: seen.facesStatus.map { s in
                    [describe(s), seen.facesBytes.map { "\($0) 字节" }, seen.facesPackage].compactMap { $0 }.joined(separator: " · ")
                } ?? "没读（内置形象）"),
            ]))
        } else {
            out.append(Section(title: "扩展最近一次", rows: [Row(label: "报告", value: "没有")]))
        }
        return out
    }

    /// The first step that does not hold, in the order the look travels: the hub and the activity's content (what
    /// the island draws), then the keychain (a package's pictures).
    static func verdict(_ i: Input) -> String {
        if case .some(let hub) = i.hub, hub != i.look {
            return hub == nil ? "hub 没有这台手机的形象：hub 要更新，或 App 还没告诉它（推送里不会带形象）" : "hub 记下的不是设置里的形象（App 告诉它时失败了）"
        }
        if case .some(let content) = i.activity, content != i.look { return "正在显示的实时活动内容里不是设置里的形象（还没重画）" }
        if let w = i.writes[FleetLookStore.facesAccount], w.status != errSecSuccess { return "App 把形象包的图写进钥匙串失败：\(describe(w.status))" }
        guard let seen = i.seen else {
            return i.look.package == nil ? "形象随实时活动的内容送到；扩展没留下报告，可能读写不了钥匙串（内置形象不受影响）"
                : "扩展没留下报告：它可能读写不了钥匙串，形象包的图就画不出来（画默认形象换成它的颜色）"
        }
        if seen.content != FleetLookReport.describe(i.look) { return "扩展上次画的不是设置里的形象（还没重画过）" }
        if i.look.package != nil, seen.facesStatus != errSecSuccess || seen.facesPackage != i.look.package {
            return "扩展读不到这个形象包的图（\(seen.facesStatus.map(describe) ?? "没读")），画的是默认形象换成它的颜色"
        }
        return "扩展画的和设置一致"
    }

    /// An OSStatus as words.
    static func describe(_ status: OSStatus) -> String {
        switch status {
        case errSecSuccess: "成功"
        case errSecItemNotFound: "没有这一条"
        case errSecMissingEntitlement: "签名里没有这个钥匙串组（\(status)）"
        case errSecInteractionNotAllowed: "设备锁着，读不了（\(status)）"
        case errSecDataNotAvailable: "写入后读回来不一样（\(status)）"
        case errSecDuplicateItem: "已经有一条（\(status)）"
        default: "错误 \(status)"
        }
    }

    static func time(_ date: Date, _ now: Date) -> String {
        let s = Int(now.timeIntervalSince(date))
        return s < 60 ? "\(max(s, 0)) 秒前" : s < 3600 ? "\(s / 60) 分钟前" : date.formatted(date: .abbreviated, time: .standard)
    }

    /// Everything on the page as text, to paste into a message.
    static func text(_ sections: [Section]) -> String {
        sections.map { s in ([s.title] + s.rows.map { "  \($0.label)：\($0.value)" }).joined(separator: "\n") }.joined(separator: "\n")
    }
}
#endif
