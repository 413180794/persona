import SwiftUI

/// The machine pages are pushed by value (MachineListRoute, MachineDetailRoute) and so are the chats they open
/// (SessionKey): a page pushed with `NavigationLink { … }` that pushes by value puts the new page *under* itself, so a
/// tap seemed to do nothing (设置 › 机器 › a card; 机器详情 › an agent). Every stack that shows them registers both.
struct MachineListRoute: Hashable {}
struct MachineDetailRoute: Hashable {
    var ref: MachineRef
    init(_ ref: MachineRef) { self.ref = ref }
    /// A machine of the default group, or "group|id" (launch arguments).
    init(id: String) { ref = MachineRef(path: id) }
}

/// What brings a machine to the top of 机器 (docs/machines.md「App」), most pressing first: it is offline, a value is
/// too high, an agent there needs you, its jarvisd is not the hub's build.
enum MachineConcern: Int, Comparable {
    case offline, critical, warning, needsYou, mismatch

    static func < (a: Self, b: Self) -> Bool { a.rawValue < b.rawValue }

    /// `alert`: the worst of the machine's latest values; `waiting`: its agents that need you.
    static func of(_ m: Machine, alert: MetricLevel, waiting: Int) -> MachineConcern? {
        if !m.online { return .offline }
        if alert == .critical { return .critical }
        if alert == .warning { return .warning }
        if waiting > 0 { return .needsYou }
        if m.nodeMismatch == true || m.deploying == true { return .mismatch }
        return nil
    }

    /// The machines with a concern first, most pressing first; the same concern and the rest keep the hub's order, so a
    /// card moves only when what it shows changes.
    static func ordered(_ machines: [Machine], by concern: (Machine) -> MachineConcern?) -> [Machine] {
        machines.enumerated()
            .map { (rank: concern($0.element)?.rawValue ?? Int.max, index: $0.offset, machine: $0.element) }
            .sorted { ($0.rank, $0.index) < ($1.rank, $1.index) }
            .map(\.machine)
    }
}

/// A machine's live agents by what they need: 需要你, 工作中, and 空闲 for the rest (finished, seen, unknown).
struct AgentTally: Equatable {
    var needsYou = 0
    var working = 0
    var idle = 0

    init(_ sessions: [Session]) {
        for s in sessions {
            switch s.state {
            case .needsYou: needsYou += 1
            case .working: working += 1
            default: idle += 1
            }
        }
    }
}

