import SwiftUI

// The Dynamic Island is an iPhone's: its preview lives in the widget's sources (LiveActivity/), and a Mac has no island.
#if os(iOS)

/// 设置 › 通知 › 灵动岛: what the compact Dynamic Island shows on each side of the camera. Kept per device on the hub
/// (prefs.island), which fills in only the numbers this device shows (docs/live-activity.md).
struct IslandSettingsView: View {
    @Binding var island: IslandPrefs
    @Environment(AppModel.self) private var model
    /// 灵动岛诊断, opened by touching and holding the first preview (not a row: it is for a phone that draws the wrong
    /// persona, when asked to).
    @State private var diagnostics = false

    var body: some View {
        List {
            Section {
                VStack(spacing: Theme.Space.l) {
                    preview("平时", Self.sample(needsYou: false), island)
                        .contentShape(.rect)
                        .onLongPressGesture { diagnostics = true }
                    preview("有 Agent 需要你时", Self.sample(needsYou: true), island)
                    VStack(spacing: Theme.Space.sm) {
                        IslandSharedPreview(state: Self.sample(needsYou: false))
                        Text("和其他 App 共用时").font(Theme.Font.label).foregroundStyle(.secondary)
                    }
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, Theme.Space.sm)
            } footer: {
                Text("有 Agent 需要你时，左侧总是换成红色的数量。别的 App 也有实时活动时，灵动岛只留给 Persona 一小块，只显示最急的一类的数量（需要你 > 在工作 > 做完未看），不受上面的样式影响。")
            }

            Section("样式") {
                ForEach(IslandPrefs.Preset.allCases, id: \.self) { preset in
                    Button {
                        island.preset = preset
                        if preset == .custom, island.left == nil, island.right == nil {
                            (island.left, island.right) = (.status, .tpm)
                        }
                    } label: {
                        HStack(spacing: Theme.Space.m) {
                            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                                Text(preset.title).foregroundStyle(.primary).lineLimit(1)
                                Text(Self.summary(preset)).font(Theme.Font.caption).foregroundStyle(.secondary)
                            }
                            Spacer(minLength: 8)
                            if island.preset == preset {
                                Image(systemName: Theme.Icon.done).font(Theme.Font.heading).foregroundStyle(Theme.accent)
                            }
                        }
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .accessibilityAddTraits(island.preset == preset ? .isSelected : [])
                }
            }

            if island.preset == .custom {
                Section {
                    Picker("左侧", selection: side(\.left)) {
                        ForEach(IslandSlot.allCases.filter { $0 != .none }, id: \.self) { Text($0.title).tag($0) }
                    }
                    Picker("右侧", selection: side(\.right)) {
                        ForEach(IslandSlot.allCases, id: \.self) { Text($0.title).tag($0) }
                    }
                } header: {
                    Text("自定义")
                } footer: {
                    Text("「状态数量」在左侧显示最急的一类（需要你 > 在工作 > 做完未看），在右侧显示下一类。")
                }
            }

            if island.sides.left == .load || island.sides.right == .load {
                Section("负载看哪台") {
                    Picker("机器", selection: Binding(get: { island.machine ?? "" }, set: { island.machine = $0.isEmpty ? nil : $0 })) {
                        Text("最忙的那台").tag("")
                        ForEach(model.machines) { Text($0.name).tag($0.id) }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                }
            }

            Section {
            } footer: {
                Text("需要你、在工作、做完了的变化立刻更新；token、负载这些数字最多每分钟更新一次（系统限制实时活动的推送次数），在 App 里时实时更新。")
            }
        }
        .themedPage()
        .navigationTitle("灵动岛")
        .navigationBarTitleDisplayMode(.inline)
        .navigationDestination(isPresented: $diagnostics) { IslandDiagnosticsView() }
        #if DEBUG
        // `-openIslandDiagnostics YES` (with `-openIslandSettings YES -openNotificationSettings YES -tab settings`).
        .task {
            if UserDefaults.standard.bool(forKey: "openIslandDiagnostics") {
                try? await Task.sleep(for: .milliseconds(600))
                diagnostics = true
            }
        }
        #endif
    }

    private func preview(_ title: String, _ state: FleetState, _ island: IslandPrefs) -> some View {
        VStack(spacing: Theme.Space.sm) {
            IslandPreview(state: state.tailored(island: island, details: false), look: model.fleetLook)
            Text(title).font(Theme.Font.label).foregroundStyle(.secondary)
        }
    }

    private func side(_ path: WritableKeyPath<IslandPrefs, IslandSlot?>) -> Binding<IslandSlot> {
        Binding { island[keyPath: path] ?? .status } set: { island[keyPath: path] = $0 }
    }

    static func summary(_ preset: IslandPrefs.Preset) -> String {
        switch preset {
        case .a: "左边最急的数量，右边下一类：需要你、在工作、做完未看"
        case .b: "左边状态，右边所有 Agent 每分钟输出的 token"
        case .c: "左边状态，右边今天用掉的 token"
        case .custom: "左右两侧各选一项"
        }
    }

    /// Made-up numbers for the previews.
    static func sample(needsYou: Bool) -> FleetState {
        FleetState(phase: needsYou ? .needsYou : .working, needsYou: needsYou ? 2 : 0, working: 3, unread: 1,
                   workingSince: Date.now.timeIntervalSince1970 - 754, assistant: true,
                   stats: .init(tpm: 12000, tokens: 42_000_000, requests: 1200, loads: [.init(id: "", name: "", percent: 35)]))
    }
}
#endif
