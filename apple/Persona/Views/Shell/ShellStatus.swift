import SwiftUI

#if os(iOS)
/// The shell's status row (docs/shell.md「状态行」): one line under the status bar for 等待网络 / 连接中… / 更新中… and the
/// app's toasts, taking its room (the pages and their bars make way) instead of floating over them. Floating, the link's
/// pill lay over Agents' filter chips and search field, and a toast over the top bar's switch, taking the tap meant
/// for it (2026-11-02).
///
/// One thing at a time: a toast over the link's state while it lasts (the link is lasting, a toast brief), then the
/// link again without folding in between. Toasts queue, and the row stays open until the last is gone. Opening and
/// folding move everything under the finger, so they wait while a finger drags or a list still glides (at most
/// `longestWait`), except for what must be seen at once: no network, an error.
@MainActor @Observable final class ShellNotices: ToastSink {
    enum Item: Equatable {
        case toast(Toast)
        case link(ConnectionStatus.Phase)

        /// Shown at once even under a moving finger.
        var urgent: Bool {
            switch self {
            case .toast(let t): t.style == .error
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
    /// The row's height with a toast in it (as drawn), and the status bar's above it (`ShellWindowHooks`).
    var toastHeight: CGFloat = 0 {
        didSet { if case .toast = shown { onRoom?(room, true) } }
    }
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
        case .toast: max(Self.linkHeight(scale: k), toastHeight)
        }
    }
    /// How many times the row opened and folded (`probe.shell`: a toast after the link must not fold it in between).
    private(set) var opened = 0
    private(set) var folded = 0
    @ObservationIgnored private var queue: [Toast] = []
    @ObservationIgnored private var link: ConnectionStatus.Phase = .online
    @ObservationIgnored private var busy = false
    @ObservationIgnored private var waitingSince: Date?
    @ObservationIgnored private var showing: Task<Void, Never>?
    static let longestWait: Duration = .seconds(3)

    /// What the row should show now.
    private var wanted: Item? {
        if let t = queue.first { return .toast(t) }
        return link == .online ? nil : .link(link)
    }

    func take(_ toast: Toast) {
        // The same words again (a retry failing twice) are one toast, shown for longer.
        if let i = queue.firstIndex(where: { $0.message == toast.message && $0.style == toast.style }) {
            if i == 0 { showing?.cancel(); showing = nil }
            queue[i] = toast
        } else {
            queue.append(toast)
        }
        settle()
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

    /// The toast was tapped, or its time is up: the next one, or the link, or folded.
    func dismiss(_ toast: Toast) {
        guard queue.first?.id == toast.id else { return }
        queue.removeFirst()
        showing?.cancel()
        showing = nil
        settle()
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
            ShellTrace.shared.note("row \(want.map { if case .toast = $0 { "toast" } else { "link" } } ?? "folds")\(late ? " late" : "")")
            #endif
            withAnimation(Theme.Motion.standard) { shown = want }
            onRoom?(room, true)
        }
        if case .toast(let t) = want, showing == nil {
            showing = Task { [weak self] in
                try? await Task.sleep(for: .seconds(t.style == .error ? 4 : 2.5))
                guard !Task.isCancelled else { return }
                #if DEBUG
                ShellTrace.shared.note("toast time up")
                #endif
                self?.dismiss(t)
            }
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
                    case .toast(let toast):
                        ShellToast(toast: toast) {
                            #if DEBUG
                            ShellTrace.shared.note("toast tapped")
                            #endif
                            notices.dismiss(toast)
                        }
                        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { h in
                            let row = h + 2 * Theme.Space.xs
                            if abs(row - notices.toastHeight) > 0.5 { notices.toastHeight = row }
                        }
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
        // Its own container: VoiceOver reads the link's state and the toasts.
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

/// A toast in the row: tapping it (only it) dismisses it.
private struct ShellToast: View {
    let toast: Toast
    let dismiss: () -> Void

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            Image(systemName: toast.style == .error ? Theme.Icon.errorFilled : toast.style == .success ? Theme.Icon.successFilled : Theme.Icon.info)
                .foregroundStyle(toast.style == .error ? Theme.danger : toast.style == .success ? Theme.positive : Theme.accent)
            Text(toast.message)
                .font(Theme.Font.label)
                .lineLimit(2)
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.xs)
        .modifier(ShellStatusSurface())
        .padding(.horizontal, Theme.Space.page)
        .contentShape(.capsule)
        .onTapGesture(perform: dismiss)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("shell-toast")
        .accessibilityAddTraits(.isButton)
        .accessibilityHint("关闭提示")
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

    static func dismantleUIView(_ v: Host, coordinator: ()) { v.setRoom(0, animated: false) }

    final class Host: UIView {
        private weak var notices: ShellNotices?
        weak var pager: ShellPager?
        private var watcher: Watcher?
        private var room: CGFloat = 0

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
#endif
