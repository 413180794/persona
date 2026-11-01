import SwiftUI

struct MachineDashboardRoute: Hashable {}
struct MachineMonitorRoute: Hashable { var id: String }

/// 机器监控: every machine on one screen (docs/machines.md「Dashboard」), a card each: online or not, its OS, CPU /
/// memory / network now and over the last hour, and what is too high called out. Refreshes every 10 s while it is on
/// screen and the app is in front; a tap opens the machine's charts.
struct MachineDashboardView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var phase
    @State private var list: [MachineMetrics] = []
    @State private var loaded = false
    @State private var failure: String?

    var body: some View {
        ScrollView {
            if !list.isEmpty { summary.padding(.horizontal, Theme.Space.l).padding(.top, Theme.Space.m) }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 330), spacing: 14)], spacing: 14) {
                ForEach(list) { m in
                    NavigationLink { MachineMonitorView(machineID: m.machineID) } label: {
                        MachineMetricsCard(metrics: m, machine: model.machine(m.machineID))
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.horizontal, Theme.Space.l)
            .padding(.vertical, Theme.Space.m)
            if loaded && list.isEmpty {
                EmptyStateView(symbol: Theme.Icon.context, title: failure == nil ? "还没有机器" : "看不到监控数据",
                               message: failure ?? "hub 连上各台电脑后，这里会显示它们的 CPU、内存和网络。")
                    .padding(.top, 60)
            } else if !loaded {
                ProgressView().padding(.top, 80)
            }
        }
        .themedPage()
        .navigationTitle("机器监控")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await load() }
        #if os(macOS)
        .macRefreshes { await load() } // ⌘R: no pull to refresh on the Mac
        #endif
        .task(id: phase == .active) {
            guard phase == .active else { return } // in the background: nothing to draw, nothing to fetch
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(10))
            }
        }
    }

    private var summary: some View {
        let online = list.filter(\.online).count
        let levels = list.compactMap { m in m.online ? m.latest.flatMap { MetricAlert.of($0, cores: m.system?.cores).first?.level } : nil }
        let alerting = levels.count
        return HStack(spacing: Theme.Space.m) {
            Label("\(online)/\(list.count) 台在线", systemImage: Theme.Icon.dot)
                .labelStyle(DotLabel(color: online == list.count ? Theme.positive : .secondary))
            if alerting > 0 {
                HStack(spacing: Theme.Space.xs) {
                    Image(systemName: Theme.Icon.errorFilled).foregroundStyle(MetricColor.level(levels.max() ?? .warning))
                    Text("\(alerting) 台需要注意")
                }
            }
            Spacer()
            Text("每 10 秒更新").foregroundStyle(.tertiary)
        }
        .font(Theme.Font.captionMedium)
        .lineLimit(1)
        .padding(.horizontal, Theme.Space.xs)
    }

    private func load() async {
        guard let api = model.api else { return }
        do {
            let fresh = try await api.metricsOverview()
            withAnimation(loaded ? .default : nil) { list = fresh }
            failure = nil
        } catch {
            if error.isCancellation { return }
            if let e = error as? APIError, e.status == 404 {
                failure = "hub 还不支持机器监控：部署新版本的 hub 和各台电脑上的 node 后再看。"
            } else if list.isEmpty {
                // Offline errors are quiet elsewhere (the title says it); here the page would claim there are no machines.
                failure = error.shown ?? "连不上 hub。连上后这里会自动刷新。"
            }
        }
        loaded = true
    }
}

private struct DotLabel: LabelStyle {
    var color: Color
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: Theme.Space.sm) {
            Circle().fill(color).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
            configuration.title
        }
    }
}

/// One machine on the dashboard.
struct MachineMetricsCard: View {
    var metrics: MachineMetrics
    var machine: Machine?

