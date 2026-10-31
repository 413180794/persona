#if DEBUG || PERF_SIGNPOST
import os
import QuartzCore
import SwiftUI
import UIKit

nonisolated enum PerfTrace {
    private static let log = OSLog(subsystem: "Jarvis.Performance", category: .pointsOfInterest)

    static func event(_ name: StaticString, _ detail: String = "") {
        os_signpost(.event, log: log, name: name, "%{public}@", detail as NSString)
    }

    /// A view's body was evaluated: a signpost (Instruments' Points of Interest) and a count (`PerfCount`).
    @MainActor static func body(_ name: StaticString) {
        os_signpost(.event, log: log, name: "Body", "%{public}@", "\(name)" as NSString)
        PerfCount.body("\(name)")
    }

    /// A hub push is being applied (`AppModel.handle`).
    @MainActor static func push(_ kind: String) {
        os_signpost(.event, log: log, name: "Push", "%{public}@", kind as NSString)
        PerfCount.pushArrived()
    }
}

/// What hub pushes cost, for `LiveUpdateTests` (launched with `-perfCounts YES`): view bodies evaluated, by view, and the
/// main thread's time from a push until the run loop goes idle again (SwiftUI's update and layout included; pushes
/// applied in the same pass of the run loop count once there). Read through the hidden `perf.counts` text.
@MainActor enum PerfCount {
    static let enabled = UserDefaults.standard.bool(forKey: "perfCounts")
    private(set) static var bodies: [String: Int] = [:]
    private(set) static var events = 0
    private(set) static var passes = 0
    private(set) static var totalMS = 0.0
    private(set) static var maxMS = 0.0
    private static var started: CFTimeInterval?

    static func body(_ name: String) { if enabled { bodies[name, default: 0] += 1 } }

    static func pushArrived() {
        guard enabled else { return }
        events += 1
        guard started == nil else { return }
        started = CACurrentMediaTime()
        // After Core Animation's commit (order 2,000,000), when the main run loop has nothing left to do.
        let observer = CFRunLoopObserverCreateWithHandler(nil, CFRunLoopActivity.beforeWaiting.rawValue, false, CFIndex.max) { _, _ in
            MainActor.assumeIsolated { finish() }
        }
        CFRunLoopAddObserver(CFRunLoopGetMain(), observer, .commonModes)
    }

    private static func finish() {
        guard let t0 = started else { return }
        let ms = (CACurrentMediaTime() - t0) * 1000
        started = nil
        passes += 1
        totalMS += ms
        maxMS = max(maxMS, ms)
    }

    static var summary: String {
        let b = bodies.sorted { $0.key < $1.key }.map { "\"\($0.key)\":\($0.value)" }.joined(separator: ",")
        return "{\"bodies\":{\(b)},\"events\":\(events),\"passes\":\(passes),\"ms\":\(String(format: "%.1f", totalMS)),\"max\":\(String(format: "%.1f", maxMS))}"
    }
}

/// The counts as a hidden text the UI tests read (`-perfCounts YES`).
struct PerfCountProbe: View {
    var body: some View {
        if PerfCount.enabled {
            TimelineView(.periodic(from: .now, by: 0.25)) { _ in
                Text(PerfCount.summary).font(Theme.Font.label).lineLimit(1).opacity(0.02).accessibilityIdentifier("perf.counts")
                    .allowsHitTesting(false)
            }
            .frame(width: Theme.Size.dotXS, height: Theme.Size.dotXS)
            .clipped()
        }
    }
}

@MainActor private final class PerfTabLifetime: ObservableObject {
    nonisolated let name: String
    nonisolated let token = UUID()

    init(_ name: String) {
        self.name = name
        PerfTrace.event("Tab lifetime start", "\(name) \(token)")
    }

    deinit { PerfTrace.event("Tab lifetime end", "\(name) \(token)") }
}

@MainActor struct PerfTabMount: ViewModifier {
    @StateObject private var lifetime: PerfTabLifetime
    @Environment(\.colorScheme) private var scheme
    @Environment(\.dynamicTypeSize) private var typeSize
    private let name: String

    init(_ name: String) {
        self.name = name
        _lifetime = StateObject(wrappedValue: PerfTabLifetime(name))
    }

    func body(content: Content) -> some View {
        let _ = PerfTrace.event("Tab mount body", "\(name) \(lifetime.token)")
        content
            .background { PerfWindowProbe(name: name).frame(width: 0, height: 0).allowsHitTesting(false).accessibilityHidden(true) }
            .onAppear { PerfTrace.event("Tab appear", "\(name) \(lifetime.token)") }
            .onDisappear { PerfTrace.event("Tab disappear", "\(name) \(lifetime.token)") }
            .onChange(of: scheme) { _, _ in PerfTrace.event("Tab color scheme", name) }
            .onChange(of: typeSize) { _, _ in PerfTrace.event("Tab type size", name) }
    }
}

@MainActor private struct PerfWindowProbe: UIViewRepresentable {
    let name: String
    func makeUIView(context: Context) -> PerfProbeView { PerfProbeView(name) }
    func updateUIView(_ view: PerfProbeView, context: Context) { PerfTrace.event("Tab UIView update", view.detail) }
}

@MainActor private final class PerfProbeView: UIView {
    nonisolated let name: String
    nonisolated let token = UUID()
    nonisolated var detail: String { "\(name) \(token)" }

    init(_ name: String) {
        self.name = name
        super.init(frame: .zero)
        PerfTrace.event("Tab UIView create", detail)
    }

    required init?(coder: NSCoder) { fatalError() }
    override func didMoveToWindow() {
        super.didMoveToWindow()
        PerfTrace.event("Tab window", "\(detail) attached=\(window != nil)")
    }
    override func traitCollectionDidChange(_ previousTraitCollection: UITraitCollection?) {
        super.traitCollectionDidChange(previousTraitCollection)
        PerfTrace.event("Tab UIKit traits", detail)
    }
    deinit { PerfTrace.event("Tab UIView destroy", detail) }
}
#endif
