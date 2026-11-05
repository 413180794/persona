#if os(iOS) || os(visionOS)
import SwiftUI
#if os(iOS)
import AVFoundation
#endif

/// 知识库 › 录音 (docs/recording.md): start a recording, see this device's recordings (the watch's arrive here) with
/// where each one is (recording, transcribing, synced), open one for its transcript. The text goes to the knowledge base
/// by itself (`RecordingSync`); nothing is added to a composer.
struct RecordingsSection: View {
    @State private var library = RecordingLibrary.shared
    @State private var transcription = RecordingTranscription.shared
    @State private var sync = RecordingSync.shared

    var body: some View {
        Section {
            RecordingCaptureControls()
        } footer: {
            Text("音频只留在本机，手表录音会回传这里。本机转写后文字自动同步到知识库，保留最近 \(sync.store.days) 天，助手和 Agent 用知识库搜索就能找到。")
        }
        Section {
            if library.entries.isEmpty {
                EmptyStateView(symbol: Theme.Icon.recording, title: String(localized: "还没有录音"),
                               message: String(localized: "用手机或 Apple Watch 录下会议、口述和现场的内容，转写好的文字会进知识库。"))
                    .listRowBackground(Color.clear)
            }
            ForEach(library.entries) { entry in
                NavigationLink { RecordingDetailView(id: entry.id) } label: { RecordingRow(entry: entry) }
                    .accessibilityIdentifier("recording-row")
            }
        } header: {
            if !library.entries.isEmpty {
                HStack(spacing: Theme.Space.sm) {
                    Text("最近的录音")
                    Text("\(library.entries.count)").foregroundStyle(Color.secondary.opacity(0.7)).monospacedDigit()
                }
            }
        } footer: {
            if let problem = sync.problem ?? library.error {
                Label(problem, systemImage: Theme.Icon.error).foregroundStyle(Theme.danger)
                    .accessibilityIdentifier("recording-sync-problem")
            }
        }
        .task { PhoneRecordingReceiver.shared.start(); transcription.resumePending(); sync.kick(reconcile: true) }
    }
}

/// One recording: where it came from, how long, and where it is on its way to the knowledge base.
struct RecordingRow: View {
    var entry: RecordingEntry

    var body: some View {
        HStack(spacing: Theme.Space.m) {
            Image(systemName: Theme.Icon.recording).font(Theme.Font.heading).foregroundStyle(Theme.accent)
                .scaledFrame(width: Theme.Size.icon)
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                Text(entry.displayTitle).font(Theme.Font.body).lineLimit(1)
                HStack(spacing: Theme.Space.xs) {
                    Text("\(entry.sourceTitle) · \(RecordingFormat.clock(entry.duration)) ·")
                    RecordingStatusText(entry: entry)
                }
                .font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
            }
        }
    }
}

/// 录音中 / 转写中 / 已同步…: the one word for where a recording is.
struct RecordingStatusText: View {
    var entry: RecordingEntry
    @State private var transcription = RecordingTranscription.shared
    @State private var sync = RecordingSync.shared

    var body: some View {
        let (text, tone) = Self.status(entry, transcription: transcription, sync: sync)
        Text(text).foregroundStyle(tone).accessibilityIdentifier("recording-status")
    }

    static func status(_ entry: RecordingEntry, transcription: RecordingTranscription, sync: RecordingSync) -> (String, Color) {
        if entry.active { return (String(localized: "录音中"), Theme.danger) }
        if let error = transcription.errors[entry.id] { return (error, Theme.danger) }
        if transcription.current == entry.id { return (String(localized: "转写中"), Theme.accent) }
        if !entry.receivedAll { return (entry.expectedSegments == 0 ? String(localized: "没有录到音频") : String(localized: "等待手表回传"), .secondary) }
        switch sync.status(entry) {
        case .synced: return (entry.transcribedAll ? String(localized: "已同步到知识库") : String(localized: "已同步一部分"), Theme.positive)
        case .uploading: return (String(localized: "正在同步"), Theme.accent)
        case .rejected: return (String(localized: "知识库没有收下"), Theme.danger)
        case .expired: return (String(localized: "已过保留期"), .secondary)
        case .pending: return (entry.transcribedAll ? String(localized: "等待同步") : String(localized: "等待转写"), .secondary)
        case .waiting: return (entry.transcribedAll ? String(localized: "没有识别到语音") : String(localized: "等待转写"), .secondary)
        }
    }
}

/// Start / pause / bookmark / stop for a recording on this phone (the watch has its own).
struct RecordingCaptureControls: View {
    @State private var capture = RecordingCapture.shared
    @State private var locale = "zh-CN"
    #if os(iOS)
    @AppStorage(RecordingInput.storageKey) private var inputRaw = RecordingInput.phone.rawValue
    @State private var hasBluetooth = false
    #endif

    var body: some View {
        #if os(iOS) // Bluetooth input is iPhone-only (`RecordingInput`); visionOS records with its own microphone
        controls
            .task { // the 麦克风 picker shows only while a Bluetooth device (glasses, headset) is on the route
                refreshBluetooth()
                for await _ in NotificationCenter.default.notifications(named: AVAudioSession.routeChangeNotification) { refreshBluetooth() }
            }
        #else
        controls
        #endif
    }

