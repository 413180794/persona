import SwiftUI

// 健康 (docs/health.md, #2691): the health space, apart from Persona's other conversations, like ChatGPT's Health. Its
// home says what it is, asks something to start, lists its past conversations and its own memory, where its data comes
// from and what stays private. Its conversations run in a directory of their own on the assistant's machine, so the
// assistant's other conversations never see them and its memory is its own (`?space=health`). The web's `/health`.

/// 健康's state and what it asks the hub (MVVM, docs/ios.md「逻辑层的选型」): the space's conversations
/// (`GET /api/v1/assistant?space=health`) and how many memories it keeps.
@Observable final class HealthModel {
    private(set) var fetched: [Session] = []
    private(set) var memoryCount: Int?
    private(set) var loaded = false
    /// Why the list could not be read; shown only while there is nothing to show.
    private(set) var error: String?
    @ObservationIgnored private let deps: AssistantDeps

    init(deps: AssistantDeps = .live) { self.deps = deps }

    func load() async {
        guard let api = deps.api() else { return }
        async let memory = try? api.assistantMemory(space: Session.healthSpace)
        do {
            fetched = try await api.healthConversations()
            error = nil
        } catch {
            self.error = error.shown ?? self.error
        }
        if let files = await memory { memoryCount = files.filter { $0.name != AssistantMemoryEntry.index }.count }
        loaded = true
    }

    /// The space's open conversations, newest first: the hub's list, with what the pushes say since (a title, its
    /// last line, a new one) over it. An archived one leaves the list.
    nonisolated static func merge(_ fetched: [Session], live: [Session]) -> [Session] {
        var by: [SessionKey: Session] = [:]
        for s in fetched { by[s.key] = s }
        for s in live where s.isHealth { by[s.key] = s }
        return by.values.filter { $0.archived != true }
            .sorted { ($0.updatedAt ?? .distantPast) > ($1.updatedAt ?? .distantPast) }
    }

    /// One-tap questions (the web's): each starts a health conversation with it.
    static let suggestions = ["这周我睡得怎么样？", "最近 7 天步数和上周比怎么样", "我的静息心率最近有变化吗", "帮我记一下今天体重 62.5 kg"]
}

struct HealthView: View {
    @Environment(AppModel.self) private var model
    /// A conversation is being started (the Persona page shows it coming).
    var disabled: Bool
    @Binding var launcher: String?
    /// Starts a health conversation with the message (`AssistantView.start(…, space:)`).
    var send: (String, [ImagePayload]) -> Void
    @State private var page = HealthModel()

    var body: some View {
        // The field under the list, not over it (a safe-area inset): the Mac's list ran on under the field.
        VStack(spacing: 0) {
            sections(HealthModel.merge(page.fetched, live: model.healthConversations))
                .themedPage()
            Group {
                if AppStyle.current.usesBeautifulUI {
                    BeautifulStarterComposer(disabled: disabled, launcher: $launcher, placeholder: "问健康相关的问题…", send: send)
                } else {
                    StarterComposer(disabled: disabled, placeholder: "问健康相关的问题…", send: send)
                }
            }
            .padding(.horizontal, Theme.Space.m)
            .padding(.top, Theme.Space.s)
            .padding(.bottom, Theme.Space.s)
            .frame(maxWidth: ChatView.readableWidth)
            .frame(maxWidth: .infinity)
            .background(Theme.groupedPage.ignoresSafeArea(edges: .bottom))
        }
        .background(Theme.groupedPage)
        .pageTitle("健康")
        .task { await page.load() }
        .refreshable { await page.load() }
    }

