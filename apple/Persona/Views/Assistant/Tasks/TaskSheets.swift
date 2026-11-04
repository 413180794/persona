import SwiftUI

/// A task's detail, risen from the bottom like a link (docs/unified-entry.md「任务详情」): opens full, pulled down to
/// 0.6 the conversation behind stays usable. Its agent's page is the Agents page's own chat (`ChatView(style: .dev)`),
/// so everything that page does works here; a task with several agents switches between them at the top. An agent no
/// task stands for (the main conversation's dock, #1113) opens in the same sheet, its page under its own header.
struct TaskDetailSheet: View {
    private var todoID: String?
    private var agentKey: SessionKey?
    @State private var detent: PresentationDetent = .large
    @State private var picked: SessionKey?
    @State private var removing: TodoItem?
    @Environment(\.dismiss) private var dismiss
    private var store: TasksStore { .shared }

    init(todoID: String) { self.todoID = todoID }
    init(agent: SessionKey) { agentKey = agent }

    var body: some View {
        NavigationStack {
            Group {
                if let agentKey {
                    agentContent(agentKey)
                } else if let todo = todoID.flatMap({ store.todos[$0] }) {
                    content(todo)
                } else {
                    EmptyStateView(symbol: Theme.Icon.todo, title: "没有这个任务了", message: "它可能被删了。")
                }
            }
            // The agent's page hides the bar (it draws its own header): the sheet's close is in the task's header.
            .toolbarVisibility(.hidden, for: .navigationBar)
        }
        .confirmTaskRemoval($removing)
        .presentationDetents([.fraction(0.6), .large], selection: $detent)
        .presentationDragIndicator(.visible)
        .presentationBackgroundInteraction(.enabled(upThrough: .fraction(0.6)))
    }

