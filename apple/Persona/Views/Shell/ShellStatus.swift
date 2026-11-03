import BeautifulUI
import SwiftUI

#if os(iOS)
/// The shell's status row (docs/shell.md「状态行」): one line under the status bar for what lasts, the hub's link
/// (等待网络 / 连接中… / 更新中…), taking its room (the pages and their bars make way) instead of floating over them.
/// Floating, the link's pill lay over Agents' filter chips and search field (2026-11-02).
///
/// The app's toasts are not the row's: the result of something just done (已归档, 已复制) is brief, and opening the
/// row for it pushed the pages and the drawer down and back (2026-11-04, #1053). They float over everything instead,
/// in a window that takes no touches (`ToastWindow`), under the row while it is open.
/// Opening and folding move everything under the finger, so they wait while a finger drags or a list still glides (at
/// most `longestWait`), except for what must be seen at once: no network.
@MainActor @Observable final class ShellNotices {
    enum Item: Equatable {
        case link(ConnectionStatus.Phase)

        /// Shown at once even under a moving finger.
        var urgent: Bool {
            switch self {
            case .link(let p): p == .waitingForNetwork
            }
        }
    }

    /// What the row shows; nil while it is folded away.
    private(set) var shown: Item? {
        didSet { if let shown { last = shown } }
    }
    /// What it showed last: drawn, transparent, while the row folds (a view inserted with a transition each time it
    /// opened was left transparent from the second time on, under the safe area's own animation).
    private(set) var last: Item?
    /// The status bar's height above the row (`ShellWindowHooks`).
    var statusBar: CGFloat = 0
    /// The text size's scale (the link's pill grows with it).
    @ObservationIgnored var scale: CGFloat = 1 {
        didSet { if scale != oldValue { onRoom?(room, false) } }
    }
    /// Told the row's room as it changes (`ShellWindowHooks` makes it): at once, not when a view is next drawn, which
    /// came a second late or more (the pill over the switch meanwhile).
    @ObservationIgnored var onRoom: ((CGFloat, Bool) -> Void)?
    var room: CGFloat { room(scale: scale) }

    /// The row's height with the link's pill in it at text scale `k`: known before the row opens.
    nonisolated static func linkHeight(scale k: CGFloat) -> CGFloat { Theme.Size.icon * k + 2 * Theme.Space.xs }

    /// The room the row takes from the pages at text scale `k`: the screen's top safe area grows by it.
    func room(scale k: CGFloat) -> CGFloat {
        switch shown {
        case nil: 0
        case .link: Self.linkHeight(scale: k)
        }
    }
    /// How many times the row opened and folded (`probe.shell`: a toast must not open it).
    private(set) var opened = 0
    private(set) var folded = 0
    @ObservationIgnored private var link: ConnectionStatus.Phase = .online
    @ObservationIgnored private var busy = false
    @ObservationIgnored private var waitingSince: Date?
    static let longestWait: Duration = .seconds(3)

    /// What the row should show now.
    private var wanted: Item? {
        link == .online ? nil : .link(link)
    }

    func linkChanged(_ phase: ConnectionStatus.Phase) {
        link = phase
        settle()
    }

    /// A finger is down on the screen, or a list still glides after one (`ShellWindowHooks`).
    func fingers(busy: Bool) {
        #if DEBUG
        ShellTrace.shared.note(busy ? "fingers busy" : "fingers idle")
        #endif
        self.busy = busy
        if !busy { settle() }
    }

    /// `late`: the longest wait is up, whatever the finger does.
    private func settle(late: Bool = false) {
        let want = wanted
        if (want == nil) != (shown == nil), busy, !late, !(want ?? shown)!.urgent {
            // Opening or folding moves the pages: not under a moving finger, but not for ever either.
            guard waitingSince == nil else { return }
            waitingSince = .now
            Task { [weak self] in
                try? await Task.sleep(for: Self.longestWait)
                guard let self, self.waitingSince != nil else { return }
                self.settle(late: true)
            }
            return
        }
        waitingSince = nil
        if want != shown {
            if want == nil { folded += 1 } else if shown == nil { opened += 1 }
            #if DEBUG
            ShellTrace.shared.note("row \(want == nil ? "folds" : "link")\(late ? " late" : "")")
            #endif
            withAnimation(Theme.Motion.standard) { shown = want }
            onRoom?(room, true)
        }
    }
}

/// The row itself, under the status bar in the room `ShellWindowHooks` makes for it (`ShellView`).
struct ShellStatusRow: View {
    let notices: ShellNotices

