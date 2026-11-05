import BeautifulUI
import SwiftUI

/// Persona's profile, opened from its avatar on the Persona tab (and from 设置 › 助手): who it is and where it runs, what
/// it will do on its own next, and everything that shapes it — its scheduled tasks, the history of runs, what it
/// remembers, what it is told about the user, and its launcher.
struct AssistantProfileView: View {
    @Environment(AppModel.self) private var model
    @State private var tasks: [APIClient.ScheduledTask]?
    @State private var runs: [APIClient.ScheduleRun]?
    @State private var memoryCount: Int?
    #if DEBUG
    @State private var debugPage: String?
    #endif

    private var machineID: String { model.assistantConfig?.machineID ?? model.hubMachineID }
    private var assistantCwd: String {
        if let cwd = model.assistantConfig?.cwd, !cwd.isEmpty { return cwd }
        return "~/.persona/assistant"
    }
    private var machineName: String { model.machine(model.hubRef(machineID))?.name ?? machineID }

    /// The machines the hub connects to, by name ("服务器、笔记本…"), from the hub's own list.
    private var machinesLine: String {
        let names = model.machines.map(\.name)
        let list = names.joined(separator: String(localized: "、", comment: "Separator between machine names"))
        return names.isEmpty ? String(localized: "hub 接入的各台电脑（ssh）") : String(localized: "\(list)（ssh）")
    }

    var body: some View {
        Group { if AppStyle.current.usesBeautifulUI { cards } else { list } }
        .pageTitle("Persona")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
        #if DEBUG
        .onAppear {
            if debugPage == nil, let p = UserDefaults.standard.string(forKey: "openAssistantPage"),
               !["profile", "assistant", "avatars"].contains(p) { debugPage = p }
        }
        .navigationDestination(item: $debugPage) { p in
            switch p {
            case "routines", "routine", "newRoutine", "followups", "followup", "intervalTask", "bypassTask": AssistantRoutinesView()
            case "runs": AssistantRunsView()
            case "memory", "memoryFile", "memoryEdit": AssistantMemoryView()
            default: AssistantNotesView()
            }
        }
        #endif
    }

    private var list: some View {
        List {
            Section { header }
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
            if !upcoming.isEmpty {
                Section {
                    ForEach(upcoming) { u in AssistantUpcomingRow(item: u) }
                } header: {
                    Text("接下来")
                }
            }
            Section {
                NavigationLink { AssistantRoutinesView() } label: {
                    row("定时任务", "alarm", detail: tasks.map { t in
                        let on = t.filter(\.enabled).count
                        return on == 0 ? String(localized: "未开启") : String(localized: "\(on) 个开启")
                    })
                }
                NavigationLink { AssistantRunsView() } label: {
                    row("运行记录", "clock.arrow.circlepath", detail: runs.flatMap(\.first).map { AssistantRunText.statusWord($0) + " · " + Fmt.relative($0.at) })
                }
                NavigationLink { TaskTrustView() } label: { row("直接开的任务", Theme.Icon.taskTrust, detail: nil) }
            } header: {
                Text("主动做事")
            } footer: {
                Text("到点自己开始工作、跟进答应你的事，跑完推送到手机。在对话里说「每天 9 点给我早报」「一小时后提醒我」就会加到这里。")
            }
            Section {
                NavigationLink { AssistantNotesView() } label: {
                    row("关于我", "person.text.rectangle", detail: nil)
                }
                NavigationLink { AssistantMemoryView() } label: {
                    row("记忆", "brain", detail: memoryCount.flatMap { $0 > 0 ? String(localized: "\($0) 条", comment: "Memory count") : nil })
                }
            } header: {
                Text("了解你")
            } footer: {
                Text("「关于我」是你告诉 Persona 的；「记忆」是它在对话里自己记下的，都可以改。")
            }
            if let quiet = quietTasks {
                Section {
                    Toggle(isOn: quiet) {
                        Label(Self.quietTitle, systemImage: Theme.Icon.notifications).lineLimit(1)
                    }
                    .accessibilityIdentifier("assistant-main-tasks-quiet")
                } header: {
                    Text("任务通知")
                } footer: {
                    Text(Self.quietFooter)
                }
            }
            Section {
                NavigationLink { SkillsPage() } label: { row("Skills", Theme.Icon.skills, detail: nil) }
                LabeledContent("运行在", value: machineName)
                LabeledContent("工作目录", value: assistantCwd)
            } header: {
                Text("设置")
            } footer: {
                Text("每段对话在 \(machineName) 的这个目录里后台运行一个 Claude，不进 herdr。用哪个模型在「设置 › 模型配置 › 主对话」里选，单段对话在它自己的菜单里换。")
            }
            Section {
                Label(machinesLine, systemImage: Theme.Icon.machines)
                Label("各台电脑上的 Agent：看状态、读对话、发消息、开新的", systemImage: "rectangle.stack.badge.person.crop")
                Label("飞书：消息、日历、文档、会议纪要、任务、邮件", systemImage: "building.2")
                Label("iPhone：通讯录、日历、提醒、位置、健康、照片", systemImage: Theme.Icon.phone)
                Label("你添加的所有邮箱", systemImage: Theme.Icon.mail)
                Label("在手机上问你、给你推送", systemImage: Theme.Icon.notifications)
            } header: {
                Text("能访问")
            } footer: {
                Text("发飞书消息、发邮件、新建日程这类会替你出面的操作，都先在手机上问你。")
            }
        }
        .themedPage()
    }

