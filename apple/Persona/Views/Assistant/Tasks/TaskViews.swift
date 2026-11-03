import SwiftUI

// The unified entry's pieces in the assistant's conversation (docs/unified-entry.md): the plan card, the task card,
// the lines the hub adds (answered, receipt, routed), the dock above the composer, the questions of the tasks' agents,
// and the route chip with its @ list. They read `TasksStore` and `AppModel`, so a card follows its task live.

/// A task's mark: the agent state dot it borrows (needs you breathes), a check once done.
struct TaskMark: View {
    var state: TaskState
    var size: CGFloat = Theme.Size.dotL

    var body: some View {
        if state == .done || state == .unread {
            Image(systemName: Theme.Icon.successFilled)
                .font(Theme.Font.labelStrong)
                .foregroundStyle(state == .done ? Color.secondary : Theme.positive)
                .accessibilityLabel(state == .done ? "完成" : "做完了")
        } else {
            StatusDot(state: state.agentState, size: size)
        }
    }
}

extension TaskState {
    var label: String {
        switch self {
        case .needsYou: "等你"
        case .working: "在做"
        case .unread: "做完了"
        case .followup: "停着"
        case .notStarted: "正在开"
        case .done: "完成"
        }
    }
}

/// "12 分钟", "1 小时 5 分" since a date.
enum TaskClock {
    static func since(_ date: Date?, now: Date = .now) -> String? {
        guard let date, date.isMeaningful else { return nil }
        let m = max(0, Int(now.timeIntervalSince(date) / 60))
        if m < 1 { return "刚开始" }
        if m < 60 { return "\(m) 分钟" }
        let h = m / 60
        return m % 60 == 0 ? "\(h) 小时" : "\(h) 小时 \(m % 60) 分"
    }
}

/// Where a task runs, as the user names it: the machine's name, the project, the agent.
@MainActor
enum TaskPlace {
    static func machineName(_ id: String) -> String {
        let model = AppModel.shared
        return model.machine(model.hubRef(id))?.name ?? id
    }

    static func line(_ todo: TodoItem, lead: Session?) -> String {
        var parts: [String] = []
        if let a = todo.activeAgents.first ?? todo.agents.first {
            parts.append(machineName(a.machineID))
            if let cwd = a.cwd, !cwd.isEmpty { parts.append((cwd as NSString).lastPathComponent) }
        }
        if let lead { parts.append(lead.agentLabel) }
        return parts.joined(separator: " · ")
    }
}

// MARK: - Plan card

struct PlanCard: View {
    var plan: TaskPlan
    @State private var remember = false
    @State private var editing = false
    private var store: TasksStore { .shared }

    var body: some View {
        switch plan.state {
        case .open: open
        case .started: settled(symbol: Theme.Icon.done, text: "开了 \(plan.tasks.count) 个任务", failed: plan.tasks.filter { $0.error != nil })
        case .dismissed, .unknown: settled(symbol: Theme.Icon.close, text: "没开 · \(plan.tasks.count) 个任务", failed: [])
        }
    }

    private var open: some View {
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            Label(plan.tasks.count > 1 ? "拆成 \(plan.tasks.count) 件事，这样开" : "这样开", systemImage: Theme.Icon.taskPlan)
                .font(Theme.Font.captionStrong)
                .foregroundStyle(.secondary)
            ForEach(Array(plan.tasks.enumerated()), id: \.offset) { n, task in
                if n > 0 { Divider() }
                PlanTaskRow(number: n + 1, task: task, numbered: plan.tasks.count > 1)
            }
            Button {
                withAnimation(Theme.Motion.state) { remember.toggle() }
            } label: {
                Label("这类以后直接开", systemImage: remember ? Theme.Icon.successFilled : Theme.Icon.unchecked)
                    .font(Theme.Font.caption)
                    .foregroundStyle(remember ? Theme.accent : .secondary)
                    .lineLimit(1)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            HStack(spacing: Theme.Space.s) {
                Button {
                    Task { await store.start(plan, remember: remember) }
                } label: {
                    Text(plan.tasks.count > 1 ? "开始 \(plan.tasks.count) 个任务" : "开始").lineLimit(1)
                        .frame(maxWidth: .infinity)
                }
                .themedButton(prominent: true)
                Button { editing = true } label: { Text("改一下").lineLimit(1) }
                    .themedButton()
                Button {
                    Task { await store.dismiss(plan) }
                } label: { Text("不开").lineLimit(1) }
                    .themedButton()
            }
            .disabled(store.busyPlans.contains(plan.id))
        }
        .card(padding: Theme.Space.l)
        .sheet(isPresented: $editing) {
            PlanEditSheet(plan: plan) { edits in
                editing = false
                Task { await store.start(plan, edits: edits, remember: remember) }
            }
        }
    }

