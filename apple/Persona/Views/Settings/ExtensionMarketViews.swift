import SwiftUI

/// 设置 › 扩展 › 扩展市场 (docs/extensions.md「扩展市场」): the plugins of every market the hub has, searched and filtered by
/// market; a row each with its name, version, what it does, its tags and how it stands on this hub. The markets
/// themselves are one tap away (市场).
struct ExtensionMarketView: View {
    @State private var page = ExtensionMarketModel()

    var body: some View {
        SettingsForm {
            // Laid out as 知识库's kind chips under its search field: a compact section whose row has no inset of its own,
            // so the chips sit right under the search field and line up with the cards below.
            if page.markets.count > 1 {
                #if os(macOS)
                marketChips
                #else
                Section { marketChips }
                    .listSectionSpacing(.compact)
                #endif
            }
            ForEach(page.markets.filter { $0.error?.isEmpty == false }) { m in
                Section {
                    Label { Text("「\(m.title)」读不到：\(m.error ?? "")") } icon: {
                        Image(systemName: Theme.Icon.error).foregroundStyle(Theme.caution)
                    }
                    .font(Theme.Font.label)
                }
            }
            if let items = page.items {
                if page.markets.isEmpty {
                    Section {
                        EmptyStateView(symbol: Theme.Icon.pluginMarket, title: "还没有扩展市场",
                                       message: "扩展市场是一个仓库或网站发布的扩展索引。添加一个市场后，这里列出它的扩展，可以直接安装。")
                        NavigationLink(value: SettingsPage.pluginMarkets) { Text("添加市场").lineLimit(1) }
                            .accessibilityIdentifier("market-add-first")
                    }
                    .listRowBackground(Color.clear)
                } else if items.isEmpty {
                    Section {
                        EmptyStateView(symbol: Theme.Icon.search, title: "没有找到扩展",
                                       message: page.query.isEmpty ? "这个市场里还没有扩展。" : "换个词试试，名字、说明、发布者和标签都能搜。")
                    }
                    .listRowBackground(Color.clear)
                } else {
                    Section {
                        ForEach(items) { p in
                            NavigationLink(value: SettingsPage.marketExtension(market: p.market, id: p.pluginID)) {
                                MarketExtensionRow(item: p, showsMarket: page.market == nil && page.markets.count > 1)
                            }
                            .accessibilityIdentifier("market-plugin-\(p.pluginID)")
                        }
                    } footer: {
                        if let p = page.platform, !p.isEmpty { Text("这台 hub 是 \(p)；装之前会列出扩展能做什么，你同意后才装。") }
                    }
                }
            }
            if let e = page.error {
                Section {
                    LoadProblem(text: e)
                    Button { Task { await page.load() } } label: { Text("重试").lineLimit(1) }
                }
            }
            #if os(macOS)
            // In the page, as the Mac's settings panes have their buttons (#1223): the window's toolbar put it over the
            // settings list's column.
            if !page.markets.isEmpty {
                Section {
                    NavigationLink(value: SettingsPage.pluginMarkets) {
                        Label { Text("管理市场").lineLimit(1) } icon: { Image(systemName: Theme.Icon.pluginMarket) }
                    }
                    .accessibilityIdentifier("market-manage")
                }
            }
            #endif
        }
        .themedPage()
        #if os(iOS) || os(visionOS)
        // The chips come right under the search field: the list's own top margin (a first section's header band) made a
        // gap several times the page's other spacing.
        .contentMargins(.top, Theme.Space.s, for: .scrollContent)
        #endif
        .overlay { if page.items == nil && page.error == nil { ProgressView() } }
        .detailTitle("扩展市场")
        .navigationBarTitleDisplayMode(.inline)
        .pageSearchable(text: $page.query, placement: .navigationBarDrawer(displayMode: .always), prompt: "名字、说明、发布者或标签")
        #if os(iOS) || os(visionOS)
        .pageToolbar {
            ToolbarItem(placement: .topBarTrailing) {
                NavigationLink(value: SettingsPage.pluginMarkets) { Text("市场").lineLimit(1) }
                    .accessibilityIdentifier("market-manage")
            }
        }
        #endif
        // The search waits for the typing to pause; the first read goes at once.
        .task(id: page.query) {
            if page.items != nil { try? await Task.sleep(for: .milliseconds(300)) }
            guard !Task.isCancelled else { return }
            await page.load()
        }
        .refreshable { await page.load(refresh: true) }
    }

