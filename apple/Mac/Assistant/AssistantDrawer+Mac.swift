import AppKit
import SwiftUI

// The Mac's Persona sidebar drawer (iOS: `Views/Assistant/AssistantSidebar.swift`). A Mac window is wide, so the
// Persona page shows the sidebar as a split column (`horizontalSizeClass` is `.regular` here); the drawer below is
// only the narrow-window fallback: it slides in over the page and a click on the page closes it. No drag: a
// trackpad has no edge swipe to follow.

@Observable final class AssistantDrawer {
    var progress: CGFloat = 0 {
        didSet { if (progress > 0) != isOut { isOut = progress > 0 } }
    }
    private(set) var isOut = false
    @ObservationIgnored var width: CGFloat = 300
    @ObservationIgnored var dragStart: CGFloat = 0
    var screenBottom: CGFloat = 0

    var isOpen: Bool { isOut }

    func open(velocity: CGFloat = 0) {
        NSApp?.keyWindow?.makeFirstResponder(nil)
        withAnimation(Theme.Motion.layout) { progress = 1 }
    }

    func close(velocity: CGFloat = 0) { withAnimation(Theme.Motion.layout) { progress = 0 } }

    func dragBegan() { dragStart = progress }

    func drag(_ x: CGFloat) { progress = min(1, max(0, dragStart + x / max(width, 1))) }

    @discardableResult func letGo(velocity: CGFloat) -> Bool {
        let opens = Self.settlesOpen(progress: progress, velocity: velocity, width: max(width, 1))
        opens ? open() : close()
        return opens
    }

    nonisolated static func settlesOpen(progress: CGFloat, velocity: CGFloat, width: CGFloat) -> Bool {
        if abs(velocity) > 300 { return velocity > 0 }
        return progress + velocity * 0.2 / max(width, 1) > 0.5
    }

    nonisolated static func takes(dx: CGFloat, dy: CGFloat, isOpen: Bool) -> Bool {
        abs(dx) > abs(dy) && (isOpen || dx > 0)
    }
}

struct AssistantDrawerLayout<Sidebar: View, Page: View>: View {
    let drawer: AssistantDrawer
    var enabled: Bool
    @ViewBuilder var sidebar: Sidebar
    var page: Page

    var body: some View {
        GeometryReader { geo in
            let width = min(geo.size.width * 0.82, Theme.Size.macDrawer)
            ZStack(alignment: .leading) {
                page
                if drawer.isOut {
                    Theme.Fill.scrim.opacity(drawer.progress)
                        .contentShape(.rect)
                        .onTapGesture { drawer.close() }
                        .accessibilityLabel("Close Sidebar")
                        .accessibilityAddTraits(.isButton)
                    sidebar
                        .frame(width: width)
                        .frame(maxHeight: .infinity)
                        .background(Theme.systemCard)
                        .offset(x: -(1 - drawer.progress) * width)
                        .transition(.move(edge: .leading))
                }
            }
            .frame(width: geo.size.width, height: geo.size.height, alignment: .leading)
            .onAppear { drawer.width = width }
            .onChange(of: width) { drawer.width = width }
        }
    }
}

#if DEBUG
/// The iOS drag's profiler (`-showDrawerProbe`): a Mac has no drag to measure.
@MainActor final class DrawerProbe: NSObject {
    static let shared = DrawerProbe()
    static let on = false
    static func note(_ what: String, _ detail: [String: Any] = [:]) {}
    func dragBegan() {}
    func dragEnded(_ outcome: String) {}
}

/// `-showDrawerAutoDrag <rounds>`: scripted drags on iOS; on a Mac it opens and closes the drawer that many times.
@MainActor final class DrawerAutoDrag: NSObject {
    private let drawer: AssistantDrawer
    init(drawer: AssistantDrawer) { self.drawer = drawer }

    func run(rounds: Int) async {
        for _ in 0..<rounds {
            drawer.open()
            try? await Task.sleep(for: .milliseconds(600))
            drawer.close()
            try? await Task.sleep(for: .milliseconds(600))
        }
    }
}
#endif