/// 机器 (docs/machines.md「App」): every machine on one screen, what needs a look first. A card each: online or not,
/// the system, its agents (需要你 / 工作中 / 空闲), CPU / memory / network now and over the last hour, what is too high,
/// a build unlike the hub's with 重新部署. The machine list is live; the metrics refresh every 10 s while the page is
/// on screen and the app is in front. A tap opens the machine.
struct MachinesView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var phase
    @Environment(\.tabIsActive) private var inFront
    @State private var metrics: [MachineRef: MachineMetrics] = [:]
    /// The groups whose hub keeps no metrics (404: before machine monitoring): their cards have no CPU / memory / network.
    @State private var unmonitoredGroups = Set<String>()
    private var unmonitored: Bool { !unmonitoredGroups.isEmpty && unmonitoredGroups.count >= model.groupIDs.count }

    var body: some View {
        // Behind another section (the Mac keeps the page, `MacPageStack`): as they are now, not followed push by push.
        let machines = model.machines(inFront: inFront)
        let agents = MachineDetailView.agentsByMachine(model, inFront: inFront)
        let tallies = Dictionary(machines.map { ($0.ref, AgentTally(agents[$0.ref] ?? [])) }, uniquingKeysWith: { a, _ in a })
        let alerts = Dictionary(machines.map { ($0.ref, alertLevel($0)) }, uniquingKeysWith: { a, _ in a })
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.m) {
                if !machines.isEmpty {
                    summary(machines, alerts: alerts.values.filter { $0 > .normal }, waiting: tallies.values.reduce(0) { $0 + $1.needsYou })
                }
                if model.isMultiHub {
                    // Several groups of hubs: one block each, under its name (docs/multi-hub.md).
                    ForEach(model.groupIDs, id: \.self) { g in
                        let mine = machines.filter { $0.group == g }
                        if !mine.isEmpty {
                            groupHeader(g, machines: mine)
                            grid(mine, tallies: tallies, alerts: alerts)
                        }
                    }
                } else {
                    grid(machines, tallies: tallies, alerts: alerts)
                }
                #if !os(macOS)
                UsageWindowsCard() // the Mac has it at the foot of its sections column
                #endif
                if machines.isEmpty {
                    EmptyStateView(symbol: Theme.Icon.machines, title: String(localized: "还没有机器"), message: String(localized: "hub 连上后，这里会显示 hub 所在的机器和它接入的各台电脑。"))
                } else if !unmonitored {
                    Text("CPU、内存、网络每 10 秒更新，曲线是最近 1 小时。")
                        .font(Theme.Font.label).foregroundStyle(.secondary)
                        .padding(.horizontal, Theme.Space.xs)
                }
            }
            .padding(Theme.Space.l)
        }
        #if os(macOS)
        .modifier(MacBelowTitleRowIfAsked()) // under 机器's title in the window: the cards start below it (#1243)
        #endif
        .themedGroupedPage()
        .navigationTitle("机器")
        // Large wherever it is pushed (the Agents list's stack would make it inline, and the principal item below would
        // then hide the title); the bar's middle is only for 连接中… and the like.
        .navigationBarTitleDisplayMode(.large)
        .pageToolbar {
            #if os(macOS)
            // Empty while connected: its glass drew a thin line in the middle of the title bar's row (#830).
            ToolbarItem(placement: .principal) { ConnectionTitle() }.sharedBackgroundVisibility(.hidden)
            #else
            ToolbarItem(placement: .principal) { ConnectionTitle() }
            #endif
        }
        .refreshable {
            await model.refresh()
            await loadMetrics()
            #if !os(macOS)
            await UsageWindowsStore.shared.load(model)
            #endif
        }
        #if os(macOS)
        .macRefreshes { // ⌘R: no pull to refresh on the Mac
            await model.refresh()
            await loadMetrics()
        }
        #endif
        .tabActiveTask(id: phase == .active) {
            guard phase == .active else { return } // in the background or another tab: nothing to draw, nothing to fetch
            while !Task.isCancelled {
                await loadMetrics()
                try? await Task.sleep(for: .seconds(10))
            }
        }
        #if !os(macOS)
        .tabActiveTask(id: phase == .active ? "usage" : "") {
            guard phase == .active else { return }
            await UsageWindowsStore.shared.poll(model)
        }
        #endif
    }

    /// The cards, what needs a look first.
    private func grid(_ machines: [Machine], tallies: [MachineRef: AgentTally], alerts: [MachineRef: MetricLevel]) -> some View {
        let ordered = MachineConcern.ordered(machines) { m in
            MachineConcern.of(m, alert: alerts[m.ref] ?? .normal, waiting: tallies[m.ref]?.needsYou ?? 0)
        }
        // The cards of a row are as tall as its tallest (`fillsRow`): a card with more alert chips or a two-line
        // subtitle left its neighbours short and their charts at another height (#905).
        return LazyVGrid(columns: [GridItem(.adaptive(minimum: Theme.Size.cardColumn), spacing: Theme.Space.m, alignment: .top)],
                         spacing: Theme.Space.m) {
            ForEach(ordered, id: \.ref) { m in
                NavigationLink(value: MachineDetailRoute(m.ref)) {
                    MachineCard(machine: m, tally: tallies[m.ref], metrics: metrics[m.ref], showsMetrics: !unmonitoredGroups.contains(m.group),
                                fillsRow: true)
                }
                .buttonStyle(.plain)
            }
        }
        .animation(Theme.Motion.layout, value: ordered.map(\.ref))
    }

    /// A group's name over its cards, with how its link is and how many of its machines are online.
    private func groupHeader(_ group: String, machines: [Machine]) -> some View {
        HStack(spacing: Theme.Space.sm) {
            if let live = model.socket(group) { HubStatusDot(live: live) }
            Text(model.groupName(group)).font(Theme.Font.sectionHeader).foregroundStyle(.secondary).lineLimit(1)
            Text("\(machines.filter(\.online).count)/\(machines.count)").font(Theme.Font.label).foregroundStyle(.tertiary)
            Spacer(minLength: 0)
        }
        .padding(.horizontal, Theme.Space.xs)
        .padding(.top, Theme.Space.s)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("machines.group.\(group)")
    }

    /// The worst of a machine's latest values; normal while it is offline or has sent nothing lately.
    private func alertLevel(_ m: Machine) -> MetricLevel {
        guard m.online, let mm = metrics[m.ref], let s = mm.latest else { return .normal }
        return MetricAlert.of(s, cores: (mm.system ?? m.system)?.cores).first?.level ?? .normal
    }

    private func summary(_ machines: [Machine], alerts: [MetricLevel], waiting: Int) -> some View {
        let online = machines.filter(\.online).count
        return HStack(spacing: Theme.Space.l) {
            Label("\(online)/\(machines.count) 台在线", systemImage: Theme.Icon.dot)
                .labelStyle(DotLabel(color: online == machines.count ? Theme.positive : .secondary))
            if let worst = alerts.max() {
                HStack(spacing: Theme.Space.xs) {
                    Image(systemName: Theme.Icon.errorFilled).foregroundStyle(MetricColor.level(worst))
                    Text("\(alerts.count) 台用量偏高")
                }
            }
            if waiting > 0 {
                Label("\(waiting) 个 Agent 需要你", systemImage: Theme.Icon.dot)
                    .labelStyle(DotLabel(color: StatusPalette.needsYou.dot))
            }
            Spacer(minLength: 0)
        }
        .font(Theme.Font.captionMedium)
        .lineLimit(1)
        .minimumScaleFactor(0.8)
        .padding(.horizontal, Theme.Space.xs)
    }

    /// Every group's metrics (each hub keeps its own machines').
    private func loadMetrics() async {
        for g in model.groupIDs {
            guard let api = model.api(g) else { continue }
            do {
                let fresh = try await api.metricsOverview()
                var all = metrics.filter { $0.key.group != g }
                for m in fresh where all[MachineRef(group: g, id: m.machineID)] == nil { all[MachineRef(group: g, id: m.machineID)] = m }
                metrics = all
                unmonitoredGroups.remove(g)
            } catch {
                if error.isCancellation { return }
                // A hub before machine monitoring: the cards go without numbers. Other errors keep the last numbers (the
                // title says when the hub is out of reach).
                if let e = error as? APIError, e.status == 404 { unmonitoredGroups.insert(g) }
            }
        }
    }

}

