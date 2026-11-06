import Foundation
import Testing
@testable import Persona

@MainActor struct BotmuxSwitchTests {
    private struct Boom: Error {}

    @Test func loadsAndSaves() async {
        var saved: [Bool] = []
        let m = BotmuxSwitchModel(deps: .init(load: { true }, save: { saved.append($0); return $0 }))
        #expect(m.on == nil)
        await m.load()
        #expect(m.on == true)
        await m.set(false)
        #expect(m.on == false && saved == [false] && m.error == nil && !m.saving)
    }

    @Test func failedSavePutsTheOldValueBack() async {
        let m = BotmuxSwitchModel(deps: .init(load: { true }, save: { _ in throw Boom() }))
        await m.load()
        await m.set(false)
        #expect(m.on == true && m.error != nil)
    }

    @Test func olderHubShowsNoSwitch() async {
        var saved = 0
        let m = BotmuxSwitchModel(deps: .init(load: { throw Boom() }, save: { saved += 1; return $0 }))
        await m.load()
        #expect(m.on == nil)
        await m.set(false)
        #expect(m.on == nil && saved == 0)
    }
}
