import AVFoundation
import BeautifulUI
import Speech
import SwiftUI

/// The composer's box, shared by an agent's chat (`Composer`) and a new assistant conversation (`StarterComposer`):
/// BeautifulUI's prompt bar laid out as a toolbar. The draft is on top; under it + (photos, camera, files), @, /, the
/// model, the mic, and send — or, while the agent works and nothing is typed, a red stop.
/// The callers keep their own state: the chat's drafts, SendSafety and uploads stay in `Composer`.
struct PromptComposer: View {
    var placeholder: String
    @Binding var text: String
    @Binding var selection: TextSelection?
    var focused: FocusState<Bool>.Binding
    /// Photos or quoted context the caller holds: sendable without text.
    var hasAttachments: Bool
    var disabled = false
    /// Set while the agent works.
    var stop: (() -> Void)?
    var attach: [PromptSource]
    /// Built when the @ menu opens (it reads the chat), not on every keystroke.
    var mentions: () -> [PromptSource] = { [] }
    var commands: [PromptCommand] = []
    var models: [PromptModel] = []
    /// The model shown; `pickModel` gets the one picked from the menu.
    var modelID = ""
    var pickModel: (String) -> Void = { _ in }
    var onAttach: (PromptSource) -> Void
    /// True when the caller took the mention (a quoted session); otherwise its `insert` is typed.
    var onMention: (PromptSource) -> Bool = { _ in false }
    /// True when the caller took the command (/clear asks first); otherwise its `insert` is typed.
    var onCommand: (PromptCommand) -> Bool = { _ in false }
    var send: () -> Void

    @State private var bar = PromptBarModel(selectedModelID: "")
    @State private var mentionRows: [PromptSource] = []
    @State private var dictation = Dictation()

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("PromptComposer")
        #endif
        var o = PromptBar.Options()
        o.layout = .stacked
        o.attachSources = attach
        o.showsMentionButton = true
        o.showsCommandButton = !commands.isEmpty
        o.controlSize = 34
        o.inputSize = Theme.Level.body.size / 1.2 // body size once the theme's ×1.2 applies
        o.cornerRadius = Theme.Radius.panel
        o.drawsSurface = false // the style's own surface (glass, paper…), below
        o.canSendExtra = hasAttachments
        o.text = $text
        o.selection = $selection
        o.focus = focused
        o.onStop = stop
        o.onDictation = toggleDictation
        o.onAttachSource = onAttach
        o.onPickSource = onMention
        o.onPickCommand = onCommand
        return PromptBar(model: bar, sources: mentionRows, commands: commands, models: models, placeholder: placeholder,
                         options: o, onSend: { _ in if !disabled { stopDictation(); send() } })
            .surface(interactive: true, in: .rect(cornerRadius: Theme.Radius.panel, style: .continuous))
            .onAppear { bar.selectedModelID = modelID }
            .onChange(of: modelID) { _, id in bar.selectedModelID = id }
            .onChange(of: bar.selectedModelID) { _, id in if id != modelID { pickModel(id) } }
            .onChange(of: bar.token?.kind == .mention) { _, open in if open { mentionRows = mentions() } }
            .onChange(of: dictation.isListening) { _, on in bar.isListening = on }
            .onDisappear { stopDictation() }
            #if DEBUG
            .task { await debugMenu() }
            #endif
    }

    private func toggleDictation() {
        if dictation.isListening { stopDictation(); return }
        let base = text.isEmpty || text.last?.isWhitespace == true ? text : text + " "
        Task {
            if let problem = await dictation.start(update: { heard in text = base + heard }) {
                AppModel.shared.toast = Toast(problem, style: .error)
            }
        }
    }

    private func stopDictation() { if dictation.isListening { dictation.stop() } }

    #if DEBUG
    /// Screenshots: `-showPromptMenu mention|command|attach|model` opens that menu a few seconds in.
    private func debugMenu() async {
        guard let which = UserDefaults.standard.string(forKey: "showPromptMenu") else { return }
        try? await Task.sleep(for: .seconds(5))
        switch which {
        case "mention": mentionRows = mentions(); text = "@"
        case "command": text = "/"
        case "attach": bar.isSourceMenuOpen = true
        default: bar.isModelMenuOpen = true
        }
    }
    #endif
}

