import SwiftUI

/// A primary destination in the sidebar.
public struct SidebarItem: Identifiable, Sendable {
    public var id: String
    public var label: String
    /// An SF Symbol name.
    public var symbol: String
    /// A trailing count, e.g. "3/10".
    public var count: String?

    public init(id: String, label: String, symbol: String, count: String? = nil) {
        self.id = id
        self.label = label
        self.symbol = symbol
        self.count = count
    }
}

/// A past chat in the sidebar's history.
public struct SidebarRecent: Identifiable, Sendable {
    public var id: String
    public var label: String

    public init(id: String, label: String) {
        self.id = id
        self.label = label
    }
}

/// Workspace navigation: a workspace switcher, primary items, searchable chat history and a
/// footer action, collapsing to a 52 pt icon rail. The original's gliding hover highlight
/// becomes a press highlight; the current item keeps its fill.
public struct SidebarNav: View {
    var workspace: String
    var items: [SidebarItem]
    var recents: [SidebarRecent]
    var footerLabel: String
    var onNewChat: (() -> Void)?
    var onSelect: ((String) -> Void)?
    var onFooter: (() -> Void)?

    @State private var collapsed: Bool
    @State private var current: String
    @State private var workspaceOpen: Bool
    @State private var searchOpen: Bool
    @State private var query = ""
    @FocusState private var searchFocused: Bool
    @Environment(\.buiTheme) private var theme

    static let expandedWidth: CGFloat = 224
    static let collapsedWidth: CGFloat = 52
    /// `cubic-bezier(.16, 1, .3, 1)` over 280 ms; copy fades over 180 ms.
    static let motion = BUICurve.link.animation(duration: 0.28)
    static let copyMotion = BUICurve.link.animation(duration: 0.18)

    public init(
        workspace: String,
        items: [SidebarItem],
        recents: [SidebarRecent],
        selection: String? = nil,
        footerLabel: String = "Upgrade",
        collapsed: Bool = false,
        showsWorkspaceMenu: Bool = false,
        showsSearch: Bool = false,
        onNewChat: (() -> Void)? = nil,
        onSelect: ((String) -> Void)? = nil,
        onFooter: (() -> Void)? = nil
    ) {
        self.workspace = workspace
        self.items = items
        self.recents = recents
        self.footerLabel = footerLabel
        self.onNewChat = onNewChat
        self.onSelect = onSelect
        self.onFooter = onFooter
        _collapsed = State(initialValue: collapsed)
        _current = State(initialValue: selection ?? "")
        _workspaceOpen = State(initialValue: showsWorkspaceMenu)
        _searchOpen = State(initialValue: showsSearch)
    }

