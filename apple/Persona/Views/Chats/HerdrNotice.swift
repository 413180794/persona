import SwiftUI

// 「MacBook 的 herdr 没在运行」 with 启动, at the top of the Agents list (#1916, docs/engines.md「herdr 没在运行时」): told
// before a message to an agent there fails, not after. Only classic herdr (engines herdr and herdr-both): Herdr App's
// herdr-core has no server for the node to start.

/// Classic herdr's server on one machine, as its hub answers `GET /api/v1/machines/{mid}/herdr`. `known` false: the
/// node could not say (offline, older, or driving Herdr App), which is not "it does not run".
nonisolated struct HerdrState: Decodable, Equatable, Sendable {
    var autostart: Bool
    var running: Bool
    var known: Bool
}

nonisolated extension APIClient {
    /// Whether herdr's server answers on the machine now (and the 需要时自动启动 switch).
    func herdr(machine: String) async throws -> HerdrState {
        try await get("/api/v1/machines/\(machine)/herdr", as: HerdrState.self)
    }

    /// The row's 启动: the node starts herdr's server whatever the switch says. The hub waits up to 40 s for it.
    func startHerdr(machine: String) async throws -> HerdrState {
        try await post("/api/v1/machines/\(machine)/herdr/start", timeout: 45, as: HerdrState.self)
    }
}

/// Why a 启动 did not work: what the row says, in words, and what 复制详情 copies (the hub's own text, its status and
/// request id), kept out of the row.
nonisolated struct HerdrFailure: Equatable, Sendable {
    var text: String
    var detail: String

    init(text: String, detail: String) {
        self.text = text
        self.detail = detail
    }

    init(_ error: Error) {
        guard let e = error as? APIError else {
            self.init(text: error.localizedDescription, detail: String(describing: error))
            return
        }
        var detail = e.message + "\nHTTP \(e.status)"
        if let id = e.requestID { detail += String(localized: "\n请求 \(id)") }
        self.init(text: Self.says(e.message), detail: detail)
    }

    /// The node's and the hub's known failures of herdr.server.start (server/internal/node/herdrstart.go) in words;
    /// anything else as it came.
    static func says(_ message: String) -> String {
        let m = message.trimmingCharacters(in: .whitespacesAndNewlines)
        if let r = m.range(of: "herdr server stopped right after starting") {
            let why = m[r.upperBound...]
            if let code = (why.firstMatch(of: /exit status (-?\d+)/)?.1).map(String.init) { return String(localized: "herdr 起来后马上退出了（退出码 \(code)）") }
            if let sig = (why.firstMatch(of: /signal: ([\w ]+)/)?.1).map(String.init) { return String(localized: "herdr 起来后马上被结束了（\(sig)）") }
            return String(localized: "herdr 起来后马上退出了")
        }
        if m.contains("herdr is not installed") { return String(localized: "这台电脑上没装 herdr（PATH 和登录 shell 里都找不到 herdr 命令）") }
        if m.contains("herdr did not answer on") {
            if let s = (m.firstMatch(of: /within (\d+)s/)?.1).map(String.init) { return String(localized: "herdr 启动了，但 \(s) 秒内没有应答") }
            return String(localized: "herdr 启动了，但一直没有应答")
        }
        if m.contains("start herdr server:") { return String(localized: "没能运行 herdr 命令") }
        if m.contains("unknown method herdr.server.start") { return String(localized: "这台电脑上的 Persona 版本太旧，不会启动 herdr，更新后再试") }
        if m.contains("machine is offline") { return String(localized: "这台电脑现在离线") }
        if APIError.isTimeoutText(m) { return String(localized: "等了很久 herdr 还没起来，稍后再看看") }
        return m
    }
}

/// Which machines' herdr does not run, read while the Agents list is in front (`AgentsList`), and the starts made
/// from its rows. Its own object so a read that changes nothing redraws nothing, and a start only redraws its row.
@Observable final class HerdrWatch {
    /// How often the list asks while it is in front.
    static let interval: Duration = .seconds(10)

    /// The machines whose herdr is known not to run, as last read.
    private(set) var stopped: Set<MachineRef> = []
    /// 启动 pressed and not answered yet.
    private(set) var starting: Set<MachineRef> = []
    /// Why the last 启动 did not work (shown under the row until it is pressed again).
    private(set) var failures: [MachineRef: HerdrFailure] = [:]
    /// Their hub has no such row (older than the row): not asked again.
    @ObservationIgnored private var unsupported: Set<MachineRef> = []
    /// Started from here, and the node said it answers. A hub that only knew what the node said when it connected went
    /// on saying "not running" until the node reconnected: the start's answer stands until the hub says running too.
    @ObservationIgnored private var started: Set<MachineRef> = []

    /// A machine the row can be about: online, on classic herdr.
    nonisolated static func watched(_ m: Machine) -> Bool {
        m.online && (m.engine == .herdr || m.engine == .herdrBoth)
    }

    /// What starts the reading again: the machines it is about, not each heartbeat of theirs.
    nonisolated static func key(_ machines: [Machine]) -> [String] {
        machines.filter(watched).map { "\($0.ref.path) \($0.engine.rawValue)" }.sorted()
    }

