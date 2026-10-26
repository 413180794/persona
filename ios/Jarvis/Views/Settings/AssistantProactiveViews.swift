import MarkdownUI
import SwiftUI

// 设置 › 助手: what the assistant does on its own (routines, follow-ups it scheduled), what it remembers, and what it
// is told about the user (docs/assistant.md). Every page reads straight from the hub; nothing is cached on the phone.

// MARK: - Formatting

enum AssistantScheduleText {
    static let dayNames = ["一", "二", "三", "四", "五", "六", "日"]
    static let workdays = [1, 2, 3, 4, 5]

    /// "每天" / "工作日" / "周末" / "每周一、三、五".
    static func repeats(_ days: [Int]) -> String {
        let d = Set(days)
        if d.isEmpty || d.count == 7 { return "每天" }
        if d == Set(workdays) { return "工作日" }
        if d == [6, 7] { return "周末" }
        return "每周" + d.sorted().map { dayNames[$0 - 1] }.joined(separator: "、")
    }

    /// "工作日 09:00".
    static func schedule(_ r: APIClient.Routine) -> String { "\(repeats(r.weekdays)) \(r.time)" }

    /// A future moment, roughly: "马上" / "25 分钟后" / "2 小时后" / "3 天后".
    static func until(_ date: Date, now: Date = .now) -> String {
        let s = date.timeIntervalSince(now)
        if s < 60 { return s < -60 ? "已过时间" : "马上" }
        if s < 3600 { return "\(Int(s / 60)) 分钟后" }
        if s < 86400 {
            let h = Int(s / 3600), m = Int(s.truncatingRemainder(dividingBy: 3600) / 60)
            return h < 3 && m >= 10 ? "\(h) 小时 \(m) 分钟后" : "\(h) 小时后"
        }
        let days = Calendar.current.dateComponents([.day], from: Calendar.current.startOfDay(for: now),
                                                   to: Calendar.current.startOfDay(for: date)).day ?? 0
        return "\(max(days, 1)) 天后"
    }

    /// "今天 15:30" / "明天 09:00" / "周三 09:00" / "11月2日 09:00".
    static func moment(_ date: Date, now: Date = .now) -> String {
        let cal = Calendar.current
        let time = date.formatted(.dateTime.hour(.twoDigits(amPM: .omitted)).minute(.twoDigits))
        if cal.isDateInToday(date) { return "今天 \(time)" }
        if cal.isDateInTomorrow(date) { return "明天 \(time)" }
        let days = cal.dateComponents([.day], from: cal.startOfDay(for: now), to: cal.startOfDay(for: date)).day ?? 0
        if days > 1, days < 7 { return "周\(dayNames[(cal.component(.weekday, from: date) + 5) % 7]) \(time)" }
        return "\(cal.component(.month, from: date))月\(cal.component(.day, from: date))日 \(time)"
    }
}

// MARK: - 定时任务

struct AssistantRoutinesView: View {
    @Environment(AppModel.self) private var model
    @State private var routines: [APIClient.Routine] = []
    @State private var loaded = false
    @State private var error: String?
    @State private var editing: APIClient.Routine?
    @State private var creating: APIClient.Routine?

