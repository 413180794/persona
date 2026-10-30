import MarkdownUI
import SwiftUI

/// What Jarvis knows (the hub's ~/.jarvis/data, docs/knowledge.md): projects first, then chats, people, docs, meetings and
/// events, most important and most relevant first. Not a browser of Feishu: every row is Jarvis' own digest, and the
/// way out is handing it to Jarvis or an Agent.
///
/// Layout (2026-10-31 redo): a row of kind chips instead of a six-way segmented control, a tag menu instead of a second
/// chip row, projects grouped into 重点 / 进行中 / 已完成 / 不是项目, and search hits that open their entry.
struct KnowledgeView: View {
    @Environment(AppModel.self) private var model
    @State private var kind: KBKind = .projects
    @State private var tag: String?
    @State private var lists: [KBKind: KBList] = [:]
    @State private var error: String?
    @State private var query = ""
    @State private var hits: [KBHit]?
    /// Entries by "kind/id", from every list loaded (search hits only carry a path; this gives them a name).
    @State private var index: [String: KBEntry] = [:]
    @State private var showIgnored = false
    #if DEBUG
    @State private var debugEntry: KBEntry?
    #endif

    private var list: KBList? { lists[kind] }

    var body: some View {
        List {
            // Search covers every kind: the chips (one of them lit) would say otherwise.
            if query.isEmpty {
                Section {
                    KBKindBar(kind: $kind, counts: lists.mapValues { $0.items.count })
                        .listRowBackground(Color.clear)
                        .listRowInsets(EdgeInsets(top: 2, leading: 0, bottom: 2, trailing: 0))
                }
                .listSectionSpacing(.compact)
            }
            if !query.isEmpty {
                searchResults
            } else if let list {
                content(list)
            } else if let error {
                Section { Label(error, systemImage: "exclamationmark.triangle").foregroundStyle(Theme.danger).font(.footnote) }
            } else {
                Section { ProgressView().frame(maxWidth: .infinity).padding(.vertical, 24) }.listRowBackground(Color.clear)
            }
        }
        .listStyle(.insetGrouped)
        .themedPage()
        .navigationTitle("知识库")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) { tagMenu }
        }
        .searchable(text: $query, prompt: "搜索档案和聊天记录")
        .task(id: query) {
            guard !query.isEmpty, let api = model.api else { hits = nil; return }
            try? await Task.sleep(for: .milliseconds(300)) // typing
            guard !Task.isCancelled else { return }
            let found = (try? await api.kbSearch(query)) ?? []
            // Names for the hits: the lists of the kinds they are in, once each.
            for k in Set(found.compactMap { $0.entry?.kind }) where !index.keys.contains(where: { $0.hasPrefix(k.rawValue + "/") }) {
                if let l = try? await api.kbList(k) { remember(l.items) }
            }
            guard !Task.isCancelled else { return }
            hits = found
        }
        .task(id: "\(kind.rawValue)|\(tag ?? "")") { await load() }
        .refreshable { await load() }
        .navigationDestination(for: KBEntry.self) { e in KnowledgeDetailView(entry: e) }
        #if DEBUG
        // `-showKnowledgeEntry <kind>/<id>` (with `-openAssistantPage knowledge`): that entry's page, for screenshots;
        // `-showKnowledgeSearch <text>`: searching for it.
        .navigationDestination(item: $debugEntry) { e in KnowledgeDetailView(entry: e) }
        .onAppear {
            if let q = UserDefaults.standard.string(forKey: "showKnowledgeSearch") { query = q }
            if let ref = UserDefaults.standard.string(forKey: "showKnowledgeEntry"), let slash = ref.firstIndex(of: "/") {
                let id = String(ref[ref.index(after: slash)...])
                debugEntry = KBEntry(kind: String(ref[..<slash]), id: id, name: id, relevance: 0)
            }
        }
        #endif
    }

    // MARK: Lists

    @ViewBuilder private func content(_ list: KBList) -> some View {
        if list.items.isEmpty {
            Section {
                EmptyStateView(symbol: kind.symbol, title: tag == nil ? "还没有\(kind.title)" : "没有带 #\(tag!) 的\(kind.title)",
                               message: "hub 会把飞书和邮件里的会话归档、压缩，自动整理出项目、人、文档和会议，并按重要级和相关性排好。")
            }
            .listRowBackground(Color.clear)
        } else if kind == .projects {
            let active = list.items.filter { !$0.projectDone && !$0.projectIgnored }
            let focus = active.filter { $0.importance == "P0" || $0.importance == "P1" }
            let rest = active.filter { $0.importance != "P0" && $0.importance != "P1" }
            let done = list.items.filter(\.projectDone)
            let ignored = list.items.filter(\.projectIgnored)
            if !focus.isEmpty { Section { rows(focus) } header: { header("重点", focus.count) } }
            if !rest.isEmpty { Section { rows(rest) } header: { header(focus.isEmpty ? "进行中" : "其他进行中", rest.count) } }
            if !done.isEmpty { Section { rows(done) } header: { header("已完成", done.count) } }
            if !ignored.isEmpty {
                Section {
                    Button { withAnimation(.snappy) { showIgnored.toggle() } } label: {
                        HStack {
                            Text("不是项目").foregroundStyle(.secondary)
                            Text("\(ignored.count)").foregroundStyle(.tertiary).monospacedDigit()
                            Spacer()
                            Image(systemName: "chevron.right").font(.footnote.weight(.semibold)).foregroundStyle(.tertiary)
                                .rotationEffect(.degrees(showIgnored ? 90 : 0))
                        }
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    if showIgnored { rows(ignored) }
                } footer: {
                    Text("只在通知、营销邮件、机器人会话里出现的，不会再更新。打标里改回「进行中」就恢复。")
                }
            }
        } else {
            Section { rows(list.items) } header: { header(tag.map { "#" + $0 } ?? "按重要级和相关性", list.items.count) }
        }
    }

    private func rows(_ items: [KBEntry]) -> some View {
        ForEach(items) { e in
            NavigationLink(value: e) { KBRow(entry: e) }
        }
    }

    private func header(_ title: String, _ count: Int) -> some View {
        HStack(spacing: 6) {
            Text(title)
            Text("\(count)").foregroundStyle(Color.secondary.opacity(0.7)).monospacedDigit()
        }
    }

    // MARK: Tags

    private var tagMenu: some View {
        Menu {
            Button { tag = nil } label: {
                if tag == nil { Label("全部", systemImage: "checkmark") } else { Text("全部") }
            }
            Section("常用标签") {
                ForEach(list?.tags ?? [], id: \.self) { t in
                    Button { Haptics.tap(); tag = t } label: {
                        if tag == t { Label("#" + t, systemImage: "checkmark") } else { Text("#" + t) }
                    }
                }
            }
        } label: {
            Image(systemName: tag == nil ? "line.3.horizontal.decrease.circle" : "line.3.horizontal.decrease.circle.fill")
        }
        .accessibilityLabel(tag.map { "按标签筛选，当前 #\($0)" } ?? "按标签筛选")
    }

    // MARK: Search

    @ViewBuilder private var searchResults: some View {
        if let hits {
            if hits.isEmpty {
                Section {
                    ContentUnavailableView.search(text: query).listRowBackground(Color.clear)
                }
            } else {
                let groups = KBSearchGroup.make(hits)
                ForEach(KBKind.allCases) { k in
                    let these = groups.filter { $0.kind == k }
                    if !these.isEmpty {
                        Section { ForEach(these) { g in groupRow(g) } } header: { header(k.title, these.count) }
                    }
                }
            }
        } else {
            Section { ProgressView().frame(maxWidth: .infinity).padding(.vertical, 24) }.listRowBackground(Color.clear)
        }
    }

    private func groupRow(_ g: KBSearchGroup) -> some View {
        let known = index[g.key]
        let entry = known ?? KBEntry(kind: g.kind.rawValue, id: g.id, name: g.id, relevance: 0)
        return NavigationLink(value: entry) {
            KBHitRow(entry: entry, named: known != nil, group: g, query: query)
        }
    }

    private func remember(_ items: [KBEntry]) {
        for e in items { index[e.kind + "/" + e.id] = e }
    }

    private func load() async {
        guard let api = model.api else { return }
        do {
            let l = try await api.kbList(kind, tag: tag)
            lists[kind] = l
            remember(l.items)
            error = nil
        } catch {
            self.error = error.shown
        }
    }
}
/// The six kinds as chips with their icon (and how many, once loaded): fits a phone without squeezing six segments.
struct KBKindBar: View {
    @Binding var kind: KBKind
    var counts: [KBKind: Int]

