import Foundation

/// A pairing link handed to the app from outside (Notes, AirDrop, Mail, Safari): `persona://link?…`,
/// the link a signed-in phone or the web makes for one computer (docs/onboarding.md 4.2).
/// Opening one only fills in the pairing window: the user sees which hub it is and pairs by tapping 「配对」 (Pair). A forwarded
/// or forged link must never hand this computer to someone else's hub by itself.
struct PairLink: Equatable {
    /// The link as it came, for `persona-link pair -`.
    var raw: String
    /// The machine id the hub made the link for; nil when the link names none.
    var machine: String?
    /// The hub's host (and port) from `url`, to show before pairing.
    var hubHost: String?
    /// The link carries the hub's TLS key fingerprint (`fp`): the hub is pinned, not checked by name.
    var pinned: Bool

    enum Parsed: Equatable {
        /// A link for Persona Link: fill in the window.
        case link(PairLink)
        /// One of ours, for another app (`persona://pair?…` for the phone and the Mac app, `open`, …): do nothing.
        case otherApp
        /// A link for Persona Link without what pairing needs (no token).
        case incomplete
        /// Not a Persona link at all.
        case notOurs
    }

    static let schemes: Set<String> = ["persona"]

    static func parse(_ url: URL) -> Parsed {
        parse(url.absoluteString)
    }

    static func parse(_ raw: String) -> Parsed {
        let s = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let c = URLComponents(string: s), schemes.contains(c.scheme?.lowercased() ?? "") else { return .notOurs }
        guard c.host?.lowercased() == "link" else { return .otherApp }
        let q = c.queryItems ?? []
        func value(_ name: String) -> String? {
            q.first { $0.name == name }?.value.flatMap { $0.isEmpty ? nil : $0 }
        }
        guard value("token") != nil else { return .incomplete }
        let host = value("url").map { LinkText.host($0) }.flatMap { $0.isEmpty ? nil : $0 }
        return .link(PairLink(raw: s, machine: value("machine"), hubHost: host, pinned: value("fp") != nil))
    }

    /// What the window says about a link that came from outside and did not fill it in.
    static func notice(for parsed: Parsed) -> String? {
        switch parsed {
        case .link, .notOurs: nil
        case .otherApp: "这是给手机 / Mac App 用的链接，不是给这台电脑配对用的。在手机或网页的「添加电脑」里拷贝这台电脑的配对链接。"
        case .incomplete: "这条配对链接不完整（没有 token）：在手机或网页的「添加电脑」里重新拷贝一次。"
        }
    }
}