    /// The BeautifulUI style: its list cards, a heading over each and a note under it.
    private var cards: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.xl) {
                header
                if !upcoming.isEmpty {
                    BUIListSection(String(localized: "接下来")) {
                        ForEach(upcoming) { u in AssistantUpcomingRow(item: u) }
                    }
                }
                BUIListSection(String(localized: "主动做事"), footer: String(localized: "到点自己开始工作、跟进答应你的事，跑完推送到手机。在对话里说「每天 9 点给我早报」「一小时后提醒我」就会加到这里。")) {
                    NavigationLink { AssistantRoutinesView() } label: {
                        buiRow("定时任务", Theme.Icon.schedule, detail: tasks.map { t in
                            let on = t.filter(\.enabled).count
                            return on == 0 ? String(localized: "未开启") : String(localized: "\(on) 个开启")
                        })
                    }
                    NavigationLink { AssistantRunsView() } label: {
                        buiRow("运行记录", "clock.arrow.circlepath", detail: runs.flatMap(\.first).map { AssistantRunText.statusWord($0) + " · " + Fmt.relative($0.at) })
                    }
                    NavigationLink { TaskTrustView() } label: { buiRow("直接开的任务", Theme.Icon.taskTrust, detail: nil) }
                }
                BUIListSection(String(localized: "了解你"), footer: String(localized: "「关于我」是你告诉 Persona 的；「记忆」是它在对话里自己记下的，都可以改。")) {
                    NavigationLink { AssistantNotesView() } label: {
                        buiRow("关于我", "person.text.rectangle", detail: nil)
                    }
                    NavigationLink { AssistantMemoryView() } label: {
                        buiRow("记忆", "brain", detail: memoryCount.flatMap { $0 > 0 ? String(localized: "\($0) 条", comment: "Memory count") : nil })
                    }
                }
                if let quiet = quietTasks {
                    BUIListSection(String(localized: "任务通知"), footer: Self.quietFooter) {
                        BUIListRow(Self.quietTitle) { BUIIconTile(Theme.Icon.notifications, tint: Theme.accent) } trailing: {
                            Toggle(Self.quietTitle, isOn: quiet).labelsHidden()
                                .accessibilityIdentifier("assistant-main-tasks-quiet")
                        }
                    }
                }
                BUIListSection(String(localized: "设置"), footer: String(localized: "每段对话在 \(machineName) 的这个目录里后台运行一个 Claude，不进 herdr。用哪个模型在「设置 › 模型配置 › 主对话」里选，单段对话在它自己的菜单里换。")) {
                    NavigationLink { SkillsPage() } label: { buiRow("Skills", Theme.Icon.skills, detail: nil) }
                    BUIListRow(String(localized: "运行在"), value: machineName)
                    BUIListRow(String(localized: "工作目录"), value: assistantCwd)
                }
                BUIListSection(String(localized: "能访问"), footer: String(localized: "发飞书消息、发邮件、新建日程这类会替你出面的操作，都先在手机上问你。")) {
                    buiReach(machinesLine, Theme.Icon.machines)
                    buiReach(String(localized: "各台电脑上的 Agent：看状态、读对话、发消息、开新的"), "rectangle.stack.badge.person.crop")
                    buiReach(String(localized: "飞书：消息、日历、文档、会议纪要、任务、邮件"), "building.2")
                    buiReach(String(localized: "iPhone：通讯录、日历、提醒、位置、健康、照片"), Theme.Icon.phone)
                    buiReach(String(localized: "你添加的所有邮箱"), Theme.Icon.mail)
                    buiReach(String(localized: "在手机上问你、给你推送"), Theme.Icon.notifications)
                }
            }
            .padding(.horizontal, Theme.Space.l)
            .padding(.bottom, Theme.Space.xxl)
        }
        .themedGroupedPage()
    }

    private var header: some View {
        VStack(spacing: Theme.Space.m) {
            PersonaAvatar(size: 88, working: working)
                .shadow(color: Theme.persona.opacity(0.3), radius: 16, y: 6)
            Text(model.persona.name).font(Theme.Font.display).fontDesign(.rounded)
            Text("你的私人助手 · 运行在 \(machineName)")
                .font(Theme.Font.secondary)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            HStack(spacing: Theme.Space.s) {
                let online = model.machine(model.hubRef(machineID))?.online ?? false
                Chip(text: online ? String(localized: "在线") : String(localized: "离线"), symbol: online ? Theme.Icon.successFilled : "moon.zzz", look: online ? .tint(Theme.positive) : .neutral)
                if working { Chip(text: String(localized: "工作中"), symbol: Theme.Icon.more, look: .tint(Theme.accent)) }
                if let next = upcoming.first {
                    Chip(text: String(localized: "下一件 \(AssistantScheduleText.moment(next.at))"), symbol: Theme.Icon.schedule)
                }
            }
        }
        .frame(maxWidth: .infinity)
        // Room above for the face's glow (radius 16): the list row clips what sticks out, as a hard square edge.
        .modifier(HeaderRoom())
    }

    /// The header's room: in the BeautifulUI style on the token grid, in every other style what it always had.
    private struct HeaderRoom: ViewModifier {
        @ViewBuilder func body(content: Content) -> some View {
            if AppStyle.current.usesBeautifulUI {
                content.padding(.top, Theme.Space.xl)
            } else {
                content.padding(.top, 28).padding(.bottom, Theme.Space.m)
            }
        }
    }

    /// Any assistant conversation running a turn right now.
    private var working: Bool {
        (model.sessionsByMachine[model.hubRef(machineID)] ?? []).contains { $0.assistant && $0.state == .working }
    }

    /// 主对话派的任务只在主对话里通知 (#1731); nil (no switch) from a hub without the setting.
    private var quietTasks: Binding<Bool>? {
        guard let on = model.assistantConfig?.mainTasksQuiet else { return nil }
        return Binding(get: { on }, set: { v in Task { await model.setMainTasksQuiet(v) } })
    }
    private static let quietTitle = String(localized: "只在主对话里通知")
    private static let quietFooter = String(localized: "主对话派出去的任务不进灵动岛、推送和角标，做完、等你回答或出错时由 Persona 在主对话里告诉你。只影响之后派的任务。")

    private var upcoming: [AssistantUpcoming] {
        AssistantUpcoming.merge(tasks ?? []).prefix(3).map { $0 }
    }

    private func row(_ title: LocalizedStringResource, _ symbol: String, detail: String?) -> some View {
        LabeledContent {
            if let detail { Text(detail).lineLimit(1) }
        } label: {
            Label(String(localized: title), systemImage: symbol).lineLimit(1)
        }
    }

    /// The BeautifulUI style's row that opens a page: its mark, its name, what is there now.
    private func buiRow(_ title: LocalizedStringResource, _ symbol: String, detail: String?) -> some View {
        BUIListRow(String(localized: title), value: detail, accessory: .disclosure) { BUIIconTile(symbol, tint: Theme.accent) }
    }

    /// The BeautifulUI style's row for what Persona can reach.
    private func buiReach(_ text: String, _ symbol: String) -> some View {
        BUIListRow(text) { BUIIconTile(symbol, tint: Theme.Hue.gray) }
    }

    /// Reloaded each time the page shows (coming back from a sub-page included).
    private func load() async {
        guard let api = model.api else { return }
        async let r = try? api.scheduledTasks()
        async let h = try? api.scheduleRuns()
        async let m = try? api.assistantMemory()
        // The switch shows once the hub says it has the setting (#1731): a hub updated since the app read it, too.
        if model.assistantConfig?.mainTasksQuiet == nil { await model.refreshAssistant() }
        tasks = await r
        runs = await h
        if let files = await m { memoryCount = files.filter { $0.name != AssistantMemoryEntry.index }.count }
    }
}

