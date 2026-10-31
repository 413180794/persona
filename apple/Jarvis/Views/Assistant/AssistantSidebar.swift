import SwiftUI
#if canImport(UIKit)
import UIKit
#endif

#if os(iOS)
/// How far the Jarvis tab's sidebar is out (0 closed … 1 open). Its own object so following the finger only redraws
/// the page's motion modifier (offset, scale, opacities) and moves two UIKit views, never the conversation or the sidebar.
@Observable final class AssistantDrawer {
    var progress: CGFloat = 0 {
        didSet {
            if (progress > 0) != isOut { isOut = progress > 0 }
            applyToUIKit()
        }
    }
    /// Out at all: changes only when the sidebar starts to come out or is fully back, so views that only care
    /// whether it is out (the sidebar clearing its search) do not redraw on every frame of a drag.
    private(set) var isOut = false
    /// The sidebar's width, measured by `AssistantDrawerLayout`.
    @ObservationIgnored var width: CGFloat = 300
    @ObservationIgnored var dragStart: CGFloat = 0
    /// The tab bar fades out as the sidebar comes out (it would sit over the sidebar's bottom row and repeat its 设置).
    /// Faded, not hidden: hiding it changes the page's safe area, and the whole conversation was laid out again on the
    /// first frame of every drag and the last of every close (20–40 ms each, sampled). Set by `DrawerTabBarProbe`.
    @ObservationIgnored weak var tabBar: UITabBar? {
        didSet { applyToUIKit() }
    }
    /// The sidebar's own hosting view (`DrawerSidebarHost`): moved and faded here, hidden while closed.
    @ObservationIgnored weak var sidebarView: UIView? {
        didSet { applyToUIKit() }
    }
    /// The screen's own bottom inset (the home indicator) without the tab bar: the sidebar reaches down to it.
    var screenBottom: CGFloat = 0
    /// A settle's animation is running: the sidebar is hidden when it ends, not when it starts.
    @ObservationIgnored private var settling = false

    var isOpen: Bool { isOut }

    /// The UIKit side of the progress: the tab bar's fade, the sidebar's slide and fade. Inside `settle` these
    /// animate with UIKit, on the same spring as the page.
    func applyToUIKit() {
        if let tabBar {
            // The bar's own container, when it holds only the bar: the tab bar controller sets the bar's alpha back to
            // 1 whenever SwiftUI refreshes the tab bar's visibility (a launch with the sidebar out showed it again).
            let fading = tabBar.superview.flatMap { $0.subviews == [tabBar] ? $0 : nil } ?? tabBar
            let alpha = 1 - progress
            if fading.alpha != alpha { fading.alpha = alpha }
            tabBar.isUserInteractionEnabled = progress == 0
            tabBar.accessibilityElementsHidden = progress > 0
        }
        if let sidebarView {
            sidebarView.transform = CGAffineTransform(translationX: -(1 - progress) * width * 0.3, y: 0)
            let hidden = progress == 0 && !settling
            sidebarView.alpha = hidden ? 0 : 0.4 + 0.6 * progress
            sidebarView.isHidden = hidden
            sidebarView.accessibilityElementsHidden = hidden
        }
    }

    /// `velocity`: the finger's, in points per second, when a drag lets go (the spring carries on at that speed).
    func open(velocity: CGFloat = 0) {
        UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
        settle(at: 1, velocity: velocity)
    }

    func close(velocity: CGFloat = 0) { settle(at: 0, velocity: velocity) }

    /// A drag took hold: the finger takes over where the page is (even mid-animation).
    func dragBegan() {
        dragStart = progress
        if progress == 0 {
            UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
        }
    }

    /// The finger is `x` points to the right of where the drag took hold. Straight to the value: no animation.
    func drag(_ x: CGFloat) {
        var t = Transaction()
        t.disablesAnimations = true
        withTransaction(t) { progress = min(1, max(0, dragStart + x / max(width, 1))) }
    }

    /// The finger let go at `velocity` points per second; returns whether it settles open.
    @discardableResult func letGo(velocity: CGFloat) -> Bool {
        let opens = Self.settlesOpen(progress: progress, velocity: velocity, width: max(width, 1))
        if opens {
            if dragStart == 0 { Haptics.tap() }
            open(velocity: velocity)
        } else {
            close(velocity: velocity)
        }
        return opens
    }

