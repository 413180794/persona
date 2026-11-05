import EventKit
import Foundation

/// Reminder tools: list, create and complete. Writes ask the user first.
enum RemindersTool {
    static var tools: [PhoneTool] {
        [
            PhoneTool(
                spec: ToolSpec(
                    name: "phone_reminders_list",
                    description: """
                    List reminders, by default only the incomplete ones. Returns id, title, due date \
                    (ISO 8601 with timezone; reminders with a date but no time are due at 00:00), \
                    completed flag, list name, priority (0 = none, 1–4 high, 5 medium, 6–9 low) \
                    and the first 300 characters of notes. Use the id with phone_reminders_complete.
                    """,
                    inputSchema: Schema.object([
                        "list": Schema.string("Only return reminders from this list (calendar) name."),
                        "include_completed": Schema.boolean("Include completed reminders. Default false."),
                        "limit": Schema.integer("Maximum number of reminders, default 50, max 200.", min: 1, max: 200),
                    ])
                ),
                source: .reminders,
                run: { args in try await list(args) }
            ),
            PhoneTool(
                spec: ToolSpec(
                    name: "phone_reminders_create",
                    description: """
                    Create a reminder. `due` is optional ISO 8601 with a timezone \
                    (e.g. 2026-10-24T18:00:00+08:00). priority is 0 (none, the default), 1–4 high, \
                    5 medium or 6–9 low. The user must approve the reminder on the phone. \
                    Returns the new reminder's id.
                    """,
                    inputSchema: Schema.object([
                        "title": Schema.string("Reminder title."),
                        "due": Schema.string("Optional due date, ISO 8601 with a timezone."),
                        "notes": Schema.string("Optional notes."),
                        "list": Schema.string("Reminder list name. Default: the default list for new reminders."),
                        "priority": Schema.integer("0 none (default), 1–4 high, 5 medium, 6–9 low.", min: 0, max: 9),
                    ], required: ["title"])
                ),
                source: .reminders,
                confirmation: { args in
                    var summary = "新建提醒事项「\(ToolArgs.string(args, "title") ?? "(无标题)")」"
                    if let due = ToolArgs.string(args, "due") { summary += "\n到期：\(due)" }
                    if let list = ToolArgs.string(args, "list") { summary += "\n列表：\(list)" }
                    return summary
                },
                run: { args in try await create(args) }
            ),
            PhoneTool(
                spec: ToolSpec(
                    name: "phone_reminders_complete",
                    description: """
                    Mark a reminder as completed. Pass the id returned by phone_reminders_list. \
                    The user must approve on the phone.
                    """,
                    inputSchema: Schema.object([
                        "id": Schema.string("Reminder identifier from phone_reminders_list."),
                    ], required: ["id"])
                ),
                source: .reminders,
                confirmation: { args in completeConfirmation(args) },
                run: { args in try await complete(args) }
            ),
        ]
    }

    // MARK: - Listing

    /// A plain-value copy of the reminder fields we report. EventKit objects are
    /// tied to their store and are not Sendable, so the callback converts them
    /// before handing anything back.
    struct ReminderRow: Sendable {
        var id: String
        var title: String
        var due: DateComponents?
        var completed: Bool
        var completionDate: Date?
        var list: String
        var priority: Int
        var notes: String?
    }

    private static func fetch(_ store: EKEventStore, in lists: [EKCalendar]?) async -> [ReminderRow] {
        let predicate = store.predicateForReminders(in: lists)
        return await withCheckedContinuation { (continuation: CheckedContinuation<[ReminderRow], Never>) in
            // EventKit calls back on its own queue: `@Sendable` keeps the block off the main actor (BackgroundCallbackTests).
            store.fetchReminders(matching: predicate) { @Sendable reminders in
                let rows = (reminders ?? []).compactMap { reminder -> ReminderRow? in
                    guard let calendar = reminder.calendar else { return nil }
                    return ReminderRow(
                        id: reminder.calendarItemIdentifier,
                        title: reminder.title ?? "",
                        due: reminder.dueDateComponents,
                        completed: reminder.isCompleted,
                        completionDate: reminder.completionDate,
                        list: calendar.title,
                        priority: reminder.priority,
                        notes: reminder.notes
                    )
                }
                continuation.resume(returning: rows)
            }
        }
    }

