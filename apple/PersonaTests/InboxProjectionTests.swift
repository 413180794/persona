import Foundation
import Testing
@testable import Persona

@MainActor
struct InboxProjectionTests {
    private func item(_ id: String, kind: String = "dm", source: String = "lark", account: String = "feishu") -> InboxItem {
        InboxItem(id: id, source: source, kind: kind, account: account, accountName: account,
                  title: id, preview: "", count: 1)
    }

    /// The previous page's predicate, kept here to check aliases, localization and search without broadening them.
    private func legacyMatches(_ item: InboxItem, filter: InboxProjection.Filter) -> Bool {
        if !filter.source.isEmpty && item.account != filter.source { return false }
        if !filter.kind.isEmpty && !(item.kind == filter.kind || filter.kind == "group" && item.kind == "bot" || filter.kind == "mention" && (item.kind == "doc" || item.kind == "moment" && item.kindLabel == String(localized: "@Me"))) { return false }
        if filter.query.isEmpty { return true }
        return [item.title, item.sender ?? "", item.preview, item.summary ?? ""].contains {
            $0.localizedCaseInsensitiveContains(filter.query)
        }
    }

    @Test func storeRevisionReusesUnchangedProjectionAndInvalidatesEveryMutation() {
        let memo = InboxProjectionMemo()
        let original = item("same-id")
        let first = memo([original], filter: .init(), revision: 0)
        #expect(memo([original], filter: .init(), revision: 0) === first)
        var changed = original
        changed.preview = "fresh payload with unchanged id"
        let second = memo([changed], filter: .init(), revision: 1)
        #expect(second !== first)
        #expect(second.openItems.first?.preview == changed.preview)
        changed.doneAt = Date(timeIntervalSince1970: 10)
        let third = memo([changed], filter: .init(), revision: 2)
        #expect(third.openItems.isEmpty)
        #expect(third.doneItems == [changed])
        #expect(memo([original], filter: .init(), revision: 3).openItems == [original])
    }

    @Test func filterChangesStillInvalidateAnUnchangedStoreRevision() {
        let memo = InboxProjectionMemo()
        let items = [item("first"), item("second")]
        let all = memo(items, filter: .init(), revision: 4)
        let filtered = memo(items, filter: .init(query: "second"), revision: 4)
        #expect(filtered !== all)
        #expect(filtered.openItems.map(\.id) == ["second"])
    }

    @Test func preservesOrderAndCurrentAndLegacyTierRules() {
        var fyi = item("fyi")
        fyi.tier = "fyi"
        var working = item("working")
        working.status = "working"
        working.tier = "noise"
        var oldWorking = item("old-working")
        oldWorking.tier = "working" // An older hub's tier alone never means In Progress.
        var noise = item("noise")
        noise.status = "unknown"
        noise.tier = "noise"
        var done = item("done")
        done.doneAt = Date(timeIntervalSince1970: 20)
        let action = item("action")
        let projection = InboxProjection(items: [fyi, working, oldWorking, done, action, noise], filter: .init())
        #expect(projection.openItems.map(\.id) == ["fyi", "working", "old-working", "action", "noise"])
        #expect(projection.byTier[.action]?.map(\.id) == ["old-working", "action"])
        #expect(projection.byTier[.working] == [working])
        #expect(projection.byTier[.fyi] == [fyi])
        #expect(projection.byTier[.noise] == [noise])
        #expect(projection.doneItems == [done])
    }

