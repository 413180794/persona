import Foundation
import Network
import Testing
import AppKit
import WebKit
@testable import Sandbox

@Suite(.serialized, .timeLimit(.minutes(1)))
@MainActor
struct MacHTMLNetworkTests {
    /// A page that tries every way out it can and reports what it saw.
    private static func probe(port: UInt16) -> String {
        """
        <!doctype html><html><body><p>probe</p>
        <img src="http://127.0.0.1:\(port)/img.png">
        <link rel="stylesheet" href="http://127.0.0.1:\(port)/a.css">
        <iframe src="http://127.0.0.1:\(port)/frame"></iframe>
        </body></html>
        """
    }

    private static let checks = """
        const base = "http://127.0.0.1:" + port;
        const out = {};
        try { await fetch(base + "/fetch"); out.fetch = "ok"; } catch (e) { out.fetch = "blocked"; }
        out.xhr = await new Promise(r => { const x = new XMLHttpRequest(); x.onload = () => r("ok"); x.onerror = () => r("blocked");
          try { x.open("GET", base + "/xhr"); x.send(); } catch (e) { r("blocked"); } });
        out.img = await new Promise(r => { const i = new Image(); i.onload = () => r("ok"); i.onerror = () => r("blocked"); i.src = base + "/img2.png"; });
        out.ws = await new Promise(r => { try { const w = new WebSocket("ws://127.0.0.1:" + port + "/ws");
          w.onopen = () => r("ok"); w.onerror = () => r("blocked"); } catch (e) { r("blocked"); } setTimeout(() => r("timeout"), 1500); });
        try { navigator.sendBeacon(base + "/beacon", "x"); } catch (e) {}
        document.cookie = "probe=1";
        out.cookie = document.cookie;
        out.persona = typeof window.persona?.reply === "function" && typeof window.persona?.open === "function";
        out.handler = typeof window.webkit?.messageHandlers?.persona?.postMessage === "function";
        try { window.persona = null; } catch (e) {}
        out.personaStays = typeof window.persona?.reply === "function";
        out.popup = window.open(base + "/popup") === null;
        location.href = base + "/away";
        await new Promise(r => setTimeout(r, 800));
        out.href = location.href;
        return out;
        """

    /// Counts the connections made to a local port.
    private final class Listener: @unchecked Sendable {
        let listener: NWListener
        private let lock = NSLock()
        private var count = 0
        var connections: Int { lock.withLock { count } }

        init() throws {
            let params = NWParameters.tcp
            params.requiredLocalEndpoint = .hostPort(host: "127.0.0.1", port: .any)
            listener = try NWListener(using: params)
            listener.newConnectionHandler = { [weak self] c in
                self?.lock.withLock { self?.count += 1 }
                c.cancel()
            }
        }

        func start() async throws -> UInt16 {
            let ready = AsyncStream<UInt16?>.makeStream()
            listener.stateUpdateHandler = { [listener] state in
                switch state {
                case .ready: ready.continuation.yield(listener.port?.rawValue); ready.continuation.finish()
                case .failed, .cancelled: ready.continuation.yield(nil); ready.continuation.finish()
                default: break
                }
            }
            let timer = Task {
                try? await Task.sleep(for: .seconds(5))
                ready.continuation.yield(nil)
                ready.continuation.finish()
            }
            defer { timer.cancel() }
            listener.start(queue: .global())
            for await port in ready.stream { if let port { return port } }
            throw CocoaError(.featureUnsupported)
        }
    }

    /// Waits until the page whose first paragraph says `marker` is in and done (not the blank one before it).
    private func waitLoaded(_ web: WKWebView, _ marker: String) async throws {
        let js = "document.readyState === 'complete' && document.querySelector('p')?.textContent === '\(marker)'"
        let deadline = ContinuousClock.now.advanced(by: .seconds(8))
        while ContinuousClock.now < deadline {
            if web.url != nil, !web.isLoading {
                let value = try await bounded { done in
                    web.evaluateJavaScript(js) { value, error in
                        if let error { done(.failure(error)) }
                        else { done(.success((value as? Bool) == true ? "ready" : "waiting")) }
                    }
                }
                if value == "ready" { return }
            }
            try await Task.sleep(for: .milliseconds(50))
        }
        throw CocoaError(.coderReadCorrupt)
    }

