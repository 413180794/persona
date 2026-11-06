import Foundation
import Network
import Testing
import UIKit
import WebKit
@testable import Persona

/// The assistant's free drawing (#2025): what a card's page may ask for, and that its sandbox reaches nothing.
@Suite(.serialized)
@MainActor
struct AssistantHTMLCardTests {
    @Test func aReplyIsUsedOnceButOpensRemainUsable() {
        let reply = HTMLCardRequest.reply("选 B")
        let open = HTMLCardRequest.open(URL(string: "https://example.com/card")!)
        var actions = HTMLCardActions()
        actions.receive(reply, answered: false)
        #expect(actions.pending == reply)
        #expect({ actions.approve(reply, answered: false) }())
        #expect({ !actions.approve(reply, answered: false) }())
        actions.receive(open, answered: false)
        #expect(actions.pending == nil)
        actions.complete(reply, success: true)
        actions.restart()
        actions.receive(reply, answered: false)
        #expect(actions.pending == nil)
        for _ in 0..<2 {
            actions.receive(open, answered: true)
            #expect(actions.pending == open)
            #expect({ actions.approve(open, answered: true) }())
            actions.complete(open, success: true)
        }
    }

    @Test func decliningMutesUntilRestartAndAReplyFailureCanRetry() {
        let reply = HTMLCardRequest.reply("选 B")
        var actions = HTMLCardActions()
        actions.receive(reply, answered: false)
        actions.decline()
        actions.receive(reply, answered: false)
        #expect(actions.pending == nil)
        actions.restart()
        actions.receive(reply, answered: false)
        #expect({ actions.approve(reply, answered: false) }())
        actions.complete(reply, success: false)
        actions.receive(reply, answered: false)
        #expect(actions.pending == reply)
    }

    @Test func anAnswerOnAnotherDeviceRevokesPendingConfirmation() {
        let reply = HTMLCardRequest.reply("选 B")
        var actions = HTMLCardActions()
        actions.receive(reply, answered: true)
        #expect(actions.pending == nil)
        actions.receive(reply, answered: false)
        #expect({ !actions.approve(reply, answered: true) }())
        #expect(actions.pending == nil && !actions.sending)
        actions.receive(reply, answered: false)
        actions.dismiss()
        #expect({ !actions.approve(reply, answered: false) }())
    }

