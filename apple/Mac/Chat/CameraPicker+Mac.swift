import SwiftUI

/// The composer's Take Photo is an iPhone's: on a Mac there is no camera to take one with (`isAvailable` is false),
/// so this is never presented. Images come from files, the pasteboard or a drop instead.
struct CameraPicker: View {
    static var isAvailable: Bool { false }
    var onImage: (UIImage) -> Void
    var body: some View { EmptyView() }
}