    /// The bar's own scroll position (not ScrollViewReader: its scrollTo drags the list above along, CLAUDE.md).
    @State private var shown: KBKind?

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    ForEach(KBKind.allCases) { k in
                        let on = k == kind
                        Button {
                            Haptics.tap()
                            withAnimation(.snappy) { kind = k }
                        } label: {
                            HStack(spacing: 5) {
                                Image(systemName: k.symbol).font(.footnote.weight(.semibold))
                                Text(k.title).lineLimit(1)
                                if let n = counts[k], on { Text("\(n)").monospacedDigit().opacity(0.75) }
                            }
                            .font(.subheadline.weight(on ? .semibold : .medium))
                            .padding(.horizontal, 12)
                            .padding(.vertical, 7)
                            .foregroundStyle(on ? Color.white : Color.primary)
                            .background(on ? Theme.accent : Color.primary.opacity(0.06), in: .capsule)
                            .contentShape(.capsule)
                        }
                        .buttonStyle(.plain)
                        .id(k)
                        .accessibilityAddTraits(on ? .isSelected : [])
                    }
                }
                .scrollTargetLayout()
                .padding(.horizontal, 2)
        }
        .scrollPosition(id: $shown, anchor: .center)
        .onAppear { shown = kind }
        .onChange(of: kind) { _, k in withAnimation(.snappy) { shown = k } }
    }
}