    private func settled(symbol: String, text: String, failed: [PlanTask]) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            Label(text, systemImage: symbol)
                .font(Theme.Font.secondary)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            ForEach(failed, id: \.title) { t in
                Label("「\(t.title)」没开起来：\(t.error ?? "")", systemImage: Theme.Icon.error)
                    .font(Theme.Font.caption)
                    .foregroundStyle(Theme.danger)
                    .lineLimit(2)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// One task of an open plan: its title, where it would run (each a chip), and why there.
struct PlanTaskRow: View {
    var number: Int
    var task: PlanTask
    var numbered: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
                if numbered {
                    Text("\(number)")
                        .font(Theme.Font.labelHeavy)
                        .foregroundStyle(Theme.accent)
                        .scaledFrame(width: Theme.Size.badge, height: Theme.Size.badge)
                        .background(Theme.accent.opacity(0.14), in: .circle)
                }
                Text(task.title).font(Theme.Font.bodyStrong)
            }
            FlowLayoutRow(spacing: Theme.Space.xs) {
                Chip(text: TaskPlace.machineName(task.machine), symbol: Theme.Icon.laptop)
                Chip(text: task.project, symbol: Theme.Icon.folder)
                Chip(text: task.model.flatMap { $0.isEmpty ? nil : $0 } ?? task.launcher.flatMap { $0.isEmpty ? nil : $0 } ?? "默认模型",
                     symbol: Theme.Icon.cpu)
                if task.isReadOnly {
                    Chip(text: "只读", symbol: Theme.Icon.view, look: .tint(Theme.accent))
                } else if let p = task.permission, !p.isEmpty {
                    Chip(text: Self.permissionLabel(p))
                }
            }
            if let why = task.why, !why.isEmpty {
                Text(why).font(Theme.Font.caption).foregroundStyle(.secondary)
            }
        }
    }

    static func permissionLabel(_ mode: String) -> String {
        switch mode {
        case "acceptEdits": "可以改文件"
        case "plan": "先出方案"
        case "bypassPermissions": "全部放行"
        default: "每步都问"
        }
    }
}

/// 改一下: the plan's fields the user can change before it starts.
struct PlanEditSheet: View {
    var plan: TaskPlan
    var start: ([PlanTask]) -> Void
    @State private var tasks: [PlanTask] = []
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Form {
                ForEach(tasks.indices, id: \.self) { i in
                    Section(tasks.count > 1 ? "任务 \(i + 1)" : "任务") {
                        TextField("标题", text: $tasks[i].title)
                        Picker("电脑", selection: $tasks[i].machine) {
                            ForEach(AppModel.shared.hubMachines, id: \.id) { m in Text(m.name).tag(m.id) }
                            if !AppModel.shared.hubMachines.contains(where: { $0.id == tasks[i].machine }) {
                                Text(tasks[i].machine).tag(tasks[i].machine)
                            }
                        }
                        TextField("目录", text: $tasks[i].cwd)
                            .font(Theme.Font.code)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                        TextField("模型（空着用默认）", text: Binding(get: { tasks[i].model ?? "" }, set: { tasks[i].model = $0 }))
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                        Picker("权限", selection: Binding(get: { tasks[i].permission ?? "default" }, set: { tasks[i].permission = $0 })) {
                            ForEach(["default", "acceptEdits", "plan", "bypassPermissions"], id: \.self) { Text(PlanTaskRow.permissionLabel($0)).tag($0) }
                        }
                    }
                    Section("交给 Agent 的话") {
                        TextField("第一条消息", text: $tasks[i].prompt, axis: .vertical).lineLimit(3...8)
                    }
                }
            }
            .navigationTitle("改一下")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) { Button("开始") { start(tasks) }.bold() }
            }
        }
        .onAppear { if tasks.isEmpty { tasks = plan.tasks } }
    }
}

