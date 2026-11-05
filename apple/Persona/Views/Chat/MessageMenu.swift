import SwiftUI
#if canImport(UIKit)
import UIKit
import UIKit.UIGestureRecognizerSubclass
#endif

/// WeChat-style message actions: long-press a message and a dark bar with 复制 / 选择 / 引用 / 分享 (and 编辑 for your
/// own messages) appears right where you pressed — no lifted preview first. Tapping anywhere or scrolling closes it.
@Observable
final class MessageMenu {
    var selected: String?
    /// Where the press landed, in the message row's coordinates (`MessageMenu.row`), where the bar is placed.
    var anchor: CGPoint = .zero
    /// Text shown in the "select text" sheet.
    var selecting: SelectableText?
    /// The message being shared as a card.
    var sharing: ShareMessage?
    /// Where the chat's messages come from ("app › leader"), printed on share cards (read when sharing: the
    /// session may not be loaded yet when the chat binds the menu).
    @ObservationIgnored var source: () -> String = { "" }
    /// Who answers in this chat ("Claude" / "Codex" / "TRAEX"), for share cards.
    @ObservationIgnored var agent: () -> String = { "Claude" }
    /// 引用: the message's text, its id, and whether it is the user's own.
    @ObservationIgnored var quote: ((String, String, Bool) -> Void)?
    /// A message dragged to the left is quoted (Telegram's reply, #1058): Agents chats. The assistant's pages sit in
    /// the shell's pager, whose own sideways swipe this would take. Observed: the chat turns it on as it appears, after
    /// its first rows may have been drawn without it (they never got the drag, now and then).
    var swipeQuotes = false
    @ObservationIgnored var edit: ((String) -> Void)?
    /// 分支到新聊天 (#2137): a new conversation from the reply with this item id. canFork: the chat's session can.
    @ObservationIgnored var fork: ((String) -> Void)?
    @ObservationIgnored var canFork: () -> Bool = { false }
    /// 回退到这里 (#2915) on your own message: (item id, its text, what to restore). rewindBlock: nil when the chat's
    /// session never offers it, else why it is off now ("" when on).
    @ObservationIgnored var rewind: ((String, String, Rewind.Restore) -> Void)?
    @ObservationIgnored var rewindBlock: () -> String? = { nil }
    /// The message of yours whose 回退 was tapped: the chat asks what to restore (a dialog of the page, like `more`).
    var rewinding: RewindAsk?
    /// The reply whose ⋯ was tapped: the chat shows its menu (a dialog of the page, not of the bar, which the list's
    /// tap-to-close takes away with the same tap).
    var more: String?
    /// The press that opened the bar. Its own lift must not close it: the list's simultaneous tap-to-close gesture
    /// recognizes that lift as a tap (only presses longer than ~2 s fail it), and a held finger's small drift shows
    /// up as list scrolling.
    /// How wide the chat is (`MessageMenu.area`): the bar stays inside it, not the screen — beside a list on a wide
    /// display (iPhone Duo open) the screen is much wider than the chat.
    @ObservationIgnored var areaWidth: CGFloat = 0
    /// The chat's coordinate space, set by the chat on its panes.
    static let area = "message-menu-area"
    /// A message row's coordinate space (`messageActionBar`): the press is reported in it, where the bar is placed, not
    /// in the pressed bubble's, which sits at the right of the row for your own messages (#501).
    static let row = "message-menu-row"
    @ObservationIgnored private var pressing = false
    @ObservationIgnored private var pressEnded = Date.distantPast

    #if os(iOS) || os(visionOS)
    func pressChanged(_ state: UIGestureRecognizer.State) {
        switch state {
        case .began: pressing = true
        case .ended, .cancelled, .failed: pressing = false; pressEnded = .now
        default: break
        }
    }
    #else
    /// The Mac's secondary click (LongPressAt in apple/Mac/Media/MessageGestures.swift) reports the same phases.
    func pressChanged(_ state: PressPhase) {
        switch state {
        case .began: pressing = true
        case .ended, .cancelled, .failed: pressing = false; pressEnded = .now
        default: break
        }
    }
    #endif

