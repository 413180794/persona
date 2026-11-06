import Foundation
import Network
import Testing
import UIKit
import WebKit
@testable import Persona

/// Additional #2025 acceptance probes. Every page and HTTP response is synthetic and loopback-only.
/// These tests exercise a real WebView, not a JavaScript DOM substitute or a mocked message handler.
@Suite(.serialized, .timeLimit(.minutes(1)))
@MainActor
struct AssistantHTMLSecurityTests {
    @Test func getAndPostFormsCannotLeaveTheCard() async throws {
        let server = try LocalHTTPServer()
        defer { server.stop() }
        let port = try await server.start()
        for method in ["GET", "POST"] {
            let path = "/form-\(method.lowercased())"
            let page = """
                <p id="marker">form</p>
                <form id="probe" method="\(method)" action="http://127.0.0.1:\(port)\(path)">
                  <input name="synthetic" value="form-value">
                </form>
                """
            let control = Page(sandboxed: false)
            defer { control.close() }
            control.load(page)
            try await loaded(control.web, marker: "form")
            _ = try await script(control.web, "setTimeout(() => document.querySelector('form').submit(), 0); return {};")
            try await waitUntil("the control form reached the listener") {
                server.requests.contains { $0.method == method && $0.path.hasPrefix(path) }
            }
            control.close()
            let before = server.connections

            let card = Page()
            defer { card.close() }
            card.load(page)
            try await loaded(card.web, marker: "form")
            _ = try await script(card.web, "setTimeout(() => document.querySelector('form').submit(), 0); return {};")
            try await Task.sleep(for: .milliseconds(500))
            let result = try await script(card.web, "return { marker: document.querySelector('#marker')?.textContent, href: location.href };")
            #expect(server.connections == before, "\(method) form escaped the sandbox")
            #expect(result["marker"] as? String == "form")
            #expect(result["href"] as? String == "about:blank")
        }
    }

