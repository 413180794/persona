import AppKit
import SwiftUI
import WebKit

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
        init(height: Binding<CGFloat>) { self.height = height }

        func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
            webView.evaluateJavaScript("document.body.scrollHeight") { value, _ in
                if let h = value as? CGFloat { Task { @MainActor in self.height.wrappedValue = h + 20 } }
            }
        }

        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction) async -> WKNavigationActionPolicy {
            if action.navigationType == .linkActivated, let url = action.request.url {
                NSWorkspace.shared.open(url)
                return .cancel
            }
            return .allow
        }
    }
}