    @ViewBuilder private func content(_ todo: TodoItem) -> some View {
        let sessions = store.sessions(todo)
        let key = picked.flatMap { p in sessions.first { $0.key == p }?.key } ?? store.leadSession(todo)?.key
        // Top-aligned: when the agent's page asks for more than the sheet has (several tall cards waiting), what gives
        // is its bottom, never the task's header with its close.
        GeometryReader { geo in
        VStack(spacing: 0) {
            header(todo, sessions: sessions, selected: key)
            Divider()
            Group {
                if let key {
                    ChatView(key: key, style: .dev)
                        .id(key)
                } else if let failure = store.startFailure(todo) ?? todo.agents.first?.startError {
                    EmptyStateView(symbol: Theme.Icon.errorFilled, title: "Agent 没开起来", message: failure,
                                   action: (title: "移除任务", run: { store.askRemove(todo, pending: $removing) }))
                } else {
                    EmptyStateView(symbol: Theme.Icon.working, title: "正在开 Agent", message: "开好以后这里就是它的对话。")
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .frame(width: geo.size.width, height: geo.size.height, alignment: .top) // the sheet's height, not the page's wish
        .clipped()
        }
    }

    private func header(_ todo: TodoItem, sessions: [Session], selected: SessionKey?) -> some View {
        let state = store.state(todo)
        return VStack(alignment: .leading, spacing: Theme.Space.s) {
            HStack(spacing: Theme.Space.s) {
                TaskMark(state: state).scaledFrame(width: Theme.Size.markM)
                Text(todo.title).font(Theme.Font.heading).lineLimit(2)
                Spacer(minLength: Theme.Space.s)
                if state.isRemovable {
                    Menu {
                        TaskRemoveMenuItem(todo: todo, state: state, pending: $removing)
                    } label: {
                        Image(systemName: Theme.Icon.more).font(Theme.Font.secondaryStrong)
                            .scaledFrame(width: Theme.Size.well, height: Theme.Size.well)
                    }
                    .themedButton()
                    .clipShape(.circle)
                    .accessibilityLabel("更多")
                }
                closeButton
            }
            if sessions.count > 1 {
                ScrollView(.horizontal) {
                    HStack(spacing: Theme.Space.s) {
                        ForEach(sessions) { s in
                            Button { picked = s.key } label: {
                                HStack(spacing: Theme.Space.xs) {
                                    StatusDot(state: s.state, size: Theme.Size.dotS)
                                    Text(s.title.isEmpty ? s.agentLabel : s.title).lineLimit(1)
                                }
                                .font(Theme.Font.captionStrong)
                                .padding(.horizontal, Theme.Space.m)
                                .padding(.vertical, Theme.Space.sm)
                                .background(s.key == selected ? Theme.accent.opacity(0.16) : Theme.Fill.chip, in: .capsule)
                            }
                            .buttonStyle(.plain)
                        }
                    }
                }
                .scrollIndicators(.hidden)
            }
        }
        .padding(.horizontal, Theme.Space.page)
        .padding(.top, Theme.Space.l) // below the sheet's grabber
        .padding(.bottom, Theme.Space.m)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

extension TaskDetailSheet {
    /// The agent's page under a header like a task's: its state, its title, and that it is an agent of its own.
    @ViewBuilder private func agentContent(_ key: SessionKey) -> some View {
        let session = AppModel.shared.session(key)
        GeometryReader { geo in
        VStack(spacing: 0) {
            HStack(spacing: Theme.Space.s) {
                if let session { StatusDot(state: session.state, size: Theme.Size.dotL).scaledFrame(width: Theme.Size.markM) }
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(session?.displayTitle ?? "Agent").font(Theme.Font.heading).lineLimit(2)
                    AgentMarkLine(text: "Agent · 不是从对话开的")
                }
                Spacer(minLength: Theme.Space.s)
                closeButton
            }
            .padding(.horizontal, Theme.Space.page)
            .padding(.top, Theme.Space.l) // below the sheet's grabber
            .padding(.bottom, Theme.Space.m)
            .frame(maxWidth: .infinity, alignment: .leading)
            Divider()
            Group {
                if session != nil {
                    ChatView(key: key, style: .dev).id(key)
                } else {
                    EmptyStateView(symbol: Theme.Icon.agents, title: "没有这个 Agent 了", message: "它可能已经关掉了。")
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .frame(width: geo.size.width, height: geo.size.height, alignment: .top)
        .clipped()
        }
    }

    private var closeButton: some View {
        Button { dismiss() } label: {
            Image(systemName: Theme.Icon.close).font(Theme.Font.secondaryStrong)
                .scaledFrame(width: Theme.Size.well, height: Theme.Size.well)
        }
        .themedButton()
        .clipShape(.circle)
        .accessibilityLabel("关闭")
    }
}

/// The Agents mark and a line after it, tight: what says an agent is no task's on the board and in its sheet.
private struct AgentMarkLine: View {
    var text: String

    var body: some View {
        HStack(spacing: Theme.Space.xs) {
            Image(systemName: Theme.Icon.agents)
            Text(text).lineLimit(1)
        }
        .font(Theme.Font.caption)
        .foregroundStyle(.secondary)
    }
}

/// 全部: the conversation's tasks by where they are — waiting for the user, going on, done, stopped.
struct TaskBoardSheet: View {
    var conversation: String
    @State private var removing: TodoItem?
    @Environment(\.dismiss) private var dismiss
    private var store: TasksStore { .shared }

    var body: some View {
        NavigationStack {
            let rows = store.tasks(of: conversation).map { (todo: $0, state: store.state($0)) }
            let agents = store.agents(of: conversation)
            List {
                if rows.isEmpty && agents.isEmpty {
                    EmptyStateView(symbol: Theme.Icon.todo, title: "还没有任务", message: "在对话里说要做的事，Persona 会把它变成任务。")
                        .listRowBackground(Color.clear)
                }
                section("等你", rows.filter { $0.state == .needsYou }, agents: agents.filter(\.isWaiting))
                section("在做", rows.filter { $0.state == .working || $0.state == .notStarted }, agents: agents.filter { !$0.isWaiting })
                section("做完了", rows.filter { $0.state == .unread })
                section("停着", rows.filter { $0.state == .followup })
                section("已完成", rows.filter { $0.state == .done })
                section("没开起来", rows.filter { $0.state == .failed })
            }
            .themedGroupedPage()
            .navigationTitle("任务")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button { dismiss() } label: { Image(systemName: Theme.Icon.close) }
                        .accessibilityLabel("关闭")
                }
            }
        }
        .confirmTaskRemoval($removing)
        .presentationDetents([.large])
        .presentationDragIndicator(.visible)
    }

    /// A section's tasks, then (in the main conversation) the agents no task stands for in the same state.
    @ViewBuilder private func section(_ title: String, _ rows: [(todo: TodoItem, state: TaskState)], agents: [DockAgent] = []) -> some View {
        if !rows.isEmpty || !agents.isEmpty {
            Section {
                ForEach(rows, id: \.todo.id) { row in
                    Button {
                        store.openAfterBoard = row.todo.id // opened once the board is down (one sheet at a time)
                        store.showBoard = false
                    } label: {
                        HStack(spacing: Theme.Space.m) {
                            TaskMark(state: row.state).scaledFrame(width: Theme.Size.markM)
                            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                                Text(row.todo.title).font(Theme.Font.body).lineLimit(1)
                                Text(subtitle(row.todo)).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                            }
                            Spacer(minLength: Theme.Space.s)
                            Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong).foregroundStyle(.tertiary)
                        }
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    // The Agents page's 关闭, for a task: swiped from the trailing edge, only when nothing of it runs.
                    .swipeActions(edge: .trailing) {
                        if row.state.isRemovable {
                            Button { store.askRemove(row.todo, pending: $removing) } label: {
                                Label("移除", systemImage: Theme.Icon.delete)
                            }
                            .tint(Theme.danger)
                        }
                    }
                    .contextMenu { TaskRemoveMenuItem(todo: row.todo, state: row.state, pending: $removing) }
                }
                ForEach(agents) { agent in agentRow(agent) }
            } header: {
                Text("\(title) · \(rows.count + agents.count)")
            }
        }
    }

    private func agentRow(_ agent: DockAgent) -> some View {
        Button {
            store.openAgentAfterBoard = agent.key // opened once the board is down (one sheet at a time)
            store.showBoard = false
        } label: {
            HStack(spacing: Theme.Space.m) {
                StatusDot(state: agent.state, size: Theme.Size.dotL).scaledFrame(width: Theme.Size.markM)
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(agent.title).font(Theme.Font.body).lineLimit(1)
                    AgentMarkLine(text: agentSubtitle(agent))
                }
                Spacer(minLength: Theme.Space.s)
                Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong).foregroundStyle(.tertiary)
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("task-board.agent")
    }

    /// Where the agent is and what it said last, like a task's line: machine · project · agent · last words.
    private func agentSubtitle(_ agent: DockAgent) -> String {
        let model = AppModel.shared
        let session = model.session(agent.key)
        let last = session?.lastText?.trimmingCharacters(in: .whitespacesAndNewlines)
        return [model.machine(agent.key.machineRef)?.name ?? agent.key.machineID, session?.folderName, session?.agentLabel, last]
            .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private func subtitle(_ todo: TodoItem) -> String {
        let lead = store.leadSession(todo)
        let place = TaskPlace.line(todo, lead: lead)
        if let failure = store.startFailure(todo) { return place.isEmpty ? failure : place + " · " + failure }
        guard let now = TaskNow(lead: lead, working: store.state(todo) == .working) else { return place }
        return place + " · " + now.text
    }
}
