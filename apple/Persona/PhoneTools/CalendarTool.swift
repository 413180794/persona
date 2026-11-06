import EventKit
import Foundation

/// Calendar tools: read a time window, and create an event after the user
/// confirms on the phone.
enum CalendarTool {
    static var tools: [PhoneTool] {
        [
            PhoneTool(
                spec: ToolSpec(
                    name: "phone_calendar_events",
                    description: """
                    List calendar events in a time window. Defaults to the next 7 days starting \
                    at 00:00 today. Times are ISO 8601 with a timezone offset \
                    (e.g. 2026-10-24T14:00:00+08:00). Returns each event's id, title, start, end, \
                    all-day flag, calendar name, location, the first 300 characters of notes and \
                    the attendees. Use it to check whether the user is free.
                    """,
                    inputSchema: Schema.object([
                        "start": Schema.string("Window start, ISO 8601 (e.g. 2026-10-24T00:00:00+08:00). Default: 00:00 today."),
                        "end": Schema.string("Window end, ISO 8601. Default: 7 days after start."),
                        "query": Schema.string("Only return events whose title, location or notes contain this text."),
                        "limit": Schema.integer("Maximum number of events to return, default 50, max 200.", min: 1, max: 200),
                    ])
                ),
                source: .calendar,
                run: { args in try await list(args) }
            ),
            PhoneTool(
                spec: ToolSpec(
                    name: "phone_calendar_create_event",
                    description: """
                    Create a calendar event. `start` and `end` are ISO 8601 with a timezone \
                    (e.g. 2026-10-24T14:00:00+08:00). For an all-day event pass all_day=true and \
                    dates at 00:00. The user must approve the event on the phone before it is saved. \
                    Returns the new event's identifier.
                    """,
                    inputSchema: Schema.object([
                        "title": Schema.string("Event title."),
                        "start": Schema.string("Start time, ISO 8601 with a timezone."),
                        "end": Schema.string("End time, ISO 8601 with a timezone. Must be after start."),
                        "location": Schema.string("Optional location."),
                        "notes": Schema.string("Optional notes."),
                        "all_day": Schema.boolean("Mark the event as all-day. Default false."),
                        "calendar": Schema.string("Calendar name (title) to put the event in. Default: the default calendar for new events."),
                    ], required: ["title", "start", "end"])
                ),
                source: .calendar,
                confirmation: { args in
                    let title = ToolArgs.string(args, "title") ?? "(untitled)"
                    let start = ToolArgs.string(args, "start") ?? "?"
                    let end = ToolArgs.string(args, "end") ?? "?"
                    let location = ToolArgs.string(args, "location")
                    var summary = String(localized: "New event “\(title)”\\n\(start) → \(end)")
                    if ToolArgs.bool(args, "all_day") == true { summary += String(localized: " (all day)") }
                    if let location { summary += String(localized: "\\nLocation: \(location)") }
                    return summary
                },
                run: { args in try await create(args) }
            ),
        ]
    }

    // MARK: - Listing

    static func list(_ args: JSONValue) async throws -> [ToolContent] {
        try CalendarAccess.requireEvents()
        let calendar = Calendar.current
        let now = Date()
        let today = calendar.startOfDay(for: now)
        let start = try ToolDate.parse(ToolArgs.string(args, "start")) ?? today
        let end = try ToolDate.parse(ToolArgs.string(args, "end")) ?? calendar.date(byAdding: .day, value: 7, to: start) ?? today
        guard end > start else {
            throw ToolFailure(.invalidArgument, "end must be after start")
        }
        let limit = ToolArgs.int(args, "limit", fallback: 50, min: 1, max: 200)
        let query = ToolArgs.string(args, "query")?.lowercased()

        let store = EKEventStore()
        let predicate = store.predicateForEvents(withStart: start, end: end, calendars: nil)
        var events = store.events(matching: predicate).filter { !$0.isDetached }
        events.sort { $0.startDate < $1.startDate }

        var rows: [JSONValue] = []
        for event in events {
            if let query, !Self.matches(event, query: query) { continue }
            guard rows.count < limit else { break }
            rows.append(Self.encode(event))
        }

        return Output.json(.object([
            "start": .string(ToolDate.string(start)),
            "end": .string(ToolDate.string(end)),
            "count": .number(Double(rows.count)),
            "truncated": .bool(rows.count >= limit && events.count > rows.count),
            "events": .array(rows),
        ]))
    }

