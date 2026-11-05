import AppIntents
import Foundation

/// "问 Claude": sends a message to the newest live session on a machine, or starts a new one.
struct AskClaudeIntent: AppIntent {
    nonisolated static let title: LocalizedStringResource = "问 Claude"
    nonisolated static let description = IntentDescription(
        "把你这句话发给某台机器上最近的会话，或者在 herdr 里新开一个。",
        categoryName: "对话",
        searchKeywords: ["Claude", "会话", "发送"]
    )
    /// The hub is plain HTTP over an SSH tunnel; the app does not need to come to the foreground. The result still
    /// carries `OpenSessionIntent`, so tapping the dialog's button opens the chat.
    nonisolated static var supportedModes: IntentModes { .background }

    @Parameter(title: "内容", requestValueDialog: "要跟 Claude 说什么？")
    var text: String

    @Parameter(title: "机器")
    var machine: MachineEntity?

    @Parameter(title: "新会话", default: false)
    var newSession: Bool

    nonisolated init() {}

    nonisolated init(text: String, machine: MachineEntity? = nil, newSession: Bool = false) {
        self.text = text
        self.machine = machine
        self.newSession = newSession
    }

    nonisolated static var parameterSummary: some ParameterSummary {
        When(\.$newSession, .equalTo, true) {
            Summary("在 \(\.$machine) 上新开对话问 \(\.$text)")
        } otherwise: {
            Summary("用 \(\.$machine) 问 \(\.$text)")
        }
    }

    nonisolated func perform() async throws -> some IntentResult & ProvidesDialog & OpensIntent {
        let body = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !body.isEmpty else { throw IntentFailureSentence(String(localized: "要跟 Claude 说什么？")) }

        do {
            let target = try await resolveMachine()
            // To the hub of the machine's group.
            let api = try IntentSupport.client(group: target.ref.group)
            let session = try await deliver(api, on: target, text: body)
            return .result(
                opensIntent: OpenSessionIntent(target: SessionEntity(session)),
                dialog: IntentSupport.dialog(IntentText.sent(to: session.title))
            )
        } catch {
            throw IntentFailureSentence(IntentFailure.sentence(for: error, host: IntentSupport.host()))
        }
    }

    /// An explicit machine wins; otherwise the one used last in the new-chat sheet, else the first online one.
    private func resolveMachine() async throws -> MachineEntity {
        if let machine { return machine }
        let machines = try await IntentSupport.machines()
        guard let picked = IntentPicker.machine(machines, preferred: UserDefaults.standard.string(forKey: IntentText.machineKey))
        else { throw IntentFailure.noMachine }
        return MachineEntity(picked)
    }

    private func deliver(_ api: APIClient, on machine: MachineEntity, text: String) async throws -> Session {
        guard machine.online else { throw IntentFailure.offline(machine.name) }
        if newSession { return try await startSession(api, on: machine, text: text) }

        let sessions = try await api.sessions()
        guard let session = IntentPicker.session(sessions, machine: machine.ref.id, group: machine.ref.group) else {
            throw IntentFailure.noSession(machine.name)
        }
        guard session.canSend else { throw IntentFailureSentence(session.sourceSubtitle ?? String(localized: "此会话仅可查看")) }
        try await api.send(session.key, text: text)
        return session
    }

    private func startSession(_ api: APIClient, on machine: MachineEntity, text: String) async throws -> Session {
        let defaults = UserDefaults.standard
        guard let launcher = defaults.string(forKey: IntentText.launcherKey(machine.id)), !launcher.isEmpty,
              let cwd = defaults.string(forKey: IntentText.cwdKey(machine.id)), !cwd.isEmpty
        else { throw IntentFailure.noLauncher(machine.name) }
        // The prompt rides along with the start request; sending it again would duplicate it.
        return try await api.start(.init(machineID: machine.ref.id, cwd: cwd, launcher: launcher, prompt: text, async: true))
    }
}
