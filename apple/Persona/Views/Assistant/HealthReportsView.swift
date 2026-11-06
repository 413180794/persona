import SwiftUI

// Health Reports (docs/health.md「体检报告」, #4998): what was uploaded to the health space's reports/. A report opens as the
// record the health conversation wrote or as the original, and deleting takes both. The web's HealthReports.

/// The reports' state and what it asks the hub (MVVM, docs/ios.md「逻辑层的选型」).
@Observable final class HealthReportsModel {
    private(set) var reports: [APIClient.HealthReport] = []
    private(set) var loaded = false
    /// Why the list could not be read; shown only while there is nothing to show.
    private(set) var error: String?
    @ObservationIgnored private let machine: String
    @ObservationIgnored private let deps: AssistantDeps

    init(machine: String, deps: AssistantDeps = .live) {
        self.machine = machine
        self.deps = deps
    }

    func load() async {
        guard let api = deps.api() else { return }
        do {
            reports = try await api.healthReports(machine: machine)
            error = nil
        } catch {
            self.error = error.shown ?? self.error
        }
        loaded = true
    }

    func delete(_ report: APIClient.HealthReport) async {
        guard let api = deps.api() else { return }
        do {
            try await api.deleteHealthReport(name: report.name, machine: machine)
            Haptics.success()
            await load()
        } catch {
            deps.report(error)
        }
    }
}

struct HealthReportsView: View {
    var machine: MachineRef
    @State private var page: HealthReportsModel
    @State private var file: FileRef?
    @State private var deleting: APIClient.HealthReport?

    init(machine: MachineRef) {
        self.machine = machine
        _page = State(initialValue: HealthReportsModel(machine: machine.id))
    }

    var body: some View {
        content
            .themedPage()
            .pageTitle(String(localized: "体检报告"))
            .task { await page.load() }
            .refreshable { await page.load() }
            #if DEBUG
            .task(id: page.loaded) { // the menu's items are out of the runtime CLI's reach: straight to what they open
                guard let first = page.reports.first else { return }
                if UserDefaults.standard.bool(forKey: "showReportDelete") { deleting = first }
                if UserDefaults.standard.bool(forKey: "openReportRecord"), let record = first.recordPath { open(record) }
            }
            #endif
            .sheet(item: $file) { FileViewer(ref: $0) }
            .confirmationDialog(
                "删除这份报告？",
                isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                titleVisibility: .visible,
                presenting: deleting
            ) { report in
                Button("确定删除", role: .destructive) { Task { await page.delete(report) } }
                Button("不删了", role: .cancel) {}
            } message: { _ in
                Text("原件和整理好的记录会永久删除，不能恢复。健康对话和健康记忆里已经提到的内容不会一起删，要清掉需要单独处理。")
            }
    }

    @ViewBuilder private var content: some View {
        if !page.loaded {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if page.reports.isEmpty {
            if let error = page.error {
                EmptyStateView(symbol: Theme.Icon.document, title: String(localized: "没读到体检报告"), message: error)
            } else {
                EmptyStateView(symbol: Theme.Icon.document, title: String(localized: "还没有体检报告"),
                               message: String(localized: "在健康页点「上传体检报告」，传过的报告会留在这里，可以随时查看和删除。"))
            }
        } else {
            List(page.reports) { report in row(report) }
        }
    }

    private func open(_ path: String) { file = FileRef(group: machine.group, machineID: machine.id, path: path) }

    @ViewBuilder private func actions(_ report: APIClient.HealthReport) -> some View {
        if let record = report.recordPath {
            Button { open(record) } label: { Label("查看整理记录", systemImage: Theme.Icon.document) }
        }
        if let path = report.path {
            Button { open(path) } label: { Label("打开原件", systemImage: Theme.Icon.file) }
        }
        Button(role: .destructive) { deleting = report } label: { Label("删除", systemImage: Theme.Icon.delete) }
    }

    /// Every action is one tap away, in a menu: a report has two things to open and they read differently.
    private func row(_ report: APIClient.HealthReport) -> some View {
        Menu { actions(report) } label: {
            HStack(spacing: Theme.Space.m) {
                Image(systemName: Theme.Icon.document)
                    .font(Theme.Font.secondary)
                    .foregroundStyle(Theme.Hue.pink)
                    .scaledFrame(width: Theme.Size.well, height: Theme.Size.well)
                    .background(Theme.Hue.pink.opacity(0.14), in: .circle)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(report.title).font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
                    Text(report.recordPath == nil ? "还没整理" : "已整理")
                        .font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1)
                }
                Spacer(minLength: Theme.Space.s)
                if let at = report.modifiedAt {
                    Text(at.formatted(date: .numeric, time: .omitted)).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                }
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .menuStyle(.button)
        .accessibilityIdentifier("health-report")
        .swipeActions(edge: .trailing) {
            Button { deleting = report } label: { Label("删除", systemImage: Theme.Icon.delete) }.tint(Theme.danger)
        }
    }
}
