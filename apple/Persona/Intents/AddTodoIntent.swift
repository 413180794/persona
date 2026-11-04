import AppIntents
import Foundation

/// "新建待办": writes a todo (docs/todos.md) straight to the hub, so Siri, the Action button or a shortcut can note
/// something down without opening the app. It goes to the hub group the app has selected, where the todos page reads.
struct AddTodoIntent: AppIntent {
    nonisolated static let title: LocalizedStringResource = "新建待办"
    nonisolated static let description = IntentDescription(
        "在 Persona 里记一条待办，之后可以在待办页交给 Agent 去做。",
        categoryName: "待办",
        searchKeywords: ["待办", "todo", "to-do", "任务", "记一下", "提醒"]
    )
    nonisolated static var supportedModes: IntentModes { .background }

    @Parameter(title: "标题", requestValueDialog: "要记什么待办？")
    var todoTitle: String

    @Parameter(title: "说明", description: "交给 Agent 时作为上下文一起发出", inputOptions: String.IntentInputOptions(multiline: true))
    var notes: String?

    nonisolated init() {}
    nonisolated init(todoTitle: String, notes: String? = nil) {
        self.todoTitle = todoTitle
        self.notes = notes
    }

    nonisolated static var parameterSummary: some ParameterSummary {
        Summary("新建待办 \(\.$todoTitle)") { \.$notes }
    }

    nonisolated func perform() async throws -> some IntentResult & ProvidesDialog & ReturnsValue<String> {
        let api: APIClient
        do {
            api = try IntentSupport.client(group: UserDefaults.standard.string(forKey: IntentText.hubGroupKey) ?? "")
        } catch {
            throw IntentFailureSentence(TodoIntentText.failure(error))
        }
        let todo = try await AddTodoFlow.add(api, title: todoTitle, notes: notes)
        return .result(value: todo.title, dialog: IntentSupport.dialog(TodoIntentText.added(todo.title)))
    }
}

/// The network part of AddTodoIntent, separate from the pairing so the tests can hand it a FakeHub client.
nonisolated enum AddTodoFlow {
    /// Creates the todo; every failure comes back as one sentence Siri can read.
    static func add(_ api: APIClient, title: String, notes: String?) async throws -> TodoItem {
        guard let title = TodoIntentText.cleaned(title) else { throw IntentFailureSentence(TodoIntentText.askTitle) }
        do {
            return try await api.createTodo(title: title, notes: TodoIntentText.cleaned(notes))
        } catch {
            throw IntentFailureSentence(TodoIntentText.failure(error))
        }
    }
}

/// Wording for AddTodoIntent. Pure, so SiriIntentsTests can pin it.
nonisolated enum TodoIntentText {
    static let askTitle = "要记什么待办？"
    static let oldHub = "hub 的版本还不支持待办，先更新 hub。"

    /// Trimmed text, or nil when nothing is left.
    static func cleaned(_ text: String?) -> String? {
        guard let t = text?.trimmingCharacters(in: .whitespacesAndNewlines), !t.isEmpty else { return nil }
        return t
    }

    static func added(_ title: String) -> String {
        "已记下待办：「\(IntentText.excerpt(title, limit: 60))」"
    }

    static func failure(_ error: Error) -> String {
        if TodosLogic.isOldHub(error) { return oldHub }
        return IntentFailure.sentence(for: error, host: IntentSupport.host())
    }
}
