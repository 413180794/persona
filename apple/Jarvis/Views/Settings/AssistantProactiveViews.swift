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

    /// The rule in words, as Reminders says it: "每个工作日 早上 9:00" / "每天 晚上 9:30" / "每周五 下午 5:00" /
    /// "每 30 分钟" / "一次 · 今天 15:30".
    static func sentence(_ r: APIClient.ScheduledTask) -> String {
        switch r.repeats {
        case .interval: return every(r.everyMinutes)
        case .once: return r.at.map { "一次 · \(moment($0))" } ?? "一次"
        case .daily: break
        }
        let d = Set(r.weekdays)
        let days: String
        if d.isEmpty || d.count == 7 { days = "每天" }
        else if d == Set(workdays) { days = "每个工作日" }
        else if d == [6, 7] { days = "每个周末" }
        else { days = "每周" + d.sorted().map { dayNames[$0 - 1] }.joined(separator: "、") }
        return "\(days) \(clock(r.time))"
    }

    /// "09:00" → "早上 9:00", "21:30" → "晚上 9:30", "00:15" → "凌晨 12:15".
    static func clock(_ hhmm: String) -> String {
        let parts = hhmm.split(separator: ":").compactMap { Int($0) }
        guard parts.count == 2 else { return hhmm }
        let (h, m) = (parts[0], parts[1])
        let part = switch h {
        case 0..<5: "凌晨"
        case 5..<9: "早上"
        case 9..<12: "上午"
        case 12..<13: "中午"
        case 13..<18: "下午"
        default: "晚上"
        }
        let h12 = h % 12 == 0 ? 12 : h % 12
        return String(format: "%@ %d:%02d", part, h12, m)
    }

    /// The day part of a moment: "今天" / "明天" / "昨天" / "周三" / "11月2日".
    static func day(_ date: Date, now: Date = .now) -> String {
        let cal = Calendar.current
        if cal.isDateInToday(date) { return "今天" }
        if cal.isDateInTomorrow(date) { return "明天" }
        if cal.isDateInYesterday(date) { return "昨天" }
        let days = cal.dateComponents([.day], from: cal.startOfDay(for: now), to: cal.startOfDay(for: date)).day ?? 0
        if abs(days) < 7 { return "周\(dayNames[(cal.component(.weekday, from: date) + 5) % 7])" }
        return "\(cal.component(.month, from: date))月\(cal.component(.day, from: date))日"
    }

    /// "每 30 分钟" / "每 2 小时" / "每 1 小时 30 分钟".
    static func every(_ minutes: Int) -> String {
        if minutes < 60 { return "每 \(minutes) 分钟" }
        return minutes % 60 == 0 ? "每 \(minutes / 60) 小时" : "每 \(minutes / 60) 小时 \(minutes % 60) 分钟"
    }

    /// The intervals the editor offers.
    static let intervals = [5, 10, 15, 20, 30, 45, 60, 90, 120, 180, 240, 360, 480, 720, 1440]

    /// Its title, or the first line of the prompt for a follow-up without one.
    static func title(_ r: APIClient.ScheduledTask) -> String {
        if !r.title.isEmpty { return r.title }
        return r.prompt.split(whereSeparator: \.isNewline).first.map(String.init) ?? r.prompt
    }

    /// What it runs with when that isn't the default, in a few words: "claude-fast · 自动改文件 · 思考低"; `danger`
    /// for 全部放行.
    static func runsWith(_ r: APIClient.ScheduledTask) -> (text: String, danger: Bool)? {
        let parts = [r.launcher, SessionModes.modeLabel(r.permissionMode), r.effort.map { "思考\(SessionModes.effortLabel($0) ?? $0)" }]
            .compactMap { $0 }
        return parts.isEmpty ? nil : (parts.joined(separator: " · "), r.permissionMode == "bypassPermissions")
    }

    /// The last run in a few words, nil when it never ran: "上次 今天 09:00 · 成功".
    static func lastRun(_ r: APIClient.ScheduledTask) -> (text: String, failed: Bool)? {
        guard let at = r.lastRun else { return nil }
        let word = switch r.lastStatus {
        case "failed": "失败"
        case "skipped": "已跳过"
        case "ok": "已运行"
        default: "已运行"
        }
        return ("上次 \(moment(at)) · \(word)", r.lastStatus == "failed")
    }

    /// An SF Symbol for a task, from its title.
    static func symbol(_ r: APIClient.ScheduledTask) -> String {
        let t = r.title
        if t.contains("早") { return "sun.horizon" }
        if t.contains("晚") || t.contains("回顾") { return "moon.stars" }
        if t.contains("周报") || t.contains("报告") { return "doc.text" }
        if t.contains("会") { return "person.2" }
        if t.contains("盘点") || t.contains("待办") { return "checklist" }
        if t.contains("巡检") || t.contains("检查") { return "binoculars" }
        return switch r.repeats {
        case .once: "arrow.turn.down.right"
        case .interval: "arrow.triangle.2.circlepath"
        case .daily: "alarm"
        }
    }

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

    /// "17:05", always 24-hour: `.hour(.twoDigits(amPM: .omitted))` in a Chinese locale gives the 12-hour "05:05".
    static func hm(_ date: Date) -> String { hmFormatter.string(from: date) }
    private static let hmFormatter: DateFormatter = {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "HH:mm"
        return f
    }()

    /// "今天 15:30" / "明天 09:00" / "周三 09:00" / "11月2日 09:00".
    static func moment(_ date: Date, now: Date = .now) -> String {
        let cal = Calendar.current
        let time = hm(date)
        if cal.isDateInToday(date) { return "今天 \(time)" }
        if cal.isDateInTomorrow(date) { return "明天 \(time)" }
        let days = cal.dateComponents([.day], from: cal.startOfDay(for: now), to: cal.startOfDay(for: date)).day ?? 0
        if days > 1, days < 7 { return "周\(dayNames[(cal.component(.weekday, from: date) + 5) % 7]) \(time)" }
        return "\(cal.component(.month, from: date))月\(cal.component(.day, from: date))日 \(time)"
    }
}

