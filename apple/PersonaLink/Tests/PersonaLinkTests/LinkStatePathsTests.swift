import Foundation
import XCTest
@testable import PersonaLink

final class LinkStatePathsTests: XCTestCase {
    func testExplicitRootsAndDefinedEmptyPrecedence() throws {
        let home = URL(fileURLWithPath: "/tmp/example-home")
        for (input, expected) in [("~/profile", "/tmp/example-home/profile"), ("profile/../chosen", "/tmp/example-home/chosen"),
                                  ("~", "/tmp/example-home"), ("/var/lib/a/../state", "/var/lib/state"), ("profile space", "/tmp/example-home/profile space")] {
            XCTAssertEqual(LinkStatePaths.root(home: home, environment: ["PERSONA_HOME": input, "JARVIS_HOME": "wrong"]).path, expected)
            XCTAssertEqual(LinkStatePaths.root(home: home, environment: ["JARVIS_HOME": input]).path, expected)
        }
        XCTAssertEqual(LinkStatePaths.root(home: home, environment: ["PERSONA_HOME": "", "JARVIS_HOME": "wrong"]).path, home.path + "/.persona")
    }

    func testLoginUsesTheResidentNodesStoredRootWithoutReadingCredentials() throws {
        let home = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: home) }
        let plist = home.appendingPathComponent("Library/LaunchAgents/com.jarvis.node.plist")
        try FileManager.default.createDirectory(at: plist.deletingLastPathComponent(), withIntermediateDirectories: true)
        let stored = ["EnvironmentVariables": ["JARVIS_HOME": "~/profile with spaces", "UNRELATED": "not-imported"]]
        let data = try PropertyListSerialization.data(fromPropertyList: stored, format: .xml, options: 0)
        try data.write(to: plist)
        let environment = LinkStatePaths.serviceEnvironment(home: home, environment: ["PATH": "/usr/bin"])
        XCTAssertEqual(environment, ["PATH": "/usr/bin", "JARVIS_HOME": "~/profile with spaces"])
        XCTAssertEqual(LinkStatePaths.root(home: home, environment: environment).path, home.path + "/profile with spaces")
        XCTAssertFalse(FileManager.default.fileExists(atPath: home.path + "/profile with spaces"))
        let explicit = ["PERSONA_HOME": "", "JARVIS_HOME": "wrong"]
        XCTAssertEqual(LinkStatePaths.serviceEnvironment(home: home, environment: explicit), explicit)
    }

    func testDefaultAliasesAndIndependentRootsDoNotMoveData() throws {
        let home = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: home) }
        let current = home.appendingPathComponent(".persona", isDirectory: true), legacy = home.appendingPathComponent(".jarvis", isDirectory: true)
        XCTAssertEqual(LinkStatePaths.root(home: home, environment: [:]), current)
        XCTAssertFalse(FileManager.default.fileExists(atPath: current.path))
        try FileManager.default.createDirectory(at: legacy, withIntermediateDirectories: true)
        XCTAssertEqual(LinkStatePaths.root(home: home, environment: [:]), legacy)
        try FileManager.default.createSymbolicLink(at: current, withDestinationURL: legacy)
        XCTAssertEqual(LinkStatePaths.root(home: home, environment: [:]), current)
        try FileManager.default.removeItem(at: current)
        try FileManager.default.createDirectory(at: current, withIntermediateDirectories: true)
        XCTAssertEqual(LinkStatePaths.root(home: home, environment: [:]), legacy)
        try FileManager.default.removeItem(at: legacy)
        try FileManager.default.createSymbolicLink(atPath: legacy.path, withDestinationPath: "missing")
        XCTAssertEqual(LinkStatePaths.root(home: home, environment: [:]), legacy)
        XCTAssertTrue(FileManager.default.fileExists(atPath: current.path))
    }
}
