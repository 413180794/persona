import AppKit
import Foundation
import UserNotifications

/// What the Mac does outside its window when an agent needs you or finishes (docs/mac.md「和 iOS 的对应」):
/// the Dock badge (how many need you) and a system notification for each agent that just turned 需要你 / 做完了.
/// While the app runs it watches its own session list (the WebSocket stays open) and follows the same 设置 › 通知
/// choices (`NotifyPrefs`); the hub pushes these to the Mac only while it is not connected (docs/mac.md「远程推送（Mac）」).
enum MacAttention {
    /// The notifications to post for a session-list change: agents that turned needs-you or finished. Not for the
    /// chat on screen while the app is in front, a muted agent, a session the list just learned about, or a finish
    /// right after a needs-you (the same stop). Persona's own conversations ping when they ask, not when they reply.
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

    /// The Dock badge: agents and Persona conversations waiting on you, muted ones left out (the sidebar's badges).
    static func badge(_ sessions: [Session], muted: (Session) -> Bool) -> String? {
        let n = sessions.filter { $0.state == .needsYou && !muted($0) }.count
        return n > 0 ? "\(n)" : nil
    }

    static func text(_ s: Session, _ state: AgentState) -> (title: String, body: String) {
        let title = state == .needsYou ? String(localized: "\(s.displayTitle) 需要你") : String(localized: "\(s.displayTitle) 做完了")
        // No last text yet: name the speaker. The main conversation is Persona, not an agent.
        let actor = s.assistant ? "Persona" : "Agent"
        let body = s.lastText?.replacingOccurrences(of: "\n", with: " ")
            ?? (state == .needsYou ? String(localized: "\(actor) 停下来等你处理") : String(localized: "\(actor) 做完了"))
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
    /// When the current connected stretch began (MacAttention.connectedAt); nil until the first connect, and again
    /// while the socket is down, so a reconnect runs the settle instead of diffing the offline list at once.
    private var connectedAt: Date?
    private var asked = false

    func start(_ model: AppModel) {
        observe(model)
    }

    private func observe(_ model: AppModel) {
        let sessions = withObservationTracking {
            _ = model.pendingInteractions.count
            _ = model.config?.url
            _ = model.live.state
            // Muting changes the badge too: read what isMuted reads, and what isQuiet does (a task found quiet, #1731).
            _ = model.allSessions.map { model.isMuted($0) || model.isQuiet($0) }
            return model.allSessions
        } onChange: {
            // Called before the change lands; read the new values on the next turn of the main queue.
            DispatchQueue.main.async { MainActor.assumeIsolated { self.observe(model) } }
        }
        update(model, sessions: sessions)
    }

    private func update(_ model: AppModel, sessions: [Session]) {
        // A quiet task's agent (#1731) is the main conversation's to tell: no banner, no count, like a muted one.
        let muted: (Session) -> Bool = { model.isMuted($0) || model.isQuiet($0) }
        NSApp.dockTile.badgeLabel = model.isPaired ? MacAttention.badge(sessions, muted: muted) : nil
        defer { last = sessions }
        let now = Date()
        let startingToConnect = connectedAt == nil && model.live.state == .connected
        connectedAt = MacAttention.connectedAt(connectedAt, state: model.live.state, now: now)
        if startingToConnect {
            // Nothing else may change after the settle time: look again then.
            DispatchQueue.main.asyncAfter(deadline: .now() + MacAttention.settle + 0.1) { [weak self] in
                MainActor.assumeIsolated { self?.last = model.allSessions }
            }
        }
        // 体验模式's agents are made up.
        guard !model.isDemo, MacAttention.armed(connectedAt: connectedAt, now: Date()), !last.isEmpty else { return }
        let prefs = LiveActivityController.shared.prefs ?? NotifyPrefs()
        let ping = MacAttention.transitions(old: last, new: sessions, open: model.visibleSession, appActive: NSApp.isActive,
                                            prefs: prefs, now: Date(), muted: muted)
        guard !ping.isEmpty else { return }
        let requests = ping.map { s, state in
            // One per session and state: a second 做完了 for the same agent replaces the first.
            UNNotificationRequest(identifier: "\(s.machineID)/\(s.sessionID)#\(state.rawValue)",
                                  content: MacAttention.content(s, state, interaction: model.pendingInteractions.first { $0.sessionKey == s.key }),
                                  trigger: nil)
        }
        waiting += requests
        if asked {
            postWaiting()
        } else {
            // Asked the first time there is something to say, not at launch; posted once answered (added while
            // the question is open, they would be dropped).
            asked = true
            UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge]) { @Sendable granted, _ in
                DispatchQueue.main.async {
                    MainActor.assumeIsolated {
                        if granted { MacAttentionWatcher.shared.postWaiting() } else { MacAttentionWatcher.shared.waiting = [] }
                    }
                }
            }
        }
    }

    /// Notifications made before permission was answered, then posted.
    private var waiting: [UNNotificationRequest] = []

    private func postWaiting() {
        let center = UNUserNotificationCenter.current()
        for r in waiting { center.add(r) { @Sendable _ in } }
        waiting = []
    }
}

extension MacAttention {
    /// How long after the socket connects the list counts as the hub's: the refresh and the catch-up land then.
    static let settle: TimeInterval = 3

    /// Notifications start once the hub has answered: before, the list is the cache from the last run, and its
    /// differences from the first refresh are things that happened while the app was closed.
    static func armed(connectedAt: Date?, now: Date) -> Bool {
        guard let connectedAt else { return false }
        return now.timeIntervalSince(connectedAt) >= settle
    }

    /// When the current connected stretch began, from the watcher's stored value and the socket state. The first
    /// `.connected` arms at `now`; leaving `.connected` (a drop, a failed attempt, `.idle`) clears it, so when the
    /// socket reconnects `armed` and the settle run again instead of replaying the offline list's jumps as banners
    /// (which the hub may also have delivered as remote pushes). Staying `.connected` keeps the original value.
    static func connectedAt(_ current: Date?, state: LiveConnection.State, now: Date) -> Date? {
        switch state {
        case .connected:
            return current ?? now
        case .idle, .connecting, .failed:
            return nil
        }
    }
}
