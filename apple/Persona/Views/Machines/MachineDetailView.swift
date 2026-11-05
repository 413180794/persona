import BeautifulUI
import SwiftUI

/// One machine (docs/machines.md「机器详情」), most used first: how it is (the card of 机器, without its numbers), its
/// agents (a tap opens the chat), its values now and CPU / memory / network / load over 1, 6 or 24 hours (hold a chart
/// to read a moment, all charts follow), the system, the launchers, how the hub reaches it (offline: right under the
/// card, with 重新连接). The metrics refresh while on screen and the app is in front: every 10 s for the hour, every
/// minute for the longer ranges.
struct MachineDetailView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var phase
    var ref: MachineRef
    private var machineID: String { ref.id }
    @State private var range: MetricRange = Self.initialRange
    @State private var data: MachineMetrics?
    @State private var shownRange: MetricRange?
    @State private var failure: String?
    @State private var selection: Date?
    @State private var launchers: [Launcher] = []
    @State private var launcherError: String?
    @State private var reconnecting = false
    @State private var allAgents = false
    @State private var plugins = ExtensionMachineModel()
    @State private var pluginLink: LinkTarget?

    /// A machine's agents: live ones, not the 对话 tab's assistant conversations (the Agents tab leaves those out too).
    static func agents(_ model: AppModel, _ ref: MachineRef) -> [Session] {
        (model.sessionsByMachine[ref] ?? []).filter { $0.status.isLive && !$0.assistant }
    }

    /// Every machine's agents (`agents(_:_:)`), for a page that can be kept hidden behind another: watched only in front
    /// (`AppModel.allSessions(inFront:)`).
    static func agentsByMachine(_ model: AppModel, inFront: Bool) -> [MachineRef: [Session]] {
        Dictionary(grouping: model.allSessions(inFront: inFront).filter { $0.status.isLive && !$0.assistant }, by: \.machineRef)
    }

    /// The agents before 显示全部: with dozens on one machine its charts would be screens away.
    static let agentsShown = 5

    private static var initialRange: MetricRange {
        #if DEBUG
        // `-openMonitorRange 6h|24h`: the page opens on that range (screenshots, UI tests).
        if let r = UserDefaults.standard.string(forKey: "openMonitorRange").flatMap(MetricRange.init(rawValue:)) { return r }
        #endif
        return .hour
    }

    var body: some View {
        let machine = model.machine(ref)
        let system = data?.system ?? machine?.system
        let online = machine?.online ?? data?.online ?? false
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.section) {
                if let machine {
                    VStack(spacing: Theme.Space.m) {
                        MachineCard(machine: machine, metrics: data, showsMetrics: false)
                        if !online { connection(machine) }
                    }
                    agentsSection(machine)
                    // The node's self-check by feature, after the engine as on the web (#3018).
                    NodeHealthSection(ref: ref, machine: machine)
                    if let computer = machine.computer { ComputerSection(ref: ref, computer: computer, online: online) }
                    if let browser = machine.browser { BrowserSection(ref: ref, browser: browser, online: online) }
                    if machine.browser?.setUp == true { BrowserLoginsSection(ref: ref, online: online) }
                    VStack(alignment: .leading, spacing: Theme.Space.m) {
                        heading("监控")
                        if let s = data?.latest, online { NowGrid(sample: s, cores: system?.cores) }
                        SegmentedChoice("时间范围", MetricRange.allCases.map { ($0, $0.title) }, selection: $range)
                            .accessibilityIdentifier("metric-range")
                        charts(system: system)
                        if let cores = data?.latest?.cores, online, cores.count > 1 { CoreBars(cores: cores) }
                    }
                    if let system {
                        VStack(alignment: .leading, spacing: Theme.Space.m) {
                            heading("系统")
                            SystemFacts(system: system, machine: machine)
                        }
                    }
                    launchersSection(machine)
                    // What plugins draw for this machine (docs/extensions.md「UI 插槽」machine_section).
                    ExtensionMachineSections(page: plugins, link: $pluginLink)
                    VStack(alignment: .leading, spacing: Theme.Space.m) {
                        if online { connection(machine) }
                        Text("监控历史保留 24 小时。最近 1 小时每 10 秒一个点，更早的每分钟一个点；中间断开的时段不连线。")
                            .font(Theme.Font.label).foregroundStyle(.secondary)
                            .padding(.horizontal, Theme.Space.xs)
                    }
                } else if model.machines.isEmpty {
                    ProgressView().frame(maxWidth: .infinity).padding(.vertical, Theme.Space.xxl)
                } else {
                    EmptyStateView(symbol: Theme.Icon.machines, title: "找不到这台机器", message: "hub 的配置里已经没有它了。")
                }
            }
            .padding(Theme.Space.l)
        }
        .themedGroupedPage()
        .pluginLinks($pluginLink)
        .extensionPushes(Array(plugins.models.values))
        .detailTitle(machine?.name ?? machineID)
        .navigationBarTitleDisplayMode(.inline)
        .refreshable {
            await model.refresh()
            await load()
            await loadLaunchers(refresh: true)
            await plugins.load(ref, app: model, redraw: true)
        }
        #if os(macOS)
        .macRefreshes(id: machineID) {
            await model.refresh()
            await load()
            await loadLaunchers(refresh: true)
            await plugins.load(ref, app: model, redraw: true)
        }
        #endif
        .tabActiveTask(id: Poll(range: range, active: phase == .active)) { // stops in the background and in another tab
            guard phase == .active else { return }
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(range == .hour ? 10 : 60))
            }
        }
        // Again when it comes online (after 重新连接): an offline machine has no launchers to list.
        .task(id: machine?.online == true) { await loadLaunchers(refresh: false) }
        // Again once the hub says it has the slots (the page can open before its hello).
        .task(id: ExtensionsKey(ref: ref, slots: model.features(ref.group).hasExtensionFeature("ui_slots"))) {
            await plugins.load(ref, app: model, redraw: false)
        }
    }

    nonisolated private struct Poll: Hashable, Sendable { var range: MetricRange; var active: Bool }
    nonisolated private struct ExtensionsKey: Hashable, Sendable { var ref: MachineRef; var slots: Bool }

    private func heading(_ title: String, count: Int? = nil) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.sm) {
            Text(title).font(Theme.Font.heading)
            if let count { Text("\(count)").font(Theme.Font.secondary.monospacedDigit()).foregroundStyle(.secondary) }
        }
        .padding(.horizontal, Theme.Space.xs)
    }

    // MARK: Agents

    /// Needs-you first, then working, then the rest, each in the hub's order.
    private func agentsSection(_ machine: Machine) -> some View {
        let rank: (AgentState) -> Int = { $0 == .needsYou ? 0 : $0 == .working ? 1 : 2 }
        let sessions = Self.agents(model, ref).enumerated()
            .sorted { (rank($0.element.state), $0.offset) < (rank($1.element.state), $1.offset) }
            .map(\.element)
        let shown = allAgents ? sessions : Array(sessions.prefix(Self.agentsShown))
        return VStack(alignment: .leading, spacing: Theme.Space.m) {
            heading("Agent", count: machine.online ? sessions.count : nil)
            if sessions.isEmpty {
                Text(machine.online ? "没有运行中的 Agent" : "机器离线，连上后显示它的 Agent")
                    .font(Theme.Font.secondary).foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .card(padding: 14)
            } else {
                VStack(spacing: 0) {
                    ForEach(Array(shown.enumerated()), id: \.element.id) { i, s in
                        if i > 0 { Divider().padding(.leading, Theme.Space.l) }
                        NavigationLink(value: s.key) {
                            HStack(spacing: Theme.Space.s) {
                                SessionRow(session: s, machine: machine, showMachine: false)
                                Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong).foregroundStyle(.tertiary)
                            }
                            .padding(.horizontal, Theme.Space.l)
                            .padding(.vertical, Theme.Space.m)
                            .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                    }
                    if sessions.count > shown.count {
                        Divider()
                        Button {
                            withAnimation(Theme.Motion.layout) { allAgents = true }
                        } label: {
                            Text("显示全部 \(sessions.count) 个").font(Theme.Font.secondaryMedium).lineLimit(1)
                                .frame(maxWidth: .infinity)
                                .padding(.vertical, Theme.Space.m)
                                .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                        .foregroundStyle(Theme.accent)
                    }
                }
                .card(padding: 0)
            }
            engineRow(machine)
        }
    }

    /// Which engine's sessions the machine shows (herdr, Herdr App…): it decides the list above.
    private func engineRow(_ machine: Machine) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            NavigationLink { // a leaf: nothing is pushed from the picker, so a view push is fine in this value stack
                EngineSelectionView(ref: ref)
            } label: {
                HStack(spacing: Theme.Space.s) {
                    Text("引擎").font(Theme.Font.body).lineLimit(1)
                    Spacer(minLength: Theme.Space.s)
                    Text(machine.engineName).font(Theme.Font.body).foregroundStyle(.secondary).lineLimit(1)
                    Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong).foregroundStyle(.tertiary)
                }
                .padding(.horizontal, Theme.Space.l)
                .padding(.vertical, Theme.Space.m)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("machine.engine")
            .card(padding: 0)
            Text("只显示所选引擎里的会话。").font(Theme.Font.label).foregroundStyle(.secondary)
                .padding(.horizontal, Theme.Space.xs)
        }
    }

    // MARK: Launchers, connection

    private func launchersSection(_ machine: Machine) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            heading("启动器", count: machine.online && !launchers.isEmpty ? launchers.count : nil)
            VStack(alignment: .leading, spacing: 0) {
                if !machine.online {
                    Text("机器离线，连上后显示").font(Theme.Font.secondary).foregroundStyle(.secondary).padding(.vertical, Theme.Space.s)
                } else if let launcherError, launchers.isEmpty {
                    Label(launcherError, systemImage: Theme.Icon.error).font(Theme.Font.caption).foregroundStyle(Theme.danger)
                        .padding(.vertical, Theme.Space.s)
                } else if launchers.isEmpty {
                    ProgressView().frame(maxWidth: .infinity).padding(.vertical, Theme.Space.s)
                }
                ForEach(Array(launchers.enumerated()), id: \.element.id) { i, l in
                    if i > 0 { Divider() }
                    HStack {
                        Text(l.name).font(Theme.Font.body.monospaced())
                        Spacer()
                        Text(Fmt.model(l.model) ?? "").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                    }
                    .padding(.vertical, Theme.Space.s)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .card(padding: 14)
        }
    }

    /// How the hub reaches the machine, and 重新连接.
    private func connection(_ machine: Machine) -> some View {
        HStack(spacing: Theme.Space.m) {
            Text(machine.kind == "local" ? "hub 所在机器，直接运行 node。" : "hub 通过 `ssh \(machine.sshHost ?? machine.id)` 接入。")
                .font(Theme.Font.label).foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button { Task { await reconnect() } } label: {
                if reconnecting {
                    ProgressView().controlSize(.small)
                } else {
                    Label("重新连接", systemImage: Theme.Icon.retry).font(Theme.Font.labelStrong).lineLimit(1)
                }
            }
            .themedButton()
            .disabled(reconnecting)
            .accessibilityLabel(reconnecting ? "正在重新连接" : "重新连接")
        }
        .card(padding: 14)
    }

    // MARK: Loading

    private func load() async {
        guard let api = model.api(ref.group) else { return }
        let r = range
        do {
            let fresh = try await api.machineMetrics(machineID, range: r)
            guard r == range else { return }
            data = fresh
            shownRange = r
            failure = nil
            #if DEBUG
            // `-openMonitorScrub 0.8`: the charts open held at that share of the range (screenshots of a scrub).
            if selection == nil, let f = UserDefaults.standard.object(forKey: "openMonitorScrub") as? Double ?? Double(UserDefaults.standard.string(forKey: "openMonitorScrub") ?? "") {
                selection = Date.now.addingTimeInterval(-r.seconds * (1 - f))
            }
            #endif
        } catch {
            if error.isCancellation { return }
            if let e = error as? APIError, e.status == 404 {
                failure = "这个 hub 还没有监控数据：部署新版本的 hub 和这台电脑上的 node 后再看。"
            } else if data == nil {
                failure = error.shown ?? "连不上 hub。连上后这里会自动刷新。"
            }
        }
    }

    private func loadLaunchers(refresh: Bool) async {
        guard model.machine(ref)?.online == true else { launchers = []; return }
        do {
            launchers = try await model.launchers(ref, refresh: refresh)
            launcherError = nil
        } catch {
            launcherError = error.shown.map { "读不到启动器：\($0)" }
        }
    }

    /// Asks the hub to reconnect, then waits (up to 15 s) for the machine to come online; says so if it does not.
    private func reconnect() async {
        reconnecting = true
        defer { reconnecting = false }
        do {
            try await model.api(ref.group)?.reconnect(machine: machineID)
        } catch {
            if let m = error.shown { model.toast = Toast("重新连接失败：\(m)", style: .error) }
            return
        }
        for _ in 0..<10 {
            try? await Task.sleep(for: .seconds(1.5))
            await model.refresh()
            if model.machine(ref)?.online == true {
                Haptics.success()
                await loadLaunchers(refresh: true)
                return
            }
        }
        let err = model.machine(ref)?.error
        model.toast = Toast("还没连上\(err.map { "：\($0)" } ?? "")", style: .error)
    }

    // MARK: Charts

    @ViewBuilder private func charts(system: MachineSystem?) -> some View {
        if let data, shownRange != nil {
            let r = shownRange ?? range
            let end = Date.now
            let domain = end.addingTimeInterval(-r.seconds)...end
            let step: TimeInterval = r == .hour ? 10 : r == .sixHours ? 60 : 240
            let memTotal = Double(data.latest?.memTotal ?? system?.memory ?? 0)
            let live = data.online ? data.latest : nil
            let cores = Double(system?.cores ?? 0)
            Group {
                MetricChart(title: "CPU", points: data.points, series: [ChartSeries(name: "CPU", color: MetricColor.cpu) { $0.cpu }],
                            domain: domain, step: step, yDomain: 0...100, format: { MetricFormat.percent($0) },
                            now: [live?.cpu], selection: $selection)
                MetricChart(title: memTotal > 0 ? "内存（共 \(MetricFormat.bytes(memTotal))）" : "内存", points: data.points,
                            series: [ChartSeries(name: "内存", color: MetricColor.mem) { p in p.mem.map { memTotal > 0 ? 100 * $0 / memTotal : 0 } }],
                            domain: domain, step: step, yDomain: 0...100, format: { MetricFormat.percent($0) },
                            now: [live?.memPercent], selection: $selection)
                MetricChart(title: "网络", points: data.points, series: [
                    ChartSeries(name: "下行", color: MetricColor.rx) { $0.rx },
                    ChartSeries(name: "上行", color: MetricColor.tx) { $0.tx },
                ], domain: domain, step: step, format: { MetricFormat.rate($0) }, now: [live?.netRx, live?.netTx], selection: $selection)
                MetricChart(title: "负载（1 分钟）", points: data.points, series: [ChartSeries(name: "负载", color: MetricColor.load) { $0.load }],
                            domain: domain, step: step, reference: cores > 0 ? (cores, "\(Int(cores)) 核") : nil,
                            format: { MetricFormat.load($0) }, now: [live?.load], selection: $selection)
            }
            .animation(.default, value: shownRange)
            if data.points.allSatisfy(\.isGap) {
                Text(data.online ? "刚开始记录：过一会儿这里就有曲线了。" : "这段时间这台机器没有连上。")
                    .font(Theme.Font.caption).foregroundStyle(.secondary).frame(maxWidth: .infinity)
            }
        } else if let failure {
            EmptyStateView(symbol: "chart.xyaxis.line", title: "看不到监控数据", message: failure)
                .padding(.vertical, 30)
        } else {
            ProgressView().frame(maxWidth: .infinity).padding(.vertical, 60)
        }
    }
}

