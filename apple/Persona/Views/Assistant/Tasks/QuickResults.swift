import SwiftUI

// Things the assistant did on the phone in a second (docs/unified-entry.md「派发」, 秒办): a reminder or an event it added
// is drawn as its result under the turn's work line, with 撤销, instead of hiding in 「工具 N」.

/// A reminder or an event a tool call added, read from the call's result.
nonisolated struct QuickResult: Hashable, Sendable {
    enum Kind: Sendable { case reminder, event }

    var kind: Kind
    var callID: String
    var itemID: String
    var title: String
    /// When it is due / starts, as the tool wrote it (ISO 8601), and where it went (list / calendar).
    var when: Date?
    var place: String?

    /// The calls that are drawn this way, by the tool's own name (an MCP name ends in it: mcp__<server>__<name>).
    static func kind(of name: String) -> Kind? {
        let short = name.components(separatedBy: "__").last ?? name
        switch short {
        case "phone_reminders_create": return .reminder
        case "phone_calendar_create_event": return .event
        default: return nil
        }
    }

    init?(_ call: ToolCall) {
        guard let kind = Self.kind(of: call.name), let result = call.result, result.isError != true,
              let text = result.text, let data = text.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let id = obj["id"] as? String, !id.isEmpty else { return nil }
        self.kind = kind
        callID = call.id
        itemID = id
        title = (obj["title"] as? String) ?? ""
        let raw = kind == .reminder ? obj["due"] as? String : obj["start"] as? String
        when = raw.flatMap(JSON.parseDate)
        place = (kind == .reminder ? obj["list"] : obj["calendar"]) as? String
    }
}

extension ChatLines {
    /// The lines with each turn's quick results right after its work line (after its steps when it is open).
    static func quickResults(_ lines: [ChatLine]) -> [ChatLine] {
        var out: [ChatLine] = []
        var pending: [ChatLine] = []
        func flush() { out += pending; pending = [] }
        for line in lines {
            switch line.content {
            case .workStep, .toolRun, .workEnd:
                out.append(line)
                continue
            default:
                flush()
            }
            out.append(line)
            switch line.content {
            case .work(_, let items, _, _, _):
                pending = items.compactMap { if case .step(.call(let c)) = $0 { QuickResult(c).map { .quick($0) } } else { nil } }
            case .row(.tools(let steps)):
                pending = steps.compactMap { if case .call(let c) = $0 { QuickResult(c).map { .quick($0) } } else { nil } }
            default:
                break
            }
        }
        flush()
        return out
    }
}

/// 已加提醒 / 已加到日历, what and when, and 撤销 (it is taken off the phone; the line says so).
struct QuickResultCard: View {
    var result: QuickResult
    private var store: TasksStore { .shared }

    var body: some View {
        let undone = store.undone.contains(result.callID)
        HStack(spacing: Theme.Space.m) {
            Image(systemName: result.kind == .reminder ? Theme.Icon.addedReminder : Theme.Icon.addedEvent)
                .font(Theme.Font.secondaryStrong)
                .foregroundStyle(result.kind == .reminder ? Theme.caution : Theme.accent)
                .scaledFrame(width: Theme.Size.wellS, height: Theme.Size.wellS)
                .background((result.kind == .reminder ? Theme.caution : Theme.accent).opacity(0.14),
                            in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(undone ? "已撤销" : (result.kind == .reminder ? "已加提醒" : "已加到日历"))
                    .font(Theme.Font.secondaryStrong)
                Text(subtitle).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(2)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if !undone {
                Button {
                    Task { await store.undo(result) }
                } label: {
                    Label("撤销", systemImage: Theme.Icon.undo).labelStyle(.titleOnly).lineLimit(1)
                }
                .font(Theme.Font.captionStrong)
                .themedButton()
            }
        }
        .padding(Theme.Space.m)
        .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        .opacity(undone ? 0.6 : 1)
        .animation(Theme.Motion.state, value: undone)
    }

    private var subtitle: String {
        var parts: [String] = []
        if let when = result.when { parts.append(Self.when(when, allDayish: result.kind == .reminder)) }
        parts.append(result.title)
        if let place = result.place, !place.isEmpty { parts.append(place) }
        return parts.joined(separator: " · ")
    }

    /// "今天 18:00", "明天 9:30", "周六 10:00", "11月12日 14:00".
    static func when(_ date: Date, allDayish: Bool = false, now: Date = .now) -> String {
        let cal = Calendar.current
        let time = date.formatted(.dateTime.hour(.defaultDigits(amPM: .omitted)).minute())
        if cal.isDate(date, inSameDayAs: now) { return String(localized: "今天 \(time)") }
        if let tomorrow = cal.date(byAdding: .day, value: 1, to: now), cal.isDate(date, inSameDayAs: tomorrow) { return String(localized: "明天 \(time)") }
        if let days = cal.dateComponents([.day], from: cal.startOfDay(for: now), to: cal.startOfDay(for: date)).day, (2..<7).contains(days) {
            return date.formatted(.dateTime.weekday(.abbreviated)) + " " + time
        }
        return date.formatted(.dateTime.month().day()) + " " + time
    }
}

// MARK: - Kinds that start without asking

/// 直接开的任务: the kinds remembered with 这类以后直接开, each removable (it asks again next time).
struct TaskTrustView: View {
    @State private var kinds: [APIClient.TrustKind]?
    @State private var error: String?

    var body: some View {
        List {
            if let kinds {
                if kinds.isEmpty {
                    EmptyStateView(symbol: Theme.Icon.taskTrust, title: String(localized: "还没有"),
                                   message: String(localized: "在对话的计划卡上勾「这类以后直接开」，同一台电脑、同一个目录、同样读或改的任务以后就不再先问。"))
                        .listRowBackground(Color.clear)
                } else {
                    Section {
                        ForEach(kinds, id: \.key) { k in
                            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                                Text((k.cwd as NSString).lastPathComponent).font(Theme.Font.body)
                                Text("\(TaskPlace.machineName(k.machine)) · \(k.readonly ? String(localized: "只读") : String(localized: "可以改文件")) · \(k.cwd)")
                                    .font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                            }
                            .themedRow()
                        }
                        .onDelete { idx in Task { await forget(idx.map { kinds[$0] }) } }
                    } footer: {
                        Text("这些任务 Persona 会直接开，不先给你看计划。左滑删掉的，以后又会先问。")
                    }
                }
            } else if let error {
                EmptyStateView(symbol: Theme.Icon.error, title: String(localized: "没读到"), message: error).listRowBackground(Color.clear)
            } else {
                ProgressView().frame(maxWidth: .infinity).listRowBackground(Color.clear)
            }
        }
        .themedPage()
        .navigationTitle("直接开的任务")
        .task { await load() }
    }

    private func load() async {
        guard let api = AppModel.shared.api else { return }
        do { kinds = try await api.taskTrust() } catch { self.error = error.localizedDescription }
    }

    private func forget(_ list: [APIClient.TrustKind]) async {
        guard let api = AppModel.shared.api else { return }
        for k in list { try? await api.forgetTrust(k.key) }
        await load()
    }
}
