import SwiftUI

// The unified entry's pieces in the assistant's conversation (docs/unified-entry.md): the plan card, the task card,
// the lines the hub adds (answered, receipt, routed), the dock above the composer, the questions of the tasks' agents,
// and the route chip with its @ list. They read `TasksStore` and `AppModel`, so a card follows its task live.

/// A task's mark: the agent state dot it borrows (needs you breathes), a check once done.
struct TaskMark: View {
    var state: TaskState
    var size: CGFloat = Theme.Size.dotL

    var body: some View {
        if state == .failed {
            Image(systemName: Theme.Icon.errorFilled)
                .font(Theme.Font.labelStrong)
                .foregroundStyle(Theme.danger)
                .accessibilityLabel("没开起来")
        } else if state == .done || state == .unread {
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
        case .failed: "没开起来"
        }
    }

    /// The color of the task's state label: its agent state's, red when it never started.
    var labelColor: Color { self == .failed ? Theme.danger : agentState.tone.text }
}

// MARK: - Removing a task

/// 移除 for a task that does not run, like the Agents page's 关闭 for an agent: a task whose agents ran asks first (their
/// chats stay on the Agents page), one whose agent never started goes at once.
struct TaskRemoval: ViewModifier {
    @Binding var pending: TodoItem?

    func body(content: Content) -> some View {
        content.confirmationDialog(pending.map { "移除「\($0.title)」？" } ?? "",
                                   isPresented: Binding(get: { pending != nil }, set: { if !$0 { pending = nil } }),
                                   titleVisibility: .visible, presenting: pending) { todo in
            Button("移除", role: .destructive) { Task { await TasksStore.shared.remove(todo) } }
            Button("取消", role: .cancel) {}
        } message: { _ in
            Text("任务从这段对话里拿掉；它的 Agent 和对话记录还在 Agents 页。")
        }
    }
}

extension View {
    func confirmTaskRemoval(_ pending: Binding<TodoItem?>) -> some View { modifier(TaskRemoval(pending: pending)) }
}

extension TasksStore {
    /// Removes a task at once when no agent of it ever ran, else asks first (through `pending`).
    func askRemove(_ todo: TodoItem, pending: Binding<TodoItem?>) {
        if todo.agents.isEmpty { Task { await remove(todo) } } else { pending.wrappedValue = todo }
    }
}

/// The 移除任务 of a task's menus (long press on its card, line or pill), when nothing of it runs.
struct TaskRemoveMenuItem: View {
    var todo: TodoItem
    var state: TaskState
    var pending: Binding<TodoItem?>

    var body: some View {
        if state.isRemovable {
            Button(role: .destructive) { TasksStore.shared.askRemove(todo, pending: pending) } label: {
                Label("移除任务", systemImage: Theme.Icon.delete)
            }
        }
    }
}

/// "12 分钟", "1 小时 5 分" since a date.
enum TaskClock {
    /// How often a running step's time is drawn again: it shows minutes.
    static let tick: TimeInterval = 30

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
        } else if let planned = todo.planID.flatMap({ TasksStore.shared.plans[$0] })?.tasks.first(where: { $0.todoID == todo.id }) {
            // No agent (yet, or ever): where the plan meant it to run.
            parts.append(machineName(planned.machine))
            if !planned.project.isEmpty { parts.append(planned.project) }
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
            .sheetCancel(unsaved: !tasks.isEmpty && tasks != plan.tasks) { dismiss() }
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("开始") { start(tasks) }.bold() }
            }
        }
        #if os(macOS)
        .macSheet()
        #endif
        .onAppear { if tasks.isEmpty { tasks = plan.tasks } }
    }
}

// MARK: - Task card

