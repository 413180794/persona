import XCTest

/// 设置 › 模型供应商 (#2455) in 体验模式 (DemoHub, no hub needed): the provider wizard walked through (选供应商 → 连接 →
/// 勾选模型 → 保存), 从 ~/.zshrc 导入 (a machine, its aliases ticked, imported) and a machine whose personad is too old.
/// TEST_RUNNER_JARVIS_SHOTS=<dir> keeps a screenshot of each step, named with TEST_RUNNER_JARVIS_APPEARANCE (light / dark).
final class ProviderWizardUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws { continueAfterFailure = false }

    @MainActor private func launch(_ step: String) {
        app = XCUIApplication()
        app.launchArguments = ["-demo.on", "YES", "-ui.oldLayout", "YES", "-tab", "settings", "-dev.openChat", "",
                               "-openModelLaunchers", "YES", "-openProviderWizard", step]
        app.launchPinned()
    }

    @MainActor private func shot(_ name: String) {
        let s = app.screenshot()
        let a = XCTAttachment(screenshot: s)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        guard let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] else { return }
        let look = ProcessInfo.processInfo.environment["JARVIS_APPEARANCE"] ?? "light"
        try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "wizard-\(name)-\(look).png"))
    }

    @MainActor private func el(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }

    @MainActor private func type(_ id: String, _ text: String) {
        let f = el(id)
        XCTAssertTrue(f.waitForExistence(timeout: 5), "no \(id):\n\(app.debugDescription)")
        f.tap()
        f.typeText(text)
    }

    @MainActor func testTheWizardAddsAProvider() {
        launch("pick")
        let custom = el("wizard.kind.custom")
        XCTAssertTrue(custom.waitForExistence(timeout: 15), "no 选供应商:\n\(app.debugDescription)")
        XCTAssertTrue(el("wizard.kind.anthropic").exists && el("wizard.kind.import").exists)
        shot("1-pick")
        custom.tap()

        let next = app.buttons["wizard.next"]
        XCTAssertTrue(next.waitForExistence(timeout: 5))
        XCTAssertFalse(next.isEnabled, "下一步 before anything is filled in")
        type("wizard.name", "jv_ui\n")
        type("wizard.url", "https://gateway.example.com\n")
        type("wizard.key", "sk-test-ui")
        XCTAssertTrue(next.isEnabled, "下一步 once name, address and key are in")
        app.buttons["wizard.discover"].tap()
        XCTAssertTrue(app.staticTexts["5 个"].waitForExistence(timeout: 5), "no models discovered:\n\(app.debugDescription)")
        app.buttons["wizard.test"].tap()
        XCTAssertTrue(el("wizard.testResult").waitForExistence(timeout: 5), "no test result")
        XCTAssertTrue(app.staticTexts["连接正常"].exists)
        shot("2-connect")

        next.tap()
        let glm = app.buttons["wizard.model.glm-5"]
        XCTAssertTrue(glm.waitForExistence(timeout: 5), "no 勾选模型:\n\(app.debugDescription)")
        let save = app.buttons["wizard.save"]
        XCTAssertFalse(save.isEnabled, "保存 with no model ticked")
        glm.tap()
        app.buttons["wizard.model.kimi-k2"].tap()
        XCTAssertEqual(glm.label, "已勾选 GLM-5")
        XCTAssertTrue(save.isEnabled)
        shot("3-models")
        // 上一步 keeps what was typed.
        app.buttons["wizard.back"].tap()
        XCTAssertTrue(next.waitForExistence(timeout: 5))
        next.tap()
        XCTAssertTrue(app.buttons["wizard.model.glm-5"].waitForExistence(timeout: 5))
        XCTAssertEqual(app.buttons["wizard.model.glm-5"].label, "已勾选 GLM-5")
        save.tap()
        XCTAssertTrue(save.waitForNonExistence(timeout: 10), "the sheet stays:\n\(app.debugDescription)")
        PasswordPrompt.assertNone(app, "a provider's API key")
        XCTAssertTrue(app.staticTexts["jv_ui"].waitForExistence(timeout: 10), "jv_ui is not in the list:\n\(app.debugDescription)")
    }

    @MainActor func testImportFromZshrc() {
        launch("import")
        let mac = app.buttons["import.machine.macbook"]
        XCTAssertTrue(mac.waitForExistence(timeout: 15), "no machine list:\n\(app.debugDescription)")
        shot("4-import-machine")
        mac.tap()
        let glm = app.buttons["import.provider.cc_glm"]
        XCTAssertTrue(glm.waitForExistence(timeout: 10), "no preview:\n\(app.debugDescription)")
        XCTAssertEqual(glm.label, "已勾选 cc_glm")
        XCTAssertTrue(app.staticTexts["合并到 demo_gateway"].exists)
        shot("5-import-preview")
        glm.tap()
        XCTAssertEqual(glm.label, "未勾选 cc_glm")
        app.buttons["import.confirm"].tap()
        XCTAssertTrue(el("import.result").waitForExistence(timeout: 10), "no result:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "新增 1 个")).firstMatch.exists, "\(app.debugDescription)")
        shot("6-import-done")
        app.buttons["import.done"].tap()
        XCTAssertTrue(app.staticTexts["cc_kimi"].waitForExistence(timeout: 10), "cc_kimi is not in the list")
    }

    /// 模型配置 › 主对话 has 运行方式; saving Agent SDK shows it in the category's line.
    @MainActor func testTheMainConversationsRuntime() {
        app = XCUIApplication()
        app.launchArguments = ["-demo.on", "YES", "-ui.oldLayout", "YES", "-tab", "settings", "-dev.openChat", "",
                               "-openModelConfiguration", "YES", "-openModelPurpose", "assistant"]
        app.launchPinned()
        let runtime = el("model-config-runtime")
        XCTAssertTrue(runtime.waitForExistence(timeout: 15), "no 运行方式:\n\(app.debugDescription)")
        runtime.tap()
        let sdk = app.buttons["Agent SDK"].firstMatch
        XCTAssertTrue(sdk.waitForExistence(timeout: 5), "no Agent SDK choice:\n\(app.debugDescription)")
        sdk.tap()
        shot("8-runtime")
        app.buttons["model-config-save"].tap()
        app.navigationBars.buttons.firstMatch.tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "Agent SDK")).firstMatch.waitForExistence(timeout: 10),
                      "主对话's line does not say Agent SDK:\n\(app.debugDescription)")
    }

    @MainActor func testAnOutdatedNodeIsExplained() {
        launch("import")
        let mini = app.buttons["import.machine.mini"]
        XCTAssertTrue(mini.waitForExistence(timeout: 15))
        mini.tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "personad 太旧")).firstMatch.waitForExistence(timeout: 10),
                      "no outdated message:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "重新部署这台电脑")).firstMatch.exists)
        shot("7-import-outdated")
    }
}