/// The values now, two columns: CPU, memory, network, load, disk, swap.
private struct NowGrid: View {
    var sample: MetricSample
    var cores: Int?

    var body: some View {
        let cols = [GridItem(.flexible(), spacing: 10), GridItem(.flexible(), spacing: 10)]
        LazyVGrid(columns: cols, spacing: 10) {
            tile("CPU", MetricFormat.percent(sample.cpu), detail: nil, fraction: sample.cpu.map { $0 / 100 }, color: MetricColor.cpu,
                 level: MetricLevel.of(sample.cpu, warn: 75, crit: 90))
            tile("内存", MetricFormat.percent(sample.memPercent), detail: MetricFormat.bytes(sample.memUsed) + " / " + MetricFormat.bytes(sample.memTotal),
                 fraction: sample.memPercent.map { $0 / 100 }, color: MetricColor.mem, level: MetricLevel.of(sample.memPercent, warn: 80, crit: 90))
            tile("网络", "↓ " + MetricFormat.rate(sample.netRx), detail: "↑ " + MetricFormat.rate(sample.netTx), fraction: nil, color: MetricColor.rx, level: .normal)
            tile("负载", MetricFormat.load(sample.load), detail: cores.map { "\($0) 核" }, fraction: nil, color: MetricColor.load,
                 level: MetricLevel.of(sample.load.flatMap { l in cores.map { l / Double(max($0, 1)) } }, warn: 1, crit: 2))
            tile("磁盘", MetricFormat.percent(sample.diskPercent), detail: MetricFormat.bytes(sample.diskUsed) + " / " + MetricFormat.bytes(sample.diskTotal),
                 fraction: sample.diskPercent.map { $0 / 100 }, color: .secondary, level: MetricLevel.of(sample.diskPercent, warn: 80, crit: 90))
            tile("交换", sample.swapTotal > 0 ? MetricFormat.bytes(sample.swapUsed) : "未启用",
                 detail: sample.swapTotal > 0 ? "共 " + MetricFormat.bytes(sample.swapTotal) : nil,
                 fraction: sample.swapPercent.map { $0 / 100 }, color: .secondary, level: .normal)
        }
    }