    private func settle(at target: CGFloat, velocity: CGFloat) {
        let way = (target - progress) * max(width, 1)
        let share = abs(way) > 1 ? max(0, Double(velocity / way)) : 0
        let motion = Theme.Motion.drawer(velocity: share)
        #if DEBUG
        DrawerProbe.note("settle", ["from": Double(progress), "to": Double(target), "share": share])
        #endif
        // The same spring for the tab bar and the sidebar (UIKit) as for the page (SwiftUI).
        settling = true
        UIView.animate(motion) {
            withAnimation(motion) { progress = target }
        } completion: { [weak self] in
            guard let self else { return }
            settling = false
            applyToUIKit()
        }
    }

    /// Where a drag lets go, like the system's back swipe: a flick goes its way, otherwise past half way (counting a
    /// little of where it is heading) opens. `velocity` in points per second.
    nonisolated static func settlesOpen(progress: CGFloat, velocity: CGFloat, width: CGFloat) -> Bool {
        if abs(velocity) > 300 { return velocity > 0 }
        return progress + velocity * 0.2 / max(width, 1) > 0.5
    }

    /// Whether a touch that has moved `dx`, `dy` from where it went down is the drawer's: within 45° of horizontal
    /// (a thumb's slant; vertical scrolls are steeper), and while closed only to the right.
    nonisolated static func takes(dx: CGFloat, dy: CGFloat, isOpen: Bool) -> Bool {
        abs(dx) > abs(dy) && (isOpen || dx > 0)
    }
}

/// The sidebar beside the page, ChatGPT-style: it comes out from the left and pushes the page to the right, where
/// the page stays as a rounded card at the edge (a tap on it, or a drag back, closes). Dragging to the right
/// anywhere on the page opens it (`DrawerPan`).
///
/// The sidebar stays built while closed (hidden): building it on the first frame of a drag was most of that frame.
/// While the finger moves only `DrawerPageMotion` reads the progress (the sidebar and the tab bar are moved by UIKit,
/// `AssistantDrawer.applyToUIKit`), and it changes nothing but transforms and opacities: the corners, shadow and edge
/// line are fixed shapes faded in, not redrawn per frame. docs/testing.md「侧边栏拖动」has the numbers.
struct AssistantDrawerLayout<Sidebar: View, Page: View>: View {
    let drawer: AssistantDrawer
    /// The page is at its root (nothing pushed): only then does a drag open the sidebar.
    var enabled: Bool
    /// Behind the sidebar: the system's grouped gray, or the shell's near-white (docs/ios.md「新外壳」).
    var backdrop: Color = Color(.secondarySystemBackground)
    /// The drag opens only from the screen's left edge: the page's rows swipe to the right themselves (the Agents page's 置顶 / 中断).
    var edgeOnly = false
    @ViewBuilder var sidebar: Sidebar
    var page: Page

    var body: some View {
        GeometryReader { geo in
            let width = min(geo.size.width * 0.82, 380)
            ZStack(alignment: .leading) {
                DrawerSidebarHost(drawer: drawer, content: sidebar.padding(.bottom, drawer.screenBottom))
                    .frame(width: width)
                    // Down under the faded tab bar, to the screen's own edge.
                    .ignoresSafeArea(.container, edges: .bottom)
                page
                    .modifier(DrawerPageMotion(drawer: drawer, width: width))
            }
            .frame(width: geo.size.width, height: geo.size.height, alignment: .leading)
            .background { backdrop.ignoresSafeArea() }
            .gesture(DrawerPan(drawer: drawer, enabled: enabled, edgeOnly: edgeOnly))
            .background { DrawerTabBarProbe(drawer: drawer) }
            .onAppear { drawer.width = width }
            .onChange(of: width) { drawer.width = width }
        }
    }
}

/// Finds the tab bar and the screen's bottom inset for `AssistantDrawer`.
private struct DrawerTabBarProbe: UIViewRepresentable {
    let drawer: AssistantDrawer

    func makeUIView(context: Context) -> Probe {
        let v = Probe()
        v.drawer = drawer
        v.isUserInteractionEnabled = false
        return v
    }

    func updateUIView(_ v: Probe, context: Context) {}

    final class Probe: UIView {
        weak var drawer: AssistantDrawer?

        override func didMoveToWindow() {
            super.didMoveToWindow()
            guard let drawer else { return }
            guard let window else {
                // Another tab (a notification can switch while the sidebar is out): the bar is that tab's again.
                // Without animation: the bar shows on the other tab, and the sidebar is in when the user comes back.
                drawer.progress = 0
                drawer.tabBar = nil
                return
            }
            var r: UIResponder? = self
            while let next = r?.next, !(next is UIViewController) { r = next }
            drawer.tabBar = (r?.next as? UIViewController)?.tabBarController?.tabBar
            drawer.screenBottom = window.safeAreaInsets.bottom
        }

