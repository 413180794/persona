#if DEBUG
import QuartzCore
#if os(iOS)
import UIKit
typealias TracedScroll = UIScrollView
#else
import AppKit
typealias TracedScroll = NSScrollView
#endif

/// `-scrollTrace YES`: one line per display frame about the chat list, written to a file (`url`), to tell
/// why the chat moved while a reply streamed (StreamingScrollTests on iOS, MacScrollLab on the Mac; a recording says
/// whether it did). The numbers are UIKit's / AppKit's own, read on the frame: SwiftUI's geometry can lag a move made
/// from inside its callbacks (#1046). Columns: wall clock (s), offset, content height, inset top, inset bottom, a finger
/// on it (tracking / a trackpad gesture), gliding (decelerating / momentum), then the chat's own state (`ChatView`).
/// On the Mac a timer at 120 Hz stands in for the display link, which does not tick while the screen is locked.
@MainActor
final class ScrollTrace: NSObject {
    static let enabled = UserDefaults.standard.bool(forKey: "scrollTrace")
    static let shared = ScrollTrace()

    private var list: () -> TracedScroll? = { nil }
    private var state: () -> String = { "" }
    private var lines: [String] = []
    private var lastLine = ""
    private var lastFlush: CFTimeInterval = 0
    private var started = false
    /// iOS: the app's Documents. The Mac app is not sandboxed: its temporary directory, or `-scrollTracePath`.
    #if os(iOS)
    private let url = URL.documentsDirectory.appending(path: "scroll-trace.txt")
    #else
    private let url = URL(fileURLWithPath: UserDefaults.standard.string(forKey: "scrollTracePath")
        ?? NSTemporaryDirectory() + "persona-scroll-trace.txt")
    #endif
    #if os(macOS)
    /// The trackpad's phases, as the scroll view last received them (`MacScrollLab` and real gestures alike).
    var gesture = false
    var momentum = false
    #endif

    /// The chat's list being traced (`MacScrollLab` scrolls this one).
    var currentList: TracedScroll? { list() }

    func start(list: @escaping () -> TracedScroll?, state: @escaping () -> String) {
        self.list = list
        self.state = state
        guard !started else { return }
        started = true
        try? Data().write(to: url)
        #if os(iOS)
        let link = CADisplayLink(target: self, selector: #selector(tick))
        link.add(to: .main, forMode: .common)
        #else
        let timer = Timer(timeInterval: 1.0 / 120, repeats: true) { _ in MainActor.assumeIsolated { ScrollTrace.shared.tick() } }
        RunLoop.main.add(timer, forMode: .common)
        #endif
    }

    @objc private func tick() {
        guard let l = list() else { return }
        #if os(iOS)
        let i = l.adjustedContentInset
        let (offset, height, top, bottom) = (l.contentOffset.y, l.contentSize.height, i.top, i.bottom)
        let (finger, glide) = (l.isTracking, l.isDecelerating)
        #else
        let i = l.contentInsets
        let (offset, height, top, bottom) = (l.contentView.bounds.origin.y, l.documentView?.frame.height ?? 0, i.top, i.bottom)
        let (finger, glide) = (gesture, momentum)
        #endif
        let body = String(format: "%.1f %.1f %.1f %.1f %d %d ", offset, height, top, bottom, finger ? 1 : 0, glide ? 1 : 0) + state()
        // Only frames where something changed: a minute at rest is one line.
        if body != lastLine {
            lines.append(String(format: "%.4f ", Date.now.timeIntervalSince1970) + body)
            lastLine = body
        }
        let now = CACurrentMediaTime()
        if now - lastFlush > 1, !lines.isEmpty {
            lastFlush = now
            let text = lines.joined(separator: "\n") + "\n"
            lines = []
            if let h = try? FileHandle(forWritingTo: url) {
                h.seekToEndOfFile()
                h.write(Data(text.utf8))
                try? h.close()
            }
        }
    }
}
#endif