    @MainActor private final class Pending {
        var continuation: CheckedContinuation<String, any Error>?
        var timer: Task<Void, Never>?
        func finish(_ result: Result<String, any Error>) {
            guard let continuation else { return }
            self.continuation = nil
            timer?.cancel(); timer = nil
            continuation.resume(with: result)
        }
    }

    private func bounded(_ operation: (@escaping @MainActor (Result<String, any Error>) -> Void) -> Void) async throws -> String {
        let pending = Pending()
        return try await withCheckedThrowingContinuation { continuation in
            pending.continuation = continuation
            pending.timer = Task {
                do { try await Task.sleep(for: .seconds(5)) } catch { return }
                pending.finish(.failure(CocoaError(.featureUnsupported)))
            }
            operation { pending.finish($0) }
        }
    }

    /// The assistant's free drawing in a real web view.
    private func run(sandboxed: Bool) async throws -> (out: [String: Any], connections: Int) {
        let listener = try Listener()
        let port = try await listener.start()
        defer { listener.listener.cancel() }
        let app = NSApplication.shared
        app.setActivationPolicy(.prohibited)
        #expect(app.activationPolicy() == .prohibited && !app.isActive)
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 320, height: 480),
                              styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        let web: WKWebView
        var coordinator: SandboxedHTMLCoordinator?
        if sandboxed {
            let c = SandboxedHTMLCoordinator { _ in }
            web = SandboxedHTMLCoordinator.makeWebView(c)
            c.load(Self.probe(port: port), in: web)
            coordinator = c
        } else {
            let config = WKWebViewConfiguration()
            config.websiteDataStore = .nonPersistent()
            web = WKWebView(frame: .zero, configuration: config)
            web.loadHTMLString(Self.probe(port: port), baseURL: nil)
        }
        web.frame = window.contentLayoutRect
        window.contentView?.addSubview(web)
        #expect(!window.isVisible && !window.isKeyWindow && !window.isMainWindow && !app.isActive)
        defer {
            coordinator?.dismantle(web)
            web.stopLoading()
            web.removeFromSuperview()
            window.close()
            #expect(!app.isActive)
        }
        try await waitLoaded(web, "probe")
        let text = try await bounded { done in
            web.callAsyncJavaScript("return JSON.stringify(await (async () => {\(Self.checks)})());",
                                    arguments: ["port": Int(port)], in: nil, in: .page, completionHandler: { result in
                switch result {
                case .success(let value):
                    if let text = value as? String { done(.success(text)) }
                    else { done(.failure(CocoaError(.coderReadCorrupt))) }
                case .failure(let error): done(.failure(error))
                }
            })
        }
        let out = try JSONSerialization.jsonObject(with: Data(text.utf8)) as? [String: Any] ?? [:]
        try await Task.sleep(for: .milliseconds(500)) // a beacon or a late subresource
        return (out, listener.connections)
    }

    /// Without the sandbox the same page does reach the port: the check below is a real one.
    @Test func theProbeReachesTheNetworkOutsideTheSandbox() async throws {
        let r = try await run(sandboxed: false)
        #expect(r.connections > 0)
    }

    @Test func theSandboxReachesNothing() async throws {
        let r = try await run(sandboxed: true)
        #expect(r.connections == 0, "requests that left the page: \(r.connections)")
        #expect(r.out["fetch"] as? String == "blocked")
        #expect(r.out["xhr"] as? String == "blocked")
        #expect(r.out["img"] as? String == "blocked")
        #expect(r.out["ws"] as? String != "ok")
        #expect((r.out["cookie"] as? String ?? "x").isEmpty)
        #expect(r.out["persona"] as? Bool == true)
        #expect(r.out["handler"] as? Bool == true)
        #expect(r.out["personaStays"] as? Bool == true)
        #expect(r.out["popup"] as? Bool == true)
        #expect(r.out["href"] as? String == "about:blank")
    }

}
