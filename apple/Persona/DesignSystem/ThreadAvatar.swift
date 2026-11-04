import SwiftUI

/// A person's or a chat's picture in a connector's conversation page (Telegram's chats, GitHub's threads): fetched
/// through the hub by its media ref (kept on disk by the app and the hub), the initial on the neutral disc without one
/// or while it cannot be fetched.
struct ThreadAvatar: View {
    var connector: String
    var name: String
    var ref: String?
    var isGroup = false
    var size: CGFloat = Theme.Size.wellM

    var body: some View {
        LarkAvatarImage(name: name, url: ref.flatMap { $0.isEmpty ? nil : ConnectorMedia.url(connector, ref: $0) }, isGroup: isGroup, size: size)
            .accessibilityHidden(true)
    }
}
