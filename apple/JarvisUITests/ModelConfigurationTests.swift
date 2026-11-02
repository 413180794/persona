import XCTest

/// Against the mock hub: separated providers/purposes and a real PATCH through the editor.
final class ModelConfigurationTests: XCTestCase {
    private var hub: String!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let value = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py")
        }
        hub = value
    }

    @MainActor private func launch(_ extra: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-dev.openChat", ""] + extra
        app.launchPinned()
        return app
    }

    @MainActor func testSettingsSeparatesProvidersAndModelConfiguration() {
        let app = launch()
        let providers = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "模型供应商")).firstMatch
        let purposes = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "模型配置")).firstMatch
        for _ in 0..<4 where !purposes.isHittable { app.swipeUp() }
        XCTAssertTrue(providers.exists && purposes.exists)
        purposes.tap()
        XCTAssertTrue(app.descendants(matching: .any)["model-purpose-lark.agent"].waitForExistence(timeout: 10))
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    @MainActor func testPurposeSaveUsesTheUnifiedConfigurationEndpoint() throws {
        let app = launch(["-openModelConfiguration", "YES", "-openModelPurpose", "lark.agent"])
        let field = app.textFields["model-config-model"]
        XCTAssertTrue(field.waitForExistence(timeout: 10))
        field.tap()
        if let current = field.value as? String, !current.contains("留空"), !current.isEmpty {
            field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: current.count))
        }
        let requested = "model-ui-" + UUID().uuidString.prefix(8).lowercased()
        field.typeText(requested)
        let save = app.buttons["model-config-save"]
        XCTAssertTrue(save.isEnabled)
        save.tap()
        let deadline = Date().addingTimeInterval(10)
        var model: String?
        repeat {
            let data = try Data(contentsOf: URL(string: hub + "/api/v1/model-config/purposes/lark.agent")!)
            let result = try JSONSerialization.jsonObject(with: data) as? [String: Any]
            model = ((result?["purpose"] as? [String: Any])?["override"] as? [String: Any])?["model"] as? String
            if model != requested { usleep(100_000) }
        } while model != requested && Date() < deadline
        XCTAssertEqual(model, requested)
    }
}