    private var marketChips: some View {
        FlowLayoutRow {
            FilterChip(title: "全部", on: page.market == nil) { choose(nil) }
                .accessibilityIdentifier("market-filter-all")
            ForEach(page.markets) { m in
                FilterChip(title: m.title, on: page.market == m.id) { choose(m.id) }
                    .accessibilityIdentifier("market-filter-\(m.id)")
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .listRowBackground(Color.clear)
        .listRowInsets(EdgeInsets(top: Theme.Space.xxs, leading: 0, bottom: Theme.Space.xxs, trailing: 0))
    }

    private func choose(_ market: String?) {
        guard page.market != market else { return }
        page.market = market
        Task { await page.load() }
    }
}

/// A plugin of the catalog: name and version, what it does, its tags, how it stands here.
private struct MarketExtensionRow: View {
    var item: ExtensionCatalogItem
    var showsMarket: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            HStack(spacing: Theme.Space.s) {
                Text(item.name).font(Theme.Font.bodyStrong).lineLimit(1)
                Text(item.version).font(Theme.Font.label.monospacedDigit()).foregroundStyle(.secondary).lineLimit(1)
                Spacer(minLength: Theme.Space.s)
                MarketStateChip(item: item)
            }
            if let d = item.description, !d.isEmpty {
                Text(d).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(2)
            }
            let tags = (showsMarket ? [item.marketName ?? ""] : []) + (item.tags ?? [])
            if tags.contains(where: { !$0.isEmpty }) {
                FlowLayoutRow {
                    ForEach(Array(tags.filter { !$0.isEmpty }.enumerated()), id: \.offset) { i, t in
                        Chip(text: t, symbol: showsMarket && i == 0 ? Theme.Icon.pluginMarket : nil, look: showsMarket && i == 0 ? .outline : .neutral)
                    }
                }
            }
            if let p = item.problem, !p.isEmpty {
                Text(p).font(Theme.Font.label).foregroundStyle(Theme.danger).lineLimit(2)
            }
        }
        .padding(.vertical, Theme.Space.xxs)
    }
}

/// 可安装 / 可升级 / 已安装 / 装不了, in its tone.
private struct MarketStateChip: View {
    var item: ExtensionCatalogItem

    var body: some View {
        switch ExtensionMarketText.step(item) {
        case .install: Chip(text: "可安装", look: .tint(Theme.accent))
        case .upgrade: Chip(text: "可升级", look: .tint(Theme.caution))
        case .upToDate: Chip(text: "已安装", look: .tint(Theme.positive))
        case .unavailable: Chip(text: "装不了", look: .neutral)
        }
    }
}

/// 扩展市场 › one plugin: what it is, what it may do, and the one thing to do with it — install, upgrade (with what the
/// new version asks for in addition), or remove when it came from this market.
struct MarketExtensionDetailView: View {
    @Environment(AppModel.self) private var app
    @State private var page: MarketExtensionModel
    @State private var confirmRemove = false

    init(market: String, id: String) { _page = State(initialValue: MarketExtensionModel(market: market, id: id)) }

