import SwiftUI

// The Dynamic Island is an iPhone's: a Mac has no island to diagnose.
#if os(iOS)

/// 灵动岛诊断 (hidden: touch and hold the first preview in 设置 › 通知 › 灵动岛): the steps between 设置 › 形象 and the
/// island, for a phone that draws the wrong persona — what the app saved for the widget extension, what the shared
/// App Group container holds, what the extension last read (docs/live-activity.md「诊断」). 「复制全部」 is what to send.
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
                Button(action: redraw) {
                    Label("重新写入并重画", systemImage: Theme.Icon.retry).lineLimit(1)
                }
                Button {
                    UIPasteboard.general.string = IslandDiagnostics.text(sections)
                    model.toast = Toast(String(localized: "已复制"), style: .success)
                } label: {
                    Label("复制全部", systemImage: Theme.Icon.copy).lineLimit(1)
                }
            } footer: {
                Text("「扩展最近一次」在灵动岛每次重画后更新（同样的内容最多十分钟写一次）。看不到它时，扩展可能用不了 App Group，这时只有系统日志里有它的记录（控制台，子系统 jarvis，类别 island）。")
            }
        }
        .themedPage()
        .navigationTitle("灵动岛诊断")
        .navigationBarTitleDisplayMode(.inline)
        .task { reload() }
        .refreshable { reload() }
    }

    private func redraw() {
        LiveActivityController.shared.look(model.fleetLook, force: true) { model.fleetFaces }
        reload()
    }

    private func reload() {
        let store = FleetLookStore.shared
        var input = IslandDiagnostics.Input(look: model.fleetLook, group: FleetLookStore.group, root: store.root?.path(percentEncoded: false),
                                            writes: FleetLookStore.lastWrites, files: store.files() ?? [], seen: FleetLookReport.seen)
        input.activity = LiveActivityController.shared.runningLook
        sections = IslandDiagnostics.sections(input, now: .now)
        Task {
            guard let api = model.api else { return }
            do { input.hub = .some(try await api.fleetLook(device: PushRegistrar.deviceID)) } catch { return }
            sections = IslandDiagnostics.sections(input, now: .now)
        }
    }
}