/// What the composer's menus offer.
enum PromptItems {
    static func attachments(camera: Bool) -> [PromptSource] {
        var out = [PromptSource(id: "photo", name: "照片", detail: "从相册选，最多 4 张", icon: .symbol("photo.on.rectangle"), kind: .attach)]
        if camera { out.append(PromptSource(id: "camera", name: "拍照", detail: "用相机拍一张", icon: .symbol("camera"), kind: .attach)) }
        out.append(PromptSource(id: "file", name: "文件", detail: "传到那台电脑，路径插进输入框", icon: .symbol("doc"), kind: .attach))
        out.append(PromptSource(id: "recording", name: "录音上下文", detail: "手机或手表录音，本机转写后确认", icon: .symbol(Theme.Icon.recording), kind: .attach))
        return out
    }

    /// Persona' quick messages under short "/" words (picking one types its message, sent with send), then Claude
    /// Code's own commands.
    static func commands(clear: Bool) -> [PromptCommand] {
        var out = SlashCommand.all.map { PromptCommand($0.word, detail: $0.detail, insert: $0.text) }
        if clear { out.append(PromptCommand("/clear", detail: "清空上下文，开新会话")) }
        return out
    }

    /// Claude Code's `/model` aliases: picking one sends `/model <alias>` to the session.
    static let claudeModels: [PromptModel] = [
        PromptModel(id: "default", name: "默认", tag: "账号默认"),
        PromptModel(id: "opus", name: "Opus", tag: "最强"),
        PromptModel(id: "sonnet", name: "Sonnet", tag: "均衡"),
        PromptModel(id: "haiku", name: "Haiku", tag: "最快"),
    ]

    /// The alias a session's model reads as ("claude-opus-…" → opus).
    static func claudeModelID(_ model: String?) -> String {
        let m = (model ?? "").lowercased()
        return ["opus", "sonnet", "haiku"].first { m.contains($0) } ?? "default"
    }

    /// Files the chat's tools read or wrote, newest first: picking one types `@path` (relative to the session's
    /// folder), which Claude Code and Codex read as that file.
    static func files(_ items: [Item], cwd: String) -> [PromptSource] {
        var seen = Set<String>()
        var out: [PromptSource] = []
        for it in items.reversed() where it.kind == .toolUse {
            guard let path = it.toolInput?["file_path"]?.string ?? it.toolInput?["notebook_path"]?.string,
                  !path.isEmpty, seen.insert(path).inserted else { continue }
            let rel = !cwd.isEmpty && path.hasPrefix(cwd + "/") ? String(path.dropFirst(cwd.count + 1)) : path
            let name = (rel as NSString).lastPathComponent
            let dir = (rel as NSString).deletingLastPathComponent
            out.append(PromptSource(id: "file:" + path, name: name, detail: dir.isEmpty ? "本会话用过的文件" : dir,
                                    icon: .symbol("doc.text"), insert: "@\(rel) "))
            if out.count == 12 { break }
        }
        return out
    }

    /// Other agents' sessions, most recently active first: picking one quotes it (title, folder, state, its last
    /// words) as context for the message.
    static func sessions(_ all: [Session], except key: SessionKey?) -> [PromptSource] {
        all.filter { $0.key != key && !$0.assistant }
            .sorted { ($0.updatedAt ?? .distantPast) > ($1.updatedAt ?? .distantPast) }
            .prefix(8)
            .map { s in
                PromptSource(id: "session:" + s.id, name: s.displayTitle, detail: "\(s.agentLabel) · \(s.folderName)",
                             icon: .symbol("bubble.left.and.text.bubble.right"))
            }
    }

    /// The quote for a picked session (`ContextRef`, sent as a `<jarvis-context>` tag before the message).
    static func quote(_ source: PromptSource, in all: [Session]) -> ContextRef? {
        guard source.id.hasPrefix("session:"), let s = all.first(where: { "session:" + $0.id == source.id }) else { return nil }
        var lines = ["另一个会话：\(s.displayTitle)（\(s.agentLabel)）", "目录：\(s.cwd)"]
        if let last = s.lastText?.trimmingCharacters(in: .whitespacesAndNewlines), !last.isEmpty {
            lines.append("它最近说：\(String(last.prefix(600)))")
        }
        return ContextRef(kind: "context", title: s.displayTitle, url: "jarvis://session/\(s.machineID)/\(s.sessionID)",
                          text: lines.joined(separator: "\n"))
    }
}