    var body: some View {
        let open = notices.shown != nil
        ZStack(alignment: .top) {
            if let item = notices.shown ?? notices.last {
                Group {
                    switch item {
                    case .link(let phase):
                        linkPill(phase)
                            .accessibilityElement(children: .combine)
                            .accessibilityIdentifier("shell-connection")
                    }
                }
                .padding(.vertical, Theme.Space.xs)
                .frame(maxWidth: .infinity)
            }
        }
        .opacity(open ? 1 : 0)
        .allowsHitTesting(open)
        .accessibilityHidden(!open)
        .animation(Theme.Motion.standard, value: notices.shown)
        // Its own container: VoiceOver reads the link's state.
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("shell-status-row")
        .padding(.top, notices.statusBar)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .ignoresSafeArea(edges: .top)
    }

    private func linkPill(_ phase: ConnectionStatus.Phase) -> some View {
        ConnectionLabel(phase: phase, compact: true)
            .padding(.horizontal, Theme.Space.m)
            .scaledFrame(height: Theme.Size.icon)
            .modifier(ShellStatusSurface())
            .allowsHitTesting(false)
    }
}

/// The status row's capsule: a material, or in the BeautifulUI style its raised capsule.
struct ShellStatusSurface: ViewModifier {
    func body(content: Content) -> some View {
        if AppStyle.current.usesBeautifulUI {
            content.buiSurface(Capsule(), elevation: .button)
        } else {
            content.background(.regularMaterial, in: .capsule)
        }
    }
}

/// The shell's hooks into its window (`ShellNotices`): the room for the status row, as more top safe area on the
/// window's root controller, so every page, bar, navigation bar and list makes way alike (a SwiftUI safe-area inset
/// reached the SwiftUI bars but not the UIKit navigation under them); and whether a finger is on the screen or a list
/// still glides after one: a recognizer on the window that only watches (it never begins, delays or cancels a touch),
/// then the window's scroll views until none glides.
struct ShellWindowHooks: UIViewRepresentable {
    let notices: ShellNotices
    let pager: ShellPager

    func makeUIView(context: Context) -> Host {
        let v = Host()
        v.isUserInteractionEnabled = false
        v.isAccessibilityElement = false
        v.attach(notices)
        v.pager = pager
        return v
    }

    func updateUIView(_ v: Host, context: Context) {
        v.attach(notices)
        v.pager = pager
        notices.scale = Theme.sizeScale(context.environment.dynamicTypeSize)
    }

    static func dismantleUIView(_ v: Host, coordinator: ()) {
        v.setRoom(0, animated: false)
        v.toasts?.close()
    }

    final class Host: UIView {
        private weak var notices: ShellNotices?
        weak var pager: ShellPager?
        private var watcher: Watcher?
        private var room: CGFloat = 0
        private(set) var toasts: ToastWindow?

        func attach(_ n: ShellNotices) {
            guard notices !== n else { return }
            notices = n
            n.onRoom = { [weak self] room, animated in self?.setRoom(room, animated: animated) }
            setRoom(n.room, animated: false)
        }

        override func didMoveToWindow() {
            super.didMoveToWindow()
            if let watcher { watcher.view?.removeGestureRecognizer(watcher) }
            guard let window else { return }
            let w = Watcher()
            w.changed = { [weak self] busy in self?.notices?.fingers(busy: busy) }
            w.touching = { [weak self] down in self?.pager?.fingers(down: down) }
            window.addGestureRecognizer(w)
            watcher = w
            if toasts?.over !== window, let notices {
                toasts?.close()
                toasts = ToastWindow(over: window, notices: notices)
            }
            noteStatusBar()
            setRoom(room, animated: false)
        }

        override func safeAreaInsetsDidChange() {
            super.safeAreaInsetsDidChange()
            noteStatusBar()
        }

        /// The window's own top inset (the status bar, the island): the row goes right under it.
        private func noteStatusBar() {
            guard let top = window?.safeAreaInsets.top, let notices, abs(notices.statusBar - top) > 0.5 else { return }
            notices.statusBar = top
        }

        func setRoom(_ room: CGFloat, animated: Bool) {
            self.room = room
            guard let root = window?.rootViewController, root.additionalSafeAreaInsets.top != room else { return }
            #if DEBUG || PERF_SIGNPOST
            NotificationCenter.default.post(name: .shellRoomChanges, object: room) // probe.agents-scroll's `room`
            #endif
            // Lists resting at their top stay at their top, moved with the page as one piece: their offset follows the
            // room's change, counted from before it (read after the layout, it can be a passing 0, below).
            let delta = room - root.additionalSafeAreaInsets.top
            let atTop = Self.scrollViews(in: root.view)
                .filter { abs($0.contentOffset.y + $0.adjustedContentInset.top) < 1 }
                .map { ($0, $0.contentOffset.y - delta) }
            let change = {
                root.additionalSafeAreaInsets.top = room
                root.view.layoutIfNeeded()
                for (s, y) in atTop { s.contentOffset.y = y }
            }
            if animated { UIView.animate(Theme.Motion.standard, changes: change) } else { change() }
            // Folding, a list in the navigation stack still reads an inset and offset of 0 for 100–200 ms while the stack
            // lays itself out again, and now and then (2 in 12) the navigation bar takes that for a scroll and folds a
            // search pulled out of it. Once settled, such a list goes back to where it rested, the search out again.
            DispatchQueue.main.asyncAfter(deadline: .now() + .milliseconds(600)) {
                for (s, y) in atTop where abs(s.contentOffset.y - y) > 1 && !s.isTracking {
                    s.setContentOffset(CGPoint(x: s.contentOffset.x, y: y), animated: true)
                }
            }
        }