// MARK: - Task card

/// A task in the conversation, updated in place: its mark, title, time, where it runs, what its agent says now.
struct TaskCard: View {
    var todoID: String
    var conversation: String
    private var store: TasksStore { .shared }

    var body: some View {
        if let todo = store.todos[todoID] {
            let state = store.state(todo)
            let lead = store.leadSession(todo)
            Button {
                store.detailTask = todo.id
            } label: {
                VStack(alignment: .leading, spacing: Theme.Space.s) {
                    HStack(spacing: Theme.Space.s) {
                        TaskMark(state: state).scaledFrame(width: Theme.Size.markM)
                        Text(todo.title).font(Theme.Font.bodyStrong).lineLimit(1)
                        Spacer(minLength: Theme.Space.s)
                        if let t = TaskClock.since(todo.createdAt), state != .done {
                            Text(t).font(Theme.Font.caption).foregroundStyle(.tertiary).lineLimit(1).fixedSize()
                        }
                    }
                    Text(TaskPlace.line(todo, lead: lead))
                        .font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                    TaskNowLine(todo: todo, state: state, lead: lead)
                    if state != .done {
                        HStack(spacing: Theme.Space.l) {
                            Text(state.label).font(Theme.Font.captionMedium).foregroundStyle(state.agentState.tone.text)
                            Spacer()
                            Button {
                                store.routes[conversation] = todo.id
                            } label: {
                                Label("说一句", systemImage: Theme.Icon.routeToTask).lineLimit(1)
                            }
                            .font(Theme.Font.captionStrong)
                            .foregroundStyle(Theme.accent)
                            .buttonStyle(.plain)
                        }
                    }
                }
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .card(padding: Theme.Space.l)
            .overlay {
                if state == .needsYou {
                    RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
                        .stroke(state.agentState.color, lineWidth: Theme.Size.lineMedium)
                }
            }
            .animation(Theme.Motion.state, value: state)
        }
    }
}

/// What the task's agent is doing or said last, in a well: waiting for the user in its tone.
struct TaskNowLine: View {
    var todo: TodoItem
    var state: TaskState
    var lead: Session?

    var body: some View {
        let text: String = {
            if state == .needsYou { return "有事等你回答" }
            if let s = lead?.lastText?.trimmingCharacters(in: .whitespacesAndNewlines), !s.isEmpty { return s }
            if let e = todo.agents.first?.startError, !e.isEmpty { return "没开起来：\(e)" }
            return state == .notStarted ? "正在开 Agent" : "还没有动静"
        }()
        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
            Image(systemName: state == .needsYou ? Theme.Icon.askFirst : Theme.Icon.terminal)
                .font(Theme.Font.label)
                .foregroundStyle(state == .needsYou ? state.agentState.tone.text : .secondary)
            Text(text)
                .font(Theme.Font.secondary)
                .foregroundStyle(state == .needsYou ? state.agentState.tone.text : .primary)
                .lineLimit(2)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.s)
        .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
    }
}

// MARK: - The hub's lines

/// One line the hub added to the conversation (docs/protocol.md「任务和对话动态」).
struct FeedLine: View {
    var item: FeedItem
    @Environment(\.shellChat) private var shellChat
    private var store: TasksStore { .shared }

    var body: some View {
        switch item.kind {
        case .plan:
            if let id = item.ref, let plan = store.plans[id] { PlanCard(plan: plan) }
        case .task:
            if let id = item.ref { TaskCard(todoID: id, conversation: item.conversation) }
        case .answered: answered
        case .receipt: ReceiptCard(item: item)
        case .routed: routed
        case .unknown: EmptyView()
        }
    }

    private var taskTitle: String { item.ref.flatMap { store.todos[$0]?.title } ?? "任务" }

