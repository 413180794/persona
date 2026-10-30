import SwiftUI
import UIKit

/// How far the Jarvis tab's sidebar is out (0 closed … 1 open). Its own object so following the finger only redraws
/// the drawer's frame (offsets, the page's corners), never the conversation.
@Observable final class AssistantDrawer {
    var progress: CGFloat = 0 {
        didSet { if (progress > 0) != isOut { isOut = progress > 0 } }
    }
    /// Out at all: changes only when the sidebar starts to come out or is fully back, so views that only care
    /// whether it is out (the tab bar) do not redraw on every frame of a drag.
    private(set) var isOut = false
    /// The sidebar's width, measured by `AssistantDrawerLayout`.
    @ObservationIgnored var width: CGFloat = 300
    @ObservationIgnored var dragStart: CGFloat = 0

    var isOpen: Bool { isOut }

    func open() {
        UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
        withAnimation(.snappy(duration: 0.32)) { progress = 1 }
    }

    func close() { withAnimation(.snappy(duration: 0.3)) { progress = 0 } }

    /// Where a drag lets go: past half way (or flicked) open, otherwise closed. `velocity` in points per second.
    nonisolated static func settlesOpen(progress: CGFloat, velocity: CGFloat, width: CGFloat) -> Bool {
        if abs(velocity) > 600 { return velocity > 0 }
        return progress + velocity * 0.15 / max(width, 1) > 0.5
    }
}

/// The sidebar beside the page, ChatGPT-style: it comes out from the left and pushes the page to the right, where
/// the page stays as a rounded card at the edge (a tap on it, or a drag back, closes). Dragging to the right
/// anywhere on the page opens it (`DrawerPan`).
struct AssistantDrawerLayout<Sidebar: View, Page: View>: View {
    let drawer: AssistantDrawer
    /// The page is at its root (nothing pushed): only then does a drag open the sidebar.
    var enabled: Bool
    @ViewBuilder var sidebar: Sidebar
    var page: Page

    var body: some View {
        GeometryReader { geo in
            let width = min(geo.size.width * 0.82, 380)
            let p = drawer.progress
            ZStack(alignment: .leading) {
                if p > 0 {
                    sidebar
                        .frame(width: width)
                        .offset(x: -(1 - p) * width * 0.3)
                        .opacity(0.4 + 0.6 * p)
                        .accessibilityIdentifier("assistant-sidebar")
                }
                page
                    // While the sidebar is out the page is only a card to tap back to (the overlay below says so).
                    .accessibilityHidden(p > 0)
                    .mask { RoundedRectangle(cornerRadius: 34 * p, style: .continuous).ignoresSafeArea() }
                    .background {
                        RoundedRectangle(cornerRadius: 34 * p, style: .continuous)
                            .fill(Theme.pageBackground)
                            .shadow(color: .black.opacity(0.18 * p), radius: 24)
                            .ignoresSafeArea()
                    }
                    // In dark mode the shadow is invisible: a hairline keeps the card apart from the sidebar.
                    .overlay {
                        if p > 0 {
                            RoundedRectangle(cornerRadius: 34 * p, style: .continuous)
                                .strokeBorder(Color.primary.opacity(0.12 * p), lineWidth: 1)
                                .ignoresSafeArea()
                                .allowsHitTesting(false)
                        }
                    }
                    .overlay {
                        if p > 0 {
                            // The page at the edge: dimmed a little, and a tap on it closes the sidebar.
                            Color.black.opacity(0.06 * p)
                                .contentShape(.rect)
                                .ignoresSafeArea()
                                .onTapGesture { drawer.close() }
                                .accessibilityElement()
                                .accessibilityLabel("关闭侧边栏")
                                .accessibilityAddTraits(.isButton)
                                .accessibilityAction { drawer.close() }
                                .accessibilityIdentifier("assistant-drawer-close")
                        }
                    }
                    .scaleEffect(1 - 0.06 * p, anchor: .leading)
                    .offset(x: p * width)
            }
            .frame(width: geo.size.width, height: geo.size.height, alignment: .leading)
            .background { Color(.secondarySystemBackground).ignoresSafeArea() }
            .gesture(DrawerPan(drawer: drawer, enabled: enabled))
            .onAppear { drawer.width = width }
            .onChange(of: width) { drawer.width = width }
        }
    }
}

