import SwiftUI

/// 输入框里的录音选择与录音控制，不改变原有输入框的画法。
struct RecordingContextSheet: View {
    @Environment(\.dismiss) private var dismiss
    @State private var library = RecordingLibrary.shared
    @State private var transcription = RecordingTranscription.shared
    #if os(iOS) || os(visionOS)
    @State private var capture = RecordingCapture.shared
    #endif
    @State private var locale = "zh-CN"
    @State private var opened: UUID?
    @State private var deleting: UUID?
    @State private var failure: String?
    var pick: (ContextRef) -> Void

    var body: some View {
        NavigationStack {
            List {
                #if os(iOS) || os(visionOS)
                Section {
                    captureControls
                } footer: {
                    Text("音频留在本机，手表录音会回传这里。本机转写后选为上下文，点发送才交给 Persona。")
                }
                #endif
                Section("可用录音") {
                    if library.entries.isEmpty {
                        Text("还没有录音。用手机或 Apple Watch 录下要交给 Persona 的内容。")
                            .font(Theme.Font.secondary).foregroundStyle(.secondary)
                    }
                    ForEach(library.entries) { entry in
                        Button { opened = entry.id } label: {
                            HStack(spacing: Theme.Space.m) {
                                Image(systemName: Theme.Icon.recording).foregroundStyle(Theme.accent)
                                VStack(alignment: .leading, spacing: Theme.Space.xs) {
                                    Text(entry.displayTitle).font(Theme.Font.body).lineLimit(1)
                                    Text("\(entry.sourceTitle) · \(RecordingFormat.clock(entry.duration)) · \(status(entry))")
                                        .font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                                }
                                Spacer(minLength: Theme.Space.s)
                                Image(systemName: Theme.Icon.disclosure).foregroundStyle(.secondary)
                            }
                        }
                        .buttonStyle(.plain)
                        .accessibilityIdentifier("recording-row")
                    }
                }
                if let error = library.error { Text(error).font(Theme.Font.secondary).foregroundStyle(Theme.danger) }
            }
            .themedPage()
            .navigationTitle("录音上下文")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("关闭") { dismiss() }.lineLimit(1)
                }
            }
            .navigationDestination(isPresented: Binding(get: { opened != nil }, set: { if !$0 { opened = nil } })) {
                if let id = opened { detail(id) }
            }
            .confirmationDialog("删除这条录音和文字稿？", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), titleVisibility: .visible) {
                Button("删除", role: .destructive) {
                    guard let id = deleting else { return }
                    transcription.cancel(id)
                    do { try library.remove(id); opened = nil } catch { failure = "录音没有删除，请稍后重试" }
                    deleting = nil
                }
            }
            .alert("录音操作未完成", isPresented: Binding(get: { failure != nil }, set: { if !$0 { failure = nil } })) {
                Button("知道了") { failure = nil }.lineLimit(1)
            } message: { Text(failure ?? "") }
            .task { transcription.resumePending() }
            #if DEBUG
            .onAppear { opened = RecordingSamples.installIfAsked() }
            #endif
        }
        #if os(iOS) || os(visionOS)
        .onAppear { PhoneRecordingReceiver.shared.start() }
        #endif
    }

    #if os(iOS) || os(visionOS)
    @ViewBuilder private var captureControls: some View {
        if capture.state == .idle {
            Picker("录音语言", selection: $locale) {
                Text("中文").tag("zh-CN")
                Text("英语").tag("en-US")
                Text("日语").tag("ja-JP")
            }.font(Theme.Font.body)
            Button { Task { await capture.start(locale: locale) } } label: {
                Label("开始录音", systemImage: Theme.Icon.recording).lineLimit(1)
            }.accessibilityIdentifier("recording-start")
        } else if capture.state == .starting {
            ProgressView("等待麦克风权限").font(Theme.Font.secondary)
            Button("取消") { capture.cancelStarting() }.lineLimit(1)
        } else {
            HStack(spacing: Theme.Space.s) {
                Text(RecordingFormat.clock(capture.elapsed)).font(Theme.Font.title.monospacedDigit())
                Spacer()
                Text(capture.state == .paused ? "已暂停" : "正在录音").font(Theme.Font.secondary).foregroundStyle(Theme.danger)
            }
            HStack(spacing: Theme.Space.m) {
                Button { capture.togglePause() } label: {
                    Label(capture.state == .paused ? "继续" : "暂停", systemImage: capture.state == .paused ? Theme.Icon.run : Theme.Icon.pause).lineLimit(1)
                }.themedButton()
                Button { capture.addMark() } label: { Label("书签", systemImage: Theme.Icon.pin).lineLimit(1) }.themedButton()
                Button { opened = capture.stop() } label: { Label("结束", systemImage: Theme.Icon.interrupt).lineLimit(1) }
                    .themedButton().tint(Theme.danger).accessibilityIdentifier("recording-stop")
            }
            Text("关闭此页或锁屏仍会继续录音，结束时请点「结束」。").font(Theme.Font.caption).foregroundStyle(.secondary)
        }
        if let error = capture.error { Text(error).font(Theme.Font.secondary).foregroundStyle(Theme.danger) }
    }
    #endif

    private func status(_ entry: RecordingEntry) -> String {
        if entry.active { return "录音中" }
        if let error = transcription.errors[entry.id] { return error }
        if transcription.current == entry.id { return "转写中" }
        if !entry.receivedAll { return entry.expectedSegments == 0 ? "没有录到音频" : "等待手表回传" }
        if entry.transcribedAll { return entry.context == nil ? "没有识别到语音" : "可作为上下文" }
        return "等待本机转写"
    }

    @ViewBuilder private func detail(_ id: UUID) -> some View {
        if let entry = library.entry(id) {
            List {
                Section {
                    Text(entry.displayTitle).font(Theme.Font.heading)
                    Text("\(entry.sourceTitle) · \(RecordingFormat.clock(entry.duration)) · \(status(entry))")
                        .font(Theme.Font.secondary).foregroundStyle(.secondary)
                    if transcription.current == id { ProgressView().tint(Theme.accent) }
                }
                if let ref = entry.context {
                    Section {
                        Button { pick(ref); dismiss() } label: {
                            Label("作为上下文", systemImage: Theme.Icon.addContext).lineLimit(1)
                        }.accessibilityIdentifier("recording-attach")
                    } footer: { Text("只添加文字稿，不上传原始音频，也不会自动发送。") }
                }
                Section("文字稿") {
                    if entry.sentences.isEmpty {
                        Text("还没有文字稿。").font(Theme.Font.secondary).foregroundStyle(.secondary)
                    }
                    ForEach(Array(entry.sentences.enumerated()), id: \.offset) { _, sentence in
                        VStack(alignment: .leading, spacing: Theme.Space.xs) {
                            Text(RecordingFormat.clock(sentence.start)).font(Theme.Font.caption.monospacedDigit()).foregroundStyle(.secondary)
                            Text(sentence.text).font(Theme.Font.body).textSelection(.enabled)
                        }
                    }
                }
                if !entry.active {
                    Section {
                        Picker("重新转写语言", selection: Binding(get: { entry.locale }, set: { transcription.setLocale(id, $0) })) {
                            Text("中文").tag("zh-CN"); Text("英语").tag("en-US"); Text("日语").tag("ja-JP")
                        }.disabled(transcription.current == id)
                        if transcription.current == id {
                            Button("停止转写") { transcription.cancel(id) }.lineLimit(1)
                        } else {
                            Button("继续转写") { transcription.enqueue(id) }.lineLimit(1)
                        }
                        Button("删除录音", role: .destructive) { deleting = id }.lineLimit(1)
                    }
                }
            }.themedPage().navigationTitle("录音预览")
        }
    }
}

extension View {
    func recordingContextSheet(isPresented: Binding<Bool>, pick: @escaping (ContextRef) -> Void) -> some View {
        sheet(isPresented: isPresented) { RecordingContextSheet(pick: pick) }
        #if DEBUG
            .onAppear { if UserDefaults.standard.bool(forKey: "openRecording") { isPresented.wrappedValue = true } }
        #endif
    }
}
