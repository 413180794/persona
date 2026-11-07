import AppKit
import SwiftUI
import Testing
@testable import Persona

/// Offscreen native snapshot acceptance against the actual isolated source hub. This is not
/// XCUITest/window-server capture and does not claim GUI gesture or focus acceptance.
@MainActor @Suite(.serialized) struct ExtensionStreamSnapshotTests {
    @Test(.enabled(if: ProcessInfo.processInfo.environment["PERSONA_STREAM_TEST_PAIR"] != nil))
    func realShowcaseOffscreenLightDarkAndNativeFence() async throws {
        let env = ProcessInfo.processInfo.environment
        let pair = try #require(env["PERSONA_STREAM_TEST_PAIR"])
        let parts = try #require(URLComponents(string: pair))
        let address = try #require(parts.queryItems?.first { $0.name == "url" }?.value)
        let url = try #require(URL(string: address))
        #expect(["127.0.0.1", "localhost"].contains(url.host() ?? ""))
        guard ["127.0.0.1", "localhost"].contains(url.host() ?? "") else { throw ExtensionStreamJSON.Failure.unsupported }
        let token = try #require(parts.queryItems?.first { $0.name == "token" }?.value)
        let config = ServerConfig(url: url, token: token)
        let connection = LiveConnection()
        connection.usesExtensionUIStream = true
        let page = ExtensionViewModel(plugin: "showcase", ui: "showcase", context: .here().forPage(), state: #"{"p":"button"}"#,
            deps: .init(api: { APIClient(config: config) }, connection: { connection }))
        defer { page.stream?.close(); connection.disconnect() }
        page.setVisible(true)
        connection.connect(config)
        await page.load()
        let deadline = Date().addingTimeInterval(25)
        while page.stream?.ready != true && Date() < deadline { try await Task.sleep(for: .milliseconds(100)) }
        #expect(connection.extensionStreamFeatures?.contains(ExtensionPageStream.feature) == true)
        #expect(page.stream?.supported == true && page.stream?.ready == true)
        let tree = try #require(page.stream?.tree)
        let view = try #require(page.view)
        #expect(tree.version == 1)
        print("Mac offscreen snapshot: authenticated native stream; snapshot v1; no HTTP fallback")
        let directory = try #require(env["PERSONA_STREAM_SHOTS"])
        let output = URL(fileURLWithPath: directory)
        try FileManager.default.createDirectory(at: output, withIntermediateDirectories: true)
        for (name, scheme) in [("light", ColorScheme.light), ("dark", ColorScheme.dark)] {
            let size = CGSize(width: 900, height: 680)
            let content = VStack(alignment: .leading, spacing: Theme.Space.l) {
                Text(view.title ?? "Showcase").font(Theme.Font.title).foregroundStyle(Color.primary)
                ExtensionViewStack(view: view, model: page, link: .constant(nil))
                Spacer(minLength: Theme.Space.flush)
            }
            .padding(Theme.Space.page)
            .frame(width: size.width, height: size.height, alignment: .topLeading)
            .background(Theme.pageBackground)
            .environment(AppModel.shared)
            .environment(\.colorScheme, scheme)
            .environment(\.extensionDisplayedRevision, page.stream?.displayedRevision)
            let hosting = NSHostingView(rootView: content)
            hosting.frame = NSRect(origin: .zero, size: size)
            hosting.appearance = NSAppearance(named: scheme == .dark ? .darkAqua : .aqua)
            hosting.layoutSubtreeIfNeeded()
            try await Task.sleep(for: .milliseconds(200))
            let bitmap = try #require(hosting.bitmapImageRepForCachingDisplay(in: hosting.bounds))
            hosting.cacheDisplay(in: hosting.bounds, to: bitmap)
            let png = try #require(bitmap.representation(using: .png, properties: [:]))
            #expect(png.count > 1_000)
            try png.write(to: output.appending(path: "mac-showcase-offscreen-\(name).png"))
            print("Mac offscreen \(name): NSHostingView PNG rendered from live showcase stream")
        }
        let button = try #require(view.sections.flatMap(\.items).first { $0.key == "button-button-item-0-0" })
        guard case .button(let component) = button else { throw ExtensionStreamJSON.Failure.unsupported }
        let execute = page.prepared(component.action, displayed: page.stream?.displayedRevision)
        let result = await execute()
        #expect(result.error == nil && !result.cancelled && result.toast?.isEmpty == false)
        #expect(page.stream?.tree?.version == 2)
        #expect(page.stream?.tree?.view == tree.view)
        print("Mac native callback: empty +1 fence v2 applied before accepted toast receipt")
        guard case .button(let dangerous) = view.sections[0].items[1] else { throw ExtensionStreamJSON.Failure.unsupported }
        let approve = page.prepared(dangerous.action, displayed: page.stream?.displayedRevision)
        let approval = Task { await approve() }
        for _ in 0..<10 where page.confirmation == nil { await Task.yield() }
        #expect(page.confirmation?.action == dangerous.action)
        page.approveConfirmation()
        let confirmed = await approval.value
        #expect(confirmed.error == nil && confirmed.toast?.isEmpty == false && page.stream?.tree?.version == 3)
        print("Mac native confirmation: explicit frozen consent resolved the matching host gate; callback fenced v3")
        page.setVisible(false)
        #expect(page.stream?.ready == false && page.stream?.tree?.version == 3)
        connection.disconnect()
        connection.connect(config)
        page.setVisible(true)
        let resumed = Date().addingTimeInterval(25)
        while page.stream?.ready != true && Date() < resumed { try await Task.sleep(for: .milliseconds(100)) }
        #expect(page.stream?.ready == true && page.stream?.tree?.session == tree.session)
        #expect(page.stream?.tree?.version == 3)
        print("Mac native reconnect: owned session resumed by full snapshot")
    }

