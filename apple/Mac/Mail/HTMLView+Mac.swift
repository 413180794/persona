import AppKit
import SwiftUI
import WebKit

/// Whether a fresh measurement should become the mail web view's frame height. Pure, so the jitter guard is testable
/// without a WKWebView: the document gets `padding` points of slack and measurements within `threshold` points of
/// the current height are rounding noise.
nonisolated enum FittedHeight {
    /// The new frame height, or nil when `measured + padding` is within `threshold` of `current`.
    static func fittedHeight(measured: CGFloat, current: CGFloat, padding: CGFloat = 20, threshold: CGFloat = 1) -> CGFloat? {
        let fitted = measured + padding
        return abs(fitted - current) > threshold ? fitted : nil
    }

    static func shouldUpdate(from current: CGFloat, to measured: CGFloat, padding: CGFloat = 20, threshold: CGFloat = 1) -> Bool {
        fittedHeight(measured: measured, current: current, padding: padding, threshold: threshold) != nil
    }
}

/// A mail's HTML body (iOS: `Views/Mail/MailViews.swift`): a non-scrolling web view as tall as its page, scripts
/// off, links opened in the browser.
struct HTMLView: NSViewRepresentable {
    var html: String
    @Binding var height: CGFloat

    func makeCoordinator() -> Coordinator { Coordinator(height: $height) }

    func makeNSView(context: Context) -> FittedWebView {
        let config = WKWebViewConfiguration()
        config.defaultWebpagePreferences.allowsContentJavaScript = false
        let v = FittedWebView(frame: .zero, configuration: config)
        v.setValue(false, forKey: "drawsBackground") // transparent, over the page's own background
        v.navigationDelegate = context.coordinator
        context.coordinator.attach(to: v)
        return v
    }

    func updateNSView(_ v: FittedWebView, context: Context) {
        let page = """
        <html><head><meta name="viewport" content="width=device-width, initial-scale=1">
        <style>body{font:-apple-system-body;margin:0;color:CanvasText;background:transparent;word-wrap:break-word}
        img{max-width:100%;height:auto} table{max-width:100%}
        @media (prefers-color-scheme: dark){body{color:#eee} a{color:#7ab}}</style></head><body>\(html)</body></html>
        """
        if context.coordinator.loaded != html {
            context.coordinator.loaded = html
            v.loadHTMLString(page, baseURL: nil)
        }
    }

    /// Its height is the page's: the scroll wheel goes to the mail around it.
    final class FittedWebView: WKWebView {
        override func scrollWheel(with event: NSEvent) { nextResponder?.scrollWheel(with: event) }
    }

    final class Coordinator: NSObject, WKNavigationDelegate {
        var height: Binding<CGFloat>
        var loaded: String?
        private var didAttach = false
        private var didFinishLoad = false
        private var progressObservation: NSKeyValueObservation?
        // JavaScript completions can arrive out of order; a stale, smaller height must not overwrite a newer one.
        private var nextMeasurementID = 0
        private var appliedMeasurementID = -1

        init(height: Binding<CGFloat>) { self.height = height }

        deinit {
            progressObservation?.invalidate()
        }

        /// Registers the height observer once per web view (never from `updateNSView`): loading new HTML keeps it.
        @MainActor
        func attach(to webView: WKWebView) {
            guard !didAttach else { return }
            didAttach = true

            // Remote images keep the load going after the first layout ticks; estimatedProgress is public on macOS
            // and re-fires as their bytes arrive. (WKWebView.scrollView is iOS-only; macOS has no contentSize KVO.)
            progressObservation = webView.observe(\.estimatedProgress, options: [.new]) { [weak self, weak webView] _, _ in
                Task { @MainActor in
                    guard let self, let webView, self.didFinishLoad else { return }
                    self.refit(webView)
                }
            }
        }

        func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
            didFinishLoad = true
            refit(webView) // immediate first measurement
            schedulePostLoadMeasurements(for: webView)
        }

        /// Remote images gain their intrinsic height only once headers/decode land, which can lag `didFinish`; with
        /// content JavaScript off the page cannot call back. A short, finite ladder of remeasurements covers it, and
        /// every one goes through `FittedHeight`, so an unchanged page only does a cheap height read.
        private static let postLoadDelays: [Duration] = [.milliseconds(200), .milliseconds(600), .milliseconds(1200), .seconds(2)]

        @MainActor
        private func schedulePostLoadMeasurements(for webView: WKWebView) {
            for delay in Self.postLoadDelays {
                Task { @MainActor [weak self, weak webView] in
                    try? await Task.sleep(for: delay)
                    guard let self, let webView else { return }
                    self.refit(webView)
                }
            }
        }

        /// Reads the document's real height (`document.body.scrollHeight`, the source of truth) and only changes the
        /// binding when the page actually grew or shrank past the jitter threshold.
        @MainActor
        private func refit(_ webView: WKWebView) {
            let measurementID = nextMeasurementID
            nextMeasurementID += 1
            webView.evaluateJavaScript("document.body.scrollHeight") { [weak self] value, _ in
                guard let self, let measured = (value as? CGFloat).flatMap({ $0 > 0 ? $0 : nil }) else { return }
                Task { @MainActor in
                    guard measurementID >= self.appliedMeasurementID else { return }
                    guard let fitted = FittedHeight.fittedHeight(measured: measured, current: self.height.wrappedValue) else { return }
                    self.appliedMeasurementID = measurementID
                    self.height.wrappedValue = fitted
                }
            }
        }

        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction) async -> WKNavigationActionPolicy {
            if action.navigationType == .linkActivated, let url = action.request.url {
                await MainActor.run { LinkPolicy.open(url) } // NSWorkspace would run any scheme it names
                return .cancel
            }
            return .allow
        }
    }
}