    var body: some View {
        List {
            if !routines.isEmpty {
                Section {
                    ForEach(routines) { r in row(r) }
                } footer: {
                    Text("到点时 Jarvis 开一段新对话跑这段提示词，跑完推送到手机。时间按手机所在的时区。")
                }
            }
        }
        .themedPage()
        .overlay {
            if !loaded {
                ProgressView()
            } else if let error, routines.isEmpty {
                ContentUnavailableView("读不到定时任务", systemImage: "exclamationmark.triangle", description: Text(error))
            } else if routines.isEmpty {
                ContentUnavailableView("没有定时任务", systemImage: "alarm",
                                       description: Text("点右上角的 ＋，让 Jarvis 每天定时做一件事，比如早报、下班前盘点。"))
            }
        }
        .navigationTitle("定时任务")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Section("从模板开始") {
                        ForEach(AssistantRoutineTemplate.all) { t in
                            Button { creating = t.routine } label: { Label(t.routine.title, systemImage: t.symbol) }
                        }
                    }
                    Button { creating = AssistantRoutineTemplate.blank } label: { Label("空白", systemImage: "square.and.pencil") }
                } label: {
                    Image(systemName: "plus")
                }
                .accessibilityLabel("新建定时任务")
            }
        }
        .navigationDestination(item: $editing) { r in
            AssistantRoutineEditor(routine: r, save: save, delete: delete)
        }
        .sheet(item: $creating) { r in
            NavigationStack { AssistantRoutineEditor(routine: r, save: save, delete: delete) }
        }
        .task { await load() }
        .refreshable { await load() }
    }

    private func row(_ r: APIClient.Routine) -> some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 3) {
                Text(r.title).font(.body.weight(.semibold)).lineLimit(1)
                HStack(spacing: 6) {
                    Text(AssistantScheduleText.schedule(r)).monospacedDigit()
                    if r.enabled, let next = r.nextRun {
                        Text("·")
                        Text("下次 \(AssistantScheduleText.until(next))")
                    } else if !r.enabled {
                        Text("·")
                        Text("已关闭")
                    }
                }
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(.rect)
            .onTapGesture { editing = r }
            .accessibilityAddTraits(.isButton)
            Toggle(r.title, isOn: Binding(get: { r.enabled }, set: { v in
                var changed = r
                changed.enabled = v
                Haptics.tap()
                Task { _ = await save(changed) }
            }))
            .labelsHidden()
        }
        .padding(.vertical, 2)
        .opacity(r.enabled ? 1 : 0.75)
    }

    private func load() async {
        guard let api = model.api else { return }
        do {
            routines = try await api.routines()
            error = nil
        } catch {
            self.error = error.shown ?? self.error
        }
        loaded = true
        #if DEBUG
        switch UserDefaults.standard.string(forKey: "openAssistantPage") {
        case "routine" where editing == nil: editing = routines.first
        case "newRoutine" where creating == nil: creating = AssistantRoutineTemplate.all.last?.routine
        default: break
        }
        #endif
    }

    /// Replaces the hub's whole list with `r` added or updated; returns `r` as saved (new ones get their id here).
    private func save(_ r: APIClient.Routine) async -> APIClient.Routine? {
        guard let api = model.api else { return nil }
        var list = routines
        if let i = list.firstIndex(where: { $0.id == r.id && !r.id.isEmpty }) { list[i] = r } else { list.append(r) }
        withAnimation(.snappy) { routines = list } // optimistic: the toggle shouldn't bounce back while saving
        do {
            let saved = try await api.setRoutines(list)
            routines = saved
            return r.id.isEmpty ? saved.last : saved.first { $0.id == r.id }
        } catch {
            model.show(error)
            if let fresh = try? await api.routines() { routines = fresh }
            return nil
        }
    }

    private func delete(_ r: APIClient.Routine) async {
        guard let api = model.api, !r.id.isEmpty else { return }
        let list = routines.filter { $0.id != r.id }
        do {
            routines = try await api.setRoutines(list)
            model.toast = Toast("已删除「\(r.title)」", style: .success)
        } catch {
            model.show(error)
        }
    }
}

/// Starting points for the ＋ menu.
struct AssistantRoutineTemplate: Identifiable {
    var routine: APIClient.Routine
    var symbol: String
    var id: String { routine.title }

    static let blank = APIClient.Routine(title: "", prompt: "", time: "09:00", weekdays: AssistantScheduleText.workdays, enabled: true)

