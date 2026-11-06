import AppKit
import SwiftUI

/// Pairing on a Mac (iOS scans the hub's QR code, `Views/Settings/SettingsView.swift`): there is no camera scanner
/// here, so the same place takes the `persona://pair?…` link from the pasteboard.
enum DataScannerViewController {
    static let isSupported = true
    static let isAvailable = true
}

/// Stands where the iPhone's camera view is: a paste button for the pairing link, one row of the grouped form (the
/// camera's square would leave a tall empty card). `onCode` gets the link, as the scanner hands over a scanned code.
struct QRScanner: View {
    var onCode: (String) -> Void
    @State private var failed = false

    var body: some View {
        VStack(spacing: Theme.Space.s) {
            Button {
                let s = NSPasteboard.general.string(forType: .string)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
                if ServerConfig(pairURL: s) != nil {
                    failed = false
                    onCode(s)
                } else {
                    failed = true
                }
            } label: {
                Label("Paste Pairing Link", systemImage: Theme.Icon.paste).lineLimit(1)
            }
            .themedButton(prominent: true)
            .controlSize(.large)
            if failed {
                Text("The clipboard doesn't contain a pairing link").font(Theme.Font.caption).foregroundStyle(Theme.danger)
            }
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, Theme.Space.l)
    }
}
