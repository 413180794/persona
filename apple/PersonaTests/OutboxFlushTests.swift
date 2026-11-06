import Testing
@testable import Persona

/// Which chats' waiting messages go out, by group (#4704): a group's messages go once its own link is up, whatever the
/// hub-level group's link is doing, and a group that is sending already or out of reach keeps no other group back.
struct OutboxFlushTests {
    private let home = SessionKey(machineID: "mac", sessionID: "a")
    private let homeB = SessionKey(machineID: "mac", sessionID: "b")
    private let work = SessionKey(group: "work", machineID: "box", sessionID: "c")

    @Test func aGroupSendsWhileTheHubLevelOneIsDown() {
        let out = OutboxFlush.batches([home, work], ready: { $0 == "work" }, busy: [])
        #expect(out == ["work": [work]])
    }

    @Test func everyGroupThatIsUpGetsItsOwnBatch() {
        let out = OutboxFlush.batches([work, homeB, home], ready: { _ in true }, busy: [])
        #expect(out == ["": [home, homeB], "work": [work]])
    }

    @Test func aGroupSendingAlreadyHoldsBackOnlyItself() {
        let out = OutboxFlush.batches([home, work], ready: { _ in true }, busy: [""])
        #expect(out == ["work": [work]])
    }

    @Test func nothingGoesWhileNoGroupIsUp() {
        #expect(OutboxFlush.batches([home, work], ready: { _ in false }, busy: []).isEmpty)
    }
}
