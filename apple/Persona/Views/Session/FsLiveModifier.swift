import SwiftUI

extension View {
    /// Live refresh for a Files or Git pane (#3089, `FsLive`): while the pane is in front (in the window and its tab
    /// picked, `onTabVisibility`) the hub watches `paths` (and with git the Git status of `cwd`) and `onChange` is told
    /// what changed, debounced. Coming back to the front after being hidden refreshes everything once, since changes
    /// were missed meanwhile.
    func fsLive(group: String, machineID: String, cwd: String, paths: [String], git: Bool,
                onChange: @escaping (FsChange) -> Void) -> some View {
        modifier(FsLiveScope(scope: FsLive.Scope(group: group, machineID: machineID, cwd: cwd), paths: paths, git: git, onChange: onChange))
    }
}

@MainActor
private struct FsLiveScope: ViewModifier {
    let scope: FsLive.Scope
    let paths: [String]
    let git: Bool
    let onChange: (FsChange) -> Void
    @Environment(AppModel.self) private var model
    @State private var box = Box()

    /// The watch in progress and whether the pane was in front before, kept across redraws without causing any.
    @MainActor final class Box {
        var handle: FsLive.Handle?
        var shownBefore = false
        var visible = false
        var onChange: (FsChange) -> Void = { _ in }
    }

    func body(content: Content) -> some View {
        box.onChange = onChange
        return content
            .onTabVisibility(appear: start, disappear: stop)
            .onChange(of: paths) { _, now in box.handle?.update(paths: now, git: git) }
            .onChange(of: git) { _, now in box.handle?.update(paths: paths, git: now) }
            .onChange(of: scope) { _, _ in
                // Another session folder (or the session's folder just came in): a watch of its own.
                guard box.visible else { return }
                box.handle?.stop()
                box.handle = nil
                watch()
            }
    }

    private func start() {
        box.visible = true
        watch()
        if box.shownBefore { box.onChange(FsChange(dirs: paths, git: git)) } // missed while hidden
        box.shownBefore = true
    }

    private func watch() {
        guard !scope.cwd.isEmpty, box.handle == nil else { return }
        let box = box
        box.handle = model.fsLive.watch(scope, paths: paths, git: git) { box.onChange($0) }
    }

    private func stop() {
        box.visible = false
        box.handle?.stop()
        box.handle = nil
    }
}