    @Test(.enabled(if: ProcessInfo.processInfo.environment["PERSONA_STREAM_TEST_PAIR"] != nil))
    func realLegacyFullSnapshotAndSavedCallbackRemainAvailable() async throws {
        let parts = try #require(URLComponents(string: ProcessInfo.processInfo.environment["PERSONA_STREAM_TEST_PAIR"] ?? ""))
        let address = try #require(parts.queryItems?.first { $0.name == "url" }?.value)
        let url = try #require(URL(string: address))
        guard ["127.0.0.1", "localhost"].contains(url.host() ?? "") else { throw ExtensionStreamJSON.Failure.unsupported }
        let token = try #require(parts.queryItems?.first { $0.name == "token" }?.value)
        let api = APIClient(config: ServerConfig(url: url, token: token))
        let page = ExtensionViewModel(plugin: "showcase", ui: "showcase", context: .here().forPage(), state: #"{"p":"button"}"#,
            deps: .init(api: { api }))
        await page.load()
        #expect(page.stream == nil)
        let view = try #require(page.view)
        guard case .button(let button) = view.sections[0].items[0] else { throw ExtensionStreamJSON.Failure.unsupported }
        let result = await page.perform(button.action)
        #expect(result.error == nil && result.toast?.isEmpty == false)
        print("Mac legacy path: actual authenticated HTTP full snapshot and saved opaque callback passed")
    }

    @Test(.enabled(if: ProcessInfo.processInfo.environment["PERSONA_STREAM_TEST_PAIR"] != nil))
    func fieldDraftSurvivesActualReconnectSnapshot() async throws {
        let parts = try #require(URLComponents(string: ProcessInfo.processInfo.environment["PERSONA_STREAM_TEST_PAIR"] ?? ""))
        let address = try #require(parts.queryItems?.first { $0.name == "url" }?.value)
        let url = try #require(URL(string: address))
        guard ["127.0.0.1", "localhost"].contains(url.host() ?? "") else { throw ExtensionStreamJSON.Failure.unsupported }
        let token = try #require(parts.queryItems?.first { $0.name == "token" }?.value)
        let config = ServerConfig(url: url, token: token)
        let connection = LiveConnection(); connection.usesExtensionUIStream = true
        let page = ExtensionViewModel(plugin: "showcase", ui: "showcase", context: .here().forPage(), state: #"{"p":"field"}"#,
            deps: .init(api: { APIClient(config: config) }, connection: { connection }))
        defer { page.stream?.close(); connection.disconnect() }
        page.setVisible(true); connection.connect(config); await page.load()
        let deadline = Date().addingTimeInterval(25)
        while page.stream?.ready != true && Date() < deadline { try await Task.sleep(for: .milliseconds(100)) }
        let tree = try #require(page.stream?.tree)
        #expect(page.values["empty"] == "")
        page.values["empty"] = "Synthetic retained draft"
        connection.disconnect()
        #expect(page.stream?.ready == false)
        connection.connect(config)
        let resumed = Date().addingTimeInterval(25)
        while page.stream?.ready != true && Date() < resumed { try await Task.sleep(for: .milliseconds(100)) }
        #expect(page.stream?.ready == true && page.stream?.tree?.session == tree.session)
        #expect(page.values["empty"] == "Synthetic retained draft")
        print("Mac native fields: actual reconnect snapshot retained a declared nonsecret draft")
    }