    @Test func repliesAndLinksAPageMaySend() {
        #expect(HTMLCardRequest.parse(["kind": "reply", "text": "  选 B  "], mainFrame: true) == .reply("选 B"))
        #expect(HTMLCardRequest.parse(["kind": "open", "url": "https://example.com/a"], mainFrame: true)
            == .open(URL(string: "https://example.com/a")!))
        #expect(HTMLCardRequest.parse(["kind": "open", "url": "persona://session/my-mac/abc"], mainFrame: true) != nil)
        #expect(HTMLCardRequest.parse(["kind": "open", "url": "persona://kb/people/x.md"], mainFrame: true) != nil)
        #expect(HTMLCardRequest.parse(["kind": "open", "url": "persona://task/example"], mainFrame: true) != nil)
        #expect(HTMLCardRequest.parse(["kind": "open", "url": "persona://file/my-mac/%2FUsers%2Fme%2Freport.txt"], mainFrame: true) != nil)
    }

    @Test func whatAPageCannotSend() {
        let refused: [(Any, Bool)] = [
            (["kind": "reply", "text": "选 B"], false), // another frame's (an iframe of the page)
            (["kind": "reply", "text": "   "], true),
            (["kind": "reply", "text": String(repeating: "长", count: HTMLCardRequest.maxReply + 1)], true),
            (["kind": "reply"], true),
            (["kind": "reply", "text": 3], true),
            (["kind": "send_mail", "to": "me@example.com"], true),
            ("reply", true),
            (["kind": "open", "url": "http://example.com"], true),
            (["kind": "open", "url": "javascript:alert(1)"], true),
            (["kind": "open", "url": "persona://settings/hub"], true),
            (["kind": "open", "url": "persona://session"], true),
            (["kind": "open", "url": "tel:10086"], true),
            (["kind": "open", "url": "https://user:password@example.com/"], true),
            (["kind": "open", "url": "https://example.com/%ZZ"], true),
            (["kind": "open", "url": "https://example.com/%0A"], true),
            (["kind": "open", "url": "persona://session/one-part"], true),
            (["kind": "open", "url": "persona://task/example/extra"], true),
            (["kind": "open", "url": "persona://kb/../secret"], true),
            (["kind": "open", "url": "persona://kb/%2e%2e/secret"], true),
            (["kind": "open", "url": "persona://kb/%2fetc"], true),
            (["kind": "open", "url": "persona://file/my-mac/../secret"], true),
            (["kind": "open", "url": "persona://file/my-mac/report.txt"], true),
            (["kind": "open", "url": "persona://file/my-mac/Users/me/report.txt"], true),
            (["kind": "open", "url": "persona://kb/folder//file"], true),
            (["kind": "open", "url": "persona://kb/file?path=other"], true),
            (["kind": "open", "url": "https://" + String(repeating: "a", count: HTMLCardRequest.maxURL)], true),
        ]
        for (body, main) in refused { #expect(HTMLCardRequest.parse(body, mainFrame: main) == nil, "\(body)") }
    }

    @Test func heightStaysInRange() {
        #expect(AssistantHTMLCard.clampedHeight(nil) == 320)
        #expect(AssistantHTMLCard.clampedHeight(40) == 160)
        #expect(AssistantHTMLCard.clampedHeight(480) == 480)
        #expect(AssistantHTMLCard.clampedHeight(5000) == 640)
    }

    // MARK: The sandbox, in a real web view

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
            let ready = AsyncStream<UInt16?> { cont in
                listener.stateUpdateHandler = { [listener] state in
                    switch state {
                    case .ready: cont.yield(listener.port?.rawValue); cont.finish()
                    case .failed, .cancelled: cont.yield(nil); cont.finish()
                    default: break
                    }
                }
            }
            listener.start(queue: .global())
            for await port in ready { if let port { return port } }
            throw CocoaError(.featureUnsupported)
        }
    }

    /// Waits until the page whose first paragraph says `marker` is in and done (not the blank one before it).
    private func waitLoaded(_ web: WKWebView, _ marker: String) async throws {
        let js = "document.readyState === 'complete' && document.querySelector('p')?.textContent === '\(marker)'"
        for _ in 0..<100 {
            try await Task.sleep(for: .milliseconds(50))
            if !web.isLoading, (try? await web.evaluateJavaScript(js)) as? Bool == true { return }
        }
        Issue.record("the page never finished loading")
    }

    /// Explicitly suspended compilation lets the test finish old and new requests in either order.
    private final class RulesLatch {
        private var waiting: [CheckedContinuation<WKContentRuleList, any Error>] = []
        var count: Int { waiting.count }

        func next() async throws -> WKContentRuleList {
            try await withCheckedThrowingContinuation { waiting.append($0) }
        }

        func finish(_ index: Int, with rules: WKContentRuleList) { waiting.remove(at: index).resume(returning: rules) }
        func cancelAll() {
            let pending = waiting
            waiting.removeAll()
            for wait in pending { wait.resume(throwing: CancellationError()) }
        }

        func waitFor(_ count: Int) async throws {
            for _ in 0..<100 {
                if waiting.count >= count { return }
                try await Task.sleep(for: .milliseconds(10))
            }
            throw CocoaError(.featureUnsupported)
        }
    }

    @Test func obsoleteCompilationCannotReplaceTheLatestPage() async throws {
        let rules = try await SandboxedHTMLCoordinator.compiledRules()
        let latch = RulesLatch()
        defer { latch.cancelAll() }
        let c = SandboxedHTMLCoordinator(rulesLoader: { try await latch.next() }) { _ in }
        let web = SandboxedHTMLCoordinator.makeWebView(c)
        defer { c.dismantle(web) }
        c.load("<p>old</p>", in: web)
        try await latch.waitFor(1)
        c.load("<p>latest</p>", in: web)
        try await latch.waitFor(2)
        latch.finish(1, with: rules)
        try await waitLoaded(web, "latest")
        latch.finish(0, with: rules)
        try await Task.sleep(for: .milliseconds(200))
        #expect(try await web.evaluateJavaScript("document.querySelector('p')?.textContent") as? String == "latest")
    }

    @Test func dismantleCancelsCompilationAndRemovesTheBridge() async throws {
        let rules = try await SandboxedHTMLCoordinator.compiledRules()
        let latch = RulesLatch()
        defer { latch.cancelAll() }
        let c = SandboxedHTMLCoordinator(rulesLoader: { try await latch.next() }) { _ in Issue.record("a destroyed card sent a request") }
        let web = SandboxedHTMLCoordinator.makeWebView(c)
        c.load("<p>must not run</p><script>persona.reply('late')</script>", in: web)
        try await latch.waitFor(1)
        c.dismantle(web)
        latch.finish(0, with: rules)
        try await Task.sleep(for: .milliseconds(300))
        #expect(web.navigationDelegate == nil && web.uiDelegate == nil)
        #expect(web.configuration.userContentController.userScripts.isEmpty)
        #expect(try await web.evaluateJavaScript("document.querySelector('p')?.textContent ?? ''") as? String == "")
        #expect(try await web.evaluateJavaScript("typeof window.webkit?.messageHandlers?.persona") as? String == "undefined")
    }

    @Test func oldPageMessagesAreIgnoredAfterAnUpdate() async throws {
        var got: [HTMLCardRequest] = []
        let c = SandboxedHTMLCoordinator { got.append($0) }
        let web = SandboxedHTMLCoordinator.makeWebView(c)
        defer { c.dismantle(web) }
        c.load("<p>old</p>", in: web)
        try await waitLoaded(web, "old")
        let oldInstance = try #require(c.instance)
        c.load("<p>new</p>", in: web)
        try await waitLoaded(web, "new")
        #expect(c.instance != oldInstance)
        _ = try await web.callAsyncJavaScript("""
            window.webkit.messageHandlers.persona.postMessage({kind:'reply',text:'stale',instance:oldInstance});
            persona.reply('current');
            await new Promise(r => setTimeout(r, 100));
            """, arguments: ["oldInstance": oldInstance], contentWorld: .page)
        #expect(got == [.reply("current")])
    }

    @Test func failureToCompileRulesNeverLoadsAssistantHTML() async throws {
        let c = SandboxedHTMLCoordinator(rulesLoader: { throw CocoaError(.featureUnsupported) }) { _ in
            Issue.record("a card ran without its rules")
        }
        let web = SandboxedHTMLCoordinator.makeWebView(c)
        defer { c.dismantle(web) }
        c.load("<p>unsafe</p><script>persona.reply('unsafe')</script>", in: web)
        try await waitLoaded(web, "无法隔离网络，不运行这张卡片")
        #expect(try await web.evaluateJavaScript("document.querySelector('p')?.textContent") as? String == "无法隔离网络，不运行这张卡片")
    }

    /// The assistant's free drawing in a real web view.
    private func run(sandboxed: Bool) async throws -> (out: [String: Any], connections: Int) {
        let listener = try Listener()
        let port = try await listener.start()
        defer { listener.listener.cancel() }
        let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 320, height: 480))
        let web: WKWebView
        var coordinator: SandboxedHTMLCoordinator?
        if sandboxed {
            let c = SandboxedHTMLCoordinator { _ in }
            web = SandboxedHTMLCoordinator.makeWebView(c)
            c.load(Self.probe(port: port), in: web)
            coordinator = c
        } else {
            web = WKWebView(frame: .zero, configuration: WKWebViewConfiguration())
            web.loadHTMLString(Self.probe(port: port), baseURL: nil)
        }
        web.frame = window.bounds
        window.addSubview(web)
        window.isHidden = false
        defer { window.isHidden = true; _ = coordinator }
        try await waitLoaded(web, "probe")
        let out = try await web.callAsyncJavaScript(Self.checks, arguments: ["port": Int(port)], contentWorld: .page) as? [String: Any] ?? [:]
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

    @Test func thePageReachesTheAppOnlyThroughPersona() async throws {
        var got: [HTMLCardRequest] = []
        let c = SandboxedHTMLCoordinator { got.append($0) }
        let web = SandboxedHTMLCoordinator.makeWebView(c)
        c.load("<!doctype html><p>bridge</p>", in: web)
        try await waitLoaded(web, "bridge")
        _ = try await web.callAsyncJavaScript("""
            persona.reply("选 B");
            persona.open("https://example.com/x");
            persona.open("http://example.com/x");
            window.webkit.messageHandlers.persona.postMessage({kind: "send_mail", to: "me@example.com"});
            const f = document.createElement("iframe");
            f.srcdoc = "<script>window.webkit.messageHandlers.persona.postMessage({kind:'reply',text:'from a frame'})<\\/script>";
            document.body.appendChild(f);
            await new Promise(r => setTimeout(r, 400));
            """, contentWorld: .page)
        try await Task.sleep(for: .milliseconds(200))
        #expect(got == [.reply("选 B"), .open(URL(string: "https://example.com/x")!)])
    }
}