    @Test func everyFilterCombinationMatchesThePreviousPage() {
        var items = ["dm", "bot", "group", "mention", "doc", "moment", "agent", "unknown"].map { item($0, kind: $0) }
        items.append(item("mail", kind: "mail", source: "mail", account: "mailbox-a"))
        var docComment = item("doc-comment", kind: "doc")
        docComment.doc = .init(url: "https://example.com/doc", type: "docx", notice: "comment")
        items.append(docComment)
        var atMe = item("moment-at-me", kind: "moment")
        atMe.moment = .init(postID: "post-a", notice: "atInComment")
        items.append(atMe)
        var reply = atMe
        reply.id = "moment-reply"
        reply.moment?.notice = "reply"
        items.append(reply)
        var readMention = item("read-mention", kind: "mention")
        readMention.read = true
        items.append(readMention)
        // A connector's label takes precedence even on a Moments item: retain the page's label-based alias.
        var connector = item("connector", kind: "moment", source: "example-connector", account: "connector-a")
        connector.connector = .init(thread: "thread-a", label: String(localized: "@Me"))
        items.append(connector)
        for index in items.indices {
            items[index].title += " Résumé TITLE"
            items[index].sender = index.isMultiple(of: 2) ? "sender" : nil
            items[index].preview = "preview 发送"
            items[index].summary = index.isMultiple(of: 3) ? "summary PLAN" : nil
            items[index].tier = ["action", "fyi", "noise", "unknown"][index % 4]
            if index.isMultiple(of: 2) { items[index].doneAt = Date(timeIntervalSince1970: Double(index)) }
        }
        for source in ["", "feishu", "mailbox-a", "connector-a", "missing"] {
            for kind in ["", "dm", "bot", "group", "mention", "doc", "moment", "mail", "agent", "unknown"] {
                for query in ["", "title", "résumé", "resume", "SENDER", "preview", "pLaN", "发送", " ", " preview ", "missing"] {
                    let filter = InboxProjection.Filter(source: source, kind: kind, query: query)
                    let matching = items.filter { legacyMatches($0, filter: filter) }
                    let open = matching.filter { !$0.isDone }
                    let projection = InboxProjection(items: items, filter: filter)
                    #expect(projection.openItems == open)
                    #expect(projection.byTier == Dictionary(grouping: open, by: \.triage))
                    #expect(projection.doneItems == matching.filter(\.isDone))
                    #expect(projection.sortedDoneItems == matching.filter(\.isDone).sorted {
                        ($0.doneAt ?? .distantPast) > ($1.doneAt ?? .distantPast)
                    })
                }
            }
        }
    }

    @Test func searchesAllFourFieldsButDoesNotTrimOrSearchTranslations() {
        var title = item("title")
        title.title = "Needle"
        var sender = item("sender")
        sender.sender = "Needle"
        var preview = item("preview")
        preview.preview = "Needle"
        var summary = item("summary")
        summary.summary = "Needle"
        var translated = item("translated", kind: "mail", source: "mail", account: "mailbox-a")
        translated.translations = ["en": .init(subject: "Needle", preview: "Needle")]
        let items = [title, sender, preview, summary, translated]
        let memo = InboxProjectionMemo()
        #expect(memo(items, filter: .init(query: "nEeDlE")).openItems.map(\.id) == ["title", "sender", "preview", "summary"])
        #expect(memo(items, filter: .init(query: " Needle ")).openItems.isEmpty)
        #expect(memo(items, filter: .init(query: "")).openItems == items)
    }

    @Test func connectorSourcesIncludeArchivedAndFilteredOutItemsInFirstOccurrenceOrder() {
        var archived = item("archived", source: "example-connector", account: "connector-b")
        archived.accountName = "First name"
        archived.connector = .init(thread: "thread-a")
        archived.doneAt = Date(timeIntervalSince1970: 20)
        var duplicate = archived
        duplicate.id = "duplicate"
        duplicate.accountName = "Other name"
        duplicate.doneAt = nil
        var other = item("other", source: "example-connector", account: "connector-a")
        other.connector = .init(thread: "thread-b")
        let items = [item("plain"), archived, duplicate, other]
        let projection = InboxProjection(items: items, filter: .init(source: "feishu", query: "no match"))
        #expect(projection.openItems.isEmpty && projection.doneItems.isEmpty)
        #expect(projection.connectorSources == [
            .init(id: "connector-b", name: "First name"), .init(id: "connector-a", name: "connector-a")
        ])
    }

    @Test func unchangedSnapshotsReuseTheProjectionAcrossUnrelatedRedraws() {
        let items = [item("a"), item("b")]
        let memo = InboxProjectionMemo()
        let filter = InboxProjection.Filter()
        let first = memo(items, filter: filter)
        for _ in 0..<10 { #expect(memo(items, filter: filter) === first) }
        // A freshly decoded but equal list also reuses the derived data; native storage identity is not the only key.
        #expect(memo(items.map { $0 }, filter: filter) === first)
        #expect(InboxProjectionMemo()(items, filter: filter) !== first)
    }

    @Test func eachFilterAndLocalizationKeyInvalidatesSynchronously() {
        let items = [item("a"), item("b", kind: "mail", source: "mail", account: "mailbox-a")]
        let memo = InboxProjectionMemo()
        let initial = InboxProjection.Filter()
        let first = memo(items, filter: initial)
        for filter in [
            InboxProjection.Filter(source: "mailbox-a"), .init(kind: "mail"), .init(query: "b"),
            .init(localeIdentifier: initial.localeIdentifier + "-test"), .init(mentionLabel: "different localized label")
        ] {
            let next = memo(items, filter: filter)
            #expect(next !== first)
            #expect(next.openItems == items.filter { filter.matches($0) })
            #expect(memo(items, filter: filter) === next)
            _ = memo(items, filter: initial)
        }
    }

    @Test func sameIDAndVersionNeverHideFreshRowPayloads() {
        var original = item("a")
        original.version = "v1"
        let memo = InboxProjectionMemo()
        let edits: [(inout InboxItem) -> Void] = [
            { $0.title = "New title" }, { $0.sender = "New sender" }, { $0.preview = "New preview" },
            { $0.summary = "New summary" }, { $0.count = 3 }, { $0.status = "working" }, { $0.tier = "noise" },
            { $0.updatedAt = Date(timeIntervalSince1970: 50) }, { $0.read = true },
            { $0.translations = ["en": .init(subject: "New subject", preview: "New translation")] },
            { $0.agent = .init(state: "drafted", draft: "New draft") },
            { $0.doc = .init(url: "https://example.com/doc", type: "base", notice: "mention",
                            rows: [.init(ref: "row-a", title: "New row")]) },
            { $0.connector = .init(thread: "thread-a", label: "New label") },
            { $0.accountName = "New account name" }, { $0.doneAt = Date(timeIntervalSince1970: 60) }
        ]
        for edit in edits {
            let before = memo([original], filter: .init())
            var changed = original
            edit(&changed)
            let fresh = memo([changed], filter: .init())
            #expect(fresh !== before)
            #expect(fresh.openItems + fresh.doneItems == [changed])
            #expect(changed.id == original.id && changed.version == original.version)
        }
    }

    @Test func freshSearchMatchesAndConnectorNamesAreVisibleImmediately() {
        var connector = item("connector", source: "example-connector", account: "connector-a")
        connector.version = "v1"
        connector.connector = .init(thread: "thread-a")
        let memo = InboxProjectionMemo()
        let filter = InboxProjection.Filter(query: "Needle")
        #expect(memo([connector], filter: filter).openItems.isEmpty)
        connector.preview = "Needle"
        connector.accountName = "Updated name"
        let fresh = memo([connector], filter: filter)
        #expect(fresh.openItems == [connector])
        #expect(fresh.connectorSources == [.init(id: "connector-a", name: "Updated name")])
        connector.preview = ""
        #expect(memo([connector], filter: filter).openItems.isEmpty)
        connector.connector = nil
        #expect(memo([connector], filter: filter).connectorSources.isEmpty)
    }

    @Test func filingRollbackAndDoneTimestampUpdatesAppearOnTheFirstLookup() {
        let open = item("open")
        var done = item("done")
        done.doneAt = Date(timeIntervalSince1970: 30)
        let memo = InboxProjectionMemo()
        let before = memo([open, done], filter: .init())
        #expect(before.sortedDoneItems == [done])
        var filed = open
        filed.doneAt = Date(timeIntervalSince1970: 40)
        let optimistic = memo([filed, done], filter: .init())
        #expect(optimistic.openItems.isEmpty)
        #expect(optimistic.sortedDoneItems == [filed, done])
        done.doneAt = Date(timeIntervalSince1970: 50)
        #expect(memo([filed, done], filter: .init()).sortedDoneItems == [done, filed])
        let rollback = memo([open, done], filter: .init())
        #expect(rollback.openItems == [open] && rollback.sortedDoneItems == [done])
    }

    @Test func insertionsRemovalReorderingAndForgetAppearImmediately() {
        let a = item("a"), b = item("b"), c = item("c")
        let memo = InboxProjectionMemo()
        #expect(memo([a, b], filter: .init()).openItems == [a, b])
        #expect(memo([b, a], filter: .init()).openItems == [b, a])
        #expect(memo([c, b, a], filter: .init()).openItems == [c, b, a])
        #expect(memo([a], filter: .init()).openItems == [a])
        let empty = memo([], filter: .init())
        #expect(empty.openItems.isEmpty && empty.byTier.isEmpty && empty.doneItems.isEmpty && empty.connectorSources.isEmpty)
    }

    @Test func doneSortingPreservesEqualTimestampOrderOnTheCachedProjection() {
        var a = item("a"), b = item("b"), c = item("c")
        a.doneAt = Date(timeIntervalSince1970: 20)
        b.doneAt = Date(timeIntervalSince1970: 30)
        c.doneAt = a.doneAt
        let items = [c, b, a]
        let memo = InboxProjectionMemo()
        let projection = memo(items, filter: .init())
        #expect(projection.doneItems == items)
        #expect(projection.sortedDoneItems == [b, c, a])
        let again = memo(items, filter: .init())
        #expect(again === projection)
        #expect(again.sortedDoneItems == [b, c, a])
    }
}
