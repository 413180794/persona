import SwiftUI

/// Jarvis's profile, opened from its avatar on the Jarvis tab (and from 设置 › 助手): who it is and where it runs, what
/// it will do on its own next, and everything that shapes it — routines, follow-ups, the history of runs, what it
/// remembers, what it is told about the user, and its launcher.
struct AssistantProfileView: View {
    @Environment(AppModel.self) private var model
    @State private var launchers: [Launcher] = []
    @State private var selection = ""
    @State private var routines: [APIClient.Routine]?
    @State private var followups: [APIClient.Followup]?
    @State private var runs: [APIClient.ScheduleRun]?
    @State private var memoryCount: Int?
    #if DEBUG
    @State private var debugPage: String?
    #endif

    private var machineID: String { model.assistantConfig?.machineID ?? model.hubMachineID }
    private var machineName: String { model.machine(machineID)?.name ?? machineID }

    /// The machines the hub connects to, by name ("服务器、笔记本…"), from the hub's own list.
    private var machinesLine: String {
        let names = model.machines.map(\.name)
        return names.isEmpty ? "hub 接入的各台电脑（ssh）" : names.joined(separator: "、") + "（ssh）"
    }

    var body: some View {
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
                    row("定时任务", "alarm", detail: routines.map { r in
                        let on = r.filter(\.enabled).count
                        return on == 0 ? "未开启" : "\(on) 个开启"
                    })
                }
                NavigationLink { AssistantFollowupsView() } label: {
                    row("已安排的跟进", "calendar.badge.clock", detail: followups.flatMap { $0.isEmpty ? nil : "\($0.count)" })
                }
                NavigationLink { AssistantRunsView() } label: {
                    row("运行记录", "clock.arrow.circlepath", detail: runs.flatMap(\.first).map { AssistantRunText.statusWord($0) + " · " + Fmt.relative($0.at) })
                }
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
                    row("记忆", "brain", detail: memoryCount.flatMap { $0 > 0 ? "\($0) 条" : nil })
                }
            } header: {
                Text("了解你")
            } footer: {
                Text("「关于我」是你告诉 Jarvis 的；「记忆」是它在对话里自己记下的，都可以改。")
            }
            Section {
                NavigationLink {
                    LauncherPicker(launchers: launchers, selection: $selection)
                } label: {
                    LabeledContent("启动器（模型）") {
                        Text(selection).font(Theme.Font.body.monospaced()).lineLimit(1)
                    }
                }
                LabeledContent("运行在", value: machineName)
                LabeledContent("工作目录", value: "~/.jarvis/assistant")
            } header: {
                Text("设置")
            } footer: {
                Text("每段对话在 \(machineName) 的这个目录里后台运行一个 Claude，不进 herdr。换启动器只影响新对话。")
            }
            Section {
                Label(machinesLine, systemImage: "macbook.and.iphone")
                Label("各台电脑上的 Agent：看状态、读对话、发消息、开新的", systemImage: "rectangle.stack.badge.person.crop")
                Label("飞书：消息、日历、文档、会议纪要、任务、邮件", systemImage: "building.2")
                Label("iPhone：通讯录、日历、提醒、位置、健康、照片", systemImage: "iphone")
                Label("你添加的所有邮箱", systemImage: "envelope")
                Label("在手机上问你、给你推送", systemImage: "bell.badge")
            } header: {
                Text("能访问")
            } footer: {
                Text("发飞书消息、发邮件、新建日程这类会替你出面的操作，都先在手机上问你。")
            }
        }
        .themedPage()
        .navigationTitle("Jarvis")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            selection = model.assistantConfig?.launcher ?? "claude"
            launchers = (try? await model.launchers(machineID)) ?? []
        }
        .task { await load() }
        .refreshable { await load() }
        .onChange(of: selection) { old, new in
            guard !old.isEmpty, old != new else { return }
            Task { await model.setAssistantLauncher(new) }
        }
        #if DEBUG
        .onAppear {
            if debugPage == nil, let p = UserDefaults.standard.string(forKey: "openAssistantPage"),
               !["profile", "assistant", "avatars"].contains(p) { debugPage = p }
        }
        .navigationDestination(item: $debugPage) { p in
            switch p {
            case "routines", "routine", "newRoutine": AssistantRoutinesView()
            case "followups": AssistantFollowupsView()
            case "runs": AssistantRunsView()
            case "memory", "memoryFile", "memoryEdit": AssistantMemoryView()
            default: AssistantNotesView()
            }
        }
        #endif
    }

    private var header: some View {
        VStack(spacing: Theme.Space.m) {
            JarvisAvatar(size: 88, working: working)
                .shadow(color: Theme.accent.opacity(0.3), radius: 16, y: 6)
            Text("Jarvis").font(Theme.Font.display).fontDesign(.rounded)
            Text("你的私人助手 · 运行在 \(machineName)")
                .font(Theme.Font.secondary)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            HStack(spacing: 8) {
                let online = model.machine(machineID)?.online ?? false
                Chip(text: online ? "在线" : "离线", symbol: online ? "checkmark.circle.fill" : "moon.zzz", look: online ? .tint(Theme.positive) : .neutral)
                if working { Chip(text: "工作中", symbol: "ellipsis", look: .tint(Theme.accent)) }
                if let next = upcoming.first {
                    Chip(text: "下一件 \(AssistantScheduleText.moment(next.at))", symbol: "alarm")
                }
            }
        }
        .frame(maxWidth: .infinity)
        // Room above for the face's glow (radius 16): the list row clips what sticks out, as a hard square edge.
        .padding(.top, 28)
        .padding(.bottom, 12)
    }

    /// Any assistant conversation running a turn right now.
    private var working: Bool {
        (model.sessionsByMachine[machineID] ?? []).contains { $0.assistant && $0.state == .working }
    }

    private var upcoming: [AssistantUpcoming] {
        AssistantUpcoming.merge(routines: routines ?? [], followups: followups ?? []).prefix(3).map { $0 }
    }

    private func row(_ title: String, _ symbol: String, detail: String?) -> some View {
        LabeledContent {
            if let detail { Text(detail).lineLimit(1) }
        } label: {
            Label(title, systemImage: symbol).lineLimit(1)
        }
    }

    /// Reloaded each time the page shows (coming back from a sub-page included).
    private func load() async {
        guard let api = model.api else { return }
        async let r = try? api.routines()
        async let f = try? api.followups()
        async let h = try? api.scheduleRuns()
        async let m = try? api.assistantMemory()
        routines = await r
        followups = await f
        runs = await h
        if let files = await m { memoryCount = files.filter { $0.name != AssistantMemoryEntry.index }.count }
    }
}

