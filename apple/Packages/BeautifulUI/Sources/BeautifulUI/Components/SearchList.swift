import SwiftUI

/// Command search with live filtering: a field with a clear button over the matching items,
/// or an empty state once a 3+ character query finds nothing.
public struct SearchList: View {
    var items: [String]
    var placeholder: String
    var emptyTitle: String
    var emptyHint: String
    var onPick: ((String) -> Void)?

    @State private var query: String
    @Environment(\.buiTheme) private var theme

    public init(
        items: [String],
        query: String = "",
        placeholder: String = "Search…",
        emptyTitle: String = "No results found",
        emptyHint: String = "Adjust your search to try again",
        onPick: ((String) -> Void)? = nil
    ) {
        self.items = items
        self.placeholder = placeholder
        self.emptyTitle = emptyTitle
        self.emptyHint = emptyHint
        self.onPick = onPick
        _query = State(initialValue: query)
    }

    /// Matches for `query`; with no query, the first five items.
    nonisolated static func results(_ items: [String], query: String) -> [String] {
        query.isEmpty ? Array(items.prefix(5)) : items.filter { $0.lowercased().contains(query.lowercased()) }
    }

    /// The empty state shows only once the query is long enough to mean something.
    nonisolated static func isEmptyState(_ items: [String], query: String) -> Bool {
        query.count > 2 && results(items, query: query).isEmpty
    }

    public var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                BUIIcon(.magnifier, size: 14).foregroundStyle(theme.colors.ink3)
                TextField(placeholder, text: $query)
                    .buiFont(BUITextStyle(size: 13, lineHeight: 1.19))
                    .foregroundStyle(theme.colors.ink)
                    .autocorrectionDisabled()
                if !query.isEmpty {
                    Button {
                        query = ""
                    } label: {
                        BUIIcon(.close, size: 11, lineWidth: 2.2)
                            .foregroundStyle(theme.colors.ink3)
                            .frame(width: 24, height: 24)
                            .contentShape(Circle())
                    }
                    .buttonStyle(PressHighlightStyle(radius: 12, color: theme.colors.line.opacity(0.7)))
                    .transition(.opacity.animation(.easeOut(duration: 0.15)))
                    .accessibilityLabel("Clear search")
                }
            }
            .padding(.horizontal, 12)
            .frame(height: 40)
            .overlay(alignment: .bottom) { Rectangle().fill(theme.colors.line).frame(height: 1) }

            if Self.isEmptyState(items, query: query) {
                VStack(spacing: 4) {
                    BUIIcon(.magnifier, size: 15, lineWidth: 1.8)
                        .foregroundStyle(theme.colors.ink3)
                        .frame(width: 32, height: 32)
                        .background(theme.colors.inset, in: .rect(cornerRadius: theme.radii.control))
                        .overlay(RoundedRectangle(cornerRadius: theme.radii.control).strokeBorder(theme.colors.line, lineWidth: 1))
                        .padding(.bottom, 6)
                    Text(emptyTitle)
                        .buiFont(BUITextStyle(size: 13, weight: .medium))
                        .foregroundStyle(theme.colors.ink)
                    Text(emptyHint)
                        .buiFont(.caption)
                        .foregroundStyle(theme.colors.ink3)
                }
                .frame(maxWidth: .infinity)
                .padding(.horizontal, 16)
                .padding(.vertical, 32)
                .buiEntrance(.fadeIn, duration: 0.25, curve: .easeOut)
            } else {
                VStack(spacing: 1) {
                    ForEach(Self.results(items, query: query), id: \.self) { item in
                        Button {
                            query = item
                            onPick?(item)
                        } label: {
                            Text(item)
                                .buiFont(BUITextStyle(size: 13, lineHeight: 1.19))
                                .foregroundStyle(theme.colors.ink)
                                .padding(.horizontal, 8)
                                .frame(maxWidth: .infinity, minHeight: 32, alignment: .leading)
                                .contentShape(.rect(cornerRadius: 6))
                        }
                        .buttonStyle(PressHighlightStyle(radius: 6, color: theme.colors.hover))
                        .buiEntrance(.fadeIn, duration: 0.2, curve: .easeOut)
                    }
                }
                .padding(4)
            }
        }
        .buiSurface(radius: theme.radii.card, elevation: .raised)
        .frame(maxWidth: 288)
        .frame(minHeight: 248, alignment: .top)
    }
}