/// A task in the conversation, updated in place: its mark, title, time, where it runs, what its agent says now.
struct TaskCard: View {
    var todoID: String
    var conversation: String
    @State private var removing: TodoItem?
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
                        if let t = TaskClock.since(todo.createdAt), state != .done, state != .failed {
                            Text(t).font(Theme.Font.caption).foregroundStyle(.tertiary).lineLimit(1).fixedSize()
                        }
                    }
                    Text(TaskPlace.line(todo, lead: lead))
                        .font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                    TaskNowLine(todo: todo, state: state, lead: lead)
                    if state != .done {
                        HStack(spacing: Theme.Space.l) {
                            Text(state.label).font(Theme.Font.captionMedium).foregroundStyle(state.labelColor).lineLimit(1)
                            Spacer()
                            if state == .failed {
                                // Nothing to say a word to: the way out is on the card.
                                Button {
                                    store.askRemove(todo, pending: $removing)
                                } label: {
                                    Label("移除", systemImage: Theme.Icon.delete).lineLimit(1)
                                }
                                .font(Theme.Font.captionStrong)
                                .foregroundStyle(Theme.danger)
                                .buttonStyle(.plain)
                            } else {
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
            .menuPreviewShape(.rect(cornerRadius: Theme.Radius.card, style: .continuous))
            .contextMenu { TaskRemoveMenuItem(todo: todo, state: state, pending: $removing) }
            .confirmTaskRemoval($removing)
            .animation(Theme.Motion.state, value: state)
        }
    }
}

/// What the task's agent is doing or said last, in a well: waiting for the user in its tone; while it only runs tools,
/// the step it is on and how long it has been at it (`TaskNow`).
struct TaskNowLine: View {
    var todo: TodoItem
    var state: TaskState
    var lead: Session?

    private var store: TasksStore { .shared }

    var body: some View {
        let now = TaskNow(lead: lead, working: state == .working)
        let text: String = {
            if state == .needsYou { return "有事等你回答" }
            if state == .failed { return "没开起来：\(store.startFailure(todo) ?? "原因不明")" }
            if let now { return now.text }
            if let e = todo.agents.first?.startError, !e.isEmpty { return "没开起来：\(e)" }
            return state == .notStarted ? "正在开 Agent" : "还没有动静"
        }()
        let tint: Color? = state == .needsYou ? state.agentState.tone.text : state == .failed ? Theme.danger : nil
        let since: Date? = if state != .needsYou, state != .failed, case .step(_, let at)? = now { at } else { nil }
        let isStep: Bool = if state != .needsYou, state != .failed, case .step? = now { true } else { false }
        // What the agent says starts at the well's edge; only a question for the user keeps its mark.
        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
            if state == .needsYou {
                Image(systemName: Theme.Icon.askFirst)
                    .font(Theme.Font.label)
                    .foregroundStyle(state.agentState.tone.text)
            }
            Text(text)
                .font(Theme.Font.secondary)
                .foregroundStyle(tint ?? (isStep ? .secondary : .primary))
                .lineLimit(isStep ? 1 : 2)
                .frame(maxWidth: .infinity, alignment: .leading)
            if let since {
                // How long the step has gone on: a long test run or CI wait reads as one, not as a stuck card.
                TimelineView(.periodic(from: .now, by: TaskClock.tick)) { context in
                    if let t = TaskClock.since(since, now: context.date) {
                        Text(t).font(Theme.Font.caption).foregroundStyle(.tertiary).lineLimit(1).fixedSize()
                    }
                }
            }
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.s)
        .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
    }
}

/// A task of another day, folded: one line; a tap opens its detail.
struct TaskLine: View {
    var todoID: String
    @State private var removing: TodoItem?
    private var store: TasksStore { .shared }

    var body: some View {
        if let todo = store.todos[todoID] {
            let state = store.state(todo)
            Button { store.detailTask = todo.id } label: {
                HStack(spacing: Theme.Space.s) {
                    TaskMark(state: state).scaledFrame(width: Theme.Size.markM)
                    Text(todo.title).font(Theme.Font.secondaryMedium).lineLimit(1)
                    Spacer(minLength: Theme.Space.s)
                    Text(state.label).font(Theme.Font.caption).foregroundStyle(state.labelColor).lineLimit(1).fixedSize()
                    Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong).foregroundStyle(.tertiary)
                }
                .padding(.horizontal, Theme.Space.m)
                .padding(.vertical, Theme.Space.s)
                .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .menuPreviewShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
            .contextMenu { TaskRemoveMenuItem(todo: todo, state: state, pending: $removing) }
            .confirmTaskRemoval($removing)
        }
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
            if let id = item.ref, let plan = store.plans[id] ?? item.plan { PlanCard(plan: plan) }
        case .task:
            if let id = item.ref {
                // A task card of another day folds into a line: its mark, title and where it got to.
                if Calendar.current.isDateInToday(item.at) { TaskCard(todoID: id, conversation: item.conversation) } else { TaskLine(todoID: id) }
            }
        case .today:
            if let body = item.today { TodayCard(item: item, body: body) }
        case .message:
            if let id = item.ref, !store.laterMessages.contains(id) { MessageCard(item: item, itemID: id) }
        case .answered: AnsweredLine(item: item, taskTitle: taskTitle)
        case .receipt: ReceiptCard(item: item)
        case .routed: routed
        case .unknown: EmptyView()
        }
    }

    private var taskTitle: String { item.ref.flatMap { store.todos[$0]?.title } ?? "任务" }

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

