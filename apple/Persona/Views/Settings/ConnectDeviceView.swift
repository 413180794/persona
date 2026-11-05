import SwiftUI

// 设置 › 设备 › 连接新设备 (#3077, after the web's #2731): a one-time pairing code from the hub
// (POST /api/v1/auth/pair-codes, docs/protocol.md「临时配对」) for another app (its QR code, the code itself and the
// pairing link, which the Mac and Android paste) or for a browser (the sign-in link and its QR code, which a phone's
// camera opens). The code lasts ten minutes and is traded once; the sheet says which device took it.

/// What the code is for: another Persona App, or a browser.
enum ConnectTarget: String, CaseIterable, Identifiable, Sendable {
    case app, web
    var id: String { rawValue }
    var title: String { self == .app ? "Persona App" : "浏览器" }
}

/// The words and arithmetic, kept apart from the view to test.
enum ConnectDeviceText {
    /// Whole seconds the code has left at `now`; 0 once it has expired.
    static func secondsLeft(_ expiresAt: Date, now: Date) -> Int {
        max(0, Int(expiresAt.timeIntervalSince(now).rounded(.up)))
    }

    /// m:ss, for the countdown.
    static func clock(_ seconds: Int) -> String {
        let s = max(0, seconds)
        return "\(s / 60):" + String(format: "%02d", s % 60)
    }

    /// The page the browser sign-in link opens: the hub serves the web at its own address (docs/web.md), under the
    /// sub-path a reverse proxy puts it at. Nil for anything but http(s).
    static func webBase(_ hub: URL) -> String? {
        guard let scheme = hub.scheme?.lowercased(), scheme == "http" || scheme == "https",
              var c = URLComponents(url: hub, resolvingAgainstBaseURL: false) else { return nil }
        c.query = nil
        c.fragment = nil
        c.user = nil
        c.password = nil
        if !c.path.hasSuffix("/") { c.path += "/" }
        return c.string
    }

    /// Whether only this device can open the hub's address (an SSH tunnel's 127.0.0.1): a link to it does nothing
    /// on another device.
    static func loopbackOnly(_ hub: URL) -> Bool {
        guard let host = hub.host?.lowercased() else { return false }
        return host == "localhost" || host.hasSuffix(".localhost") || host == "::1" || host == "[::1]" || host.hasPrefix("127.")
    }

    /// The device that paired with the code: a credential in use that was not there when the code was made.
    static func pairedDevice(known: Set<String>, _ tokens: [APIClient.DeviceToken]) -> APIClient.DeviceToken? {
        tokens.first { $0.revokedAt == nil && !known.contains($0.id) }
    }

    static func hint(_ t: ConnectTarget) -> String {
        t == .app ? "在新设备的 Persona 配对页扫这个码。Mac 和 Android 也可以拷贝配对链接后粘贴。"
            : "用手机相机扫码直接在浏览器里打开，或把登录链接拷到另一台电脑的浏览器。"
    }
}

/// 设置 › 设备 › 连接新设备: makes the code, counts it down and watches the device list for the one that used it.
@Observable final class ConnectDeviceModel {
    struct Deps {
        var api: () -> APIClient?
        var now: () -> Date = { .now }
        static var live: Deps { .init(api: { AppModel.shared.api }) }
    }

    enum Phase: Equatable {
        case loading
        case failed(String)
        case showing(APIClient.PairCode)
        case paired(String)
    }

    private(set) var phase: Phase = .loading
    /// The clock the countdown reads; `tick()` moves it on.
    private(set) var now: Date
    /// The hub's address this device uses, for the browser link's warning.
    private(set) var hubURL: URL?
    @ObservationIgnored private var known: Set<String> = []
    @ObservationIgnored private let deps: Deps

    init(deps: Deps = .live) {
        self.deps = deps
        now = deps.now()
    }

    var code: APIClient.PairCode? {
        if case .showing(let c) = phase { return c }
        return nil
    }

    var secondsLeft: Int { code.map { ConnectDeviceText.secondsLeft($0.expiresAt, now: now) } ?? 0 }
    var expired: Bool { code != nil && secondsLeft == 0 }
    /// Counting down and waiting for the other device.
    var live: Bool { code != nil && secondsLeft > 0 }

    /// A new code (also after it expired, and for another device after one paired).
    func make() async {
        phase = .loading
        do {
            guard let api = deps.api() else { throw APIError(status: 0, message: "还没连接 hub") }
            hubURL = api.config.url
            // The credentials there are now: the one that shows up later is the device that used this code.
            known = Set(try await api.deviceTokens().tokens.map(\.id))
            let c = try await api.pairCode(web: ConnectDeviceText.webBase(api.config.url))
            now = deps.now()
            phase = .showing(c)
        } catch {
            phase = .failed(error.localizedDescription)
        }
    }

    func tick() { now = deps.now() }

    /// Reads the device list once; true when a new device has taken the code (the caller reloads its list).
    @discardableResult func poll() async -> Bool {
        guard live, let api = deps.api(), let tokens = try? await api.deviceTokens().tokens,
              let d = ConnectDeviceText.pairedDevice(known: known, tokens) else { return false }
        guard live else { return false }
        phase = .paired(d.name.isEmpty ? d.id : d.name)
        return true
    }