    /// A tap or a scroll on the list: ignored while the press that opened the bar is held or has just lifted.
    func dismiss() {
        guard !pressing, Date.now.timeIntervalSince(pressEnded) > 0.3 else { return }
        close()
    }

    func close() {
        if selected != nil { withAnimation(.easeOut(duration: 0.15)) { selected = nil } }
    }
}

struct RewindAsk: Identifiable, Equatable {
    var id: String
    var text: String
}

struct SelectableText: Identifiable {
    var text: String
    var id: String { text }
}

#if os(iOS)
/// A drag to the left on a message, Telegram's reply (#1058). It fails as soon as the finger goes another way, and
/// the scroll views around it (the chat list, a pager) wait for that: scrolling starts as it always did, while a drag
/// that does go left moves the message and not the list. The left edge's swipe back goes right, so never starts it.
final class LeftDragRecognizer: UIPanGestureRecognizer {
    private var start: CGPoint?

    override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent) {
        start = touches.first?.location(in: view)
        super.touchesBegan(touches, with: event)
    }

    override func touchesMoved(_ touches: Set<UITouch>, with event: UIEvent) {
        if state == .possible, let start, let p = touches.first?.location(in: view) {
            let dx = p.x - start.x, dy = p.y - start.y
            if hypot(dx, dy) > MessageSwipe.decide {
                guard dx < 0, abs(dx) > abs(dy) * MessageSwipe.slope else {
                    state = .failed
                    return
                }
            }
        }
        super.touchesMoved(touches, with: event)
    }

    override func reset() {
        super.reset()
        start = nil
    }

    override func shouldBeRequiredToFail(by other: UIGestureRecognizer) -> Bool {
        other.view is UIScrollView && other is UIPanGestureRecognizer
    }

    // It waits for no one but the scroll views. The message's long press (the action bar) is a child's gesture, which
    // SwiftUI makes this one wait for, and the navigation stack's back pans (the edge, and on iOS 26 anywhere in the
    // content) are around it in the old layout: now and then the drag lost those races and never started. A drag
    // that goes left has moved past the press's allowance and is not a way back, so they never both happen.
    private static func standsAside(_ other: UIGestureRecognizer) -> Bool {
        other is UILongPressGestureRecognizer || (other is UIPanGestureRecognizer && !(other.view is UIScrollView))
    }

    override func require(toFail other: UIGestureRecognizer) {
        if Self.standsAside(other) { return }
        super.require(toFail: other)
    }

    override func shouldRequireFailure(of other: UIGestureRecognizer) -> Bool {
        Self.standsAside(other) ? false : super.shouldRequireFailure(of: other)
    }

    override func canBePrevented(by preventing: UIGestureRecognizer) -> Bool {
        Self.standsAside(preventing) ? false : super.canBePrevented(by: preventing)
    }
}

/// The drag as SwiftUI sees it: how far left (≤ 0) while it moves, and where it ended.
struct LeftDrag: UIGestureRecognizerRepresentable {
    var moved: (CGFloat) -> Void
    var ended: (CGFloat) -> Void

    func makeUIGestureRecognizer(context: Context) -> LeftDragRecognizer { LeftDragRecognizer() }

    func handleUIGestureRecognizerAction(_ recognizer: LeftDragRecognizer, context: Context) {
        let x = min(0, recognizer.translation(in: recognizer.view).x)
        switch recognizer.state {
        case .began, .changed: moved(x)
        case .ended: ended(x)
        case .cancelled, .failed: ended(0)
        default: break
        }
    }
}

/// UIKit swipe: it waits for a clear sideways flick and lets scroll views have everything else (a SwiftUI drag on the
/// chat list would take its scrolling).
struct SwipeTo: UIGestureRecognizerRepresentable {
    var direction: UISwipeGestureRecognizer.Direction
    var action: () -> Void