        override func safeAreaInsetsDidChange() {
            super.safeAreaInsetsDidChange()
            if let window, let drawer, drawer.screenBottom != window.safeAreaInsets.bottom { drawer.screenBottom = window.safeAreaInsets.bottom }
        }
    }
}

/// The sidebar in a hosting controller of its own, with the page's whole environment. It stays built while closed
/// (building it on a drag's first frame cost that frame 15–20 ms on the simulator), and closed it has to be gone from
/// VoiceOver too: SwiftUI's `accessibilityHidden` left its search field, scroll view and rows in the tree. A UIKit view
/// can be hidden for real (`AssistantDrawer.sidebarView`), and moved and faded without SwiftUI updating anything.
private struct DrawerSidebarHost<Content: View>: UIViewControllerRepresentable {
    let drawer: AssistantDrawer
    let content: Content

    /// SwiftUI sets the frame, transform, alpha and hidden flag of the view it is given (every update put the
    /// sidebar back in place, shown): the hosting view inside it is the drawer's own.
    final class Container: UIViewController {
        let host: UIHostingController<AnyView>

        init(_ root: AnyView) {
            host = UIHostingController(rootView: root)
            super.init(nibName: nil, bundle: nil)
        }

        required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

        /// Touches only for the sidebar itself: closed (hidden), the container is clear room over the page's top bar and
        /// conversation, and took the 历史对话 button's tap on a 6.3" phone.
        final class PassThrough: UIView {
            override func hitTest(_ point: CGPoint, with event: UIEvent?) -> UIView? {
                let hit = super.hitTest(point, with: event)
                return hit === self ? nil : hit
            }
        }

        override func loadView() { view = PassThrough() }

        override func viewDidLoad() {
            super.viewDidLoad()
            view.backgroundColor = .clear
            // The frame given here is the sidebar's whole room; only the keyboard still insets it.
            host.safeAreaRegions = .keyboard
            host.sizingOptions = []
            host.view.backgroundColor = .clear
            addChild(host)
            view.addSubview(host.view)
            host.didMove(toParent: self)
        }

        override func viewDidLayoutSubviews() {
            super.viewDidLayoutSubviews()
            // Bounds and center, not frame: the drawer keeps a transform on it.
            host.view.bounds = view.bounds
            host.view.center = CGPoint(x: view.bounds.midX, y: view.bounds.midY)
        }
    }

    func makeUIViewController(context: Context) -> Container {
        let c = Container(root(context))
        c.loadViewIfNeeded()
        drawer.sidebarView = c.host.view
        return c
    }

    func updateUIViewController(_ c: Container, context: Context) {
        c.host.rootView = root(context)
        if drawer.sidebarView !== c.host.view { drawer.sidebarView = c.host.view }
    }

    private func root(_ context: Context) -> AnyView {
        AnyView(content.accessibilityIdentifier("assistant-sidebar").environment(\.self, context.environment))
    }
}

/// The page pushed aside: a card with rounded corners, a shadow and (dark mode) an edge line, dimmed a little; a tap on
/// it closes the sidebar.
private struct DrawerPageMotion: ViewModifier {
    let drawer: AssistantDrawer
    let width: CGFloat

