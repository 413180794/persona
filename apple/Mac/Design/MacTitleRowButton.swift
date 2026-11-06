import SwiftUI

/// A round button in a column's title row (the Agents list's personal Agent and New Session, #1223), drawn as Persona's list
/// draws its search button there: the symbol on the material's circle, its name as the tooltip.
struct MacTitleRowButton: View {
    let title: String
    let symbol: String
    let action: () -> Void

    init(_ title: String, symbol: String, action: @escaping () -> Void) {
        self.title = title
        self.symbol = symbol
        self.action = action
    }

    var body: some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(Theme.Font.heading)
                .foregroundStyle(Color.primary)
                .scaledFrame(width: Theme.Size.avatarL, height: Theme.Size.avatarL)
                .surface(interactive: true, in: .circle)
        }
        .buttonStyle(.plain)
        .help(title)
        .accessibilityLabel(title)
    }
}
