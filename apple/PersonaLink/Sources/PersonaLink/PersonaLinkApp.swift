import SwiftUI

@main
struct PersonaLinkApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    private let model = LinkModel.shared

    var body: some Scene {
        MenuBarExtra {
            MenuView(model: model)
        } label: {
            MenuBarIcon(model: model)
        }
        .menuBarExtraStyle(.window)

        // A `jarvis://link?…` / `persona://link?…` link opened anywhere (Notes, AirDrop, Mail, Safari) opens this window
        // with the link filled in (Info.plist CFBundleURLTypes); pairing still waits for the 「配对」 (Pair) button.
        Window("配对 / 修改连接", id: "pair") {
            PairView(model: model)
                .onOpenURL { url in
                    model.openLink(url)
                    NSApp.activate(ignoringOtherApps: true)
                }
        }
        .windowResizability(.contentSize)
        .defaultLaunchBehavior(.suppressed)
        .handlesExternalEvents(matching: ["*"])
    }
}

/// The menu bar icon: a template symbol, so it reads on light and dark menu bars alike.
struct MenuBarIcon: View {
    let model: LinkModel

    var body: some View {
        Image(systemName: model.look.symbol)
            .accessibilityLabel("Persona Link")
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        let args = CommandLine.arguments
        if let i = args.firstIndex(of: "--snapshot"), i + 1 < args.count {
            // Render the menu and the pairing window, light and dark, and quit (scripts/app/mac-link.sh snapshot).
            MainActor.assumeIsolated { Snapshots.render(to: URL(fileURLWithPath: args[i + 1])) }
            exit(0)
        }
        MainActor.assumeIsolated { LinkModel.shared.start() }
    }

    func applicationWillTerminate(_ notification: Notification) {
        MainActor.assumeIsolated { LinkModel.shared.stopHelper() }
    }
}
