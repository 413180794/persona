import SwiftUI

/// An Agents chat hosted by a page that is about something else: a task's sheet opened from the main conversation
/// (`TaskDetailSheet`, #3255). The page draws no header of its own; the chat's header is the only one and takes the
/// page's part: its leading button leaves the page (✕ at a sheet's root, ‹ when pushed into the detail column), its
/// title is the page's (the task's), and its ⋯ starts with the page's own actions. It is one agent's page: no strip of
/// the space's other agents. When the page stands for several agents (a task with more than one), the title's ⌄
/// switches between them; the space's others are a step away in the Agents page (「在 Agents 里打开」).
struct HostedChat {
    /// The page's title in place of the session's (a task's). Nil or empty keeps the session's.
    var title: String?
    /// A line about the page at the top of ⋯ (an agent no task stands for says so).
    var note: String?
    /// The page is a sheet's root: its leading button closes (✕). Otherwise it is pushed and goes back (‹).
    var closes: Bool
    /// Leaves the page (closes the sheet, or goes back).
    var leave: () -> Void
    /// The agents the title's ⌄ switches between, the chat's own among them; with fewer than two there is no ⌄.
    var agents: [Session] = []
    var select: (SessionKey) -> Void = { _ in }
    /// The page's own items at the top of ⋯ (a task's Remove).
    var menu: AnyView?
    /// Shows the agent in the Agents page, where its space's other agents are.
    var openInAgents: (SessionKey) -> Void
    /// Full Screen (#3743): the sheet goes down and the agent's page is pushed onto the assistant page. Nil: no button
    /// (pushed into the detail column it already fills it).
    var expand: ((SessionKey) -> Void)?

    /// The leading button's symbol and its name.
    var leaveSymbol: String { closes ? Theme.Icon.close : Theme.Icon.back }
    var leaveLabel: String { closes ? String(localized: "Close") : String(localized: "Back [Task Sheets 572cf4]") }
}

extension EnvironmentValues {
    /// Set by a page that hosts an Agents chat (`HostedChat`); nil for the Agents page's own chats.
    @Entry var hostedChat: HostedChat? = nil
}
