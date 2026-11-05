import SwiftUI

/// Assistant-only execution context. Extension pages never receive this environment and do not run HTML.
struct AssistantHTMLRenderingContext {
    var version: Int64
    var fallback: String?
    var answered: Bool
    var enabled: Bool
    var bridgeAvailable: Bool
    var onRequest: (HTMLCardRequest, String, Int64) async -> Bool
}

extension EnvironmentValues {
    @Entry var assistantHTMLRendering: AssistantHTMLRenderingContext? = nil
}
