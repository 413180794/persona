import SwiftUI
import UniformTypeIdentifiers

/// Files dragged onto a composer (docs/mac.md「细节」): pictures go into the message as images (as the + menu's 照片
/// does), everything else is uploaded to the session's machine and its path typed in (as 文件 does).
nonisolated enum MacDrop {
    /// The + menu's photo limit per message.
    static let maxImages = 4

    /// Splits dropped URLs into pictures to attach and files to upload. `already` is how many pictures the composer
    /// holds; pictures past the limit are uploaded as files instead of dropped. Non-file URLs (a dragged web link) are ignored;
    /// folders are counted, not uploaded (an upload is one file).
    static func split(_ urls: [URL], already: Int = 0) -> (images: [URL], files: [URL], folders: Int) {
        var images: [URL] = []
        var files: [URL] = []
        var folders = 0
        for url in urls where url.isFileURL {
            if url.hasDirectoryPath || (try? url.resourceValues(forKeys: [.isDirectoryKey]).isDirectory) == true {
                folders += 1
            } else if isPicture(url), already + images.count < maxImages {
                images.append(url)
            } else {
                files.append(url)
            }
        }
        return (images, files, folders)
    }

    static func isPicture(_ url: URL) -> Bool {
        guard !url.pathExtension.isEmpty, let type = UTType(filenameExtension: url.pathExtension) else { return false }
        return type.conforms(to: .image)
    }
}

/// What ⌘V in a composer does with the pasteboard.
nonisolated enum MacPaste {
    enum Plan: Equatable {
        case text
        case image
        case files([URL])
    }

    /// Files first (Finder's ⌘C also puts each file's icon on the pasteboard as an image), then a picture, else text.
    static func plan(fileURLs: [URL], hasImage: Bool) -> Plan {
        if !fileURLs.isEmpty { return .files(fileURLs) }
        return hasImage ? .image : .text
    }

    @MainActor static func current(_ board: NSPasteboard = .general) -> Plan {
        let urls = board.readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL] ?? []
        return plan(fileURLs: urls, hasImage: NSImage.canInit(with: board))
    }
}

/// ⌘V while this composer has focus: files and pictures are taken here; text goes on to the field as usual. A key
/// monitor, because the focused SwiftUI text field takes `paste:` itself and drops pictures. Only the composer in
/// the key window (a conversation can also be open in a window of its own) acts.
struct MacComposerPaste: ViewModifier {
    var focused: Bool
    var image: (NSImage) -> Void
    var files: ([URL]) -> Void
    @State private var window = WindowRef()
    @State private var monitor: Any?

    /// What the monitor reads when a key comes in: refreshed on every update, so it never acts on a stale focus.
    final class WindowRef {
        weak var window: NSWindow?
        var focused = false
        var image: (NSImage) -> Void = { _ in }
        var files: ([URL]) -> Void = { _ in }
    }

    func body(content: Content) -> some View {
        window.focused = focused
        window.image = image
        window.files = files
        return content
            .background(WindowReader(ref: window))
            .onAppear { install() }
            .onDisappear {
                if let monitor { NSEvent.removeMonitor(monitor) }
                monitor = nil
            }
    }

    private func install() {
        guard monitor == nil else { return }
        let ref = window
        monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
            guard ref.focused, event.modifierFlags.intersection(.deviceIndependentFlagsMask) == .command,
                  event.charactersIgnoringModifiers == "v", let w = ref.window, event.window === w else { return event }
            switch MacPaste.current() {
            case .text: return event
            case .image:
                guard let img = NSImage(pasteboard: .general) else { return event }
                ref.image(img)
                return nil
            case .files(let urls):
                ref.files(urls)
                return nil
            }
        }
    }

    private struct WindowReader: NSViewRepresentable {
        let ref: WindowRef
        func makeNSView(context: Context) -> NSView { Probe(ref: ref) }
        func updateNSView(_ nsView: NSView, context: Context) {}

        final class Probe: NSView {
            let ref: WindowRef
            init(ref: WindowRef) {
                self.ref = ref
                super.init(frame: .zero)
            }
            required init?(coder: NSCoder) { nil }
            override func viewDidMoveToWindow() {
                super.viewDidMoveToWindow()
                ref.window = window
            }
        }
    }
}