    @ViewBuilder private func tile(_ title: String, _ value: String, detail: String?, fraction: Double?, color: Color, level: MetricLevel) -> some View {
        if AppStyle.current.usesBeautifulUI {
            // Its stat tile: what it is (偏高 / 过高 after it), the number, the line under it, a meter for what has a
            // share; a high reading colors the number and its meter.
            BUIStatTile(level > .normal ? "\(title) · \(level == .critical ? "过高" : "偏高")" : title, value: value, detail: detail ?? " ",
                        tone: level == .critical ? .red : level > .normal ? .orange : .neutral, progress: fraction)
        } else {
            plainTile(title, value, detail: detail, fraction: fraction, color: color, level: level)
        }
    }

    private func plainTile(_ title: String, _ value: String, detail: String?, fraction: Double?, color: Color, level: MetricLevel) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            HStack(spacing: Theme.Space.xs) {
                Text(title).font(Theme.Font.label).foregroundStyle(.secondary)
                if level > .normal {
                    HStack(spacing: Theme.Space.xxs) {
                        Image(systemName: Theme.Icon.errorFilled).foregroundStyle(MetricColor.level(level))
                        Text(level == .critical ? "过高" : "偏高").foregroundStyle(.secondary)
                    }
                    .font(Theme.Font.labelStrong).lineLimit(1)
                }
            }
            Text(value).font(Theme.Font.title.monospacedDigit()).lineLimit(1).minimumScaleFactor(0.6)
            Text(detail ?? " ").font(Theme.Font.label.monospacedDigit()).foregroundStyle(.secondary).lineLimit(1).minimumScaleFactor(0.7)
            if let fraction { MeterBar(fraction: fraction, color: level > .normal ? MetricColor.level(level) : color) }
            else { Color.clear.scaledFrame(height: Theme.Size.dotXS) } // every tile the same height
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card(padding: 12)
        .accessibilityElement(children: .combine)
    }
}

