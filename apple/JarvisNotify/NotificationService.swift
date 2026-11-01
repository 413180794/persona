import os
import UserNotifications

/// Opens pushes that came through the relay (docs/push-relay.md): the relay only saw 「有新消息」 and a sealed payload; here
/// the phone decrypts it and shows the real title, text and buttons. Not opened in time, no key yet (the phone has not
/// been unlocked since it started), or a payload that does not open: the fallback stays, never anything in the clear.
final class NotificationService: UNNotificationServiceExtension {
    private static let log = Logger(subsystem: "jarvis", category: "notify")
    private var contentHandler: ((UNNotificationContent) -> Void)?
    private var fallback: UNNotificationContent?

    override func didReceive(_ request: UNNotificationRequest, withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void) {
        self.contentHandler = contentHandler
        fallback = request.content
        let (content, outcome) = PushCrypto.service(request.content)
        // Why a push stayed 「有新消息」 is the first question when one does (no content in the log).
        switch outcome {
        case .notSealed: break
        case .noKey: Self.log.error("sealed push: no key (not unlocked since boot, or none made yet)")
        case .unopened: Self.log.error("sealed push: did not open, or too old")
        case .opened: Self.log.notice("sealed push opened")
        }
        finish(content)
    }

    override func serviceExtensionTimeWillExpire() {
        if let fallback { finish(fallback) }
    }

    private func finish(_ content: UNNotificationContent) {
        guard let handler = contentHandler else { return }
        contentHandler = nil
        handler(content)
    }
}
