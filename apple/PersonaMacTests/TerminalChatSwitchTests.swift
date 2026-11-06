import Testing
@testable import Persona

/// The chat page's Terminal | Chat on the Mac (#3183): the terminal mounts the first time it shows and stays mounted while
/// the chat shows, so coming back to it does not connect again.
struct TerminalChatSwitchTests {
    @Test func theTerminalMountsWhenFirstShownAndStaysUnderTheChat() {
        var s = TerminalChatSwitch()
        #expect(!s.mounted)
        #expect(s.pick(.terminal, from: .chat, canTerminal: true) == .terminal)
        #expect(s.mounted)
        #expect(s.pick(.chat, from: .terminal, canTerminal: true) == .chat)
        #expect(s.mounted) // still subscribed under the chat
        #expect(s.pick(.terminal, from: .chat, canTerminal: true) == .terminal)
        #expect(s.mounted)
    }

    @Test func noTerminalNoSwitch() {
        var s = TerminalChatSwitch()
        #expect(s.pick(.terminal, from: .chat, canTerminal: false) == nil)
        #expect(s.toggle(from: .chat, canTerminal: false) == nil)
        #expect(!s.mounted)
    }

    @Test func pickingWhatShowsChangesNothing() {
        var s = TerminalChatSwitch()
        #expect(s.pick(.chat, from: .chat, canTerminal: true) == nil)
        #expect(s.pick(.git, from: .chat, canTerminal: true) == nil) // not one of the switch's two
        #expect(!s.mounted)
    }

    @Test func toggleFlipsAndGoesToTheTerminalFromFilesOrGit() {
        var s = TerminalChatSwitch()
        #expect(s.toggle(from: .chat, canTerminal: true) == .terminal)
        #expect(s.toggle(from: .terminal, canTerminal: true) == .chat)
        #expect(s.toggle(from: .git, canTerminal: true) == .terminal)
        #expect(s.toggle(from: .files, canTerminal: true) == .terminal)
    }

    @Test func otherWaysToTheTerminalMountItToo() {
        var s = TerminalChatSwitch()
        s.shown(.git)
        #expect(!s.mounted)
        s.shown(.terminal)
        #expect(s.mounted)
    }

    @Test func anotherAgentConnectsItsTerminalOnlyOnceShown() {
        var s = TerminalChatSwitch()
        _ = s.pick(.terminal, from: .chat, canTerminal: true)
        _ = s.pick(.chat, from: .terminal, canTerminal: true)
        // On the chat: the next agent's terminal is not connected for nothing.
        #expect(s.switchedAgent(pane: .chat, canTerminal: true) == .chat)
        #expect(!s.mounted)
        // On the terminal: the next agent's terminal shows (the panel follows the key).
        _ = s.pick(.terminal, from: .chat, canTerminal: true)
        #expect(s.switchedAgent(pane: .terminal, canTerminal: true) == .terminal)
        #expect(s.mounted)
        // One with no terminal shows its chat.
        #expect(s.switchedAgent(pane: .terminal, canTerminal: false) == .chat)
        #expect(!s.mounted)
    }
}
