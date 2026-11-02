#if DEBUG
import Foundation
import QuartzCore
#if os(macOS)
import AppKit
#endif

/// Simulator profiling (`-showAutoScroll YES`, ChatView): measures every frame while the chat list sweeps end to end
/// and writes the hitches (frames late by more than half a frame) to Documents/frames.json for scripts to read.
@MainActor
final class FrameStats: NSObject {
    static let shared = FrameStats()

    private var link: CADisplayLink?
    private var last: CFTimeInterval = 0
    private var expected: CFTimeInterval = 1.0 / 60
    private var deltas: [CFTimeInterval] = []
    /// Main-thread time between two frames (run loop wake → after Core Animation's commit), in ms.
    private var busies: [Double] = []
    private var busy: CFTimeInterval = 0
    private var passStart: CFTimeInterval?
    private var observers: [CFRunLoopObserver] = []

    private var bodiesAtStart: [String: Int] = [:]
    private var pushAtStart = (passes: 0, ms: 0.0)

    func start() {
        bodiesAtStart = PerfCount.bodies
        pushAtStart = (PerfCount.passes, PerfCount.totalMS)
        deltas = []
        busies = []
        busy = 0
        last = 0
        let wake = CFRunLoopObserverCreateWithHandler(nil, CFRunLoopActivity.afterWaiting.rawValue, true, CFIndex.min) { _, _ in
            MainActor.assumeIsolated { FrameStats.shared.passStart = CACurrentMediaTime() }
        }
        let sleep = CFRunLoopObserverCreateWithHandler(nil, CFRunLoopActivity.beforeWaiting.rawValue, true, CFIndex.max) { _, _ in
            MainActor.assumeIsolated {
                let s = FrameStats.shared
                if let t = s.passStart { s.busy += CACurrentMediaTime() - t }
                s.passStart = nil
            }
        }
        for o in [wake, sleep] { CFRunLoopAddObserver(CFRunLoopGetMain(), o, .commonModes) }
        observers = [wake, sleep].compactMap { $0 }
        #if os(iOS)
        let link = CADisplayLink(target: self, selector: #selector(tick(_:)))
        #else
        // A Mac's display link comes from the screen it draws on.
        guard let link = NSScreen.main?.displayLink(target: self, selector: #selector(tick(_:))) else { return }
        #endif
        link.add(to: .main, forMode: .common)
        self.link = link
    }

    @objc private func tick(_ link: CADisplayLink) {
        if last > 0 { deltas.append(link.timestamp - last); busies.append(busy * 1000) }
        busy = 0
        expected = max(link.targetTimestamp - link.timestamp, 1.0 / 120)
        last = link.timestamp
    }

    func stop(label: String) {
        link?.invalidate()
        link = nil
        for o in observers { CFRunLoopRemoveObserver(CFRunLoopGetMain(), o, .commonModes) }
        observers = []
        let busy = busies.sorted()
        func bpct(_ p: Double) -> Double { busy.isEmpty ? 0 : busy[min(busy.count - 1, Int(Double(busy.count) * p))] }
        let total = deltas.reduce(0, +)
        let late = deltas.filter { $0 > expected * 1.5 }
        let hitchTime = late.reduce(0) { $0 + ($1 - expected) }
        let sorted = deltas.sorted()
        func pct(_ p: Double) -> Double { sorted.isEmpty ? 0 : sorted[min(sorted.count - 1, Int(Double(sorted.count) * p))] * 1000 }
        let result: [String: Any] = [
            "label": label,
            "frames": deltas.count,
            "seconds": total,
            "hitches": late.count,
            "hitch_ms_per_s": total > 0 ? hitchTime * 1000 / total : 0,
            "p50_ms": pct(0.5), "p95_ms": pct(0.95), "p99_ms": pct(0.99),
            "max_ms": (sorted.last ?? 0) * 1000,
            "busy_p50_ms": bpct(0.5), "busy_p95_ms": bpct(0.95), "busy_max_ms": busy.last ?? 0,
            // When the long frames came (seconds from the start) and how long they were (ms).
            "long": zip(deltas.indices, deltas).filter { $0.1 > 0.05 }.map { i, d in
                [Double(deltas[..<i].reduce(0, +)), d * 1000] },
            // View bodies evaluated meanwhile (with `-perfCounts YES`).
            "bodies": PerfCount.bodies.compactMapValues { $0 }.reduce(into: [String: Int]()) { r, kv in
                let n = kv.value - (bodiesAtStart[kv.key] ?? 0)
                if n > 0 { r[kv.key] = n } },
            // Hub pushes applied meanwhile, and the main thread's time for them (until the run loop went idle).
            "push_passes": PerfCount.passes - pushAtStart.passes, "push_ms": PerfCount.totalMS - pushAtStart.ms,
            "busy_over_8ms": busy.filter { $0 > 8 }.count, "busy_over_16ms": busy.filter { $0 > 16 }.count,
        ]
        if let dir = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first,
           let data = try? JSONSerialization.data(withJSONObject: result, options: [.prettyPrinted, .sortedKeys]) {
            try? data.write(to: dir.appending(path: "frames.json"))
            try? data.write(to: dir.appending(path: "frames-\(label).json"))
        }
    }
}

#if os(iOS)
import UIKit

/// Any page, measured (`-showPagePerf YES`, docs/testing.md「整个 App」): 10 s after launch, 8 s on the page as it is
/// (a script pushes hub updates meanwhile), then the page's main scroll view (the largest one on screen) glides
/// down for 3 s and back up for 3 s at 2000 pt/s. Writes Documents/frames-page-idle.json and frames-page-glide.json.
@MainActor enum PagePerf {
    static func runIfAsked() {
        guard UserDefaults.standard.bool(forKey: "showPagePerf") else { return }
        Task { @MainActor in
            try? await Task.sleep(for: .seconds(10))
            FrameStats.shared.start()
            // For the script: push now (Documents/page-idle-started).
            if let dir = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first {
                try? Data().write(to: dir.appending(path: "page-idle-started"))
            }
            try? await Task.sleep(for: .seconds(8))
            FrameStats.shared.stop(label: "page-idle")
            try? await Task.sleep(for: .seconds(1))
            guard let list = mainScrollView() else { return }
            list.setContentOffset(CGPoint(x: list.contentOffset.x, y: -list.adjustedContentInset.top), animated: false)
            try? await Task.sleep(for: .seconds(1))
            FrameStats.shared.start()
            await ListGlide.run(list, seconds: 3, speed: 2000)
            await ListGlide.run(list, seconds: 3, speed: -2000)
            FrameStats.shared.stop(label: "page-glide")
        }
    }

    /// The tallest-content scroll view that is on screen and taller than half the window.
    private static func mainScrollView() -> UIScrollView? {
        guard let window = UIApplication.shared.connectedScenes.compactMap({ $0 as? UIWindowScene })
            .flatMap(\.windows).first(where: \.isKeyWindow) else { return nil }
        var best: UIScrollView?
        func visit(_ v: UIView) {
            if v.isHidden || v.alpha < 0.01 { return }
            if let s = v as? UIScrollView, s.isScrollEnabled, s.bounds.height > window.bounds.height / 2,
               s.contentSize.height > s.bounds.height,
               s.convert(s.bounds, to: window).intersects(window.bounds),
               s.contentSize.height > (best?.contentSize.height ?? 0) {
                best = s
            }
            for sub in v.subviews { visit(sub) }
        }
        visit(window)
        return best
    }
}

/// Moves a scroll view at a steady speed, one step per display frame (`-showAutoScroll`'s glide).
@MainActor final class ListGlide: NSObject {
    private var done: CheckedContinuation<Void, Never>?
    private var list: UIScrollView?
    private var speed: CGFloat = 0
    private var until: CFTimeInterval = 0
    private var last: CFTimeInterval = 0
    private var link: CADisplayLink?

    static func run(_ list: UIScrollView, seconds: Double, speed: CGFloat) async {
        let g = ListGlide()
        await withCheckedContinuation { c in
            g.done = c
            g.list = list
            g.speed = speed
            g.until = CACurrentMediaTime() + seconds
            let l = CADisplayLink(target: g, selector: #selector(tick(_:)))
            l.add(to: .main, forMode: .common)
            g.link = l
        }
    }

    @objc private func tick(_ l: CADisplayLink) {
        guard let list else { return }
        if last > 0 {
            let maxY = max(0, list.contentSize.height - list.bounds.height + list.adjustedContentInset.bottom)
            let y = min(maxY, max(-list.adjustedContentInset.top, list.contentOffset.y + speed * (l.timestamp - last)))
            list.contentOffset.y = y
        }
        last = l.timestamp
        if l.timestamp >= until {
            l.invalidate()
            done?.resume()
            done = nil
        }
    }
}
#endif
#endif