// MARK: - 定时任务

/// Everything Jarvis does on its own, on one page: what runs next, what repeats (every day at a time, every N
/// minutes; the rule in words, next and last run, an on/off switch), the one-offs it scheduled from a conversation,
/// and the history of runs. Each opens the same editor: when, the prompt, and what it runs with.
struct AssistantRoutinesView: View {
    @State private var page = AssistantRoutinesModel()
    @State private var editing: APIClient.ScheduledTask?
    @State private var creating: APIClient.ScheduledTask?

    var body: some View {
        let repeating = page.repeating, oneOff = page.oneOff, lastRuns = page.lastRuns, upcoming = page.upcoming
        List {
            if !upcoming.isEmpty {
                Section {
                    ForEach(upcoming) { u in AssistantUpcomingRow(item: u) }
                } header: {
                    Text("接下来")
                }
            }
            if !repeating.isEmpty {
                Section {
                    ForEach(repeating) { t in row(t) }
                } header: {
                    Text("定时任务")
                } footer: {
                    Text("到点时 Jarvis 按提示词去做，跑完推送到手机。时间按手机所在的时区。")
                }
            }
            if !oneOff.isEmpty {
                Section {
                    ForEach(oneOff) { t in row(t) }
                } header: {
                    Text("一次性跟进")
                } footer: {
                    Text("Jarvis 在对话里替自己安排的，到点自己醒来接着做。左滑删除，长按可以立即运行。")
                }
            }
            if !lastRuns.isEmpty {
                Section {
                    ForEach(lastRuns.prefix(3)) { r in AssistantRunRow(run: r) }
                    NavigationLink { AssistantRunsView() } label: { Text("全部运行记录").lineLimit(1) }
                } header: {
                    Text("最近运行")
                }
            }
        }
        .themedPage()
        .overlay {
            if !page.loaded {
                ProgressView()
            } else if let error = page.error, page.tasks.isEmpty {
                ContentUnavailableView("读不到定时任务", systemImage: Theme.Icon.error, description: Text(error))
            } else if page.tasks.isEmpty {
                ContentUnavailableView("没有定时任务", systemImage: Theme.Icon.schedule,
                                       description: Text("点右上角的 ＋，或者在对话里说「每天 9 点给我早报」「每 30 分钟巡检一次」，让 Jarvis 定时做一件事。"))
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
                    Button { creating = AssistantRoutineTemplate.blank } label: { Label("空白", systemImage: Theme.Icon.compose) }
                } label: {
                    Image(systemName: Theme.Icon.add)
                }
                .accessibilityLabel("新建定时任务")
            }
        }
        .navigationDestination(item: $editing) { t in
            AssistantTaskEditor(task: t, save: page.save, delete: page.delete)
        }
        .sheet(item: $creating) { t in
            NavigationStack { AssistantTaskEditor(task: t, save: page.save, delete: page.delete) }
        }
        .task { await load() }
        .refreshable { await page.load() }
    }

    private func row(_ t: APIClient.ScheduledTask) -> some View {
        AssistantTaskRow(task: t, open: { editing = t }) { on in
            var changed = t
            changed.enabled = on
            Haptics.tap()
            Task { _ = await page.save(changed) }
        }
        .swipeActions(edge: .trailing, allowsFullSwipe: t.repeats == .once) {
            Button(role: .destructive) { Task { await page.delete(t) } } label: { Label("删除", systemImage: Theme.Icon.delete) }
        }
        .contextMenu {
            Button { Task { await page.run(t) } } label: { Label("立即运行", systemImage: Theme.Icon.run) }
            Button { editing = t } label: { Label("编辑", systemImage: Theme.Icon.compose) }
            Button(role: .destructive) { Task { await page.delete(t) } } label: { Label("删除", systemImage: Theme.Icon.delete) }
        }
    }

    private func load() async {
        await page.load()
        #if DEBUG
        switch UserDefaults.standard.string(forKey: "openAssistantPage") {
        case "routine" where editing == nil: editing = page.repeating.first
        case "intervalTask" where editing == nil: editing = page.repeating.first { $0.repeats == .interval }
        case "followup" where editing == nil: editing = page.oneOff.first
        case "newRoutine" where creating == nil: creating = AssistantRoutineTemplate.all.last?.routine
        default: break
        }
        #endif
    }
}