/// One ranked entry: a kind tile carrying the importance, the name, its one-line state, then tags and when it changed.
struct KBRow: View {
    var entry: KBEntry

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            KBAvatar(entry: entry)
            VStack(alignment: .leading, spacing: 4) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(entry.name).font(.body.weight(.semibold)).lineLimit(1)
                    Spacer(minLength: 4)
                    if let d = entry.updated, d.realDate != nil {
                        Text(Fmt.relative(d)).font(.caption).foregroundStyle(.tertiary).lineLimit(1).fixedSize()
                    }
                }
                if let line = entry.oneLine, !line.isEmpty {
                    Text(line).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                }
                // As many tags as fit whole (a cut "#Manage…" says nothing).
                ViewThatFits(in: .horizontal) {
                    badges(tags: 2)
                    badges(tags: 1)
                    badges(tags: 0)
                }
                .padding(.top, 1)
            }
        }
        .padding(.vertical, 3)
        .accessibilityElement(children: .combine)
    }

    private func badges(tags: Int) -> some View {
        HStack(spacing: 6) {
            ImportanceBadge(importance: entry.importance)
            if let k = entry.projectKind { KBPill(text: k, tint: k == "长期" ? Theme.Hue.blue : nil) }
            if entry.relevance > 0 { KBPill(text: "相关 \(entry.relevance)", tint: RelevanceMeter.tint(entry.relevance)) }
            ForEach((entry.tags ?? []).prefix(tags), id: \.self) { t in
                Text("#" + t).font(.caption).foregroundStyle(.secondary).lineLimit(1).fixedSize()
            }
        }
    }
}

/// A rounded tile: the kind's symbol, or a person's / chat's first character; tinted by importance.
struct KBAvatar: View {
    var entry: KBEntry
    var size: CGFloat = 36

