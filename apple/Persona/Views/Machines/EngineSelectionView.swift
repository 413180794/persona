import SwiftUI

/// Pairing uses the same picker as Settings, with an always-available way to continue without saving.
struct PairingEngineView: View {
    var ref: MachineRef

    var body: some View {
        NavigationStack {
            EngineSelectionView(ref: ref, pairing: true)
        }
    }
}

struct EngineSelectionView: View {
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    var ref: MachineRef
    var pairing = false
    @State private var selection: EngineSelectionModel?
    @State private var botmux: BotmuxSwitchModel?
    private var machineID: String { ref.id }

    var body: some View {
        let machine = app.machine(ref)
        Form {
            if let machine, let selection {
                #if os(iOS) || os(visionOS)
                if pairing {
                    Section {
                        Text("这台电脑用哪个引擎").font(Theme.Font.title)
                        Text(machine.name).font(Theme.Font.secondary).foregroundStyle(.secondary)
                    }
                }
                #endif
                Section {
                    ForEach(EngineSelectionModel.options(machine)) { found in
                        let engine = MachineEngine(rawValue: found.id)
                        Button { selection.choose(engine) } label: {
                            HStack(spacing: Theme.Space.m) {
                                VStack(alignment: .leading, spacing: Theme.Space.xs) {
                                    Text(found.name).font(Theme.Font.heading).foregroundStyle(.primary).lineLimit(1)
                                    if let detail = engine.detail {
                                        Text(detail).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1)
                                    }
                                    Text(detection(engine, machine)).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                                    if let note = found.note, !note.isEmpty {
                                        Text(note).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                                            .minimumScaleFactor(0.8)
                                    }
                                }
                                Spacer(minLength: Theme.Space.s)
                                Image(systemName: selection.selected == engine ? Theme.Icon.successFilled : Theme.Icon.unchecked)
                                    .font(Theme.Font.heading)
                                    .accessibilityHidden(true)
                                    .foregroundStyle(selection.selected == engine ? Theme.accent : Color.secondary)
                            }
                            .padding(.vertical, Theme.Space.s)
                            .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                        .accessibilityIdentifier("engine.option.\(engine.rawValue)")
                        .accessibilityAddTraits(selection.selected == engine ? .isSelected : [])
                        .disabled(selection.saving || !EngineSelectionModel.canSelect(engine, on: machine))
                    }
                } header: {
                    #if os(macOS)
                    // The Mac's pairing step fills the window: the question heads the page, not a card of its own.
                    if pairing {
                        VStack(alignment: .leading, spacing: Theme.Space.xs) {
                            Text("这台电脑用哪个引擎").font(Theme.Font.title).foregroundStyle(.primary)
                            Text(machine.name).font(Theme.Font.secondary).foregroundStyle(.secondary)
                        }
                        .textCase(nil)
                        .padding(.bottom, Theme.Space.s)
                    } else {
                        Text("引擎")
                    }
                    #else
                    Text("引擎")
                    #endif
                } footer: {
                    if selection.selected == .herdrBoth {
                        Text(machine.online ? "同时显示 herdr 和 Herdr App 里的会话；从手机新建的会话仍开在 herdr 里。随时可在设置 › 机器里修改。" : "机器离线，选择会在连接后生效。同时显示两个引擎里的会话。")
                    } else {
                        Text(machine.online ? "只显示所选引擎里的会话，随时可在设置 › 机器里修改。" : "机器离线，选择会在连接后生效。只显示所选引擎里的会话。")
                    }
                }
                if let error = selection.error {
                    Section {
                        Text(error).font(Theme.Font.secondary).foregroundStyle(Theme.danger)
                        if pairing { Text("必须选一个引擎才能继续，检查网络后再点一次确认。").font(Theme.Font.secondary) }
                    }
                }
                #if os(iOS) || os(visionOS)
                Section {
                    Button { save(selection) } label: {
                        HStack {
                            Text(pairing ? "确认并继续" : "保存").lineLimit(1)
                            Spacer()
                            if selection.saving { ProgressView() }
                        }
                    }
                    .disabled(selection.saving || !EngineSelectionModel.canSelect(selection.selected, on: machine))
                    .accessibilityIdentifier("engine.save")
                }
                #endif
                if !pairing, let botmux, let on = botmux.on {
                    // Saved as soon as it is flipped, not with 保存: it is not part of the engine choice (#2575).
                    Section {
                        Toggle(isOn: Binding(get: { on }, set: { next in Task { await botmux.set(next) } })) {
                            Text("同时显示 botmux 会话").font(Theme.Font.body).lineLimit(1)
                        }
                        .disabled(botmux.saving)
                        .accessibilityIdentifier("engine.botmux")
                        if let error = botmux.error {
                            Text(error).font(Theme.Font.secondary).foregroundStyle(Theme.danger)
                        }
                    } footer: {
                        Text("这台电脑装了 botmux 时，botmux 里的会话和所选引擎的会话一起列出。关掉后不再列出，也不再运行 botmux。")
                    }
                }
            } else {
                Section {
                    Text("暂时读不到这台机器")
                    if pairing { Button { finish() } label: { Text("先继续").lineLimit(1) } }
                }
            }
        }
        .themedPage()
        #if os(macOS)
        // A form row of tinted text read as a link, not the step's way on (2026-11-02): the Mac puts the action
        // where its setup sheets do, a prominent button at the bottom right, and ⏎ presses it.
        .safeAreaInset(edge: .bottom, spacing: 0) {
            if let machine, let selection { saveBar(selection, machine) }
        }
        #endif
        .detailTitle(String(localized: "选择引擎"))
        .navigationBarTitleDisplayMode(.inline)
        .task(id: ref) {
            guard let machine else { return }
            selection = EngineSelectionModel(machine: machine, pairing: pairing, deps: .init(api: { [ref] in app.api(ref.group) }, updated: { app.applyMachine($0) }))
            guard !pairing else { return }
            let api = { [ref] in app.api(ref.group) }
            let model = BotmuxSwitchModel(deps: .init(
                load: { [ref] in
                    guard let api = api() else { throw APIError(status: 0, message: String(localized: "还没连接 hub")) }
                    return try await api.botmux(machine: ref.id)
                },
                save: { [ref] on in
                    guard let api = api() else { throw APIError(status: 0, message: String(localized: "还没连接 hub")) }
                    return try await api.setBotmux(machine: ref.id, on: on)
                }))
            botmux = model
            await model.load()
        }
        .onChange(of: machine) { _, updated in
            if let updated { selection?.synchronize(updated, pairing: pairing) }
        }
    }