    private func sections(_ list: [Session]) -> some View {
        ScrollViewReader { proxy in
            List {
                Section { hero }
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets())
                Section {
                    ForEach(HealthModel.suggestions, id: \.self) { q in
                        Button { send(q, []) } label: {
                            Label { Text(q).foregroundStyle(Color.primary).lineLimit(1) } icon: {
                                Image(systemName: Theme.Icon.ai).foregroundStyle(Theme.Hue.pink)
                            }
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                        .disabled(disabled)
                        .accessibilityIdentifier("health-suggestion")
                    }
                } header: {
                    Text("问问看")
                }
                Section {
                    if !list.isEmpty {
                        ForEach(list) { s in row(s) }
                    } else if !page.loaded {
                        ProgressView().frame(maxWidth: .infinity)
                    } else if let error = page.error {
                        Text("没读到健康对话：\(error)").font(Theme.Font.secondary).foregroundStyle(.secondary)
                    } else {
                        Text("还没有健康对话。在下面问一个问题，就会开始第一段。").font(Theme.Font.secondary).foregroundStyle(.secondary)
                    }
                } header: {
                    Text("健康对话")
                }
                Section {
                    NavigationLink { AssistantMemoryView(space: Session.healthSpace) } label: {
                        HStack {
                            Label { Text("健康记忆").lineLimit(1) } icon: { Image(systemName: Theme.Icon.memory) }
                            Spacer(minLength: Theme.Space.s)
                            if let n = page.memoryCount, n > 0 {
                                Text("\(n) 条").font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1)
                            }
                        }
                    }
                    .accessibilityIdentifier("health-memory")
                } footer: {
                    Text("健康对话里记下的目标、过敏、用药等，单独存放，不会流到其他对话；可以随时查看和删除。")
                }
                Section {
                    source(Theme.Icon.health, "Apple Health", "经你的 iPhone 读取步数、睡眠、心率、体重等汇总，需要 Persona 在手机上打开并授权。")
                    source(Theme.Icon.edit, "记一条数据", "说「记一下体重」「喝了 300 毫升水」，手机上确认后写进健康。")
                    source(Theme.Icon.photo, "化验单和报告", "发图片，逐项读出来并标出超出参考范围的。")
                } header: {
                    Text("数据来源")
                }
                Section {
                    VStack(alignment: .leading, spacing: Theme.Space.s) {
                        Label { Text("隐私").font(Theme.Font.heading) } icon: { Image(systemName: Theme.Icon.privacy) }
                        ForEach(Self.privacy, id: \.self) { line in
                            Text("· " + line).font(Theme.Font.secondary).foregroundStyle(.secondary)
                        }
                    }
                    .padding(.vertical, Theme.Space.xs)
                    .id("health-privacy")
                }
            }
            #if DEBUG
            // `-showHealthBottom YES`: scrolled to the privacy note, for screenshots of the lower sections.
            .task(id: page.loaded) {
                guard page.loaded, UserDefaults.standard.bool(forKey: "showHealthBottom") else { return }
                try? await Task.sleep(for: .milliseconds(300))
                proxy.scrollTo("health-privacy", anchor: .bottom)
            }
            #endif
        }
    }

    private static let privacy = [
        "健康对话只在这里：Persona 的其他对话看不到、读不到。",
        "对话和记忆都留在 hub 所在的机器上，不进知识库。",
        "这里给的是解释和建议，不能替代医生的诊断和治疗。",
    ]

    private var hero: some View {
        VStack(spacing: Theme.Space.s) {
            Image(systemName: Theme.Icon.healthFilled)
                .font(Theme.Font.title)
                .foregroundStyle(Theme.Hue.pink)
                .scaledFrame(width: Theme.Size.box56, height: Theme.Size.box56)
                .background(Theme.Hue.pink.opacity(0.14), in: .circle)
                .accessibilityHidden(true)
            Text("问问你的健康").font(Theme.Font.title).lineLimit(1)
            Text("睡眠、步数、心率、体重、饮食，基于你手机里的健康数据回答。这里的对话和记忆与 Persona 的其他对话分开。")
                .font(Theme.Font.secondary)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, Theme.Space.l)
    }

    private func row(_ s: Session) -> some View {
        let title = s.title.isEmpty || s.title == "Claude Code" ? "新对话" : s.title
        return Button { model.openAssistantConversation(s.key) } label: {
            HStack(spacing: Theme.Space.m) {
                Image(systemName: Theme.Icon.healthFilled)
                    .font(Theme.Font.secondary)
                    .foregroundStyle(Theme.Hue.pink)
                    .scaledFrame(width: Theme.Size.well, height: Theme.Size.well)
                    .background(Theme.Hue.pink.opacity(0.14), in: .circle)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(title).font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
                    if let last = s.lastText, !last.isEmpty {
                        Text(last).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
                Spacer(minLength: Theme.Space.s)
                if let at = s.updatedAt {
                    Text(Fmt.relative(at)).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                }
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("health-conversation")
        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
            Button { Task { await model.archiveConversation(s.key, true); await page.load() } } label: {
                Label("归档", systemImage: Theme.Icon.archive)
            }
        }
        .contextMenu {
            Button { Task { await model.archiveConversation(s.key, true); await page.load() } } label: {
                Label("归档", systemImage: Theme.Icon.archive)
            }
        }
    }

    private func source(_ symbol: String, _ title: String, _ body: String) -> some View {
        Label {
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(title).font(Theme.Font.body).lineLimit(1)
                Text(body).font(Theme.Font.secondary).foregroundStyle(.secondary)
            }
        } icon: {
            Image(systemName: symbol).foregroundStyle(symbol == Theme.Icon.health ? Theme.Hue.pink : .secondary)
        }
    }
}
