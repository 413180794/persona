import AppKit
import SwiftUI

/// What the window's commands open over the page (⌘K, ⌘N): set from the menu, the menu bar or ⌘K itself.
@Observable final class MacShellState {
    static let shared = MacShellState()
    var quickOpen = false
    var newAgent = false
    /// What ⌘K opens with typed in (DEBUG `-openQuickOpen <query>`).
    var seed = ""
}

/// ⌘K: a floating panel near the window's top, like Xcode's Open Quickly / Spotlight. The field keeps the focus;
/// ↑↓ move the selection, ⏎ opens it, Esc or a click outside closes.
struct MacQuickOpenPanel: View {
    @Environment(AppModel.self) private var model
    @State private var query = ""
    @State private var selected = 0
    @FocusState private var focused: Bool

    var body: some View {
        let groups = MacQuickOpen.sections(query, sessions: model.allSessions, machines: model.machines, inbox: InboxStore.shared.items)
        let hits = groups.flatMap(\.hits)
        VStack(spacing: 0) {
            HStack(spacing: Theme.Space.s) {
                Image(systemName: Theme.Icon.search)
                    .foregroundStyle(.secondary)
                    .accessibilityHidden(true)
                TextField("快速打开：Agent、Jarvis 对话、收件箱…", text: $query)
                    .textFieldStyle(.plain)
                    .font(Theme.Font.title)
                    .focused($focused)
                    .onSubmit { if hits.indices.contains(selected) { open(hits[selected]) } }
                    .onKeyPress(.downArrow) { selected = MacQuickOpen.move(selected, by: 1, count: hits.count); return .handled }
                    .onKeyPress(.upArrow) { selected = MacQuickOpen.move(selected, by: -1, count: hits.count); return .handled }
                    .onKeyPress(.escape) { close(); return .handled }
                    .accessibilityLabel("快速打开")
            }
            .padding(Theme.Space.m)
            if !hits.isEmpty {
                Divider()
                ScrollViewReader { proxy in
                    ScrollView {
                        LazyVStack(alignment: .leading, spacing: 0) {
                            ForEach(groups) { g in
                                Text(g.title)
                                    .font(Theme.Font.caption.weight(.semibold))
                                    .foregroundStyle(.secondary)
                                    .padding(.horizontal, Theme.Space.m)
                                    .padding(.top, Theme.Space.s)
                                    .padding(.bottom, Theme.Space.xs)
                                ForEach(g.hits) { hit in
                                    let i = hits.firstIndex(of: hit) ?? 0
                                    QuickOpenRow(hit: hit, selected: i == selected)
                                        .id(hit.id)
                                        .contentShape(Rectangle())
                                        .onTapGesture { open(hit) }
                                        .onHover { if $0 { selected = i } }
                                }
                            }
                        }
                        .padding(.bottom, Theme.Space.s)
                    }
                    .frame(maxHeight: Theme.Size.macQuickOpenListMax)
                    .fixedSize(horizontal: false, vertical: true)
                    .onChange(of: selected) { _, i in
                        if hits.indices.contains(i) { proxy.scrollTo(hits[i].id) }
                    }
                }
            } else if !query.isEmpty {
                Divider()
                Text("没有找到「\(query)」")
                    .foregroundStyle(.secondary)
                    .padding(Theme.Space.l)
            }
        }
        .frame(width: Theme.Size.macQuickOpenWidth)
        .background(Theme.Fill.macPanel, in: RoundedRectangle(cornerRadius: Theme.Radius.panel))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.panel).strokeBorder(.separator))
        .shadow(radius: Theme.Space.xl, y: Theme.Space.s)
        .onAppear {
            query = MacShellState.shared.seed
            MacShellState.shared.seed = ""
            focused = true
        }
        .onChange(of: query) { selected = 0 }
    }

    private func open(_ hit: MacQuickOpen.Hit) {
        close()
        switch hit.target {
        case .agent(let k), .conversation(let k): model.open(k)
        case .inbox(let id):
            model.selectedTab = .mail
            MacInboxState.shared.selection = [.item(id)]
        case .section(let s): model.selectedTab = s.tab
        case .newAgent: MacShellState.shared.newAgent = true
        case .newConversation:
            model.selectedTab = .assistant
            model.assistantCurrent = nil
        }
    }

    private func close() { MacShellState.shared.quickOpen = false }
}

private struct QuickOpenRow: View {
    var hit: MacQuickOpen.Hit
    var selected: Bool

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            Group {
                if let state = hit.state, state != .idle {
                    StatusDot(state: state)
                } else {
                    Image(systemName: hit.icon).foregroundStyle(selected ? .white : .secondary)
                }
            }
            .frame(width: Theme.Size.iconXS)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(hit.title).lineLimit(1)
                if !hit.subtitle.isEmpty {
                    Text(hit.subtitle)
                        .font(Theme.Font.caption)
                        .foregroundStyle(selected ? .white : .secondary)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 0)
        }
        .foregroundStyle(selected ? .white : .primary)
        .padding(.horizontal, Theme.Space.s)
        .padding(.vertical, Theme.Space.xs)
        .background(selected ? Color.accentColor : .clear, in: RoundedRectangle(cornerRadius: Theme.Radius.badge))
        .padding(.horizontal, Theme.Space.xs)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
    }
}

/// ⌘K over the whole window: a faint dim that closes it on a click, the panel near the top.
struct MacQuickOpenLayer: View {
    var body: some View {
        if MacShellState.shared.quickOpen {
            ZStack(alignment: .top) {
                Theme.Fill.macScrim
                    .ignoresSafeArea()
                    .onTapGesture { MacShellState.shared.quickOpen = false }
                    .accessibilityHidden(true)
                MacQuickOpenPanel()
                    .padding(.top, Theme.Size.macQuickOpenTop)
            }
        }
    }
}
