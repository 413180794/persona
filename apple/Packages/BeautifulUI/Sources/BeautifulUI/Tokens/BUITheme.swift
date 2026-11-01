import SwiftUI

/// Everything a component reads to draw itself. Inject a custom theme with
/// `.buiTheme(_:)` to map the library onto a host app's own tokens.
public struct BUITheme: Sendable {
    public var colors: BUIColors
    public var radii: BUIRadii
    /// Multiplies every `buiFont` size. The original's sizes are desktop sizes (13 pt labels);
    /// a phone host whose body text is 17 pt sets about 1.15–1.2.
    public var typeScale: CGFloat
    /// One-shot entrances (`buiEntrance`). A host whose lists rebuild rows as they scroll in
    /// turns them off, or every row of an opened history would fade up at once.
    public var entrances: Bool
    /// What VoiceOver says for the components' states, in the host's language.
    public var strings: BUIStrings

    public init(
        colors: BUIColors = .standard,
        radii: BUIRadii = .standard,
        typeScale: CGFloat = 1,
        entrances: Bool = true,
        strings: BUIStrings = .standard
    ) {
        self.colors = colors
        self.radii = radii
        self.typeScale = typeScale
        self.entrances = entrances
        self.strings = strings
    }

    public static let standard = BUITheme()
}

/// Words the components show or speak, in the host's language. English by default.
public struct BUIStrings: Sendable, Equatable {
    public var expanded: String
    public var collapsed: String
    /// The prompt bar's: field and button labels, and its menus' footers.
    public var prompt = "Prompt"
    public var listening = "Listening…"
    public var attach = "Add attachments and sources"
    public var mention = "Mention"
    public var commands = "Commands"
    public var chooseModel = "Choose model"
    public var startDictation = "Start dictation"
    public var stopDictation = "Stop dictation"
    public var send = "Send"
    public var stop = "Stop"
    public var connect = "Connect"
    public var connected = "Connected"
    public var sourcesHint = "Type to search sources & files"
    public var commandsHint = "Type to search commands"
    /// `%@` is the query.
    public var noMatches = "No matches for “%@”"
    /// `%@` is the file.
    public var remove = "Remove %@"
    /// The approval card's.
    public var openApproval = "Open approval"
    public var startOver = "Start over"
    public var dismiss = "Dismiss"
    public var customAnswer = "Custom answer"
    public var previousQuestion = "Previous question"
    public var nextQuestion = "Next question"

    public init(expanded: String = "Expanded", collapsed: String = "Collapsed") {
        self.expanded = expanded
        self.collapsed = collapsed
    }

    public static let standard = BUIStrings()

    public func expanded(_ isOpen: Bool) -> String { isOpen ? expanded : collapsed }
    func noMatches(_ query: String) -> String { noMatches.replacingOccurrences(of: "%@", with: query) }
    func remove(_ file: String) -> String { remove.replacingOccurrences(of: "%@", with: file) }
}

public extension EnvironmentValues {
    @Entry var buiTheme: BUITheme = .standard
}

public extension View {
    func buiTheme(_ theme: BUITheme) -> some View {
        environment(\.buiTheme, theme)
    }
}
