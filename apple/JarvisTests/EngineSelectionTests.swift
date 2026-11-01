import Foundation
import Testing
@testable import Jarvis

@MainActor struct EngineSelectionTests {
    private func machine(_ extra: String = "") throws -> Machine {
        try JSON.decoder.decode(Machine.self, from: Data("{\"id\":\"computer\",\"name\":\"电脑\",\"kind\":\"local\",\"online\":false\(extra)}".utf8))
    }

    @Test func compatibleDecoding() throws {
        let old = try machine()
        #expect(old.engine == .herdr && old.engines.isEmpty)
        let unknown = try machine(",\"engine\":\"future\",\"engines\":[{\"id\":\"herdr\",\"name\":\"herdr\",\"available\":true}]")
        #expect(unknown.engine == .herdr && unknown.engines.isEmpty)
        let current = try machine(",\"engine\":\"herdr-core\",\"engines\":[{\"id\":\"herdr-core\",\"name\":\"Herdr App\",\"available\":true,\"version\":\"1.2\"}]")
        #expect(current.engine == .herdrApp)
        #expect(current.engines.first?.version == "1.2")
        #expect(try JSON.decoder.decode(Machine.self, from: JSON.encoder.encode(current)) == current)
    }

    @Test func pairingTargetAndPreselection() throws {
        var local = try machine()
        var remote = local
        remote.id = "remote"
        remote.kind = "ssh"
        #expect(EngineSelectionModel.pairingMachine([remote, local])?.id == local.id)
        #expect(EngineSelectionModel.pairingMachine([remote])?.id == remote.id)
        #expect(EngineSelectionModel.pairingMachine([]) == nil)
        #expect(EngineSelectionModel.initialChoice(local, pairing: true) == .herdr)
        local.engines = [.init(id: "herdr-core", name: "Herdr App", available: true)]
        #expect(EngineSelectionModel.initialChoice(local, pairing: true) == .herdrApp)
        #expect(EngineSelectionModel.initialChoice(local, pairing: false) == .herdr)
        local.engines.append(.init(id: "herdr", name: "herdr", available: true))
        #expect(EngineSelectionModel.initialChoice(local, pairing: true) == .herdr)
        local.engine = .herdrApp
        local.engines = []
        #expect(EngineSelectionModel.initialChoice(local, pairing: true) == .herdrApp)
    }

    @Test func savesOfflineAndDoesNotLoseAnEditedChoiceToPushes() async throws {
        let hub = FakeHub()
        var offline = try machine()
        offline.engine = .herdrApp
        let response = try JSON.encoder.encode(offline)
        hub.on("PUT", "/api/v1/machines/computer/engine") { req in
            let body = try JSON.decoder.decode([String: String].self, from: #require(req.body))
            #expect(body == ["engine": "herdr-core"])
            return response
        }
        var updated: Machine?
        let model = EngineSelectionModel(machine: try machine(), pairing: false, deps: .init(api: { hub.client }, updated: { updated = $0 }))
        model.choose(.herdrApp)
        model.synchronize(try machine(), pairing: false)
        #expect(model.selected == .herdrApp)
        #expect(await model.save(machineID: "computer"))
        #expect(updated == offline && model.error == nil && !model.saving)
        model.synchronize(try machine(), pairing: false)
        #expect(model.selected == .herdr)
    }

    @Test func failureKeepsChoiceAndAllowsRetry() async throws {
        let hub = FakeHub()
        hub.on("PUT", "/api/v1/machines/computer/engine") { _ in throw APIError(status: 503, message: "暂时无法保存") }
        var updates = 0
        let model = EngineSelectionModel(machine: try machine(), pairing: true, deps: .init(api: { hub.client }, updated: { _ in updates += 1 }))
        model.choose(.herdrApp)
        #expect(!(await model.save(machineID: "computer")))
        #expect(model.selected == .herdrApp && model.error?.contains("暂时无法保存") == true && !model.saving && updates == 0)
        var saved = try machine()
        saved.engine = .herdrApp
        hub.on("PUT", "/api/v1/machines/computer/engine", json: String(decoding: try JSON.encoder.encode(saved), as: UTF8.self))
        #expect(await model.save(machineID: "computer"))
        #expect(updates == 1 && model.error == nil)
    }

    @Test func demoSupportsEngineChangesAndValidation() async throws {
        DemoHub.install()
        DemoHub.shared.reset()
        let api = APIClient(config: .demo)
        let original = try #require(try await api.machines().first { !$0.online })
        let saved = try await api.setEngine(machine: original.id, engine: .herdrApp)
        #expect(saved.engine == .herdrApp && !saved.online && saved.engines.isEmpty)
        #expect(try await api.machines().first { $0.id == original.id }?.engine == .herdrApp)
        do {
            _ = try await api.request("PUT", "/api/v1/machines/\(original.id)/engine", body: ["engine": "unknown"])
            Issue.record("unknown engine accepted")
        } catch let error as APIError { #expect(error.status == 400) }
        do {
            _ = try await api.setEngine(machine: "missing", engine: .herdr)
            Issue.record("unknown machine accepted")
        } catch let error as APIError { #expect(error.status == 404) }
    }
}
