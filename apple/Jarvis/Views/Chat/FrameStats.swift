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

    func start() {
        deltas = []
        last = 0
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
        if last > 0 { deltas.append(link.timestamp - last) }
        expected = max(link.targetTimestamp - link.timestamp, 1.0 / 120)
        last = link.timestamp
    }

    func stop(label: String) {
        link?.invalidate()
        link = nil
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
        ]
        if let dir = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first,
           let data = try? JSONSerialization.data(withJSONObject: result, options: [.prettyPrinted, .sortedKeys]) {
            try? data.write(to: dir.appending(path: "frames.json"))
        }
    }
}
#endif