    var body: some View {
        SettingsForm {
            if let p = page.item {
                header(p)
                actions(p)
                if let perms = p.permissions, !perms.isEmpty {
                    Section {
                        ForEach(perms) { PermissionLine(permission: $0) }
                    } header: {
                        SettingsHeading("它可以")
                    } footer: {
                        Text("安装前会再列一次，以下载到的包为准；新版本要得更多时，升级前会单独列出多出来的。")
                    }
                }
                about(p)
            } else if let e = page.error {
                Section {
                    EmptyStateView(symbol: Theme.Icon.error, title: "读不到这个扩展", message: e,
                                   action: ("重试", { Task { await page.load() } }))
                }
                .listRowBackground(Color.clear)
            }
        }
        .themedPage()
        .overlay { if page.item == nil && page.error == nil { ProgressView() } }
        .detailTitle(page.item?.name ?? "扩展")
        .navigationBarTitleDisplayMode(.inline)
        .confirmationDialog("停用依赖它的扩展后再卸载？", isPresented: Binding(
            get: { page.uninstallConfirmation != nil }, set: { if !$0 { page.uninstallConfirmation = nil } }),
            titleVisibility: .visible) {
            Button(role: .destructive) {
                Task {
                    if await page.uninstall(cascade: true, members: page.removingMembers) {
                        app.toast = Toast(page.removalMessage, style: .success)
                    }
                }
            } label: { Text("停用依赖并卸载").lineLimit(1) }
            .accessibilityIdentifier("market-uninstall-cascade")
            Button("取消", role: .cancel) {}
        } message: {
            Text(page.uninstallConfirmation?.message ?? "")
        }
        .sheet(item: $page.preview) { pv in
            ExtensionInstallSheet(preview: pv, name: page.item?.name ?? pv.manifest.name ?? pv.manifest.id) {
                let upgrade = pv.upgrade == true
                if await page.confirm() {
                    Haptics.success()
                    app.toast = Toast(upgrade ? "已升级" : "已安装", style: .success)
                    return true
                }
                return false
            }
        }
        .task { await page.load() }
        .refreshable { await page.load() }
    }

    @ViewBuilder private func header(_ p: ExtensionCatalogItem) -> some View {
        Section {
            HStack(spacing: Theme.Space.m) {
                Image(systemName: Theme.Icon.plugins).resizable().scaledToFit().foregroundStyle(Theme.accent)
                    .scaledFrame(width: Theme.Size.wellM, height: Theme.Size.wellM)
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(p.name).font(Theme.Font.heading).lineLimit(1)
                    Text([p.version, p.publisher, p.marketName].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                        .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                }
                Spacer(minLength: Theme.Space.s)
                MarketStateChip(item: p)
            }
            .padding(.vertical, Theme.Space.xs)
            if let d = p.description, !d.isEmpty { Text(d).font(Theme.Font.secondary).foregroundStyle(.secondary) }
            if let tags = p.tags, !tags.isEmpty {
                FlowLayoutRow { ForEach(tags, id: \.self) { Chip(text: $0) } }
            }
            if let e = page.error { Text(e).font(Theme.Font.label).foregroundStyle(Theme.danger).textSelection(.enabled) }
        }
    }

    @ViewBuilder private func actions(_ p: ExtensionCatalogItem) -> some View {
        Section {
            switch ExtensionMarketText.step(p) {
            case .install:
                primary("安装", symbol: Theme.Icon.install, id: "market-install")
            case .upgrade:
                primary("升级到 \(p.version)", symbol: Theme.Icon.upgrade, id: "market-upgrade")
            case .upToDate:
                LabeledContent("已安装", value: p.installed ?? p.version)
            case .unavailable:
                Label { Text(p.problem ?? "这台 hub 装不了") } icon: {
                    Image(systemName: Theme.Icon.error).foregroundStyle(Theme.caution)
                }
                .font(Theme.Font.label)
            }
            if p.installed != nil {
                NavigationLink(value: SettingsPage.plugin(p.pluginID)) {
                    Label { Text("打开扩展设置").lineLimit(1) } icon: { Image(systemName: Theme.Icon.settings) }
                }
                .accessibilityIdentifier("market-open-plugin")
            }
            if page.canUninstall {
                Button(role: .destructive) { confirmRemove = true } label: {
                    Label { Text("卸载").lineLimit(1) } icon: { Image(systemName: Theme.Icon.delete) }
                }
                .tint(Theme.danger) // a Mac form draws a button in the accent whatever its role (#1512)
                .disabled(page.working)
                .accessibilityIdentifier("market-uninstall")
                .confirmationDialog("卸载「\(p.name)」？", isPresented: $confirmRemove, titleVisibility: .visible) {
                    Button(role: .destructive) {
                        Task { if await page.uninstall() { app.toast = Toast("已卸载", style: .success) } }
                    } label: { Text(page.isPack ? "只卸载合集" : "卸载").lineLimit(1) }
                    .accessibilityIdentifier("market-uninstall-confirm")
                    if page.isPack {
                        Button(role: .destructive) {
                            Task { if await page.uninstall(members: true) { app.toast = Toast(page.removalMessage, style: .success) } }
                        } label: { Text("连同成员一起卸载").lineLimit(1) }
                        .accessibilityIdentifier("market-uninstall-members")
                    }
                    Button("取消", role: .cancel) {}
                } message: {
                    Text(page.isPack ? "只卸载合集会保留成员。连成员一起卸载会删除成员的设置和数据。" : "它的设置和数据一起删掉；以后可以从市场再装。")
                }
            }
        } footer: {
            if p.installed != nil && !page.canUninstall && p.installedSource != nil {
                Text("这个扩展不是从这个市场装的，在 hub 的管理界面（/admin）或 hub 所在的机器上卸载。")
            }
        }
    }

