#if DEBUG && (os(iOS) || os(visionOS))
import PersonaKit
import UIKit

extension RuntimeCLI {
    static let imageViewerCommands: [String: Command] = [
        "viewerPaging": { _ in ImagePagingProbe.shared.snapshot() },
        "viewerPagingArmCancel": { args in ImagePagingProbe.shared.armCancel(args["fraction"] as? Double ?? 0.25) },
    ]
}

/// Test-only observations of the actual UIKit pager. Never sets a recognizer's state or calls its targets manually.
/// Disabling an already-active recognizer asks UIKit to cancel it; the resulting `.cancelled` callback is recorded.
/// The decode gate opens on an actual `.changed` pan, not after a timer, and observes completion and page update.
@MainActor
final class ImagePagingProbe {
    static let shared = ImagePagingProbe()
    private let enabled = UserDefaults.standard.bool(forKey: "showViewerPagingProbe")
    private let gateDecode = UserDefaults.standard.bool(forKey: "showViewerDecodeDuringPan")
    private weak var pager: PagerScrollView?
    private var events: [[String: Any]] = []
    private var gesture = 0
    private var gestureStart: CGFloat = 0
    private var cancelFraction: CGFloat?
    private var waiting: CheckedContinuation<Void, Never>?
    private var gateOpened = false
    private var waitingPage: Int?

    func attach(_ pager: PagerScrollView) {
        guard enabled else { return }
        self.pager = pager
    }

    func armCancel(_ fraction: Double) -> [String: Any] {
        guard enabled, let pager, fraction > 0, fraction < 1 else { return ["ok": false, "error": "no pager or invalid fraction"] }
        pager.panGestureRecognizer.isEnabled = true
        cancelFraction = CGFloat(fraction)
        return ["armed": true, "gesture": gesture + 1]
    }

    func pan(_ recognizer: UIPanGestureRecognizer) {
        guard enabled, let pager else { return }
        if recognizer.state == .began {
            gesture += 1
            gestureStart = pager.contentOffset.x
        }
        record("pan")
        if recognizer.state == .changed, pager.isDragging {
            if !gateOpened, waiting != nil {
                gateOpened = true
                record("decode-gate-open", page: waitingPage)
                let continuation = waiting
                waiting = nil
                continuation?.resume()
            }
            if let fraction = cancelFraction, abs(pager.contentOffset.x - gestureStart) >= pager.bounds.width * fraction {
                cancelFraction = nil
                record("cancel-request")
                // UIKit transitions a real began/changed recognizer to cancelled. Do not fabricate events.
                recognizer.isEnabled = false
            }
        }
    }

    /// The next portrait fixture is held before entering the real decoder. Thumbnail decoding is a separate path.
    func beforeDecode(_ page: Int) async {
        guard enabled, gateDecode, page == 2, !gateOpened else { return }
        waitingPage = page
        record("decode-wait", page: page)
        await withCheckedContinuation { waiting = $0 }
        record("decode-start", page: page)
    }

    func decoded(_ page: Int, image: UIImage?) {
        guard enabled, gateDecode, page == 2 else { return }
        record(image == nil ? "decode-failed" : "decode-complete", page: page)
    }

    func updated(_ incoming: [Int: UIImage], previous: [Int: UIImage]) {
        guard enabled, gateDecode else { return }
        for (page, image) in incoming where previous[page] !== image { record("image-update", page: page) }
    }

    func terminalAction() {
        guard enabled else { return }
        record("settle-request")
    }

    func settled(_ page: Int) {
        guard enabled else { return }
        record("settled", page: page)
    }

    private func record(_ event: String, page: Int? = nil) {
        guard let pager else { return }
        var row: [String: Any] = [
            "sequence": events.count, "gesture": gesture, "event": event,
            "state": Self.state(pager.panGestureRecognizer.state), "dragging": pager.isDragging,
            "tracking": pager.isTracking, "decelerating": pager.isDecelerating,
            "offset": Double(pager.contentOffset.x), "width": Double(pager.bounds.width),
        ]
        if let page { row["page"] = page }
        events.append(row)
    }

    func snapshot() -> [String: Any] {
        guard enabled, let pager else { return ["ok": false, "error": "viewer paging probe disabled or no viewer"] }
        let nearest = PagerMath.nearestPage(offset: pager.contentOffset.x, width: pager.bounds.width, count: pager.testPageCount)
        let offset = PagerMath.offset(page: nearest, width: pager.bounds.width, count: pager.testPageCount)
        return [
            "events": events, "gesture": gesture, "offset": Double(pager.contentOffset.x),
            "alignmentError": Double(abs(pager.contentOffset.x - offset)), "page": nearest,
            "dragging": pager.isDragging, "decelerating": pager.isDecelerating,
            "state": Self.state(pager.panGestureRecognizer.state),
            "appearance": pager.traitCollection.userInterfaceStyle == .dark ? "dark" : "light",
            "decodeWaiting": waiting != nil,
        ]
    }

    private static func state(_ state: UIGestureRecognizer.State) -> String {
        switch state {
        case .possible: "possible"
        case .began: "began"
        case .changed: "changed"
        case .ended: "ended"
        case .cancelled: "cancelled"
        case .failed: "failed"
        @unknown default: "unknown"
        }
    }
}
#endif
