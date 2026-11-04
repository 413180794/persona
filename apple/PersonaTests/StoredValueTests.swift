import Foundation
import Observation
import Testing
@testable import Jarvis

/// `@StoredValue` redraws only for its own key (#1337): `@AppStorage` redrew every view holding one on any write to
/// UserDefaults (a Mac window resize writes the split view's frames every frame).
@MainActor
@Suite(.serialized)
struct StoredValueTests {
    let defaults = UserDefaults(suiteName: "StoredValueTests")!

    init() { defaults.removePersistentDomain(forName: "StoredValueTests") }

    /// Whether reading `read` registered a change by the time `act` ran and the notification queue drained.
    private func changes(_ read: @escaping () -> Void, _ act: () -> Void) async -> Bool {
        nonisolated(unsafe) var fired = false
        withObservationTracking(read) { fired = true }
        act()
        await Task.yield()
        try? await Task.sleep(for: .milliseconds(50))
        return fired
    }

    @Test func anotherKeysWriteDoesNotRedraw() async {
        let compact = StoredValue(wrappedValue: false, "test.compact", store: defaults)
        #expect(await !changes({ _ = compact.wrappedValue }) { defaults.set("x", forKey: "test.other") })
        // The same value written again is no change either.
        #expect(await !changes({ _ = compact.wrappedValue }) { defaults.set(false, forKey: "test.compact") })
    }

    @Test func itsOwnKeyRedraws() async {
        let compact = StoredValue(wrappedValue: false, "test.compact2", store: defaults)
        #expect(await changes({ _ = compact.wrappedValue }) { compact.wrappedValue = true })
        #expect(defaults.bool(forKey: "test.compact2"))
        // Written elsewhere (an `@AppStorage` of the same key): picked up.
        #expect(await changes({ _ = compact.wrappedValue }) { defaults.set(false, forKey: "test.compact2") })
        #expect(compact.wrappedValue == false)
    }

    @Test func rawValuesAndSharing() {
        enum Mode: String { case a, b }
        let one = StoredValue(wrappedValue: Mode.a, "test.mode", store: defaults)
        let two = StoredValue(wrappedValue: Mode.a, "test.mode", store: defaults)
        one.wrappedValue = .b
        #expect(two.wrappedValue == .b, "views storing the same key share one value")
        #expect(defaults.string(forKey: "test.mode") == "b")
    }
}
