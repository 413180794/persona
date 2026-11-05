import SwiftUI

// The main conversation's 压缩上下文 and 新建主对话 (#1762, the hub's side in #1727; docs/assistant.md「手动压缩」
// 「新建主对话」): the menu entries, the note above the composer while it compacts, and the question before a new one.

extension AppModel {
    /// 压缩上下文 is offered: on the main conversation, when its hub can.
    func canCompact(_ key: SessionKey?) -> Bool {
        guard let key, key == assistantMain else { return false }
        return features(key.group).contains(MainConversationActions.compactFeature)
    }

    /// 新建主对话… is offered: on the main conversation, when its hub can.
    func canStartNewMain(_ key: SessionKey?) -> Bool {
        guard let key, key == assistantMain else { return false }
        return features(key.group).contains(MainConversationActions.newMainFeature)
    }

    /// Asks the hub to compact `key`; its progress comes on the session (`compaction`). A refusal (正在回答, 已经在压…)
    /// is the hub's own words.
    func compactConversation(_ key: SessionKey) async {
        guard let api = api(key.group) else { return }
        compactionRequests[key] = Date()
        Haptics.tap()
        do {
            try await api.compactConversation(key.sessionID, machineID: key.machineID)
        } catch {
            compactionRequests[key] = nil
            Haptics.error()
            toast = Toast(String(localized: "没开始压缩：\(error.shown ?? String(localized: "请稍后再试"))"), style: .error)
        }
    }

    /// Archives the main conversation and leaves none: back on the empty main page, the next message starts the new
    /// one (with the old one's summary in front of it when `carry`). False when the hub refused (the toast says why).
    func startNewMain(carry: Bool) async -> Bool {
        guard let main = assistantMain, let api = api(main.group) else { return false }
        do {
            let r = try await api.newMainConversation(carry: carry, expect: main.sessionID)
            await toEmptyMain()
            Haptics.success()
            toast = Toast(r.carried == true ? String(localized: "已新建主对话，会带上旧对话的摘要") : String(localized: "已新建主对话"), style: .success)
            return true
        } catch let e as APIError where e.code == "main_changed" {
            // Another device replaced it already: show what the hub has now.
            await toEmptyMain()
            toast = Toast(String(localized: "主对话已在别的设备上换过了，已刷新"), style: .info)
            return true
        } catch {
            Haptics.error()
            toast = Toast(String(localized: "没新建：\(error.shown ?? String(localized: "请稍后再试"))"), style: .error)
            return false
        }
    }

    private func toEmptyMain() async {
        assistantCurrent = nil
        await refreshAssistant()
    }
}

/// The menu entries (the top bar's menu, the Mac's toolbar menu, the sidebar row's context menu); nothing where they
/// do not apply. In its own view: the compaction state it reads changes with every push.
struct MainConversationMenu: View {
    @Environment(AppModel.self) private var model
    var key: SessionKey?

    var body: some View {
        if let key, model.canCompact(key) {
            let running = model.session(key)?.compaction?.state == .running || model.compactionRequests[key] != nil
            Button { Task { await model.compactConversation(key) } } label: {
                Label(running ? "正在压缩上下文…" : "压缩上下文", systemImage: Theme.Icon.compactContext)
            }
            .disabled(running)
        }
        if model.canStartNewMain(key) {
            Button { model.newMainAsked = true } label: { Label("新建主对话…", systemImage: Theme.Icon.newConversation) }
        }
    }
}

/// Works out what the composer's note shows for `key`'s compaction (`AppModel.compactionShown`): running while it runs or
/// was just asked for; done for a moment and failed until closed, only for a change this chat saw or one that just
/// finished. Drawn behind the composer, so it lives as long as the chat does.
struct CompactionTracker: View {
    @Environment(AppModel.self) private var model
    var key: SessionKey
    /// The state this chat saw last.
    @State private var seen: CompactionRun.State?

    private var run: CompactionRun? { model.session(key)?.compaction }

    private var signature: String {
        guard let run else { return "" }
        return [MainConversationActions.key(run), run.finishedAt.map { "\($0.timeIntervalSince1970)" } ?? "", run.error ?? ""].joined(separator: "|")
    }

    var body: some View {
        Color.clear
            .allowsHitTesting(false)
            .accessibilityHidden(true)
            .onChange(of: signature, initial: true) { old, new in
                // The hub's first word after 压缩上下文 was asked: it speaks for itself from now on.
                if old != new { model.compactionRequests[key] = nil }
                update()
                seen = run?.state
            }
            .onChange(of: model.compactionDismissed[key]) { update() }
            .task(id: model.compactionRequests[key]) { await requestExpires() }
            .task(id: model.compactionShown[key]) { await doneFades() }
    }

