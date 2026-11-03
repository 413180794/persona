import Foundation
import Testing
@testable import Jarvis

/// Pure logic behind the intents: wording, selection rules and what each interaction kind resolves to.
/// Everything here runs without a hub, which is why it is testable at all.
struct IntentsTests {
    // MARK: Fixtures

    // `Machine` and `Session` declare `init(from:)`, so the memberwise init is gone: build them from JSON,
    // exactly as they arrive from the hub.
    private func decode<T: Decodable>(_ json: String) -> T {
        do { return try JSON.decoder.decode(T.self, from: Data(json.utf8)) }
        catch { fatalError("bad fixture: \(error)") }
    }

    private func machine(_ id: String, online: Bool = true, name: String? = nil) -> Machine {
        decode("""
        {"id":"\(id)","name":"\(name ?? id)","kind":"mac","online":\(online)}
        """)
    }

    private func session(_ id: String, machine: String = "server", status: SessionStatus = .idle,
                         updated: TimeInterval = 0, title: String? = nil) -> Session {
        decode("""
        {"machine_id":"\(machine)","session_id":"\(id)","title":"\(title ?? id)","cwd":"/tmp/p",
         "status":"\(status.rawValue)","updated_at":"\(ISO8601DateFormatter().string(from: Date(timeIntervalSince1970: updated)))"}
        """)
    }

    private func interaction(_ kind: InteractionKind, created: TimeInterval = 0, tool: String? = nil,
                             summary: String? = nil, questions: [Question]? = nil,
                             plan: String? = nil) -> Interaction {
        Interaction(
            id: "ix_\(kind.rawValue)_\(Int(created))",
            machineID: "server",
            sessionID: "s1",
            sessionTitle: "修构建",
            cwd: "/tmp/p",
            kind: kind,
            createdAt: Date(timeIntervalSince1970: created),
            expiresAt: nil,
            toolName: tool,
            toolUseID: nil,
            questions: questions,
            plan: plan,
            toolInput: nil,
            summary: summary,
            screen: nil
        )
    }

    // MARK: Wording

    @Test func sentTextNamesTheSession() {
        #expect(IntentText.sent(to: "修构建") == "已发给 修构建")
        // An untitled session still reads as a sentence instead of "已发给 ".
        #expect(IntentText.sent(to: "  ") == "已发给 会话")
    }

    @Test func statusCountsAndFirstQuestion() {
        #expect(IntentText.status(working: 2, waiting: 0, first: nil) == "2 个在工作，0 个等你处理")
        let text = IntentText.status(working: 0, waiting: 1, first: "要我用 opus 还是 sonnet？")
        #expect(text.hasPrefix("0 个在工作，1 个等你处理："))
        #expect(text.contains("要我用 opus 还是 sonnet？"))
    }

    @Test func statusExcerptCollapsesWhitespaceAndClips() {
        #expect(IntentText.excerpt("a\n\n  b\tc") == "a b c")
        let long = String(repeating: "长", count: 200)
        let short = IntentText.excerpt(long, limit: 10)
        #expect(short.hasSuffix("…"))
        #expect(short.count == 11)
    }

    @Test func waitingPromptForEachKind() {
        let question = Question(question: "用哪个分支？", header: nil, multiSelect: false, options: [])
        #expect(IntentText.waitingPrompt(interaction(.question, questions: [question])) == "用哪个分支？")
        // No question text at all: fall back to the summary instead of saying nothing.
        #expect(IntentText.waitingPrompt(interaction(.question, summary: "问了点东西")) == "问了点东西")

        #expect(IntentText.waitingPrompt(interaction(.plan, plan: "1. 改 A\n2. 改 B")) == "计划：1. 改 A 2. 改 B")
        #expect(IntentText.waitingPrompt(interaction(.plan)) == "有个计划在等你批准")

        #expect(IntentText.waitingPrompt(interaction(.permission, summary: "git push origin master")) == "git push origin master")
        #expect(IntentText.waitingPrompt(interaction(.permission, tool: "Bash")) == "Bash 在申请授权")
        #expect(IntentText.waitingPrompt(interaction(.screen)) == "终端在等你输入")
    }

    @Test func approvedTexts() {
        #expect(IntentText.approvedPermission("Bash") == "已允许 Bash")
        #expect(IntentText.approvedPermission(nil) == "已允许")
        #expect(IntentText.approvedPlan == "已批准计划，开始执行")
        #expect(IntentText.needsApp(.question).contains("提问"))
        #expect(IntentText.needsApp(.screen).contains("终端"))
    }

    @Test func phrasesCarryTheAppName() {
        #expect(IntentPhrases.fill(IntentPhrases.askClaude, appName: "Persona") == "用 Persona 问 Claude")
        #expect(IntentPhrases.fill(IntentPhrases.status, appName: "Persona") == "Persona 状态")
        #expect(IntentPhrases.fill(IntentPhrases.approve, appName: "Persona") == "用 Persona 批准第一个")
        // Every template needs the placeholder, otherwise Siri cannot address the app by name.
        for template in [IntentPhrases.askClaude, IntentPhrases.status, IntentPhrases.approve] {
            #expect(template.contains("%@"))
        }
    }