    static let all: [AssistantRoutineTemplate] = [
        .init(routine: .init(title: "会前准备", prompt: "看看今天飞书日历和 iPhone 日历里的会。对每个需要准备的会，用 schedule_followup 在会前 10 分钟" +
                             "安排一次会前准备：相关文档、上次的会议纪要、还没定的事、我该说的要点。今天没有要准备的会就简单说一声。",
                             time: "08:45", weekdays: AssistantScheduleText.workdays, enabled: true), symbol: "person.2.badge.gearshape"),
        .init(routine: .init(title: "周报", prompt: "帮我起草这周的周报：从飞书日历、会议纪要、文档和消息里整理这周做了什么、进展和结果、" +
                             "遇到的问题、下周计划；各台电脑上的 Agent 这周完成的主要改动也算进去。先给我草稿，问我要不要改，不要直接发出去。",
                             time: "17:00", weekdays: [5], enabled: true), symbol: "doc.text"),
        .init(routine: .init(title: "下班前盘点", prompt: "下班前盘点：今天还有哪些消息没回、答应别人的事还没做、各台电脑上还在跑或在等我的 Agent，" +
                             "明天上午有哪些会要准备。按紧急程度列出来，最后问我哪些要设提醒。",
                             time: "18:30", weekdays: AssistantScheduleText.workdays, enabled: true), symbol: "checklist"),
    ]
}

/// Edits one routine. An existing one is saved when leaving the page (like iOS Settings); a new one comes up as a
/// sheet with 取消 / 添加, so trying a template and backing out doesn't create anything.
struct AssistantRoutineEditor: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var draft: APIClient.Routine
    /// What the hub has; nil until a new routine is first saved.
    @State private var saved: APIClient.Routine?
    @State private var running = false
    @State private var confirmDelete = false
    @State private var deleted = false
    /// Presented as the "new routine" sheet: nothing is saved unless 添加 (or 立即运行) is tapped.
    private let creating: Bool
    var save: (APIClient.Routine) async -> APIClient.Routine?
    var delete: (APIClient.Routine) async -> Void

    init(routine: APIClient.Routine, save: @escaping (APIClient.Routine) async -> APIClient.Routine?,
         delete: @escaping (APIClient.Routine) async -> Void) {
        _draft = State(initialValue: routine)
        _saved = State(initialValue: routine.id.isEmpty ? nil : routine)
        creating = routine.id.isEmpty
        self.save = save
        self.delete = delete
    }

    private var isValid: Bool {
        !draft.title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && !draft.prompt.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
    private var changed: Bool { saved.map { !same($0, draft) } ?? true }

    var body: some View {
        Form {
            Section {
                TextField("标题，比如「早报」", text: $draft.title)
                Toggle("开启", isOn: $draft.enabled)
            }
            Section {
                DatePicker("时间", selection: time, displayedComponents: .hourAndMinute)
                AssistantWeekdayPicker(days: $draft.weekdays)
                    .listRowInsets(EdgeInsets(top: 12, leading: 16, bottom: 12, trailing: 16))
            } header: {
                Text("什么时候")
            } footer: {
                if draft.enabled, let next = saved?.nextRun, saved.map({ same($0, draft) }) == true {
                    Text("下次运行：\(AssistantScheduleText.moment(next))（\(AssistantScheduleText.until(next))）")
                } else {
                    Text("\(AssistantScheduleText.repeats(draft.weekdays)) \(draft.time)，按手机所在的时区。")
                }
            }
            Section {
                TextField("要 Jarvis 做什么", text: $draft.prompt, axis: .vertical)
                    .lineLimit(8...)
            } header: {
                Text("提示词")
            } footer: {
                Text("像在对话里一样写：看哪些数据、怎么汇报。它可以用 schedule_followup 再安排一次性的跟进，比如会前 10 分钟。")
            }
            Section {
                Button { Task { await runNow() } } label: {
                    HStack {
                        Label("立即运行", systemImage: "play.fill").lineLimit(1)
                        Spacer()
                        if running { ProgressView() }
                    }
                }
                .disabled(running || !isValid)
                if let last = saved?.lastSessionID, !last.isEmpty {
                    Button { open(SessionKey(machineID: model.assistantConfig?.machineID ?? model.hubMachineID, sessionID: last)) } label: {
                        HStack {
                            Label("查看上次结果", systemImage: "text.bubble").lineLimit(1)
                            Spacer()
                            if let at = saved?.lastRun {
                                Text(Fmt.relative(at)).font(.callout).foregroundStyle(.secondary).lineLimit(1)
                            }
                        }
                    }
                }
            } footer: {
                Text("立即运行会开一段新对话，跑完在「对话」里看结果。")
            }
            if !creating {
                Section {
                    Button(role: .destructive) { confirmDelete = true } label: {
                        Label("删除定时任务", systemImage: "trash").lineLimit(1)
                    }
                }
            }
        }
        .themedPage()
        .navigationTitle(creating ? "新建定时任务" : (draft.title.isEmpty ? "定时任务" : draft.title))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if creating {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("添加") {
                        Task { if await persist(leaving: false) != nil { dismiss() } }
                    }
                    .disabled(!isValid || running)
                }
            }
        }
        .confirmationDialog("删除「\(draft.title)」？", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("删除", role: .destructive) {
                guard let s = saved else { return }
                deleted = true
                Task { await delete(s) }
                dismiss()
            }
        } message: {
            Text("删除后不再定时运行，已经跑过的对话还在。")
        }
        .onDisappear { if !creating { Task { await persist(leaving: true) } } }
    }

    /// The DatePicker edits "HH:MM" through a date today.
    private var time: Binding<Date> {
        Binding {
            let parts = draft.time.split(separator: ":").compactMap { Int($0) }
            return Calendar.current.date(bySettingHour: parts.first ?? 9, minute: parts.count > 1 ? parts[1] : 0, second: 0, of: .now) ?? .now
        } set: { d in
            let c = Calendar.current.dateComponents([.hour, .minute], from: d)
            draft.time = String(format: "%02d:%02d", c.hour ?? 0, c.minute ?? 0)
        }
    }

    /// Ignores the hub-only fields (last / next run).
    private func same(_ a: APIClient.Routine, _ b: APIClient.Routine) -> Bool {
        a.title == b.title && a.prompt == b.prompt && a.time == b.time && Set(a.weekdays) == Set(b.weekdays) && a.enabled == b.enabled
    }

    @discardableResult private func persist(leaving: Bool) async -> APIClient.Routine? {
        guard !deleted, changed else { return saved }
        guard isValid else {
            if leaving {
                model.toast = Toast("标题和提示词都要填，这次的修改没有保存", style: .error)
            }
            return nil
        }
        var r = draft
        r.title = r.title.trimmingCharacters(in: .whitespacesAndNewlines)
        if Set(r.weekdays).count == 7 { r.weekdays = [] }
        r.weekdays.sort()
        guard let s = await save(r) else { return nil }
        saved = s
        if !leaving { draft.id = s.id }
        return s
    }

    private func runNow() async {
        guard let api = model.api else { return }
        running = true
        defer { running = false }
        guard let r = await persist(leaving: false) else { return }
        do {
            let key = try await api.runRoutine(r.id)
            Haptics.success()
            open(key)
        } catch {
            model.show(error)
        }
    }

    private func open(_ key: SessionKey) {
        if creating { dismiss() }
        model.selectedTab = .assistant
        model.assistantCurrent = key
    }
}