    var body: some View {
        let tint = ImportanceBadge.color(entry.importance ?? "")
        let kind = KBKind(rawValue: entry.kind)
        ZStack {
            RoundedRectangle(cornerRadius: size * 0.28, style: .continuous).fill(tint.opacity(0.14))
            if kind == .people || kind == .chats, let c = entry.name.first(where: { !$0.isWhitespace && !"#@（(【[「『《〈\"'“‘".contains($0) }) {
                Text(String(c).uppercased()).font(.system(size: size * 0.42, weight: .semibold)).foregroundStyle(tint)
            } else {
                Image(systemName: kind?.symbol ?? "circle").font(.system(size: size * 0.4, weight: .semibold)).foregroundStyle(tint)
            }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

struct ImportanceBadge: View {
    var importance: String?

    var body: some View {
        let p = importance ?? ""
        if !p.isEmpty {
            KBPill(text: p, tint: Self.color(p), bold: true)
                .accessibilityLabel("重要级 \(p)")
        }
    }

    /// P0 red, P1 orange, P2 blue, P3 and unranked grey.
    static func color(_ p: String) -> Color {
        switch p {
        case "P0": Theme.needsYou
        case "P1": Theme.busy
        case "P2": Theme.Hue.blue
        default: Color.secondary
        }
    }
}

/// A small capsule: tinted text on a soft tint of the same color, or neutral.
struct KBPill: View {
    var text: String
    var tint: Color?
    var bold = false

    var body: some View {
        Text(text)
            .font(.caption.weight(bold ? .bold : .medium).monospacedDigit())
            .lineLimit(1)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .foregroundStyle(tint ?? Color.secondary)
            .background((tint ?? Color.primary).opacity(tint == nil ? 0.06 : 0.13), in: .capsule)
            .fixedSize()
    }
}

/// Relevance to the user (0–100): the color its pill takes.
enum RelevanceMeter {
    static func tint(_ value: Int) -> Color? {
        value >= 70 ? Theme.needsYou.opacity(0.9) : value >= 40 ? Theme.busy : nil
    }
}

/// The hits of one entry: in the order found, with the files they are in.
struct KBSearchGroup: Identifiable {
    var kind: KBKind
    var id: String
    var hits: [KBHit]
    var key: String { kind.rawValue + "/" + id }

    static func make(_ hits: [KBHit]) -> [KBSearchGroup] {
        var order: [String] = []
        var by: [String: KBSearchGroup] = [:]
        for h in hits {
            guard let ref = h.entry else { continue }
            let key = ref.kind.rawValue + "/" + ref.id
            if by[key] == nil { order.append(key); by[key] = KBSearchGroup(kind: ref.kind, id: ref.id, hits: []) }
            if !by[key]!.hits.contains(where: { $0.text == h.text }) { by[key]!.hits.append(h) }
        }
        // 档案 and 时间线 say more than a tag in 资料: those lines first.
        return order.compactMap { by[$0] }.map { g in
            var g = g
            g.hits = g.hits.filter { $0.source != "资料" } + g.hits.filter { $0.source == "资料" }
            return g
        }
    }
}

/// One entry found by a search: its tile and name, then up to two matching lines with what was searched for in bold.
struct KBHitRow: View {
    var entry: KBEntry
    var named: Bool
    var group: KBSearchGroup
    var query: String

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            KBAvatar(entry: entry, size: 30)
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 6) {
                    Text(named ? entry.name : group.kind.title).font(.subheadline.weight(.semibold)).lineLimit(1)
                    if group.hits.count > 2 {
                        Text("\(group.hits.count) 处").font(.caption).foregroundStyle(.tertiary).lineLimit(1)
                    }
                }
                ForEach(group.hits.prefix(2), id: \.self) { h in
                    VStack(alignment: .leading, spacing: 1) {
                        Text(h.source).font(.caption2).foregroundStyle(.tertiary).lineLimit(1)
                        Text(Self.marked(h.text, query)).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                    }
                }
            }
        }
        .padding(.vertical, 2)
    }

    static func marked(_ text: String, _ query: String) -> AttributedString {
        var a = AttributedString(text)
        let q = query.trimmingCharacters(in: .whitespaces)
        guard !q.isEmpty else { return a }
        var from = a.startIndex
        while let r = a[from...].range(of: q, options: .caseInsensitive) {
            a[r].foregroundColor = .primary
            a[r].inlinePresentationIntent = .stronglyEmphasized
            from = r.upperBound
        }
        return a
    }
}

