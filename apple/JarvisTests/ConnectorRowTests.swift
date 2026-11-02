import Foundation
import Testing
@testable import Jarvis

/// 设置 › 连接器's rows from the hub's list (`ConnectorRow.rows`, migration M6): mail and Feishu keep their rows and
/// their place whether the hub lists them or not.
@MainActor struct ConnectorRowTests {
    private func c(_ id: String) -> HubConnector { HubConnector(id: id, name: id, status: "ok") }

    @Test func aHubThatListsThemAll() {
        let rows = ConnectorRow.rows([c("lark"), c("mail"), c("github"), c("imessage")])
        #expect(rows.map(\.id) == ["mail", "lark", "github", "imessage"])
        #expect(rows.first == .mail && rows[1] == .lark, "drawn as their own rows, not as a hub connector")
    }

    @Test func aHubFromBeforeOrNoListYet() {
        #expect(ConnectorRow.rows([]).map(\.id) == ["mail", "lark"])
        #expect(ConnectorRow.rows([c("github")]).map(\.id) == ["mail", "lark", "github"])
    }

    @Test func aHubWithoutMail() {
        #expect(ConnectorRow.rows([c("lark"), c("github")]).map(\.id) == ["lark", "github"])
    }
}
