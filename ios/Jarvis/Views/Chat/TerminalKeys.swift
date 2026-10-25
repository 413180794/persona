import SwiftUI

/// Terminal keys in one row that never wraps: a sticky ctrl, esc, tab, and a game-pad button that floats a
/// direction pad (↑ ↓ ← → with enter in the middle) above the bar.
struct TerminalKeyBar: View {
    var send: ([String]) -> Void
    @State private var ctrl = false
    #if DEBUG
    @State private var pad = UserDefaults.standard.bool(forKey: "showPad")
    #else
    @State private var pad = false
    #endif

    var body: some View {
        HStack(spacing: 8) {
            key("ctrl", active: ctrl) { ctrl.toggle() }
            key("esc") { press("esc") }
            key("tab") { press("tab") }
            Spacer(minLength: 0)
            Button {
                Haptics.tap()
                withAnimation(.spring(duration: 0.3, bounce: 0.3)) { pad.toggle() }
            } label: {
                Image(systemName: pad ? "gamecontroller.fill" : "gamecontroller")
                    .font(.title3)
                    .frame(width: 48, height: 40)
            }
            .themedButton()
            .tint(pad ? Theme.accent : nil)
            .accessibilityLabel("方向键")
        }
        .overlay(alignment: .bottomTrailing) {
            if pad {
                DirectionPad { press($0) }
                    .offset(y: -52)
                    .transition(.scale(scale: 0.6, anchor: .bottomTrailing).combined(with: .opacity))
            }
        }
    }

    private func key(_ label: String, active: Bool = false, action: @escaping () -> Void) -> some View {
        Button {
            Haptics.tap()
            action()
        } label: {
            Text(label)
                .font(.system(.subheadline, design: .monospaced).weight(.semibold))
                .lineLimit(1)
                .fixedSize()
                .frame(minWidth: 44, minHeight: 40)
        }
        .themedButton()
        .tint(active ? Theme.accent : nil)
        .overlay(Capsule().stroke(active ? Theme.accent : .clear, lineWidth: 1.5))
        .accessibilityAddTraits(active ? .isSelected : [])
    }

    /// ctrl applies to the next key and then releases.
    private func press(_ key: String) {
        send([ctrl && key != "esc" ? "ctrl+\(key)" : key])
        ctrl = false
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