/// One thing Persona will do on its own: a task's next run.
struct AssistantUpcoming: Identifiable, Hashable {
    var id: String
    var at: Date
    var title: String
    var routine: Bool
    var detail: String
    var key: SessionKey?

    /// Soonest first; what is switched off is not coming up.
    static func merge(_ tasks: [APIClient.ScheduledTask]) -> [AssistantUpcoming] {
        var out: [AssistantUpcoming] = []
        for t in tasks where t.enabled {
            guard let next = t.nextRun ?? t.at else { continue }
            if t.repeats == .once {
                out.append(.init(id: "t-" + t.id, at: next, title: AssistantScheduleText.title(t), routine: false,
                                 detail: t.sessionID == nil ? String(localized: "跟进 · 新对话") : String(localized: "跟进 · 接着原来的对话"),
                                 key: t.sessionID.map { SessionKey(machineID: t.machineID ?? "", sessionID: $0) }))
            } else {
                out.append(.init(id: "t-" + t.id, at: next, title: AssistantScheduleText.title(t), routine: true,
                                 detail: AssistantScheduleText.sentence(t)))
            }
        }
        return out.sorted { $0.at < $1.at }
    }
}

/// A timeline row like Reminders': the time big on the left, what happens on the right.
struct AssistantUpcomingRow: View {
    var item: AssistantUpcoming

