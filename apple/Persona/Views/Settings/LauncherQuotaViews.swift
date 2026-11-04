import SwiftUI

/// 设置 › 模型 › a launcher › 额度 (docs/launchers.md「额度」): what its key may still spend at its gateway, as a quota plugin
/// read it (titled with the gateway name the hub sends) —
/// a budget on the key, each account / provider it draws on with its windows (5 小时, 每周, 每月…), and the key's own use.
/// Read by the hub (a Persona launcher) or by a machine that has the launcher (a ~/.zshrc one: the key stays there).
struct LauncherQuotaSection: View {
    @Environment(AppModel.self) private var model
    var name: String
    @State private var answer: LauncherQuotaAnswer?
    @State private var loading = false
    @State private var failed: String?

    var body: some View {
        Section {
            if let failed {
                Label(failed, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger)
            } else if let a = answer {
                if let q = a.quota { report(q) } else {
                    Text(a.error ?? "读不到额度").font(Theme.Font.label).foregroundStyle(.secondary)
                }
            } else {
                HStack(spacing: Theme.Space.s) {
                    ProgressView()
                    Text("正在读取额度…").font(Theme.Font.label).foregroundStyle(.secondary)
                }
            }
        } header: {
            HStack {
                Text(answer?.quota.map { "额度 · " + $0.gatewayName } ?? "额度")
                Spacer()
                if answer?.unsupported != true {
                    Button { Task { await load(refresh: true) } } label: {
                        if loading { ProgressView().controlSize(.mini) } else { Image(systemName: Theme.Icon.retry) }
                    }
                    .disabled(loading)
                    .accessibilityLabel("刷新额度")
                }
            }
        } footer: {
            if let a = answer, a.quota != nil {
                Text(footer(a))
            }
        }
        .task { await load(refresh: false) }
    }

    @ViewBuilder private func report(_ q: LauncherQuota) -> some View {
        if let note = q.note { Text(note).font(Theme.Font.label).foregroundStyle(.secondary) }
        ForEach(q.budget ?? [], id: \.label) { w in QuotaWindowRow(window: w) }
        ForEach(q.pools ?? [], id: \.name) { p in QuotaPoolRow(pool: p) }
        if let usage = q.usage, !usage.isEmpty {
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                Text("这个 Key 的用量").font(Theme.Font.labelStrong).foregroundStyle(.secondary)
                ForEach(usage, id: \.label) { u in
                    LabeledContent(u.label) {
                        Text(QuotaText.usage(u)).font(Theme.Font.caption.monospacedDigit()).foregroundStyle(.secondary)
                    }
                    .font(Theme.Font.caption)
                }
            }
            .padding(.vertical, Theme.Space.xxs)
        }
        if (q.pools ?? []).isEmpty, (q.budget ?? []).isEmpty, q.note == nil {
            Text("这个 Key 没有设额度上限。").font(Theme.Font.label).foregroundStyle(.secondary)
        }
    }

    private func footer(_ a: LauncherQuotaAnswer) -> String {
        var parts: [String] = []
        if let k = a.quota?.keyName, !k.isEmpty { parts.append("网关上的 Key 名：\(k)") }
        if let m = a.machine, !m.isEmpty { parts.append("由 \(model.machine(model.hubRef(m))?.name ?? m) 读取，Key 不离开那台电脑") }
        if let t = a.checkedAt { parts.append(Fmt.relative(t) + "读的，一分钟内再打开用这次的") }
        return parts.joined(separator: "；") + "。"
    }

    private func load(refresh: Bool) async {
        guard let api = model.api else { return }
        loading = true
        defer { loading = false }
        do {
            answer = try await api.launcherQuota(name, refresh: refresh)
            failed = nil
        } catch {
            if let shown = error.shown { failed = shown }
        }
    }
}

/// An account or provider: its name and state, then its windows; per-model windows folded under it.
struct QuotaPoolRow: View {
    var pool: LauncherQuota.Pool
    @State private var showModels = false