/// The drag that opens and closes the sidebar. A UIKit pan so it can decide at the start of the touch: it only
/// starts sideways (vertical drags stay the list's), only to the right while closed, and never on something that
/// scrolls sideways itself (a wide code block or table that is not at its left edge) — those scroll first.
struct DrawerPan: UIGestureRecognizerRepresentable {
    let drawer: AssistantDrawer
    var enabled: Bool

    final class Coordinator: NSObject, UIGestureRecognizerDelegate {
        var drawer: AssistantDrawer
        var enabled = true
        init(drawer: AssistantDrawer) { self.drawer = drawer }

        func gestureRecognizerShouldBegin(_ g: UIGestureRecognizer) -> Bool {
            guard enabled, let pan = g as? UIPanGestureRecognizer, let view = pan.view else { return false }
            let v = pan.velocity(in: view), t = pan.translation(in: view)
            let dx = t.x != 0 ? t.x : v.x, dy = t.y != 0 ? t.y : v.y
            guard abs(dx) > abs(dy) * 1.2 else { return false } // sideways only
            if drawer.isOpen { return true }
            guard dx > 0 else { return false } // closed: only a drag to the right opens it
            let start = CGPoint(x: pan.location(in: view).x - t.x, y: pan.location(in: view).y - t.y)
            return !Self.sidewaysScroller(at: start, in: view)
        }

        /// Something under the finger scrolls sideways and has room to go back (it is not at its left edge).
        static func sidewaysScroller(at point: CGPoint, in view: UIView) -> Bool {
            var hit = view.hitTest(point, with: nil)
            while let v = hit, v !== view {
                if let s = v as? UIScrollView, s.isScrollEnabled, s.contentSize.width > s.bounds.width + 1,
                   s.contentOffset.x > -s.adjustedContentInset.left + 1 {
                    return true
                }
                hit = v.superview
            }
            return false
        }
    }

    func makeCoordinator(converter: CoordinateSpaceConverter) -> Coordinator { Coordinator(drawer: drawer) }

    func makeUIGestureRecognizer(context: Context) -> UIPanGestureRecognizer {
        let g = UIPanGestureRecognizer()
        g.delegate = context.coordinator
        g.maximumNumberOfTouches = 1
        return g
    }

    func updateUIGestureRecognizer(_ recognizer: UIPanGestureRecognizer, context: Context) {
        context.coordinator.enabled = enabled
    }

    func handleUIGestureRecognizerAction(_ recognizer: UIPanGestureRecognizer, context: Context) {
        let width = max(drawer.width, 1)
        switch recognizer.state {
        case .began:
            drawer.dragStart = drawer.progress
            if drawer.progress == 0 {
                UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
            }
        case .changed:
            let x = recognizer.translation(in: recognizer.view).x
            drawer.progress = min(1, max(0, drawer.dragStart + x / width))
        case .ended, .cancelled, .failed:
            let v = recognizer.velocity(in: recognizer.view).x
            if recognizer.state == .ended, AssistantDrawer.settlesOpen(progress: drawer.progress, velocity: v, width: width) {
                if drawer.dragStart == 0 { Haptics.tap() }
                drawer.open()
            } else {
                drawer.close()
            }
        default: break
        }
    }
}

/// What the sidebar lists (Jarvis's own things on top, then its conversations): searching, 置顶 / 最近, 查看全部.
struct AssistantSidebar: View {
    @Environment(AppModel.self) private var model
    var current: SessionKey?
    var open: (SessionKey) -> Void
    var newConversation: () -> Void
    var openProfile: () -> Void
    var openRoutines: () -> Void
    var openKnowledge: () -> Void
    /// nil when the tab bar is beside the sidebar (a column on a wide display): no second way to 设置.
    var openSettings: (() -> Void)?
    @State private var query = ""
    @State private var showAll = false
    @State private var ending: Session?
    @FocusState private var searching: Bool
    /// Conversations pinned on this phone (session ids, comma-separated); the main conversation always is.
    @AppStorage("assistant.pinned") private var pinnedRaw = ""