    static func list(_ args: JSONValue) async throws -> [ToolContent] {
        try CalendarAccess.requireReminders()
        let includeCompleted = ToolArgs.bool(args, "include_completed") ?? false
        let limit = ToolArgs.int(args, "limit", fallback: 50, min: 1, max: 200)

        let store = EKEventStore()
        var lists: [EKCalendar]?
        if let wanted = ToolArgs.string(args, "list")?.lowercased() {
            let all = store.calendars(for: .reminder)
            let matched = all.filter { $0.title.lowercased() == wanted || $0.title.lowercased().contains(wanted) }
            guard !matched.isEmpty else {
                let names = all.map(\.title).sorted().joined(separator: ", ")
                throw ToolFailure(.invalidArgument, "no reminder list named \"\(wanted)\"; available lists: \(names)")
            }
            lists = matched
        }

        let rows = await fetch(store, in: lists)
        var pending: [ReminderRow] = []
        var completed: [ReminderRow] = []
        for row in rows {
            if row.completed { completed.append(row) } else { pending.append(row) }
        }
        func sortKey(_ row: ReminderRow) -> Date {
            if let components = row.due, let date = Calendar.current.date(from: components) { return date }
            return Date.distantFuture
        }
        pending.sort { sortKey($0) < sortKey($1) }
        completed.sort { ($0.completionDate ?? .distantPast) > ($1.completionDate ?? .distantPast) }

        var chosen = pending
        if includeCompleted { chosen += completed }
        let truncated = chosen.count > limit
        chosen = Array(chosen.prefix(limit))

        return Output.json(Output.object([
            ("count", .number(Double(chosen.count))),
            ("pending", .number(Double(pending.count))),
            ("completed", .number(Double(completed.count))),
            ("truncated", .bool(truncated)),
            ("reminders", .array(chosen.map(encode))),
        ]))
    }

    // MARK: - Creating

    static func create(_ args: JSONValue) async throws -> [ToolContent] {
        try CalendarAccess.requireReminders()
        guard let title = ToolArgs.string(args, "title") else {
            throw ToolFailure(.invalidArgument, "title is required")
        }
        let store = EKEventStore()
        let reminder = EKReminder(eventStore: store)
        reminder.title = title
        if let notes = ToolArgs.string(args, "notes") { reminder.notes = notes }
        if let priority = ToolArgs.int(args, "priority"), (0...9).contains(priority) {
            reminder.priority = priority
        }
        if let due = try ToolDate.parse(ToolArgs.string(args, "due")) {
            let parts = Calendar.current.dateComponents([.year, .month, .day, .hour, .minute], from: due)
            reminder.dueDateComponents = parts
        }
        if let wanted = ToolArgs.string(args, "list")?.lowercased() {
            let lists = store.calendars(for: .reminder)
            guard let match = lists.first(where: { $0.title.lowercased() == wanted })
                ?? lists.first(where: { $0.title.lowercased().contains(wanted) }) else {
                let names = lists.map(\.title).sorted().joined(separator: ", ")
                throw ToolFailure(.invalidArgument, "no reminder list named \"\(wanted)\"; available lists: \(names)")
            }
            reminder.calendar = match
        } else if let fallback = store.defaultCalendarForNewReminders() {
            reminder.calendar = fallback
        }
        guard reminder.calendar != nil else {
            throw ToolFailure(.unavailable, "此 iPhone 上没有可写入的提醒事项列表 / No writable reminder list is configured")
        }
        try store.save(reminder, commit: true)
        return Output.json(encode(reminder))
    }

    /// Takes back a reminder the conversation added (its 撤销, docs/unified-entry.md); false when it is gone already.
    static func remove(id: String) throws -> Bool {
        let store = EKEventStore()
        guard let item = reminder(id: id, in: store) else { return false }
        try store.remove(item, commit: true)
        return true
    }

