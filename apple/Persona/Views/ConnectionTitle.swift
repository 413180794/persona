import SwiftUI

/// A main screen's title spot (Agents, Assistant, Inbox, Machines): its title, or while the hub is out of reach a small spinner
/// and Waiting for Network / Connecting… / Updating… (ConnectionStatus); a tap on that explains. Put it in the `.principal` toolbar item.
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
    @Environment(AppModel.self) private var model
    var phase: ConnectionStatus.Phase
    var compact = false

    var body: some View {
        HStack(spacing: compact ? 5 : 7) {
            if phase == .rejected {
                // Not trying anything the user can wait out: no spinner.
                Image(systemName: Theme.Icon.error)
                    .font(compact ? Theme.Font.label : Theme.Font.heading)
                    .foregroundStyle(Theme.caution)
            } else {
                ProgressView().controlSize(compact ? .mini : .small)
            }
            // The hub restarting into a new version: the connection going away is expected (docs/hub-update.md).
            Text(phase == .connecting && model.hubUpdate?.restarting == true ? String(localized: "hub 正在更新…") : phase.title)
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

/// Why the title says what it says, since when, and Retry Now: what used to be a banner over the list.
struct ConnectionDetails: View {
    @Environment(AppModel.self) private var model
    @State private var pairing = false

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
                Text(phase == .connecting && model.hubUpdate?.restarting == true
                     ? String(localized: "hub 正在更新到新版本，重启期间会断开一会儿，好了自动连上。")
                     : Self.explanation(phase, stalled: model.live.stalled)).font(Theme.Font.secondary).foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                let lines = Self.facts(phase: phase, link: model.live.state, nextTry: model.live.nextTry, lastOnline: status.lastOnline,
                                       rtt: model.live.rtt, outbox: model.outboxCount, now: ctx.date, ssh: sshFact)
                if !lines.isEmpty {
                    VStack(alignment: .leading, spacing: Theme.Space.xs) {
                        ForEach(lines, id: \.self) { Text($0) }
                    }
                    .font(Theme.Font.caption)
                    .foregroundStyle(.secondary)
                }
                if phase == .rejected {
                    Button {
                        Haptics.tap()
                        pairing = true
                    } label: {
                        Text("重新配对").lineLimit(1).frame(maxWidth: .infinity)
                    }
                    .themedButton(prominent: true)
                    .padding(.top, Theme.Space.xxs)
                    .accessibilityIdentifier("connection.repair")
                }
                if phase != .online {
                    Button {
                        Haptics.tap()
                        model.live.reconnectNow()
                        Task { await model.refresh() }
                    } label: {
                        Text("立即重试").lineLimit(1).frame(maxWidth: .infinity)
                    }
                    .themedButton(prominent: phase != .rejected)
                    .padding(.top, Theme.Space.xxs)
                }
            }
            .padding(Theme.Space.l)
            .scaledFrame(width: Theme.Size.box300, alignment: .leading)
        }
        .sheet(isPresented: $pairing) { PairView() }
    }

    /// The hub-level group's SSH tunnel, when its hub has one.
    private var sshFact: SSHFact? {
        guard let group = model.hub?.group, let t = model.tunnel(group) else { return nil }
        var problem: String?
        if case .down(let why) = t.state { problem = why }
        return SSHFact(server: t.hub.ssh?.label ?? "", routed: t.routed, problem: problem)
    }

    static func explanation(_ phase: ConnectionStatus.Phase, stalled: Bool) -> String {
        switch phase {
        case .online: String(localized: "消息和状态都是最新的。")
        #if os(macOS)
        case .waitingForNetwork: String(localized: "这台 Mac 现在没有网络。网络恢复后会自动连上，期间写的消息会自动发出去。")
        #else
        case .waitingForNetwork: String(localized: "iPhone 现在没有网络。网络恢复后会自动连上，期间写的消息会自动发出去。")
        #endif
        case .connecting:
            stalled ? String(localized: "网络很慢：hub 好几秒没有回应了，正在等它。")
                : String(localized: "有网络，但连不上 hub（它所在的机器可能关机、休眠或断网）。会一直自动重试。")
        case .updating: String(localized: "已经连上，正在补上断开期间的消息和待处理的请求。")
        #if os(macOS)
        case .rejected: String(localized: "hub 不再认这台 Mac 的凭据：它可能在「设置 › 设备」里被撤销了，或者上次配对没有完成。重新粘贴配对链接就能恢复。")
        #else
        case .rejected: String(localized: "hub 不再认这台设备的凭据：它可能在「设置 › 设备」里被撤销了，或者上次配对没有完成。在 hub 所在的机器上生成新的配对码，重新扫码就能恢复。")
        #endif
        }
    }

    /// The hub's SSH tunnel as the details mention it (docs/network.md「经 SSH 隧道连 hub」): which server, whether the
    /// traffic goes through it now, and why it is not up.
    struct SSHFact: Equatable {
        var server: String
        var routed: Bool
        var problem: String?
    }

    /// Short lines under the explanation: why the last try failed, when the next is, since when, what is waiting, and
    /// the way to the hub when it has an SSH tunnel (straight, or through it).
    static func facts(phase: ConnectionStatus.Phase, link: LiveConnection.State, nextTry: Date?, lastOnline: Date?,
                      rtt: TimeInterval?, outbox: Int, now: Date, ssh: SSHFact? = nil) -> [String] {
        var out: [String] = []
        if let ssh {
            out.append(ssh.routed ? String(localized: "线路：经 SSH 隧道（\(ssh.server)）") : String(localized: "线路：直连"))
            if phase != .online, let why = ssh.problem { out.append(String(localized: "SSH 隧道：\(why)")) }
        }
        if phase == .connecting || phase == .rejected, case .failed(let why) = link { out.append(String(localized: "上次失败：\(why)")) }
        if phase == .connecting || phase == .rejected, let nextTry {
            let s = Int(ceil(nextTry.timeIntervalSince(now)))
            out.append(s > 0 ? String(localized: "\(s) 秒后重试") : String(localized: "正在重试…"))
        }
        if phase != .online, let lastOnline { out.append(String(localized: "上次连上：\(ago(lastOnline, now: now))")) }
        if phase == .online, let rtt {
            let ms = String(format: "%.0f", rtt * 1000)
            out.append(String(localized: "延迟 \(ms) 毫秒"))
        }
        if outbox > 0 { out.append(String(localized: "\(outbox) 条消息连上后自动发送")) }
        return out
    }

    /// "12 seconds ago", "3 minutes ago", then the time of day.
    static func ago(_ date: Date, now: Date) -> String {
        let s = max(0, Int(now.timeIntervalSince(date)))
        if s < 60 { return String(localized: "\(s) 秒前") }
        if s < 3600 { return String(localized: "\(s / 60) 分钟前") }
        return Fmt.relative(date, now: now)
    }
}
