import Testing
@testable import Jarvis

/// 设置 › 手机数据 on the Mac: the sources the Mac has tools for. A Mac has no health data (no health tools are
/// registered there), so no health switch either.
@MainActor
struct PhoneDataSourceTests {
    @Test func noHealthOnTheMac() {
        #expect(!PhoneDataSource.onThisDevice.contains(.health))
        #expect(PhoneDataSource.onThisDevice == PhoneDataSource.allCases.filter { $0 != .health })
    }
}