    /// Something wrong with it (cooling down, used up, unreadable); "no quota interface" is just how it is.
    private var trouble: Bool { !["available", "ok", "unsupported", nil].contains(pool.state) }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            HStack(spacing: Theme.Space.sm) {
                Text(pool.name).font(Theme.Font.secondaryStrong).lineLimit(1)
                Spacer(minLength: Theme.Space.s)
                if let d = pool.detail, !d.isEmpty {
                    Text(d).font(Theme.Font.label).foregroundStyle(trouble ? Theme.cautionText : Color.secondary).lineLimit(2)
                        .multilineTextAlignment(.trailing)
                }
            }
            ForEach(pool.windows ?? [], id: \.label) { w in QuotaWindowRow(window: w) }
            if let models = pool.models, !models.isEmpty {
                DisclosureGroup(isExpanded: $showModels) {
                    ForEach(models, id: \.model) { m in
                        VStack(alignment: .leading, spacing: Theme.Space.xs) {
                            Text(m.model).font(Theme.Font.label.monospaced()).foregroundStyle(.secondary).lineLimit(1)
                            ForEach(m.windows, id: \.label) { w in QuotaWindowRow(window: w) }
                        }
                    }
                } label: {
                    Text("按模型 \(models.count)").font(Theme.Font.caption).lineLimit(1)
                }
            }
        }
        .padding(.vertical, Theme.Space.xxs)
    }
}

/// One window: its name, how much is used (of what), a bar, when it resets.
struct QuotaWindowRow: View {
    var window: LauncherQuota.Window

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            HStack(spacing: Theme.Space.sm) {
                Text(window.label).font(Theme.Font.caption).lineLimit(1)
                Spacer(minLength: Theme.Space.s)
                Text(QuotaText.amount(window)).font(Theme.Font.caption.monospacedDigit())
                    .foregroundStyle(window.exhausted == true ? Theme.danger : Color.secondary).lineLimit(1)
            }
            if let r = window.usedRatio {
                ProgressView(value: min(max(r, 0), 1))
                    .tint(QuotaText.tint(r, exhausted: window.exhausted == true))
                    .accessibilityLabel("\(window.label) 已用 \(Int((r * 100).rounded()))%")
            }
            if let reset = window.resetsAt {
                Text(QuotaText.reset(reset)).font(Theme.Font.label).foregroundStyle(.tertiary).lineLimit(1)
            }
        }
    }
}

/// How quota numbers read.
enum QuotaText {
    static func tint(_ ratio: Double, exhausted: Bool) -> Color {
        if exhausted || ratio >= 0.9 { return Theme.danger }
        if ratio >= 0.7 { return Theme.caution }
        return Theme.accent
    }

    /// "已用 64%", "6401 / 10000", "$2,100 / $3,000", "已用完".
    static func amount(_ w: LauncherQuota.Window) -> String {
        if w.exhausted == true && (w.usedRatio ?? 1) >= 1 { return "已用完" }
        switch w.unit {
        case "usd":
            if let u = w.used, let l = w.limit { return "$\(money(u)) / $\(money(l))" }
        case "tokens":
            if let u = w.used, let l = w.limit { return "\(count(u)) / \(count(l)) token" }
        case "requests":
            if let u = w.used, let l = w.limit { return "\(count(u)) / \(count(l)) 次" }
        case "credits":
            if let u = w.used, let l = w.limit { return "\(count(u)) / \(count(l))" }
        default: break
        }
        if let r = w.usedRatio { return "已用 \(Int((r * 100).rounded()))%" }
        return "—"
    }

    static func usage(_ u: LauncherQuota.Usage) -> String {
        var s = "\(count(Double(u.requests))) 次 · \(count(Double(u.tokens))) token"
        if let c = u.costUSD { s += " · $\(money(c))" }
        return s
    }

    static func reset(_ d: Date) -> String {
        d.timeIntervalSinceNow <= 0 ? "已重置" : d.formatted(.dateTime.month().day().hour().minute()) + " 重置"
    }

    /// 1.2 万, 3.4 亿.
    static func count(_ v: Double) -> String {
        func short(_ x: Double) -> String {
            let s = String(format: "%.1f", x)
            return s.hasSuffix(".0") ? String(s.dropLast(2)) : s
        }
        switch abs(v) {
        case 1e8...: return short(v / 1e8) + " 亿"
        case 1e4...: return short(v / 1e4) + " 万"
        default: return String(format: "%.0f", v)
        }
    }

    static func money(_ v: Double) -> String { v >= 100 ? String(format: "%.0f", v) : String(format: "%.2f", v) }
}
