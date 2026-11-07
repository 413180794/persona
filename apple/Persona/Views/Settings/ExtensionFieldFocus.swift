import SwiftUI

nonisolated struct ExtensionDisplayedRevision: Equatable, Sendable {
    let session: String
    let epoch: String
    let version: Int64
}

extension EnvironmentValues {
    @Entry var extensionDisplayedRevision: ExtensionDisplayedRevision? = nil
    /// The page owns focus, not a row's position in a section. Keyed moves can rebind the same
    /// native field without inventing another draft or transferring focus to a different callback.
    @Entry var extensionFieldFocus: FocusState<String?>.Binding? = nil
}

struct ExtensionStreamProbe: ViewModifier {
    let stream: ExtensionPageStream?
    var lifecycle = ""
    func body(content: Content) -> some View {
        #if DEBUG
        content.accessibilityValue(stream?.supported == true ? (stream?.ready == true ? "stream:\(stream?.tree?.version ?? 0)" :
            "stream:0:rendered=\(stream?.tree?.version ?? 0):visible=\(stream?.visible == true):recovery=\(stream?.recoveryIssue ?? "none"):\(lifecycle)") : "legacy")
        #else
        content
        #endif
    }
}

struct ExtensionFieldFocus: ViewModifier {
    let binding: FocusState<String?>.Binding?
    let key: String
    @ViewBuilder func body(content: Content) -> some View {
        if let binding { content.focused(binding, equals: key) }
        else { content }
    }
}