    var body: some View {
        if AppStyle.current.usesBeautifulUI { beautiful } else { plain }
    }

    /// The BeautifulUI style: its list row, the kind's mark, what and when, the time on the right.
    private var beautiful: some View {
        BUIListRow(item.title, detail: "\(item.detail) · \(AssistantScheduleText.until(item.at))",
                   value: "\(AssistantScheduleText.day(item.at)) \(AssistantScheduleText.hm(item.at))") {
            BUIIconTile(item.routine ? Theme.Icon.recurring : Theme.Icon.followOn, tint: Theme.accent)
        }
        .accessibilityElement(children: .combine)
    }

    private var plain: some View {
        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.l) {
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(AssistantScheduleText.hm(item.at))
                    .font(Theme.Font.heading.monospacedDigit())
                Text(AssistantScheduleText.day(item.at)).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
            .scaledFrame(width: Theme.Size.box58, alignment: .leading)
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                HStack(spacing: Theme.Space.sm) {
                    Image(systemName: item.routine ? Theme.Icon.recurring : Theme.Icon.followOn)
                        .font(Theme.Font.labelHeavy)
                        .foregroundStyle(Theme.accent)
                    Text(item.title).font(Theme.Font.heading).lineLimit(1)
                }
                Text("\(item.detail) · \(AssistantScheduleText.until(item.at))")
                    .font(Theme.Font.label)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
        }
        .padding(.vertical, Theme.Space.xxs)
        .accessibilityElement(children: .combine)
    }
}

// MARK: - 运行记录

enum AssistantRunText {
    static func statusWord(_ r: APIClient.ScheduleRun) -> String {
        switch r.status {
        case "ok": String(localized: "已运行")
        case "skipped": String(localized: "已跳过")
        default: String(localized: "失败")
        }
    }

    static func symbol(_ r: APIClient.ScheduleRun) -> (String, Color) {
        switch r.status {
        case "ok": (Theme.Icon.successFilled, Theme.positive)
        case "skipped": ("forward.end.circle.fill", .secondary)
        default: (Theme.Icon.errorFilled, Theme.danger)
        }
    }

    /// "准时" / "晚了 21 秒" / "手动运行" / the error.
    static func detail(_ r: APIClient.ScheduleRun) -> String {
        if r.manual == true { return String(localized: "手动运行") }
        if r.status != "ok" { return r.error ?? statusWord(r) }
        let late = r.at.timeIntervalSince(r.due)
        if late < 5 { return String(localized: "准时") }
        if late < 120 { return String(localized: "晚了 \(Int(late)) 秒") }
        if late < 7200 { return String(localized: "晚了 \(Int(late / 60)) 分钟") }
        return String(localized: "晚了 \(Int(late / 3600)) 小时")
    }
}

