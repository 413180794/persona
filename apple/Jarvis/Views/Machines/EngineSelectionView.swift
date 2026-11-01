import SwiftUI

/// Pairing uses the same picker as Settings, with an always-available way to continue without saving.
struct PairingEngineView: View {
    var machineID: String

    var body: some View {
        NavigationStack {
            EngineSelectionView(machineID: machineID, pairing: true)
        }
    }
}

struct EngineSelectionView: View {
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    var machineID: String
    var pairing = false
    @State private var selection: EngineSelectionModel?

    var body: some View {
        let machine = app.machine(machineID)
        Form {
            if let machine, let selection {
                if pairing {
                    Section {
                        Text("这台电脑用哪个引擎").font(Theme.Font.title)
                        Text(machine.name).font(Theme.Font.secondary).foregroundStyle(.secondary)
                    }
                }
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
                    Text("引擎")
                } footer: {
                    Text(machine.online ? "只显示所选引擎里的会话，随时可在设置 › 机器里修改。" : "机器离线，选择会在连接后生效。只显示所选引擎里的会话。")
                }
                if let error = selection.error {
                    Section {
                        Text(error).font(Theme.Font.secondary).foregroundStyle(Theme.danger)
                        if pairing { Text("必须选一个引擎才能继续，检查网络后再点一次确认。").font(Theme.Font.secondary) }
                    }
                }
                Section {
                    Button { Task { if await selection.save(machineID: machineID) { finish() } } } label: {
                        HStack {
                            Text(pairing ? "确认并继续" : "保存").lineLimit(1)
                            Spacer()
                            if selection.saving { ProgressView() }
                        }
                    }
                    .disabled(selection.saving || !EngineSelectionModel.canSelect(selection.selected, on: machine))
                    .accessibilityIdentifier("engine.save")
                }
            } else {
                Section {
                    Text("暂时读不到这台机器")
                    if pairing { Button { finish() } label: { Text("先继续").lineLimit(1) } }
                }
            }
        }
        .themedPage()
        .navigationTitle("选择引擎")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: machineID) {
            guard let machine else { return }
            selection = EngineSelectionModel(machine: machine, pairing: pairing, deps: .init(api: { app.api }, updated: { app.applyMachine($0) }))
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

    private func finish() {
        if pairing { app.pairingEngineMachineID = nil } else { dismiss() }
    }
}