    @Test(.enabled(if: ProcessInfo.processInfo.environment["PERSONA_STREAM_TEST_PAIR"] != nil))
    func systemAccessoryFamiliesRemainFullHTTPSnapshots() async throws {
        let parts = try #require(URLComponents(string: ProcessInfo.processInfo.environment["PERSONA_STREAM_TEST_PAIR"] ?? ""))
        let address = try #require(parts.queryItems?.first { $0.name == "url" }?.value)
        let url = try #require(URL(string: address))
        guard ["127.0.0.1", "localhost"].contains(url.host() ?? "") else { throw ExtensionStreamJSON.Failure.unsupported }
        let token = try #require(parts.queryItems?.first { $0.name == "token" }?.value)
        let api = APIClient(config: ServerConfig(url: url, token: token))
        let place = try #require(try await api.extensionWidgets().first { $0.extension == "showcase" })
        for family in ["accessory_circular", "accessory_rectangular", "accessory_inline"] {
            let answer = try await api.extensionWidget(place, family: family)
            #expect(!answer.view.sections.isEmpty && answer.expiration != nil)
            #expect(answer.view.sections.flatMap(\.items).count == 1)
        }
        #expect(!LiveConnection.queryItems(EventCursor()).contains { $0.value?.contains(ExtensionPageStream.feature) == true })
        print("System accessory families: actual authenticated fitted HTTP snapshots; streaming not advertised")
    }

    @Test(.enabled(if: ProcessInfo.processInfo.environment["PERSONA_STREAM_TEST_PAIR"] != nil))
    func serviceContinuesWhilePageIsSuspended() async throws {
        let parts = try #require(URLComponents(string: ProcessInfo.processInfo.environment["PERSONA_STREAM_TEST_PAIR"] ?? ""))
        let address = try #require(parts.queryItems?.first { $0.name == "url" }?.value)
        let url = try #require(URL(string: address))
        guard ["127.0.0.1", "localhost"].contains(url.host() ?? "") else { throw ExtensionStreamJSON.Failure.unsupported }
        let token = try #require(parts.queryItems?.first { $0.name == "token" }?.value)
        let config = ServerConfig(url: url, token: token)
        let connection = LiveConnection(); connection.usesExtensionUIStream = true
        let page = ExtensionViewModel(plugin: "showcase", ui: "showcase", context: .here().forPage(), state: #"{"p":"invalidate"}"#,
            deps: .init(api: { APIClient(config: config) }, connection: { connection }))
        defer { page.stream?.close(); connection.disconnect() }
        page.setVisible(true); connection.connect(config); await page.load()
        let deadline = Date().addingTimeInterval(25)
        while page.stream?.ready != true && Date() < deadline { try await Task.sleep(for: .milliseconds(100)) }
        let view = try #require(page.view)
        let button = try #require(view.sections.flatMap(\.items).first { item in
            if case .button = item { return true }; return false
        })
        guard case .button(let component) = button else { throw ExtensionStreamJSON.Failure.unsupported }
        let execute = page.prepared(component.action, displayed: page.stream?.displayedRevision)
        let result = await execute()
        #expect(result.error == nil)
        let committed = try #require(page.stream?.tree)
        page.setVisible(false)
        try await Task.sleep(for: .seconds(4))
        #expect(page.stream?.tree == committed && page.stream?.ready == false)
        page.setVisible(true)
        let resumed = Date().addingTimeInterval(25)
        while page.stream?.ready != true && Date() < resumed { try await Task.sleep(for: .milliseconds(100)) }
        #expect(page.stream?.ready == true)
        #expect(page.view?.sections.flatMap(\.items).contains { item in
            if case .metric = item { return true }; return false
        } == true)
        #expect((page.stream?.tree?.version ?? 0) > committed.version)
        print("Mac native service: timer completed while hidden; resumed snapshot contains delivered data")
    }
}