    private func update() {
        let shown = MainConversationActions.shown(run, seen: seen, now: Date(), dismissed: model.compactionDismissed[key])
        if model.compactionShown[key] != shown { model.compactionShown[key] = shown }
    }

    /// Asked, and the hub said nothing for a while: the note goes back to what the hub says.
    private func requestExpires() async {
        guard let at = model.compactionRequests[key] else { return }
        try? await Task.sleep(for: .seconds(MainConversationActions.requestGrace))
        if !Task.isCancelled, model.compactionRequests[key] == at { model.compactionRequests[key] = nil }
    }

    private func doneFades() async {
        guard case .done = model.compactionShown[key] else { return }
        try? await Task.sleep(for: .seconds(MainConversationActions.doneShown))
        if !Task.isCancelled, case .done = model.compactionShown[key] { model.compactionShown[key] = nil }
    }
}

extension AppModel {
    /// What the composer's note shows for `key`: asked here, running before the hub says so; else what the hub said.
    func compactionNote(_ key: SessionKey) -> MainConversationActions.Shown? {
        compactionRequests[key] != nil ? .running : compactionShown[key]
    }
}

/// The note's place above the composer: nothing at all while there is nothing to say.
struct CompactionSlot: View {
    @Environment(AppModel.self) private var model
    var key: SessionKey

    var body: some View {
        if let shown = model.compactionNote(key) {
            CompactionNote(key: key, shown: shown)
                .transition(.move(edge: .bottom).combined(with: .opacity))
        }
    }
}

/// On the composer: the tracker behind it (assistant conversations only) and the note's animation.
struct CompactionTracking: ViewModifier {
    @Environment(AppModel.self) private var model
    var key: SessionKey

    func body(content: Content) -> some View {
        content
            .animation(Theme.Motion.standard, value: model.compactionNote(key))
            .background { if model.session(key)?.assistant == true { CompactionTracker(key: key) } }
    }
}

/// Above the composer: 正在压缩上下文…, 已压缩上下文 for a moment, 压缩失败：<why> with a close button.
struct CompactionNote: View {
    @Environment(AppModel.self) private var model
    var key: SessionKey
    var shown: MainConversationActions.Shown

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
            switch shown {
            case .running:
                ProgressView().controlSize(.mini)
                Text("正在压缩上下文…").font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1)
                Spacer(minLength: 0)
            case .done:
                Image(systemName: Theme.Icon.successFilled).font(Theme.Font.labelStrong).foregroundStyle(Theme.positive)
                Text("已压缩上下文").font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1)
                Spacer(minLength: 0)
            case .failed(let k, let error):
                Image(systemName: Theme.Icon.errorFilled).font(Theme.Font.labelStrong).foregroundStyle(Theme.danger)
                Text("压缩失败：\(error)").font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(3)
                Spacer(minLength: 0)
                Button {
                    withAnimation(Theme.Motion.standard) {
                        model.compactionDismissed[key] = k
                        model.compactionShown[key] = nil
                    }
                } label: {
                    Image(systemName: Theme.Icon.close).font(Theme.Font.labelStrong)
                        .scaledFrame(width: Theme.Size.icon, height: Theme.Size.icon)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
                .accessibilityLabel("关闭")
                .accessibilityIdentifier("chat.compaction.close")
            }
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.s)
        .surface(tint: isFailed ? Theme.danger.opacity(0.06) : nil, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("chat.compaction")
    }

    private var isFailed: Bool { if case .failed = shown { true } else { false } }
}

/// The question before 新建主对话: the main conversation is archived (renamed 旧主对话 · 至 M月D日, under 已归档), the
/// next message starts the new one; 带上旧对话的摘要 on by default.
struct NewMainSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var carry = true
    @State private var busy = false

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text("现在的主对话会归档，改名为「\(MainConversationActions.archivedTitle())」，之后在「已归档」里能找到。新的主对话从你发的下一条消息开始。")
                        .font(Theme.Font.secondary)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Section {
                    Toggle(isOn: $carry) { Text("带上旧对话的摘要").lineLimit(1) }
                        .disabled(busy)
                        .accessibilityIdentifier("newmain.carry")
                } footer: {
                    Text("没压缩过的只带最近几条；想带完整摘要，可以先压缩上下文。")
                }
            }
            .navigationTitle("新建主对话？")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button { dismiss() } label: { Text("取消").lineLimit(1) }
                        .disabled(busy)
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button { Task { await go() } } label: {
                        if busy { ProgressView().controlSize(.small) } else { Text("新建主对话").lineLimit(1) }
                    }
                    .disabled(busy)
                    .accessibilityIdentifier("newmain.confirm")
                }
            }
        }
        #if os(macOS)
        .macSheet()
        #else
        .presentationDetents([.medium])
        #endif
    }

    private func go() async {
        busy = true
        let done = await model.startNewMain(carry: carry)
        busy = false
        if done { dismiss() }
    }
}
