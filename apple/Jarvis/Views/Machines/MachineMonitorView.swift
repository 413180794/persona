import SwiftUI

/// One machine's system and its last hours (docs/machines.md「机器详情」): what it runs on, the values now, and
/// CPU / memory / network / load over 1, 6 or 24 hours. Hold a chart to read a moment (all charts follow). Refreshes
/// while on screen and the app is in front: every 10 s for the hour, every minute for the longer ranges.
struct MachineMonitorView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var phase
    var machineID: String
    @State private var range: MetricRange = Self.initialRange
    @State private var data: MachineMetrics?
    @State private var shownRange: MetricRange?
    @State private var failure: String?
    @State private var selection: Date?

    private static var initialRange: MetricRange {
        #if DEBUG
        // `-openMonitorRange 6h|24h`: the detail opens on that range (screenshots, UI tests).
        if let r = UserDefaults.standard.string(forKey: "openMonitorRange").flatMap(MetricRange.init(rawValue:)) { return r }
        #endif
        return .hour
    }

    var body: some View {
        let machine = model.machine(machineID)
        let system = data?.system ?? machine?.system
        let online = data?.online ?? machine?.online ?? false
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.l) {
                header(machine, system: system, online: online)
                if let s = data?.latest, online { NowGrid(sample: s, cores: system?.cores) }
                Picker("时间范围", selection: $range) {
                    ForEach(MetricRange.allCases) { Text($0.title).tag($0) }
                }
                .pickerStyle(.segmented)
                .accessibilityIdentifier("metric-range")
                charts(system: system)
                if let cores = data?.latest?.cores, online, cores.count > 1 { CoreBars(cores: cores) }
                if let system { SystemFacts(system: system, machine: machine) }
                Text("历史保留 24 小时。最近 1 小时每 10 秒一个点，更早的每分钟一个点；中间断开的时段不连线。")
                    .font(Theme.Font.label).foregroundStyle(.secondary)
                    .padding(.horizontal, Theme.Space.xs)
            }
            .padding(Theme.Space.l)
        }
        .themedPage()
        .navigationTitle(machine?.name ?? machineID)
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await load() }
        .task(id: Poll(range: range, active: phase == .active)) {
            guard phase == .active else { return }
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(range == .hour ? 10 : 60))
            }
        }
    }

    private struct Poll: Hashable { var range: MetricRange; var active: Bool }

    private func load() async {
        guard let api = model.api else { return }
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
                failure = "hub 还不支持机器监控：部署新版本的 hub 和这台电脑上的 node 后再看。"
            } else if data == nil {
                failure = error.shown ?? "连不上 hub。连上后这里会自动刷新。"
            }
        }
    }

    private func header(_ m: Machine?, system: MachineSystem?, online: Bool) -> some View {
        HStack(spacing: Theme.Space.l) {
            OSBadge(system: system, fallback: m?.symbol ?? "desktopcomputer", size: 54, online: online)
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                Text(system?.name ?? m?.os ?? "未知系统").font(Theme.Font.heading).lineLimit(2)
                HStack(spacing: Theme.Space.sm) {
                    Circle().fill(online ? Theme.positive : Color.secondary).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
                    Text(online ? "在线" : "离线").font(Theme.Font.secondaryStrong).foregroundStyle(online ? Theme.positive : .secondary)
                    if online, let boot = system?.bootDate {
                        Text("已运行 " + MetricFormat.uptime(since: boot)).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1)
                    } else if !online, let seen = m?.lastSeen, seen.timeIntervalSince1970 > 0 {
                        Text("最后在线 " + Fmt.relative(seen)).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
            }
            Spacer(minLength: 0)
        }
        .card(padding: 14)
    }

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

    private func tile(_ title: String, _ value: String, detail: String?, fraction: Double?, color: Color, level: MetricLevel) -> some View {
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

/// 系统: OS, kernel, hardware, versions.
private struct SystemFacts: View {
    var system: MachineSystem
    var machine: Machine?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("系统").font(Theme.Font.secondaryStrong).padding(.bottom, Theme.Space.s)
            row("系统", system.name + (system.build.map { "（\($0)）" } ?? ""))
            if let k = system.kernel, !k.isEmpty { row("内核", k) }
            row("架构", system.arch ?? machine?.arch ?? "—")
            if let m = system.model, !m.isEmpty { row("机型", m) }
            if let c = system.cpu, !c.isEmpty { row("处理器", c) }
            row("核心", "\(system.cores)")
            row("内存", MetricFormat.bytes(system.memory))
            if let boot = system.bootDate { row("开机于", boot.formatted(date: .abbreviated, time: .shortened)) }
            if let h = machine?.herdrVersion, !h.isEmpty { row("herdr", h) }
            if let c = machine?.claudeVersion, !c.isEmpty { row("Claude Code", c) }
            if let n = machine?.nodeVersion, !n.isEmpty { row("Jarvis node", n, last: true) }
        }
        .card(padding: 14)
    }

    private func row(_ k: String, _ v: String, last: Bool = false) -> some View {
        VStack(spacing: 0) {
            HStack(alignment: .firstTextBaseline, spacing: Theme.Space.m) {
                Text(k).foregroundStyle(.secondary)
                Spacer(minLength: 8)
                Text(v).multilineTextAlignment(.trailing).textSelection(.enabled)
            }
            .font(Theme.Font.secondary)
            .padding(.vertical, Theme.Space.s)
            if !last { Divider() }
        }
    }
}
