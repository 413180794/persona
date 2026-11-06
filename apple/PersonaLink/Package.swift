// swift-tools-version: 6.0
// Persona Link: the menu bar app around persona-link on a Mac (docs/link.md). scripts/app/mac-link.sh builds the .app.
import PackageDescription

let package = Package(
    name: "PersonaLink",
    platforms: [.macOS(.v15)],
    targets: [
        .executableTarget(name: "PersonaLink", path: "Sources/PersonaLink"),
        .testTarget(name: "PersonaLinkTests", dependencies: ["PersonaLink"], path: "Tests/PersonaLinkTests")
    ],
    swiftLanguageModes: [.v5]
)
