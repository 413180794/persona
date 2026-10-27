import Foundation
import Testing
@testable import Jarvis

struct SyncEventTests {
    @Test func decodesRevs() throws {
        let data = Data(#"{"type":"revs","machine_id":"m1","revs":{"s1":120,"s2":7}}"#.utf8)
        guard case .revs(let machine, let revs) = try #require(ServerEvent.decode(data)) else { Issue.record("not revs"); return }
        #expect(machine == "m1")
        #expect(revs == ["s1": 120, "s2": 7])
    }

    @Test func decodesLark() throws {
        let data = Data(#"{"type":"lark","feed_ids":["a","b"]}"#.utf8)
        guard case .lark(let ids) = try #require(ServerEvent.decode(data)) else { Issue.record("not lark"); return }
        #expect(ids == ["a", "b"])
    }

    @Test func sessionCarriesRev() throws {
        let s = try JSON.decoder.decode(Session.self, from: Data(#"{"session_id":"s","status":"idle","rev":42}"#.utf8))
        #expect(s.rev == 42)
    }

    @Test func lazyImagesLearnTheirRef() {
        var it = Item(id: "u#1", seq: 3, ts: nil, role: "user", kind: .image, text: nil, toolUseID: nil, toolName: nil, toolInput: nil, isError: nil, image: nil)
        var img = ImagePayload(mime: "image/png", data: "")
        img.lazy = true
        it.image = img
        let out = ChatStore.withImageRefs([it], SessionKey(machineID: "m", sessionID: "s"))
        #expect(out[0].image?.ref == "m/s/u#1")
    }
}
