import SwiftUI

/// The composer's 拍照 is an iPhone's: on a Mac `UIImagePickerController.isSourceTypeAvailable(.camera)` is false,
/// so this is never presented. Images come from files, the pasteboard or a drop instead.
struct CameraPicker: View {
    var onImage: (UIImage) -> Void
    var body: some View { EmptyView() }
}