    func body(content: Content) -> some View {
        let p = drawer.progress
        let out = p > 0
        // The corners only change when the page leaves or comes back to full screen (where the screen's own, rounder
        // corners hide them), so the mask and the shadow are not redrawn on every frame.
        let card = RoundedRectangle(cornerRadius: out ? Theme.Radius.page : 0, style: .continuous)
        content
            // While the sidebar is out the page is only a card to tap back to (the overlay below says so).
            .accessibilityHidden(out)
            .mask { card.ignoresSafeArea() }
            .background {
                ZStack {
                    card.fill(Color.black).shadow(color: .black.opacity(0.18), radius: 24).opacity(p)
                    card.fill(Theme.pageBackground)
                }
                .ignoresSafeArea()
            }
            .overlay {
                if out {
                    // In dark mode the shadow is invisible: a hairline keeps the card apart from the sidebar.
                    card.strokeBorder(Color.primary.opacity(0.12), lineWidth: 1)
                        .opacity(p)
                        .ignoresSafeArea()
                        .allowsHitTesting(false)
                    // The page at the edge: dimmed a little, and a tap on it closes the sidebar.
                    Color.black.opacity(0.06)
                        .opacity(p)
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
}

/// The drag that opens and closes the sidebar. A UIKit pan so it can decide when the finger has moved a little: it takes
/// the touch when that move is within 45° of horizontal (steeper drags stay the list's), only to the right while closed,
/// and never on something that scrolls sideways itself (a wide code block or table that is not at its left edge) —
/// those scroll first. The direction comes from where the touch went down (`Recognizer.start`): the pan's own
/// translation is still zero when it asks, and its velocity then is a guess from the last two samples (a 30° drag
/// read as almost straight down, so a slanted swipe did nothing).
struct DrawerPan: UIGestureRecognizerRepresentable {
    let drawer: AssistantDrawer
    var enabled: Bool
    var edgeOnly = false

    /// How far from the left edge a drag may start when `edgeOnly`: the system's back swipe takes about as much.
    static let edge: CGFloat = Theme.Space.xl

    final class Coordinator: NSObject, UIGestureRecognizerDelegate {
        var drawer: AssistantDrawer
        var enabled = true
        var edgeOnly = false
        init(drawer: AssistantDrawer) { self.drawer = drawer }

        func gestureRecognizerShouldBegin(_ g: UIGestureRecognizer) -> Bool {
            guard enabled, let pan = g as? Recognizer, let view = pan.view, let start = pan.start else { return false }
            let now = pan.location(in: view)
            let dx = now.x - start.x, dy = now.y - start.y
            #if DEBUG
            pan.asked = true
            func refused(_ why: String) -> Bool {
                DrawerProbe.note("refused", ["why": why, "dx": Int(dx), "dy": Int(dy)])
                return false
            }
            #else
            func refused(_ why: String) -> Bool { false }
            #endif
            guard AssistantDrawer.takes(dx: dx, dy: dy, isOpen: drawer.isOpen) else { return refused("direction") }
            if drawer.isOpen { return true }
            if edgeOnly, start.x > DrawerPan.edge { return refused("not from the edge") }
            return Self.sidewaysScroller(at: start, in: view) ? refused("sideways scroller") : true
        }

        /// Scroll views under the page wait for the drawer to decide, which it does within the same few points they
        /// would start in (so scrolling starts no later). Without this the conversation's pan won the race on a
        /// thumb's 40° swipe (its vertical part passed the list's threshold first) and the drawer was never asked:
        /// the swipe did nothing. A code block at its left edge likewise took a drag to the right to bounce.
        func gestureRecognizer(_ g: UIGestureRecognizer, shouldBeRequiredToFailBy other: UIGestureRecognizer) -> Bool {
            guard let s = other.view as? UIScrollView else { return false }
            return other === s.panGestureRecognizer
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

    /// The pan, remembering where the touch went down.
    final class Recognizer: UIPanGestureRecognizer {
        private(set) var start: CGPoint?
        #if DEBUG
        var asked = false
        private var moved: CGPoint = .zero
        #endif

        override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent) {
            if start == nil { start = touches.first?.location(in: view) }
            #if DEBUG
            asked = false
            moved = .zero
            #endif
            super.touchesBegan(touches, with: event)
        }

        #if DEBUG
        override func touchesMoved(_ touches: Set<UITouch>, with event: UIEvent) {
            super.touchesMoved(touches, with: event)
            if let s = start, let p = touches.first?.location(in: view) { moved = CGPoint(x: p.x - s.x, y: p.y - s.y) }
        }
        #endif

        override func reset() {
            #if DEBUG
            // Another gesture took the touch before this one was asked (`DrawerProbe`).
            if !asked, hypot(moved.x, moved.y) > 10 { DrawerProbe.note("lost", ["dx": Int(moved.x), "dy": Int(moved.y)]) }
            #endif
            start = nil
            super.reset()
        }
    }

    func makeCoordinator(converter: CoordinateSpaceConverter) -> Coordinator { Coordinator(drawer: drawer) }

    func makeUIGestureRecognizer(context: Context) -> Recognizer {
        let g = Recognizer()
        g.delegate = context.coordinator
        g.maximumNumberOfTouches = 1
        return g
    }

    func updateUIGestureRecognizer(_ recognizer: Recognizer, context: Context) {
        context.coordinator.enabled = enabled
        context.coordinator.edgeOnly = edgeOnly
    }

    func handleUIGestureRecognizerAction(_ recognizer: Recognizer, context: Context) {
        switch recognizer.state {
        case .began:
            #if DEBUG
            DrawerProbe.note("began", ["from": Double(drawer.progress)])
            DrawerProbe.shared.dragBegan()
            #endif
            drawer.dragBegan()
            drawer.drag(recognizer.translation(in: recognizer.view).x)
        case .changed:
            drawer.drag(recognizer.translation(in: recognizer.view).x)
        case .ended:
            let v = recognizer.velocity(in: recognizer.view).x
            let at = drawer.progress
            let opens = drawer.letGo(velocity: v)
            #if DEBUG
            DrawerProbe.note("ended", ["at": Double(at), "velocity": Int(v), "open": opens])
            DrawerProbe.shared.dragEnded(opens ? "open" : "closed")
            #endif
        case .cancelled, .failed:
            drawer.close()
            #if DEBUG
            DrawerProbe.shared.dragEnded("cancelled")
            #endif
        default: break
        }
    }
}

#endif

/// Calls `closed` when the drawer is back in: only this empty view reads whether it is out, so the sidebar itself is
/// not redrawn when it comes out.
private struct SidebarClosed: View {
    let drawer: AssistantDrawer
    var closed: () -> Void

    var body: some View {
        Color.clear.onChange(of: drawer.isOut) { _, out in if !out { closed() } }
    }
}

/// What the sidebar lists (2026-10-31, laid out like 豆包's): Jarvis's name with a search button; the things to start
/// (新对话, 定时任务, 知识库); 置顶 (the main conversation first) and 最近, one line each with a round mark; Jarvis's face,
/// where it runs and 设置 at the foot.
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
    /// The drawer it is in (nil as a column). The drawer's sidebar stays built while closed: closing it clears the search.
    var drawer: AssistantDrawer?
    /// The shell's drawer (docs/ios.md「新外壳」) also lists 收件箱 and 机器, and ends in 新对话 + 设置 like ChatGPT's.
    var openInbox: (() -> Void)? = nil
    var openMachines: (() -> Void)? = nil
    private var isShell: Bool { openInbox != nil }
    @State private var query = ""
    @State private var searchOpen = false
    @State private var showAll = false
    @State private var ending: Session?
    @State private var feed = SidebarFeed()
    @FocusState private var searching: Bool
    /// Conversations pinned on this phone (session ids, comma-separated); the main conversation always is.
    @AppStorage("assistant.pinned") private var pinnedRaw = ""

    /// 最近 shows this many until 查看全部.
    private static let recentCount = 30

    private var pinnedIDs: Set<String> { Set(pinnedRaw.split(separator: ",").map(String.init)) }

    private func isPinned(_ s: Session) -> Bool { s.key == model.assistantMain || pinnedIDs.contains(s.sessionID) }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("AssistantSidebar")
        #endif
        // Closed in its drawer, the sidebar keeps the list it last showed: `assistantConversations` reads every session
        // on every machine, so each hub push re-ran this body (three times a push) while nobody could see it. It
        // reads the live list again as soon as it starts to come out (`isOut`).
        let all = feed.conversations(drawer?.isOut ?? true ? model.assistantConversations : nil)
        VStack(spacing: 0) {
            topBar
                .padding(.horizontal, Theme.Space.xl)
                .padding(.top, Theme.Space.s)
                .padding(.bottom, Theme.Space.s)
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    if query.isEmpty {
                        actions
                        Divider().padding(.horizontal, Theme.Space.m).padding(.vertical, Theme.Space.s)
                        conversations(all)
                    } else {
                        results(AssistantSidebarSearch.filter(all, query))
                    }
                }
                .padding(.horizontal, Theme.Space.s)
                .padding(.bottom, Theme.Space.l)
            }
            .scrollDismissesKeyboard(.interactively)
            footer
        }
        .confirmClose($ending)
        .background {
            if let drawer {
                SidebarClosed(drawer: drawer) {
                    query = ""
                    searchOpen = false
                    searching = false
                }
            }
        }
    }