/// Voice to text for the composer's mic: what is heard goes into the draft as it comes; a second tap ends it.
@Observable
final class Dictation {
    private(set) var isListening = false
    @ObservationIgnored private var engine: AVAudioEngine?
    @ObservationIgnored private var request: SFSpeechAudioBufferRecognitionRequest?
    @ObservationIgnored private var task: SFSpeechRecognitionTask?
    @ObservationIgnored private var update: ((String) -> Void)?
    @ObservationIgnored private let microphoneOwner = UUID()
    @ObservationIgnored private var listeningRun = UUID()

    /// Starts listening; `update` gets the whole transcript so far. Returns what went wrong, if it could not start.
    func start(update: @escaping (String) -> Void) async -> String? {
        #if os(iOS)
        guard RecordingCapture.shared.state == .idle else { return "录音正在进行，请先结束录音再听写" }
        #endif
        guard await Self.allowed() else { return "没有麦克风或语音识别的权限，可以到 设置 › Persona 打开" }
        #if os(iOS)
        guard RecordingCapture.shared.state == .idle else { return "录音正在进行，请先结束录音再听写" }
        #endif
        guard let recognizer = SFSpeechRecognizer(locale: Locale(identifier: "zh-CN")), recognizer.isAvailable else {
            return "语音识别现在用不了"
        }
        guard RecordingMicrophone.shared.claim(microphoneOwner) else { return "麦克风正在使用，请先结束录音或听写" }
        #if os(iOS) // a Mac has no audio session to configure: the engine takes the input as it is
        do {
            let session = AVAudioSession.sharedInstance()
            try session.setCategory(.record, mode: .measurement, options: .duckOthers)
            try session.setActive(true, options: .notifyOthersOnDeactivation)
        } catch {
            RecordingMicrophone.shared.release(microphoneOwner)
            return "麦克风打不开"
        }
        #endif
        let engine = AVAudioEngine()
        let request = SFSpeechAudioBufferRecognitionRequest()
        request.shouldReportPartialResults = true
        Self.tap(engine.inputNode, into: request)
        engine.prepare()
        do { try engine.start() } catch {
            engine.inputNode.removeTap(onBus: 0)
            #if os(iOS)
            try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
            #endif
            RecordingMicrophone.shared.release(microphoneOwner)
            return "麦克风打不开"
        }
        self.engine = engine
        self.request = request
        self.update = update
        isListening = true
        listeningRun = UUID(); let run = listeningRun
        task = Self.recognize(recognizer, request) { [weak self] heard, done in
            Task { @MainActor in
                guard let self, self.listeningRun == run else { return }
                if let heard { self.update?(heard) }
                if done { self.stop() }
            }
        }
        return nil
    }

    func stop() {
        guard RecordingMicrophone.shared.release(microphoneOwner) else { return }
        listeningRun = UUID()
        engine?.stop()
        engine?.inputNode.removeTap(onBus: 0)
        request?.endAudio()
        task?.finish()
        engine = nil
        request = nil
        task = nil
        update = nil
        isListening = false
        #if os(iOS)
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        #endif
    }

    private static func allowed() async -> Bool {
        let speech = await withCheckedContinuation { c in
            // Answered on a background queue: `@Sendable` keeps the block off the main actor (BackgroundCallbackTests).
            SFSpeechRecognizer.requestAuthorization { @Sendable in c.resume(returning: $0) }
        }
        guard speech == .authorized else { return false }
        return await AVAudioApplication.requestRecordPermission()
    }

    // The audio tap and the recognizer call back off the main thread: their closures are made here, outside the
    // main actor, so they are not taken for main-actor code.
    nonisolated private static func tap(_ input: AVAudioInputNode, into request: SFSpeechAudioBufferRecognitionRequest) {
        input.installTap(onBus: 0, bufferSize: 1024, format: input.outputFormat(forBus: 0)) { buffer, _ in
            request.append(buffer)
        }
    }

    nonisolated private static func recognize(_ recognizer: SFSpeechRecognizer, _ request: SFSpeechAudioBufferRecognitionRequest,
                                              report: @escaping @Sendable (String?, Bool) -> Void) -> SFSpeechRecognitionTask {
        recognizer.recognitionTask(with: request) { result, error in
            report(result?.bestTranscription.formattedString, error != nil || result?.isFinal == true)
        }
    }
}