    private func primary(_ title: String, symbol: String, id: String) -> some View {
        FormAction {
            Button { Task { await page.prepare() } } label: {
                HStack(spacing: Theme.Space.s) {
                    if page.working { ProgressView().controlSize(.small) } else { Image(systemName: symbol) }
                    Text(title).lineLimit(1)
                }
                .formActionWidth()
            }
            .themedButton(prominent: true)
            .disabled(page.working)
            .accessibilityIdentifier(id)
        }
    }

    @ViewBuilder private func about(_ p: ExtensionCatalogItem) -> some View {
        Section {
            LabeledContent("版本", value: p.version)
            if let v = p.installed, !v.isEmpty { LabeledContent("已安装的版本", value: v) }
            if let m = p.marketName, !m.isEmpty { LabeledContent("市场", value: m) }
            if let pub = p.publisher, !pub.isEmpty { LabeledContent("发布者", value: pub) }
            if let c = p.contributes, !c.isEmpty {
                ForEach(c.keys.sorted(), id: \.self) { point in
                    LabeledContent(ExtensionText.point(point)) {
                        Text((c[point] ?? []).joined(separator: "、")).lineLimit(2).multilineTextAlignment(.trailing)
                    }
                }
            }
            if let h = p.homepage, let url = URL(string: h) {
                Link(destination: url) { Label("主页", systemImage: Theme.Icon.openExternal).lineLimit(1) }
            }
        } header: {
            SettingsHeading("关于")
        }
    }
}

/// A line of what a plugin may do: what, and the detail under it.
private struct PermissionLine: View {
    var permission: ExtensionPermission
    var tone: Color? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
            Text(permission.title).font(Theme.Font.body).foregroundStyle(tone ?? Color.primary)
            if let d = permission.detail, !d.isEmpty { Text(d).font(Theme.Font.label).foregroundStyle(.secondary) }
        }
        .padding(.vertical, Theme.Space.xxs)
    }
}

/// The preview to agree to: the package downloaded and checked, what it may do (for an upgrade, what it asks for in
/// addition first), then 安装 / 升级.
private struct ExtensionInstallSheet: View {
    var preview: ExtensionPreview
    var name: String
    var confirm: () async -> Bool
    @Environment(\.dismiss) private var dismiss
    @State private var busy = false
    @State private var previewTime = Date.now