    /// The microphone a recording starts with: the Bluetooth one only when chosen and still connected.
    private var input: RecordingInput {
        #if os(iOS)
        hasBluetooth ? (RecordingInput(rawValue: inputRaw) ?? .phone) : .phone
        #else
        .phone
        #endif
    }

    @ViewBuilder private var controls: some View {
        if capture.state == .idle {
            Picker("录音语言", selection: $locale) {
                Text("中文").tag("zh-CN")
                Text("英语").tag("en-US")
                Text("日语").tag("ja-JP")
            }.font(Theme.Font.body)
            #if os(iOS)
            if hasBluetooth {
                Picker("麦克风", selection: $inputRaw) {
                    ForEach(RecordingInput.allCases, id: \.rawValue) { Text($0.title).tag($0.rawValue) }
                }.font(Theme.Font.body)
            }
            #endif
            Button { let chosen = input; Task { await capture.start(locale: locale, input: chosen) } } label: {
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
            #if os(iOS)
            if hasBluetooth, RecordingInput(rawValue: inputRaw) == .bluetooth, let name = capture.inputName {
                Text("麦克风：\(name)").font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
            }
            #endif
            HStack(spacing: Theme.Space.m) {
                Button { capture.togglePause() } label: {
                    Label(capture.state == .paused ? "继续" : "暂停", systemImage: capture.state == .paused ? Theme.Icon.run : Theme.Icon.pause).lineLimit(1)
                }.themedButton()
                Button { capture.addMark() } label: { Label("书签", systemImage: Theme.Icon.pin).lineLimit(1) }.themedButton()
                Button { _ = capture.stop() } label: { Label("结束", systemImage: Theme.Icon.interrupt).lineLimit(1) }
                    .themedButton().tint(Theme.danger).accessibilityIdentifier("recording-stop")
            }
            Text("离开此页或锁屏仍会继续录音，结束时请点「结束」。").font(Theme.Font.caption).foregroundStyle(.secondary)
        }
        if let error = capture.error { Text(error).font(Theme.Font.secondary).foregroundStyle(Theme.danger) }
        if let notice = capture.notice { Text(notice).font(Theme.Font.secondary).foregroundStyle(.secondary) }
    }

    #if os(iOS)
    private func refreshBluetooth() {
        hasBluetooth = RecordingInput.bluetoothConnected
    }
    #endif
}

/// One recording's transcript, with re-transcribing in another language and delete (here and in the knowledge base).
struct RecordingDetailView: View {
    var id: UUID
    @Environment(\.dismiss) private var dismiss
    @State private var library = RecordingLibrary.shared
    @State private var transcription = RecordingTranscription.shared
    @State private var sync = RecordingSync.shared
    @State private var deleting = false
    @State private var failure: String?

    var body: some View {
        Group {
            if let entry = library.entry(id) { content(entry) } else {
                EmptyStateView(symbol: Theme.Icon.recording, title: String(localized: "录音已删除"), message: String(localized: "这条录音和它在知识库里的文字都已经删掉了。"))
            }
        }
        .themedPage()
        .navigationTitle("录音")
        .navigationBarTitleDisplayMode(.inline)
        .confirmationDialog("删除这条录音？", isPresented: $deleting, titleVisibility: .visible) {
            Button("删除", role: .destructive) {
                transcription.cancel(id)
                do { try sync.delete(id); dismiss() } catch { failure = String(localized: "录音没有删除，请稍后重试") }
            }
        } message: { Text("本机的音频和知识库里的文字都会删掉。") }
        .alert("录音操作未完成", isPresented: Binding(get: { failure != nil }, set: { if !$0 { failure = nil } })) {
            Button("知道了") { failure = nil }.lineLimit(1)
        } message: { Text(failure ?? "") }
    }

    private func content(_ entry: RecordingEntry) -> some View {
        List {
            Section {
                Text(entry.displayTitle).font(Theme.Font.heading)
                HStack(spacing: Theme.Space.xs) {
                    Text("\(entry.sourceTitle) · \(RecordingFormat.clock(entry.duration)) ·")
                    RecordingStatusText(entry: entry)
                }
                .font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1)
                if !entry.marks.isEmpty {
                    Label(String(localized: "书签 \(entry.marks.map(RecordingFormat.clock).joined(separator: "、"))"), systemImage: Theme.Icon.pin)
                        .font(Theme.Font.secondary).foregroundStyle(.secondary)
                }
                if transcription.current == id { ProgressView().tint(Theme.accent) }
            } footer: {
                Text("只有文字和时间戳进知识库，原始音频不上传；过了保留期（\(sync.store.days) 天）本机和知识库里都会清掉。")
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
                    } else if !entry.transcribedAll {
                        Button("继续转写") { transcription.enqueue(id) }.lineLimit(1)
                    }
                    Button("删除录音", role: .destructive) { deleting = true }.lineLimit(1)
                        .accessibilityIdentifier("recording-delete")
                }
            }
        }
    }
}
#endif