    /// The countdown each second and the device list every three, until the code is used or expires.
    func run(onPaired: @escaping @MainActor () -> Void) async {
        var n = 0
        while !Task.isCancelled {
            try? await Task.sleep(for: .seconds(1))
            guard !Task.isCancelled else { return }
            tick()
            guard live else { continue }
            n += 1
            if n % 3 == 0, await poll() { onPaired() }
        }
    }
}

/// The sheet 连接新设备 opens.
struct ConnectDeviceSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var page = ConnectDeviceModel()
    @State private var target: ConnectTarget = .app
    private let onPaired: () -> Void

    /// `onPaired`: a device took the code (the list behind reloads).
    init(onPaired: @escaping () -> Void = {}) { self.onPaired = onPaired }

    var body: some View {
        NavigationStack {
            SettingsForm {
                if case .paired = page.phase {} else {
                    Section {
                        Picker("连接什么", selection: $target) {
                            ForEach(ConnectTarget.allCases) { Text($0.title).lineLimit(1).tag($0) }
                        }
                        .pickerStyle(.segmented)
                        .labelsHidden()
                        .accessibilityIdentifier("connectDevice.target")
                    }
                }
                content
            }
            .themedPage()
            .navigationTitle("连接新设备")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() }.lineLimit(1) }
            }
        }
        .presentationDetents([.large])
        .presentationDragIndicator(.visible)
        .task { await page.make() }
        #if DEBUG
        .onAppear { if UserDefaults.standard.string(forKey: "openConnectDevice") == "web" { target = .web } }
        #endif
        .task(id: page.code) {
            guard page.code != nil else { return }
            await page.run { onPaired() }
        }
    }

    @ViewBuilder private var content: some View {
        switch page.phase {
        case .loading:
            Section { ProgressView().frame(maxWidth: .infinity) }
        case .failed(let message):
            Section {
                Button { Task { await page.make() } } label: {
                    Label("重试", systemImage: Theme.Icon.reload).lineLimit(1)
                }
                .accessibilityIdentifier("connectDevice.retry")
            } header: {
                Text(message).foregroundStyle(Theme.danger).textCase(nil)
            }
        case .paired(let name):
            Section {
                VStack(spacing: Theme.Space.s) {
                    Image(systemName: Theme.Icon.successFilled)
                        .font(Theme.Font.display)
                        .foregroundStyle(Theme.positive)
                    Text("已连接：\(name)").font(Theme.Font.heading).lineLimit(1)
                    Text("它已经有了自己的凭据，可以在设备列表里单独撤销。")
                        .font(Theme.Font.secondary).foregroundStyle(.secondary).multilineTextAlignment(.center)
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, Theme.Space.l)
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("connectDevice.paired")
                Button { Task { await page.make() } } label: {
                    Label("再连一台", systemImage: Theme.Icon.pairing).lineLimit(1)
                }
            }
        case .showing(let code):
            if page.expired {
                Section {
                    Button { Task { await page.make() } } label: {
                        Label("重新生成", systemImage: Theme.Icon.reload).lineLimit(1)
                    }
                    .accessibilityIdentifier("connectDevice.renew")
                } header: {
                    Text("这个码已经过期。").textCase(nil)
                }
            } else {
                showing(code)
            }
        }
    }

    @ViewBuilder private func showing(_ code: APIClient.PairCode) -> some View {
        let qr = target == .app ? code.qr : code.webQR
        let link = target == .app ? code.pairLink : code.webURL
        Section {
            VStack(spacing: Theme.Space.m) {
                if let qr {
                    QRCodeView(rows: qr.rows)
                        .scaledFrame(width: Theme.Size.box240, height: Theme.Size.box240)
                        .accessibilityLabel(target == .app ? "App 配对二维码" : "网页登录二维码")
                        .accessibilityIdentifier("connectDevice.qr")
                }
                Text(ConnectDeviceText.hint(target))
                    .font(Theme.Font.secondary).foregroundStyle(.secondary).multilineTextAlignment(.center)
                if target == .web, let hub = page.hubURL, ConnectDeviceText.loopbackOnly(hub) {
                    Text("这个地址只有这台设备能打开。要在别的设备上登录，先换成 hub 的地址连接，再生成。")
                        .font(Theme.Font.caption).foregroundStyle(Theme.caution).multilineTextAlignment(.center)
                }
                Text("\(ConnectDeviceText.clock(page.secondsLeft)) 后失效，只能用一次")
                    .font(Theme.Font.caption.monospacedDigit()).foregroundStyle(.secondary).lineLimit(1)
                    .accessibilityIdentifier("connectDevice.countdown")
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, Theme.Space.s)
        }
        Section {
            if target == .app {
                LabeledContent("临时码") {
                    Text(code.token).font(Theme.Font.code).lineLimit(1).truncationMode(.middle).textSelection(.enabled)
                }
                .accessibilityIdentifier("connectDevice.code")
            }
            if let link {
                Button { copy(link, said: target == .app ? "已拷贝配对链接" : "已拷贝登录链接") } label: {
                    Label(target == .app ? "拷贝配对链接" : "拷贝登录链接", systemImage: Theme.Icon.copy).lineLimit(1)
                }
                .accessibilityIdentifier("connectDevice.copy")
            }
        }
    }

    private func copy(_ text: String, said: String) {
        UIPasteboard.general.string = text
        model.toast = Toast(said, style: .success)
    }
}