    private var upgrade: Bool { preview.upgrade == true }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    LabeledContent(upgrade ? "升级" : "版本",
                                   value: upgrade ? "\(preview.from ?? "") → \(preview.manifest.version ?? "")" : preview.manifest.version ?? "")
                    if let d = preview.manifest.description, !d.isEmpty { Text(d).font(Theme.Font.secondary).foregroundStyle(.secondary) }
                }
                if let dependencies = preview.depends, !dependencies.isEmpty {
                    Section {
                        ForEach(dependencies) { dependency in
                            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                                Text(dependency.title).font(Theme.Font.body).lineLimit(1)
                                Text(dependency.description).font(Theme.Font.label)
                                    .foregroundStyle(dependency.state == "ok" ? Color.secondary : Theme.cautionText)
                            }
                        }
                    } header: { SettingsHeading("依赖") }
                }
                if let members = preview.manifest.extensionPack, !members.isEmpty {
                    Section {
                        ForEach(members, id: \.id) { member in
                            LabeledContent(member.id, value: member.version ?? "")
                        }
                    } header: { SettingsHeading("合集成员") }
                }
                if let bundle = preview.bundle, !bundle.isEmpty {
                    ForEach(bundle) { member in
                        Section {
                            LabeledContent(member.manifest.name ?? member.manifest.id, value: member.manifest.version ?? "")
                            ForEach(member.permissions) { PermissionLine(permission: $0) }
                        } header: { SettingsHeading("一起安装") }
                    }
                }
                if let unresolved = preview.unresolved, !unresolved.isEmpty {
                    Section {
                        Text("市场里找不到：" + unresolved.joined(separator: "、"))
                            .font(Theme.Font.secondary).foregroundStyle(Theme.cautionText)
                    } footer: { Text("先添加提供这些扩展的市场，再重新预览。没有装好依赖时不会安装。") }
                }
                if let problem = preview.confirmationProblem(at: previewTime) {
                    Section {
                        Text(problem).font(Theme.Font.secondary).foregroundStyle(Theme.cautionText)
                            .accessibilityIdentifier("market-preview-problem")
                    }
                }
                if upgrade, let added = preview.addedPermissions, !added.isEmpty {
                    Section {
                        ForEach(added) { PermissionLine(permission: $0, tone: Theme.caution) }
                    } header: {
                        SettingsHeading("新版本多要的")
                    } footer: {
                        Text("升级就是同意这些。")
                    }
                }
                Section {
                    if preview.permissions.isEmpty {
                        Text(preview.manifest.extensionPack?.isEmpty == false
                             ? "合集本身不申请额外权限；成员按上方各自的权限运行。" : "这个包不申请额外权限。")
                            .font(Theme.Font.secondary).foregroundStyle(.secondary)
                    } else {
                        ForEach(preview.permissions) { PermissionLine(permission: $0) }
                    }
                } header: {
                    SettingsHeading(upgrade ? "新版本可以" : "它可以")
                } footer: {
                    Text("扩展按预览列出的权限和 hub 的隔离配置运行。包已按市场给的校验和核对过。")
                }
                if let w = preview.warnings, !w.isEmpty {
                    Section {
                        ForEach(w, id: \.self) { t in
                            Label { Text(t) } icon: { Image(systemName: Theme.Icon.error).foregroundStyle(Theme.caution) }
                                .font(Theme.Font.label)
                        }
                    }
                }
            }
            .themedPage()
            .navigationTitle(upgrade ? "升级 \(name)" : "安装 \(name)")
            .navigationBarTitleDisplayMode(.inline)
            .sheetCancel(unsaved: false) { dismiss() }
            .task(id: preview.expirationBoundary) {
                previewTime = .now
                guard let expires = preview.expirationBoundary else { return }
                if expires > .now { try? await Task.sleep(for: .seconds(expires.timeIntervalSinceNow)) }
                guard !Task.isCancelled else { return }
                previewTime = .now
            }
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button {
                        Task {
                            busy = true
                            if await confirm() { dismiss() }
                            busy = false
                        }
                    } label: {
                        if busy { ProgressView() } else { Text(upgrade ? "升级" : "安装").lineLimit(1) }
                    }
                    .disabled(busy || preview.confirmationProblem(at: previewTime) != nil)
                    .accessibilityIdentifier("market-confirm")
                }
            }
        }
        #if os(macOS)
        .macSheet()
        #endif
    }
}

/// 扩展市场 › 市场: the markets the hub reads, each with what it is, how many plugins, when it was read and what went
/// wrong; added with an address and an optional token (written, never shown), refreshed, given a new token, removed.
struct ExtensionMarketsView: View {
    @State private var page = ExtensionMarketModel()
    @State private var adding = false
    @State private var tokenFor: ExtensionMarket?

