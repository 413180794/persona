import UIKit
import XCTest

/// A Claude whose turn ended while a multi-agent workflow runs in the background (#2935): the Agents list shows it as
/// working with the workflow's name, phase and progress; its chat has a card per workflow that expands to its agents. Against scripts/mock/mock_hub.py with `MOCK_WORKFLOW=1` (PERSONA_MOCK_HUB); with PERSONA_SHOTS set the
/// screenshots are written there too (PERSONA_SHOTS_APPEARANCE names the simulator's appearance in the file names).
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
        if let dir = env["PERSONA_SHOTS"] {
            let device = UIDevice.current.userInterfaceIdiom == .pad ? "ipad" : "iphone"
            let file = "workflow-\(device)-\(name)-\(env["PERSONA_SHOTS_APPEARANCE"] ?? "unknown").png"
            try? image.pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: file))
        }
    }

    private func labelled(_ text: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }

    @MainActor func testWorkflowInTheListTheChatAndItsAgents() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_WORKFLOW=1 (PERSONA_MOCK_HUB)")
        }
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-dev.openChat", "", "-openAgentsFresh", "YES", "-dev.grouping", "state",
                                 "-holdChromeOpen", "YES"] // the header's cards are tapped: it must not fold by itself
        app.launchPinned()

        // The list: under Working, not Seen / Unread, with the workflow's line.
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

        // Show Agents: its agents, the running ones first.
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
    /// Three workflows (#4532, `MOCK_WORKFLOW=crowded`): the running one in full, the ended ones a line each (the stopped
    /// one's error kept); the section stays in the top part of the screen, its ten agents scroll inside it.
    @MainActor func testCrowdedWorkflowsLeaveRoomForTheChat() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB_CROWDED"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_WORKFLOW=crowded (PERSONA_MOCK_HUB_CROWDED)")
        }
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-dev.openChat", "", "-openAgentsFresh", "YES", "-dev.grouping", "state",
                                 "-holdChromeOpen", "YES"] // the header's cards are tapped: it must not fold by itself
        app.launchPinned()

        let working = app.buttons["agents.filter.working"]
        XCTAssertTrue(working.waitForExistence(timeout: 30), "no Agents list")
        working.tap()
        let row = app.buttons.matching(identifier: "agents.row").matching(NSPredicate(format: "label CONTAINS %@", "新手引导")).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "the onboarding agent is not among the working ones")
        row.tap()
        let card = app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", "后台 workflow onboarding-round4，在跑")).firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 15), "no workflow card in the chat")
        XCTAssertTrue(labelled("Error: Workflow aborted").exists, "the stopped one's error is missing")
        let window = app.windows.firstMatch.frame
        let section = app.descendants(matching: .any)["workflow.section"]
        XCTAssertTrue(section.exists, "no workflow section")
        XCTAssertLessThan(section.frame.maxY, window.minY + window.height * 0.5, "the folded section takes half the screen")
        sleep(1)
        shot("crowded-chat")

        app.buttons.matching(identifier: "workflow.agents").firstMatch.tap()
        XCTAssertTrue(labelled("verify:copy").waitForExistence(timeout: 5), "a running agent is missing")
        sleep(1)
        shot("crowded-agents")

        // Open, it takes at most its share of the page (`Theme.Size.workflowShare`) and the chat stays under it.
        XCTAssertLessThanOrEqual(section.frame.height, window.height * 0.4, "the open agents push the chat off the screen")
        XCTAssertLessThan(section.frame.maxY, window.minY + window.height * 0.6, "the open section reaches the conversation's half")

        // The agents scroll inside the section: the first one started (the last listed) comes into view.
        let first = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "plan:steps")).firstMatch
        for _ in 0..<6 where !(first.exists && first.isHittable && section.frame.contains(first.frame)) {
            section.swipeUp(velocity: .slow)
        }
        XCTAssertTrue(first.isHittable, "the last agent cannot be scrolled to")
        sleep(1)
        shot("crowded-agents-scrolled")
    }
}
