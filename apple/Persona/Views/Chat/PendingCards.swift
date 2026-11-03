import SwiftUI

/// The height the chat page has (the list and the composer together), for what sits above the composer to size
/// itself by. A class of its own so that only the views reading it are redrawn as it changes (the keyboard sliding).
@Observable final class FooterRoom {
    var height: CGFloat = 0
}

extension EnvironmentValues {
    /// The height a pending card's form measures its cap by: the length of the container the cards sit in (the page's
    /// window or column), as `containerRelativeFrame` gives it outside their scroll area. 0 until measured.
    @Entry var cardRoom: CGFloat = 0
}

/// The agent's pending cards (questions, permissions) above the composer. Where they fit they are laid out as they
/// always were; past half the page they scroll on their own under 「N 个待处理」, the permissions first and the newest
/// next, so the most urgent is in sight: the page's header, its back button and the conversation stay on screen
/// however many there are (#926).
struct PendingCards: View {
    struct Card: Identifiable {
        var interaction: Interaction
        /// Answerable from here (the session lets the phone approve); debug samples always are.
        var enabled: Bool
        var id: String { interaction.id }
    }

    var cards: [Card]
    var room: FooterRoom
    @State private var contentHeight: CGFloat = 0
    /// The cards' container's length (see `cardRoom`).
    @State private var containerLength: CGFloat = 0
    @State private var position = ScrollPosition(edge: .top)

    /// The share of the page the cards may take before they scroll.
    private static let share: CGFloat = 0.5

    var body: some View {
        let cap = room.height * Self.share
        let over = cap > 0 && contentHeight > cap + 1
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            if over {
                Text("\(cards.count) 个待处理")
                    .font(Theme.Font.sectionHeader).foregroundStyle(.secondary).lineLimit(1)
                    .padding(.horizontal, Theme.Space.xs)
                    .accessibilityIdentifier("pending-cards-count")
            }
            // One scroll view whether or not they fit: a card keeps what is typed into it in its own state.
            ScrollView {
                VStack(spacing: Theme.Space.m) {
                    ForEach(over ? Self.urgentFirst(cards) : cards) { card in
                        InteractionPanel(interaction: card.interaction).disabled(!card.enabled)
                    }
                }
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { contentHeight = $0 }
            }
            .scrollPosition($position)
            .accessibilityIdentifier("pending-cards")
            // Fitting, it is the stack it always was: not scrollable, nothing clipped (the cards' shadows).
            .scrollDisabled(!over)
            .scrollClipDisabled(!over)
            // Scrolling, room for the first card's shadow under the clipped top edge (it showed as a flat band).
            .contentMargins(.top, over ? Theme.Space.l : 0, for: .scrollContent)
            .contentMargins(.bottom, over ? Theme.Space.s : 0, for: .scrollContent)
            // As tall as the cards as measured, at most the cap; before the first measure, its own size.
            .frame(height: height(over: over, cap: cap))
            .fixedSize(horizontal: false, vertical: contentHeight == 0)
        }
        .environment(\.cardRoom, containerLength)
        // The container's length, read out here where the container is still the page's, not the scroll area.
        .background {
            Color.clear.containerRelativeFrame(.vertical)
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { containerLength = $0 }
                .hidden()
        }
        // A new card while they scroll: back to the top, where the most urgent is.
        .onChange(of: cards.map(\.id)) { old, new in
            if over, !Set(new).isSubset(of: Set(old)) { withAnimation(Theme.Motion.standard) { position.scrollTo(edge: .top) } }
        }
    }

    private func height(over: Bool, cap: CGFloat) -> CGFloat? {
        contentHeight == 0 ? nil : over ? cap : contentHeight
    }

    /// Permissions first (they hold a tool call back), then everything newest first.
    static func urgentFirst(_ cards: [Card]) -> [Card] {
        cards.enumerated().sorted { a, b in
            let (pa, pb) = (a.element.interaction.kind == .permission, b.element.interaction.kind == .permission)
            if pa != pb { return pa }
            let (ta, tb) = (a.element.interaction.createdAt ?? .distantPast, b.element.interaction.createdAt ?? .distantPast)
            return ta != tb ? ta > tb : a.offset > b.offset
        }.map(\.element)
    }
}