    var body: some View {
        SettingsForm {
            if page.markets.isEmpty && !page.loading && page.error == nil {
                Section {
                    EmptyStateView(symbol: Theme.Icon.pluginMarket, title: "还没有扩展市场",
                                   message: "填一个市场的地址：https://… 的索引、github:owner/repo（私有仓库要一个只读 token），或 git:…（用 hub 机器的 SSH 凭据）。",
                                   action: ("添加市场", { adding = true }))
                }
                .listRowBackground(Color.clear)
            }
            if let auto = page.autoUpgrade, !page.markets.isEmpty {
                Section {
                    Toggle(isOn: Binding(get: { auto }, set: { on in Task { await page.setAutoUpgrade(on) } })) {
                        Text("自动升级扩展").lineLimit(1)
                    }
                    .accessibilityIdentifier("market-auto-upgrade")
                } footer: {
                    Text(auto ? "从这些市场装的扩展出了新版本，扩展空闲时 hub 自动装上；要的权限变多时先问你。每个扩展也可以在自己的页面里单独关掉。"
                              : "关着：有新版本时只在扩展的页面里提示，你自己去升级。")
                }
            }
            ForEach(page.markets) { m in
                Section {
                    MarketRow(market: m, busy: page.busyMarket == m.id) { act in
                        switch act {
                        case .refresh: Task { await page.refreshMarket(m.id) }
                        case .token: tokenFor = m
                        case .remove: Task { await page.removeMarket(m.id) }
                        }
                    }
                }
            }
            if let e = page.error {
                Section { LoadProblem(text: e) }
            }
            #if os(macOS)
            Section {
                HStack {
                    Spacer(minLength: 0)
                    Button("添加市场…") { adding = true }.lineLimit(1).formButton()
                        .accessibilityIdentifier("market-add")
                }
            }
            #endif
        }
        .themedPage()
        .overlay { if page.markets.isEmpty && page.loading { ProgressView() } }
        .detailTitle("市场")
        .navigationBarTitleDisplayMode(.inline)
        #if os(iOS) || os(visionOS)
        .pageToolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button { adding = true } label: { Image(systemName: Theme.Icon.add) }
                    .accessibilityLabel("添加市场")
                    .accessibilityIdentifier("market-add")
            }
        }
        #endif
        .sheet(isPresented: $adding) {
            MarketTokenSheet(title: "添加市场", asksSpec: true, hasToken: false) { spec, token in
                await page.addMarket(spec: spec, token: token)
            }
        }
        .sheet(item: $tokenFor) { m in
            MarketTokenSheet(title: "「\(m.title)」的 token", asksSpec: false, hasToken: m.hasToken) { _, token in
                await page.setToken(m.id, token: token)
            }
        }
        .task { await page.loadMarkets(); await page.loadAutoUpgrade() }
        .refreshable { await page.loadMarkets(); await page.loadAutoUpgrade() }
    }
}

/// A market: its name and address, how many plugins and when it was read, its error and warnings; then its actions
/// (refresh now, a token, remove) in a row of their own.
private struct MarketRow: View {
    enum Act { case refresh, token, remove }
    var market: ExtensionMarket
    var busy: Bool
    var act: (Act) -> Void
    @State private var confirmRemove = false

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            HStack(spacing: Theme.Space.s) {
                Text(market.title).font(Theme.Font.bodyStrong).lineLimit(1)
                Spacer(minLength: Theme.Space.s)
                if busy { ProgressView().controlSize(.small) }
            }
            Text(market.spec).font(Theme.Font.code).foregroundStyle(.secondary).lineLimit(2).truncationMode(.middle).textSelection(.enabled)
            if let d = market.description, !d.isEmpty { Text(d).font(Theme.Font.secondary).foregroundStyle(.secondary) }
            Text(ExtensionMarketText.marketSummary(market)).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            if let e = market.error, !e.isEmpty {
                Label { Text(e) } icon: { Image(systemName: Theme.Icon.error) }
                    .font(Theme.Font.label)
                    .foregroundStyle(Theme.danger)
            }
            ForEach(market.warnings ?? [], id: \.self) { w in
                Label { Text(w) } icon: { Image(systemName: Theme.Icon.error).foregroundStyle(Theme.caution) }
                    .font(Theme.Font.label)
            }
        }
        .padding(.vertical, Theme.Space.xxs)
        HStack(spacing: Theme.Space.l) {
            action("刷新", .refresh, id: "market-refresh-\(market.id)")
            action(market.hasToken ? "换 token" : "设 token", .token, id: "market-token-\(market.id)")
            Spacer(minLength: 0)
            Button(role: .destructive) { confirmRemove = true } label: { Text("删除").lineLimit(1) }
            .buttonStyle(.borderless)
            .foregroundStyle(Theme.danger) // a Mac borderless button draws its role in the accent
            .accessibilityIdentifier("market-remove-\(market.id)")
            .confirmationDialog("删除市场「\(market.title)」？", isPresented: $confirmRemove, titleVisibility: .visible) {
                Button(role: .destructive) { act(.remove) } label: { Text("删除").lineLimit(1) }
                    .accessibilityIdentifier("market-remove-confirm")
                Button("取消", role: .cancel) {}
            } message: {
                Text("hub 不再读这个市场；从它装的扩展留着。")
            }
        }
        .font(Theme.Font.secondary)
        .disabled(busy)
    }

    /// Several buttons in one row of a list: borderless, or the whole row is one button.
    private func action(_ title: String, _ a: Act, id: String) -> some View {
        Button { act(a) } label: { Text(title).lineLimit(1) }
        .buttonStyle(.borderless)
        .accessibilityIdentifier(id)
    }
}