    func makeUIGestureRecognizer(context: Context) -> UISwipeGestureRecognizer {
        let g = UISwipeGestureRecognizer()
        g.direction = direction
        return g
    }

    func handleUIGestureRecognizerAction(_ recognizer: UISwipeGestureRecognizer, context: Context) {
        if recognizer.state == .ended { action() }
    }
}

/// UIKit long press: SwiftUI's own gesture does not say where the finger is. Reports it in the enclosing
/// `messageActionBar` row's coordinates (`MessageMenu.row`), so use it inside one.
struct LongPressAt: UIGestureRecognizerRepresentable {
    var changed: (UIGestureRecognizer.State) -> Void = { _ in }
    var action: (CGPoint) -> Void

    func makeUIGestureRecognizer(context: Context) -> UILongPressGestureRecognizer {
        let g = UILongPressGestureRecognizer()
        g.minimumPressDuration = 0.35
        return g
    }

    func handleUIGestureRecognizerAction(_ recognizer: UILongPressGestureRecognizer, context: Context) {
        changed(recognizer.state)
        if recognizer.state == .began { action(context.converter.location(in: .named(MessageMenu.row))) }
    }
}
#endif

extension View {
    /// The pressable part of a message (the bubble / the text).
    func messageLongPress(id: String) -> some View { modifier(MessageLongPress(id: id)) }
    /// The whole row of a message: hosts the action bar above the press point and lifts the row above its neighbours.
    func messageActionBar(id: String, text: String, isUser: Bool, date: Date? = nil) -> some View {
        modifier(MessageActionBarHost(id: id, text: text, isUser: isUser, date: date))
    }
}

private struct MessageLongPress: ViewModifier {
    @Environment(MessageMenu.self) private var menu: MessageMenu?
    let id: String

    func body(content: Content) -> some View {
        if let menu {
            content
                .gesture(LongPressAt(changed: menu.pressChanged) { p in
                    Haptics.tap()
                    menu.anchor = p
                    withAnimation(.snappy(duration: 0.2)) { menu.selected = id }
                })
                .opacity(menu.selected == id ? 0.75 : 1)
        } else {
            content
        }
    }
}

/// How a drag on a message quotes it (#1058).
enum MessageSwipe {
    /// How far the finger travels before the drag is told to be sideways or not.
    static let decide: CGFloat = 10
    /// Sideways is this much more across than down.
    static let slope: CGFloat = 1.3
    /// How far left a message goes before letting go quotes it: past this, a tap of the haptics and the icon fills.
    static var threshold: CGFloat { Theme.Size.tapTarget * 1.5 }
    /// Past the threshold the message follows the finger at this rate (a rubber band).
    static let stretch: CGFloat = 0.25

    /// Where the message sits for a drag of `x` (≤ 0).
    static func offset(_ x: CGFloat) -> CGFloat {
        x >= -threshold ? x : -threshold + (x + threshold) * stretch
    }
}

private struct MessageActionBarHost: ViewModifier {
    @Environment(MessageMenu.self) private var menu: MessageMenu?
    let id: String
    let text: String
    let isUser: Bool
    let date: Date?
    @State private var drag: CGFloat = 0
    @State private var armed = false

