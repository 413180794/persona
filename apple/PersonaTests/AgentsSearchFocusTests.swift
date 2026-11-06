import SwiftUI
import Testing
import UIKit
@testable import Persona

/// UIKit focus probes are not user intent; explicit activation remains a supported search entry point.
@MainActor struct AgentsSearchFocusTests {
    @Test func passiveFocusProbeDoesNotBeginSearch() {
        var began = 0
        let field = AgentsSearchField(text: .constant(""), active: false, begin: { began += 1 })
        let coordinator = AgentsSearchField.Coordinator(field)
        #expect(!coordinator.textFieldShouldBeginEditing(UISearchTextField()))
        #expect(began == 0)
    }

    @Test func activeInputCanEditWithoutBeginningAnotherSearch() {
        var began = 0
        let field = AgentsSearchField(text: .constant("query"), active: true, begin: { began += 1 })
        let coordinator = AgentsSearchField.Coordinator(field)
        #expect(coordinator.textFieldShouldBeginEditing(UISearchTextField()))
        #expect(began == 0)
    }

    @Test func explicitAccessibilityActivationBeginsSearchOnce() {
        let bar = UISearchBar()
        let holder = AgentsSearchField.Holder(field: bar.searchTextField, bar: bar)
        var began = 0
        holder.activateSearch = { began += 1 }
        #expect(holder.accessibilityActivate())
        #expect(began == 1)
    }
}