/// One task on 定时任务: its icon, title, the rule in words, next and last run, what it runs with when that isn't the
/// default, and an on/off switch. Tapping opens the editor.
struct AssistantTaskRow: View {
    var task: APIClient.ScheduledTask
    var open: () -> Void
    var toggle: (Bool) -> Void

    var body: some View {
        let t = task
        HStack(spacing: Theme.Space.m) {
            Image(systemName: AssistantScheduleText.symbol(t))
                .font(Theme.Font.heading)
                .foregroundStyle(.white)
                .scaledFrame(width: Theme.Size.wellM, height: Theme.Size.wellM)
                .background((t.enabled ? Theme.accent : Color.secondary).gradient, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                HStack(spacing: Theme.Space.sm) {
                    Text(AssistantScheduleText.title(t)).font(Theme.Font.heading).lineLimit(1)
                    if t.createdBy == "assistant" && t.repeats != .once {
                        Text("Jarvis 建的").font(Theme.Font.labelMedium).foregroundStyle(Theme.accent)
                            .padding(.horizontal, Theme.Space.sm).padding(.vertical, 1)
                            .background(Theme.accent.opacity(0.12), in: .capsule)
                            .lineLimit(1)
                            .fixedSize()
                    }
                }
                Text(t.repeats == .once ? (t.sessionID == nil ? "一次 · 新对话" : "一次 · 接着原来的对话") : AssistantScheduleText.sentence(t))
                    .font(Theme.Font.secondary)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                Group {
                    if !t.enabled {
                        Text("已关闭")
                    } else if let next = t.nextRun ?? t.at {
                        Text("下次 \(AssistantScheduleText.moment(next)) · \(AssistantScheduleText.until(next))")
                    }
                }
                .font(Theme.Font.label)
                .foregroundStyle(.secondary)
                .monospacedDigit()
                .lineLimit(1)
                if let with = AssistantScheduleText.runsWith(t) {
                    Label(with.text, systemImage: with.danger ? Theme.Icon.allowEverything : Theme.Icon.effort)
                        .labelStyle(AssistantCompactLabel())
                        .font(Theme.Font.label)
                        .foregroundStyle(with.danger ? Theme.danger : Color.secondary)
                        .lineLimit(1)
                }
                if let last = AssistantScheduleText.lastRun(t) {
                    Label(last.text, systemImage: last.failed ? Theme.Icon.errorFilled : Theme.Icon.done)
                        .labelStyle(AssistantCompactLabel())
                        .font(Theme.Font.label)
                        .foregroundStyle(last.failed ? Theme.danger : Color.secondary)
                        .lineLimit(1)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(.rect)
            .onTapGesture(perform: open)
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(.isButton)
            .accessibilityIdentifier("task-\(t.id)")
            Toggle(AssistantScheduleText.title(t), isOn: Binding(get: { t.enabled }, set: toggle))
                .labelsHidden()
                .accessibilityIdentifier("task-switch-\(t.id)")
        }
        .padding(.vertical, Theme.Space.xxs)
        .opacity(t.enabled ? 1 : 0.75)
    }
}

/// Starting points for the ＋ menu.
struct AssistantRoutineTemplate: Identifiable {
    var routine: APIClient.ScheduledTask
    var symbol: String
    var id: String { routine.title }

    static let blank = APIClient.ScheduledTask(title: "", prompt: "", time: "09:00", weekdays: AssistantScheduleText.workdays)

    static let all: [AssistantRoutineTemplate] = [
        .init(routine: .init(title: "会前准备", prompt: "看看今天飞书日历和 iPhone 日历里的会。对每个需要准备的会，用 schedule_followup 在会前 10 分钟" +
                             "安排一次会前准备：相关文档、上次的会议纪要、还没定的事、我该说的要点。今天没有要准备的会就简单说一声。",
                             time: "08:45", weekdays: AssistantScheduleText.workdays), symbol: "person.2.badge.gearshape"),
        .init(routine: .init(title: "周报", prompt: "帮我起草这周的周报：从飞书日历、会议纪要、文档和消息里整理这周做了什么、进展和结果、" +
                             "遇到的问题、下周计划；各台电脑上的 Agent 这周完成的主要改动也算进去。先给我草稿，问我要不要改，不要直接发出去。",
                             time: "17:00", weekdays: [5]), symbol: Theme.Icon.document),
        .init(routine: .init(title: "Agent 巡检", prompt: "看看各台电脑上的 Agent：哪些在等我、哪些卡住或报错了、哪些跑完了没人看。" +
                             "有要我处理的就列出来（一行一个，说清楚在哪台电脑、等什么）；都正常就回一句「都正常」。",
                             repeats: .interval, everyMinutes: 30), symbol: "binoculars"),
        .init(routine: .init(title: "下班前盘点", prompt: "下班前盘点：今天还有哪些消息没回、答应别人的事还没做、各台电脑上还在跑或在等我的 Agent，" +
                             "明天上午有哪些会要准备。按紧急程度列出来，最后问我哪些要设提醒。",
                             time: "18:30", weekdays: AssistantScheduleText.workdays), symbol: Theme.Icon.todo),
    ]
}

/// Edits one task: when (once / every day / every N minutes), the prompt, what it runs with (launcher, permission
/// mode, effort) and which conversation runs go into. An existing one is saved when leaving the page (like iOS
/// Settings); a new one comes up as a sheet with 取消 / 添加, so trying a template and backing out doesn't create
/// anything.
struct AssistantTaskEditor: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var draft: APIClient.ScheduledTask
    /// What the hub has; nil until a new task is first saved.
    @State private var saved: APIClient.ScheduledTask?
    @State private var running = false
    @State private var confirmDelete = false
    @State private var confirmBypass = false
    @State private var deleted = false
    @State private var runs: [APIClient.ScheduleRun] = []
    @State private var launchers: [Launcher] = []
    /// Presented as the "new task" sheet: nothing is saved unless 添加 (or 立即运行) is tapped.
    private let creating: Bool
    /// The conversation it came from, to switch back to after turning 接着原来的对话 off.
    private let conversation: String?
    var save: (APIClient.ScheduledTask) async -> APIClient.ScheduledTask?
    var delete: (APIClient.ScheduledTask) async -> Void

    init(task: APIClient.ScheduledTask, save: @escaping (APIClient.ScheduledTask) async -> APIClient.ScheduledTask?,
         delete: @escaping (APIClient.ScheduledTask) async -> Void) {
        _draft = State(initialValue: task)
        _saved = State(initialValue: task.id.isEmpty ? nil : task)
        creating = task.id.isEmpty
        conversation = task.sessionID
        self.save = save
        self.delete = delete
    }

    private var isValid: Bool {
        let titled = draft.repeats == .once || !draft.title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        return titled && !draft.prompt.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
    private var changed: Bool { saved.map { !Self.same($0, draft) } ?? true }
    private var machineID: String { model.assistantConfig?.machineID ?? model.hubMachineID }

    var body: some View {
        Form {
            Section {
                TextField(draft.repeats == .once ? "标题（可不填）" : "标题，比如「早报」", text: $draft.title)
                Toggle("开启", isOn: $draft.enabled)
            }
            whenSection
            Section {
                #if os(macOS)
                // A grouped Mac form shows a titled field as label + trailing value: the prompt took the right half.
                TextField("要 Jarvis 做什么", text: $draft.prompt, prompt: Text("要 Jarvis 做什么"), axis: .vertical)
                    .labelsHidden()
                    .lineLimit(8...)
                #else
                TextField("要 Jarvis 做什么", text: $draft.prompt, axis: .vertical)
                    .lineLimit(8...)
                #endif
            } header: {
                Text("提示词")
            } footer: {
                Text("像在对话里一样写：看哪些数据、怎么汇报。它可以用 schedule_followup 再安排一次性的跟进，比如会前 10 分钟。")
            }
            runsWithSection
            if conversation != nil {
                Section {
                    Toggle("接着原来的对话", isOn: Binding(get: { draft.sessionID != nil }, set: { draft.sessionID = $0 ? conversation : nil }))
                } footer: {
                    Text(draft.sessionID != nil ? "每次运行都发进安排它的那段对话，带着那里的上下文接着做。" : "每次运行都开一段新对话。")
                }
            }
            Section {
                Button { Task { await runNow() } } label: {
                    HStack {
                        Label("立即运行", systemImage: Theme.Icon.run).lineLimit(1)
                        Spacer()
                        if running { ProgressView() }
                    }
                }
                .disabled(running || !isValid)
                if let last = saved?.lastSessionID, !last.isEmpty {
                    Button { open(SessionKey(machineID: machineID, sessionID: last)) } label: {
                        HStack {
                            Label("查看上次结果", systemImage: Theme.Icon.conversation).lineLimit(1)
                            Spacer()
                            if let at = saved?.lastRun {
                                Text(Fmt.relative(at)).font(Theme.Font.body).foregroundStyle(.secondary).lineLimit(1)
                            }
                        }
                    }
                }
            } footer: {
                if saved?.lastStatus == "failed", let err = saved?.lastError {
                    Text("上次没有跑起来：\(err)").foregroundStyle(Theme.danger)
                } else {
                    Text("立即运行不改它的时间，跑完在「对话」里看结果。")
                }
            }
            if !runs.isEmpty {
                Section {
                    ForEach(runs.prefix(5)) { r in AssistantRunRow(run: r, showTitle: false) }
                    if runs.count > 5, let id = saved?.id {
                        NavigationLink { AssistantRunsView(ref: id) } label: { Text("全部 \(runs.count) 次").lineLimit(1) }
                    }
                } header: {
                    Text("历史记录")
                }
            }
            if !creating {
                Section {
                    Button(role: .destructive) { confirmDelete = true } label: {
                        Label("删除定时任务", systemImage: Theme.Icon.delete).lineLimit(1)
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
        .confirmationDialog("删除「\(AssistantScheduleText.title(draft))」？", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("删除", role: .destructive) {
                guard let s = saved else { return }
                deleted = true
                Task { await delete(s) }
                dismiss()
            }
        } message: {
            Text("删除后不再定时运行，已经跑过的对话还在。")
        }
        .alert("全部放行？", isPresented: $confirmBypass) {
            Button("全部放行", role: .destructive) { draft.permissionMode = "bypassPermissions" }
            Button("取消", role: .cancel) {}
        } message: {
            Text("这个任务运行时，跑任何命令、改任何文件、用任何工具都不再问你，也没有人能中途拦下。只给你信得过、出了错也能接受的提示词用。")
        }
        .onDisappear { if !creating { Task { await persist(leaving: true) } } }
        .task(id: saved?.id) {
            guard let id = saved?.id, !id.isEmpty, let api = model.api else { return }
            runs = (try? await api.scheduleRuns(ref: id)) ?? []
        }
        .task { launchers = ((try? await model.launchers(machineID)) ?? []).filter(\.runsAssistant) }
        #if DEBUG
        .onAppear { if UserDefaults.standard.string(forKey: "openAssistantPage") == "bypassTask" { confirmBypass = true } }
        #endif
    }

    @ViewBuilder private var whenSection: some View {
        Section {
            Picker("重复", selection: repeats) {
                Text("一次").tag(APIClient.ScheduledTask.Repeat.once)
                Text("每天").tag(APIClient.ScheduledTask.Repeat.daily)
                Text("每隔").tag(APIClient.ScheduledTask.Repeat.interval)
            }
            .pickerStyle(.segmented)
            .listRowInsets(EdgeInsets(top: 10, leading: 16, bottom: 10, trailing: 16))
            switch draft.repeats {
            case .once:
                DatePicker("时间", selection: at, in: Date.now..., displayedComponents: [.date, .hourAndMinute])
            case .daily:
                DatePicker("时间", selection: time, displayedComponents: .hourAndMinute)
                AssistantWeekdayPicker(days: $draft.weekdays)
                    .listRowInsets(EdgeInsets(top: 12, leading: 16, bottom: 12, trailing: 16))
            case .interval:
                Picker("间隔", selection: $draft.everyMinutes) {
                    ForEach(intervals, id: \.self) { m in Text(AssistantScheduleText.every(m)).tag(m) }
                }
            }
        } header: {
            Text("什么时候")
        } footer: {
            if draft.enabled, let next = saved?.nextRun, saved.map({ Self.same($0, draft) }) == true {
                Text("下次运行：\(AssistantScheduleText.moment(next))（\(AssistantScheduleText.until(next))）")
            } else {
                switch draft.repeats {
                case .once: Text("到点运行一次，跑完就从列表里拿掉（运行记录里还在）。")
                case .daily: Text("\(AssistantScheduleText.repeats(draft.weekdays)) \(draft.time)，按手机所在的时区。")
                case .interval: Text("\(AssistantScheduleText.every(draft.everyMinutes))运行一次，从保存时算起。")
                }
            }
        }
    }

    /// The model (launcher), permission mode and effort a run starts claude with; "" in a picker = the default.
    private var runsWithSection: some View {
        Section {
            Picker(selection: optional(\.launcher)) {
                Text("跟随 Jarvis（\(model.assistantConfig?.launcher ?? "claude")）").tag("")
                ForEach(launcherNames, id: \.self) { name in
                    let l = launchers.first { $0.name == name }
                    Text([name, Fmt.model(l?.model)].compactMap { $0 }.joined(separator: " · ")).tag(name)
                }
            } label: {
                Label("模型", systemImage: Theme.Icon.cpu).lineLimit(1)
            }
            // A Menu around an inline picker: a menu-style Picker greys out a value that has an icon, and 全部放行 is red.
            LabeledContent {
                Menu {
                    Picker("权限模式", selection: mode) {
                        Label { Text("默认"); Text("和平时一样：改文件、跑命令前先问你") } icon: { Image(systemName: Theme.Icon.askFirst) }.tag("")
                        ForEach(SessionModes.permissionModes.filter { $0.id != "default" }) { o in
                            Label { Text(o.title); Text(o.id == "bypassPermissions" ? "什么都不问，有风险" : o.detail) } icon: { Image(systemName: o.symbol) }
                                .tag(o.id)
                        }
                    }
                    .pickerStyle(.inline)
                } label: {
                    HStack(spacing: Theme.Space.xs) {
                        Text(SessionModes.modeLabel(draft.permissionMode) ?? "默认").lineLimit(1)
                        Image(systemName: Theme.Icon.choices).font(Theme.Font.labelStrong).imageScale(.small)
                    }
                    .foregroundStyle(draft.permissionMode == "bypassPermissions" ? Theme.danger : Theme.accent)
                }
                .accessibilityIdentifier("task-mode")
            } label: {
                Label("权限模式", systemImage: Theme.Icon.askFirst).lineLimit(1)
            }
            Picker(selection: optional(\.effort)) {
                Text("跟随启动器").tag("")
                ForEach(SessionModes.effortLevels) { o in Text(o.title).tag(o.id) }
            } label: {
                Label("思考强度", systemImage: Theme.Icon.effort).lineLimit(1)
            }
        } header: {
            Text("运行方式")
        } footer: {
            if draft.permissionMode == "bypassPermissions" {
                Label("全部放行：运行时跑命令、改文件都不再问你。", systemImage: "exclamationmark.shield.fill")
                    .foregroundStyle(Theme.danger)
            } else {
                Text("定时任务在后台跑，要你批准时会推送到手机，没人点就一直等着。要它跑命令也不问你，可以选「自动」（自己判断风险）。"
                     + (draft.sessionID != nil ? "接着原来的对话运行时，那段对话也会换成这里的设置。" : ""))
            }
        }
    }

    /// The hub's launchers, plus the task's own when the list doesn't have it (yet).
    private var launcherNames: [String] {
        var names = launchers.map(\.name)
        if let l = draft.launcher, !names.contains(l) { names.append(l) }
        return names
    }

    private var intervals: [Int] {
        var list = AssistantScheduleText.intervals
        if !list.contains(draft.everyMinutes) { list.append(draft.everyMinutes); list.sort() }
        return list
    }

    private func optional(_ path: WritableKeyPath<APIClient.ScheduledTask, String?>) -> Binding<String> {
        Binding { draft[keyPath: path] ?? "" } set: { draft[keyPath: path] = $0.isEmpty ? nil : $0 }
    }

    /// 全部放行 asks first.
    private var mode: Binding<String> {
        Binding { draft.permissionMode ?? "" } set: { new in
            if new == "bypassPermissions" && draft.permissionMode != new {
                confirmBypass = true
            } else {
                draft.permissionMode = new.isEmpty ? nil : new
            }
        }
    }

    /// Switching to 一次 picks a time an hour from now; to 每隔, half an hour.
    private var repeats: Binding<APIClient.ScheduledTask.Repeat> {
        Binding { draft.repeats } set: { r in
            withAnimation(Theme.Motion.standard) {
                draft.repeats = r
                if r == .once, (draft.at ?? .distantPast) < .now {
                    let hour = Calendar.current.dateInterval(of: .hour, for: .now.addingTimeInterval(3600))?.start ?? .now
                    draft.at = hour
                }
                if r == .interval, draft.everyMinutes < 5 { draft.everyMinutes = 30 }
            }
        }
    }

    private var at: Binding<Date> {
        Binding { draft.at ?? .now.addingTimeInterval(3600) } set: { draft.at = $0 }
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

    /// Ignores the hub-only fields (last / next run) and, outside its rule, the fields another rule uses.
    static func same(_ a: APIClient.ScheduledTask, _ b: APIClient.ScheduledTask) -> Bool {
        guard a.title == b.title, a.prompt == b.prompt, a.repeats == b.repeats, a.enabled == b.enabled, a.sessionID == b.sessionID,
              a.launcher == b.launcher, a.permissionMode == b.permissionMode, a.effort == b.effort else { return false }
        return switch a.repeats {
        case .once: a.at == b.at
        case .daily: a.time == b.time && Set(a.weekdays) == Set(b.weekdays)
        case .interval: a.everyMinutes == b.everyMinutes
        }
    }

    @discardableResult private func persist(leaving: Bool) async -> APIClient.ScheduledTask? {
        guard !deleted, changed else { return saved }
        guard isValid else {
            if leaving {
                model.toast = Toast(draft.repeats == .once ? "提示词要填，这次的修改没有保存" : "标题和提示词都要填，这次的修改没有保存", style: .error)
            }
            return nil
        }
        var t = draft
        t.title = t.title.trimmingCharacters(in: .whitespacesAndNewlines)
        if Set(t.weekdays).count == 7 { t.weekdays = [] }
        t.weekdays.sort()
        guard let s = await save(t) else { return nil }
        saved = s
        if !leaving { draft.id = s.id }
        return s
    }

    private func runNow() async {
        guard let api = model.api else { return }
        running = true
        defer { running = false }
        guard let t = await persist(leaving: false) else { return }
        do {
            let key = try await api.runTask(t.id)
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
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            HStack(spacing: 0) {
                ForEach(1...7, id: \.self) { d in
                    let on = selected.contains(d)
                    Button { toggle(d) } label: {
                        Text(AssistantScheduleText.dayNames[d - 1])
                            .font(Theme.Font.secondaryStrong)
                            .foregroundStyle(on ? Color.white : Color.primary)
                            .scaledFrame(width: Theme.Size.wellXL, height: Theme.Size.wellXL)
                            .background(Circle().fill(on ? AnyShapeStyle(Theme.accent) : AnyShapeStyle(Color.secondary.opacity(0.14))))
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("周\(AssistantScheduleText.dayNames[d - 1])")
                    .accessibilityAddTraits(on ? .isSelected : [])
                    .frame(maxWidth: .infinity)
                }
            }
            HStack(spacing: Theme.Space.s) {
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
            withAnimation(Theme.Motion.standard) { days = value }
        } label: {
            Text(title)
                .font(Theme.Font.captionMedium)
                .lineLimit(1)
                .padding(.horizontal, Theme.Space.m)
                .padding(.vertical, Theme.Space.sm)
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
        withAnimation(Theme.Motion.standard) { days = s.count == 7 ? [] : s.sorted() }
    }
}

/// Icon and text closer together than the default label.
struct AssistantCompactLabel: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: Theme.Space.xs) { configuration.icon; configuration.title }
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
        case "user": Theme.Hue.purple
        case "feedback": Theme.Hue.orange
        case "project": Theme.Hue.green
        case "reference": Theme.Hue.blue
        default: .secondary
        }
    }
}

struct AssistantMemoryView: View {
    @State private var page = AssistantMemoryModel()
    @State private var pendingDelete: AssistantMemoryEntry?
    @State private var debugOpen: AssistantMemoryEntry?

    private var files: [APIClient.MemoryFile] { page.files }
    private var loaded: Bool { page.loaded }
    private var error: String? { page.error }

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
                            Button { pendingDelete = e } label: { Label("删除", systemImage: Theme.Icon.delete) }.tint(Theme.danger)
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
                        Label { Text("索引").foregroundStyle(Color.primary) } icon: { Image(systemName: Theme.Icon.list) }
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
                ContentUnavailableView("读不到记忆", systemImage: Theme.Icon.error, description: Text(error))
            } else if entries.isEmpty {
                ContentUnavailableView("还没有记忆", systemImage: Theme.Icon.memory,
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
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
                Text(title).font(Theme.Font.heading).lineLimit(1)
                Spacer(minLength: 4)
                if let t = e.typeTitle { AssistantMemoryTag(title: t, color: e.typeColor) }
            }
            if let detail {
                Text(detail).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(2)
            }
        }
        .padding(.vertical, Theme.Space.xxs)
    }

    private func load() async {
        await page.load()
        #if DEBUG
        if ["memoryFile", "memoryEdit"].contains(UserDefaults.standard.string(forKey: "openAssistantPage")), debugOpen == nil {
            debugOpen = entries.first
        }
        #endif
    }

    private func delete(_ e: AssistantMemoryEntry) async { await page.delete(e.file.name) }
}

struct AssistantMemoryTag: View {
    var title: String
    var color: Color

    var body: some View {
        Text(title)
            .font(Theme.Font.labelStrong)
            .lineLimit(1)
            .foregroundStyle(color)
            .padding(.horizontal, Theme.Space.s)
            .padding(.vertical, Theme.Space.xxs)
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
            VStack(alignment: .leading, spacing: Theme.Space.l) {
                if onSaved != nil {
                    VStack(alignment: .leading, spacing: Theme.Space.sm) {
                        HStack(spacing: Theme.Space.s) {
                            if let t = entry.typeTitle { AssistantMemoryTag(title: t, color: entry.typeColor) }
                            if let at = entry.file.updatedAt {
                                Text("更新于 \(Fmt.relative(at))").font(Theme.Font.label).foregroundStyle(.secondary)
                            }
                        }
                        if let d = entry.description {
                            Text(d).font(Theme.Font.secondary).foregroundStyle(.secondary)
                        }
                    }
                    Divider()
                }
                Markdown(entry.body.isEmpty ? "（空）" : Links.markdown(entry.body))
                    .markdownTheme(.jarvis)
                    .textSelection(.enabled)
                    .environment(\.openURL, OpenURLAction { url in
                        #if os(macOS)
                        url.scheme == nil ? .discarded : LinkPolicy.system(url) // NSWorkspace would run any scheme
                        #else
                        url.scheme == nil ? .discarded : .systemAction
                        #endif
                    })
                Text(entry.file.name).font(Theme.Font.code).foregroundStyle(.tertiary).padding(.top, Theme.Space.s)
            }
            .padding(Theme.Space.xl)
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
                .font(Theme.Font.body)
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .scrollContentBackground(.hidden)
                .padding(.horizontal, Theme.Space.m)
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
        #if os(macOS)
        // A Mac sheet sizes to its content: a text editor alone came up a few lines tall.
        .macSheet()
        #endif
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
                    .font(Theme.Font.body)
                    .scaledFrame(minHeight: Theme.Size.box380)
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