    func body(content: Content) -> some View {
        if let menu {
            swipeable(content, menu)
                .coordinateSpace(.named(MessageMenu.row))
                .overlay(alignment: .topLeading) {
                    if menu.selected == id {
                        GeometryReader { geo in
                            let frame = geo.frame(in: .global)
                            // The bar may stick out of a narrow bubble, but not out of the chat.
                            let inArea = geo.frame(in: .named(MessageMenu.area))
                            let areaWidth = max(menu.areaWidth, inArea.maxX)
                            let count = MessageActionBar.count(id: id, isUser: isUser, menu: menu)
                            // Six actions are wider than a small phone: they share what the chat has.
                            let width = min(MessageActionBar.width(count: count), max(menu.areaWidth, inArea.maxX) - 16)
                            let x = min(max(inArea.minX + menu.anchor.x, width / 2 + 8), areaWidth - width / 2 - 8) - inArea.minX
                            // Above the finger; below it when that would go under the navigation bar.
                            let above = frame.minY + menu.anchor.y - MessageActionBar.height - 16 > 140
                            let y = menu.anchor.y + (above ? -(MessageActionBar.height / 2 + 16) : MessageActionBar.height / 2 + 24)
                            MessageActionBar(id: id, text: text, isUser: isUser, date: date, menu: menu, itemWidth: MessageActionBar.itemWidth(width, count: count))
                                .position(x: x, y: y)
                                .transition(.scale(scale: 0.85).combined(with: .opacity))
                        }
                    }
                }
                .zIndex(menu.selected == id ? 1 : 0)
        } else {
            content.contextMenu { CopyButton(text: text) }
        }
    }

    /// The row as a drag to the left moves it, with the reply icon coming in at the right (#1058).
    @ViewBuilder private func swipeable(_ content: Content, _ menu: MessageMenu) -> some View {
        #if os(iOS)
        if menu.swipeQuotes {
            content
                .offset(x: MessageSwipe.offset(drag))
                .overlay(alignment: .trailing) {
                    if drag < 0 {
                        let progress = min(1, -drag / MessageSwipe.threshold)
                        Image(systemName: Theme.Icon.reply)
                            .font(Theme.Font.labelStrong)
                            .foregroundStyle(armed ? Color.white : Color.secondary)
                            .scaledFrame(width: Theme.Size.wellM, height: Theme.Size.wellM)
                            .background(armed ? AnyShapeStyle(Theme.accent) : AnyShapeStyle(Theme.Fill.chip), in: .circle)
                            .scaleEffect(0.6 + 0.4 * progress)
                            .opacity(progress)
                            .padding(.trailing, Theme.Space.s)
                            .accessibilityHidden(true)
                    }
                }
                .gesture(LeftDrag(moved: moved, ended: ended))
                .accessibilityAction(named: "引用") { menu.quote?(text, id, isUser) }
        } else {
            content
        }
        #else
        content
        #endif
    }

    private func moved(_ x: CGFloat) {
        menu?.close()
        drag = x
        let past = -x >= MessageSwipe.threshold
        if past != armed {
            armed = past
            if past { Haptics.tap() }
        }
    }

    private func ended(_ x: CGFloat) {
        if armed { menu?.quote?(text, id, isUser) }
        armed = false
        withAnimation(Theme.Motion.springBack) { drag = 0 }
    }
}

struct MessageActionBar: View {
    var id = ""
    var text: String
    var isUser: Bool
    var date: Date?
    var menu: MessageMenu
    var itemWidth: CGFloat = Self.item

    static let height: CGFloat = 62
    static let item: CGFloat = 58
    static func width(isUser: Bool, more: Bool = false) -> CGFloat { width(count: (isUser ? 5 : 4) + (more ? 1 : 0)) }
    static func width(count: Int) -> CGFloat { item * CGFloat(count) + 12 }
    /// Each action's width when the bar is `width` wide.
    static func itemWidth(_ width: CGFloat, count: Int) -> CGFloat { min(item, (width - 12) / CGFloat(max(count, 1))) }
    /// How many actions the bar has for this message.
    static func count(id: String, isUser: Bool, menu: MessageMenu) -> Int {
        (isUser ? 5 : 4) + (forks(id: id, isUser: isUser, menu: menu) ? 1 : 0) + (rewinds(id: id, isUser: isUser, menu: menu) ? 1 : 0)
    }
    /// Your own message ends in 回退 when the chat's session is a Claude agent's and the message is a transcript line
    /// of its own (`<uuid>#<n>`); while the session cannot, the dialog says why.
    static func rewinds(id: String, isUser: Bool, menu: MessageMenu) -> Bool {
        isUser && menu.rewind != nil && menu.rewindBlock() != nil && Rewind.entry(of: id) != nil
    }
    /// A reply ends in ⋯ with 分支到新聊天 when the session can fork and the reply is a Claude transcript line of its
    /// own (`<uuid>#<n>`).
    static func forks(id: String, isUser: Bool, menu: MessageMenu) -> Bool {
        guard !isUser, menu.fork != nil, menu.canFork(), let line = id.split(separator: "#").first, id.contains("#") else { return false }
        return UUID(uuidString: String(line)) != nil
    }