    /// Recents whose label contains `query` (case-insensitive, trimmed).
    nonisolated static func filter(_ recents: [SidebarRecent], query: String) -> [SidebarRecent] {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        return q.isEmpty ? recents : recents.filter { $0.label.lowercased().contains(q) }
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
                .frame(height: 40)
                .padding(.bottom, 10)
            VStack(spacing: 1) {
                row(symbol: "square.and.pencil", label: "New chat", active: false) {
                    current = ""
                    onNewChat?()
                }
                ForEach(items) { item in
                    row(symbol: item.symbol, label: item.label, count: item.count, active: current == item.id) {
                        select(item.id)
                    }
                }
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 1) {
                    chatsHeader
                        .padding(.horizontal, 8)
                        .padding(.bottom, 4)
                        .copy(hidden: collapsed)
                    let visible = Self.filter(recents, query: query)
                    ForEach(visible) { recent in
                        row(symbol: nil, label: recent.label, active: current == recent.id) {
                            select(recent.id)
                        }
                        .copy(hidden: collapsed)
                    }
                    if !query.isEmpty && visible.isEmpty {
                        Text("No chats found")
                            .buiFont(BUITextStyle(size: 12.5))
                            .foregroundStyle(theme.colors.ink3)
                            .padding(.horizontal, 16)
                            .padding(.vertical, 8)
                    }
                }
            }
            .scrollIndicators(.hidden)
            .padding(.top, 12)
            .frame(maxHeight: .infinity)
            Button(footerLabel) { onFooter?() }
                .buttonStyle(FooterButtonStyle())
                .padding(.top, 12)
                .overlay(alignment: .top) { Rectangle().fill(theme.colors.line).frame(height: 1) }
                .frame(width: 208)
                .padding(.horizontal, 8)
                .copy(hidden: collapsed)
        }
        .frame(width: Self.expandedWidth, alignment: .leading)
        .frame(width: collapsed ? Self.collapsedWidth : Self.expandedWidth, alignment: .leading)
        .clipped()
        .overlay(alignment: .topLeading) {
            if workspaceOpen {
                WorkspaceMenu(workspace: workspace) {
                    withAnimation(BUIMotion.popIn) { workspaceOpen = false }
                }
                .offset(x: 8, y: 42)
                .transition(.scale(scale: 0.95, anchor: .topLeading).combined(with: .opacity))
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Workspace navigation")
    }

    private var header: some View {
        ZStack(alignment: .topLeading) {
            Button {
                withAnimation(BUICurve.outStrong.animation(duration: 0.18)) { workspaceOpen.toggle() }
            } label: {
                HStack(spacing: 6) {
                    BUIIcon(BUIGlyph(BUIGlyph.rect(7, 2, 10, 14, rx: 5) + "M12 16v6M10 7v4"), size: 18, lineWidth: 2)
                        .foregroundStyle(theme.colors.ink)
                        .frame(width: 20, height: 20)
                    Text(workspace)
                        .buiFont(BUITextStyle(size: 14, weight: .medium, lineHeight: 1.19))
                        .foregroundStyle(theme.colors.ink2)
                        .lineLimit(1)
                    Spacer(minLength: 0)
                    Image(systemName: "chevron.down")
                        .font(.system(size: 10, weight: .semibold))
                        .foregroundStyle(theme.colors.ink3)
                }
                .padding(.horizontal, 8)
                .frame(width: 164, height: 32)
                .contentShape(.rect(cornerRadius: 8))
            }
            .buttonStyle(PressHighlightStyle(radius: 8))
            .padding(.leading, 8)
            .padding(.top, 4)
            .opacity(collapsed ? 0 : 1)
            .allowsHitTesting(!collapsed)
            .accessibilityValue(workspaceOpen ? "Expanded" : "Collapsed")

            Button {
                withAnimation(Self.motion) {
                    collapsed = true
                    workspaceOpen = false
                    searchOpen = false
                    query = ""
                }
            } label: {
                Image(systemName: "sidebar.left").font(.system(size: 15))
            }
            .buttonStyle(IconButtonStyle(size: 32))
            .padding(.top, 4)
            .padding(.leading, Self.expandedWidth - 8 - 32)
            .opacity(collapsed ? 0 : 1)
            .allowsHitTesting(!collapsed)
            .accessibilityLabel("Collapse sidebar")

            Button {
                withAnimation(Self.motion) { collapsed = false }
            } label: {
                Image(systemName: "sidebar.left").font(.system(size: 15)).scaleEffect(x: -1)
            }
            .buttonStyle(IconButtonStyle(size: 36))
            .padding(.leading, 8)
            .padding(.top, 2)
            .opacity(collapsed ? 1 : 0)
            .allowsHitTesting(collapsed)
            .accessibilityLabel("Expand sidebar")
        }
        .animation(Self.copyMotion, value: collapsed)
    }

    private var chatsHeader: some View {
        ZStack(alignment: .trailing) {
            HStack(spacing: 6) {
                Image(systemName: "chevron.down").font(.system(size: 10, weight: .semibold))
                Text("Chats").buiFont(BUITextStyle(size: 12.5, weight: .medium, lineHeight: 1.19))
                Spacer(minLength: 0)
            }
            .foregroundStyle(theme.colors.ink3)
            .padding(.horizontal, 8)
            .opacity(searchOpen ? 0 : 1)
            .offset(x: searchOpen ? -4 : 0)

            // The field grows right to left out of the search button (180 ms).
            HStack(spacing: 6) {
                BUIIcon(.magnifier, size: 15).foregroundStyle(theme.colors.ink3)
                    .padding(.leading, searchOpen ? 8 : 6.5)
                if searchOpen {
                    TextField("Search chats", text: $query)
                        .buiFont(BUITextStyle(size: 13, weight: .medium, lineHeight: 1.19))
                        .foregroundStyle(theme.colors.ink)
                        .focused($searchFocused)
                        .accessibilityLabel("Search chat history")
                    Button {
                        withAnimation(BUICurve.link.animation(duration: 0.18)) {
                            searchOpen = false
                            query = ""
                        }
                    } label: {
                        BUIIcon(.close, size: 12, lineWidth: 2)
                    }
                    .buttonStyle(IconButtonStyle(size: 32))
                    .accessibilityLabel("Close chat search")
                }
            }
            .frame(width: searchOpen ? nil : 28, height: 32, alignment: .leading)
            .frame(maxWidth: searchOpen ? .infinity : 28, alignment: .trailing)
            .background(searchOpen ? theme.colors.field : .clear, in: .rect(cornerRadius: 8))
            .overlay {
                if searchOpen { RoundedRectangle(cornerRadius: 8).strokeBorder(theme.colors.line, lineWidth: 1) }
            }
            .contentShape(.rect)
            .onTapGesture {
                guard !searchOpen else { return }
                withAnimation(BUICurve.link.animation(duration: 0.18)) { searchOpen = true }
                searchFocused = true
            }
            // Closed, the control is one button; open, its field and close button stand alone.
            .accessibilityElement(children: searchOpen ? .contain : .ignore)
            .accessibilityAddTraits(searchOpen ? [] : .isButton)
            .accessibilityLabel(searchOpen ? "" : "Search chats")
        }
        .frame(height: 32)
    }

    private func row(symbol: String?, label: String, count: String? = nil, active: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 6) {
                if let symbol {
                    Image(systemName: symbol)
                        .font(.system(size: 15, weight: .medium))
                        .foregroundStyle(active ? theme.colors.ink : theme.colors.ink2)
                        .frame(width: 20, height: 20)
                }
                Text(label)
                    .buiFont(BUITextStyle(size: 14, weight: .medium, lineHeight: 1.19))
                    .foregroundStyle(active ? theme.colors.ink : theme.colors.ink2)
                    .lineLimit(1)
                    .copy(hidden: collapsed)
                Spacer(minLength: 0)
                if let count {
                    Text(count)
                        .buiFont(BUITextStyle(size: 12, weight: .medium, lineHeight: 1.19))
                        .monospacedDigit()
                        .foregroundStyle(theme.colors.ink3)
                        .padding(.trailing, 8)
                        .copy(hidden: collapsed)
                }
            }
            .padding(.horizontal, 8)
            .frame(width: collapsed ? 36 : 208, height: 32, alignment: .leading)
            .background(active ? theme.colors.hover2 : .clear, in: .rect(cornerRadius: 8))
            .contentShape(.rect(cornerRadius: 8))
        }
        .buttonStyle(RowPressScaleStyle())
        .padding(.horizontal, 8)
        .accessibilityAddTraits(active ? .isSelected : [])
    }

    private func select(_ id: String) {
        withAnimation(.easeOut(duration: 0.15)) { current = id }
        onSelect?(id)
    }
}