/// Seven round day buttons (一 … 日) plus 每天 / 工作日 shortcuts; empty `days` means every day.
struct AssistantWeekdayPicker: View {
    @Binding var days: [Int]

    private var selected: Set<Int> { days.isEmpty ? Set(1...7) : Set(days) }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 0) {
                ForEach(1...7, id: \.self) { d in
                    let on = selected.contains(d)
                    Button { toggle(d) } label: {
                        Text(AssistantScheduleText.dayNames[d - 1])
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(on ? Color.white : Color.primary)
                            .frame(width: 38, height: 38)
                            .background(Circle().fill(on ? AnyShapeStyle(Theme.accent) : AnyShapeStyle(Color.secondary.opacity(0.14))))
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("周\(AssistantScheduleText.dayNames[d - 1])")
                    .accessibilityAddTraits(on ? .isSelected : [])
                    .frame(maxWidth: .infinity)
                }
            }
            HStack(spacing: 8) {
                shortcut("每天", [])
                shortcut("工作日", AssistantScheduleText.workdays)
                shortcut("周末", [6, 7])
                Spacer(minLength: 0)
            }
        }
    }

    private func shortcut(_ title: String, _ value: [Int]) -> some View {
        let on = AssistantScheduleText.repeats(days) == AssistantScheduleText.repeats(value)
        return Button {
            Haptics.tap()
            withAnimation(.snappy) { days = value }
        } label: {
            Text(title)
                .font(.footnote.weight(.medium))
                .lineLimit(1)
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
                .foregroundStyle(on ? Theme.accent : Color.secondary)
                .background(Capsule().fill(on ? Theme.accent.opacity(0.14) : Color.secondary.opacity(0.1)))
        }
        .buttonStyle(.plain)
    }

    private func toggle(_ d: Int) {
        var s = selected
        if s.contains(d) {
            guard s.count > 1 else { Haptics.error(); return } // no days would read as "every day"
            s.remove(d)
        } else {
            s.insert(d)
        }
        Haptics.tap()
        withAnimation(.snappy) { days = s.count == 7 ? [] : s.sorted() }
    }
}

