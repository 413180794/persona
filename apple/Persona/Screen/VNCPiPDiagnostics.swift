#if os(iOS)
import AVKit
import SwiftUI

/// 小窗诊断 (#1839; VNC page › …): whether this device can show the small window and what it did, step by step, so a
/// phone whose small window does not come up says where it stopped. 「复制全部」 is what to send.
struct VNCPiPDiagnostics: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss

    private var facts: [(String, String)] {
        let audio = AVAudioSession.sharedInstance()
        return [
            ("支持画中画", VNCPiP.supported ? "是" : "否"),
            ("小窗开着", VNCPiP.shared.active ? "是" : "否"),
            ("音频会话", Self.categories[audio.category] ?? audio.category.rawValue),
            ("后台音频", (Bundle.main.object(forInfoDictionaryKey: "UIBackgroundModes") as? [String])?.contains("audio") == true ? "有" : "没有"),
        ]
    }

    private static let categories: [AVAudioSession.Category: String] = [
        .playback: "播放", .soloAmbient: "默认（不能后台）", .ambient: "环境", .playAndRecord: "播放和录音", .record: "录音",
    ]

    private var text: String {
        (facts.map { "\($0.0)：\($0.1)" } + VNCPiPLog.lines).joined(separator: "\n")
    }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    ForEach(facts, id: \.0) { label, value in
                        LabeledContent {
                            Text(value).textSelection(.enabled)
                        } label: {
                            Text(label).lineLimit(1)
                        }
                        .font(Theme.Font.secondary)
                    }
                }
                Section("最近的步骤") {
                    if VNCPiPLog.lines.isEmpty {
                        Text("还没有开过小窗。").font(Theme.Font.secondary).foregroundStyle(.secondary)
                    }
                    ForEach(Array(VNCPiPLog.lines.enumerated().reversed()), id: \.offset) { _, line in
                        Text(line).font(Theme.Font.code).textSelection(.enabled)
                    }
                }
                Section {
                    Button {
                        UIPasteboard.general.string = text
                        model.toast = Toast("已复制", style: .success)
                    } label: {
                        Label("复制全部", systemImage: Theme.Icon.copy).lineLimit(1)
                    }
                } footer: {
                    Text("系统日志里也有（控制台，子系统 jarvis，类别 vnc）。")
                }
            }
            .themedPage()
            .navigationTitle("小窗诊断")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button { dismiss() } label: { Image(systemName: Theme.Icon.close) }
                        .accessibilityLabel("关闭")
                }
            }
        }
        .accessibilityIdentifier("vnc-pip-diagnostics")
    }
}
#endif