    var body: some View {
        HStack(spacing: 0) {
            action(String(localized: "复制"), "doc.on.doc") {
                UIPasteboard.general.string = text
                Haptics.success()
            }
            action(String(localized: "选择"), "selection.pin.in.out") { menu.selecting = SelectableText(text: text) }
            action(String(localized: "引用"), "quote.opening") { menu.quote?(text, id, isUser) }
            action(String(localized: "分享"), "square.and.arrow.up") {
                menu.sharing = ShareMessage(text: text, isUser: isUser, source: menu.source(), date: date, agent: menu.agent())
            }
            if isUser {
                action(String(localized: "编辑"), "pencil") { menu.edit?(text) }
            }
            if Self.rewinds(id: id, isUser: isUser, menu: menu) {
                action(String(localized: "回退"), Theme.Icon.rewindChat) { menu.rewinding = RewindAsk(id: id, text: text) }
            }
            if Self.forks(id: id, isUser: isUser, menu: menu) {
                action(String(localized: "更多"), Theme.Icon.more) { menu.more = id }
            }
        }
        .padding(.horizontal, Theme.Space.sm)
        .frame(height: Self.height)
        .background(Color(white: 0.17), in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        .shadow(color: .black.opacity(0.25), radius: 10, y: 4)
        .environment(\.colorScheme, .dark)
    }

    private func action(_ title: String, _ symbol: String, perform: @escaping () -> Void) -> some View {
        Button {
            perform()
            menu.close()
        } label: { label(title, symbol) }
        .buttonStyle(.plain)
    }

    private func label(_ title: String, _ symbol: String) -> some View {
        VStack(spacing: Theme.Space.xs) {
            // Symbols differ in height: a fixed box keeps every label on one line.
            Image(systemName: symbol).font(Theme.Font.heading).dynamicTypeSize(...DynamicTypeSize.xxLarge).scaledFrame(height: Theme.Size.iconS)
            Text(title).font(Theme.Font.label).lineLimit(1)
        }
        .foregroundStyle(.white)
        .frame(width: itemWidth, height: Self.height)
        .contentShape(.rect)
    }
}

/// The message in a plain text view where any part of it can be selected and copied ("选择").
struct SelectableTextSheet: View {
    @Environment(\.dismiss) private var dismiss
    var text: String

    var body: some View {
        NavigationStack {
            SelectableTextView(text: text)
                .navigationTitle("选择文本")
                .navigationBarTitleDisplayMode(.inline)
                .pageToolbar {
                    ToolbarItem(placement: .cancellationAction) { Button("完成") { dismiss() } }
                    ToolbarItem(placement: .primaryAction) {
                        Button {
                            UIPasteboard.general.string = text
                            Haptics.success()
                            dismiss()
                        } label: { Text("全部拷贝").lineLimit(1) }
                    }
                }
        }
        .presentationDetents([.medium, .large])
    }
}

#if os(iOS) || os(visionOS)
struct SelectableTextView: UIViewRepresentable {
    var text: String

    func makeUIView(context: Context) -> UITextView {
        let v = UITextView()
        v.isEditable = false
        v.isSelectable = true
        v.font = .preferredFont(forTextStyle: .body)
        v.adjustsFontForContentSizeCategory = true
        v.textContainerInset = UIEdgeInsets(top: 16, left: 12, bottom: 24, right: 12)
        v.backgroundColor = .clear
        return v
    }

    func updateUIView(_ v: UITextView, context: Context) {
        if v.text != text { v.text = text }
    }
}
#endif
