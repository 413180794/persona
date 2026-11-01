import XCTest

extension XCUIApplication {
    /// Agents starts with its search drawer folded. Reveal it with the same pull a user makes before typing.
    @MainActor @discardableResult func revealAgentsSearch() -> XCUIElement {
        let field = searchFields.firstMatch
        if field.exists && field.isHittable { return field }
        let list = collectionViews.firstMatch
        XCTAssertTrue(list.waitForExistence(timeout: 15), "the Agents list did not open")
        list.swipeDown(velocity: .slow)
        let visible = NSPredicate(format: "exists == true AND hittable == true")
        XCTAssertEqual(XCTWaiter.wait(for: [XCTNSPredicateExpectation(predicate: visible, object: field)], timeout: 5), .completed,
                       "the pull did not reveal Agents search")
        return field
    }
}
