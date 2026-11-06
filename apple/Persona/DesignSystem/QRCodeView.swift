import SwiftUI

/// A QR code to scan with another app (a connector's sign-in step, `ConnectorPrompt.qr`): the modules the hub sends,
/// rows of "0" / "1", drawn dark on a light ground in both appearances with the quiet zone cameras need.
struct QRCodeView: View {
    var rows: [String]

    /// Modules of light ground around the code (the QR standard's quiet zone).
    static let quietZone = 4

    var body: some View {
        Canvas { ctx, size in
            let side = min(size.width, size.height)
            let n = CGFloat(rows.count + 2 * Self.quietZone)
            let cell = side / n
            ctx.fill(Path(CGRect(x: 0, y: 0, width: side, height: side)), with: .color(Theme.Scan.ground))
            var dark = Path()
            for (y, row) in rows.enumerated() {
                for (x, c) in row.enumerated() where c == "1" {
                    dark.addRect(CGRect(x: CGFloat(x + Self.quietZone) * cell, y: CGFloat(y + Self.quietZone) * cell,
                                        width: cell, height: cell))
                }
            }
            ctx.fill(dark, with: .color(Theme.Scan.module))
        }
        .aspectRatio(1, contentMode: .fit)
        .accessibilityElement()
        .accessibilityLabel("QR Code")
    }

    /// The code as a picture to save or share (scanned from the photo library by the other app).
    @MainActor static func image(_ rows: [String]) -> UIImage? {
        let r = ImageRenderer(content: QRCodeView(rows: rows))
        r.proposedSize = ProposedViewSize(width: Theme.Size.box300, height: Theme.Size.box300)
        r.scale = 3
        return r.uiImage
    }
}
