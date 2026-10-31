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

/// What the sidebar lists: search and 新对话 on top, Jarvis itself (its card, 定时任务, 知识库), then its conversations —
/// 置顶 (the main one first), the rest by day (今天 / 昨天 / 最近 7 天 / 更早). Each conversation shows its last message.
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
    @State private var showAllOlder = false
    @State private var ending: Session?
    @FocusState private var searching: Bool
    /// Conversations pinned on this phone (session ids, comma-separated); the main conversation always is.
    @AppStorage("assistant.pinned") private var pinnedRaw = ""

    /// 更早 shows this many until 查看全部.
    private static let olderCount = 8

    private var pinnedIDs: Set<String> { Set(pinnedRaw.split(separator: ",").map(String.init)) }

    private func isPinned(_ s: Session) -> Bool { s.key == model.assistantMain || pinnedIDs.contains(s.sessionID) }

    var body: some View {
        let all = model.assistantConversations
        VStack(spacing: 0) {
            topBar
                .padding(.horizontal, Theme.Space.l)
                .padding(.top, Theme.Space.s)
                .padding(.bottom, Theme.Space.m)
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    if query.isEmpty {
                        jarvisCard.padding(.bottom, Theme.Space.m)
                        shortcuts
                        conversations(all)
                    } else {
                        results(AssistantSidebarSearch.filter(all, query))
                    }
                }
                .padding(.horizontal, Theme.Space.m)
                .padding(.bottom, Theme.Space.l)
            }
            .scrollDismissesKeyboard(.interactively)
            if let openSettings { settingsRow(openSettings) }
        }
        .confirmClose($ending)
    }

    // MARK: Top: search and 新对话

    private var topBar: some View {
        HStack(spacing: Theme.Space.m) {
            HStack(spacing: Theme.Space.s) {
                Image(systemName: "magnifyingglass").font(Theme.Font.secondaryMedium).foregroundStyle(.secondary)
                TextField("搜索对话", text: $query)
                    .focused($searching)
                    .submitLabel(.search)
                    .autocorrectionDisabled()
                if !query.isEmpty {
                    Button { query = "" } label: { Image(systemName: Theme.Icon.clear).foregroundStyle(.tertiary) }
                        .buttonStyle(.plain)
                        .accessibilityLabel("清除搜索")
                }
            }
            .padding(.horizontal, Theme.Space.m)
            .frame(height: 40)
            .background(Theme.Fill.chip, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
            Button(action: newConversation) {
                Image(systemName: Theme.Icon.compose)
                    .font(Theme.Font.heading)
                    .foregroundStyle(Theme.accent)
                    .frame(width: 40, height: 40)
                    .background(Theme.accent.opacity(0.12), in: .circle)
                    .contentShape(.circle)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("新对话")
            .accessibilityIdentifier("sidebar-new")
        }
    }

    // MARK: Jarvis itself

    private var machineID: String { model.assistantConfig?.machineID ?? model.hubMachineID }

    /// Jarvis: its face, where it runs and whether that machine is up; a tap opens its profile.
    private var jarvisCard: some View {
        let machine = model.machine(machineID)
        let online = machine?.online ?? false
        return Button(action: openProfile) {
            HStack(spacing: Theme.Space.m) {
                JarvisAvatar(size: 42)
                VStack(alignment: .leading, spacing: Theme.Space.xs) {
                    Text("Jarvis").font(Theme.Font.heading).foregroundStyle(Color.primary).lineLimit(1)
                    HStack(spacing: Theme.Space.sm) {
                        Circle().fill(online ? Theme.positive : Color.secondary.opacity(0.5)).frame(width: 6, height: 6)
                        Text(online ? "在线 · \(machine?.name ?? machineID)" : "离线 · \(machine?.name ?? machineID)")
                            .lineLimit(1)
                    }
                    .font(Theme.Font.caption)
                    .foregroundStyle(.secondary)
                }
                Spacer(minLength: 0)
                Image(systemName: Theme.Icon.disclosure).font(Theme.Font.captionStrong).foregroundStyle(.tertiary)
            }
            .card(padding: 12)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Jarvis，\(online ? "在线" : "离线")，查看资料")
    }

    /// 定时任务 and 知识库, side by side.
    private var shortcuts: some View {
        HStack(spacing: Theme.Space.m) {
            tile("定时任务", "alarm", action: openRoutines)
            tile("知识库", "books.vertical", action: openKnowledge)
        }
    }

    private func tile(_ title: String, _ symbol: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: Theme.Space.s) {
                Image(systemName: symbol)
                    .font(Theme.Font.secondaryStrong)
                    .foregroundStyle(Theme.accent)
                    .frame(width: 28, height: 28)
                    .background(Theme.accent.opacity(0.12), in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
                Text(title).font(Theme.Font.secondaryStrong).foregroundStyle(Color.primary).lineLimit(1)
                Spacer(minLength: 0)
            }
            .card(padding: 10)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
    }

    // MARK: Conversations

    @ViewBuilder private func conversations(_ all: [Session]) -> some View {
        let main = model.assistantMain
        let pinned = all.filter(isPinned).sorted { $0.key == main && $1.key != main } // the main one first
        if !pinned.isEmpty {
            heading("置顶")
            ForEach(pinned) { row($0) }
        }
        let rest = all.filter { !isPinned($0) }
        if rest.isEmpty {
            Text(pinned.isEmpty ? "还没有对话：点右上角的新对话开始" : "还没有别的对话")
                .font(Theme.Font.secondary).foregroundStyle(.secondary)
                .padding(.horizontal, Theme.Space.m).padding(.top, Theme.Space.xl)
        }
        ForEach(AssistantSidebarGroups.group(rest), id: \.title) { group in
            heading(group.title)
            let older = group.title == AssistantSidebarGroups.older
            let shown = older && !showAllOlder ? Array(group.sessions.prefix(Self.olderCount)) : group.sessions
            ForEach(shown) { row($0) }
            if older, group.sessions.count > Self.olderCount {
                Button {
                    withAnimation(Theme.Motion.standard) { showAllOlder.toggle() }
                } label: {
                    Text(showAllOlder ? "收起" : "查看全部（\(group.sessions.count)）")
                        .font(Theme.Font.secondaryMedium)
                        .foregroundStyle(Theme.accent)
                        .lineLimit(1)
                        .padding(.horizontal, Theme.Space.m)
                        .padding(.vertical, Theme.Space.m)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
            }
        }
    }

    @ViewBuilder private func results(_ hits: [Session]) -> some View {
        if hits.isEmpty {
            VStack(spacing: Theme.Space.sm) {
                Image(systemName: "magnifyingglass").font(Theme.Font.title).foregroundStyle(.tertiary)
                Text("没有找到「\(query)」").font(Theme.Font.secondary).foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity)
            .padding(.top, 40)
        } else {
            heading("\(hits.count) 个对话")
            ForEach(hits) { row($0) }
        }
    }

    private func heading(_ text: String) -> some View {
        Text(text)
            .font(Theme.Font.sectionHeader)
            .foregroundStyle(Color.secondary)
            .padding(.horizontal, Theme.Space.m)
            .padding(.top, Theme.Space.xl)
            .padding(.bottom, Theme.Space.sm)
            .accessibilityAddTraits(.isHeader)
    }

    /// A conversation: its title (主对话 marked) over its last message; when, and its state, on the right.
    private func row(_ s: Session) -> some View {
        let selected = s.key == current
        let isMain = s.key == model.assistantMain
        let title = s.title.isEmpty || s.title == "Claude Code" ? "新对话" : s.title
        let preview = s.lastText.map { Fmt.plain($0).replacingOccurrences(of: "\n", with: " ") } ?? ""
        return Button { open(s.key) } label: {
            HStack(alignment: .center, spacing: Theme.Space.m) {
                VStack(alignment: .leading, spacing: Theme.Space.xs) {
                    HStack(spacing: Theme.Space.sm) {
                        Text(title).font(Theme.Font.secondaryStrong).foregroundStyle(Color.primary).lineLimit(1)
                        if isMain {
                            Text("主对话")
                                .font(Theme.Font.labelStrong)
                                .foregroundStyle(Theme.accent)
                                .padding(.horizontal, Theme.Space.sm)
                                .padding(.vertical, Theme.Space.xxs)
                                .background(Theme.accent.opacity(0.12), in: .capsule)
                                .fixedSize()
                        }
                    }
                    if !preview.isEmpty {
                        Text(preview).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
                Spacer(minLength: 4)
                VStack(alignment: .trailing, spacing: Theme.Space.sm) {
                    Text(Fmt.relative(s.updatedAt)).font(Theme.Font.label).foregroundStyle(.tertiary).lineLimit(1)
                    mark(s)
                }
            }
            .padding(.horizontal, Theme.Space.m)
            .padding(.vertical, Theme.Space.m)
            .background {
                if selected {
                    RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).fill(Theme.accent.opacity(0.12))
                }
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel([title, isMain ? "主对话" : nil, stateWord(s), preview.isEmpty ? nil : preview]
            .compactMap { $0 }.joined(separator: "，"))
        .accessibilityAddTraits(selected ? .isSelected : [])
        .contextMenu {
            if !isMain {
                Button { togglePin(s) } label: {
                    isPinned(s) ? Label("取消置顶", systemImage: Theme.Icon.unpin) : Label("置顶", systemImage: Theme.Icon.pin)
                }
            }
            Button {
                UIPasteboard.general.string = s.sessionID
                model.toast = Toast("已拷贝会话 ID", style: .success)
            } label: { Label("拷贝会话 ID", systemImage: Theme.Icon.id) }
            if s.status.isLive {
                Button(role: .destructive) { ending = s } label: { Label("结束…", systemImage: Theme.Icon.end) }
            }
        }
    }

    /// Waiting for you, working or unread as a small mark under the time; nothing otherwise.
    @ViewBuilder private func mark(_ s: Session) -> some View {
        switch s.state {
        case .working: ProgressView().controlSize(.mini).frame(height: 8)
        case .needsYou: Circle().fill(Theme.needsYou).frame(width: 8, height: 8)
        case .unread: Circle().fill(Theme.accent).frame(width: 8, height: 8)
        default: Color.clear.frame(width: 8, height: 8)
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

    /// 设置 at the bottom of the drawer (the tab bar is hidden while it is out).
    private func settingsRow(_ action: @escaping () -> Void) -> some View {
        VStack(spacing: 0) {
            Divider().padding(.horizontal, Theme.Space.l)
            Button(action: action) {
                HStack(spacing: Theme.Space.m) {
                    Image(systemName: Theme.Icon.settings).font(Theme.Font.heading).foregroundStyle(.secondary).frame(width: 28)
                    Text("设置").font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
                    Spacer(minLength: 0)
                }
                .padding(.horizontal, Theme.Space.xl)
                .frame(height: 50)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("设置")
        }
    }
}

/// The sidebar's conversations by day, newest first: 今天, 昨天, 最近 7 天, 更早 (and those without a time).
nonisolated enum AssistantSidebarGroups {
    static let older = "更早"

    struct Group: Equatable {
        var title: String
        var sessions: [Session]
    }

    static func group(_ sessions: [Session], now: Date = .now, calendar: Calendar = .current) -> [Group] {
        let sorted = sessions.sorted {
            let a = $0.updatedAt ?? .distantPast, b = $1.updatedAt ?? .distantPast
            return a != b ? a > b : $0.sessionID < $1.sessionID // a fixed order for equal times
        }
        let today = calendar.startOfDay(for: now)
        let yesterday = calendar.date(byAdding: .day, value: -1, to: today) ?? today
        let week = calendar.date(byAdding: .day, value: -7, to: today) ?? today
        var out: [Group] = []
        for s in sorted {
            let title: String
            if let d = s.updatedAt, d.isMeaningful {
                title = d >= today ? "今天" : d >= yesterday ? "昨天" : d >= week ? "最近 7 天" : older
            } else {
                title = older
            }
            if out.last?.title == title { out[out.count - 1].sessions.append(s) } else { out.append(Group(title: title, sessions: [s])) }
        }
        return out
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
