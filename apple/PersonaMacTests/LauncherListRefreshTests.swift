import Testing
@testable import Jarvis

/// Hiding, restoring or deleting a launcher on its own page left 设置 › 模型供应商's list as it was (#1433): the list
/// loaded once, and only the per-machine cache was forgotten. A change now moves `launchersRevision`, which the list
/// reloads on.
@MainActor
struct LauncherListRefreshTests {
    @Test func forgettingTheLaunchersMovesTheRevision() {
        let model = AppModel()
        let before = model.launchersRevision
        model.launchersChanged()
        #expect(model.launchersRevision != before)
        let once = model.launchersRevision
        model.launchersChanged()
        #expect(model.launchersRevision != once)
    }
}
