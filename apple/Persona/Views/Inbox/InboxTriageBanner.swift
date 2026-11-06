import SwiftUI

// New items wait for triage (#1773): since #1644 a plugin connector's new items enter the inbox only once the model
// has sorted them, and the hub says in every list how that stands (`InboxStore.triage`). While some wait and triage is
// not ok, the inbox says so at the top of its list, the same words on every app; Set Up opens the model for inbox triage
// when none is set up. An older hub says nothing, and nothing shows.

/// The banner's words and button, from the hub's triage status.
nonisolated enum InboxTriageText {
    /// What each state means when the hub gives no reason of its own.
    static let reasons: [String: String] = [
        "unconfigured": String(localized: "No triage model set up"),
        "paused": String(localized: "The triage model is paused"),
        "error": String(localized: "The triage model hit an error"),
        "timeout": String(localized: "The triage model is too slow"),
        "behind": String(localized: "Lots of new messages, waiting in line"),
    ]

    /// The model purpose Set Up opens: inbox triage.
    static let purpose = "inbox.triage"

    /// The banner's line, nil when there is none: triage ok, nothing waiting, or an older hub (no status).
    /// One source: 「有 3 条飞书消息在等分拣：还没配分拣模型」; several: 「有 5 条消息在等分拣：…」.
    static func line(_ t: InboxTriageStatus?) -> String? {
        guard let t, t.state != "ok", t.waiting > 0 else { return nil }
        let own = t.reason?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let why = own.isEmpty ? reasons[t.state] : own
        let sources = (t.sources ?? []).filter { $0.waiting > 0 }
        let name = sources.count == 1 ? sources[0].name ?? "" : ""
        let head = name.isEmpty ? String(localized: "\(t.waiting) messages waiting for triage") : String(localized: "\(t.waiting) \(name) messages waiting for triage")
        return why.map { String(localized: "\(head): \($0)") } ?? head
    }

    /// Set Up is offered: no model is set up for triage, and this device may change settings.
    static func configurable(_ t: InboxTriageStatus?, mayChangeSettings: Bool) -> Bool {
        mayChangeSettings && t?.state == "unconfigured"
    }
}

/// The inbox's banner about new items waiting for triage, in the caution tone with a clock: not dismissable, gone once
/// triage is ok. With Set Up (the inbox triage model's settings) when no model is set up and this device may change
/// settings: under the words on the phone, on the right on the Mac.
struct InboxTriageBanner: View {
    var line: String
    var configurable: Bool
    @State private var configuring = false

    var body: some View {
        NoticeBanner(text: line, symbol: Theme.Icon.time) { // a clock: they wait, as on the web and Android
            if configurable {
                Button("Set Up") { configuring = true }
                    .accessibilityIdentifier("inbox.triage.configure")
            }
        }
        .accessibilityIdentifier("inbox.triage")
        .modelConfigurationSheet(isPresented: $configuring, purposeID: InboxTriageText.purpose)
    }
}
