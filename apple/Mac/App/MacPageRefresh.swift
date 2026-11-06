import SwiftUI

/// The pages on screen that refresh themselves (iOS pulls them: Knowledge, Machines, a chat's Files / Git pane), per window.
/// View › Refresh ⌘R asks the key window's latest one; when it goes, the one under it answers again.
struct MacPageRefreshes {
    private var entries: [(id: UUID, window: Int)] = []

    mutating func add(_ id: UUID, window: Int) {
        entries.removeAll { $0.id == id }
        entries.append((id, window))
    }

    /// Its window number changed (or arrived): it moves there, on top.
    mutating func move(_ id: UUID, to window: Int) { add(id, window: window) }

    mutating func remove(_ id: UUID) { entries.removeAll { $0.id == id } }

    func top(window: Int?) -> UUID? {
        guard let window else { return nil }
        return entries.last { $0.window == window }?.id
    }
}

/// The registered pages and their refresh actions.
@MainActor final class MacPageRefreshRegistry {
    static let shared = MacPageRefreshRegistry()
    private var stack = MacPageRefreshes()
    private var actions: [UUID: () async -> Void] = [:]

    func set(_ id: UUID, window: Int, action: @escaping () async -> Void) {
        stack.move(id, to: window)
        actions[id] = action
    }

    func remove(_ id: UUID) {
        stack.remove(id)
        actions[id] = nil
    }

    /// The key window's page refresh, if a page there has one.
    func action(window: Int?) -> (() async -> Void)? {
        stack.top(window: window).flatMap { actions[$0] }
    }
}

extension View {
    /// This page refreshes itself on ⌘R (View › Refresh) while it is on screen in the key window: the Mac's pull to refresh.
    /// `id`: what the action reads that is not state (a pane's `cwd`, a machine's id); when it changes, the action
    /// registered with the old value is replaced.
    func macRefreshes(id: AnyHashable = 0, _ action: @escaping () async -> Void) -> some View {
        modifier(MacRefreshesModifier(key: id, action: action))
    }
}

private struct MacRefreshesModifier: ViewModifier {
    let key: AnyHashable
    let action: () async -> Void
    @State private var id = UUID()
    @State private var window: Int?

    func body(content: Content) -> some View {
        content
            .macWindowNumber($window)
            .onChange(of: window, initial: true) { _, w in
                if let w { MacPageRefreshRegistry.shared.set(id, window: w, action: action) }
            }
            .onAppear {
                // Back on screen in the same window (a pushed page popped): on top again.
                if let window { MacPageRefreshRegistry.shared.set(id, window: window, action: action) }
            }
            .onChange(of: key) {
                if let window { MacPageRefreshRegistry.shared.set(id, window: window, action: action) }
            }
            .onDisappear { MacPageRefreshRegistry.shared.remove(id) }
            // A section's page kept behind another one (`MacPageStack`) is not on screen: ⌘R goes to the one in front.
            .background {
                PageFrontWatcher(initial: false) { [id, window, action] front in
                    if !front { MacPageRefreshRegistry.shared.remove(id) }
                    else if let window { MacPageRefreshRegistry.shared.set(id, window: window, action: action) }
                }
            }
    }
}
