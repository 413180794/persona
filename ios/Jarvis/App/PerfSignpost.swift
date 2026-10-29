#if PERF_SIGNPOST
import os
import SwiftUI
import UIKit

nonisolated enum PerfTrace {
    private static let log = OSLog(subsystem: "Jarvis.Performance", category: .pointsOfInterest)

    static func event(_ name: StaticString, _ detail: String = "") {
        os_signpost(.event, log: log, name: name, "%{public}@", detail as NSString)
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