/// Each core now (Linux), one thin column each.
private struct CoreBars: View {
    var cores: [Double]

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            HStack {
                Text("每个核心").font(Theme.Font.secondaryStrong)
                Spacer()
                Text("最忙 \(MetricFormat.percent(cores.max()))").font(Theme.Font.label.monospacedDigit()).foregroundStyle(.secondary)
            }
            GeometryReader { g in
                let gap: CGFloat = cores.count > 32 ? 1 : 2
                let w = max(1, (g.size.width - gap * CGFloat(cores.count - 1)) / CGFloat(cores.count))
                HStack(alignment: .bottom, spacing: gap) {
                    ForEach(cores.indices, id: \.self) { i in
                        ZStack(alignment: .bottom) {
                            RoundedRectangle(cornerRadius: min(2, w / 2)).fill(Color.primary.opacity(0.07))
                            RoundedRectangle(cornerRadius: min(2, w / 2)).fill(MetricColor.cpu)
                                .frame(height: max(2, g.size.height * cores[i] / 100))
                        }
                        .frame(width: w)
                    }
                }
            }
            .scaledFrame(height: Theme.Size.tapTarget)
        }
        .card(padding: 14)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("每个核心的使用率")
        .accessibilityValue("\(cores.count) 个核心，最忙 \(MetricFormat.percent(cores.max()))")
    }
}

