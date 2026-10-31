import AppKit
import SwiftUI

/// Pairing on a Mac (iOS scans the QR code `jarvisd hub pair` prints, `Views/Settings/SettingsView.swift`): there is
/// no camera scanner here, so the same place takes the printed `jarvis://pair?…` link from the pasteboard.
enum DataScannerViewController {
    static let isSupported = true
    static let isAvailable = true
}

/// Stands where the iPhone's camera view is: a paste button for the pairing link. `onCode` gets the link, as the
/// scanner hands over a scanned code.
struct QRScanner: View {
    var onCode: (String) -> Void
    @State private var failed = false

    var body: some View {
        VStack(spacing: Theme.Space.m) {
            Image(systemName: Theme.Icon.paste)
                .font(Theme.Font.display)
                .foregroundStyle(.secondary)
            Button {
                let s = NSPasteboard.general.string(forType: .string)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
                if ServerConfig(pairURL: s) != nil {
                    failed = false
                    onCode(s)
                } else {
                    failed = true
                }
            } label: {
                Label("粘贴配对链接", systemImage: Theme.Icon.paste).lineLimit(1)
            }
            .themedButton(prominent: true)
            .keyboardShortcut("v", modifiers: .command)
            if failed {
                Text("剪贴板里不是配对链接").font(Theme.Font.caption).foregroundStyle(Theme.danger)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
