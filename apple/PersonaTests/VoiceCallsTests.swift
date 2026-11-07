import Foundation
import Testing
@testable import Persona

/// Who owns the app's one call (docs/assistant/voice.md 6.3, 6.5, 6.10「VoiceCalls 的所有权」): one at a time, another
/// conversation's call ends the one before, the background and leaving the page end only the phone's.
@MainActor
struct VoiceCallsTests {
    @Test func oneCallAtATimeAndAnotherConversationEndsTheFirst() async throws {
        let rig = VoiceRig()
        let calls = VoiceCalls()
        await calls.start(rig.key, extensionID: "voice", surface: .phone, deps: rig.deps)
        let first = try #require(calls.current)
        #expect(calls.isBound(to: rig.key))

        // The same conversation again: the same call, no second socket.
        await calls.start(rig.key, extensionID: "voice", surface: .phone, deps: rig.deps)
        #expect(calls.current === first && rig.sockets.count == 1)

        let other = SessionKey(group: rig.key.group, machineID: "m2", sessionID: "s9")
        await calls.start(other, extensionID: "voice", surface: .phone, deps: rig.deps)
        #expect(first.phase == .ended(.replaced))
        #expect(calls.current !== first && calls.isBound(to: other) && !calls.isBound(to: rig.key))
        #expect(rig.sockets.count == 2 && rig.sockets[0].closed)
        #expect(rig.sockets[1].start?["target"] as? [String: String] == ["machine_id": "m2", "session_id": "s9"])
    }

    @Test func theBackgroundEndsOnlyThePhonesCall() async throws {
        let rig = VoiceRig()
        let calls = VoiceCalls()
        await calls.start(rig.key, extensionID: "voice", surface: .carPlay, deps: rig.deps)
        calls.appDidEnterBackground()
        #expect(calls.current?.phase == .connecting, "a call from the car goes on")

        let phone = VoiceRig()
        let phoneCalls = VoiceCalls()
        await phoneCalls.start(phone.key, extensionID: "voice", surface: .phone, deps: phone.deps)
        phoneCalls.appDidEnterBackground()
        let ended = try #require(phoneCalls.current)
        #expect(ended.phase == .ended(.background))
        #expect(phoneCalls.isBound(to: phone.key), "继续 stays on the page")
    }

    @Test func aSurfaceEndsOnlyItsOwnCallButTheUserEndsAny() async {
        let rig = VoiceRig()
        let calls = VoiceCalls()
        await calls.start(rig.key, extensionID: "voice", surface: .phone, deps: rig.deps)
        calls.end(.left, from: .carPlay)
        #expect(calls.current?.phase == .connecting, "the car does not stop the phone's call")
        calls.end(.user, from: .carPlay)
        #expect(calls.current == nil, "结束 tapped ends it whoever started it, and nothing stays")
    }

    @Test func leavingThePageEndsItsCallAndItsContinueRow() async {
        let rig = VoiceRig()
        let calls = VoiceCalls()
        await calls.start(rig.key, extensionID: "voice", surface: .phone, deps: rig.deps)
        calls.leave(SessionKey(machineID: "m1", sessionID: "other"))
        #expect(calls.isBound(to: rig.key), "another page leaving changes nothing")
        let call = calls.current
        calls.leave(rig.key)
        #expect(call?.phase == .ended(.left))
        #expect(calls.current == nil)
        #expect(rig.microphone.claim(UUID()))
    }

    @Test func leavingAPageDoesNotEndTheCarsCall() async {
        let rig = VoiceRig()
        let calls = VoiceCalls()
        await calls.start(rig.key, extensionID: "voice", surface: .carPlay, deps: rig.deps)
        calls.leave(rig.key)
        #expect(calls.current?.phase == .connecting)
    }

    @Test func continueOnAnEndedCallPicksItUp() async throws {
        let rig = VoiceRig()
        let calls = VoiceCalls()
        await calls.start(rig.key, extensionID: "voice", surface: .phone, deps: rig.deps)
        rig.socket?.ready(rig.key)
        rig.audio.emit(.interrupted)
        let call = try #require(calls.current)
        #expect(call.phase == .ended(.interrupted))
        await calls.resume(rig.key)
        #expect(calls.current === call && call.phase == .connecting && rig.sockets.count == 2)
        // Starting the same conversation again does the same.
        call.end(.lost)
        await calls.start(rig.key, extensionID: "voice", surface: .phone, deps: rig.deps)
        #expect(calls.current === call && rig.sockets.count == 3)
    }
}