/// Adding a market (its address and an optional token), or a market's new token: the token is typed in hidden and never
/// shown again; the hub reads the index before it answers, and why it could not stays on the sheet.
private struct MarketTokenSheet: View {
    var title: String
    var asksSpec: Bool
    var hasToken: Bool
    /// nil: done; otherwise why not.
    var save: (_ spec: String, _ token: String) async -> String?
    @Environment(\.dismiss) private var dismiss
    @State private var spec = ""
    @State private var token = ""
    @State private var busy = false
    @State private var problem: String?

    var body: some View {
        NavigationStack {
            List {
                if asksSpec {
                    Section {
                        // Sent as typed: the hub reads it and says what is wrong (#1373), the app does not judge the form.
                        TextField("https://…、github:owner/repo 或 git:…", text: $spec)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .accessibilityIdentifier("market-spec")
                    } header: {
                        SettingsHeading("地址")
                    } footer: {
                        Text(verbatim: "三种写法：https://example.com/extensions/（不以 .json 结尾的读它下面的 persona-extensions.json，没有再读 persona-plugins.json）；github:owner/repo（仓库根目录的那份，私有的要 token）；git:<地址>[#分支]（git@host:path.git、ssh://…、https://…，用 hub 机器自己的 SSH 凭据，不用 token）。")
                    }
                }
                Section {
                    SecretField(hasToken ? "输入新的 token 替换" : (asksSpec ? "公开的市场不用填" : "输入 token"), filled: "已填写，点保存",
                                text: $token, identifier: "market-token")
                } header: {
                    SettingsHeading(asksSpec ? "Token（可选）" : "Token")
                } footer: {
                    Text("私有仓库用只读的 token（GitHub：这个仓库的 Contents 读权限）。加密存在 hub 上，只发给市场自己，存好后不再显示。git: 市场用 hub 机器的 SSH 凭据，不用填。")
                }
                if !asksSpec && hasToken {
                    Section {
                        Button(role: .destructive) { submit(clearing: true) } label: { Text("去掉 token").lineLimit(1) }
                            .tint(Theme.danger)
                            .disabled(busy)
                    }
                }
                if let problem {
                    Section {
                        Label { Text(problem).textSelection(.enabled) } icon: { Image(systemName: Theme.Icon.error) }
                            .font(Theme.Font.label)
                            .foregroundStyle(Theme.danger)
                    }
                }
            }
            .themedPage()
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .sheetCancel(unsaved: !spec.isEmpty || !token.isEmpty) { dismiss() }
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button { submit(clearing: false) } label: {
                        if busy { ProgressView() } else { Text(asksSpec ? "添加" : "保存").lineLimit(1) }
                    }
                    .disabled(busy || (asksSpec ? spec.trimmingCharacters(in: .whitespaces).isEmpty : token.trimmingCharacters(in: .whitespaces).isEmpty))
                    .accessibilityIdentifier("market-save")
                }
            }
        }
        .presentationDetents([.medium, .large])
        #if os(macOS)
        .macSheet()
        #endif
    }

    private func submit(clearing: Bool) {
        Task {
            busy = true
            problem = await save(spec, clearing ? "" : token)
            busy = false
            if problem == nil { dismiss() }
        }
    }
}