    private func detection(_ engine: MachineEngine, _ machine: Machine) -> String {
        guard machine.online else { return String(localized: "离线，暂无检测信息") }
        guard let found = machine.engines.first(where: { $0.id == engine.rawValue }) else { return String(localized: "暂无检测信息") }
        let status = found.available ? String(localized: "检测到可用") : String(localized: "没检测到")
        return found.available && !(found.version ?? "").isEmpty ? "\(status) · \(found.version!)" : status
    }

    private func save(_ selection: EngineSelectionModel) {
        Task { if await selection.save(machineID: machineID) { finish() } }
    }

    #if os(macOS)
    private func saveBar(_ selection: EngineSelectionModel, _ machine: Machine) -> some View {
        HStack(spacing: Theme.Space.m) {
            Spacer()
            if selection.saving { ProgressView().controlSize(.small) }
            Button { save(selection) } label: {
                Text(pairing ? "确认并继续" : "保存").lineLimit(1)
            }
            .themedButton(prominent: true)
            .controlSize(.large)
            // ⏎ only on the pairing step (a window of its own): pushed into the main window from 机器, a window-wide
            // default button took ⏎ from the ⌘K panel and the search field, saved and closed the page.
            .keyboardShortcut(pairing ? .defaultAction : nil)
            .disabled(selection.saving || !EngineSelectionModel.canSelect(selection.selected, on: machine))
            .accessibilityIdentifier("engine.save")
        }
        .padding(.horizontal, Theme.Space.xl)
        .padding(.vertical, Theme.Space.l)
        .background(.bar) // the Mac's bottom bars (find bar, the inbox's reply bar): the window's own tint in dark
        .overlay(alignment: .top) { Divider() }
    }
    #endif

    private func finish() {
        if pairing { app.pairingEngineMachine = nil } else { dismiss() }
    }
}
