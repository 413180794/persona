import SwiftUI

// What PersonaFace.swift reads from the rest of the app, for rendering it on the Mac outside the app (main.swift).
// Only 需要你's ring reads StatusPalette (the icons are idle), and 团子 its hex colors.
enum StatusPalette {
    struct Tone { let dot: Color }
    static let needsYou = Tone(dot: .red)
}

extension Color {
    /// StatusPalette.swift's, for 团子's colors.
    init(hex: UInt32) {
        self.init(red: Double((hex >> 16) & 0xff) / 255, green: Double((hex >> 8) & 0xff) / 255, blue: Double(hex & 0xff) / 255)
    }
}
