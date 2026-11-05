import SwiftUI

/// What can be done with an opened inbox item (#831; the iPhone's `InboxActionBar` over the item): the AI's summary,
/// the AI 分身 at work, and the actions as labelled buttons: AI 草拟回复 / 查看草稿 (or, where the source is answered
/// in its own app, 在信息里回复 / 在 Telegram 里回复 / 打开), 交给 AI, and 已处理 (移回 once filed, the others staying as
/// on the iPhone). A column at the detail's trailing edge; where the detail is too narrow for one, a strip over the item.
struct MacInboxActions: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openURL) private var openURL
    var item: InboxItem
    /// A column at the trailing edge (the buttons one under another), or a strip over the item (side by side).
    var column: Bool
    /// A document is open beside the item: the strip starts with 显示列表 / 收起列表 (the list folds away for it).
    var reading = false
    var draft: () -> Void
    var handOff: () -> Void

    var body: some View {
        if column {
            ScrollView {
                VStack(alignment: .leading, spacing: Theme.Space.l) {
                    summary
                    VStack(spacing: Theme.Space.s) { buttons(iconsOnly: false) }
                }
                .padding(Theme.Space.l)
            }
            .background(Theme.Fill.macPanel) // the chat's 会话信息 column's
        } else {
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                summary
                // The words while they fit; a narrow strip keeps the icons (each says what it does on hover) rather
                // than cutting the words to 「查…」「交给…」.
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: Theme.Space.s) { listButton; buttons(iconsOnly: false) }.fixedSize()
                    HStack(spacing: Theme.Space.s) { listButton; buttons(iconsOnly: true) }
                }
            }
            .padding(.horizontal, Theme.Space.l)
            .padding(.vertical, Theme.Space.s)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.bar)
            .overlay(alignment: .bottom) { Divider() }
        }
    }

    /// While a document is read: brings the folded list back beside it, or folds it again.
    @ViewBuilder private var listButton: some View {
        if reading {
            let state = MacInboxState.shared
            action(state.listWhileReading ? "收起列表" : "显示列表", Theme.Icon.list, iconsOnly: true,
                   help: state.listWhileReading ? "收起列表，把地方留给文档" : "显示收件箱列表（关掉文档也会回来）") {
                state.listWhileReading.toggle()
            }
        }
    }

    /// The AI's summary of the item, and the AI 分身 at work when it is.
    @ViewBuilder private var summary: some View {
        let s = item.summary ?? ""
        if !s.isEmpty {
            (Text(Image(systemName: Theme.Icon.ai)).foregroundStyle(Theme.ai) + Text(" " + s))
                .font(Theme.Font.secondary)
                .lineLimit(column ? nil : 3)
                .fixedSize(horizontal: false, vertical: column)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        if item.agentWorking {
            HStack(spacing: Theme.Space.sm) {
                ProgressView().controlSize(.mini)
                Text(item.agent?.state == "queued" ? "AI 分身排队中" : "AI 分身正在处理" + (item.agent?.task.map { "：" + $0 } ?? ""))
                    .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(column ? 3 : 1)
            }
        }
    }

    @ViewBuilder private func buttons(iconsOnly: Bool) -> some View {
        // A connector's thread that cannot be answered here (a CI run, a text message) is answered in its own app.
        if item.connector.map({ $0.canReply == true }) ?? true {
            action(item.agentDraft == nil ? "AI 草拟回复" : "查看草稿", Theme.Icon.draftReply, prominent: true, iconsOnly: iconsOnly,
                   help: item.agentDraft == nil ? "让 AI 用你的口吻草拟一条回复" : "AI 分身草拟好了回复，看看再发", perform: draft)
        } else if let open = item.connector?.openURL, let url = InboxActionBar.systemURL(open, draft: item.agentDraft) {
            let sms = url.scheme == "sms"
            action(InboxActionBar.openTitle(item, sms: sms), sms ? Theme.Icon.messagesApp : Theme.Icon.openExternal,
                   prominent: true, iconsOnly: iconsOnly, help: "在它自己的 App 里回复") { openURL(url) }
        }
        action("交给 AI", Theme.Icon.handToAI, iconsOnly: iconsOnly, help: "把这件事交给 Persona 或一个 Agent", perform: handOff)
        if item.isDone {
            action("移回", Theme.Icon.undo, iconsOnly: iconsOnly, help: "移回收件箱") { InboxStore.shared.setDone(item, false) }
        } else {
            action("已处理", Theme.Icon.done, iconsOnly: iconsOnly, help: "标为已处理（列表里按 E）") {
                MacInboxState.shared.toggleDone([.item(item.id)], model: model)
            }
        }
    }

    @ViewBuilder private func action(_ title: String, _ symbol: String, prominent: Bool = false, iconsOnly: Bool,
                                     help: String, perform: @escaping () -> Void) -> some View {
        let button = Button(action: perform) {
            Label(title, systemImage: symbol)
                .lineLimit(1)
                .minimumScaleFactor(0.8) // 「在 Telegram 里回复」 in the column
                .frame(maxWidth: column ? .infinity : nil)
        }
        .themedButton(prominent: prominent)
        .help(help)
        if iconsOnly { button.labelStyle(.iconOnly) } else { button }
    }
}

extension View {
    /// The item's actions (`MacInboxActions`) beside it when the detail has room for a column, over it otherwise.
    func macInboxActions(_ item: InboxItem, reading: Bool = false, draft: @escaping () -> Void, handOff: @escaping () -> Void) -> some View {
        modifier(MacInboxActionsHost(item: item, reading: reading, draft: draft, handOff: handOff))
    }
}

private struct MacInboxActionsHost: ViewModifier {
    var item: InboxItem
    /// A document is open beside the item: the actions are a row over it, the column's width is the document's.
    var reading: Bool
    var draft: () -> Void
    var handOff: () -> Void
    @State private var wide = true
    private var room: Bool { wide && !reading }

    /// Siblings, as the link panel and 会话信息 are: the item narrows rather than running under the column. One
    /// structure for both, so crossing the width keeps the item (its scroll position, what was pushed).
    func body(content: Content) -> some View {
        HStack(spacing: 0) {
            VStack(spacing: 0) {
                if !room { MacInboxActions(item: item, column: false, reading: reading, draft: draft, handOff: handOff) }
                content.frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            if room {
                Divider()
                MacInboxActions(item: item, column: true, draft: draft, handOff: handOff)
                    .frame(width: Theme.Size.macInboxActionsWidth)
            }
        }
        .onGeometryChange(for: Bool.self) {
            $0.size.width >= Theme.Size.macInboxActionsWidth + Theme.Size.macInboxActionsPageMin
        } action: { wide = $0 }
    }
}
