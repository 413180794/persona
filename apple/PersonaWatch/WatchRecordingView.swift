import SwiftUI

/// 手表录音独立于 hub 连接：没有网络也可以先保存，回到手机旁再回传。
struct WatchRecordingView: View {
    @State private var capture = RecordingCapture.shared
    @State private var library = RecordingLibrary.shared
    @State private var transfer = WatchRecordingTransfer.shared
    @State private var locale = "zh-CN"

    var body: some View {
        ScrollView {
            VStack(spacing: Theme.Space.m) {
                Text(RecordingFormat.clock(capture.elapsed))
                    .font(Theme.Font.display.monospacedDigit())
                    .foregroundStyle(capture.state == .recording ? Theme.danger : .primary)
                if capture.state == .idle {
                    Button {
                        let languages = ["zh-CN", "en-US", "ja-JP"]
                        let index = languages.firstIndex(of: locale) ?? 0
                        locale = languages[(index + 1) % languages.count]
                    } label: {
                        Text("语言：\(locale == "zh-CN" ? "中文" : locale == "en-US" ? "英语" : "日语")")
                            .font(Theme.Font.secondary).lineLimit(1)
                    }
                    .buttonStyle(.bordered)
                    Button { Task { await capture.start(locale: locale) } } label: {
                        Label("开始录音", systemImage: Theme.Icon.recording).lineLimit(1)
                    }
                    .buttonStyle(.borderedProminent)
                    .recordingPrimaryGesture()
                } else if capture.state == .starting {
                    ProgressView("等待麦克风权限").font(Theme.Font.secondary)
                    Button("取消") { capture.cancelStarting() }.lineLimit(1)
                } else {
                    Text(capture.state == .paused ? "已暂停" : "正在录音 · 放下手腕也会继续")
                        .font(Theme.Font.caption).foregroundStyle(.secondary)
                    HStack(spacing: Theme.Space.s) {
                        Button { capture.togglePause() } label: {
                            Label(capture.state == .paused ? "继续" : "暂停", systemImage: capture.state == .paused ? Theme.Icon.run : Theme.Icon.pause)
                                .labelStyle(.iconOnly)
                        }
                        Button { capture.addMark() } label: { Label("书签", systemImage: Theme.Icon.pin).labelStyle(.iconOnly) }
                            .recordingPrimaryGesture()
                    }
                    Button { capture.stop(); transfer.pump(force: true) } label: {
                        Label("结束并保存", systemImage: Theme.Icon.interrupt).lineLimit(1)
                    }.tint(Theme.danger)
                }
                if let error = capture.error ?? transfer.error ?? library.error {
                    Text(error).font(Theme.Font.caption).foregroundStyle(Theme.caution)
                }
                Text("回传手机后本机转写，在手机上确认后才能交给 Persona。")
                    .font(Theme.Font.caption).foregroundStyle(.secondary)
                ForEach(Array(library.entries.filter { $0.source == .watch }.prefix(10))) { entry in
                    VStack(alignment: .leading, spacing: Theme.Space.xs) {
                        Text(entry.displayTitle).font(Theme.Font.secondaryMedium).lineLimit(2)
                        Text("\(RecordingFormat.clock(entry.duration)) · \(transfer.pending(entry.id) == 0 && entry.endDelivered ? "已回传手机" : "等待回传")")
                            .font(Theme.Font.caption).foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(Theme.Space.s)
                    .background(Theme.Watch.cardFill, in: .rect(cornerRadius: Theme.Radius.card))
                }
            }.padding(Theme.Space.m)
        }
        .navigationTitle("录音上下文")
        .onAppear { transfer.start() }
    }
}

private extension View {
    @ViewBuilder func recordingPrimaryGesture() -> some View {
        if #available(watchOS 11.0, *) { handGestureShortcut(.primaryAction) } else { self }
    }
}