// MARK: - 已安排的跟进

struct AssistantFollowupsView: View {
    @Environment(AppModel.self) private var model
    @State private var followups: [APIClient.Followup] = []
    @State private var loaded = false
    @State private var error: String?

    var body: some View {
        List {
            if !followups.isEmpty {
                Section {
                    ForEach(followups) { f in row(f) }
                } footer: {
                    Text("Jarvis 在对话里替自己安排的一次性跟进，到点自己醒来接着做。左滑可以取消。")
                }
            }
        }
        .themedPage()
        .overlay {
            if !loaded {
                ProgressView()
            } else if let error, followups.isEmpty {
                ContentUnavailableView("读不到跟进", systemImage: "exclamationmark.triangle", description: Text(error))
            } else if followups.isEmpty {
                ContentUnavailableView("没有安排的跟进", systemImage: "calendar.badge.clock",
                                       description: Text("在对话里说「一小时后提醒我……」「明早会前帮我准备」，Jarvis 会安排在这里。"))
            }
        }
        .navigationTitle("已安排的跟进")
        .task { await load() }
        .refreshable { await load() }
    }

    @ViewBuilder private func row(_ f: APIClient.Followup) -> some View {
        let firstLine = f.prompt.split(whereSeparator: \.isNewline).first.map(String.init) ?? f.prompt
        let title = f.title.flatMap { $0.isEmpty ? nil : $0 } ?? firstLine
        let content = HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 4) {
                Text(title).font(.body.weight(.semibold)).foregroundStyle(Color.primary).lineLimit(2)
                if title != f.prompt {
                    Text(f.prompt).font(.subheadline).foregroundStyle(Color.secondary).lineLimit(2)
                }
                HStack(spacing: 10) {
                    Label("\(AssistantScheduleText.moment(f.at)) · \(AssistantScheduleText.until(f.at))", systemImage: "clock")
                        .monospacedDigit()
                    Label(f.sessionID == nil ? "新对话" : "继续对话",
                          systemImage: f.sessionID == nil ? "plus.bubble" : "arrowshape.turn.up.left")
                }
                .labelStyle(AssistantCompactLabel())
                .font(.caption)
                .foregroundStyle(Color.secondary)
                .lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if f.sessionID != nil {
                Image(systemName: "chevron.right").font(.footnote.weight(.semibold)).foregroundStyle(Color(uiColor: .tertiaryLabel))
            }
        }
        .padding(.vertical, 3)
        Group {
            if let sid = f.sessionID {
                Button {
                    model.selectedTab = .assistant
                    model.assistantCurrent = SessionKey(machineID: f.machineID, sessionID: sid)
                } label: { content }
            } else {
                content
            }
        }
        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
            Button(role: .destructive) { Task { await cancel(f) } } label: { Label("取消", systemImage: "xmark") }
        }
    }

    private func load() async {
        guard let api = model.api else { return }
        do {
            followups = try await api.followups().sorted { $0.at < $1.at }
            error = nil
        } catch {
            self.error = error.shown ?? self.error
        }
        loaded = true
    }

    private func cancel(_ f: APIClient.Followup) async {
        guard let api = model.api else { return }
        withAnimation { followups.removeAll { $0.id == f.id } }
        do {
            try await api.cancelFollowup(f.id)
            Haptics.success()
        } catch {
            model.show(error)
            await load()
        }
    }
}

