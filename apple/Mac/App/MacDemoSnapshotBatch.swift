#if DEBUG
import AppKit
import SwiftUI

@MainActor enum MacDemoSnapshotBatch {
    struct Plan: Decodable {
        var demoScenes: [Scene]
        var output: URL
    }

    struct Scene: Decodable {
        var name: String
        var session: String?
        var permission: Bool?
    }

    enum Failure: Error { case invalidPlan, notReady, focusChanged, capture }

    static func plan(at path: String) -> Plan? {
        guard let data = try? Data(contentsOf: URL(fileURLWithPath: path)) else { return nil }
        return try? JSONDecoder().decode(Plan.self, from: data)
    }

    static func run(_ plan: Plan) async {
        do {
            let model = AppModel.shared
            let output = plan.output.standardizedFileURL
            guard model.isDemo, NSApp.activationPolicy() == .prohibited,
                  output.isFileURL, output.host == nil || output.host == "" || output.host == "localhost",
                  output.path.hasPrefix("/tmp/") || output.path.hasPrefix("/private/tmp/"),
                  !FileManager.default.fileExists(atPath: output.path), !plan.demoScenes.isEmpty,
                  plan.demoScenes.allSatisfy({ !$0.name.isEmpty && !$0.name.contains("/") && $0.name != "." && $0.name != ".." })
            else { throw Failure.invalidPlan }
            try FileManager.default.createDirectory(at: output, withIntermediateDirectories: true)
            for appearance in ["light", "dark"] {
                MacRichSnapshotBatch.shared.scheme = appearance == "dark" ? .dark : .light
                NSApp.appearance = NSAppearance(named: appearance == "dark" ? .darkAqua : .aqua)
                for scene in plan.demoScenes {
                    MacShellState.shared.section = nil
                    model.selectedTab = .chats
                    UserDefaults.standard.set(scene.permission == true, forKey: "showSamplePermission")
                    if let session = scene.session {
                        let pieces = session.split(separator: "/", maxSplits: 1)
                        guard pieces.count == 2 else { throw Failure.invalidPlan }
                        let key = SessionKey(machineID: String(pieces[0]), sessionID: String(pieces[1]))
                        guard DemoHub.shared.sessionKeys.contains(key) else { throw Failure.invalidPlan }
                        model.openChat = key
                    } else {
                        model.openChat = nil
                    }
                    try await Task.sleep(for: .seconds(1))
                    let deadline = ContinuousClock.now.advanced(by: .seconds(20))
                    var window: NSWindow?
                    while ContinuousClock.now < deadline {
                        guard !NSApp.isActive,
                              NSWorkspace.shared.frontmostApplication?.processIdentifier != getpid()
                        else { throw Failure.focusChanged }
                        window = NSApp.windows.first { $0.identifier?.rawValue.hasPrefix("main") == true && $0.contentView != nil && !($0 is NSPanel) }
                        if let window, !model.sessionsNow.isEmpty {
                            let size = NSSize(width: 1280, height: 900)
                            if window.contentView?.bounds.size != size { window.setContentSize(size) }
                            if !window.isVisible { window.orderBack(nil) }
                            break
                        }
                        try await Task.sleep(for: .milliseconds(200))
                    }
                    guard let window, !window.isKeyWindow else { throw Failure.notReady }
                    try await Task.sleep(for: .seconds(3))
                    guard !NSApp.isActive, !window.isKeyWindow else { throw Failure.focusChanged }
                    let url = output.appending(path: "mac-\(scene.name)-\(appearance).png")
                    guard MacSnapshot.capture(to: url, window: window) else { throw Failure.capture }
                    FileHandle.standardError.write(Data("demo snapshot wrote \(url.lastPathComponent)\n".utf8))
                }
            }
            FileHandle.standardError.write(Data("RESULT Mac demo snapshots: \(plan.demoScenes.count * 2) captures; no activation\n".utf8))
            NSApp.terminate(nil)
            exit(0)
        } catch {
            FileHandle.standardError.write(Data("RESULT Mac demo snapshots: FAILED \(error)\n".utf8))
            NSApp.terminate(nil)
            exit(1)
        }
    }
}
#endif