/// The page's content, worked out apart from the container so it can be tested.
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
        /// The app's `Fleet/` in the group's container; nil when it has none.
        var root: String? = "/Fleet"
        var writes: [String: (at: Date, error: String?, bytes: Int?)] = [:]
        var files: [FleetLookStore.File] = []
        var seen: FleetLookReport?
        /// The running activity's content: nil without an activity, .some(nil) when its content carries no look.
        var activity: FleetLook??
        /// What the hub keeps for this device: nil until asked, .some(nil) when it has none (an older hub, or not told).
        var hub: FleetLook??
    }

    static func sections(_ i: Input, now: Date) -> [Section] {
        let current = FleetLookReport.describe(i.look)
        func shown(_ l: FleetLook??, none: String, missing: String) -> String {
            switch l { case .none: none; case .some(.none): missing; case .some(.some(let l)): FleetLookReport.describe(l) }
        }
        var out = [Section(title: String(localized: "结论"), rows: [Row(label: String(localized: "看起来"), value: verdict(i), wide: true)])]
        out.append(Section(title: String(localized: "形象"), rows: [
            Row(label: String(localized: "设置里"), value: current),
            Row(label: String(localized: "颜色"), value: String(format: "%06X / %06X", i.look.light, i.look.dark)),
            Row(label: String(localized: "hub 记下的"), value: shown(i.hub, none: String(localized: "还没问到"), missing: String(localized: "没有（hub 太旧，或还没告诉它）"))),
            Row(label: String(localized: "实时活动的内容里"), value: shown(i.activity, none: String(localized: "没有正在显示的实时活动"), missing: String(localized: "没有"))),
        ]))
        let written = [FleetLookStore.lookFile, FleetLookStore.facesDir].map { name in
            guard let hit = i.writes.first(where: { $0.key == name || $0.key.hasPrefix(name + "/") }) else {
                return Row(label: name, value: String(localized: "还没写过（和已存的一样时不写）"))
            }
            let w = hit.value
            return Row(label: hit.key, value: [w.error.map { String(localized: "失败：\($0)") } ?? String(localized: "成功"), w.bytes.map { String(localized: "\($0) 字节") }, time(w.at, now)].compactMap { $0 }.joined(separator: " · "))
        }
        out.append(Section(title: String(localized: "App 这次启动写进 App Group"), rows: [
            Row(label: "App Group", value: i.group ?? String(localized: "无：安装包的 Info.plist 里没有")),
            Row(label: String(localized: "容器"), value: i.root ?? String(localized: "没有：签名里没有这个 App Group")),
        ] + written))
        let listed = i.files.isEmpty
            ? [Row(label: String(localized: "文件"), value: String(localized: "没有"))]
            : i.files.map { Row(label: $0.name, value: String(localized: "\($0.bytes) 字节") + ($0.modified.map { " · " + time($0, now) } ?? "")) }
        out.append(Section(title: String(localized: "容器里"), rows: listed))
        if let seen = i.seen {
            out.append(Section(title: String(localized: "扩展最近一次"), rows: [
                Row(label: String(localized: "时间"), value: time(seen.at, now)),
                Row(label: String(localized: "画的形象"), value: seen.content ?? String(localized: "内容里没有形象")),
                Row(label: String(localized: "容器"), value: seen.container == false ? String(localized: "没有") : String(localized: "有")),
                Row(label: String(localized: "存下的形象"), value: seen.saved ?? String(localized: "没有")),
                Row(label: String(localized: "形象包的图"), value: seen.facesPackage.map { p in
                    seen.faceFile.map { String(localized: "\($0) · \(seen.faceBytes ?? 0) 字节 · \(p)") } ?? String(localized: "没有 \(p) 的图")
                } ?? String(localized: "没读（内置形象）")),
            ]))
        } else {
            out.append(Section(title: String(localized: "扩展最近一次"), rows: [Row(label: String(localized: "报告"), value: String(localized: "没有"))]))
        }
        return out
    }

    /// The first step that does not hold, in the order the look travels: the hub and the activity's content (what
    /// the island draws), then the App Group container (a package's pictures).
    static func verdict(_ i: Input) -> String {
        if case .some(let hub) = i.hub, hub != i.look {
            return hub == nil ? String(localized: "hub 没有这台手机的形象：hub 要更新，或 App 还没告诉它（推送里不会带形象）") : String(localized: "hub 记下的不是设置里的形象（App 告诉它时失败了）")
        }
        if case .some(let content) = i.activity, content != i.look { return String(localized: "正在显示的实时活动内容里不是设置里的形象（还没重画）") }
        if i.group == nil || i.root == nil {
            let group = i.group ?? String(localized: "这个组")
            return String(localized: "App 拿不到 App Group 容器（签名里没有 \(group)）：形象包的图送不到扩展，画的是默认形象换成它的颜色")
        }
        if let failed = i.writes.first(where: { $0.key.hasPrefix(FleetLookStore.facesDir) && $0.value.error != nil }) {
            let error = failed.value.error ?? ""
            return String(localized: "App 把形象包的图写进 App Group 失败（\(failed.key)）：\(error)")
        }
        guard let seen = i.seen else {
            return i.look.package == nil ? String(localized: "形象随实时活动的内容送到；扩展没留下报告（它可能写不了 App Group，内置形象不受影响）")
                : String(localized: "扩展没留下报告：它可能用不了 App Group，形象包的图就画不出来（岛上是形象包的图就没事）")
        }
        if seen.content != FleetLookReport.describe(i.look) { return String(localized: "扩展上次画的不是设置里的形象（还没重画过）") }
        if seen.container == false { return String(localized: "扩展拿不到 App Group 容器（签名里没有这个组）：画的是默认形象换成它的颜色") }
        if let package = i.look.package, seen.facesPackage != package || seen.faceFile == nil {
            return String(localized: "扩展读不到这个形象包的图，画的是默认形象换成它的颜色")
        }
        return String(localized: "扩展画的和设置一致")
    }

    static func time(_ date: Date, _ now: Date) -> String {
        let s = Int(now.timeIntervalSince(date))
        return s < 60 ? String(localized: "\(max(s, 0)) 秒前") : s < 3600 ? String(localized: "\(s / 60) 分钟前") : date.formatted(date: .abbreviated, time: .standard)
    }

    /// Everything on the page as text, to paste into a message.
    static func text(_ sections: [Section]) -> String {
        sections.map { s in ([s.title] + s.rows.map { "  \($0.label)：\($0.value)" }).joined(separator: "\n") }.joined(separator: "\n") // i18n-ignore: a diagnostics report
    }
}
#endif
