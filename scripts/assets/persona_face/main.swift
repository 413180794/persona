import AppKit
import SwiftUI

// A built-in persona's face as a PNG, drawn by the app's own PersonaFace (apple/Shared/Design/PersonaFace.swift), so
// the home screen icon is the face the app shows. Built (with shim.swift and PersonaFace.swift) and run by make_alt_icons.py:
//   render <design> <accent RRGGBB> <light|dark> <side px> <out.png>
let args = CommandLine.arguments
guard args.count == 6, let design = PersonaFace.Design(rawValue: args[1]), let hex = UInt32(args[2], radix: 16),
      let side = Double(args[4]) else {
    FileHandle.standardError.write("usage: render <design> <RRGGBB> <light|dark> <side> <out.png>\n".data(using: .utf8)!)
    exit(2)
}
let accent = Color(red: Double((hex >> 16) & 0xff) / 255, green: Double((hex >> 8) & 0xff) / 255, blue: Double(hex & 0xff) / 255)
let out = URL(fileURLWithPath: args[5])
MainActor.assumeIsolated {
    let face = PersonaFace(size: side, design: design, state: .idle, tint: accent, mark: "sparkle")
        .frame(width: side, height: side)
        .environment(\.colorScheme, args[3] == "dark" ? .dark : .light)
    let renderer = ImageRenderer(content: face)
    renderer.scale = 1
    guard let image = renderer.cgImage,
          let png = NSBitmapImageRep(cgImage: image).representation(using: .png, properties: [:]) else { exit(1) }
    do { try png.write(to: out) } catch { exit(1) }
}