struct DotLabel: LabelStyle {
    var color: Color
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: Theme.Space.sm) {
            Circle().fill(color).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
            configuration.title
        }
    }
}

/// One machine: on 机器 with its numbers, and at the top of its page without them (the page has the charts).
struct MachineCard: View {
    @Environment(AppModel.self) private var model
    var machine: Machine
    /// Its agents counted; nil counts them here.
    var tally: AgentTally?
    /// Its latest values (and for 机器 the last hour); nil until they come, or from a hub without machine monitoring.
    var metrics: MachineMetrics?
    var showsMetrics = true
    /// In 机器's grid: as tall as the row, an online card's counts and charts at the bottom so they line up across it
    /// (an offline card keeps its words at the top). At the top of a machine's page it is as tall as its content.
    var fillsRow = false

    var body: some View {
        let system = machine.system ?? metrics?.system
        let sample = machine.online ? metrics?.latest : nil
        let alerts = sample.map { MetricAlert.of($0, cores: system?.cores) } ?? []
        let worst = alerts.first?.level ?? .normal
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            VStack(alignment: .leading, spacing: Theme.Space.m) {
                HStack(spacing: Theme.Space.m) {
                    OSBadge(system: system, fallback: machine.symbol, size: Theme.Size.avatarM, online: machine.online)
                    VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                        // Color.primary / Color.secondary, not .primary / .secondary: the hierarchical styles come out a step
                        // lighter where the card sits in a section header.
                        Text(machine.name).font(Theme.Font.heading).foregroundStyle(Color.primary).lineLimit(1)
                        Text(Self.subtitle(machine, system: system)).font(Theme.Font.label).foregroundStyle(Color.secondary).lineLimit(2)
                    }
                    Spacer(minLength: Theme.Space.sm)
                    status
                }
                if !alerts.isEmpty {
                    FlowLayoutRow {
                        ForEach(alerts, id: \.label) { a in AlertChip(alert: a) }
                    }
                }
            }
            .frame(maxHeight: fillsRow && machine.online ? .infinity : nil, alignment: .top)
            if machine.online {
                VStack(alignment: .leading, spacing: Theme.Space.m) {
                    AgentCounts(tally: tally ?? AgentTally(MachineDetailView.agents(model, machine.ref)))
                    if showsMetrics { metricsRow(sample) }
                }
                // Sized before the stretching part above: second in line, the values (which may shrink to fit) came
                // out smaller than in a card of their own.
                .layoutPriority(1)
            } else if let err = machine.error, !err.isEmpty {
                Text(err).font(Theme.Font.label).foregroundStyle(Color.secondary).lineLimit(2)
            }
            if machine.nodeMismatch == true || machine.deploying == true {
                DeployRow(machine: machine)
            }
        }
        .frame(maxHeight: fillsRow ? .infinity : nil, alignment: .top)
        .card(padding: 14)
        .overlay {
            if worst > .normal {
                RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
                    .stroke(MetricColor.level(worst).opacity(0.7), lineWidth: Theme.Size.lineMedium)
            }
        }
        .contentShape(.rect)
    }

    /// "Ubuntu 24.04.1 LTS · 16 核 · 64.0 GB"; the host name when the machine never said what it runs.
    static func subtitle(_ m: Machine, system: MachineSystem?) -> String {
        guard let sys = system else { return m.hostname ?? m.sshHost ?? m.id }
        var parts = [sys.name]
        if sys.cores > 0 { parts.append(String(localized: "\(sys.cores) 核")) }
        if sys.memory > 0 { parts.append(MetricFormat.bytes(sys.memory)) }
        return parts.joined(separator: " · ")
    }

    @ViewBuilder private var status: some View {
        if machine.online {
            HStack(spacing: Theme.Space.sm) {
                Circle().fill(Theme.positive).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
                Text("在线").font(Theme.Font.labelStrong).foregroundStyle(Theme.positive)
            }
            .fixedSize()
        } else {
            VStack(alignment: .trailing, spacing: Theme.Space.xxs) {
                Text("离线").font(Theme.Font.labelStrong).foregroundStyle(Color.secondary)
                if let seen = machine.lastSeen, seen.timeIntervalSince1970 > 0 {
                    Text(Fmt.relative(seen)).font(Theme.Font.label).foregroundStyle(.tertiary)
                }
            }
            .fixedSize()
        }
    }

    private func metricsRow(_ s: MetricSample?) -> some View {
        let points = metrics?.points ?? []
        let end = Date.now
        let domain = end.addingTimeInterval(-3600)...end
        return HStack(alignment: .top, spacing: Theme.Space.m) {
            MetricCell(title: "CPU", value: MetricFormat.percent(s?.cpu), detail: s?.load.map { String(localized: "负载 \(MetricFormat.load($0))") },
                       level: MetricLevel.of(s?.cpu, warn: 75, crit: 90)) {
                Sparkline(points: points, series: [ChartSeries(name: "CPU", color: MetricColor.cpu) { $0.cpu }], domain: domain, yMax: 100)
            }
            MetricCell(title: String(localized: "内存"), value: MetricFormat.percent(s?.memPercent),
                       detail: s.map { MetricFormat.bytes($0.memUsed) + " / " + MetricFormat.bytes($0.memTotal) },
                       level: MetricLevel.of(s?.memPercent, warn: 80, crit: 90)) {
                let total = Double(s?.memTotal ?? metrics?.system?.memory ?? 0)
                Sparkline(points: points, series: [ChartSeries(name: String(localized: "内存"), color: MetricColor.mem) { p in
                    p.mem.map { total > 0 ? 100 * $0 / total : 0 }
                }], domain: domain, yMax: 100)
            }
            MetricCell(title: String(localized: "网络"), value: s == nil ? "—" : "↓ " + MetricFormat.rate(s?.netRx),
                       detail: s == nil ? nil : "↑ " + MetricFormat.rate(s?.netTx), level: .normal) {
                Sparkline(points: points, series: [
                    ChartSeries(name: String(localized: "下行"), color: MetricColor.rx) { $0.rx },
                    ChartSeries(name: String(localized: "上行"), color: MetricColor.tx) { $0.tx },
                ], domain: domain, yMax: nil)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// 需要你 / 工作中 / 空闲 side by side, a dot and a count each; a state with none stays gray.
struct AgentCounts: View {
    var tally: AgentTally

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            item(tally.needsYou, String(localized: "需要你"), tone: StatusPalette.needsYou, ink: StatusPalette.needsYou.text)
            item(tally.working, String(localized: "工作中"), tone: StatusPalette.working, ink: StatusPalette.working.text)
            item(tally.idle, String(localized: "空闲"), tone: StatusPalette.idle, ink: Color.primary)
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.s)
        .background(Color.primary.opacity(0.04), in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
    }

    private func item(_ n: Int, _ label: String, tone: StatusTone, ink: Color) -> some View {
        HStack(spacing: Theme.Space.sm) {
            Circle().fill(n > 0 ? tone.dot : StatusPalette.offline.dot).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
            Text("\(n)").font(Theme.Font.bodyStrong.monospacedDigit()).foregroundStyle(n > 0 ? ink : Color.secondary)
                .contentTransition(.numericText(value: Double(n)))
            Text(label).font(Theme.Font.label).foregroundStyle(Color.secondary).lineLimit(1)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(n) 个 Agent \(label)")
    }
}

/// CPU / 内存 / 网络 on a card: the value now, a detail under it, the last hour under that.
struct MetricCell<Chart: View>: View {
    var title: String
    var value: String
    var detail: String?
    var level: MetricLevel
    @ViewBuilder var chart: Chart

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            HStack(spacing: Theme.Space.xs) {
                Text(title).font(Theme.Font.label).foregroundStyle(Color.secondary)
                if level > .normal {
                    Image(systemName: Theme.Icon.errorFilled).font(Theme.Font.label).imageScale(.small).foregroundStyle(MetricColor.level(level))
                }
            }
            Text(value).font(Theme.Font.bodyStrong.monospacedDigit()).foregroundStyle(Color.primary).lineLimit(1).minimumScaleFactor(0.7)
            Text(detail ?? " ").font(Theme.Font.label.monospacedDigit()).foregroundStyle(Color.secondary).lineLimit(1).minimumScaleFactor(0.7)
            chart.scaledFrame(height: Theme.Size.wellS).padding(.top, Theme.Space.xxs)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// What is too high: the icon carries the level's color, the words stay in text ink (warning yellow is unreadable
/// as text on a light card).
struct AlertChip: View {
    var alert: MetricAlert

    var body: some View {
        let color = MetricColor.level(alert.level)
        HStack(spacing: Theme.Space.xs) {
            Image(systemName: alert.level == .critical ? Theme.Icon.critical : Theme.Icon.errorFilled)
                .foregroundStyle(color)
            Text(alert.label).foregroundStyle(Color.primary)
        }
        .font(Theme.Font.labelStrong)
        .lineLimit(1)
        .padding(.horizontal, Theme.Space.s)
        .padding(.vertical, Theme.Space.xs)
        .background(color.opacity(0.16), in: .capsule)
        .accessibilityLabel(alert.level == .critical ? String(localized: "严重：\(alert.label)") : String(localized: "注意：\(alert.label)"))
    }
}

/// A machine whose jarvisd is not the hub's build: says so, and 重新部署 has the hub install its own build there.
struct DeployRow: View {
    @Environment(AppModel.self) private var model
    var machine: Machine
    @State private var starting = false

    var body: some View {
        let busy = machine.deploying == true || starting
        HStack(spacing: Theme.Space.m) {
            Label(text(busy), systemImage: busy ? Theme.Icon.retry : Theme.Icon.errorFilled)
                .font(Theme.Font.label)
                .foregroundStyle(Theme.cautionText)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button { Task { await deploy() } } label: {
                if busy {
                    ProgressView().controlSize(.small)
                } else {
                    Text("重新部署").font(Theme.Font.labelStrong).lineLimit(1)
                }
            }
            .themedButton()
            .disabled(busy)
            .accessibilityLabel(busy ? "正在部署" : "重新部署")
        }
    }

    private func text(_ busy: Bool) -> String {
        if busy { return String(localized: "正在把 hub 这一版的 Persona 装到这台电脑…") }
        if let e = machine.deployError, !e.isEmpty { return String(localized: "上次部署没成：\(e)") }
        return String(localized: "这台电脑上的 Persona 和 hub 不是同一次部署")
    }

    private func deploy() async {
        starting = true
        defer { starting = false }
        do {
            try await model.api?.deploy(machine: machine.id)
            Haptics.tap()
            await model.refresh()
        } catch {
            if let m = error.shown { model.toast = Toast(String(localized: "部署失败：\(m)"), style: .error) }
        }
    }
}