    private var answered: some View {
        HStack(spacing: Theme.Space.s) {
            Image(systemName: Theme.Icon.done).font(Theme.Font.labelStrong)
            Text("\(taskTitle) · \(item.answer ?? "回答了")").lineLimit(1)
        }
        .font(Theme.Font.caption)
        .foregroundStyle(.secondary)
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.sm)
        .background(Theme.Fill.chip, in: .capsule)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    private var routed: some View {
        VStack(alignment: .trailing, spacing: Theme.Space.xs) {
            Label("对「\(taskTitle)」说", systemImage: Theme.Icon.routeToTask)
                .font(Theme.Font.captionMedium)
                .foregroundStyle(Theme.accent)
                .lineLimit(1)
            Text(item.text ?? "")
                .font(Theme.Font.body)
                .foregroundStyle(Theme.userBubbleText(shell: shellChat))
                .padding(.horizontal, Theme.Space.m)
                .padding(.vertical, Theme.Space.s)
                .background(Theme.userBubbleFill(shell: shellChat), in: .rect(cornerRadius: Theme.Radius.bubble, style: .continuous))
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
        .padding(.leading, Theme.Space.xxl)
    }
}

/// A task finished (or its agent did not start): what it did, its PR, and what to do next.
struct ReceiptCard: View {
    var item: FeedItem
    @Environment(\.openURL) private var openURL
    private var store: TasksStore { .shared }

    var body: some View {
        let tint = item.failed ? Theme.danger : Theme.positive
        HStack(alignment: .top, spacing: Theme.Space.m) {
            Image(systemName: item.failed ? Theme.Icon.errorFilled : Theme.Icon.successFilled)
                .font(Theme.Font.heading)
                .foregroundStyle(tint)
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                Text(item.failed ? "\(item.title ?? "任务")没开起来" : "\(item.title ?? "任务") 完成")
                    .font(Theme.Font.bodyStrong)
                if let s = item.summary, !s.isEmpty {
                    Text(s).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(4)
                }
                HStack(spacing: Theme.Space.s) {
                    if let pr = item.pr, let url = URL(string: pr), let label = item.prLabel {
                        Button { openURL(url) } label: { Label(label, systemImage: Theme.Icon.pullRequest).lineLimit(1) }
                            .themedButton()
                    }
                    if let id = item.ref, store.todos[id] != nil, !item.failed {
                        Button { store.detailTask = id } label: { Text("看看").lineLimit(1) }
                            .themedButton()
                    }
                }
                .font(Theme.Font.captionStrong)
                .padding(.top, Theme.Space.xs)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(Theme.Space.l)
        .background(tint.opacity(0.12), in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
    }
}

// MARK: - Above the composer

/// What the conversation's tasks are asking, at the bottom where the user is: the oldest question under its task's
/// name, answered where it is; the next comes up when it is done (「还有 N 件」 counts them).
struct TaskQuestions: View {
    var conversation: String
    private var store: TasksStore { .shared }

    var body: some View {
        let asks = store.tasks(of: conversation)
            .flatMap { todo in store.interactions(todo).map { (todo: todo, ix: $0) } }
            .sorted { ($0.ix.createdAt ?? .distantPast) < ($1.ix.createdAt ?? .distantPast) }
        if let first = asks.first {
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                HStack(spacing: Theme.Space.s) {
                    Label(first.todo.title, systemImage: Theme.Icon.askFirst).lineLimit(1)
                    Spacer(minLength: Theme.Space.s)
                    if asks.count > 1 { Text("还有 \(asks.count - 1) 件").lineLimit(1).fixedSize() }
                }
                .font(Theme.Font.captionStrong)
                .foregroundStyle(AgentState.needsYou.tone.text)
                .padding(.horizontal, Theme.Space.xs)
                InteractionPanel(interaction: first.ix)
            }
            .id(first.ix.id)
            .transition(.move(edge: .bottom).combined(with: .opacity))
        }
    }
}

/// The dock: a pill for each task going on (waiting for the user first), then 全部 for the board.
struct TaskDock: View {
    var conversation: String
    private var store: TasksStore { .shared }

    var body: some View {
        let dock = store.dock(conversation)
        let all = store.tasks(of: conversation)
        if !all.isEmpty {
            ScrollView(.horizontal) {
                HStack(spacing: Theme.Space.s) {
                    ForEach(dock) { todo in pill(todo) }
                    Button {
                        store.showBoard = true
                    } label: {
                        Label("全部 \(all.count)", systemImage: Theme.Icon.taskBoard)
                            .font(Theme.Font.captionMedium)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                            .padding(.horizontal, Theme.Space.m)
                            .padding(.vertical, Theme.Space.sm)
                            .background(Theme.Fill.chip, in: .capsule)
                    }
                    .buttonStyle(.plain)
                }
                .padding(.horizontal, Theme.Space.xs)
            }
            .scrollIndicators(.hidden)
            .animation(Theme.Motion.layout, value: dock.map(\.id))
        }
    }

