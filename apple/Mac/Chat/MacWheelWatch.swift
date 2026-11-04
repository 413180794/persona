import AppKit

/// The user's own scrolling of the chat on the Mac, from the scroll events themselves. SwiftUI's scroll phase went idle
/// the moment the fingers left the trackpad, while the glide's events kept coming: the chat counted the user's
/// scrolling as over, and the next streamed words took the list back to the newest under the glide (2026-11-04,
/// `MacScrollLab`). Fingers on the trackpad, their glide, and a mouse wheel turned in the last half second are the
/// user scrolling; `scrolling(false)` once all of that is over. Over the chat's list only.
@MainActor final class MacWheelWatch {
    private var monitor: Any?
    private var active = false
    private var quiet: DispatchWorkItem?

    #if DEBUG
    /// `MacScrollLab` hands its events here too: sent straight to the list (a locked Mac delivers no posted events),
    /// they pass no monitor.
    static var feed: ((NSEvent) -> Void)?
    #endif

    func start(list: @escaping () -> NSScrollView?, scrolling: @escaping (Bool) -> Void) {
        guard monitor == nil else { return }
        #if DEBUG
        Self.feed = { [weak self] e in self?.saw(e, scrolling) }
        #endif
        monitor = NSEvent.addLocalMonitorForEvents(matching: .scrollWheel) { [weak self] e in
            guard let self, let l = list(), let w = l.window, e.window === w,
                  l.convert(l.bounds, to: nil).contains(e.locationInWindow) else { return e }
            self.saw(e, scrolling)
            return e
        }
    }

    func stop() {
        if let monitor { NSEvent.removeMonitor(monitor) }
        monitor = nil
        quiet?.cancel()
    }

    private func saw(_ e: NSEvent, _ scrolling: @escaping (Bool) -> Void) {
        let fingers = e.phase, glide = e.momentumPhase
        // Every event says so again: SwiftUI may have ended it meanwhile.
        if !fingers.isEmpty || !glide.isEmpty || e.scrollingDeltaY != 0 {
            active = true
            scrolling(true)
        }
        quiet?.cancel()
        // A glide that ends is the end; fingers lifted may be followed by a glide within a few frames; a wheel has no
        // phases at all.
        let wait: Double = glide.contains(.ended) || glide.contains(.cancelled) ? 0.05 : (fingers.contains(.ended) ? 0.15 : 0.5)
        let item = DispatchWorkItem { [weak self] in
            guard let self, self.active else { return }
            self.active = false
            scrolling(false)
        }
        quiet = item
        DispatchQueue.main.asyncAfter(deadline: .now() + wait, execute: item)
    }
}