/// Every run of a routine or follow-up, newest first; `ref` limits it to one of them. A run opens its conversation.
struct AssistantRunsView: View {
    @Environment(AppModel.self) private var model
    var ref: String?
    @State private var runs: [APIClient.ScheduleRun] = []
    @State private var loaded = false
    @State private var error: String?

    var body: some View {
        List {
            ForEach(days, id: \.0) { day, items in
                Section(day) {
                    ForEach(items) { r in AssistantRunRow(run: r) }
                }
            }
        }
        .themedPage()
        .overlay {
            if !loaded {
                ProgressView()
            } else if let error, runs.isEmpty {
                ContentUnavailableView("读不到运行记录", systemImage: Theme.Icon.error, description: Text(error))
            } else if runs.isEmpty {
                ContentUnavailableView("还没有运行过", systemImage: Theme.Icon.recent,
                                       description: Text("定时任务和跟进每跑一次（包括失败和跳过的）都记在这里。"))
            }
        }
        .pageTitle(String(localized: "运行记录"))
        .task { await load() }
        .refreshable { await load() }
    }

    private var days: [(String, [APIClient.ScheduleRun])] {
        var out: [(String, [APIClient.ScheduleRun])] = []
        for r in runs {
            let d = AssistantScheduleText.day(r.at)
            if out.last?.0 == d { out[out.count - 1].1.append(r) } else { out.append((d, [r])) }
        }
        return out
    }

    private func load() async {
        guard let api = model.api else { return }
        do {
            runs = try await api.scheduleRuns(ref: ref).sorted { $0.at > $1.at } // one section per day
            error = nil
        } catch {
            self.error = error.shown ?? self.error
        }
        loaded = true
    }
}

struct AssistantRunRow: View {
    @Environment(AppModel.self) private var model
    var run: APIClient.ScheduleRun
    /// Inside one routine's page the title is the page's.
    var showTitle = true

    var body: some View {
        if AppStyle.current.usesBeautifulUI { beautiful } else { plain }
    }

    /// BeautifulUI's task row: the green check, the red cross, or an empty ring for a skipped run; what ran and how
    /// (准时 / 晚了 21 秒 / the error) under it; the chevron when it has a conversation to open.
    @ViewBuilder private var beautiful: some View {
        let row = TaskRow(TaskItem(id: run.id, title: showTitle ? run.title : AssistantScheduleText.moment(run.at),
                                   subtitle: showTitle ? "\(AssistantScheduleText.hm(run.at)) · \(AssistantRunText.detail(run))" : AssistantRunText.detail(run),
                                   status: run.status == "ok" ? .completed : .failed),
                          shape: .plain, accessory: run.key != nil ? .disclosure : .none, subtitleLines: 2) {
            if run.status == "skipped" {
                Circle().strokeBorder(Theme.cardStroke, lineWidth: Theme.Size.rule).frame(width: Theme.Size.badge, height: Theme.Size.badge)
            } else {
                TaskBadge(run.status == "ok" ? .completed : .failed)
            }
        } trailing: {
            EmptyView()
        } detail: {
            EmptyView()
        }
        if let key = run.key {
            Button {
                model.selectedTab = .assistant
                model.assistantCurrent = model.inHub(key)
            } label: { row }
            .buttonStyle(.plain)
            .accessibilityHint("打开这次运行的对话")
        } else {
            row
        }
    }

    @ViewBuilder private var plain: some View {
        let (symbol, tint) = AssistantRunText.symbol(run)
        let content = HStack(spacing: Theme.Space.m) {
            Image(systemName: symbol).font(Theme.Font.title).foregroundStyle(tint)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(showTitle ? run.title : AssistantScheduleText.moment(run.at))
                    .font(Theme.Font.heading).foregroundStyle(Color.primary).lineLimit(1)
                Text(showTitle ? "\(AssistantScheduleText.hm(run.at)) · \(AssistantRunText.detail(run))"
                     : AssistantRunText.detail(run))
                    .font(Theme.Font.label)
                    .foregroundStyle(run.status == "failed" ? Theme.danger : Color.secondary)
                    .lineLimit(2)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if run.key != nil {
                Image(systemName: Theme.Icon.disclosure).font(Theme.Font.captionStrong).foregroundStyle(Color(uiColor: .tertiaryLabel))
            }
        }
        if let key = run.key {
            Button {
                model.selectedTab = .assistant
                model.assistantCurrent = model.inHub(key)
            } label: { content }
            .accessibilityHint("打开这次运行的对话")
        } else {
            content
        }
    }
}
