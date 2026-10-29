import SwiftUI

/// One modifier on the key bar, the way Moshi does it: a tap arms it for the next key, a quick second tap locks it
/// until tapped again, and holding it keeps it down while the finger stays (let go without pressing anything and it
/// stays armed for the next key).
enum ModifierState: Equatable { case off, once, held, locked }

/// ctrl, alt and shift on the key bar, shared by its keys and the text field under it.
struct TerminalModifiers: Equatable {
    enum Key: CaseIterable { case ctrl, alt, shift }
    var ctrl = ModifierState.off
    var alt = ModifierState.off
    var shift = ModifierState.off
    /// Whether a key went out while one was held (letting go then releases it instead of arming it).
    private var usedWhileHeld = false

    subscript(k: Key) -> ModifierState {
        get { switch k { case .ctrl: ctrl; case .alt: alt; case .shift: shift } }
        set { switch k { case .ctrl: ctrl = newValue; case .alt: alt = newValue; case .shift: shift = newValue } }
    }

    var active: Bool { Key.allCases.contains { self[$0] != .off } }

    /// The key name herdr takes with the modifiers that are down: "ctrl+alt+x", "shift+tab". esc goes out bare.
    func apply(_ key: String) -> String {
        guard key != "esc" else { return key }
        let names: [(Key, String)] = [(.ctrl, "ctrl"), (.alt, "alt"), (.shift, "shift")]
        return (names.filter { self[$0.0] != .off }.map(\.1) + [key]).joined(separator: "+")
    }

    /// A key went out: armed-once modifiers release, held and locked ones stay.
    mutating func pressed() {
        for k in Key.allCases {
            if self[k] == .once { self[k] = .off }
            if self[k] == .held { usedWhileHeld = true }
        }
    }

    /// A tap; `quick` when it follows the previous tap on the same key closely (a double tap).
    mutating func tap(_ k: Key, quick: Bool) {
        switch self[k] {
        case .off: self[k] = .once
        case .once: self[k] = quick ? .locked : .off
        case .held, .locked: self[k] = .off
        }
    }

    /// Held long enough to count as holding it down.
    mutating func hold(_ k: Key) {
        self[k] = .held
        usedWhileHeld = false
    }

    /// Let go after holding.
    mutating func release(_ k: Key) {
        guard self[k] == .held else { return }
        self[k] = usedWhileHeld ? .off : .once
    }

    /// What one character typed in the text field sends while modifiers are down: a key for herdr ("ctrl+c"), or — shift
    /// alone, or a character no key name covers — the text itself (shift capitalized).
    func typed(_ ch: Character) -> (key: String?, text: String?) {
        let ctrlOrAlt = ctrl != .off || alt != .off
        if !ctrlOrAlt { return (nil, shift != .off ? ch.uppercased() : String(ch)) }
        guard ch.isASCII, !ch.isNewline else { return (nil, String(ch)) }
        let name = ch == " " ? "space" : ch == "\t" ? "tab" : ch.lowercased()
        return (apply(name), nil)
    }
}

/// Terminal keys in one row that never wraps: ctrl / alt / shift (tap, double tap, hold), esc, tab, a ⌃C that goes
/// out straight away, and a game-pad button that floats a direction pad (↑ ↓ ← → with enter in the middle) above the bar.
struct TerminalKeyBar: View {
    /// Shared with a text field that should honor them; the bar keeps its own when nil.
    var mods: Binding<TerminalModifiers>? = nil
    /// The optional buttons shown (TerminalPrefs.allKeys ids); ctrl always is.
    var visible: Set<String> = Set(TerminalPrefs.allKeys.map(\.id))
    var send: ([String]) -> Void
    @State private var own = TerminalModifiers()
    #if DEBUG
    @State private var preset = false
    #endif
    @State private var downAt: [TerminalModifiers.Key: Date] = [:]
    @State private var lastTap: [TerminalModifiers.Key: Date] = [:]
    #if DEBUG
    @State private var pad = UserDefaults.standard.bool(forKey: "showPad")
    #else
    @State private var pad = false
    #endif

    private var m: Binding<TerminalModifiers> { mods ?? $own }

    var body: some View {
        HStack(spacing: 6) {
            modifier(.ctrl, "ctrl", name: "Control")
            if visible.contains("alt") { modifier(.alt, "alt", name: "Option") }
            if visible.contains("shift") { modifier(.shift, "⇧", name: "Shift") }
            if visible.contains("esc") { key("esc") { press("esc") } }
            if visible.contains("tab") { key("tab") { press("tab") } }
            if visible.contains("ctrlc") { key("⌃C", name: "发送 Control C") { send(["ctrl+c"]) } }
            Spacer(minLength: 0)
            if visible.contains("pad") { padButton }
        }
        #if DEBUG
        // `-showKeyMods locked,once,held`: ctrl, alt, shift in those states, for screenshots.
        .onAppear {
            guard !preset, let v = UserDefaults.standard.string(forKey: "showKeyMods") else { return }
            preset = true
            let states: [String: ModifierState] = ["off": .off, "once": .once, "held": .held, "locked": .locked]
            for (k, name) in zip(TerminalModifiers.Key.allCases, v.split(separator: ",")) {
                m.wrappedValue[k] = states[String(name)] ?? .off
            }
        }
        #endif
        .overlay(alignment: .bottomTrailing) {
            if pad {
                DirectionPad { press($0) }
                    .offset(y: -52)
                    .transition(.scale(scale: 0.6, anchor: .bottomTrailing).combined(with: .opacity))
            }
        }
    }

