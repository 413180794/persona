import SwiftUI

/// A picture attached to the composer, as the web's attachment tray draws it (web/src/ui/AttachmentTray.tsx): the
/// picture alone, and a small dark × inside its top-right corner that shows while the pointer is over the picture or
/// the × has keyboard focus (the web's `group-hover` / `focus-visible`). Clicking the picture edits it, its tooltip
/// says so (the web's title), no pencil drawn over it.
struct MacComposerThumb: View {
    var image: UIImage
    var index: Int
    var editable: Bool
    var edit: () -> Void
    var remove: () -> Void

    @State private var hovering = false
    @FocusState private var removeFocused: Bool

    var body: some View {
        picture
            .overlay(alignment: .topTrailing) {
                // `.plain`: an unstyled Button is a bordered push button on macOS, the white and gray plate of #2543.
                Button(action: remove) {
                    Image(systemName: Theme.Icon.close)
                        .font(Theme.Font.labelHeavy)
                        .foregroundStyle(.white)
                        .scaledFrame(width: Theme.Size.badge, height: Theme.Size.badge)
                        .background(.black.opacity(0.6), in: .circle)
                        .contentShape(.circle)
                }
                .buttonStyle(.plain)
                .focused($removeFocused)
                .padding(Theme.Space.xxs)
                .opacity(shown ? 1 : 0)
                .help("Remove [Task Sheets 6135d4]")
                .accessibilityLabel("Remove Image \(index + 1)")
            }
            .onHover { inside in withAnimation(Theme.Motion.standard) { hovering = inside } }
    }

    private var shown: Bool { hovering || removeFocused || MacHover.showAll }

    /// A button when it can be edited (Tab and Space reach it, as the web's `<button>`), the picture alone otherwise.
    @ViewBuilder private var picture: some View {
        if editable {
            Button(action: edit) { thumb }
                .buttonStyle(.plain)
                .help("Click to mark up before sending")
                .accessibilityLabel("Edit Image \(index + 1)")
        } else {
            thumb.accessibilityLabel("Image \(index + 1) [Composer Parts 0d2e3a]")
        }
    }

    private var thumb: some View {
        Image(uiImage: image).resizable().scaledToFill()
            .scaledFrame(width: Theme.Size.box56, height: Theme.Size.box56)
            .clipShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
            .contentShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
    }
}