/// A card of a task's agent the user dealt with, kept as a line「✓ 任务 · 回答」. Tap it for every question with its
/// answer in full (#1231), selectable, and again to fold it; the task's name opens the task.
struct AnsweredLine: View {
    var item: FeedItem
    var taskTitle: String
    @State private var open = false
    @State private var unfold = Unfolding()
    /// Where the answers end on screen, to bring them into view once open (`ChatPin.show`).
    @State private var bottom: CGFloat = 0
    @Environment(\.chatPin) private var pin
    private var store: TasksStore { .shared }
    private var pinKey: String { "answered-" + item.id }

    var body: some View {
        let items = item.answeredItems
        VStack(alignment: .leading, spacing: 0) {
            header(expandable: !items.isEmpty)
            if open {
                VStack(alignment: .leading, spacing: Theme.Space.m) {
                    ForEach(Array(items.enumerated()), id: \.offset) { _, it in
                        VStack(alignment: .leading, spacing: Theme.Space.xs) {
                            if !it.question.isEmpty {
                                Text(it.question).font(Theme.Font.captionStrong).foregroundStyle(.secondary)
                            }
                            if !it.answer.isEmpty {
                                Text(it.answer).font(Theme.Font.secondary).foregroundStyle(.primary)
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .fixedSize(horizontal: false, vertical: true)
                        .textSelection(.enabled)
                        .accessibilityElement(children: .combine)
                    }
                }
                .padding(.horizontal, Theme.Space.m)
                .padding(.bottom, Theme.Space.m)
                .accessibilityIdentifier("answered-detail")
                .unfolding(unfold)
                .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .frame(maxWidth: open ? .infinity : nil, alignment: .leading)
        .onGeometryChange(for: CGFloat.self, of: { $0.frame(in: .global).maxY }) { bottom = $0 }
        .background(Theme.Fill.chip, in: open ? AnyShape(.rect(cornerRadius: Theme.Radius.card, style: .continuous)) : AnyShape(.capsule))
        .frame(maxWidth: .infinity, alignment: .leading)
        .chatPinned(pinKey)
    }

    private func header(expandable: Bool) -> some View {
        HStack(spacing: Theme.Space.s) {
            Image(systemName: Theme.Icon.done).font(Theme.Font.labelStrong)
            HStack(spacing: Theme.Space.xs) {
                if let id = item.ref, store.todos[id] != nil {
                    Button { store.detailTask = id } label: { Text(taskTitle).lineLimit(1) }
                        .buttonStyle(.plain)
                } else {
                    Text(taskTitle).lineLimit(1)
                }
                Text("· \(item.answer ?? "回答了")").lineLimit(1)
            }
            if expandable {
                Image(systemName: Theme.Icon.disclosure)
                    .font(Theme.Font.labelStrong)
                    .foregroundStyle(.tertiary)
                    .rotationEffect(.degrees(open ? 90 : 0))
                    .accessibilityHidden(true)
            }
        }
        .font(Theme.Font.caption)
        .foregroundStyle(.secondary)
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.sm)
        .contentShape(.rect)
        .onTapGesture { if expandable { toggle() } }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("answered-line")
        .accessibilityValue(expandable ? (open ? "已展开" : "已收起") : "")
        .accessibilityAction(named: open ? "收起" : "展开") { if expandable { toggle() } }
        .accessibilityAction(named: "打开任务") { if let id = item.ref, store.todos[id] != nil { store.detailTask = id } }
    }

    private func toggle() {
        Haptics.tap()
        let opening = !open
        toggleInPlace(pin, pinKey, animation: Theme.Motion.state, unfold: $unfold, closing: open) { open.toggle() }
        #if os(iOS)
        // The line as the chat's last row: the answers open under it, past the composer.
        guard opening, let pin else { return }
        Task { @MainActor in
            try? await Task.sleep(for: .seconds(Theme.Motion.revealTime))
            if open { pin.show(pinKey, bottom: bottom) }
        }
        #endif
    }
}

/// A message about one of the conversation's tasks, with the reply the AI 分身 drafted from the task's progress
/// (docs/unified-entry.md「信息和任务互相认得」): who said what, the task (live), the draft, 发送 / 改一下 / 稍后.
struct MessageCard: View {
    var item: FeedItem
    var itemID: String
    @State private var editing = false
    @State private var text = ""
    @State private var sending = false
    private var store: TasksStore { .shared }

    var body: some View {
        let settled = store.messageSettled(itemID)
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            HStack(spacing: Theme.Space.s) {
                Image(systemName: item.source == "mail" ? Theme.Icon.mail : Theme.Icon.groupChat)
                    .font(Theme.Font.labelStrong)
                    .foregroundStyle(Theme.loop)
                Text([item.sender, item.chat].compactMap { $0?.isEmpty == false ? $0 : nil }.joined(separator: " · "))
                    .font(Theme.Font.captionStrong).foregroundStyle(.secondary).lineLimit(1)
            }
            Text(item.text ?? "")
                .font(Theme.Font.body)
                .lineLimit(4)
                .padding(.leading, Theme.Space.m)
                .overlay(alignment: .leading) { Capsule().fill(Theme.loop).frame(width: Theme.Size.bar) }
            if let id = item.todoID, let todo = store.todos[id] {
                let state = store.state(todo)
                Button { store.detailTask = id } label: {
                    HStack(spacing: Theme.Space.s) {
                        TaskMark(state: state).scaledFrame(width: Theme.Size.markM)
                        Text(todo.title).font(Theme.Font.captionStrong).lineLimit(1)
                        Text(TaskNow(lead: store.leadSession(todo), working: state == .working)?.text ?? state.label)
                            .font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                        Spacer(minLength: 0)
                        Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong).foregroundStyle(.tertiary)
                    }
                    .padding(.horizontal, Theme.Space.m)
                    .padding(.vertical, Theme.Space.s)
                    .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
            }
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                Label(settled ? "已以你的名义回复" : "按任务进度起草的回复", systemImage: settled ? Theme.Icon.done : Theme.Icon.draftReply)
                    .font(Theme.Font.captionStrong)
                    .foregroundStyle(settled ? Theme.positive : Theme.accent)
                    .lineLimit(1)
                if editing {
                    TextField("回复", text: $text, axis: .vertical)
                        .font(Theme.Font.body)
                        .lineLimit(2...8)
                } else {
                    Text(text.isEmpty ? (item.draft ?? "") : text).font(Theme.Font.body)
                }
            }
            .padding(Theme.Space.m)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background((settled ? Theme.positive : Theme.accent).opacity(0.12), in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
            if !settled {
                HStack(spacing: Theme.Space.s) {
                    Button {
                        sending = true
                        Task {
                            await store.sendMessage(itemID, text: text.isEmpty ? (item.draft ?? "") : text)
                            sending = false
                            editing = false
                        }
                    } label: { Text("发送").lineLimit(1).frame(maxWidth: .infinity) }
                        .themedButton(prominent: true)
                    Button {
                        if text.isEmpty { text = item.draft ?? "" }
                        withAnimation(Theme.Motion.state) { editing.toggle() }
                    } label: { Text(editing ? "改好了" : "改一下").lineLimit(1) }
                        .themedButton()
                    Button { withAnimation(Theme.Motion.state) { store.later(itemID) } } label: { Text("稍后").lineLimit(1) }
                        .themedButton()
                }
                .disabled(sending)
            }
        }
        .card(padding: Theme.Space.l)
        .animation(Theme.Motion.state, value: settled)
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

/// The dock: 全部 for the board, held at the leading edge so it never scrolls out of reach, then a pill for each task
/// going on (waiting for the user first), scrolling sideways beside it. In the main conversation the agents going on
/// that no task stands for (opened on the Agents page, #1113) follow the tasks, marked as agents.
struct TaskDock: View {
    var conversation: String
    @State private var removing: TodoItem?
    private var store: TasksStore { .shared }

    var body: some View {
        let dock = store.dock(conversation)
        let all = store.tasks(of: conversation)
        let agents = store.agents(of: conversation)
        if !all.isEmpty || !agents.isEmpty {
            HStack(spacing: Theme.Space.s) {
                board(all.count + agents.count)
                if !dock.isEmpty || !agents.isEmpty {
                    ScrollView(.horizontal) {
                        HStack(spacing: Theme.Space.s) {
                            ForEach(dock) { todo in pill(todo) }
                            ForEach(agents) { agent in pill(agent) }
                        }
                    }
                    .scrollIndicators(.hidden)
                    #if os(macOS)
                    // The pills fade out at the trailing edge instead of being cut off by it (#1224 B6): a window
                    // narrower than the dock showed half a pill with its words cut.
                    .mask {
                        Color.black
                            .padding(.trailing, Theme.Size.macRowFade)
                            .overlay(alignment: .trailing) {
                                LinearGradient(colors: [.black, .clear], startPoint: .leading, endPoint: .trailing)
                                    .frame(width: Theme.Size.macRowFade)
                            }
                    }
                    #endif
                    .animation(Theme.Motion.layout, value: dock.map(\.id) + agents.map(\.key.path))
                }
            }
            .padding(.horizontal, Theme.Space.xs)
            .frame(maxWidth: .infinity, alignment: .leading)
            .confirmTaskRemoval($removing)
        }
    }

    private func board(_ count: Int) -> some View {
        Button {
            store.showBoard = true
        } label: {
            Text("全部 \(count)")
                .font(Theme.Font.captionMedium)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .padding(.horizontal, Theme.Space.m)
                .padding(.vertical, Theme.Space.sm)
                .background(Theme.Fill.chip, in: .capsule)
                .fixedSize() // the pills give way, 全部 does not
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("task-dock.board")
    }

    private func pill(_ todo: TodoItem) -> some View {
        let state = store.state(todo)
        return Button {
            store.detailTask = todo.id
        } label: {
            DockPillLabel(title: todo.title, state: state.agentState, waiting: state == .needsYou) {
                TaskMark(state: state, size: Theme.Size.dot)
            }
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("task-dock.pill")
        .menuPreviewShape(.capsule)
        .contextMenu { TaskRemoveMenuItem(todo: todo, state: state, pending: $removing) }
        .transition(.scale.combined(with: .opacity))
    }

    /// An agent no task stands for: its state's dot and the Agents mark, opening its page in the task's sheet.
    private func pill(_ agent: DockAgent) -> some View {
        Button {
            store.detailAgent = agent.key
        } label: {
            DockPillLabel(title: agent.title, state: agent.state, waiting: agent.isWaiting) {
                StatusDot(state: agent.state, size: Theme.Size.dot)
                Image(systemName: Theme.Icon.agents).font(Theme.Font.label)
                    .foregroundStyle(agent.isWaiting ? AnyShapeStyle(agent.state.tone.text) : AnyShapeStyle(.secondary))
            }
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Agent \(agent.title)，\(agent.isWaiting ? "等你" : "在做")")
        .accessibilityIdentifier("task-dock.agent")
        .transition(.scale.combined(with: .opacity))
    }
}

/// A dock pill's face, the same for a task and an agent: its marks, the title (which gives way), and 等你 tinted with
/// the state's tone while it waits for the user.
struct DockPillLabel<Marks: View>: View {
    var title: String
    var state: AgentState
    var waiting: Bool
    @ViewBuilder var marks: Marks

    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            marks
            Text(title).lineLimit(1)
            if waiting { Text("等你").lineLimit(1).fixedSize() } // the title gives way, the ask does not
        }
        .font(Theme.Font.captionStrong)
        .foregroundStyle(waiting ? state.tone.text : .primary)
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.sm)
        .background(waiting ? state.tone.tint : Theme.Fill.chip, in: .capsule)
        .scaledFrame(maxWidth: Theme.Size.box200)
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
        let tasks = store.tasks(of: conversation).filter { store.state($0) != .done && store.state($0) != .failed }
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

extension View {
    /// The shape a long press lifts for the context menu's preview (iOS). The Mac's right-click menu has no preview,
    /// and `ContentShapeKinds.contextMenuPreview` does not exist there (#1083).
    @ViewBuilder func menuPreviewShape(_ shape: some Shape) -> some View {
        #if os(iOS)
        contentShape(.contextMenuPreview, shape)
        #else
        self
        #endif
    }
}
