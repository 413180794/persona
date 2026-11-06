import AVFoundation
import Speech

// The on-device engines and timestamp-based sentence splitting come from Hourhand's SpeechEngines.swift, without the cloud path.

nonisolated protocol RecordingSpeechEngine: Sendable {
    /// Stored in `Recording.engine`: "analyzer", "dictation", "speech", "cloud".
    var id: String { get }
    var isCloud: Bool { get }
    func transcribe(_ url: URL, locale: Locale) async throws -> [RecordingSentence]
}

nonisolated enum RecordingSpeechError: LocalizedError {
    case unsupportedLanguage
    case noSpeechPermission
    case modelUnavailable
    case empty

    var errorDescription: String? {
        switch self {
        case .unsupportedLanguage: String(localized: "This device has no on-device transcription model for the selected language")
        case .noSpeechPermission: String(localized: "Speech recognition is off. Allow it for Persona in Settings.")
        case .modelUnavailable: String(localized: "Couldn't download the on-device speech model. Check your network and try again.")
        case .empty: String(localized: "No speech was recognized in the recording")
        }
    }
}

/// Groups timed words / tokens into sentences: ends at terminal punctuation, on a long pause, or when very long.
nonisolated enum RecordingSentenceBuilder {
    static let terminators: Set<Character> = [".", "?", "!", "。", "？", "！", "…", "؟", "।"] // i18n-ignore

    /// Languages written without spaces between words.
    static func usesSpaces(_ locale: Locale) -> Bool {
        !["zh", "ja", "th", "yue", "lo", "km", "my", "bo"].contains(locale.language.languageCode?.identifier ?? "")
    }

    static func build(_ tokens: [(text: String, start: Double, end: Double)]) -> [RecordingSentence] {
        var out: [RecordingSentence] = []
        var text = "", start = 0.0, end = 0.0
        func flush() {
            let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
            if !t.isEmpty { out.append(RecordingSentence(start: start, end: max(end, start), text: t)) }
            text = ""
        }
        for token in tokens {
            if !text.isEmpty, token.start - end > 1.5 { flush() }
            if text.isEmpty { start = token.start }
            text += token.text
            end = token.end
            let trimmed = token.text.trimmingCharacters(in: .whitespaces)
            if let last = trimmed.last, terminators.contains(last) { flush() } else if text.count > 320 { flush() }
        }
        flush()
        return out
    }
}

// MARK: - iOS 26: SpeechAnalyzer

@available(iOS 26.0, macOS 26.0, *)
nonisolated struct RecordingAnalyzerEngine: RecordingSpeechEngine {
    enum Kind { case speech, dictation }
    let kind: Kind
    var id: String { kind == .speech ? "analyzer" : "dictation" }
    var isCloud: Bool { false }

    /// The best on-device module for a locale, or nil if neither supports it.
    static func best(for locale: Locale) async -> (RecordingAnalyzerEngine, Locale)? {
        if SpeechTranscriber.isAvailable, let l = await SpeechTranscriber.supportedLocale(equivalentTo: locale) {
            return (RecordingAnalyzerEngine(kind: .speech), l)
        }
        if let l = await DictationTranscriber.supportedLocale(equivalentTo: locale) {
            return (RecordingAnalyzerEngine(kind: .dictation), l)
        }
        return nil
    }

    private func module(_ locale: Locale) -> any SpeechModule {
        switch kind {
        case .speech:
            SpeechTranscriber(locale: locale, transcriptionOptions: [], reportingOptions: [], attributeOptions: [.audioTimeRange])
        case .dictation:
            DictationTranscriber(locale: locale, contentHints: [], transcriptionOptions: [.punctuation], reportingOptions: [], attributeOptions: [.audioTimeRange])
        }
    }

    /// Downloads the on-device model if needed (Apple's asset, shared with other apps).
    static func ensureAssets(_ modules: [any SpeechModule]) async throws {
        if let request = try await AssetInventory.assetInstallationRequest(supporting: modules) {
            try await request.downloadAndInstall()
        }
    }

    func transcribe(_ url: URL, locale: Locale) async throws -> [RecordingSentence] {
        let module = module(locale)
        try await Self.ensureAssets([module])
        let file = try AVAudioFile(forReading: url)
        let analyzer = SpeechAnalyzer(modules: [module], options: .init(priority: .userInitiated, modelRetention: .processLifetime))
        let collector = Task { () throws -> [(String, Double, Double)] in
            var tokens: [(String, Double, Double)] = []
            switch module {
            case let t as SpeechTranscriber:
                for try await result in t.results { tokens += Self.tokens(result.text, fallback: result.range) }
            case let t as DictationTranscriber:
                for try await result in t.results { tokens += Self.tokens(result.text, fallback: result.range) }
            default:
                break
            }
            return tokens
        }
        let tokens = try await withTaskCancellationHandler {
            do {
                if let last = try await analyzer.analyzeSequence(from: file) {
                    try await analyzer.finalizeAndFinish(through: last)
                } else {
                    await analyzer.cancelAndFinishNow()
                }
                return try await collector.value
            } catch {
                await analyzer.cancelAndFinishNow()
                collector.cancel()
                throw error
            }
        } onCancel: {
            collector.cancel()
            Task { await analyzer.cancelAndFinishNow() }
        }
        try Task.checkCancellation()
        return RecordingSentenceBuilder.build(tokens.map { (text: $0.0, start: $0.1, end: $0.2) })
    }

    /// Runs of a result with their audio time ranges.
    private static func tokens(_ text: AttributedString, fallback: CMTimeRange) -> [(String, Double, Double)] {
        var out: [(String, Double, Double)] = []
        for run in text.runs {
            let piece = String(text[run.range].characters)
            if let range = run[AttributeScopes.SpeechAttributes.TimeRangeAttribute.self] {
                out.append((piece, range.start.seconds, range.end.seconds))
            } else if let last = out.last {
                // Punctuation and spaces carry no time: attach to the previous token.
                out[out.count - 1] = (last.0 + piece, last.1, last.2)
            } else {
                out.append((piece, fallback.start.seconds, fallback.start.seconds))
            }
        }
        return out
    }
}