    /// Asks each watched machine's hub. A request that fails keeps what the row showed (one lost poll must not flash
    /// it); a machine no longer watched (offline, another engine) loses its row and its start.
    func refresh(_ machines: [Machine], api: (String) -> APIClient?) async {
        let watched = machines.filter(Self.watched)
        let refs = Set(watched.map(\.ref))
        started.formIntersection(refs)
        var next = stopped.intersection(refs)
        for m in watched where !unsupported.contains(m.ref) {
            guard let client = api(m.group) else { continue }
            let state: HerdrState
            do {
                state = try await client.herdr(machine: m.id)
            } catch let e as APIError where e.status == 404 {
                unsupported.insert(m.ref)
                next.remove(m.ref)
                continue
            } catch {
                if Task.isCancelled { return }
                continue
            }
            // A start made while this was asked answers for itself (`start`).
            if starting.contains(m.ref) { continue }
            if state.running { started.remove(m.ref) } // the hub knows now: a later stop shows again
            if state.known && !state.running && !started.contains(m.ref) { next.insert(m.ref) } else { next.remove(m.ref) }
        }
        if next != stopped { withAnimation(Theme.Motion.state) { stopped = next } }
    }

    /// The row's 启动. Not cancelled with the list: the start goes on when the row is scrolled away.
    func start(_ m: Machine, api: APIClient) async {
        guard !starting.contains(m.ref) else { return }
        starting.insert(m.ref)
        failures[m.ref] = nil
        defer { starting.remove(m.ref) }
        do {
            let state = try await api.startHerdr(machine: m.id)
            if state.running {
                started.insert(m.ref)
                withAnimation(Theme.Motion.state) { _ = stopped.remove(m.ref) }
            } else {
                failures[m.ref] = HerdrFailure(text: String(localized: "叫它启动了，但 herdr 还没有应答"), detail: "herdr.server.start: running false")
            }
        } catch {
            failures[m.ref] = HerdrFailure(error)
        }
    }
}

/// Reads `HerdrWatch` every `interval` while the list is in front and not covered by a chat; the machines' heartbeats
/// do not start it over (`HerdrWatch.key`).
struct HerdrPolling: ViewModifier {
    @Environment(AppModel.self) private var model
    let watch: HerdrWatch
    /// `HerdrWatch.key` of the list's machines.
    let key: [String]
    let covered: Bool

    func body(content: Content) -> some View {
        let id: [String] = [String(covered)] + key
        content.tabActiveTask(id: id) {
            guard !covered else { return }
            while !Task.isCancelled {
                await watch.refresh(model.machines) { model.api($0) }
                try? await Task.sleep(for: HerdrWatch.interval)
            }
        }
    }
}

/// One machine's 「herdr 没在运行」 row: 启动, then 正在启动…, and why when it did not start.
struct HerdrNoticeRow: View {
    @Environment(AppModel.self) private var model
    let machine: Machine
    let watch: HerdrWatch

    var body: some View {
        let starting = watch.starting.contains(machine.ref)
        HStack(spacing: Theme.Space.s) {
            Group {
                if starting { ProgressView().controlSize(.mini) } else { Image(systemName: Theme.Icon.error) }
            }
            .foregroundStyle(Theme.caution)
            // Square: the Mac's spinner asks for more height than it draws, and the row grew by a line while it starts.
            .scaledFrame(width: Theme.Size.iconS, height: Theme.Size.iconS)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(starting ? "正在启动 \(machine.title) 的 herdr…" : "\(machine.title) 的 herdr 没在运行")
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                if !starting, let failure = watch.failures[machine.ref] {
                    Text("没启动：\(failure.text)")
                        .foregroundStyle(Theme.danger)
                        .lineLimit(3)
                        .accessibilityIdentifier("herdrNotice.failure")
                        // The hub's own words and the request id, for a report: long press / right-click.
                        .contextMenu {
                            Button {
                                UIPasteboard.general.string = failure.detail
                                Haptics.success()
                            } label: { Label("复制详情", systemImage: Theme.Icon.copy).lineLimit(1) }
                        }
                }
            }
            Spacer(minLength: Theme.Space.s)
            // Kept while it starts, unseen: the row keeps its height (on the Mac it is over the list, which moved when
            // the row did).
            Button("启动") {
                Haptics.tap()
                press()
            }
            .lineLimit(1)
            .buttonStyle(.borderless) // in a list row: the button takes only its own taps
            .opacity(starting ? 0 : 1)
            .disabled(starting)
            .accessibilityHidden(starting)
            .accessibilityIdentifier("herdrNotice.start")
        }
        .font(Theme.Font.secondary)
        .accessibilityElement(children: .contain) // or the row's id hides its button's (docs/lessons/ios.md)
        .accessibilityIdentifier("agents.herdrNotice")
        #if DEBUG
        // `-showHerdrStart YES`: the first row shown presses its 启动 by itself, once (screenshots of 正在启动… and of a
        // failure, with the mock's MOCK_HERDR_START=slow / fail).
        .task {
            guard UserDefaults.standard.bool(forKey: "showHerdrStart"), !Self.pressedForShots else { return }
            Self.pressedForShots = true
            press()
        }
        #endif
    }

    private func press() {
        guard let api = model.api(machine.group) else { return }
        Task { await watch.start(machine, api: api) }
    }

    #if DEBUG
    private static var pressedForShots = false
    #endif
}