struct KnowledgeDetailView: View {
    @Environment(AppModel.self) private var model
    var entry: KBEntry
    @State private var detail: KBDetailResponse?
    @State private var error: String?
    @State private var editing = false
    @State private var newAgent = false
    @State private var showRecent = false
    @State private var allLog = false
    @State private var openKinds: Set<String> = []

    /// `-showKnowledgeLower YES`: the header and Agents left out, so a screenshot reaches the timeline and what follows.
    #if DEBUG
    static var debugLower: Bool { UserDefaults.standard.bool(forKey: "showKnowledgeLower") }
    #else
    static let debugLower = false
    #endif

    /// Timeline lines shown before 显示全部.
    static let logShown = 6
    /// Related entries of one kind shown before 全部.
    static let linksShown = 4

    var body: some View {
        List {
            if !Self.debugLower {
                Section { header }
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets(top: 4, leading: 4, bottom: 8, trailing: 4))
            }
            if let d = detail?.detail {
                if !Self.debugLower, let agents = detail?.agents, !agents.isEmpty {
                    Section {
                        ForEach(agents) { s in
                            Button { model.open(s.key) } label: {
                                SessionRow(session: s, machine: model.machine(s.machineID))
                            }
                            .buttonStyle(.plain)
                        }
                    } header: { sectionHeader("在做这个项目的 Agent", agents.count) }
                }
                if let log = d.log, !log.isEmpty { timeline(log) }
                if let sum = d.summary, !sum.isEmpty {
                    Section("档案") {
                        Markdown(Links.markdown(Self.profile(sum, oneLine: shown.oneLine))).markdownTheme(.jarvis).textSelection(.enabled)
                            .padding(.vertical, 6)
                    }
                }
                links(d)
                if let recent = d.recent, !recent.isEmpty {
                    Section {
                        DisclosureGroup(isExpanded: $showRecent) {
                            Text(recent).font(.footnote.monospaced()).textSelection(.enabled)
                        } label: {
                            Label("最近 7 天的消息", systemImage: "text.bubble")
                        }
                    }
                }
                Section {
                    Text(d.path).font(.caption.monospaced()).foregroundStyle(.secondary).textSelection(.enabled)
                } header: {
                    Text("目录")
                } footer: {
                    Text("Jarvis、AI 分身和各台电脑上的 Agent 都能直接读这个目录。")
                }
            } else if let error {
                Section { Label(error, systemImage: "exclamationmark.triangle").foregroundStyle(Theme.danger).font(.footnote) }
            } else {
                Section { ProgressView().frame(maxWidth: .infinity).padding(.vertical, 24) }.listRowBackground(Color.clear)
            }
        }
        .listStyle(.insetGrouped)
        .themedPage()
        .navigationTitle(KBKind(rawValue: entry.kind)?.title ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button { editing = true } label: { Label("打标", systemImage: "tag") }
                    .accessibilityLabel("打标")
            }
        }
        .sheet(isPresented: $editing) {
            KBTagEditor(entry: shown) { await load() }
        }
        .sheet(isPresented: $newAgent) {
            NewChatView(initialPrompt: agentPrompt) { s in
                newAgent = false
                model.open(s.key)
            }
        }
        .task { await load() }
        .refreshable { await load() }
    }

    // MARK: Header

    private var header: some View {
        let e = shown
        return VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .center, spacing: 12) {
                KBAvatar(entry: e, size: 48)
                VStack(alignment: .leading, spacing: 4) {
                    Text(e.name).font(.title2.weight(.bold)).lineLimit(3)
                    HStack(spacing: 6) {
                        ImportanceBadge(importance: e.importance)
                        if let k = e.projectKind { KBPill(text: k + "项目", tint: k == "长期" ? Theme.Hue.blue : nil) }
                        if let st = projectStatus { KBPill(text: st) }
                        if e.relevance > 0 { KBPill(text: "相关 \(e.relevance)", tint: RelevanceMeter.tint(e.relevance)) }
                    }
                }
            }
            if let line = e.oneLine, !line.isEmpty {
                Text(line).font(.body).foregroundStyle(.primary.opacity(0.85)).fixedSize(horizontal: false, vertical: true)
            }
            if let tags = e.tags, !tags.isEmpty {
                LarkFlowLayout(spacing: 6, lineSpacing: 6) {
                    ForEach(tags, id: \.self) { t in KBPill(text: "#" + t) }
                }
            }
            HStack(spacing: 10) {
                Button { Task { await ask() } } label: {
                    // White on the prominent fill: the symbol took the tint and vanished into the blue.
                    Label("问 Jarvis", systemImage: "sparkles").lineLimit(1).frame(maxWidth: .infinity)
                        .foregroundStyle(.white)
                }
                .themedButton(prominent: true)
                if entry.kind == KBKind.projects.rawValue {
                    Button { newAgent = true } label: {
                        Label("开 Agent", systemImage: "plus.bubble").lineLimit(1).frame(maxWidth: .infinity)
                    }
                    .themedButton()
                }
            }
            .controlSize(.large)
            .padding(.top, 2)
        }
    }

    /// A project's status when it is not the usual 进行中.
    private var projectStatus: String? {
        guard entry.kind == KBKind.projects.rawValue, let st = detail?.detail.meta?["status"]?.string ?? entry.extra?.components(separatedBy: " · ").last else { return nil }
        switch st {
        case "paused": return "暂停"
        case "done": return "已完成"
        case "ignored": return "不是项目"
        default: return nil
        }
    }

    private func sectionHeader(_ title: String, _ count: Int) -> some View {
        HStack(spacing: 6) {
            Text(title)
            Text("\(count)").foregroundStyle(Color.secondary.opacity(0.7)).monospacedDigit()
        }
    }

    // MARK: Timeline

    /// Newest first, a dot and a rail down the left, when and from where above each line.
    @ViewBuilder private func timeline(_ log: [String]) -> some View {
        let lines = log.reversed().map(KBLogLine.init)
        let shownLines = allLog ? lines : Array(lines.prefix(Self.logShown))
        Section {
            ForEach(Array(shownLines.enumerated()), id: \.offset) { i, l in
                KBTimelineRow(line: l, first: i == 0, last: i == shownLines.count - 1 && (allLog || lines.count <= Self.logShown))
                    .listRowSeparator(.hidden)
                    .listRowInsets(EdgeInsets(top: 0, leading: 16, bottom: 0, trailing: 16))
            }
            if lines.count > Self.logShown {
                Button { withAnimation(.snappy) { allLog.toggle() } } label: {
                    Text(allLog ? "收起" : "显示全部 \(lines.count) 条").font(.subheadline.weight(.medium)).lineLimit(1)
                }
                .listRowSeparator(.hidden)
            }
        } header: { sectionHeader("时间线", lines.count) }
    }

    // MARK: Related

    /// Related entries by kind (projects, chats, people…), a few each with 全部 for the rest; then the code directories.
    @ViewBuilder private func links(_ d: KBDetail) -> some View {
        let links = d.links ?? []
        let dirs = d.meta?["dirs"]?.array?.compactMap(\.string) ?? []
        let byKind = Dictionary(grouping: links, by: \.kind)
        ForEach(KBKind.allCases) { k in
            if let group = byKind[k.rawValue], !group.isEmpty {
                let open = openKinds.contains(k.rawValue)
                Section {
                    ForEach(open ? group : Array(group.prefix(Self.linksShown))) { e in
                        NavigationLink(value: e) { KBLinkRow(entry: e) }
                    }
                    if group.count > Self.linksShown {
                        Button {
                            withAnimation(.snappy) {
                                if open { openKinds.remove(k.rawValue) } else { openKinds.insert(k.rawValue) }
                            }
                        } label: {
                            Text(open ? "收起" : "全部 \(group.count) 个").font(.subheadline.weight(.medium)).lineLimit(1)
                        }
                    }
                } header: { sectionHeader("关联的" + k.title, group.count) }
            }
        }
        if !dirs.isEmpty {
            Section {
                ForEach(dirs, id: \.self) { dir in
                    Label {
                        Text(dir).font(.subheadline.monospaced()).lineLimit(1).truncationMode(.head)
                    } icon: {
                        Image(systemName: "folder").foregroundStyle(.secondary)
                    }
                }
            } header: { sectionHeader("代码目录", dirs.count) }
        }
    }

    /// The profile without what the header already shows: its title, the one line, the tags line of a person's
    /// generated profile (「重要级 P0 · 与我相关性 95 · …」).
    static func profile(_ md: String, oneLine: String?) -> String {
        let one = oneLine?.trimmingCharacters(in: .whitespaces) ?? ""
        return dropTitle(md).components(separatedBy: "\n").filter { line in
            let t = line.trimmingCharacters(in: .whitespaces)
            return !(t.hasPrefix("重要级 ") && t.contains("相关性")) && (one.isEmpty || t != one)
        }
        .joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
    }

    static func dropTitle(_ md: String) -> String {
        guard md.hasPrefix("# ") else { return md }
        return md.split(separator: "\n", maxSplits: 1, omittingEmptySubsequences: false).dropFirst().joined(separator: "\n")
    }

    /// The entry as last loaded (a linked row or a search hit starts with only its name or id).
    private var shown: KBEntry {
        guard let m = detail?.detail.meta else { return entry }
        var e = entry
        if let n = m["name"]?.string ?? m["title"]?.string, !n.isEmpty { e.name = n }
        if let i = m["importance"]?.string { e.importance = i }
        if let r = m["relevance"]?.int { e.relevance = r }
        if let t = m["tags"]?.array { e.tags = t.compactMap(\.string) }
        if let l = m["one_line"]?.string ?? m["note"]?.string, !l.isEmpty { e.oneLine = l }
        if e.kind == KBKind.projects.rawValue, let k = m["kind"]?.string { e.extra = k + " · " + (m["status"]?.string ?? "active") }
        return e
    }

    private var context: ContextRef {
        let kindTitle = KBKind(rawValue: entry.kind)?.title ?? ""
        var text = detail?.detail.summary ?? entry.oneLine ?? ""
        if let log = detail?.detail.log, !log.isEmpty { text += "\n\n时间线（最近）：\n" + log.suffix(10).joined(separator: "\n") }
        if let path = detail?.detail.path { text += "\n\n知识库目录：\(path)" }
        return ContextRef(kind: "context", title: "\(kindTitle) · \(shown.name)", url: "", text: text)
    }

    private var agentPrompt: String {
        context.xml + "\n\n"
    }

    private func ask() async {
        await model.askJarvis(context.xml + "\n\n说说「\(shown.name)」现在怎么样了：最新进展、在等我的事、接下来该做什么，能替我处理的先处理。")
    }

    private func load() async {
        guard let api = model.api else { return }
        do {
            detail = try await api.kbDetail(entry.kind, id: entry.id)
            error = nil
        } catch {
            self.error = error.shown
        }
    }
}

