import UIKit
import XCTest

/// A Claude whose turn ended while a multi-agent workflow runs in the background (#2935): the Agents list shows it as
/// working with the workflow's name, phase and progress; its chat has a card per workflow that expands to its agents. Against scripts/mock/mock_hub.py with `MOCK_WORKFLOW=1` (JARVIS_MOCK_HUB); with JARVIS_SHOTS set the
/// screenshots are written there too (JARVIS_SHOTS_APPEARANCE names the simulator's appearance in the file names).
final class WorkflowUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    override func tearDown() {
        if let app, (testRun?.totalFailureCount ?? 0) > 0 {
            let a = XCTAttachment(screenshot: app.screenshot())
            a.lifetime = .keepAlways
            add(a)
            let t = XCTAttachment(string: app.debugDescription)
            t.lifetime = .keepAlways
            add(t)
        }
        super.tearDown()
    }

    @MainActor private func shot(_ name: String) {
        let env = ProcessInfo.processInfo.environment
        let image = app.screenshot()
        let a = XCTAttachment(screenshot: image)
        a.name = "\(name) \(UITestStyle.name)"
        a.lifetime = .keepAlways
        add(a)
        if let dir = env["JARVIS_SHOTS"] {
            let device = UIDevice.current.userInterfaceIdiom == .pad ? "ipad" : "iphone"
            let file = "workflow-\(device)-\(name)-\(env["JARVIS_SHOTS_APPEARANCE"] ?? "unknown").png"
            try? image.pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: file))
        }
    }

    private func labelled(_ text: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }

    @MainActor func testWorkflowInTheListTheChatAndItsAgents() throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_WORKFLOW=1 (JARVIS_MOCK_HUB)")
        }
        app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-dev.openChat", "", "-openAgentsFresh", "YES", "-dev.grouping", "state"]
        app.launchPinned()

        // The list: under 工作中, not 已看 / 完成未看, with the workflow's line.
        let working = app.buttons["agents.filter.working"]
        XCTAssertTrue(working.waitForExistence(timeout: 30), "no Agents list")
        working.tap()
        let row = app.buttons.matching(identifier: "agents.row").matching(NSPredicate(format: "label CONTAINS %@", "检查 PR #42")).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "the review agent is not among the working ones")
        XCTAssertTrue(labelled("后台 workflow「parity-audit」").waitForExistence(timeout: 5), "no workflow line on its row")
        sleep(1)
        shot("list")

        // Its chat: the 「后台 workflow」 card (the killed one beside it), phases and progress.
        row.tap()
        let card = app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", "后台 workflow parity-audit，在跑")).firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 15), "no workflow card in the chat")
        XCTAssertTrue(card.label.contains("169/185"), card.label)
        XCTAssertTrue(labelled("16 个在跑").exists, "no running count")
        XCTAssertTrue(labelled("Error: Workflow aborted").exists, "the killed one's error is missing")
        sleep(1)
        shot("chat")

        // 展开 agent: its agents, the running ones first.
        let expand = app.buttons.matching(identifier: "workflow.agents").firstMatch
        XCTAssertTrue(expand.waitForExistence(timeout: 5))
        expand.tap()
        let island = labelled("verify:island"), rich = labelled("audit:chat-rich")
        XCTAssertTrue(island.waitForExistence(timeout: 5), "a running agent is missing")
        XCTAssertTrue(rich.exists, "a finished agent is missing")
        XCTAssertLessThan(island.frame.minY, rich.frame.minY, "running agents come first")
        sleep(1)
        shot("agents")
    }
}