    @Test func metaRefreshAndLocationCannotStartARedirectChain() async throws {
        let server = try LocalHTTPServer()
        defer { server.stop() }
        let port = try await server.start()
        let url = "http://127.0.0.1:\(port)/redirect/start"
        // Calibrate both HTTP hops, not merely an attempted request to a port that rejects connections.
        let control = Page(sandboxed: false)
        defer { control.close() }
        control.web.load(URLRequest(url: try #require(URL(string: url))))
        try await loaded(control.web, marker: "redirected", phase: "redirect control load")
        #expect(server.requests.contains { $0.path == "/redirect/start" })
        #expect(server.requests.contains { $0.path == "/redirect/final" })
        control.close()
        let before = server.connections

        for redirect in [
            "<meta http-equiv=\"refresh\" content=\"0;url=\(url)\">",
            "<script>setTimeout(() => location.replace('\(url)'), 100)</script>",
        ] {
            let card = Page()
            defer { card.close() }
            card.load("<p id='marker'>redirect</p>" + redirect)
            let variant = redirect.hasPrefix("<meta") ? "meta" : "location"
            try await loaded(card.web, marker: "redirect", phase: "\(variant) redirect load")
            try await Task.sleep(for: .milliseconds(600))
            let result = try await script(card.web, "return { marker: document.querySelector('#marker')?.textContent, href: location.href };",
                                          phase: "\(variant) redirect snapshot")
            #expect(server.connections == before, "a redirect's first hop escaped the sandbox")
            #expect(result["marker"] as? String == "redirect")
            #expect(result["href"] as? String == "about:blank")
        }
    }

    @Test func externalScriptsFontsCSSImportsAndSVGResourcesStayOffline() async throws {
        let server = try LocalHTTPServer()
        defer { server.stop() }
        let port = try await server.start()
        let base = "http://127.0.0.1:\(port)"
        let page = """
            <p id="marker">resources</p>
            <link rel="stylesheet" href="\(base)/style.css">
            <script src="\(base)/script.js"></script>
            <svg xmlns="http://www.w3.org/2000/svg" width="64" height="64">
              <image href="\(base)/vector.svg" width="64" height="64"/>
            </svg>
            <video preload="metadata" poster="\(base)/poster.svg" src="\(base)/video.mp4"></video>
            <audio preload="metadata" src="\(base)/audio.wav"></audio>
            <object data="\(base)/object.html" type="text/html"></object>
            <embed src="\(base)/embed.html" type="text/html">
            """
        let control = Page(sandboxed: false)
        defer { control.close() }
        control.load(page)
        do { try await loaded(control.web, marker: "resources") }
        catch { throw ProbeError.timeout("control resources: \(server.requests.map(\.path)), \(error)") }
        let calibrated = ["/style.css", "/import.css", "/font.woff2", "/script.js", "/vector.svg", "/poster.svg"]
        try await waitUntil("all calibrated external resources reached the listener") {
            calibrated.allSatisfy { path in server.requests.contains { $0.path == path } }
        }
        let controlResult = try await script(control.web, "return { externalScript: window.externalScriptRan === true };")
        #expect(controlResult["externalScript"] as? Bool == true)
        control.close()
        // Let cancelled control subresources drain before measuring the isolated page.
        try await Task.sleep(for: .milliseconds(200))
        let before = server.connections

        let card = Page()
        defer { card.close() }
        card.load(page)
        do { try await loaded(card.web, marker: "resources") }
        catch { throw ProbeError.timeout("sandbox resources: \(server.requests.map(\.path)), \(error)") }
        try await Task.sleep(for: .milliseconds(600))
        let result = try await script(card.web, "return { externalScript: window.externalScriptRan === true, bridge: typeof persona.reply === 'function' };")
        #expect(server.connections == before, "an external resource escaped the sandbox")
        #expect(result["externalScript"] as? Bool == false)
        #expect(result["bridge"] as? Bool == true)
    }

    @Test func javaScriptSystemDialogsReturnTheirDeniedValuesWithoutPresentingUI() async throws {
        let card = Page()
        defer { card.close() }
        card.load("<p id='marker'>dialogs</p>")
        try await loaded(card.web, marker: "dialogs")
        let result = try await script(card.web, """
            const alertResult = alert('synthetic-alert');
            const confirmResult = confirm('synthetic-confirm');
            const promptResult = prompt('synthetic-prompt', 'synthetic-default');
            return { alertReturned: alertResult === undefined, confirmDenied: confirmResult === false,
                     promptDenied: promptResult === null, finished: true };
            """)
        #expect(result["alertReturned"] as? Bool == true)
        #expect(result["confirmDenied"] as? Bool == true)
        #expect(result["promptDenied"] as? Bool == true)
        #expect(result["finished"] as? Bool == true)
        #expect(card.root.presentedViewController == nil)
        #expect(card.requests.isEmpty)
    }

    @Test func cookiesAndStorageAreNotSharedAcrossCardsOrTheControlPage() async throws {
        let server = try LocalHTTPServer()
        defer { server.stop() }
        let port = try await server.start()
        let control = Page(sandboxed: false)
        let first = Page()
        let second = Page()
        defer { control.close(); first.close(); second.close() }
        control.web.load(URLRequest(url: try #require(URL(string: "http://127.0.0.1:\(port)/storage"))))
        try await loaded(control.web, marker: "storage")
        let positive = try await script(control.web, """
            document.cookie = 'security-control=synthetic-control; path=/';
            localStorage.setItem('security-control', 'synthetic-control');
            sessionStorage.setItem('security-control', 'synthetic-control');
            return { cookie: document.cookie.includes('security-control=synthetic-control'),
                     local: localStorage.getItem('security-control'), session: sessionStorage.getItem('security-control') };
            """)
        #expect(positive["cookie"] as? Bool == true)
        #expect(positive["local"] as? String == "synthetic-control")
        #expect(positive["session"] as? String == "synthetic-control")
        let firstStore = first.web.configuration.websiteDataStore
        let secondStore = second.web.configuration.websiteDataStore
        #expect(!firstStore.isPersistent && !secondStore.isPersistent)
        #expect(firstStore !== secondStore)
        #expect(firstStore !== control.web.configuration.websiteDataStore)
        #expect(try await cookies(first.web).isEmpty)
        #expect(try await cookies(second.web).isEmpty)

        // A native write is a positive control for cookie-store isolation even when about:blank denies DOM storage.
        let cookie = try #require(HTTPCookie(properties: [.domain: "127.0.0.1", .path: "/", .name: "security-first", .value: "synthetic-first"]))
        _ = try await completeWithin("seed a synthetic cookie") { done in
            firstStore.httpCookieStore.setCookie(cookie) { done(.success("done")) }
        }
        #expect(try await cookies(first.web).contains("security-first"))
        #expect(try await cookies(second.web).isEmpty)
        #expect(try await cookies(control.web).contains("security-control"))
        #expect(try await cookies(control.web).contains("security-first") == false)

        for card in [first, second] {
            card.load("<p id='marker'>isolated-storage</p>")
            try await loaded(card.web, marker: "isolated-storage")
            let result = try await script(card.web, """
                const out = { cookie: document.cookie };
                for (const name of ['localStorage', 'sessionStorage']) {
                  try { const store = window[name]; out[name] = store.getItem('security-control');
                        store.setItem('security-card', 'synthetic-card'); out[name + 'Write'] = store.getItem('security-card'); }
                  catch (e) { out[name] = e.name; out[name + 'Write'] = e.name; }
                }
                return out;
                """)
            #expect(result["cookie"] as? String == "")
            for name in ["localStorage", "sessionStorage"] {
                #expect(result[name] as? String != "synthetic-control")
                #expect(result[name + "Write"] as? String == "SecurityError", "the card unexpectedly has a storage origin")
            }
        }
        let unchanged = try await script(control.web, "return { value: localStorage.getItem('security-control') };")
        #expect(unchanged["value"] as? String == "synthetic-control", "isolating a card must not clear another WebView's storage")
    }

    @Test func unknownHandlersAndKindsCannotReachNativeActions() async throws {
        let card = Page()
        defer { card.close() }
        card.load("<p id='marker'>handlers</p>")
        try await loaded(card.web, marker: "handlers")
        let instance = try #require(card.coordinator?.instance)
        let result = try await script(card.web, """
            const instance = \(json(instance));
            const names = ['reply', 'open', 'native', 'phone', 'send_mail', 'unknown'];
            const absent = names.every(name => typeof window.webkit.messageHandlers[name] === 'undefined');
            for (const name of names) {
              try { window.webkit.messageHandlers[name].postMessage({ kind: 'reply', text: 'forged-handler', instance }); } catch (_) {}
            }
            window.webkit.messageHandlers.persona.postMessage({ kind: 'send_mail', text: 'forged-kind', instance });
            window.webkit.messageHandlers.persona.postMessage({ kind: 'reply', text: 42, instance });
            window.webkit.messageHandlers.persona.postMessage({ kind: 'open', url: 'javascript:alert(1)', instance });
            persona.reply('main-frame-control');
            return { absent };
            """)
        try await waitUntil("the main-frame positive control was delivered") { !card.requests.isEmpty }
        try await Task.sleep(for: .milliseconds(200))
        #expect(result["absent"] as? Bool == true)
        #expect(card.requests == [.reply("main-frame-control")])
    }

    @Test func childFramesCannotPostWithTheCurrentNonceOrCallTheParentBridge() async throws {
        let card = Page()
        defer { card.close() }
        card.load("<p id='marker'>frames</p>")
        try await loaded(card.web, marker: "frames")
        let instance = try #require(card.coordinator?.instance)
        _ = try await script(card.web, """
            window.frameProbe = [];
            const frame = document.createElement('iframe');
            frame.srcdoc = `<p>synthetic-child</p><script>
              parent.frameProbe.push('child-ran');
              try { window.webkit.messageHandlers.persona.postMessage({kind:'reply',text:'child-direct',instance:\(json(instance))}); }
              catch (_) { parent.frameProbe.push('direct-denied'); }
              try { parent.persona.reply('child-through-parent'); }
              catch (_) { parent.frameProbe.push('parent-denied'); }
              parent.frameProbe.push('child-finished');
            <\\/script>`;
            document.body.appendChild(frame);
            persona.reply('main-frame-control');
            return {};
            """)
        try await waitUntil("the main-frame positive control was delivered") { !card.requests.isEmpty }
        try await Task.sleep(for: .milliseconds(500))
        let result = try await script(card.web, """
            const frame = document.querySelector('iframe');
            return { attempts: window.frameProbe, childExists: !!frame,
                     childBlocked: !frame?.contentDocument?.querySelector('p') };
            """)
        #expect(result["childExists"] as? Bool == true)
        // Either the child is blocked entirely, or its actual script must be observed through completion.
        #expect(result["childBlocked"] as? Bool == true || (result["attempts"] as? [String])?.contains("child-finished") == true)
        #expect(card.requests == [.reply("main-frame-control")], "a same-origin srcdoc child called the parent bridge")
    }

    @Test func terminationCallbackRevokesTheBridgeAndAllowsTheSameHTMLToRetry() async throws {
        let card = Page()
        defer { card.close() }
        let html = "<p id='marker'>restart</p>"
        card.load(html)
        try await loaded(card.web, marker: "restart")
        let coordinator = try #require(card.coordinator)
        var unavailableCount = 0
        coordinator.onUnavailable = { unavailableCount += 1 }
        let oldInstance = try #require(coordinator.instance)
        let delegate: any WKNavigationDelegate = coordinator
        let handlesExit = delegate.responds(to: #selector(WKNavigationDelegate.webViewWebContentProcessDidTerminate(_:)))
        try #require(handlesExit, "there is no public content-process-exit recovery callback")
        // Public delegate fault injection, not an OS process kill. Never use private WebKit PID/termination APIs.
        // Removing the old DOM makes a same-HTML no-op retry fail rather than looking like recovery.
        _ = try await script(card.web, "document.body.replaceChildren(); return {};")
        card.web.stopLoading()
        delegate.webViewWebContentProcessDidTerminate?(card.web)
        #expect(unavailableCount == 1)
        #expect(coordinator.instance == nil, "content-process exit retained the old bridge instance")
        #expect(card.web.configuration.userContentController.userScripts.isEmpty)
        coordinator.load(html, in: card.web)
        try await loaded(card.web, marker: "restart")
        #expect(coordinator.instance != oldInstance)
        _ = try await script(card.web, """
            window.webkit.messageHandlers.persona.postMessage({kind:'reply',text:'before-exit',instance:\(json(oldInstance))});
            persona.reply('after-restart');
            return {};
            """)
        try await waitUntil("the restarted page delivered its positive control") { !card.requests.isEmpty }
        try await Task.sleep(for: .milliseconds(200))
        #expect(card.requests == [.reply("after-restart")])
    }

    @Test func inactiveAndRepeatedTerminationCallbacksDoNotNotifyAgain() async throws {
        let card = Page()
        defer { card.close() }
        let coordinator = try #require(card.coordinator)
        var unavailableCount = 0
        coordinator.onUnavailable = { unavailableCount += 1 }
        let delegate: any WKNavigationDelegate = coordinator
        delegate.webViewWebContentProcessDidTerminate?(card.web)
        #expect(unavailableCount == 0, "an unloaded page has no execution to invalidate")
        card.load("<p id='marker'>one-exit</p>")
        try await loaded(card.web, marker: "one-exit")
        delegate.webViewWebContentProcessDidTerminate?(card.web)
        delegate.webViewWebContentProcessDidTerminate?(card.web)
        #expect(unavailableCount == 1, "repeated public callbacks must not invalidate twice")
        coordinator.load("<p id='marker'>after-exit</p>", in: card.web)
        try await loaded(card.web, marker: "after-exit")
        coordinator.dismantle(card.web)
        delegate.webViewWebContentProcessDidTerminate?(card.web)
        #expect(unavailableCount == 1, "a callback arriving after dismantle must be ignored")
        #expect(coordinator.instance == nil)
        #expect(card.web.configuration.userContentController.userScripts.isEmpty)
    }

    @Test func leavingClearsLiveTimersBoundedWorkAndRetainedBridgeClosures() async throws {
        let card = Page()
        defer { card.close() }
        card.load("""
            <p id="marker">bounded-work</p><script>
            window.workProbe = { ticks: 0, iterations: 0, bytes: 0 };
            window.workBuffers = Array.from({length:8}, () => new Uint8Array(512 * 1024));
            for (const buffer of workBuffers) { buffer.fill(1); workProbe.bytes += buffer.byteLength; }
            const deadline = performance.now() + 100;
            while (workProbe.iterations < 200000 && performance.now() < deadline) { workProbe.iterations++; }
            const timer = setInterval(() => {
              workProbe.ticks++;
              persona.reply('bounded-tick-' + workProbe.ticks);
              if (workProbe.ticks >= 20) { clearInterval(timer); window.workBuffers = []; }
            }, 50);
            </script>
            """)
        try await loaded(card.web, marker: "bounded-work", phase: "work initial load")
        try await waitUntil("at least two live timer messages were delivered") { card.requests.count >= 2 }
        let before = try await script(card.web, "return { ...window.workProbe, activeBuffers: window.workBuffers.length };", phase: "live work snapshot")
        #expect(before["bytes"] as? Int == 4 * 1024 * 1024)
        #expect(before["activeBuffers"] as? Int == 8, "the allocation must still be live at teardown")
        #expect((before["iterations"] as? Int ?? 0) <= 200000)
        #expect((before["ticks"] as? Int ?? 0) >= 2)
        #expect((before["ticks"] as? Int ?? 20) < 20, "the timer must still be active at teardown")
        weak var releasedCoordinator = card.coordinator
        card.coordinator?.dismantle(card.web)
        card.coordinator = nil
        try await loaded(card.web, marker: nil, phase: "work cleared-page load")
        let requestsAfterLeaving = card.requests.count
        try await Task.sleep(for: .milliseconds(500))
        let after = try await script(card.web, """
            return { workGone: typeof window.workProbe === 'undefined' && typeof window.workBuffers === 'undefined',
                     handlerGone: typeof window.webkit?.messageHandlers?.persona === 'undefined' };
            """, phase: "work cleared-page snapshot")
        #expect(card.requests.count == requestsAfterLeaving)
        #expect(after["workGone"] as? Bool == true)
        #expect(after["handlerGone"] as? Bool == true)
        #expect(card.web.navigationDelegate == nil && card.web.uiDelegate == nil)
        #expect(releasedCoordinator == nil, "a retained WebView kept its old bridge closure alive")
        // Four MiB and a bounded 100 ms / 200,000-iteration loop are a cleanup probe, not a CPU or memory quota.
        // No infinite loop or memory-exhaustion payload is run on the shared development machine.
    }

    /// Run only as a separate test-without-building command in a disposable simulator, under a 30-second
    /// external watchdog that terminates that test host and shuts down that simulator on every exit path.
    /// A content process has no public termination/CPU-quota API; releasing the last page is the operation under test.
    @Test(.enabled(if: ProcessInfo.processInfo.environment["PERSONA_HTML_DESTRUCTIVE_WATCHDOG_SECONDS"] == "30",
                   "Requires the dedicated 30-second external simulator watchdog"))
    func aRealInfiniteLoopCanBeDiscardedWithoutBlockingANewCard() async throws {
        var card: Page? = Page()
        defer { card?.close() }
        card?.load("<p id='marker'>infinite-loop</p>")
        try await loaded(try #require(card?.web), marker: "infinite-loop")
        weak var oldWeb = card?.web
        var enteredLoop = false
        var callbackFinished = false
        card?.coordinator?.onRequest = {
            if $0 == .reply("entered-loop") {
                enteredLoop = true
                FileHandle.standardOutput.write(Data("PERSONA_HTML_SECURITY_LOOP_ENTERED\n".utf8))
            }
        }
        FileHandle.standardOutput.write(Data("PERSONA_HTML_SECURITY_LOOP_START\n".utf8))
        card?.web.evaluateJavaScript("persona.reply('entered-loop'); while (true) {}") { _, _ in callbackFinished = true }
        try await waitUntil("the infinite-loop entry message arrived") { enteredLoop }
        try await Task.sleep(for: .milliseconds(200))
        #expect(!callbackFinished, "the deliberately non-terminating script unexpectedly returned")
        card?.close()
        card = nil
        try await waitUntil("the discarded WebView was released") { oldWeb == nil }
        FileHandle.standardOutput.write(Data("PERSONA_HTML_SECURITY_LOOP_RELEASED\n".utf8))
        let replacement = Page()
        defer { replacement.close() }
        replacement.load("<p id='marker'>after-infinite-loop</p>")
        try await loaded(replacement.web, marker: "after-infinite-loop")
        _ = try await script(replacement.web, "persona.reply('replacement-ready'); return {};")
        try await waitUntil("the replacement bridge is responsive") { !replacement.requests.isEmpty }
        #expect(replacement.requests == [.reply("replacement-ready")])
        FileHandle.standardOutput.write(Data("PERSONA_HTML_SECURITY_LOOP_REPLACEMENT_READY\n".utf8))
        // Successful page release/replacement is not evidence of an OS crash callback or a hard resource quota.
    }

    // MARK: Bounded, public-API WebView harness

    @MainActor
    private final class Page {
        let web: WKWebView
        let root = UIViewController()
        let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 320, height: 480))
        var coordinator: SandboxedHTMLCoordinator?
        var requests: [HTMLCardRequest] = []
        private var closed = false

        init(sandboxed: Bool = true) {
            if sandboxed {
                let coordinator = SandboxedHTMLCoordinator { _ in }
                self.coordinator = coordinator
                web = SandboxedHTMLCoordinator.makeWebView(coordinator)
            } else {
                let config = WKWebViewConfiguration()
                config.websiteDataStore = .nonPersistent()
                web = WKWebView(frame: .zero, configuration: config)
            }
            coordinator?.onRequest = { [weak self] in self?.requests.append($0) }
            window.rootViewController = root
            web.frame = window.bounds
            root.view.addSubview(web)
            window.isHidden = false
        }

        func load(_ html: String) {
            if let coordinator { coordinator.load(html, in: web) }
            else { web.loadHTMLString(html, baseURL: nil) }
        }

        func close() {
            guard !closed else { return }
            closed = true
            if let coordinator { coordinator.dismantle(web) }
            else { web.stopLoading(); web.loadHTMLString("", baseURL: nil) }
            web.removeFromSuperview()
            window.isHidden = true
            window.rootViewController = nil
            coordinator = nil
        }
    }

    private enum ProbeError: Error {
        case timeout(String)
        case invalidResult
    }

    @MainActor
    private final class PendingResult {
        var continuation: CheckedContinuation<String, any Error>?
        var timeout: Task<Void, Never>?

        func finish(_ result: Result<String, any Error>) {
            guard let continuation else { return }
            self.continuation = nil
            timeout?.cancel()
            timeout = nil
            continuation.resume(with: result)
        }
    }

    /// A callback must finish within five seconds even if a WebKit operation never completes its callback.
    /// Clearing the page is best effort; the test command also needs an external test-process deadline.
    private func completeWithin(_ label: String, web: WKWebView? = nil,
                                operation: (@escaping @MainActor (Result<String, any Error>) -> Void) -> Void) async throws -> String {
        let pending = PendingResult()
        return try await withCheckedThrowingContinuation { continuation in
            pending.continuation = continuation
            pending.timeout = Task { @MainActor [weak web] in
                do { try await Task.sleep(for: .seconds(5)) } catch { return }
                guard pending.continuation != nil else { return }
                web?.stopLoading()
                web?.loadHTMLString("", baseURL: nil)
                pending.finish(.failure(ProbeError.timeout(label)))
            }
            operation { pending.finish($0) }
        }
    }

    private func script(_ web: WKWebView, _ body: String, phase: String = "JavaScript probe") async throws -> [String: Any] {
        let text = try await completeWithin(phase, web: web) { done in
            web.evaluateJavaScript("JSON.stringify((() => {\(body)})())") { value, error in
                if let error { done(.failure(error)) }
                else if let text = value as? String { done(.success(text)) }
                else { done(.failure(ProbeError.invalidResult)) }
            }
        }
        guard let data = text.data(using: .utf8), let value = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw ProbeError.invalidResult
        }
        return value
    }

    private func json(_ value: String) -> String {
        // Encoding an array and dropping its brackets is valid on older Foundation versions too.
        let text = String(data: try! JSONSerialization.data(withJSONObject: [value]), encoding: .utf8)!
        return String(text.dropFirst().dropLast())
    }

    private func cookies(_ web: WKWebView) async throws -> [String] {
        let text = try await completeWithin("read synthetic cookie names") { done in
            web.configuration.websiteDataStore.httpCookieStore.getAllCookies { cookies in
                let names = cookies.map(\.name).sorted()
                let data = try! JSONSerialization.data(withJSONObject: names)
                done(.success(String(data: data, encoding: .utf8)!))
            }
        }
        return try JSONSerialization.jsonObject(with: Data(text.utf8)) as? [String] ?? []
    }

    private func loaded(_ web: WKWebView, marker: String?, phase: String? = nil) async throws {
        let label = phase ?? "load \(marker ?? "empty page")"
        let deadline = ContinuousClock.now.advanced(by: .seconds(8))
        repeat {
            // A new/transitioning document must finish its native load before starting a five-second JS callback.
            // A nil URL also covers the coordinator's asynchronous rule compilation before the first navigation.
            if web.url == nil || web.isLoading {
                try await Task.sleep(for: .milliseconds(50))
                continue
            }
            let result = try await script(web, """
                return { complete: document.readyState === 'complete', marker: document.querySelector('#marker')?.textContent ?? '',
                         empty: document.body?.textContent.trim() === '' };
                """, phase: "\(label): DOM ready")
            if !web.isLoading, result["complete"] as? Bool == true {
                if let marker, result["marker"] as? String == marker { return }
                if marker == nil, result["empty"] as? Bool == true { return }
            }
            try await Task.sleep(for: .milliseconds(50))
        } while ContinuousClock.now < deadline
        throw ProbeError.timeout(label)
    }

    private func waitUntil(_ label: String, condition: () -> Bool) async throws {
        let deadline = ContinuousClock.now.advanced(by: .seconds(5))
        repeat {
            if condition() { return }
            try await Task.sleep(for: .milliseconds(25))
        } while ContinuousClock.now < deadline
        throw ProbeError.timeout(label)
    }

    // MARK: One loopback HTTP listener per test, with bounded receives and unconditional cancellation

    private final class LocalHTTPServer: @unchecked Sendable {
        struct Request: Sendable { let method: String; let path: String }
        let listener: NWListener
        private let queue = DispatchQueue(label: "persona.html-security.loopback")
        private let lock = NSLock()
        private var ready = false
        private var failed = false
        private var connectionCount = 0
        private var received: [Request] = []
        private var active: [UUID: NWConnection] = [:]
        var connections: Int { lock.withLock { connectionCount } }
        var requests: [Request] { lock.withLock { received } }

        init() throws {
            let parameters = NWParameters.tcp
            parameters.requiredLocalEndpoint = .hostPort(host: "127.0.0.1", port: .any)
            listener = try NWListener(using: parameters)
            listener.stateUpdateHandler = { [weak self] state in
                self?.lock.withLock {
                    switch state {
                    case .ready: self?.ready = true
                    case .failed, .cancelled: self?.failed = true
                    default: break
                    }
                }
            }
            listener.newConnectionHandler = { [weak self] connection in
                guard let self else { connection.cancel(); return }
                let id = UUID()
                lock.withLock { connectionCount += 1; active[id] = connection }
                connection.start(queue: queue)
                receive(connection, id: id, data: Data())
                // An incomplete HTTP header cannot retain a socket indefinitely.
                queue.asyncAfter(deadline: .now() + 3) { [weak self] in self?.cancel(connection, id: id) }
            }
        }

        func start() async throws -> UInt16 {
            listener.start(queue: queue)
            for _ in 0..<200 {
                let state = lock.withLock { (ready, failed) }
                if state.0, let port = listener.port?.rawValue { return port }
                if state.1 { throw ProbeError.invalidResult }
                try await Task.sleep(for: .milliseconds(25))
            }
            throw ProbeError.timeout("start loopback listener")
        }

        func stop() {
            listener.cancel()
            let pending = lock.withLock { let value = Array(active.values); active.removeAll(); return value }
            for connection in pending { connection.cancel() }
        }

        private func cancel(_ connection: NWConnection, id: UUID) {
            lock.withLock { _ = active.removeValue(forKey: id) }
            connection.cancel()
        }

        private func receive(_ connection: NWConnection, id: UUID, data: Data) {
            connection.receive(minimumIncompleteLength: 1, maximumLength: 8192) { [weak self] chunk, _, complete, error in
                guard let self else { connection.cancel(); return }
                var data = data
                if let chunk { data.append(chunk) }
                guard data.count <= 8192 else { cancel(connection, id: id); return }
                if let text = String(data: data, encoding: .utf8), text.contains("\r\n\r\n") {
                    let parts = text.components(separatedBy: "\r\n")[0].split(separator: " ")
                    guard parts.count >= 2 else { cancel(connection, id: id); return }
                    let request = Request(method: String(parts[0]), path: String(parts[1]))
                    lock.withLock { received.append(request) }
                    let response = Self.response(for: request.path)
                    connection.send(content: response, completion: .contentProcessed { [weak self] _ in self?.cancel(connection, id: id) })
                } else if complete || error != nil { cancel(connection, id: id) }
                else { receive(connection, id: id, data: data) }
            }
        }

        private static func response(for path: String) -> Data {
            let body: String
            let mime: String
            let status: String
            let extra: String
            switch path {
            case "/redirect/start":
                body = ""; mime = "text/html"; status = "302 Found"; extra = "Location: /redirect/final\r\n"
            case "/redirect/final":
                body = "<p id='marker'>redirected</p>"; mime = "text/html"; status = "200 OK"; extra = ""
            case "/storage":
                body = "<p id='marker'>storage</p>"; mime = "text/html"; status = "200 OK"; extra = ""
            case "/style.css":
                body = "@import url('/import.css'); @font-face{font-family:probe;src:url('/font.woff2')} p{font-family:probe}"
                mime = "text/css"; status = "200 OK"; extra = ""
            case "/import.css":
                body = "p{color:rgb(30,30,30)}"; mime = "text/css"; status = "200 OK"; extra = ""
            case "/script.js":
                body = "window.externalScriptRan=true;"; mime = "application/javascript"; status = "200 OK"; extra = ""
            case "/object.html", "/embed.html":
                body = "<p>synthetic-object</p>"; mime = "text/html"; status = "200 OK"; extra = ""
            case "/video.mp4", "/audio.wav":
                // A definitive error finishes the media preload; these probes test requests, not media decoding.
                body = ""; mime = path.hasSuffix(".mp4") ? "video/mp4" : "audio/wav"; status = "404 Not Found"; extra = ""
            case "/vector.svg", "/poster.svg":
                body = "<svg xmlns='http://www.w3.org/2000/svg' width='64' height='64'><rect width='64' height='64'/></svg>"
                mime = "image/svg+xml"; status = "200 OK"; extra = ""
            default:
                body = "synthetic-resource"; mime = "application/octet-stream"; status = "200 OK"; extra = ""
            }
            return Data(("HTTP/1.1 \(status)\r\nContent-Type: \(mime)\r\nContent-Length: \(body.utf8.count)\r\n" +
                         "Access-Control-Allow-Origin: *\r\nCache-Control: no-store\r\nConnection: close\r\n\(extra)\r\n\(body)").utf8)
        }
    }
}
