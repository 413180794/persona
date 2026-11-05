import SwiftUI

// The voice conversation's tokens (docs/voice.md「设计系统」): how the orb in the voice bar moves and how large it is.
// A file of their own, away from Theme.swift (the CarPlay voice PR edits it too). Shared/, so the watch compiles them;
// it draws no voice UI.

nonisolated extension Theme.Motion {
    /// One way of the orb's breath while it listens: it swells for this long and settles for as long, eased at both
    /// ends. The same as Android's `Voice.kt` (`ListeningBreath`, 1600 ms each way).
    static let voiceBreath: TimeInterval = 1.6
    /// One way of the orb's beat while Persona speaks: quick enough to read as a voice. Android's `SpeakingBeat`
    /// (420 ms each way).
    static let voiceBeat: TimeInterval = 0.42
    /// One lap of the ring around the orb while the call connects: a spinner's pace.
    static let voiceSpin: TimeInterval = 1.1
    /// One lap of the ring around the orb while the agent works: slow, a sign that something goes on, not a wait.
    static let voiceLap: TimeInterval = 2.8
    /// How often a moving orb is drawn: 30 a second, as `StatusDot` and the avatar's halo; a 1.6 s breath and a level
    /// that changes about 20 times a second gain nothing from more, and a call can run for many minutes with the screen
    /// on. A clock drives it, not a repeating animation (those leaked into the layout of rows around them, `StatusDot`).
    static let voiceFrame: TimeInterval = 1.0 / 30
}

nonisolated extension Theme.Size {
    /// The voice orb's box at the default text size (`scaledFrame` grows it with the text): the sphere and the room it
    /// swells and glows into, more than a finger's target. The voice bar's middle; a larger orb passes its own size.
    static let voiceOrb: CGFloat = 52
    /// The orb's level meter (the BeautifulUI style's five bars) at the default orb: a bar's width, the gap between
    /// two, and the tallest bar. A larger orb scales them with it.
    static let voiceLevelBar: CGFloat = 3
    static let voiceLevelGap: CGFloat = 2
    static let voiceLevelHeight: CGFloat = 16
}
