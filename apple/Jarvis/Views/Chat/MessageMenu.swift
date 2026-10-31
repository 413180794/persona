import SwiftUI
#if canImport(UIKit)
import UIKit
#endif

/// WeChat-style message actions: long-press a message and a dark bar with 复制 / 选择 / 引用 / 分享 (and 编辑 for your
/// own messages) appears right where you pressed — no lifted preview first. Tapping anywhere or scrolling closes it.
@Observable
final class MessageMenu {
    var selected: String?
    /// Where the press landed, in the pressed view's coordinates.
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
    @ObservationIgnored var quote: ((String) -> Void)?
    @ObservationIgnored var edit: ((String) -> Void)?
    /// The press that opened the bar. Its own lift must not close it: the list's simultaneous tap-to-close gesture
    /// recognizes that lift as a tap (only presses longer than ~2 s fail it), and a held finger's small drift shows
    /// up as list scrolling.
    /// How wide the chat is (`MessageMenu.area`): the bar stays inside it, not the screen — beside a list on a wide
    /// display (iPhone Duo open) the screen is much wider than the chat.
    @ObservationIgnored var areaWidth: CGFloat = 0
    /// The chat's coordinate space, set by the chat on its panes.
    static let area = "message-menu-area"
    @ObservationIgnored private var pressing = false
    @ObservationIgnored private var pressEnded = Date.distantPast

    #if os(iOS)
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

struct SelectableText: Identifiable {
    var text: String
    var id: String { text }
}

#if os(iOS)
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

/// UIKit long press: SwiftUI's own gesture does not say where the finger is.
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
        if recognizer.state == .began { action(context.converter.localLocation) }
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

private struct MessageActionBarHost: ViewModifier {
    @Environment(MessageMenu.self) private var menu: MessageMenu?
    let id: String
    let text: String
    let isUser: Bool
    let date: Date?

    func body(content: Content) -> some View {
        if let menu {
            content
                .overlay(alignment: .topLeading) {
                    if menu.selected == id {
                        GeometryReader { geo in
                            let frame = geo.frame(in: .global)
                            // The bar may stick out of a narrow bubble, but not out of the chat.
                            let inArea = geo.frame(in: .named(MessageMenu.area))
                            let areaWidth = max(menu.areaWidth, inArea.maxX)
                            let width = MessageActionBar.width(isUser: isUser)
                            let x = min(max(inArea.minX + menu.anchor.x, width / 2 + 8), areaWidth - width / 2 - 8) - inArea.minX
                            // Above the finger; below it when that would go under the navigation bar.
                            let above = frame.minY + menu.anchor.y - MessageActionBar.height - 16 > 140
                            let y = menu.anchor.y + (above ? -(MessageActionBar.height / 2 + 16) : MessageActionBar.height / 2 + 24)
                            MessageActionBar(text: text, isUser: isUser, date: date, menu: menu)
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
}

struct MessageActionBar: View {
    var text: String
    var isUser: Bool
    var date: Date?
    var menu: MessageMenu

    static let height: CGFloat = 62
    static let item: CGFloat = 58
    static func width(isUser: Bool) -> CGFloat { item * (isUser ? 5 : 4) + 12 }

    var body: some View {
        HStack(spacing: 0) {
            action("复制", "doc.on.doc") {
                UIPasteboard.general.string = text
                Haptics.success()
            }
            action("选择", "selection.pin.in.out") { menu.selecting = SelectableText(text: text) }
            action("引用", "quote.opening") { menu.quote?(text) }
            action("分享", "square.and.arrow.up") {
                menu.sharing = ShareMessage(text: text, isUser: isUser, source: menu.source(), date: date, agent: menu.agent())
            }
            if isUser {
                action("编辑", "pencil") { menu.edit?(text) }
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
        .frame(width: Self.item, height: Self.height)
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
                .toolbar {
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

#if os(iOS)
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
