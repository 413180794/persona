import XCTest

/// Mock-only sends; the same draft, preview, outbox and transport as a picked attachment.
final class LarkAttachmentFlowTests: XCTestCase {
    @MainActor private func open(_ draft: String) throws -> XCUIApplication {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else { throw XCTSkip("needs mock Hub") }
        continueAfterFailure = false
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "mail", "-demo.on", "NO", "-ui.oldLayout", "NO",
                               "-inbox.source", "", "-inbox.kind", "mention", "-openInboxFirst", "NO",
                               "-showLarkAttachmentDraft", draft]
        app.launchPinned()
        let chat = app.staticTexts["设计评审"].firstMatch
        XCTAssertTrue(chat.waitForExistence(timeout: 20)); chat.tapCenter()
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "移除 ")).firstMatch.waitForExistence(timeout: 15))
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name; attachment.lifetime = .keepAlways; add(attachment)
        if let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] {
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + ".png"))
        }
    }

    @MainActor func testPhotoPreviewPullDownRemoveAndPicker() throws {
        let app = try open("photo")
        shot(app, "lark-photo-draft")
        app.buttons["预览 照片.jpg"].tapCenter()
        XCTAssertTrue(app.buttons["关闭"].waitForExistence(timeout: 8))
        shot(app, "lark-photo-preview")
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.4)).press(forDuration: 0.1,
            thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.85)))
        XCTAssertTrue(app.buttons["移除 照片.jpg"].waitForExistence(timeout: 5))
        app.buttons["移除 照片.jpg"].tapCenter()
        XCTAssertFalse(app.buttons["移除 照片.jpg"].exists)
        XCTAssertTrue(app.buttons["lark.send"].waitForNonExistence(timeout: 3), "removing the last attachment hides the empty send button")
        app.buttons["添加附件"].tapCenter()
        XCTAssertTrue(app.buttons["照片"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["文件"].exists)
        app.buttons["照片"].tapCenter()
        let cancel = app.buttons.matching(NSPredicate(format: "label IN %@", ["取消", "Cancel"])).firstMatch
        XCTAssertTrue(cancel.waitForExistence(timeout: 8)); cancel.tapCenter()
        XCTAssertTrue(app.buttons["添加附件"].waitForExistence(timeout: 5))
        app.terminate()
    }

    @MainActor func testPhotoSendReconcilesOnce() throws {
        let app = try open("photo")
        XCTAssertTrue(app.buttons["lark.send"].isEnabled, "a photo can be sent without typing text")
        app.buttons["发送"].tapCenter()
        XCTAssertFalse(app.buttons["移除 照片.jpg"].exists)
        XCTAssertTrue(app.descendants(matching: .any).matching(identifier: "lark.outgoing.sending").firstMatch.waitForExistence(timeout: 3))
        shot(app, "lark-photo-sending")
        XCTAssertTrue(app.descendants(matching: .any).matching(identifier: "lark.outgoing.sending").firstMatch.waitForNonExistence(timeout: 15))
        XCTAssertTrue(app.buttons["预览 照片.jpg"].waitForNonExistence(timeout: 10))
        let latest = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "回到最新")).firstMatch
        let reachedBottom = latest.waitForNonExistence(timeout: 5)
        shot(app, "lark-photo-sent")
        XCTAssertTrue(reachedBottom, "a receipt must keep the just-sent image at the bottom")
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.4)).press(forDuration: 0.1,
            thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.75)))
        XCTAssertTrue(latest.waitForExistence(timeout: 5), "scrolling back into history must still offer the latest jump")
        app.terminate()
    }

    @MainActor func testFileFailureRestoreAndRetry() throws {
        let app = try open("fail")
        shot(app, "lark-file-draft")
        app.buttons["预览 retry-once.txt"].tapCenter()
        let done = app.buttons.matching(NSPredicate(format: "label IN %@", ["关闭", "Close", "完成", "Done"])).firstMatch
        XCTAssertTrue(done.waitForExistence(timeout: 8))
        XCTAssertTrue(app.buttons["QLTextItemViewControllerBarSearchRightButtonAccessibilityIdentifier"].waitForExistence(timeout: 8))
        shot(app, "lark-file-preview")
        // Quick Look is hosted out of process; use its screen frame, not its remote coordinate origin.
        let closeFrame = done.frame
        app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: closeFrame.midX, dy: closeFrame.midY)).tap()
        XCTAssertTrue(done.waitForNonExistence(timeout: 5))
        app.buttons["发送"].tapCenter()
        let failed = app.buttons["lark.outgoing.failed"]
        let didFail = failed.waitForExistence(timeout: 12)
        shot(app, "lark-file-failed")
        XCTAssertTrue(didFail)
        // Leaving the chat does not discard the failed file or require picking it again.
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.01, dy: 0.5)).press(forDuration: 0.1,
            thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.5)))
        let chat = app.staticTexts["设计评审"].firstMatch
        XCTAssertTrue(chat.waitForExistence(timeout: 8)); chat.tapCenter()
        XCTAssertTrue(failed.waitForExistence(timeout: 10))
        failed.tapCenter()
        XCTAssertTrue(app.buttons["重发"].waitForExistence(timeout: 5))
        app.buttons["重发"].tapCenter()
        // A fast retry may finish while the menu dismisses; assert the durable outcome, not a minimum spinner time.
        XCTAssertTrue(app.descendants(matching: .any).matching(identifier: "lark.outgoing.sending").firstMatch.waitForNonExistence(timeout: 15))
        XCTAssertTrue(failed.waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.buttons["预览 retry-once.txt"].waitForNonExistence(timeout: 10))
        shot(app, "lark-file-retried")
        app.terminate()
    }
}
