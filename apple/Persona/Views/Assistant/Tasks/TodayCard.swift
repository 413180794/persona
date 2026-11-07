import EventKit
import SwiftUI

// The top of the day in the main conversation (docs/unified-entry.md「对话的形状」): a line with the date, and the
// Today card — what finished overnight, what waits, the day's meetings, the chats that @ the user, the phone's reminders
// due today. Each row is one tap: open the task, or say the obvious thing to Persona.

/// Today at a Glance: the hub's `today` item, plus the phone's reminders due today when the app may read them.
struct TodayCard: View {
    var item: FeedItem
    var today: TodayBody
    @State private var reminders: [TodayReminder] = []
    private var store: TasksStore { .shared }

    init(item: FeedItem, body: TodayBody) {
        self.item = item
        today = body
    }

    var body: some View {
        let rows = self.rows
        VStack(alignment: .leading, spacing: 0) {
            Label("Today at a Glance", systemImage: Theme.Icon.today)
                .font(Theme.Font.captionStrong)
                .foregroundStyle(.secondary)
                .padding(.bottom, Theme.Space.s)
            if rows.isEmpty {
                Text("Nothing needs you, and your calendar is clear.").font(Theme.Font.secondary).foregroundStyle(.secondary)
            }
            ForEach(Array(rows.enumerated()), id: \.offset) { n, row in
                if n > 0 { Divider().padding(.leading, Theme.Size.wellS + Theme.Space.m) }
                TodayRow(row: row)
            }
        }
        .card(padding: Theme.Space.l)
        .task(id: item.id) { reminders = await TodayReminder.dueToday() }
    }

    private var rows: [TodayRow.Model] {
        var out: [TodayRow.Model] = []
        if let done = today.done, !done.isEmpty {
            out.append(.init(symbol: Theme.Icon.successFilled, tint: Theme.positive,
                             title: done.count == 1 ? String(localized: "Done: \(done[0].title)") : String(localized: "\(done.count) tasks done"),
                             detail: done.count > 1 ? done.map(\.title).joined(separator: String(localized: "、", comment: "Separator between list items")) : nil,
                             action: String(localized: "View [Task Views d87059]"), run: .open(done[0].id)))
        }
        if let waiting = today.waiting, !waiting.isEmpty {
            out.append(.init(symbol: Theme.Icon.askFirst, tint: AgentState.needsYou.tone.text,
                             title: waiting.count == 1 ? String(localized: "Needs you: \(waiting[0].title)") : StateName.count(.needsYou, waiting.count),
                             detail: waiting.count > 1 ? waiting.map(\.title).joined(separator: String(localized: "、", comment: "Separator between list items")) : nil,
                             action: String(localized: "View [Today Card 88f472]"), run: .open(waiting[0].id)))
        }
        if let working = today.working, working > 0 {
            out.append(.init(symbol: Theme.Icon.working, tint: AgentState.working.tone.text, title: String(localized: "\(working) tasks in progress"),
                             detail: nil, action: String(localized: "All"), run: .board))
        }
        for m in (today.meetings ?? []).prefix(3) {
            let time = m.start.formatted(.dateTime.hour(.defaultDigits(amPM: .omitted)).minute())
            out.append(.init(symbol: Theme.Icon.addedEvent, tint: Theme.accent, title: "\(time) \(m.title)", detail: nil,
                             action: String(localized: "Prep"), run: .say(String(localized: "Help me prepare for “\(m.title)” at \(time): docs to read, last time's conclusions, and decisions I need to make"))))
        }
        if let chats = today.atMe, !chats.isEmpty {
            out.append(.init(symbol: Theme.Icon.mention, tint: Theme.loop,
                             title: (today.unread ?? 0) > 0 ? String(localized: "Lark: @mentioned in \(chats.count) chats · \(today.unread ?? 0) other unread") : String(localized: "Lark: @mentioned in \(chats.count) chats"),
                             detail: chats.prefix(2).map { c in c.preview.map { String(localized: "\(c.name): \($0)") } ?? c.name }.joined(separator: "\n"),
                             action: String(localized: "Review [Today Card 1f01e5]"), run: .say(String(localized: "Go through my Lark messages that @mention me: which need a reply from me, and draft the ones you can"))))
        }
        for r in reminders.prefix(3) {
            out.append(.init(symbol: Theme.Icon.addedReminder, tint: Theme.caution,
                             title: (r.due.map { $0.formatted(.dateTime.hour(.defaultDigits(amPM: .omitted)).minute()) + " " } ?? "") + r.title,
                             detail: String(localized: "Reminder [Add Todo Intent 8f29b3]"), action: nil, run: nil))
        }
        return out
    }
}