    var body: some View {
        let s = metrics.latest
        let alerts = s.map { MetricAlert.of($0, cores: metrics.system?.cores) } ?? []
        let worst = metrics.online ? alerts.first?.level ?? .normal : .normal
        let end = Date.now
        let domain = end.addingTimeInterval(-3600)...end
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            HStack(spacing: Theme.Space.m) {
                OSBadge(system: metrics.system ?? machine?.system, fallback: machine?.symbol ?? "desktopcomputer", size: 40, online: metrics.online)
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(machine?.name ?? metrics.machineID).font(Theme.Font.heading).lineLimit(1)
                    Text(subtitle).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                }
                Spacer(minLength: 6)
                status
            }
            if metrics.online, !alerts.isEmpty {
                HStack(spacing: Theme.Space.sm) {
                    ForEach(alerts, id: \.label) { a in AlertChip(alert: a) }
                }
            }
            HStack(alignment: .top, spacing: Theme.Space.m) {
                cell("CPU", value: metrics.online ? MetricFormat.percent(s?.cpu) : "—",
                     detail: s?.load.map { "负载 " + MetricFormat.load($0) },
                     level: MetricLevel.of(metrics.online ? s?.cpu : nil, warn: 75, crit: 90)) {
                    Sparkline(points: metrics.points, series: [ChartSeries(name: "CPU", color: MetricColor.cpu) { $0.cpu }],
                              domain: domain, yMax: 100)
                }
                cell("内存", value: metrics.online ? MetricFormat.percent(s?.memPercent) : "—",
                     detail: s.map { MetricFormat.bytes($0.memUsed) + " / " + MetricFormat.bytes($0.memTotal) },
                     level: MetricLevel.of(metrics.online ? s?.memPercent : nil, warn: 80, crit: 90)) {
                    let total = Double(s?.memTotal ?? metrics.system?.memory ?? 0)
                    Sparkline(points: metrics.points, series: [ChartSeries(name: "内存", color: MetricColor.mem) { p in
                        p.mem.map { total > 0 ? 100 * $0 / total : 0 }
                    }], domain: domain, yMax: 100)
                }
                cell("网络", value: metrics.online ? "↓ " + MetricFormat.rate(s?.netRx) : "—",
                     detail: metrics.online ? "↑ " + MetricFormat.rate(s?.netTx) : nil, level: .normal) {
                    Sparkline(points: metrics.points, series: [
                        ChartSeries(name: "下行", color: MetricColor.rx) { $0.rx },
                        ChartSeries(name: "上行", color: MetricColor.tx) { $0.tx },
                    ], domain: domain, yMax: nil)
                }
            }
            .opacity(metrics.online ? 1 : 0.55)
        }
        .card(padding: 14)
        .overlay {
            if worst > .normal {
                RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(MetricColor.level(worst).opacity(0.7), lineWidth: 1.5)
            }
        }
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }

    private var subtitle: String {
        guard let sys = metrics.system ?? machine?.system else { return machine?.hostname ?? "" }
        var parts = [sys.name]
        if sys.cores > 0 { parts.append("\(sys.cores) 核") }
        if sys.memory > 0 { parts.append(MetricFormat.bytes(sys.memory)) }
        return parts.joined(separator: " · ")
    }

    @ViewBuilder private var status: some View {
        if metrics.online {
            HStack(spacing: Theme.Space.sm) {
                Circle().fill(Theme.positive).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
                Text("在线").font(Theme.Font.labelStrong).foregroundStyle(Theme.positive)
            }
            .fixedSize()
        } else {
            VStack(alignment: .trailing, spacing: Theme.Space.xxs) {
                Text("离线").font(Theme.Font.labelStrong).foregroundStyle(.secondary)
                if let seen = machine?.lastSeen, seen.timeIntervalSince1970 > 0 {
                    Text(Fmt.relative(seen)).font(Theme.Font.label).foregroundStyle(.tertiary)
                }
            }
            .fixedSize()
        }
    }

    private func cell(_ title: String, value: String, detail: String?, level: MetricLevel,
                      @ViewBuilder chart: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            HStack(spacing: Theme.Space.xs) {
                Text(title).font(Theme.Font.label).foregroundStyle(.secondary)
                if level > .normal {
                    Image(systemName: Theme.Icon.errorFilled).font(.system(size: 9)).foregroundStyle(MetricColor.level(level))
                }
            }
            Text(value).font(Theme.Font.bodyStrong.monospacedDigit()).lineLimit(1).minimumScaleFactor(0.7)
            Text(detail ?? " ").font(Theme.Font.label.monospacedDigit()).foregroundStyle(.secondary).lineLimit(1).minimumScaleFactor(0.7)
            chart().scaledFrame(height: Theme.Size.wellS).padding(.top, Theme.Space.xxs)
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
            Text(alert.label).foregroundStyle(.primary)
        }
        .font(Theme.Font.labelStrong)
        .lineLimit(1)
        .padding(.horizontal, Theme.Space.s)
        .padding(.vertical, Theme.Space.xs)
        .background(color.opacity(0.16), in: .capsule)
        .accessibilityLabel((alert.level == .critical ? "严重：" : "注意：") + alert.label)
    }
}
