import Testing
@testable import Jarvis

/// The shell composer's model button (#1091): the short model names and "model · effort".
@MainActor
struct ComposerModesTitleTests {
    @Test func shortModelNames() {
        #expect(SessionModes.modelName("claude-opus-5-5") == "Opus 5.5")
        #expect(SessionModes.modelName("claude-sonnet-4-5-20250929") == "Sonnet 4.5")
        #expect(SessionModes.modelName("claude-haiku-5-5") == "Haiku 5.5")
        #expect(SessionModes.modelName("claude-fable-5-1") == "Fable 5.1")
        #expect(SessionModes.modelName("opus") == "Opus")
        #expect(SessionModes.modelName("opus[1m]") == "Opus")
        #expect(SessionModes.modelName("claude-3-5-sonnet-20241022") == "Sonnet")
        #expect(SessionModes.modelName("openai/gpt-5.5") == "gpt-5.5")
        #expect(SessionModes.modelName("") == nil)
        #expect(SessionModes.modelName(nil) == nil)
    }

    @Test func titles() {
        #expect(SessionModes.composerTitle(model: "Opus 5.5", effort: "高") == .init(model: "Opus 5.5", effort: "高"))
        #expect(SessionModes.composerTitle(model: "Opus 5.5", effort: "高")?.spoken == "Opus 5.5 高") // no 「·」 (#1117)
        #expect(SessionModes.composerTitle(model: "gpt-5.5", effort: nil) == .init(model: "gpt-5.5"))
        #expect(SessionModes.composerTitle(model: "gpt-5.5", effort: nil)?.spoken == "gpt-5.5")
        #expect(SessionModes.composerTitle(model: nil, effort: "很高") == .init(model: "思考", effort: "很高"))
        #expect(SessionModes.composerTitle(model: nil, effort: nil) == nil)
    }
}