    private static let recentCount = 8

    private var pinnedIDs: Set<String> { Set(pinnedRaw.split(separator: ",").map(String.init)) }

    private func isPinned(_ s: Session) -> Bool { s.key == model.assistantMain || pinnedIDs.contains(s.sessionID) }

    var body: some View {
        let all = model.assistantConversations
        VStack(spacing: 0) {
            search
                .padding(.horizontal, 16)
                .padding(.top, 8)
                .padding(.bottom, 6)
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 2) {
                    if !query.isEmpty {
                        let hits = AssistantSidebarSearch.filter(all, query)
                        if hits.isEmpty {
                            Text("没有找到「\(query)」").font(.subheadline).foregroundStyle(.secondary)
                                .padding(.horizontal, 12).padding(.top, 12)
                        }
                        ForEach(hits) { conversation($0) }
                    } else {
                        entries
                        let main = model.assistantMain
                        let pinned = all.filter(isPinned).sorted { $0.key == main && $1.key != main } // the main one first
                        if !pinned.isEmpty {
                            heading("置顶")
                            ForEach(pinned) { conversation($0) }
                        }
                        let recent = all.filter { !isPinned($0) }
                        heading("最近")
                        if recent.isEmpty {
                            Text("还没有别的对话").font(.subheadline).foregroundStyle(.secondary).padding(.horizontal, 12)
                        }
                        ForEach(showAll ? recent : Array(recent.prefix(Self.recentCount))) { conversation($0) }
                        if recent.count > Self.recentCount {
                            Button {
                                withAnimation(.snappy) { showAll.toggle() }
                            } label: {
                                Text(showAll ? "收起" : "查看全部（\(recent.count)）…")
                                    .font(.subheadline.weight(.medium))
                                    .foregroundStyle(.secondary)
                                    .lineLimit(1)
                                    .padding(.horizontal, 12)
                                    .padding(.vertical, 10)
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                    .contentShape(.rect)
                            }
                            .buttonStyle(.plain)
                        }
                    }
                }
                .padding(.horizontal, 8)
                .padding(.bottom, 12)
            }
            .scrollDismissesKeyboard(.interactively)
            bottomBar
        }
        .confirmClose($ending)
    }

    private var search: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass").foregroundStyle(.secondary)
            TextField("搜索对话", text: $query)
                .focused($searching)
                .submitLabel(.search)
                .autocorrectionDisabled()
            if !query.isEmpty {
                Button { query = "" } label: { Image(systemName: "xmark.circle.fill").foregroundStyle(.tertiary) }
                    .buttonStyle(.plain)
                    .accessibilityLabel("清除搜索")
            }
        }
        .padding(.horizontal, 14)
        .frame(height: 42)
        .background(Theme.Fill.well, in: .capsule)
    }

    /// Jarvis itself, its routines and the knowledge base: the places that are not conversations.
    @ViewBuilder private var entries: some View {
        entry(action: openProfile) {
            JarvisAvatar(size: 28)
        } title: { "Jarvis" }
        entry(action: openRoutines) {
            Image(systemName: "alarm").font(.body.weight(.medium)).frame(width: 28)
        } title: { "定时任务" }
        entry(action: openKnowledge) {
            Image(systemName: "books.vertical").font(.body.weight(.medium)).frame(width: 28)
        } title: { "知识库" }
    }

    private func entry<Icon: View>(action: @escaping () -> Void, @ViewBuilder icon: () -> Icon, title: () -> String) -> some View {
        Button(action: action) {
            HStack(spacing: 12) {
                icon()
                Text(title()).font(.body.weight(.medium)).foregroundStyle(Color.primary).lineLimit(1)
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 12)
            .frame(height: 46)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
    }

    private func heading(_ text: String) -> some View {
        Text(text)
            .font(.footnote.weight(.semibold))
            .foregroundStyle(Color.secondary)
            .padding(.horizontal, 12)
            .padding(.top, 18)
            .padding(.bottom, 4)
    }

    private func conversation(_ s: Session) -> some View {
        let selected = s.key == current
        let title = s.title.isEmpty || s.title == "Claude Code" ? "新对话" : s.title
        return Button { open(s.key) } label: {
            HStack(spacing: 8) {
                if s.key == model.assistantMain {
                    Image(systemName: "house.fill").font(.caption).foregroundStyle(Theme.accent)
                }
                Text(title).font(.body).foregroundStyle(Color.primary).lineLimit(1)
                Spacer(minLength: 4)
                mark(s)
            }
            .padding(.horizontal, 12)
            .frame(height: 44)
            .background {
                if selected { RoundedRectangle(cornerRadius: 12, style: .continuous).fill(Theme.Fill.well) }
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel([title, s.key == model.assistantMain ? "主对话" : nil, stateWord(s)].compactMap { $0 }.joined(separator: "，"))
        .accessibilityAddTraits(selected ? .isSelected : [])
        .contextMenu {
            if s.key != model.assistantMain {
                Button { togglePin(s) } label: {
                    isPinned(s) ? Label("取消置顶", systemImage: "pin.slash") : Label("置顶", systemImage: "pin")
                }
            }
            Button {
                UIPasteboard.general.string = s.sessionID
                model.toast = Toast("已拷贝会话 ID", style: .success)
            } label: { Label("拷贝会话 ID", systemImage: "number") }
            if s.status.isLive {
                Button(role: .destructive) { ending = s } label: { Label("结束…", systemImage: "xmark.circle") }
            }
        }
    }

    /// Working / waiting for you / unread as a small mark; otherwise when it was last active.
    @ViewBuilder private func mark(_ s: Session) -> some View {
        switch s.state {
        case .working: ProgressView().controlSize(.mini)
        case .needsYou: Circle().fill(Theme.needsYou).frame(width: 8, height: 8)
        case .unread: Circle().fill(Theme.accent).frame(width: 8, height: 8)
        default: Text(Fmt.relative(s.updatedAt)).font(.caption).foregroundStyle(.secondary).lineLimit(1)
        }
    }

    private func stateWord(_ s: Session) -> String? {
        switch s.state {
        case .working, .needsYou, .unread: s.state.label
        default: s.updatedAt.map { Fmt.relative($0) }
        }
    }

    private func togglePin(_ s: Session) {
        var ids = pinnedIDs
        if ids.contains(s.sessionID) { ids.remove(s.sessionID) } else { ids.insert(s.sessionID) }
        pinnedRaw = ids.sorted().joined(separator: ",")
        Haptics.tap()
    }

    /// 新对话 as the main button, settings beside it.
    private var bottomBar: some View {
        HStack(spacing: 12) {
            Button(action: newConversation) {
                Label("新对话", systemImage: "square.and.pencil")
                    .font(.body.weight(.semibold))
                    .lineLimit(1)
                    .padding(.horizontal, 8)
                    .frame(height: 36)
            }
            .themedButton(prominent: true)
            .accessibilityIdentifier("sidebar-new")
            Spacer(minLength: 0)
            if let openSettings {
                Button(action: openSettings) {
                    Image(systemName: "gearshape").font(.body.weight(.semibold)).frame(width: 44, height: 44).contentShape(.circle)
                }
                .buttonStyle(.plain)
                .surface(interactive: true, in: .circle)
                .accessibilityLabel("设置")
            }
        }
        .padding(.horizontal, 16)
        .padding(.top, 8)
        .padding(.bottom, 10)
    }
}

/// The sidebar's search: a conversation matches on its title or its last message, ignoring case and width.
nonisolated enum AssistantSidebarSearch {
    static func filter(_ sessions: [Session], _ query: String) -> [Session] {
        let q = query.trimmingCharacters(in: .whitespaces)
        guard !q.isEmpty else { return sessions }
        let opts: String.CompareOptions = [.caseInsensitive, .widthInsensitive, .diacriticInsensitive]
        return sessions.filter { $0.title.range(of: q, options: opts) != nil || ($0.lastText ?? "").range(of: q, options: opts) != nil }
    }
}
