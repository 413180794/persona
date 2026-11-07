import Foundation

/// The same component renderer serves extension pages and assistant cards; only their control transport differs.
@MainActor
protocol ExtensionViewControlling: AnyObject {
    var values: [String: String] { get set }
    var query: String { get set }
    var running: String? { get }
    var loadingMore: String? { get }
    var fieldsEnabled: Bool { get }
    var managesConfirmation: Bool { get }
    func isEnabled(_ action: ExtensionAction) -> Bool
    func selected(_ action: String) -> Bool
    func prepared(_ action: ExtensionAction, with chosen: [String: String], displayed: ExtensionDisplayedRevision?) -> @MainActor () async -> ExtensionViewModel.Outcome
    func perform(_ action: ExtensionAction, with chosen: [String: String]) async -> ExtensionViewModel.Outcome
    func choose(_ name: String, _ value: String, action: ExtensionAction) async -> ExtensionViewModel.Outcome
    func filter(_ value: String) async -> ExtensionViewModel.Outcome
    func search() async -> ExtensionViewModel.Outcome
    func more(_ more: ExtensionMore) async -> ExtensionViewModel.Outcome
}

extension ExtensionViewControlling {
    var fieldsEnabled: Bool { true }
    var managesConfirmation: Bool { false }
    func isEnabled(_ action: ExtensionAction) -> Bool { running == nil }
    func selected(_ action: String) -> Bool { false }
    func prepared(_ action: ExtensionAction, with chosen: [String: String] = [:], displayed: ExtensionDisplayedRevision? = nil) -> @MainActor () async -> ExtensionViewModel.Outcome {
        { await self.perform(action, with: chosen) }
    }
    func perform(_ action: ExtensionAction) async -> ExtensionViewModel.Outcome { await perform(action, with: [:]) }
}

extension ExtensionViewModel: ExtensionViewControlling {}