    private func pill(_ todo: TodoItem) -> some View {
        let state = store.state(todo)
        let waiting = state == .needsYou
        return Button {
            store.detailTask = todo.id
        } label: {
            HStack(spacing: Theme.Space.sm) {
                TaskMark(state: state, size: Theme.Size.dot)
                Text(todo.title).lineLimit(1)
                if waiting { Text("等你").lineLimit(1).fixedSize() } // the title gives way, the ask does not
            }
            .font(Theme.Font.captionStrong)
            .foregroundStyle(waiting ? state.agentState.tone.text : .primary)
            .padding(.horizontal, Theme.Space.m)
            .padding(.vertical, Theme.Space.sm)
            .background(waiting ? state.agentState.tone.tint : Theme.Fill.chip, in: .capsule)
            .scaledFrame(maxWidth: Theme.Size.box200)
        }
        .buttonStyle(.plain)
        .transition(.scale.combined(with: .opacity))
    }
}

/// 「对「xx」说」 above the composer while the next message goes straight to a task; × sends it to Persona again.
struct RouteChip: View {
    var conversation: String
    private var store: TasksStore { .shared }

    var body: some View {
        if let id = store.routes[conversation], let todo = store.todos[id] {
            HStack(spacing: Theme.Space.xs) {
                Label("对「\(todo.title)」说", systemImage: Theme.Icon.routeToTask).lineLimit(1)
                Button {
                    withAnimation(Theme.Motion.state) { store.routes[conversation] = nil }
                } label: {
                    Image(systemName: Theme.Icon.close).font(Theme.Font.labelStrong)
                        .scaledFrame(width: Theme.Size.badge, height: Theme.Size.badge)
                        .contentShape(.circle)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("交给 Persona")
            }
            .font(Theme.Font.captionStrong)
            .foregroundStyle(Theme.accent)
            .padding(.leading, Theme.Space.m)
            .padding(.trailing, Theme.Space.xs)
            .padding(.vertical, Theme.Space.xs)
            .background(Theme.accent.opacity(0.14), in: .capsule)
            .frame(maxWidth: .infinity, alignment: .leading)
            .transition(.move(edge: .bottom).combined(with: .opacity))
        }
    }
}

/// The @ list: the conversation's tasks; picking one routes the next message to it.
struct TaskMentionList: View {
    var conversation: String
    var pick: (TodoItem) -> Void
    private var store: TasksStore { .shared }

    var body: some View {
        let tasks = store.tasks(of: conversation).filter { store.state($0) != .done }
        VStack(alignment: .leading, spacing: 0) {
            Text(tasks.isEmpty ? "这段对话还没有任务" : "对一个任务说")
                .font(Theme.Font.sectionHeader)
                .foregroundStyle(.secondary)
                .padding(.horizontal, Theme.Space.m)
                .padding(.vertical, Theme.Space.s)
            ForEach(tasks) { todo in
                Button { pick(todo) } label: {
                    HStack(spacing: Theme.Space.s) {
                        TaskMark(state: store.state(todo)).scaledFrame(width: Theme.Size.markM)
                        Text(todo.title).font(Theme.Font.body).lineLimit(1)
                        Spacer(minLength: Theme.Space.s)
                        Text(TaskPlace.line(todo, lead: nil)).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                    }
                    .padding(.horizontal, Theme.Space.m)
                    .padding(.vertical, Theme.Space.s)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.vertical, Theme.Space.xs)
        .surface(in: .rect(cornerRadius: Theme.Radius.panel, style: .continuous))
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }

    /// The @ the draft ends with ("@", "@登录"), if any.
    static func mention(in draft: String) -> Range<String.Index>? {
        guard let at = draft.lastIndex(of: "@") else { return nil }
        let rest = draft[draft.index(after: at)...]
        guard !rest.contains(where: \.isWhitespace) else { return nil }
        if at > draft.startIndex, !draft[draft.index(before: at)].isWhitespace { return nil } // an address, not a mention
        return at..<draft.endIndex
    }
}