/// 系统: OS, kernel, hardware, host name, uptime, versions. A long value wraps under itself (a CPU's full name).
private struct SystemFacts: View {
    var system: MachineSystem
    var machine: Machine?

    var body: some View {
        let facts = facts
        VStack(alignment: .leading, spacing: 0) {
            ForEach(facts.indices, id: \.self) { i in
                if i > 0 { Divider() }
                HStack(alignment: .firstTextBaseline, spacing: Theme.Space.m) {
                    Text(facts[i].0).foregroundStyle(.secondary).fixedSize()
                    Spacer(minLength: Theme.Space.s)
                    Text(facts[i].1).multilineTextAlignment(.trailing).textSelection(.enabled)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .font(Theme.Font.secondary)
                .padding(.vertical, Theme.Space.s)
            }
        }
        .card(padding: 14)
    }

    private var facts: [(String, String)] {
        var out: [(String, String)] = [("系统", system.name + (system.build.map { "（\($0)）" } ?? ""))]
        if let k = system.kernel, !k.isEmpty { out.append(("内核", k)) }
        out.append(("架构", system.arch ?? machine?.arch ?? "—"))
        if let m = system.model, !m.isEmpty { out.append(("机型", m)) }
        if let c = system.cpu, !c.isEmpty { out.append(("处理器", c)) }
        out.append(("核心", "\(system.cores)"))
        out.append(("内存", MetricFormat.bytes(system.memory)))
        if let h = machine?.hostname, !h.isEmpty { out.append(("主机名", h)) }
        if let boot = system.bootDate {
            out.append(("开机于", boot.formatted(date: .abbreviated, time: .shortened)))
            if machine?.online == true { out.append(("已运行", MetricFormat.uptime(since: boot))) }
        }
        if let h = machine?.herdrVersion, !h.isEmpty { out.append(("herdr", h)) }
        if let c = machine?.claudeVersion, !c.isEmpty { out.append(("Claude Code", c)) }
        if let n = machine?.nodeVersion, !n.isEmpty { out.append(("Persona node", n)) }
        return out
    }
}
