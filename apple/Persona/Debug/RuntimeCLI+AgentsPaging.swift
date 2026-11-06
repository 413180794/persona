#if DEBUG && (os(iOS) || os(visionOS))
import UIKit

extension RuntimeCLI {
    /// Controlled local UI measurements. CADisplayLink intervals measure main-thread delivery, not GPU presentation.
    static let agentsPagingCommands: [String: Command] = [
        "agents-list-window": { _ in
            let lists = AppModel.shared.sessionLists
            return ["loaded": lists.sessions.count, "total": lists.total, "hasMore": lists.hasMore, "loading": lists.loading,
                    "lists": lists.lists.mapValues { ["loaded": $0.rows.count, "total": $0.total, "paged": $0.paged,
                                                       "revision": $0.revision ?? "", "error": $0.error ?? ""] as [String: Any] }]
        },
        "agents-list-glide": { args in
            guard let scroll = agentsScrollView() else { return fail("No visible Agents list") }
            let feed = AgentsFeed.pagingProbe
            let moving = feed?.reader.moving
            feed?.reader.cancel()
            feed?.reader.moving = { true }
            feed?.hold.set(.scrolling, true)
            defer {
                feed?.reader.cancel()
                if let moving { feed?.reader.moving = moving }
                feed?.hold.set(.scrolling, false)
            }
            let sampler = AgentsGlideSampler(scroll: scroll, duration: min(15, max(1, args["seconds"] as? Double ?? 6)),
                                             velocity: args["velocity"] as? Double ?? 2000)
            return await sampler.run()
        },
    ]

    private static func agentsScrollView() -> UIScrollView? {
        var found: [UIScrollView] = []
        func visit(_ v: UIView) {
            if let scroll = v as? UIScrollView, scroll.window != nil, drawn(scroll), scroll.bounds.height > 200,
               scroll.contentSize.height > scroll.bounds.height { found.append(scroll) }
            v.subviews.forEach(visit)
        }
        windows.forEach(visit)
        return found.max { $0.bounds.width * $0.bounds.height < $1.bounds.width * $1.bounds.height }
    }
}

@MainActor private final class AgentsGlideSampler: NSObject {
    private weak var scroll: UIScrollView?
    private let duration: Double
    private var velocity: Double
    private var start: Double = 0
    private var previous: Double = 0
    private var intervals: [Double] = []
    private var link: CADisplayLink?
    private var completion: CheckedContinuation<RuntimeCLI.Reply, Never>?

    init(scroll: UIScrollView, duration: Double, velocity: Double) {
        self.scroll = scroll
        self.duration = duration
        self.velocity = velocity
    }

    func run() async -> RuntimeCLI.Reply {
        await withCheckedContinuation { continuation in
            completion = continuation
            start = CACurrentMediaTime()
            previous = start
            let link = CADisplayLink(target: self, selector: #selector(tick))
            link.preferredFrameRateRange = CAFrameRateRange(minimum: 60, maximum: 60, preferred: 60)
            link.add(to: .main, forMode: .common)
            self.link = link
        }
    }

    @objc private func tick(_ link: CADisplayLink) {
        let now = CACurrentMediaTime()
        let dt = now - previous
        previous = now
        intervals.append(dt)
        if now - start >= duration || scroll == nil {
            self.link?.invalidate()
            self.link = nil
            let elapsed = now - start
            let budget = 1.0 / 60
            let dropped = intervals.reduce(0) { $0 + max(0, Int(($1 / budget).rounded()) - 1) }
            let hitch = intervals.filter { $0 > budget * 1.5 }.reduce(0) { $0 + $1 - budget }
            completion?.resume(returning: ["seconds": elapsed, "frames": intervals.count, "sampledFPS": Double(intervals.count) / elapsed,
                                          "maxFrameMs": (intervals.max() ?? 0) * 1000, "dropped": dropped,
                                          "hitchMsPerSecond": hitch * 1000 / elapsed,
                                          "loaded": AppModel.shared.sessionLists.sessions.count, "total": AppModel.shared.sessionLists.total])
            completion = nil
            return
        }
        guard let scroll else { return }
        let top = -scroll.adjustedContentInset.top
        let bottom = max(top, scroll.contentSize.height + scroll.adjustedContentInset.bottom - scroll.bounds.height)
        var y = scroll.contentOffset.y + velocity * dt
        if y > bottom { y = bottom; velocity = -abs(velocity) }
        if y < top { y = top; velocity = abs(velocity) }
        scroll.setContentOffset(CGPoint(x: scroll.contentOffset.x, y: y), animated: false)
    }
}
#endif
