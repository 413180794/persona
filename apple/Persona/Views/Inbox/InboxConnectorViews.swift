import SwiftUI

// An inbox item of one of the hub's connectors (GitHub…), drawn the same for every source from the item's
// `connector` block (docs/plan-connectors.md「六、App 侧」). No pages for browsing the source: the thread as the hub
// read it, and the page itself in the app's link sheet.

/// A connector's item opened: a chat app's conversation as its own page (ConnectorThreadView, a connector with
/// `threads`: Telegram); otherwise, or while that cannot be read, what the hub collected about it.
struct InboxConnectorDetailView: View {
    var item: InboxItem
    @State private var thread: ConnectorThreadModel

    init(item: InboxItem) {
        self.item = item
        _thread = State(initialValue: ConnectorThreadModel(connector: item.source, thread: item.connector?.thread ?? ""))
    }

    var body: some View {
        if thread.loaded && (thread.unavailable || thread.chat?.isChat == false) {
            InboxConnectorTextView(item: item, problem: thread.failure)
        } else {
            ConnectorThreadView(model: thread)
                .navigationTitle(thread.chat?.name ?? item.title)
                .navigationBarTitleDisplayMode(.inline)
        }
    }
}

/// What the hub collected about a connector's item (the description and the newest comments, read for triage) and
/// 在 GitHub 打开. The action bar above it drafts the reply, which goes where `reply_to` says.
struct InboxConnectorTextView: View {
    var item: InboxItem
    /// Why the conversation page could not be read (a rate limit), said over the text.
    var problem: String? = nil
    @Environment(AppModel.self) private var model
    @State private var context: String?
    @State private var failure: String?
    @State private var link: LinkTarget?

    private var connector: InboxItem.Connector? { item.connector }

    var body: some View {
        List {
            Section {
                VStack(alignment: .leading, spacing: Theme.Space.s) {
                    HStack(spacing: Theme.Space.sm) {
                        if let brand = ConnectorText.brand(item.source) {
                            BrandMark(brand, side: Theme.Size.iconS)
                        }
                        Text([item.accountName, connector?.reference ?? ""].filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                        if let label = item.kindLabel {
                            Chip(text: label, look: item.kind == "mention" ? .tint(Theme.danger) : .neutral)
                        }
                    }
                    Text(item.title).font(Theme.Font.title).textSelection(.enabled)
                }
                .padding(.vertical, Theme.Space.xs)
                if let url = connector?.url.flatMap(URL.init(string:)) {
                    Button { link = LinkTarget(url: url) } label: {
                        Label("在 \(item.accountName) 打开", systemImage: Theme.Icon.safari).lineLimit(1)
                    }
                }
            } footer: {
                if let to = connector?.replyTo, connector?.canReply == true {
                    Text("回复会" + to + "。")
                }
            }
            if let problem, !problem.isEmpty, item.source == "telegram" {
                Section {
                    Label(problem, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(.secondary)
                }
            }
            Section("内容") {
                if let context, !context.isEmpty {
                    Text(context).font(Theme.Font.secondary).textSelection(.enabled)
                } else if let failure {
                    Text(failure).font(Theme.Font.label).foregroundStyle(Theme.danger)
                } else {
                    ProgressView().frame(maxWidth: .infinity)
                }
            }
        }
        .themedPage()
        .navigationTitle(item.accountName)
        .navigationBarTitleDisplayMode(.inline)
        .task(id: item.id) { await load() }
        #if os(macOS)
        .macLinkPanel($link)
        #else
        .sheet(item: $link) { t in
            if let key = model.assistantMain ?? model.assistantCurrent { LinkSheet(target: t, key: key) }
        }
        #endif
    }

    private func load() async {
        do {
            context = try await model.api?.inboxItem(item.id).context ?? ""
            failure = nil
        } catch {
            failure = error.shown.map { "读不到内容：\($0)" }
        }
    }
}