    // MARK: - Completing

    /// The reminder an id points at, or nil when it is gone or reminders access is missing
    /// (EventKit answers nil in both cases).
    private static func reminder(id: String, in store: EKEventStore) -> EKReminder? {
        store.calendarItem(withIdentifier: id) as? EKReminder
    }

    /// What the approval card names for a reminder about to be completed.
    struct CompletionTarget: Equatable {
        var title: String
        var list: String?
        var due: String?
    }

    /// Approval text for phone_reminders_complete. It runs before the permission check,
    /// so it looks the reminder up itself and names it; an opaque id alone tells the
    /// user nothing about what they approve.
    static func completeConfirmation(_ args: JSONValue) -> String {
        let id = ToolArgs.string(args, "id")
        let target = id.flatMap { reminder(id: $0, in: EKEventStore()) }.map { item in
            CompletionTarget(title: item.title ?? "", list: item.calendar?.title, due: dueString(item.dueDateComponents))
        }
        return completeSummary(id: id, target: target)
    }

    /// Pure formatting half of `completeConfirmation`. Without a target the card keeps the id
    /// and says why nothing matched; the confirmation cannot skip the prompt, and `complete`
    /// then fails with invalidArgument, so the text does not promise anything about running.
    static func completeSummary(id: String?, target: CompletionTarget?) -> String {
        guard let target else {
            return "找不到这条提醒（或没有提醒事项权限）：\(id ?? "?")"
        }
        let title = target.title.isEmpty ? "(无标题)" : target.title
        var summary = "把「\(title)」"
        if let list = target.list { summary += "（\(list)）" }
        summary += "标记为已完成"
        if let due = target.due { summary += "\n到期：\(due)" }
        return summary
    }

    static func complete(_ args: JSONValue) async throws -> [ToolContent] {
        try CalendarAccess.requireReminders()
        guard let id = ToolArgs.string(args, "id") else {
            throw ToolFailure(.invalidArgument, "id is required")
        }
        let store = EKEventStore()
        guard let item = reminder(id: id, in: store) else {
            throw ToolFailure(.invalidArgument, "no reminder with id \"\(id)\"; call phone_reminders_list to get a current id")
        }
        item.isCompleted = true
        item.completionDate = Date()
        try store.save(item, commit: true)
        return Output.json(encode(item))
    }

    static func encode(_ reminder: EKReminder) -> JSONValue {
        Output.object([
            ("id", .string(reminder.calendarItemIdentifier)),
            ("title", Output.opt(reminder.title)),
            ("due", Output.opt(dueString(reminder.dueDateComponents))),
            ("completed", .bool(reminder.isCompleted)),
            ("completed_at", Output.opt(reminder.completionDate.map(ToolDate.string))),
            ("list", Output.opt(reminder.calendar?.title)),
            ("priority", .number(Double(reminder.priority))),
            ("notes", Output.opt(reminder.notes.map { String($0.prefix(300)) })),
        ])
    }

    static func encode(_ row: ReminderRow) -> JSONValue {
        Output.object([
            ("id", .string(row.id)),
            ("title", Output.opt(row.title)),
            ("due", Output.opt(dueString(row.due))),
            ("completed", .bool(row.completed)),
            ("completed_at", Output.opt(row.completionDate.map(ToolDate.string))),
            ("list", Output.opt(row.list)),
            ("priority", .number(Double(row.priority))),
            ("notes", Output.opt(row.notes.map { String($0.prefix(300)) })),
        ])
    }

    private static func dueString(_ components: DateComponents?) -> String? {
        guard let components else { return nil }
        let calendar = Calendar.current
        if let date = calendar.date(from: components) { return ToolDate.string(date) }
        // A bare date with no time still deserves to be shown.
        guard let year = components.year, let month = components.month, let day = components.day else { return nil }
        return String(format: "%04d-%02d-%02d", year, month, day)
    }
}