    private static func matches(_ event: EKEvent, query: String) -> Bool {
        for value in [event.title, event.location, event.notes] {
            if let value, value.lowercased().contains(query) { return true }
        }
        return false
    }

    /// Takes back an event the conversation added (its Undo, docs/unified-entry.md); false when it is gone already.
    static func remove(id: String) throws -> Bool {
        let store = EKEventStore()
        guard let event = store.event(withIdentifier: id) else { return false }
        try store.remove(event, span: .thisEvent, commit: true)
        return true
    }

    static func encode(_ event: EKEvent) -> JSONValue {
        let attendees: [JSONValue] = (event.attendees ?? []).map { attendee in
            Output.object([
                ("name", Output.opt(attendee.name)),
                ("role", .string(Self.roleName(attendee.participantRole))),
                ("status", .string(Self.statusName(attendee.participantStatus))),
                ("is_current_user", .bool(attendee.isCurrentUser)),
            ])
        }
        let notes = event.notes.map { String($0.prefix(300)) }
        return Output.object([
            ("id", .string(event.eventIdentifier ?? "")),
            ("title", Output.opt(event.title)),
            ("start", .string(ToolDate.string(event.startDate))),
            ("end", .string(ToolDate.string(event.endDate))),
            ("all_day", .bool(event.isAllDay)),
            ("calendar", Output.opt(event.calendar?.title)),
            ("location", Output.opt(event.location)),
            ("notes", Output.opt(notes)),
            ("attendees", attendees.isEmpty ? nil : .array(attendees)),
        ])
    }

    private static func roleName(_ role: EKParticipantRole) -> String {
        switch role {
        case .required: "required"
        case .optional: "optional"
        case .chair: "chair"
        case .nonParticipant: "non_participant"
        default: "unknown"
        }
    }

    private static func statusName(_ status: EKParticipantStatus) -> String {
        switch status {
        case .accepted: "accepted"
        case .declined: "declined"
        case .tentative: "tentative"
        case .pending: "pending"
        case .delegated: "delegated"
        case .completed: "completed"
        case .inProcess: "in_process"
        default: "unknown"
        }
    }

    // MARK: - Creating

    static func create(_ args: JSONValue) async throws -> [ToolContent] {
        try CalendarAccess.requireEvents()
        guard let title = ToolArgs.string(args, "title") else {
            throw ToolFailure(.invalidArgument, "title is required")
        }
        guard let start = try ToolDate.parse(ToolArgs.string(args, "start")) else {
            throw ToolFailure(.invalidArgument, "start is required, as ISO 8601 with a timezone")
        }
        guard let end = try ToolDate.parse(ToolArgs.string(args, "end")) else {
            throw ToolFailure(.invalidArgument, "end is required, as ISO 8601 with a timezone")
        }
        guard end > start else {
            throw ToolFailure(.invalidArgument, "end must be after start")
        }

        let store = EKEventStore()
        let event = EKEvent(eventStore: store)
        event.title = title
        event.startDate = start
        event.endDate = end
        event.isAllDay = ToolArgs.bool(args, "all_day") ?? false
        if let location = ToolArgs.string(args, "location") { event.location = location }
        if let notes = ToolArgs.string(args, "notes") { event.notes = notes }

        if let wanted = ToolArgs.string(args, "calendar")?.lowercased() {
            let calendars = store.calendars(for: .event)
            guard let match = calendars.first(where: { ($0.title.lowercased()) == wanted })
                ?? calendars.first(where: { $0.title.lowercased().contains(wanted) }) else {
                let names = calendars.map(\.title).sorted().joined(separator: ", ")
                throw ToolFailure(.invalidArgument, "no calendar named \"\(wanted)\"; available calendars: \(names)")
            }
            event.calendar = match
        } else if let fallback = store.defaultCalendarForNewEvents {
            event.calendar = fallback
        }
        guard event.calendar != nil else {
            throw ToolFailure(.unavailable, "此 iPhone 上没有可写入的日历 / No writable calendar is configured on this iPhone") // i18n-ignore
        }

        try store.save(event, span: .thisEvent, commit: true)
        return Output.json(Output.object([
            ("id", .string(event.eventIdentifier ?? "")),
            ("title", .string(title)),
            ("start", .string(ToolDate.string(start))),
            ("end", .string(ToolDate.string(end))),
            ("calendar", Output.opt(event.calendar?.title)),
        ]))
    }
}