/// One timeline line: a dot on a rail, when and from where in small type, then what happened.
struct KBTimelineRow: View {
    var line: KBLogLine
    var first: Bool
    var last: Bool
    static let rail = Color.primary.opacity(0.1)

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            ZStack(alignment: .top) {
                // The rail joins the dots: none above the newest, none below the oldest shown.
                VStack(spacing: 0) {
                    Rectangle().fill(first ? Color.clear : Self.rail).frame(width: 2, height: 14)
                    Rectangle().fill(last ? Color.clear : Self.rail).frame(width: 2).frame(maxHeight: .infinity)
                }
                Circle().fill(first ? Theme.accent : Color.secondary.opacity(0.45))
                    .frame(width: 9, height: 9)
                    .padding(.top, 10)
            }
            .frame(width: 12)
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    if !line.when.isEmpty { Text(line.shortWhen()).font(.caption.weight(.semibold).monospacedDigit()) }
                    if !line.source.isEmpty { Text(line.source).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
                }
                .foregroundStyle(first ? Theme.accent : .secondary)
                Text(line.text).font(.subheadline).fixedSize(horizontal: false, vertical: true)
            }
            .padding(.vertical, 8)
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .combine)
    }
}

/// A related entry: its tile, name and one line.
struct KBLinkRow: View {
    var entry: KBEntry

