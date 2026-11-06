import Foundation

/// The device token a UI test pairs with: `TEST_RUNNER_PERSONA_DEVICE_TOKEN` when the mock checks one
/// (`MOCK_DEVICE_TOKEN`, a real hub's `jd_…` shape, #1498), else the mock's `mock`.
enum UITestToken {
    static var device: String { ProcessInfo.processInfo.environment["PERSONA_DEVICE_TOKEN"] ?? "mock" }
}
