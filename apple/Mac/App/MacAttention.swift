import AppKit
import Foundation
import UserNotifications

/// What the Mac does outside its window when an agent needs you or finishes (docs/mac.md「和 iOS 的对应」):
/// the Dock badge (how many need you) and a system notification for each agent that just turned 需要你 / 做完了.
/// The phone gets these as pushes from the hub; the Mac has no APNs entitlement, so it watches its own session list
/// (the WebSocket stays open while the app runs) and follows the same 设置 › 通知 choices (`NotifyPrefs`).
enum MacAttention {
    /// The notifications to post for a session-list change: agents that turned needs-you or finished. Not for the
    /// chat on screen while the app is in front, a muted agent, a session the list just learned about, or a finish
    /// right after a needs-you (the same stop). Jarvis's own conversations ping when they ask, not when they reply.
    static func transitions(old: [Session], new: [Session], open: SessionKey?, appActive: Bool, prefs: NotifyPrefs,
                            now: Date, muted: (Session) -> Bool) -> [(Session, AgentState)] {
        let before = Dictionary(old.map { ($0.key, $0) }, uniquingKeysWith: { a, _ in a })
        let longTurn = TimeInterval(prefs.longTurnSeconds ?? 60) // the hub's default (notify.go)
        return new.compactMap { s in
            guard let was = before[s.key], was.state != s.state, !muted(s) else { return nil }
            if appActive && s.key == open { return nil }
            switch s.state {
            case .needsYou:
                return prefs.input ? (s, .needsYou) : nil
            case .unread:
                guard !s.assistant, was.state != .needsYou else { return nil }
                switch prefs.done {
                case .off: return nil
                case .always: return (s, .unread)
                case .long:
                    // How long the turn ran: since the hub saw it start working.
                    guard was.state == .working, let since = was.stateSince, now.timeIntervalSince(since) >= longTurn else { return nil }
                    return (s, .unread)
                }
            default:
                return nil
            }
        }
    }

    /// The Dock badge: agents and Jarvis conversations waiting on you, muted ones left out (the sidebar's badges).
    static func badge(_ sessions: [Session], muted: (Session) -> Bool) -> String? {
        let n = sessions.filter { $0.state == .needsYou && !muted($0) }.count
        return n > 0 ? "\(n)" : nil
    }

    static func text(_ s: Session, _ state: AgentState) -> (title: String, body: String) {
        let title = s.displayTitle + (state == .needsYou ? " 需要你" : " 做完了")
        let body = s.lastText?.replacingOccurrences(of: "\n", with: " ") ?? (state == .needsYou ? "Agent 停下来等你处理" : "Agent 做完了")
        return (title, String(body.prefix(200)))
    }
}

extension MacAttention {
    /// The banner for one agent. The userInfo keys are the hub push's (`machine_id`, `session_id`, `interaction_id`),
    /// so MacAppDelegate opens the chat or resolves a button the same way for both.
    static func content(_ s: Session, _ state: AgentState, interaction: Interaction?) -> UNMutableNotificationContent {
        let (title, body) = text(s, state)
        let c = UNMutableNotificationContent()
        c.title = title
        c.body = body
        c.threadIdentifier = s.machineID
        c.sound = state == .needsYou ? .default : nil
        var info: [String: String] = ["machine_id": s.machineID, "session_id": s.sessionID]
        if state == .needsYou, let interaction {
            c.categoryIdentifier = NotificationActions.category(interaction.kind)
            info["interaction_id"] = interaction.id
        }
        c.userInfo = info
        return c
    }
}

/// Keeps the Dock badge and the notifications in step with AppModel's session list, window open or not.
@MainActor final class MacAttentionWatcher {
    static let shared = MacAttentionWatcher()

    private var last: [Session] = []
    /// Nothing pings until a few seconds after launch: the cached list and the first refresh can disagree about
    /// things that changed while the app was closed, and those are old news.
    private var quietUntil = Date.distantFuture
    private var asked = false

    func start(_ model: AppModel) {
        quietUntil = Date().addingTimeInterval(5)
        observe(model)
    }

    private func observe(_ model: AppModel) {
        let sessions = withObservationTracking {
            _ = model.pendingInteractions.count
            _ = model.config?.url
            return model.allSessions
        } onChange: {
            // Called before the change lands; read the new values on the next turn of the main queue.
            DispatchQueue.main.async { MainActor.assumeIsolated { self.observe(model) } }
        }
        update(model, sessions: sessions)
    }

    private func update(_ model: AppModel, sessions: [Session]) {
        let muted: (Session) -> Bool = { model.isMuted($0) }
        NSApp.dockTile.badgeLabel = model.isPaired ? MacAttention.badge(sessions, muted: muted) : nil
        defer { last = sessions }
        // 体验模式's agents are made up.
        guard !model.isDemo, Date() >= quietUntil, !last.isEmpty else { return }
        let prefs = LiveActivityController.shared.prefs ?? NotifyPrefs()
        let ping = MacAttention.transitions(old: last, new: sessions, open: model.visibleSession, appActive: NSApp.isActive,
                                            prefs: prefs, now: Date(), muted: muted)
        guard !ping.isEmpty else { return }
        let center = UNUserNotificationCenter.current()
        if !asked {
            // Asked the first time there is something to say, not at launch.
            asked = true
            center.requestAuthorization(options: [.alert, .sound, .badge]) { @Sendable _, _ in }
        }
        for (s, state) in ping {
            let ix = model.pendingInteractions.first { $0.sessionKey == s.key }
            // One per session and state: a second 做完了 for the same agent replaces the first.
            let req = UNNotificationRequest(identifier: "\(s.machineID)/\(s.sessionID)#\(state.rawValue)",
                                            content: MacAttention.content(s, state, interaction: ix), trigger: nil)
            center.add(req) { @Sendable _ in }
        }
    }
}