    // MARK: Top: the name, search

    @ViewBuilder private var topBar: some View {
        if searchOpen {
            HStack(spacing: Theme.Space.m) {
                HStack(spacing: Theme.Space.s) {
                    Image(systemName: Theme.Icon.search).font(Theme.Font.secondaryMedium).foregroundStyle(.secondary)
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
                .scaledFrame(height: Theme.Size.avatar)
                .background(Theme.Fill.chip, in: .capsule)
                Button("取消") {
                    withAnimation(Theme.Motion.state) {
                        query = ""
                        searchOpen = false
                        searching = false
                    }
                }
                .font(Theme.Font.body)
                .lineLimit(1)
            }
        } else {
            HStack {
                Text(model.persona.name).font(isShell ? Theme.Font.display : Theme.Font.title).foregroundStyle(Color.primary).lineLimit(1)
                Spacer(minLength: Theme.Space.s)
                Button {
                    withAnimation(Theme.Motion.state) { searchOpen = true }
                    searching = true
                } label: {
                    Image(systemName: Theme.Icon.search)
                        .font(Theme.Font.heading)
                        .foregroundStyle(Color.primary)
                        .scaledFrame(width: Theme.Size.avatarL, height: Theme.Size.avatarL)
                        .surface(interactive: true, in: .circle)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("搜索对话")
            }
        }
    }

    // MARK: What to start

    @ViewBuilder private var actions: some View {
        if let openInbox, let openMachines {
            // The shell's: Jarvis's own page first (认识 Jarvis, like ChatGPT's 认识 Dot), then the places to go.
            VStack(alignment: .leading, spacing: 0) {
                Button(action: openProfile) {
                    HStack(spacing: Theme.Space.l) {
                        PersonaAvatar(size: Theme.Size.icon)
                            .scaledFrame(width: Theme.Size.icon)
                        Text("认识 \(model.persona.name)").font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
                        Spacer(minLength: 0)
                    }
                    .padding(.horizontal, Theme.Space.m)
                    .scaledFrame(minHeight: Theme.Size.box50)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("sidebar-profile")
                action("收件箱", Theme.Icon.inbox, id: "sidebar-inbox", count: InboxStore.shared.actionCount, run: openInbox)
                action("定时任务", Theme.Icon.schedule, run: openRoutines)
                action("知识库", Theme.Icon.knowledge, run: openKnowledge)
                action("机器", Theme.Icon.machines, id: "sidebar-machines", run: openMachines)
            }
        } else {
            VStack(alignment: .leading, spacing: 0) {
                action("新对话", Theme.Icon.conversation, id: "sidebar-new", run: newConversation)
                action("定时任务", Theme.Icon.schedule, run: openRoutines)
                action("知识库", Theme.Icon.knowledge, run: openKnowledge)
            }
        }
    }

    private func action(_ title: String, _ symbol: String, id: String? = nil, count: Int = 0, run: @escaping () -> Void) -> some View {
        Button(action: run) {
            HStack(spacing: Theme.Space.l) {
                // One box for every symbol: a wide one (知识库's books) scales down into it instead of pushing its words.
                Image(systemName: symbol).resizable().scaledToFit().fontWeight(isShell ? .medium : .regular).foregroundStyle(Color.primary)
                    .scaledFrame(width: Theme.Size.iconS, height: Theme.Size.iconS)
                    .scaledFrame(width: Theme.Size.icon)
                Text(title).font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
                Spacer(minLength: 0)
                if count > 0 { CountBadge(count: count, look: .solid(Theme.needsYou)) }
            }
            .padding(.horizontal, Theme.Space.m)
            .scaledFrame(minHeight: isShell ? Theme.Size.box50 : Theme.Size.avatarL)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier(id ?? "sidebar-" + title)
    }

    // MARK: Conversations

    @ViewBuilder private func conversations(_ all: [Session]) -> some View {
        let main = model.assistantMain
        let pinned = all.filter(isPinned).sorted { $0.key == main && $1.key != main } // the main one first
        if !pinned.isEmpty {
            heading("置顶") {
                if pinned.contains(where: { $0.key != main }) {
                    Button { unpinAll() } label: { Label("全部取消置顶", systemImage: Theme.Icon.unpin) }
                }
            }
            ForEach(pinned) { row($0) }
        }
        let rest = AssistantSidebarGroups.newestFirst(all.filter { !isPinned($0) })
        if rest.isEmpty {
            Text(pinned.isEmpty ? "还没有对话：点上面的新对话开始" : "还没有别的对话")
                .font(Theme.Font.secondary).foregroundStyle(.secondary)
                .padding(.horizontal, Theme.Space.m).padding(.top, Theme.Space.xl)
        } else {
            heading("最近") {
                Button { newConversation() } label: { Label("新对话", systemImage: Theme.Icon.compose) }
                if rest.count > Self.recentCount {
                    Button { withAnimation(Theme.Motion.standard) { showAll.toggle() } } label: {
                        Label(showAll ? "只看最近 \(Self.recentCount) 个" : "查看全部（\(rest.count)）", systemImage: Theme.Icon.list)
                    }
                }
            }
            ForEach(showAll ? rest : Array(rest.prefix(Self.recentCount))) { row($0) }
            if rest.count > Self.recentCount {
                Button {
                    withAnimation(Theme.Motion.standard) { showAll.toggle() }
                } label: {
                    Text(showAll ? "收起" : "查看全部（\(rest.count)）")
                        .font(Theme.Font.secondary)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .padding(.leading, Theme.Space.m + Theme.Size.wellM + Theme.Space.m)
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
                Image(systemName: Theme.Icon.search).font(Theme.Font.title).foregroundStyle(.tertiary)
                Text("没有找到「\(query)」").font(Theme.Font.secondary).foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity)
            .padding(.top, 40)
        } else {
            heading("\(hits.count) 个对话") { EmptyView() }
            ForEach(hits) { row($0) }
        }
    }

    /// A section's name, small and gray, with its ⋯ menu on the right when it has one.
    private func heading<Menu: View>(_ text: String, @ViewBuilder menu: () -> Menu) -> some View {
        let items = menu()
        return HStack {
            Text(text).font(Theme.Font.caption).foregroundStyle(Color.secondary).accessibilityAddTraits(.isHeader)
            Spacer(minLength: 0)
            if !(items is EmptyView) {
                SwiftUI.Menu { items } label: {
                    Image(systemName: Theme.Icon.more).font(Theme.Font.captionStrong).foregroundStyle(.tertiary)
                        .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.wellXS)
                        .contentShape(.rect)
                }
                .tint(Color.secondary) // a gray ⋯, not the accent
                .accessibilityLabel(text + "的更多操作")
            }
        }
        .padding(.leading, Theme.Space.m)
        .padding(.top, Theme.Space.l)
        .padding(.bottom, Theme.Space.xs)
    }

    /// A conversation: a round mark (Jarvis's face for the main one), its title on one line, its state on the right.
    private func row(_ s: Session) -> some View {
        let selected = s.key == current
        let isMain = s.key == model.assistantMain
        let title = isMain ? "主对话" : s.title.isEmpty || s.title == "Claude Code" ? "新对话" : s.title
        let preview = s.lastText.map { Fmt.plain($0).replacingOccurrences(of: "\n", with: " ") } ?? ""
        return Button { open(s.key) } label: {
            HStack(spacing: Theme.Space.m) {
                // The shell's list is ChatGPT's: titles only, all in one column (the marks made it busy).
                if isShell {
                    EmptyView()
                } else if isMain {
                    PersonaAvatar(size: Theme.Size.wellM)
                } else {
                    ConversationMark(id: s.sessionID)
                }
                Text(title).font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
                Spacer(minLength: Theme.Space.xs)
                mark(s)
            }
            .padding(.horizontal, Theme.Space.m)
            .scaledFrame(minHeight: isShell ? Theme.Size.avatarXL : Theme.Size.box50)
            .background {
                if selected {
                    // Rounder in the shell: a pill, like ChatGPT's selected chat.
                    RoundedRectangle(cornerRadius: isShell ? Theme.Radius.panel : Theme.Radius.card, style: .continuous).fill(Theme.Fill.well)
                }
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel([title, isMain && s.title != title ? s.title : nil, stateWord(s), preview.isEmpty ? nil : preview]
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
        } preview: {
            // The long-press preview: what it last said (the row itself shows only the title).
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                Text(title).font(Theme.Font.heading).lineLimit(2)
                if !preview.isEmpty { Text(preview).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(6) }
                Text(Fmt.relative(s.updatedAt)).font(Theme.Font.label).foregroundStyle(.tertiary)
            }
            .padding(Theme.Space.l)
            .frame(width: Theme.Size.box280, alignment: .leading)
        }
    }

    /// Waiting for you, working or unread at the end of the row; nothing otherwise.
    @ViewBuilder private func mark(_ s: Session) -> some View {
        switch s.state {
        case .working: ProgressView().controlSize(.mini)
        case .needsYou: Circle().fill(Theme.needsYou).scaledFrame(width: Theme.Size.dot, height: Theme.Size.dot)
        case .unread: Circle().fill(Theme.accent).scaledFrame(width: Theme.Size.dot, height: Theme.Size.dot)
        default: EmptyView()
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

    private func unpinAll() {
        pinnedRaw = ""
        Haptics.tap()
    }

    // MARK: Foot: Jarvis, 设置

    private var machineID: String { model.assistantConfig?.machineID ?? model.hubMachineID }

    /// Jarvis's face and name with where it runs (a tap: its profile), 设置 on the right (the tab bar is hidden while
    /// the drawer is out).
    @ViewBuilder private var footer: some View {
        if isShell { shellFooter } else { profileFooter }
    }

    /// The shell's foot, ChatGPT's: a 新对话 pill on the left, 设置 in a glass circle on the right.
    private var shellFooter: some View {
        HStack(spacing: Theme.Space.m) {
            Button(action: newConversation) {
                Label("新对话", systemImage: Theme.Icon.compose)
                    .font(Theme.Font.bodyStrong)
                    .foregroundStyle(.white)
                    .lineLimit(1)
                    .padding(.horizontal, Theme.Space.xl)
                    .scaledFrame(height: Theme.Size.avatarL)
                    .background(Theme.Shell.newChat, in: .capsule)
                    .contentShape(.capsule)
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("sidebar-new")
            Spacer(minLength: Theme.Space.s)
            if let openSettings {
                Button(action: openSettings) {
                    Image(systemName: Theme.Icon.settings)
                        .font(Theme.Font.heading)
                        .foregroundStyle(Color.primary)
                        .scaledFrame(width: Theme.Size.avatarL, height: Theme.Size.avatarL)
                        .contentShape(.circle)
                }
                .buttonStyle(.plain)
                .surface(interactive: true, in: .circle)
                .accessibilityLabel("设置")
                .accessibilityIdentifier("sidebar-settings")
            }
        }
        .padding(.horizontal, Theme.Space.xl)
        .padding(.vertical, Theme.Space.m)
    }

    private var profileFooter: some View {
        let machine = model.machine(machineID)
        let online = machine?.online ?? false
        return VStack(spacing: 0) {
            Divider()
            HStack(spacing: Theme.Space.m) {
                Button(action: openProfile) {
                    HStack(spacing: Theme.Space.m) {
                        PersonaAvatar(size: Theme.Size.avatarL)
                        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                            HStack(spacing: Theme.Space.xs) {
                                Text(model.persona.name).font(Theme.Font.bodyStrong).foregroundStyle(Color.primary).lineLimit(1)
                                Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong).foregroundStyle(.tertiary)
                            }
                            HStack(spacing: Theme.Space.xs) {
                                Circle().fill(online ? Theme.positive : Color.secondary.opacity(0.5))
                                    .scaledFrame(width: Theme.Size.dotXS, height: Theme.Size.dotXS)
                                Text((online ? "在线 · " : "离线 · ") + (machine?.name ?? machineID)).lineLimit(1)
                            }
                            .font(Theme.Font.label)
                            .foregroundStyle(.secondary)
                        }
                    }
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityElement(children: .combine)
                .accessibilityLabel("\(model.persona.name)，\(online ? "在线" : "离线")，查看资料")
                Spacer(minLength: Theme.Space.s)
                if let openSettings {
                    Button(action: openSettings) {
                        Image(systemName: Theme.Icon.settings).font(Theme.Font.title).foregroundStyle(Color.primary)
                            .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                            .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("设置")
                }
            }
            .padding(.horizontal, Theme.Space.l)
            .padding(.vertical, Theme.Space.m)
        }
    }
}

/// A conversation's round mark in the sidebar: a chat bubble in a soft color picked by its id, so neighbours differ.
struct ConversationMark: View {
    var id: String

    private static let hues = [Theme.Hue.orange, Theme.Hue.purple, Theme.Hue.blue, Theme.Hue.green]

    var body: some View {
        let hue = Self.hues[Int(id.unicodeScalars.reduce(0) { ($0 &* 31 &+ Int($1.value)) & 0xFFFF }) % Self.hues.count]
        Circle()
            .fill(hue.opacity(0.14))
            .overlay(Circle().stroke(hue.opacity(0.25), lineWidth: Theme.Size.hairline))
            .scaledFrame(width: Theme.Size.wellM, height: Theme.Size.wellM)
            .overlay(Image(systemName: Theme.Icon.conversation).font(Theme.Font.label).foregroundStyle(hue))
            .accessibilityHidden(true)
    }
}

/// The sidebar's conversations by day, newest first: 今天, 昨天, 最近 7 天, 更早 (and those without a time).
nonisolated enum AssistantSidebarGroups {
    static let older = "更早"

    struct Group: Equatable {
        var title: String
        var sessions: [Session]
    }

    /// Newest first, a fixed order for equal times (the sidebar's 最近).
    static func newestFirst(_ sessions: [Session]) -> [Session] {
        sessions.sorted {
            let a = $0.updatedAt ?? .distantPast, b = $1.updatedAt ?? .distantPast
            return a != b ? a > b : $0.sessionID < $1.sessionID
        }
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

/// The conversations the sidebar showed last (not observed: keeping them changes nothing on screen).
final class SidebarFeed {
    private var last: [Session] = []

    /// `live` when the sidebar can be seen; nil while it is closed, for the list it had.
    func conversations(_ live: [Session]?) -> [Session] {
        if let live { last = live }
        return last
    }
}
