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
        .navigationTitle("选择引擎")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: ref) {
            guard let machine else { return }
            selection = EngineSelectionModel(machine: machine, pairing: pairing, deps: .init(api: { [ref] in app.api(ref.group) }, updated: { app.applyMachine($0) }))
        }
        .onChange(of: machine) { _, updated in
            if let updated { selection?.synchronize(updated, pairing: pairing) }
        }
    }

    private func detection(_ engine: MachineEngine, _ machine: Machine) -> String {
        guard machine.online else { return "离线，暂无检测信息" }
        guard let found = machine.engines.first(where: { $0.id == engine.rawValue }) else { return "暂无检测信息" }
        let status = found.available ? "检测到可用" : "没检测到"
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
