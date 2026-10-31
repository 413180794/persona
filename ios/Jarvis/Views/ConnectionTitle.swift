import SwiftUI

/// A main screen's title spot (Agents, 助手, 收件箱, 机器): its title, or while the hub is out of reach a small spinner
/// and 等待网络 / 连接中… / 更新中… (ConnectionStatus); a tap on that explains. Put it in the `.principal` toolbar item.
struct ConnectionTitle: View {
    /// nil: the screen shows no title (Agents) and the spot is empty while all is well.
    var title: String?
    @Environment(AppModel.self) private var model
    @State private var details = false

    var body: some View {
        let phase = model.connection.shown
        ZStack {
            if phase != .online {
                Button { Haptics.tap(); details = true } label: { ConnectionLabel(phase: phase) }
                    .buttonStyle(.plain)
                    .accessibilityLabel("\(phase.title)，查看连接详情")
                    .accessibilityIdentifier("connection-title")
                    .transition(.opacity)
            } else if let title {
                Text(title).font(Theme.Font.heading).lineLimit(1).transition(.opacity)
            }
        }
        .animation(Theme.Motion.standard, value: phase)
        .connectionDetails($details)
        #if DEBUG
        // `-openConnectionDetails YES` (with `-showConnection …`): the details open (screenshots).
        .task { if phase != .online, UserDefaults.standard.bool(forKey: "openConnectionDetails") { details = true } }
        #endif
    }
}

/// Spinner and state, at title size or (compact) subtitle size.
struct ConnectionLabel: View {
    var phase: ConnectionStatus.Phase
    var compact = false

    var body: some View {
        HStack(spacing: compact ? 5 : 7) {
            ProgressView().controlSize(compact ? .mini : .small)
            Text(phase.title)
                .font(compact ? Theme.Font.label : Theme.Font.heading)
                .foregroundStyle(compact ? .secondary : .primary)
                .lineLimit(1)
                .contentTransition(.opacity)
        }
    }
}

extension View {
    /// The connection details as a small popover from the view (the spinner title).
    func connectionDetails(_ shown: Binding<Bool>) -> some View {
        popover(isPresented: shown) {
            ConnectionDetails().presentationCompactAdaptation(.popover)
        }
    }
}

/// Why the title says what it says, since when, and 立即重试: what used to be a banner over the list.
struct ConnectionDetails: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        let status = model.connection
        // What the title says; once it is back to normal, the live state (already connected again, or not yet shown).
        let phase = status.shown == .online ? status.phase : status.shown
        TimelineView(.periodic(from: .now, by: 1)) { ctx in
            VStack(alignment: .leading, spacing: Theme.Space.m) {
                HStack(spacing: Theme.Space.s) {
                    if phase == .online {
                        Image(systemName: Theme.Icon.successFilled).foregroundStyle(Theme.positive)
                        Text("已连上 hub").font(Theme.Font.heading)
                    } else {
                        ConnectionLabel(phase: phase)
                    }
                }
                Text(Self.explanation(phase, stalled: model.live.stalled)).font(Theme.Font.secondary).foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                let lines = Self.facts(phase: phase, link: model.live.state, nextTry: model.live.nextTry, lastOnline: status.lastOnline,
                                       rtt: model.live.rtt, outbox: model.outboxCount, now: ctx.date)
                if !lines.isEmpty {
                    VStack(alignment: .leading, spacing: Theme.Space.xs) {
                        ForEach(lines, id: \.self) { Text($0) }
                    }
                    .font(Theme.Font.caption)
                    .foregroundStyle(.secondary)
                }
                if phase != .online {
                    Button {
                        Haptics.tap()
                        model.live.reconnectNow()
                        Task { await model.refresh() }
                    } label: {
                        Text("立即重试").lineLimit(1).frame(maxWidth: .infinity)
                    }
                    .themedButton(prominent: true)
                    .padding(.top, Theme.Space.xxs)
                }
            }
            .padding(Theme.Space.l)
            .frame(width: 300, alignment: .leading)
        }
    }

    static func explanation(_ phase: ConnectionStatus.Phase, stalled: Bool) -> String {
        switch phase {
        case .online: "消息和状态都是最新的。"
        case .waitingForNetwork: "iPhone 现在没有网络。网络恢复后会自动连上，期间写的消息会自动发出去。"
        case .connecting:
            stalled ? "网络很慢：hub 好几秒没有回应了，正在等它。"
                : "有网络，但连不上 hub（它所在的机器可能关机、休眠或断网）。会一直自动重试。"
        case .updating: "已经连上，正在补上断开期间的消息和待处理的请求。"
        }
    }

    /// Short lines under the explanation: why the last try failed, when the next is, since when, what is waiting.
    static func facts(phase: ConnectionStatus.Phase, link: LiveConnection.State, nextTry: Date?, lastOnline: Date?,
                      rtt: TimeInterval?, outbox: Int, now: Date) -> [String] {
        var out: [String] = []
        if phase == .connecting, case .failed(let why) = link { out.append("上次失败：\(why)") }
        if phase == .connecting, let nextTry {
            let s = Int(ceil(nextTry.timeIntervalSince(now)))
            out.append(s > 0 ? "\(s) 秒后重试" : "正在重试…")
        }
        if phase != .online, let lastOnline { out.append("上次连上：\(ago(lastOnline, now: now))") }
        if phase == .online, let rtt { out.append(String(format: "延迟 %.0f 毫秒", rtt * 1000)) }
        if outbox > 0 { out.append("\(outbox) 条消息连上后自动发送") }
        return out
    }

    /// "12 秒前", "3 分钟前", then the time of day.
    static func ago(_ date: Date, now: Date) -> String {
        let s = max(0, Int(now.timeIntervalSince(date)))
        if s < 60 { return "\(s) 秒前" }
        if s < 3600 { return "\(s / 60) 分钟前" }
        return Fmt.relative(date, now: now)
    }
}