    var body: some View {
        HStack(spacing: 12) {
            KBAvatar(entry: entry, size: 30)
            VStack(alignment: .leading, spacing: 2) {
                Text(entry.name).font(.subheadline.weight(.medium)).lineLimit(1)
                if let l = entry.oneLine, !l.isEmpty { Text(l).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
            }
        }
    }
}
/// Importance, relevance, tags (and a project's kind / status) set by hand; the hub keeps them over the automatic ones.
struct KBTagEditor: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var entry: KBEntry
    var saved: () async -> Void
    @State private var importance = ""
    @State private var relevance = 50.0
    @State private var tags = ""
    @State private var projectKind = "short"
    @State private var status = "active"
    @State private var saving = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                Section("重要级") {
                    Picker("重要级", selection: $importance) {
                        ForEach(["P0", "P1", "P2", "P3"], id: \.self) { Text($0).tag($0) }
                    }
                    .pickerStyle(.segmented)
                }
                Section {
                    Slider(value: $relevance, in: 0...100, step: 5)
                } header: { Text("与我相关 \(Int(relevance))") }
                Section {
                    TextField("用空格分隔", text: $tags)
                } header: { Text("标签") }
                if entry.kind == KBKind.projects.rawValue {
                    Section("项目") {
                        Picker("类型", selection: $projectKind) {
                            Text("短期（需求）").tag("short")
                            Text("长期（使命）").tag("long")
                        }
                        Picker("状态", selection: $status) {
                            Text("进行中").tag("active")
                            Text("暂停").tag("paused")
                            Text("已完成").tag("done")
                            Text("不是项目").tag("ignored")
                        }
                    }
                }
                if let error {
                    Section { Label(error, systemImage: "exclamationmark.triangle").foregroundStyle(Theme.danger).font(.footnote) }
                }
            }
            .navigationTitle(entry.name)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("保存") { Task { await save() } }.disabled(saving)
                }
            }
            .onAppear {
                importance = entry.importance?.isEmpty == false ? entry.importance! : "P2"
                relevance = Double(entry.relevance)
                tags = (entry.tags ?? []).joined(separator: " ")
                if let extra = entry.extra, entry.kind == KBKind.projects.rawValue {
                    let parts = extra.components(separatedBy: " · ")
                    projectKind = parts.first ?? "short"
                    status = parts.count > 1 ? parts[1] : "active"
                }
            }
        }
        .presentationDetents([.medium, .large])
    }

    private func save() async {
        guard let api = model.api else { return }
        saving = true
        defer { saving = false }
        let isProject = entry.kind == KBKind.projects.rawValue
        let u = KBUpdate(importance: importance, relevance: Int(relevance),
                         tags: tags.split(whereSeparator: \.isWhitespace).map(String.init),
                         status: isProject ? status : nil, projectKind: isProject ? projectKind : nil)
        do {
            try await api.kbUpdate(entry.kind, id: entry.id, u)
            Haptics.success()
            await saved()
            dismiss()
        } catch {
            self.error = error.shown
            Haptics.error()
        }
    }
}

/// An Agent handed to Jarvis: who it is and where it runs, so Jarvis can read it, follow up, and file its result
/// under the right project (the knowledge base's project dirs).
enum AgentHandoff {
    static func prompt(_ s: Session, machine: String?) -> String {
        let title = s.title.isEmpty ? (s.cwd as NSString).lastPathComponent : s.title
        var text = "机器：\(machine ?? s.machineID)（\(s.machineID)）\n会话：\(s.sessionID)\n目录：\(s.cwd)\n状态：\(s.state.rawValue)"
        if let last = s.lastText, !last.isEmpty { text += "\n最后说的：\(String(last.prefix(800)))" }
        let ref = ContextRef(kind: "context", title: "Agent · \(title)", url: "", text: text)
        return ref.xml + "\n\n看一下这个 Agent（agent_read）：它在做什么、做到哪了、有没有在等我。它属于哪个项目就把进展记进项目（kb_update，没登记目录的顺便登记 dirs）；"
            + "需要我拍板的列出来，能替我推进的直接推进。"
    }
}