/// Icon and text closer together than the default label.
struct AssistantCompactLabel: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 3) { configuration.icon; configuration.title }
    }
}

// MARK: - 记忆

/// One memory file split into its frontmatter and body.
struct AssistantMemoryEntry: Identifiable, Hashable {
    var file: APIClient.MemoryFile
    var name: String?
    var description: String?
    var type: String?
    var body: String
    var id: String { file.name }

    static let index = "MEMORY.md"

    init(_ file: APIClient.MemoryFile) {
        self.file = file
        body = file.content
        let lines = file.content.components(separatedBy: "\n")
        guard lines.first?.trimmingCharacters(in: .whitespaces) == "---",
              let end = lines.dropFirst().firstIndex(where: { $0.trimmingCharacters(in: .whitespaces) == "---" }) else { return }
        for line in lines[1..<end] {
            guard let colon = line.firstIndex(of: ":") else { continue }
            let key = line[..<colon].trimmingCharacters(in: .whitespaces)
            var value = line[line.index(after: colon)...].trimmingCharacters(in: .whitespaces)
            if value.count >= 2, let q = value.first, q == "\"" || q == "'", value.last == q { value = String(value.dropFirst().dropLast()) }
            guard !value.isEmpty else { continue }
            switch key { // `type` sits under `metadata:` (indented); older files have it at the top level
            case "name": name = value
            case "description": description = value
            case "type": type = value
            default: break
            }
        }
        body = lines[(end + 1)...].joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var typeTitle: String? {
        switch type {
        case "user": "用户"
        case "feedback": "反馈"
        case "project": "项目"
        case "reference": "参考"
        default: type
        }
    }

    var typeColor: Color {
        switch type {
        case "user": Theme.accent
        case "feedback": Theme.attention
        case "project": Theme.idle
        case "reference": Theme.done
        default: .secondary
        }
    }
}

struct AssistantMemoryView: View {
    @Environment(AppModel.self) private var model
    @State private var files: [APIClient.MemoryFile] = []
    @State private var loaded = false
    @State private var error: String?
    @State private var pendingDelete: AssistantMemoryEntry?
    @State private var debugOpen: AssistantMemoryEntry?

    private var entries: [AssistantMemoryEntry] {
        files.filter { $0.name != AssistantMemoryEntry.index }.map(AssistantMemoryEntry.init)
            .sorted { ($0.file.updatedAt ?? .distantPast) > ($1.file.updatedAt ?? .distantPast) }
    }
    private var index: APIClient.MemoryFile? { files.first { $0.name == AssistantMemoryEntry.index } }

    /// The index lines "- [标题](file.md) — 说明", by file: friendlier titles than the slugs in the frontmatter.
    private var indexTitles: [String: (title: String, hook: String?)] {
        guard let index else { return [:] }
        var out: [String: (String, String?)] = [:]
        for line in index.content.split(whereSeparator: \.isNewline) {
            guard let open = line.firstIndex(of: "["), let close = line[open...].firstIndex(of: "]"),
                  line[close...].hasPrefix("]("), let paren = line[close...].firstIndex(of: ")") else { continue }
            let title = String(line[line.index(after: open)..<close])
            let file = String(line[line.index(close, offsetBy: 2)..<paren])
            let rest = line[line.index(after: paren)...].trimmingCharacters(in: .whitespaces)
            let hook = rest.drop { "—–-: ".contains($0) }.trimmingCharacters(in: .whitespaces)
            out[file] = (title, hook.isEmpty ? nil : hook)
        }
        return out
    }