    // MARK: Errors

    @Test func failureSentences() {
        #expect(IntentFailure.notPaired.sentence.contains("还没配对"))
        #expect(IntentFailure.noMachine.sentence.contains("没有在线的机器"))
        #expect(IntentFailure.offline("desktop").sentence.contains("desktop"))
        #expect(IntentFailure.noSession("server").sentence.contains("没有运行中的会话"))
        #expect(IntentFailure.noLauncher("server").sentence.contains("新对话"))
        #expect(IntentFailure.noPending.sentence.contains("没有等你处理"))
    }

    @Test func hubErrorsBecomeReadableSentences() {
        #expect(IntentFailure.sentence(for: APIError(status: 401, message: "unauthorized")).contains("重新配对"))
        #expect(IntentFailure.sentence(for: APIError(status: 410, message: "gone")).contains("已经处理过"))
        #expect(IntentFailure.sentence(for: APIError(status: 500, message: "boom")) == "boom")
        #expect(IntentFailure.sentence(for: URLError(.timedOut), host: "hub:18660").contains("hub:18660"))
        let offline = IntentFailure.sentence(for: URLError(.cannotConnectToHost), host: "hub:18660")
        #expect(offline.contains("连不上 hub"))
        #expect(offline.contains("VPN"))
        // A plain NSError from URLSession takes the same path as a URLError.
        #expect(IntentFailure.sentence(for: NSError(domain: NSURLErrorDomain, code: -1004)).contains("连不上 hub"))
        // Unknown errors keep their own message rather than being flattened.
        #expect(IntentFailure.sentence(for: IntentFailure.noPending) == IntentFailure.noPending.sentence)
    }

    // MARK: Selection

    @Test func machinePrefersTheRememberedOneAndFallsBackToOnline() {
        let machines = [machine("server", online: false), machine("laptop", online: true)]
        #expect(IntentPicker.machine(machines, preferred: "server")?.id == "server")
        #expect(IntentPicker.machine(machines, preferred: "gone")?.id == "laptop")
        #expect(IntentPicker.machine(machines, preferred: nil)?.id == "laptop")
        // Nobody online: still pick something, the intent reports it as offline.
        let allOffline = [machine("server", online: false)]
        #expect(IntentPicker.machine(allOffline, preferred: nil)?.id == "server")
        #expect(IntentPicker.machine([], preferred: "server") == nil)
    }

    @Test func sessionPicksNewestLiveOneOnTheMachine() {
        let sessions = [
            session("old", status: .idle, updated: 10),
            session("newest", status: .done, updated: 30),
            session("dead", status: .offline, updated: 99),
            session("elsewhere", machine: "laptop", status: .working, updated: 42),
        ]
        #expect(IntentPicker.session(sessions, machine: "server")?.sessionID == "newest")
        #expect(IntentPicker.session(sessions, machine: "desktop") == nil)
        #expect(IntentPicker.session([session("dead", status: .offline)], machine: "server") == nil)
    }

    @Test func pendingPicksTheOldestInteraction() {
        let list = [interaction(.permission, created: 30), interaction(.plan, created: 10)]
        #expect(IntentPicker.pending(list)?.kind == .plan)
        #expect(IntentPicker.pending([]) == nil)
    }

    // MARK: Approve

    @Test func onlyPermissionAndPlanCanBeResolvedFromSiri() {
        let permission = ApproveFirstIntent.resolution(for: interaction(.permission))
        #expect(permission?.allow == true)
        #expect(permission?.approve == nil)

        let plan = ApproveFirstIntent.resolution(for: interaction(.plan))
        #expect(plan?.approve == true)
        #expect(plan?.mode == "bypassPermissions")

        #expect(ApproveFirstIntent.resolution(for: interaction(.question)) == nil)
        #expect(ApproveFirstIntent.resolution(for: interaction(.screen)) == nil)
    }

    @Test func approveRepliesDescribeWhatHappened() {
        #expect(ApproveFirstIntent.doneText(for: interaction(.permission, tool: "Bash")) == "已允许 Bash")
        #expect(ApproveFirstIntent.doneText(for: interaction(.plan)) == IntentText.approvedPlan)
        #expect(ApproveFirstIntent.doneText(for: interaction(.question)).contains("打开 Persona"))
    }

    // MARK: Entities

    @Test func sessionEntityRoundTripsThroughItsIdentifier() {
        let entity = SessionEntity(session("s1", title: "修构建"))
        #expect(entity.id == "server/s1")
        #expect(entity.key == SessionKey(machineID: "server", sessionID: "s1"))
        #expect(SessionEntity(machineID: "server", sessionID: "s1", title: "").id == entity.id)
    }

    @Test func machineEntityCarriesTheOnlineFlag() {
        #expect(MachineEntity(machine("server", online: false)).online == false)
        #expect(MachineEntity(id: "server", name: "server").online == true)
    }
}