/// One row of the Today card: a tinted mark, what it is, and its one action.
struct TodayRow: View {
    enum Run: Hashable { case open(String), board, say(String) }

    struct Model {
        var symbol: String
        var tint: Color
        var title: String
        var detail: String?
        var action: String?
        var run: Run?
    }

    var row: Model
    private var store: TasksStore { .shared }

    var body: some View {
        HStack(spacing: Theme.Space.m) {
            Image(systemName: row.symbol)
                .font(Theme.Font.secondaryStrong)
                .foregroundStyle(row.tint)
                .scaledFrame(width: Theme.Size.wellS, height: Theme.Size.wellS)
                .background(row.tint.opacity(0.14), in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(row.title).font(Theme.Font.secondaryMedium).lineLimit(2)
                if let d = row.detail { Text(d).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(2) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if let action = row.action, let run = row.run {
                Button { perform(run) } label: { Text(action).lineLimit(1) }
                    .font(Theme.Font.captionStrong)
                    .themedButton()
            }
        }
        .padding(.vertical, Theme.Space.s)
    }

    private func perform(_ run: Run) {
        switch run {
        case .open(let id): store.detailTask = id
        case .board: store.showBoard = true
        case .say(let text):
            if let key = AppModel.shared.assistantMain { AppModel.shared.sendNow(key, text: text) }
        }
    }
}

/// A reminder due today on this iPhone, read only when the app already may (it never asks for access here).
struct TodayReminder: Hashable {
    var title: String
    var due: Date?

    static func dueToday() async -> [TodayReminder] {
        guard EKEventStore.authorizationStatus(for: .reminder) == .fullAccess else { return [] }
        let store = EKEventStore()
        let cal = Calendar.current
        let start = cal.startOfDay(for: .now)
        guard let end = cal.date(byAdding: .day, value: 1, to: start) else { return [] }
        let predicate = store.predicateForIncompleteReminders(withDueDateStarting: start, ending: end, calendars: nil)
        let found: [TodayReminder] = await withCheckedContinuation { (done: CheckedContinuation<[TodayReminder], Never>) in
            store.fetchReminders(matching: predicate) { @Sendable list in
                done.resume(returning: (list ?? []).map { r in
                    TodayReminder(title: r.title ?? "", due: r.dueDateComponents.flatMap { cal.date(from: $0) })
                })
            }
        }
        return found.sorted { ($0.due ?? .distantFuture) < ($1.due ?? .distantFuture) }
    }
}

/// Today · Mon, Nov 3: the line over the first thing of each day in the assistant's conversation.
struct DayLine: View {
    var date: Date

    var body: some View {
        Text(Self.label(date))
            .font(Theme.Font.caption)
            .foregroundStyle(.tertiary)
            .frame(maxWidth: .infinity)
            .padding(.vertical, Theme.Space.xs)
            .accessibilityAddTraits(.isHeader)
    }

    static func label(_ date: Date, now: Date = .now) -> String {
        let cal = Calendar.current
        let day = date.formatted(.dateTime.month().day().weekday(.abbreviated))
        if cal.isDate(date, inSameDayAs: now) { return String(localized: "Today · \(day)") }
        if let y = cal.date(byAdding: .day, value: -1, to: now), cal.isDate(date, inSameDayAs: y) { return String(localized: "Yesterday · \(day)") }
        if !cal.isDate(date, equalTo: now, toGranularity: .year) {
            return date.formatted(.dateTime.year().month().day().weekday(.abbreviated))
        }
        return day
    }
}

extension ChatLines {
    /// The lines with a day line over the first line of each day (lines with no time of their own keep their place).
    static func daySections(_ lines: [ChatLine], calendar: Calendar = .current) -> [ChatLine] {
        var out: [ChatLine] = []
        var lastDay: Date?
        for line in lines {
            if let t = time(line) {
                let day = calendar.startOfDay(for: t)
                if day != lastDay {
                    out.append(.day(day))
                    lastDay = day
                }
            }
            out.append(line)
        }
        return out
    }

    /// When a line happened, for the day lines.
    static func time(_ line: ChatLine) -> Date? {
        switch line.content {
        case .row(let r): r.firstTS
        case .work(_, _, _, let start, _): start
        case .userImages(_, let items), .recaps(_, let items): items.first?.ts
        case .apiError(let item, _, _): item.ts
        case .feed(let f): f.at
        default: nil
        }
    }
}