    var body: some View {
        let entries = entries, titles = indexTitles
        List {
            if !entries.isEmpty {
                Section {
                    ForEach(entries) { e in
                        let t = titles[e.file.name]
                        NavigationLink {
                            AssistantMemoryDetail(entry: e, title: t?.title, onSaved: { await load() })
                        } label: {
                            row(e, title: t?.title ?? e.name ?? e.file.name, detail: e.description ?? t?.hook)
                        }
                        .swipeActions(edge: .trailing) {
                            Button { pendingDelete = e } label: { Label("删除", systemImage: "trash") }.tint(Theme.danger)
                        }
                    }
                } footer: {
                    Text("Jarvis 记住的事，一条一个文件，新对话开始时会读。说错的地方可以直接改，或者在对话里告诉它。")
                }
            }
            if let index, !entries.isEmpty {
                Section {
                    NavigationLink {
                        AssistantMemoryDetail(entry: AssistantMemoryEntry(index), title: "索引", onSaved: nil)
                    } label: {
                        Label { Text("索引").foregroundStyle(Color.primary) } icon: { Image(systemName: "list.bullet.rectangle") }
                    }
                } footer: {
                    Text("MEMORY.md：Jarvis 自己维护的目录，删除记忆时会一起更新。")
                }
            }
        }
        .themedPage()
        .overlay {
            if !loaded {
                ProgressView()
            } else if let error, files.isEmpty {
                ContentUnavailableView("读不到记忆", systemImage: "exclamationmark.triangle", description: Text(error))
            } else if entries.isEmpty {
                ContentUnavailableView("还没有记忆", systemImage: "brain",
                                       description: Text("Jarvis 会把你的偏好、纠正过它的地方记在这里。"))
            }
        }
        .navigationTitle("记忆")
        .confirmationDialog("删除这条记忆？", isPresented: Binding(get: { pendingDelete != nil }, set: { if !$0 { pendingDelete = nil } }),
                            titleVisibility: .visible, presenting: pendingDelete) { e in
            Button("删除", role: .destructive) { Task { await delete(e) } }
        } message: { e in
            Text("Jarvis 以后不再记得「\(titles[e.file.name]?.title ?? e.name ?? e.file.name)」。")
        }
        .navigationDestination(item: $debugOpen) { e in
            AssistantMemoryDetail(entry: e, title: titles[e.file.name]?.title, onSaved: { await load() })
        }
        .task { await load() }
        .refreshable { await load() }
    }

    private func row(_ e: AssistantMemoryEntry, title: String, detail: String?) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(title).font(.body.weight(.semibold)).lineLimit(1)
                Spacer(minLength: 4)
                if let t = e.typeTitle { AssistantMemoryTag(title: t, color: e.typeColor) }
            }
            if let detail {
                Text(detail).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
            }
        }
        .padding(.vertical, 2)
    }

    private func load() async {
        guard let api = model.api else { return }
        do {
            files = try await api.assistantMemory()
            error = nil
        } catch {
            self.error = error.shown ?? self.error
        }
        loaded = true
        #if DEBUG
        if ["memoryFile", "memoryEdit"].contains(UserDefaults.standard.string(forKey: "openAssistantPage")), debugOpen == nil {
            debugOpen = entries.first
        }
        #endif
    }

    private func delete(_ e: AssistantMemoryEntry) async {
        guard let api = model.api else { return }
        do {
            try await api.deleteMemory(name: e.file.name)
            Haptics.success()
            await load()
        } catch {
            model.show(error)
        }
    }
}

struct AssistantMemoryTag: View {
    var title: String
    var color: Color

    var body: some View {
        Text(title)
            .font(.caption2.weight(.semibold))
            .lineLimit(1)
            .foregroundStyle(color)
            .padding(.horizontal, 7)
            .padding(.vertical, 2)
            .background(Capsule().fill(color.opacity(0.14)))
    }
}

