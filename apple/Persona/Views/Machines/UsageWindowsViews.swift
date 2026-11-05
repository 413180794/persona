import SwiftUI

/// The subscription accounts the hubs know (docs/usage-windows.md), shared by 机器's 订阅额度 card on the phone and the
/// Mac's bars at the foot of the sections column. Empty until a hub answers, and when none has any (nothing is drawn).
@Observable
final class UsageWindowsStore {
    static let shared = UsageWindowsStore()

    private(set) var accounts: [UsageAccount] = []

    /// Every group's accounts (each hub merges its own machines' and gateways'). A group that fails keeps its last
    /// answer; one before this route answers none.
    func load(_ model: AppModel) async {
        var all: [UsageAccount] = []
        let multi = model.isMultiHub
        for g in model.groupIDs {
            guard let api = model.api(g) else { continue }
            do {
                var list = try await api.usageWindows()
                if multi { list = list.map { var a = $0; a.id = g + "|" + a.id; return a } }
                all += list
            } catch {
                if error.isCancellation { return }
                all += accounts.filter { !multi || $0.id.hasPrefix(g + "|") }
            }
        }
        if all != accounts { accounts = all }
    }

    /// Asks again every minute until cancelled (the caller runs it only while it is on screen and the app in front).
    func poll(_ model: AppModel) async {
        while !Task.isCancelled {
            await load(model)
            try? await Task.sleep(for: UsageWindows.pollInterval)
        }
    }
}

/// A bar: the track, the used part (caution colour from 80%) and, with `expected`, a tick where an even pace would be.
struct UsageMeter: View {
    var used: Double
    var expected: Double?
    var tall = false

    var body: some View {
        let height = tall ? Theme.Size.barWide : Theme.Size.bar
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                Capsule().fill(Theme.Fill.chip)
                Capsule().fill(UsageWindows.isWarn(used) ? Theme.caution : Theme.accent)
                    .frame(width: geo.size.width * UsageWindows.fraction(used))
                if let expected {
                    Capsule().fill(Color.secondary)
                        .frame(width: Theme.Size.rule, height: height + Theme.Space.xs)
                        .offset(x: geo.size.width * UsageWindows.fraction(expected) - Theme.Size.rule / 2)
                }
            }
        }
        .frame(height: height)
        .animation(Theme.Motion.state, value: used)
        .accessibilityHidden(true)
    }
}

/// One account, window by window: its name and the sample's age, plan and sources, what the gateway could not read,
/// then a row per window (length, used, pace, reset, bar).
struct UsageAccountBlock: View {
    var account: UsageAccount
    var now: Date

    var body: some View {
        let a = account
        let from = UsageWindows.sourceNames(a)
        let plan = UsageWindows.planNote(a)
        let problem = UsageWindows.problemText(a)
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
                    Text(a.name).font(Theme.Font.secondaryStrong).foregroundStyle(Color.primary).lineLimit(1)
                    Spacer(minLength: Theme.Space.s)
                    Text(UsageWindows.ageLabel(a, now: now)).font(Theme.Font.label.monospacedDigit())
                        .foregroundStyle(Color.secondary).lineLimit(1)
                }
                let note = ([plan] + [from.joined(separator: " · ")]).filter { !$0.isEmpty }.joined(separator: " · ")
                if !note.isEmpty {
                    Text(note).font(Theme.Font.label).foregroundStyle(Color.secondary).lineLimit(1)
                        .accessibilityLabel(from.isEmpty ? note : (plan.isEmpty ? "" : plan + "，") + "来源：" + from.joined(separator: "、"))
                }
                if !problem.isEmpty {
                    Text(problem).font(Theme.Font.label).foregroundStyle(Theme.cautionText).fixedSize(horizontal: false, vertical: true)
                }
            }
            ForEach(a.windows, id: \.key) { w in UsageWindowRow(name: a.name, window: w, now: now) }
        }
        .opacity(a.stale ? 0.6 : 1)
        .accessibilityElement(children: .contain)
    }
}

/// A window: 5h / 7d (and the model it is for), used, the pace when it is off, the reset, a bar with the pace's tick.
struct UsageWindowRow: View {
    var name: String
    var window: UsageWindow
    var now: Date

    var body: some View {
        let w = window
        let pace = UsageWindows.pace(w, now: now)
        let used = UsageWindows.percentText(w.usedPercent)
        let reset = UsageWindows.resetLabel(w.resetsAt, now: now)
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            if let m = w.model {
                Text(m).font(Theme.Font.label).foregroundStyle(Color.secondary).lineLimit(1)
            }
            HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
                Text(w.label).font(Theme.Font.label).foregroundStyle(Color.secondary).lineLimit(1)
                    .scaledFrame(width: Theme.Size.wellXS, alignment: .leading)
                Text(used).font(Theme.Font.labelStrong.monospacedDigit())
                    .foregroundStyle(UsageWindows.isWarn(w.usedPercent) ? Theme.cautionText : Color.primary).lineLimit(1)
                if let pace, pace.state != .on {
                    Text(UsageWindows.paceLabel(pace.state)).font(Theme.Font.label)
                        .foregroundStyle(pace.state == .over ? Theme.cautionText : Theme.positive).lineLimit(1)
                }
                Spacer(minLength: Theme.Space.s)
                Text(reset).font(Theme.Font.label.monospacedDigit()).foregroundStyle(.tertiary).lineLimit(1)
            }
            UsageMeter(used: w.usedPercent, expected: pace?.expected, tall: true)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([name, w.label, w.model, "已用 \(used)", pace.flatMap { $0.state == .on ? nil : UsageWindows.paceLabel($0.state) }, reset]
            .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: "，"))
    }
}

/// Every live account, one after another, divided; the ages and countdowns tick each second while it is on screen.
struct UsageAccountsList: View {
    var accounts: [UsageAccount]

    var body: some View {
        TimelineView(.periodic(from: .now, by: 1)) { ctx in
            let live = UsageWindows.live(accounts, now: ctx.date)
            VStack(alignment: .leading, spacing: Theme.Space.m) {
                ForEach(Array(live.enumerated()), id: \.element.id) { i, a in
                    if i > 0 { Divider() }
                    UsageAccountBlock(account: a, now: ctx.date)
                }
            }
        }
    }
}

#if !os(macOS)
/// 机器's 订阅额度 (docs/usage-windows.md「界面」): the accounts' 5h / 7d windows under the machines' cards. Not drawn while
/// the hubs have none. The Mac has the same at the foot of its sections column (`MacUsageFooter`).
struct UsageWindowsCard: View {
    var store = UsageWindowsStore.shared

    var body: some View {
        let live = UsageWindows.live(store.accounts)
        if !live.isEmpty {
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                Text("订阅额度").font(Theme.Font.sectionHeader).foregroundStyle(.secondary).lineLimit(1)
                    .padding(.horizontal, Theme.Space.xs)
                    .accessibilityAddTraits(.isHeader)
                UsageAccountsList(accounts: store.accounts)
                    .card()
            }
            .padding(.top, Theme.Space.s)
            .accessibilityIdentifier("machines.usage")
        }
    }
}
#endif