    private var padButton: some View {
        Button {
            Haptics.tap()
            withAnimation(.spring(duration: 0.3, bounce: 0.3)) { pad.toggle() }
        } label: {
            KeyCap(symbol: pad ? "gamecontroller.fill" : "gamecontroller", state: pad ? .once : .off)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("方向键")
        .accessibilityAddTraits(pad ? .isSelected : [])
    }

    private func key(_ label: String, name: String? = nil, action: @escaping () -> Void) -> some View {
        Button {
            Haptics.tap()
            action()
        } label: {
            KeyCap(label: label, state: .off)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(name ?? label)
    }

    /// Down and up are tracked by hand: a tap, a quick second tap and a hold all mean something different. Touches
    /// come from UIKit: a SwiftUI drag gesture misses the second touch of a quick double tap.
    private func modifier(_ k: TerminalModifiers.Key, _ label: String, name: String) -> some View {
        let state = m.wrappedValue[k]
        return KeyCap(label: label, state: state)
            .overlay(TouchArea(down: { down(k) }, up: { up(k) }))
            .accessibilityElement()
            .accessibilityLabel(name)
            .accessibilityValue(state == .locked ? "已锁定" : state == .off ? "" : "已按下")
            .accessibilityAddTraits(.isButton)
            .accessibilityAddTraits(state == .off ? [] : .isSelected)
            .accessibilityAction { m.wrappedValue.tap(k, quick: false) }
    }

    private func down(_ k: TerminalModifiers.Key) {
        guard downAt[k] == nil else { return }
        let at = Date()
        downAt[k] = at
        Task {
            try? await Task.sleep(for: .milliseconds(Self.holdAfter))
            guard downAt[k] == at else { return }
            Haptics.tap()
            m.wrappedValue.hold(k)
        }
    }

    private func up(_ k: TerminalModifiers.Key) {
        downAt[k] = nil
        if m.wrappedValue[k] == .held {
            m.wrappedValue.release(k)
            return
        }
        let now = Date()
        let quick = lastTap[k].map { now.timeIntervalSince($0) < Self.doubleTap } ?? false
        lastTap[k] = now
        Haptics.tap()
        m.wrappedValue.tap(k, quick: quick)
    }

    static let holdAfter = 350 // ms
    static let doubleTap: TimeInterval = 0.35

    /// Modifiers apply to the next key, then the armed-once ones let go.
    private func press(_ key: String) {
        send([m.wrappedValue.apply(key)])
        m.wrappedValue.pressed()
    }
}

/// Reports every touch down and up on it (each finger on its own key), nothing else.
private struct TouchArea: UIViewRepresentable {
    var down: () -> Void
    var up: () -> Void

    func makeUIView(context: Context) -> TouchView { TouchView() }
    func updateUIView(_ v: TouchView, context: Context) { v.down = down; v.up = up }

    final class TouchView: UIView {
        var down: (() -> Void)?
        var up: (() -> Void)?
        override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent?) { down?() }
        override func touchesEnded(_ touches: Set<UITouch>, with event: UIEvent?) { up?() }
        override func touchesCancelled(_ touches: Set<UITouch>, with event: UIEvent?) { up?() }
    }
}

/// A key of the bar: armed / held keys are tinted and outlined, a locked one is filled with a bar under its label.
struct KeyCap: View {
    var label = ""
    var symbol: String? = nil
    var state: ModifierState

    var body: some View {
        Group {
            if let symbol {
                Image(systemName: symbol).font(.system(size: 17, weight: .medium)).padding(.horizontal, 4)
            } else {
                Text(label)
                    .font(.system(.subheadline, design: .monospaced).weight(.semibold))
                    .lineLimit(1)
                    .fixedSize()
            }
        }
        .foregroundStyle(state == .locked ? Color.white : state == .off ? Color.primary : Theme.accent)
        .padding(.horizontal, 10)
        .frame(minWidth: 40, minHeight: 40)
        .surface(tint: state == .locked ? Theme.accent : state == .off ? nil : Theme.accent.opacity(0.16),
                 interactive: true, in: .capsule)
        .overlay(Capsule().stroke(state == .once || state == .held ? Theme.accent : .clear, lineWidth: 1.5))
        .overlay(alignment: .bottom) {
            if state == .locked { Capsule().fill(.white).frame(width: 12, height: 2).padding(.bottom, 6) }
        }
        .scaleEffect(state == .held ? 0.94 : 1)
        .animation(.snappy(duration: 0.15), value: state)
        .contentShape(.capsule)
    }
}

/// Five keys laid out like a game pad.
struct DirectionPad: View {
    var press: (String) -> Void
    private let size: CGFloat = 52

    var body: some View {
        Grid(horizontalSpacing: 6, verticalSpacing: 6) {
            GridRow {
                Color.clear.frame(width: size, height: size)
                pad("arrow.up", "up")
                Color.clear.frame(width: size, height: size)
            }
            GridRow {
                pad("arrow.left", "left")
                pad("return", "enter", prominent: true)
                pad("arrow.right", "right")
            }
            GridRow {
                Color.clear.frame(width: size, height: size)
                pad("arrow.down", "down")
                Color.clear.frame(width: size, height: size)
            }
        }
        .padding(10)
        .surface(in: .rect(cornerRadius: 28, style: .continuous))
        .shadow(color: .black.opacity(0.18), radius: 18, y: 6)
    }

    @ViewBuilder private func pad(_ symbol: String, _ key: String, prominent: Bool = false) -> some View {
        let label = Image(systemName: symbol).font(.title3.weight(.semibold)).frame(width: size, height: size)
        if prominent {
            Button { Haptics.tap(); press(key) } label: { label }
                .themedButton(prominent: true).clipShape(.circle).accessibilityLabel(key)
        } else {
            Button { Haptics.tap(); press(key) } label: { label }
                .themedButton().clipShape(.circle).accessibilityLabel(key)
        }
    }
}
