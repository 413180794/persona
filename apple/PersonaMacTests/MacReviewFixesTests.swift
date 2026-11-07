import Foundation
import Testing
@testable import Persona

/// The first review round's findings on the Mac shell (docs/apple/mac.md).
@MainActor
struct MacReviewFixesTests {
    /// Opening a chat from ⌘K, the menu bar or a notification leaves Knowledge / Machines even when the tab and the open
    /// chat are already the ones asked for (no onChange fires then).
    @Test func openingAChatLeavesTheMacSections() {
        let shell = MacShellState()
        shell.section = .machines
        shell.willOpen()
        #expect(shell.section == nil)
    }

    /// A todo's Agent row opens its chat from the todos section, which must not stay on screen behind it.
    @Test func openingATodoAgentLeavesTheTodosSection() {
        let shell = MacShellState()
        shell.section = .todos
        shell.willOpen()
        #expect(shell.section == nil)
    }

    /// The hand-off sheet opens an existing Agent through the same helper as the todo's Agent rows, so it leaves the
    /// todos section too.
    @Test func openingAnAgentFromTheHandOffLeavesTheTodosSection() {
        MacShellState.shared.section = .todos
        openTodoAgentChat(SessionKey(machineID: "m", sessionID: "s"), in: AppModel())
        #expect(MacShellState.shared.section == nil)
    }

    /// A `persona://pair` link is not one a click in a message may follow: it would move the app to another hub.
    @Test func pairingLinksAreNotFollowedFromMessages() {
        #expect(!LinkPolicy.opensOnMac(URL(string: "persona://pair?url=https://elsewhere.example&token=x")!))
    }

    /// A pairing link while paired to a real hub does not switch hubs silently.
    @Test func aPairingLinkDoesNotReplaceAPairing() {
        #expect(MacPairing.accepts(isPaired: false, isDemo: false))
        #expect(MacPairing.accepts(isPaired: true, isDemo: true)) // Demo Mode ends when a real hub comes
        #expect(!MacPairing.accepts(isPaired: true, isDemo: false))
    }

    /// Notifications wait until the hub has answered once: before that the list is the cache from the last run.
    @Test func quietUntilConnectedAndCaughtUp() {
        let t0 = Date(timeIntervalSince1970: 1000)
        #expect(!MacAttention.armed(connectedAt: nil, now: t0))
        #expect(!MacAttention.armed(connectedAt: t0, now: t0.addingTimeInterval(1)))
        #expect(MacAttention.armed(connectedAt: t0, now: t0.addingTimeInterval(MacAttention.settle + 0.1)))
    }

    @Test func resumeCommandQuotesTheFolder() {
        #expect(MacAgentCommands.resumeLine(cwd: "/w/My Projects", command: "claude --resume", id: "abc")
                == "cd '/w/My Projects' && claude --resume abc")
        #expect(MacAgentCommands.resumeLine(cwd: "/w/it's; rm -rf ~", command: "claude --resume", id: "abc")
                == #"cd '/w/it'\''s; rm -rf ~' && claude --resume abc"#)
    }
}