private extension View {
    /// `.sidebar-copy`: fades and slides 8 pt left when the rail collapses.
    func copy(hidden: Bool) -> some View {
        opacity(hidden ? 0 : 1)
            .offset(x: hidden ? -8 : 0)
            .allowsHitTesting(!hidden)
            .animation(SidebarNav.copyMotion, value: hidden)
    }
}

/// `active:scale-[0.98]` plus the press fill that stands in for hover.
struct RowPressScaleStyle: ButtonStyle {
    @Environment(\.buiTheme) private var theme

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(configuration.isPressed ? theme.colors.hover2 : .clear, in: .rect(cornerRadius: 8))
            .scaleEffect(configuration.isPressed ? 0.98 : 1)
            .animation(.easeOut(duration: 0.15), value: configuration.isPressed)
    }
}

private struct FooterButtonStyle: ButtonStyle {
    @Environment(\.buiTheme) private var theme

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .buiFont(BUITextStyle(size: 12.5, weight: .medium, lineHeight: 1.19))
            .foregroundStyle(theme.colors.ink)
            .frame(maxWidth: .infinity, minHeight: 32)
            .background(configuration.isPressed ? theme.colors.lineStrong : theme.colors.hover2, in: .rect(cornerRadius: theme.radii.control))
            .scaleEffect(configuration.isPressed ? 0.98 : 1)
            .animation(.easeOut(duration: 0.15), value: configuration.isPressed)
    }
}

/// The workspace switcher menu (`w-64 rounded-[14px] bg-surface p-1.5 shadow-overlay`).
private struct WorkspaceMenu: View {
    var workspace: String
    var onClose: () -> Void
    @Environment(\.buiTheme) private var theme

    var body: some View {
        VStack(spacing: 1) {
            menuButton {
                HStack(spacing: 6) {
                    Text(workspace.prefix(1))
                        .buiFont(BUITextStyle(size: 11, weight: .semibold, lineHeight: 1.19))
                        .foregroundStyle(theme.colors.surface)
                        .frame(width: 24, height: 24)
                        .background(theme.colors.ink, in: .rect(cornerRadius: 7))
                    Text(workspace)
                        .buiFont(BUITextStyle(size: 13.5, weight: .medium, lineHeight: 1.19))
                        .foregroundStyle(theme.colors.ink)
                    Spacer(minLength: 0)
                    Image(systemName: "checkmark").font(.system(size: 12, weight: .semibold)).foregroundStyle(theme.colors.ink)
                }
                .frame(height: 40)
            }
            divider
            ForEach([("New workspace", "plus"), ("Workspace settings", "gearshape"), ("Invite team members", "person.badge.plus")], id: \.0) { label, symbol in
                menuButton { item(label, symbol) }
            }
            divider
            menuButton { item("Sign out", "rectangle.portrait.and.arrow.right") }
        }
        .padding(6)
        .frame(width: 256)
        .buiSurface(radius: theme.radii.window, elevation: .overlay)
    }

    private var divider: some View {
        Rectangle().fill(theme.colors.line).frame(height: 1).padding(.vertical, 4)
    }

    private func item(_ label: String, _ symbol: String) -> some View {
        HStack(spacing: 6) {
            Image(systemName: symbol)
                .font(.system(size: 13))
                .foregroundStyle(theme.colors.ink2)
                .frame(width: 20, height: 20)
            Text(label)
                .buiFont(BUITextStyle(size: 13.5, lineHeight: 1.19))
                .foregroundStyle(theme.colors.ink)
            Spacer(minLength: 0)
        }
        .frame(height: 36)
    }

    private func menuButton<Label: View>(@ViewBuilder _ label: () -> Label) -> some View {
        Button(action: onClose) {
            label()
                .padding(.horizontal, 8)
                .contentShape(.rect(cornerRadius: 8))
        }
        .buttonStyle(PressHighlightStyle(radius: 8))
    }
}