        private static func scrollViews(in v: UIView) -> [UIScrollView] {
            let below = v.subviews.filter { !$0.isHidden }.flatMap { scrollViews(in: $0) }
            return (v as? UIScrollView).map { [$0] + below } ?? below
        }
    }

    final class Watcher: UIGestureRecognizer, UIGestureRecognizerDelegate {
        /// Busy from the first finger down until the last lets go and the lists it flung rest.
        var changed: (Bool) -> Void = { _ in }
        /// A finger is down, as the touches come and go.
        var touching: (Bool) -> Void = { _ in }
        private var down = 0
        private var gliding: Task<Void, Never>?

        override init(target: Any? = nil, action: Selector? = nil) {
            super.init(target: target, action: action)
            cancelsTouchesInView = false
            delaysTouchesBegan = false
            delaysTouchesEnded = false
            delegate = self
        }

        func gestureRecognizer(_ g: UIGestureRecognizer, shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer) -> Bool { true }

        override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent) {
            if down == 0 { gliding?.cancel(); changed(true); touching(true) }
            down += touches.count
        }

        override func touchesEnded(_ touches: Set<UITouch>, with event: UIEvent) { lift(touches) }
        override func touchesCancelled(_ touches: Set<UITouch>, with event: UIEvent) { lift(touches) }

        private func lift(_ touches: Set<UITouch>) {
            down = max(0, down - touches.count)
            guard down == 0 else { return }
            touching(false)
            state = .failed // back to .possible for the next touch; it never began
            gliding = Task { @MainActor [weak self] in
                // A flung list keeps moving under where the finger was: wait until it rests.
                for _ in 0..<30 {
                    guard let self, let root = self.view else { return }
                    if !Self.glides(root) { break }
                    try? await Task.sleep(for: .milliseconds(100))
                }
                guard let self, !Task.isCancelled, self.down == 0 else { return }
                self.changed(false)
            }
        }

        private static func glides(_ v: UIView) -> Bool {
            if let s = v as? UIScrollView, s.isDecelerating { return true }
            return v.subviews.contains { !$0.isHidden && glides($0) }
        }
    }
}
/// The shell's toasts (`AppModel.toast`) over everything in its scene, the drawer, the pages and a sheet over them, in a
/// window of their own above the shell's: it takes no touches, so what is under a toast takes the tap (a toast over the
/// top bar's switch took the tap meant for it, 2026-11-02), and it moves nothing (opening the status row for a toast
/// pushed the pages and the drawer down and back, #1053). Under the status row while that is open; the keyboard is
/// at the bottom, out of its way.
@MainActor final class ToastWindow {
    private(set) weak var over: UIWindow?
    private var window: UIWindow?

    init(over shell: UIWindow, notices: ShellNotices) {
        over = shell
        guard let scene = shell.windowScene else { return }
        let w = PassThrough(windowScene: scene)
        w.windowLevel = .normal + 1
        w.backgroundColor = .clear
        // 设置 › 外观 › 深浅色 reaches every window of the scene as it changes (`ColorSchemeChoice`); this one starts as the shell's.
        w.overrideUserInterfaceStyle = shell.overrideUserInterfaceStyle
        let host = UIHostingController(rootView: ToastWindowRoot(model: AppModel.shared, notices: notices))
        host.view.backgroundColor = .clear
        w.rootViewController = host
        w.isHidden = false
        window = w
    }

    func close() {
        window?.isHidden = true
        window = nil
    }

    /// Takes no touch: every one goes to the windows below.
    private final class PassThrough: UIWindow {
        override func hitTest(_ point: CGPoint, with event: UIEvent?) -> UIView? { nil }
    }
}

/// The toast window's content: the app's toast just below the top bars, under the status row's room while it is open,
/// in the app's text size and look. Right under the status bar it lay on the switch (助手 / Agents unreadable while it
/// showed) and on the drawer's title and search.
private struct ToastWindowRoot: View {
    @Environment(\.dynamicTypeSize) private var systemTypeSize
    let model: AppModel
    let notices: ShellNotices

    var body: some View {
        let size = model.textSize.applied(to: systemTypeSize)
        ToastOverlay(tapToDismiss: false)
            .padding(.top, notices.room + ShellNavigationClearance.needed(at: size))
            .animation(Theme.Motion.standard, value: notices.room)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
            .environment(model)
            .dynamicTypeSize(size)
            .buiTheme(.jarvis())
            .tint(Theme.accent)
    }
}
#endif