/// One memory, rendered; 编辑 opens the whole file (frontmatter included) as plain text. `onSaved == nil`: read-only.
struct AssistantMemoryDetail: View {
    @Environment(AppModel.self) private var model
    @State var entry: AssistantMemoryEntry
    var title: String?
    var onSaved: (() async -> Void)?
    @State private var editing = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                if onSaved != nil {
                    VStack(alignment: .leading, spacing: 6) {
                        HStack(spacing: 8) {
                            if let t = entry.typeTitle { AssistantMemoryTag(title: t, color: entry.typeColor) }
                            if let at = entry.file.updatedAt {
                                Text("更新于 \(Fmt.relative(at))").font(.caption).foregroundStyle(.secondary)
                            }
                        }
                        if let d = entry.description {
                            Text(d).font(.subheadline).foregroundStyle(.secondary)
                        }
                    }
                    Divider()
                }
                Markdown(entry.body.isEmpty ? "（空）" : entry.body)
                    .markdownTheme(.jarvis)
                    .textSelection(.enabled)
                    .environment(\.openURL, OpenURLAction { url in url.scheme == nil ? .discarded : .systemAction })
                Text(entry.file.name).font(.caption.monospaced()).foregroundStyle(.tertiary).padding(.top, 8)
            }
            .padding(20)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .themedPage()
        .navigationTitle(title ?? entry.name ?? entry.file.name)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if onSaved != nil {
                ToolbarItem(placement: .primaryAction) { Button("编辑") { editing = true } }
            }
        }
        .sheet(isPresented: $editing) {
            AssistantMemoryEditor(name: entry.file.name, text: entry.file.content) { content in
                var f = entry.file
                f.content = content
                f.updatedAt = .now
                entry = AssistantMemoryEntry(f)
                await onSaved?()
            }
        }
        #if DEBUG
        .onAppear { if UserDefaults.standard.string(forKey: "openAssistantPage") == "memoryEdit", onSaved != nil { editing = true } }
        #endif
    }
}

struct AssistantMemoryEditor: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var name: String
    @State var text: String
    var saved: (String) async -> Void
    @State private var saving = false

    var body: some View {
        NavigationStack {
            TextEditor(text: $text)
                .font(.callout.monospaced())
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .scrollContentBackground(.hidden)
                .padding(.horizontal, 12)
                .background(Theme.pageBackground)
                .navigationTitle(name)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                    ToolbarItem(placement: .confirmationAction) {
                        Button { Task { await save() } } label: { if saving { ProgressView() } else { Text("保存") } }
                            .disabled(saving)
                    }
                }
        }
        .interactiveDismissDisabled(saving)
    }

    private func save() async {
        guard let api = model.api else { return }
        saving = true
        defer { saving = false }
        do {
            try await api.saveMemory(name: name, content: text)
            await saved(text)
            Haptics.success()
            dismiss()
        } catch {
            model.show(error)
        }
    }
}

// MARK: - 关于我

/// What the assistant is told about the user; saved when leaving the page (or with 保存).
struct AssistantNotesView: View {
    @Environment(AppModel.self) private var model
    @State private var text = ""
    @State private var original: String?
    @State private var saving = false
    @FocusState private var focused: Bool

    private var changed: Bool { original != nil && text != original }

    var body: some View {
        Form {
            Section {
                TextEditor(text: $text)
                    .font(.callout)
                    .frame(minHeight: 380)
                    .focused($focused)
                    .listRowInsets(EdgeInsets(top: 8, leading: 12, bottom: 8, trailing: 12))
            } footer: {
                Text("这些内容会附在 Jarvis 的说明最后；改了以后新对话生效。可以写代码放在哪、常用哪些 skills、哪些机器或目录别碰、你习惯的汇报方式。")
            }
        }
        .themedPage()
        .navigationTitle("关于我")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button { Task { await save() } } label: { if saving { ProgressView() } else { Text("保存") } }
                    .disabled(!changed || saving)
            }
        }
        .task {
            guard original == nil else { return }
            if model.assistantConfig == nil { await model.refreshAssistant() }
            text = model.assistantConfig?.notes ?? ""
            original = text
        }
        .onDisappear { if changed { Task { await save() } } }
    }

    private func save() async {
        guard let api = model.api, changed else { return }
        saving = true
        defer { saving = false }
        let value = text
        do {
            _ = try await api.setAssistant(notes: value)
            original = value
            focused = false
            await model.refreshAssistant()
            Haptics.success()
            model.toast = Toast("已保存，新对话生效", style: .success)
        } catch {
            model.show(error)
        }
    }
}