/// One thing Jarvis will do on its own: a routine's next run or a follow-up.
struct AssistantUpcoming: Identifiable, Hashable {
    var id: String
    var at: Date
    var title: String
    var routine: Bool
    var detail: String
    var key: SessionKey?

    /// Soonest first.
    static func merge(routines: [APIClient.Routine], followups: [APIClient.Followup]) -> [AssistantUpcoming] {
        var out: [AssistantUpcoming] = []
        for r in routines where r.enabled {
            if let next = r.nextRun {
                out.append(.init(id: "r-" + r.id, at: next, title: r.title, routine: true, detail: AssistantScheduleText.sentence(r)))
            }
        }
        for f in followups {
            let first = f.prompt.split(whereSeparator: \.isNewline).first.map(String.init) ?? f.prompt
            out.append(.init(id: "f-" + f.id, at: f.at, title: f.title.flatMap { $0.isEmpty ? nil : $0 } ?? first, routine: false,
                             detail: f.sessionID == nil ? "跟进 · 新对话" : "跟进 · 接着原来的对话",
                             key: f.sessionID.map { SessionKey(machineID: f.machineID, sessionID: $0) }))
        }
        return out.sorted { $0.at < $1.at }
    }
}

/// A timeline row like Reminders': the time big on the left, what happens on the right.
struct AssistantUpcomingRow: View {
    var item: AssistantUpcoming

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 14) {
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(AssistantScheduleText.hm(item.at))
                    .font(Theme.Font.heading.monospacedDigit())
                Text(AssistantScheduleText.day(item.at)).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
            .frame(width: 58, alignment: .leading)
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                HStack(spacing: 6) {
                    Image(systemName: item.routine ? "repeat" : "arrow.turn.down.right")
                        .font(Theme.Font.label.weight(.bold))
                        .foregroundStyle(Theme.accent)
                    Text(item.title).font(Theme.Font.heading).lineLimit(1)
                }
                Text("\(item.detail) · \(AssistantScheduleText.until(item.at))")
                    .font(Theme.Font.label)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }
}

// MARK: - 运行记录

enum AssistantRunText {
    static func statusWord(_ r: APIClient.ScheduleRun) -> String {
        switch r.status {
        case "ok": "已运行"
        case "skipped": "已跳过"
        default: "失败"
        }
    }

    static func symbol(_ r: APIClient.ScheduleRun) -> (String, Color) {
        switch r.status {
        case "ok": ("checkmark.circle.fill", Theme.positive)
        case "skipped": ("forward.end.circle.fill", .secondary)
        default: ("exclamationmark.octagon.fill", Theme.danger)
        }
    }

    /// "准时" / "晚了 21 秒" / "手动运行" / the error.
    static func detail(_ r: APIClient.ScheduleRun) -> String {
        if r.manual == true { return "手动运行" }
        if r.status != "ok" { return r.error ?? statusWord(r) }
        let late = r.at.timeIntervalSince(r.due)
        if late < 5 { return "准时" }
        if late < 120 { return "晚了 \(Int(late)) 秒" }
        if late < 7200 { return "晚了 \(Int(late / 60)) 分钟" }
        return "晚了 \(Int(late / 3600)) 小时"
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
                ContentUnavailableView("读不到运行记录", systemImage: "exclamationmark.triangle", description: Text(error))
            } else if runs.isEmpty {
                ContentUnavailableView("还没有运行过", systemImage: "clock.arrow.circlepath",
                                       description: Text("定时任务和跟进每跑一次（包括失败和跳过的）都记在这里。"))
            }
        }
        .navigationTitle("运行记录")
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
        let (symbol, tint) = AssistantRunText.symbol(run)
        let content = HStack(spacing: 12) {
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
                Image(systemName: "chevron.right").font(Theme.Font.caption.weight(.semibold)).foregroundStyle(Color(uiColor: .tertiaryLabel))
            }
        }
        if let key = run.key {
            Button {
                model.selectedTab = .assistant
                model.assistantCurrent = key
            } label: { content }
            .accessibilityHint("打开这次运行的对话")
        } else {
            content
        }
    }
}
